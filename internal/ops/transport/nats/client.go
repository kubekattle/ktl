package natstransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const defaultNATSBinary = "nats"

type Config struct {
	Target       string
	Server       string
	Creds        string
	NKey         string
	NATSBinary   string
	ExtraArgs    []string
	Timeout      time.Duration
	RedactValues []string
	Runner       transport.Runner
}

// Client sends command assignments over a NATS request/reply subject and
// expects the worker response to be an OperationResult JSON receipt.
type Client struct {
	target   string
	server   string
	creds    string
	nkey     string
	binary   string
	extra    []string
	timeout  time.Duration
	redactor transport.Redactor
	runner   transport.Runner
}

type Runner = transport.Runner
type RunOutput = transport.RunOutput
type OperationResult = transport.OperationResult
type Redactor = transport.Redactor

type commandAssignment struct {
	APIVersion string `json:"apiVersion"`
	Kind       string `json:"kind"`
	Operation  string `json:"operation"`
	Target     string `json:"target"`
	Command    string `json:"command,omitempty"`
	SentAt     string `json:"sentAt"`
}

func New(config Config) (*Client, error) {
	target := NormalizeTarget(config.Target)
	if target == "" {
		return nil, fmt.Errorf("nats target subject is required")
	}
	if strings.ContainsAny(target, " \t\r\n") {
		return nil, fmt.Errorf("nats target subject must not contain whitespace")
	}
	binary := strings.TrimSpace(config.NATSBinary)
	if binary == "" {
		binary = defaultNATSBinary
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := config.Runner
	if runner == nil {
		runner = transport.ExecRunner{}
	}
	server := strings.TrimSpace(config.Server)
	creds := strings.TrimSpace(config.Creds)
	nkey := strings.TrimSpace(config.NKey)
	redactValues := append([]string(nil), config.RedactValues...)
	redactValues = append(redactValues, target, server, creds, nkey)
	return &Client{
		target:   target,
		server:   server,
		creds:    creds,
		nkey:     nkey,
		binary:   binary,
		extra:    append([]string(nil), config.ExtraArgs...),
		timeout:  timeout,
		redactor: transport.NewRedactor(redactValues),
		runner:   runner,
	}, nil
}

func NormalizeTarget(target string) string {
	target = strings.TrimSpace(target)
	target = strings.TrimPrefix(target, "nats-mesh://")
	target = strings.TrimPrefix(target, "nats://")
	return target
}

func TargetDigest(target string) string {
	return transport.ValueDigest(NormalizeTarget(target))
}

func (c *Client) TargetDigest() string {
	return TargetDigest(c.target)
}

func (c *Client) Connect(ctx context.Context) OperationResult {
	return c.request(ctx, "connect", "")
}

func (c *Client) Run(ctx context.Context, command string) OperationResult {
	return c.request(ctx, "run", command)
}

func (c *Client) request(ctx context.Context, operation string, command string) OperationResult {
	started := time.Now()
	assignment := commandAssignment{
		APIVersion: "torque.dev/nats-assignment/v1",
		Kind:       "CommandAssignment",
		Operation:  operation,
		Target:     c.target,
		Command:    command,
		SentAt:     started.UTC().Format(time.RFC3339Nano),
	}
	rawAssignment, err := json.Marshal(assignment)
	if err != nil {
		return c.resultFromError(operation, started, err)
	}
	args := c.requestArgs(string(rawAssignment))
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	output, runErr := c.runner.Run(runCtx, c.binary, args)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	result, parsed := c.parseWorkerResponse(output.Stdout)
	if !parsed {
		status := "succeeded"
		if timedOut {
			status = "timeout"
		} else if runErr != nil || output.ExitCode != 0 {
			status = "failed"
		}
		result = transport.OperationResult{
			Operation: operation,
			Status:    status,
			Stdout:    c.redactor.RedactString(string(output.Stdout)),
			Stderr:    c.redactor.RedactString(string(output.Stderr)),
			ExitCode:  output.ExitCode,
			TimedOut:  timedOut,
		}
	}
	result.Operation = firstNonEmpty(result.Operation, operation)
	result.TargetDigest = firstNonEmpty(result.TargetDigest, c.TargetDigest())
	result.Command = c.redactor.RedactArgs(append([]string{c.binary}, args...))
	result.Stdout = c.redactor.RedactString(result.Stdout)
	result.Stderr = c.redactor.RedactString(result.Stderr)
	result.Error = c.redactor.RedactString(result.Error)
	result.TimedOut = result.TimedOut || timedOut
	if timedOut {
		result.Status = "timeout"
	}
	if result.Status == "" {
		if runErr != nil || output.ExitCode != 0 {
			result.Status = "failed"
		} else {
			result.Status = "succeeded"
		}
	}
	if result.ExitCode == 0 && output.ExitCode != 0 {
		result.ExitCode = output.ExitCode
	}
	if runErr != nil && result.Error == "" {
		result.Error = c.redactor.RedactString(runErr.Error())
	}
	result.DurationMillis = time.Since(started).Milliseconds()
	return result
}

func (c *Client) requestArgs(payload string) []string {
	args := []string{"request", "--raw"}
	if c.server != "" {
		args = append(args, "--server", c.server)
	}
	if c.creds != "" {
		args = append(args, "--creds", c.creds)
	}
	if c.nkey != "" {
		args = append(args, "--nkey", c.nkey)
	}
	args = append(args, "--timeout", c.timeout.String())
	args = append(args, c.extra...)
	args = append(args, c.target, payload)
	return args
}

func (c *Client) parseWorkerResponse(raw []byte) (transport.OperationResult, bool) {
	body := strings.TrimSpace(string(raw))
	if body == "" {
		return transport.OperationResult{}, false
	}
	var result transport.OperationResult
	if err := json.Unmarshal([]byte(body), &result); err == nil && (result.Status != "" || result.Operation != "") {
		return result, true
	}
	var wrapper struct {
		OperationResult *transport.OperationResult `json:"operationResult,omitempty"`
		Receipt         *transport.OperationResult `json:"receipt,omitempty"`
	}
	if err := json.Unmarshal([]byte(body), &wrapper); err != nil {
		return transport.OperationResult{}, false
	}
	if wrapper.OperationResult != nil {
		return *wrapper.OperationResult, true
	}
	if wrapper.Receipt != nil {
		return *wrapper.Receipt, true
	}
	return transport.OperationResult{}, false
}

func (c *Client) resultFromError(operation string, started time.Time, err error) OperationResult {
	return OperationResult{
		Operation:      operation,
		Status:         "failed",
		TargetDigest:   c.TargetDigest(),
		Command:        []string{c.binary, "request", c.target},
		ExitCode:       1,
		DurationMillis: time.Since(started).Milliseconds(),
		Error:          c.redactor.RedactString(err.Error()),
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func ShellQuote(value string) string {
	return transport.ShellQuote(value)
}
