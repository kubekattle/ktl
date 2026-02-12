// File: cmd/ktl-agent/main.go
// Brief: Remote agent CLI entrypoint.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kubekattle/ktl/internal/agent"
	"github.com/kubekattle/ktl/internal/workflows/buildsvc"
)

func main() {
	listen := flag.String("listen", ":7443", "gRPC listen address (host:port)")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig for log/traffic services")
	kubeContext := flag.String("context", "", "Kubeconfig context for log/traffic services")
	token := flag.String("token", "", "Authentication token required for all RPCs (optional; sent as `authorization: Bearer <token>`)")
	httpListen := flag.String("http-listen", "", "HTTP listen address for the mirror gateway (optional; exposes /api/v1/mirror/*)")
	tlsCert := flag.String("tls-cert", "", "TLS certificate PEM file for gRPC (optional; enables TLS when set with -tls-key)")
	tlsKey := flag.String("tls-key", "", "TLS private key PEM file for gRPC (optional; enables TLS when set with -tls-cert)")
	tlsClientCA := flag.String("tls-client-ca", "", "Client CA bundle PEM file for mTLS (optional; when set, client certs are required)")
	mirrorStore := flag.String("mirror-store", "", "Path to the SQLite flight recorder DB for MirrorService (optional; enables ListSessions/Export and durable replay)")
	mirrorMaxSessions := flag.Int("mirror-max-sessions", 0, "Max number of mirror sessions to retain in the flight recorder (0 = unlimited)")
	mirrorMaxFrames := flag.Uint64("mirror-max-frames", 0, "Max frames to retain per mirror session in the flight recorder (0 = unlimited)")
	mirrorMaxBytes := flag.Int64("mirror-max-bytes", 0, "Soft cap for retained mirror DB size in bytes (0 = unlimited; best-effort)")
	mirrorPruneInterval := flag.Duration("mirror-prune-interval", 0, "How often to enforce mirror retention (0 = default)")
	flag.Parse()

	cfg := agent.Config{
		ListenAddr:                *listen,
		KubeconfigPath:            *kubeconfig,
		KubeContext:               *kubeContext,
		AuthToken:                 *token,
		HTTPListenAddr:            *httpListen,
		TLSCertFile:               *tlsCert,
		TLSKeyFile:                *tlsKey,
		TLSClientCAFile:           *tlsClientCA,
		MirrorStore:               *mirrorStore,
		MirrorMaxSessions:         *mirrorMaxSessions,
		MirrorMaxFramesPerSession: *mirrorMaxFrames,
		MirrorMaxBytes:            *mirrorMaxBytes,
		MirrorPruneInterval:       *mirrorPruneInterval,
	}
	srv, err := agent.New(cfg, buildsvc.New(buildsvc.Dependencies{}))
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	if err := srv.Run(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
