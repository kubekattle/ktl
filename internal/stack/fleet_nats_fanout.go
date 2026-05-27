package stack

import (
	"context"
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ingresslabs/torque/internal/ops/agent/heartbeat"
	"github.com/ingresslabs/torque/internal/ops/slotledger"
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
	ReceiptRunID       string                      `json:"receiptRunId,omitempty"`
	ResumeFromRunID    string                      `json:"resumeFromRunId,omitempty"`
	RequiredCapability string                      `json:"requiredCapability,omitempty"`
	GeneratedAt        string                      `json:"generatedAt"`
	Policy             fleetNATSFanoutPolicy       `json:"policy"`
	Summary            fleetNATSFanoutSummary      `json:"summary"`
	Targets            []fleetNATSFanoutTargetView `json:"targets,omitempty"`
	Results            []fleetNATSFanoutResult     `json:"results,omitempty"`
}

type fleetNATSFanoutPolicy struct {
	MaxParallel         int                                   `json:"maxParallel"`
	MaxFailed           int                                   `json:"maxFailed"`
	MinSucceededPercent int                                   `json:"minSucceededPercent"`
	OnPartialFailure    string                                `json:"onPartialFailure"`
	Delivery            string                                `json:"delivery"`
	TargetConcurrency   RunnerFanoutTargetConcurrencyResolved `json:"targetConcurrency,omitempty"`
	Retry               RunnerFanoutRetryResolved             `json:"retry,omitempty"`
}

type fleetNATSFanoutSummary struct {
	TargetCount          int `json:"targetCount"`
	Succeeded            int `json:"succeeded"`
	Failed               int `json:"failed,omitempty"`
	Blocked              int `json:"blocked,omitempty"`
	TimedOut             int `json:"timedOut,omitempty"`
	MissingReceipts      int `json:"missingReceipts,omitempty"`
	WorkerSlotsTotal     int `json:"workerSlotsTotal,omitempty"`
	WorkerSlotsAvailable int `json:"workerSlotsAvailable,omitempty"`
	SlotLeases           int `json:"slotLeases,omitempty"`
	SucceededPercent     int `json:"succeededPercent"`
	NonSucceeded         int `json:"nonSucceeded,omitempty"`
	PolicyViolations     int `json:"policyViolations,omitempty"`
}

type fleetNATSFanoutTargetView struct {
	AgentID              string              `json:"agentId"`
	TargetID             string              `json:"targetId"`
	Hostname             string              `json:"hostname,omitempty"`
	WorkerSubject        string              `json:"workerSubject"`
	CapabilityDigest     string              `json:"capabilityDigest,omitempty"`
	WorkerSlots          heartbeat.Slots     `json:"workerSlots,omitempty"`
	WorkerSlotsAvailable int                 `json:"workerSlotsAvailable,omitempty"`
	SlotLease            *fleetNATSSlotLease `json:"slotLease,omitempty"`
	Labels               map[string]string   `json:"labels,omitempty"`
}

type fleetNATSFanoutResult struct {
	AgentID            string                                   `json:"agentId"`
	TargetID           string                                   `json:"targetId"`
	Hostname           string                                   `json:"hostname,omitempty"`
	WorkerSubject      string                                   `json:"workerSubject"`
	Status             string                                   `json:"status"`
	Error              string                                   `json:"error,omitempty"`
	Assignment         *natstransport.CommandAssignment         `json:"assignment,omitempty"`
	AssignmentEnvelope *natstransport.CommandAssignmentEnvelope `json:"assignmentEnvelope,omitempty"`
	AssignmentOffset   *natstransport.StreamOffset              `json:"assignmentOffset,omitempty"`
	ReceiptOffset      *natstransport.StreamOffset              `json:"receiptOffset,omitempty"`
	SlotLease          *fleetNATSSlotLease                      `json:"slotLease,omitempty"`
	Receipt            transport.OperationResult                `json:"receipt"`
}

type fleetNATSFanoutTarget struct {
	agentID          string
	targetID         string
	hostname         string
	workerSubject    string
	capabilityDigest string
	workerSlots      heartbeat.Slots
	slotLease        *fleetNATSSlotLease
	labels           map[string]string
}

type fleetNATSSlotLease struct {
	ID                   string `json:"id"`
	TargetID             string `json:"targetId"`
	LeaseRunID           string `json:"leaseRunId,omitempty"`
	NodeID               string `json:"nodeId,omitempty"`
	SlotIndex            int    `json:"slotIndex"`
	Slots                int    `json:"slots"`
	MaxPerTarget         int    `json:"maxPerTarget"`
	Status               string `json:"status,omitempty"`
	LedgerStore          string `json:"ledgerStore,omitempty"`
	LedgerStoreKey       string `json:"ledgerStoreKey,omitempty"`
	LedgerTokenDigest    string `json:"ledgerTokenDigest,omitempty"`
	WorkerSlotsTotal     int    `json:"workerSlotsTotal"`
	WorkerSlotsInUse     int    `json:"workerSlotsInUse"`
	WorkerSlotsAvailable int    `json:"workerSlotsAvailable"`
	LeaseTTL             string `json:"leaseTtl"`
	AcquiredAt           string `json:"acquiredAt,omitempty"`
	ExpiresAt            string `json:"expiresAt"`
	RenewedAt            string `json:"renewedAt,omitempty"`
	Renewals             int    `json:"renewals,omitempty"`
	RenewalError         string `json:"renewalError,omitempty"`
	ReleasedAt           string `json:"releasedAt,omitempty"`
	ReleaseError         string `json:"releaseError,omitempty"`
	Reclaimed            int    `json:"reclaimed,omitempty"`
	Escrowed             bool   `json:"escrowed,omitempty"`
	Recovered            bool   `json:"leaseRecovered,omitempty"`
	releaseToken         string
	renewalMu            *sync.Mutex
}

