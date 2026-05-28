package localtransport

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	defaultTarget = "localhost"
	defaultShell  = "sh"
)

// Config describes a localhost transport endpoint.
type Config struct {
	Target       string
	ShellBinary  string
	Timeout      time.Duration
	RedactValues []string
	Runner       transport.Runner
	LineObserver transport.LineObserver
}

// Client executes transport primitives on the local host and returns the same
// evidence receipt shape as remote transports.
type Client struct {
	target   string
	shell    string
	timeout  time.Duration
	redactor transport.Redactor
	runner   transport.Runner
	observer transport.LineObserver
}

type Runner = transport.Runner
type RunOutput = transport.RunOutput
type OperationResult = transport.OperationResult
type Redactor = transport.Redactor

func New(config Config) (*Client, error) {
	target := NormalizeTarget(config.Target)
	if target == "" {
		target = defaultTarget
	}
	if strings.ContainsAny(target, " \t\r\n") {
		return nil, fmt.Errorf("local target must not contain whitespace")
	}
	shell := strings.TrimSpace(config.ShellBinary)
	if shell == "" {
		shell = defaultShell
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := config.Runner
	if runner == nil {
		runner = transport.ExecRunner{}
	}
	redactValues := append([]string(nil), config.RedactValues...)
	redactValues = append(redactValues, target)
	return &Client{
		target:   target,
		shell:    shell,
		timeout:  timeout,
		redactor: transport.NewRedactor(redactValues),
		runner:   runner,
		observer: config.LineObserver,
	}, nil
}

func NormalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "local://")
	if target == "" {
		return defaultTarget
	}
	return target
}

func TargetDigest(target string) string {
	return transport.ValueDigest(NormalizeTarget(target))
}

func (c *Client) TargetDigest() string {
	return TargetDigest(c.target)
}

func (c *Client) Connect(ctx context.Context) OperationResult {
	return c.run(ctx, "connect", c.shell, []string{"-c", "true"})
}

func (c *Client) Run(ctx context.Context, command string) OperationResult {
	return c.run(ctx, "run", c.shell, []string{"-c", command})
}

func (c *Client) Upload(ctx context.Context, localPath, targetPath string) OperationResult {
	return c.copy(ctx, "upload", localPath, targetPath)
}

func (c *Client) Download(ctx context.Context, sourcePath, localPath string) OperationResult {
	return c.copy(ctx, "download", sourcePath, localPath)
}

func (c *Client) run(ctx context.Context, operation, binary string, args []string) OperationResult {
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.runCommand(runCtx, binary, args)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	status := "succeeded"
	if timedOut {
		status = "timeout"
	} else if err != nil || output.ExitCode != 0 {
		status = "failed"
	}

	result := transport.OperationResult{
		Operation:      operation,
		Status:         status,
		TargetDigest:   c.TargetDigest(),
		Command:        c.redactor.RedactArgs(append([]string{binary}, args...)),
		Stdout:         c.redactor.RedactString(string(output.Stdout)),
		Stderr:         c.redactor.RedactString(string(output.Stderr)),
		ExitCode:       output.ExitCode,
		TimedOut:       timedOut,
		DurationMillis: time.Since(started).Milliseconds(),
	}
	if err != nil {
		result.Error = c.redactor.RedactString(err.Error())
	}
	return result
}

func (c *Client) runCommand(ctx context.Context, binary string, args []string) (transport.RunOutput, error) {
	if streaming, ok := c.runner.(transport.StreamingRunner); ok {
		return streaming.RunStream(ctx, binary, args, c.observer)
	}
	return c.runner.Run(ctx, binary, args)
}

func (c *Client) copy(ctx context.Context, operation, sourcePath, targetPath string) OperationResult {
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	command := []string{"local-copy", sourcePath, targetPath}
	output := transport.RunOutput{ExitCode: 0}
	err := runCtx.Err()
	if err == nil {
		err = copyFile(sourcePath, targetPath)
	}
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	status := "succeeded"
	if timedOut {
		status = "timeout"
	} else if err != nil {
		status = "failed"
		output.ExitCode = 1
	}

	result := transport.OperationResult{
		Operation:      operation,
		Status:         status,
		TargetDigest:   c.TargetDigest(),
		Command:        c.redactor.RedactArgs(command),
		ExitCode:       output.ExitCode,
		TimedOut:       timedOut,
		DurationMillis: time.Since(started).Milliseconds(),
	}
	if err != nil {
		result.Error = c.redactor.RedactString(err.Error())
	}
	return result
}

func copyFile(sourcePath, targetPath string) error {
	in, err := os.Open(sourcePath)
	if err != nil {
		return err
	}
	defer in.Close()

	if err := os.MkdirAll(filepath.Dir(targetPath), 0o755); err != nil {
		return err
	}
	out, err := os.OpenFile(targetPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func NewRedactor(values []string) Redactor {
	return transport.NewRedactor(values)
}

func ShellQuote(value string) string {
	return transport.ShellQuote(value)
}
