package stack

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"text/template"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type dispatchExecutor struct {
	helm   *helmExecutor
	custom *customNodeExecutor
}

func (d *dispatchExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	if isHelmNode(node.ResolvedRelease) {
		return d.helm.RunNode(ctx, node, command)
	}
	return d.custom.RunNode(ctx, node, command)
}

type customNodeExecutor struct {
	run    *runState
	out    io.Writer
	errOut io.Writer
	dryRun bool
	diff   bool
}

func (e *customNodeExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	switch normalizeNodeKind(node.Kind) {
	case NodeKindAction:
		return e.runScriptNode(ctx, node, command)
	case NodeKindActionPlugin:
		return e.runActionPluginNode(ctx, node, command)
	case NodeKindDBRestorePoint:
		return e.runDBRestorePointNode(ctx, node, command)
	case NodeKindDBSchemaExpand:
		return e.runDBSchemaExpandNode(ctx, node, command)
	case NodeKindDBBackfill:
		return e.runDBBackfillNode(ctx, node, command)
	case NodeKindDBVerify:
		return e.runDBVerifyNode(ctx, node, command)
	case NodeKindDBCutover:
		return e.runDBCutoverNode(ctx, node, command)
	case NodeKindDBSchemaContract:
		return e.runDBSchemaContractNode(ctx, node, command)
	case NodeKindHostCommandRun:
		return e.runHostCommandNode(ctx, node, command)
	case NodeKindHostFileRender:
		return e.runHostFileRenderNode(ctx, node, command)
	case NodeKindHostFileCopy:
		return e.runHostFileCopyNode(ctx, node, command)
	case NodeKindHostPackageInstall:
		return e.runHostPackageInstallNode(ctx, node, command)
	case NodeKindHostServiceManage:
		return e.runHostServiceManageNode(ctx, node, command)
	case NodeKindHostUserManage:
		return e.runHostUserManageNode(ctx, node, command)
	case NodeKindHostCronManage:
		return e.runHostCronManageNode(ctx, node, command)
	case NodeKindHostSystemdUnit:
		return e.runHostSystemdUnitNode(ctx, node, command)
	case NodeKindK8sClusterInspect:
		return e.runKubernetesClusterInspectNode(ctx, node, command)
	case NodeKindK8sManifestApply:
		return e.runKubernetesManifestApplyNode(ctx, node, command)
	case NodeKindK8sCertInspect:
		return e.runKubernetesCertInspectNode(ctx, node, command)
	case NodeKindK8sCertRenew:
		return e.runKubernetesCertRenewNode(ctx, node, command)
	case NodeKindK8sClusterVerify:
		return e.runKubernetesClusterVerifyNode(ctx, node, command)
	default:
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("unsupported node kind %q", normalizeNodeKind(node.Kind)))
	}
}

func (e *customNodeExecutor) runScriptNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Action.Apply
	phase := "script"
	if strings.EqualFold(command, "delete") {
		spec = node.Action.Delete
		phase = "delete-script"
	}
	if spec == nil {
		return nil
	}
	cursor := map[string]any{
		"kind":  normalizeNodeKind(node.Kind),
		"phase": phase,
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	workDir := strings.TrimSpace(spec.WorkDir)
	if workDir == "" {
		workDir = node.Dir
	}
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), flattenEnv(spec.Env)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		e.recordActionNodeArtifacts(node, phase, spec, workDir, "failure", msg)
		runErr := &RunError{Class: "SCRIPT_FAILED", Message: msg, Digest: computeRunErrorDigest("SCRIPT_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "failure",
			"message": msg,
			"cursor":  cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("script phase %s: %w", phase, err))
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = "success"
	}
	e.recordActionNodeArtifacts(node, phase, spec, workDir, "success", msg)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":   phase,
		"status":  "success",
		"message": msg,
		"cursor":  cursor,
	}, nil)
	return nil
}

func (e *customNodeExecutor) recordActionNodeArtifacts(node *runNode, phase string, spec *ScriptHookConfig, workDir string, status string, output string) {
	if e == nil || e.run == nil || node == nil {
		return
	}
	artifactName := "script-output.json"
	if strings.TrimSpace(phase) != "" && phase != "script" {
		artifactName = strings.TrimSpace(phase) + "-output.json"
	}
	payload := map[string]any{
		"apiVersion": "torque.dev/action-node/v1",
		"kind":       "ActionNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"status":     strings.TrimSpace(status),
		"workDir":    strings.TrimSpace(workDir),
		"output":     strings.TrimSpace(output),
	}
	if spec != nil && len(spec.Command) > 0 {
		payload["command"] = append([]string(nil), spec.Command...)
	}
	trimmed := strings.TrimSpace(output)
	if trimmed != "" {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			payload["outputFormat"] = "json"
			payload["result"] = decoded
		} else {
			payload["outputFormat"] = "text"
		}
	}
	e.run.RecordJSONArtifact(node.ID, artifactName, payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) runHostCommandNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	remoteCommand := strings.TrimSpace(spec.Command)
	phase := "host-command"
	if strings.EqualFold(command, "delete") {
		remoteCommand = strings.TrimSpace(spec.DeleteCommand)
		phase = "delete-host-command"
	}
	if remoteCommand == "" {
		return nil
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		observe := e.hostCommandObserveReceipt(node, phase, "")
		plan := e.hostCommandPlanReceipt(node, phase, remoteCommand, "skipped", reason)
		verify := hostCommandVerifyReceipt{
			APIVersion:    "torque.dev/host-command-node/v1",
			Kind:          "HostCommandVerifyReceipt",
			NodeID:        node.ID,
			TargetID:      strings.TrimSpace(spec.TargetID),
			Phase:         phase,
			Status:        "skipped",
			Reason:        reason,
			Redaction:     hostCommandRedactionProof{},
			VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			ReceiptStatus: "skipped",
		}
		e.recordHostCommandReceipts(node, phase, "skipped", reason, observe, plan, nil, verify)
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
	observe := e.hostCommandObserveReceipt(node, phase, targetDigest)
	plan := e.hostCommandPlanReceipt(node, phase, remoteCommand, "planned", "eligible")
	if guardErr := e.validateHostCommandOpsGuard(node, &plan); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := hostCommandVerifyReceipt{
			APIVersion:    "torque.dev/host-command-node/v1",
			Kind:          "HostCommandVerifyReceipt",
			NodeID:        node.ID,
			TargetID:      plan.TargetID,
			Phase:         phase,
			Status:        "blocked",
			Reason:        guardErr.Error(),
			Redaction:     hostCommandRedactionProof{},
			VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
			ReceiptStatus: "blocked",
		}
		e.recordHostCommandReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, nil, verify)
		runErr := &RunError{Class: "HOST_COMMAND_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_COMMAND_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": plan.TargetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host command phase %s: %w", phase, guardErr))
	}
	receipt := transportClient.Run(ctx, remoteCommand)
	verify := e.hostCommandVerifyReceipt(node, phase, plan.TargetID, receipt)
	e.recordHostCommandReceipts(node, phase, receipt.Status, strings.TrimSpace(receipt.Error), observe, plan, &receipt, verify)
	if !nodeStepSucceeded(receipt.Status) {
		msg := strings.TrimSpace(receipt.Error)
		if msg == "" {
			msg = strings.TrimSpace(receipt.Stderr)
		}
		if msg == "" {
			msg = fmt.Sprintf("host command status %s", receipt.Status)
		}
		runErr := &RunError{Class: "HOST_COMMAND_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_COMMAND_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "failure",
			"cursor":  cursor,
			"receipt": receipt,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host command phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"cursor":  cursor,
		"receipt": receipt,
	}, nil)
	return nil
}

