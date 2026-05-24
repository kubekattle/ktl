package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const defaultKubernetesCertRenewBefore = 30 * 24 * time.Hour
const defaultKubernetesHealthCheckAttempts = 24
const defaultKubernetesHealthCheckSleep = 5 * time.Second

type kubernetesCertTargetRunner struct {
	spec   KubernetesCertTarget
	runner hostCommandRunner
}

type kubernetesCertBatch struct {
	Index   int
	Targets []kubernetesCertTargetRunner
}

type kubernetesCertTargetState struct {
	APIVersion        string `json:"apiVersion"`
	Kind              string `json:"kind"`
	Status            string `json:"status"`
	Phase             string `json:"phase"`
	ForceOnceID       string `json:"forceOnceId,omitempty"`
	IntentDigest      string `json:"intentDigest,omitempty"`
	TargetID          string `json:"targetId"`
	Provider          string `json:"provider,omitempty"`
	Role              string `json:"role,omitempty"`
	Batch             int    `json:"batch,omitempty"`
	HealthDigest      string `json:"healthDigest,omitempty"`
	PreInspectDigest  string `json:"preInspectDigest,omitempty"`
	PostInspectDigest string `json:"postInspectDigest,omitempty"`
	Error             string `json:"error,omitempty"`
	StartedAt         string `json:"startedAt,omitempty"`
	UpdatedAt         string `json:"updatedAt,omitempty"`
	CompletedAt       string `json:"completedAt,omitempty"`
}

type kubernetesCertTargetEvidence struct {
	ID                  string                    `json:"id"`
	Role                string                    `json:"role,omitempty"`
	Provider            string                    `json:"provider"`
	Service             string                    `json:"service,omitempty"`
	Batch               int                       `json:"batch,omitempty"`
	CheckpointPath      string                    `json:"checkpointPath,omitempty"`
	CheckpointStatus    string                    `json:"checkpointStatus,omitempty"`
	CheckpointPhase     string                    `json:"checkpointPhase,omitempty"`
	IntentDigest        string                    `json:"intentDigest,omitempty"`
	HealthDigest        string                    `json:"healthDigest,omitempty"`
	TargetDigest        string                    `json:"targetDigest,omitempty"`
	CertificateCount    int                       `json:"certificateCount,omitempty"`
	EarliestExpiry      string                    `json:"earliestExpiry,omitempty"`
	RenewalNeeded       bool                      `json:"renewalNeeded,omitempty"`
	Renewed             bool                      `json:"renewed,omitempty"`
	SkippedReason       string                    `json:"skippedReason,omitempty"`
	DetectionReceipt    transport.OperationResult `json:"detectionReceipt,omitempty"`
	PreInspectReceipt   transport.OperationResult `json:"preInspectReceipt,omitempty"`
	PreHealthReceipt    transport.OperationResult `json:"preHealthReceipt,omitempty"`
	ResumeHealthReceipt transport.OperationResult `json:"resumeHealthReceipt,omitempty"`
	StateReceipt        transport.OperationResult `json:"stateReceipt,omitempty"`
	CheckpointReceipt   transport.OperationResult `json:"checkpointReceipt,omitempty"`
	RenewReceipt        transport.OperationResult `json:"renewReceipt,omitempty"`
	PostInspectReceipt  transport.OperationResult `json:"postInspectReceipt,omitempty"`
	VerifyReceipt       transport.OperationResult `json:"verifyReceipt,omitempty"`
}

type kubernetesCertTargetsFromEvidence struct {
	SourceNodeID    string                                    `json:"sourceNodeId"`
	Artifact        string                                    `json:"artifact"`
	Provider        string                                    `json:"provider,omitempty"`
	AddressType     string                                    `json:"addressType,omitempty"`
	RoleFilter      []string                                  `json:"roleFilter,omitempty"`
	IncludeNotReady bool                                      `json:"includeNotReady,omitempty"`
	DerivedCount    int                                       `json:"derivedCount"`
	Skipped         []kubernetesCertTargetsFromSkipEvidence   `json:"skipped,omitempty"`
	Targets         []kubernetesCertTargetsFromTargetEvidence `json:"targets,omitempty"`
}

type kubernetesCertTargetsFromTargetEvidence struct {
	ID          string `json:"id"`
	Role        string `json:"role,omitempty"`
	Provider    string `json:"provider,omitempty"`
	AddressType string `json:"addressType,omitempty"`
	Address     string `json:"address,omitempty"`
	Transport   string `json:"transport,omitempty"`
	TargetEnv   string `json:"targetEnv,omitempty"`
	NodeAddress string `json:"nodeAddress,omitempty"`
}

type kubernetesCertTargetsFromSkipEvidence struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

func (e *customNodeExecutor) runKubernetesCertInspectNode(ctx context.Context, node *runNode, command string) error {
	return e.runKubernetesCertNode(ctx, node, command, false)
}

func (e *customNodeExecutor) runKubernetesCertRenewNode(ctx context.Context, node *runNode, command string) error {
	return e.runKubernetesCertNode(ctx, node, command, true)
}

