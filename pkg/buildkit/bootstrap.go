package buildkit

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"

	"github.com/moby/buildkit/client"
)

const dockerFallbackBuilderName = "ktl-buildkit"

var dockerLookPath = exec.LookPath
var dockerBuildxRunner = runDockerBuildxImpl
var dockerVersionRunner = runDockerVersionImpl
var dockerContextLister = listDockerContextsImpl

var dockerEnvMu sync.Mutex

type dockerFallbackMemo struct {
	mu       sync.Mutex
	resolved bool
	addr     string
	context  string
	err      error
}

var dockerFallback dockerFallbackMemo

type buildkitClientFactory struct {
	allowFallback bool
	logWriter     io.Writer
	dockerContext string
}

func (f buildkitClientFactory) new(ctx context.Context, addr string) (*client.Client, string, error) {
	c, err := dialBuildkitWithDockerContext(ctx, addr, f.dockerContext)
	if err == nil {
		return c, addr, nil
	}
	if !f.allowFallback || !isDialError(err) {
		return nil, addr, fmt.Errorf("connect to buildkitd at %s: %w", addr, err)
	}
	fallbackAddr, fallbackCtx, fbErr := ensureDockerBackedBuilder(ctx, f.logWriter, f.dockerContext)
	if fbErr != nil {
		return nil, addr, fmt.Errorf("connect to buildkitd at %s and fallback failed: %w", addr, errors.Join(err, fbErr))
	}
	if f.dockerContext == "" {
		f.dockerContext = fallbackCtx
	}

	c, err = dialBuildkitWithDockerContext(ctx, fallbackAddr, f.dockerContext)
	if err != nil {
		return nil, fallbackAddr, fmt.Errorf("connect to buildkitd at %s after fallback: %w", fallbackAddr, err)
	}

	return c, fallbackAddr, nil
}

func ensureDockerBackedBuilder(ctx context.Context, logWriter io.Writer, dockerContext string) (string, string, error) {
	dockerFallback.mu.Lock()
	defer dockerFallback.mu.Unlock()
	if dockerFallback.resolved {
		return dockerFallback.addr, dockerFallback.context, dockerFallback.err
	}
	dockerFallback.resolved = true

	if _, err := dockerLookPath("docker"); err != nil {
		dockerFallback.err = fmt.Errorf("docker CLI not found: %w", err)
		return "", "", dockerFallback.err
	}

	selectedCtx, err := ensureDockerDaemonReachable(ctx, logWriter, dockerContext)
	if err != nil {
		dockerFallback.err = err
		return "", "", dockerFallback.err
	}
	dockerFallback.context = selectedCtx

	builder := dockerFallbackBuilderName
	if logWriter != nil {
		fmt.Fprintf(logWriter, "BuildKit endpoint unavailable; provisioning Docker Buildx builder %s...\n", builder)
	}
	if err := dockerBuildxRunner(ctx, logWriter, selectedCtx, "inspect", builder); err != nil {
		if err := dockerBuildxRunner(ctx, logWriter, selectedCtx, "create", "--name", builder, "--driver", "docker-container"); err != nil {
			dockerFallback.err = err
			return "", selectedCtx, dockerFallback.err
		}
	}

	if err := dockerBuildxRunner(ctx, logWriter, selectedCtx, "inspect", "--bootstrap", builder); err != nil {
		dockerFallback.err = err
		return "", selectedCtx, dockerFallback.err
	}
	if logWriter != nil {
		fmt.Fprintf(logWriter, "Using Docker Buildx builder %s\n", builder)
	}

	containerName := fmt.Sprintf("buildx_buildkit_%s0", builder)
	dockerFallback.addr = fmt.Sprintf("docker-container://%s", containerName)
	return dockerFallback.addr, selectedCtx, dockerFallback.err
}

