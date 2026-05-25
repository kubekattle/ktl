package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

const defaultKubernetesClusterVerifyInterval = 5 * time.Second

type kubernetesClusterVerifyEvidence struct {
	APIVersion       string                                     `json:"apiVersion"`
	Kind             string                                     `json:"kind"`
	NodeID           string                                     `json:"nodeId"`
	NodeKind         string                                     `json:"nodeKind"`
	Status           string                                     `json:"status"`
	Message          string                                     `json:"message"`
	StableIterations int                                        `json:"stableIterations"`
	StableInterval   string                                     `json:"stableInterval"`
	APIReceipts      []transport.OperationResult                `json:"apiReceipts,omitempty"`
	NodesReceipt     transport.OperationResult                  `json:"nodesReceipt,omitempty"`
	TotalNodes       int                                        `json:"totalNodes"`
	ReadyNodes       int                                        `json:"readyNodes"`
	NotReadyNodes    []string                                   `json:"notReadyNodes,omitempty"`
	Namespaces       []kubernetesClusterVerifyNamespaceEvidence `json:"namespaces,omitempty"`
	AppProbes        []kubernetesClusterVerifyAppProbeEvidence  `json:"appProbes,omitempty"`
}

type kubernetesClusterVerifyNamespaceEvidence struct {
	Namespace string                    `json:"namespace"`
	Receipt   transport.OperationResult `json:"receipt"`
	TotalPods int                       `json:"totalPods"`
	ReadyPods int                       `json:"readyPods"`
	BadPods   []string                  `json:"badPods,omitempty"`
}

type kubernetesClusterVerifyAppProbeEvidence struct {
	ID      string                    `json:"id"`
	Expect  string                    `json:"expect,omitempty"`
	Receipt transport.OperationResult `json:"receipt"`
	Matched bool                      `json:"matched,omitempty"`
}

