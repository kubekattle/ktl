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

type hostUserAccountState struct {
	Exists              bool     `json:"exists"`
	Name                string   `json:"name,omitempty"`
	UID                 int      `json:"uid,omitempty"`
	GID                 int      `json:"gid,omitempty"`
	Group               string   `json:"group,omitempty"`
	Home                string   `json:"home,omitempty"`
	Shell               string   `json:"shell,omitempty"`
	Comment             string   `json:"comment,omitempty"`
	SupplementaryGroups []string `json:"supplementaryGroups,omitempty"`
}

type hostUserGroupState struct {
	Exists  bool     `json:"exists"`
	Name    string   `json:"name,omitempty"`
	GID     int      `json:"gid,omitempty"`
	Members []string `json:"members,omitempty"`
}

type hostUserState struct {
	User  hostUserAccountState `json:"user"`
	Group hostUserGroupState   `json:"group"`
}

type hostUserChangeSet struct {
	User    bool `json:"user"`
	Group   bool `json:"group"`
	UID     bool `json:"uid,omitempty"`
	GID     bool `json:"gid,omitempty"`
	Home    bool `json:"home,omitempty"`
	Shell   bool `json:"shell,omitempty"`
	Comment bool `json:"comment,omitempty"`
	Groups  bool `json:"groups,omitempty"`
}