func (e *customNodeExecutor) runKubernetesCertNode(ctx context.Context, node *runNode, command string, renew bool) error {
	phase := "k8s-cert-inspect"
	if renew {
		phase = "k8s-cert-renew"
	}
	if strings.EqualFold(command, "delete") {
		e.recordKubernetesCertArtifact(node, phase, "skipped", "delete does not mutate Kubernetes certificates", nil)
		return nil
	}
	cursor := map[string]any{
		"kind":  normalizeNodeKind(node.Kind),
		"phase": phase,
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		e.recordKubernetesCertArtifact(node, phase, "skipped", reason, nil)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	certs := node.Kubernetes.Certificates
	var policyDecision *kubernetesLifecyclePolicyDecision
	attachPolicy := func(payload map[string]any) map[string]any {
		if policyDecision == nil {
			return payload
		}
		payload["policyStatus"] = strings.TrimSpace(policyDecision.Status)
		payload["policyDecisionArtifact"] = kubernetesLifecyclePolicyDecisionArtifact
		return payload
	}
	renewBefore := defaultKubernetesCertRenewBefore
	if certs.RenewBefore != nil && *certs.RenewBefore > 0 {
		renewBefore = *certs.RenewBefore
	}
	targetsFromEvidence, err := e.resolveKubernetesCertTargetsFrom(ctx, node, &certs)
	if err != nil {
		payload := attachPolicy(e.kubernetesCertPayload(node, phase, "failed", err.Error(), nil, false, targetsFromEvidence))
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		runErr := &RunError{Class: "K8S_CERT_TARGET_RESOLVE_FAILED", Message: err.Error(), Digest: computeRunErrorDigest("K8S_CERT_TARGET_RESOLVE_FAILED", err.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, err.Error(), map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	runners, err := kubernetesCertTargetRunners(certs.Targets)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	batches := kubernetesCertTargetBatches(runners, certs)
	if renew && kubernetesLifecyclePolicyConfigured(certs.Policy) {
		decision := e.evaluateKubernetesLifecyclePolicy(ctx, node, certs, targetsFromEvidence, runners, batches)
		policyDecision = &decision
		if decision.Status == "blocked" {
			overrideDecision := e.evaluateKubernetesLifecyclePolicyOverride(node, certs, decision, runners)
			overrideAttempted := overrideDecision.RuntimeEnabled || kubernetesLifecyclePolicyOverrideConfigured(certs.Policy.Override)
			if overrideAttempted {
				e.run.RecordJSONArtifact(node.ID, kubernetesLifecyclePolicyOverrideArtifact, overrideDecision)
			}
			if overrideDecision.Status == "approved" {
				decision.Status = "override-approved"
				decision.Message = strings.TrimSpace(overrideDecision.Message)
				if decision.Message == "" {
					decision.Message = "lifecycle policy override approved"
				}
				policyDecision = &decision
				e.run.RecordJSONArtifact(node.ID, kubernetesLifecyclePolicyDecisionArtifact, decision)
			} else {
				e.run.RecordJSONArtifact(node.ID, kubernetesLifecyclePolicyDecisionArtifact, decision)
				msg := strings.TrimSpace(decision.Message)
				if overrideAttempted && strings.TrimSpace(overrideDecision.Message) != "" {
					msg = strings.TrimSpace(overrideDecision.Message)
				}
				if msg == "" {
					msg = "lifecycle policy blocked Kubernetes certificate renewal"
				}
				payload := attachPolicy(e.kubernetesCertPayload(node, phase, "failed", msg, nil, false, targetsFromEvidence))
				e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
				e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
				runErr := &RunError{Class: "K8S_LIFECYCLE_POLICY_BLOCKED", Message: msg, Digest: computeRunErrorDigest("K8S_LIFECYCLE_POLICY_BLOCKED", msg)}
				e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
					"phase":  phase,
					"status": "failure",
					"cursor": cursor,
				}, runErr, true)
				return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s", msg))
			}
		} else {
			e.run.RecordJSONArtifact(node.ID, kubernetesLifecyclePolicyDecisionArtifact, decision)
		}
	}
	targets := make([]kubernetesCertTargetEvidence, 0, len(runners))
	changed := false
	for _, batch := range batches {
		for _, targetRunner := range batch.Targets {
			targetEvidence, targetChanged, err := e.runKubernetesCertTarget(ctx, node, targetRunner, renew, renewBefore, batch.Index)
			targets = append(targets, targetEvidence)
			if targetChanged {
				changed = true
			}
			if err != nil {
				payload := attachPolicy(e.kubernetesCertPayload(node, phase, "failed", err.Error(), targets, changed, targetsFromEvidence))
				e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
				e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
				class := kubernetesCertErrorClass(err)
				runErr := &RunError{Class: class, Message: err.Error(), Digest: computeRunErrorDigest(class, err.Error())}
				e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, err.Error(), map[string]any{
					"phase":   phase,
					"status":  "failure",
					"cursor":  cursor,
					"changed": changed,
				}, runErr, true)
				return wrapNodeErr(node.ResolvedRelease, err)
			}
		}
	}
	if strings.TrimSpace(certs.VerifyCommand) != "" && len(runners) > 0 {
		verify := runners[0].runAccess(ctx, certs.VerifyCommand)
		if len(targets) > 0 {
			targets[len(targets)-1].VerifyReceipt = verify
		}
		if !nodeStepSucceeded(verify.Status) {
			msg := strings.TrimSpace(verify.Stderr)
			if msg == "" {
				msg = strings.TrimSpace(verify.Error)
			}
			if msg == "" {
				msg = "Kubernetes certificate lifecycle verify command failed"
			}
			payload := attachPolicy(e.kubernetesCertPayload(node, phase, "failed", msg, targets, changed, targetsFromEvidence))
			e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
			e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
			runErr := &RunError{Class: "K8S_CERT_VERIFY_FAILED", Message: msg, Digest: computeRunErrorDigest("K8S_CERT_VERIFY_FAILED", msg)}
			e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
				"phase":   phase,
				"status":  "failure",
				"cursor":  cursor,
				"changed": changed,
			}, runErr, true)
			return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s", msg))
		}
	}
	status := "succeeded"
	message := "certificates inspected"
	if renew {
		if changed {
			message = "certificates renewed"
		} else {
			message = "certificates already fresh"
		}
	}
	payload := attachPolicy(e.kubernetesCertPayload(node, phase, status, message, targets, changed, targetsFromEvidence))
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, message, map[string]any{
		"phase":   phase,
		"status":  status,
		"changed": changed,
		"cursor":  cursor,
	}, nil)
	return nil
}

