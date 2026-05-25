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

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

type kubernetesManifestResourceRef struct {
	ID         string `json:"id"`
	Scope      string `json:"scope"`
	APIVersion string `json:"apiVersion,omitempty"`
	Kind       string `json:"kind"`
	Namespace  string `json:"namespace,omitempty"`
	Name       string `json:"name"`
}

type kubernetesManifestObservedResource struct {
	kubernetesManifestResourceRef
	Exists                bool   `json:"exists"`
	Owned                 bool   `json:"owned"`
	Manager               string `json:"manager,omitempty"`
	UIDDigest             string `json:"uidDigest,omitempty"`
	ResourceVersionDigest string `json:"resourceVersionDigest,omitempty"`
	Generation            int64  `json:"generation,omitempty"`
	ObjectDigest          string `json:"objectDigest,omitempty"`
	Error                 string `json:"error,omitempty"`
}

type kubernetesManifestState struct {
	Resources []kubernetesManifestObservedResource `json:"resources"`
}

type kubernetesManifestChangeSet struct {
	Objects bool `json:"objects"`
}

type kubernetesManifestCommandReceipt struct {
	Action        string `json:"action"`
	Status        string `json:"status"`
	ExitCode      int    `json:"exitCode"`
	CommandDigest string `json:"commandDigest,omitempty"`
	StdoutDigest  string `json:"stdoutDigest,omitempty"`
	StderrDigest  string `json:"stderrDigest,omitempty"`
	StdoutBytes   int    `json:"stdoutBytes,omitempty"`
	StderrBytes   int    `json:"stderrBytes,omitempty"`
}

type kubernetesManifestOperationResult struct {
	APIVersion     string                             `json:"apiVersion"`
	Kind           string                             `json:"kind"`
	Operation      string                             `json:"operation"`
	Status         string                             `json:"status"`
	Reason         string                             `json:"reason,omitempty"`
	TargetDigest   string                             `json:"targetDigest,omitempty"`
	Namespace      string                             `json:"namespace,omitempty"`
	FieldManager   string                             `json:"fieldManager"`
	DesiredState   string                             `json:"desiredState,omitempty"`
	ManifestDigest string                             `json:"manifestDigest"`
	Changed        bool                               `json:"changed"`
	Changes        kubernetesManifestChangeSet        `json:"changes"`
	Before         kubernetesManifestState            `json:"before"`
	After          kubernetesManifestState            `json:"after"`
	Commands       []kubernetesManifestCommandReceipt `json:"commands,omitempty"`
	Error          string                             `json:"error,omitempty"`
	CompletedAt    string                             `json:"completedAt"`
}

type kubernetesManifestObserveReceipt struct {
	APIVersion     string                          `json:"apiVersion"`
	Kind           string                          `json:"kind"`
	NodeID         string                          `json:"nodeId"`
	NodeKind       string                          `json:"nodeKind"`
	TargetID       string                          `json:"targetId,omitempty"`
	Phase          string                          `json:"phase"`
	Status         string                          `json:"status"`
	Namespace      string                          `json:"namespace,omitempty"`
	FieldManager   string                          `json:"fieldManager"`
	TargetDigest   string                          `json:"targetDigest,omitempty"`
	ManifestDigest string                          `json:"manifestDigest,omitempty"`
	Resources      []kubernetesManifestResourceRef `json:"resources,omitempty"`
	State          kubernetesManifestState         `json:"state"`
	ObservedAt     string                          `json:"observedAt"`
}

type kubernetesManifestPlanReceipt struct {
	APIVersion     string                          `json:"apiVersion"`
	Kind           string                          `json:"kind"`
	NodeID         string                          `json:"nodeId"`
	NodeKind       string                          `json:"nodeKind"`
	TargetID       string                          `json:"targetId,omitempty"`
	Phase          string                          `json:"phase"`
	Status         string                          `json:"status"`
	Reason         string                          `json:"reason,omitempty"`
	Operation      string                          `json:"operation"`
	Namespace      string                          `json:"namespace,omitempty"`
	FieldManager   string                          `json:"fieldManager"`
	DesiredState   string                          `json:"desiredState"`
	ManifestDigest string                          `json:"manifestDigest,omitempty"`
	RemoveOnDelete bool                            `json:"removeOnDelete,omitempty"`
	ForceConflicts bool                            `json:"forceConflicts,omitempty"`
	Resources      []kubernetesManifestResourceRef `json:"resources,omitempty"`
	Changes        kubernetesManifestChangeSet     `json:"changes"`
	PlannedAt      string                          `json:"plannedAt"`
}

