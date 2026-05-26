package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const (
	modulePhaseObserve = "observe"
	modulePhaseDiff    = "diff"
	modulePhasePlan    = "plan"
	modulePhaseApply   = "apply"
	modulePhaseDelete  = "delete"
	modulePhaseVerify  = "verify"
	modulePhaseExport  = "export"
)

type moduleResourceRequest struct {
	APIVersion string                      `json:"apiVersion"`
	Phase      string                      `json:"phase"`
	Command    string                      `json:"command"`
	DryRun     bool                        `json:"dryRun,omitempty"`
	Diff       bool                        `json:"diff,omitempty"`
	RunID      string                      `json:"runId,omitempty"`
	Attempt    int                         `json:"attempt,omitempty"`
	Stack      actionPluginStackContext    `json:"stack"`
	Node       actionPluginNodeContext     `json:"node"`
	Module     moduleResourceModuleContext `json:"module"`
	Input      map[string]any              `json:"input,omitempty"`
}

type moduleResourceModuleContext struct {
	Source  string   `json:"source,omitempty"`
	Version string   `json:"version,omitempty"`
	Phases  []string `json:"phases,omitempty"`
}

type moduleResourceResult struct {
	APIVersion string         `json:"apiVersion,omitempty"`
	Status     string         `json:"status,omitempty"`
	Message    string         `json:"message,omitempty"`
	Changed    bool           `json:"changed,omitempty"`
	SafeToRun  *bool          `json:"safeToRun,omitempty"`
	Risk       string         `json:"risk,omitempty"`
	Before     any            `json:"before,omitempty"`
	After      any            `json:"after,omitempty"`
	Diff       any            `json:"diff,omitempty"`
	Evidence   map[string]any `json:"evidence,omitempty"`
	Receipt    map[string]any `json:"receipt,omitempty"`
	Cursor     map[string]any `json:"cursor,omitempty"`
	Artifacts  map[string]any `json:"artifacts,omitempty"`
}

func isModuleBackedNode(n *ResolvedRelease) bool {
	return n != nil && len(n.Module.Command) > 0
}

func validateModuleSpec(name string, kind string, spec *ModuleSpec) error {
	if spec == nil || len(spec.Command) == 0 || strings.TrimSpace(spec.Command[0]) == "" {
		return fmt.Errorf("module-backed node %s requires module.command", name)
	}
	phases, err := normalizeModulePhases(spec.Phases)
	if err != nil {
		return fmt.Errorf("module-backed node %s: %w", name, err)
	}
	if len(phases) == 0 {
		return nil
	}
	required := map[string]bool{
		modulePhaseObserve: false,
		modulePhaseDiff:    false,
		modulePhasePlan:    false,
		modulePhaseVerify:  false,
	}
	hasMutatingPhase := false
	for _, phase := range phases {
		if _, ok := required[phase]; ok {
			required[phase] = true
		}
		if phase == modulePhaseApply || phase == modulePhaseDelete {
			hasMutatingPhase = true
		}
	}
	for phase, ok := range required {
		if !ok {
			return fmt.Errorf("module-backed node %s kind %q phases must include %s", name, normalizeNodeKind(kind), phase)
		}
	}
	if !hasMutatingPhase {
		return fmt.Errorf("module-backed node %s kind %q phases must include apply or delete", name, normalizeNodeKind(kind))
	}
	return nil
}

