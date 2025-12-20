package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/example/ktl/internal/agent"
	"github.com/example/ktl/internal/workflows/buildsvc"
)

func main() {
	listen := flag.String("listen", ":7443", "gRPC listen address (host:port)")
	kubeconfig := flag.String("kubeconfig", "", "Path to kubeconfig for log/traffic services")
	kubeContext := flag.String("context", "", "Kubeconfig context for log/traffic services")
	flag.Parse()

	cfg := agent.Config{
		ListenAddr:     *listen,
		KubeconfigPath: *kubeconfig,
		KubeContext:    *kubeContext,
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