type kubernetesManifestDiffReceipt struct {
	APIVersion     string                             `json:"apiVersion"`
	Kind           string                             `json:"kind"`
	NodeID         string                             `json:"nodeId"`
	TargetID       string                             `json:"targetId,omitempty"`
	Phase          string                             `json:"phase"`
	Status         string                             `json:"status"`
	Namespace      string                             `json:"namespace,omitempty"`
	FieldManager   string                             `json:"fieldManager"`
	ManifestDigest string                             `json:"manifestDigest,omitempty"`
	Resources      []kubernetesManifestResourceRef    `json:"resources,omitempty"`
	Changed        bool                               `json:"changed"`
	Changes        kubernetesManifestChangeSet        `json:"changes"`
	DiffQuality    string                             `json:"diffQuality"`
	Commands       []kubernetesManifestCommandReceipt `json:"commands,omitempty"`
	GeneratedAt    string                             `json:"generatedAt"`
}

type kubernetesManifestVerifyReceipt struct {
	APIVersion     string                               `json:"apiVersion"`
	Kind           string                               `json:"kind"`
	NodeID         string                               `json:"nodeId"`
	TargetID       string                               `json:"targetId,omitempty"`
	Phase          string                               `json:"phase"`
	Status         string                               `json:"status"`
	Reason         string                               `json:"reason,omitempty"`
	Namespace      string                               `json:"namespace,omitempty"`
	FieldManager   string                               `json:"fieldManager"`
	DesiredState   string                               `json:"desiredState"`
	ManifestDigest string                               `json:"manifestDigest,omitempty"`
	Changed        bool                                 `json:"changed"`
	Resources      []kubernetesManifestObservedResource `json:"resources,omitempty"`
	VerifiedAt     string                               `json:"verifiedAt"`
}