type hostCommandObserveReceipt struct {
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
	FactSources      []string `json:"factSources,omitempty"`
	FactDigests      []string `json:"factDigests,omitempty"`
	TargetDigest     string   `json:"targetDigest,omitempty"`
	ObservedAt       string   `json:"observedAt"`
}

type hostCommandPlanReceipt struct {
	APIVersion      string   `json:"apiVersion"`
	Kind            string   `json:"kind"`
	NodeID          string   `json:"nodeId"`
	NodeKind        string   `json:"nodeKind"`
	TargetID        string   `json:"targetId,omitempty"`
	Phase           string   `json:"phase"`
	Status          string   `json:"status"`
	Reason          string   `json:"reason,omitempty"`
	GuardMode       string   `json:"guardMode"`
	Operation       string   `json:"operation"`
	CommandDigest   string   `json:"commandDigest,omitempty"`
	SelectedTargets []string `json:"selectedTargets,omitempty"`
	LockScopes      []string `json:"lockScopes,omitempty"`
	PolicySources   []string `json:"policySources,omitempty"`
	PlannedAt       string   `json:"plannedAt"`
}

type hostCommandVerifyReceipt struct {
	APIVersion    string                    `json:"apiVersion"`
	Kind          string                    `json:"kind"`
	NodeID        string                    `json:"nodeId"`
	TargetID      string                    `json:"targetId,omitempty"`
	Phase         string                    `json:"phase"`
	Status        string                    `json:"status"`
	Reason        string                    `json:"reason,omitempty"`
	ReceiptStatus string                    `json:"receiptStatus"`
	ExitCode      int                       `json:"exitCode,omitempty"`
	StdoutDigest  string                    `json:"stdoutDigest,omitempty"`
	StderrDigest  string                    `json:"stderrDigest,omitempty"`
	Redaction     hostCommandRedactionProof `json:"redaction"`
	VerifiedAt    string                    `json:"verifiedAt"`
}

type hostCommandRedactionProof struct {
	StdoutBytes           int  `json:"stdoutBytes"`
	StderrBytes           int  `json:"stderrBytes"`
	StdoutRedacted        bool `json:"stdoutRedacted"`
	StderrRedacted        bool `json:"stderrRedacted"`
	NoSecretRefs          bool `json:"noSecretRefs"`
	NoSensitiveKV         bool `json:"noSensitiveKeyValues"`
	NoAuthorizationBearer bool `json:"noAuthorizationBearer"`
}

