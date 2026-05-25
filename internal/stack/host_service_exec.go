package stack

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

type hostServiceState struct {
	Manager       string `json:"manager,omitempty"`
	Name          string `json:"name"`
	Exists        bool   `json:"exists"`
	Active        bool   `json:"active"`
	Enabled       bool   `json:"enabled"`
	LoadState     string `json:"loadState,omitempty"`
	ActiveState   string `json:"activeState,omitempty"`
	SubState      string `json:"subState,omitempty"`
	UnitFileState string `json:"unitFileState,omitempty"`
}

type hostServiceChangeSet struct {
	Active  bool `json:"active"`
	Enabled bool `json:"enabled"`
	Restart bool `json:"restart,omitempty"`
}

type hostServiceCommandReceipt struct {
	Action        string `json:"action"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
}

type hostServiceOperationResult struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	Operation      string                      `json:"operation"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	TargetDigest   string                      `json:"targetDigest,omitempty"`
	ServiceDigest  string                      `json:"serviceDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredEnabled *bool                       `json:"desiredEnabled,omitempty"`
	ServiceManager string                      `json:"serviceManager,omitempty"`
	Changed        bool                        `json:"changed"`
	Changes        hostServiceChangeSet        `json:"changes"`
	Before         hostServiceState            `json:"before"`
	After          hostServiceState            `json:"after"`
	Commands       []hostServiceCommandReceipt `json:"commands,omitempty"`
	Error          string                      `json:"error,omitempty"`
	CompletedAt    string                      `json:"completedAt"`
}

type hostServiceObserveReceipt struct {
	APIVersion       string           `json:"apiVersion"`
	Kind             string           `json:"kind"`
	NodeID           string           `json:"nodeId"`
	NodeKind         string           `json:"nodeKind"`
	TargetID         string           `json:"targetId,omitempty"`
	Phase            string           `json:"phase"`
	Status           string           `json:"status"`
	GuardMode        string           `json:"guardMode"`
	SelectedTargetID string           `json:"selectedTargetId,omitempty"`
	SelectedTargets  []string         `json:"selectedTargets,omitempty"`
	TargetDigest     string           `json:"targetDigest,omitempty"`
	ServiceDigest    string           `json:"serviceDigest,omitempty"`
	State            hostServiceState `json:"state"`
	ObservedAt       string           `json:"observedAt"`
}

type hostServicePlanReceipt struct {
	APIVersion      string               `json:"apiVersion"`
	Kind            string               `json:"kind"`
	NodeID          string               `json:"nodeId"`
	NodeKind        string               `json:"nodeKind"`
	TargetID        string               `json:"targetId,omitempty"`
	Phase           string               `json:"phase"`
	Status          string               `json:"status"`
	Reason          string               `json:"reason,omitempty"`
	GuardMode       string               `json:"guardMode"`
	Operation       string               `json:"operation"`
	ServiceDigest   string               `json:"serviceDigest,omitempty"`
	ServiceName     string               `json:"service,omitempty"`
	ServiceManager  string               `json:"serviceManager,omitempty"`
	DesiredState    string               `json:"desiredState,omitempty"`
	DesiredEnabled  *bool                `json:"desiredEnabled,omitempty"`
	StopOnDelete    bool                 `json:"stopOnDelete,omitempty"`
	DisableOnDelete bool                 `json:"disableOnDelete,omitempty"`
	SelectedTargets []string             `json:"selectedTargets,omitempty"`
	LockScopes      []string             `json:"lockScopes,omitempty"`
	PolicySources   []string             `json:"policySources,omitempty"`
	Changes         hostServiceChangeSet `json:"changes"`
	PlannedAt       string               `json:"plannedAt"`
}

type hostServiceDiffReceipt struct {
	APIVersion     string               `json:"apiVersion"`
	Kind           string               `json:"kind"`
	NodeID         string               `json:"nodeId"`
	TargetID       string               `json:"targetId,omitempty"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	ServiceDigest  string               `json:"serviceDigest,omitempty"`
	Before         hostServiceState     `json:"before"`
	DesiredState   string               `json:"desiredState,omitempty"`
	DesiredEnabled *bool                `json:"desiredEnabled,omitempty"`
	Changes        hostServiceChangeSet `json:"changes"`
	DiffQuality    string               `json:"diffQuality"`
	GeneratedAt    string               `json:"generatedAt"`
}

