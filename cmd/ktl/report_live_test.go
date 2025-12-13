package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/report"
)

func TestLiveServerHandleReport(t *testing.T) {
	opts := report.NewOptions()
	srv := newLiveServer(&kube.Client{}, opts, io.Discard)
	now := time.Now().UTC()
	srv.collectFn = func(ctx context.Context, _ *kube.Client, _ *report.Options) (report.Data, []string, error) {
		data := report.Data{}
		data.GeneratedAt = now
		data.ClusterServer = "https://cluster"
		return data, []string{"default"}, nil
	}
	srv.collectEventsFn = func(ctx context.Context, _ *kube.Client, _ []string, _ int) ([]report.EventEntry, error) {
		return []report.EventEntry{{Type: "Normal", Reason: "Synced", Message: "ok", Timestamp: now}}, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	srv.handleReport(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", rr.Code)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "ktl Namespace Report") {
		t.Fatalf("expected body to contain report title, got %q", body)
	}
	if !strings.Contains(body, "Last 50 events") {
		t.Fatalf("expected body to mention events panel")
	}
}

func TestLiveServerHandleReportError(t *testing.T) {
	opts := report.NewOptions()
	srv := newLiveServer(&kube.Client{}, opts, io.Discard)
	srv.collectFn = func(ctx context.Context, _ *kube.Client, _ *report.Options) (report.Data, []string, error) {
		return report.Data{}, nil, context.DeadlineExceeded
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rr := httptest.NewRecorder()

	srv.handleReport(rr, req)

	if rr.Code != http.StatusBadGateway {
		t.Fatalf("expected status 502, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "collect report data") {
		t.Fatalf("expected error message in body, got %q", rr.Body.String())
	}
}

func TestLiveServerTrimmedVsFull(t *testing.T) {
	opts := report.NewOptions()
	srv := newLiveServer(&kube.Client{}, opts, io.Discard)
	now := time.Now().UTC()
	var include bool
	srv.collectFn = func(ctx context.Context, _ *kube.Client, opt *report.Options) (report.Data, []string, error) {
		include = opt.IncludeIngress
		return report.Data{GeneratedAt: now}, []string{"default"}, nil
	}
	srv.collectEventsFn = func(ctx context.Context, _ *kube.Client, _ []string, _ int) ([]report.EventEntry, error) {
		return nil, nil
	}

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	srv.handleReport(httptest.NewRecorder(), req)
	if include {
		t.Fatalf("expected lite request to disable ingress collection")
	}

	reqFull := httptest.NewRequest(http.MethodGet, "/?full=1", nil)
	srv.handleReport(httptest.NewRecorder(), reqFull)
	if !include {
		t.Fatalf("expected full request to enable ingress collection")
	}
}

func TestLiveServerCaching(t *testing.T) {
	opts := report.NewOptions()
	srv := newLiveServer(&kube.Client{}, opts, io.Discard)
	srv.cacheTTL = time.Second
	var calls int32
	now := time.Now().UTC()
	srv.collectFn = func(ctx context.Context, _ *kube.Client, _ *report.Options) (report.Data, []string, error) {
		atomic.AddInt32(&calls, 1)
		return report.Data{GeneratedAt: now}, []string{"default"}, nil
	}
	srv.collectEventsFn = func(ctx context.Context, _ *kube.Client, _ []string, _ int) ([]report.EventEntry, error) {
		return nil, nil
	}

	if _, err := srv.getSnapshot(context.Background(), false); err != nil {
		t.Fatalf("getSnapshot failed: %v", err)
	}
	if _, err := srv.getSnapshot(context.Background(), false); err != nil {
		t.Fatalf("getSnapshot second call failed: %v", err)
	}
	if c := atomic.LoadInt32(&calls); c != 1 {
		t.Fatalf("expected collector to run once, ran %d times", c)
	}
}

func TestLiveServerSSE(t *testing.T) {
	opts := report.NewOptions()
	srv := newLiveServer(&kube.Client{}, opts, io.Discard)
	srv.cacheTTL = 5 * time.Millisecond
	srv.requestTimeout = 50 * time.Millisecond
	var counter int64
	srv.collectFn = func(ctx context.Context, _ *kube.Client, _ *report.Options) (report.Data, []string, error) {
		value := atomic.AddInt64(&counter, 1)
		return report.Data{GeneratedAt: time.Unix(value, 0)}, []string{"default"}, nil
	}
	srv.collectEventsFn = func(ctx context.Context, _ *kube.Client, _ []string, _ int) ([]report.EventEntry, error) {
		return nil, nil
	}

	rec := newSSERecorder()
	req := httptest.NewRequest(http.MethodGet, "/events/live", nil)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req = req.WithContext(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		srv.handleEvents(rec, req)
	}()

	time.Sleep(20 * time.Millisecond)
	cancel()
	<-done

	if !strings.Contains(rec.Buffer.String(), "data: ") {
		t.Fatalf("expected SSE output, got %q", rec.Buffer.String())
	}
}

type sseRecorder struct {
	bytes.Buffer
	header http.Header
}

func newSSERecorder() *sseRecorder {
	return &sseRecorder{header: make(http.Header)}
}

func (s *sseRecorder) Header() http.Header { return s.header }

func (s *sseRecorder) Write(b []byte) (int, error) { return s.Buffer.Write(b) }

func (s *sseRecorder) WriteHeader(statusCode int) {}

func (s *sseRecorder) Flush() {}
