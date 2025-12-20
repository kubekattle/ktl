//go:build linux

// File: internal/workflows/buildsvc/sandbox_linux.go
// Brief: Internal buildsvc package implementation for 'sandbox linux'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/example/ktl/pkg/buildkit"
)

func getSandboxInjector() sandboxInjector {
	return maybeReexecInSandbox
}

func maybeReexecInSandbox(ctx context.Context, opts *Options, streams Streams, contextAbs string) (bool, error) {
	if sandboxDisabled() && !opts.RequireSandbox {
		return false, nil
	}
	if sandboxActive() {
		return false, nil
	}
	bin := opts.SandboxBin
	if bin == "" {
		bin = "nsjail"
	}
	if _, err := exec.LookPath(bin); err != nil {
		if opts.SandboxConfig != "" || opts.RequireSandbox {
			return false, fmt.Errorf("sandbox binary not found: %w", err)
		}
		return false, nil
	}

	configPath := opts.SandboxConfig
	if configPath == "" {
		path, err := ensureDefaultSandboxConfig()
		if err != nil {
			return false, err
		}
		configPath = path
	}
	if _, err := os.Stat(configPath); err != nil {
		return false, fmt.Errorf("sandbox config: %w", err)
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = buildkit.DefaultCacheDir()
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return false, fmt.Errorf("create cache dir: %w", err)
	}

	builderAddr := opts.Builder
	if builderAddr == "" {
		builderAddr = buildkit.DefaultBuilderAddress()
	}

	exe, err := os.Executable()
	if err != nil {
		return false, fmt.Errorf("resolve executable: %w", err)
	}

	sandboxExe, err := ensureSandboxExecutable(exe, cacheDir)
	if err != nil {
		return false, err
	}

	homeDir, _ := os.UserHomeDir()
	binds := buildSandboxBinds(contextAbs, cacheDir, builderAddr, sandboxExe, homeDir, opts.SandboxBinds)

	logDir := filepath.Join(cacheDir, "sandbox-logs")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		return false, fmt.Errorf("sandbox logs dir: %w", err)
	}
	logPath := filepath.Join(logDir, fmt.Sprintf("sandbox-%d.log", time.Now().UnixNano()))

	args := []string{"--config", configPath, "--log", logPath}
	if !opts.SandboxLogs {
		args = append(args, "--quiet")
	}
	if opts.SandboxWorkdir != "" {
		args = append(args, "--cwd", opts.SandboxWorkdir)
	}
	for _, bind := range binds {
		args = append(args, bind.flag, bind.spec)
	}
	args = append(args, "--")
	args = append(args, sandboxExe)
	args = append(args, os.Args[1:]...)

	sandboxCmd := exec.Command(bin, args...)
	sandboxCmd.Stdin = streams.InReader()
	sandboxCmd.Stdout = streams.OutWriter()
	sandboxCmd.Stderr = streams.ErrWriter()
	env := append(os.Environ(),
		sandboxActiveEnvKey+"=1",
		legacySandboxActiveEnvKey+"=1",
	)
	env = append(env,
		fmt.Sprintf("%s=%s", sandboxContextEnvKey, contextAbs),
		fmt.Sprintf("%s=%s", legacySandboxContextEnvKey, contextAbs),
		fmt.Sprintf("%s=%s", sandboxCacheEnvKey, cacheDir),
		fmt.Sprintf("%s=%s", legacySandboxCacheEnvKey, cacheDir),
		fmt.Sprintf("%s=%s", sandboxBuilderEnvKey, builderAddr),
		fmt.Sprintf("%s=%s", legacySandboxBuilderEnvKey, builderAddr),
		fmt.Sprintf("%s=%s", sandboxLogPathEnvKey, logPath),
		fmt.Sprintf("%s=%s", legacySandboxLogPathEnv, logPath),
	)
	sandboxCmd.Env = env

	var stopSandboxLogs func()
	if opts.SandboxLogs {
		stop, streamErr := startSandboxLogStreamer(ctx, logPath, streams.ErrWriter(), nil)
		if streamErr != nil {
			fmt.Fprintf(streams.ErrWriter(), "sandbox logs unavailable: %v\n", streamErr)
		} else {
			stopSandboxLogs = stop
		}
	}

	if err := sandboxCmd.Start(); err != nil {
		return false, fmt.Errorf("start sandbox runtime: %w", err)
	}

	sigCh := make(chan os.Signal, 4)
	forwardSignals := []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGTSTP, syscall.SIGQUIT}
	signal.Notify(sigCh, forwardSignals...)
	stopForward := make(chan struct{})
	forwardDone := make(chan struct{})
	go func() {
		defer close(forwardDone)
		for {
			select {
			case sig := <-sigCh:
				if sig == nil {
					continue
				}
				if sandboxCmd.Process != nil {
					_ = sandboxCmd.Process.Signal(sig)
				}
			case <-stopForward:
				return
			}
		}
	}()

	runErr := sandboxCmd.Wait()
	close(stopForward)
	signal.Stop(sigCh)
	<-forwardDone

	if stopSandboxLogs != nil {
		stopSandboxLogs()
	}

	if runErr != nil && !opts.SandboxLogs {
		if data, err := os.ReadFile(logPath); err == nil {
			trimmed := strings.TrimSpace(string(data))
			if trimmed != "" {
				fmt.Fprintln(streams.ErrWriter(), "[sandbox] sandbox runtime logs:")
				for _, line := range strings.Split(trimmed, "\n") {
					line = strings.TrimSpace(line)
					if line == "" {
						continue
					}
					fmt.Fprintf(streams.ErrWriter(), "[sandbox] %s\n", line)
				}
			}
		}
	}

	if logPath != "" {
		_ = os.Remove(logPath)
	}
	if runErr != nil {
		var exitErr *exec.ExitError
		if errors.As(runErr, &exitErr) && exitErr.ProcessState != nil {
			os.Exit(exitErr.ProcessState.ExitCode())
		}
		return false, runErr
	}
	if sandboxCmd.ProcessState != nil {
		os.Exit(sandboxCmd.ProcessState.ExitCode())
	}
	os.Exit(0)
	return true, nil
}

func ensureSandboxExecutable(exePath, cacheDir string) (string, error) {
	if strings.TrimSpace(exePath) == "" {
		return "", errors.New("sandbox: executable path is empty")
	}
	if strings.TrimSpace(cacheDir) == "" {
		return "", errors.New("sandbox: cache dir is empty")
	}
	dir := filepath.Join(cacheDir, "sandbox-bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("sandbox: create executable dir: %w", err)
	}
	target := filepath.Join(dir, "ktl")

	in, err := os.Open(exePath)
	if err != nil {
		return "", fmt.Errorf("sandbox: open executable: %w", err)
	}
	defer in.Close()

	tmp := target + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return "", fmt.Errorf("sandbox: stage executable: %w", err)
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		_ = os.Remove(tmp)
		return "", fmt.Errorf("sandbox: copy executable: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("sandbox: close staged executable: %w", err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("sandbox: finalize executable: %w", err)
	}
	return target, nil
}