type fleetNATSAssignmentSigner struct {
	privateKey   ed25519.PrivateKey
	issuer       string
	tenant       string
	ttl          time.Duration
	policyDigest string
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
	leaseStore, err := e.openFleetNATSSlotLedger(ctx, policy)
	if err != nil {
		receipt.Status = "blocked"
		receipt.Reason = err.Error()
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	if leaseStore != nil {
		defer leaseStore.Close()
	}
	leaseRunID := receipt.RunID
	if policy.Delivery == RunnerFanoutDeliveryJetStream {
		leaseRunID = e.fleetNATSAssignmentRunID(ctx, spec)
	}
	targets, err = e.assignFleetNATSSlotLeases(ctx, leaseStore, policy, leaseRunID, receipt.NodeID, targets)
	if err != nil {
		receipt.Status = "blocked"
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
		e.releaseFleetNATSSlotLeases(ctx, leaseStore, targets, &receipt)
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
		stopRenewal := e.startFleetNATSSlotLeaseRenewal(ctx, leaseStore, policy, targets)
		jetReceipt, _ := e.runHostCommandFleetNATSJetStreamFanout(ctx, started, receipt, targets, spec, command, timeout, server, creds, nkey)
		stopRenewal()
		e.releaseFleetNATSSlotLeases(ctx, leaseStore, targets, &jetReceipt)
		return jetReceipt, e.fleetNATSFanoutOperationResult(started, jetReceipt)
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
		e.releaseFleetNATSSlotLeases(ctx, leaseStore, targets, &receipt)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	defer requester.Close()
	stopRenewal := e.startFleetNATSSlotLeaseRenewal(ctx, leaseStore, policy, targets)

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
					Target:                   target.workerSubject,
					Server:                   server,
					Creds:                    creds,
					NKey:                     nkey,
					Timeout:                  timeout,
					RedactValues:             []string{target.workerSubject, server},
					TargetID:                 target.targetID,
					ExpectedAgentID:          target.agentID,
					RequiredCapability:       strings.TrimSpace(spec.RequiredCap),
					NodeKind:                 strings.TrimSpace(spec.NodeKind),
					RunID:                    strings.TrimSpace(spec.RunID),
					NodeID:                   strings.TrimSpace(spec.NodeID),
					PlanDigest:               strings.TrimSpace(spec.PlanDigest),
					SlotLeaseID:              target.slotLeaseID(),
					SlotLeaseTargetID:        target.slotLeaseTargetID(),
					SlotLeaseIndex:           target.slotLeaseIndex(),
					SlotLeaseSlots:           target.slotLeaseSlots(),
					SlotLeaseTTL:             target.slotLeaseTTL(),
					SlotLeaseExpiresAt:       target.slotLeaseExpiresAt(),
					SlotLeaseToken:           target.slotLeaseToken(),
					SlotLeaseTokenDigest:     target.slotLeaseTokenDigest(),
					SlotLeaseRenewInterval:   target.slotLeaseRenewInterval(policy),
					SlotLeaseLedgerStore:     target.slotLeaseLedgerStore(policy),
					SlotLeaseLedgerStorePath: target.slotLeaseLedgerStorePath(policy),
					SlotLeaseLedgerStoreKey:  target.slotLeaseLedgerStoreKey(),
					SlotLeaseEtcdEndpoints:   target.slotLeaseEtcdEndpoints(policy),
					SlotLeaseEtcdPrefix:      target.slotLeaseEtcdPrefix(policy),
					Requester:                requester,
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
	stopRenewal()

	receipt.Results = results
	e.finalizeFleetNATSFanoutReceipt(&receipt)
	e.releaseFleetNATSSlotLeases(ctx, leaseStore, targets, &receipt)
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
	assignmentRunID := e.fleetNATSAssignmentRunID(ctx, spec)
	resumeFromRunID := ""
	if e != nil && e.run != nil {
		resumeFromRunID = strings.TrimSpace(e.run.ResumeFromRunID)
	}
	receipt.ReceiptRunID = assignmentRunID
	if resumeFromRunID != "" {
		receipt.ResumeFromRunID = resumeFromRunID
	}
	receiptSubject := natstransport.ReceiptSubjectWildcard(tenant, assignmentRunID)
	durable := natstransport.DefaultReceiptConsumer + "-" + natstransport.NormalizeSubjectToken(assignmentRunID+"-"+spec.NodeID, "run")
	storedCheckpoints, err := e.loadFleetNATSReceiptCheckpoints(ctx, assignmentRunID, spec.NodeID)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("load receipt offsets: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	lastStoredSequence := maxFleetNATSReceiptSequence(storedCheckpoints, receiptStream)
	subOpts := []natsgo.SubOpt{
		natsgo.BindStream(receiptStream),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
		natsgo.PullMaxWaiting(128),
	}
	if _, err := js.ConsumerInfo(receiptStream, durable, natsgo.Context(ctx)); err == nil {
		// Existing durable consumers resume from their server-side ACK cursor.
	} else if lastStoredSequence > 0 {
		subOpts = append(subOpts, natsgo.StartSequence(lastStoredSequence+1))
	} else {
		subOpts = append(subOpts, natsgo.DeliverAll())
	}
	sub, err := js.PullSubscribe(
		receiptSubject,
		durable,
		subOpts...,
	)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = fmt.Sprintf("subscribe receipt stream: %v", err)
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}

	results := make([]fleetNATSFanoutResult, len(targets))
	assignments := make([]natstransport.CommandAssignment, len(targets))
	envelopes := make([]*natstransport.CommandAssignmentEnvelope, len(targets))
	assignmentOffsets := make([]*natstransport.StreamOffset, len(targets))
	targetIndex := make(map[string]int, len(targets))
	seenReceipts := make(map[string]struct{}, len(targets))
	signer, err := e.fleetNATSAssignmentSigner(timeout, tenant)
	if err != nil {
		receipt.Status = "failed"
		receipt.Reason = err.Error()
		return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
	}
	for idx, target := range targets {
		targetIndex[target.targetID] = idx
		assignment := natstransport.NewCommandAssignmentWithMetadata("run", target.workerSubject, command, time.Now(), natstransport.CommandAssignmentMetadata{
			TargetID:                 target.targetID,
			ExpectedAgentID:          target.agentID,
			RequiredCapability:       strings.TrimSpace(spec.RequiredCap),
			NodeKind:                 strings.TrimSpace(spec.NodeKind),
			RunID:                    assignmentRunID,
			NodeID:                   strings.TrimSpace(spec.NodeID),
			PlanDigest:               strings.TrimSpace(spec.PlanDigest),
			SlotLeaseID:              target.slotLeaseID(),
			SlotLeaseTargetID:        target.slotLeaseTargetID(),
			SlotLeaseIndex:           target.slotLeaseIndex(),
			SlotLeaseSlots:           target.slotLeaseSlots(),
			SlotLeaseTTL:             target.slotLeaseTTL(),
			SlotLeaseExpiresAt:       target.slotLeaseExpiresAt(),
			SlotLeaseToken:           target.slotLeaseToken(),
			SlotLeaseTokenDigest:     target.slotLeaseTokenDigest(),
			SlotLeaseRenewInterval:   target.slotLeaseRenewInterval(receipt.Policy),
			SlotLeaseLedgerStore:     target.slotLeaseLedgerStore(receipt.Policy),
			SlotLeaseLedgerStorePath: target.slotLeaseLedgerStorePath(receipt.Policy),
			SlotLeaseLedgerStoreKey:  target.slotLeaseLedgerStoreKey(),
			SlotLeaseEtcdEndpoints:   target.slotLeaseEtcdEndpoints(receipt.Policy),
			SlotLeaseEtcdPrefix:      target.slotLeaseEtcdPrefix(receipt.Policy),
		})
		assignments[idx] = assignment
		if stored, ok := storedCheckpoints[assignment.AssignmentID]; ok {
			op := stored.Receipt
			op.Metadata = mergeStringMap(op.Metadata, map[string]string{
				"receiptOffsetResumed": "true",
				"resumeFromRunId":      resumeFromRunID,
				"receiptRunId":         assignmentRunID,
			})
			if e != nil && e.run != nil && strings.TrimSpace(e.run.RunID) != "" && strings.TrimSpace(e.run.RunID) != assignmentRunID {
				if err := e.storeFleetNATSReceiptCheckpointForRunIDs(ctx, []string{strings.TrimSpace(e.run.RunID)}, assignmentRunID, spec.NodeID, target, assignment, stored.Offset, op); err != nil {
					receipt.Status = "failed"
					receipt.Reason = fmt.Sprintf("store resumed JetStream receipt offset: %v", err)
					return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
				}
			}
			results[idx] = target.resultWithEvidence(op, assignment, nil, nil, stored.Offset)
			seenReceipts[fleetNATSReceiptDedupeKey(op, assignment)] = struct{}{}
			continue
		}
		var raw []byte
		if signer != nil {
			envelope, err := signer.sign(assignment)
			if err != nil {
				results[idx] = target.resultWithEvidence(transport.OperationResult{
					Operation:    "run",
					Status:       "failed",
					TargetDigest: natstransport.TargetDigest(target.workerSubject),
					ExitCode:     1,
					Error:        fmt.Sprintf("sign JetStream assignment: %v", err),
				}, assignment, nil, nil, nil)
				continue
			}
			envelopes[idx] = &envelope
			raw, err = json.Marshal(envelope)
		} else {
			raw, err = json.Marshal(assignment)
		}
		if err != nil {
			results[idx] = target.resultWithEvidence(transport.OperationResult{
				Operation:    "run",
				Status:       "failed",
				TargetDigest: natstransport.TargetDigest(target.workerSubject),
				ExitCode:     1,
				Error:        fmt.Sprintf("marshal JetStream assignment: %v", err),
			}, assignment, envelopes[idx], nil, nil)
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
			}, assignment, envelopes[idx], nil, nil)
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
				"receiptRunId":       assignmentRunID,
			},
		}, assignment, envelopes[idx], assignmentOffsets[idx], nil)
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
			key := fleetNATSReceiptDedupeKey(op, assignments[idx])
			if _, seen := seenReceipts[key]; seen && results[idx].Status != "timeout" {
				_ = msg.Ack(natsgo.Context(ctx))
				continue
			}
			if err := e.storeFleetNATSReceiptCheckpoint(ctx, assignmentRunID, spec.NodeID, targets[idx], assignments[idx], receiptOffset, op); err != nil {
				receipt.Status = "failed"
				receipt.Reason = fmt.Sprintf("store JetStream receipt offset: %v", err)
				receipt.Results = results
				e.finalizeFleetNATSFanoutReceipt(&receipt)
				return receipt, e.fleetNATSFanoutOperationResult(started, receipt)
			}
			seenReceipts[key] = struct{}{}
			if results[idx].Status == "timeout" {
				pending--
			}
			results[idx] = targets[idx].resultWithEvidence(op, assignments[idx], envelopes[idx], assignmentOffsets[idx], receiptOffset)
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
		TargetConcurrency: RunnerFanoutTargetConcurrencyResolved{
			RequireAvailable: true,
			MaxPerTarget:     1,
			LeaseTTL:         30 * time.Second,
			Ledger: RunnerFanoutTargetSlotLedgerResolved{
				Store: slotledger.StoreFile,
			},
		},
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
	policy.TargetConcurrency = resolved.TargetConcurrency
	if policy.TargetConcurrency.MaxPerTarget < 1 {
		policy.TargetConcurrency.MaxPerTarget = 1
	}
	if policy.TargetConcurrency.LeaseTTL <= 0 {
		policy.TargetConcurrency.LeaseTTL = 30 * time.Second
	}
	policy.TargetConcurrency.Ledger = normalizeFleetNATSSlotLedgerPolicy(policy.TargetConcurrency.Ledger, e)
	if policy.TargetConcurrency.Ledger.Enabled && policy.TargetConcurrency.Ledger.RenewInterval <= 0 {
		policy.TargetConcurrency.Ledger.RenewInterval = defaultFleetNATSSlotLeaseRenewInterval(policy.TargetConcurrency.LeaseTTL)
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
			workerSlots:      targetWorkerSlots(agent),
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

func (e *customNodeExecutor) assignFleetNATSSlotLeases(ctx context.Context, store slotledger.Store, policy fleetNATSFanoutPolicy, runID string, nodeID string, targets []fleetNATSFanoutTarget) ([]fleetNATSFanoutTarget, error) {
	if !policy.TargetConcurrency.Enabled {
		return targets, nil
	}
	if policy.TargetConcurrency.Ledger.Enabled && store == nil {
		return nil, fmt.Errorf("target slot ledger is enabled but no ledger store is open")
	}
	out := append([]fleetNATSFanoutTarget(nil), targets...)
	now := time.Now().UTC()
	expiresAt := now.Add(policy.TargetConcurrency.LeaseTTL).Format(time.RFC3339Nano)
	for idx := range out {
		target := &out[idx]
		capacity, available := targetWorkerSlotCapacity(target.workerSlots, policy.TargetConcurrency.MaxPerTarget)
		if policy.TargetConcurrency.RequireAvailable && target.workerSlots.Total <= 0 {
			return nil, fmt.Errorf("target %s did not advertise workerSlots", target.targetID)
		}
		if policy.TargetConcurrency.RequireAvailable && available < 1 {
			return nil, fmt.Errorf("target %s has no available worker slots (total=%d inUse=%d maxPerTarget=%d)", target.targetID, target.workerSlots.Total, target.workerSlots.InUse, policy.TargetConcurrency.MaxPerTarget)
		}
		if capacity < 1 {
			capacity = policy.TargetConcurrency.MaxPerTarget
		}
		if available < 1 {
			available = 1
		}
		leaseID := fleetNATSSlotLeaseID(runID, nodeID, target.targetID)
		lease := fleetNATSSlotLease{
			ID:                   leaseID,
			TargetID:             strings.TrimSpace(target.targetID),
			LeaseRunID:           strings.TrimSpace(runID),
			NodeID:               strings.TrimSpace(nodeID),
			SlotIndex:            1,
			Slots:                1,
			MaxPerTarget:         policy.TargetConcurrency.MaxPerTarget,
			Status:               "assigned",
			WorkerSlotsTotal:     target.workerSlots.Total,
			WorkerSlotsInUse:     target.workerSlots.InUse,
			WorkerSlotsAvailable: available,
			LeaseTTL:             policy.TargetConcurrency.LeaseTTL.String(),
			AcquiredAt:           now.Format(time.RFC3339Nano),
			ExpiresAt:            expiresAt,
			renewalMu:            &sync.Mutex{},
		}
		if store != nil && policy.TargetConcurrency.Ledger.Enabled {
			recovered, recoveredOK, recoverErr := e.recoverFleetNATSSlotLease(ctx, store, policy, runID, nodeID, target, leaseID, now)
			if recoverErr != nil {
				return nil, recoverErr
			}
			if recoveredOK {
				lease = recovered
				target.slotLease = &lease
				continue
			}
			reservation, err := store.Reserve(ctx, slotledger.ReserveRequest{
				Tenant:   e.fleetNATSTenant(),
				TargetID: target.targetID,
				Holder:   strings.TrimSpace(e.run.RunID),
				RunID:    strings.TrimSpace(runID),
				NodeID:   strings.TrimSpace(nodeID),
				LeaseID:  leaseID,
				MaxSlots: available,
				Slots:    1,
				TTL:      policy.TargetConcurrency.LeaseTTL,
				Now:      now,
				Metadata: map[string]string{
					"nodeId":        strings.TrimSpace(nodeID),
					"stackRunId":    strings.TrimSpace(e.run.RunID),
					"assignmentRun": strings.TrimSpace(runID),
				},
			})
			if err != nil {
				return nil, fmt.Errorf("reserve target %s slot lease: %w", target.targetID, err)
			}
			if reservation.Decision != "acquired" || reservation.Lease == nil {
				return nil, fmt.Errorf("target %s slot ledger blocked: %s", target.targetID, firstNonEmptyString(reservation.Reason, "no slot lease acquired"))
			}
			lease = fleetNATSSlotLeaseFromRecord(*reservation.Lease, policy, target.workerSlots, reservation, strings.TrimSpace(reservation.Lease.ReleaseToken), false, runID, nodeID)
			if err := e.storeFleetNATSSlotLeaseToken(ctx, lease); err != nil {
				return nil, err
			}
		}
		if lease.WorkerSlotsTotal <= 0 {
			lease.WorkerSlotsTotal = capacity
		}
		target.slotLease = &lease
	}
	return out, nil
}

func normalizeFleetNATSSlotLedgerPolicy(ledger RunnerFanoutTargetSlotLedgerResolved, e *customNodeExecutor) RunnerFanoutTargetSlotLedgerResolved {
	ledger.Store = strings.ToLower(strings.TrimSpace(ledger.Store))
	ledger.StorePath = strings.TrimSpace(ledger.StorePath)
	ledger.EtcdEndpoints = normalizeTrimmedStringSlice(ledger.EtcdEndpoints)
	ledger.EtcdPrefix = strings.TrimSpace(ledger.EtcdPrefix)
	if e != nil && e.run != nil && e.run.Plan != nil {
		readiness := normalizeFleetReadiness(e.run.Plan.Runner.Readiness)
		if strings.TrimSpace(ledger.Store) == "" {
			ledger.Store = readiness.Store
		}
		if ledger.Store == slotledger.StoreEtcd {
			if len(ledger.EtcdEndpoints) == 0 {
				ledger.EtcdEndpoints = append([]string(nil), readiness.EtcdEndpoints...)
			}
			if ledger.EtcdPrefix == "" {
				ledger.EtcdPrefix = readiness.EtcdPrefix
			}
		}
		if ledger.StorePath == "" {
			root := strings.TrimSpace(e.run.Plan.StackRoot)
			if root != "" {
				ledger.StorePath = filepath.Join(root, ".torque", "fleet", "target-slot-ledger.sqlite")
			}
		}
	}
	if ledger.Store == "" {
		ledger.Store = slotledger.StoreFile
	}
	if ledger.EtcdPrefix == "" {
		ledger.EtcdPrefix = slotledger.DefaultStorePrefix
	}
	return ledger
}

func (e *customNodeExecutor) recoverFleetNATSSlotLease(ctx context.Context, store slotledger.Store, policy fleetNATSFanoutPolicy, runID string, nodeID string, target *fleetNATSFanoutTarget, leaseID string, now time.Time) (fleetNATSSlotLease, bool, error) {
	if e == nil || e.run == nil || e.run.store == nil || store == nil || target == nil {
		return fleetNATSSlotLease{}, false, nil
	}
	tokens, err := e.run.store.ListSlotLeaseTokens(ctx, runID, nodeID)
	if err != nil {
		return fleetNATSSlotLease{}, false, fmt.Errorf("read slot lease token escrow: %w", err)
	}
	for _, token := range tokens {
		if strings.TrimSpace(token.TargetID) != strings.TrimSpace(target.targetID) || strings.TrimSpace(token.LeaseID) != strings.TrimSpace(leaseID) {
			continue
		}
		rawToken := strings.TrimSpace(token.Token)
		if rawToken == "" || strings.TrimSpace(token.Status) != slotledger.StatusHeld {
			continue
		}
		renewed, err := store.Renew(ctx, slotledger.RenewRequest{
			Tenant:   e.fleetNATSTenant(),
			TargetID: target.targetID,
			LeaseID:  leaseID,
			Token:    rawToken,
			TTL:      policy.TargetConcurrency.LeaseTTL,
			Now:      now,
		})
		if err != nil {
			return fleetNATSSlotLease{}, false, fmt.Errorf("recover target %s slot lease: %w", target.targetID, err)
		}
		lease := fleetNATSSlotLeaseFromRecord(renewed, policy, target.workerSlots, slotledger.ReserveResult{}, rawToken, true, runID, nodeID)
		lease.RenewedAt = strings.TrimSpace(renewed.UpdatedAt)
		lease.Renewals = 1
		lease.Escrowed = true
		if err := e.storeFleetNATSSlotLeaseToken(ctx, lease); err != nil {
			return fleetNATSSlotLease{}, false, err
		}
		return lease, true, nil
	}
	return fleetNATSSlotLease{}, false, nil
}

func (e *customNodeExecutor) openFleetNATSSlotLedger(ctx context.Context, policy fleetNATSFanoutPolicy) (slotledger.Store, error) {
	if !policy.TargetConcurrency.Enabled || !policy.TargetConcurrency.Ledger.Enabled {
		return nil, nil
	}
	ledger := normalizeFleetNATSSlotLedgerPolicy(policy.TargetConcurrency.Ledger, e)
	switch strings.ToLower(strings.TrimSpace(ledger.Store)) {
	case "", slotledger.StoreFile:
		path := strings.TrimSpace(ledger.StorePath)
		if path == "" {
			path = strings.TrimSpace(os.Getenv("TORQUE_TARGET_SLOT_LEDGER_FILE"))
		}
		if path == "" && e != nil && e.run != nil && e.run.Plan != nil && strings.TrimSpace(e.run.Plan.StackRoot) != "" {
			path = filepath.Join(e.run.Plan.StackRoot, ".torque", "fleet", "target-slot-ledger.sqlite")
		}
		if path == "" {
			return nil, fmt.Errorf("target slot ledger file path is required")
		}
		return slotledger.NewSQLiteStore(ctx, path)
	case slotledger.StoreEtcd:
		endpoints := append([]string(nil), ledger.EtcdEndpoints...)
		if len(endpoints) == 0 {
			endpoints = heartbeat.ParseEtcdEndpoints(firstNonEmptyString(os.Getenv("TORQUE_TARGET_SLOT_LEDGER_ETCD_ENDPOINTS"), os.Getenv("TORQUE_ETCD_ENDPOINTS"), os.Getenv("ETCD_ENDPOINTS")))
		}
		prefix := firstNonEmptyString(ledger.EtcdPrefix, os.Getenv("TORQUE_TARGET_SLOT_LEDGER_ETCD_PREFIX"), slotledger.DefaultStorePrefix)
		return slotledger.NewEtcdStore(ctx, slotledger.EtcdConfig{
			Endpoints:   endpoints,
			Prefix:      prefix,
			DialTimeout: 5 * time.Second,
		})
	default:
		return nil, fmt.Errorf("unsupported target slot ledger store %q", ledger.Store)
	}
}

func (e *customNodeExecutor) startFleetNATSSlotLeaseRenewal(ctx context.Context, store slotledger.Store, policy fleetNATSFanoutPolicy, targets []fleetNATSFanoutTarget) func() {
	if store == nil || !policy.TargetConcurrency.Enabled || !policy.TargetConcurrency.Ledger.Enabled || len(targets) == 0 {
		return func() {}
	}
	interval := policy.TargetConcurrency.Ledger.RenewInterval
	if interval <= 0 {
		interval = defaultFleetNATSSlotLeaseRenewInterval(policy.TargetConcurrency.LeaseTTL)
	}
	if interval <= 0 {
		return func() {}
	}
	renewCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-renewCtx.Done():
				return
			case <-ticker.C:
				e.renewFleetNATSSlotLeases(renewCtx, store, policy, targets)
			}
		}
	}()
	return func() {
		cancel()
		wait := interval + time.Second
		if wait < time.Second {
			wait = time.Second
		}
		select {
		case <-done:
		case <-time.After(wait):
		}
	}
}

