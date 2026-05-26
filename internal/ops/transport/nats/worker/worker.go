package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsgo "github.com/nats-io/nats.go"
)

type Config struct {
	Server       string
	Subject      string
	Queue        string
	Creds        string
	NKey         string
	Timeout      time.Duration
	ShellBinary  string
	RedactValues []string
	Runner       transport.Runner
	Ready        chan<- struct{}
}

// Worker subscribes to one NATS assignment subject and executes supported
// command assignments through the local transport contract.
type Worker struct {
	server   string
	subject  string
	queue    string
	creds    string
	nkey     string
	timeout  time.Duration
	shell    string
	redactor transport.Redactor
	runner   transport.Runner
	ready    chan<- struct{}
}

func New(config Config) (*Worker, error) {
	subject := natstransport.NormalizeTarget(config.Subject)
	if subject == "" {
		return nil, fmt.Errorf("nats worker subject is required")
	}
	if strings.ContainsAny(subject, " \t\r\n") {
		return nil, fmt.Errorf("nats worker subject must not contain whitespace")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	runner := config.Runner
	if runner == nil {
		runner = transport.ExecRunner{}
	}
	server := natstransport.ServerOrDefault(config.Server)
	creds := strings.TrimSpace(config.Creds)
	nkey := strings.TrimSpace(config.NKey)
	redactValues := append([]string(nil), config.RedactValues...)
	redactValues = append(redactValues, subject, server, creds, nkey)
	return &Worker{
		server:   server,
		subject:  subject,
		queue:    strings.TrimSpace(config.Queue),
		creds:    creds,
		nkey:     nkey,
		timeout:  timeout,
		shell:    strings.TrimSpace(config.ShellBinary),
		redactor: transport.NewRedactor(redactValues),
		runner:   runner,
		ready:    config.Ready,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	opts, err := natstransport.ConnectOptions(natstransport.DialConfig{
		Server:  w.server,
		Creds:   w.creds,
		NKey:    w.nkey,
		Timeout: w.timeout,
		Name:    "torque-agent-nats-worker",
	})
	if err != nil {
		return err
	}
	conn, err := natsgo.Connect(w.server, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()

	handler := func(msg *natsgo.Msg) {
		result := w.HandleMessage(ctx, msg.Data)
		raw, err := json.Marshal(result)
		if err != nil {
			raw, _ = json.Marshal(w.errorResult("run", "", fmt.Errorf("marshal operation result: %w", err), false))
		}
		if msg.Reply != "" {
			_ = msg.Respond(raw)
		}
	}
	if w.queue != "" {
		_, err = conn.QueueSubscribe(w.subject, w.queue, handler)
	} else {
		_, err = conn.Subscribe(w.subject, handler)
	}
	if err != nil {
		return err
	}
	if err := conn.Flush(); err != nil {
		return err
	}
	if w.ready != nil {
		close(w.ready)
	}
	<-ctx.Done()
	if err := conn.Drain(); err != nil && !errors.Is(ctx.Err(), context.Canceled) {
		return err
	}
	return nil
}

func (w *Worker) HandleMessage(ctx context.Context, raw []byte) transport.OperationResult {
	assignment, err := natstransport.ParseCommandAssignment(raw)
	if err != nil {
		return w.errorResult("run", "", err, false)
	}
	return w.HandleAssignment(ctx, assignment)
}

func (w *Worker) HandleAssignment(ctx context.Context, assignment natstransport.CommandAssignment) transport.OperationResult {
	operation := strings.TrimSpace(assignment.Operation)
	target := natstransport.NormalizeTarget(assignment.Target)
	if target != w.subject {
		return w.errorResult(operation, target, fmt.Errorf("assignment target %q does not match worker subject %q", target, w.subject), false)
	}
	client, err := localtransport.New(localtransport.Config{
		Target:       "local://" + w.subject,
		ShellBinary:  w.shell,
		Timeout:      w.timeout,
		RedactValues: append([]string{w.subject, w.server}, w.redactorValues()...),
		Runner:       w.runner,
	})
	if err != nil {
		return w.errorResult(operation, target, err, false)
	}
	var result transport.OperationResult
	switch operation {
	case "connect":
		result = client.Connect(ctx)
	case "run":
		if strings.TrimSpace(assignment.Command) == "" {
			return w.errorResult(operation, target, fmt.Errorf("run assignment command is required"), false)
		}
		result = client.Run(ctx, assignment.Command)
	default:
		return w.errorResult(operation, target, fmt.Errorf("unsupported assignment operation %q", operation), false)
	}
	result.Operation = operation
	result.TargetDigest = natstransport.TargetDigest(target)
	result.Stdout = w.redactor.RedactString(result.Stdout)
	result.Stderr = w.redactor.RedactString(result.Stderr)
	result.Error = w.redactor.RedactString(result.Error)
	result.Command = w.redactor.RedactArgs(result.Command)
	return result
}

func (w *Worker) errorResult(operation string, target string, err error, timedOut bool) transport.OperationResult {
	if strings.TrimSpace(operation) == "" {
		operation = "run"
	}
	if strings.TrimSpace(target) == "" {
		target = w.subject
	}
	status := "failed"
	if timedOut {
		status = "timeout"
	}
	return transport.OperationResult{
		Operation:    strings.TrimSpace(operation),
		Status:       status,
		TargetDigest: natstransport.TargetDigest(target),
		Command:      w.redactor.RedactArgs([]string{"torque-agent", "nats", "worker", "--subject", w.subject}),
		ExitCode:     1,
		TimedOut:     timedOut,
		Error:        w.redactor.RedactString(err.Error()),
	}
}

func (w *Worker) redactorValues() []string {
	return []string{w.subject, w.server, w.creds, w.nkey}
}