type hostServiceVerifyReceipt struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	NodeID         string                      `json:"nodeId"`
	TargetID       string                      `json:"targetId,omitempty"`
	Phase          string                      `json:"phase"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	ServiceDigest  string                      `json:"serviceDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredEnabled *bool                       `json:"desiredEnabled,omitempty"`
	Active         bool                        `json:"active"`
	Enabled        bool                        `json:"enabled"`
	Changed        bool                        `json:"changed"`
	Commands       []hostServiceCommandReceipt `json:"commands,omitempty"`
	VerifiedAt     string                      `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostServiceManageNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-service"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-service"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	serviceDigest := digestString(spec.ServiceName)
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		state := hostServiceState{Name: strings.TrimSpace(spec.ServiceName)}
		observe := e.hostServiceObserveReceipt(node, phase, targetID, guardMode, selected, "", serviceDigest, state, "skipped")
		plan := e.hostServicePlanReceipt(node, phase, targetID, guardMode, selected, serviceDigest, hostServiceChangeSet{}, "skipped", reason)
		diff := e.hostServiceDiffReceipt(node, phase, targetID, serviceDigest, state, hostServiceChangeSet{}, "skipped")
		verify := e.hostServiceVerifyReceipt(node, phase, targetID, serviceDigest, hostServiceOperationResult{Status: "skipped", Reason: reason})
		e.recordHostServiceReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	transportClient, err := hostCommandTransport(spec)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	targetDigest := transportClient.TargetDigest()
	observeResult, err := e.runHostServiceOperation(ctx, transportClient, hostServicePayload(spec, "observe"))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostServiceObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, serviceDigest, observeResult.After, observeResult.Status)
	changes := hostServiceChanges(observeResult.After, spec, operation)
	plan := e.hostServicePlanReceipt(node, phase, targetID, guardMode, selected, serviceDigest, changes, "planned", "eligible")
	diff := e.hostServiceDiffReceipt(node, phase, targetID, serviceDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostServiceManage); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostServiceVerifyReceipt(node, phase, targetID, serviceDigest, hostServiceOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostServiceReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_SERVICE_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_SERVICE_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host service phase %s: %w", phase, guardErr))
	}

	var result hostServiceOperationResult
	if operation == "delete" {
		result, err = e.runHostServiceOperation(ctx, transportClient, hostServicePayload(spec, "delete"))
	} else {
		result, err = e.runHostServiceOperation(ctx, transportClient, hostServicePayload(spec, "apply"))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.ServiceDigest = serviceDigest
	if result.Changes == (hostServiceChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostServiceVerifyReceipt(node, phase, targetID, serviceDigest, result)
	e.recordHostServiceReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "host service operation failed")
		runErr := &RunError{Class: "HOST_SERVICE_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_SERVICE_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host service phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func (e *customNodeExecutor) runHostServiceOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostServiceOperationResult, error) {
	command, err := hostServicePythonCommand(payload)
	if err != nil {
		return hostServiceOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostServiceOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostServiceOperationResult{}, fmt.Errorf("decode host.service.manage receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-service-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostServiceOperationReceipt"
	}
	if result.Operation == "" {
		if op, ok := payload["operation"].(string); ok {
			result.Operation = op
		}
	}
	if result.Status == "" {
		result.Status = receipt.Status
	}
	if result.Status == "" {
		result.Status = "failed"
	}
	if !nodeStepSucceeded(receipt.Status) && nodeStepSucceeded(result.Status) {
		result.Status = "failed"
	}
	if result.Error == "" && strings.TrimSpace(receipt.Stderr) != "" {
		result.Error = strings.TrimSpace(receipt.Stderr)
	}
	if result.CompletedAt == "" {
		result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	return result, nil
}

func hostServicePayload(spec HostCommandSpec, operation string) map[string]any {
	var enabled any
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	return map[string]any{
		"operation":       strings.TrimSpace(operation),
		"service":         strings.TrimSpace(spec.ServiceName),
		"serviceManager":  strings.TrimSpace(spec.ServiceManager),
		"state":           hostServiceDesiredState(spec),
		"enabled":         enabled,
		"stopOnDelete":    spec.StopOnDelete,
		"disableOnDelete": spec.DisableOnDelete,
	}
}

func hostServicePythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_SERVICE_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostServicePythonScript + "\nPY", nil
}

func hostServiceDesiredState(spec HostCommandSpec) string {
	return strings.ToLower(strings.TrimSpace(spec.State))
}

func hostServiceChanges(current hostServiceState, spec HostCommandSpec, operation string) hostServiceChangeSet {
	if operation == "delete" {
		return hostServiceChangeSet{
			Active:  spec.StopOnDelete && current.Active,
			Enabled: spec.DisableOnDelete && current.Enabled,
		}
	}
	state := hostServiceDesiredState(spec)
	changes := hostServiceChangeSet{}
	switch state {
	case "started":
		changes.Active = !current.Active
	case "stopped":
		changes.Active = current.Active
	case "restarted":
		changes.Active = true
		changes.Restart = true
	}
	if spec.Enabled != nil {
		changes.Enabled = current.Enabled != *spec.Enabled
	}
	return changes
}

func (e *customNodeExecutor) hostServiceObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, serviceDigest string, state hostServiceState, status string) hostServiceObserveReceipt {
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostServiceObserveReceipt{
		APIVersion:       "torque.dev/host-service-node/v1",
		Kind:             "HostServiceObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           firstNonEmptyString(strings.TrimSpace(status), "observed"),
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		TargetDigest:     strings.TrimSpace(targetDigest),
		ServiceDigest:    strings.TrimSpace(serviceDigest),
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostServicePlanReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, serviceDigest string, changes hostServiceChangeSet, status string, reason string) hostServicePlanReceipt {
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
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostServicePlanReceipt{
		APIVersion:      "torque.dev/host-service-node/v1",
		Kind:            "HostServicePlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostServiceManage,
		ServiceDigest:   serviceDigest,
		ServiceName:     strings.TrimSpace(node.Host.ServiceName),
		ServiceManager:  strings.TrimSpace(node.Host.ServiceManager),
		DesiredState:    hostServiceDesiredState(node.Host),
		DesiredEnabled:  cloneBoolPtr(node.Host.Enabled),
		StopOnDelete:    node.Host.StopOnDelete,
		DisableOnDelete: node.Host.DisableOnDelete,
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		Changes:         changes,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostServiceDiffReceipt(node *runNode, phase string, targetID string, serviceDigest string, before hostServiceState, changes hostServiceChangeSet, status string) hostServiceDiffReceipt {
	return hostServiceDiffReceipt{
		APIVersion:     "torque.dev/host-service-node/v1",
		Kind:           "HostServiceDiffReceipt",
		NodeID:         node.ID,
		TargetID:       targetID,
		Phase:          phase,
		Status:         status,
		ServiceDigest:  serviceDigest,
		Before:         before,
		DesiredState:   hostServiceDesiredState(node.Host),
		DesiredEnabled: cloneBoolPtr(node.Host.Enabled),
		Changes:        changes,
		DiffQuality:    "exact",
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostServiceVerifyReceipt(node *runNode, phase string, targetID string, serviceDigest string, result hostServiceOperationResult) hostServiceVerifyReceipt {
	status := "succeeded"
	reason := "service receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Reason, "service receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "service operation skipped")
	}
	desiredState := firstNonEmptyString(strings.TrimSpace(result.DesiredState), hostServiceDesiredState(node.Host))
	desiredEnabled := result.DesiredEnabled
	if desiredEnabled == nil {
		desiredEnabled = cloneBoolPtr(node.Host.Enabled)
	}
	if status == "succeeded" {
		switch desiredState {
		case "started", "restarted":
			if !result.After.Active {
				status = "failed"
				reason = "service was not active"
			}
		case "stopped":
			if result.After.Active {
				status = "failed"
				reason = "service remained active"
			}
		}
		if desiredEnabled != nil && result.After.Enabled != *desiredEnabled {
			status = "failed"
			reason = "service enablement did not match desired state"
		}
	}
	return hostServiceVerifyReceipt{
		APIVersion:     "torque.dev/host-service-node/v1",
		Kind:           "HostServiceVerifyReceipt",
		NodeID:         node.ID,
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         status,
		Reason:         reason,
		ServiceDigest:  serviceDigest,
		DesiredState:   desiredState,
		DesiredEnabled: cloneBoolPtr(desiredEnabled),
		Active:         result.After.Active,
		Enabled:        result.After.Enabled,
		Changed:        result.Changed,
		Commands:       append([]hostServiceCommandReceipt(nil), result.Commands...),
		VerifiedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func cloneBoolPtr(value *bool) *bool {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func (e *customNodeExecutor) recordHostServiceReceipts(node *runNode, phase string, status string, reason string, observe hostServiceObserveReceipt, plan hostServicePlanReceipt, diff hostServiceDiffReceipt, apply *hostServiceOperationResult, verify hostServiceVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-service-node/v1",
		"kind":       "HostServiceNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"guardMode":  strings.TrimSpace(plan.GuardMode),
		"observe":    observe,
		"plan":       plan,
		"diff":       diff,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if apply != nil {
		payload["targetDigest"] = apply.TargetDigest
		payload["apply"] = *apply
	}
	e.run.RecordJSONArtifact(node.ID, "host-service-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-service-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-service-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-service-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-service-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostServicePythonScript = `
import base64
import hashlib
import json
import os
import shutil
import subprocess
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    if isinstance(data, str):
        data = data.encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()

def digest_json(value):
    return digest_bytes(json.dumps(value, sort_keys=True, separators=(",", ":")))

commands = []

def command_receipt(action, args, proc):
    stdout = proc.stdout or ""
    stderr = proc.stderr or ""
    doc = {
        "action": action,
        "status": "succeeded" if proc.returncode == 0 else "failed",
        "exitCode": int(proc.returncode),
        "commandDigest": digest_json(args),
    }
    if stdout:
        doc["stdoutDigest"] = digest_bytes(stdout)
    if stderr:
        doc["stderrDigest"] = digest_bytes(stderr)
    return doc

def run(action, args):
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    commands.append(command_receipt(action, args, proc))
    return proc

def have(command):
    return shutil.which(command) is not None

def normalize_manager(requested):
    requested = str(requested or "").strip().lower()
    if requested in ("", "systemd", "systemctl"):
        if have("systemctl"):
            return ("systemd", "systemctl")
    raise RuntimeError("unsupported service manager")

def enabled_from_unit_file_state(value):
    return str(value or "").strip() in ("enabled", "enabled-runtime", "linked", "linked-runtime")

def observe_systemd(name, command):
    proc = run("show", [
        command,
        "show",
        "--property=LoadState",
        "--property=ActiveState",
        "--property=SubState",
        "--property=UnitFileState",
        name,
    ])
    props = {
        "LoadState": "",
        "ActiveState": "",
        "SubState": "",
        "UnitFileState": "",
    }
    if proc.returncode == 0:
        for line in proc.stdout.splitlines():
            if "=" not in line:
                continue
            key, value = line.split("=", 1)
            if key in props:
                props[key] = value.strip()
    load_state = props.get("LoadState") or ""
    active_state = props.get("ActiveState") or ""
    unit_file_state = props.get("UnitFileState") or ""
    exists = bool(load_state) and load_state not in ("not-found", "bad-setting")
    return {
        "manager": "systemd",
        "name": name,
        "exists": exists,
        "active": active_state == "active",
        "enabled": enabled_from_unit_file_state(unit_file_state),
        "loadState": load_state,
        "activeState": active_state,
        "subState": props.get("SubState") or "",
        "unitFileState": unit_file_state,
    }

