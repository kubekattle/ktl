package natstransport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natsgo "github.com/nats-io/nats.go"
)

type Config struct {
	Target                   string
	Server                   string
	Creds                    string
	NKey                     string
	Timeout                  time.Duration
	RedactValues             []string
	TargetID                 string
	ExpectedAgentID          string
	RequiredCapability       string
	NodeKind                 string
	RunID                    string
	NodeID                   string
	PlanDigest               string
	Resource                 json.RawMessage
	SlotLeaseID              string
	SlotLeaseTargetID        string
	SlotLeaseIndex           int
	SlotLeaseSlots           int
	SlotLeaseTTL             string
	SlotLeaseExpiresAt       string
	SlotLeaseToken           string
	SlotLeaseTokenDigest     string
	SlotLeaseRenewInterval   string
	SlotLeaseLedgerStore     string
	SlotLeaseLedgerStorePath string
	SlotLeaseLedgerStoreKey  string
	SlotLeaseEtcdEndpoints   []string
	SlotLeaseEtcdPrefix      string
	Requester                Requester
	Dialer                   RequestDialer
}

// Client sends command assignments over a NATS request/reply subject and
// expects the worker response to be an OperationResult JSON receipt.
type Client struct {
	target   string
	server   string
	creds    string
	nkey     string
	timeout  time.Duration
	metadata CommandAssignmentMetadata
	redactor transport.Redactor

	requester Requester
	dialer    RequestDialer
}

type RunOutput = transport.RunOutput
type OperationResult = transport.OperationResult
type Redactor = transport.Redactor

type Requester interface {
	Request(ctx context.Context, subject string, payload []byte) ([]byte, error)
	Close()
}

type RequestDialer func(ctx context.Context, config DialConfig) (Requester, error)