func (e *customNodeExecutor) runKubernetesCertTarget(ctx context.Context, node *runNode, target kubernetesCertTargetRunner, renew bool, renewBefore time.Duration, batch int) (kubernetesCertTargetEvidence, bool, error) {
	spec := target.spec
	evidence := kubernetesCertTargetEvidence{
		ID:             strings.TrimSpace(spec.ID),
		Role:           strings.TrimSpace(spec.Role),
		Batch:          batch,
		CheckpointPath: kubernetesCertTargetStatePath(node.Kubernetes.Certificates, strings.TrimSpace(spec.ID)),
		IntentDigest:   kubernetesCertTargetIntentDigest(node, spec),
		TargetDigest:   target.runner.TargetDigest(),
	}
	provider, detection := detectKubernetesCertProvider(ctx, target, node.Kubernetes.Provider)
	evidence.Provider = provider
	evidence.DetectionReceipt = detection
	if !nodeStepSucceeded(detection.Status) {
		return evidence, false, fmt.Errorf("detect Kubernetes certificate provider on %s: %s", evidence.ID, firstReceiptMessage(detection))
	}
	inspectReceipt := target.runNode(ctx, kubernetesCertInspectCommand(provider, spec))
	evidence.PreInspectReceipt = inspectReceipt
	if !nodeStepSucceeded(inspectReceipt.Status) {
		return evidence, false, fmt.Errorf("inspect Kubernetes certificates on %s: %s", evidence.ID, firstReceiptMessage(inspectReceipt))
	}
	expiry, count := parseKubernetesCertExpiry(inspectReceipt.Stdout)
	evidence.CertificateCount = count
	if !expiry.IsZero() {
		evidence.EarliestExpiry = expiry.UTC().Format(time.RFC3339)
		evidence.RenewalNeeded = time.Until(expiry) <= renewBefore
	}
	if !renew {
		return evidence, false, nil
	}
	state, stateReceipt, hasState, err := readKubernetesCertTargetState(ctx, target, node.Kubernetes.Certificates, evidence.ID)
	evidence.StateReceipt = stateReceipt
	if !nodeStepSucceeded(stateReceipt.Status) {
		return evidence, false, fmt.Errorf("read Kubernetes certificate checkpoint on %s: %s", evidence.ID, firstReceiptMessage(stateReceipt))
	}
	if err != nil {
		return evidence, false, fmt.Errorf("read Kubernetes certificate checkpoint on %s: %w", evidence.ID, err)
	}
	if hasState {
		evidence.CheckpointStatus = strings.TrimSpace(state.Status)
		evidence.CheckpointPhase = strings.TrimSpace(state.Phase)
		if kubernetesCertStateCompletedForIntent(state, evidence.IntentDigest, node.Kubernetes.Certificates, evidence.ID) {
			evidence.SkippedReason = "checkpoint already completed"
			return evidence, false, nil
		}
		if kubernetesCertStateInProgressForIntent(state, evidence.IntentDigest) {
			healthDigest, healthReceipt, err := kubernetesCertHealthDigest(ctx, target, node.Kubernetes.Certificates)
			evidence.ResumeHealthReceipt = healthReceipt
			if err != nil {
				return evidence, false, fmt.Errorf("check Kubernetes health before resuming %s: %w", evidence.ID, err)
			}
			if state.HealthDigest != "" && healthDigest != "" && state.HealthDigest != healthDigest {
				return evidence, false, fmt.Errorf("checkpoint blocked for %s: cluster health changed since checkpoint (%s != %s)", evidence.ID, state.HealthDigest, healthDigest)
			}
		}
	}
	if !node.Kubernetes.Certificates.Force && !evidence.RenewalNeeded {
		evidence.SkippedReason = "certificates outside renewal window"
		return evidence, false, nil
	}
	healthDigest, healthReceipt, err := kubernetesCertHealthDigest(ctx, target, node.Kubernetes.Certificates)
	evidence.PreHealthReceipt = healthReceipt
	evidence.HealthDigest = healthDigest
	if err != nil {
		return evidence, false, fmt.Errorf("check Kubernetes health before renewing %s: %w", evidence.ID, err)
	}
	preInspectDigest := digestString(inspectReceipt.Stdout)
	checkpoint := kubernetesCertTargetCheckpoint(node, evidence, "running", "pre-renew", healthDigest, preInspectDigest, "")
	checkpointReceipt := writeKubernetesCertTargetState(ctx, target, node.Kubernetes.Certificates, checkpoint)
	evidence.CheckpointReceipt = checkpointReceipt
	if !nodeStepSucceeded(checkpointReceipt.Status) {
		return evidence, false, fmt.Errorf("write Kubernetes certificate checkpoint on %s: %s", evidence.ID, firstReceiptMessage(checkpointReceipt))
	}
	evidence.CheckpointStatus = checkpoint.Status
	evidence.CheckpointPhase = checkpoint.Phase
	renewReceipt := target.runNode(ctx, kubernetesCertRenewCommand(provider, spec, node.Kubernetes.Certificates))
	evidence.RenewReceipt = renewReceipt
	if !nodeStepSucceeded(renewReceipt.Status) {
		failed := kubernetesCertTargetCheckpoint(node, evidence, "failed", "renew", healthDigest, preInspectDigest, firstReceiptMessage(renewReceipt))
		evidence.CheckpointReceipt = writeKubernetesCertTargetState(ctx, target, node.Kubernetes.Certificates, failed)
		evidence.CheckpointStatus = failed.Status
		evidence.CheckpointPhase = failed.Phase
		return evidence, false, fmt.Errorf("renew Kubernetes certificates on %s: %s", evidence.ID, firstReceiptMessage(renewReceipt))
	}
	evidence.Renewed = true
	postInspect := target.runNode(ctx, kubernetesCertInspectCommand(provider, spec))
	evidence.PostInspectReceipt = postInspect
	if !nodeStepSucceeded(postInspect.Status) {
		failed := kubernetesCertTargetCheckpoint(node, evidence, "failed", "post-inspect", healthDigest, preInspectDigest, firstReceiptMessage(postInspect))
		evidence.CheckpointReceipt = writeKubernetesCertTargetState(ctx, target, node.Kubernetes.Certificates, failed)
		evidence.CheckpointStatus = failed.Status
		evidence.CheckpointPhase = failed.Phase
		return evidence, true, fmt.Errorf("post-renew inspect Kubernetes certificates on %s: %s", evidence.ID, firstReceiptMessage(postInspect))
	}
	if expiry, count := parseKubernetesCertExpiry(postInspect.Stdout); !expiry.IsZero() {
		evidence.CertificateCount = count
		evidence.EarliestExpiry = expiry.UTC().Format(time.RFC3339)
		evidence.RenewalNeeded = time.Until(expiry) <= renewBefore
	}
	completed := kubernetesCertTargetCheckpoint(node, evidence, "succeeded", "post-renew", healthDigest, preInspectDigest, "")
	completed.PostInspectDigest = digestString(postInspect.Stdout)
	evidence.CheckpointReceipt = writeKubernetesCertTargetState(ctx, target, node.Kubernetes.Certificates, completed)
	if !nodeStepSucceeded(evidence.CheckpointReceipt.Status) {
		return evidence, true, fmt.Errorf("write Kubernetes certificate completion checkpoint on %s: %s", evidence.ID, firstReceiptMessage(evidence.CheckpointReceipt))
	}
	evidence.CheckpointStatus = completed.Status
	evidence.CheckpointPhase = completed.Phase
	return evidence, true, nil
}

