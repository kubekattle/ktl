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

type hostPackageState struct {
	Manager   string `json:"manager,omitempty"`
	Name      string `json:"name"`
	Installed bool   `json:"installed"`
	Version   string `json:"version,omitempty"`
	Candidate string `json:"candidate,omitempty"`
}

type hostPackageChangeSet struct {
	Installed bool `json:"installed"`
	Version   bool `json:"version"`
}

type hostPackageCommandReceipt struct {
	Action        string `json:"action"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
}

type hostPackageOperationResult struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	Operation      string                      `json:"operation"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	TargetDigest   string                      `json:"targetDigest,omitempty"`
	PackageDigest  string                      `json:"packageDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredVersion string                      `json:"desiredVersion,omitempty"`
	PackageManager string                      `json:"packageManager,omitempty"`
	Changed        bool                        `json:"changed"`
	Changes        hostPackageChangeSet        `json:"changes"`
	Before         hostPackageState            `json:"before"`
	After          hostPackageState            `json:"after"`
	Commands       []hostPackageCommandReceipt `json:"commands,omitempty"`
	Error          string                      `json:"error,omitempty"`
	CompletedAt    string                      `json:"completedAt"`
}

type hostPackageObserveReceipt struct {
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
	PackageDigest    string           `json:"packageDigest,omitempty"`
	State            hostPackageState `json:"state"`
	ObservedAt       string           `json:"observedAt"`
}

type hostPackagePlanReceipt struct {
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
	PackageDigest   string               `json:"packageDigest,omitempty"`
	PackageName     string               `json:"package,omitempty"`
	PackageManager  string               `json:"packageManager,omitempty"`
	DesiredState    string               `json:"desiredState"`
	DesiredVersion  string               `json:"desiredVersion,omitempty"`
	UpdateCache     bool                 `json:"updateCache,omitempty"`
	Purge           bool                 `json:"purge,omitempty"`
	RemoveOnDelete  bool                 `json:"removeOnDelete,omitempty"`
	SelectedTargets []string             `json:"selectedTargets,omitempty"`
	LockScopes      []string             `json:"lockScopes,omitempty"`
	PolicySources   []string             `json:"policySources,omitempty"`
	Changes         hostPackageChangeSet `json:"changes"`
	PlannedAt       string               `json:"plannedAt"`
}

type hostPackageDiffReceipt struct {
	APIVersion     string               `json:"apiVersion"`
	Kind           string               `json:"kind"`
	NodeID         string               `json:"nodeId"`
	TargetID       string               `json:"targetId,omitempty"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	PackageDigest  string               `json:"packageDigest,omitempty"`
	Before         hostPackageState     `json:"before"`
	DesiredState   string               `json:"desiredState"`
	DesiredVersion string               `json:"desiredVersion,omitempty"`
	Changes        hostPackageChangeSet `json:"changes"`
	DiffQuality    string               `json:"diffQuality"`
	GeneratedAt    string               `json:"generatedAt"`
}

