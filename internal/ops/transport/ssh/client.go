package sshtransport

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	defaultSSHBinary = "ssh"
	defaultSCPBinary = "scp"
)

// Config describes an OpenSSH-backed transport endpoint.
type Config struct {
	Target       string
	IdentityFile string
	SSHBinary    string
	SCPBinary    string
	ExtraArgs    []string
	Timeout      time.Duration
	RedactValues []string
	Runner       transport.Runner
}

// Client runs SSH transport primitives and returns evidence-safe results.
type Client struct {
	target       string
	identityFile string
	sshBinary    string
	scpBinary    string
	extraArgs    []string
	timeout      time.Duration
	redactor     transport.Redactor
	runner       transport.Runner
}

type Runner = transport.Runner
type RunOutput = transport.RunOutput
type OperationResult = transport.OperationResult
type Redactor = transport.Redactor

// New constructs a Client with conservative OpenSSH defaults.
func New(config Config) (*Client, error) {
	target := NormalizeTarget(config.Target)
	if target == "" {
		return nil, fmt.Errorf("ssh target is required")
	}
	if strings.ContainsAny(target, " \t\r\n") {
		return nil, fmt.Errorf("ssh target must not contain whitespace")
	}
	sshBinary := strings.TrimSpace(config.SSHBinary)
	if sshBinary == "" {
		sshBinary = defaultSSHBinary
	}
	scpBinary := strings.TrimSpace(config.SCPBinary)
	if scpBinary == "" {
		scpBinary = defaultSCPBinary
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
		target:       target,
		identityFile: strings.TrimSpace(config.IdentityFile),
		sshBinary:    sshBinary,
		scpBinary:    scpBinary,
		extraArgs:    append([]string(nil), config.ExtraArgs...),
		timeout:      timeout,
		redactor:     transport.NewRedactor(redactValues),
		runner:       runner,
	}, nil
}

func NormalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	return strings.TrimPrefix(target, "ssh://")
}

func TargetDigest(target string) string {
	return transport.ValueDigest(NormalizeTarget(target))
}

func (c *Client) TargetDigest() string {
	return TargetDigest(c.target)
}

func (c *Client) Connect(ctx context.Context) OperationResult {
	return c.run(ctx, "connect", c.sshBinary, c.sshArgs("true"))
}

func (c *Client) Run(ctx context.Context, command string) OperationResult {
	return c.run(ctx, "run", c.sshBinary, c.sshArgs(command))
}

func (c *Client) Upload(ctx context.Context, localPath, remotePath string) OperationResult {
	args := append([]string{"-q"}, c.baseArgs()...)
	args = append(args, localPath, c.remoteSpec(remotePath))
	return c.run(ctx, "upload", c.scpBinary, args)
}

func (c *Client) Download(ctx context.Context, remotePath, localPath string) OperationResult {
	args := append([]string{"-q"}, c.baseArgs()...)
	args = append(args, c.remoteSpec(remotePath), localPath)
	return c.run(ctx, "download", c.scpBinary, args)
}

func (c *Client) sshArgs(command string) []string {
	args := c.baseArgs()
	args = append(args, c.target, command)
	return args
}

func (c *Client) baseArgs() []string {
	args := []string{"-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=accept-new"}
	if c.identityFile != "" {
		args = append(args, "-i", c.identityFile)
	}
	args = append(args, c.extraArgs...)
	return args
}

func (c *Client) remoteSpec(path string) string {
	return c.target + ":" + path
}

func (c *Client) run(ctx context.Context, operation, binary string, args []string) OperationResult {
	started := time.Now()
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, err := c.runner.Run(runCtx, binary, args)
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

func NewRedactor(values []string) Redactor {
	return transport.NewRedactor(values)
}

func ShellQuote(value string) string {
	return transport.ShellQuote(value)
}