func (e *customNodeExecutor) runModuleResourceNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Module
	phases, err := moduleResourcePhasesForCommand(spec.Phases, command, e.dryRun, e.diff)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	cursor := map[string]any{
		"kind":   normalizeNodeKind(node.Kind),
		"source": strings.TrimSpace(spec.Source),
	}
	var decisions []map[string]any
	for _, phase := range phases {
		phaseCursor := cloneAnyMap(cursor)
		phaseCursor["phase"] = phase
		e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "module "+phase, map[string]any{
			"phase":  phase,
			"cursor": phaseCursor,
		}, nil)
		result, runErr := e.invokeModuleResourcePhase(ctx, node, command, phase, spec)
		decision := moduleResourcePhaseDecision(phase, result)
		decisions = append(decisions, decision)
		e.recordModuleResourceDecision(node, decisions)
		if runErr == nil {
			runErr = moduleResourceResultError(phase, result)
		}
		status := normalizeModuleStatus(result.Status)
		if status == "" {
			status = defaultModuleStatus(phase)
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
			e.recordModuleResourceAggregate(node, "failed", decisions)
			class := moduleResourceErrorClass(runErr)
			e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, message, fields, &RunError{
				Class:   class,
				Message: runErr.Error(),
				Digest:  computeRunErrorDigest(class, runErr.Error()),
			}, true)
			return wrapNodeErr(node.ResolvedRelease, runErr)
		}
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, message, fields, nil)
	}
	e.recordModuleResourceAggregate(node, "succeeded", decisions)
	return nil
}

func (e *customNodeExecutor) invokeModuleResourcePhase(ctx context.Context, node *runNode, command string, phase string, spec ModuleSpec) (moduleResourceResult, error) {
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

	phases, _ := normalizeModulePhases(spec.Phases)
	req := moduleResourceRequest{
		APIVersion: "torque.dev/module-resource/v1",
		Phase:      phase,
		Command:    strings.TrimSpace(command),
		DryRun:     e.dryRun,
		Diff:       e.diff,
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
		Module: moduleResourceModuleContext{
			Source:  strings.TrimSpace(spec.Source),
			Version: strings.TrimSpace(spec.Version),
			Phases:  phases,
		},
		Input: cloneAnyMap(spec.Input),
	}
	reqJSON, err := json.Marshal(req)
	if err != nil {
		return moduleResourceResult{}, err
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
		"TORQUE_MODULE_KIND="+normalizeNodeKind(node.Kind),
		"TORQUE_MODULE_PHASE="+phase,
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
				return moduleResourceResult{}, fmt.Errorf("module phase %s timed out or was canceled: %w", phase, runCtx.Err())
			}
			return moduleResourceResult{}, fmt.Errorf("module phase %s exited without JSON output: %w%s", phase, exitErr, actionPluginStderrSuffix(rawErr))
		}
		return moduleResourceResult{}, fmt.Errorf("module phase %s exited without JSON output", phase)
	}
	var result moduleResourceResult
	if err := json.Unmarshal(rawOut, &result); err != nil {
		return moduleResourceResult{}, fmt.Errorf("module phase %s returned invalid JSON: %w%s", phase, err, actionPluginStderrSuffix(rawErr))
	}
	e.recordModuleResourcePhaseArtifact(node, phase, command, result, rawErr, workDir)
	for name, artifact := range result.Artifacts {
		e.recordModuleResourceResultArtifact(node, phase, name, artifact)
	}
	if exitErr != nil {
		if runCtx.Err() != nil {
			return result, fmt.Errorf("module phase %s timed out or was canceled: %w", phase, runCtx.Err())
		}
		return result, fmt.Errorf("module phase %s failed: %w%s", phase, exitErr, actionPluginStderrSuffix(rawErr))
	}
	return result, nil
}