func (e *customNodeExecutor) hostCommandObserveReceipt(node *runNode, phase string, targetDigest string) hostCommandObserveReceipt {
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	var factSources []string
	var factDigests []string
	if e != nil && e.run != nil && e.run.Plan != nil && e.run.Plan.Ops != nil {
		for _, facts := range e.run.Plan.Ops.FactEvidence {
			if strings.TrimSpace(facts.Source) != "" {
				factSources = append(factSources, strings.TrimSpace(facts.Source))
			}
			if strings.TrimSpace(facts.Digest) != "" {
				factDigests = append(factDigests, strings.TrimSpace(facts.Digest))
			}
		}
	}
	sort.Strings(factSources)
	sort.Strings(factDigests)
	return hostCommandObserveReceipt{
		APIVersion:       "torque.dev/host-command-node/v1",
		Kind:             "HostCommandObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           "observed",
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		FactSources:      factSources,
		FactDigests:      factDigests,
		TargetDigest:     strings.TrimSpace(targetDigest),
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostCommandPlanReceipt(node *runNode, phase string, remoteCommand string, status string, reason string) hostCommandPlanReceipt {
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
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
	return hostCommandPlanReceipt{
		APIVersion:      "torque.dev/host-command-node/v1",
		Kind:            "HostCommandPlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostCommandRun,
		CommandDigest:   digestString(remoteCommand),
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostCommandVerifyReceipt(node *runNode, phase string, targetID string, receipt transport.OperationResult) hostCommandVerifyReceipt {
	status := "succeeded"
	reason := "command receipt succeeded"
	if !nodeStepSucceeded(receipt.Status) {
		status = "failed"
		reason = firstNonEmptyString(receipt.Error, receipt.Stderr, "command receipt failed")
	}
	return hostCommandVerifyReceipt{
		APIVersion:    "torque.dev/host-command-node/v1",
		Kind:          "HostCommandVerifyReceipt",
		NodeID:        node.ID,
		TargetID:      strings.TrimSpace(targetID),
		Phase:         phase,
		Status:        status,
		Reason:        reason,
		ReceiptStatus: strings.TrimSpace(receipt.Status),
		ExitCode:      receipt.ExitCode,
		StdoutDigest:  digestString(receipt.Stdout),
		StderrDigest:  digestString(receipt.Stderr),
		Redaction:     hostCommandRedaction(receipt),
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostCommandReceipts(node *runNode, phase string, status string, reason string, observe hostCommandObserveReceipt, plan hostCommandPlanReceipt, execute *transport.OperationResult, verify hostCommandVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-command-node/v1",
		"kind":       "HostCommandNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"guardMode":  strings.TrimSpace(plan.GuardMode),
		"observe":    observe,
		"plan":       plan,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if execute != nil {
		payload["targetDigest"] = execute.TargetDigest
		payload["receipt"] = *execute
		payload["execute"] = *execute
	}
	e.run.RecordJSONArtifact(node.ID, "host-command-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-command-plan.json", plan)
	if execute != nil {
		e.run.RecordJSONArtifact(node.ID, "host-command-execute.json", *execute)
	}
	e.run.RecordJSONArtifact(node.ID, "host-command-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func hostCommandRedaction(receipt transport.OperationResult) hostCommandRedactionProof {
	stdout := strings.TrimSpace(receipt.Stdout)
	stderr := strings.TrimSpace(receipt.Stderr)
	combined := strings.ToLower(stdout + "\n" + stderr)
	return hostCommandRedactionProof{
		StdoutBytes:           len(receipt.Stdout),
		StderrBytes:           len(receipt.Stderr),
		StdoutRedacted:        strings.Contains(stdout, "[REDACTED"),
		StderrRedacted:        strings.Contains(stderr, "[REDACTED"),
		NoSecretRefs:          !strings.Contains(combined, "secret://"),
		NoSensitiveKV:         !hostCommandHasRawSensitiveKV(combined),
		NoAuthorizationBearer: !strings.Contains(combined, "authorization: bearer "),
	}
}

func hostCommandHasRawSensitiveKV(value string) bool {
	for _, key := range []string{"password=", "passwd=", "token=", "secret="} {
		idx := strings.Index(value, key)
		for idx >= 0 {
			rest := value[idx+len(key):]
			if !strings.HasPrefix(rest, "[redacted]") {
				return true
			}
			next := strings.Index(rest, key)
			if next < 0 {
				break
			}
			idx += len(key) + next
		}
	}
	return false
}

func (e *customNodeExecutor) validateHostCommandOpsGuard(node *runNode, plan *hostCommandPlanReceipt) error {
	if e == nil || e.run == nil || e.run.Plan == nil || e.run.Plan.Ops == nil {
		if plan != nil {
			plan.GuardMode = "legacy"
		}
		return nil
	}
	targetID := strings.TrimSpace(plan.TargetID)
	return e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostCommandRun)
}

func (e *customNodeExecutor) hostCommandTargetContext(node *runNode) (string, string, []string) {
	targetID := ""
	if node != nil {
		targetID = strings.TrimSpace(node.Host.TargetID)
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

func opsFactsContainTarget(ops *OpsPlanInputs, targetID string) bool {
	if ops == nil || targetID == "" {
		return false
	}
	for _, facts := range ops.FactEvidence {
		for _, target := range facts.Targets {
			if strings.TrimSpace(target.TargetID) != targetID {
				continue
			}
			switch strings.ToLower(strings.TrimSpace(target.Status)) {
			case "", "collected", "cached", "fresh", "succeeded", "success":
				return true
			default:
				return false
			}
		}
	}
	return false
}

func opsLockAllowsTarget(ops *OpsPlanInputs, targetID string) bool {
	if ops == nil || targetID == "" {
		return false
	}
	for _, lockInput := range ops.Locks {
		if strings.TrimSpace(lockInput.TargetID) != targetID {
			continue
		}
		if lockInput.Found && strings.EqualFold(strings.TrimSpace(lockInput.Status), "held") {
			return true
		}
	}
	return false
}

func opsPolicyAllowsTarget(ops *OpsPlanInputs, targetID string) bool {
	return opsPolicyAllowsOperationTarget(ops, targetID, NodeKindHostCommandRun)
}

func opsPolicyAllowsOperationTarget(ops *OpsPlanInputs, targetID string, operation string) bool {
	if ops == nil {
		return false
	}
	operation = strings.TrimSpace(operation)
	for _, decision := range ops.PolicyDecisions {
		decisionTarget := strings.TrimSpace(decision.TargetID)
		if decisionTarget != "" && decisionTarget != targetID {
			continue
		}
		if strings.EqualFold(strings.TrimSpace(decision.Operation), operation) && strings.TrimSpace(decision.Decision) == "allow" {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(decision.Adapter), operation) && strings.TrimSpace(decision.Decision) == "allow" {
			return true
		}
		if strings.TrimSpace(decision.Operation) == "" && strings.TrimSpace(decision.Decision) == "allow" {
			return true
		}
	}
	return false
}

func stringInSlice(needle string, haystack []string) bool {
	needle = strings.TrimSpace(needle)
	for _, item := range haystack {
		if strings.TrimSpace(item) == needle {
			return true
		}
	}
	return false
}

type hostCommandRunner interface {
	TargetDigest() string
	Run(ctx context.Context, command string) transport.OperationResult
}

func hostCommandTransport(spec HostCommandSpec) (hostCommandRunner, error) {
	transportKind := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transportKind == "" {
		transportKind = "local"
	}
	timeout := 30 * time.Second
	if spec.Timeout != nil && *spec.Timeout > 0 {
		timeout = *spec.Timeout
	}
	target := strings.TrimSpace(spec.Target)
	if envName := strings.TrimSpace(spec.TargetEnv); envName != "" {
		target = strings.TrimSpace(os.Getenv(envName))
		if target == "" {
			return nil, fmt.Errorf("host command target env %s is empty", envName)
		}
	}
	switch transportKind {
	case "local", "localhost":
		if target == "" {
			target = "local://localhost"
		}
		return localtransport.New(localtransport.Config{
			Target:       target,
			Timeout:      timeout,
			RedactValues: []string{target},
		})
	case "ssh":
		if target == "" {
			return nil, fmt.Errorf("host.command.run ssh transport requires host.target or host.targetEnv")
		}
		return sshtransport.New(sshtransport.Config{
			Target:       target,
			IdentityFile: strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_IDENTITY")),
			ExtraArgs:    strings.Fields(strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_OPTS"))),
			Timeout:      timeout,
			RedactValues: []string{target},
		})
	default:
		return nil, fmt.Errorf("unsupported host.command.run transport %q", transportKind)
	}
}

type hostFileState struct {
	Exists bool   `json:"exists"`
	Type   string `json:"type,omitempty"`
	Path   string `json:"path,omitempty"`
	Sha256 string `json:"sha256,omitempty"`
	Size   int64  `json:"size,omitempty"`
	Mode   string `json:"mode,omitempty"`
	Owner  string `json:"owner,omitempty"`
	Group  string `json:"group,omitempty"`
	UID    int    `json:"uid,omitempty"`
	GID    int    `json:"gid,omitempty"`
}

type hostFileValidationResult struct {
	Status   string `json:"status"`
	Command  string `json:"command,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
}

type hostFileChangeSet struct {
	Content bool `json:"content"`
	Mode    bool `json:"mode"`
	Owner   bool `json:"owner"`
	Group   bool `json:"group"`
}

type hostFileOperationResult struct {
	APIVersion       string                   `json:"apiVersion"`
	Kind             string                   `json:"kind"`
	Operation        string                   `json:"operation"`
	Status           string                   `json:"status"`
	Reason           string                   `json:"reason,omitempty"`
	TargetDigest     string                   `json:"targetDigest,omitempty"`
	PathDigest       string                   `json:"pathDigest,omitempty"`
	DesiredDigest    string                   `json:"desiredDigest,omitempty"`
	SourceDigest     string                   `json:"sourceDigest,omitempty"`
	BackupPathDigest string                   `json:"backupPathDigest,omitempty"`
	Changed          bool                     `json:"changed"`
	Changes          hostFileChangeSet        `json:"changes"`
	Before           hostFileState            `json:"before"`
	After            hostFileState            `json:"after"`
	Backup           *hostFileState           `json:"backup,omitempty"`
	Restored         bool                     `json:"restored,omitempty"`
	Validation       hostFileValidationResult `json:"validation,omitempty"`
	Error            string                   `json:"error,omitempty"`
	CompletedAt      string                   `json:"completedAt"`
}

type hostFileObserveReceipt struct {
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
	State            hostFileState `json:"state"`
	ObservedAt       string        `json:"observedAt"`
}

type hostFilePlanReceipt struct {
	APIVersion       string            `json:"apiVersion"`
	Kind             string            `json:"kind"`
	NodeID           string            `json:"nodeId"`
	NodeKind         string            `json:"nodeKind"`
	TargetID         string            `json:"targetId,omitempty"`
	Phase            string            `json:"phase"`
	Status           string            `json:"status"`
	Reason           string            `json:"reason,omitempty"`
	GuardMode        string            `json:"guardMode"`
	Operation        string            `json:"operation"`
	PathDigest       string            `json:"pathDigest,omitempty"`
	DesiredDigest    string            `json:"desiredDigest,omitempty"`
	SourceDigest     string            `json:"sourceDigest,omitempty"`
	Mode             string            `json:"mode,omitempty"`
	Owner            string            `json:"owner,omitempty"`
	Group            string            `json:"group,omitempty"`
	Backup           bool              `json:"backup,omitempty"`
	BackupPathDigest string            `json:"backupPathDigest,omitempty"`
	RestoreOnDelete  bool              `json:"restoreOnDelete,omitempty"`
	SelectedTargets  []string          `json:"selectedTargets,omitempty"`
	LockScopes       []string          `json:"lockScopes,omitempty"`
	PolicySources    []string          `json:"policySources,omitempty"`
	Changes          hostFileChangeSet `json:"changes"`
	PlannedAt        string            `json:"plannedAt"`
}

type hostFileDiffReceipt struct {
	APIVersion  string            `json:"apiVersion"`
	Kind        string            `json:"kind"`
	NodeID      string            `json:"nodeId"`
	TargetID    string            `json:"targetId,omitempty"`
	Phase       string            `json:"phase"`
	Status      string            `json:"status"`
	PathDigest  string            `json:"pathDigest,omitempty"`
	Before      hostFileState     `json:"before"`
	AfterDigest string            `json:"afterDigest,omitempty"`
	AfterMode   string            `json:"afterMode,omitempty"`
	AfterOwner  string            `json:"afterOwner,omitempty"`
	AfterGroup  string            `json:"afterGroup,omitempty"`
	Changes     hostFileChangeSet `json:"changes"`
	DiffQuality string            `json:"diffQuality"`
	GeneratedAt string            `json:"generatedAt"`
}

type hostFileVerifyReceipt struct {
	APIVersion    string                   `json:"apiVersion"`
	Kind          string                   `json:"kind"`
	NodeID        string                   `json:"nodeId"`
	TargetID      string                   `json:"targetId,omitempty"`
	Phase         string                   `json:"phase"`
	Status        string                   `json:"status"`
	Reason        string                   `json:"reason,omitempty"`
	PathDigest    string                   `json:"pathDigest,omitempty"`
	DesiredDigest string                   `json:"desiredDigest,omitempty"`
	ActualDigest  string                   `json:"actualDigest,omitempty"`
	Mode          string                   `json:"mode,omitempty"`
	Owner         string                   `json:"owner,omitempty"`
	Group         string                   `json:"group,omitempty"`
	Changed       bool                     `json:"changed"`
	Validation    hostFileValidationResult `json:"validation,omitempty"`
	VerifiedAt    string                   `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostFileRenderNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-file-render"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-file-render"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	desired, err := renderHostFileDesiredContent(node)
	if err != nil && operation != "delete" {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	pathDigest := digestString(spec.Path)
	desiredDigest := digestBytes(desired)
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		observe := e.hostFileObserveReceipt(node, phase, targetID, guardMode, selected, "", pathDigest, hostFileState{}, "skipped")
		plan := e.hostFilePlanReceipt(node, phase, targetID, guardMode, selected, desiredDigest, pathDigest, hostFileChangeSet{}, "skipped", reason)
		diff := e.hostFileDiffReceipt(node, phase, targetID, pathDigest, desiredDigest, hostFileState{}, hostFileChangeSet{}, "skipped")
		verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, hostFileOperationResult{Status: "skipped", Reason: reason})
		e.recordHostFileRenderReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	observeResult, err := e.runHostFileOperation(ctx, transportClient, hostFilePayload(spec, "observe", nil))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostFileObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, pathDigest, observeResult.After, observeResult.Status)
	changes := hostFileChanges(observeResult.After, spec, desiredDigest)
	plan := e.hostFilePlanReceipt(node, phase, targetID, guardMode, selected, desiredDigest, pathDigest, changes, "planned", "eligible")
	diff := e.hostFileDiffReceipt(node, phase, targetID, pathDigest, desiredDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostFileRender); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, hostFileOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostFileRenderReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_FILE_RENDER_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_FILE_RENDER_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host file render phase %s: %w", phase, guardErr))
	}

	var result hostFileOperationResult
	if operation == "delete" {
		result, err = e.runHostFileOperation(ctx, transportClient, hostFilePayload(spec, "delete", nil))
	} else {
		result, err = e.runHostFileOperation(ctx, transportClient, hostFilePayload(spec, "apply", desired))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.PathDigest = pathDigest
	if result.DesiredDigest == "" {
		result.DesiredDigest = desiredDigest
	}
	if result.Changes == (hostFileChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostFileVerifyReceipt(node, phase, targetID, desiredDigest, pathDigest, result)
	e.recordHostFileRenderReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Validation.Error, result.Validation.Stderr, result.Reason, verify.Reason, "host file render failed")
		runErr := &RunError{Class: "HOST_FILE_RENDER_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_FILE_RENDER_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host file render phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func renderHostFileDesiredContent(node *runNode) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("nil host.file.render node")
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
			return nil, fmt.Errorf("read host.file.render template: %w", err)
		}
		source = string(raw)
	}
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("host.file.render requires content, template, or templatePath")
	}
	tpl, err := template.New("host-file-render").Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse host.file.render template: %w", err)
	}
	data := map[string]any{}
	for k, v := range spec.Data {
		data[k] = v
	}
	data["NodeID"] = node.ID
	data["NodeName"] = node.Name
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render host.file.render template: %w", err)
	}
	return out.Bytes(), nil
}

func (e *customNodeExecutor) runHostFileOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostFileOperationResult, error) {
	command, err := hostFilePythonCommand(payload)
	if err != nil {
		return hostFileOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostFileOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostFileOperationResult{}, fmt.Errorf("decode host.file.render receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-file-render-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostFileRenderOperationReceipt"
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

func hostFilePayload(spec HostCommandSpec, operation string, desired []byte) map[string]any {
	return map[string]any{
		"operation":      strings.TrimSpace(operation),
		"path":           strings.TrimSpace(spec.Path),
		"contentB64":     base64.StdEncoding.EncodeToString(desired),
		"mode":           strings.TrimSpace(spec.Mode),
		"owner":          strings.TrimSpace(spec.Owner),
		"group":          strings.TrimSpace(spec.Group),
		"validate":       strings.TrimSpace(spec.Validate),
		"removeOnDelete": spec.RemoveOnDelete,
	}
}

func hostFilePythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_FILE_RENDER_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostFilePythonScript + "\nPY", nil
}

const hostFilePythonScript = `
import base64
import grp
import hashlib
import json
import os
import pwd
import shutil
import stat
import subprocess
import sys
import tempfile
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    return "sha256:" + hashlib.sha256(data).hexdigest()

def normalize_mode(value):
    value = str(value or "").strip()
    if not value:
        return ""
    if value.startswith("0o"):
        value = value[2:]
    value = value[-4:]
    return format(int(value, 8), "04o")

def observe(path):
    path = str(path or "").strip()
    doc = {"exists": False, "path": path}
    if not os.path.lexists(path):
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

def run_validation(command, temp_path, path, mode):
    command = str(command or "").strip()
    if not command:
        return {"status": "skipped"}
    env = os.environ.copy()
    env["TORQUE_FILE_RENDER_PATH"] = path
    env["TORQUE_FILE_RENDER_TEMP_PATH"] = temp_path
    env["TORQUE_FILE_RENDER_MODE"] = mode
    proc = subprocess.run(command, shell=True, env=env, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    return {
        "status": "succeeded" if proc.returncode == 0 else "failed",
        "command": command,
        "exitCode": int(proc.returncode),
        "stdout": proc.stdout.strip(),
        "stderr": proc.stderr.strip(),
    }

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-file-render-node/v1")
    doc.setdefault("kind", "HostFileRenderOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_FILE_RENDER_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    path = str(payload.get("path") or "").strip()
    desired = base64.b64decode(str(payload.get("contentB64") or ""))
    desired_digest = digest_bytes(desired)
    mode = normalize_mode(payload.get("mode"))
    owner = str(payload.get("owner") or "").strip()
    group = str(payload.get("group") or "").strip()
    validate = str(payload.get("validate") or "").strip()
    remove_on_delete = bool(payload.get("removeOnDelete"))
    if not path:
        finish({"operation": operation, "status": "failed", "error": "path is required"}, 1)
    before = observe(path)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "pathDigest": digest_bytes(path.encode("utf-8")),
            "desiredDigest": desired_digest if desired else "",
            "changed": False,
            "changes": {"content": False, "mode": False, "owner": False, "group": False},
            "before": before,
            "after": before,
            "validation": {"status": "skipped"},
        })
    if operation == "delete":
        if not remove_on_delete:
            finish({
                "operation": operation,
                "status": "skipped",
                "reason": "removeOnDelete is false",
                "before": before,
                "after": before,
                "changed": False,
                "changes": {"content": False, "mode": False, "owner": False, "group": False},
                "validation": {"status": "skipped"},
            })
        if before.get("exists"):
            if before.get("type") != "file":
                finish({"operation": operation, "status": "failed", "before": before, "after": before, "error": "target path is not a regular file"}, 1)
            os.unlink(path)
        after = observe(path)
        finish({
            "operation": operation,
            "status": "succeeded",
            "before": before,
            "after": after,
            "changed": bool(before.get("exists")),
            "changes": {"content": bool(before.get("exists")), "mode": False, "owner": False, "group": False},
            "validation": {"status": "skipped"},
        })
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if before.get("exists") and before.get("type") != "file":
        finish({"operation": operation, "status": "failed", "before": before, "after": before, "error": "target path is not a regular file"}, 1)
    changes = {
        "content": (not before.get("exists")) or before.get("sha256") != desired_digest,
        "mode": bool(mode) and before.get("mode") != mode,
        "owner": bool(owner) and before.get("owner") != owner and str(before.get("uid", "")) != owner,
        "group": bool(group) and before.get("group") != group and str(before.get("gid", "")) != group,
    }
    changed = bool(changes["content"] or changes["mode"] or changes["owner"] or changes["group"])
    parent = os.path.dirname(path) or "."
    os.makedirs(parent, exist_ok=True)
    fd, temp_path = tempfile.mkstemp(prefix=".torque-file-render-", dir=parent)
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(desired)
        if mode:
            os.chmod(temp_path, int(mode, 8))
        validation = run_validation(validate, temp_path, path, mode)
        if validation.get("status") == "failed":
            finish({
                "operation": operation,
                "status": "failed",
                "before": before,
                "after": before,
                "desiredDigest": desired_digest,
                "changed": False,
                "changes": changes,
                "validation": validation,
                "error": "validation command failed",
            }, 1)
        if changed:
            os.replace(temp_path, path)
        else:
            os.unlink(temp_path)
            temp_path = ""
        if mode:
            os.chmod(path, int(mode, 8))
        if owner or group:
            shutil.chown(path, user=owner or None, group=group or None)
        after = observe(path)
        finish({
            "operation": operation,
            "status": "succeeded",
            "before": before,
            "after": after,
            "desiredDigest": desired_digest,
            "changed": changed,
            "changes": changes,
            "validation": validation,
        })
    finally:
        if temp_path:
            try:
                os.unlink(temp_path)
            except FileNotFoundError:
                pass
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc)}, 1)
`

func hostFileChanges(current hostFileState, spec HostCommandSpec, desiredDigest string) hostFileChangeSet {
	mode := normalizeHostFileMode(spec.Mode)
	return hostFileChangeSet{
		Content: !current.Exists || strings.TrimSpace(current.Sha256) != strings.TrimSpace(desiredDigest),
		Mode:    mode != "" && strings.TrimSpace(current.Mode) != mode,
		Owner:   strings.TrimSpace(spec.Owner) != "" && strings.TrimSpace(current.Owner) != strings.TrimSpace(spec.Owner) && strconv.Itoa(current.UID) != strings.TrimSpace(spec.Owner),
		Group:   strings.TrimSpace(spec.Group) != "" && strings.TrimSpace(current.Group) != strings.TrimSpace(spec.Group) && strconv.Itoa(current.GID) != strings.TrimSpace(spec.Group),
	}
}

func normalizeHostFileMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return ""
	}
	mode = strings.TrimPrefix(mode, "0o")
	if len(mode) > 4 {
		mode = mode[len(mode)-4:]
	}
	if parsed, err := strconv.ParseInt(mode, 8, 64); err == nil {
		return fmt.Sprintf("%04o", parsed)
	}
	return mode
}

func digestBytes(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + fmt.Sprintf("%x", sum[:])
}

func hostFileReceiptMeta(node *runNode) (string, string, string) {
	switch normalizeNodeKind(node.Kind) {
	case NodeKindHostFileCopy:
		return "torque.dev/host-file-copy-node/v1", "HostFileCopy", NodeKindHostFileCopy
	default:
		return "torque.dev/host-file-render-node/v1", "HostFileRender", NodeKindHostFileRender
	}
}

func (e *customNodeExecutor) hostFileObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, pathDigest string, state hostFileState, status string) hostFileObserveReceipt {
	apiVersion, kindPrefix, _ := hostFileReceiptMeta(node)
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostFileObserveReceipt{
		APIVersion:       apiVersion,
		Kind:             kindPrefix + "ObserveReceipt",
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
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostFilePlanReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, desiredDigest string, pathDigest string, changes hostFileChangeSet, status string, reason string) hostFilePlanReceipt {
	apiVersion, kindPrefix, operation := hostFileReceiptMeta(node)
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
	sourceDigest := ""
	if operation == NodeKindHostFileCopy {
		sourceDigest = desiredDigest
	}
	backupPathDigest := ""
	if node.Host.Backup || node.Host.RestoreOnDelete || strings.TrimSpace(node.Host.BackupPath) != "" {
		backupPathDigest = digestString(firstNonEmptyString(strings.TrimSpace(node.Host.BackupPath), defaultHostFileBackupPath(node.Host.Path)))
	}
	return hostFilePlanReceipt{
		APIVersion:       apiVersion,
		Kind:             kindPrefix + "PlanReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           status,
		Reason:           reason,
		GuardMode:        guardMode,
		Operation:        operation,
		PathDigest:       pathDigest,
		DesiredDigest:    desiredDigest,
		SourceDigest:     sourceDigest,
		Mode:             normalizeHostFileMode(node.Host.Mode),
		Owner:            strings.TrimSpace(node.Host.Owner),
		Group:            strings.TrimSpace(node.Host.Group),
		Backup:           node.Host.Backup,
		BackupPathDigest: backupPathDigest,
		RestoreOnDelete:  node.Host.RestoreOnDelete,
		SelectedTargets:  selected,
		LockScopes:       lockScopes,
		PolicySources:    policySources,
		Changes:          changes,
		PlannedAt:        time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostFileDiffReceipt(node *runNode, phase string, targetID string, pathDigest string, desiredDigest string, before hostFileState, changes hostFileChangeSet, status string) hostFileDiffReceipt {
	apiVersion, kindPrefix, _ := hostFileReceiptMeta(node)
	return hostFileDiffReceipt{
		APIVersion:  apiVersion,
		Kind:        kindPrefix + "DiffReceipt",
		NodeID:      node.ID,
		TargetID:    targetID,
		Phase:       phase,
		Status:      status,
		PathDigest:  pathDigest,
		Before:      before,
		AfterDigest: desiredDigest,
		AfterMode:   normalizeHostFileMode(node.Host.Mode),
		AfterOwner:  strings.TrimSpace(node.Host.Owner),
		AfterGroup:  strings.TrimSpace(node.Host.Group),
		Changes:     changes,
		DiffQuality: "exact",
		GeneratedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostFileVerifyReceipt(node *runNode, phase string, targetID string, desiredDigest string, pathDigest string, result hostFileOperationResult) hostFileVerifyReceipt {
	apiVersion, kindPrefix, _ := hostFileReceiptMeta(node)
	status := "succeeded"
	reason := "file receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Validation.Error, result.Validation.Stderr, result.Reason, "file receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "file operation skipped")
	}
	actualDigest := result.After.Sha256
	if strings.TrimSpace(actualDigest) == "" {
		actualDigest = result.Before.Sha256
	}
	if status == "succeeded" && strings.TrimSpace(result.Operation) == "apply" && strings.TrimSpace(desiredDigest) != "" && strings.TrimSpace(actualDigest) != strings.TrimSpace(desiredDigest) {
		status = "failed"
		reason = "file digest did not match desired digest"
	}
	return hostFileVerifyReceipt{
		APIVersion:    apiVersion,
		Kind:          kindPrefix + "VerifyReceipt",
		NodeID:        node.ID,
		TargetID:      strings.TrimSpace(targetID),
		Phase:         phase,
		Status:        status,
		Reason:        reason,
		PathDigest:    pathDigest,
		DesiredDigest: desiredDigest,
		ActualDigest:  actualDigest,
		Mode:          result.After.Mode,
		Owner:         result.After.Owner,
		Group:         result.After.Group,
		Changed:       result.Changed,
		Validation:    result.Validation,
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostFileRenderReceipts(node *runNode, phase string, status string, reason string, observe hostFileObserveReceipt, plan hostFilePlanReceipt, diff hostFileDiffReceipt, apply *hostFileOperationResult, verify hostFileVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-file-render-node/v1",
		"kind":       "HostFileRenderNodeArtifact",
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
	e.run.RecordJSONArtifact(node.ID, "host-file-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-file-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-file-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-file-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-file-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) validateHostAdapterOpsGuard(node *runNode, targetID string, operation string) error {
	if e == nil || e.run == nil || e.run.Plan == nil || e.run.Plan.Ops == nil {
		return nil
	}
	ops := e.run.Plan.Ops
	targetID = strings.TrimSpace(targetID)
	if targetID == "" {
		return fmt.Errorf("ops-backed %s requires host.targetId or exactly one selected TargetGraph target", operation)
	}
	if ops.TargetGraph == nil {
		return fmt.Errorf("ops-backed %s requires TargetGraph plan inputs", operation)
	}
	if !stringInSlice(targetID, ops.TargetGraph.Selection.MatchedTargetIDs) {
		return fmt.Errorf("host target %s was not selected by TargetGraph", targetID)
	}
	if !opsFactsContainTarget(ops, targetID) {
		return fmt.Errorf("host target %s has no fresh fact evidence", targetID)
	}
	if !opsLockAllowsTarget(ops, targetID) {
		return fmt.Errorf("host target %s has no held target lock", targetID)
	}
	if !opsPolicyAllowsOperationTarget(ops, targetID, operation) {
		return fmt.Errorf("host target %s has no allow policy decision", targetID)
	}
	return nil
}

type cutoverState struct {
	ObjectID                  string
	CutoverEpoch              string
	IntentDigest              string
	Phase                     string
	PhaseStatus               string
	FenceToken                string
	CommitMarker              string
	StabilizationStartedAtNS  int64
	StabilizationDeadlineAtNS int64
	AmbiguityStatus           string
	UpdatedAtNS               int64
}

type sqlDialect struct {
	name        string
	driver      string
	placeholder func(int) string
}

func dialectFor(driver string) (sqlDialect, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql":
		return sqlDialect{
			name:   "postgres",
			driver: "postgres",
			placeholder: func(i int) string {
				return fmt.Sprintf("$%d", i)
			},
		}, nil
	case "mysql", "mariadb":
		return sqlDialect{
			name:   "mysql",
			driver: "mysql",
			placeholder: func(i int) string {
				return "?"
			},
		}, nil
	case "sqlite", "":
		return sqlDialect{
			name:   "sqlite",
			driver: "sqlite",
			placeholder: func(i int) string {
				return "?"
			},
		}, nil
	default:
		return sqlDialect{}, fmt.Errorf("unsupported database driver %q", driver)
	}
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (e *customNodeExecutor) runDBCutoverNode(ctx context.Context, node *runNode, command string) error {
	if !strings.EqualFold(command, "apply") {
		return nil
	}
	dialect, err := dialectFor(node.Database.Driver)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	dsn, err := resolveDatabaseDSN(node.Database)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	table, err := sanitizeSQLIdent(node.Database.MetadataTable)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("metadata table: %w", err))
	}
	db, err := sql.Open(dialect.driver, dsn)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("open database: %w", err))
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("ping database: %w", err))
	}
	if err := ensureCutoverTable(ctx, db, dialect, table); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	objectID := node.ID
	intent := strings.TrimSpace(node.EffectiveInputHash)
	if intent == "" {
		intent = strings.TrimSpace(node.Name)
	}
	state, err := loadCutoverState(ctx, db, dialect, table, objectID)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	if state == nil {
		state = &cutoverState{
			ObjectID:     objectID,
			CutoverEpoch: fmt.Sprintf("%s/%s", e.run.RunID, node.ID),
			IntentDigest: intent,
			Phase:        "",
			PhaseStatus:  "",
			FenceToken:   fmt.Sprintf("fence:%s/%d", e.run.RunID, node.Attempt),
			UpdatedAtNS:  time.Now().UTC().UnixNano(),
		}
	} else if strings.TrimSpace(state.IntentDigest) != "" && state.IntentDigest != intent && !cutoverComplete(state) {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("cutover intent changed while previous epoch %s is incomplete", state.CutoverEpoch))
	}
	if strings.TrimSpace(state.IntentDigest) == "" {
		state.IntentDigest = intent
	}
	if strings.TrimSpace(state.CutoverEpoch) == "" {
		state.CutoverEpoch = fmt.Sprintf("%s/%s", e.run.RunID, node.ID)
	}
	if strings.TrimSpace(state.FenceToken) == "" {
		state.FenceToken = fmt.Sprintf("fence:%s/%d", e.run.RunID, node.Attempt)
	}
	if cutoverComplete(state) {
		e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
			"apiVersion": "torque.dev/db-node/v1",
			"kind":       "DBNodeArtifact",
			"nodeId":     node.ID,
			"nodeKind":   normalizeNodeKind(node.Kind),
			"driver":     strings.TrimSpace(node.Database.Driver),
			"status":     "success",
			"phase":      "finalize",
			"state":      state,
		})
		return nil
	}

	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "prepare", strings.TrimSpace(node.Database.PrepareSQL), false); err != nil {
		return err
	}
	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "arm", strings.TrimSpace(node.Database.ArmSQL), false); err != nil {
		return err
	}
	if err := e.runCutoverCommit(ctx, db, dialect, table, node, state); err != nil {
		return err
	}
	if err := e.runCutoverStabilize(ctx, db, dialect, table, node, state); err != nil {
		return err
	}
	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "finalize", strings.TrimSpace(node.Database.FinalizeSQL), true); err != nil {
		return err
	}
	e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
		"apiVersion": "torque.dev/db-node/v1",
		"kind":       "DBNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"driver":     strings.TrimSpace(node.Database.Driver),
		"status":     "success",
		"phase":      "finalize",
		"state":      state,
	})
	return nil
}

