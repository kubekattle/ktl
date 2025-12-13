package buildkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"

	gateway "github.com/moby/buildkit/frontend/gateway/client"
	"github.com/moby/buildkit/solver/errdefs"
	"github.com/moby/buildkit/solver/pb"
)

func TestLaunchInteractiveShellBuildsContainerRequest(t *testing.T) {
	exec := &pb.ExecOp{
		Meta: &pb.Meta{
			Args: []string{"/bin/sh", "-c", "echo boom"},
			Env:  []string{"FOO=bar"},
			Cwd:  "/workspace",
		},
		Mounts: []*pb.Mount{{Dest: "/", MountType: pb.MountType_BIND}},
	}
	op := &pb.Op{Op: &pb.Op_Exec{Exec: exec}}
	solveErr := &errdefs.SolveError{Solve: &errdefs.Solve{Op: op, MountIDs: []string{"rootfs"}}, Err: errors.New("boom")}

	gw := &stubGateway{container: &stubContainer{process: &stubProcess{}}}
	stderr := &bytes.Buffer{}
	cfg := &InteractiveShellConfig{
		Shell:  []string{"/bin/sh"},
		Stdin:  bytes.NewBuffer(nil),
		Stdout: io.Discard,
		Stderr: stderr,
	}

	if err := launchInteractiveShell(context.Background(), gw, solveErr, cfg); err != nil {
		t.Fatalf("launchInteractiveShell returned error: %v", err)
	}

	if gw.req.Mounts[0].ResultID != "rootfs" {
		t.Fatalf("expected mount to use rootfs, got %q", gw.req.Mounts[0].ResultID)
	}
	if gw.container.startReq.Cwd != "/workspace" {
		t.Fatalf("cwd mismatch: %s", gw.container.startReq.Cwd)
	}
	if len(gw.container.startReq.Env) == 0 || gw.container.startReq.Env[0] != "FOO=bar" {
		t.Fatalf("env not forwarded: %#v", gw.container.startReq.Env)
	}
	if gw.container.startReq.Args[0] != "/bin/sh" {
		t.Fatalf("shell args not propagated: %#v", gw.container.startReq.Args)
	}
	if stderr.Len() == 0 {
		t.Fatalf("expected intro message on stderr")
	}
}

func TestLaunchInteractiveShellRejectsNonExec(t *testing.T) {
	op := &pb.Op{Op: &pb.Op_File{File: &pb.FileOp{}}}
	solveErr := &errdefs.SolveError{Solve: &errdefs.Solve{Op: op}}
	cfg := &InteractiveShellConfig{Shell: []string{"/bin/sh"}, Stderr: io.Discard}

	err := launchInteractiveShell(context.Background(), &stubGateway{container: &stubContainer{process: &stubProcess{}}}, solveErr, cfg)
	if err == nil || !strings.Contains(err.Error(), "RUN steps") {
		t.Fatalf("expected RUN-only error, got %v", err)
	}
}

type stubGateway struct {
	req       gateway.NewContainerRequest
	container *stubContainer
}

func (s *stubGateway) NewContainer(_ context.Context, req gateway.NewContainerRequest) (gateway.Container, error) {
	s.req = req
	return s.container, nil
}

type stubContainer struct {
	startReq gateway.StartRequest
	process  *stubProcess
}

func (s *stubContainer) Start(_ context.Context, req gateway.StartRequest) (gateway.ContainerProcess, error) {
	s.startReq = req
	if s.process == nil {
		s.process = &stubProcess{}
	}
	return s.process, nil
}

func (s *stubContainer) Release(context.Context) error { return nil }

type stubProcess struct{}

func (stubProcess) Wait() error                                   { return nil }
func (stubProcess) Resize(context.Context, gateway.WinSize) error { return nil }
func (stubProcess) Signal(context.Context, syscall.Signal) error  { return nil }