func moduleResourcePhasesForCommand(configured []string, command string, dryRun bool, diff bool) ([]string, error) {
	phases, err := normalizeModulePhases(configured)
	if err != nil {
		return nil, err
	}
	command = strings.ToLower(strings.TrimSpace(command))
	if command == "" {
		command = modulePhaseApply
	}
	if len(phases) == 0 {
		if dryRun || diff {
			return []string{modulePhaseObserve, modulePhaseDiff, modulePhasePlan}, nil
		}
		if command == modulePhaseDelete {
			return []string{modulePhaseObserve, modulePhaseDiff, modulePhasePlan, modulePhaseDelete, modulePhaseVerify}, nil
		}
		return []string{modulePhaseObserve, modulePhaseDiff, modulePhasePlan, modulePhaseApply, modulePhaseVerify}, nil
	}
	out := make([]string, 0, len(phases))
	for _, phase := range phases {
		if dryRun || diff {
			if phase == modulePhaseObserve || phase == modulePhaseDiff || phase == modulePhasePlan {
				out = append(out, phase)
			}
			continue
		}
		if phase == modulePhaseApply || phase == modulePhaseDelete {
			if phase == command {
				out = append(out, phase)
			}
			continue
		}
		out = append(out, phase)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("module.phases selected no phases for command %q", command)
	}
	return out, nil
}

func normalizeModulePhases(phases []string) ([]string, error) {
	var out []string
	seen := map[string]bool{}
	for _, phase := range phases {
		normalized := normalizeModulePhase(phase)
		if normalized == "" {
			continue
		}
		if !validModulePhase(normalized) {
			return nil, fmt.Errorf("unsupported module phase %q", phase)
		}
		if seen[normalized] {
			continue
		}
		seen[normalized] = true
		out = append(out, normalized)
	}
	return out, nil
}

func normalizedModulePhasesForHash(phases []string) []string {
	out, err := normalizeModulePhases(phases)
	if err != nil {
		out = append([]string(nil), phases...)
	}
	return out
}

func normalizeModulePhase(phase string) string {
	phase = strings.ToLower(strings.TrimSpace(phase))
	phase = strings.ReplaceAll(phase, "_", "-")
	switch phase {
	case "export-evidence", "exportevidence":
		return modulePhaseExport
	default:
		return phase
	}
}

func validModulePhase(phase string) bool {
	switch phase {
	case modulePhaseObserve, modulePhaseDiff, modulePhasePlan, modulePhaseApply, modulePhaseDelete, modulePhaseVerify, modulePhaseExport:
		return true
	default:
		return false
	}
}

func moduleResourceResultError(phase string, result moduleResourceResult) error {
	status := normalizeModuleStatus(result.Status)
	if status != "" && !validModuleStatus(status) {
		return fmt.Errorf("module failed phase %s: unsupported status %q", phase, result.Status)
	}
	switch status {
	case "blocked":
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "module reported blocked"
		}
		return fmt.Errorf("module blocked phase %s: %s", phase, msg)
	case "failed", "error":
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "module reported failure"
		}
		return fmt.Errorf("module failed phase %s: %s", phase, msg)
	}
	if phase == modulePhasePlan && result.SafeToRun != nil && !*result.SafeToRun {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "plan marked safeToRun=false"
		}
		return fmt.Errorf("module blocked phase %s: %s", phase, msg)
	}
	if phase == modulePhaseVerify && status != "" && status != "succeeded" && status != "noop" && status != "skipped" {
		msg := strings.TrimSpace(result.Message)
		if msg == "" {
			msg = "verify did not succeed"
		}
		return fmt.Errorf("module failed phase %s: %s", phase, msg)
	}
	return nil
}

func validModuleStatus(status string) bool {
	switch status {
	case "succeeded", "noop", "planned", "changed", "skipped", "blocked", "failed", "error":
		return true
	default:
		return false
	}
}

func normalizeModuleStatus(status string) string {
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

func defaultModuleStatus(phase string) string {
	if phase == modulePhasePlan || phase == modulePhaseDiff {
		return "planned"
	}
	return "succeeded"
}

func moduleResourceErrorClass(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "module blocked"):
		return "MODULE_BLOCKED"
	case strings.Contains(msg, "module failed"):
		return "MODULE_FAILED"
	default:
		return classifyError(err)
	}
}