func defaultFleetNATSSlotLeaseRenewInterval(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return 0
	}
	interval := ttl / 2
	if interval <= 0 {
		interval = ttl
	}
	if interval < 250*time.Millisecond {
		interval = 250 * time.Millisecond
	}
	return interval
}

func (e *customNodeExecutor) renewFleetNATSSlotLeases(ctx context.Context, store slotledger.Store, policy fleetNATSFanoutPolicy, targets []fleetNATSFanoutTarget) {
	if store == nil || policy.TargetConcurrency.LeaseTTL <= 0 {
		return
	}
	now := time.Now().UTC()
	for idx := range targets {
		lease := targets[idx].slotLease
		if lease == nil {
			continue
		}
		token := lease.releaseTokenValue()
		if token == "" {
			continue
		}
		renewed, err := store.Renew(ctx, slotledger.RenewRequest{
			Tenant:   e.fleetNATSTenant(),
			TargetID: targets[idx].targetID,
			LeaseID:  lease.id(),
			Token:    token,
			TTL:      policy.TargetConcurrency.LeaseTTL,
			Now:      now,
		})
		if err != nil {
			lease.markRenewalError(err)
			continue
		}
		lease.markRenewed(renewed)
		_ = e.storeFleetNATSSlotLeaseToken(ctx, *lease)
	}
}