type hostUserCommandReceipt struct {
	Action        string `json:"action"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
}

type hostUserOperationResult struct {
	APIVersion   string                   `json:"apiVersion"`
	Kind         string                   `json:"kind"`
	Operation    string                   `json:"operation"`
	Status       string                   `json:"status"`
	Reason       string                   `json:"reason,omitempty"`
	TargetDigest string                   `json:"targetDigest,omitempty"`
	UserDigest   string                   `json:"userDigest,omitempty"`
	GroupDigest  string                   `json:"groupDigest,omitempty"`
	DesiredState string                   `json:"desiredState,omitempty"`
	Changed      bool                     `json:"changed"`
	Changes      hostUserChangeSet        `json:"changes"`
	Before       hostUserState            `json:"before"`
	After        hostUserState            `json:"after"`
	Commands     []hostUserCommandReceipt `json:"commands,omitempty"`
	Error        string                   `json:"error,omitempty"`
	CompletedAt  string                   `json:"completedAt"`
}

type hostUserObserveReceipt struct {
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
	UserDigest       string        `json:"userDigest,omitempty"`
	GroupDigest      string        `json:"groupDigest,omitempty"`
	State            hostUserState `json:"state"`
	ObservedAt       string        `json:"observedAt"`
}

type hostUserPlanReceipt struct {
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
	UserDigest      string            `json:"userDigest,omitempty"`
	GroupDigest     string            `json:"groupDigest,omitempty"`
	UserName        string            `json:"user,omitempty"`
	GroupName       string            `json:"groupName,omitempty"`
	UserGroup       string            `json:"userGroup,omitempty"`
	DesiredState    string            `json:"desiredState"`
	UID             *int              `json:"uid,omitempty"`
	GID             *int              `json:"gid,omitempty"`
	HomeDigest      string            `json:"homeDigest,omitempty"`
	Shell           string            `json:"shell,omitempty"`
	CommentDigest   string            `json:"commentDigest,omitempty"`
	Groups          []string          `json:"groups,omitempty"`
	CreateHome      bool              `json:"createHome,omitempty"`
	RemoveHome      bool              `json:"removeHome,omitempty"`
	RemoveOnDelete  bool              `json:"removeOnDelete,omitempty"`
	System          bool              `json:"system,omitempty"`
	SelectedTargets []string          `json:"selectedTargets,omitempty"`
	LockScopes      []string          `json:"lockScopes,omitempty"`
	PolicySources   []string          `json:"policySources,omitempty"`
	Changes         hostUserChangeSet `json:"changes"`
	PlannedAt       string            `json:"plannedAt"`
}

type hostUserDiffReceipt struct {
	APIVersion   string            `json:"apiVersion"`
	Kind         string            `json:"kind"`
	NodeID       string            `json:"nodeId"`
	TargetID     string            `json:"targetId,omitempty"`
	Phase        string            `json:"phase"`
	Status       string            `json:"status"`
	UserDigest   string            `json:"userDigest,omitempty"`
	GroupDigest  string            `json:"groupDigest,omitempty"`
	Before       hostUserState     `json:"before"`
	DesiredState string            `json:"desiredState"`
	Changes      hostUserChangeSet `json:"changes"`
	DiffQuality  string            `json:"diffQuality"`
	GeneratedAt  string            `json:"generatedAt"`
}

type hostUserVerifyReceipt struct {
	APIVersion   string                   `json:"apiVersion"`
	Kind         string                   `json:"kind"`
	NodeID       string                   `json:"nodeId"`
	TargetID     string                   `json:"targetId,omitempty"`
	Phase        string                   `json:"phase"`
	Status       string                   `json:"status"`
	Reason       string                   `json:"reason,omitempty"`
	UserDigest   string                   `json:"userDigest,omitempty"`
	GroupDigest  string                   `json:"groupDigest,omitempty"`
	DesiredState string                   `json:"desiredState,omitempty"`
	UserExists   bool                     `json:"userExists"`
	GroupExists  bool                     `json:"groupExists"`
	UID          int                      `json:"uid,omitempty"`
	GID          int                      `json:"gid,omitempty"`
	GroupGID     int                      `json:"groupGid,omitempty"`
	Changed      bool                     `json:"changed"`
	Commands     []hostUserCommandReceipt `json:"commands,omitempty"`
	VerifiedAt   string                   `json:"verifiedAt"`
}

func (e *customNodeExecutor) runHostUserManageNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	phase := "host-user"
	operation := "apply"
	if strings.EqualFold(command, "delete") {
		phase = "delete-host-user"
		operation = "delete"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	userDigest := digestString(spec.UserName)
	groupDigest := digestString(spec.GroupName)
	targetID, guardMode, selected := e.hostCommandTargetContext(node)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		state := hostUserState{
			User:  hostUserAccountState{Name: strings.TrimSpace(spec.UserName)},
			Group: hostUserGroupState{Name: strings.TrimSpace(spec.GroupName)},
		}
		observe := e.hostUserObserveReceipt(node, phase, targetID, guardMode, selected, "", userDigest, groupDigest, state, "skipped")
		plan := e.hostUserPlanReceipt(node, phase, targetID, guardMode, selected, userDigest, groupDigest, hostUserChangeSet{}, "skipped", reason)
		diff := e.hostUserDiffReceipt(node, phase, targetID, userDigest, groupDigest, state, hostUserChangeSet{}, "skipped")
		verify := e.hostUserVerifyReceipt(node, phase, targetID, userDigest, groupDigest, hostUserOperationResult{Status: "skipped", Reason: reason})
		e.recordHostUserReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	observeResult, err := e.runHostUserOperation(ctx, transportClient, hostUserPayload(spec, "observe"))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observe := e.hostUserObserveReceipt(node, phase, targetID, guardMode, selected, targetDigest, userDigest, groupDigest, observeResult.After, observeResult.Status)
	changes := hostUserChanges(observeResult.After, spec, operation)
	plan := e.hostUserPlanReceipt(node, phase, targetID, guardMode, selected, userDigest, groupDigest, changes, "planned", "eligible")
	diff := e.hostUserDiffReceipt(node, phase, targetID, userDigest, groupDigest, observeResult.After, changes, "planned")
	if guardErr := e.validateHostAdapterOpsGuard(node, targetID, NodeKindHostUserManage); guardErr != nil {
		plan.Status = "blocked"
		plan.Reason = guardErr.Error()
		verify := e.hostUserVerifyReceipt(node, phase, targetID, userDigest, groupDigest, hostUserOperationResult{Status: "blocked", Reason: guardErr.Error(), Before: observeResult.After, After: observeResult.After})
		e.recordHostUserReceipts(node, phase, "blocked", guardErr.Error(), observe, plan, diff, nil, verify)
		runErr := &RunError{Class: "HOST_USER_BLOCKED", Message: guardErr.Error(), Digest: computeRunErrorDigest("HOST_USER_BLOCKED", guardErr.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, guardErr.Error(), map[string]any{
			"phase":    phase,
			"status":   "blocked",
			"targetId": targetID,
			"cursor":   cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host user phase %s: %w", phase, guardErr))
	}

	var result hostUserOperationResult
	if operation == "delete" {
		result, err = e.runHostUserOperation(ctx, transportClient, hostUserPayload(spec, "delete"))
	} else {
		result, err = e.runHostUserOperation(ctx, transportClient, hostUserPayload(spec, "apply"))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	result.UserDigest = userDigest
	result.GroupDigest = groupDigest
	if result.Changes == (hostUserChangeSet{}) {
		result.Changes = changes
	}
	verify := e.hostUserVerifyReceipt(node, phase, targetID, userDigest, groupDigest, result)
	e.recordHostUserReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "host user operation failed")
		runErr := &RunError{Class: "HOST_USER_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_USER_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host user phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":  phase,
		"status": "success",
		"cursor": cursor,
		"result": result,
	}, nil)
	return nil
}

func (e *customNodeExecutor) runHostUserOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (hostUserOperationResult, error) {
	command, err := hostUserPythonCommand(payload)
	if err != nil {
		return hostUserOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result hostUserOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return hostUserOperationResult{}, fmt.Errorf("decode host.user.manage receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/host-user-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "HostUserOperationReceipt"
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

func hostUserPayload(spec HostCommandSpec, operation string) map[string]any {
	return map[string]any{
		"operation":      strings.TrimSpace(operation),
		"user":           strings.TrimSpace(spec.UserName),
		"groupName":      strings.TrimSpace(spec.GroupName),
		"userGroup":      strings.TrimSpace(spec.UserGroup),
		"state":          hostUserDesiredState(spec),
		"uid":            spec.UID,
		"gid":            spec.GID,
		"home":           strings.TrimSpace(spec.Home),
		"shell":          strings.TrimSpace(spec.Shell),
		"comment":        strings.TrimSpace(spec.Comment),
		"groups":         append([]string(nil), spec.Groups...),
		"createHome":     spec.CreateHome,
		"removeHome":     spec.RemoveHome,
		"removeOnDelete": spec.RemoveOnDelete,
		"system":         spec.System,
	}
}

func hostUserPythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_USER_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + hostUserPythonScript + "\nPY", nil
}

func hostUserDesiredState(spec HostCommandSpec) string {
	state := strings.ToLower(strings.TrimSpace(spec.State))
	if state == "" {
		return "present"
	}
	return state
}

func hostUserPrimaryGroup(spec HostCommandSpec) string {
	if strings.TrimSpace(spec.UserGroup) != "" {
		return strings.TrimSpace(spec.UserGroup)
	}
	return strings.TrimSpace(spec.GroupName)
}

func hostUserChanges(current hostUserState, spec HostCommandSpec, operation string) hostUserChangeSet {
	if operation == "delete" {
		if !spec.RemoveOnDelete {
			return hostUserChangeSet{}
		}
		return hostUserChangeSet{User: current.User.Exists, Group: current.Group.Exists}
	}
	if hostUserDesiredState(spec) == "absent" {
		return hostUserChangeSet{User: current.User.Exists, Group: current.Group.Exists}
	}
	changes := hostUserChangeSet{}
	if strings.TrimSpace(spec.GroupName) != "" {
		changes.Group = !current.Group.Exists
		if current.Group.Exists && spec.GID != nil && current.Group.GID != *spec.GID {
			changes.GID = true
		}
	}
	if strings.TrimSpace(spec.UserName) != "" {
		changes.User = !current.User.Exists
		if current.User.Exists {
			if spec.UID != nil && current.User.UID != *spec.UID {
				changes.UID = true
			}
			if primary := hostUserPrimaryGroup(spec); primary != "" && current.User.Group != primary {
				changes.GID = true
			}
			if strings.TrimSpace(spec.Home) != "" && current.User.Home != strings.TrimSpace(spec.Home) {
				changes.Home = true
			}
			if strings.TrimSpace(spec.Shell) != "" && current.User.Shell != strings.TrimSpace(spec.Shell) {
				changes.Shell = true
			}
			if strings.TrimSpace(spec.Comment) != "" && current.User.Comment != strings.TrimSpace(spec.Comment) {
				changes.Comment = true
			}
			if len(spec.Groups) > 0 && !stringSlicesEqualTrimmed(current.User.SupplementaryGroups, spec.Groups) {
				changes.Groups = true
			}
		}
	}
	return changes
}

func stringSlicesEqualTrimmed(a, b []string) bool {
	aa := normalizeStringList(a)
	bb := normalizeStringList(b)
	if len(aa) != len(bb) {
		return false
	}
	for i := range aa {
		if aa[i] != bb[i] {
			return false
		}
	}
	return true
}

func normalizeStringList(in []string) []string {
	out := make([]string, 0, len(in))
	seen := map[string]struct{}{}
	for _, item := range in {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	sort.Strings(out)
	return out
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	copied := *value
	return &copied
}

func (e *customNodeExecutor) hostUserObserveReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, targetDigest string, userDigest string, groupDigest string, state hostUserState, status string) hostUserObserveReceipt {
	selected = append([]string(nil), selected...)
	sort.Strings(selected)
	return hostUserObserveReceipt{
		APIVersion:       "torque.dev/host-user-node/v1",
		Kind:             "HostUserObserveReceipt",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		TargetID:         targetID,
		Phase:            phase,
		Status:           firstNonEmptyString(strings.TrimSpace(status), "observed"),
		GuardMode:        guardMode,
		SelectedTargetID: targetID,
		SelectedTargets:  selected,
		TargetDigest:     strings.TrimSpace(targetDigest),
		UserDigest:       strings.TrimSpace(userDigest),
		GroupDigest:      strings.TrimSpace(groupDigest),
		State:            state,
		ObservedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostUserPlanReceipt(node *runNode, phase string, targetID string, guardMode string, selected []string, userDigest string, groupDigest string, changes hostUserChangeSet, status string, reason string) hostUserPlanReceipt {
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
	groups := normalizeStringList(node.Host.Groups)
	return hostUserPlanReceipt{
		APIVersion:      "torque.dev/host-user-node/v1",
		Kind:            "HostUserPlanReceipt",
		NodeID:          node.ID,
		NodeKind:        normalizeNodeKind(node.Kind),
		TargetID:        targetID,
		Phase:           phase,
		Status:          status,
		Reason:          reason,
		GuardMode:       guardMode,
		Operation:       NodeKindHostUserManage,
		UserDigest:      userDigest,
		GroupDigest:     groupDigest,
		UserName:        strings.TrimSpace(node.Host.UserName),
		GroupName:       strings.TrimSpace(node.Host.GroupName),
		UserGroup:       hostUserPrimaryGroup(node.Host),
		DesiredState:    hostUserDesiredState(node.Host),
		UID:             cloneIntPtr(node.Host.UID),
		GID:             cloneIntPtr(node.Host.GID),
		HomeDigest:      digestString(node.Host.Home),
		Shell:           strings.TrimSpace(node.Host.Shell),
		CommentDigest:   digestString(node.Host.Comment),
		Groups:          groups,
		CreateHome:      node.Host.CreateHome,
		RemoveHome:      node.Host.RemoveHome,
		RemoveOnDelete:  node.Host.RemoveOnDelete,
		System:          node.Host.System,
		SelectedTargets: selected,
		LockScopes:      lockScopes,
		PolicySources:   policySources,
		Changes:         changes,
		PlannedAt:       time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostUserDiffReceipt(node *runNode, phase string, targetID string, userDigest string, groupDigest string, before hostUserState, changes hostUserChangeSet, status string) hostUserDiffReceipt {
	return hostUserDiffReceipt{
		APIVersion:   "torque.dev/host-user-node/v1",
		Kind:         "HostUserDiffReceipt",
		NodeID:       node.ID,
		TargetID:     targetID,
		Phase:        phase,
		Status:       status,
		UserDigest:   userDigest,
		GroupDigest:  groupDigest,
		Before:       before,
		DesiredState: hostUserDesiredState(node.Host),
		Changes:      changes,
		DiffQuality:  "exact",
		GeneratedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) hostUserVerifyReceipt(node *runNode, phase string, targetID string, userDigest string, groupDigest string, result hostUserOperationResult) hostUserVerifyReceipt {
	status := "succeeded"
	reason := "user receipt succeeded"
	if !nodeStepSucceeded(result.Status) {
		status = "failed"
		reason = firstNonEmptyString(result.Error, result.Reason, "user receipt failed")
	} else if strings.TrimSpace(result.Status) == "skipped" {
		status = "skipped"
		reason = firstNonEmptyString(result.Reason, "user operation skipped")
	}
	desiredState := firstNonEmptyString(strings.TrimSpace(result.DesiredState), hostUserDesiredState(node.Host))
	if status == "succeeded" {
		if desiredState == "absent" {
			if strings.TrimSpace(node.Host.UserName) != "" && result.After.User.Exists {
				status = "failed"
				reason = "user remained present"
			}
			if strings.TrimSpace(node.Host.GroupName) != "" && result.After.Group.Exists {
				status = "failed"
				reason = "group remained present"
			}
		} else {
			if strings.TrimSpace(node.Host.GroupName) != "" {
				if !result.After.Group.Exists {
					status = "failed"
					reason = "group was not present"
				} else if node.Host.GID != nil && result.After.Group.GID != *node.Host.GID {
					status = "failed"
					reason = "group gid did not match desired gid"
				}
			}
			if strings.TrimSpace(node.Host.UserName) != "" {
				if !result.After.User.Exists {
					status = "failed"
					reason = "user was not present"
				} else if node.Host.UID != nil && result.After.User.UID != *node.Host.UID {
					status = "failed"
					reason = "user uid did not match desired uid"
				} else if primary := hostUserPrimaryGroup(node.Host); primary != "" && result.After.User.Group != primary {
					status = "failed"
					reason = "user primary group did not match desired group"
				}
			}
		}
	}
	return hostUserVerifyReceipt{
		APIVersion:   "torque.dev/host-user-node/v1",
		Kind:         "HostUserVerifyReceipt",
		NodeID:       node.ID,
		TargetID:     strings.TrimSpace(targetID),
		Phase:        phase,
		Status:       status,
		Reason:       reason,
		UserDigest:   userDigest,
		GroupDigest:  groupDigest,
		DesiredState: desiredState,
		UserExists:   result.After.User.Exists,
		GroupExists:  result.After.Group.Exists,
		UID:          result.After.User.UID,
		GID:          result.After.User.GID,
		GroupGID:     result.After.Group.GID,
		Changed:      result.Changed,
		Commands:     append([]hostUserCommandReceipt(nil), result.Commands...),
		VerifiedAt:   time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordHostUserReceipts(node *runNode, phase string, status string, reason string, observe hostUserObserveReceipt, plan hostUserPlanReceipt, diff hostUserDiffReceipt, apply *hostUserOperationResult, verify hostUserVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/host-user-node/v1",
		"kind":       "HostUserNodeArtifact",
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
	e.run.RecordJSONArtifact(node.ID, "host-user-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "host-user-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "host-user-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "host-user-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "host-user-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

const hostUserPythonScript = `
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

def require_commands(names):
    missing = [name for name in names if not have(name)]
    if missing:
        raise RuntimeError("missing required user management commands: " + ", ".join(missing))

def parse_int(value):
    if value is None or value == "":
        return None
    return int(value)

def normalize_groups(values):
    out = []
    seen = set()
    for value in values or []:
        value = str(value or "").strip()
        if not value or value in seen:
            continue
        seen.add(value)
        out.append(value)
    out.sort()
    return out

def getent(database, key):
    if key is None or str(key).strip() == "":
        return ""
    proc = run("query-" + database, ["getent", database, str(key)])
    if proc.returncode != 0:
        return ""
    return proc.stdout.strip().splitlines()[0] if proc.stdout.strip() else ""

def parse_passwd(line):
    if not line:
        return {"exists": False}
    parts = line.split(":", 6)
    if len(parts) < 7:
        return {"exists": False}
    return {
        "exists": True,
        "name": parts[0],
        "uid": int(parts[2]),
        "gid": int(parts[3]),
        "comment": parts[4],
        "home": parts[5],
        "shell": parts[6],
    }

def parse_group(line):
    if not line:
        return {"exists": False}
    parts = line.split(":", 3)
    if len(parts) < 4:
        return {"exists": False}
    members = normalize_groups(parts[3].split(",") if parts[3] else [])
    return {
        "exists": True,
        "name": parts[0],
        "gid": int(parts[2]),
        "members": members,
    }

def user_groups(name, primary):
    if not name:
        return []
    proc = run("query-user-groups", ["id", "-nG", name])
    if proc.returncode != 0:
        return []
    groups = normalize_groups(proc.stdout.strip().split())
    return [item for item in groups if item != primary]

def observe(user_name, group_name):
    user = {"exists": False, "name": user_name}
    group = {"exists": False, "name": group_name}
    if group_name:
        group = parse_group(getent("group", group_name))
    if user_name:
        user = parse_passwd(getent("passwd", user_name))
        if user.get("exists"):
            primary = parse_group(getent("group", user.get("gid")))
            if primary.get("exists"):
                user["group"] = primary.get("name", "")
            elif group.get("exists") and group.get("gid") == user.get("gid"):
                user["group"] = group.get("name", "")
            user["supplementaryGroups"] = user_groups(user_name, user.get("group", ""))
    return {"user": user, "group": group}

def desired_primary_group(payload):
    return str(payload.get("userGroup") or payload.get("groupName") or "").strip()

def changes_for(payload, before, operation):
    user_name = str(payload.get("user") or "").strip()
    group_name = str(payload.get("groupName") or "").strip()
    state = str(payload.get("state") or "present").strip().lower()
    uid = parse_int(payload.get("uid"))
    gid = parse_int(payload.get("gid"))
    home = str(payload.get("home") or "").strip()
    shell = str(payload.get("shell") or "").strip()
    comment = str(payload.get("comment") or "").strip()
    groups = normalize_groups(payload.get("groups") or [])
    primary = desired_primary_group(payload)
    user = before.get("user") or {}
    group = before.get("group") or {}
    if operation == "delete":
        if not bool(payload.get("removeOnDelete")):
            return {"user": False, "group": False}
        return {"user": bool(user_name and user.get("exists")), "group": bool(group_name and group.get("exists"))}
    if state == "absent":
        return {"user": bool(user_name and user.get("exists")), "group": bool(group_name and group.get("exists"))}
    doc = {"user": False, "group": False}
    if group_name:
        doc["group"] = not bool(group.get("exists"))
        if group.get("exists") and gid is not None and group.get("gid") != gid:
            doc["gid"] = True
    if user_name:
        doc["user"] = not bool(user.get("exists"))
        if user.get("exists"):
            if uid is not None and user.get("uid") != uid:
                doc["uid"] = True
            if primary and user.get("group") != primary:
                doc["gid"] = True
            if home and user.get("home") != home:
                doc["home"] = True
            if shell and user.get("shell") != shell:
                doc["shell"] = True
            if comment and user.get("comment") != comment:
                doc["comment"] = True
            if groups and normalize_groups(user.get("supplementaryGroups") or []) != groups:
                doc["groups"] = True
    return doc

def changed(changes):
    return any(bool(value) for value in (changes or {}).values())

def groupadd_args(payload):
    args = ["groupadd"]
    if payload.get("system"):
        args.append("--system")
    gid = parse_int(payload.get("gid"))
    if gid is not None:
        args += ["-g", str(gid)]
    args.append(str(payload.get("groupName") or "").strip())
    return args

def groupmod_args(payload):
    args = ["groupmod"]
    gid = parse_int(payload.get("gid"))
    if gid is not None:
        args += ["-g", str(gid)]
    args.append(str(payload.get("groupName") or "").strip())
    return args

def useradd_args(payload):
    args = ["useradd"]
    if payload.get("system"):
        args.append("--system")
    uid = parse_int(payload.get("uid"))
    if uid is not None:
        args += ["-u", str(uid)]
    primary = desired_primary_group(payload)
    if primary:
        args += ["-g", primary]
    home = str(payload.get("home") or "").strip()
    if home:
        args += ["-d", home]
    shell = str(payload.get("shell") or "").strip()
    if shell:
        args += ["-s", shell]
    comment = str(payload.get("comment") or "").strip()
    if comment:
        args += ["-c", comment]
    groups = normalize_groups(payload.get("groups") or [])
    if groups:
        args += ["-G", ",".join(groups)]
    if payload.get("createHome"):
        args.append("-m")
    args.append(str(payload.get("user") or "").strip())
    return args

def usermod_args(payload, before):
    args = ["usermod"]
    uid = parse_int(payload.get("uid"))
    if uid is not None and before.get("uid") != uid:
        args += ["-u", str(uid)]
    primary = desired_primary_group(payload)
    if primary and before.get("group") != primary:
        args += ["-g", primary]
    home = str(payload.get("home") or "").strip()
    if home and before.get("home") != home:
        args += ["-d", home]
    shell = str(payload.get("shell") or "").strip()
    if shell and before.get("shell") != shell:
        args += ["-s", shell]
    comment = str(payload.get("comment") or "").strip()
    if comment and before.get("comment") != comment:
        args += ["-c", comment]
    groups = normalize_groups(payload.get("groups") or [])
    if groups and normalize_groups(before.get("supplementaryGroups") or []) != groups:
        args += ["-G", ",".join(groups)]
    args.append(str(payload.get("user") or "").strip())
    return args

def delete_user(payload, before):
    user_name = str(payload.get("user") or "").strip()
    if user_name and before.get("user", {}).get("exists"):
        args = ["userdel"]
        if payload.get("removeHome"):
            args.append("-r")
        args.append(user_name)
        proc = run("delete-user", args)
        if proc.returncode != 0:
            if not getent("passwd", user_name):
                return True, ""
            return False, "user delete failed"
    return True, ""

def delete_group(payload, before):
    group_name = str(payload.get("groupName") or "").strip()
    if group_name and before.get("group", {}).get("exists"):
        proc = run("delete-group", ["groupdel", group_name])
        if proc.returncode != 0:
            if not getent("group", group_name):
                return True, ""
            return False, "group delete failed"
    return True, ""

def verify(payload, operation, after):
    user_name = str(payload.get("user") or "").strip()
    group_name = str(payload.get("groupName") or "").strip()
    state = "absent" if operation == "delete" else str(payload.get("state") or "present").strip().lower()
    uid = parse_int(payload.get("uid"))
    gid = parse_int(payload.get("gid"))
    primary = desired_primary_group(payload)
    if state == "absent":
        if user_name and after.get("user", {}).get("exists"):
            return "failed", "user remained present"
        if group_name and after.get("group", {}).get("exists"):
            return "failed", "group remained present"
        return "succeeded", ""
    if group_name:
        group = after.get("group") or {}
        if not group.get("exists"):
            return "failed", "group was not present"
        if gid is not None and group.get("gid") != gid:
            return "failed", "group gid did not match desired gid"
    if user_name:
        user = after.get("user") or {}
        if not user.get("exists"):
            return "failed", "user was not present"
        if uid is not None and user.get("uid") != uid:
            return "failed", "user uid did not match desired uid"
        if primary and user.get("group") != primary:
            return "failed", "user primary group did not match desired group"
    return "succeeded", ""

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/host-user-node/v1")
    doc.setdefault("kind", "HostUserOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_USER_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    user_name = str(payload.get("user") or "").strip()
    group_name = str(payload.get("groupName") or "").strip()
    state = str(payload.get("state") or "present").strip().lower()
    if not user_name and not group_name:
        finish({"operation": operation, "status": "failed", "error": "user or groupName is required"}, 1)
    if state not in ("present", "absent"):
        finish({"operation": operation, "status": "failed", "error": "unsupported user state"}, 1)
    require_commands(["getent", "useradd", "usermod", "userdel", "groupadd", "groupmod", "groupdel"])
    before = observe(user_name, group_name)
    planned_changes = changes_for(payload, before, operation)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "userDigest": digest_bytes(user_name),
            "groupDigest": digest_bytes(group_name),
            "desiredState": state,
            "changed": False,
            "changes": {"user": False, "group": False},
            "before": before,
            "after": before,
            "commands": commands,
        })
    if operation == "delete":
        if not bool(payload.get("removeOnDelete")):
            finish({
                "operation": operation,
                "status": "skipped",
                "reason": "removeOnDelete is false",
                "userDigest": digest_bytes(user_name),
                "groupDigest": digest_bytes(group_name),
                "desiredState": "absent",
                "changed": False,
                "changes": {"user": False, "group": False},
                "before": before,
                "after": before,
                "commands": commands,
            })
        ok, error = delete_user(payload, before)
        if ok:
            ok, error = delete_group(payload, before)
        after = observe(user_name, group_name)
        if not ok:
            finish({
                "operation": operation,
                "status": "failed",
                "userDigest": digest_bytes(user_name),
                "groupDigest": digest_bytes(group_name),
                "desiredState": "absent",
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": error,
            }, 1)
        status, error = verify(payload, operation, after)
        finish({
            "operation": operation,
            "status": status,
            "userDigest": digest_bytes(user_name),
            "groupDigest": digest_bytes(group_name),
            "desiredState": "absent",
            "changed": changed(planned_changes),
            "changes": planned_changes,
            "before": before,
            "after": after,
            "commands": commands,
            "error": error,
        }, 0 if status == "succeeded" else 1)
    if operation != "apply":
        finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
    if state == "absent":
        ok, error = delete_user(payload, before)
        if ok:
            ok, error = delete_group(payload, before)
        after = observe(user_name, group_name)
        if not ok:
            finish({
                "operation": operation,
                "status": "failed",
                "userDigest": digest_bytes(user_name),
                "groupDigest": digest_bytes(group_name),
                "desiredState": state,
                "changed": False,
                "changes": planned_changes,
                "before": before,
                "after": after,
                "commands": commands,
                "error": error,
            }, 1)
    else:
        if group_name:
            group = before.get("group") or {}
            if not group.get("exists"):
                proc = run("create-group", groupadd_args(payload))
                if proc.returncode != 0:
                    after = observe(user_name, group_name)
                    finish({
                        "operation": operation,
                        "status": "failed",
                        "userDigest": digest_bytes(user_name),
                        "groupDigest": digest_bytes(group_name),
                        "desiredState": state,
                        "changed": False,
                        "changes": planned_changes,
                        "before": before,
                        "after": after,
                        "commands": commands,
                        "error": "group create failed",
                    }, 1)
            elif planned_changes.get("gid"):
                proc = run("modify-group", groupmod_args(payload))
                if proc.returncode != 0:
                    after = observe(user_name, group_name)
                    finish({
                        "operation": operation,
                        "status": "failed",
                        "userDigest": digest_bytes(user_name),
                        "groupDigest": digest_bytes(group_name),
                        "desiredState": state,
                        "changed": False,
                        "changes": planned_changes,
                        "before": before,
                        "after": after,
                        "commands": commands,
                        "error": "group modify failed",
                    }, 1)
        if user_name:
            current = observe(user_name, group_name)
            user = current.get("user") or {}
            if not user.get("exists"):
                proc = run("create-user", useradd_args(payload))
                if proc.returncode != 0:
                    after = observe(user_name, group_name)
                    finish({
                        "operation": operation,
                        "status": "failed",
                        "userDigest": digest_bytes(user_name),
                        "groupDigest": digest_bytes(group_name),
                        "desiredState": state,
                        "changed": False,
                        "changes": planned_changes,
                        "before": before,
                        "after": after,
                        "commands": commands,
                        "error": "user create failed",
                    }, 1)
            else:
                args = usermod_args(payload, user)
                if len(args) > 2:
                    proc = run("modify-user", args)
                    if proc.returncode != 0:
                        after = observe(user_name, group_name)
                        finish({
                            "operation": operation,
                            "status": "failed",
                            "userDigest": digest_bytes(user_name),
                            "groupDigest": digest_bytes(group_name),
                            "desiredState": state,
                            "changed": False,
                            "changes": planned_changes,
                            "before": before,
                            "after": after,
                            "commands": commands,
                            "error": "user modify failed",
                        }, 1)
        after = observe(user_name, group_name)
    status, error = verify(payload, operation, after)
    finish({
        "operation": operation,
        "status": status,
        "userDigest": digest_bytes(user_name),
        "groupDigest": digest_bytes(group_name),
        "desiredState": state,
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