func cutoverComplete(state *cutoverState) bool {
	return state != nil && state.Phase == "finalize" && nodeStepSucceeded(state.PhaseStatus)
}

func (e *customNodeExecutor) runCutoverPhase(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState, phase string, sqlText string, terminal bool) error {
	if state == nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("cutover state is required"))
	}
	if cutoverPhaseDone(state, phase) {
		return nil
	}
	cursor := cutoverCursor(state, phase)
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	if strings.TrimSpace(sqlText) != "" {
		if err := execSQLScript(ctx, db, sqlText); err != nil {
			return e.cutoverPhaseFailure(node, phase, cursor, "DB_CUTOVER_FAILED", err)
		}
	}
	state.Phase = phase
	state.PhaseStatus = "success"
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if terminal {
		state.FenceToken = ""
	}
	if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"cursor":  cursor,
		"message": "success",
	}, nil)
	return nil
}

func (e *customNodeExecutor) runCutoverCommit(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState) error {
	if cutoverPhaseDone(state, "commit") {
		return nil
	}
	cursor := cutoverCursor(state, "commit")
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "commit", map[string]any{"phase": "commit", "cursor": cursor}, nil)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("begin cutover commit: %w", err))
	}
	if err := execSQLScript(ctx, tx, node.Database.CommitSQL); err != nil {
		_ = tx.Rollback()
		return e.cutoverPhaseFailure(node, "commit", cursor, "DB_CUTOVER_FAILED", err)
	}
	state.Phase = "commit"
	state.PhaseStatus = "success"
	if strings.TrimSpace(state.CommitMarker) == "" {
		state.CommitMarker = fmt.Sprintf("commit:%s", state.CutoverEpoch)
	}
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if err := upsertCutoverStateTx(ctx, tx, dialect, table, state); err != nil {
		_ = tx.Rollback()
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	if err := tx.Commit(); err != nil {
		state.AmbiguityStatus = "commit-uncertain"
		state.PhaseStatus = "ambiguous"
		state.UpdatedAtNS = time.Now().UTC().UnixNano()
		_ = upsertCutoverState(ctx, db, dialect, table, state)
		return e.cutoverPhaseFailure(node, "commit", cursor, "DB_CUTOVER_AMBIGUOUS", err)
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   "commit",
		"status":  "success",
		"cursor":  cutoverCursor(state, "commit"),
		"message": "success",
	}, nil)
	return nil
}