type hostPackageVerifyReceipt struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	NodeID         string                      `json:"nodeId"`
	TargetID       string                      `json:"targetId,omitempty"`
	Phase          string                      `json:"phase"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	PackageDigest  string                      `json:"packageDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredVersion string                      `json:"desiredVersion,omitempty"`
	ActualVersion  string                      `json:"actualVersion,omitempty"`
	Installed      bool                        `json:"installed"`
	Changed        bool                        `json:"changed"`
	Commands       []hostPackageCommandReceipt `json:"commands,omitempty"`
	VerifiedAt     string                      `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostPackageInstallNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-package"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-package"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	packageDigest := digestString(spec.PackageName)
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		observe := e.hostPackageObserveReceipt(node, phase, targetID, guardMode, selected, "", packageDigest, hostPackageState{Name: strings.TrimSpace(spec.PackageName)}, "skipped")
		plan := e.hostPackagePlanReceipt(node, phase, targetID, guardMode, selected, packageDigest, hostPackageChangeSet{}, "skipped", reason)
		diff := e.hostPackageDiffReceipt(node, phase, targetID, packageDigest, hostPackageState{Name: strings.TrimSpace(spec.PackageName)}, hostPackageChangeSet{}, "skipped")
		verify := e.hostPackageVerifyReceipt(node, phase, targetID, packageDigest, hostPackageOperationResult{Status: "skipped", Reason: reason})
		e.recordHostPackageReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	observeResult, err := e.runHostPackageOperation(ctx, transportClient, hostPackagePayload(spec, "observe"))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostPackageObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, packageDigest, observeResult.After, observeResult.Status)
	changes := hostPackageChanges(observeResult.After, spec)
	if operation == "delete" {
		changes = hostPackageChangeSet{Installed: observeResult.After.Installed}
	}
	plan := e.hostPackagePlanReceipt(node, phase, targetID, guardMode, selected, packageDigest, changes, "planned", "eligible")
	diff := e.hostPackageDiffReceipt(node, phase, targetID, packageDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostPackageInstall); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostPackageVerifyReceipt(node, phase, targetID, packageDigest, hostPackageOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostPackageReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_PACKAGE_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_PACKAGE_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host package phase %s: %w", phase, guardErr))
	}

	var result hostPackageOperationResult
	if operation == "delete" {
		result, err = e.runHostPackageOperation(ctx, transportClient, hostPackagePayload(spec, "delete"))
	} else {
		result, err = e.runHostPackageOperation(ctx, transportClient, hostPackagePayload(spec, "apply"))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.PackageDigest = packageDigest
	if result.Changes == (hostPackageChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostPackageVerifyReceipt(node, phase, targetID, packageDigest, result)
	e.recordHostPackageReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "host package operation failed")
		runErr := &RunError{Class: "HOST_PACKAGE_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_PACKAGE_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host package phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func (e *customNodeExecutor) runHostPackageOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostPackageOperationResult, error) {
	command, err := hostPackagePythonCommand(payload)
	if err != nil {
		return hostPackageOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostPackageOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostPackageOperationResult{}, fmt.Errorf("decode host.package.install receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-package-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostPackageOperationReceipt"
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

func hostPackagePayload(spec HostCommandSpec, operation string) map[string]any {
	return map[string]any{
		"operation":      strings.TrimSpace(operation),
		"package":        strings.TrimSpace(spec.PackageName),
		"packageManager": strings.TrimSpace(spec.PackageManager),
		"state":          hostPackageDesiredState(spec),
		"version":        strings.TrimSpace(spec.Version),
		"updateCache":    spec.UpdateCache,
		"purge":          spec.Purge,
		"removeOnDelete": spec.RemoveOnDelete,
	}
}

func hostPackagePythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_PACKAGE_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostPackagePythonScript + "\nPY", nil
}

func hostPackageDesiredState(spec HostCommandSpec) string {
	state := strings.ToLower(strings.TrimSpace(spec.State))
	if state == "" {
		return "present"
	}
	return state
}

func hostPackageChanges(current hostPackageState, spec HostCommandSpec) hostPackageChangeSet {
	state := hostPackageDesiredState(spec)
	version := strings.TrimSpace(spec.Version)
	switch state {
	case "absent":
		return hostPackageChangeSet{Installed: current.Installed}
	case "latest":
		return hostPackageChangeSet{
			Installed: !current.Installed,
			Version:   current.Installed && strings.TrimSpace(current.Candidate) != "" && strings.TrimSpace(current.Version) != strings.TrimSpace(current.Candidate),
		}
	default:
		return hostPackageChangeSet{
			Installed: !current.Installed,
			Version:   version != "" && strings.TrimSpace(current.Version) != version,
		}
	}
}

func (e *customNodeExecutor) hostPackageObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, packageDigest string, state hostPackageState, status string) hostPackageObserveReceipt {
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostPackageObserveReceipt{
		APIVersion:       "torque.dev/host-package-node/v1",
		Kind:             "HostPackageObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           firstNonEmptyString(strings.TrimSpace(status), "observed"),
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		TargetDigest:     strings.TrimSpace(targetDigest),
		PackageDigest:    strings.TrimSpace(packageDigest),
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostPackagePlanReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, packageDigest string, changes hostPackageChangeSet, status string, reason string) hostPackagePlanReceipt {
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
	return hostPackagePlanReceipt{
		APIVersion:      "torque.dev/host-package-node/v1",
		Kind:            "HostPackagePlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostPackageInstall,
		PackageDigest:   packageDigest,
		PackageName:     strings.TrimSpace(node.Host.PackageName),
		PackageManager:  strings.TrimSpace(node.Host.PackageManager),
		DesiredState:    hostPackageDesiredState(node.Host),
		DesiredVersion:  strings.TrimSpace(node.Host.Version),
		UpdateCache:     node.Host.UpdateCache,
		Purge:           node.Host.Purge,
		RemoveOnDelete:  node.Host.RemoveOnDelete,
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		Changes:         changes,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostPackageDiffReceipt(node *runNode, phase string, targetID string, packageDigest string, before hostPackageState, changes hostPackageChangeSet, status string) hostPackageDiffReceipt {
	return hostPackageDiffReceipt{
		APIVersion:     "torque.dev/host-package-node/v1",
		Kind:           "HostPackageDiffReceipt",
		NodeID:         node.ID,
		TargetID:       targetID,
		Phase:          phase,
		Status:         status,
		PackageDigest:  packageDigest,
		Before:         before,
		DesiredState:   hostPackageDesiredState(node.Host),
		DesiredVersion: strings.TrimSpace(node.Host.Version),
		Changes:        changes,
		DiffQuality:    "exact",
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostPackageVerifyReceipt(node *runNode, phase string, targetID string, packageDigest string, result hostPackageOperationResult) hostPackageVerifyReceipt {
	status := "succeeded"
	reason := "package receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Reason, "package receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "package operation skipped")
	}
	desiredState := firstNonEmptyString(strings.TrimSpace(result.DesiredState), hostPackageDesiredState(node.Host))
	desiredVersion := firstNonEmptyString(strings.TrimSpace(result.DesiredVersion), strings.TrimSpace(node.Host.Version))
	if status == "succeeded" {
		switch desiredState {
		case "absent":
			if result.After.Installed {
				status = "failed"
				reason = "package remained installed"
			}
		default:
			if strings.TrimSpace(result.Operation) == "apply" && !result.After.Installed {
				status = "failed"
				reason = "package was not installed"
			}
			if strings.TrimSpace(result.Operation) == "apply" && desiredVersion != "" && strings.TrimSpace(result.After.Version) != desiredVersion {
				status = "failed"
				reason = "package version did not match desired version"
			}
		}
	}
	return hostPackageVerifyReceipt{
		APIVersion:     "torque.dev/host-package-node/v1",
		Kind:           "HostPackageVerifyReceipt",
		NodeID:         node.ID,
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         status,
		Reason:         reason,
		PackageDigest:  packageDigest,
		DesiredState:   desiredState,
		DesiredVersion: desiredVersion,
		ActualVersion:  result.After.Version,
		Installed:      result.After.Installed,
		Changed:        result.Changed,
		Commands:       append([]hostPackageCommandReceipt(nil), result.Commands...),
		VerifiedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostPackageReceipts(node *runNode, phase string, status string, reason string, observe hostPackageObserveReceipt, plan hostPackagePlanReceipt, diff hostPackageDiffReceipt, apply *hostPackageOperationResult, verify hostPackageVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-package-node/v1",
		"kind":       "HostPackageNodeArtifact",
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
	e.run.RecordJSONArtifact(node.ID, "host-package-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-package-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-package-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-package-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-package-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostPackagePythonScript = `
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
    if requested in ("apt", "apt-get"):
        return ("apt", "apt-get")
    if requested == "dnf":
        return ("dnf", "dnf")
    if requested == "yum":
        return ("yum", "yum")
    if requested == "apk":
        return ("apk", "apk")
    if have("dpkg-query") and have("apt-get"):
        return ("apt", "apt-get")
    if have("rpm") and have("dnf"):
        return ("dnf", "dnf")
    if have("rpm") and have("yum"):
        return ("yum", "yum")
    if have("apk"):
        return ("apk", "apk")
    raise RuntimeError("unsupported package manager")

