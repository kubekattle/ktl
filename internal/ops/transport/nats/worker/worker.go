package worker

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	agentcapability "github.com/ingresslabs/torque/internal/ops/agent/capability"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsgo "github.com/nats-io/nats.go"
)

type Config struct {
	Server                     string
	Subject                    string
	Queue                      string
	Delivery                   string
	AssignmentStream           string
	ReceiptStream              string
	Durable                    string
	LedgerPath                 string
	StreamMaxAge               time.Duration
	MaxDeliver                 int
	AckWait                    time.Duration
	Backoff                    []time.Duration
	NakDelay                   time.Duration
	OnExhausted                string
	Creds                      string
	NKey                       string
	Timeout                    time.Duration
	ShellBinary                string
	RedactValues               []string
	Capabilities               []string
	DisableCapabilityDiscovery bool
	AgentID                    string
	Tenant                     string
	TargetID                   string
	Hostname                   string
	Runner                     transport.Runner
	Ready                      chan<- struct{}
}

// Worker subscribes to one NATS assignment subject and executes supported
// command assignments through the local transport contract.
type Worker struct {
	server           string
	subject          string
	queue            string
	delivery         string
	assignmentStream string
	receiptStream    string
	durable          string
	ledgerPath       string
	streamMaxAge     time.Duration
	maxDeliver       int
	ackWait          time.Duration
	backoff          []time.Duration
	nakDelay         time.Duration
	onExhausted      string
	creds            string
	nkey             string
	timeout          time.Duration
	shell            string
	redactor         transport.Redactor
	capabilities     map[string]struct{}
	capabilityDigest string
	agentID          string
	tenant           string
	targetID         string
	hostname         string
	runner           transport.Runner
	ready            chan<- struct{}
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
	delivery := natstransport.NormalizeDelivery(config.Delivery)
	if delivery != natstransport.DeliveryRequestReply && delivery != natstransport.DeliveryJetStream {
		return nil, fmt.Errorf("unsupported NATS worker delivery %q", config.Delivery)
	}
	creds := strings.TrimSpace(config.Creds)
	nkey := strings.TrimSpace(config.NKey)
	assignmentStream := natstransport.AssignmentStreamName(config.AssignmentStream)
	receiptStream := natstransport.ReceiptStreamName(config.ReceiptStream)
	durable := strings.TrimSpace(config.Durable)
	if durable == "" {
		durable = strings.TrimSpace(config.Queue)
	}
	if durable == "" {
		durable = natstransport.DefaultAssignmentConsumer + "-" + natstransport.NormalizeSubjectToken(firstNonEmptyWorker(config.TargetID, config.AgentID, subject), "target")
	}
	durable = natstransport.NormalizeSubjectToken(durable, natstransport.DefaultAssignmentConsumer)
	streamMaxAge := config.StreamMaxAge
	if streamMaxAge <= 0 {
		streamMaxAge = 24 * time.Hour
	}
	maxDeliver := config.MaxDeliver
	if maxDeliver <= 0 {
		maxDeliver = 3
	}
	ackWait := config.AckWait
	if ackWait <= 0 {
		ackWait = 30 * time.Second
	}
	nakDelay := config.NakDelay
	if nakDelay < 0 {
		return nil, fmt.Errorf("nats worker nakDelay must be >= 0")
	}
	onExhausted := normalizeOnExhausted(config.OnExhausted)
	if onExhausted == "" {
		onExhausted = "block"
	}
	if onExhausted != "block" && onExhausted != "continue" {
		return nil, fmt.Errorf("nats worker onExhausted must be block or continue")
	}
	backoff := normalizeDurations(config.Backoff)
	redactValues := append([]string(nil), config.RedactValues...)
	redactValues = append(redactValues, subject, server, creds, nkey, assignmentStream, receiptStream)
	capabilities, capabilityDigest := workerCapabilities(config)
	identity := workerIdentity(config)
	return &Worker{
		server:           server,
		subject:          subject,
		queue:            strings.TrimSpace(config.Queue),
		delivery:         delivery,
		assignmentStream: assignmentStream,
		receiptStream:    receiptStream,
		durable:          durable,
		ledgerPath:       defaultLedgerPath(config.LedgerPath),
		streamMaxAge:     streamMaxAge,
		maxDeliver:       maxDeliver,
		ackWait:          ackWait,
		backoff:          backoff,
		nakDelay:         nakDelay,
		onExhausted:      onExhausted,
		creds:            creds,
		nkey:             nkey,
		timeout:          timeout,
		shell:            strings.TrimSpace(config.ShellBinary),
		redactor:         transport.NewRedactor(redactValues),
		capabilities:     capabilities,
		capabilityDigest: capabilityDigest,
		agentID:          identity.agentID,
		tenant:           identity.tenant,
		targetID:         identity.targetID,
		hostname:         identity.hostname,
		runner:           runner,
		ready:            config.Ready,
	}, nil
}