func (e *customNodeExecutor) runKubernetesClusterVerifyNode(ctx context.Context, node *runNode, command string) error {
	phase := "k8s-cluster-verify"
	if strings.EqualFold(command, "delete") {
		payload := e.kubernetesClusterVerifyPayload(node, "skipped", "delete does not verify Kubernetes clusters", nil)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		return nil
	}
	cursor := map[string]any{"kind": normalizeNodeKind(node.Kind), "phase": phase}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		payload := e.kubernetesClusterVerifyPayload(node, "skipped", reason, nil)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	evidence, err := e.verifyKubernetesCluster(ctx, node)
	if err != nil {
		evidence.Status = "failed"
		evidence.Message = err.Error()
		payload := e.kubernetesClusterVerifyPayload(node, "failed", err.Error(), &evidence)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		e.recordKubernetesLifecycleSummary(ctx, node)
		runErr := &RunError{Class: "K8S_CLUSTER_VERIFY_FAILED", Message: err.Error(), Digest: computeRunErrorDigest("K8S_CLUSTER_VERIFY_FAILED", err.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, err.Error(), map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	evidence.Status = "succeeded"
	evidence.Message = "cluster verified"
	payload := e.kubernetesClusterVerifyPayload(node, "succeeded", "cluster verified", &evidence)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
	e.recordKubernetesLifecycleSummary(ctx, node)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "cluster verified", map[string]any{
		"phase":      phase,
		"status":     "succeeded",
		"cursor":     cursor,
		"readyNodes": evidence.ReadyNodes,
		"totalNodes": evidence.TotalNodes,
	}, nil)
	return nil
}

func (e *customNodeExecutor) verifyKubernetesCluster(ctx context.Context, node *runNode) (kubernetesClusterVerifyEvidence, error) {
	spec := node.Kubernetes.Cluster
	runner, err := kubernetesClusterVerifyRunner(spec)
	if err != nil {
		return kubernetesClusterVerifyEvidence{}, err
	}
	iterations := spec.StableIterations
	if iterations <= 0 {
		iterations = 1
	}
	interval := defaultKubernetesClusterVerifyInterval
	if spec.StableInterval != nil && *spec.StableInterval > 0 {
		interval = *spec.StableInterval
	}
	evidence := kubernetesClusterVerifyEvidence{
		APIVersion:       "torque.dev/stack-lifecycle/v1",
		Kind:             "KubernetesClusterVerify",
		NodeID:           node.ID,
		NodeKind:         normalizeNodeKind(node.Kind),
		StableIterations: iterations,
		StableInterval:   interval.String(),
	}

	for i := 0; i < iterations; i++ {
		receipt := runner.Run(ctx, kubernetesClusterAPICommand(spec))
		evidence.APIReceipts = append(evidence.APIReceipts, receipt)
		if !nodeStepSucceeded(receipt.Status) {
			return evidence, fmt.Errorf("kubernetes API verify failed: %s", firstReceiptMessage(receipt))
		}
		if i+1 < iterations {
			select {
			case <-ctx.Done():
				return evidence, ctx.Err()
			case <-time.After(interval):
			}
		}
	}

	nodesReceipt := runner.Run(ctx, kubernetesClusterNodesCommand(spec))
	evidence.NodesReceipt = nodesReceipt
	if !nodeStepSucceeded(nodesReceipt.Status) {
		return evidence, fmt.Errorf("kubernetes nodes verify failed: %s", firstReceiptMessage(nodesReceipt))
	}
	total, ready, notReady, err := parseKubernetesNodeReadiness(nodesReceipt.Stdout)
	if err != nil {
		return evidence, fmt.Errorf("parse Kubernetes nodes: %w", err)
	}
	evidence.TotalNodes = total
	evidence.ReadyNodes = ready
	evidence.NotReadyNodes = notReady
	if total == 0 {
		return evidence, fmt.Errorf("kubernetes nodes verify failed: no nodes returned")
	}
	if spec.MinReadyNodes > 0 && ready < spec.MinReadyNodes {
		return evidence, fmt.Errorf("kubernetes nodes verify failed: ready nodes %d < required %d", ready, spec.MinReadyNodes)
	}
	if len(notReady) > 0 {
		return evidence, fmt.Errorf("kubernetes nodes verify failed: not ready nodes: %s", strings.Join(notReady, ", "))
	}

	for _, namespace := range kubernetesClusterVerifyNamespaces(spec.Namespaces) {
		receipt := runner.Run(ctx, kubernetesClusterPodsCommand(spec, namespace))
		nsEvidence := kubernetesClusterVerifyNamespaceEvidence{Namespace: namespace, Receipt: receipt}
		if !nodeStepSucceeded(receipt.Status) {
			evidence.Namespaces = append(evidence.Namespaces, nsEvidence)
			return evidence, fmt.Errorf("kubernetes pods verify failed in namespace %s: %s", namespace, firstReceiptMessage(receipt))
		}
		totalPods, readyPods, badPods, err := parseKubernetesPodReadiness(receipt.Stdout)
		if err != nil {
			evidence.Namespaces = append(evidence.Namespaces, nsEvidence)
			return evidence, fmt.Errorf("parse Kubernetes pods in namespace %s: %w", namespace, err)
		}
		nsEvidence.TotalPods = totalPods
		nsEvidence.ReadyPods = readyPods
		nsEvidence.BadPods = badPods
		evidence.Namespaces = append(evidence.Namespaces, nsEvidence)
		if len(badPods) > 0 {
			return evidence, fmt.Errorf("kubernetes pods verify failed in namespace %s: unhealthy pods: %s", namespace, strings.Join(badPods, ", "))
		}
	}

	for _, probe := range spec.AppProbes {
		receipt := runner.Run(ctx, probe.Command)
		probeEvidence := kubernetesClusterVerifyAppProbeEvidence{ID: strings.TrimSpace(probe.ID), Expect: strings.TrimSpace(probe.Expect), Receipt: receipt}
		if !nodeStepSucceeded(receipt.Status) {
			evidence.AppProbes = append(evidence.AppProbes, probeEvidence)
			return evidence, fmt.Errorf("kubernetes app probe %s failed: %s", probe.ID, firstReceiptMessage(receipt))
		}
		if strings.TrimSpace(probe.Expect) != "" {
			combined := receipt.Stdout + "\n" + receipt.Stderr
			probeEvidence.Matched = strings.Contains(combined, strings.TrimSpace(probe.Expect))
			if !probeEvidence.Matched {
				evidence.AppProbes = append(evidence.AppProbes, probeEvidence)
				return evidence, fmt.Errorf("kubernetes app probe %s did not match expected output", probe.ID)
			}
		}
		evidence.AppProbes = append(evidence.AppProbes, probeEvidence)
	}
	return evidence, nil
}

func (e *customNodeExecutor) kubernetesClusterVerifyPayload(node *runNode, status string, message string, evidence *kubernetesClusterVerifyEvidence) map[string]any {
	payload := map[string]any{
		"apiVersion": "torque.dev/stack-lifecycle/v1",
		"kind":       "KubernetesClusterVerify",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"status":     status,
		"message":    strings.TrimSpace(message),
	}
	if evidence != nil {
		payload["evidence"] = evidence
	}
	return payload
}

func kubernetesClusterVerifyRunner(spec KubernetesClusterSpec) (hostCommandRunner, error) {
	transportKind := strings.TrimSpace(spec.Transport)
	if transportKind == "" {
		transportKind = "local"
	}
	timeout := 5 * time.Minute
	if spec.Timeout != nil && *spec.Timeout > 0 {
		timeout = *spec.Timeout
	}
	return hostCommandTransport(HostCommandSpec{
		Transport: transportKind,
		Target:    spec.Target,
		TargetEnv: spec.TargetEnv,
		Timeout:   &timeout,
	})
}

func kubernetesClusterKubectlBase(spec KubernetesClusterSpec) string {
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
	return shellJoin(args)
}

func kubernetesClusterKubectlCommandParts(spec KubernetesClusterSpec) []string {
	command := strings.TrimSpace(spec.KubectlCommand)
	if command == "" {
		return []string{"kubectl"}
	}
	return strings.Fields(command)
}

func kubernetesClusterAPICommand(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.APICommand) != "" {
		return spec.APICommand
	}
	return kubernetesClusterKubectlBase(spec) + " version --request-timeout=10s"
}

func kubernetesClusterNodesCommand(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.NodesCommand) != "" {
		return spec.NodesCommand
	}
	return kubernetesClusterKubectlBase(spec) + " get nodes -o json"
}

