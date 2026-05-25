package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const (
	defaultKubernetesLogsCaptureTailLines   int64 = 100
	defaultKubernetesLogsCaptureLimitBytes  int64 = 64 * 1024
	defaultKubernetesLogsCaptureMaxRequests       = 5
)

type kubernetesLogsCaptureCommandReceipt struct {
	Action         string   `json:"action"`
	Status         string   `json:"status"`
	TargetDigest   string   `json:"targetDigest,omitempty"`
	CommandDigest  string   `json:"commandDigest,omitempty"`
	Command        []string `json:"command,omitempty"`
	StdoutDigest   string   `json:"stdoutDigest,omitempty"`
	StdoutBytes    int      `json:"stdoutBytes,omitempty"`
	StderrDigest   string   `json:"stderrDigest,omitempty"`
	StderrBytes    int      `json:"stderrBytes,omitempty"`
	ErrorDigest    string   `json:"errorDigest,omitempty"`
	ErrorBytes     int      `json:"errorBytes,omitempty"`
	ExitCode       int      `json:"exitCode"`
	TimedOut       bool     `json:"timedOut,omitempty"`
	DurationMillis int64    `json:"durationMillis,omitempty"`
}

type kubernetesLogsCaptureTargetSpec struct {
	Namespace      string `json:"namespace,omitempty"`
	Resource       string `json:"resource,omitempty"`
	Kind           string `json:"kind,omitempty"`
	Name           string `json:"name,omitempty"`
	Selector       string `json:"selector,omitempty"`
	Container      string `json:"container,omitempty"`
	AllContainers  bool   `json:"allContainers,omitempty"`
	Previous       bool   `json:"previous,omitempty"`
	Timestamps     bool   `json:"timestamps,omitempty"`
	Prefix         bool   `json:"prefix,omitempty"`
	Since          string `json:"since,omitempty"`
	SinceTime      string `json:"sinceTime,omitempty"`
	TailLines      int64  `json:"tailLines,omitempty"`
	LimitBytes     int64  `json:"limitBytes,omitempty"`
	MaxLogRequests int    `json:"maxLogRequests,omitempty"`
}

type kubernetesLogsCaptureObjectRef struct {
	Kind                  string `json:"kind,omitempty"`
	Namespace             string `json:"namespace,omitempty"`
	Name                  string `json:"name,omitempty"`
	UIDDigest             string `json:"uidDigest,omitempty"`
	ResourceVersionDigest string `json:"resourceVersionDigest,omitempty"`
	ObjectDigest          string `json:"objectDigest,omitempty"`
}

type kubernetesLogsCaptureTargetState struct {
	Exists    bool                             `json:"exists"`
	Selector  string                           `json:"selector,omitempty"`
	Resource  string                           `json:"resource,omitempty"`
	Kind      string                           `json:"kind,omitempty"`
	Namespace string                           `json:"namespace,omitempty"`
	Name      string                           `json:"name,omitempty"`
	ItemCount int                              `json:"itemCount,omitempty"`
	Items     []kubernetesLogsCaptureObjectRef `json:"items,omitempty"`
	Error     string                           `json:"error,omitempty"`
}

type kubernetesLogsCaptureLine struct {
	Index  int    `json:"index"`
	Digest string `json:"digest"`
	Bytes  int    `json:"bytes"`
	Text   string `json:"text,omitempty"`
}

type kubernetesLogsCaptureEvidence struct {
	LogDigest             string                      `json:"logDigest"`
	CommandStdoutDigest   string                      `json:"commandStdoutDigest,omitempty"`
	CommandStdoutBytes    int                         `json:"commandStdoutBytes,omitempty"`
	CapturedBytes         int                         `json:"capturedBytes"`
	CapturedLineCount     int                         `json:"capturedLineCount"`
	ObservedBytes         int                         `json:"observedBytes"`
	ObservedLineCount     int                         `json:"observedLineCount"`
	TailLines             int64                       `json:"tailLines"`
	LimitBytes            int64                       `json:"limitBytes"`
	TruncatedByLimitBytes bool                        `json:"truncatedByLimitBytes,omitempty"`
	TruncatedByTailLines  bool                        `json:"truncatedByTailLines,omitempty"`
	Lines                 []kubernetesLogsCaptureLine `json:"lines,omitempty"`
	Redaction             hostCommandRedactionProof   `json:"redaction"`
}

