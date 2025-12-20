//go:build !windows

package buildkit

import (
	"os"
	"os/signal"
	"syscall"

	"github.com/containerd/console"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
)

func startResizeMonitor(proc gateway.ContainerProcess, tty console.Console) func() {
	if proc == nil || tty == nil {
		return func() {}
	}
	applyTTYResize(proc, tty)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGWINCH)

	stopCh := make(chan struct{})
	go func() {
		for {
			select {
			case <-stopCh:
				return
			case <-sigCh:
				applyTTYResize(proc, tty)
			}
		}
	}()

	return func() {
		close(stopCh)
		signal.Stop(sigCh)
	}
}