func (e *customNodeExecutor) runCutoverStabilize(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState) error {
	if cutoverPhaseDone(state, "stabilize") {
		return nil
	}
	cursor := cutoverCursor(state, "stabilize")
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "stabilize", map[string]any{"phase": "stabilize", "cursor": cursor}, nil)
	now := time.Now().UTC()
	if state.StabilizationStartedAtNS == 0 {
		state.StabilizationStartedAtNS = now.UnixNano()
		window := time.Duration(0)
		if node.Database.StabilizationWindow != nil {
			window = *node.Database.StabilizationWindow
		}
		state.StabilizationDeadlineAtNS = now.Add(window).UnixNano()
		state.Phase = "stabilize"
		state.PhaseStatus = "running"
		state.UpdatedAtNS = now.UnixNano()
		if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
	}
	deadline := time.Unix(0, state.StabilizationDeadlineAtNS)
	if state.StabilizationDeadlineAtNS == 0 {
		deadline = now
	}
	for {
		ok, msg, err := cutoverVerify(ctx, db, node.Database.VerifySQL)
		if err != nil {
			return e.cutoverPhaseFailure(node, "stabilize", cursor, "DB_CUTOVER_FAILED", err)
		}
		if ok && !time.Now().UTC().Before(deadline) {
			state.Phase = "stabilize"
			state.PhaseStatus = "success"
			state.UpdatedAtNS = time.Now().UTC().UnixNano()
			if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
				return wrapNodeErr(node.ResolvedRelease, err)
			}
			e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
				"phase":   "stabilize",
				"status":  "success",
				"cursor":  cutoverCursor(state, "stabilize"),
				"message": msg,
			}, nil)
			return nil
		}
		if !ok && !time.Now().UTC().Before(deadline) {
			return e.cutoverPhaseFailure(node, "stabilize", cursor, "DB_CUTOVER_FAILED", fmt.Errorf("stabilization verification failed: %s", msg))
		}
		wait := 500 * time.Millisecond
		if rem := time.Until(deadline); rem > 0 && rem < wait {
			wait = rem
		}
		if wait <= 0 {
			wait = 50 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (e *customNodeExecutor) cutoverPhaseFailure(node *runNode, phase string, cursor map[string]any, class string, err error) error {
	msg := err.Error()
	runErr := &RunError{Class: class, Message: msg, Digest: computeRunErrorDigest(class, msg)}
	e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
		"apiVersion": "torque.dev/db-node/v1",
		"kind":       "DBNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"driver":     strings.TrimSpace(node.Database.Driver),
		"status":     "failure",
		"phase":      phase,
		"message":    msg,
		"cursor":     cursor,
		"errorClass": class,
	})
	e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":   phase,
		"status":  "failure",
		"cursor":  cursor,
		"message": msg,
	}, runErr, true)
	return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s phase %s: %w", normalizeNodeKind(node.Kind), phase, err))
}

