package buildkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/containerd/console"
	bkclient "github.com/moby/buildkit/client"
	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
)

func runInteractiveDockerfile(ctx context.Context, c *bkclient.Client, solveOpt bkclient.SolveOpt, opts DockerfileBuildOptions, statusCh chan *bkclient.SolveStatus) (*bkclient.SolveResponse, error) {
	if opts.Interactive == nil {
		return c.Solve(ctx, nil, solveOpt, statusCh)
	}

	buildFunc := func(ctx context.Context, gw gateway.Client) (*gateway.Result, error) {
		res, err := gw.Solve(ctx, gateway.SolveRequest{
			Frontend:    solveOpt.Frontend,
			FrontendOpt: solveOpt.FrontendAttrs,
		})
		if err != nil {
			var solveErr *errdefs.SolveError
			if errors.As(err, &solveErr) {
				if shellErr := launchInteractiveShell(ctx, gw, solveErr, opts.Interactive); shellErr != nil {
					logInteractiveMessage(opts.Interactive, fmt.Sprintf("Interactive shell unavailable: %v", shellErr))
				}
			}
			return nil, err
		}
		return res, nil
	}

	return c.Build(ctx, solveOpt, "ktl", buildFunc, statusCh)
}

type containerCreator interface {
	NewContainer(context.Context, gateway.NewContainerRequest) (gateway.Container, error)
}

func launchInteractiveShell(ctx context.Context, gw containerCreator, solveErr *errdefs.SolveError, cfg *InteractiveShellConfig) error {
	if cfg == nil {
		return errors.New("interactive shell config missing")
	}

	req, execOp, err := buildInteractiveContainerRequest(solveErr)
	if err != nil {
		return err
	}

	logInteractiveMessage(cfg, describeInteractiveIntro(execOp, cfg))

	ctr, err := gw.NewContainer(ctx, req)
	if err != nil {
		return fmt.Errorf("create debug container: %w", err)
	}
	defer ctr.Release(ctx)

	ttyConsole, cleanupTTY, err := prepareTTY(cfg)
	if err != nil {
		return err
	}
	if cleanupTTY != nil {
		defer cleanupTTY()
	}

	stdin := readerCloser(cfg.Stdin)
	stdout := writerCloser(cfg.Stdout)
	stderr := writerCloser(cfg.Stderr)

	meta := execOp.GetMeta()
	startReq := gateway.StartRequest{
		Args:                      cfg.Shell,
		Env:                       meta.GetEnv(),
		User:                      meta.GetUser(),
		Cwd:                       meta.GetCwd(),
		Tty:                       ttyConsole != nil,
		Stdin:                     stdin,
		Stdout:                    stdout,
		Stderr:                    stderr,
		SecurityMode:              execOp.GetSecurity(),
		SecretEnv:                 execOp.GetSecretenv(),
		RemoveMountStubsRecursive: meta.GetRemoveMountStubsRecursive(),
	}

	if ttyConsole != nil {
		startReq.Stderr = nil
	}

	proc, err := ctr.Start(ctx, startReq)
	if err != nil {
		return fmt.Errorf("start interactive shell: %w", err)
	}

	if ttyConsole != nil {
		stop := startResizeMonitor(proc, ttyConsole)
		defer stop()
	}

	if err := proc.Wait(); err != nil {
		return fmt.Errorf("interactive shell exited: %w", err)
	}

	return nil
}

func buildInteractiveContainerRequest(solveErr *errdefs.SolveError) (gateway.NewContainerRequest, *pb.ExecOp, error) {
	if solveErr == nil || solveErr.Op == nil {
		return gateway.NewContainerRequest{}, nil, errors.New("missing operation context for interactive shell")
	}
	opExec, ok := solveErr.Op.Op.(*pb.Op_Exec)
	if !ok {
		return gateway.NewContainerRequest{}, nil, fmt.Errorf("interactive shell is limited to RUN steps in the Dockerfile")
	}
	exec := opExec.Exec
	if exec == nil {
		return gateway.NewContainerRequest{}, nil, errors.New("exec metadata missing")
	}
	if len(exec.Mounts) != len(solveErr.MountIDs) {
		return gateway.NewContainerRequest{}, nil, fmt.Errorf("mount metadata mismatch for interactive shell (%d IDs vs %d mounts)", len(solveErr.MountIDs), len(exec.Mounts))
	}

	mounts := make([]gateway.Mount, len(exec.Mounts))
	for i, m := range exec.Mounts {
		mounts[i] = gateway.Mount{
			Selector:  m.Selector,
			Dest:      m.Dest,
			ResultID:  solveErr.MountIDs[i],
			Readonly:  m.Readonly,
			MountType: m.MountType,
			CacheOpt:  m.CacheOpt,
			SecretOpt: m.SecretOpt,
			SSHOpt:    m.SSHOpt,
		}
	}

	meta := exec.GetMeta()
	req := gateway.NewContainerRequest{
		Mounts:      mounts,
		NetMode:     exec.Network,
		ExtraHosts:  meta.GetExtraHosts(),
		Platform:    solveErr.Op.Platform,
		Constraints: solveErr.Op.Constraints,
		Hostname:    meta.GetHostname(),
	}

	return req, exec, nil
}

func describeInteractiveIntro(exec *pb.ExecOp, cfg *InteractiveShellConfig) string {
	meta := exec.GetMeta()
	cmd := strings.Join(meta.GetArgs(), " ")
	if cmd == "" {
		cmd = "<unknown RUN>"
	}
	return fmt.Sprintf("RUN %s failed. Dropping into %s. Exit the shell to resume.", cmd, strings.Join(cfg.Shell, " "))
}

func prepareTTY(cfg *InteractiveShellConfig) (console.Console, func(), error) {
	switch {
	case cfg.Console != nil:
		if err := cfg.Console.SetRaw(); err != nil {
			return nil, nil, fmt.Errorf("configure tty: %w", err)
		}
		return cfg.Console, func() { _ = cfg.Console.Reset() }, nil
	case cfg.TTY != nil:
		tty, err := console.ConsoleFromFile(cfg.TTY)
		if err != nil {
			return nil, nil, fmt.Errorf("access tty: %w", err)
		}
		if err := tty.SetRaw(); err != nil {
			return nil, nil, fmt.Errorf("configure tty: %w", err)
		}
		return tty, func() { _ = tty.Reset() }, nil
	default:
		return nil, nil, nil
	}
}

func readerCloser(r io.Reader) io.ReadCloser {
	if rc, ok := r.(io.ReadCloser); ok {
		return rc
	}
	if r == nil {
		return io.NopCloser(bytes.NewReader(nil))
	}
	return io.NopCloser(r)
}

func writerCloser(w io.Writer) io.WriteCloser {
	if wc, ok := w.(io.WriteCloser); ok {
		return wc
	}
	if w == nil {
		return nopWriteCloser{}
	}
	return nopWriteCloser{Writer: w}
}

type nopWriteCloser struct {
	io.Writer
}

func (n nopWriteCloser) Write(p []byte) (int, error) {
	if n.Writer == nil {
		return len(p), nil
	}
	return n.Writer.Write(p)
}

func (nopWriteCloser) Close() error { return nil }

func logInteractiveMessage(cfg *InteractiveShellConfig, msg string) {
	if cfg == nil || cfg.Stderr == nil || msg == "" {
		return
	}
	fmt.Fprintln(cfg.Stderr, msg)
}