func (w *Worker) Run(ctx context.Context) error {
	if w.delivery == natstransport.DeliveryJetStream {
		return w.runJetStream(ctx)
	}
	return w.runRequestReply(ctx)
}

func (w *Worker) runRequestReply(ctx context.Context) error {
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

func (w *Worker) runJetStream(ctx context.Context) error {
	opts, err := natstransport.ConnectOptions(natstransport.DialConfig{
		Server:  w.server,
		Creds:   w.creds,
		NKey:    w.nkey,
		Timeout: w.timeout,
		Name:    "torque-agent-nats-worker-jetstream",
	})
	if err != nil {
		return err
	}
	conn, err := natsgo.Connect(w.server, opts...)
	if err != nil {
		return err
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(w.timeout))
	if err != nil {
		return err
	}
	if err := natstransport.EnsureStream(ctx, js, w.assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, w.streamMaxAge); err != nil {
		return fmt.Errorf("ensure assignment stream: %w", err)
	}
	if err := natstransport.EnsureStream(ctx, js, w.receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, w.streamMaxAge); err != nil {
		return fmt.Errorf("ensure receipt stream: %w", err)
	}
	ledger, err := openAssignmentLedger(ctx, w.ledgerPath)
	if err != nil {
		return err
	}
	defer func() { _ = ledger.Close() }()
	subOpts := []natsgo.SubOpt{
		natsgo.BindStream(w.assignmentStream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.AckWait(w.ackWait),
		natsgo.MaxDeliver(w.maxDeliver),
		natsgo.ManualAck(),
		natsgo.PullMaxWaiting(128),
	}
	if len(w.backoff) > 0 {
		subOpts = append(subOpts, natsgo.BackOff(append([]time.Duration(nil), w.backoff...)))
	}
	sub, err := js.PullSubscribe(
		w.subject,
		w.durable,
		subOpts...,
	)
	if err != nil {
		return err
	}
	if w.ready != nil {
		close(w.ready)
	}
	fetchWait := 500 * time.Millisecond
	if w.timeout > 0 && w.timeout < fetchWait {
		fetchWait = w.timeout
	}
	for {
		if err := ctx.Err(); err != nil {
			return nil
		}
		msgs, err := sub.Fetch(1, natsgo.MaxWait(fetchWait))
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				continue
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		for _, msg := range msgs {
			if err := w.handleJetStreamMessage(ctx, js, ledger, msg); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				_ = msg.Nak(natsgo.Context(ctx))
				return err
			}
		}
	}
}

func (w *Worker) handleJetStreamMessage(ctx context.Context, js natsgo.JetStreamContext, ledger *assignmentLedger, msg *natsgo.Msg) error {
	assignmentOffset := natstransport.OffsetFromMessage(msg, w.assignmentStream, w.durable)
	assignment, err := natstransport.ParseCommandAssignment(msg.Data)
	if err != nil {
		result := w.errorResult("run", "", err, false)
		result.Metadata = w.jetStreamReceiptMetadata(result.Metadata, assignmentOffset, ledgerDecision{})
		if err := w.publishJetStreamReceipt(ctx, js, result); err != nil {
			return err
		}
		return w.ackJetStreamAssignment(ctx, msg, ledger, "")
	}
	decision, err := ledger.Begin(ctx, assignment)
	if err != nil {
		return err
	}
	if decision.Replay {
		result := decision.Receipt
		result.Metadata = w.jetStreamReceiptMetadata(result.Metadata, assignmentOffset, decision)
		result.Metadata = mergeMetadata(result.Metadata, map[string]string{
			"deduped":         "true",
			"replayedReceipt": "true",
			"ledgerStatus":    decision.Status,
			"workerDecision":  "deduped",
		})
		if err := w.publishJetStreamReceipt(ctx, js, result); err != nil {
			return err
		}
		return w.ackJetStreamAssignment(ctx, msg, ledger, assignment.AssignmentID)
	}
	if decision.UnsafeReplay {
		result := w.blockedResultWithReason("run", assignment.Target, assignment, "assignment already has a running ledger entry without a stored receipt; refusing duplicate execution")
		result.Metadata = w.jetStreamReceiptMetadata(result.Metadata, assignmentOffset, decision)
		result.Metadata = mergeMetadata(result.Metadata, map[string]string{
			"ledgerUnsafeReplay": "true",
			"workerDecision":     "blocked",
		})
		if err := ledger.SaveReceipt(ctx, assignment.AssignmentID, result); err != nil {
			return err
		}
		if err := w.publishJetStreamReceipt(ctx, js, result); err != nil {
			return err
		}
		return w.ackJetStreamAssignment(ctx, msg, ledger, assignment.AssignmentID)
	}

	result := w.HandleAssignment(ctx, assignment)
	result.Metadata = w.jetStreamReceiptMetadata(result.Metadata, assignmentOffset, decision)
	if w.retryableResult(result) {
		if w.retryBudgetExhausted(assignmentOffset) {
			result = w.deadLetterResult(result, assignment, assignmentOffset)
		} else {
			result.Metadata = mergeMetadata(result.Metadata, map[string]string{
				"workerDecision": "retrying",
				"retrying":       "true",
			})
			if err := ledger.MarkRetry(ctx, assignment.AssignmentID, result); err != nil {
				return err
			}
			return w.nakJetStreamAssignment(ctx, msg)
		}
	}
	if err := ledger.SaveReceipt(ctx, assignment.AssignmentID, result); err != nil {
		return err
	}
	if err := w.publishJetStreamReceipt(ctx, js, result); err != nil {
		return err
	}
	return w.ackJetStreamAssignment(ctx, msg, ledger, assignment.AssignmentID)
}

func (w *Worker) retryableResult(result transport.OperationResult) bool {
	switch strings.ToLower(strings.TrimSpace(result.Status)) {
	case "failed", "timeout":
		return true
	default:
		return false
	}
}

func (w *Worker) retryBudgetExhausted(offset *natstransport.StreamOffset) bool {
	if w.maxDeliver <= 0 {
		return false
	}
	delivered := uint64(1)
	if offset != nil && offset.NumDelivered > 0 {
		delivered = offset.NumDelivered
	}
	return delivered >= uint64(w.maxDeliver)
}

func (w *Worker) nakJetStreamAssignment(ctx context.Context, msg *natsgo.Msg) error {
	if w.nakDelay > 0 {
		if err := msg.NakWithDelay(w.nakDelay, natsgo.Context(ctx)); err != nil {
			return fmt.Errorf("nak assignment with delay: %w", err)
		}
		return nil
	}
	if err := msg.Nak(natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("nak assignment: %w", err)
	}
	return nil
}

func (w *Worker) deadLetterResult(result transport.OperationResult, assignment natstransport.CommandAssignment, offset *natstransport.StreamOffset) transport.OperationResult {
	delivered := uint64(1)
	if offset != nil && offset.NumDelivered > 0 {
		delivered = offset.NumDelivered
	}
	result.Metadata = mergeMetadata(result.Metadata, map[string]string{
		"workerDecision": "dead-letter",
		"deadLetter":     "true",
		"retryExhausted": "true",
	})
	result.Metadata = mergeMetadata(result.Metadata, w.retryMetadata(delivered))
	previousError := strings.TrimSpace(result.Error)
	if strings.ToLower(strings.TrimSpace(w.onExhausted)) == "block" {
		result.Status = "blocked"
		result.ExitCode = 1
	}
	result.Error = w.redactor.RedactString(fmt.Sprintf("assignment retry budget exhausted after %d deliveries: %s", delivered, previousError))
	if previousError == "" {
		result.Error = fmt.Sprintf("assignment retry budget exhausted after %d deliveries", delivered)
	}
	if strings.TrimSpace(result.Operation) == "" {
		result.Operation = firstNonEmptyWorker(assignment.Operation, "run")
	}
	return result
}

func (w *Worker) publishJetStreamReceipt(ctx context.Context, js natsgo.JetStreamContext, result transport.OperationResult) error {
	raw, err := json.Marshal(result)
	if err != nil {
		result = w.errorResult("run", "", fmt.Errorf("marshal operation result: %w", err), false)
		raw, err = json.Marshal(result)
		if err != nil {
			return err
		}
	}
	subject := natstransport.ReceiptSubject(
		w.tenant,
		firstNonEmptyWorker(result.Metadata["runId"], "run"),
		firstNonEmptyWorker(result.Metadata["assignmentTargetId"], result.Metadata["targetId"], w.targetID),
	)
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("publish receipt: %w", err)
	}
	return nil
}