func cutoverPhaseDone(state *cutoverState, phase string) bool {
	if state == nil || !nodeStepSucceeded(state.PhaseStatus) {
		return false
	}
	order := map[string]int{"prepare": 1, "arm": 2, "commit": 3, "stabilize": 4, "finalize": 5}
	return order[strings.TrimSpace(state.Phase)] >= order[phase]
}

func cutoverCursor(state *cutoverState, phase string) map[string]any {
	if state == nil {
		return map[string]any{"phase": phase}
	}
	return map[string]any{
		"phase":          phase,
		"cutoverEpoch":   state.CutoverEpoch,
		"intentDigest":   state.IntentDigest,
		"fenceToken":     state.FenceToken,
		"commitMarker":   state.CommitMarker,
		"ambiguityState": state.AmbiguityStatus,
	}
}

func resolveDatabaseDSN(spec DatabaseSpec) (string, error) {
	if strings.TrimSpace(spec.DSN) != "" {
		return strings.TrimSpace(spec.DSN), nil
	}
	if env := strings.TrimSpace(spec.DSNEnv); env != "" {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("environment variable %s is not set", env)
	}
	return "", fmt.Errorf("database dsn is required")
}

func sanitizeSQLIdent(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("identifier is required")
	}
	for _, part := range strings.Split(v, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("empty identifier segment")
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				continue
			}
			return "", fmt.Errorf("invalid identifier %q", v)
		}
	}
	return v, nil
}

