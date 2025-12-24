// File: internal/stack/run.go
// Brief: Stack runner (apply/delete orchestration).

package stack

import (
	"context"
	"fmt"
	"io"
)

type RunOptions struct {
	Command     string
	Plan        *Plan
	Concurrency int
	FailFast    bool
	AutoApprove bool

	Kubeconfig      *string
	KubeContext     *string
	LogLevel        *string
	RemoteAgentAddr *string
}

func Run(ctx context.Context, opts RunOptions, out io.Writer, errOut io.Writer) error {
	_ = ctx
	_ = out
	_ = errOut
	_ = opts
	return fmt.Errorf("ktl stack %s is not implemented yet (plan works; runner is next)", opts.Command)
}
