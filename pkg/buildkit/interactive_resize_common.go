package buildkit

import (
	"context"

	"github.com/containerd/console"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
)

func applyTTYResize(proc gateway.ContainerProcess, tty console.Console) {
	if proc == nil || tty == nil {
		return
	}
	size, err := tty.Size()
	if err != nil {
		return
	}
	_ = proc.Resize(context.Background(), gateway.WinSize{Rows: uint32(size.Height), Cols: uint32(size.Width)})
}