func (e *customNodeExecutor) recordKubernetesCertArtifact(node *runNode, phase string, status string, message string, targets []kubernetesCertTargetEvidence) {
	payload := e.kubernetesCertPayload(node, phase, status, message, targets, false, nil)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) kubernetesCertPayload(node *runNode, phase string, status string, message string, targets []kubernetesCertTargetEvidence, changed bool, targetsFrom *kubernetesCertTargetsFromEvidence) map[string]any {
	payload := map[string]any{
		"apiVersion":  "torque.dev/stack-lifecycle/v1",
		"kind":        "KubernetesCertificateLifecycle",
		"nodeId":      node.ID,
		"nodeKind":    normalizeNodeKind(node.Kind),
		"phase":       phase,
		"status":      status,
		"message":     strings.TrimSpace(message),
		"changed":     changed,
		"provider":    strings.TrimSpace(node.Kubernetes.Provider),
		"renewBefore": durationString(node.Kubernetes.Certificates.RenewBefore),
		"force":       node.Kubernetes.Certificates.Force,
		"forceOnceId": strings.TrimSpace(node.Kubernetes.Certificates.ForceOnceID),
		"order":       normalizedKubernetesCertOrder(node.Kubernetes.Certificates.Order),
		"batchSize":   normalizedKubernetesCertBatchSize(node.Kubernetes.Certificates.BatchSize),
		"targetCount": len(targets),
		"targets":     targets,
	}
	if targetsFrom != nil {
		payload["targetsFrom"] = targetsFrom
	}
	return payload
}

func (e *customNodeExecutor) resolveKubernetesCertTargetsFrom(ctx context.Context, node *runNode, certs *KubernetesCertSpec) (*kubernetesCertTargetsFromEvidence, error) {
	if certs == nil {
		return nil, nil
	}
	spec := certs.TargetsFrom
	if strings.TrimSpace(spec.SourceNode) == "" && strings.TrimSpace(spec.SourceNodeID) == "" {
		return nil, nil
	}
	sourceNodeID := e.resolveKubernetesCertTargetsFromNodeID(spec)
	artifactName := strings.TrimSpace(spec.Artifact)
	if artifactName == "" {
		artifactName = "k8s-cluster-inspect.json"
	}
	evidence := &kubernetesCertTargetsFromEvidence{
		SourceNodeID:    sourceNodeID,
		Artifact:        artifactName,
		AddressType:     normalizedKubernetesCertTargetsFromAddressType(spec.AddressType),
		RoleFilter:      normalizedKubernetesCertTargetsFromRoles(spec.Roles),
		IncludeNotReady: spec.IncludeNotReady,
	}
	inspect, err := e.loadKubernetesClusterInspectEvidence(ctx, sourceNodeID, artifactName)
	if err != nil {
		return evidence, err
	}
	provider := strings.TrimSpace(spec.Provider)
	if provider == "" {
		provider = strings.TrimSpace(inspect.CertificateRenewal.Provider)
	}
	if provider == "" {
		provider = strings.TrimSpace(inspect.Provider.Distribution)
	}
	if provider == "" {
		provider = "custom"
	}
	evidence.Provider = provider
	if !inspect.CertificateRenewal.Supported && strings.TrimSpace(spec.Provider) == "" {
		return evidence, fmt.Errorf("targetsFrom %s reports unsupported certificate renewal provider %q: %s", sourceNodeID, inspect.CertificateRenewal.Provider, inspect.CertificateRenewal.Reason)
	}

	targets := make([]KubernetesCertTarget, 0, len(inspect.Nodes))
	roleFilter := kubernetesCertTargetsFromRoleFilter(spec.Roles)
	for _, inspectNode := range inspect.Nodes {
		targetID := strings.TrimSpace(inspectNode.Name)
		if targetID == "" || targetID == "<unknown>" {
			targetID = "node-" + fmt.Sprintf("%d", len(targets)+1)
		}
		role := kubernetesCertTargetsFromNodeRole(inspectNode.Roles)
		if !spec.IncludeNotReady && !inspectNode.Ready {
			evidence.Skipped = append(evidence.Skipped, kubernetesCertTargetsFromSkipEvidence{ID: targetID, Reason: "node not Ready"})
			continue
		}
		if len(roleFilter) > 0 && !kubernetesCertTargetsFromRoleAllowed(inspectNode.Roles, roleFilter) {
			evidence.Skipped = append(evidence.Skipped, kubernetesCertTargetsFromSkipEvidence{ID: targetID, Reason: "role filter"})
			continue
		}
		addressType, address := kubernetesCertTargetsFromAddress(inspectNode, spec.AddressType)
		data := kubernetesCertTargetTemplateData{
			Name:        targetID,
			Role:        role,
			Provider:    provider,
			Address:     address,
			AddressType: addressType,
			InternalIP:  kubernetesCertTargetsFromAddressByType(inspectNode, "InternalIP"),
			ExternalIP:  kubernetesCertTargetsFromAddressByType(inspectNode, "ExternalIP"),
			Hostname:    kubernetesCertTargetsFromAddressByType(inspectNode, "Hostname"),
		}
		targetValue := strings.TrimSpace(spec.Target)
		if strings.TrimSpace(spec.TargetTemplate) != "" {
			targetValue, err = renderKubernetesCertTargetTemplate("targetTemplate", spec.TargetTemplate, data)
			if err != nil {
				return evidence, fmt.Errorf("render targetsFrom targetTemplate for %s: %w", targetID, err)
			}
		}
		nodeAddress := ""
		if strings.TrimSpace(spec.NodeAddressTemplate) != "" {
			nodeAddress, err = renderKubernetesCertTargetTemplate("nodeAddressTemplate", spec.NodeAddressTemplate, data)
			if err != nil {
				return evidence, fmt.Errorf("render targetsFrom nodeAddressTemplate for %s: %w", targetID, err)
			}
		}
		target := KubernetesCertTarget{
			ID:               targetID,
			Role:             role,
			Provider:         provider,
			Transport:        firstNonEmptyString(spec.Transport, "ssh"),
			Target:           targetValue,
			TargetEnv:        strings.TrimSpace(spec.TargetEnv),
			Timeout:          spec.Timeout,
			Service:          strings.TrimSpace(spec.Service),
			NodeAddress:      nodeAddress,
			NodeIdentityFile: strings.TrimSpace(spec.NodeIdentityFile),
			NodeSSHOptions:   strings.TrimSpace(spec.NodeSSHOptions),
			InspectCommand:   strings.TrimSpace(spec.InspectCommand),
			RenewCommand:     strings.TrimSpace(spec.RenewCommand),
			RestartCommand:   strings.TrimSpace(spec.RestartCommand),
		}
		targets = append(targets, target)
		evidence.Targets = append(evidence.Targets, kubernetesCertTargetsFromTargetEvidence{
			ID:          target.ID,
			Role:        target.Role,
			Provider:    target.Provider,
			AddressType: addressType,
			Address:     address,
			Transport:   target.Transport,
			TargetEnv:   target.TargetEnv,
			NodeAddress: target.NodeAddress,
		})
	}
	if len(targets) == 0 {
		return evidence, fmt.Errorf("targetsFrom %s produced no Kubernetes certificate targets", sourceNodeID)
	}
	evidence.DerivedCount = len(targets)
	if len(certs.Targets) > 0 {
		certs.Targets = append(append([]KubernetesCertTarget(nil), certs.Targets...), targets...)
		return evidence, nil
	}
	certs.Targets = targets
	return evidence, nil
}