func (w *Worker) ackJetStreamAssignment(ctx context.Context, msg *natsgo.Msg, ledger *assignmentLedger, assignmentID string) error {
	if strings.TrimSpace(assignmentID) != "" {
		if err := ledger.MarkReceiptPublished(ctx, assignmentID); err != nil {
			return err
		}
	}
	if err := msg.Ack(natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("ack assignment: %w", err)
	}
	return nil
}

func (w *Worker) jetStreamReceiptMetadata(base map[string]string, assignmentOffset *natstransport.StreamOffset, decision ledgerDecision) map[string]string {
	metadata := mergeMetadata(base, streamOffsetMetadata("assignment", assignmentOffset))
	delivered := uint64(1)
	if assignmentOffset != nil && assignmentOffset.NumDelivered > 0 {
		delivered = assignmentOffset.NumDelivered
	}
	metadata = mergeMetadata(metadata, map[string]string{
		"delivery":                natstransport.DeliveryJetStream,
		"assignmentStream":        strings.TrimSpace(w.assignmentStream),
		"receiptStream":           strings.TrimSpace(w.receiptStream),
		"assignmentConsumer":      strings.TrimSpace(w.durable),
		"assignmentStreamSubject": firstNonEmptyWorker(assignmentOffsetSubject(assignmentOffset), w.subject),
	})
	metadata = mergeMetadata(metadata, w.retryMetadata(delivered))
	if decision.Attempt > 0 {
		metadata = mergeMetadata(metadata, map[string]string{"ledgerAttempt": strconv.Itoa(decision.Attempt)})
	}
	if strings.TrimSpace(decision.Status) != "" {
		metadata = mergeMetadata(metadata, map[string]string{"ledgerStatus": strings.TrimSpace(decision.Status)})
	}
	return metadata
}

