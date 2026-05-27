package stack

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsgo "github.com/nats-io/nats.go"
)

const (
	FleetNATSFanoutAPIVersion = "torque.dev/stack/fleet-nats-fanout/v1alpha1"
	FleetNATSFanoutKind       = "FleetNATSFanout"
)

type fleetNATSFanoutReceipt struct {
	APIVersion         string                      `json:"apiVersion"`
	Kind               string                      `json:"kind"`
	NodeID             string                      `json:"nodeId"`
	NodeKind           string                      `json:"nodeKind"`
	Phase              string                      `json:"phase"`
	Status             string                      `json:"status"`
	Reason             string                      `json:"reason,omitempty"`
	RunID              string                      `json:"runId"`
	RequiredCapability string                      `json:"requiredCapability,omitempty"`
	GeneratedAt        string                      `json:"generatedAt"`
	Policy             fleetNATSFanoutPolicy       `json:"policy"`
	Summary            fleetNATSFanoutSummary      `json:"summary"`
	Targets            []fleetNATSFanoutTargetView `json:"targets,omitempty"`
	Results            []fleetNATSFanoutResult     `json:"results,omitempty"`
}

type fleetNATSFanoutPolicy struct {
	MaxParallel         int                       `json:"maxParallel"`
	MaxFailed           int                       `json:"maxFailed"`
	MinSucceededPercent int                       `json:"minSucceededPercent"`
	OnPartialFailure    string                    `json:"onPartialFailure"`
	Delivery            string                    `json:"delivery"`
	Retry               RunnerFanoutRetryResolved `json:"retry,omitempty"`
}

type fleetNATSFanoutSummary struct {
	TargetCount      int `json:"targetCount"`
	Succeeded        int `json:"succeeded"`
	Failed           int `json:"failed,omitempty"`
	Blocked          int `json:"blocked,omitempty"`
	TimedOut         int `json:"timedOut,omitempty"`
	MissingReceipts  int `json:"missingReceipts,omitempty"`
	SucceededPercent int `json:"succeededPercent"`
	NonSucceeded     int `json:"nonSucceeded,omitempty"`
	PolicyViolations int `json:"policyViolations,omitempty"`
}

