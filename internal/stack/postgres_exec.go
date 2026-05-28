package stack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	opspostgres "github.com/ingresslabs/torque/internal/ops/postgres"
	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const defaultPostgresAgentPath = "/usr/local/bin/torque-agent"

type postgresObserveReceipt struct {
	APIVersion       string   `json:"apiVersion"`
	Kind             string   `json:"kind"`
	NodeID           string   `json:"nodeId"`
	NodeKind         string   `json:"nodeKind"`
	TargetID         string   `json:"targetId,omitempty"`
	Phase            string   `json:"phase"`
	Status           string   `json:"status"`
	GuardMode        string   `json:"guardMode"`
	SelectedTargetID string   `json:"selectedTargetId,omitempty"`
	SelectedTargets  []string `json:"selectedTargets,omitempty"`
	Database         string   `json:"database,omitempty"`
	TargetDigest     string   `json:"targetDigest,omitempty"`
	ObservedAt       string   `json:"observedAt"`
}

type postgresPlanReceipt struct {
	APIVersion       string   `json:"apiVersion"`
	Kind             string   `json:"kind"`
	NodeID           string   `json:"nodeId"`
	NodeKind         string   `json:"nodeKind"`
	TargetID         string   `json:"targetId,omitempty"`
	Phase            string   `json:"phase"`
	Status           string   `json:"status"`
	Reason           string   `json:"reason,omitempty"`
	GuardMode        string   `json:"guardMode"`
	AdapterKind      string   `json:"adapterKind,omitempty"`
	Transport        string   `json:"transport,omitempty"`
	Operation        string   `json:"operation"`
	ResourceDigest   string   `json:"resourceDigest,omitempty"`
	PlannedSQLDigest string   `json:"plannedSqlDigest,omitempty"`
	CommandDigest    string   `json:"commandDigest,omitempty"`
	Database         string   `json:"database,omitempty"`
	SelectedTargets  []string `json:"selectedTargets,omitempty"`
	LockScopes       []string `json:"lockScopes,omitempty"`
	PolicySources    []string `json:"policySources,omitempty"`
	PlannedAt        string   `json:"plannedAt"`
}

type postgresVerifyReceipt struct {
	APIVersion   string `json:"apiVersion"`
	Kind         string `json:"kind"`
	NodeID       string `json:"nodeId"`
	NodeKind     string `json:"nodeKind"`
	TargetID     string `json:"targetId,omitempty"`
	Phase        string `json:"phase"`
	Status       string `json:"status"`
	Reason       string `json:"reason,omitempty"`
	Operation    string `json:"operation"`
	ExitCode     int    `json:"exitCode,omitempty"`
	StdoutDigest string `json:"stdoutDigest,omitempty"`
	StderrDigest string `json:"stderrDigest,omitempty"`
	VerifiedAt   string `json:"verifiedAt"`
}