def apt_candidate(name):
    if not have("apt-cache"):
        return ""
    proc = run("candidate", ["apt-cache", "policy", name])
    if proc.returncode != 0:
        return ""
    for line in proc.stdout.splitlines():
        line = line.strip()
        if line.startswith("Candidate:"):
            value = line.split(":", 1)[1].strip()
            if value and value != "(none)":
                return value
    return ""

def observe_apt(name, command):
    proc = run("query", ["dpkg-query", "-W", "-f=${Status}\t${Version}", "--", name])
    installed = False
    version = ""
    if proc.returncode == 0:
        parts = proc.stdout.strip().split("\t", 1)
        installed = bool(parts and parts[0] == "install ok installed")
        if installed and len(parts) > 1:
            version = parts[1].strip()
    return {
        "manager": "apt",
        "name": name,
        "installed": installed,
        "version": version,
        "candidate": apt_candidate(name),
    }

def observe_rpm(name, manager):
    proc = run("query", ["rpm", "-q", "--qf", "%{VERSION}-%{RELEASE}", name])
    return {
        "manager": manager,
        "name": name,
        "installed": proc.returncode == 0,
        "version": proc.stdout.strip() if proc.returncode == 0 else "",
        "candidate": "",
    }

def observe_apk(name):
    proc = run("query", ["apk", "info", "-e", name])
    installed = proc.returncode == 0
    version = ""
    if installed:
        version_proc = run("query-version", ["apk", "info", "-v", name])
        if version_proc.returncode == 0:
            version = version_proc.stdout.strip().splitlines()[0] if version_proc.stdout.strip() else ""
    return {
        "manager": "apk",
        "name": name,
        "installed": installed,
        "version": version,
        "candidate": "",
    }

