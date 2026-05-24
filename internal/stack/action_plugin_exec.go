package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	actionPluginPhaseObserve = "observe"
	actionPluginPhasePlan    = "plan"
	actionPluginPhaseApply   = "apply"
	actionPluginPhaseDelete  = "delete"
	actionPluginPhaseVerify  = "verify"
	actionPluginPhaseExport  = "export"
)

type actionPluginRequest struct {
	APIVersion string                   `json:"apiVersion"`
	Phase      string                   `json:"phase"`
	Command    string                   `json:"command"`
	DryRun     bool                     `json:"dryRun,omitempty"`
	RunID      string                   `json:"runId,omitempty"`
	Attempt    int                      `json:"attempt,omitempty"`
	Stack      actionPluginStackContext `json:"stack"`
	Node       actionPluginNodeContext  `json:"node"`
	Config     map[string]any           `json:"config,omitempty"`
}

type actionPluginStackContext struct {
	Root    string `json:"root,omitempty"`
	Name    string `json:"name,omitempty"`
	Profile string `json:"profile,omitempty"`
}

type actionPluginNodeContext struct {
	ID                 string            `json:"id"`
	Name               string            `json:"name"`
	Kind               string            `json:"kind"`
	Cluster            string            `json:"cluster,omitempty"`
	Namespace          string            `json:"namespace,omitempty"`
	Tags               []string          `json:"tags,omitempty"`
	Needs              []string          `json:"needs,omitempty"`
	EffectiveInputHash string            `json:"effectiveInputHash,omitempty"`
	Set                map[string]string `json:"set,omitempty"`
}

type actionPluginResult struct {
	APIVersion string         `json:"apiVersion,omitempty"`
	Status     string         `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	Changed    bool           `json:"changed,omitempty"`
	SafeToRun  *bool          `json:"safeToRun,omitempty"`
	Risk       string         `json:"risk,omitempty"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Cursor     map[string]any `json:"cursor,omitempty"`
	Artifacts  map[string]any `json:"artifacts,omitempty"`
}

func validateActionPluginSpec(spec *ActionPluginSpec) error {
	if spec == nil {
		return fmt.Errorf("action.plugin requires action.plugin")
	}
	if len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return fmt.Errorf("action.plugin.command is required")
	}
	phases, err := normalizeActionPluginPhases(spec.Phases)
	if err != nil {
		return err
	}
	if len(phases) == 0 {
		return nil
	}
	hasPlan := false
	hasMutatingPhase := false
	for _, phase := range phases {
		switch phase {
		case actionPluginPhasePlan:
			hasPlan = true
		case actionPluginPhaseApply, actionPluginPhaseDelete:
			hasMutatingPhase = true
		}
	}
	if !hasPlan {
		return fmt.Errorf("action.plugin.phases must include plan")
	}
	if !hasMutatingPhase {
		return fmt.Errorf("action.plugin.phases must include apply or delete")
	}
	return nil
}

func (e *customNodeExecutor) runActionPluginNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Action.Plugin
	if spec == nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("action.plugin requires action.plugin"))
	}
	phases, err := actionPluginPhasesForCommand(spec.Phases, command, e.dryRun)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	var decisions []map[string]any
	for _, phase := range phases {
		e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "plugin "+phase, map[string]any{
			"phase": phase,
		}, nil)
		result, runErr := e.invokeActionPluginPhase(ctx, node, command, phase, spec)
		phaseDecision := actionPluginPhaseDecision(phase, result)
		decisions = append(decisions, phaseDecision)
		e.recordActionPluginDecision(node, decisions)
		if runErr == nil {
			runErr = actionPluginResultError(phase, result)
		}
		status := normalizeActionPluginStatus(result.Status)
		if status == "" {
			status = "succeeded"
		}
		message := strings.TrimSpace(result.Message)
		if message == "" {
			message = status
		}
		fields := map[string]any{
			"phase":   phase,
			"status":  status,
			"changed": result.Changed,
			"risk":    strings.TrimSpace(result.Risk),
			"cursor":  result.Cursor,
		}
		if result.SafeToRun != nil {
			fields["safeToRun"] = *result.SafeToRun
		}
		if runErr != nil {
			class := actionPluginErrorClass(runErr)
			e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, message, fields, &RunError{
				Class:   class,
				Message: runErr.Error(),
				Digest:  computeRunErrorDigest(class, runErr.Error()),
			}, true)
			return wrapNodeErr(node.ResolvedRelease, runErr)
		}
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, message, fields, nil)
	}
	return nil
}