func (e *customNodeExecutor) releaseFleetNATSSlotLeases(ctx context.Context, store slotledger.Store, targets []fleetNATSFanoutTarget, receipt *fleetNATSFanoutReceipt) {
	if store == nil {
		e.refreshFleetNATSSlotLeases(receipt, targets)
		return
	}
	for idx := range targets {
		lease := targets[idx].slotLease
		if lease == nil || lease.releaseTokenValue() == "" {
			continue
		}
		released, err := store.Release(ctx, slotledger.ReleaseRequest{
			Tenant:   e.fleetNATSTenant(),
			TargetID: targets[idx].targetID,
			LeaseID:  lease.id(),
			Token:    lease.releaseTokenValue(),
			Now:      time.Now().UTC(),
		})
		if err != nil {
			lease.markReleaseError(err)
			if receipt != nil {
				receipt.Status = "failed"
				receipt.Reason = fmt.Sprintf("release target slot lease %s: %v", lease.id(), err)
			}
			continue
		}
		lease.markReleased(released)
		_ = e.storeFleetNATSSlotLeaseToken(ctx, *lease)
	}
	e.refreshFleetNATSSlotLeases(receipt, targets)
}

func (e *customNodeExecutor) refreshFleetNATSSlotLeases(receipt *fleetNATSFanoutReceipt, targets []fleetNATSFanoutTarget) {
	if receipt == nil {
		return
	}
	leases := map[string]*fleetNATSSlotLease{}
	for _, target := range targets {
		if target.slotLease == nil {
			continue
		}
		leases[target.slotLease.ID] = target.slotLease
	}
	for idx := range receipt.Targets {
		if receipt.Targets[idx].SlotLease == nil {
			continue
		}
		if lease := leases[receipt.Targets[idx].SlotLease.ID]; lease != nil {
			receipt.Targets[idx].SlotLease = copyFleetNATSSlotLease(lease)
		}
	}
	for idx := range receipt.Results {
		if receipt.Results[idx].SlotLease == nil {
			continue
		}
		if lease := leases[receipt.Results[idx].SlotLease.ID]; lease != nil {
			receipt.Results[idx].SlotLease = copyFleetNATSSlotLease(lease)
		}
	}
}

