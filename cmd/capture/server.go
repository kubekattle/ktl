package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type serverConfig struct {
	DBPath    string
	ReadOnly  bool
	UIAddr    string
	SessionID string
}

type server struct {
	cfg serverConfig

	httpServer *http.Server
	store      *sqliteStore
}

func newServer(cfg serverConfig) (*server, error) {
	if strings.TrimSpace(cfg.DBPath) == "" {
		return nil, fmt.Errorf("db path is required")
	}
	if strings.TrimSpace(cfg.UIAddr) == "" {
		return nil, fmt.Errorf("ui address is required")
	}
	st, err := openSQLiteStore(cfg.DBPath, cfg.ReadOnly)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()

	s := &server{
		cfg:   cfg,
		store: st,
		httpServer: &http.Server{
			Addr:              cfg.UIAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
	}

	s.routes(mux)
	return s, nil
}

func (s *server) Run(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.UIAddr)
	if err != nil {
		return err
	}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = s.Shutdown(shutdownCtx)
	}()
	return s.httpServer.Serve(ln)
}

func (s *server) Shutdown(ctx context.Context) error {
	if s.store != nil {
		_ = s.store.Close()
	}
	if s.httpServer != nil {
		return s.httpServer.Shutdown(ctx)
	}
	return nil
}

