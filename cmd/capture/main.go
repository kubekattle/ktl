package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
)

func main() {
	var (
		uiAddr    string
		readOnly  bool
		sessionID string
	)
	flag.StringVar(&uiAddr, "ui", "", "Serve the capture UI at this address (e.g. :8081)")
	flag.BoolVar(&readOnly, "ro", true, "Open the SQLite database read-only")
	flag.StringVar(&sessionID, "session", "", "Session ID to open (optional)")
	flag.Parse()

	args := flag.Args()
	if len(args) != 1 {
		fmt.Fprintln(os.Stderr, "Usage: capture --ui :8081 [--session <id>] <capture.sqlite>")
		os.Exit(2)
	}
	dbPath := strings.TrimSpace(args[0])
	if dbPath == "" {
		fmt.Fprintln(os.Stderr, "Error: capture db path is required")
		os.Exit(2)
	}
	if strings.TrimSpace(uiAddr) == "" {
		fmt.Fprintln(os.Stderr, "Error: --ui is required")
		os.Exit(2)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	srv, err := newServer(serverConfig{
		DBPath:    dbPath,
		ReadOnly:  readOnly,
		UIAddr:    uiAddr,
		SessionID: strings.TrimSpace(sessionID),
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "Serving capture UI on http://%s\n", uiAddr)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	select {
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer shutdownCancel()
		_ = srv.Shutdown(shutdownCtx)
		return
	case err := <-errCh:
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
	}
}