func (e *customNodeExecutor) fleetNATSTenant() string {
	if e != nil && e.run != nil && e.run.Plan != nil {
		return normalizeFleetReadiness(e.run.Plan.Runner.Readiness).Tenant
	}
	return natstransport.DefaultTenant
}

func (e *customNodeExecutor) storeFleetNATSSlotLeaseToken(ctx context.Context, lease fleetNATSSlotLease) error {
	if e == nil || e.run == nil || e.run.store == nil {
		return nil
	}
	token := strings.TrimSpace(lease.releaseTokenValue())
	if token == "" && strings.TrimSpace(lease.Status) == slotledger.StatusHeld {
		return nil
	}
	runIDs := []string{strings.TrimSpace(lease.LeaseRunID)}
	currentRunID := strings.TrimSpace(e.run.RunID)
	if currentRunID != "" && currentRunID != strings.TrimSpace(lease.LeaseRunID) {
		runIDs = append(runIDs, currentRunID)
	}
	for _, runID := range runIDs {
		if runID == "" {
			continue
		}
		if err := e.run.store.UpsertSlotLeaseToken(ctx, StackSlotLeaseToken{
			RunID:          runID,
			NodeID:         strings.TrimSpace(lease.NodeID),
			TargetID:       strings.TrimSpace(lease.TargetID),
			LeaseID:        strings.TrimSpace(lease.ID),
			Tenant:         e.fleetNATSTenant(),
			Token:          token,
			TokenDigest:    strings.TrimSpace(lease.LedgerTokenDigest),
			LedgerStore:    strings.TrimSpace(lease.LedgerStore),
			LedgerStoreKey: strings.TrimSpace(lease.LedgerStoreKey),
			Status:         strings.TrimSpace(lease.Status),
			AcquiredAt:     strings.TrimSpace(lease.AcquiredAt),
			ExpiresAt:      strings.TrimSpace(lease.ExpiresAt),
			ReleasedAt:     strings.TrimSpace(lease.ReleasedAt),
			UpdatedAt:      firstNonEmptyString(strings.TrimSpace(lease.RenewedAt), strings.TrimSpace(lease.ReleasedAt), strings.TrimSpace(lease.AcquiredAt)),
		}); err != nil {
			return fmt.Errorf("store slot lease token escrow: %w", err)
		}
	}
	return nil
}