type kubernetesCertTargetTemplateData struct {
	Name        string
	Role        string
	Provider    string
	Address     string
	AddressType string
	InternalIP  string
	ExternalIP  string
	Hostname    string
}

func (e *customNodeExecutor) resolveKubernetesCertTargetsFromNodeID(spec KubernetesCertTargetsFromSpec) string {
	if strings.TrimSpace(spec.SourceNodeID) != "" {
		return strings.TrimSpace(spec.SourceNodeID)
	}
	ref := strings.TrimSpace(spec.SourceNode)
	if ref == "" || e == nil || e.run == nil || e.run.Plan == nil {
		return ref
	}
	for _, n := range e.run.Plan.Nodes {
		if n == nil {
			continue
		}
		if strings.TrimSpace(n.ID) == ref {
			return n.ID
		}
	}
	for _, n := range e.run.Plan.Nodes {
		if n == nil || normalizeNodeKind(n.Kind) != NodeKindK8sClusterInspect {
			continue
		}
		if strings.TrimSpace(n.Name) == ref {
			return n.ID
		}
	}
	return ref
}

func (e *customNodeExecutor) loadKubernetesClusterInspectEvidence(ctx context.Context, nodeID string, artifactName string) (kubernetesClusterInspectEvidence, error) {
	evidence, _, err := e.loadKubernetesClusterInspectArtifact(ctx, nodeID, artifactName)
	return evidence, err
}

func renderKubernetesCertTargetTemplate(name string, raw string, data kubernetesCertTargetTemplateData) (string, error) {
	tmpl, err := template.New(name).Option("missingkey=error").Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", err
	}
	return strings.TrimSpace(buf.String()), nil
}

func normalizedKubernetesCertTargetsFromRoles(roles []string) []string {
	out := make([]string, 0, len(roles))
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role != "" {
			out = append(out, role)
		}
	}
	sort.Strings(out)
	return out
}

func kubernetesCertTargetsFromRoleFilter(roles []string) map[string]struct{} {
	out := map[string]struct{}{}
	for _, role := range normalizedKubernetesCertTargetsFromRoles(roles) {
		out[role] = struct{}{}
	}
	return out
}

func kubernetesCertTargetsFromRoleAllowed(nodeRoles []string, roleFilter map[string]struct{}) bool {
	for _, role := range nodeRoles {
		if _, ok := roleFilter[strings.ToLower(strings.TrimSpace(role))]; ok {
			return true
		}
	}
	return false
}

func kubernetesCertTargetsFromNodeRole(roles []string) string {
	for _, role := range roles {
		role = strings.ToLower(strings.TrimSpace(role))
		if role == "control-plane" || role == "master" {
			return "control-plane"
		}
	}
	for _, role := range roles {
		if strings.TrimSpace(role) != "" {
			return strings.TrimSpace(role)
		}
	}
	return "worker"
}

func normalizedKubernetesCertTargetsFromAddressType(addressType string) string {
	addressType = strings.TrimSpace(addressType)
	if addressType == "" {
		return "InternalIP"
	}
	return addressType
}

func kubernetesCertTargetsFromAddress(node kubernetesClusterInspectNode, addressType string) (string, string) {
	want := normalizedKubernetesCertTargetsFromAddressType(addressType)
	if address := kubernetesCertTargetsFromAddressByType(node, want); address != "" {
		return want, address
	}
	for _, address := range node.Addresses {
		if strings.TrimSpace(address.Type) != "" && strings.TrimSpace(address.Address) != "" {
			return strings.TrimSpace(address.Type), strings.TrimSpace(address.Address)
		}
	}
	return want, ""
}

func kubernetesCertTargetsFromAddressByType(node kubernetesClusterInspectNode, addressType string) string {
	for _, address := range node.Addresses {
		if strings.EqualFold(strings.TrimSpace(address.Type), strings.TrimSpace(addressType)) && strings.TrimSpace(address.Address) != "" {
			return strings.TrimSpace(address.Address)
		}
	}
	return ""
}

func kubernetesCertTargetRunners(targets []KubernetesCertTarget) ([]kubernetesCertTargetRunner, error) {
	out := make([]kubernetesCertTargetRunner, 0, len(targets))
	for _, target := range targets {
		spec := target
		if strings.TrimSpace(spec.Transport) == "" {
			spec.Transport = "ssh"
		}
		timeout := 5 * time.Minute
		if spec.Timeout != nil && *spec.Timeout > 0 {
			timeout = *spec.Timeout
		}
		runner, err := hostCommandTransport(HostCommandSpec{
			Transport: spec.Transport,
			Target:    spec.Target,
			TargetEnv: spec.TargetEnv,
			Timeout:   &timeout,
		})
		if err != nil {
			return nil, err
		}
		out = append(out, kubernetesCertTargetRunner{spec: spec, runner: runner})
	}
	return out, nil
}

