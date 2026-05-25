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

type hostCronState struct {
	Exists bool   `json:"exists"`
	Path   string `json:"path"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Mode   string `json:"mode,omitempty"`
	UID    int    `json:"uid,omitempty"`
	GID    int    `json:"gid,omitempty"`
}

type hostCronChangeSet struct {
	Exists  bool `json:"exists"`
	Content bool `json:"content"`
	Mode    bool `json:"mode,omitempty"`
	Owner   bool `json:"owner,omitempty"`
	Group   bool `json:"group,omitempty"`
}

type hostCronOperationResult struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	Operation     string            `json:"operation"`
	Status        string            `json:"status"`
	Reason        string            `json:"reason,omitempty"`
	TargetDigest  string            `json:"targetDigest,omitempty"`
	PathDigest    string            `json:"pathDigest,omitempty"`
	NameDigest    string            `json:"nameDigest,omitempty"`
	CommandDigest string            `json:"commandDigest,omitempty"`
	DesiredState  string            `json:"desiredState,omitempty"`
	DesiredDigest string            `json:"desiredDigest,omitempty"`
	Changed       bool              `json:"changed"`
	Changes       hostCronChangeSet `json:"changes"`
	Before        hostCronState     `json:"before"`
	After         hostCronState     `json:"after"`
	Error         string            `json:"error,omitempty"`
	CompletedAt   string            `json:"completedAt"`
}

type hostCronObserveReceipt struct {
	APIVersion       string        `json:"apiVersion"`
	Kind             string        `json:"kind"`
	NodeID           string        `json:"nodeId"`
	NodeKind         string        `json:"nodeKind"`
	TargetID         string        `json:"targetId,omitempty"`
	Phase            string        `json:"phase"`
	Status           string        `json:"status"`
	GuardMode        string        `json:"guardMode"`
	SelectedTargetID string        `json:"selectedTargetId,omitempty"`
	SelectedTargets  []string      `json:"selectedTargets,omitempty"`
	TargetDigest     string        `json:"targetDigest,omitempty"`
	PathDigest       string        `json:"pathDigest,omitempty"`
	NameDigest       string        `json:"nameDigest,omitempty"`
	State            hostCronState `json:"state"`
	ObservedAt       string        `json:"observedAt"`
}

type hostCronPlanReceipt struct {
	APIVersion      string            `json:"apiVersion"`
	Kind            string            `json:"kind"`
	NodeID          string            `json:"nodeId"`
	NodeKind        string            `json:"nodeKind"`
	TargetID        string            `json:"targetId,omitempty"`
	Phase           string            `json:"phase"`
	Status          string            `json:"status"`
	Reason          string            `json:"reason,omitempty"`
	GuardMode       string            `json:"guardMode"`
	Operation       string            `json:"operation"`
	PathDigest      string            `json:"pathDigest,omitempty"`
	NameDigest      string            `json:"nameDigest,omitempty"`
	CommandDigest   string            `json:"commandDigest,omitempty"`
	DesiredState    string            `json:"desiredState"`
	DesiredDigest   string            `json:"desiredDigest,omitempty"`
	ScheduleDigest  string            `json:"scheduleDigest,omitempty"`
	CronUser        string            `json:"cronUser,omitempty"`
	Mode            string            `json:"mode,omitempty"`
	Owner           string            `json:"owner,omitempty"`
	Group           string            `json:"group,omitempty"`
	RemoveOnDelete  bool              `json:"removeOnDelete,omitempty"`
	SelectedTargets []string          `json:"selectedTargets,omitempty"`
	LockScopes      []string          `json:"lockScopes,omitempty"`
	PolicySources   []string          `json:"policySources,omitempty"`
	Changes         hostCronChangeSet `json:"changes"`
	PlannedAt       string            `json:"plannedAt"`
}

type hostCronDiffReceipt struct {
	APIVersion    string            `json:"apiVersion"`
	Kind          string            `json:"kind"`
	NodeID        string            `json:"nodeId"`
	TargetID      string            `json:"targetId,omitempty"`
	Phase         string            `json:"phase"`
	Status        string            `json:"status"`
	PathDigest    string            `json:"pathDigest,omitempty"`
	NameDigest    string            `json:"nameDigest,omitempty"`
	CommandDigest string            `json:"commandDigest,omitempty"`
	Before        hostCronState     `json:"before"`
	DesiredState  string            `json:"desiredState"`
	DesiredDigest string            `json:"desiredDigest,omitempty"`
	Changes       hostCronChangeSet `json:"changes"`
	DiffQuality   string            `json:"diffQuality"`
	GeneratedAt   string            `json:"generatedAt"`
}

type hostCronVerifyReceipt struct {
	APIVersion    string `json:"apiVersion"`
	Kind          string `json:"kind"`
	NodeID        string `json:"nodeId"`
	TargetID      string `json:"targetId,omitempty"`
	Phase         string `json:"phase"`
	Status        string `json:"status"`
	Reason        string `json:"reason,omitempty"`
	PathDigest    string `json:"pathDigest,omitempty"`
	NameDigest    string `json:"nameDigest,omitempty"`
	CommandDigest string `json:"commandDigest,omitempty"`
	DesiredState  string `json:"desiredState,omitempty"`
	DesiredDigest string `json:"desiredDigest,omitempty"`
	Exists        bool   `json:"exists"`
	SHA256        string `json:"sha256,omitempty"`
	Changed       bool   `json:"changed"`
	VerifiedAt    string `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostCronManageNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-cron"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-cron"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	pathDigest := digestString(spec.Path)
	nameDigest := digestString(spec.CronName)
	commandDigest := digestString(spec.CronCommand)
	desiredDigest := ""
	if hostCronDesiredState(spec) == "present" {
		desiredDigest = digestBytes([]byte(hostCronDesiredContent(spec)))
	}
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		state := hostCronState{Path: strings.TrimSpace(spec.Path)}
		observe := e.hostCronObserveReceipt(node, phase, targetID, guardMode, selected, "", pathDigest, nameDigest, state, "skipped")
		plan := e.hostCronPlanReceipt(node, phase, targetID, guardMode, selected, pathDigest, nameDigest, commandDigest, desiredDigest, hostCronChangeSet{}, "skipped", reason)
		diff := e.hostCronDiffReceipt(node, phase, targetID, pathDigest, nameDigest, commandDigest, desiredDigest, state, hostCronChangeSet{}, "skipped")
		verify := e.hostCronVerifyReceipt(node, phase, targetID, pathDigest, nameDigest, commandDigest, desiredDigest, hostCronOperationResult{Status: "skipped", Reason: reason})
		e.recordHostCronReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	observeResult, err := e.runHostCronOperation(ctx, transportClient, hostCronPayload(spec, "observe"))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostCronObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, pathDigest, nameDigest, observeResult.After, observeResult.Status)
	changes := hostCronChanges(observeResult.After, spec, operation)
	plan := e.hostCronPlanReceipt(node, phase, targetID, guardMode, selected, pathDigest, nameDigest, commandDigest, desiredDigest, changes, "planned", "eligible")
	diff := e.hostCronDiffReceipt(node, phase, targetID, pathDigest, nameDigest, commandDigest, desiredDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostCronManage); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostCronVerifyReceipt(node, phase, targetID, pathDigest, nameDigest, commandDigest, desiredDigest, hostCronOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostCronReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_CRON_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_CRON_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host cron phase %s: %w", phase, guardErr))
	}

	var result hostCronOperationResult
	if operation == "delete" {
		result, err = e.runHostCronOperation(ctx, transportClient, hostCronPayload(spec, "delete"))
	} else {
		result, err = e.runHostCronOperation(ctx, transportClient, hostCronPayload(spec, "apply"))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.PathDigest = pathDigest
	result.NameDigest = nameDigest
	result.CommandDigest = commandDigest
	if result.DesiredDigest == "" {
		result.DesiredDigest = desiredDigest
	}
	if result.Changes == (hostCronChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostCronVerifyReceipt(node, phase, targetID, pathDigest, nameDigest, commandDigest, desiredDigest, result)
	e.recordHostCronReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "host cron operation failed")
		runErr := &RunError{Class: "HOST_CRON_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_CRON_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host cron phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func (e *customNodeExecutor) runHostCronOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostCronOperationResult, error) {
	command, err := hostCronPythonCommand(payload)
	if err != nil {
		return hostCronOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostCronOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostCronOperationResult{}, fmt.Errorf("decode host.cron.manage receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-cron-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostCronOperationReceipt"
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

func hostCronPayload(spec HostCommandSpec, operation string) map[string]any {
	return map[string]any{
		"operation":      strings.TrimSpace(operation),
		"path":           strings.TrimSpace(spec.Path),
		"cronName":       strings.TrimSpace(spec.CronName),
		"schedule":       strings.TrimSpace(spec.CronSchedule),
		"cronUser":       firstNonEmptyString(strings.TrimSpace(spec.CronUser), "root"),
		"cronCommand":    strings.TrimSpace(spec.CronCommand),
		"state":          hostCronDesiredState(spec),
		"mode":           firstNonEmptyString(strings.TrimSpace(spec.Mode), "0644"),
		"owner":          strings.TrimSpace(spec.Owner),
		"group":          strings.TrimSpace(spec.Group),
		"removeOnDelete": spec.RemoveOnDelete,
	}
}

func hostCronPythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_CRON_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostCronPythonScript + "\nPY", nil
}

func hostCronDesiredState(spec HostCommandSpec) string {
	state := strings.ToLower(strings.TrimSpace(spec.State))
	if state == "" {
		return "present"
	}
	return state
}

func hostCronDesiredContent(spec HostCommandSpec) string {
	name := strings.TrimSpace(spec.CronName)
	if name == "" {
		name = "torque-managed"
	}
	schedule := strings.TrimSpace(spec.CronSchedule)
	cronUser := firstNonEmptyString(strings.TrimSpace(spec.CronUser), "root")
	command := strings.TrimSpace(spec.CronCommand)
	return "# torque managed: " + name + "\n" + schedule + " " + cronUser + " " + command + "\n"
}

func hostCronChanges(current hostCronState, spec HostCommandSpec, operation string) hostCronChangeSet {
	if operation == "delete" {
		return hostCronChangeSet{Exists: spec.RemoveOnDelete && current.Exists}
	}
	if hostCronDesiredState(spec) == "absent" {
		return hostCronChangeSet{Exists: current.Exists}
	}
	desiredDigest := digestBytes([]byte(hostCronDesiredContent(spec)))
	desiredMode := firstNonEmptyString(strings.TrimSpace(spec.Mode), "0644")
	return hostCronChangeSet{
		Exists:  !current.Exists,
		Content: current.SHA256 != "" && desiredDigest != "" && current.SHA256 != desiredDigest,
		Mode:    current.Exists && desiredMode != "" && current.Mode != "" && current.Mode != desiredMode,
	}
}

func (e *customNodeExecutor) hostCronObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, pathDigest string, nameDigest string, state hostCronState, status string) hostCronObserveReceipt {
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostCronObserveReceipt{
		APIVersion:       "torque.dev/host-cron-node/v1",
		Kind:             "HostCronObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           firstNonEmptyString(strings.TrimSpace(status), "observed"),
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		TargetDigest:     strings.TrimSpace(targetDigest),
		PathDigest:       strings.TrimSpace(pathDigest),
		NameDigest:       strings.TrimSpace(nameDigest),
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostCronPlanReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, pathDigest string, nameDigest string, commandDigest string, desiredDigest string, changes hostCronChangeSet, status string, reason string) hostCronPlanReceipt {
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
	return hostCronPlanReceipt{
		APIVersion:      "torque.dev/host-cron-node/v1",
		Kind:            "HostCronPlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostCronManage,
		PathDigest:      pathDigest,
		NameDigest:      nameDigest,
		CommandDigest:   commandDigest,
		DesiredState:    hostCronDesiredState(node.Host),
		DesiredDigest:   desiredDigest,
		ScheduleDigest:  digestString(node.Host.CronSchedule),
		CronUser:        firstNonEmptyString(strings.TrimSpace(node.Host.CronUser), "root"),
		Mode:            firstNonEmptyString(strings.TrimSpace(node.Host.Mode), "0644"),
		Owner:           strings.TrimSpace(node.Host.Owner),
		Group:           strings.TrimSpace(node.Host.Group),
		RemoveOnDelete:  node.Host.RemoveOnDelete,
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		Changes:         changes,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostCronDiffReceipt(node *runNode, phase string, targetID string, pathDigest string, nameDigest string, commandDigest string, desiredDigest string, before hostCronState, changes hostCronChangeSet, status string) hostCronDiffReceipt {
	return hostCronDiffReceipt{
		APIVersion:    "torque.dev/host-cron-node/v1",
		Kind:          "HostCronDiffReceipt",
		NodeID:        node.ID,
		TargetID:      targetID,
		Phase:         phase,
		Status:        status,
		PathDigest:    pathDigest,
		NameDigest:    nameDigest,
		CommandDigest: commandDigest,
		Before:        before,
		DesiredState:  hostCronDesiredState(node.Host),
		DesiredDigest: desiredDigest,
		Changes:       changes,
		DiffQuality:   "exact",
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostCronVerifyReceipt(node *runNode, phase string, targetID string, pathDigest string, nameDigest string, commandDigest string, desiredDigest string, result hostCronOperationResult) hostCronVerifyReceipt {
	status := "succeeded"
	reason := "cron receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Reason, "cron receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "cron operation skipped")
	}
	desiredState := firstNonEmptyString(strings.TrimSpace(result.DesiredState), hostCronDesiredState(node.Host))
	if status == "succeeded" {
		if desiredState == "absent" {
			if result.After.Exists {
				status = "failed"
				reason = "cron entry remained present"
			}
		} else if !result.After.Exists {
			status = "failed"
			reason = "cron entry was not present"
		} else if desiredDigest != "" && result.After.SHA256 != desiredDigest {
			status = "failed"
			reason = "cron entry content did not match desired digest"
		}
	}
	return hostCronVerifyReceipt{
		APIVersion:    "torque.dev/host-cron-node/v1",
		Kind:          "HostCronVerifyReceipt",
		NodeID:        node.ID,
		TargetID:      strings.TrimSpace(targetID),
		Phase:         phase,
		Status:        status,
		Reason:        reason,
		PathDigest:    pathDigest,
		NameDigest:    nameDigest,
		CommandDigest: commandDigest,
		DesiredState:  desiredState,
		DesiredDigest: desiredDigest,
		Exists:        result.After.Exists,
		SHA256:        result.After.SHA256,
		Changed:       result.Changed,
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostCronReceipts(node *runNode, phase string, status string, reason string, observe hostCronObserveReceipt, plan hostCronPlanReceipt, diff hostCronDiffReceipt, apply *hostCronOperationResult, verify hostCronVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-cron-node/v1",
		"kind":       "HostCronNodeArtifact",
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
	e.run.RecordJSONArtifact(node.ID, "host-cron-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-cron-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-cron-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-cron-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-cron-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostCronPythonScript = `
import base64
import grp
import hashlib
import json
import os
import pwd
import stat
import tempfile
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    if isinstance(data, str):
        data = data.encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()

def normalize_mode(value):
    value = str(value or "").strip()
    if not value:
        return "0644"
    if value.startswith("0o"):
        value = value[2:]
    return format(int(value[-4:], 8), "04o")

def desired_content(payload):
    name = str(payload.get("cronName") or "torque-managed").strip() or "torque-managed"
    schedule = str(payload.get("schedule") or "").strip()
    cron_user = str(payload.get("cronUser") or "root").strip() or "root"
    command = str(payload.get("cronCommand") or "").strip()
    return "# torque managed: " + name + "\n" + schedule + " " + cron_user + " " + command + "\n"

def observe(path):
    path = str(path or "").strip()
    doc = {"exists": False, "path": path}
    if not path or not os.path.lexists(path):
        return doc
    st = os.lstat(path)
    doc.update({
        "exists": True,
        "mode": format(stat.S_IMODE(st.st_mode), "04o"),
        "uid": int(st.st_uid),
        "gid": int(st.st_gid),
    })
    if stat.S_ISREG(st.st_mode):
        with open(path, "rb") as fh:
            data = fh.read()
        doc["sha256"] = digest_bytes(data)
        doc["size"] = len(data)
    return doc

def chown_if_requested(path, owner, group):
    uid = -1
    gid = -1
    owner = str(owner or "").strip()
    group = str(group or "").strip()
    if owner:
        uid = pwd.getpwnam(owner).pw_uid if not owner.isdigit() else int(owner)
    if group:
        gid = grp.getgrnam(group).gr_gid if not group.isdigit() else int(group)
    if uid != -1 or gid != -1:
        os.chown(path, uid, gid)

def changes_for(before, desired_digest, mode, operation, remove_on_delete):
    if operation == "delete":
        return {"exists": bool(remove_on_delete and before.get("exists")), "content": False}
    if operation == "absent":
        return {"exists": bool(before.get("exists")), "content": False}
    return {
        "exists": not bool(before.get("exists")),
        "content": bool(before.get("exists")) and before.get("sha256") != desired_digest,
        "mode": bool(before.get("exists")) and bool(before.get("mode")) and before.get("mode") != mode,
    }

def write_file(path, content, mode, owner, group):
    directory = os.path.dirname(path) or "."
    os.makedirs(directory, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".torque-cron-", dir=directory)
    try:
        with os.fdopen(fd, "w", encoding="utf-8") as fh:
            fh.write(content)
        os.chmod(tmp, int(mode, 8))
        chown_if_requested(tmp, owner, group)
        os.replace(tmp, path)
    finally:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-cron-node/v1")
    doc.setdefault("kind", "HostCronOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_CRON_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    path = str(payload.get("path") or "").strip()
    state = str(payload.get("state") or "present").strip().lower()
    mode = normalize_mode(payload.get("mode"))
    owner = str(payload.get("owner") or "").strip()
    group = str(payload.get("group") or "").strip()
    remove_on_delete = bool(payload.get("removeOnDelete"))
    if not path:
        finish({"operation": operation, "status": "failed", "error": "path is required"}, 1)
    if state not in ("present", "absent"):
        finish({"operation": operation, "status": "failed", "error": "unsupported cron state"}, 1)
    if state == "present" and operation != "delete":
        if not str(payload.get("schedule") or "").strip():
            finish({"operation": operation, "status": "failed", "error": "schedule is required"}, 1)
        if not str(payload.get("cronCommand") or "").strip():
            finish({"operation": operation, "status": "failed", "error": "cronCommand is required"}, 1)
    before = observe(path)
    content = desired_content(payload) if state == "present" else ""
    desired_digest = digest_bytes(content) if content else ""
    desired_for_operation = "absent" if operation == "delete" else state
    planned_changes = changes_for(before, desired_digest, "absent" if desired_for_operation == "absent" else mode, "delete" if operation == "delete" else desired_for_operation, remove_on_delete)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "pathDigest": digest_bytes(path),
            "nameDigest": digest_bytes(str(payload.get("cronName") or "")),
            "commandDigest": digest_bytes(str(payload.get("cronCommand") or "")),
            "desiredState": state,
            "desiredDigest": desired_digest,
            "changed": False,
            "changes": {"exists": False, "content": False},
            "before": before,
            "after": before,
        })
    if operation == "delete":
        if not remove_on_delete:
            finish({
                "operation": operation,
                "status": "skipped",
                "reason": "removeOnDelete is false",
                "pathDigest": digest_bytes(path),
                "nameDigest": digest_bytes(str(payload.get("cronName") or "")),
                "commandDigest": digest_bytes(str(payload.get("cronCommand") or "")),
                "desiredState": "absent",
                "changed": False,
                "changes": {"exists": False, "content": False},
                "before": before,
                "after": before,
            })
        if before.get("exists"):
            os.unlink(path)
        after = observe(path)
        status = "succeeded"
        error = ""
        if after.get("exists"):
            status = "failed"
            error = "cron entry remained present"
        finish({
            "operation": operation,
            "status": status,
            "pathDigest": digest_bytes(path),
            "nameDigest": digest_bytes(str(payload.get("cronName") or "")),
            "commandDigest": digest_bytes(str(payload.get("cronCommand") or "")),
            "desiredState": "absent",
            "changed": bool(before.get("exists")),
            "changes": planned_changes,
            "before": before,
            "after": after,
            "error": error,
        }, 0 if status == "succeeded" else 1)
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if state == "absent":
        if before.get("exists"):
            os.unlink(path)
    else:
        if before.get("sha256") != desired_digest or before.get("mode") != mode:
            write_file(path, content, mode, owner, group)
    after = observe(path)
    status = "succeeded"
    error = ""
    if state == "absent" and after.get("exists"):
        status = "failed"
        error = "cron entry remained present"
    elif state == "present" and not after.get("exists"):
        status = "failed"
        error = "cron entry was not present"
    elif state == "present" and after.get("sha256") != desired_digest:
        status = "failed"
        error = "cron entry content did not match desired digest"
    finish({
        "operation": operation,
        "status": status,
        "pathDigest": digest_bytes(path),
        "nameDigest": digest_bytes(str(payload.get("cronName") or "")),
        "commandDigest": digest_bytes(str(payload.get("cronCommand") or "")),
        "desiredState": state,
        "desiredDigest": desired_digest,
        "changed": bool(planned_changes.get("exists") or planned_changes.get("content") or planned_changes.get("mode")),
        "changes": planned_changes,
        "before": before,
        "after": after,
        "error": error,
    }, 0 if status == "succeeded" else 1)
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc)}, 1)
`