func fleetNATSSlotLeaseFromRecord(record slotledger.LeaseRecord, policy fleetNATSFanoutPolicy, slots heartbeat.Slots, reservation slotledger.ReserveResult, releaseToken string, recovered bool, runID string, nodeID string) fleetNATSSlotLease {
	available := targetWorkerSlotsAvailable(slots)
	if available < 0 {
		available = 0
	}
	lease := fleetNATSSlotLease{
		ID:                   strings.TrimSpace(record.LeaseID),
		TargetID:             strings.TrimSpace(record.TargetID),
		LeaseRunID:           strings.TrimSpace(runID),
		NodeID:               strings.TrimSpace(nodeID),
		SlotIndex:            record.SlotIndex,
		Slots:                record.Slots,
		MaxPerTarget:         policy.TargetConcurrency.MaxPerTarget,
		Status:               record.Status,
		LedgerStore:          strings.TrimSpace(record.Store),
		LedgerStoreKey:       strings.TrimSpace(record.StoreKey),
		LedgerTokenDigest:    strings.TrimSpace(record.TokenDigest),
		WorkerSlotsTotal:     slots.Total,
		WorkerSlotsInUse:     slots.InUse,
		WorkerSlotsAvailable: available,
		LeaseTTL:             policy.TargetConcurrency.LeaseTTL.String(),
		AcquiredAt:           strings.TrimSpace(record.AcquiredAt),
		ExpiresAt:            strings.TrimSpace(record.ExpiresAt),
		ReleasedAt:           strings.TrimSpace(record.ReleasedAt),
		Reclaimed:            len(reservation.Reclaimed),
		Escrowed:             strings.TrimSpace(firstNonEmptyString(releaseToken, record.ReleaseToken)) != "",
		Recovered:            recovered,
		releaseToken:         strings.TrimSpace(firstNonEmptyString(releaseToken, record.ReleaseToken)),
		renewalMu:            &sync.Mutex{},
	}
	if lease.WorkerSlotsTotal <= 0 {
		lease.WorkerSlotsTotal = record.MaxSlots
	}
	return lease
}

func (e *customNodeExecutor) fleetNATSAssignmentSigner(timeout time.Duration, tenant string) (*fleetNATSAssignmentSigner, error) {
	keyPath := strings.TrimSpace(os.Getenv("TORQUE_NATS_ASSIGNMENT_SIGNING_KEY"))
	if keyPath == "" {
		return nil, nil
	}
	_, _, privateKey, err := LoadBundleKey(keyPath)
	if err != nil {
		return nil, fmt.Errorf("load assignment signing key: %w", err)
	}
	if len(privateKey) != ed25519.PrivateKeySize {
		return nil, fmt.Errorf("assignment signing key must contain an ed25519 private key")
	}
	ttl := 5 * time.Minute
	if timeout > 0 && timeout+time.Minute > ttl {
		ttl = timeout + time.Minute
	}
	if raw := strings.TrimSpace(os.Getenv("TORQUE_NATS_ASSIGNMENT_TTL")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			return nil, fmt.Errorf("parse TORQUE_NATS_ASSIGNMENT_TTL: %w", err)
		}
		if parsed <= 0 {
			return nil, fmt.Errorf("TORQUE_NATS_ASSIGNMENT_TTL must be > 0")
		}
		ttl = parsed
	}
	return &fleetNATSAssignmentSigner{
		privateKey:   privateKey,
		issuer:       firstNonEmptyString(os.Getenv("TORQUE_NATS_ASSIGNMENT_ISSUER"), "torque-stack"),
		tenant:       firstNonEmptyString(tenant, os.Getenv("TORQUE_NATS_ASSIGNMENT_TENANT"), natstransport.DefaultTenant),
		ttl:          ttl,
		policyDigest: strings.TrimSpace(os.Getenv("TORQUE_NATS_ASSIGNMENT_POLICY_DIGEST")),
	}, nil
}

func (s *fleetNATSAssignmentSigner) sign(assignment natstransport.CommandAssignment) (natstransport.CommandAssignmentEnvelope, error) {
	if s == nil {
		return natstransport.CommandAssignmentEnvelope{}, fmt.Errorf("assignment signer is nil")
	}
	issuedAt := time.Now().UTC()
	policyDigest := firstNonEmptyString(s.policyDigest, assignment.PlanDigest)
	return natstransport.SignCommandAssignmentEnvelope(assignment, natstransport.CommandAssignmentEnvelopeOptions{
		PrivateKey:   s.privateKey,
		Issuer:       s.issuer,
		Tenant:       s.tenant,
		PolicyDigest: policyDigest,
		IssuedAt:     issuedAt,
		ExpiresAt:    issuedAt.Add(s.ttl),
	})
}

func (e *customNodeExecutor) fleetNATSAssignmentRunID(ctx context.Context, spec HostCommandSpec) string {
	runID := strings.TrimSpace(spec.RunID)
	resumeFromRunID := ""
	if e != nil && e.run != nil {
		resumeFromRunID = strings.TrimSpace(e.run.ResumeFromRunID)
	}
	if resumeFromRunID == "" {
		return runID
	}
	if e != nil && e.run != nil && e.run.store != nil {
		checkpoints, err := e.run.store.ListReceiptOffsets(ctx, resumeFromRunID, spec.NodeID)
		if err == nil {
			for _, checkpoint := range checkpoints {
				if strings.TrimSpace(checkpoint.ReceiptRunID) != "" {
					return strings.TrimSpace(checkpoint.ReceiptRunID)
				}
			}
		}
	}
	return resumeFromRunID
}

func (e *customNodeExecutor) loadFleetNATSReceiptCheckpoints(ctx context.Context, runID string, nodeID string) (map[string]StackReceiptOffsetCheckpoint, error) {
	out := map[string]StackReceiptOffsetCheckpoint{}
	if e == nil || e.run == nil || e.run.store == nil || strings.TrimSpace(runID) == "" {
		return out, nil
	}
	checkpoints, err := e.run.store.ListReceiptOffsets(ctx, runID, nodeID)
	if err != nil {
		return nil, err
	}
	for _, checkpoint := range checkpoints {
		assignmentID := strings.TrimSpace(checkpoint.AssignmentID)
		if assignmentID == "" {
			continue
		}
		out[assignmentID] = checkpoint
	}
	return out, nil
}

