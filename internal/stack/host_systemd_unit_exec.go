package stack

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

type hostSystemdFileState struct {
	Exists bool   `json:"exists"`
	Type   string `json:"type,omitempty"`
	Path   string `json:"path,omitempty"`
	SHA256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Group  string `json:"group,omitempty"`
	UID    int    `json:"uid,omitempty"`
	GID    int    `json:"gid,omitempty"`
}

type hostSystemdRuntimeState struct {
	Exists        bool   `json:"exists"`
	Unit          string `json:"unit"`
	Active        bool   `json:"active"`
	Enabled       bool   `json:"enabled"`
	LoadState     string `json:"loadState,omitempty"`
	ActiveState   string `json:"activeState,omitempty"`
	SubState      string `json:"subState,omitempty"`
	UnitFileState string `json:"unitFileState,omitempty"`
}

type hostSystemdJournalEvidence struct {
	Status        string `json:"status"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
	LineCount     int    `json:"lineCount,omitempty"`
	ExitCode      int    `json:"exitCode,omitempty"`
	CollectedAt   string `json:"collectedAt,omitempty"`
	Reason        string `json:"reason,omitempty"`
}

type hostSystemdState struct {
	Unit    string                      `json:"unit"`
	Path    string                      `json:"path"`
	File    hostSystemdFileState        `json:"file"`
	Runtime hostSystemdRuntimeState     `json:"runtime"`
	Journal *hostSystemdJournalEvidence `json:"journal,omitempty"`
}

type hostSystemdChangeSet struct {
	Content      bool `json:"content"`
	Mode         bool `json:"mode,omitempty"`
	Owner        bool `json:"owner,omitempty"`
	Group        bool `json:"group,omitempty"`
	DaemonReload bool `json:"daemonReload,omitempty"`
	Active       bool `json:"active,omitempty"`
	Enabled      bool `json:"enabled,omitempty"`
	Restart      bool `json:"restart,omitempty"`
}

type hostSystemdCommandReceipt struct {
	Action        string `json:"action"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
}

type hostSystemdOperationResult struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	Operation      string                      `json:"operation"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	TargetDigest   string                      `json:"targetDigest,omitempty"`
	UnitDigest     string                      `json:"unitDigest,omitempty"`
	PathDigest     string                      `json:"pathDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredDigest  string                      `json:"desiredDigest,omitempty"`
	DesiredEnabled *bool                       `json:"desiredEnabled,omitempty"`
	Changed        bool                        `json:"changed"`
	Changes        hostSystemdChangeSet        `json:"changes"`
	Before         hostSystemdState            `json:"before"`
	After          hostSystemdState            `json:"after"`
	Commands       []hostSystemdCommandReceipt `json:"commands,omitempty"`
	Journal        *hostSystemdJournalEvidence `json:"journal,omitempty"`
	Error          string                      `json:"error,omitempty"`
	CompletedAt    string                      `json:"completedAt"`
}

type hostSystemdObserveReceipt struct {
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
	UnitDigest       string           `json:"unitDigest,omitempty"`
	PathDigest       string           `json:"pathDigest,omitempty"`
	State            hostSystemdState `json:"state"`
	ObservedAt       string           `json:"observedAt"`
}

type hostSystemdPlanReceipt struct {
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
	UnitDigest      string               `json:"unitDigest,omitempty"`
	PathDigest      string               `json:"pathDigest,omitempty"`
	DesiredState    string               `json:"desiredState"`
	DesiredDigest   string               `json:"desiredDigest,omitempty"`
	DesiredEnabled  *bool                `json:"desiredEnabled,omitempty"`
	Mode            string               `json:"mode,omitempty"`
	Owner           string               `json:"owner,omitempty"`
	Group           string               `json:"group,omitempty"`
	StopOnDelete    bool                 `json:"stopOnDelete,omitempty"`
	DisableOnDelete bool                 `json:"disableOnDelete,omitempty"`
	RemoveOnDelete  bool                 `json:"removeOnDelete,omitempty"`
	SelectedTargets []string             `json:"selectedTargets,omitempty"`
	LockScopes      []string             `json:"lockScopes,omitempty"`
	PolicySources   []string             `json:"policySources,omitempty"`
	Changes         hostSystemdChangeSet `json:"changes"`
	PlannedAt       string               `json:"plannedAt"`
}