func New(config Config) (*Client, error) {
	target := NormalizeTarget(config.Target)
	if target == "" {
		return nil, fmt.Errorf("nats target subject is required")
	}
	if strings.ContainsAny(target, " \t\r\n") {
		return nil, fmt.Errorf("nats target subject must not contain whitespace")
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := config.Dialer
	if dialer == nil {
		dialer = defaultRequestDialer
	}
	server := ServerOrDefault(config.Server)
	creds := strings.TrimSpace(config.Creds)
	nkey := strings.TrimSpace(config.NKey)
	redactValues := append([]string(nil), config.RedactValues...)
	redactValues = append(redactValues, target, server, creds, nkey, strings.TrimSpace(config.SlotLeaseToken))
	return &Client{
		target:  target,
		server:  server,
		creds:   creds,
		nkey:    nkey,
		timeout: timeout,
		metadata: CommandAssignmentMetadata{
			TargetID:                 strings.TrimSpace(config.TargetID),
			ExpectedAgentID:          strings.TrimSpace(config.ExpectedAgentID),
			RequiredCapability:       strings.TrimSpace(config.RequiredCapability),
			NodeKind:                 strings.TrimSpace(config.NodeKind),
			RunID:                    strings.TrimSpace(config.RunID),
			NodeID:                   strings.TrimSpace(config.NodeID),
			PlanDigest:               strings.TrimSpace(config.PlanDigest),
			Resource:                 cloneRawMessage(config.Resource),
			SlotLeaseID:              strings.TrimSpace(config.SlotLeaseID),
			SlotLeaseTargetID:        strings.TrimSpace(config.SlotLeaseTargetID),
			SlotLeaseIndex:           config.SlotLeaseIndex,
			SlotLeaseSlots:           config.SlotLeaseSlots,
			SlotLeaseTTL:             strings.TrimSpace(config.SlotLeaseTTL),
			SlotLeaseExpiresAt:       strings.TrimSpace(config.SlotLeaseExpiresAt),
			SlotLeaseToken:           strings.TrimSpace(config.SlotLeaseToken),
			SlotLeaseTokenDigest:     strings.TrimSpace(config.SlotLeaseTokenDigest),
			SlotLeaseRenewInterval:   strings.TrimSpace(config.SlotLeaseRenewInterval),
			SlotLeaseLedgerStore:     strings.ToLower(strings.TrimSpace(config.SlotLeaseLedgerStore)),
			SlotLeaseLedgerStorePath: strings.TrimSpace(config.SlotLeaseLedgerStorePath),
			SlotLeaseLedgerStoreKey:  strings.TrimSpace(config.SlotLeaseLedgerStoreKey),
			SlotLeaseEtcdEndpoints:   normalizeAssignmentStringSlice(config.SlotLeaseEtcdEndpoints),
			SlotLeaseEtcdPrefix:      strings.TrimSpace(config.SlotLeaseEtcdPrefix),
		},
		redactor:  transport.NewRedactor(redactValues),
		requester: config.Requester,
		dialer:    dialer,
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

func (c *Client) RunResource(ctx context.Context, resource json.RawMessage) OperationResult {
	return c.requestResource(ctx, "resource", resource)
}

func (c *Client) request(ctx context.Context, operation string, command string) OperationResult {
	return c.requestWithResource(ctx, operation, command, nil)
}

func (c *Client) requestResource(ctx context.Context, operation string, resource json.RawMessage) OperationResult {
	return c.requestWithResource(ctx, operation, "", resource)
}

func (c *Client) requestWithResource(ctx context.Context, operation string, command string, resource json.RawMessage) OperationResult {
	started := time.Now()
	metadata := c.metadata
	metadata.Resource = cloneRawMessage(resource)
	assignment := NewCommandAssignmentWithMetadata(operation, c.target, command, started, metadata)
	rawAssignment, err := json.Marshal(assignment)
	if err != nil {
		return c.resultFromError(operation, started, err)
	}
	runCtx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	requester := c.requester
	closeRequester := false
	if requester == nil {
		requester, err = c.dialer(runCtx, DialConfig{
			Server:  c.server,
			Creds:   c.creds,
			NKey:    c.nkey,
			Timeout: c.timeout,
			Name:    "torque-nats-command-client",
		})
		if err != nil {
			return c.resultFromRequestError(operation, started, string(rawAssignment), runCtx, err)
		}
		closeRequester = true
	}
	if closeRequester {
		defer requester.Close()
	}

	response, requestErr := requester.Request(runCtx, c.target, rawAssignment)
	timedOut := errors.Is(runCtx.Err(), context.DeadlineExceeded)
	if requestErr != nil {
		return c.resultFromRequestError(operation, started, string(rawAssignment), runCtx, requestErr)
	}
	result, parsed := c.parseWorkerResponse(response)
	if !parsed {
		result = transport.OperationResult{
			Operation: operation,
			Status:    "succeeded",
			Stdout:    c.redactor.RedactString(string(response)),
			ExitCode:  0,
			TimedOut:  timedOut,
		}
	}
	result.Operation = firstNonEmpty(result.Operation, operation)
	result.TargetDigest = firstNonEmpty(result.TargetDigest, c.TargetDigest())
	result.Command = c.commandEvidence(string(rawAssignment))
	result.Stdout = c.redactor.RedactString(result.Stdout)
	result.Stderr = c.redactor.RedactString(result.Stderr)
	result.Error = c.redactor.RedactString(result.Error)
	result.TimedOut = result.TimedOut || timedOut
	if timedOut {
		result.Status = "timeout"
	}
	if result.Status == "" {
		result.Status = "succeeded"
	}
	result.DurationMillis = time.Since(started).Milliseconds()
	return result
}

func (c *Client) commandEvidence(payload string) []string {
	args := []string{"nats.request"}
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
	args = append(args, c.target, payload)
	return c.redactor.RedactArgs(args)
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
		Command:        c.commandEvidence(""),
		ExitCode:       1,
		DurationMillis: time.Since(started).Milliseconds(),
		Error:          c.redactor.RedactString(err.Error()),
	}
}

func (c *Client) resultFromRequestError(operation string, started time.Time, payload string, ctx context.Context, err error) OperationResult {
	status := "failed"
	timedOut := errors.Is(ctx.Err(), context.DeadlineExceeded)
	if timedOut {
		status = "timeout"
	}
	return OperationResult{
		Operation:      operation,
		Status:         status,
		TargetDigest:   c.TargetDigest(),
		Command:        c.commandEvidence(payload),
		ExitCode:       1,
		TimedOut:       timedOut,
		DurationMillis: time.Since(started).Milliseconds(),
		Error:          c.redactor.RedactString(err.Error()),
	}
}

type natsRequester struct {
	conn *natsgo.Conn
}

func defaultRequestDialer(ctx context.Context, config DialConfig) (Requester, error) {
	return DialRequester(ctx, config)
}

func DialRequester(ctx context.Context, config DialConfig) (Requester, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	opts, err := ConnectOptions(config)
	if err != nil {
		return nil, err
	}
	conn, err := natsgo.Connect(ServerOrDefault(config.Server), opts...)
	if err != nil {
		return nil, err
	}
	return natsRequester{conn: conn}, nil
}

func (r natsRequester) Request(ctx context.Context, subject string, payload []byte) ([]byte, error) {
	msg, err := r.conn.RequestWithContext(ctx, subject, payload)
	if err != nil {
		return nil, err
	}
	return msg.Data, nil
}

func (r natsRequester) Close() {
	if r.conn != nil {
		r.conn.Close()
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
