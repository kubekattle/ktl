// report_live.go serves the 'ktl diag report --live' HTTP endpoint, streaming refreshed HTML reports over SSE/Web.
package main

import (
	"compress/gzip"
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/report"
	"github.com/gorilla/mux"
	"github.com/spf13/cobra"
	"golang.org/x/sync/singleflight"
)

const (
	defaultLiveEventsLimit    = 50
	defaultLiveRequestTimeout = 30 * time.Second
	defaultLiveCacheTTL       = 5 * time.Second
)

type liveServer struct {
	client          *kube.Client
	opts            *report.Options
	eventsLimit     int
	requestTimeout  time.Duration
	stderr          io.Writer
	cacheTTL        time.Duration
	cacheMu         sync.RWMutex
	cache           map[liveCacheKey]cachedSnapshot
	sf              singleflight.Group
	collectFn       func(context.Context, *kube.Client, *report.Options) (report.Data, []string, error)
	collectEventsFn func(context.Context, *kube.Client, []string, int) ([]report.EventEntry, error)
}

type liveCacheKey struct {
	includeIngress bool
}

type cachedSnapshot struct {
	snapshot snapshot
	expires  time.Time
}

type snapshot struct {
	data       report.Data
	namespaces []string
	hash       string
}

func newLiveServer(client *kube.Client, opts *report.Options, stderr io.Writer) *liveServer {
	return &liveServer{
		client:          client,
		opts:            opts,
		eventsLimit:     defaultLiveEventsLimit,
		requestTimeout:  defaultLiveRequestTimeout,
		cacheTTL:        defaultLiveCacheTTL,
		cache:           make(map[liveCacheKey]cachedSnapshot),
		stderr:          stderr,
		collectFn:       report.Collect,
		collectEventsFn: report.CollectRecentEvents,
	}
}

func serveLiveReports(ctx context.Context, cmd *cobra.Command, client *kube.Client, opts *report.Options, listenAddr string) error {
	srv := newLiveServer(client, opts, cmd.ErrOrStderr())

	router := mux.NewRouter()
	router.Handle("/", srv.withGzip(http.HandlerFunc(srv.handleReport))).Methods(http.MethodGet)
	router.HandleFunc("/healthz", srv.handleHealth).Methods(http.MethodGet)
	router.HandleFunc("/events/live", srv.handleEvents).Methods(http.MethodGet)

	server := &http.Server{
		Addr:              listenAddr,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
	}

	shutdownErr := make(chan error, 1)
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		shutdownErr <- server.Shutdown(shutdownCtx)
	}()

	fmt.Fprintf(cmd.OutOrStdout(), "Live report server listening on %s\n", listenAddr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	select {
	case err := <-shutdownErr:
		return err
	default:
		return nil
	}
}

func (s *liveServer) handleHealth(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("ok"))
}

func (s *liveServer) handleReport(w http.ResponseWriter, r *http.Request) {
	includeIngress := r.URL.Query().Get("full") == "1"
	snap, err := s.getSnapshot(r.Context(), includeIngress)
	if err != nil {
		s.writeError(w, http.StatusBadGateway, fmt.Errorf("collect report data: %w", err))
		return
	}

	html, err := report.RenderHTML(snap.data)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("render html: %w", err))
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store, max-age=0")
	w.WriteHeader(http.StatusOK)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
	_, _ = io.WriteString(w, html)
	if f, ok := w.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *liveServer) writeError(w http.ResponseWriter, code int, err error) {
	http.Error(w, err.Error(), code)
	s.logf("error: %v", err)
}

func (s *liveServer) logf(format string, args ...interface{}) {
	if s.stderr == nil {
		return
	}
	fmt.Fprintf(s.stderr, format+"\n", args...)
}

