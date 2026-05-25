package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const defaultKubernetesEventsCaptureLimit = 100

type kubernetesEventsCaptureCommandReceipt struct {
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

type kubernetesEventsCaptureTargetSpec struct {
	Namespace     string   `json:"namespace,omitempty"`
	Resource      string   `json:"resource,omitempty"`
	Kind          string   `json:"kind,omitempty"`
	Name          string   `json:"name,omitempty"`
	FieldSelector string   `json:"fieldSelector,omitempty"`
	Types         []string `json:"types,omitempty"`
	Reasons       []string `json:"reasons,omitempty"`
	Since         string   `json:"since,omitempty"`
	SinceTime     string   `json:"sinceTime,omitempty"`
	EventLimit    int      `json:"eventLimit,omitempty"`
}

type kubernetesEventsCaptureNamespaceState struct {
	Exists                bool   `json:"exists"`
	Namespace             string `json:"namespace,omitempty"`
	UIDDigest             string `json:"uidDigest,omitempty"`
	ResourceVersionDigest string `json:"resourceVersionDigest,omitempty"`
	ObjectDigest          string `json:"objectDigest,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type kubernetesEventsCaptureInvolvedObject struct {
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind,omitempty"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name,omitempty"`
	UIDDigest  string `json:"uidDigest,omitempty"`
	FieldPath  string `json:"fieldPath,omitempty"`
}

type kubernetesEventsCaptureEvent struct {
	Name                string                                `json:"name,omitempty"`
	Namespace           string                                `json:"namespace,omitempty"`
	Type                string                                `json:"type,omitempty"`
	Reason              string                                `json:"reason,omitempty"`
	Action              string                                `json:"action,omitempty"`
	ReportingController string                                `json:"reportingController,omitempty"`
	Count               int64                                 `json:"count,omitempty"`
	FirstTimestamp      string                                `json:"firstTimestamp,omitempty"`
	LastTimestamp       string                                `json:"lastTimestamp,omitempty"`
	EventTime           string                                `json:"eventTime,omitempty"`
	CreationTimestamp   string                                `json:"creationTimestamp,omitempty"`
	InvolvedObject      kubernetesEventsCaptureInvolvedObject `json:"involvedObject"`
	MessageDigest       string                                `json:"messageDigest,omitempty"`
	MessageBytes        int                                   `json:"messageBytes,omitempty"`
}

type kubernetesEventsCaptureEvidence struct {
	EventLimit       int                            `json:"eventLimit"`
	ObservedCount    int                            `json:"observedCount"`
	MatchedCount     int                            `json:"matchedCount"`
	CapturedCount    int                            `json:"capturedCount"`
	FilteredOutCount int                            `json:"filteredOutCount"`
	Truncated        bool                           `json:"truncated,omitempty"`
	TypeCounts       map[string]int                 `json:"typeCounts,omitempty"`
	ReasonCounts     map[string]int                 `json:"reasonCounts,omitempty"`
	Events           []kubernetesEventsCaptureEvent `json:"events,omitempty"`
	Redaction        hostCommandRedactionProof      `json:"redaction"`
}

type kubernetesEventsCaptureObserveReceipt struct {
	APIVersion string                                `json:"apiVersion"`
	Kind       string                                `json:"kind"`
	NodeID     string                                `json:"nodeId"`
	NodeKind   string                                `json:"nodeKind"`
	TargetID   string                                `json:"targetId,omitempty"`
	Phase      string                                `json:"phase"`
	Status     string                                `json:"status"`
	Events     kubernetesEventsCaptureTargetSpec     `json:"events"`
	State      kubernetesEventsCaptureNamespaceState `json:"state"`
	GetReceipt kubernetesEventsCaptureCommandReceipt `json:"getReceipt"`
	ObservedAt string                                `json:"observedAt"`
}

type kubernetesEventsCapturePlanReceipt struct {
	APIVersion string                            `json:"apiVersion"`
	Kind       string                            `json:"kind"`
	NodeID     string                            `json:"nodeId"`
	NodeKind   string                            `json:"nodeKind"`
	TargetID   string                            `json:"targetId,omitempty"`
	Phase      string                            `json:"phase"`
	Status     string                            `json:"status"`
	Reason     string                            `json:"reason,omitempty"`
	Operation  string                            `json:"operation"`
	Events     kubernetesEventsCaptureTargetSpec `json:"events"`
	PlannedAt  string                            `json:"plannedAt"`
}

type kubernetesEventsCaptureEventsReceipt struct {
	APIVersion string                                `json:"apiVersion"`
	Kind       string                                `json:"kind"`
	NodeID     string                                `json:"nodeId"`
	NodeKind   string                                `json:"nodeKind"`
	TargetID   string                                `json:"targetId,omitempty"`
	Phase      string                                `json:"phase"`
	Status     string                                `json:"status"`
	Reason     string                                `json:"reason,omitempty"`
	Events     kubernetesEventsCaptureTargetSpec     `json:"events"`
	Changed    bool                                  `json:"changed"`
	Evidence   kubernetesEventsCaptureEvidence       `json:"evidence"`
	Receipt    kubernetesEventsCaptureCommandReceipt `json:"receipt"`
	CapturedAt string                                `json:"capturedAt"`
}

type kubernetesEventsCaptureVerifyReceipt struct {
	APIVersion    string                            `json:"apiVersion"`
	Kind          string                            `json:"kind"`
	NodeID        string                            `json:"nodeId"`
	NodeKind      string                            `json:"nodeKind"`
	TargetID      string                            `json:"targetId,omitempty"`
	Phase         string                            `json:"phase"`
	Status        string                            `json:"status"`
	Reason        string                            `json:"reason,omitempty"`
	Events        kubernetesEventsCaptureTargetSpec `json:"events"`
	CapturedCount int                               `json:"capturedCount"`
	TypeCounts    map[string]int                    `json:"typeCounts,omitempty"`
	ReasonCounts  map[string]int                    `json:"reasonCounts,omitempty"`
	Redaction     hostCommandRedactionProof         `json:"redaction"`
	VerifiedAt    string                            `json:"verifiedAt"`
}

func (e *customNodeExecutor) runKubernetesEventsCaptureNode(ctx context.Context, node *runNode, command string) error {
	phase := "k8s-events-capture"
	if strings.EqualFold(command, "delete") {
		payload := e.kubernetesEventsCaptureSkippedPayload(node, phase, "delete does not capture Kubernetes events")
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}
	spec := node.Kubernetes
	eventsSpec := spec.Events
	targetID := kubernetesManifestTargetID(spec.Cluster)
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"events":    kubernetesEventsCaptureTarget(eventsSpec),
		"transport": strings.TrimSpace(spec.Cluster.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	if e.dryRun || e.diff {
		reason := "diff"
		if e.dryRun {
			reason = "dry-run"
		}
		observe := e.kubernetesEventsCaptureObserveReceipt(node, phase, targetID, eventsSpec, kubernetesEventsCaptureNamespaceState{}, kubernetesEventsCaptureCommandReceipt{Action: "get-namespace", Status: "skipped"}, "skipped")
		plan := e.kubernetesEventsCapturePlanReceipt(node, phase, targetID, eventsSpec, "skipped", reason)
		capture := e.kubernetesEventsCaptureEventsReceipt(node, phase, targetID, eventsSpec, kubernetesEventsCaptureEvidence{}, kubernetesEventsCaptureCommandReceipt{Action: "events", Status: "skipped"}, "skipped", reason)
		verify := e.kubernetesEventsCaptureVerifyReceipt(node, phase, targetID, eventsSpec, kubernetesEventsCaptureEvidence{}, "skipped", reason)
		e.recordKubernetesEventsCaptureReceipts(node, phase, "skipped", reason, observe, plan, capture, verify)
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
	observeCommand := kubernetesEventsCaptureObserveCommand(spec.Cluster, eventsSpec)
	observeResult := runner.Run(ctx, observeCommand)
	state := kubernetesEventsCaptureNamespaceStateFromReceipt(eventsSpec, observeResult)
	observeStatus := observeResult.Status
	if observeStatus == "" {
		observeStatus = "failed"
	}
	observe := e.kubernetesEventsCaptureObserveReceipt(node, phase, targetID, eventsSpec, state, compactKubernetesEventsCaptureCommandReceipt("get-namespace", observeCommand, observeResult), observeStatus)
	plan := e.kubernetesEventsCapturePlanReceipt(node, phase, targetID, eventsSpec, "planned", "eligible")

	eventsCommand := kubernetesEventsCaptureCommand(spec.Cluster, eventsSpec)
	eventsResult := runner.Run(ctx, eventsCommand)
	evidence, parseErr := kubernetesEventsCaptureEvidenceFromOutput(eventsResult.Stdout, eventsSpec)
	status := "succeeded"
	reason := "events captured"
	if !nodeStepSucceeded(eventsResult.Status) {
		status = "failed"
		reason = firstReceiptMessage(eventsResult)
		if strings.TrimSpace(reason) == "" {
			reason = "kubectl events capture failed"
		}
	} else if parseErr != nil {
		status = "failed"
		reason = parseErr.Error()
	} else if !kubernetesEventsCaptureRedactionVerified(evidence.Redaction) {
		status = "failed"
		reason = "captured event evidence failed redaction scan"
	}
	capture := e.kubernetesEventsCaptureEventsReceipt(node, phase, targetID, eventsSpec, evidence, compactKubernetesEventsCaptureCommandReceipt("events", eventsCommand, eventsResult), status, reason)
	verify := e.kubernetesEventsCaptureVerifyReceipt(node, phase, targetID, eventsSpec, evidence, status, reason)
	e.recordKubernetesEventsCaptureReceipts(node, phase, status, reason, observe, plan, capture, verify)
	if status != "succeeded" {
		runErr := &RunError{Class: "K8S_EVENTS_CAPTURE_FAILED", Message: reason, Digest: computeRunErrorDigest("K8S_EVENTS_CAPTURE_FAILED", reason)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, reason, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("kubernetes events capture failed: %s", reason))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "events captured", map[string]any{
		"phase":    phase,
		"status":   "succeeded",
		"cursor":   cursor,
		"captured": evidence.CapturedCount,
	}, nil)
	return nil
}

func (e *customNodeExecutor) kubernetesEventsCaptureObserveReceipt(node *runNode, phase string, targetID string, spec KubernetesEventsSpec, state kubernetesEventsCaptureNamespaceState, receipt kubernetesEventsCaptureCommandReceipt, status string) kubernetesEventsCaptureObserveReceipt {
	return kubernetesEventsCaptureObserveReceipt{
		APIVersion: "torque.dev/k8s-events-capture-node/v1",
		Kind:       "KubernetesEventsCaptureObserveReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Events:     sanitizeKubernetesEventsSpec(spec),
		State:      state,
		GetReceipt: receipt,
		ObservedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesEventsCapturePlanReceipt(node *runNode, phase string, targetID string, spec KubernetesEventsSpec, status string, reason string) kubernetesEventsCapturePlanReceipt {
	return kubernetesEventsCapturePlanReceipt{
		APIVersion: "torque.dev/k8s-events-capture-node/v1",
		Kind:       "KubernetesEventsCapturePlanReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Operation:  "capture",
		Events:     sanitizeKubernetesEventsSpec(spec),
		PlannedAt:  time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesEventsCaptureEventsReceipt(node *runNode, phase string, targetID string, spec KubernetesEventsSpec, evidence kubernetesEventsCaptureEvidence, receipt kubernetesEventsCaptureCommandReceipt, status string, reason string) kubernetesEventsCaptureEventsReceipt {
	return kubernetesEventsCaptureEventsReceipt{
		APIVersion: "torque.dev/k8s-events-capture-node/v1",
		Kind:       "KubernetesEventsCaptureEventsReceipt",
		NodeID:     node.ID,
		NodeKind:   normalizeNodeKind(node.Kind),
		TargetID:   strings.TrimSpace(targetID),
		Phase:      phase,
		Status:     strings.TrimSpace(status),
		Reason:     strings.TrimSpace(reason),
		Events:     sanitizeKubernetesEventsSpec(spec),
		Changed:    false,
		Evidence:   evidence,
		Receipt:    receipt,
		CapturedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesEventsCaptureVerifyReceipt(node *runNode, phase string, targetID string, spec KubernetesEventsSpec, evidence kubernetesEventsCaptureEvidence, status string, reason string) kubernetesEventsCaptureVerifyReceipt {
	return kubernetesEventsCaptureVerifyReceipt{
		APIVersion:    "torque.dev/k8s-events-capture-node/v1",
		Kind:          "KubernetesEventsCaptureVerifyReceipt",
		NodeID:        node.ID,
		NodeKind:      normalizeNodeKind(node.Kind),
		TargetID:      strings.TrimSpace(targetID),
		Phase:         phase,
		Status:        strings.TrimSpace(status),
		Reason:        strings.TrimSpace(reason),
		Events:        sanitizeKubernetesEventsSpec(spec),
		CapturedCount: evidence.CapturedCount,
		TypeCounts:    copyStringIntMap(evidence.TypeCounts),
		ReasonCounts:  copyStringIntMap(evidence.ReasonCounts),
		Redaction:     evidence.Redaction,
		VerifiedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordKubernetesEventsCaptureReceipts(node *runNode, phase string, status string, reason string, observe kubernetesEventsCaptureObserveReceipt, plan kubernetesEventsCapturePlanReceipt, capture kubernetesEventsCaptureEventsReceipt, verify kubernetesEventsCaptureVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/k8s-events-capture-node/v1",
		"kind":       "KubernetesEventsCaptureNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
		"observe":    observe,
		"plan":       plan,
		"events":     capture,
		"verify":     verify,
	}
	if strings.TrimSpace(reason) != "" {
		payload["reason"] = strings.TrimSpace(reason)
	}
	e.run.RecordJSONArtifact(node.ID, "k8s-events-capture-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "k8s-events-capture-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "k8s-events-capture-events.json", capture)
	e.run.RecordJSONArtifact(node.ID, "k8s-events-capture-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, "k8s-events-capture.json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) kubernetesEventsCaptureSkippedPayload(node *runNode, phase string, reason string) map[string]any {
	return map[string]any{
		"apiVersion": "torque.dev/k8s-events-capture-node/v1",
		"kind":       "KubernetesEventsCaptureNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     "skipped",
		"reason":     strings.TrimSpace(reason),
	}
}

func kubernetesEventsCaptureObserveCommand(cluster KubernetesClusterSpec, spec KubernetesEventsSpec) string {
	return kubernetesClusterKubectlBase(cluster) + " get namespace " + transport.ShellQuote(strings.TrimSpace(spec.Namespace)) + " -o json"
}

func kubernetesEventsCaptureCommand(cluster KubernetesClusterSpec, spec KubernetesEventsSpec) string {
	command := kubernetesClusterKubectlBase(cluster) + " -n " + transport.ShellQuote(strings.TrimSpace(spec.Namespace)) + " get events"
	if selector := kubernetesEventsCaptureFieldSelector(spec); selector != "" {
		command += " --field-selector " + transport.ShellQuote(selector)
	}
	return command + " -o json"
}

func kubernetesEventsCaptureFieldSelector(spec KubernetesEventsSpec) string {
	selectors := []string{}
	if strings.TrimSpace(spec.FieldSelector) != "" {
		for _, item := range strings.Split(spec.FieldSelector, ",") {
			if item = strings.TrimSpace(item); item != "" {
				selectors = append(selectors, item)
			}
		}
	}
	if kind := kubernetesEventsCaptureInvolvedKind(spec); kind != "" {
		selectors = append(selectors, "involvedObject.kind="+kind)
	}
	if strings.TrimSpace(spec.Name) != "" {
		selectors = append(selectors, "involvedObject.name="+strings.TrimSpace(spec.Name))
	}
	return strings.Join(selectors, ",")
}

func kubernetesEventsCaptureTarget(spec KubernetesEventsSpec) string {
	if strings.TrimSpace(spec.Resource) != "" {
		return strings.TrimSpace(spec.Resource)
	}
	if strings.TrimSpace(spec.Name) == "" {
		return strings.TrimSpace(spec.Kind)
	}
	return strings.TrimSpace(spec.Kind) + "/" + strings.TrimSpace(spec.Name)
}

func kubernetesEventsCaptureInvolvedKind(spec KubernetesEventsSpec) string {
	kind := strings.TrimSpace(spec.Kind)
	if kind == "" {
		kind, _ = splitKubernetesResourceName(spec.Resource)
	}
	return kubernetesResourceWaitEventKind(KubernetesResourceSpec{Kind: kind})
}

func kubernetesEventsCaptureNamespaceStateFromReceipt(spec KubernetesEventsSpec, receipt transport.OperationResult) kubernetesEventsCaptureNamespaceState {
	state := kubernetesEventsCaptureNamespaceState{Namespace: strings.TrimSpace(spec.Namespace)}
	if !nodeStepSucceeded(receipt.Status) || strings.TrimSpace(receipt.Stdout) == "" {
		state.Exists = false
		state.Error = "namespace not found or inaccessible"
		return state
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(receipt.Stdout), &obj); err != nil {
		state.Exists = true
		state.ObjectDigest = digestBytes([]byte(receipt.Stdout))
		state.Error = "kubectl namespace output was redacted or invalid json"
		return state
	}
	metadata := asStringAnyMap(obj["metadata"])
	state.Exists = true
	if raw, err := json.Marshal(obj); err == nil {
		state.ObjectDigest = digestBytes(raw)
	}
	if value := strings.TrimSpace(stringFromAny(metadata["name"])); value != "" {
		state.Namespace = value
	}
	if value := strings.TrimSpace(stringFromAny(metadata["uid"])); value != "" {
		state.UIDDigest = digestString(value)
	}
	if value := strings.TrimSpace(stringFromAny(metadata["resourceVersion"])); value != "" {
		state.ResourceVersionDigest = digestString(value)
	}
	return state
}

func kubernetesEventsCaptureEvidenceFromOutput(raw string, spec KubernetesEventsSpec) (kubernetesEventsCaptureEvidence, error) {
	evidence := kubernetesEventsCaptureEvidence{
		EventLimit:   spec.EventLimit,
		TypeCounts:   map[string]int{},
		ReasonCounts: map[string]int{},
	}
	var decoded struct {
		Items []map[string]any `json:"items"`
	}
	if strings.TrimSpace(raw) == "" {
		evidence.Redaction = hostCommandRedaction(transport.OperationResult{})
		return evidence, nil
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return evidence, fmt.Errorf("invalid json from kubectl get events")
	}
	evidence.ObservedCount = len(decoded.Items)
	cutoff, hasCutoff := kubernetesEventsCaptureCutoff(spec)
	messages := make([]string, 0, len(decoded.Items))
	matched := make([]kubernetesEventsCaptureEvent, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		event := kubernetesEventsCaptureEventFromMap(item)
		if !kubernetesEventsCaptureEventMatches(event, spec, cutoff, hasCutoff) {
			continue
		}
		matched = append(matched, event)
		if strings.TrimSpace(event.Type) != "" {
			evidence.TypeCounts[event.Type]++
		}
		if strings.TrimSpace(event.Reason) != "" {
			evidence.ReasonCounts[event.Reason]++
		}
		if message := strings.TrimSpace(stringFromAny(item["message"])); message != "" {
			messages = append(messages, message)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool {
		left := kubernetesEventsCaptureSortKey(matched[i])
		right := kubernetesEventsCaptureSortKey(matched[j])
		return left < right
	})
	evidence.MatchedCount = len(matched)
	evidence.FilteredOutCount = evidence.ObservedCount - evidence.MatchedCount
	evidence.Events = matched
	if spec.EventLimit > 0 && len(evidence.Events) > spec.EventLimit {
		evidence.Events = evidence.Events[len(evidence.Events)-spec.EventLimit:]
		evidence.Truncated = true
	}
	evidence.CapturedCount = len(evidence.Events)
	evidence.Redaction = hostCommandRedaction(transport.OperationResult{Stdout: strings.Join(messages, "\n")})
	if len(evidence.TypeCounts) == 0 {
		evidence.TypeCounts = nil
	}
	if len(evidence.ReasonCounts) == 0 {
		evidence.ReasonCounts = nil
	}
	return evidence, nil
}

func kubernetesEventsCaptureEventFromMap(item map[string]any) kubernetesEventsCaptureEvent {
	metadata := asStringAnyMap(item["metadata"])
	involved := asStringAnyMap(item["involvedObject"])
	series := asStringAnyMap(item["series"])
	message := strings.TrimSpace(stringFromAny(item["message"]))
	count := int64FromAny(item["count"])
	if count == 0 {
		count = int64FromAny(series["count"])
	}
	return kubernetesEventsCaptureEvent{
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
		CreationTimestamp:   strings.TrimSpace(stringFromAny(metadata["creationTimestamp"])),
		InvolvedObject: kubernetesEventsCaptureInvolvedObject{
			APIVersion: strings.TrimSpace(stringFromAny(involved["apiVersion"])),
			Kind:       strings.TrimSpace(stringFromAny(involved["kind"])),
			Namespace:  strings.TrimSpace(stringFromAny(involved["namespace"])),
			Name:       strings.TrimSpace(stringFromAny(involved["name"])),
			UIDDigest:  digestString(stringFromAny(involved["uid"])),
			FieldPath:  strings.TrimSpace(stringFromAny(involved["fieldPath"])),
		},
		MessageDigest: digestString(message),
		MessageBytes:  len([]byte(message)),
	}
}

func kubernetesEventsCaptureEventMatches(event kubernetesEventsCaptureEvent, spec KubernetesEventsSpec, cutoff time.Time, hasCutoff bool) bool {
	if len(spec.Types) > 0 && !stringSliceContainsFold(spec.Types, event.Type) {
		return false
	}
	if len(spec.Reasons) > 0 && !stringSliceContainsFold(spec.Reasons, event.Reason) {
		return false
	}
	if hasCutoff {
		eventTime, ok := kubernetesEventsCaptureEventTime(event)
		if ok && eventTime.Before(cutoff) {
			return false
		}
	}
	return true
}

func kubernetesEventsCaptureCutoff(spec KubernetesEventsSpec) (time.Time, bool) {
	if strings.TrimSpace(spec.SinceTime) != "" {
		if parsed, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(spec.SinceTime)); err == nil {
			return parsed.UTC(), true
		}
		return time.Time{}, false
	}
	if spec.Since != nil && *spec.Since > 0 {
		return time.Now().UTC().Add(-*spec.Since), true
	}
	return time.Time{}, false
}

func kubernetesEventsCaptureEventTime(event kubernetesEventsCaptureEvent) (time.Time, bool) {
	for _, value := range []string{event.LastTimestamp, event.EventTime, event.FirstTimestamp, event.CreationTimestamp} {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
			return parsed.UTC(), true
		}
	}
	return time.Time{}, false
}

func kubernetesEventsCaptureSortKey(event kubernetesEventsCaptureEvent) string {
	return firstNonEmptyString(event.LastTimestamp, event.EventTime, event.FirstTimestamp, event.CreationTimestamp, event.Name)
}

func stringSliceContainsFold(values []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, value := range values {
		if strings.EqualFold(strings.TrimSpace(value), want) {
			return true
		}
	}
	return false
}

func kubernetesEventsCaptureRedactionVerified(redaction hostCommandRedactionProof) bool {
	return redaction.NoSecretRefs && redaction.NoSensitiveKV && redaction.NoAuthorizationBearer
}

func compactKubernetesEventsCaptureCommandReceipt(action string, command string, receipt transport.OperationResult) kubernetesEventsCaptureCommandReceipt {
	out := kubernetesEventsCaptureCommandReceipt{
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

func sanitizeKubernetesEventsSpec(spec KubernetesEventsSpec) kubernetesEventsCaptureTargetSpec {
	out := kubernetesEventsCaptureTargetSpec{
		Namespace:     strings.TrimSpace(spec.Namespace),
		Resource:      strings.TrimSpace(spec.Resource),
		Kind:          strings.TrimSpace(spec.Kind),
		Name:          strings.TrimSpace(spec.Name),
		FieldSelector: strings.TrimSpace(spec.FieldSelector),
		Types:         append([]string(nil), spec.Types...),
		Reasons:       append([]string(nil), spec.Reasons...),
		SinceTime:     strings.TrimSpace(spec.SinceTime),
		EventLimit:    spec.EventLimit,
	}
	if spec.Since != nil && *spec.Since > 0 {
		out.Since = spec.Since.String()
	}
	return out
}

func copyStringIntMap(in map[string]int) map[string]int {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]int, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}