func (w *Worker) retryMetadata(numDelivered uint64) map[string]string {
	if numDelivered == 0 {
		numDelivered = 1
	}
	metadata := map[string]string{
		"numDelivered": strconv.FormatUint(numDelivered, 10),
		"maxDeliver":   strconv.Itoa(w.maxDeliver),
		"ackWait":      w.ackWait.String(),
		"nakDelay":     w.nakDelay.String(),
		"onExhausted":  strings.TrimSpace(w.onExhausted),
		"retryPolicy":  fmt.Sprintf("maxDeliver=%d,ackWait=%s,nakDelay=%s,onExhausted=%s", w.maxDeliver, w.ackWait, w.nakDelay, strings.TrimSpace(w.onExhausted)),
	}
	if len(w.backoff) > 0 {
		metadata["backoff"] = joinWorkerDurations(w.backoff)
		metadata["retryPolicy"] += ",backoff=" + metadata["backoff"]
	}
	return metadata
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
		return w.blockedResultWithReason(operation, target, assignment, fmt.Sprintf("assignment target %q does not match worker subject %q", target, w.subject))
	}
	if expectedAgentID := strings.TrimSpace(assignment.ExpectedAgentID); expectedAgentID != "" && expectedAgentID != strings.TrimSpace(w.agentID) {
		return w.blockedResultWithReason(operation, target, assignment, fmt.Sprintf("assignment expected agentId %s but worker agentId is %s", expectedAgentID, strings.TrimSpace(w.agentID)))
	}
	if assignmentTargetID := strings.TrimSpace(assignment.TargetID); assignmentTargetID != "" && assignmentTargetID != strings.TrimSpace(w.targetID) {
		return w.blockedResultWithReason(operation, target, assignment, fmt.Sprintf("assignment targetId %s does not match worker targetId %s", assignmentTargetID, strings.TrimSpace(w.targetID)))
	}
	requiredCapability := strings.TrimSpace(assignment.RequiredCapability)
	if requiredCapability != "" && !w.hasCapability(requiredCapability) {
		return w.blockedResult(operation, target, assignment)
	}
	client, err := localtransport.New(localtransport.Config{
		Target:       "local://" + w.subject,
		ShellBinary:  w.shell,
		Timeout:      w.timeout,
		RedactValues: append([]string{w.subject, w.server}, w.redactorValues()...),
		Runner:       w.runner,
	})
	if err != nil {
		return w.errorResultForAssignment(operation, target, assignment, err, false)
	}
	var result transport.OperationResult
	switch operation {
	case "connect":
		result = client.Connect(ctx)
	case "run":
		if strings.TrimSpace(assignment.Command) == "" {
			return w.blockedResultWithReason(operation, target, assignment, "run assignment command is required")
		}
		result = client.Run(ctx, assignment.Command)
	default:
		return w.blockedResultWithReason(operation, target, assignment, fmt.Sprintf("unsupported assignment operation %q", operation))
	}
	result.Operation = operation
	result.TargetDigest = natstransport.TargetDigest(target)
	result.Stdout = w.redactor.RedactString(result.Stdout)
	result.Stderr = w.redactor.RedactString(result.Stderr)
	result.Error = w.redactor.RedactString(result.Error)
	result.Command = w.redactor.RedactArgs(result.Command)
	result.Metadata = mergeMetadata(result.Metadata, w.receiptMetadata(assignment, "executed"))
	return result
}