func ensureCutoverTable(ctx context.Context, db *sql.DB, dialect sqlDialect, table string) error {
	stmt := cutoverTableDDL(dialect, table)
	if err := execSQLScript(ctx, db, stmt); err != nil {
		return fmt.Errorf("ensure cutover table %s: %w", table, err)
	}
	return nil
}

func loadCutoverState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, objectID string) (*cutoverState, error) {
	q := fmt.Sprintf(`SELECT object_id, cutover_epoch, intent_digest, phase, phase_status, fence_token, commit_marker,
stabilization_started_at_ns, stabilization_deadline_at_ns, ambiguity_status, updated_at_ns
FROM %s WHERE object_id = %s`, table, dialect.placeholder(1))
	var state cutoverState
	err := db.QueryRowContext(ctx, q, objectID).Scan(
		&state.ObjectID,
		&state.CutoverEpoch,
		&state.IntentDigest,
		&state.Phase,
		&state.PhaseStatus,
		&state.FenceToken,
		&state.CommitMarker,
		&state.StabilizationStartedAtNS,
		&state.StabilizationDeadlineAtNS,
		&state.AmbiguityStatus,
		&state.UpdatedAtNS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load cutover state: %w", err)
	}
	return &state, nil
}

func upsertCutoverState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, state *cutoverState) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cutover state tx: %w", err)
	}
	if err := upsertCutoverStateTx(ctx, tx, dialect, table, state); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cutover state tx: %w", err)
	}
	return nil
}