func (e *customNodeExecutor) runKubernetesManifestApplyNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Kubernetes
	manifestSpec := spec.Manifest
	phase := "k8s-manifest"
	operation := "apply"
	desiredState := "present"
	if strings.EqualFold(command, "delete") {
		phase = "delete-k8s-manifest"
		operation = "delete"
		desiredState = "absent"
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Cluster.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	manifest, err := renderKubernetesManifestContent(node)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	manifestDigest := digestBytes(manifest)
	resources, err := kubernetesManifestResourceRefs(string(manifest), manifestSpec.Namespace)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	if len(resources) == 0 {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s node %s rendered no Kubernetes resources", NodeKindK8sManifestApply, node.Name))
	}
	targetID := kubernetesManifestTargetID(spec.Cluster)
	if e.dryRun {
		reason := "dry-run"
		state := kubernetesManifestState{}
		observe := e.kubernetesManifestObserveReceipt(node, phase, targetID, "", manifestSpec, manifestDigest, resources, state, "skipped")
		plan := e.kubernetesManifestPlanReceipt(node, phase, operation, desiredState, targetID, manifestSpec, manifestDigest, resources, kubernetesManifestChangeSet{}, "skipped", reason)
		diff := e.kubernetesManifestDiffReceipt(node, phase, targetID, manifestSpec, manifestDigest, resources, false, kubernetesManifestChangeSet{}, "skipped", nil)
		verify := e.kubernetesManifestVerifyReceipt(node, phase, targetID, manifestSpec, desiredState, manifestDigest, kubernetesManifestOperationResult{Status: "skipped", Reason: reason})
		e.recordKubernetesManifestReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
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
	targetDigest := runner.TargetDigest()
	observeResult, err := e.runKubernetesManifestOperation(ctx, runner, kubernetesManifestPayload(spec, "observe", desiredState, manifest, resources))
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	observeResult.TargetDigest = targetDigest
	observe := e.kubernetesManifestObserveReceipt(node, phase, targetID, targetDigest, manifestSpec, manifestDigest, resources, observeResult.After, observeResult.Status)

	var diffResult kubernetesManifestOperationResult
	changes := kubernetesManifestChangeSet{}
	if operation == "delete" {
		changes.Objects = kubernetesManifestStateAnyExists(observeResult.After)
		diffResult = kubernetesManifestOperationResult{
			APIVersion:     "torque.dev/k8s-manifest-node/v1",
			Kind:           "KubernetesManifestOperationReceipt",
			Operation:      "delete-plan",
			Status:         "planned",
			TargetDigest:   targetDigest,
			Namespace:      strings.TrimSpace(manifestSpec.Namespace),
			FieldManager:   strings.TrimSpace(manifestSpec.FieldManager),
			DesiredState:   desiredState,
			ManifestDigest: manifestDigest,
			Changed:        changes.Objects,
			Changes:        changes,
			Before:         observeResult.After,
			After:          observeResult.After,
			CompletedAt:    time.Now().UTC().Format(time.RFC3339Nano),
		}
	} else {
		diffResult, err = e.runKubernetesManifestOperation(ctx, runner, kubernetesManifestPayload(spec, "diff", desiredState, manifest, resources))
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		diffResult.TargetDigest = targetDigest
		changes = diffResult.Changes
		if changes == (kubernetesManifestChangeSet{}) {
			changes.Objects = diffResult.Changed
		}
	}
	plan := e.kubernetesManifestPlanReceipt(node, phase, operation, desiredState, targetID, manifestSpec, manifestDigest, resources, changes, "planned", "eligible")
	diff := e.kubernetesManifestDiffReceipt(node, phase, targetID, manifestSpec, manifestDigest, resources, diffResult.Changed, changes, diffResult.Status, diffResult.Commands)

	if e.diff {
		reason := "diff"
		verify := e.kubernetesManifestVerifyReceipt(node, phase, targetID, manifestSpec, desiredState, manifestDigest, kubernetesManifestOperationResult{Status: "skipped", Reason: reason, Before: observeResult.After, After: observeResult.After})
		e.recordKubernetesManifestReceipts(node, phase, "skipped", reason, observe, plan, diff, nil, verify)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "server-side diff complete", map[string]any{
			"phase":   phase,
			"status":  "skipped",
			"changed": diffResult.Changed,
			"cursor":  cursor,
		}, nil)
		return nil
	}

	var result kubernetesManifestOperationResult
	if operation == "delete" {
		result, err = e.runKubernetesManifestOperation(ctx, runner, kubernetesManifestPayload(spec, "delete", desiredState, manifest, resources))
	} else {
		result, err = e.runKubernetesManifestOperation(ctx, runner, kubernetesManifestPayload(spec, "apply", desiredState, manifest, resources))
	}
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	result.TargetDigest = targetDigest
	if result.ManifestDigest == "" {
		result.ManifestDigest = manifestDigest
	}
	if result.Changes == (kubernetesManifestChangeSet{}) {
		result.Changes = changes
	}
	verify := e.kubernetesManifestVerifyReceipt(node, phase, targetID, manifestSpec, desiredState, manifestDigest, result)
	e.recordKubernetesManifestReceipts(node, phase, result.Status, strings.TrimSpace(result.Error), observe, plan, diff, &result, verify)
	if !nodeStepSucceeded(result.Status) || verify.Status == "failed" {
		msg := firstNonEmptyString(result.Error, result.Reason, verify.Reason, "kubernetes manifest apply failed")
		runErr := &RunError{Class: "K8S_MANIFEST_FAILED", Message: msg, Digest: computeRunErrorDigest("K8S_MANIFEST_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
			"result": result,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("kubernetes manifest phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"changed": result.Changed,
		"cursor":  cursor,
		"result":  result,
	}, nil)
	return nil
}

func renderKubernetesManifestContent(node *runNode) ([]byte, error) {
	if node == nil {
		return nil, fmt.Errorf("nil k8s.manifest.apply node")
	}
	spec := node.Kubernetes.Manifest
	if strings.TrimSpace(spec.Content) != "" {
		return []byte(spec.Content), nil
	}
	if strings.TrimSpace(spec.Path) != "" {
		path := spec.Path
		if !filepath.IsAbs(path) {
			path = filepath.Join(node.Dir, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read k8s.manifest.apply manifest: %w", err)
		}
		return raw, nil
	}
	source := spec.Template
	if strings.TrimSpace(source) == "" && strings.TrimSpace(spec.TemplatePath) != "" {
		path := spec.TemplatePath
		if !filepath.IsAbs(path) {
			path = filepath.Join(node.Dir, path)
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read k8s.manifest.apply template: %w", err)
		}
		source = string(raw)
	}
	if strings.TrimSpace(source) == "" {
		return nil, fmt.Errorf("k8s.manifest.apply requires content, path, template, or templatePath")
	}
	tpl, err := template.New("k8s-manifest-apply").Option("missingkey=error").Parse(source)
	if err != nil {
		return nil, fmt.Errorf("parse k8s.manifest.apply template: %w", err)
	}
	data := map[string]any{}
	for k, v := range spec.Data {
		data[k] = v
	}
	data["NodeID"] = node.ID
	data["NodeName"] = node.Name
	data["Namespace"] = strings.TrimSpace(spec.Namespace)
	var out bytes.Buffer
	if err := tpl.Execute(&out, data); err != nil {
		return nil, fmt.Errorf("render k8s.manifest.apply template: %w", err)
	}
	return out.Bytes(), nil
}

func kubernetesManifestResourceRefs(manifest string, defaultNamespace string) ([]kubernetesManifestResourceRef, error) {
	objects, err := parseManifestObjects(manifest)
	if err != nil {
		return nil, err
	}
	out := make([]kubernetesManifestResourceRef, 0, len(objects))
	for _, obj := range objects {
		ref := kubernetesManifestResourceRefFromObject(obj, defaultNamespace)
		if strings.TrimSpace(ref.Kind) == "" || strings.TrimSpace(ref.Name) == "" {
			continue
		}
		out = append(out, ref)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func kubernetesManifestResourceRefFromObject(obj *unstructured.Unstructured, defaultNamespace string) kubernetesManifestResourceRef {
	if obj == nil {
		return kubernetesManifestResourceRef{}
	}
	namespace := effectiveObjectNamespace(obj, defaultNamespace)
	scope := "namespace"
	if namespace == "" {
		scope = "cluster"
	}
	ref := kubernetesManifestResourceRef{
		Scope:      scope,
		APIVersion: strings.TrimSpace(obj.GetAPIVersion()),
		Kind:       strings.TrimSpace(obj.GetKind()),
		Namespace:  namespace,
		Name:       strings.TrimSpace(obj.GetName()),
	}
	ref.ID = kubernetesManifestResourceID(ref)
	return ref
}

func kubernetesManifestResourceID(ref kubernetesManifestResourceRef) string {
	kind := strings.TrimSpace(ref.Kind)
	name := strings.TrimSpace(ref.Name)
	if strings.TrimSpace(ref.Namespace) == "" {
		return "cluster/" + kind + "/" + name
	}
	return strings.TrimSpace(ref.Namespace) + "/" + kind + "/" + name
}

func kubernetesManifestTargetID(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.Target) != "" {
		return strings.TrimSpace(spec.Target)
	}
	if strings.TrimSpace(spec.TargetEnv) != "" {
		if value := strings.TrimSpace(os.Getenv(strings.TrimSpace(spec.TargetEnv))); value != "" {
			return value
		}
		return "$" + strings.TrimSpace(spec.TargetEnv)
	}
	transportKind := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transportKind == "" || transportKind == "local" || transportKind == "localhost" {
		return "local://localhost"
	}
	return transportKind
}

func kubernetesManifestKubectlArgs(spec KubernetesClusterSpec) []string {
	args := kubernetesClusterKubectlCommandParts(spec)
	kubeconfig := strings.TrimSpace(spec.Kubeconfig)
	if kubeconfig == "" && strings.TrimSpace(spec.KubeconfigEnv) != "" {
		kubeconfig = strings.TrimSpace(os.Getenv(strings.TrimSpace(spec.KubeconfigEnv)))
	}
	if kubeconfig != "" {
		args = append(args, "--kubeconfig", kubeconfig)
	}
	if strings.TrimSpace(spec.Context) != "" {
		args = append(args, "--context", strings.TrimSpace(spec.Context))
	}
	return args
}

func kubernetesManifestPayload(spec KubernetesSpec, operation string, desiredState string, manifest []byte, resources []kubernetesManifestResourceRef) map[string]any {
	return map[string]any{
		"operation":      strings.TrimSpace(operation),
		"desiredState":   strings.TrimSpace(desiredState),
		"manifestB64":    base64.StdEncoding.EncodeToString(manifest),
		"manifestDigest": digestBytes(manifest),
		"namespace":      strings.TrimSpace(spec.Manifest.Namespace),
		"fieldManager":   strings.TrimSpace(spec.Manifest.FieldManager),
		"forceConflicts": spec.Manifest.ForceConflicts,
		"removeOnDelete": spec.Manifest.RemoveOnDelete,
		"kubectlArgs":    kubernetesManifestKubectlArgs(spec.Cluster),
		"resources":      resources,
	}
}

func (e *customNodeExecutor) runKubernetesManifestOperation(ctx context.Context, runner hostCommandRunner, payload map[string]any) (kubernetesManifestOperationResult, error) {
	command, err := kubernetesManifestPythonCommand(payload)
	if err != nil {
		return kubernetesManifestOperationResult{}, err
	}
	receipt := runner.Run(ctx, command)
	var result kubernetesManifestOperationResult
	if strings.TrimSpace(receipt.Stdout) != "" {
		if err := json.Unmarshal([]byte(receipt.Stdout), &result); err != nil {
			return kubernetesManifestOperationResult{}, fmt.Errorf("decode k8s.manifest.apply receipt: %w: %s", err, strings.TrimSpace(receipt.Stdout))
		}
	}
	if result.APIVersion == "" {
		result.APIVersion = "torque.dev/k8s-manifest-node/v1"
	}
	if result.Kind == "" {
		result.Kind = "KubernetesManifestOperationReceipt"
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

func kubernetesManifestPythonCommand(payload map[string]any) (string, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	return "TORQUE_K8S_MANIFEST_PAYLOAD_B64=" + transport.ShellQuote(encoded) + " python3 - <<'PY'\n" + kubernetesManifestPythonScript + "\nPY", nil
}

func (e *customNodeExecutor) kubernetesManifestObserveReceipt(node *runNode, phase string, targetID string, targetDigest string, spec KubernetesManifestSpec, manifestDigest string, resources []kubernetesManifestResourceRef, state kubernetesManifestState, status string) kubernetesManifestObserveReceipt {
	return kubernetesManifestObserveReceipt{
		APIVersion:     "torque.dev/k8s-manifest-node/v1",
		Kind:           "KubernetesManifestObserveReceipt",
		NodeID:         node.ID,
		NodeKind:       normalizeNodeKind(node.Kind),
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         strings.TrimSpace(status),
		Namespace:      strings.TrimSpace(spec.Namespace),
		FieldManager:   strings.TrimSpace(spec.FieldManager),
		TargetDigest:   strings.TrimSpace(targetDigest),
		ManifestDigest: manifestDigest,
		Resources:      append([]kubernetesManifestResourceRef(nil), resources...),
		State:          state,
		ObservedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesManifestPlanReceipt(node *runNode, phase string, operation string, desiredState string, targetID string, spec KubernetesManifestSpec, manifestDigest string, resources []kubernetesManifestResourceRef, changes kubernetesManifestChangeSet, status string, reason string) kubernetesManifestPlanReceipt {
	return kubernetesManifestPlanReceipt{
		APIVersion:     "torque.dev/k8s-manifest-node/v1",
		Kind:           "KubernetesManifestPlanReceipt",
		NodeID:         node.ID,
		NodeKind:       normalizeNodeKind(node.Kind),
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         strings.TrimSpace(status),
		Reason:         strings.TrimSpace(reason),
		Operation:      strings.TrimSpace(operation),
		Namespace:      strings.TrimSpace(spec.Namespace),
		FieldManager:   strings.TrimSpace(spec.FieldManager),
		DesiredState:   strings.TrimSpace(desiredState),
		ManifestDigest: manifestDigest,
		RemoveOnDelete: spec.RemoveOnDelete,
		ForceConflicts: spec.ForceConflicts,
		Resources:      append([]kubernetesManifestResourceRef(nil), resources...),
		Changes:        changes,
		PlannedAt:      time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesManifestDiffReceipt(node *runNode, phase string, targetID string, spec KubernetesManifestSpec, manifestDigest string, resources []kubernetesManifestResourceRef, changed bool, changes kubernetesManifestChangeSet, status string, commands []kubernetesManifestCommandReceipt) kubernetesManifestDiffReceipt {
	return kubernetesManifestDiffReceipt{
		APIVersion:     "torque.dev/k8s-manifest-node/v1",
		Kind:           "KubernetesManifestDiffReceipt",
		NodeID:         node.ID,
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         strings.TrimSpace(status),
		Namespace:      strings.TrimSpace(spec.Namespace),
		FieldManager:   strings.TrimSpace(spec.FieldManager),
		ManifestDigest: manifestDigest,
		Resources:      append([]kubernetesManifestResourceRef(nil), resources...),
		Changed:        changed,
		Changes:        changes,
		DiffQuality:    "server-side",
		Commands:       append([]kubernetesManifestCommandReceipt(nil), commands...),
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) kubernetesManifestVerifyReceipt(node *runNode, phase string, targetID string, spec KubernetesManifestSpec, desiredState string, manifestDigest string, result kubernetesManifestOperationResult) kubernetesManifestVerifyReceipt {
	status := strings.TrimSpace(result.Status)
	reason := strings.TrimSpace(firstNonEmptyString(result.Error, result.Reason))
	resources := append([]kubernetesManifestObservedResource(nil), result.After.Resources...)
	if status == "" {
		status = "failed"
	}
	if nodeStepSucceeded(status) {
		if strings.TrimSpace(desiredState) == "absent" {
			for _, resource := range resources {
				if resource.Exists {
					status = "failed"
					reason = "one or more manifest resources still exist"
					break
				}
			}
		} else {
			for _, resource := range resources {
				if !resource.Exists {
					status = "failed"
					reason = "one or more manifest resources are missing"
					break
				}
				if !resource.Owned {
					status = "failed"
					reason = "one or more manifest resources are not owned by the field manager"
					break
				}
			}
		}
	}
	return kubernetesManifestVerifyReceipt{
		APIVersion:     "torque.dev/k8s-manifest-node/v1",
		Kind:           "KubernetesManifestVerifyReceipt",
		NodeID:         node.ID,
		TargetID:       strings.TrimSpace(targetID),
		Phase:          phase,
		Status:         status,
		Reason:         reason,
		Namespace:      strings.TrimSpace(spec.Namespace),
		FieldManager:   strings.TrimSpace(spec.FieldManager),
		DesiredState:   strings.TrimSpace(desiredState),
		ManifestDigest: manifestDigest,
		Changed:        result.Changed,
		Resources:      resources,
		VerifiedAt:     time.Now().UTC().Format(time.RFC3339Nano),
	}
}

func (e *customNodeExecutor) recordKubernetesManifestReceipts(node *runNode, phase string, status string, reason string, observe kubernetesManifestObserveReceipt, plan kubernetesManifestPlanReceipt, diff kubernetesManifestDiffReceipt, apply *kubernetesManifestOperationResult, verify kubernetesManifestVerifyReceipt) {
	payload := map[string]any{
		"apiVersion": "torque.dev/k8s-manifest-node/v1",
		"kind":       "KubernetesManifestNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      phase,
		"status":     strings.TrimSpace(status),
		"targetId":   strings.TrimSpace(plan.TargetID),
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
	e.run.RecordJSONArtifact(node.ID, "k8s-manifest-observe.json", observe)
	e.run.RecordJSONArtifact(node.ID, "k8s-manifest-plan.json", plan)
	e.run.RecordJSONArtifact(node.ID, "k8s-manifest-diff.json", diff)
	if apply != nil {
		e.run.RecordJSONArtifact(node.ID, "k8s-manifest-apply.json", *apply)
	}
	e.run.RecordJSONArtifact(node.ID, "k8s-manifest-verify.json", verify)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func kubernetesManifestStateAnyExists(state kubernetesManifestState) bool {
	for _, resource := range state.Resources {
		if resource.Exists {
			return true
		}
	}
	return false
}

const kubernetesManifestPythonScript = `
import base64
import hashlib
import json
import os
import subprocess
import tempfile
import time

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

def digest_bytes(data):
    if isinstance(data, str):
        data = data.encode("utf-8")
    return "sha256:" + hashlib.sha256(data).hexdigest()

def command_receipt(action, args, proc):
    command = json.dumps(args, separators=(",", ":"))
    stdout = proc.stdout or ""
    stderr = proc.stderr or ""
    return {
        "action": action,
        "status": "succeeded" if proc.returncode == 0 else "failed",
        "exitCode": int(proc.returncode),
        "commandDigest": digest_bytes(command),
        "stdoutDigest": digest_bytes(stdout.encode("utf-8")) if stdout else "",
        "stderrDigest": digest_bytes(stderr.encode("utf-8")) if stderr else "",
        "stdoutBytes": len(stdout.encode("utf-8")),
        "stderrBytes": len(stderr.encode("utf-8")),
    }

def run_command(action, args, ok_codes=(0,)):
    proc = subprocess.run(args, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
    receipt = command_receipt(action, args, proc)
    if proc.returncode in ok_codes:
        receipt["status"] = "succeeded"
    return proc, receipt

def ref_id(ref):
    namespace = str(ref.get("namespace") or "").strip()
    kind = str(ref.get("kind") or "").strip()
    name = str(ref.get("name") or "").strip()
    if namespace:
        return namespace + "/" + kind + "/" + name
    return "cluster/" + kind + "/" + name

def observe_resource(kubectl, ref, field_manager):
    kind = str(ref.get("kind") or "").strip()
    name = str(ref.get("name") or "").strip()
    namespace = str(ref.get("namespace") or "").strip()
    args = list(kubectl)
    if namespace:
        args.extend(["-n", namespace])
    args.extend(["get", kind, name, "-o", "json", "--show-managed-fields"])
    proc, receipt = run_command("get", args)
    observed = {
        "id": ref_id(ref),
        "scope": "namespace" if namespace else "cluster",
        "apiVersion": str(ref.get("apiVersion") or "").strip(),
        "kind": kind,
        "namespace": namespace,
        "name": name,
        "exists": False,
        "owned": False,
    }
    if proc.returncode != 0:
        observed["error"] = "not found or inaccessible"
        return observed, receipt
    try:
        obj = json.loads(proc.stdout or "{}")
    except Exception:
        observed["error"] = "invalid json from kubectl get"
        receipt["status"] = "failed"
        return observed, receipt
    metadata = obj.get("metadata") or {}
    managers = []
    for field in metadata.get("managedFields") or []:
        manager = str((field or {}).get("manager") or "").strip()
        if manager:
            managers.append(manager)
    observed.update({
        "exists": True,
        "owned": field_manager in managers,
        "manager": field_manager if field_manager in managers else (managers[0] if managers else ""),
        "uidDigest": digest_bytes(str(metadata.get("uid") or "")) if metadata.get("uid") else "",
        "resourceVersionDigest": digest_bytes(str(metadata.get("resourceVersion") or "")) if metadata.get("resourceVersion") else "",
        "objectDigest": digest_bytes(json.dumps(obj, sort_keys=True, separators=(",", ":"))),
    })
    generation = metadata.get("generation")
    if isinstance(generation, int):
        observed["generation"] = generation
    return observed, receipt

def observe_all(kubectl, resources, field_manager):
    observed = []
    commands = []
    for ref in resources:
        item, receipt = observe_resource(kubectl, ref, field_manager)
        observed.append(item)
        commands.append(receipt)
    observed.sort(key=lambda item: item.get("id") or "")
    return {"resources": observed}, commands

def namespace_args(namespace):
    namespace = str(namespace or "").strip()
    if not namespace:
        return []
    return ["-n", namespace]

def finish(doc, code=0):
    doc.setdefault("apiVersion", "torque.dev/k8s-manifest-node/v1")
    doc.setdefault("kind", "KubernetesManifestOperationReceipt")
    doc.setdefault("completedAt", now())
    print(json.dumps(doc, sort_keys=True))
    raise SystemExit(code)

try:
    payload = json.loads(base64.b64decode(os.environ["TORQUE_K8S_MANIFEST_PAYLOAD_B64"]).decode("utf-8"))
    operation = str(payload.get("operation") or "").strip()
    desired_state = str(payload.get("desiredState") or "").strip()
    namespace = str(payload.get("namespace") or "").strip()
    field_manager = str(payload.get("fieldManager") or "torque").strip() or "torque"
    force_conflicts = bool(payload.get("forceConflicts"))
    remove_on_delete = bool(payload.get("removeOnDelete"))
    kubectl = [str(x) for x in (payload.get("kubectlArgs") or ["kubectl"]) if str(x).strip()]
    resources = list(payload.get("resources") or [])
    manifest = base64.b64decode(str(payload.get("manifestB64") or ""))
    manifest_digest = str(payload.get("manifestDigest") or "").strip() or digest_bytes(manifest)
    if not kubectl:
        finish({"operation": operation, "status": "failed", "error": "kubectl command is required"}, 1)
    before, before_commands = observe_all(kubectl, resources, field_manager)
    if operation == "observe":
        finish({
            "operation": operation,
            "status": "succeeded",
            "namespace": namespace,
            "fieldManager": field_manager,
            "desiredState": desired_state,
            "manifestDigest": manifest_digest,
            "changed": False,
            "changes": {"objects": False},
            "before": before,
            "after": before,
            "commands": before_commands,
        })
    fd, path = tempfile.mkstemp(prefix="torque-k8s-manifest-", suffix=".yaml")
    try:
        with os.fdopen(fd, "wb") as fh:
            fh.write(manifest)
        if operation == "diff":
            args = list(kubectl) + namespace_args(namespace) + ["diff", "--server-side", "--field-manager", field_manager, "-f", path]
            proc, receipt = run_command("diff", args, ok_codes=(0, 1))
            changed = proc.returncode == 1
            status = "succeeded" if proc.returncode in (0, 1) else "failed"
            finish({
                "operation": operation,
                "status": status,
                "namespace": namespace,
                "fieldManager": field_manager,
                "desiredState": desired_state,
                "manifestDigest": manifest_digest,
                "changed": changed,
                "changes": {"objects": changed},
                "before": before,
                "after": before,
                "commands": [receipt],
                "error": "" if status == "succeeded" else "kubectl diff failed",
            }, 0 if status == "succeeded" else 1)
        if operation == "delete":
            if not remove_on_delete:
                finish({
                    "operation": operation,
                    "status": "skipped",
                    "reason": "removeOnDelete is false",
                    "namespace": namespace,
                    "fieldManager": field_manager,
                    "desiredState": desired_state,
                    "manifestDigest": manifest_digest,
                    "changed": False,
                    "changes": {"objects": False},
                    "before": before,
                    "after": before,
                    "commands": before_commands,
                })
            args = list(kubectl) + namespace_args(namespace) + ["delete", "-f", path, "--ignore-not-found=true"]
            proc, receipt = run_command("delete", args)
            after, after_commands = observe_all(kubectl, resources, field_manager)
            changed = any(item.get("exists") for item in before.get("resources") or [])
            status = "succeeded" if proc.returncode == 0 and not any(item.get("exists") for item in after.get("resources") or []) else "failed"
            finish({
                "operation": operation,
                "status": status,
                "namespace": namespace,
                "fieldManager": field_manager,
                "desiredState": desired_state,
                "manifestDigest": manifest_digest,
                "changed": changed,
                "changes": {"objects": changed},
                "before": before,
                "after": after,
                "commands": [receipt] + after_commands,
                "error": "" if status == "succeeded" else "kubectl delete failed or resources still exist",
            }, 0 if status == "succeeded" else 1)
        if operation != "apply":
            finish({"operation": operation, "status": "failed", "error": "unsupported operation"}, 1)
        diff_args = list(kubectl) + namespace_args(namespace) + ["diff", "--server-side", "--field-manager", field_manager, "-f", path]
        diff_proc, diff_receipt = run_command("diff", diff_args, ok_codes=(0, 1))
        if diff_proc.returncode not in (0, 1):
            finish({
                "operation": operation,
                "status": "failed",
                "namespace": namespace,
                "fieldManager": field_manager,
                "desiredState": desired_state,
                "manifestDigest": manifest_digest,
                "changed": False,
                "changes": {"objects": False},
                "before": before,
                "after": before,
                "commands": [diff_receipt],
                "error": "kubectl diff failed",
            }, 1)
        changed = diff_proc.returncode == 1
        apply_args = list(kubectl) + namespace_args(namespace) + ["apply", "--server-side", "--field-manager", field_manager]
        if force_conflicts:
            apply_args.append("--force-conflicts")
        apply_args.extend(["-f", path])
        apply_proc, apply_receipt = run_command("apply", apply_args)
        after, after_commands = observe_all(kubectl, resources, field_manager)
        owned = all(item.get("exists") and item.get("owned") for item in after.get("resources") or [])
        status = "succeeded" if apply_proc.returncode == 0 and owned else "failed"
        finish({
            "operation": operation,
            "status": status,
            "namespace": namespace,
            "fieldManager": field_manager,
            "desiredState": desired_state,
            "manifestDigest": manifest_digest,
            "changed": changed,
            "changes": {"objects": changed},
            "before": before,
            "after": after,
            "commands": [diff_receipt, apply_receipt] + after_commands,
            "error": "" if status == "succeeded" else "kubectl apply failed or field-manager ownership was not verified",
        }, 0 if status == "succeeded" else 1)
    finally:
        try:
            os.unlink(path)
        except Exception:
            pass
except Exception as exc:
    finish({"operation": locals().get("operation", ""), "status": "failed", "error": str(exc)}, 1)
`