type hostSystemdDiffReceipt struct {
	APIVersion     string               `json:"apiVersion"`
	Kind           string               `json:"kind"`
	NodeID         string               `json:"nodeId"`
	TargetID       string               `json:"targetId,omitempty"`
	Phase          string               `json:"phase"`
	Status         string               `json:"status"`
	UnitDigest     string               `json:"unitDigest,omitempty"`
	PathDigest     string               `json:"pathDigest,omitempty"`
	Before         hostSystemdState     `json:"before"`
	DesiredState   string               `json:"desiredState"`
	DesiredDigest  string               `json:"desiredDigest,omitempty"`
	DesiredEnabled *bool                `json:"desiredEnabled,omitempty"`
	Changes        hostSystemdChangeSet `json:"changes"`
	DiffQuality    string               `json:"diffQuality"`
	GeneratedAt    string               `json:"generatedAt"`
}

type hostSystemdVerifyReceipt struct {
	APIVersion     string                      `json:"apiVersion"`
	Kind           string                      `json:"kind"`
	NodeID         string                      `json:"nodeId"`
	TargetID       string                      `json:"targetId,omitempty"`
	Phase          string                      `json:"phase"`
	Status         string                      `json:"status"`
	Reason         string                      `json:"reason,omitempty"`
	UnitDigest     string                      `json:"unitDigest,omitempty"`
	PathDigest     string                      `json:"pathDigest,omitempty"`
	DesiredState   string                      `json:"desiredState,omitempty"`
	DesiredDigest  string                      `json:"desiredDigest,omitempty"`
	DesiredEnabled *bool                       `json:"desiredEnabled,omitempty"`
	FileExists     bool                        `json:"fileExists"`
	RuntimeExists  bool                        `json:"runtimeExists"`
	Active         bool                        `json:"active"`
	Enabled        bool                        `json:"enabled"`
	Changed        bool                        `json:"changed"`
	Commands       []hostSystemdCommandReceipt `json:"commands,omitempty"`
	Journal        *hostSystemdJournalEvidence `json:"journal,omitempty"`
	VerifiedAt     string                      `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostSystemdUnitNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-systemd"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-systemd"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	unitName := strings.TrimSpace(spec.UnitName)
	unitDigest := digestString(unitName)
	path := hostSystemdUnitPath(spec)
	pathDigest := digestString(path)
	desired := []byte(nil)
	desiredDigest := ""
	var err error
	if operation != "delete" && hostSystemdDesiredState(spec) != "absent" {
		desired, err = renderHostSystemdUnitContent(node)
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		desiredDigest = digestBytes(desired)
	}
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		state := hostSystemdState{Unit: unitName, Path: path, File: hostSystemdFileState{Path: path}, Runtime: hostSystemdRuntimeState{Unit: unitName}}
		observe := e.hostSystemdObserveReceipt(node, phase, targetID, guardMode, selected, "", unitDigest, pathDigest, state, "skipped")
		plan := e.hostSystemdPlanReceipt(node, phase, operation, targetID, guardMode, selected, unitDigest, pathDigest, desiredDigest, hostSystemdChangeSet{}, "skipped", reason)
		diff := e.hostSystemdDiffReceipt(node, phase, operation, targetID, unitDigest, pathDigest, desiredDigest, state, hostSystemdChangeSet{}, "skipped")
		verify := e.hostSystemdVerifyReceipt(node, phase, operation, targetID, unitDigest, pathDigest, desiredDigest, hostSystemdOperationResult{Status: "skipped", Reason: reason})
		e.recordHostSystemdReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	observeResult, err := e.runHostSystemdOperation(ctx, transportClient, hostSystemdPayload(spec, "observe", nil))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostSystemdObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, unitDigest, pathDigest, observeResult.After, observeResult.Status)
	changes := hostSystemdChanges(observeResult.After, spec, operation, desiredDigest)
	plan := e.hostSystemdPlanReceipt(node, phase, operation, targetID, guardMode, selected, unitDigest, pathDigest, desiredDigest, changes, "planned", "eligible")
	diff := e.hostSystemdDiffReceipt(node, phase, operation, targetID, unitDigest, pathDigest, desiredDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostSystemdUnit); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostSystemdVerifyReceipt(node, phase, operation, targetID, unitDigest, pathDigest, desiredDigest, hostSystemdOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostSystemdReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_SYSTEMD_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_SYSTEMD_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host systemd phase %s: %w", phase, guardErr))
	}

	var result hostSystemdOperationResult
	if operation == "delete" {
		result, err = e.runHostSystemdOperation(ctx, transportClient, hostSystemdPayload(spec, "delete", nil))
	} else {
		result, err = e.runHostSystemdOperation(ctx, transportClient, hostSystemdPayload(spec, "apply", desired))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.UnitDigest = unitDigest
	result.PathDigest = pathDigest
	if result.DesiredDigest == "" {
		result.DesiredDigest = desiredDigest
	}
	if result.Changes == (hostSystemdChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostSystemdVerifyReceipt(node, phase, operation, targetID, unitDigest, pathDigest, desiredDigest, result)
	e.recordHostSystemdReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "host systemd unit operation failed")
		runErr := &RunError{Class: "HOST_SYSTEMD_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_SYSTEMD_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host systemd phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func renderHostSystemdUnitContent(node *runNode) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("nil host.systemd.unit node")
	}
	spec := node.Host
	if strings.TrimSpace(spec.Content) != "" {
		return []byte(spec.Content), nil
	}
	source := spec.Template
	if strings.TrimSpace(source) == "" && strings.TrimSpace(spec.TemplatePath) != "" {
		path := spec.TemplatePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(node.Dir, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read host.systemd.unit template: %w", err)
		}
		source = string(raw)
	}
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("host.systemd.unit requires content, template, or templatePath")
	}
	tpl, err := template.New("host-systemd-unit").Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse host.systemd.unit template: %w", err)
	}
	data := map[string]any{}
	for k, v := range spec.Data {
		data[k] = v
	}
	data["NodeID"] = node.ID
	data["NodeName"] = node.Name
	data["Unit"] = strings.TrimSpace(spec.UnitName)
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render host.systemd.unit template: %w", err)
	}
	return out.Bytes(), nil
}

func (e *customNodeExecutor) runHostSystemdOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostSystemdOperationResult, error) {
	command, err := hostSystemdPythonCommand(payload)
	if err != nil {
		return hostSystemdOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostSystemdOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostSystemdOperationResult{}, fmt.Errorf("decode host.systemd.unit receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-systemd-unit-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostSystemdUnitOperationReceipt"
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

func hostSystemdPayload(spec HostCommandSpec, operation string, desired []byte) map[string]any {
	var enabled any
	if spec.Enabled != nil {
		enabled = *spec.Enabled
	}
	return map[string]any{
		"operation":       strings.TrimSpace(operation),
		"unit":            strings.TrimSpace(spec.UnitName),
		"path":            hostSystemdUnitPath(spec),
		"contentB64":      base64.StdEncoding.EncodeToString(desired),
		"mode":            firstNonEmptyString(strings.TrimSpace(spec.Mode), "0644"),
		"owner":           strings.TrimSpace(spec.Owner),
		"group":           strings.TrimSpace(spec.Group),
		"state":           hostSystemdDesiredState(spec),
		"enabled":         enabled,
		"stopOnDelete":    spec.StopOnDelete,
		"disableOnDelete": spec.DisableOnDelete,
		"removeOnDelete":  spec.RemoveOnDelete,
	}
}

func hostSystemdPythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_SYSTEMD_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostSystemdPythonScript + "\nPY", nil
}

func hostSystemdUnitPath(spec HostCommandSpec) string {
	if strings.TrimSpace(spec.Path) != "" {
		return strings.TrimSpace(spec.Path)
	}
	unit := strings.TrimSpace(spec.UnitName)
	if unit == "" {
		return ""
	}
	return "/etc/systemd/system/" + unit
}

func hostSystemdDesiredState(spec HostCommandSpec) string {
	state := strings.ToLower(strings.TrimSpace(spec.State))
	if state == "" {
		return "present"
	}
	return state
}

func hostSystemdDesiredStateForOperation(spec HostCommandSpec, operation string) string {
	if operation == "delete" {
		return "absent"
	}
	return hostSystemdDesiredState(spec)
}

func hostSystemdChanges(current hostSystemdState, spec HostCommandSpec, operation string, desiredDigest string) hostSystemdChangeSet {
	desiredState := hostSystemdDesiredStateForOperation(spec, operation)
	changes := hostSystemdChangeSet{}
	if desiredState == "absent" {
		changes.Content = current.File.Exists && (operation != "delete" || spec.RemoveOnDelete)
		changes.Active = current.Runtime.Active && (operation != "delete" || spec.StopOnDelete)
		changes.Enabled = current.Runtime.Enabled && (operation != "delete" || spec.DisableOnDelete)
		changes.DaemonReload = changes.Content
		return changes
	}
	desiredMode := firstNonEmptyString(strings.TrimSpace(spec.Mode), "0644")
	changes.Content = !current.File.Exists || (desiredDigest != "" && current.File.SHA256 != desiredDigest)
	changes.Mode = current.File.Exists && desiredMode != "" && current.File.Mode != "" && current.File.Mode != desiredMode
	owner := strings.TrimSpace(spec.Owner)
	group := strings.TrimSpace(spec.Group)
	changes.Owner = current.File.Exists && owner != "" && current.File.Owner != owner && fmt.Sprintf("%d", current.File.UID) != owner
	changes.Group = current.File.Exists && group != "" && current.File.Group != group && fmt.Sprintf("%d", current.File.GID) != group
	changes.DaemonReload = changes.Content || changes.Mode || changes.Owner || changes.Group
	switch desiredState {
	case "started":
		changes.Active = !current.Runtime.Active
	case "stopped":
		changes.Active = current.Runtime.Active
	case "restarted":
		changes.Active = true
		changes.Restart = true
	}
	if spec.Enabled != nil {
		changes.Enabled = current.Runtime.Enabled != *spec.Enabled
	}
	return changes
}

func (e *customNodeExecutor) hostSystemdObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, unitDigest string, pathDigest string, state hostSystemdState, status string) hostSystemdObserveReceipt {
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostSystemdObserveReceipt{
		APIVersion:       "torque.dev/host-systemd-unit-node/v1",
		Kind:             "HostSystemdObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           firstNonEmptyString(strings.TrimSpace(status), "observed"),
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		TargetDigest:     strings.TrimSpace(targetDigest),
		UnitDigest:       unitDigest,
		PathDigest:       pathDigest,
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostSystemdPlanReceipt(node *runNode, phase string, operation string, targetID string, guardMode string, selected []string, unitDigest string, pathDigest string, desiredDigest string, changes hostSystemdChangeSet, status string, reason string) hostSystemdPlanReceipt {
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
	return hostSystemdPlanReceipt{
		APIVersion:      "torque.dev/host-systemd-unit-node/v1",
		Kind:            "HostSystemdPlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostSystemdUnit,
		UnitDigest:      unitDigest,
		PathDigest:      pathDigest,
		DesiredState:    hostSystemdDesiredStateForOperation(node.Host, operation),
		DesiredDigest:   desiredDigest,
		DesiredEnabled:  cloneBoolPtr(node.Host.Enabled),
		Mode:            firstNonEmptyString(strings.TrimSpace(node.Host.Mode), "0644"),
		Owner:           strings.TrimSpace(node.Host.Owner),
		Group:           strings.TrimSpace(node.Host.Group),
		StopOnDelete:    node.Host.StopOnDelete,
		DisableOnDelete: node.Host.DisableOnDelete,
		RemoveOnDelete:  node.Host.RemoveOnDelete,
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		Changes:         changes,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostSystemdDiffReceipt(node *runNode, phase string, operation string, targetID string, unitDigest string, pathDigest string, desiredDigest string, before hostSystemdState, changes hostSystemdChangeSet, status string) hostSystemdDiffReceipt {
	return hostSystemdDiffReceipt{
		APIVersion:     "torque.dev/host-systemd-unit-node/v1",
		Kind:           "HostSystemdDiffReceipt",
		NodeID:         node.ID,
		TargetID:       targetID,
		Phase:          phase,
		Status:         status,
		UnitDigest:     unitDigest,
		PathDigest:     pathDigest,
		Before:         before,
		DesiredState:   hostSystemdDesiredStateForOperation(node.Host, operation),
		DesiredDigest:  desiredDigest,
		DesiredEnabled: cloneBoolPtr(node.Host.Enabled),
		Changes:        changes,
		DiffQuality:    "exact",
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostSystemdVerifyReceipt(node *runNode, phase string, operation string, targetID string, unitDigest string, pathDigest string, desiredDigest string, result hostSystemdOperationResult) hostSystemdVerifyReceipt {
	status := "succeeded"
	reason := "systemd unit receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Reason, "systemd unit receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "systemd unit operation skipped")
	}
	desiredState := firstNonEmptyString(strings.TrimSpace(result.DesiredState), hostSystemdDesiredStateForOperation(node.Host, operation))
	desiredEnabled := result.DesiredEnabled
	if desiredEnabled == nil {
		desiredEnabled = cloneBoolPtr(node.Host.Enabled)
	}
	if status == "succeeded" {
		switch desiredState {
		case "absent":
			if result.After.File.Exists {
				status = "failed"
				reason = "systemd unit file remained present"
			}
		default:
			if !result.After.File.Exists {
				status = "failed"
				reason = "systemd unit file was not present"
			} else if desiredDigest != "" && result.After.File.SHA256 != desiredDigest {
				status = "failed"
				reason = "systemd unit content did not match desired digest"
			}
		}
		if status == "succeeded" {
			switch desiredState {
			case "started", "restarted":
				if !result.After.Runtime.Active {
					status = "failed"
					reason = "systemd unit was not active"
				}
			case "stopped":
				if result.After.Runtime.Active {
					status = "failed"
					reason = "systemd unit remained active"
				}
			}
		}
		if status == "succeeded" && desiredEnabled != nil && result.After.Runtime.Enabled != *desiredEnabled {
			status = "failed"
			reason = "systemd unit enablement did not match desired state"
		}
	}
	return hostSystemdVerifyReceipt{
		APIVersion:     "torque.dev/host-systemd-unit-node/v1",
		Kind:           "HostSystemdVerifyReceipt",
		NodeID:         node.ID,
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         status,
		Reason:         reason,
		UnitDigest:     unitDigest,
		PathDigest:     pathDigest,
		DesiredState:   desiredState,
		DesiredDigest:  desiredDigest,
		DesiredEnabled: cloneBoolPtr(desiredEnabled),
		FileExists:     result.After.File.Exists,
		RuntimeExists:  result.After.Runtime.Exists,
		Active:         result.After.Runtime.Active,
		Enabled:        result.After.Runtime.Enabled,
		Changed:        result.Changed,
		Commands:       append([]hostSystemdCommandReceipt(nil), result.Commands...),
		Journal:        result.Journal,
		VerifiedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostSystemdReceipts(node *runNode, phase string, status string, reason string, observe hostSystemdObserveReceipt, plan hostSystemdPlanReceipt, diff hostSystemdDiffReceipt, apply *hostSystemdOperationResult, verify hostSystemdVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-systemd-unit-node/v1",
		"kind":       "HostSystemdUnitNodeArtifact",
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
		if apply.Journal != nil {
			payload["journal"] = *apply.Journal
		}
	}
	e.run.RecordJSONArtifact(node.ID, "host-systemd-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-systemd-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-systemd-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-systemd-apply.json", *apply)
		if apply.Journal != nil {
			e.run.RecordJSONArtifact(node.ID, "host-systemd-journal.json", *apply.Journal)
			e.run.RecordJSONArtifact(node.ID, "journal-evidence.json", *apply.Journal)
		}
	}
	e.run.RecordJSONArtifact(node.ID, "host-systemd-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostSystemdPythonScript = `
import base64
import grp
import hashlib
import json
import os
import pwd
import shutil
import stat
import subprocess
import tempfile
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    if isinstance(data, str):
        data = data.encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()

def digest_json(value):
    return digest_bytes(json.dumps(value, sort_keys=True, separators=(",", ":")))

def normalize_mode(value):
    value = str(value or "").strip()
    if not value:
        return "0644"
    if value.startswith("0o"):
        value = value[2:]
    return format(int(value[-4:], 8), "04o")

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

def systemctl_command():
    if have("systemctl"):
        return "systemctl"
    raise RuntimeError("systemctl is required")

def enabled_from_unit_file_state(value):
    return str(value or "").strip() in ("enabled", "enabled-runtime", "linked", "linked-runtime")

def observe_file(path):
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
    try:
        doc["owner"] = pwd.getpwuid(st.st_uid).pw_name
    except KeyError:
        doc["owner"] = str(st.st_uid)
    try:
        doc["group"] = grp.getgrgid(st.st_gid).gr_name
    except KeyError:
        doc["group"] = str(st.st_gid)
    if stat.S_ISREG(st.st_mode):
        doc["type"] = "file"
        with open(path, "rb") as fh:
            data = fh.read()
        doc["sha256"] = digest_bytes(data)
        doc["size"] = len(data)
    elif stat.S_ISLNK(st.st_mode):
        doc["type"] = "symlink"
    elif stat.S_ISDIR(st.st_mode):
        doc["type"] = "directory"
    else:
        doc["type"] = "other"
    return doc

def observe_runtime(unit, command):
    proc = run("show", [
        command,
        "show",
        "--property=LoadState",
        "--property=ActiveState",
        "--property=SubState",
        "--property=UnitFileState",
        unit,
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
        "unit": unit,
        "exists": exists,
        "active": active_state == "active",
        "enabled": enabled_from_unit_file_state(unit_file_state),
        "loadState": load_state,
        "activeState": active_state,
        "subState": props.get("SubState") or "",
        "unitFileState": unit_file_state,
    }

def journal_evidence(unit, lines=20):
    if not have("journalctl"):
        return {"status": "skipped", "reason": "journalctl not found", "collectedAt": now()}
    args = ["journalctl", "-u", unit, "-n", str(lines), "--no-pager", "--output", "short-iso"]
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    stdout = proc.stdout or ""
    stderr = proc.stderr or ""
    doc = {
        "status": "succeeded" if proc.returncode == 0 else "failed",
        "exitCode": int(proc.returncode),
        "commandDigest": digest_json(args),
        "lineCount": len([line for line in stdout.splitlines() if line.strip()]),
        "collectedAt": now(),
    }
    if stdout:
        doc["stdoutDigest"] = digest_bytes(stdout)
    if stderr:
        doc["stderrDigest"] = digest_bytes(stderr)
    return doc

def observe(path, unit, command, include_journal=False):
    journal = journal_evidence(unit) if include_journal else None
    doc = {
        "unit": unit,
        "path": path,
        "file": observe_file(path),
        "runtime": observe_runtime(unit, command),
    }
    if journal is not None:
        doc["journal"] = journal
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

def write_file(path, content, mode, owner, group):
    directory = os.path.dirname(path) or "."
    os.makedirs(directory, exist_ok=True)
    fd, tmp = tempfile.mkstemp(prefix=".torque-systemd-", dir=directory)
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(content)
        os.chmod(tmp, int(mode, 8))
        chown_if_requested(tmp, owner, group)
        os.replace(tmp, path)
    finally:
        try:
            os.unlink(tmp)
        except FileNotFoundError:
            pass

def changes_for(before, desired_state, desired_digest, mode, owner, group, desired_enabled, operation, stop_on_delete, disable_on_delete, remove_on_delete):
    file_state = before.get("file") or {}
    runtime = before.get("runtime") or {}
    if desired_state == "absent":
        content = bool(file_state.get("exists")) and (operation != "delete" or remove_on_delete)
        return {
            "content": content,
            "daemonReload": content,
            "active": bool(runtime.get("active")) and (operation != "delete" or stop_on_delete),
            "enabled": bool(runtime.get("enabled")) and (operation != "delete" or disable_on_delete),
        }
    content = (not bool(file_state.get("exists"))) or (bool(desired_digest) and file_state.get("sha256") != desired_digest)
    mode_change = bool(file_state.get("exists")) and bool(mode) and bool(file_state.get("mode")) and file_state.get("mode") != mode
    owner_change = bool(file_state.get("exists")) and bool(owner) and file_state.get("owner") != owner and str(file_state.get("uid", "")) != owner
    group_change = bool(file_state.get("exists")) and bool(group) and file_state.get("group") != group and str(file_state.get("gid", "")) != group
    changes = {
        "content": content,
        "mode": mode_change,
        "owner": owner_change,
        "group": group_change,
        "daemonReload": bool(content or mode_change or owner_change or group_change),
        "active": False,
        "enabled": False,
    }
    if desired_state == "started":
        changes["active"] = not bool(runtime.get("active"))
    elif desired_state == "stopped":
        changes["active"] = bool(runtime.get("active"))
    elif desired_state == "restarted":
        changes["active"] = True
        changes["restart"] = True
    if desired_enabled is not None:
        changes["enabled"] = bool(runtime.get("enabled")) != bool(desired_enabled)
    return changes

def changed(changes):
    return bool(changes.get("content") or changes.get("mode") or changes.get("owner") or changes.get("group") or changes.get("daemonReload") or changes.get("active") or changes.get("enabled") or changes.get("restart"))

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-systemd-unit-node/v1")
    doc.setdefault("kind", "HostSystemdUnitOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_SYSTEMD_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    unit = str(payload.get("unit") or "").strip()
    path = str(payload.get("path") or "").strip()
    desired = base64.b64decode(str(payload.get("contentB64") or ""))
    desired_digest = digest_bytes(desired) if desired else ""
    mode = normalize_mode(payload.get("mode"))
    owner = str(payload.get("owner") or "").strip()
    group = str(payload.get("group") or "").strip()
    state = str(payload.get("state") or "present").strip().lower()
    desired_enabled = payload.get("enabled") if "enabled" in payload and payload.get("enabled") is not None else None
    stop_on_delete = bool(payload.get("stopOnDelete"))
    disable_on_delete = bool(payload.get("disableOnDelete"))
    remove_on_delete = bool(payload.get("removeOnDelete"))
    if not unit:
        finish({"operation": operation, "status": "failed", "error": "unit is required"}, 1)
    if "/" in unit:
        finish({"operation": operation, "status": "failed", "error": "unit must not contain slash"}, 1)
    if not path:
        path = "/etc/systemd/system/" + unit
    if state not in ("present", "started", "stopped", "restarted", "absent"):
        finish({"operation": operation, "status": "failed", "error": "unsupported systemd unit state"}, 1)
    if state != "absent" and operation != "delete" and not desired:
        finish({"operation": operation, "status": "failed", "error": "unit content is required"}, 1)
    command = systemctl_command()
    before = observe(path, unit, command, False)
    desired_state = "absent" if operation == "delete" else state
    planned_changes = changes_for(before, desired_state, desired_digest, mode, owner, group, desired_enabled, operation, stop_on_delete, disable_on_delete, remove_on_delete)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "unitDigest": digest_bytes(unit),
            "pathDigest": digest_bytes(path),
            "desiredState": state,
            "desiredDigest": desired_digest,
            "desiredEnabled": desired_enabled,
            "changed": False,
            "changes": {"content": False, "daemonReload": False, "active": False, "enabled": False},
            "before": before,
            "after": before,
            "commands": commands,
        })
    if operation == "delete" or state == "absent":
        if disable_on_delete or state == "absent":
            if before.get("runtime", {}).get("enabled"):
                proc = run("disable", [command, "disable", unit])
                if proc.returncode != 0:
                    after = observe(path, unit, command, True)
                    finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": "absent", "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit disable failed"}, 1)
        if stop_on_delete or state == "absent":
            if before.get("runtime", {}).get("active"):
                proc = run("stop", [command, "stop", unit])
                if proc.returncode != 0:
                    after = observe(path, unit, command, True)
                    finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": "absent", "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit stop failed"}, 1)
        should_remove = state == "absent" or remove_on_delete
        if should_remove and before.get("file", {}).get("exists"):
            if before.get("file", {}).get("type") != "file":
                finish({"operation": operation, "status": "failed", "before": before, "after": before, "commands": commands, "error": "systemd unit path is not a regular file"}, 1)
            os.unlink(path)
            proc = run("daemon-reload", [command, "daemon-reload"])
            if proc.returncode != 0:
                after = observe(path, unit, command, True)
                finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": "absent", "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd daemon-reload failed"}, 1)
        after = observe(path, unit, command, True)
        status = "succeeded"
        error = ""
        if should_remove and after.get("file", {}).get("exists"):
            status = "failed"
            error = "systemd unit file remained present"
        if (stop_on_delete or state == "absent") and after.get("runtime", {}).get("active"):
            status = "failed"
            error = "systemd unit remained active"
        if (disable_on_delete or state == "absent") and after.get("runtime", {}).get("enabled"):
            status = "failed"
            error = "systemd unit remained enabled"
        finish({
            "operation": operation,
            "status": status,
            "unitDigest": digest_bytes(unit),
            "pathDigest": digest_bytes(path),
            "desiredState": "absent",
            "desiredDigest": "",
            "desiredEnabled": False if (disable_on_delete or state == "absent") else None,
            "changed": changed(planned_changes),
            "changes": planned_changes,
            "before": before,
            "after": after,
            "commands": commands,
            "journal": after.get("journal"),
            "error": error,
        }, 0 if status == "succeeded" else 1)
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if before.get("file", {}).get("exists") and before.get("file", {}).get("type") != "file":
        finish({"operation": operation, "status": "failed", "before": before, "after": before, "commands": commands, "error": "systemd unit path is not a regular file"}, 1)
    if planned_changes.get("content") or planned_changes.get("mode") or planned_changes.get("owner") or planned_changes.get("group"):
        write_file(path, desired, mode, owner, group)
        proc = run("daemon-reload", [command, "daemon-reload"])
        if proc.returncode != 0:
            after = observe(path, unit, command, True)
            finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": state, "desiredDigest": desired_digest, "desiredEnabled": desired_enabled, "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd daemon-reload failed"}, 1)
    if desired_enabled is not None and bool(before.get("runtime", {}).get("enabled")) != bool(desired_enabled):
        action = "enable" if bool(desired_enabled) else "disable"
        proc = run(action, [command, action, unit])
        if proc.returncode != 0:
            after = observe(path, unit, command, True)
            finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": state, "desiredDigest": desired_digest, "desiredEnabled": desired_enabled, "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit " + action + " failed"}, 1)
    if state == "started" and not before.get("runtime", {}).get("active"):
        proc = run("start", [command, "start", unit])
        if proc.returncode != 0:
            after = observe(path, unit, command, True)
            finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": state, "desiredDigest": desired_digest, "desiredEnabled": desired_enabled, "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit start failed"}, 1)
    elif state == "stopped" and before.get("runtime", {}).get("active"):
        proc = run("stop", [command, "stop", unit])
        if proc.returncode != 0:
            after = observe(path, unit, command, True)
            finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": state, "desiredDigest": desired_digest, "desiredEnabled": desired_enabled, "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit stop failed"}, 1)
    elif state == "restarted":
        proc = run("restart", [command, "restart", unit])
        if proc.returncode != 0:
            after = observe(path, unit, command, True)
            finish({"operation": operation, "status": "failed", "unitDigest": digest_bytes(unit), "pathDigest": digest_bytes(path), "desiredState": state, "desiredDigest": desired_digest, "desiredEnabled": desired_enabled, "changed": False, "changes": planned_changes, "before": before, "after": after, "commands": commands, "journal": after.get("journal"), "error": "systemd unit restart failed"}, 1)
    after = observe(path, unit, command, True)
    status = "succeeded"
    error = ""
    if not after.get("file", {}).get("exists"):
        status = "failed"
        error = "systemd unit file was not present"
    elif after.get("file", {}).get("sha256") != desired_digest:
        status = "failed"
        error = "systemd unit content did not match desired digest"
    elif state in ("started", "restarted") and not after.get("runtime", {}).get("active"):
        status = "failed"
        error = "systemd unit was not active"
    elif state == "stopped" and after.get("runtime", {}).get("active"):
        status = "failed"
        error = "systemd unit remained active"
    if desired_enabled is not None and bool(after.get("runtime", {}).get("enabled")) != bool(desired_enabled):
        status = "failed"
        error = "systemd unit enablement did not match desired state"
    finish({
        "operation": operation,
        "status": status,
        "unitDigest": digest_bytes(unit),
        "pathDigest": digest_bytes(path),
        "desiredState": state,
        "desiredDigest": desired_digest,
        "desiredEnabled": desired_enabled,
        "changed": changed(planned_changes),
        "changes": planned_changes,
        "before": before,
        "after": after,
        "commands": commands,
        "journal": after.get("journal"),
        "error": error,
    }, 0 if status == "succeeded" else 1)
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc), "commands": commands}, 1)
`