func (e *customNodeExecutor) invokeActionPluginPhase(ctx context.Context, node *runNode, command string, phase string, spec *ActionPluginSpec) (actionPluginResult, error) {
	timeout := 5 * time.Minute
	if spec.Timeout != nil {
		timeout = *spec.Timeout
	}
	runCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		runCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	req := actionPluginRequest{
		APIVersion: "torque.dev/action-plugin/v1",
		Phase:      phase,
		Command:    strings.TrimSpace(command),
		DryRun:     e.dryRun,
		RunID:      strings.TrimSpace(e.run.RunID),
		Attempt:    node.Attempt,
		Stack: actionPluginStackContext{
			Root:    strings.TrimSpace(e.run.Plan.StackRoot),
			Name:    strings.TrimSpace(e.run.Plan.StackName),
			Profile: strings.TrimSpace(e.run.Plan.Profile),
		},
		Node: actionPluginNodeContext{
			ID:                 strings.TrimSpace(node.ID),
			Name:               strings.TrimSpace(node.Name),
			Kind:               normalizeNodeKind(node.Kind),
			Cluster:            strings.TrimSpace(node.Cluster.Name),
			Namespace:          strings.TrimSpace(node.Namespace),
			Tags:               append([]string(nil), node.Tags...),
			Needs:              append([]string(nil), node.Needs...),
			EffectiveInputHash: strings.TrimSpace(node.EffectiveInputHash),
			Set:                cloneStringMap(node.Set),
		},
		Config: cloneAnyMap(spec.Config),
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return actionPluginResult{}, err
	}

	cmd := exec.CommandContext(runCtx, spec.Command[0], spec.Command[1:]...)
	workDir := strings.TrimSpace(spec.WorkDir)
	if workDir == "" {
		workDir = node.Dir
	}
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), flattenEnv(spec.Env)...)
	cmd.Env = append(cmd.Env,
		"TORQUE_STACK_RUN_ID="+strings.TrimSpace(e.run.RunID),
		"TORQUE_STACK_NODE_ID="+strings.TrimSpace(node.ID),
		"TORQUE_STACK_PHASE="+phase,
		"TORQUE_STACK_COMMAND="+strings.TrimSpace(command),
		"TORQUE_STACK_INTENT_DIGEST="+strings.TrimSpace(node.EffectiveInputHash),
	)
	cmd.Stdin = bytes.NewReader(reqJSON)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	exitErr := cmd.Run()
	rawOut := bytes.TrimSpace(stdout.Bytes())
	rawErr := strings.TrimSpace(stderr.String())
	if len(rawOut) == 0 {
		if exitErr != nil {
			if runCtx.Err() != nil {
				return actionPluginResult{}, fmt.Errorf("plugin phase %s timed out or was canceled: %w", phase, runCtx.Err())
			}
			return actionPluginResult{}, fmt.Errorf("plugin phase %s exited without JSON output: %w%s", phase, exitErr, actionPluginStderrSuffix(rawErr))
		}
		return actionPluginResult{}, fmt.Errorf("plugin phase %s exited without JSON output", phase)
	}
	var result actionPluginResult
	if err := json.Unmarshal(rawOut, &result); err != nil {
		return actionPluginResult{}, fmt.Errorf("plugin phase %s returned invalid JSON: %w%s", phase, err, actionPluginStderrSuffix(rawErr))
	}
	e.recordActionPluginPhaseArtifact(node, phase, command, result, rawErr, workDir)
	for name, artifact := range result.Artifacts {
		e.recordActionPluginResultArtifact(node, phase, name, artifact)
	}
	if exitErr != nil {
		if runCtx.Err() != nil {
			return result, fmt.Errorf("plugin phase %s timed out or was canceled: %w", phase, runCtx.Err())
		}
		return result, fmt.Errorf("plugin phase %s failed: %w%s", phase, exitErr, actionPluginStderrSuffix(rawErr))
	}
	return result, nil
}