def observe(name, manager, command):
    if manager == "systemd":
        return observe_systemd(name, command)
    raise RuntimeError("unsupported service manager: " + manager)

def changes_for(state, desired_enabled, before, operation, stop_on_delete, disable_on_delete):
    if operation == "delete":
        return {
            "active": bool(stop_on_delete and before.get("active")),
            "enabled": bool(disable_on_delete and before.get("enabled")),
        }
    changes = {"active": False, "enabled": False}
    if state == "started":
        changes["active"] = not bool(before.get("active"))
    elif state == "stopped":
        changes["active"] = bool(before.get("active"))
    elif state == "restarted":
        changes["active"] = True
        changes["restart"] = True
    if desired_enabled is not None:
        changes["enabled"] = bool(before.get("enabled")) != bool(desired_enabled)
    return changes

def changed(changes):
    return bool(changes.get("active") or changes.get("enabled") or changes.get("restart"))

def desired_for_delete(stop_on_delete, disable_on_delete):
    state = "stopped" if stop_on_delete else ""
    enabled = False if disable_on_delete else None
    return state, enabled

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-service-node/v1")
    doc.setdefault("kind", "HostServiceOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_SERVICE_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    name = str(payload.get("service") or "").strip()
    requested_manager = str(payload.get("serviceManager") or "").strip()
    state = str(payload.get("state") or "").strip().lower()
    desired_enabled = payload.get("enabled") if "enabled" in payload and payload.get("enabled") is not None else None
    stop_on_delete = bool(payload.get("stopOnDelete"))
    disable_on_delete = bool(payload.get("disableOnDelete"))
    if not name:
        finish({"operation": operation, "status": "failed", "error": "service is required"}, 1)
    if state not in ("", "started", "stopped", "restarted"):
        finish({"operation": operation, "status": "failed", "error": "unsupported service state"}, 1)
    manager, command = normalize_manager(requested_manager)
    before = observe(name, manager, command)
    planned_changes = changes_for(state, desired_enabled, before, operation, stop_on_delete, disable_on_delete)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "serviceDigest": digest_bytes(name),
            "desiredState": state,
            "desiredEnabled": desired_enabled,
            "serviceManager": manager,
            "changed": False,
            "changes": {"active": False, "enabled": False},
            "before": before,
            "after": before,
            "commands": commands,
        })
    if operation == "delete":
        desired_state, desired_delete_enabled = desired_for_delete(stop_on_delete, disable_on_delete)
        if disable_on_delete and before.get("enabled"):
            proc = run("disable", [command, "disable", name])
            if proc.returncode != 0:
                after = observe(name, manager, command)
                finish({
                    "operation": operation,
                    "status": "failed",
                    "serviceDigest": digest_bytes(name),
                    "desiredState": desired_state,
                    "desiredEnabled": desired_delete_enabled,
                    "serviceManager": manager,
                    "changed": False,
                    "changes": planned_changes,
                    "before": before,
                    "after": after,
                    "commands": commands,
                    "error": "service disable failed",
                }, 1)
        if stop_on_delete and before.get("active"):
            proc = run("stop", [command, "stop", name])
            if proc.returncode != 0:
                after = observe(name, manager, command)
                finish({
                    "operation": operation,
                    "status": "failed",
                    "serviceDigest": digest_bytes(name),
                    "desiredState": desired_state,
                    "desiredEnabled": desired_delete_enabled,
                    "serviceManager": manager,
                    "changed": False,
                    "changes": planned_changes,
                    "before": before,
                    "after": after,
                    "commands": commands,
                    "error": "service stop failed",
                }, 1)
        after = observe(name, manager, command)
        status = "succeeded"
        error = ""
        if stop_on_delete and after.get("active"):
            status = "failed"
            error = "service remained active"
        if disable_on_delete and after.get("enabled"):
            status = "failed"
            error = "service remained enabled"
        finish({
            "operation": operation,
            "status": status,
            "serviceDigest": digest_bytes(name),
            "desiredState": desired_state,
            "desiredEnabled": desired_delete_enabled,
            "serviceManager": manager,
            "changed": changed(planned_changes),
            "changes": planned_changes,
            "before": before,
            "after": after,
            "commands": commands,
            "error": error,
        }, 0 if status == "succeeded" else 1)
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if desired_enabled is not None and bool(before.get("enabled")) != bool(desired_enabled):
        action = "enable" if bool(desired_enabled) else "disable"
        proc = run(action, [command, action, name])
        if proc.returncode != 0:
            after = observe(name, manager, command)
            finish({
                "operation": operation,
                "status": "failed",
                "serviceDigest": digest_bytes(name),
                "desiredState": state,
                "desiredEnabled": desired_enabled,
                "serviceManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": "service " + action + " failed",
            }, 1)
    if state == "started" and not before.get("active"):
        proc = run("start", [command, "start", name])
        if proc.returncode != 0:
            after = observe(name, manager, command)
            finish({
                "operation": operation,
                "status": "failed",
                "serviceDigest": digest_bytes(name),
                "desiredState": state,
                "desiredEnabled": desired_enabled,
                "serviceManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": "service start failed",
            }, 1)
    elif state == "stopped" and before.get("active"):
        proc = run("stop", [command, "stop", name])
        if proc.returncode != 0:
            after = observe(name, manager, command)
            finish({
                "operation": operation,
                "status": "failed",
                "serviceDigest": digest_bytes(name),
                "desiredState": state,
                "desiredEnabled": desired_enabled,
                "serviceManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": "service stop failed",
            }, 1)
    elif state == "restarted":
        proc = run("restart", [command, "restart", name])
        if proc.returncode != 0:
            after = observe(name, manager, command)
            finish({
                "operation": operation,
                "status": "failed",
                "serviceDigest": digest_bytes(name),
                "desiredState": state,
                "desiredEnabled": desired_enabled,
                "serviceManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": "service restart failed",
            }, 1)
    after = observe(name, manager, command)
    status = "succeeded"
    error = ""
    if state in ("started", "restarted") and not after.get("active"):
        status = "failed"
        error = "service was not active"
    elif state == "stopped" and after.get("active"):
        status = "failed"
        error = "service remained active"
    if desired_enabled is not None and bool(after.get("enabled")) != bool(desired_enabled):
        status = "failed"
        error = "service enablement did not match desired state"
    finish({
        "operation": operation,
        "status": status,
        "serviceDigest": digest_bytes(name),
        "desiredState": state,
        "desiredEnabled": desired_enabled,
        "serviceManager": manager,
        "changed": changed(planned_changes),
        "changes": planned_changes,
        "before": before,
        "after": after,
        "commands": commands,
        "error": error,
    }, 0 if status == "succeeded" else 1)
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc), "commands": commands}, 1)
`