def observe(name, manager, command):
    if manager == "apt":
        return observe_apt(name, command)
    if manager in ("dnf", "yum"):
        return observe_rpm(name, manager)
    if manager == "apk":
        return observe_apk(name)
    raise RuntimeError("unsupported package manager: " + manager)

def install_args(manager, command, name, state, version, installed):
    if manager == "apt":
        pkg = name + "=" + version if version else name
        if state == "latest" and installed:
            return [command, "install", "-y", "--only-upgrade", pkg]
        return [command, "install", "-y", pkg]
    if manager in ("dnf", "yum"):
        pkg = name + "-" + version if version else name
        if state == "latest" and installed:
            return [command, "upgrade", "-y", pkg]
        return [command, "install", "-y", pkg]
    if manager == "apk":
        pkg = name + "=" + version if version else name
        if state == "latest":
            return [command, "add", "-u", pkg]
        return [command, "add", pkg]
    raise RuntimeError("unsupported package manager: " + manager)

def remove_args(manager, command, name, purge):
    if manager == "apt":
        return [command, "purge" if purge else "remove", "-y", name]
    if manager in ("dnf", "yum"):
        return [command, "remove", "-y", name]
    if manager == "apk":
        return [command, "del", name]
    raise RuntimeError("unsupported package manager: " + manager)

def update_cache(manager, command):
    if manager == "apt":
        return run("update-cache", [command, "update"])
    if manager == "dnf":
        return run("update-cache", [command, "makecache", "-y"])
    if manager == "yum":
        return run("update-cache", [command, "makecache", "-y"])
    if manager == "apk":
        return run("update-cache", [command, "update"])
    raise RuntimeError("unsupported package manager: " + manager)