func kubernetesClusterPodsCommand(spec KubernetesClusterSpec, namespace string) string {
	if strings.TrimSpace(spec.PodsCommand) != "" {
		return strings.ReplaceAll(spec.PodsCommand, "{{namespace}}", namespace)
	}
	return kubernetesClusterKubectlBase(spec) + " -n " + transport.ShellQuote(namespace) + " get pods -o json"
}

func kubernetesClusterVerifyNamespaces(namespaces []string) []string {
	out := make([]string, 0, len(namespaces))
	for _, namespace := range namespaces {
		if strings.TrimSpace(namespace) != "" {
			out = append(out, strings.TrimSpace(namespace))
		}
	}
	if len(out) == 0 {
		out = append(out, "kube-system")
	}
	return out
}

func parseKubernetesNodeReadiness(raw string) (int, int, []string, error) {
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Conditions []struct {
					Type   string `json:"type"`
					Status string `json:"status"`
					Reason string `json:"reason,omitempty"`
				} `json:"conditions"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return 0, 0, nil, err
	}
	total := len(decoded.Items)
	ready := 0
	var notReady []string
	for _, item := range decoded.Items {
		isReady := false
		reason := ""
		for _, condition := range item.Status.Conditions {
			if condition.Type == "Ready" {
				isReady = condition.Status == "True"
				reason = condition.Reason
				break
			}
		}
		if isReady {
			ready++
			continue
		}
		name := strings.TrimSpace(item.Metadata.Name)
		if name == "" {
			name = "<unknown>"
		}
		if strings.TrimSpace(reason) != "" {
			name += "(" + strings.TrimSpace(reason) + ")"
		}
		notReady = append(notReady, name)
	}
	return total, ready, notReady, nil
}

func parseKubernetesPodReadiness(raw string) (int, int, []string, error) {
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Name  string `json:"name"`
					Ready bool   `json:"ready"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return 0, 0, nil, err
	}
	total := len(decoded.Items)
	ready := 0
	var bad []string
	for _, item := range decoded.Items {
		name := strings.TrimSpace(item.Metadata.Name)
		if name == "" {
			name = "<unknown>"
		}
		phase := strings.TrimSpace(item.Status.Phase)
		if phase == "Succeeded" {
			ready++
			continue
		}
		if phase != "Running" {
			bad = append(bad, name+"("+phase+")")
			continue
		}
		allContainersReady := true
		for _, container := range item.Status.ContainerStatuses {
			if !container.Ready {
				allContainersReady = false
				containerName := strings.TrimSpace(container.Name)
				if containerName == "" {
					containerName = "container"
				}
				bad = append(bad, name+"("+containerName+" not ready)")
			}
		}
		if allContainersReady {
			ready++
		}
	}
	return total, ready, bad, nil
}