type fleetNATSFanoutTargetView struct {
	AgentID          string            `json:"agentId"`
	TargetID         string            `json:"targetId"`
	Hostname         string            `json:"hostname,omitempty"`
	WorkerSubject    string            `json:"workerSubject"`
	CapabilityDigest string            `json:"capabilityDigest,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
}

type fleetNATSFanoutResult struct {
	AgentID          string                           `json:"agentId"`
	TargetID         string                           `json:"targetId"`
	Hostname         string                           `json:"hostname,omitempty"`
	WorkerSubject    string                           `json:"workerSubject"`
	Status           string                           `json:"status"`
	Error            string                           `json:"error,omitempty"`
	Assignment       *natstransport.CommandAssignment `json:"assignment,omitempty"`
	AssignmentOffset *natstransport.StreamOffset      `json:"assignmentOffset,omitempty"`
	ReceiptOffset    *natstransport.StreamOffset      `json:"receiptOffset,omitempty"`
	Receipt          transport.OperationResult        `json:"receipt"`
}

type fleetNATSFanoutTarget struct {
	agentID          string
	targetID         string
	hostname         string
	workerSubject    string
	capabilityDigest string
	labels           map[string]string
}

func (e *customNodeExecutor) shouldUseFleetNATSFanout(spec HostCommandSpec) bool {
	if e == nil || e.run == nil || e.run.Plan == nil {
		return false
	}
	if strings.ToLower(strings.TrimSpace(e.run.Plan.Runner.Mode)) != RunnerModeFleet {
		return false
	}
	if strings.TrimSpace(spec.Target) != "" || strings.TrimSpace(spec.TargetEnv) != "" {
		return false
	}
	return transportIsNATS(spec.Transport)
}

func (e *customNodeExecutor) runHostCommandFleetNATSFanout(ctx context.Context, node *runNode, phase string, spec HostCommandSpec, command string) (fleetNATSFanoutReceipt, transport.OperationResult) {
	started := time.Now()
	policy := e.fleetNATSFanoutPolicy()
	receipt := fleetNATSFanoutReceipt{
		APIVersion:         FleetNATSFanoutAPIVersion,
		Kind:               FleetNATSFanoutKind,
		NodeID:             strings.TrimSpace(node.ID),
		NodeKind:           normalizeNodeKind(node.Kind),
		Phase:              strings.TrimSpace(phase),
		Status:             "checking",
		RunID:              strings.TrimSpace(e.run.RunID),
		RequiredCapability: strings.TrimSpace(spec.RequiredCap),
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339Nano),
		Policy:             policy,
	}
	targets, err := e.fleetNATSFanoutTargets(ctx, spec.RequiredCap)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = err.Error()
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	receipt.Targets = make([]fleetNATSFanoutTargetView, 0, len(targets))
	for _, target := range targets {
		receipt.Targets = append(receipt.Targets, target.view())
	}
	receipt.Summary.TargetCount = len(targets)
	if guardErr := e.validateFleetNATSFanoutOpsGuard(node, targets, spec.RequiredCap); guardErr != nil {
		receipt.Status = "blocked"
		receipt.Reason = guardErr.Error()
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}

	timeout := 30 * time.Second
	if spec.Timeout != nil && *spec.Timeout > 0 {
		timeout = *spec.Timeout
	}
	server := firstNonEmptyString(os.Getenv("TORQUE_NATS_URL"), os.Getenv("TORQUE_NATS_SERVER"))
	creds := strings.TrimSpace(os.Getenv("TORQUE_NATS_CREDS"))
	nkey := strings.TrimSpace(os.Getenv("TORQUE_NATS_NKEY"))
	if policy.Delivery == RunnerFanoutDeliveryJetStream {
		return e.runHostCommandFleetNATSJetStreamFanout(ctx, started, receipt, targets, spec, command, timeout, server, creds, nkey)
	}
	requester, err := natstransport.DialRequester(ctx, natstransport.DialConfig{
		Server:  server,
		Creds:   creds,
		NKey:    nkey,
		Timeout: timeout,
		Name:    "torque-stack-fleet-fanout",
	})
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("connect NATS fleet fan-out requester: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	defer requester.Close()

	results := make([]fleetNATSFanoutResult, len(targets))
	limit := policy.MaxParallel
	if limit > len(targets) {
		limit = len(targets)
	}
	if limit < 1 {
		limit = 1
	}
	jobs := make(chan int)
	var wg sync.WaitGroup
	for i := 0; i < limit; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for idx := range jobs {
				target := targets[idx]
				client, clientErr := natstransport.New(natstransport.Config{
					Target:             target.workerSubject,
					Server:             server,
					Creds:              creds,
					NKey:               nkey,
					Timeout:            timeout,
					RedactValues:       []string{target.workerSubject, server},
					TargetID:           target.targetID,
					ExpectedAgentID:    target.agentID,
					RequiredCapability: strings.TrimSpace(spec.RequiredCap),
					NodeKind:           strings.TrimSpace(spec.NodeKind),
					RunID:              strings.TrimSpace(spec.RunID),
					NodeID:             strings.TrimSpace(spec.NodeID),
					PlanDigest:         strings.TrimSpace(spec.PlanDigest),
					Requester:          requester,
				})
				if clientErr != nil {
					results[idx] = target.result(transport.OperationResult{
						Operation:    "run",
						Status:       "failed",
						TargetDigest: natstransport.TargetDigest(target.workerSubject),
						ExitCode:     1,
						Error:        clientErr.Error(),
					})
					continue
				}
				results[idx] = target.result(client.Run(ctx, command))
			}
		}()
	}
	for i := range targets {
		jobs <- i
	}
	close(jobs)
	wg.Wait()

	receipt.Results = results
	e.finalizeFleetNATSFanoutReceipt(&receipt)
	return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
}

func (e *customNodeExecutor) runHostCommandFleetNATSJetStreamFanout(ctx context.Context, started time.Time, receipt fleetNATSFanoutReceipt, targets []fleetNATSFanoutTarget, spec HostCommandSpec, command string, timeout time.Duration, server string, creds string, nkey string) (fleetNATSFanoutReceipt, transport.OperationResult) {
	opts, err := natstransport.ConnectOptions(natstransport.DialConfig{
		Server:  server,
		Creds:   creds,
		NKey:    nkey,
		Timeout: timeout,
		Name:    "torque-stack-fleet-jetstream-fanout",
	})
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("connect NATS fleet JetStream options: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	conn, err := natsgo.Connect(natstransport.ServerOrDefault(server), opts...)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("connect NATS fleet JetStream: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(timeout))
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("open NATS JetStream context: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	assignmentStream := natstransport.AssignmentStreamName(os.Getenv("TORQUE_NATS_ASSIGNMENT_STREAM"))
	receiptStream := natstransport.ReceiptStreamName(os.Getenv("TORQUE_NATS_RECEIPT_STREAM"))
	if err := natstransport.EnsureStream(ctx, js, assignmentStream, []string{natstransport.DefaultAssignmentStreamSubject}, 24*time.Hour); err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("ensure assignment stream: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	if err := natstransport.EnsureStream(ctx, js, receiptStream, []string{natstransport.DefaultReceiptStreamSubject}, 24*time.Hour); err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("ensure receipt stream: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}

	tenant := "default"
	if e != nil && e.run != nil && e.run.Plan != nil {
		tenant = normalizeFleetReadiness(e.run.Plan.Runner.Readiness).Tenant
	}
	receiptSubject := natstransport.ReceiptSubjectWildcard(tenant, spec.RunID)
	durable := natstransport.DefaultReceiptConsumer + "-" + natstransport.NormalizeSubjectToken(spec.RunID+"-"+spec.NodeID, "run")
	sub, err := js.PullSubscribe(
		receiptSubject,
		durable,
		natsgo.BindStream(receiptStream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
		natsgo.PullMaxWaiting(128),
	)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("subscribe receipt stream: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}

	results := make([]fleetNATSFanoutResult, len(targets))
	assignments := make([]natstransport.CommandAssignment, len(targets))
	assignmentOffsets := make([]*natstransport.StreamOffset, len(targets))
	targetIndex := make(map[string]int, len(targets))
	for idx, target := range targets {
		targetIndex[target.targetID] = idx
		assignment := natstransport.NewCommandAssignmentWithMetadata("run", target.workerSubject, command, time.Now(), natstransport.CommandAssignmentMetadata{
			TargetID:           target.targetID,
			ExpectedAgentID:    target.agentID,
			RequiredCapability: strings.TrimSpace(spec.RequiredCap),
			NodeKind:           strings.TrimSpace(spec.NodeKind),
			RunID:              strings.TrimSpace(spec.RunID),
			NodeID:             strings.TrimSpace(spec.NodeID),
			PlanDigest:         strings.TrimSpace(spec.PlanDigest),
		})
		assignments[idx] = assignment
		raw, err := json.Marshal(assignment)
		if err != nil {
			results[idx] = target.resultWithEvidence(transport.OperationResult{
				Operation:    "run",
				Status:       "failed",
				TargetDigest: natstransport.TargetDigest(target.workerSubject),
				ExitCode:     1,
				Error:        fmt.Sprintf("marshal JetStream assignment: %v", err),
			}, assignment, nil, nil)
			continue
		}
		ack, err := js.Publish(target.workerSubject, raw, natsgo.Context(ctx))
		if err != nil {
			results[idx] = target.resultWithEvidence(transport.OperationResult{
				Operation:    "run",
				Status:       "failed",
				TargetDigest: natstransport.TargetDigest(target.workerSubject),
				ExitCode:     1,
				Error:        fmt.Sprintf("publish JetStream assignment: %v", err),
			}, assignment, nil, nil)
			continue
		}
		assignmentOffsets[idx] = natstransport.OffsetFromPublish(target.workerSubject, ack)
		results[idx] = target.resultWithEvidence(transport.OperationResult{
			Operation:    "run",
			Status:       "timeout",
			TargetDigest: natstransport.TargetDigest(target.workerSubject),
			ExitCode:     1,
			TimedOut:     true,
			Error:        "missing JetStream receipt for target " + target.targetID,
			Metadata: map[string]string{
				"delivery":           natstransport.DeliveryJetStream,
				"assignmentStream":   assignmentStream,
				"receiptStream":      receiptStream,
				"receiptSubject":     receiptSubject,
				"assignmentTargetId": target.targetID,
				"expectedAgentId":    target.agentID,
			},
		}, assignment, assignmentOffsets[idx], nil)
	}

	pending := 0
	for idx := range results {
		if results[idx].Status == "timeout" {
			pending++
		}
	}
	deadline := time.Now().Add(timeout)
	for pending > 0 && time.Now().Before(deadline) {
		if err := ctx.Err(); err != nil {
			break
		}
		wait := time.Until(deadline)
		if wait > 500*time.Millisecond {
			wait = 500 * time.Millisecond
		}
		if wait <= 0 {
			break
		}
		msgs, err := sub.Fetch(1, natsgo.MaxWait(wait))
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				continue
			}
			receipt.Status = "failed"
			receipt.Reason = fmt.Sprintf("fetch JetStream receipt: %v", err)
			receipt.Results = results
			e.finalizeFleetNATSFanoutReceipt(&receipt)
			return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
		}
		for _, msg := range msgs {
			var op transport.OperationResult
			if err := json.Unmarshal(msg.Data, &op); err != nil {
				_ = msg.Nak(natsgo.Context(ctx))
				receipt.Status = "failed"
				receipt.Reason = fmt.Sprintf("parse JetStream receipt: %v", err)
				receipt.Results = results
				e.finalizeFleetNATSFanoutReceipt(&receipt)
				return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
			}
			targetID := firstNonEmptyString(op.Metadata["assignmentTargetId"], op.Metadata["targetId"])
			idx, ok := targetIndex[targetID]
			if !ok {
				_ = msg.Ack(natsgo.Context(ctx))
				continue
			}
			receiptOffset := natstransport.OffsetFromMessage(msg, receiptStream, durable)
			if results[idx].Status == "timeout" {
				pending--
			}
			results[idx] = targets[idx].resultWithEvidence(op, assignments[idx], assignmentOffsets[idx], receiptOffset)
			if err := msg.Ack(natsgo.Context(ctx)); err != nil {
				receipt.Status = "failed"
				receipt.Reason = fmt.Sprintf("ack JetStream receipt: %v", err)
				receipt.Results = results
				e.finalizeFleetNATSFanoutReceipt(&receipt)
				return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
			}
		}
	}

	receipt.Results = results
	e.finalizeFleetNATSFanoutReceipt(&receipt)
	return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
}

func (e *customNodeExecutor) fleetNATSFanoutPolicy() fleetNATSFanoutPolicy {
	policy := fleetNATSFanoutPolicy{
		MaxParallel:         64,
		MaxFailed:           0,
		MinSucceededPercent: 100,
		OnPartialFailure:    RunnerFanoutOnBlock,
		Delivery:            RunnerFanoutDeliveryRequestReply,
		Retry: RunnerFanoutRetryResolved{
			MaxDeliver:  3,
			AckWait:     30 * time.Second,
			OnExhausted: RunnerFanoutRetryOnBlock,
		},
	}
	if e == nil || e.run == nil || e.run.Plan == nil {
		return policy
	}
	resolved := e.run.Plan.Runner.Fanout
	if resolved.MaxParallel > 0 {
		policy.MaxParallel = resolved.MaxParallel
	}
	if resolved.MaxFailed >= 0 {
		policy.MaxFailed = resolved.MaxFailed
	}
	if resolved.MinSucceededPercent >= 0 && resolved.MinSucceededPercent <= 100 {
		policy.MinSucceededPercent = resolved.MinSucceededPercent
	}
	if strings.TrimSpace(resolved.OnPartialFailure) != "" {
		policy.OnPartialFailure = strings.ToLower(strings.TrimSpace(resolved.OnPartialFailure))
	}
	if policy.OnPartialFailure != RunnerFanoutOnContinue {
		policy.OnPartialFailure = RunnerFanoutOnBlock
	}
	if strings.TrimSpace(resolved.Delivery) != "" {
		policy.Delivery = normalizeRunnerFanoutDelivery(resolved.Delivery)
	}
	if policy.Delivery != RunnerFanoutDeliveryJetStream {
		policy.Delivery = RunnerFanoutDeliveryRequestReply
	}
	if resolved.Retry.MaxDeliver > 0 {
		policy.Retry.MaxDeliver = resolved.Retry.MaxDeliver
	}
	if resolved.Retry.AckWait > 0 {
		policy.Retry.AckWait = resolved.Retry.AckWait
	}
	if resolved.Retry.Backoff != nil {
		policy.Retry.Backoff = append([]time.Duration(nil), resolved.Retry.Backoff...)
	}
	if strings.TrimSpace(resolved.Retry.OnExhausted) != "" {
		policy.Retry.OnExhausted = normalizeRunnerFanoutRetryOnExhausted(resolved.Retry.OnExhausted)
	}
	return policy
}

func (e *customNodeExecutor) fleetNATSFanoutTargets(ctx context.Context, requiredCapability string) ([]fleetNATSFanoutTarget, error) {
	if e == nil || e.run == nil || e.run.Plan == nil {
		return nil, fmt.Errorf("fleet fan-out requires stack run context")
	}
	readiness := normalizeFleetReadiness(e.run.Plan.Runner.Readiness)
	store, err := openFleetReadinessStore(ctx, readiness)
	if err != nil {
		return nil, fmt.Errorf("read agent registry: %w", err)
	}
	defer store.Close()
	snapshot, err := heartbeat.SnapshotFromStore(ctx, store, heartbeat.SnapshotRequest{
		Tenant:     readiness.Tenant,
		Selector:   readiness.Selector,
		StaleAfter: readiness.StaleAfter,
	})
	if err != nil {
		return nil, fmt.Errorf("read agent registry snapshot: %w", err)
	}
	var out []fleetNATSFanoutTarget
	for _, agent := range snapshot.Agents {
		if !strings.EqualFold(strings.TrimSpace(agent.Health), "ready") {
			continue
		}
		if !agentHasCapability(agent, requiredCapability) {
			continue
		}
		agentID := strings.TrimSpace(agent.AgentID)
		targetID := firstNonEmptyString(agent.TargetID, agent.AgentID)
		if agentID == "" || targetID == "" {
			continue
		}
		out = append(out, fleetNATSFanoutTarget{
			agentID:          agentID,
			targetID:         targetID,
			hostname:         strings.TrimSpace(agent.Hostname),
			workerSubject:    fleetNATSAssignmentSubject(readiness.Tenant, targetID),
			capabilityDigest: strings.TrimSpace(agent.CapabilityDigest),
			labels:           cloneStringMap(agent.Labels),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].targetID != out[j].targetID {
			return out[i].targetID < out[j].targetID
		}
		return out[i].agentID < out[j].agentID
	})
	if len(out) == 0 {
		return nil, fmt.Errorf("no ready capable agents matched runner.readiness.selector")
	}
	return out, nil
}

func (e *customNodeExecutor) finalizeFleetNATSFanoutReceipt(receipt *fleetNATSFanoutReceipt) {
	if receipt == nil {
		return
	}
	for _, result := range receipt.Results {
		status := strings.ToLower(strings.TrimSpace(result.Status))
		if nodeStepSucceeded(status) {
			receipt.Summary.Succeeded++
			continue
		}
		receipt.Summary.NonSucceeded++
		switch status {
		case "blocked":
			receipt.Summary.Blocked++
		case "timeout":
			receipt.Summary.TimedOut++
		default:
			receipt.Summary.Failed++
		}
		if strings.TrimSpace(result.Receipt.Metadata["agentId"]) == "" {
			receipt.Summary.MissingReceipts++
		}
	}
	if receipt.Summary.TargetCount > 0 {
		receipt.Summary.SucceededPercent = (receipt.Summary.Succeeded * 100) / receipt.Summary.TargetCount
	}
	if receipt.Summary.NonSucceeded > receipt.Policy.MaxFailed {
		receipt.Summary.PolicyViolations++
	}
	if receipt.Summary.SucceededPercent < receipt.Policy.MinSucceededPercent {
		receipt.Summary.PolicyViolations++
	}
	switch {
	case receipt.Summary.PolicyViolations == 0 && receipt.Summary.NonSucceeded == 0:
		receipt.Status = "succeeded"
		receipt.Reason = "all targeted NATS agents succeeded"
	case receipt.Summary.PolicyViolations == 0:
		receipt.Status = "partial"
		receipt.Reason = "targeted NATS fan-out completed within execution budget"
	case receipt.Policy.OnPartialFailure == RunnerFanoutOnContinue:
		receipt.Status = "partial"
		receipt.Reason = "targeted NATS fan-out exceeded execution budget; continuing by policy"
	default:
		receipt.Status = "failed"
		receipt.Reason = "targeted NATS fan-out exceeded execution budget"
	}
}

func (e *customNodeExecutor) fleetNATSFanoutOperationResult(started time.Time, receipt fleetNATSFanoutReceipt) transport.OperationResult {
	status := "succeeded"
	switch strings.ToLower(strings.TrimSpace(receipt.Status)) {
	case "blocked":
		status = "blocked"
	case "failed", "timeout":
		status = "failed"
	}
	targets := make([]string, 0, len(receipt.Targets))
	for _, target := range receipt.Targets {
		targets = append(targets, target.WorkerSubject)
	}
	metadata := map[string]string{
		"fanout":              "targeted-nats",
		"targetCount":         strconv.Itoa(receipt.Summary.TargetCount),
		"succeeded":           strconv.Itoa(receipt.Summary.Succeeded),
		"nonSucceeded":        strconv.Itoa(receipt.Summary.NonSucceeded),
		"missingReceipts":     strconv.Itoa(receipt.Summary.MissingReceipts),
		"minSucceededPercent": strconv.Itoa(receipt.Policy.MinSucceededPercent),
		"maxFailed":           strconv.Itoa(receipt.Policy.MaxFailed),
		"onPartialFailure":    receipt.Policy.OnPartialFailure,
		"delivery":            receipt.Policy.Delivery,
		"retryMaxDeliver":     strconv.Itoa(receipt.Policy.Retry.MaxDeliver),
		"retryAckWait":        receipt.Policy.Retry.AckWait.String(),
		"retryOnExhausted":    receipt.Policy.Retry.OnExhausted,
	}
	if len(receipt.Policy.Retry.Backoff) > 0 {
		metadata["retryBackoff"] = joinDurations(receipt.Policy.Retry.Backoff)
	}
	errorMessage := ""
	if !nodeStepSucceeded(status) {
		errorMessage = strings.TrimSpace(receipt.Reason)
	}
	return transport.OperationResult{
		Operation:      "run",
		Status:         status,
		TargetDigest:   digestStringSlice(targets),
		Command:        []string{"nats.fanout", "--delivery", receipt.Policy.Delivery, "--targets", strconv.Itoa(receipt.Summary.TargetCount), "--max-parallel", strconv.Itoa(receipt.Policy.MaxParallel)},
		ExitCode:       boolToExitCode(status != "succeeded"),
		DurationMillis: time.Since(started).Milliseconds(),
		Error:          errorMessage,
		Metadata:       metadata,
	}
}

func (e *customNodeExecutor) validateFleetNATSFanoutOpsGuard(node *runNode, targets []fleetNATSFanoutTarget, operation string) error {
	if e == nil || e.run == nil || e.run.Plan == nil || e.run.Plan.Ops == nil {
		return nil
	}
	for _, target := range targets {
		if err := e.validateHostAdapterOpsGuard(node, target.targetID, operation); err != nil {
			return err
		}
	}
	return nil
}

func (e *customNodeExecutor) recordHostCommandFanoutReceipts(node *runNode, phase string, status string, reason string, observe hostCommandObserveReceipt, plan hostCommandPlanReceipt, fanout fleetNATSFanoutReceipt, execute transport.OperationResult, verify hostCommandVerifyReceipt) {
	payload := map[string]any{
		"apiVersion":   "torque.dev/host-command-node/v1",
		"kind":         "HostCommandNodeArtifact",
		"nodeId":       node.ID,
		"nodeKind":     normalizeNodeKind(node.Kind),
		"phase":        phase,
		"status":       strings.TrimSpace(status),
		"targetId":     strings.TrimSpace(plan.TargetID),
		"guardMode":    strings.TrimSpace(plan.GuardMode),
		"targetDigest": execute.TargetDigest,
		"observe":      observe,
		"plan":         plan,
		"receipt":      execute,
		"execute":      execute,
		"fanout":       fanout,
		"verify":       verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	e.run.RecordJSONArtifact(node.ID, "host-command-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-command-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-command-execute.json", execute)
	e.run.RecordJSONArtifact(node.ID, "host-command-fanout.json", fanout)
	e.run.RecordJSONArtifact(node.ID, "host-command-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (t fleetNATSFanoutTarget) view() fleetNATSFanoutTargetView {
	return fleetNATSFanoutTargetView{
		AgentID:          strings.TrimSpace(t.agentID),
		TargetID:         strings.TrimSpace(t.targetID),
		Hostname:         strings.TrimSpace(t.hostname),
		WorkerSubject:    strings.TrimSpace(t.workerSubject),
		CapabilityDigest: strings.TrimSpace(t.capabilityDigest),
		Labels:           cloneStringMap(t.labels),
	}
}

func (t fleetNATSFanoutTarget) result(receipt transport.OperationResult) fleetNATSFanoutResult {
	return t.resultWithEvidence(receipt, natstransport.CommandAssignment{}, nil, nil)
}

func (t fleetNATSFanoutTarget) resultWithEvidence(receipt transport.OperationResult, assignment natstransport.CommandAssignment, assignmentOffset *natstransport.StreamOffset, receiptOffset *natstransport.StreamOffset) fleetNATSFanoutResult {
	var assignmentPtr *natstransport.CommandAssignment
	if strings.TrimSpace(assignment.Target) != "" {
		copy := assignment
		assignmentPtr = &copy
	}
	return fleetNATSFanoutResult{
		AgentID:          strings.TrimSpace(t.agentID),
		TargetID:         strings.TrimSpace(t.targetID),
		Hostname:         strings.TrimSpace(t.hostname),
		WorkerSubject:    strings.TrimSpace(t.workerSubject),
		Status:           strings.TrimSpace(receipt.Status),
		Error:            strings.TrimSpace(receipt.Error),
		Assignment:       assignmentPtr,
		AssignmentOffset: assignmentOffset,
		ReceiptOffset:    receiptOffset,
		Receipt:          receipt,
	}
}

func fleetNATSAssignmentSubject(tenant string, targetID string) string {
	return natstransport.AssignmentSubject(tenant, targetID)
}

func boolToExitCode(failed bool) int {
	if failed {
		return 1
	}
	return 0
}

func joinDurations(values []time.Duration) string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value > 0 {
			out = append(out, value.String())
		}
	}
	return strings.Join(out, ",")
}