func (s *liveServer) getSnapshot(ctx context.Context, includeIngress bool) (snapshot, error) {
	key := liveCacheKey{includeIngress: includeIngress}
	now := time.Now()
	s.cacheMu.RLock()
	if cached, ok := s.cache[key]; ok && now.Before(cached.expires) {
		s.cacheMu.RUnlock()
		return cached.snapshot, nil
	}
	s.cacheMu.RUnlock()

	result, err, _ := s.sf.Do(fmt.Sprintf("collect-%t", includeIngress), func() (interface{}, error) {
		optsCopy := *s.opts
		optsCopy.IncludeIngress = includeIngress
		ctxCollect, cancelCollect := s.contextWithTimeout(ctx)
		defer cancelCollect()
		data, namespaces, err := s.collectFn(ctxCollect, s.client, &optsCopy)
		if err != nil {
			return snapshot{}, err
		}
		ctxEvents, cancelEvents := s.contextWithTimeout(ctx)
		defer cancelEvents()
		events, err := s.collectEventsFn(ctxEvents, s.client, namespaces, s.eventsLimit)
		if err != nil {
			s.logf("warn: unable to load recent events: %v", err)
		} else {
			for i := range events {
				if events[i].Timestamp.IsZero() {
					continue
				}
				events[i].Age = report.HumanDuration(data.GeneratedAt.Sub(events[i].Timestamp))
			}
			data.RecentEvents = events
		}
		snap := snapshot{
			data:       data,
			namespaces: append([]string(nil), namespaces...),
			hash:       hashReportData(data),
		}
		s.cacheMu.Lock()
		s.cache[key] = cachedSnapshot{snapshot: snap, expires: time.Now().Add(s.cacheTTL)}
		s.cacheMu.Unlock()
		return snap, nil
	})
	if err != nil {
		return snapshot{}, err
	}
	return result.(snapshot), nil
}

func (s *liveServer) contextWithTimeout(parent context.Context) (context.Context, context.CancelFunc) {
	if s.requestTimeout <= 0 {
		return parent, func() {}
	}
	return context.WithTimeout(parent, s.requestTimeout)
}

func hashReportData(data report.Data) string {
	h := sha1.New()
	fmt.Fprintf(h, "%s|%d|%d|%d|%d|%d", data.GeneratedAt.UTC().Format(time.RFC3339Nano), data.Summary.TotalNamespaces, data.Summary.TotalPods, data.Summary.ReadyPods, data.Summary.TotalContainers, data.Summary.TotalRestarts)
	for _, ns := range data.Namespaces {
		fmt.Fprintf(h, "|%s|%d|%d|%d|%t", ns.Name, len(ns.Pods), len(ns.Ingresses), ns.PodSecurityFindings, ns.HasPodSecurityIssues)
		for _, pod := range ns.Pods {
			fmt.Fprintf(h, "|%s|%s|%s|%d|%d|%s", pod.Name, pod.Phase, pod.Node, pod.Restarts, pod.MaxRestarts, pod.ReadyCount)
		}
	}
	for _, ev := range data.RecentEvents {
		fmt.Fprintf(h, "|%s|%s|%s|%d|%d", ev.Namespace, ev.Type, ev.Reason, ev.Count, ev.Timestamp.UnixNano())
	}
	for _, check := range data.Scorecard.Checks {
		fmt.Fprintf(h, "|%s|%.2f|%.2f", check.Key, check.Score, check.Delta)
	}
	return hex.EncodeToString(h.Sum(nil))
}

func (s *liveServer) withGzip(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
			next.ServeHTTP(w, r)
			return
		}
		gzw := newGzipResponseWriter(w)
		gzw.Header().Set("Content-Encoding", "gzip")
		defer gzw.Close()
		next.ServeHTTP(gzw, r)
	})
}

type gzipResponseWriter struct {
	http.ResponseWriter
	writer *gzip.Writer
}

func newGzipResponseWriter(w http.ResponseWriter) *gzipResponseWriter {
	return &gzipResponseWriter{
		ResponseWriter: w,
		writer:         gzip.NewWriter(w),
	}
}

func (g *gzipResponseWriter) Header() http.Header {
	h := g.ResponseWriter.Header()
	h.Set("Content-Encoding", "gzip")
	return h
}

func (g *gzipResponseWriter) Write(b []byte) (int, error) {
	return g.writer.Write(b)
}

func (g *gzipResponseWriter) Flush() {
	_ = g.writer.Flush()
	if f, ok := g.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (g *gzipResponseWriter) Close() error {
	return g.writer.Close()
}

func (s *liveServer) handleEvents(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.writeError(w, http.StatusInternalServerError, fmt.Errorf("streaming not supported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	includeIngress := r.URL.Query().Get("full") == "1"
	ticker := time.NewTicker(s.cacheTTL)
	defer ticker.Stop()
	lastHash := ""

	for {
		ctx := r.Context()
		snap, err := s.getSnapshot(ctx, includeIngress)
		if err != nil {
			s.logf("warn: live events snapshot error: %v", err)
		} else if snap.hash != lastHash {
			lastHash = snap.hash
			fmt.Fprintf(w, "data: %s\n\n", snap.hash)
			flusher.Flush()
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
