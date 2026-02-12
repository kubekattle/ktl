package helpui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"strings"
	"time"

	_ "embed"

	"github.com/go-logr/logr"
	"github.com/kubekattle/ktl/internal/version"
	"github.com/spf13/cobra"
)

type Server struct {
	addr     string
	root     *cobra.Command
	logger   logr.Logger
	template *template.Template
	all      bool
}

type Option func(*Server)

func WithAll() Option {
	return func(s *Server) {
		if s != nil {
			s.all = true
		}
	}
}

func New(addr string, root *cobra.Command, logger logr.Logger, opts ...Option) *Server {
	addr = strings.TrimSpace(addr)
	server := &Server{
		addr:     addr,
		root:     root,
		logger:   logger,
		template: template.Must(template.New("help_ui").Parse(helpUIHTML)),
	}
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	return server
}

func (s *Server) Run(ctx context.Context) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/api/index.json", s.handleIndexJSON)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "ok")
	})
	srv := &http.Server{Addr: s.addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	s.logger.V(1).Info("help ui listener ready", "addr", s.addr)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

type templateData struct {
	Title   string
	All     bool
	Version string
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	title := "ktl help"
	if s != nil && s.root != nil {
		title = strings.TrimSpace(s.root.Name()) + " help"
	}
	if s == nil || s.template == nil {
		_, _ = fmt.Fprint(w, title)
		return
	}
	all := s.all
	ver := strings.TrimSpace(version.Get().Version)
	if ver != "" {
		ver = "ktl " + ver
	}
	var buf bytes.Buffer
	_ = s.template.Execute(&buf, templateData{Title: template.HTMLEscapeString(title), All: all, Version: template.HTMLEscapeString(ver)})
	_, _ = w.Write(buf.Bytes())
}

func (s *Server) handleIndexJSON(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.Error(w, "help ui unavailable", http.StatusServiceUnavailable)
		return
	}
	includeHidden := s.all
	if !includeHidden {
		includeHidden = strings.EqualFold(r.URL.Query().Get("all"), "1") || strings.EqualFold(r.URL.Query().Get("all"), "true")
	}
	index := BuildIndex(s.root, includeHidden)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(index)
}

var (
	//go:embed templates/help_ui.html
	helpUIHTML string
)