func (w *Worker) errorResult(operation string, target string, err error, timedOut bool) transport.OperationResult {
	return w.errorResultForAssignment(operation, target, natstransport.CommandAssignment{}, err, timedOut)
}

func (w *Worker) errorResultForAssignment(operation string, target string, assignment natstransport.CommandAssignment, err error, timedOut bool) transport.OperationResult {
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
		Metadata:     w.receiptMetadata(assignment, "failed"),
	}
}

func (w *Worker) blockedResult(operation string, target string, assignment natstransport.CommandAssignment) transport.OperationResult {
	msg := fmt.Sprintf("missing required capability %s", strings.TrimSpace(assignment.RequiredCapability))
	return w.blockedResultWithReason(operation, target, assignment, msg)
}

func (w *Worker) blockedResultWithReason(operation string, target string, assignment natstransport.CommandAssignment, msg string) transport.OperationResult {
	if strings.TrimSpace(operation) == "" {
		operation = "run"
	}
	if strings.TrimSpace(target) == "" {
		target = w.subject
	}
	return transport.OperationResult{
		Operation:    strings.TrimSpace(operation),
		Status:       "blocked",
		TargetDigest: natstransport.TargetDigest(target),
		Command: w.redactor.RedactArgs([]string{
			"torque-agent",
			"nats",
			"worker",
			"--subject",
			w.subject,
		}),
		ExitCode: 1,
		Error:    w.redactor.RedactString(msg),
		Metadata: w.receiptMetadata(assignment, "blocked"),
	}
}

func (w *Worker) hasCapability(requiredCapability string) bool {
	requiredCapability = strings.TrimSpace(requiredCapability)
	if requiredCapability == "" {
		return true
	}
	_, ok := w.capabilities[requiredCapability]
	return ok
}

func (w *Worker) redactorValues() []string {
	return []string{w.subject, w.server, w.creds, w.nkey}
}

func workerCapabilities(config Config) (map[string]struct{}, string) {
	names := append([]string(nil), config.Capabilities...)
	if !config.DisableCapabilityDiscovery {
		report := agentcapability.Discover(agentcapability.Options{})
		names = append(names, agentcapability.AvailableAdapters(report)...)
	}
	names = normalizeCapabilityNames(names)
	out := make(map[string]struct{}, len(names))
	for _, name := range names {
		out[name] = struct{}{}
	}
	return out, capabilityNamesDigest(names)
}