func (e *customNodeExecutor) runPostgresNode(ctx context.Context, node *runNode, command string) error {
	kind := normalizeNodeKind(node.Kind)
	phase := strings.ReplaceAll(kind, ".", "-")
	if strings.EqualFold(command, "delete") {
		payload := e.postgresNodePayload(node, phase, "skipped", "delete is not implemented for PostgreSQL admin resources", nil, nil, nil, nil)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}

	cursor := map[string]any{"kind": kind, "phase": phase, "transport": strings.TrimSpace(node.Postgres.Transport)}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	runID := e.postgresCommandRunID()
	resourcePayload, err := buildPostgresResourcePayload(kind, node.Postgres, node.ID, runID, e.postgresResourceTenant())
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	remoteCommand := ""
	if !transportIsNATS(node.Postgres.Transport) {
		if postgresExecutionMode(node.Postgres) == "native" && postgresTransportIsLocal(node.Postgres.Transport) {
			remoteCommand = "postgres.resource " + kind
		} else if postgresExecutionMode(node.Postgres) == "native" {
			remoteCommand, err = buildPostgresNativeCommand(node.Postgres, resourcePayload)
		} else {
			remoteCommand, err = buildPostgresCommand(kind, node.Postgres, node.ID, runID)
		}
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
	}
	observe := e.postgresObserveReceipt(node, phase, "")
	hostSpec := e.postgresHostCommandSpec(node, remoteCommand, kind, resourcePayload)
	plan := e.postgresPlanReceipt(node, phase, resourcePayload, hostSpec.Command, "planned", "eligible")

	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		plan.Status = "skipped"
		plan.Reason = reason
		verify := e.postgresVerifyReceipt(node, phase, plan.TargetID, transport.OperationResult{Operation: kind, Status: "skipped"})
		e.recordPostgresReceipts(node, phase, "skipped", reason, observe, plan, nil, nil, verify)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	if e.shouldUseFleetNATSFanout(hostSpec) {
		fanout, receipt := e.runHostCommandFleetNATSFanout(ctx, node, phase, hostSpec, hostSpec.Command)
		observe.TargetDigest = receipt.TargetDigest
		e.enrichPostgresPlanFromFanout(&plan, fanout)
		verify := e.postgresVerifyReceipt(node, phase, plan.TargetID, receipt)
		e.recordPostgresReceipts(node, phase, fanout.Status, strings.TrimSpace(fanout.Reason), observe, plan, &receipt, &fanout, verify)
		if postgresReceiptBlocked(receipt) || strings.EqualFold(strings.TrimSpace(fanout.Status), "blocked") {
			msg := firstNonEmptyString(postgresReceiptMessage(receipt), strings.TrimSpace(fanout.Reason), fmt.Sprintf("%s blocked", kind))
			runErr := &RunError{Class: "POSTGRES_RESOURCE_BLOCKED", Message: msg, Digest: computeRunErrorDigest("POSTGRES_RESOURCE_BLOCKED", msg)}
			e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
				"phase":   phase,
				"status":  "blocked",
				"cursor":  cursor,
				"receipt": receipt,
				"fanout":  fanout.Summary,
			}, runErr, true)
			return wrapNodeErr(node.ResolvedRelease, newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", fmt.Sprintf("%s phase %s: %s", kind, phase, msg), nil))
		}
		if !nodeStepSucceeded(receipt.Status) {
			msg := firstNonEmptyString(receipt.Error, receipt.Stderr, fmt.Sprintf("%s status %s", kind, receipt.Status))
			runErr := &RunError{Class: "POSTGRES_RESOURCE_FAILED", Message: msg, Digest: computeRunErrorDigest("POSTGRES_RESOURCE_FAILED", msg)}
			e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
				"phase":   phase,
				"status":  "failure",
				"cursor":  cursor,
				"receipt": receipt,
				"fanout":  fanout.Summary,
			}, runErr, true)
			return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s phase %s: %s", kind, phase, msg))
		}
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, firstNonEmptyString(fanout.Reason, "success"), map[string]any{
			"phase":   phase,
			"status":  "success",
			"cursor":  cursor,
			"receipt": receipt,
			"fanout":  fanout.Summary,
		}, nil)
		return nil
	}

	runner, err := hostCommandTransport(hostSpec)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	targetDigest := runner.TargetDigest()
	observe.TargetDigest = targetDigest
	if guardErr := e.validatePostgresOpsGuard(node, plan.TargetID, kind); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.postgresVerifyReceipt(node, phase, plan.TargetID, transport.OperationResult{Operation: kind, Status: "blocked", Error: guardErr.Error()})
		e.recordPostgresReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, nil, nil, verify)
		runErr := &RunError{Class: "POSTGRES_RESOURCE_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("POSTGRES_RESOURCE_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": plan.TargetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", fmt.Sprintf("%s phase %s: %s", kind, phase, guardErr.Error()), guardErr))
	}
	var receipt transport.OperationResult
	if len(resourcePayload) > 0 && postgresExecutionMode(node.Postgres) == "native" && postgresTransportIsLocal(node.Postgres.Transport) {
		receipt = runLocalPostgresResource(ctx, time.Now(), resourcePayload, targetDigest)
	} else if len(resourcePayload) > 0 && transportIsNATS(hostSpec.Transport) {
		if resourceRunner, ok := runner.(interface {
			RunResource(context.Context, json.RawMessage) transport.OperationResult
		}); ok {
			receipt = resourceRunner.RunResource(ctx, resourcePayload)
		} else {
			receipt = transport.OperationResult{
				Operation:    "resource",
				Status:       "failed",
				TargetDigest: targetDigest,
				ExitCode:     1,
				Error:        "NATS transport does not support typed resource execution",
			}
		}
	} else {
		receipt = runner.Run(ctx, hostSpec.Command)
	}
	enrichPostgresReceiptFromStdout(&receipt)
	e.enrichPostgresPlanFromReceipt(&plan, receipt)
	verify := e.postgresVerifyReceipt(node, phase, plan.TargetID, receipt)
	reason := strings.TrimSpace(receipt.Error)
	if postgresReceiptBlocked(receipt) {
		reason = postgresReceiptMessage(receipt)
	}
	e.recordPostgresReceipts(node, phase, receipt.Status, reason, observe, plan, &receipt, nil, verify)
	if postgresReceiptBlocked(receipt) {
		msg := firstNonEmptyString(postgresReceiptMessage(receipt), fmt.Sprintf("%s blocked", kind))
		runErr := &RunError{Class: "POSTGRES_RESOURCE_BLOCKED", Message: msg, Digest: computeRunErrorDigest("POSTGRES_RESOURCE_BLOCKED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "blocked",
			"cursor":  cursor,
			"receipt": receipt,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", fmt.Sprintf("%s phase %s: %s", kind, phase, msg), nil))
	}
	if !nodeStepSucceeded(receipt.Status) {
		msg := firstReceiptMessage(receipt)
		runErr := &RunError{Class: "POSTGRES_RESOURCE_FAILED", Message: msg, Digest: computeRunErrorDigest("POSTGRES_RESOURCE_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "failure",
			"cursor":  cursor,
			"receipt": receipt,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s phase %s: %s", kind, phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"cursor":  cursor,
		"receipt": receipt,
	}, nil)
	return nil
}

func (e *customNodeExecutor) postgresHostCommandSpec(node *runNode, command string, capability string, resource json.RawMessage) HostCommandSpec {
	spec := node.Postgres
	if transportIsNATS(spec.Transport) {
		command = ""
	}
	hostSpec := HostCommandSpec{
		Transport: spec.Transport,
		TargetID:  spec.TargetID,
		Target:    spec.Target,
		TargetEnv: spec.TargetEnv,
		Command:   command,
		Resource:  cloneJSONRawMessage(resource),
		Timeout:   spec.Timeout,
	}
	return e.hostCommandAssignmentSpec(hostSpec, node, capability)
}

func (e *customNodeExecutor) postgresCommandRunID() string {
	if e != nil && e.run != nil {
		if resumeFromRunID := strings.TrimSpace(e.run.ResumeFromRunID); resumeFromRunID != "" {
			return resumeFromRunID
		}
		return strings.TrimSpace(e.run.RunID)
	}
	return ""
}

func (e *customNodeExecutor) postgresResourceTenant() string {
	if e != nil && e.run != nil && e.run.Plan != nil {
		return normalizeFleetReadiness(e.run.Plan.Runner.Readiness).Tenant
	}
	return "default"
}

func (e *customNodeExecutor) validatePostgresOpsGuard(node *runNode, targetID string, operation string) error {
	if e == nil || e.run == nil || e.run.Plan == nil || e.run.Plan.Ops == nil {
		return nil
	}
	return e.validateHostAdapterOpsGuard(node, targetID, operation)
}

func buildPostgresResourcePayload(kind string, spec PostgresSpec, nodeID string, runID string, tenant string) (json.RawMessage, error) {
	rawSpec, err := json.Marshal(spec)
	if err != nil {
		return nil, fmt.Errorf("marshal PostgreSQL resource spec: %w", err)
	}
	var pgSpec opspostgres.Spec
	if err := json.Unmarshal(rawSpec, &pgSpec); err != nil {
		return nil, fmt.Errorf("parse PostgreSQL resource spec for lock policy: %w", err)
	}
	nodeKind := normalizeNodeKind(kind)
	tenant = firstNonEmptyString(tenant, "default")
	req := opspostgres.ResourceRequest{
		APIVersion: opspostgres.RequestAPIVersion,
		Kind:       opspostgres.RequestKind,
		Tenant:     tenant,
		NodeID:     strings.TrimSpace(nodeID),
		RunID:      strings.TrimSpace(runID),
		NodeKind:   nodeKind,
		Lock:       opspostgres.DefaultLockPolicy(tenant, nodeKind, pgSpec),
		Spec:       rawSpec,
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal PostgreSQL resource request: %w", err)
	}
	return json.RawMessage(raw), nil
}

func (e *customNodeExecutor) postgresObserveReceipt(node *runNode, phase string, targetDigest string) postgresObserveReceipt {
	targetID, guardMode, selected := e.postgresTargetContext(node)
	return postgresObserveReceipt{
		APIVersion:       "torque.dev/postgres-resource/v1",
		Kind:             "PostgresObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           "observed",
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		Database:         strings.TrimSpace(node.Postgres.Database),
		TargetDigest:     strings.TrimSpace(targetDigest),
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) postgresPlanReceipt(node *runNode, phase string, resource json.RawMessage, command string, status string, reason string) postgresPlanReceipt {
	targetID, guardMode, selected := e.postgresTargetContext(node)
	var lockScopes []string
	var policySources []string
	if e != nil && e.run != nil && e.run.Plan != nil && e.run.Plan.Ops != nil {
		for _, lockInput := range e.run.Plan.Ops.Locks {
			if strings.TrimSpace(lockInput.Scope) != "" {
				lockScopes = append(lockScopes, strings.TrimSpace(lockInput.Scope))
			}
		}
		for _, decision := range e.run.Plan.Ops.PolicyDecisions {
			if strings.TrimSpace(decision.Source) != "" {
				policySources = append(policySources, strings.TrimSpace(decision.Source))
			}
		}
	}
	sort.Strings(lockScopes)
	sort.Strings(policySources)
	transportName := strings.TrimSpace(node.Postgres.Transport)
	adapterKind := "postgres.shell-fallback"
	if postgresExecutionMode(node.Postgres) == "native" {
		adapterKind = "postgres.native"
	}
	if transportIsNATS(transportName) {
		command = ""
	}
	return postgresPlanReceipt{
		APIVersion:      "torque.dev/postgres-resource/v1",
		Kind:            "PostgresPlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          strings.TrimSpace(status),
		Reason:          strings.TrimSpace(reason),
		GuardMode:       guardMode,
		AdapterKind:     adapterKind,
		Transport:       transportName,
		Operation:       normalizeNodeKind(node.Kind),
		ResourceDigest:  digestString(string(resource)),
		CommandDigest:   digestString(command),
		Database:        strings.TrimSpace(node.Postgres.Database),
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) enrichPostgresPlanFromFanout(plan *postgresPlanReceipt, fanout fleetNATSFanoutReceipt) {
	if plan == nil {
		return
	}
	for _, result := range fanout.Results {
		e.enrichPostgresPlanFromReceipt(plan, result.Receipt)
		if strings.TrimSpace(plan.PlannedSQLDigest) != "" {
			return
		}
	}
}

func (e *customNodeExecutor) enrichPostgresPlanFromReceipt(plan *postgresPlanReceipt, receipt transport.OperationResult) {
	if plan == nil || strings.TrimSpace(plan.PlannedSQLDigest) != "" {
		return
	}
	if digest := strings.TrimSpace(receipt.Metadata["resourceSQLDigest"]); digest != "" {
		plan.PlannedSQLDigest = digest
		return
	}
	parsed := parsePostgresStdoutEvidence(receipt.Stdout)
	if parsed == nil {
		return
	}
	planValue, ok := parsed["plan"].(map[string]any)
	if !ok {
		return
	}
	if digest, ok := planValue["sqlDigest"].(string); ok && strings.TrimSpace(digest) != "" {
		plan.PlannedSQLDigest = strings.TrimSpace(digest)
	}
}

func (e *customNodeExecutor) postgresVerifyReceipt(node *runNode, phase string, targetID string, receipt transport.OperationResult) postgresVerifyReceipt {
	status := "succeeded"
	reason := "PostgreSQL resource receipt succeeded"
	if postgresReceiptBlocked(receipt) {
		status = "blocked"
		reason = postgresReceiptMessage(receipt)
	} else if !nodeStepSucceeded(receipt.Status) {
		status = "failed"
		reason = firstReceiptMessage(receipt)
	}
	return postgresVerifyReceipt{
		APIVersion:   "torque.dev/postgres-resource/v1",
		Kind:         "PostgresVerifyReceipt",
		NodeID:       node.ID,
		NodeKind:     normalizeNodeKind(node.Kind),
		TargetID:     strings.TrimSpace(targetID),
		Phase:        phase,
		Status:       status,
		Reason:       reason,
		Operation:    strings.TrimSpace(receipt.Operation),
		ExitCode:     receipt.ExitCode,
		StdoutDigest: digestString(receipt.Stdout),
		StderrDigest: digestString(receipt.Stderr),
		VerifiedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) postgresTargetContext(node *runNode) (string, string, []string) {
	targetID := ""
	if node != nil {
		targetID = strings.TrimSpace(node.Postgres.TargetID)
	}
	guardMode := "legacy"
	var selected []string
	if e != nil && e.run != nil && e.run.Plan != nil && e.run.Plan.Ops != nil {
		guardMode = "ops"
		if e.run.Plan.Ops.TargetGraph != nil {
			selected = append([]string(nil), e.run.Plan.Ops.TargetGraph.Selection.MatchedTargetIDs...)
		}
		if targetID == "" && len(selected) == 1 {
			targetID = selected[0]
		}
	}
	sort.Strings(selected)
	return targetID, guardMode, selected
}

func (e *customNodeExecutor) recordPostgresReceipts(node *runNode, phase string, status string, reason string, observe postgresObserveReceipt, plan postgresPlanReceipt, execute *transport.OperationResult, fanout *fleetNATSFanoutReceipt, verify postgresVerifyReceipt) {
	payload := e.postgresNodePayload(node, phase, status, reason, &observe, &plan, execute, fanout)
	payload["verify"] = verify
	e.run.RecordJSONArtifact(node.ID, "postgres-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "postgres-plan.json", plan)
	if execute != nil {
		e.run.RecordJSONArtifact(node.ID, "postgres-execute.json", *execute)
	}
	if fanout != nil {
		e.run.RecordJSONArtifact(node.ID, "postgres-fanout.json", *fanout)
	}
	e.run.RecordJSONArtifact(node.ID, "postgres-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, "postgres-resource.json", payload)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) postgresNodePayload(node *runNode, phase string, status string, reason string, observe *postgresObserveReceipt, plan *postgresPlanReceipt, execute *transport.OperationResult, fanout *fleetNATSFanoutReceipt) map[string]any {
	payload := map[string]any{
		"apiVersion": "torque.dev/postgres-resource/v1",
		"kind":       "PostgresResourceArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"status":     strings.TrimSpace(status),
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if observe != nil {
		payload["observe"] = *observe
	}
	if plan != nil {
		payload["plan"] = *plan
		payload["targetId"] = strings.TrimSpace(plan.TargetID)
		payload["guardMode"] = strings.TrimSpace(plan.GuardMode)
	}
	if execute != nil {
		payload["targetDigest"] = execute.TargetDigest
		payload["receipt"] = *execute
		payload["execute"] = *execute
		if parsed := parsePostgresStdoutEvidence(execute.Stdout); parsed != nil {
			payload["result"] = parsed
		}
	}
	if fanout != nil {
		payload["fanout"] = *fanout
	}
	return payload
}

func parsePostgresStdoutEvidence(stdout string) map[string]any {
	lines := strings.Split(strings.TrimSpace(stdout), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var parsed map[string]any
		if err := json.Unmarshal([]byte(line), &parsed); err == nil {
			return parsed
		}
	}
	return nil
}

func postgresReceiptBlocked(receipt transport.OperationResult) bool {
	return strings.EqualFold(strings.TrimSpace(receipt.Status), "blocked")
}

func postgresReceiptMessage(receipt transport.OperationResult) string {
	if parsed := parsePostgresStdoutEvidence(receipt.Stdout); parsed != nil {
		if msg := strings.TrimSpace(stringFromAny(parsed["message"])); msg != "" {
			return msg
		}
	}
	return firstReceiptMessage(receipt)
}

func postgresTransportIsLocal(transport string) bool {
	switch strings.ToLower(strings.TrimSpace(transport)) {
	case "", "local", "localhost":
		return true
	default:
		return false
	}
}

func runLocalPostgresResource(ctx context.Context, started time.Time, resource json.RawMessage, targetDigest string) transport.OperationResult {
	var req opspostgres.ResourceRequest
	if err := json.Unmarshal(resource, &req); err != nil {
		return transport.OperationResult{
			Operation:    "resource",
			Status:       "failed",
			TargetDigest: targetDigest,
			Command:      []string{"postgres.resource"},
			ExitCode:     1,
			Error:        "parse PostgreSQL resource request: " + err.Error(),
		}
	}
	result, err := opspostgres.Runner{}.Execute(ctx, req)
	raw, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		raw = []byte(`{"status":"failed","message":"marshal PostgreSQL resource result"}`)
	}
	status := strings.TrimSpace(result.Status)
	if status == "" {
		status = "succeeded"
	}
	exitCode := 0
	errMsg := ""
	if err != nil || strings.EqualFold(status, "failed") {
		exitCode = 1
		if err != nil {
			errMsg = err.Error()
		} else {
			errMsg = strings.TrimSpace(result.Message)
		}
	}
	metadata := map[string]string{
		"resourceApiVersion":        strings.TrimSpace(req.APIVersion),
		"resourceKind":              strings.TrimSpace(req.NodeKind),
		"resourceStatus":            status,
		"resourceChanged":           strconv.FormatBool(result.Changed),
		"resourcePlanAction":        strings.TrimSpace(result.Plan.Action),
		"resourceSQLDigest":         strings.TrimSpace(result.Plan.SQLDigest),
		"resourcePlannedSQLDigest":  strings.TrimSpace(result.PlannedSQLDigest),
		"resourceExecutedSQLDigest": strings.TrimSpace(result.ExecutedSQLDigest),
	}
	return transport.OperationResult{
		Operation:      "resource",
		Status:         status,
		TargetDigest:   targetDigest,
		Command:        []string{"postgres.resource", strings.TrimSpace(req.NodeKind)},
		Stdout:         string(raw) + "\n",
		ExitCode:       exitCode,
		Error:          errMsg,
		DurationMillis: time.Since(started).Milliseconds(),
		Metadata:       metadata,
	}
}

func enrichPostgresReceiptFromStdout(receipt *transport.OperationResult) {
	if receipt == nil {
		return
	}
	parsed := parsePostgresStdoutEvidence(receipt.Stdout)
	if parsed == nil {
		return
	}
	if receipt.Metadata == nil {
		receipt.Metadata = map[string]string{}
	}
	if status := strings.TrimSpace(stringFromAny(parsed["status"])); status != "" {
		receipt.Metadata["resourceStatus"] = status
		if strings.EqualFold(status, "blocked") {
			receipt.Status = "blocked"
		}
	}
	if changed, ok := boolStringFromAny(parsed["changed"]); ok {
		receipt.Metadata["resourceChanged"] = changed
	}
	if digest := firstNonEmptyString(stringFromAny(parsed["plannedSqlDigest"]), postgresPlanDigestFromParsed(parsed)); digest != "" {
		receipt.Metadata["resourceSQLDigest"] = digest
		receipt.Metadata["resourcePlannedSQLDigest"] = digest
	}
	if digest := strings.TrimSpace(stringFromAny(parsed["executedSqlDigest"])); digest != "" {
		receipt.Metadata["resourceExecutedSQLDigest"] = digest
	}
	if lock := asStringAnyMap(parsed["lock"]); lock != nil {
		copyPostgresMetadata(receipt.Metadata, "postgresLockKey", lock["lockKey"])
		copyPostgresMetadata(receipt.Metadata, "postgresLockDigest", lock["lockDigest"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresLockAcquired", lock["lockAcquired"])
		copyPostgresMetadata(receipt.Metadata, "postgresLockWaitMillis", lock["lockWaitMillis"])
		copyPostgresMetadata(receipt.Metadata, "postgresLockTimeoutMillis", lock["timeoutMillis"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresLockReleased", lock["released"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresLockBlocked", lock["blocked"])
		copyPostgresMetadata(receipt.Metadata, "postgresLockReleaseError", lock["releaseError"])
	}
	if txn := asStringAnyMap(parsed["transaction"]); txn != nil {
		copyPostgresBoolMetadata(receipt.Metadata, "postgresTransactionSupported", txn["supported"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresTransactionStarted", txn["transactionStarted"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresTransactionCommitted", txn["transactionCommitted"])
		copyPostgresBoolMetadata(receipt.Metadata, "postgresTransactionRolledBack", txn["transactionRolledBack"])
		copyPostgresMetadata(receipt.Metadata, "postgresTransactionReason", txn["reason"])
	}
	if backup := asStringAnyMap(parsed["backup"]); backup != nil {
		copyPostgresMetadata(receipt.Metadata, "postgresBackupID", backup["id"])
		copyPostgresMetadata(receipt.Metadata, "postgresBackupFile", backup["file"])
		copyPostgresMetadata(receipt.Metadata, "postgresBackupManifestPath", backup["manifestPath"])
		copyPostgresMetadata(receipt.Metadata, "postgresBackupCatalogPath", backup["catalogPath"])
		copyPostgresMetadata(receipt.Metadata, "postgresBackupSha256", backup["sha256"])
		copyPostgresMetadata(receipt.Metadata, "postgresBackupBytes", backup["bytes"])
		if store := asStringAnyMap(backup["store"]); store != nil {
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreType", store["type"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreURI", store["uri"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreBucket", store["bucket"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreKey", store["key"])
			copyPostgresBoolMetadata(receipt.Metadata, "postgresBackupStoreUploaded", store["uploaded"])
			copyPostgresBoolMetadata(receipt.Metadata, "postgresBackupStoreResumed", store["resumed"])
			copyPostgresBoolMetadata(receipt.Metadata, "postgresBackupStoreMultipart", store["multipart"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreParts", store["parts"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreSessionPath", store["sessionPath"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreManifestURI", store["manifestUri"])
			copyPostgresMetadata(receipt.Metadata, "postgresBackupStoreCatalogURI", store["catalogUri"])
		}
	}
}

func postgresPlanDigestFromParsed(parsed map[string]any) string {
	plan := asStringAnyMap(parsed["plan"])
	if plan == nil {
		return ""
	}
	return strings.TrimSpace(stringFromAny(plan["sqlDigest"]))
}

func copyPostgresMetadata(metadata map[string]string, key string, value any) {
	if out := strings.TrimSpace(stringFromAny(value)); out != "" {
		metadata[key] = out
	}
}

func copyPostgresBoolMetadata(metadata map[string]string, key string, value any) {
	if out, ok := boolStringFromAny(value); ok {
		metadata[key] = out
	}
}

func boolStringFromAny(value any) (string, bool) {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v), true
	case string:
		v = strings.TrimSpace(v)
		if v == "" {
			return "", false
		}
		if parsed, err := strconv.ParseBool(v); err == nil {
			return strconv.FormatBool(parsed), true
		}
		return v, true
	default:
		return "", false
	}
}

func buildPostgresCommand(kind string, spec PostgresSpec, nodeID string, runID string) (string, error) {
	var b strings.Builder
	b.WriteString("bash <<'TORQUE_POSTGRES_RESOURCE'\n")
	b.WriteString("set -euo pipefail\n")
	writeShellAssignment(&b, "TORQUE_POSTGRES_KIND", normalizeNodeKind(kind))
	writeShellAssignment(&b, "TORQUE_NODE_ID", nodeID)
	writeShellAssignment(&b, "TORQUE_RUN_ID", runID)
	writeShellAssignment(&b, "PG_DATABASE", spec.Database)
	writeShellAssignment(&b, "PG_HOST_VALUE", spec.Host)
	writeShellAssignment(&b, "PG_USER_VALUE", spec.User)
	writeShellAssignment(&b, "PG_PASSWORD_ENV", spec.PasswordEnv)
	writeShellAssignment(&b, "PG_SSLMODE_VALUE", spec.SSLMode)
	writeShellAssignment(&b, "PSQL_COMMAND", spec.PSQLCommand)
	writeShellAssignment(&b, "PG_DUMP_COMMAND", spec.PGDumpCommand)
	writeShellAssignment(&b, "PG_RESTORE_COMMAND", spec.PGRestoreCommand)
	writeShellAssignment(&b, "RUN_AS_USER", spec.RunAsUser)
	fmt.Fprintf(&b, "PG_PORT_VALUE=%d\n", spec.Port)
	writePostgresKindAssignments(&b, normalizeNodeKind(kind), spec)
	b.WriteString(postgresResourceScriptBody)
	b.WriteString("\nTORQUE_POSTGRES_RESOURCE\n")
	return b.String(), nil
}

func postgresExecutionMode(spec PostgresSpec) string {
	transportName := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transportName == "nats" || transportName == "nats-mesh" {
		return "native"
	}
	mode := strings.ToLower(strings.TrimSpace(spec.ExecutionMode))
	switch mode {
	case "native", "shell":
		return mode
	}
	if transportName == "ssh" {
		return "native"
	}
	return "shell"
}

func buildPostgresNativeCommand(spec PostgresSpec, resource json.RawMessage) (string, error) {
	if len(resource) == 0 {
		return "", fmt.Errorf("postgres native execution requires a resource request payload")
	}
	agentPath := strings.TrimSpace(spec.AgentPath)
	if agentPath == "" {
		agentPath = defaultPostgresAgentPath
	}
	encoded := base64.StdEncoding.EncodeToString(resource)
	quotedAgentPath := transport.ShellQuote(agentPath)
	quotedPayload := transport.ShellQuote(encoded)
	return "test -x " + quotedAgentPath + " || { echo " + transport.ShellQuote("torque-agent postgres native executor missing at "+agentPath) + " >&2; exit 127; }; exec " + quotedAgentPath + " postgres-resource-exec --request-b64 " + quotedPayload, nil
}

func writePostgresKindAssignments(b *strings.Builder, kind string, spec PostgresSpec) {
	switch kind {
	case NodeKindPostgresRoleEnsure:
		writeShellAssignment(b, "ROLE_NAME", spec.Role.Name)
		writeShellAssignment(b, "ROLE_PASSWORD_ENV", spec.Role.PasswordEnv)
		writeBoolPtrAssignment(b, "ROLE_LOGIN_SET", "ROLE_LOGIN_VALUE", spec.Role.Login)
		writeBoolPtrAssignment(b, "ROLE_SUPERUSER_SET", "ROLE_SUPERUSER_VALUE", spec.Role.Superuser)
	case NodeKindPostgresDatabaseEnsure:
		writeShellAssignment(b, "DATABASE_NAME", spec.DatabaseRef.Name)
		writeShellAssignment(b, "DATABASE_OWNER", spec.DatabaseRef.Owner)
	case NodeKindPostgresGrantEnsure:
		writeShellAssignment(b, "GRANT_ROLE", spec.Grant.Role)
		writeShellAssignment(b, "GRANT_DATABASE", spec.Grant.Database)
		writeShellAssignment(b, "GRANT_SCHEMA", spec.Grant.Schema)
		writeShellAssignment(b, "GRANT_OBJECT_TYPE", spec.Grant.ObjectType)
		writeShellAssignment(b, "GRANT_PRIVILEGES", strings.Join(spec.Grant.Privileges, ","))
	case NodeKindPostgresSchemaEnsure:
		writeShellAssignment(b, "SCHEMA_NAME", spec.Schema.Name)
		writeShellAssignment(b, "SCHEMA_DATABASE", spec.Schema.Database)
		writeShellAssignment(b, "SCHEMA_OWNER", spec.Schema.Owner)
	case NodeKindPostgresExtensionEnsure:
		writeShellAssignment(b, "EXTENSION_NAME", spec.Extension.Name)
		writeShellAssignment(b, "EXTENSION_DATABASE", spec.Extension.Database)
		writeShellAssignment(b, "EXTENSION_SCHEMA", spec.Extension.Schema)
	case NodeKindPostgresReplicationVerify:
		fmt.Fprintf(b, "EXPECTED_REPLICAS=%d\n", spec.Replication.ExpectedReplicas)
		if spec.Replication.RequireStreaming {
			b.WriteString("REQUIRE_STREAMING=1\n")
		} else {
			b.WriteString("REQUIRE_STREAMING=0\n")
		}
	case NodeKindPostgresBackupRun:
		writeShellAssignment(b, "BACKUP_PATH", spec.Backup.Path)
		writeShellAssignment(b, "BACKUP_FILE", spec.Backup.File)
		writeShellAssignment(b, "BACKUP_DATABASE", spec.Backup.Database)
		writeShellAssignment(b, "BACKUP_FORMAT", spec.Backup.Format)
		writeShellAssignment(b, "BACKUP_MANIFEST_PATH", spec.Backup.ManifestPath)
		fmt.Fprintf(b, "BACKUP_COMPRESS=%d\n", spec.Backup.Compress)
		if spec.Backup.SimulateDuration != nil {
			fmt.Fprintf(b, "SIMULATE_SECONDS=%s\n", transport.ShellQuote(fmt.Sprintf("%.3f", spec.Backup.SimulateDuration.Seconds())))
		} else {
			b.WriteString("SIMULATE_SECONDS=0\n")
		}
	case NodeKindPostgresBackupVerify:
		writeShellAssignment(b, "BACKUP_FILE", spec.Backup.File)
		writeShellAssignment(b, "BACKUP_MANIFEST_PATH", spec.Backup.ManifestPath)
		writeShellAssignment(b, "EXPECTED_SHA256", spec.Backup.ExpectedSha256)
	case NodeKindPostgresRestoreDrill:
		writeShellAssignment(b, "RESTORE_BACKUP_FILE", firstNonEmptyString(spec.Restore.BackupFile, spec.Backup.File))
		writeShellAssignment(b, "BACKUP_MANIFEST_PATH", spec.Backup.ManifestPath)
		writeShellAssignment(b, "RESTORE_DATABASE", spec.Restore.Database)
		writeShellAssignment(b, "RESTORE_VERIFY_SQL", spec.Restore.VerifySQL)
		writeShellAssignment(b, "RESTORE_EXPECT", spec.Restore.Expect)
		if spec.Restore.Cleanup {
			b.WriteString("RESTORE_CLEANUP=1\n")
		} else {
			b.WriteString("RESTORE_CLEANUP=0\n")
		}
	case NodeKindPostgresConfigEnsure:
		b.WriteString("CONFIG_SETTINGS=()\n")
		keys := make([]string, 0, len(spec.Config.Settings))
		for key := range spec.Config.Settings {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			fmt.Fprintf(b, "CONFIG_SETTINGS+=(%s)\n", transport.ShellQuote(strings.TrimSpace(key)+"="+strings.TrimSpace(spec.Config.Settings[key])))
		}
		if spec.Config.Reload {
			b.WriteString("CONFIG_RELOAD=1\n")
		} else {
			b.WriteString("CONFIG_RELOAD=0\n")
		}
	case NodeKindPostgresMaintenanceRun:
		writeShellAssignment(b, "MAINT_ACTION", spec.Maintenance.Action)
		writeShellAssignment(b, "MAINT_DATABASE", spec.Maintenance.Database)
		writeShellAssignment(b, "MAINT_TABLE", spec.Maintenance.Table)
	}
}

func writeBoolPtrAssignment(b *strings.Builder, setKey string, valueKey string, value *bool) {
	if value == nil {
		fmt.Fprintf(b, "%s=0\n%s=0\n", setKey, valueKey)
		return
	}
	fmt.Fprintf(b, "%s=1\n", setKey)
	if *value {
		fmt.Fprintf(b, "%s=1\n", valueKey)
	} else {
		fmt.Fprintf(b, "%s=0\n", valueKey)
	}
}

const postgresResourceScriptBody = `
started_at="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
changed=0
message="ok"
detail=""

json_escape() {
  local s="${1:-}"
  s="${s//\\/\\\\}"
  s="${s//\"/\\\"}"
  s="${s//$'\n'/\\n}"
  s="${s//$'\r'/}"
  printf '%s' "$s"
}

emit_json() {
  local status="$1"
  local msg="$2"
  local extra="${3:-}"
  printf '{"apiVersion":"torque.dev/postgres-resource-result/v1","kind":"PostgresResourceResult","nodeId":"%s","runId":"%s","nodeKind":"%s","status":"%s","changed":%s,"database":"%s","message":"%s","startedAt":"%s","completedAt":"%s"%s}\n' \
    "$(json_escape "${TORQUE_NODE_ID}")" \
    "$(json_escape "${TORQUE_RUN_ID}")" \
    "$(json_escape "${TORQUE_POSTGRES_KIND}")" \
    "$(json_escape "${status}")" \
    "${changed}" \
    "$(json_escape "${PG_DATABASE}")" \
    "$(json_escape "${msg}")" \
    "$(json_escape "${started_at}")" \
    "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
    "${extra}"
}

sql_literal() {
  local s="${1:-}"
  s="${s//\'/\'\'}"
  printf "'%s'" "$s"
}

sql_ident() {
  local s="${1:-}"
  s="${s//\"/\"\"}"
  printf '"%s"' "$s"
}

pg_env_args() {
  [[ -n "${PG_HOST_VALUE}" ]] && printf '%s\0' "PGHOST=${PG_HOST_VALUE}"
  [[ "${PG_PORT_VALUE}" != "0" ]] && printf '%s\0' "PGPORT=${PG_PORT_VALUE}"
  [[ -n "${PG_USER_VALUE}" ]] && printf '%s\0' "PGUSER=${PG_USER_VALUE}"
  [[ -n "${PG_SSLMODE_VALUE}" ]] && printf '%s\0' "PGSSLMODE=${PG_SSLMODE_VALUE}"
  if [[ -n "${PG_PASSWORD_ENV}" ]]; then
    local pass="${!PG_PASSWORD_ENV:-}"
    [[ -n "${pass}" ]] && printf '%s\0' "PGPASSWORD=${pass}"
  fi
}

run_as_prefix() {
  if [[ -n "${RUN_AS_USER}" && -z "${PG_HOST_VALUE}" && -z "${PG_PASSWORD_ENV}" ]]; then
    printf '%s\0' runuser -u "${RUN_AS_USER}" --
  fi
}

run_pg() {
  local db="$1"
  local sql="$2"
  local -a prefix envs
  mapfile -d '' -t prefix < <(run_as_prefix)
  mapfile -d '' -t envs < <(pg_env_args)
  if [[ "${#prefix[@]}" -gt 0 ]]; then
    "${prefix[@]}" env "${envs[@]}" "${PSQL_COMMAND}" -X -v ON_ERROR_STOP=1 -At -d "${db}" -c "${sql}"
  else
    env "${envs[@]}" "${PSQL_COMMAND}" -X -v ON_ERROR_STOP=1 -At -d "${db}" -c "${sql}"
  fi
}

run_pg_quiet() {
  local db="$1"
  local sql="$2"
  run_pg "${db}" "${sql}" >/dev/null
}

run_createdb() {
  local db="$1"
  local owner="${2:-}"
  local -a prefix envs args
  mapfile -d '' -t prefix < <(run_as_prefix)
  mapfile -d '' -t envs < <(pg_env_args)
  args=("${PSQL_COMMAND}" -X -v ON_ERROR_STOP=1 -d postgres -c "CREATE DATABASE $(sql_ident "${db}")")
  if [[ -n "${owner}" ]]; then
    args=("${PSQL_COMMAND}" -X -v ON_ERROR_STOP=1 -d postgres -c "CREATE DATABASE $(sql_ident "${db}") OWNER $(sql_ident "${owner}")")
  fi
  if [[ "${#prefix[@]}" -gt 0 ]]; then
    "${prefix[@]}" env "${envs[@]}" "${args[@]}"
  else
    env "${envs[@]}" "${args[@]}"
  fi
}

run_dropdb_if_exists() {
  local db="$1"
  run_pg_quiet postgres "DROP DATABASE IF EXISTS $(sql_ident "${db}") WITH (FORCE)"
}

run_dump() {
  local db="$1"
  local file="$2"
  local -a prefix envs args
  mapfile -d '' -t prefix < <(run_as_prefix)
  mapfile -d '' -t envs < <(pg_env_args)
  args=("${PG_DUMP_COMMAND}" -Fc -d "${db}" -f "${file}")
  if [[ "${BACKUP_COMPRESS:-0}" != "0" ]]; then
    args+=(-Z "${BACKUP_COMPRESS}")
  fi
  if [[ "${#prefix[@]}" -gt 0 ]]; then
    "${prefix[@]}" env "${envs[@]}" "${args[@]}"
  else
    env "${envs[@]}" "${args[@]}"
  fi
}

run_restore() {
  local db="$1"
  local file="$2"
  local -a prefix envs
  mapfile -d '' -t prefix < <(run_as_prefix)
  mapfile -d '' -t envs < <(pg_env_args)
  if [[ "${#prefix[@]}" -gt 0 ]]; then
    "${prefix[@]}" env "${envs[@]}" "${PG_RESTORE_COMMAND}" --no-owner --role="${PG_USER_VALUE:-postgres}" -d "${db}" "${file}"
  else
    env "${envs[@]}" "${PG_RESTORE_COMMAND}" --no-owner -d "${db}" "${file}"
  fi
}

manifest_backup_file() {
  local manifest="$1"
  [[ -f "${manifest}" ]] || return 1
  sed -n 's/.*"file"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' "${manifest}" | head -n 1
}

case "${TORQUE_POSTGRES_KIND}" in
  postgres.role.ensure)
    role_lit="$(sql_literal "${ROLE_NAME}")"
    password_value=""
    if [[ -n "${ROLE_PASSWORD_ENV}" ]]; then
      password_value="${!ROLE_PASSWORD_ENV:-}"
    fi
    password_lit="$(sql_literal "${password_value}")"
    run_pg_quiet postgres "DO \$torque\$ DECLARE role_name text := ${role_lit}; role_password text := ${password_lit}; BEGIN IF NOT EXISTS (SELECT 1 FROM pg_roles WHERE rolname = role_name) THEN EXECUTE format('CREATE ROLE %I', role_name); END IF; IF ${ROLE_LOGIN_SET:-0} = 1 THEN IF ${ROLE_LOGIN_VALUE:-0} = 1 THEN EXECUTE format('ALTER ROLE %I LOGIN', role_name); ELSE EXECUTE format('ALTER ROLE %I NOLOGIN', role_name); END IF; END IF; IF ${ROLE_SUPERUSER_SET:-0} = 1 THEN IF ${ROLE_SUPERUSER_VALUE:-0} = 1 THEN EXECUTE format('ALTER ROLE %I SUPERUSER', role_name); ELSE EXECUTE format('ALTER ROLE %I NOSUPERUSER', role_name); END IF; END IF; IF role_password <> '' THEN EXECUTE format('ALTER ROLE %I PASSWORD %L', role_name, role_password); END IF; END \$torque\$;"
    changed=1
    message="role ensured"
    detail=",\"role\":\"$(json_escape "${ROLE_NAME}")\""
    ;;
  postgres.database.ensure)
    exists="$(run_pg postgres "SELECT 1 FROM pg_database WHERE datname = $(sql_literal "${DATABASE_NAME}")" || true)"
    if [[ "${exists}" != "1" ]]; then
      run_createdb "${DATABASE_NAME}" "${DATABASE_OWNER}"
      changed=1
    fi
    message="database ensured"
    detail=",\"databaseName\":\"$(json_escape "${DATABASE_NAME}")\""
    ;;
  postgres.grant.ensure)
    object_type="${GRANT_OBJECT_TYPE:-database}"
    privs="${GRANT_PRIVILEGES:-CONNECT}"
    case "${object_type}" in
      database|"")
        run_pg_quiet postgres "GRANT ${privs} ON DATABASE $(sql_ident "${GRANT_DATABASE}") TO $(sql_ident "${GRANT_ROLE}")"
        ;;
      schema)
        run_pg_quiet "${GRANT_DATABASE}" "GRANT ${privs} ON SCHEMA $(sql_ident "${GRANT_SCHEMA}") TO $(sql_ident "${GRANT_ROLE}")"
        ;;
      tables)
        run_pg_quiet "${GRANT_DATABASE}" "GRANT ${privs} ON ALL TABLES IN SCHEMA $(sql_ident "${GRANT_SCHEMA}") TO $(sql_ident "${GRANT_ROLE}")"
        ;;
      *)
        echo "unsupported grant object type ${object_type}" >&2
        exit 2
        ;;
    esac
    changed=1
    message="grant ensured"
    detail=",\"role\":\"$(json_escape "${GRANT_ROLE}")\",\"objectType\":\"$(json_escape "${object_type}")\""
    ;;
  postgres.schema.ensure)
    owner_clause=""
    [[ -n "${SCHEMA_OWNER}" ]] && owner_clause=" AUTHORIZATION $(sql_ident "${SCHEMA_OWNER}")"
    run_pg_quiet "${SCHEMA_DATABASE}" "CREATE SCHEMA IF NOT EXISTS $(sql_ident "${SCHEMA_NAME}")${owner_clause}"
    changed=1
    message="schema ensured"
    detail=",\"schema\":\"$(json_escape "${SCHEMA_NAME}")\""
    ;;
  postgres.extension.ensure)
    schema_clause=""
    [[ -n "${EXTENSION_SCHEMA}" ]] && schema_clause=" SCHEMA $(sql_ident "${EXTENSION_SCHEMA}")"
    run_pg_quiet "${EXTENSION_DATABASE}" "CREATE EXTENSION IF NOT EXISTS $(sql_ident "${EXTENSION_NAME}")${schema_clause}"
    changed=1
    message="extension ensured"
    detail=",\"extension\":\"$(json_escape "${EXTENSION_NAME}")\""
    ;;
  postgres.replication.verify)
    role="$(run_pg postgres "SELECT CASE WHEN pg_is_in_recovery() THEN 'replica' ELSE 'primary' END")"
    if [[ "${role}" != "primary" ]]; then
      echo "postgres replication verify must run on primary, got ${role}" >&2
      exit 3
    fi
    if [[ "${REQUIRE_STREAMING:-0}" = "1" ]]; then
      replicas="$(run_pg postgres "SELECT count(*) FROM pg_stat_replication WHERE state = 'streaming'")"
    else
      replicas="$(run_pg postgres "SELECT count(*) FROM pg_stat_replication")"
    fi
    if [[ "${replicas}" -lt "${EXPECTED_REPLICAS:-0}" ]]; then
      echo "replicas ${replicas} < expected ${EXPECTED_REPLICAS}" >&2
      exit 4
    fi
    message="replication verified"
    detail=",\"replicas\":${replicas},\"expectedReplicas\":${EXPECTED_REPLICAS:-0}"
    ;;
  postgres.backup.run)
    db="${BACKUP_DATABASE:-${PG_DATABASE}}"
    if [[ -z "${BACKUP_FILE}" ]]; then
      safe_run="${TORQUE_RUN_ID//[^A-Za-z0-9_.-]/_}"
      BACKUP_FILE="${BACKUP_PATH%/}/${db}-${safe_run}.dump"
    fi
    if [[ -z "${BACKUP_MANIFEST_PATH}" ]]; then
      BACKUP_MANIFEST_PATH="${BACKUP_FILE}.manifest.json"
    fi
    mkdir -p "${BACKUP_PATH}"
    if [[ -n "${RUN_AS_USER}" ]]; then
      chown "${RUN_AS_USER}:${RUN_AS_USER}" "${BACKUP_PATH}" 2>/dev/null || true
    fi
    if awk "BEGIN {exit !(${SIMULATE_SECONDS:-0} > 0)}"; then
      sleep "${SIMULATE_SECONDS}"
    fi
    tmp="${BACKUP_FILE}.tmp.$$"
    rm -f "${tmp}"
    run_dump "${db}" "${tmp}"
    mv "${tmp}" "${BACKUP_FILE}"
    bytes="$(wc -c < "${BACKUP_FILE}" | tr -d ' ')"
    sha="$(sha256sum "${BACKUP_FILE}" | awk '{print $1}')"
    cat > "${BACKUP_MANIFEST_PATH}" <<EOF
{"apiVersion":"torque.dev/postgres-backup-manifest/v1","kind":"PostgresBackupManifest","runId":"$(json_escape "${TORQUE_RUN_ID}")","nodeId":"$(json_escape "${TORQUE_NODE_ID}")","database":"$(json_escape "${db}")","file":"$(json_escape "${BACKUP_FILE}")","sha256":"${sha}","bytes":${bytes},"createdAt":"$(date -u +%Y-%m-%dT%H:%M:%SZ)"}
EOF
    changed=1
    message="backup completed"
    detail=",\"backupFile\":\"$(json_escape "${BACKUP_FILE}")\",\"manifestPath\":\"$(json_escape "${BACKUP_MANIFEST_PATH}")\",\"sha256\":\"${sha}\",\"bytes\":${bytes}"
    ;;
  postgres.backup.verify)
    file="${BACKUP_FILE}"
    if [[ -z "${file}" && -n "${BACKUP_MANIFEST_PATH}" ]]; then
      file="$(manifest_backup_file "${BACKUP_MANIFEST_PATH}")"
    fi
    [[ -n "${file}" && -f "${file}" ]] || { echo "backup file not found: ${file}" >&2; exit 5; }
    bytes="$(wc -c < "${file}" | tr -d ' ')"
    [[ "${bytes}" -gt 0 ]] || { echo "backup file is empty: ${file}" >&2; exit 6; }
    sha="$(sha256sum "${file}" | awk '{print $1}')"
    if [[ -n "${EXPECTED_SHA256}" && "${sha}" != "${EXPECTED_SHA256}" ]]; then
      echo "backup sha256 ${sha} != expected ${EXPECTED_SHA256}" >&2
      exit 7
    fi
    "${PG_RESTORE_COMMAND}" --list "${file}" >/dev/null
    message="backup verified"
    detail=",\"backupFile\":\"$(json_escape "${file}")\",\"sha256\":\"${sha}\",\"bytes\":${bytes}"
    ;;
  postgres.restore.drill)
    file="${RESTORE_BACKUP_FILE}"
    if [[ -z "${file}" && -n "${BACKUP_MANIFEST_PATH}" ]]; then
      file="$(manifest_backup_file "${BACKUP_MANIFEST_PATH}")"
    fi
    [[ -n "${file}" && -f "${file}" ]] || { echo "restore backup file not found: ${file}" >&2; exit 8; }
    run_dropdb_if_exists "${RESTORE_DATABASE}"
    run_createdb "${RESTORE_DATABASE}" ""
    run_restore "${RESTORE_DATABASE}" "${file}"
    verify_output=""
    if [[ -n "${RESTORE_VERIFY_SQL}" ]]; then
      verify_output="$(run_pg "${RESTORE_DATABASE}" "${RESTORE_VERIFY_SQL}")"
      if [[ -n "${RESTORE_EXPECT}" && "${verify_output}" != "${RESTORE_EXPECT}" ]]; then
        echo "restore verify output ${verify_output} != expected ${RESTORE_EXPECT}" >&2
        exit 9
      fi
    fi
    if [[ "${RESTORE_CLEANUP:-0}" = "1" ]]; then
      run_dropdb_if_exists "${RESTORE_DATABASE}"
      changed=1
    else
      changed=1
    fi
    message="restore drill verified"
    detail=",\"restoreDatabase\":\"$(json_escape "${RESTORE_DATABASE}")\",\"backupFile\":\"$(json_escape "${file}")\",\"verifyOutput\":\"$(json_escape "${verify_output}")\""
    ;;
  postgres.config.ensure)
    for item in "${CONFIG_SETTINGS[@]}"; do
      key="${item%%=*}"
      value="${item#*=}"
      [[ "${key}" =~ ^[A-Za-z0-9_.]+$ ]] || { echo "unsupported postgres setting name ${key}" >&2; exit 10; }
      run_pg_quiet postgres "ALTER SYSTEM SET ${key} TO $(sql_literal "${value}")"
      changed=1
    done
    if [[ "${CONFIG_RELOAD:-0}" = "1" ]]; then
      run_pg_quiet postgres "SELECT pg_reload_conf()"
    fi
    message="config ensured"
    detail=",\"settings\":${#CONFIG_SETTINGS[@]}"
    ;;
  postgres.maintenance.run)
    db="${MAINT_DATABASE:-${PG_DATABASE}}"
    action="$(printf '%s' "${MAINT_ACTION:-analyze}" | tr '[:upper:]' '[:lower:]')"
    table_clause=""
    [[ -n "${MAINT_TABLE}" ]] && table_clause=" $(sql_ident "${MAINT_TABLE}")"
    case "${action}" in
      analyze)
        run_pg_quiet "${db}" "ANALYZE${table_clause}"
        ;;
      vacuum)
        run_pg_quiet "${db}" "VACUUM (ANALYZE)${table_clause}"
        ;;
      reindex)
        if [[ -n "${MAINT_TABLE}" ]]; then
          run_pg_quiet "${db}" "REINDEX TABLE $(sql_ident "${MAINT_TABLE}")"
        else
          run_pg_quiet "${db}" "REINDEX DATABASE $(sql_ident "${db}")"
        fi
        ;;
      *)
        echo "unsupported maintenance action ${action}" >&2
        exit 11
        ;;
    esac
    changed=1
    message="maintenance completed"
    detail=",\"action\":\"$(json_escape "${action}")\""
    ;;
  *)
    echo "unsupported PostgreSQL resource kind ${TORQUE_POSTGRES_KIND}" >&2
    exit 12
    ;;
esac

emit_json "succeeded" "${message}" "${detail}"
`
