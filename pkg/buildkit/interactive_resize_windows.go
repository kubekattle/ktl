//go:build windows

package buildkit

import (
	"github.com/containerd/console"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
)

func startResizeMonitor(proc gateway.ContainerProcess, tty console.Console) func() {
	if proc == nil || tty == nil {
		return func() {}
	}
	applyTTYResize(proc, tty)
	return func() {}
}