func normalizeCapabilityNames(names []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

type workerIdentityInfo struct {
	agentID  string
	tenant   string
	targetID string
	hostname string
}

func workerIdentity(config Config) workerIdentityInfo {
	hostname := strings.TrimSpace(config.Hostname)
	if hostname == "" {
		hostname, _ = os.Hostname()
	}
	agentID := strings.TrimSpace(config.AgentID)
	if agentID == "" {
		agentID = hostname
	}
	tenant := strings.TrimSpace(config.Tenant)
	if tenant == "" {
		tenant = "default"
	}
	targetID := strings.TrimSpace(config.TargetID)
	if targetID == "" {
		targetID = agentID
	}
	return workerIdentityInfo{
		agentID:  agentID,
		tenant:   tenant,
		targetID: targetID,
		hostname: hostname,
	}
}

func capabilityNamesDigest(names []string) string {
	raw, _ := json.Marshal(normalizeCapabilityNames(names))
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func normalizeDurations(values []time.Duration) []time.Duration {
	out := make([]time.Duration, 0, len(values))
	for _, value := range values {
		if value > 0 {
			out = append(out, value)
		}
	}
	return out
}

func normalizeOnExhausted(value string) string {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "block":
		return "block"
	case "continue":
		return "continue"
	default:
		return strings.TrimSpace(value)
	}
}

func joinWorkerDurations(values []time.Duration) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value > 0 {
			out = append(out, value.String())
		}
	}
	return strings.Join(out, ",")
}

func (w *Worker) receiptMetadata(assignment natstransport.CommandAssignment, decision string) map[string]string {
	metadata := map[string]string{
		"agentId":          strings.TrimSpace(w.agentID),
		"tenant":           strings.TrimSpace(w.tenant),
		"targetId":         strings.TrimSpace(w.targetID),
		"hostname":         strings.TrimSpace(w.hostname),
		"workerSubject":    strings.TrimSpace(w.subject),
		"capabilityDigest": strings.TrimSpace(w.capabilityDigest),
		"workerDecision":   strings.TrimSpace(decision),
	}
	addMetadata := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			metadata[key] = value
		}
	}
	addMetadata("requiredCapability", assignment.RequiredCapability)
	addMetadata("assignmentId", assignment.AssignmentID)
	addMetadata("nodeKind", assignment.NodeKind)
	addMetadata("runId", assignment.RunID)
	addMetadata("nodeId", assignment.NodeID)
	addMetadata("planDigest", assignment.PlanDigest)
	addMetadata("assignmentTargetId", assignment.TargetID)
	addMetadata("expectedAgentId", assignment.ExpectedAgentID)
	for key, value := range metadata {
		if strings.TrimSpace(value) == "" {
			delete(metadata, key)
		}
	}
	return metadata
}

func mergeMetadata(base map[string]string, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for key, value := range base {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	for key, value := range overlay {
		if strings.TrimSpace(value) != "" {
			out[key] = value
		}
	}
	return out
}

func streamOffsetMetadata(prefix string, offset *natstransport.StreamOffset) map[string]string {
	if offset == nil {
		return nil
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = "stream"
	}
	out := map[string]string{
		prefix + "Stream":   strings.TrimSpace(offset.Stream),
		prefix + "Consumer": strings.TrimSpace(offset.Consumer),
		prefix + "Subject":  strings.TrimSpace(offset.Subject),
	}
	if offset.Sequence > 0 {
		out[prefix+"Sequence"] = strconv.FormatUint(offset.Sequence, 10)
	}
	if offset.NumDelivered > 0 {
		out[prefix+"NumDelivered"] = strconv.FormatUint(offset.NumDelivered, 10)
	}
	if offset.NumPending > 0 {
		out[prefix+"NumPending"] = strconv.FormatUint(offset.NumPending, 10)
	}
	for key, value := range out {
		if strings.TrimSpace(value) == "" {
			delete(out, key)
		}
	}
	return out
}

func assignmentOffsetSubject(offset *natstransport.StreamOffset) string {
	if offset == nil {
		return ""
	}
	return strings.TrimSpace(offset.Subject)
}

func firstNonEmptyWorker(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