func (e *customNodeExecutor) storeFleetNATSReceiptCheckpoint(ctx context.Context, assignmentRunID string, nodeID string, target fleetNATSFanoutTarget, assignment natstransport.CommandAssignment, offset *natstransport.StreamOffset, receipt transport.OperationResult) error {
	if e == nil || e.run == nil || e.run.store == nil {
		return nil
	}
	runIDs := []string{strings.TrimSpace(assignmentRunID)}
	currentRunID := strings.TrimSpace(e.run.RunID)
	if currentRunID != "" && currentRunID != strings.TrimSpace(assignmentRunID) {
		runIDs = append(runIDs, currentRunID)
	}
	return e.storeFleetNATSReceiptCheckpointForRunIDs(ctx, runIDs, assignmentRunID, nodeID, target, assignment, offset, receipt)
}

func (e *customNodeExecutor) storeFleetNATSReceiptCheckpointForRunIDs(ctx context.Context, runIDs []string, assignmentRunID string, nodeID string, target fleetNATSFanoutTarget, assignment natstransport.CommandAssignment, offset *natstransport.StreamOffset, receipt transport.OperationResult) error {
	if e == nil || e.run == nil || e.run.store == nil {
		return nil
	}
	targetID := firstNonEmptyString(receipt.Metadata["assignmentTargetId"], receipt.Metadata["targetId"], target.targetID, assignment.TargetID)
	checkpoint := StackReceiptOffsetCheckpoint{
		ReceiptRunID:  strings.TrimSpace(assignmentRunID),
		NodeID:        strings.TrimSpace(nodeID),
		TargetID:      strings.TrimSpace(targetID),
		AssignmentID:  firstNonEmptyString(receipt.Metadata["assignmentId"], assignment.AssignmentID),
		AgentID:       firstNonEmptyString(receipt.Metadata["agentId"], target.agentID),
		WorkerSubject: firstNonEmptyString(receipt.Metadata["workerSubject"], target.workerSubject),
		Offset:        offset,
		LastSeenAt:    time.Now().UTC().Format(time.RFC3339Nano),
		Receipt:       receipt,
	}
	for _, runID := range runIDs {
		if runID == "" {
			continue
		}
		checkpoint.RunID = runID
		if err := e.run.store.UpsertReceiptOffset(ctx, checkpoint); err != nil {
			return err
		}
	}
	return nil
}

func maxFleetNATSReceiptSequence(checkpoints map[string]StackReceiptOffsetCheckpoint, receiptStream string) uint64 {
	var maxSeq uint64
	for _, checkpoint := range checkpoints {
		if checkpoint.Offset == nil {
			continue
		}
		if strings.TrimSpace(receiptStream) != "" && strings.TrimSpace(checkpoint.Offset.Stream) != "" && strings.TrimSpace(checkpoint.Offset.Stream) != strings.TrimSpace(receiptStream) {
			continue
		}
		if checkpoint.Offset.Sequence > maxSeq {
			maxSeq = checkpoint.Offset.Sequence
		}
	}
	return maxSeq
}

func fleetNATSReceiptDedupeKey(receipt transport.OperationResult, assignment natstransport.CommandAssignment) string {
	return strings.Join([]string{
		firstNonEmptyString(receipt.Metadata["assignmentId"], assignment.AssignmentID),
		firstNonEmptyString(receipt.Metadata["assignmentTargetId"], receipt.Metadata["targetId"], assignment.TargetID),
		firstNonEmptyString(receipt.Metadata["agentId"], assignment.ExpectedAgentID),
	}, "\x00")
}

func mergeStringMap(base map[string]string, overlay map[string]string) map[string]string {
	if len(base) == 0 && len(overlay) == 0 {
		return nil
	}
	out := make(map[string]string, len(base)+len(overlay))
	for k, v := range base {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	for k, v := range overlay {
		if strings.TrimSpace(v) != "" {
			out[k] = v
		}
	}
	return out
}

func (e *customNodeExecutor) finalizeFleetNATSFanoutReceipt(receipt *fleetNATSFanoutReceipt) {
	if receipt == nil {
		return
	}
	for _, target := range receipt.Targets {
		receipt.Summary.WorkerSlotsTotal += target.WorkerSlots.Total
		receipt.Summary.WorkerSlotsAvailable += target.WorkerSlotsAvailable
		if target.SlotLease != nil {
			receipt.Summary.SlotLeases++
		}
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
		"targetConcurrency":   strconv.FormatBool(receipt.Policy.TargetConcurrency.Enabled),
		"targetMaxPerTarget":  strconv.Itoa(receipt.Policy.TargetConcurrency.MaxPerTarget),
		"targetLeaseTTL":      receipt.Policy.TargetConcurrency.LeaseTTL.String(),
		"slotLeases":          strconv.Itoa(receipt.Summary.SlotLeases),
	}
	if receipt.Policy.TargetConcurrency.Ledger.Enabled {
		metadata["slotLedger"] = "true"
		metadata["slotLedgerStore"] = receipt.Policy.TargetConcurrency.Ledger.Store
		metadata["slotLedgerRenewInterval"] = receipt.Policy.TargetConcurrency.Ledger.RenewInterval.String()
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
		AgentID:              strings.TrimSpace(t.agentID),
		TargetID:             strings.TrimSpace(t.targetID),
		Hostname:             strings.TrimSpace(t.hostname),
		WorkerSubject:        strings.TrimSpace(t.workerSubject),
		CapabilityDigest:     strings.TrimSpace(t.capabilityDigest),
		WorkerSlots:          t.workerSlots,
		WorkerSlotsAvailable: targetWorkerSlotsAvailable(t.workerSlots),
		SlotLease:            t.slotLeaseCopy(),
		Labels:               cloneStringMap(t.labels),
	}
}

func (t fleetNATSFanoutTarget) result(receipt transport.OperationResult) fleetNATSFanoutResult {
	return t.resultWithEvidence(receipt, natstransport.CommandAssignment{}, nil, nil, nil)
}

func (t fleetNATSFanoutTarget) resultWithEvidence(receipt transport.OperationResult, assignment natstransport.CommandAssignment, envelope *natstransport.CommandAssignmentEnvelope, assignmentOffset *natstransport.StreamOffset, receiptOffset *natstransport.StreamOffset) fleetNATSFanoutResult {
	var assignmentPtr *natstransport.CommandAssignment
	if strings.TrimSpace(assignment.Target) != "" {
		copy := natstransport.RedactCommandAssignmentSecrets(assignment)
		assignmentPtr = &copy
	}
	var envelopePtr *natstransport.CommandAssignmentEnvelope
	if envelope != nil && strings.TrimSpace(envelope.Kind) != "" {
		copy := natstransport.RedactCommandAssignmentEnvelopeSecrets(*envelope)
		envelopePtr = &copy
	}
	return fleetNATSFanoutResult{
		AgentID:            strings.TrimSpace(t.agentID),
		TargetID:           strings.TrimSpace(t.targetID),
		Hostname:           strings.TrimSpace(t.hostname),
		WorkerSubject:      strings.TrimSpace(t.workerSubject),
		Status:             strings.TrimSpace(receipt.Status),
		Error:              strings.TrimSpace(receipt.Error),
		Assignment:         assignmentPtr,
		AssignmentEnvelope: envelopePtr,
		AssignmentOffset:   assignmentOffset,
		ReceiptOffset:      receiptOffset,
		SlotLease:          t.slotLeaseCopy(),
		Receipt:            receipt,
	}
}

func (t fleetNATSFanoutTarget) slotLeaseCopy() *fleetNATSSlotLease {
	return copyFleetNATSSlotLease(t.slotLease)
}

func copyFleetNATSSlotLease(lease *fleetNATSSlotLease) *fleetNATSSlotLease {
	if lease == nil {
		return nil
	}
	if lease.renewalMu != nil {
		lease.renewalMu.Lock()
		defer lease.renewalMu.Unlock()
	}
	copy := *lease
	copy.releaseToken = ""
	copy.renewalMu = nil
	return &copy
}

func (l *fleetNATSSlotLease) id() string {
	if l == nil {
		return ""
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	return strings.TrimSpace(l.ID)
}

func (l *fleetNATSSlotLease) releaseTokenValue() string {
	if l == nil {
		return ""
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	return strings.TrimSpace(l.releaseToken)
}

func (l *fleetNATSSlotLease) markRenewed(record slotledger.LeaseRecord) {
	if l == nil {
		return
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	l.Status = strings.TrimSpace(record.Status)
	l.ExpiresAt = strings.TrimSpace(record.ExpiresAt)
	l.RenewedAt = strings.TrimSpace(record.UpdatedAt)
	l.Renewals++
	l.RenewalError = ""
}

func (l *fleetNATSSlotLease) markRenewalError(err error) {
	if l == nil || err == nil {
		return
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	l.RenewalError = err.Error()
}

func (l *fleetNATSSlotLease) markReleased(record slotledger.LeaseRecord) {
	if l == nil {
		return
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	l.Status = strings.TrimSpace(record.Status)
	l.ReleasedAt = strings.TrimSpace(record.ReleasedAt)
	l.ReleaseError = ""
	l.releaseToken = ""
}

func (l *fleetNATSSlotLease) markReleaseError(err error) {
	if l == nil || err == nil {
		return
	}
	if l.renewalMu != nil {
		l.renewalMu.Lock()
		defer l.renewalMu.Unlock()
	}
	l.Status = "release_failed"
	l.ReleaseError = err.Error()
}

func (t fleetNATSFanoutTarget) slotLeaseID() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.ID)
}

func (t fleetNATSFanoutTarget) slotLeaseTargetID() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.TargetID)
}

func (t fleetNATSFanoutTarget) slotLeaseIndex() int {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return 0
	}
	return lease.SlotIndex
}

func (t fleetNATSFanoutTarget) slotLeaseSlots() int {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return 0
	}
	return lease.Slots
}