func actionPluginPhasesForCommand(configured []string, command string, dryRun bool) ([]string, error) {
	phases, err := normalizeActionPluginPhases(configured)
	if err != nil {
		return nil, err
	}
	command = strings.ToLower(strings.TrimSpace(command))
	if len(phases) == 0 {
		if dryRun {
			return []string{actionPluginPhasePlan}, nil
		}
		return []string{actionPluginPhasePlan, command}, nil
	}
	out := make([]string, 0, len(phases))
	for _, phase := range phases {
		if dryRun {
			if phase == actionPluginPhaseObserve || phase == actionPluginPhasePlan {
				out = append(out, phase)
			}
			continue
		}
		if phase == actionPluginPhaseApply || phase == actionPluginPhaseDelete {
			if phase == command {
				out = append(out, phase)
			}
			continue
		}
		out = append(out, phase)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("action.plugin.phases selected no phases for command %q", command)
	}
	return out, nil
}

func normalizeActionPluginPhases(phases []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, phase := range phases {
		normalized := normalizeActionPluginPhase(phase)
		if normalized == "" {
			continue
		}
		if !validActionPluginPhase(normalized) {
			return nil, fmt.Errorf("unsupported action.plugin phase %q", phase)
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out, nil
}

func normalizedActionPluginPhasesForHash(phases []string) []string {
	out, err := normalizeActionPluginPhases(phases)
	if err != nil {
		out = append([]string(nil), phases...)
	}
	sort.Strings(out)
	return out
}

func normalizeActionPluginPhase(phase string) string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	phase = strings.ReplaceAll(phase, "_", "-")
	switch phase {
	case "export-evidence", "exportevidence":
		return actionPluginPhaseExport
	default:
		return phase
	}
}

func validActionPluginPhase(phase string) bool {
	switch phase {
	case actionPluginPhaseObserve, actionPluginPhasePlan, actionPluginPhaseApply, actionPluginPhaseDelete, actionPluginPhaseVerify, actionPluginPhaseExport:
		return true
	default:
		return false
	}
}

func actionPluginResultError(phase string, result actionPluginResult) error {
	status := normalizeActionPluginStatus(result.Status)
	if status != "" && !validActionPluginStatus(status) {
		return fmt.Errorf("plugin failed phase %s: unsupported status %q", phase, result.Status)
	}
	switch status {
	case "blocked":
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "plugin reported blocked"
		}
		return fmt.Errorf("plugin blocked phase %s: %s", phase, msg)
	case "failed", "error":
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "plugin reported failure"
		}
		return fmt.Errorf("plugin failed phase %s: %s", phase, msg)
	}
	if phase == actionPluginPhasePlan && result.SafeToRun != nil && !*result.SafeToRun {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "plan marked safeToRun=false"
		}
		return fmt.Errorf("plugin blocked phase %s: %s", phase, msg)
	}
	return nil
}

func validActionPluginStatus(status string) bool {
	switch status {
	case "succeeded", "noop", "planned", "changed", "skipped", "blocked", "failed", "error":
		return true
	default:
		return false
	}
}

func actionPluginErrorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "plugin blocked"):
		return "PLUGIN_BLOCKED"
	case strings.Contains(msg, "plugin failed"):
		return "PLUGIN_FAILED"
	default:
		return classifyError(err)
	}
}

