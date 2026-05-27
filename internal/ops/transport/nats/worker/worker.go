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
	StreamMaxAge               time.Duration
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
	streamMaxAge     time.Duration
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
		streamMaxAge:     streamMaxAge,
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
	sub, err := js.PullSubscribe(
		w.subject,
		w.durable,
		natsgo.BindStream(w.assignmentStream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
		natsgo.PullMaxWaiting(128),
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
			if err := w.handleJetStreamMessage(ctx, js, msg); err != nil {
				if ctx.Err() != nil {
					return nil
				}
				_ = msg.Nak(natsgo.Context(ctx))
				return err
			}
		}
	}
}

func (w *Worker) handleJetStreamMessage(ctx context.Context, js natsgo.JetStreamContext, msg *natsgo.Msg) error {
	result := w.HandleMessage(ctx, msg.Data)
	assignmentOffset := natstransport.OffsetFromMessage(msg, w.assignmentStream, w.durable)
	result.Metadata = mergeMetadata(result.Metadata, streamOffsetMetadata("assignment", assignmentOffset))
	result.Metadata = mergeMetadata(result.Metadata, map[string]string{
		"delivery":                natstransport.DeliveryJetStream,
		"assignmentStream":        strings.TrimSpace(w.assignmentStream),
		"receiptStream":           strings.TrimSpace(w.receiptStream),
		"assignmentConsumer":      strings.TrimSpace(w.durable),
		"assignmentStreamSubject": strings.TrimSpace(msg.Subject),
	})
	subject := natstransport.ReceiptSubject(
		w.tenant,
		firstNonEmptyWorker(result.Metadata["runId"], "run"),
		firstNonEmptyWorker(result.Metadata["assignmentTargetId"], result.Metadata["targetId"], w.targetID),
	)
	raw, err := json.Marshal(result)
	if err != nil {
		result = w.errorResult("run", "", fmt.Errorf("marshal operation result: %w", err), false)
		result.Metadata = mergeMetadata(result.Metadata, streamOffsetMetadata("assignment", assignmentOffset))
		raw, err = json.Marshal(result)
		if err != nil {
			return err
		}
	}
	if _, err := js.Publish(subject, raw, natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("publish receipt: %w", err)
	}
	if err := msg.Ack(natsgo.Context(ctx)); err != nil {
		return fmt.Errorf("ack assignment: %w", err)
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
		return w.errorResultForAssignment(operation, target, assignment, fmt.Errorf("assignment target %q does not match worker subject %q", target, w.subject), false)
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
			return w.errorResultForAssignment(operation, target, assignment, fmt.Errorf("run assignment command is required"), false)
		}
		result = client.Run(ctx, assignment.Command)
	default:
		return w.errorResultForAssignment(operation, target, assignment, fmt.Errorf("unsupported assignment operation %q", operation), false)
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

func firstNonEmptyWorker(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