func upsertCutoverStateTx(ctx context.Context, tx *sql.Tx, dialect sqlDialect, table string, state *cutoverState) error {
	if state == nil {
		return fmt.Errorf("cutover state is required")
	}
	cols := []string{
		"object_id", "cutover_epoch", "intent_digest", "phase", "phase_status",
		"fence_token", "commit_marker", "stabilization_started_at_ns",
		"stabilization_deadline_at_ns", "ambiguity_status", "updated_at_ns",
	}
	ph := make([]string, 0, len(cols))
	for i := range cols {
		ph = append(ph, dialect.placeholder(i+1))
	}
	updateCols := []string{
		"cutover_epoch", "intent_digest", "phase", "phase_status", "fence_token",
		"commit_marker", "stabilization_started_at_ns", "stabilization_deadline_at_ns",
		"ambiguity_status", "updated_at_ns",
	}
	stmt := cutoverUpsertStmt(dialect, table, cols, ph, updateCols)
	_, err := tx.ExecContext(ctx, stmt,
		state.ObjectID,
		state.CutoverEpoch,
		state.IntentDigest,
		state.Phase,
		state.PhaseStatus,
		state.FenceToken,
		state.CommitMarker,
		state.StabilizationStartedAtNS,
		state.StabilizationDeadlineAtNS,
		state.AmbiguityStatus,
		state.UpdatedAtNS,
	)
	if err != nil {
		return fmt.Errorf("upsert cutover state: %w", err)
	}
	return nil
}

func cutoverVerify(ctx context.Context, db *sql.DB, query string) (bool, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return true, "no verify query configured", nil
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return false, "", err
	}
	if !rows.Next() {
		return false, "verify query returned no rows", nil
	}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return false, "", err
	}
	if len(raw) == 0 {
		return false, "verify query returned no columns", nil
	}
	ok := truthyDBValue(raw[0])
	msg := fmt.Sprintf("verify=%v", ok)
	if len(raw) == 1 {
		if payload, err := json.Marshal(raw[0]); err == nil {
			msg = string(payload)
		}
	} else if payload, err := json.Marshal(raw); err == nil {
		msg = string(payload)
	}
	return ok, msg, nil
}

func truthyDBValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int32:
		return x != 0
	case int:
		return x != 0
	case int8:
		return x != 0
	case int16:
		return x != 0
	case float64:
		return x != 0
	case float32:
		return x != 0
	case uint:
		return x != 0
	case uint8:
		return x != 0
	case uint16:
		return x != 0
	case uint32:
		return x != 0
	case uint64:
		return x != 0
	case []byte:
		return truthyString(string(x))
	case string:
		return truthyString(x)
	default:
		return false
	}
}

func truthyString(v string) bool {
	if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		return n != 0
	}
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "1", "t", "true", "y", "yes", "ready", "ok", "success":
		return true
	default:
		return false
	}
}

func cutoverTableDDL(dialect sqlDialect, table string) string {
	switch dialect.name {
	case "mysql":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id VARCHAR(255) PRIMARY KEY,
cutover_epoch VARCHAR(255) NOT NULL,
intent_digest VARCHAR(255) NOT NULL,
phase VARCHAR(64) NOT NULL,
phase_status VARCHAR(64) NOT NULL,
fence_token VARCHAR(255) NOT NULL DEFAULT '',
commit_marker VARCHAR(255) NOT NULL DEFAULT '',
stabilization_started_at_ns BIGINT NOT NULL DEFAULT 0,
stabilization_deadline_at_ns BIGINT NOT NULL DEFAULT 0,
ambiguity_status VARCHAR(64) NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id TEXT PRIMARY KEY,
cutover_epoch TEXT NOT NULL,
intent_digest TEXT NOT NULL,
phase TEXT NOT NULL,
phase_status TEXT NOT NULL,
fence_token TEXT NOT NULL DEFAULT '',
commit_marker TEXT NOT NULL DEFAULT '',
stabilization_started_at_ns BIGINT NOT NULL DEFAULT 0,
stabilization_deadline_at_ns BIGINT NOT NULL DEFAULT 0,
ambiguity_status TEXT NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	}
}

func cutoverUpsertStmt(dialect sqlDialect, table string, cols []string, ph []string, updateCols []string) string {
	assignments := make([]string, 0, len(updateCols))
	switch dialect.name {
	case "mysql":
		for _, col := range updateCols {
			assignments = append(assignments, fmt.Sprintf("%s = VALUES(%s)", col, col))
		}
		return fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
ON DUPLICATE KEY UPDATE %s`,
			table,
			strings.Join(cols, ", "),
			strings.Join(ph, ", "),
			strings.Join(assignments, ", "),
		)
	default:
		for _, col := range updateCols {
			assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
		}
		return fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
ON CONFLICT (object_id) DO UPDATE SET %s`,
			table,
			strings.Join(cols, ", "),
			strings.Join(ph, ", "),
			strings.Join(assignments, ", "),
		)
	}
}

func execSQLScript(ctx context.Context, execer sqlExecer, script string) error {
	stmts := splitSQLStatements(script)
	for _, stmt := range stmts {
		if _, err := execer.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func splitSQLStatements(script string) []string {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil
	}
	var out []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if inLineComment {
			buf.WriteRune(r)
			if r == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			buf.WriteRune(r)
			if r == '*' && next == '/' {
				i++
				buf.WriteRune(next)
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble && !inBacktick {
			if r == '-' && next == '-' {
				buf.WriteRune(r)
				i++
				buf.WriteRune(next)
				inLineComment = true
				continue
			}
			if r == '#' {
				buf.WriteRune(r)
				inLineComment = true
				continue
			}
			if r == '/' && next == '*' {
				buf.WriteRune(r)
				i++
				buf.WriteRune(next)
				inBlockComment = true
				continue
			}
		}
		switch r {
		case '\'':
			buf.WriteRune(r)
			if !inDouble && !inBacktick {
				if inSingle && next == '\'' {
					i++
					buf.WriteRune(next)
					continue
				}
				inSingle = !inSingle
			}
		case '"':
			buf.WriteRune(r)
			if !inSingle && !inBacktick {
				if inDouble && next == '"' {
					i++
					buf.WriteRune(next)
					continue
				}
				inDouble = !inDouble
			}
		case '`':
			buf.WriteRune(r)
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case ';':
			if inSingle || inDouble || inBacktick {
				buf.WriteRune(r)
				continue
			}
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				out = append(out, stmt)
			}
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	stmt := strings.TrimSpace(buf.String())
	if stmt != "" {
		out = append(out, stmt)
	}
	return out
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, env[k]))
	}
	return out
}
