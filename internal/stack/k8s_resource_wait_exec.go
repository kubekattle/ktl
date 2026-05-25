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
	defaultKubernetesResourceWaitTimeout    = 2 * time.Minute
	defaultKubernetesResourceWaitEventLimit = 25
)

type kubernetesResourceWaitCommandReceipt struct {
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

type kubernetesResourceWaitTargetSpec struct {
	Namespace  string `json:"namespace,omitempty"`
	Resource   string `json:"resource,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Name       string `json:"name,omitempty"`
	Selector   string `json:"selector,omitempty"`
	For        string `json:"for,omitempty"`
	Timeout    string `json:"timeout,omitempty"`
	EventLimit int    `json:"eventLimit,omitempty"`
}

type kubernetesResourceWaitState struct {
	Exists                bool   `json:"exists"`
	Ready                 bool   `json:"ready"`
	Namespace             string `json:"namespace,omitempty"`
	Resource              string `json:"resource,omitempty"`
	Kind                  string `json:"kind,omitempty"`
	Name                  string `json:"name,omitempty"`
	Condition             string `json:"condition,omitempty"`
	ConditionStatus       string `json:"conditionStatus,omitempty"`
	Reason                string `json:"reason,omitempty"`
	UIDDigest             string `json:"uidDigest,omitempty"`
	ResourceVersionDigest string `json:"resourceVersionDigest,omitempty"`
	ObjectDigest          string `json:"objectDigest,omitempty"`
	Generation            int64  `json:"generation,omitempty"`
	ObservedGeneration    int64  `json:"observedGeneration,omitempty"`
	Replicas              int64  `json:"replicas,omitempty"`
	ReadyReplicas         int64  `json:"readyReplicas,omitempty"`
	AvailableReplicas     int64  `json:"availableReplicas,omitempty"`
	UpdatedReplicas       int64  `json:"updatedReplicas,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type kubernetesResourceWaitEvent struct {
	Name                string `json:"name,omitempty"`
	Namespace           string `json:"namespace,omitempty"`
	Type                string `json:"type,omitempty"`
	Reason              string `json:"reason,omitempty"`
	Action              string `json:"action,omitempty"`
	ReportingController string `json:"reportingController,omitempty"`
	Count               int64  `json:"count,omitempty"`
	FirstTimestamp      string `json:"firstTimestamp,omitempty"`
	LastTimestamp       string `json:"lastTimestamp,omitempty"`
	EventTime           string `json:"eventTime,omitempty"`
	MessageDigest       string `json:"messageDigest,omitempty"`
	MessageBytes        int    `json:"messageBytes,omitempty"`
}

type kubernetesResourceWaitObserveReceipt struct {
	APIVersion string                               `json:"apiVersion"`
	Kind       string                               `json:"kind"`
	NodeID     string                               `json:"nodeId"`
	NodeKind   string                               `json:"nodeKind"`
	TargetID   string                               `json:"targetId,omitempty"`
	Phase      string                               `json:"phase"`
	Status     string                               `json:"status"`
	Resource   kubernetesResourceWaitTargetSpec     `json:"resource"`
	State      kubernetesResourceWaitState          `json:"state"`
	GetReceipt kubernetesResourceWaitCommandReceipt `json:"getReceipt"`
	ObservedAt string                               `json:"observedAt"`
}

type kubernetesResourceWaitPlanReceipt struct {
	APIVersion string                           `json:"apiVersion"`
	Kind       string                           `json:"kind"`
	NodeID     string                           `json:"nodeId"`
	NodeKind   string                           `json:"nodeKind"`
	TargetID   string                           `json:"targetId,omitempty"`
	Phase      string                           `json:"phase"`
	Status     string                           `json:"status"`
	Reason     string                           `json:"reason,omitempty"`
	Operation  string                           `json:"operation"`
	Resource   kubernetesResourceWaitTargetSpec `json:"resource"`
	PlannedAt  string                           `json:"plannedAt"`
}

type kubernetesResourceWaitApplyReceipt struct {
	APIVersion string                               `json:"apiVersion"`
	Kind       string                               `json:"kind"`
	NodeID     string                               `json:"nodeId"`
	NodeKind   string                               `json:"nodeKind"`
	TargetID   string                               `json:"targetId,omitempty"`
	Phase      string                               `json:"phase"`
	Status     string                               `json:"status"`
	Reason     string                               `json:"reason,omitempty"`
	Resource   kubernetesResourceWaitTargetSpec     `json:"resource"`
	Changed    bool                                 `json:"changed"`
	Before     kubernetesResourceWaitState          `json:"before"`
	After      kubernetesResourceWaitState          `json:"after"`
	Receipt    kubernetesResourceWaitCommandReceipt `json:"receipt"`
	AppliedAt  string                               `json:"appliedAt"`
}

type kubernetesResourceWaitEventsReceipt struct {
	APIVersion string                               `json:"apiVersion"`
	Kind       string                               `json:"kind"`
	NodeID     string                               `json:"nodeId"`
	NodeKind   string                               `json:"nodeKind"`
	TargetID   string                               `json:"targetId,omitempty"`
	Phase      string                               `json:"phase"`
	Status     string                               `json:"status"`
	Resource   kubernetesResourceWaitTargetSpec     `json:"resource"`
	EventLimit int                                  `json:"eventLimit"`
	Events     []kubernetesResourceWaitEvent        `json:"events,omitempty"`
	Receipt    kubernetesResourceWaitCommandReceipt `json:"receipt"`
	ObservedAt string                               `json:"observedAt"`
}

type kubernetesResourceWaitVerifyReceipt struct {
	APIVersion string                           `json:"apiVersion"`
	Kind       string                           `json:"kind"`
	NodeID     string                           `json:"nodeId"`
	NodeKind   string                           `json:"nodeKind"`
	TargetID   string                           `json:"targetId,omitempty"`
	Phase      string                           `json:"phase"`
	Status     string                           `json:"status"`
	Reason     string                           `json:"reason,omitempty"`
	Resource   kubernetesResourceWaitTargetSpec `json:"resource"`
	State      kubernetesResourceWaitState      `json:"state"`
	Events     []kubernetesResourceWaitEvent    `json:"events,omitempty"`
	VerifiedAt string                           `json:"verifiedAt"`
}

func (e *customNodeExecutor) runKubernetesResourceWaitNode(ctx context.Context, node *runNode, command string) error {
	phase := "k8s-resource-wait"
	if strings.EqualFold(command, "delete") {
		payload := e.kubernetesResourceWaitSkippedPayload(node, phase, "delete does not wait for Kubernetes resources")
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}
	spec := node.Kubernetes
	resource := spec.Resource
	targetID := kubernetesManifestTargetID(spec.Cluster)
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"resource":  kubernetesResourceWaitTarget(resource),
		"transport": strings.TrimSpace(spec.Cluster.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	if e.dryRun || e.diff {
		reason := "diff"
		if e.dryRun {
			reason = "dry-run"
		}
		observe := e.kubernetesResourceWaitObserveReceipt(node, phase, targetID, resource, kubernetesResourceWaitState{}, kubernetesResourceWaitCommandReceipt{Action: "get", Status: "skipped"}, "skipped")
		plan := e.kubernetesResourceWaitPlanReceipt(node, phase, targetID, resource, "skipped", reason)
		events := e.kubernetesResourceWaitEventsReceipt(node, phase, targetID, resource, nil, kubernetesResourceWaitCommandReceipt{Action: "events", Status: "skipped"}, "skipped")
		verify := e.kubernetesResourceWaitVerifyReceipt(node, phase, targetID, resource, kubernetesResourceWaitState{}, nil, "skipped", reason)
		e.recordKubernetesResourceWaitReceipts(node, phase, "skipped", reason, observe, plan, nil, events, verify)
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
	beforeReceipt := runner.Run(ctx, kubernetesResourceWaitGetCommand(spec.Cluster, resource))
	beforeState := kubernetesResourceWaitStateFromReceipt(resource, beforeReceipt)
	observeStatus := beforeReceipt.Status
	if observeStatus == "" {
		observeStatus = "failed"
	}
	observe := e.kubernetesResourceWaitObserveReceipt(node, phase, targetID, resource, beforeState, compactKubernetesResourceWaitCommandReceipt("get", kubernetesResourceWaitGetCommand(spec.Cluster, resource), beforeReceipt), observeStatus)
	plan := e.kubernetesResourceWaitPlanReceipt(node, phase, targetID, resource, "planned", "eligible")

	waitCommand := kubernetesResourceWaitCommand(spec.Cluster, resource)
	waitResult := runner.Run(ctx, waitCommand)
	afterReceipt := runner.Run(ctx, kubernetesResourceWaitGetCommand(spec.Cluster, resource))
	afterState := kubernetesResourceWaitStateFromReceipt(resource, afterReceipt)
	status := "succeeded"
	reason := "resource ready"
	if !nodeStepSucceeded(waitResult.Status) {
		status = "failed"
		reason = firstReceiptMessage(waitResult)
		if strings.TrimSpace(reason) == "" {
			reason = "resource wait failed"
		}
	} else if !kubernetesResourceWaitStateReady(resource, afterState) {
		status = "failed"
		reason = "resource readiness condition was not verified"
	}
	apply := e.kubernetesResourceWaitApplyReceipt(node, phase, targetID, resource, beforeState, afterState, compactKubernetesResourceWaitCommandReceipt("wait", waitCommand, waitResult), status, reason)
	eventsCommand := kubernetesResourceWaitEventsCommand(spec.Cluster, resource)
	eventsResult := runner.Run(ctx, eventsCommand)
	eventItems := parseKubernetesResourceWaitEvents(eventsResult.Stdout, resource.EventLimit)
	events := e.kubernetesResourceWaitEventsReceipt(node, phase, targetID, resource, eventItems, compactKubernetesResourceWaitCommandReceipt("events", eventsCommand, eventsResult), eventsResult.Status)
	verify := e.kubernetesResourceWaitVerifyReceipt(node, phase, targetID, resource, afterState, eventItems, status, reason)

	e.recordKubernetesResourceWaitReceipts(node, phase, status, reason, observe, plan, &apply, events, verify)
	if status != "succeeded" {
		runErr := &RunError{Class: "K8S_RESOURCE_WAIT_FAILED", Message: reason, Digest: computeRunErrorDigest("K8S_RESOURCE_WAIT_FAILED", reason)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, reason, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("kubernetes resource wait failed: %s", reason))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "resource ready", map[string]any{
		"phase":  phase,
		"status": "succeeded",
		"cursor": cursor,
		"ready":  afterState.Ready,
	}, nil)
	return nil
}

func (e *customNodeExecutor) kubernetesResourceWaitObserveReceipt(node *runNode, phase string, targetID string, spec KubernetesResourceSpec, state kubernetesResourceWaitState, receipt kubernetesResourceWaitCommandReceipt, status string) kubernetesResourceWaitObserveReceipt {
	return kubernetesResourceWaitObserveReceipt{
		APIVersion: "torque.dev/k8s-resource-wait-node/v1",
		Kind:       "KubernetesResourceWaitObserveReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Resource:   sanitizeKubernetesResourceSpec(spec),
		State:      state,
		GetReceipt: receipt,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesResourceWaitPlanReceipt(node *runNode, phase string, targetID string, spec KubernetesResourceSpec, status string, reason string) kubernetesResourceWaitPlanReceipt {
	return kubernetesResourceWaitPlanReceipt{
		APIVersion: "torque.dev/k8s-resource-wait-node/v1",
		Kind:       "KubernetesResourceWaitPlanReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Operation:  "wait",
		Resource:   sanitizeKubernetesResourceSpec(spec),
		PlannedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesResourceWaitApplyReceipt(node *runNode, phase string, targetID string, spec KubernetesResourceSpec, before kubernetesResourceWaitState, after kubernetesResourceWaitState, receipt kubernetesResourceWaitCommandReceipt, status string, reason string) kubernetesResourceWaitApplyReceipt {
	return kubernetesResourceWaitApplyReceipt{
		APIVersion: "torque.dev/k8s-resource-wait-node/v1",
		Kind:       "KubernetesResourceWaitApplyReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Resource:   sanitizeKubernetesResourceSpec(spec),
		Changed:    false,
		Before:     before,
		After:      after,
		Receipt:    receipt,
		AppliedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesResourceWaitEventsReceipt(node *runNode, phase string, targetID string, spec KubernetesResourceSpec, events []kubernetesResourceWaitEvent, receipt kubernetesResourceWaitCommandReceipt, status string) kubernetesResourceWaitEventsReceipt {
	return kubernetesResourceWaitEventsReceipt{
		APIVersion: "torque.dev/k8s-resource-wait-node/v1",
		Kind:       "KubernetesResourceWaitEventsReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Resource:   sanitizeKubernetesResourceSpec(spec),
		EventLimit: spec.EventLimit,
		Events:     append([]kubernetesResourceWaitEvent(nil), events...),
		Receipt:    receipt,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesResourceWaitVerifyReceipt(node *runNode, phase string, targetID string, spec KubernetesResourceSpec, state kubernetesResourceWaitState, events []kubernetesResourceWaitEvent, status string, reason string) kubernetesResourceWaitVerifyReceipt {
	return kubernetesResourceWaitVerifyReceipt{
		APIVersion: "torque.dev/k8s-resource-wait-node/v1",
		Kind:       "KubernetesResourceWaitVerifyReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Resource:   sanitizeKubernetesResourceSpec(spec),
		State:      state,
		Events:     append([]kubernetesResourceWaitEvent(nil), events...),
		VerifiedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordKubernetesResourceWaitReceipts(node *runNode, phase string, status string, reason string, observe kubernetesResourceWaitObserveReceipt, plan kubernetesResourceWaitPlanReceipt, apply *kubernetesResourceWaitApplyReceipt, events kubernetesResourceWaitEventsReceipt, verify kubernetesResourceWaitVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/k8s-resource-wait-node/v1",
		"kind":       "KubernetesResourceWaitNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"observe":    observe,
		"plan":       plan,
		"events":     events,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	if apply != nil {
		payload["apply"] = *apply
	}
	e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait-plan.json", plan)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait-events.json", events)
	e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, "k8s-resource-wait.json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) kubernetesResourceWaitSkippedPayload(node *runNode, phase string, reason string) map[string]any {
	return map[string]any{
		"apiVersion": "torque.dev/k8s-resource-wait-node/v1",
		"kind":       "KubernetesResourceWaitNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     "skipped",
		"reason":     strings.TrimSpace(reason),
	}
}

func kubernetesResourceWaitGetCommand(cluster KubernetesClusterSpec, spec KubernetesResourceSpec) string {
	return kubernetesClusterKubectlBase(cluster) + kubernetesResourceWaitNamespaceArg(spec) + " get " + transport.ShellQuote(kubernetesResourceWaitTarget(spec)) + " -o json"
}

func kubernetesResourceWaitCommand(cluster KubernetesClusterSpec, spec KubernetesResourceSpec) string {
	args := kubernetesClusterKubectlBase(cluster) + kubernetesResourceWaitNamespaceArg(spec) + " wait --for=" + transport.ShellQuote(strings.TrimSpace(spec.For))
	if strings.TrimSpace(spec.Selector) != "" && strings.TrimSpace(spec.Name) == "" {
		args += " " + transport.ShellQuote(strings.TrimSpace(spec.Kind)) + " -l " + transport.ShellQuote(strings.TrimSpace(spec.Selector))
	} else {
		args += " " + transport.ShellQuote(kubernetesResourceWaitTarget(spec))
	}
	args += " --timeout=" + transport.ShellQuote(kubernetesResourceWaitTimeoutString(spec))
	return args
}

func kubernetesResourceWaitEventsCommand(cluster KubernetesClusterSpec, spec KubernetesResourceSpec) string {
	selectors := []string{}
	if kind := kubernetesResourceWaitEventKind(spec); kind != "" {
		selectors = append(selectors, "involvedObject.kind="+kind)
	}
	if strings.TrimSpace(spec.Name) != "" {
		selectors = append(selectors, "involvedObject.name="+strings.TrimSpace(spec.Name))
	}
	command := kubernetesClusterKubectlBase(cluster) + kubernetesResourceWaitNamespaceArg(spec) + " get events"
	if len(selectors) > 0 {
		command += " --field-selector " + transport.ShellQuote(strings.Join(selectors, ","))
	}
	return command + " -o json"
}

func kubernetesResourceWaitNamespaceArg(spec KubernetesResourceSpec) string {
	if strings.TrimSpace(spec.Namespace) == "" {
		return ""
	}
	return " -n " + transport.ShellQuote(strings.TrimSpace(spec.Namespace))
}

func kubernetesResourceWaitTarget(spec KubernetesResourceSpec) string {
	if strings.TrimSpace(spec.Resource) != "" {
		return strings.TrimSpace(spec.Resource)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return strings.TrimSpace(spec.Kind)
	}
	return strings.TrimSpace(spec.Kind) + "/" + strings.TrimSpace(spec.Name)
}

func splitKubernetesResourceName(resource string) (string, string) {
	resource = strings.TrimSpace(resource)
	if resource == "" {
		return "", ""
	}
	parts := strings.Split(resource, "/")
	if len(parts) < 2 {
		return resource, ""
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[len(parts)-1])
}

func defaultKubernetesResourceWaitFor(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "deployment", "deploy", "deployments", "deployment.apps":
		return "condition=Available"
	default:
		return "condition=Ready"
	}
}

func kubernetesResourceWaitTimeoutString(spec KubernetesResourceSpec) string {
	if spec.Timeout == nil || *spec.Timeout <= 0 {
		return defaultKubernetesResourceWaitTimeout.String()
	}
	return spec.Timeout.String()
}

func kubernetesResourceWaitConditionType(waitFor string) string {
	waitFor = strings.TrimSpace(waitFor)
	if strings.HasPrefix(strings.ToLower(waitFor), "condition=") {
		return strings.TrimSpace(waitFor[len("condition="):])
	}
	return ""
}

func kubernetesResourceWaitStateReady(spec KubernetesResourceSpec, state kubernetesResourceWaitState) bool {
	condition := kubernetesResourceWaitConditionType(spec.For)
	if condition == "" {
		return state.Exists
	}
	return state.Exists && strings.EqualFold(strings.TrimSpace(state.Condition), condition) && strings.EqualFold(strings.TrimSpace(state.ConditionStatus), "true")
}

func kubernetesResourceWaitStateFromReceipt(spec KubernetesResourceSpec, receipt transport.OperationResult) kubernetesResourceWaitState {
	state := kubernetesResourceWaitState{
		Namespace: strings.TrimSpace(spec.Namespace),
		Resource:  kubernetesResourceWaitTarget(spec),
		Kind:      strings.TrimSpace(spec.Kind),
		Name:      strings.TrimSpace(spec.Name),
		Condition: kubernetesResourceWaitConditionType(spec.For),
	}
	if !nodeStepSucceeded(receipt.Status) || strings.TrimSpace(receipt.Stdout) == "" {
		state.Exists = false
		state.Error = "not found or inaccessible"
		return state
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(receipt.Stdout), &obj); err != nil {
		state.Exists = false
		state.Error = "invalid json from kubectl get"
		return state
	}
	state.Exists = true
	if raw, err := json.Marshal(obj); err == nil {
		state.ObjectDigest = "sha256:" + hashBytes(raw)
	}
	metadata := asStringAnyMap(obj["metadata"])
	status := asStringAnyMap(obj["status"])
	if state.Kind == "" {
		state.Kind = strings.TrimSpace(stringFromAny(obj["kind"]))
	}
	if state.Name == "" {
		state.Name = strings.TrimSpace(stringFromAny(metadata["name"]))
	}
	if state.Namespace == "" {
		state.Namespace = strings.TrimSpace(stringFromAny(metadata["namespace"]))
	}
	if value := strings.TrimSpace(stringFromAny(metadata["uid"])); value != "" {
		state.UIDDigest = digestString(value)
	}
	if value := strings.TrimSpace(stringFromAny(metadata["resourceVersion"])); value != "" {
		state.ResourceVersionDigest = digestString(value)
	}
	state.Generation = int64FromAny(metadata["generation"])
	state.ObservedGeneration = int64FromAny(status["observedGeneration"])
	state.Replicas = int64FromAny(status["replicas"])
	state.ReadyReplicas = int64FromAny(status["readyReplicas"])
	state.AvailableReplicas = int64FromAny(status["availableReplicas"])
	state.UpdatedReplicas = int64FromAny(status["updatedReplicas"])
	condition := kubernetesResourceWaitConditionType(spec.For)
	for _, item := range asAnySlice(status["conditions"]) {
		cond := asStringAnyMap(item)
		if condition == "" || strings.EqualFold(strings.TrimSpace(stringFromAny(cond["type"])), condition) {
			state.Condition = strings.TrimSpace(stringFromAny(cond["type"]))
			state.ConditionStatus = strings.TrimSpace(stringFromAny(cond["status"]))
			state.Reason = strings.TrimSpace(stringFromAny(cond["reason"]))
			break
		}
	}
	state.Ready = kubernetesResourceWaitStateReady(spec, state)
	return state
}

func parseKubernetesResourceWaitEvents(raw string, limit int) []kubernetesResourceWaitEvent {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil
	}
	out := make([]kubernetesResourceWaitEvent, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		metadata := asStringAnyMap(item["metadata"])
		involved := asStringAnyMap(item["involvedObject"])
		series := asStringAnyMap(item["series"])
		message := strings.TrimSpace(stringFromAny(item["message"]))
		count := int64FromAny(item["count"])
		if count == 0 {
			count = int64FromAny(series["count"])
		}
		event := kubernetesResourceWaitEvent{
			Name:                strings.TrimSpace(stringFromAny(metadata["name"])),
			Namespace:           firstNonEmptyString(strings.TrimSpace(stringFromAny(metadata["namespace"])), strings.TrimSpace(stringFromAny(involved["namespace"]))),
			Type:                strings.TrimSpace(stringFromAny(item["type"])),
			Reason:              strings.TrimSpace(stringFromAny(item["reason"])),
			Action:              strings.TrimSpace(stringFromAny(item["action"])),
			ReportingController: strings.TrimSpace(stringFromAny(item["reportingController"])),
			Count:               count,
			FirstTimestamp:      strings.TrimSpace(stringFromAny(item["firstTimestamp"])),
			LastTimestamp:       firstNonEmptyString(strings.TrimSpace(stringFromAny(item["lastTimestamp"])), strings.TrimSpace(stringFromAny(series["lastObservedTime"]))),
			EventTime:           strings.TrimSpace(stringFromAny(item["eventTime"])),
			MessageDigest:       digestString(message),
			MessageBytes:        len([]byte(message)),
		}
		out = append(out, event)
	}
	sort.SliceStable(out, func(i, j int) bool {
		left := firstNonEmptyString(out[i].LastTimestamp, out[i].EventTime, out[i].FirstTimestamp, out[i].Name)
		right := firstNonEmptyString(out[j].LastTimestamp, out[j].EventTime, out[j].FirstTimestamp, out[j].Name)
		return left < right
	})
	if limit > 0 && len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

func kubernetesResourceWaitEventKind(spec KubernetesResourceSpec) string {
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" {
		kind, _ = splitKubernetesResourceName(spec.Resource)
	}
	switch strings.ToLower(kind) {
	case "deploy", "deployment", "deployments", "deployment.apps":
		return "Deployment"
	case "statefulset", "statefulsets", "statefulset.apps":
		return "StatefulSet"
	case "daemonset", "daemonsets", "daemonset.apps":
		return "DaemonSet"
	case "pod", "pods":
		return "Pod"
	case "job", "jobs", "job.batch":
		return "Job"
	default:
		return kind
	}
}

func compactKubernetesResourceWaitCommandReceipt(action string, command string, receipt transport.OperationResult) kubernetesResourceWaitCommandReceipt {
	out := kubernetesResourceWaitCommandReceipt{
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

func sanitizeKubernetesResourceSpec(spec KubernetesResourceSpec) kubernetesResourceWaitTargetSpec {
	return kubernetesResourceWaitTargetSpec{
		Namespace:  strings.TrimSpace(spec.Namespace),
		Resource:   strings.TrimSpace(spec.Resource),
		Kind:       strings.TrimSpace(spec.Kind),
		Name:       strings.TrimSpace(spec.Name),
		Selector:   strings.TrimSpace(spec.Selector),
		For:        strings.TrimSpace(spec.For),
		Timeout:    kubernetesResourceWaitTimeoutString(spec),
		EventLimit: spec.EventLimit,
	}
}

func asStringAnyMap(value any) map[string]any {
	if out, ok := value.(map[string]any); ok {
		return out
	}
	return nil
}

func asAnySlice(value any) []any {
	if out, ok := value.([]any); ok {
		return out
	}
	return nil
}

func stringFromAny(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case json.Number:
		return v.String()
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	default:
		return ""
	}
}

func int64FromAny(value any) int64 {
	switch v := value.(type) {
	case int:
		return int64(v)
	case int64:
		return v
	case float64:
		return int64(v)
	case json.Number:
		out, _ := v.Int64()
		return out
	case string:
		out, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return out
	default:
		return 0
	}
}