type kubernetesLogsCaptureObserveReceipt struct {
	APIVersion string                              `json:"apiVersion"`
	Kind       string                              `json:"kind"`
	NodeID     string                              `json:"nodeId"`
	NodeKind   string                              `json:"nodeKind"`
	TargetID   string                              `json:"targetId,omitempty"`
	Phase      string                              `json:"phase"`
	Status     string                              `json:"status"`
	Logs       kubernetesLogsCaptureTargetSpec     `json:"logs"`
	State      kubernetesLogsCaptureTargetState    `json:"state"`
	GetReceipt kubernetesLogsCaptureCommandReceipt `json:"getReceipt"`
	ObservedAt string                              `json:"observedAt"`
}

type kubernetesLogsCapturePlanReceipt struct {
	APIVersion string                          `json:"apiVersion"`
	Kind       string                          `json:"kind"`
	NodeID     string                          `json:"nodeId"`
	NodeKind   string                          `json:"nodeKind"`
	TargetID   string                          `json:"targetId,omitempty"`
	Phase      string                          `json:"phase"`
	Status     string                          `json:"status"`
	Reason     string                          `json:"reason,omitempty"`
	Operation  string                          `json:"operation"`
	Logs       kubernetesLogsCaptureTargetSpec `json:"logs"`
	PlannedAt  string                          `json:"plannedAt"`
}

type kubernetesLogsCaptureLogsReceipt struct {
	APIVersion string                              `json:"apiVersion"`
	Kind       string                              `json:"kind"`
	NodeID     string                              `json:"nodeId"`
	NodeKind   string                              `json:"nodeKind"`
	TargetID   string                              `json:"targetId,omitempty"`
	Phase      string                              `json:"phase"`
	Status     string                              `json:"status"`
	Reason     string                              `json:"reason,omitempty"`
	Logs       kubernetesLogsCaptureTargetSpec     `json:"logs"`
	Changed    bool                                `json:"changed"`
	Evidence   kubernetesLogsCaptureEvidence       `json:"evidence"`
	Receipt    kubernetesLogsCaptureCommandReceipt `json:"receipt"`
	CapturedAt string                              `json:"capturedAt"`
}

type kubernetesLogsCaptureVerifyReceipt struct {
	APIVersion string                          `json:"apiVersion"`
	Kind       string                          `json:"kind"`
	NodeID     string                          `json:"nodeId"`
	NodeKind   string                          `json:"nodeKind"`
	TargetID   string                          `json:"targetId,omitempty"`
	Phase      string                          `json:"phase"`
	Status     string                          `json:"status"`
	Reason     string                          `json:"reason,omitempty"`
	Logs       kubernetesLogsCaptureTargetSpec `json:"logs"`
	LogDigest  string                          `json:"logDigest,omitempty"`
	LineCount  int                             `json:"lineCount"`
	Bytes      int                             `json:"bytes"`
	Redaction  hostCommandRedactionProof       `json:"redaction"`
	VerifiedAt string                          `json:"verifiedAt"`
}