func normalizeActionPluginStatus(status string) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "success", "succeeded", "ok":
		return "succeeded"
	case "noop", "no-op":
		return "noop"
	case "planned", "changed", "skipped", "blocked", "failed", "error":
		return strings.ToLower(strings.TrimSpace(status))
	default:
		return strings.ToLower(strings.TrimSpace(status))
	}
}

func actionPluginPhaseDecision(phase string, result actionPluginResult) map[string]any {
	out := map[string]any{
		"phase":   strings.TrimSpace(phase),
		"status":  normalizeActionPluginStatus(result.Status),
		"changed": result.Changed,
		"message": strings.TrimSpace(result.Message),
		"risk":    strings.TrimSpace(result.Risk),
	}
	if result.SafeToRun != nil {
		out["safeToRun"] = *result.SafeToRun
	}
	if len(result.Evidence) > 0 {
		out["evidence"] = result.Evidence
	}
	if len(result.Cursor) > 0 {
		out["cursor"] = result.Cursor
	}
	return out
}

func (e *customNodeExecutor) recordActionPluginPhaseArtifact(node *runNode, phase string, command string, result actionPluginResult, stderr string, workDir string) {
	payload := map[string]any{
		"apiVersion": "torque.dev/action-plugin/v1",
		"kind":       "ActionPluginPhaseArtifact",
		"nodeId":     strings.TrimSpace(node.ID),
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"command":    strings.TrimSpace(command),
		"status":     normalizeActionPluginStatus(result.Status),
		"changed":    result.Changed,
		"risk":       strings.TrimSpace(result.Risk),
		"message":    strings.TrimSpace(result.Message),
		"workDir":    strings.TrimSpace(workDir),
	}
	if result.SafeToRun != nil {
		payload["safeToRun"] = *result.SafeToRun
	}
	if len(result.Evidence) > 0 {
		payload["evidence"] = result.Evidence
	}
	if len(result.Cursor) > 0 {
		payload["cursor"] = result.Cursor
	}
	if strings.TrimSpace(stderr) != "" {
		payload["stderr"] = truncateActionPluginText(stderr, 16*1024)
	}
	e.run.RecordJSONArtifact(node.ID, "plugin-"+strings.TrimSpace(phase)+".json", payload)
}

func (e *customNodeExecutor) recordActionPluginDecision(node *runNode, phases []map[string]any) {
	payload := map[string]any{
		"apiVersion": "torque.dev/action-plugin/v1",
		"kind":       "ActionPluginDecision",
		"nodeId":     strings.TrimSpace(node.ID),
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phases":     phases,
	}
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) recordActionPluginResultArtifact(node *runNode, phase string, name string, artifact any) {
	name = sanitizeActionPluginArtifactName(name)
	if name == "" {
		return
	}
	if strings.HasPrefix(name, "plugin-") || name == "decision.json" {
		name = strings.TrimSuffix(name, ".json") + "-artifact.json"
	}
	if text, ok := artifact.(string); ok {
		e.run.RecordArtifact(node.ID, name, "text/plain", text)
		return
	}
	payload := map[string]any{
		"apiVersion": "torque.dev/action-plugin/v1",
		"kind":       "ActionPluginResultArtifact",
		"phase":      strings.TrimSpace(phase),
		"name":       name,
		"value":      artifact,
	}
	e.run.RecordJSONArtifact(node.ID, name, payload)
}

func sanitizeActionPluginArtifactName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	name = filepath.Base(name)
	name = strings.ReplaceAll(name, " ", "-")
	name = strings.ReplaceAll(name, "\t", "-")
	if !strings.Contains(name, ".") {
		name += ".json"
	}
	return name
}

func actionPluginStderrSuffix(stderr string) string {
	stderr = strings.TrimSpace(stderr)
	if stderr == "" {
		return ""
	}
	return ": stderr=" + truncateActionPluginText(stderr, 2048)
}

func truncateActionPluginText(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func cloneStringMap(in map[string]string) map[string]string {
	if in == nil {
		return nil
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
