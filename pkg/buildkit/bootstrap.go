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
	"syscall"

	"github.com/moby/buildkit/client"
)

const dockerFallbackBuilderName = "ktl-buildkit"

type buildkitClientFactory struct {
	allowFallback bool
	logWriter     io.Writer
}

func (f buildkitClientFactory) new(ctx context.Context, addr string) (*client.Client, string, error) {
	c, err := dialBuildkit(ctx, addr)
	if err == nil {
		return c, addr, nil
	}
	if !f.allowFallback || !isDialError(err) {
		return nil, addr, fmt.Errorf("connect to buildkitd at %s: %w", addr, err)
	}
	fallbackAddr, fbErr := ensureDockerBackedBuilder(ctx, f.logWriter)
	if fbErr != nil {
		return nil, addr, fmt.Errorf("connect to buildkitd at %s and fallback failed: %w", addr, errors.Join(err, fbErr))
	}

	c, err = dialBuildkit(ctx, fallbackAddr)
	if err != nil {
		return nil, fallbackAddr, fmt.Errorf("connect to buildkitd at %s after fallback: %w", fallbackAddr, err)
	}

	return c, fallbackAddr, nil
}

func ensureDockerBackedBuilder(ctx context.Context, logWriter io.Writer) (string, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return "", fmt.Errorf("docker CLI not found: %w", err)
	}

	builder := dockerFallbackBuilderName
	if logWriter != nil {
		fmt.Fprintf(logWriter, "BuildKit endpoint unavailable; provisioning Docker Buildx builder %s...\n", builder)
	}
	if err := runDockerBuildx(ctx, logWriter, "inspect", builder); err != nil {
		if err := runDockerBuildx(ctx, logWriter, "create", "--name", builder, "--driver", "docker-container"); err != nil {
			return "", err
		}
	}

	if err := runDockerBuildx(ctx, logWriter, "inspect", "--bootstrap", builder); err != nil {
		return "", err
	}
	if logWriter != nil {
		fmt.Fprintf(logWriter, "Using Docker Buildx builder %s\n", builder)
	}

	containerName := fmt.Sprintf("buildx_buildkit_%s0", builder)
	return fmt.Sprintf("docker-container://%s", containerName), nil
}

func runDockerBuildx(ctx context.Context, logWriter io.Writer, args ...string) error {
	cmd := exec.CommandContext(ctx, "docker", append([]string{"buildx"}, args...)...)
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