func kubernetesCertTargetBatches(runners []kubernetesCertTargetRunner, certs KubernetesCertSpec) []kubernetesCertBatch {
	ordered := append([]kubernetesCertTargetRunner(nil), runners...)
	switch normalizedKubernetesCertOrder(certs.Order) {
	case "control-plane-first":
		sort.SliceStable(ordered, func(i, j int) bool {
			return kubernetesCertRoleRank(ordered[i].spec.Role, true) < kubernetesCertRoleRank(ordered[j].spec.Role, true)
		})
	case "control-plane-last":
		sort.SliceStable(ordered, func(i, j int) bool {
			return kubernetesCertRoleRank(ordered[i].spec.Role, false) < kubernetesCertRoleRank(ordered[j].spec.Role, false)
		})
	}
	batchSize := normalizedKubernetesCertBatchSize(certs.BatchSize)
	out := make([]kubernetesCertBatch, 0, (len(ordered)+batchSize-1)/batchSize)
	for i := 0; i < len(ordered); i += batchSize {
		end := i + batchSize
		if end > len(ordered) {
			end = len(ordered)
		}
		out = append(out, kubernetesCertBatch{Index: len(out), Targets: ordered[i:end]})
	}
	return out
}

func normalizedKubernetesCertOrder(order string) string {
	order = strings.ToLower(strings.TrimSpace(order))
	if order == "" {
		return "as-listed"
	}
	return order
}

func normalizedKubernetesCertBatchSize(batchSize int) int {
	if batchSize <= 0 {
		return 1
	}
	return batchSize
}

func kubernetesCertRoleRank(role string, controlPlaneFirst bool) int {
	role = strings.ToLower(strings.TrimSpace(role))
	isControlPlane := strings.Contains(role, "control-plane") || strings.Contains(role, "master")
	if controlPlaneFirst {
		if isControlPlane {
			return 0
		}
		return 1
	}
	if isControlPlane {
		return 1
	}
	return 0
}

func detectKubernetesCertProvider(ctx context.Context, target kubernetesCertTargetRunner, defaultProvider string) (string, transport.OperationResult) {
	provider := strings.ToLower(strings.TrimSpace(target.spec.Provider))
	if provider == "" {
		provider = strings.ToLower(strings.TrimSpace(defaultProvider))
	}
	if provider != "" && provider != "auto" {
		return provider, transport.OperationResult{Operation: "provider.detect", Status: "skipped", TargetDigest: target.runner.TargetDigest(), Stdout: provider}
	}
	receipt := target.runNode(ctx, `if command -v kubeadm >/dev/null 2>&1 && [ -d /etc/kubernetes/pki ]; then echo kubeadm; elif command -v k3s >/dev/null 2>&1; then echo k3s; elif command -v rke2 >/dev/null 2>&1; then echo rke2; else echo custom; fi`)
	detected := strings.TrimSpace(receipt.Stdout)
	if detected == "" || detected == "custom" {
		detected = "custom"
	}
	return detected, receipt
}

func (t kubernetesCertTargetRunner) runNode(ctx context.Context, command string) transport.OperationResult {
	command = strings.TrimSpace(command)
	if strings.TrimSpace(t.spec.NodeAddress) == "" {
		return t.runner.Run(ctx, command)
	}
	return t.runner.Run(ctx, nestedSSHCommand(t.spec, command))
}

func (t kubernetesCertTargetRunner) runAccess(ctx context.Context, command string) transport.OperationResult {
	return t.runner.Run(ctx, strings.TrimSpace(command))
}

func nestedSSHCommand(spec KubernetesCertTarget, command string) string {
	args := []string{"ssh", "-o", "BatchMode=yes", "-o", "StrictHostKeyChecking=no", "-o", "UserKnownHostsFile=/dev/null", "-o", "ConnectTimeout=5"}
	if strings.TrimSpace(spec.NodeIdentityFile) != "" {
		args = append(args, "-i", strings.TrimSpace(spec.NodeIdentityFile))
	}
	if strings.TrimSpace(spec.NodeSSHOptions) != "" {
		args = append(args, strings.Fields(spec.NodeSSHOptions)...)
	}
	args = append(args, strings.TrimSpace(spec.NodeAddress), command)
	return shellJoin(args)
}

func kubernetesCertInspectCommand(provider string, spec KubernetesCertTarget) string {
	if strings.TrimSpace(spec.InspectCommand) != "" {
		return spec.InspectCommand
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "kubeadm":
		return `kubeadm certs check-expiration -o json 2>/dev/null || kubeadm certs check-expiration`
	case "rke2":
		return `rke2 certificate check --output json 2>/dev/null || rke2 certificate check`
	case "k3s":
		return `k3s certificate check --output json 2>/dev/null || k3s certificate check`
	default:
		return `printf '%s\n' 'unsupported Kubernetes certificate provider; configure inspectCommand for custom provider' >&2; exit 2`
	}
}