func (e *customNodeExecutor) runKubernetesLogsCaptureNode(ctx context.Context, node *runNode, command string) error {
	phase := "k8s-logs-capture"
	if strings.EqualFold(command, "delete") {
		payload := e.kubernetesLogsCaptureSkippedPayload(node, phase, "delete does not capture Kubernetes logs")
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}
	spec := node.Kubernetes
	logs := spec.Logs
	targetID := kubernetesManifestTargetID(spec.Cluster)
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"logs":      kubernetesLogsCaptureTarget(logs),
		"transport": strings.TrimSpace(spec.Cluster.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	if e.dryRun || e.diff {
		reason := "diff"
		if e.dryRun {
			reason = "dry-run"
		}
		observe := e.kubernetesLogsCaptureObserveReceipt(node, phase, targetID, logs, kubernetesLogsCaptureTargetState{}, kubernetesLogsCaptureCommandReceipt{Action: "get", Status: "skipped"}, "skipped")
		plan := e.kubernetesLogsCapturePlanReceipt(node, phase, targetID, logs, "skipped", reason)
		capture := e.kubernetesLogsCaptureLogsReceipt(node, phase, targetID, logs, kubernetesLogsCaptureEvidence{}, kubernetesLogsCaptureCommandReceipt{Action: "logs", Status: "skipped"}, "skipped", reason)
		verify := e.kubernetesLogsCaptureVerifyReceipt(node, phase, targetID, logs, kubernetesLogsCaptureEvidence{}, "skipped", reason)
		e.recordKubernetesLogsCaptureReceipts(node, phase, "skipped", reason, observe, plan, capture, verify)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	runner, err := kubernetesClusterVerifyRunner(spec.Cluster)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observeCommand := kubernetesLogsCaptureObserveCommand(spec.Cluster, logs)
	observeResult := runner.Run(ctx, observeCommand)
	state := kubernetesLogsCaptureStateFromReceipt(logs, observeResult)
	observeStatus := observeResult.Status
	if observeStatus == "" {
		observeStatus = "failed"
	}
	observe := e.kubernetesLogsCaptureObserveReceipt(node, phase, targetID, logs, state, compactKubernetesLogsCaptureCommandReceipt("get", observeCommand, observeResult), observeStatus)
	plan := e.kubernetesLogsCapturePlanReceipt(node, phase, targetID, logs, "planned", "eligible")

	logsCommand := kubernetesLogsCaptureCommand(spec.Cluster, logs)
	logsResult := runner.Run(ctx, logsCommand)
	evidence := kubernetesLogsCaptureEvidenceFromOutput(logsResult.Stdout, logs)
	if strings.TrimSpace(logsResult.Stdout) != "" {
		evidence.CommandStdoutDigest = digestString(logsResult.Stdout)
		evidence.CommandStdoutBytes = len([]byte(logsResult.Stdout))
	}
	status := "succeeded"
	reason := "logs captured"
	if !nodeStepSucceeded(logsResult.Status) {
		status = "failed"
		reason = firstReceiptMessage(logsResult)
		if strings.TrimSpace(reason) == "" {
			reason = "kubectl logs failed"
		}
	} else if !kubernetesLogsCaptureRedactionVerified(evidence.Redaction) {
		status = "failed"
		reason = "captured log evidence failed redaction scan"
	}
	capture := e.kubernetesLogsCaptureLogsReceipt(node, phase, targetID, logs, evidence, compactKubernetesLogsCaptureCommandReceipt("logs", logsCommand, logsResult), status, reason)
	verify := e.kubernetesLogsCaptureVerifyReceipt(node, phase, targetID, logs, evidence, status, reason)
	e.recordKubernetesLogsCaptureReceipts(node, phase, status, reason, observe, plan, capture, verify)
	if status != "succeeded" {
		runErr := &RunError{Class: "K8S_LOGS_CAPTURE_FAILED", Message: reason, Digest: computeRunErrorDigest("K8S_LOGS_CAPTURE_FAILED", reason)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, reason, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("kubernetes logs capture failed: %s", reason))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "logs captured", map[string]any{
		"phase":     phase,
		"status":    "succeeded",
		"cursor":    cursor,
		"lineCount": evidence.CapturedLineCount,
		"bytes":     evidence.CapturedBytes,
	}, nil)
	return nil
}

func (e *customNodeExecutor) kubernetesLogsCaptureObserveReceipt(node *runNode, phase string, targetID string, spec KubernetesLogsSpec, state kubernetesLogsCaptureTargetState, receipt kubernetesLogsCaptureCommandReceipt, status string) kubernetesLogsCaptureObserveReceipt {
	return kubernetesLogsCaptureObserveReceipt{
		APIVersion: "torque.dev/k8s-logs-capture-node/v1",
		Kind:       "KubernetesLogsCaptureObserveReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Logs:       sanitizeKubernetesLogsSpec(spec),
		State:      state,
		GetReceipt: receipt,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesLogsCapturePlanReceipt(node *runNode, phase string, targetID string, spec KubernetesLogsSpec, status string, reason string) kubernetesLogsCapturePlanReceipt {
	return kubernetesLogsCapturePlanReceipt{
		APIVersion: "torque.dev/k8s-logs-capture-node/v1",
		Kind:       "KubernetesLogsCapturePlanReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Operation:  "capture",
		Logs:       sanitizeKubernetesLogsSpec(spec),
		PlannedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesLogsCaptureLogsReceipt(node *runNode, phase string, targetID string, spec KubernetesLogsSpec, evidence kubernetesLogsCaptureEvidence, receipt kubernetesLogsCaptureCommandReceipt, status string, reason string) kubernetesLogsCaptureLogsReceipt {
	return kubernetesLogsCaptureLogsReceipt{
		APIVersion: "torque.dev/k8s-logs-capture-node/v1",
		Kind:       "KubernetesLogsCaptureLogsReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Logs:       sanitizeKubernetesLogsSpec(spec),
		Changed:    false,
		Evidence:   evidence,
		Receipt:    receipt,
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesLogsCaptureVerifyReceipt(node *runNode, phase string, targetID string, spec KubernetesLogsSpec, evidence kubernetesLogsCaptureEvidence, status string, reason string) kubernetesLogsCaptureVerifyReceipt {
	return kubernetesLogsCaptureVerifyReceipt{
		APIVersion: "torque.dev/k8s-logs-capture-node/v1",
		Kind:       "KubernetesLogsCaptureVerifyReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Logs:       sanitizeKubernetesLogsSpec(spec),
		LogDigest:  evidence.LogDigest,
		LineCount:  evidence.CapturedLineCount,
		Bytes:      evidence.CapturedBytes,
		Redaction:  evidence.Redaction,
		VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordKubernetesLogsCaptureReceipts(node *runNode, phase string, status string, reason string, observe kubernetesLogsCaptureObserveReceipt, plan kubernetesLogsCapturePlanReceipt, capture kubernetesLogsCaptureLogsReceipt, verify kubernetesLogsCaptureVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/k8s-logs-capture-node/v1",
		"kind":       "KubernetesLogsCaptureNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"observe":    observe,
		"plan":       plan,
		"logs":       capture,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	e.run.RecordJSONArtifact(node.ID, "k8s-logs-capture-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "k8s-logs-capture-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "k8s-logs-capture-logs.json", capture)
	e.run.RecordJSONArtifact(node.ID, "k8s-logs-capture-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, "k8s-logs-capture.json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) kubernetesLogsCaptureSkippedPayload(node *runNode, phase string, reason string) map[string]any {
	return map[string]any{
		"apiVersion": "torque.dev/k8s-logs-capture-node/v1",
		"kind":       "KubernetesLogsCaptureNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     "skipped",
		"reason":     strings.TrimSpace(reason),
	}
}

func kubernetesLogsCaptureObserveCommand(cluster KubernetesClusterSpec, spec KubernetesLogsSpec) string {
	command := kubernetesClusterKubectlBase(cluster) + kubernetesLogsCaptureNamespaceArg(spec) + " get "
	if strings.TrimSpace(spec.Selector) != "" {
		return command + "pods -l " + transport.ShellQuote(strings.TrimSpace(spec.Selector)) + " -o json"
	}
	return command + transport.ShellQuote(kubernetesLogsCaptureTarget(spec)) + " -o json"
}

func kubernetesLogsCaptureCommand(cluster KubernetesClusterSpec, spec KubernetesLogsSpec) string {
	command := kubernetesClusterKubectlBase(cluster) + kubernetesLogsCaptureNamespaceArg(spec) + " logs"
	if strings.TrimSpace(spec.Selector) != "" {
		command += " -l " + transport.ShellQuote(strings.TrimSpace(spec.Selector))
	} else {
		command += " " + transport.ShellQuote(kubernetesLogsCaptureTarget(spec))
	}
	if strings.TrimSpace(spec.Container) != "" {
		command += " -c " + transport.ShellQuote(strings.TrimSpace(spec.Container))
	}
	if spec.AllContainers {
		command += " --all-containers=true"
	}
	if spec.Previous {
		command += " --previous=true"
	}
	if spec.Timestamps {
		command += " --timestamps=true"
	}
	if spec.Prefix {
		command += " --prefix=true"
	}
	if spec.Since != nil && *spec.Since > 0 {
		command += " --since=" + transport.ShellQuote(spec.Since.String())
	}
	if strings.TrimSpace(spec.SinceTime) != "" {
		command += " --since-time=" + transport.ShellQuote(strings.TrimSpace(spec.SinceTime))
	}
	if spec.TailLines > 0 {
		command += " --tail=" + transport.ShellQuote(strconv.FormatInt(spec.TailLines, 10))
	}
	if spec.LimitBytes > 0 {
		command += " --limit-bytes=" + transport.ShellQuote(strconv.FormatInt(spec.LimitBytes, 10))
	}
	if spec.MaxLogRequests > 0 {
		command += " --max-log-requests=" + transport.ShellQuote(strconv.Itoa(spec.MaxLogRequests))
	}
	return command
}

func kubernetesLogsCaptureNamespaceArg(spec KubernetesLogsSpec) string {
	if strings.TrimSpace(spec.Namespace) == "" {
		return ""
	}
	return " -n " + transport.ShellQuote(strings.TrimSpace(spec.Namespace))
}

func kubernetesLogsCaptureTarget(spec KubernetesLogsSpec) string {
	if strings.TrimSpace(spec.Resource) != "" {
		return strings.TrimSpace(spec.Resource)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return strings.TrimSpace(spec.Kind)
	}
	return strings.TrimSpace(spec.Kind) + "/" + strings.TrimSpace(spec.Name)
}

func kubernetesLogsCaptureStateFromReceipt(spec KubernetesLogsSpec, receipt transport.OperationResult) kubernetesLogsCaptureTargetState {
	state := kubernetesLogsCaptureTargetState{
		Namespace: strings.TrimSpace(spec.Namespace),
		Resource:  kubernetesLogsCaptureTarget(spec),
		Kind:      strings.TrimSpace(spec.Kind),
		Name:      strings.TrimSpace(spec.Name),
		Selector:  strings.TrimSpace(spec.Selector),
	}
	if !nodeStepSucceeded(receipt.Status) || strings.TrimSpace(receipt.Stdout) == "" {
		state.Exists = false
		state.Error = "not found or inaccessible"
		return state
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(receipt.Stdout), &obj); err != nil {
		state.Exists = true
		state.ItemCount = 1
		state.Error = "kubectl get output was redacted or invalid json"
		state.Items = []kubernetesLogsCaptureObjectRef{{
			Kind:         strings.TrimSpace(state.Kind),
			Namespace:    strings.TrimSpace(state.Namespace),
			Name:         strings.TrimSpace(state.Name),
			ObjectDigest: digestBytes([]byte(receipt.Stdout)),
		}}
		return state
	}
	if items := asAnySlice(obj["items"]); items != nil {
		state.ItemCount = len(items)
		state.Exists = len(items) > 0
		for _, item := range items {
			if ref := kubernetesLogsCaptureObjectRefFromMap(asStringAnyMap(item)); ref.Name != "" {
				state.Items = append(state.Items, ref)
			}
		}
		sort.SliceStable(state.Items, func(i, j int) bool {
			left := state.Items[i].Namespace + "/" + state.Items[i].Name
			right := state.Items[j].Namespace + "/" + state.Items[j].Name
			return left < right
		})
		return state
	}
	ref := kubernetesLogsCaptureObjectRefFromMap(obj)
	state.Exists = ref.Name != "" || strings.TrimSpace(ref.ObjectDigest) != ""
	state.Kind = firstNonEmptyString(strings.TrimSpace(state.Kind), ref.Kind)
	state.Namespace = firstNonEmptyString(strings.TrimSpace(state.Namespace), ref.Namespace)
	state.Name = firstNonEmptyString(strings.TrimSpace(state.Name), ref.Name)
	if state.Exists {
		state.ItemCount = 1
		state.Items = []kubernetesLogsCaptureObjectRef{ref}
	}
	return state
}

func kubernetesLogsCaptureObjectRefFromMap(obj map[string]any) kubernetesLogsCaptureObjectRef {
	if len(obj) == 0 {
		return kubernetesLogsCaptureObjectRef{}
	}
	metadata := asStringAnyMap(obj["metadata"])
	ref := kubernetesLogsCaptureObjectRef{
		Kind:      strings.TrimSpace(stringFromAny(obj["kind"])),
		Namespace: strings.TrimSpace(stringFromAny(metadata["namespace"])),
		Name:      strings.TrimSpace(stringFromAny(metadata["name"])),
	}
	if raw, err := json.Marshal(obj); err == nil {
		ref.ObjectDigest = digestBytes(raw)
	}
	if value := strings.TrimSpace(stringFromAny(metadata["uid"])); value != "" {
		ref.UIDDigest = digestString(value)
	}
	if value := strings.TrimSpace(stringFromAny(metadata["resourceVersion"])); value != "" {
		ref.ResourceVersionDigest = digestString(value)
	}
	return ref
}

func kubernetesLogsCaptureEvidenceFromOutput(raw string, spec KubernetesLogsSpec) kubernetesLogsCaptureEvidence {
	redacted := transport.NewRedactor(nil).RedactString(raw)
	observedBytes := len([]byte(redacted))
	truncatedByBytes := false
	if spec.LimitBytes > 0 {
		rawBytes := []byte(redacted)
		if int64(len(rawBytes)) > spec.LimitBytes {
			redacted = string(rawBytes[:spec.LimitBytes])
			truncatedByBytes = true
		}
	}
	allLines := splitKubernetesLogLines(redacted)
	observedLineCount := len(allLines)
	lines := allLines
	truncatedByTail := false
	if spec.TailLines > 0 && int64(len(lines)) > spec.TailLines {
		lines = lines[len(lines)-int(spec.TailLines):]
		truncatedByTail = true
	}
	captured := strings.Join(lines, "\n")
	lineItems := make([]kubernetesLogsCaptureLine, 0, len(lines))
	startIndex := observedLineCount - len(lines) + 1
	if startIndex < 1 {
		startIndex = 1
	}
	for i, line := range lines {
		lineItems = append(lineItems, kubernetesLogsCaptureLine{
			Index:  startIndex + i,
			Digest: digestBytes([]byte(line)),
			Bytes:  len([]byte(line)),
			Text:   line,
		})
	}
	redaction := hostCommandRedaction(transport.OperationResult{Stdout: captured})
	return kubernetesLogsCaptureEvidence{
		LogDigest:             digestBytes([]byte(captured)),
		CapturedBytes:         len([]byte(captured)),
		CapturedLineCount:     len(lines),
		ObservedBytes:         observedBytes,
		ObservedLineCount:     observedLineCount,
		TailLines:             spec.TailLines,
		LimitBytes:            spec.LimitBytes,
		TruncatedByLimitBytes: truncatedByBytes,
		TruncatedByTailLines:  truncatedByTail,
		Lines:                 lineItems,
		Redaction:             redaction,
	}
}

func splitKubernetesLogLines(value string) []string {
	value = strings.TrimRight(value, "\n")
	if value == "" {
		return nil
	}
	return strings.Split(value, "\n")
}

func kubernetesLogsCaptureRedactionVerified(redaction hostCommandRedactionProof) bool {
	return redaction.NoSecretRefs && redaction.NoSensitiveKV && redaction.NoAuthorizationBearer
}

func compactKubernetesLogsCaptureCommandReceipt(action string, command string, receipt transport.OperationResult) kubernetesLogsCaptureCommandReceipt {
	out := kubernetesLogsCaptureCommandReceipt{
		Action:         strings.TrimSpace(action),
		Status:         strings.TrimSpace(receipt.Status),
		TargetDigest:   strings.TrimSpace(receipt.TargetDigest),
		CommandDigest:  digestString(command),
		Command:        append([]string(nil), receipt.Command...),
		ExitCode:       receipt.ExitCode,
		TimedOut:       receipt.TimedOut,
		DurationMillis: receipt.DurationMillis,
	}
	if strings.TrimSpace(receipt.Stdout) != "" {
		out.StdoutDigest = digestString(receipt.Stdout)
		out.StdoutBytes = len([]byte(receipt.Stdout))
	}
	if strings.TrimSpace(receipt.Stderr) != "" {
		out.StderrDigest = digestString(receipt.Stderr)
		out.StderrBytes = len([]byte(receipt.Stderr))
	}
	if strings.TrimSpace(receipt.Error) != "" {
		out.ErrorDigest = digestString(receipt.Error)
		out.ErrorBytes = len([]byte(receipt.Error))
	}
	if out.Status == "" {
		out.Status = "failed"
	}
	return out
}

func sanitizeKubernetesLogsSpec(spec KubernetesLogsSpec) kubernetesLogsCaptureTargetSpec {
	out := kubernetesLogsCaptureTargetSpec{
		Namespace:      strings.TrimSpace(spec.Namespace),
		Resource:       strings.TrimSpace(spec.Resource),
		Kind:           strings.TrimSpace(spec.Kind),
		Name:           strings.TrimSpace(spec.Name),
		Selector:       strings.TrimSpace(spec.Selector),
		Container:      strings.TrimSpace(spec.Container),
		AllContainers:  spec.AllContainers,
		Previous:       spec.Previous,
		Timestamps:     spec.Timestamps,
		Prefix:         spec.Prefix,
		SinceTime:      strings.TrimSpace(spec.SinceTime),
		TailLines:      spec.TailLines,
		LimitBytes:     spec.LimitBytes,
		MaxLogRequests: spec.MaxLogRequests,
	}
	if spec.Since != nil && *spec.Since > 0 {
		out.Since = spec.Since.String()
	}
	return out
}