def changes_for(state, version, before):
    if state == "absent":
        return {"installed": bool(before.get("installed")), "version": False}
    if state == "latest":
        return {
            "installed": not bool(before.get("installed")),
            "version": bool(before.get("installed")) and bool(before.get("candidate")) and before.get("version") != before.get("candidate"),
        }
    return {
        "installed": not bool(before.get("installed")),
        "version": bool(version) and before.get("version") != version,
    }

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-package-node/v1")
    doc.setdefault("kind", "HostPackageOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_PACKAGE_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    name = str(payload.get("package") or "").strip()
    requested_manager = str(payload.get("packageManager") or "").strip()
    state = str(payload.get("state") or "present").strip().lower()
    version = str(payload.get("version") or "").strip()
    update_cache_enabled = bool(payload.get("updateCache"))
    purge = bool(payload.get("purge"))
    remove_on_delete = bool(payload.get("removeOnDelete"))
    if not name:
        finish({"operation": operation, "status": "failed", "error": "package is required"}, 1)
    if state not in ("present", "latest", "absent"):
        finish({"operation": operation, "status": "failed", "error": "unsupported package state"}, 1)
    manager, command = normalize_manager(requested_manager)
    before = observe(name, manager, command)
    desired_for_operation = "absent" if operation == "delete" else state
    planned_changes = changes_for(desired_for_operation, version, before)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "packageDigest": digest_bytes(name),
            "desiredState": state,
            "desiredVersion": version,
            "packageManager": manager,
            "changed": False,
            "changes": {"installed": False, "version": False},
            "before": before,
            "after": before,
            "commands": commands,
        })
    if operation == "delete":
        if not remove_on_delete:
            finish({
                "operation": operation,
                "status": "skipped",
                "reason": "removeOnDelete is false",
                "packageDigest": digest_bytes(name),
                "desiredState": "absent",
                "desiredVersion": version,
                "packageManager": manager,
                "changed": False,
                "changes": {"installed": False, "version": False},
                "before": before,
                "after": before,
                "commands": commands,
            })
        if before.get("installed"):
            proc = run("remove", remove_args(manager, command, name, purge))
            if proc.returncode != 0:
                after = observe(name, manager, command)
                finish({
                    "operation": operation,
                    "status": "failed",
                    "packageDigest": digest_bytes(name),
                    "desiredState": "absent",
                    "desiredVersion": version,
                    "packageManager": manager,
                    "changed": False,
                    "changes": planned_changes,
                    "before": before,
                    "after": after,
                    "commands": commands,
                    "error": "package remove failed",
                }, 1)
        after = observe(name, manager, command)
        finish({
            "operation": operation,
            "status": "succeeded",
            "packageDigest": digest_bytes(name),
            "desiredState": "absent",
            "desiredVersion": version,
            "packageManager": manager,
            "changed": bool(before.get("installed")),
            "changes": planned_changes,
            "before": before,
            "after": after,
            "commands": commands,
        })
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if update_cache_enabled and (planned_changes.get("installed") or planned_changes.get("version")):
        proc = update_cache(manager, command)
        if proc.returncode != 0:
            finish({
                "operation": operation,
                "status": "failed",
                "packageDigest": digest_bytes(name),
                "desiredState": state,
                "desiredVersion": version,
                "packageManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": before,
                "commands": commands,
                "error": "package cache update failed",
            }, 1)
    if state == "absent":
        if before.get("installed"):
            proc = run("remove", remove_args(manager, command, name, purge))
            if proc.returncode != 0:
                after = observe(name, manager, command)
                finish({
                    "operation": operation,
                    "status": "failed",
                    "packageDigest": digest_bytes(name),
                    "desiredState": state,
                    "desiredVersion": version,
                    "packageManager": manager,
                    "changed": False,
                    "changes": planned_changes,
                    "before": before,
                    "after": after,
                    "commands": commands,
                    "error": "package remove failed",
                }, 1)
    elif planned_changes.get("installed") or planned_changes.get("version"):
        proc = run("install", install_args(manager, command, name, state, version, bool(before.get("installed"))))
        if proc.returncode != 0:
            after = observe(name, manager, command)
            finish({
                "operation": operation,
                "status": "failed",
                "packageDigest": digest_bytes(name),
                "desiredState": state,
                "desiredVersion": version,
                "packageManager": manager,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": "package install failed",
            }, 1)
    after = observe(name, manager, command)
    status = "succeeded"
    error = ""
    if state == "absent" and after.get("installed"):
        status = "failed"
        error = "package remained installed"
    elif state in ("present", "latest") and not after.get("installed"):
        status = "failed"
        error = "package was not installed"
    elif state == "present" and version and after.get("version") != version:
        status = "failed"
        error = "package version did not match desired version"
    finish({
        "operation": operation,
        "status": status,
        "packageDigest": digest_bytes(name),
        "desiredState": state,
        "desiredVersion": version,
        "packageManager": manager,
        "changed": bool(planned_changes.get("installed") or planned_changes.get("version")),
        "changes": planned_changes,
        "before": before,
        "after": after,
        "commands": commands,
        "error": error,
    }, 0 if status == "succeeded" else 1)
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc), "commands": commands}, 1)
`