func kubernetesCertRenewCommand(provider string, spec KubernetesCertTarget, certs KubernetesCertSpec) string {
	if strings.TrimSpace(spec.RenewCommand) != "" {
		commands := []string{strings.TrimSpace(spec.RenewCommand)}
		if strings.TrimSpace(spec.RestartCommand) != "" {
			commands = append(commands, strings.TrimSpace(spec.RestartCommand))
		}
		return strings.Join(commands, "\n")
	}
	services := normalizedKubernetesCertServices(certs.Services)
	serviceFlags := ""
	if len(services) > 0 {
		serviceFlags = " --service " + transport.ShellQuote(strings.Join(services, ","))
	}
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case "kubeadm":
		kubeadmRenew := `kubeadm certs renew all`
		if len(services) > 0 {
			var quoted []string
			for _, service := range services {
				if strings.TrimSpace(service) != "" {
					quoted = append(quoted, transport.ShellQuote(strings.TrimSpace(service)))
				}
			}
			if len(quoted) > 0 {
				kubeadmRenew = `for cert in ` + strings.Join(quoted, " ") + `; do kubeadm certs renew "${cert}"; done`
			}
		}
		restart := strings.TrimSpace(spec.RestartCommand)
		if restart == "" {
			restart = `systemctl restart kubelet
systemctl is-active kubelet`
		}
		return `set -euo pipefail
backup="/etc/kubernetes/pki.torque.$(date -u +%Y%m%dT%H%M%SZ)"
if [ -d /etc/kubernetes/pki ]; then cp -a /etc/kubernetes/pki "${backup}"; fi
` + kubeadmRenew + `
` + restart
	case "rke2":
		if strings.TrimSpace(spec.RestartCommand) != "" {
			return `set -euo pipefail
rke2 certificate rotate` + serviceFlags + `
` + strings.TrimSpace(spec.RestartCommand)
		}
		service := strings.TrimSpace(spec.Service)
		if service == "" {
			service = "$(systemctl list-unit-files 'rke2-*.service' --no-legend 2>/dev/null | awk '{print $1}' | sed 's/\\.service$//' | head -1)"
		}
		return `set -euo pipefail
svc=` + shellAssignValue(service) + `
if [ -z "${svc}" ]; then svc="rke2-server"; fi
systemctl stop "${svc}"
rke2 certificate rotate` + serviceFlags + `
systemctl start "${svc}"
systemctl is-active "${svc}"`
	case "k3s":
		if strings.TrimSpace(spec.RestartCommand) != "" {
			return `set -euo pipefail
k3s certificate rotate` + serviceFlags + `
` + strings.TrimSpace(spec.RestartCommand)
		}
		service := strings.TrimSpace(spec.Service)
		if service == "" {
			service = "$(systemctl list-units --type=service --all 'k3s*.service' --no-legend 2>/dev/null | awk '{print $1}' | sed 's/\\.service$//' | head -1)"
		}
		return `set -euo pipefail
svc=` + shellAssignValue(service) + `
if [ -z "${svc}" ]; then svc="k3s"; fi
systemctl stop "${svc}"
k3s certificate rotate` + serviceFlags + `
systemctl start "${svc}"
systemctl is-active "${svc}"`
	default:
		return `printf '%s\n' 'unsupported Kubernetes certificate provider; configure renewCommand for custom provider' >&2; exit 2`
	}
}

func readKubernetesCertTargetState(ctx context.Context, target kubernetesCertTargetRunner, certs KubernetesCertSpec, targetID string) (kubernetesCertTargetState, transport.OperationResult, bool, error) {
	statePath := kubernetesCertTargetStatePath(certs, targetID)
	if statePath == "" {
		return kubernetesCertTargetState{}, transport.OperationResult{Operation: "state.check", Status: "skipped", TargetDigest: target.runner.TargetDigest()}, false, nil
	}
	receipt := target.runAccess(ctx, `test -s `+transport.ShellQuote(statePath)+` && cat `+transport.ShellQuote(statePath)+` || true`)
	if !nodeStepSucceeded(receipt.Status) {
		return kubernetesCertTargetState{}, receipt, false, nil
	}
	if strings.TrimSpace(receipt.Stdout) == "" {
		return kubernetesCertTargetState{}, receipt, false, nil
	}
	var state kubernetesCertTargetState
	if err := json.Unmarshal([]byte(receipt.Stdout), &state); err != nil {
		return kubernetesCertTargetState{}, receipt, false, err
	}
	return state, receipt, true, nil
}

func writeKubernetesCertTargetState(ctx context.Context, target kubernetesCertTargetRunner, certs KubernetesCertSpec, state kubernetesCertTargetState) transport.OperationResult {
	if strings.TrimSpace(certs.StatePath) == "" {
		return transport.OperationResult{Operation: "state.write", Status: "skipped", TargetDigest: target.runner.TargetDigest()}
	}
	return target.runAccess(ctx, writeKubernetesCertStateCommand(certs, state))
}

func writeKubernetesCertStateCommand(certs KubernetesCertSpec, state kubernetesCertTargetState) string {
	statePath := kubernetesCertTargetStatePath(certs, state.TargetID)
	raw, _ := json.Marshal(state)
	return `mkdir -p "$(dirname ` + transport.ShellQuote(statePath) + `)" && printf '%s\n' ` + transport.ShellQuote(string(raw)) + ` > ` + transport.ShellQuote(statePath)
}

func kubernetesCertStateCompletedForIntent(state kubernetesCertTargetState, intentDigest string, certs KubernetesCertSpec, targetID string) bool {
	if strings.TrimSpace(state.Status) != "succeeded" || strings.TrimSpace(state.TargetID) != strings.TrimSpace(targetID) {
		return false
	}
	forceOnceID := strings.TrimSpace(certs.ForceOnceID)
	if forceOnceID != "" && strings.TrimSpace(state.ForceOnceID) != "" && strings.TrimSpace(state.ForceOnceID) != forceOnceID {
		return false
	}
	// Legacy checkpoints did not include an intent digest; keep them valid so
	// existing forced maintenance does not run twice after upgrading Torque.
	return strings.TrimSpace(state.IntentDigest) == "" || strings.TrimSpace(state.IntentDigest) == strings.TrimSpace(intentDigest)
}

func kubernetesCertStateInProgressForIntent(state kubernetesCertTargetState, intentDigest string) bool {
	status := strings.TrimSpace(state.Status)
	if status == "" || status == "succeeded" {
		return false
	}
	return strings.TrimSpace(state.IntentDigest) == strings.TrimSpace(intentDigest)
}

func kubernetesCertTargetCheckpoint(node *runNode, evidence kubernetesCertTargetEvidence, status string, phase string, healthDigest string, preInspectDigest string, errorMessage string) kubernetesCertTargetState {
	now := time.Now().UTC().Format(time.RFC3339)
	state := kubernetesCertTargetState{
		APIVersion:       "torque.dev/stack-lifecycle/v1",
		Kind:             "KubernetesCertificateRenewalState",
		Status:           strings.TrimSpace(status),
		Phase:            strings.TrimSpace(phase),
		ForceOnceID:      strings.TrimSpace(node.Kubernetes.Certificates.ForceOnceID),
		IntentDigest:     strings.TrimSpace(evidence.IntentDigest),
		TargetID:         strings.TrimSpace(evidence.ID),
		Provider:         strings.TrimSpace(evidence.Provider),
		Role:             strings.TrimSpace(evidence.Role),
		Batch:            evidence.Batch,
		HealthDigest:     strings.TrimSpace(healthDigest),
		PreInspectDigest: strings.TrimSpace(preInspectDigest),
		Error:            strings.TrimSpace(errorMessage),
		UpdatedAt:        now,
	}
	if status == "running" {
		state.StartedAt = now
	}
	if status == "succeeded" {
		state.CompletedAt = now
	}
	return state
}