func moduleResourcePhaseDecision(phase string, result moduleResourceResult) map[string]any {
	out := map[string]any{
		"phase":   strings.TrimSpace(phase),
		"status":  normalizeModuleStatus(result.Status),
		"changed": result.Changed,
		"message": strings.TrimSpace(result.Message),
		"risk":    strings.TrimSpace(result.Risk),
	}
	if result.SafeToRun != nil {
		out["safeToRun"] = *result.SafeToRun
	}
	if result.Diff != nil {
		out["diff"] = result.Diff
	}
	if len(result.Evidence) > 0 {
		out["evidence"] = result.Evidence
	}
	if len(result.Receipt) > 0 {
		out["receipt"] = result.Receipt
	}
	if len(result.Cursor) > 0 {
		out["cursor"] = result.Cursor
	}
	return out
}

func (e *customNodeExecutor) recordModuleResourcePhaseArtifact(node *runNode, phase string, command string, result moduleResourceResult, stderr string, workDir string) {
	payload := map[string]any{
		"apiVersion": "torque.dev/module-resource/v1",
		"kind":       "ModuleResourcePhaseReceipt",
		"nodeId":     strings.TrimSpace(node.ID),
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"command":    strings.TrimSpace(command),
		"status":     firstNonEmptyString(normalizeModuleStatus(result.Status), defaultModuleStatus(phase)),
		"changed":    result.Changed,
		"risk":       strings.TrimSpace(result.Risk),
		"message":    strings.TrimSpace(result.Message),
		"workDir":    strings.TrimSpace(workDir),
	}
	if result.SafeToRun != nil {
		payload["safeToRun"] = *result.SafeToRun
	}
	if result.Before != nil {
		payload["before"] = result.Before
	}
	if result.After != nil {
		payload["after"] = result.After
	}
	if result.Diff != nil {
		payload["diff"] = result.Diff
	}
	if len(result.Evidence) > 0 {
		payload["evidence"] = result.Evidence
	}
	if len(result.Receipt) > 0 {
		payload["receipt"] = result.Receipt
	}
	if len(result.Cursor) > 0 {
		payload["cursor"] = result.Cursor
	}
	if strings.TrimSpace(stderr) != "" {
		payload["stderr"] = truncateActionPluginText(stderr, 16*1024)
	}
	e.run.RecordJSONArtifact(node.ID, "module-"+strings.TrimSpace(phase)+".json", payload)
}

func (e *customNodeExecutor) recordModuleResourceDecision(node *runNode, phases []map[string]any) {
	payload := map[string]any{
		"apiVersion": "torque.dev/module-resource/v1",
		"kind":       "ModuleResourceDecision",
		"nodeId":     strings.TrimSpace(node.ID),
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phases":     phases,
	}
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) recordModuleResourceAggregate(node *runNode, status string, phases []map[string]any) {
	payload := map[string]any{
		"apiVersion": "torque.dev/module-resource/v1",
		"kind":       "ModuleResourceReceipt",
		"nodeId":     strings.TrimSpace(node.ID),
		"nodeKind":   normalizeNodeKind(node.Kind),
		"status":     strings.TrimSpace(status),
		"phases":     phases,
	}
	e.run.RecordJSONArtifact(node.ID, "module-resource.json", payload)
}

func (e *customNodeExecutor) recordModuleResourceResultArtifact(node *runNode, phase string, name string, artifact any) {
	name = sanitizeActionPluginArtifactName(name)
	if name == "" {
		return
	}
	if strings.HasPrefix(name, "module-") || name == "decision.json" {
		name = strings.TrimSuffix(name, ".json") + "-artifact.json"
	}
	if text, ok := artifact.(string); ok {
		e.run.RecordArtifact(node.ID, name, "text/plain", text)
		return
	}
	payload := map[string]any{
		"apiVersion": "torque.dev/module-resource/v1",
		"kind":       "ModuleResourceResultArtifact",
		"phase":      strings.TrimSpace(phase),
		"name":       name,
		"value":      artifact,
	}
	e.run.RecordJSONArtifact(node.ID, name, payload)
}