func runDockerBuildxImpl(ctx context.Context, logWriter io.Writer, dockerContext string, args ...string) error {
	dockerArgs := []string{}
	if dockerContext != "" {
		dockerArgs = append(dockerArgs, "--context", dockerContext)
	}
	dockerArgs = append(dockerArgs, "buildx")
	dockerArgs = append(dockerArgs, args...)
	cmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		if logWriter != nil && buf.Len() > 0 {
			_, _ = logWriter.Write(buf.Bytes())
		}
		return fmt.Errorf("docker buildx %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runDockerVersionImpl(ctx context.Context, dockerContext string) error {
	args := []string{}
	if dockerContext != "" {
		args = append(args, "--context", dockerContext)
	}
	args = append(args, "version")
	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func listDockerContextsImpl(ctx context.Context) ([]string, error) {
	cmd := exec.CommandContext(ctx, "docker", "context", "ls", "--format", "{{.Name}}")
	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("docker context ls: %w", err)
	}
	lines := strings.Split(buf.String(), "\n")
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		out = append(out, name)
	}
	return out, nil
}

func ensureDockerDaemonReachable(ctx context.Context, logWriter io.Writer, dockerContext string) (string, error) {
	// If the user explicitly set a context, don't try to outsmart them.
	if dockerContext != "" {
		if err := dockerVersionRunner(ctx, dockerContext); err != nil {
			return dockerContext, err
		}
		return dockerContext, nil
	}

	if err := dockerVersionRunner(ctx, ""); err == nil {
		return "", nil
	}

	// On macOS it's common to have multiple backends (Docker Desktop, Colima).
	// If the default context is down, try to find any context that is reachable.
	contexts, listErr := dockerContextLister(ctx)
	if listErr != nil || len(contexts) == 0 {
		if listErr != nil {
			return "", fmt.Errorf("docker daemon unavailable and contexts cannot be listed: %w", listErr)
		}
		return "", fmt.Errorf("docker daemon unavailable (no docker contexts found)")
	}

	preferred := []string{"colima", "desktop-linux", "default"}
	seen := make(map[string]struct{}, len(contexts))
	ordered := make([]string, 0, len(contexts))
	for _, name := range preferred {
		for _, ctxName := range contexts {
			if ctxName != name {
				continue
			}
			if _, ok := seen[ctxName]; ok {
				continue
			}
			seen[ctxName] = struct{}{}
			ordered = append(ordered, ctxName)
		}
	}
	for _, ctxName := range contexts {
		if _, ok := seen[ctxName]; ok {
			continue
		}
		seen[ctxName] = struct{}{}
		ordered = append(ordered, ctxName)
	}

	for _, ctxName := range ordered {
		if err := dockerVersionRunner(ctx, ctxName); err == nil {
			if logWriter != nil {
				fmt.Fprintf(logWriter, "Docker default context unavailable; using docker context %s\n", ctxName)
			}
			return ctxName, nil
		}
	}

	return "", fmt.Errorf("docker daemon is unavailable in all Docker contexts (%s)", strings.Join(ordered, ", "))
}

func dialBuildkitWithDockerContext(ctx context.Context, addr string, dockerContext string) (*client.Client, error) {
	if dockerContext != "" && strings.HasPrefix(addr, "docker-container://") {
		var c *client.Client
		var err error
		withDockerContextEnv(dockerContext, func() {
			c, err = dialBuildkit(ctx, addr)
		})
		return c, err
	}
	return dialBuildkit(ctx, addr)
}

func dialBuildkit(ctx context.Context, addr string) (*client.Client, error) {
	c, err := client.New(ctx, addr)
	if err != nil {
		return nil, err
	}
	if err := probeBuildkit(ctx, c); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

func withDockerContextEnv(dockerContext string, fn func()) {
	dockerEnvMu.Lock()
	defer dockerEnvMu.Unlock()

	orig, hadOrig := os.LookupEnv("DOCKER_CONTEXT")
	_ = os.Setenv("DOCKER_CONTEXT", dockerContext)
	defer func() {
		if hadOrig {
			_ = os.Setenv("DOCKER_CONTEXT", orig)
		} else {
			_ = os.Unsetenv("DOCKER_CONTEXT")
		}
	}()
	fn()
}

func probeBuildkit(ctx context.Context, c *client.Client) error {
	_, err := c.ListWorkers(ctx)
	return err
}

func isDialError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, os.ErrNotExist) {
		return true
	}
	var sysErr *os.SyscallError
	if errors.As(err, &sysErr) {
		if sysErr.Err == syscall.ENOENT || sysErr.Err == syscall.ECONNREFUSED || sysErr.Err == syscall.EACCES {
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	substrings := []string{
		"no such file or directory",
		"connect: connection refused",
		"connection refused",
		"error while dialing",
		"connect: permission denied",
	}
	for _, sub := range substrings {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}
