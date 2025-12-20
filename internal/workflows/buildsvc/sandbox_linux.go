//go:build linux

// File: internal/workflows/buildsvc/sandbox_linux.go
// Brief: Internal buildsvc package implementation for 'sandbox linux'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/example/ktl/pkg/buildkit"
)

func getSandboxInjector() sandboxInjector {
	return maybeReexecInSandbox
}

func maybeReexecInSandbox(opts *Options, streams Streams, contextAbs string) (bool, error) {
	if sandboxDisabled() {
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
		if opts.SandboxConfig != "" {
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

	homeDir, _ := os.UserHomeDir()
	binds := buildSandboxBinds(contextAbs, cacheDir, builderAddr, exe, homeDir, opts.SandboxBinds)

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
	args = append(args, exe)
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