func kubernetesCertTargetIntentDigest(node *runNode, target KubernetesCertTarget) string {
	intent := strings.TrimSpace(node.EffectiveInputHash)
	if intent == "" {
		intent = digestString(node.ID)
	}
	sum, err := hashJSONStable(struct {
		NodeIntent string `json:"nodeIntent"`
		TargetID   string `json:"targetId"`
	}{
		NodeIntent: intent,
		TargetID:   strings.TrimSpace(target.ID),
	})
	if err != nil {
		return intent
	}
	return sum
}

func kubernetesCertHealthDigest(ctx context.Context, target kubernetesCertTargetRunner, certs KubernetesCertSpec) (string, transport.OperationResult, error) {
	command := strings.TrimSpace(certs.HealthCheckCommand)
	if command == "" {
		return "", transport.OperationResult{Operation: "health.check", Status: "skipped", TargetDigest: target.runner.TargetDigest()}, nil
	}
	var receipt transport.OperationResult
	for attempt := 1; attempt <= defaultKubernetesHealthCheckAttempts; attempt++ {
		receipt = target.runAccess(ctx, command)
		if nodeStepSucceeded(receipt.Status) {
			break
		}
		if attempt == defaultKubernetesHealthCheckAttempts {
			return "", receipt, fmt.Errorf("%s", firstReceiptMessage(receipt))
		}
		select {
		case <-ctx.Done():
			return "", receipt, ctx.Err()
		case <-time.After(defaultKubernetesHealthCheckSleep):
		}
	}
	digest, err := hashJSONStable(struct {
		Stdout string `json:"stdout,omitempty"`
		Stderr string `json:"stderr,omitempty"`
	}{
		Stdout: strings.TrimSpace(receipt.Stdout),
		Stderr: strings.TrimSpace(receipt.Stderr),
	})
	if err != nil {
		return "", receipt, err
	}
	return digest, receipt, nil
}

func kubernetesCertTargetStatePath(certs KubernetesCertSpec, targetID string) string {
	statePath := strings.TrimSpace(certs.StatePath)
	if statePath == "" {
		return ""
	}
	safeTargetID := kubernetesCertSafeStateID(targetID)
	if safeTargetID == "" {
		safeTargetID = "target"
	}
	if strings.HasSuffix(statePath, "/") {
		return statePath + safeTargetID + ".json"
	}
	return statePath + "." + safeTargetID + ".json"
}

func kubernetesCertSafeStateID(targetID string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(targetID) {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

func parseKubernetesCertExpiry(raw string) (time.Time, int) {
	raw = strings.TrimSpace(raw)
	var expiries []time.Time
	var decoded any
	if err := json.Unmarshal([]byte(raw), &decoded); err == nil {
		collectExpiryTimes(decoded, &expiries)
	}
	if len(expiries) == 0 {
		expiries = append(expiries, parseExpiryTimesFromText(raw)...)
	}
	if len(expiries) == 0 {
		return time.Time{}, 0
	}
	sort.Slice(expiries, func(i, j int) bool { return expiries[i].Before(expiries[j]) })
	return expiries[0], len(expiries)
}

func collectExpiryTimes(value any, out *[]time.Time) {
	switch typed := value.(type) {
	case map[string]any:
		for key, item := range typed {
			lower := strings.ToLower(key)
			if strings.Contains(lower, "expiry") || strings.Contains(lower, "expiration") || strings.Contains(lower, "notafter") {
				if raw, ok := item.(string); ok {
					if parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(raw)); err == nil {
						*out = append(*out, parsed)
					}
				}
			}
			collectExpiryTimes(item, out)
		}
	case []any:
		for _, item := range typed {
			collectExpiryTimes(item, out)
		}
	}
}

func normalizedKubernetesCertServices(services []string) []string {
	out := make([]string, 0, len(services))
	for _, service := range services {
		if strings.TrimSpace(service) != "" {
			out = append(out, strings.TrimSpace(service))
		}
	}
	sort.Strings(out)
	return out
}

var (
	rfc3339LikePattern    = regexp.MustCompile(`20[0-9]{2}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})`)
	humanUTCExpiryPattern = regexp.MustCompile(`[A-Z][a-z]{2}\s+[0-9]{1,2},\s+20[0-9]{2}\s+[0-9]{2}:[0-9]{2}\s+UTC`)
)

func parseExpiryTimesFromText(raw string) []time.Time {
	var out []time.Time
	for _, match := range rfc3339LikePattern.FindAllString(raw, -1) {
		if parsed, err := time.Parse(time.RFC3339, match); err == nil {
			out = append(out, parsed)
		}
	}
	for _, match := range humanUTCExpiryPattern.FindAllString(raw, -1) {
		normalized := strings.Join(strings.Fields(match), " ")
		if parsed, err := time.Parse("Jan 2, 2006 15:04 MST", normalized); err == nil {
			out = append(out, parsed)
		}
	}
	return out
}

func firstReceiptMessage(receipt transport.OperationResult) string {
	for _, value := range []string{receipt.Stderr, receipt.Error, receipt.Stdout} {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return receipt.Status
}

func kubernetesCertErrorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "checkpoint blocked"):
		return "K8S_CERT_CHECKPOINT_BLOCKED"
	case strings.Contains(msg, "checkpoint"):
		return "K8S_CERT_CHECKPOINT_FAILED"
	case strings.Contains(msg, "health"):
		return "K8S_CERT_HEALTH_FAILED"
	default:
		return "K8S_CERT_LIFECYCLE_FAILED"
	}
}

func durationString(value *time.Duration) string {
	if value == nil {
		return defaultKubernetesCertRenewBefore.String()
	}
	return value.String()
}

func shellJoin(args []string) string {
	quoted := make([]string, 0, len(args))
	for _, arg := range args {
		quoted = append(quoted, transport.ShellQuote(arg))
	}
	return strings.Join(quoted, " ")
}

func shellAssignValue(value string) string {
	value = strings.TrimSpace(value)
	if strings.HasPrefix(value, "$(") && strings.HasSuffix(value, ")") {
		return value
	}
	return transport.ShellQuote(value)
}