func (t fleetNATSFanoutTarget) slotLeaseTTL() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.LeaseTTL)
}

func (t fleetNATSFanoutTarget) slotLeaseExpiresAt() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.ExpiresAt)
}

func (t fleetNATSFanoutTarget) slotLeaseToken() string {
	if t.slotLease == nil {
		return ""
	}
	return t.slotLease.releaseTokenValue()
}

func (t fleetNATSFanoutTarget) slotLeaseTokenDigest() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.LedgerTokenDigest)
}

func (t fleetNATSFanoutTarget) slotLeaseRenewInterval(policy fleetNATSFanoutPolicy) string {
	if t.slotLease == nil {
		return ""
	}
	interval := policy.TargetConcurrency.Ledger.RenewInterval
	if interval <= 0 {
		interval = defaultFleetNATSSlotLeaseRenewInterval(policy.TargetConcurrency.LeaseTTL)
	}
	if interval <= 0 {
		return ""
	}
	return interval.String()
}

func (t fleetNATSFanoutTarget) slotLeaseLedgerStore(policy fleetNATSFanoutPolicy) string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return firstNonEmptyString(strings.TrimSpace(lease.LedgerStore), strings.TrimSpace(policy.TargetConcurrency.Ledger.Store))
}

func (t fleetNATSFanoutTarget) slotLeaseLedgerStorePath(policy fleetNATSFanoutPolicy) string {
	if t.slotLease == nil {
		return ""
	}
	if strings.EqualFold(strings.TrimSpace(policy.TargetConcurrency.Ledger.Store), slotledger.StoreEtcd) {
		return ""
	}
	return strings.TrimSpace(policy.TargetConcurrency.Ledger.StorePath)
}

func (t fleetNATSFanoutTarget) slotLeaseLedgerStoreKey() string {
	lease := copyFleetNATSSlotLease(t.slotLease)
	if lease == nil {
		return ""
	}
	return strings.TrimSpace(lease.LedgerStoreKey)
}

func (t fleetNATSFanoutTarget) slotLeaseEtcdEndpoints(policy fleetNATSFanoutPolicy) []string {
	if t.slotLease == nil || !strings.EqualFold(strings.TrimSpace(policy.TargetConcurrency.Ledger.Store), slotledger.StoreEtcd) {
		return nil
	}
	return append([]string(nil), policy.TargetConcurrency.Ledger.EtcdEndpoints...)
}

func (t fleetNATSFanoutTarget) slotLeaseEtcdPrefix(policy fleetNATSFanoutPolicy) string {
	if t.slotLease == nil || !strings.EqualFold(strings.TrimSpace(policy.TargetConcurrency.Ledger.Store), slotledger.StoreEtcd) {
		return ""
	}
	return strings.TrimSpace(policy.TargetConcurrency.Ledger.EtcdPrefix)
}

func targetWorkerSlots(agent heartbeat.AgentStatus) heartbeat.Slots {
	slots := agent.WorkerSlots
	if slots.Total == 0 && slots.InUse == 0 {
		slots = agent.Slots
	}
	return heartbeat.Slots{
		Total: slots.Total,
		InUse: slots.InUse,
	}
}

func targetWorkerSlotsAvailable(slots heartbeat.Slots) int {
	_, available := targetWorkerSlotCapacity(slots, 0)
	return available
}

func targetWorkerSlotCapacity(slots heartbeat.Slots, maxPerTarget int) (int, int) {
	total := slots.Total
	if total < 0 {
		total = 0
	}
	inUse := slots.InUse
	if inUse < 0 {
		inUse = 0
	}
	capacity := total
	if maxPerTarget > 0 && (capacity == 0 || maxPerTarget < capacity) {
		capacity = maxPerTarget
	}
	if capacity < 0 {
		capacity = 0
	}
	if inUse > capacity {
		inUse = capacity
	}
	available := capacity - inUse
	if available < 0 {
		available = 0
	}
	return capacity, available
}

func fleetNATSSlotLeaseID(runID string, nodeID string, targetID string) string {
	return digestString(strings.Join([]string{
		"fleet-nats-slot-lease/v1",
		strings.TrimSpace(runID),
		strings.TrimSpace(nodeID),
		strings.TrimSpace(targetID),
	}, "\x00"))
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
