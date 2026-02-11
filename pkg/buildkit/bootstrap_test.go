package buildkit

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"strings"
	"syscall"
	"testing"
)

func TestIsDialError(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "grpc unix missing",
			err:  errors.New("transport: Error while dialing: dial unix /run/user/0/buildkit/buildkitd.sock: connect: no such file or directory"),
			want: true,
		},
		{
			name: "wrapped econ refused",
			err:  &os.SyscallError{Syscall: "connect", Err: syscall.ECONNREFUSED},
			want: true,
		},
		{
			name: "generic error",
			err:  errors.New("some other failure"),
			want: false,
		},
		{
			name: "nil",
			err:  nil,
			want: false,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isDialError(tc.err); got != tc.want {
				t.Fatalf("isDialError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestEnsureDockerBackedBuilder_CachesResult(t *testing.T) {
	dockerFallback.mu.Lock()
	dockerFallback.resolved = false
	dockerFallback.addr = ""
	dockerFallback.err = nil
	dockerFallback.mu.Unlock()

	origLookPath := dockerLookPath
	t.Cleanup(func() { dockerLookPath = origLookPath })
	dockerLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	origRunner := dockerBuildxRunner
	t.Cleanup(func() { dockerBuildxRunner = origRunner })

	origVersionRunner := dockerVersionRunner
	t.Cleanup(func() { dockerVersionRunner = origVersionRunner })
	dockerVersionRunner = func(_ context.Context, _ string) error { return nil }

	var calls []string
	dockerBuildxRunner = func(_ context.Context, _ io.Writer, dockerContext string, args ...string) error {
		calls = append(calls, dockerContext+"|"+strings.Join(args, " "))
		if len(args) == 2 && args[0] == "inspect" && args[1] == dockerFallbackBuilderName {
			return errors.New("missing builder")
		}
		return nil
	}

	var buf bytes.Buffer
	addr1, _, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() err = %v", err)
	}
	addr2, _, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() (cached) err = %v", err)
	}
	if addr1 != addr2 {
		t.Fatalf("addresses differ: %q != %q", addr1, addr2)
	}
	if want := 3; len(calls) != want {
		t.Fatalf("docker buildx calls = %d, want %d (%v)", len(calls), want, calls)
	}
	if got := buf.String(); strings.Count(got, "provisioning Docker Buildx builder") != 1 || strings.Count(got, "Using Docker Buildx builder") != 1 {
		t.Fatalf("unexpected log output:\n%s", got)
	}
}

func TestEnsureDockerBackedBuilder_PicksWorkingDockerContext(t *testing.T) {
	dockerFallback.mu.Lock()
	dockerFallback.resolved = false
	dockerFallback.addr = ""
	dockerFallback.err = nil
	dockerFallback.mu.Unlock()

	origLookPath := dockerLookPath
	t.Cleanup(func() { dockerLookPath = origLookPath })
	dockerLookPath = func(_ string) (string, error) { return "/usr/bin/docker", nil }

	origBuildxRunner := dockerBuildxRunner
	t.Cleanup(func() { dockerBuildxRunner = origBuildxRunner })

	origVersionRunner := dockerVersionRunner
	t.Cleanup(func() { dockerVersionRunner = origVersionRunner })

	origContextLister := dockerContextLister
	t.Cleanup(func() { dockerContextLister = origContextLister })

	t.Cleanup(func() { _ = os.Unsetenv("DOCKER_CONTEXT") })
	_ = os.Unsetenv("DOCKER_CONTEXT")

	dockerVersionRunner = func(_ context.Context, dockerContext string) error {
		if dockerContext == "colima" {
			return nil
		}
		return errors.New("cannot connect")
	}
	dockerContextLister = func(_ context.Context) ([]string, error) {
		return []string{"desktop-linux", "colima"}, nil
	}
	dockerBuildxRunner = func(_ context.Context, _ io.Writer, _ string, _ ...string) error { return nil }

	var buf bytes.Buffer
	_, selected, err := ensureDockerBackedBuilder(context.Background(), &buf, "")
	if err != nil {
		t.Fatalf("ensureDockerBackedBuilder() err = %v", err)
	}
	if selected != "colima" {
		t.Fatalf("selected context = %q, want %q", selected, "colima")
	}
	if got := buf.String(); !strings.Contains(got, "using docker context colima") {
		t.Fatalf("expected context selection log, got:\n%s", got)
	}
}
