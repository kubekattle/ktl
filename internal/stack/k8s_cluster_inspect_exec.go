package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

type kubernetesClusterInspectEvidence struct {
	APIVersion         string                                     `json:"apiVersion"`
	Kind               string                                     `json:"kind"`
	NodeID             string                                     `json:"nodeId"`
	NodeKind           string                                     `json:"nodeKind"`
	Status             string                                     `json:"status"`
	Message            string                                     `json:"message"`
	TargetDigest       string                                     `json:"targetDigest,omitempty"`
	Receipts           []kubernetesClusterInspectReceipt          `json:"receipts,omitempty"`
	API                kubernetesClusterInspectAPI                `json:"api"`
	Provider           kubernetesClusterInspectProvider           `json:"provider"`
	CertificateRenewal kubernetesClusterInspectCertificateRenewal `json:"certificateRenewal"`
	Nodes              []kubernetesClusterInspectNode             `json:"nodes,omitempty"`
	Namespaces         []kubernetesClusterInspectNamespace        `json:"namespaces,omitempty"`
	CorePods           []kubernetesClusterInspectPod              `json:"corePods,omitempty"`
}

type kubernetesClusterInspectReceipt struct {
	Operation      string   `json:"operation"`
	Status         string   `json:"status"`
	TargetDigest   string   `json:"targetDigest,omitempty"`
	Command        []string `json:"command,omitempty"`
	StdoutDigest   string   `json:"stdoutDigest,omitempty"`
	StdoutBytes    int      `json:"stdoutBytes,omitempty"`
	StderrDigest   string   `json:"stderrDigest,omitempty"`
	StderrBytes    int      `json:"stderrBytes,omitempty"`
	ExitCode       int      `json:"exitCode"`
	TimedOut       bool     `json:"timedOut,omitempty"`
	DurationMillis int64    `json:"durationMillis,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type kubernetesClusterInspectAPI struct {
	Server        string                          `json:"server,omitempty"`
	ServerVersion kubernetesClusterInspectVersion `json:"serverVersion,omitempty"`
	ClientVersion kubernetesClusterInspectVersion `json:"clientVersion,omitempty"`
}

type kubernetesClusterInspectVersion struct {
	GitVersion string `json:"gitVersion,omitempty"`
	Major      string `json:"major,omitempty"`
	Minor      string `json:"minor,omitempty"`
	Platform   string `json:"platform,omitempty"`
}

type kubernetesClusterInspectProvider struct {
	Distribution string   `json:"distribution"`
	Confidence   string   `json:"confidence"`
	Managed      bool     `json:"managed,omitempty"`
	Reasons      []string `json:"reasons,omitempty"`
}

type kubernetesClusterInspectCertificateRenewal struct {
	Provider                string `json:"provider"`
	Supported               bool   `json:"supported"`
	ManagedExternally       bool   `json:"managedExternally,omitempty"`
	RequiresExplicitTargets bool   `json:"requiresExplicitTargets,omitempty"`
	RequiresCustomCommands  bool   `json:"requiresCustomCommands,omitempty"`
	Reason                  string `json:"reason,omitempty"`
}

type kubernetesClusterInspectNode struct {
	Name             string                            `json:"name"`
	Roles            []string                          `json:"roles,omitempty"`
	Ready            bool                              `json:"ready"`
	ReadyReason      string                            `json:"readyReason,omitempty"`
	KubeletVersion   string                            `json:"kubeletVersion,omitempty"`
	OSImage          string                            `json:"osImage,omitempty"`
	KernelVersion    string                            `json:"kernelVersion,omitempty"`
	ContainerRuntime string                            `json:"containerRuntime,omitempty"`
	ProviderID       string                            `json:"providerId,omitempty"`
	Unschedulable    bool                              `json:"unschedulable,omitempty"`
	Addresses        []kubernetesClusterInspectAddress `json:"addresses,omitempty"`
	RoleLabels       map[string]string                 `json:"roleLabels,omitempty"`
}

type kubernetesClusterInspectAddress struct {
	Type    string `json:"type"`
	Address string `json:"address"`
}

type kubernetesClusterInspectNamespace struct {
	Name   string `json:"name"`
	Phase  string `json:"phase,omitempty"`
	System bool   `json:"system,omitempty"`
}

type kubernetesClusterInspectPod struct {
	Namespace       string `json:"namespace"`
	Name            string `json:"name"`
	Phase           string `json:"phase,omitempty"`
	ReadyContainers int    `json:"readyContainers"`
	TotalContainers int    `json:"totalContainers"`
}

func (e *customNodeExecutor) runKubernetesClusterInspectNode(ctx context.Context, node *runNode, command string) error {
	phase := "k8s-cluster-inspect"
	if strings.EqualFold(command, "delete") {
		payload := e.kubernetesClusterInspectPayload(node, "skipped", "delete does not inspect Kubernetes clusters", nil)
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
		payload := e.kubernetesClusterInspectPayload(node, "skipped", reason, nil)
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

	evidence, err := e.inspectKubernetesCluster(ctx, node)
	if err != nil {
		evidence.Status = "failed"
		evidence.Message = err.Error()
		payload := e.kubernetesClusterInspectPayload(node, "failed", err.Error(), &evidence)
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		runErr := &RunError{Class: "K8S_CLUSTER_INSPECT_FAILED", Message: err.Error(), Digest: computeRunErrorDigest("K8S_CLUSTER_INSPECT_FAILED", err.Error())}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, err.Error(), map[string]any{
			"phase":  phase,
			"status": "failure",
			"cursor": cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	evidence.Status = "succeeded"
	evidence.Message = "cluster inspected"
	payload := e.kubernetesClusterInspectPayload(node, "succeeded", "cluster inspected", &evidence)
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "cluster inspected", map[string]any{
		"phase":          phase,
		"status":         "succeeded",
		"cursor":         cursor,
		"distribution":   evidence.Provider.Distribution,
		"nodeCount":      len(evidence.Nodes),
		"namespaceCount": len(evidence.Namespaces),
	}, nil)
	return nil
}

func (e *customNodeExecutor) inspectKubernetesCluster(ctx context.Context, node *runNode) (kubernetesClusterInspectEvidence, error) {
	spec := node.Kubernetes.Cluster
	runner, err := kubernetesClusterVerifyRunner(spec)
	if err != nil {
		return kubernetesClusterInspectEvidence{}, err
	}
	evidence := kubernetesClusterInspectEvidence{
		APIVersion:   "torque.dev/stack-lifecycle/v1",
		Kind:         "KubernetesClusterInspect",
		NodeID:       node.ID,
		NodeKind:     normalizeNodeKind(node.Kind),
		TargetDigest: runner.TargetDigest(),
	}

	configReceipt := runner.Run(ctx, kubernetesClusterConfigCommand(spec))
	evidence.Receipts = append(evidence.Receipts, compactKubernetesClusterInspectReceipt(configReceipt))
	if !nodeStepSucceeded(configReceipt.Status) {
		return evidence, fmt.Errorf("Kubernetes config inspect failed: %s", firstReceiptMessage(configReceipt))
	}
	evidence.API.Server = parseKubernetesClusterServer(configReceipt.Stdout)

	versionReceipt := runner.Run(ctx, kubernetesClusterInspectAPICommand(spec))
	evidence.Receipts = append(evidence.Receipts, compactKubernetesClusterInspectReceipt(versionReceipt))
	if !nodeStepSucceeded(versionReceipt.Status) {
		return evidence, fmt.Errorf("Kubernetes API inspect failed: %s", firstReceiptMessage(versionReceipt))
	}
	api, err := parseKubernetesClusterVersion(versionReceipt.Stdout)
	if err != nil {
		return evidence, fmt.Errorf("parse Kubernetes API version: %w", err)
	}
	evidence.API.ClientVersion = api.ClientVersion
	evidence.API.ServerVersion = api.ServerVersion

	nodesReceipt := runner.Run(ctx, kubernetesClusterNodesCommand(spec))
	evidence.Receipts = append(evidence.Receipts, compactKubernetesClusterInspectReceipt(nodesReceipt))
	if !nodeStepSucceeded(nodesReceipt.Status) {
		return evidence, fmt.Errorf("Kubernetes node inspect failed: %s", firstReceiptMessage(nodesReceipt))
	}
	nodes, err := parseKubernetesClusterInspectNodes(nodesReceipt.Stdout)
	if err != nil {
		return evidence, fmt.Errorf("parse Kubernetes nodes: %w", err)
	}
	evidence.Nodes = nodes

	namespacesReceipt := runner.Run(ctx, kubernetesClusterNamespacesCommand(spec))
	evidence.Receipts = append(evidence.Receipts, compactKubernetesClusterInspectReceipt(namespacesReceipt))
	if !nodeStepSucceeded(namespacesReceipt.Status) {
		return evidence, fmt.Errorf("Kubernetes namespace inspect failed: %s", firstReceiptMessage(namespacesReceipt))
	}
	namespaces, err := parseKubernetesClusterInspectNamespaces(namespacesReceipt.Stdout)
	if err != nil {
		return evidence, fmt.Errorf("parse Kubernetes namespaces: %w", err)
	}
	evidence.Namespaces = namespaces

	for _, namespace := range kubernetesClusterInspectPodNamespaces(spec.Namespaces) {
		receipt := runner.Run(ctx, kubernetesClusterPodsCommand(spec, namespace))
		evidence.Receipts = append(evidence.Receipts, compactKubernetesClusterInspectReceipt(receipt))
		if !nodeStepSucceeded(receipt.Status) {
			return evidence, fmt.Errorf("Kubernetes core pod inspect failed in namespace %s: %s", namespace, firstReceiptMessage(receipt))
		}
		pods, err := parseKubernetesClusterInspectPods(receipt.Stdout)
		if err != nil {
			return evidence, fmt.Errorf("parse Kubernetes pods in namespace %s: %w", namespace, err)
		}
		evidence.CorePods = append(evidence.CorePods, pods...)
	}
	sort.Slice(evidence.CorePods, func(i, j int) bool {
		if evidence.CorePods[i].Namespace == evidence.CorePods[j].Namespace {
			return evidence.CorePods[i].Name < evidence.CorePods[j].Name
		}
		return evidence.CorePods[i].Namespace < evidence.CorePods[j].Namespace
	})

	evidence.Provider = inferKubernetesClusterProvider(evidence.API, evidence.Nodes, evidence.CorePods)
	evidence.CertificateRenewal = kubernetesClusterCertificateRenewalHint(evidence.Provider)
	return evidence, nil
}

func (e *customNodeExecutor) kubernetesClusterInspectPayload(node *runNode, status string, message string, evidence *kubernetesClusterInspectEvidence) map[string]any {
	payload := map[string]any{
		"apiVersion": "torque.dev/stack-lifecycle/v1",
		"kind":       "KubernetesClusterInspect",
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

func compactKubernetesClusterInspectReceipt(receipt transport.OperationResult) kubernetesClusterInspectReceipt {
	out := kubernetesClusterInspectReceipt{
		Operation:      receipt.Operation,
		Status:         receipt.Status,
		TargetDigest:   receipt.TargetDigest,
		Command:        append([]string(nil), receipt.Command...),
		ExitCode:       receipt.ExitCode,
		TimedOut:       receipt.TimedOut,
		DurationMillis: receipt.DurationMillis,
		Error:          strings.TrimSpace(receipt.Error),
	}
	if strings.TrimSpace(receipt.Stdout) != "" {
		out.StdoutDigest = digestString(receipt.Stdout)
		out.StdoutBytes = len(receipt.Stdout)
	}
	if strings.TrimSpace(receipt.Stderr) != "" {
		out.StderrDigest = digestString(receipt.Stderr)
		out.StderrBytes = len(receipt.Stderr)
	}
	return out
}

func kubernetesClusterConfigCommand(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.ConfigCommand) != "" {
		return spec.ConfigCommand
	}
	return kubernetesClusterKubectlBase(spec) + " config view --minify -o json"
}

func kubernetesClusterInspectAPICommand(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.APICommand) != "" {
		return spec.APICommand
	}
	return kubernetesClusterKubectlBase(spec) + " version --request-timeout=10s -o json"
}

func kubernetesClusterNamespacesCommand(spec KubernetesClusterSpec) string {
	if strings.TrimSpace(spec.NamespacesCommand) != "" {
		return spec.NamespacesCommand
	}
	return kubernetesClusterKubectlBase(spec) + " get namespaces -o json"
}

func kubernetesClusterInspectPodNamespaces(namespaces []string) []string {
	seen := map[string]struct{}{}
	out := []string{"kube-system"}
	seen["kube-system"] = struct{}{}
	for _, namespace := range namespaces {
		namespace = strings.TrimSpace(namespace)
		if namespace == "" {
			continue
		}
		if _, ok := seen[namespace]; ok {
			continue
		}
		seen[namespace] = struct{}{}
		out = append(out, namespace)
	}
	return out
}

func parseKubernetesClusterServer(raw string) string {
	var decoded struct {
		Clusters []struct {
			Cluster struct {
				Server string `json:"server"`
			} `json:"cluster"`
		} `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return ""
	}
	for _, cluster := range decoded.Clusters {
		if strings.TrimSpace(cluster.Cluster.Server) != "" {
			return strings.TrimSpace(cluster.Cluster.Server)
		}
	}
	return ""
}

func parseKubernetesClusterVersion(raw string) (kubernetesClusterInspectAPI, error) {
	var decoded struct {
		ClientVersion kubernetesClusterInspectVersion `json:"clientVersion"`
		ServerVersion kubernetesClusterInspectVersion `json:"serverVersion"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return kubernetesClusterInspectAPI{}, err
	}
	return kubernetesClusterInspectAPI{
		ClientVersion: decoded.ClientVersion,
		ServerVersion: decoded.ServerVersion,
	}, nil
}

func parseKubernetesClusterInspectNodes(raw string) ([]kubernetesClusterInspectNode, error) {
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name   string            `json:"name"`
				Labels map[string]string `json:"labels"`
			} `json:"metadata"`
			Spec struct {
				ProviderID    string `json:"providerID"`
				Unschedulable bool   `json:"unschedulable"`
			} `json:"spec"`
			Status struct {
				Addresses  []kubernetesClusterInspectAddress `json:"addresses"`
				Conditions []struct {
					Type    string `json:"type"`
					Status  string `json:"status"`
					Reason  string `json:"reason,omitempty"`
					Message string `json:"message,omitempty"`
				} `json:"conditions"`
				NodeInfo struct {
					KubeletVersion          string `json:"kubeletVersion"`
					OSImage                 string `json:"osImage"`
					KernelVersion           string `json:"kernelVersion"`
					ContainerRuntimeVersion string `json:"containerRuntimeVersion"`
				} `json:"nodeInfo"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	nodes := make([]kubernetesClusterInspectNode, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		node := kubernetesClusterInspectNode{
			Name:             strings.TrimSpace(item.Metadata.Name),
			Roles:            kubernetesClusterNodeRoles(item.Metadata.Labels),
			Ready:            false,
			KubeletVersion:   strings.TrimSpace(item.Status.NodeInfo.KubeletVersion),
			OSImage:          strings.TrimSpace(item.Status.NodeInfo.OSImage),
			KernelVersion:    strings.TrimSpace(item.Status.NodeInfo.KernelVersion),
			ContainerRuntime: strings.TrimSpace(item.Status.NodeInfo.ContainerRuntimeVersion),
			ProviderID:       strings.TrimSpace(item.Spec.ProviderID),
			Unschedulable:    item.Spec.Unschedulable,
			Addresses:        compactKubernetesClusterAddresses(item.Status.Addresses),
			RoleLabels:       kubernetesClusterRoleLabels(item.Metadata.Labels),
		}
		for _, condition := range item.Status.Conditions {
			if condition.Type != "Ready" {
				continue
			}
			node.Ready = condition.Status == "True"
			node.ReadyReason = strings.TrimSpace(firstNonEmptyString(condition.Reason, condition.Message))
			break
		}
		if node.Name == "" {
			node.Name = "<unknown>"
		}
		nodes = append(nodes, node)
	}
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Name < nodes[j].Name })
	return nodes, nil
}

func kubernetesClusterNodeRoles(labels map[string]string) []string {
	roleSet := map[string]struct{}{}
	for key := range labels {
		if key == "node-role.kubernetes.io/control-plane" || key == "node-role.kubernetes.io/master" {
			roleSet["control-plane"] = struct{}{}
			continue
		}
		if strings.HasPrefix(key, "node-role.kubernetes.io/") {
			role := strings.TrimPrefix(key, "node-role.kubernetes.io/")
			if strings.TrimSpace(role) != "" {
				roleSet[strings.TrimSpace(role)] = struct{}{}
			}
		}
	}
	if len(roleSet) == 0 {
		roleSet["worker"] = struct{}{}
	}
	roles := make([]string, 0, len(roleSet))
	for role := range roleSet {
		roles = append(roles, role)
	}
	sort.Strings(roles)
	return roles
}

func kubernetesClusterRoleLabels(labels map[string]string) map[string]string {
	out := map[string]string{}
	for key, value := range labels {
		if strings.HasPrefix(key, "node-role.kubernetes.io/") || strings.Contains(key, "eks.amazonaws.com/") || strings.Contains(key, "cloud.google.com/") || strings.Contains(key, "kubernetes.azure.com/") || strings.Contains(key, "k3s.io/") || strings.Contains(key, "rke2.io/") {
			out[key] = value
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func compactKubernetesClusterAddresses(addresses []kubernetesClusterInspectAddress) []kubernetesClusterInspectAddress {
	out := make([]kubernetesClusterInspectAddress, 0, len(addresses))
	for _, address := range addresses {
		if strings.TrimSpace(address.Type) == "" || strings.TrimSpace(address.Address) == "" {
			continue
		}
		out = append(out, kubernetesClusterInspectAddress{
			Type:    strings.TrimSpace(address.Type),
			Address: strings.TrimSpace(address.Address),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Type == out[j].Type {
			return out[i].Address < out[j].Address
		}
		return out[i].Type < out[j].Type
	})
	return out
}

func parseKubernetesClusterInspectNamespaces(raw string) ([]kubernetesClusterInspectNamespace, error) {
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Status struct {
				Phase string `json:"phase"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	namespaces := make([]kubernetesClusterInspectNamespace, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		name := strings.TrimSpace(item.Metadata.Name)
		if name == "" {
			continue
		}
		namespaces = append(namespaces, kubernetesClusterInspectNamespace{
			Name:   name,
			Phase:  strings.TrimSpace(item.Status.Phase),
			System: name == "kube-system" || strings.HasPrefix(name, "kube-"),
		})
	}
	sort.Slice(namespaces, func(i, j int) bool { return namespaces[i].Name < namespaces[j].Name })
	return namespaces, nil
}

func parseKubernetesClusterInspectPods(raw string) ([]kubernetesClusterInspectPod, error) {
	var decoded struct {
		Items []struct {
			Metadata struct {
				Name      string `json:"name"`
				Namespace string `json:"namespace"`
			} `json:"metadata"`
			Status struct {
				Phase             string `json:"phase"`
				ContainerStatuses []struct {
					Ready bool `json:"ready"`
				} `json:"containerStatuses"`
			} `json:"status"`
		} `json:"items"`
	}
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		return nil, err
	}
	pods := make([]kubernetesClusterInspectPod, 0, len(decoded.Items))
	for _, item := range decoded.Items {
		pod := kubernetesClusterInspectPod{
			Namespace: strings.TrimSpace(item.Metadata.Namespace),
			Name:      strings.TrimSpace(item.Metadata.Name),
			Phase:     strings.TrimSpace(item.Status.Phase),
		}
		for _, container := range item.Status.ContainerStatuses {
			pod.TotalContainers++
			if container.Ready {
				pod.ReadyContainers++
			}
		}
		if pod.Name == "" {
			pod.Name = "<unknown>"
		}
		pods = append(pods, pod)
	}
	return pods, nil
}

func inferKubernetesClusterProvider(api kubernetesClusterInspectAPI, nodes []kubernetesClusterInspectNode, pods []kubernetesClusterInspectPod) kubernetesClusterInspectProvider {
	text := strings.ToLower(api.ServerVersion.GitVersion + " " + api.ServerVersion.Platform + " " + api.Server)
	for _, node := range nodes {
		text += " " + strings.ToLower(node.KubeletVersion+" "+node.ProviderID)
		for key, value := range node.RoleLabels {
			text += " " + strings.ToLower(key+" "+value)
		}
	}
	for _, pod := range pods {
		text += " " + strings.ToLower(pod.Namespace+" "+pod.Name)
	}
	add := func(provider kubernetesClusterInspectProvider, reason string) kubernetesClusterInspectProvider {
		provider.Reasons = append(provider.Reasons, reason)
		return provider
	}
	switch {
	case strings.Contains(text, "eks.amazonaws.com") || strings.Contains(text, "eks") || strings.Contains(text, "aws://"):
		return add(kubernetesClusterInspectProvider{Distribution: "eks", Confidence: "high", Managed: true}, "EKS labels, version, or AWS providerID detected")
	case strings.Contains(text, "cloud.google.com/gke") || strings.Contains(text, "gke") || strings.Contains(text, "gce://"):
		return add(kubernetesClusterInspectProvider{Distribution: "gke", Confidence: "high", Managed: true}, "GKE labels, version, or GCE providerID detected")
	case strings.Contains(text, "kubernetes.azure.com") || strings.Contains(text, "aks") || strings.Contains(text, "azure://"):
		return add(kubernetesClusterInspectProvider{Distribution: "aks", Confidence: "high", Managed: true}, "AKS labels, version, or Azure providerID detected")
	case strings.Contains(text, "+rke2") || strings.Contains(text, "rke2.io"):
		return add(kubernetesClusterInspectProvider{Distribution: "rke2", Confidence: "high"}, "RKE2 version or labels detected")
	case strings.Contains(text, "+k3s") || strings.Contains(text, "k3s.io"):
		return add(kubernetesClusterInspectProvider{Distribution: "k3s", Confidence: "high"}, "k3s version or labels detected")
	case strings.Contains(text, "kube-apiserver") || strings.Contains(text, "kube-controller-manager") || strings.Contains(text, "kube-scheduler"):
		return add(kubernetesClusterInspectProvider{Distribution: "kubeadm", Confidence: "medium"}, "self-managed control-plane static pods detected")
	default:
		return kubernetesClusterInspectProvider{Distribution: "unknown", Confidence: "low", Reasons: []string{"no managed-provider or self-managed distribution markers detected"}}
	}
}

func kubernetesClusterCertificateRenewalHint(provider kubernetesClusterInspectProvider) kubernetesClusterInspectCertificateRenewal {
	switch provider.Distribution {
	case "k3s", "rke2", "kubeadm":
		return kubernetesClusterInspectCertificateRenewal{
			Provider:                provider.Distribution,
			Supported:               true,
			RequiresExplicitTargets: false,
			Reason:                  "Torque can derive certificate maintenance targets from this inspect evidence when certificates.targetsFrom supplies the access template.",
		}
	case "eks", "gke", "aks":
		return kubernetesClusterInspectCertificateRenewal{
			Provider:          provider.Distribution,
			Supported:         false,
			ManagedExternally: true,
			Reason:            "managed Kubernetes provider controls certificate rotation; use provider-specific automation or custom commands only when explicitly required.",
		}
	default:
		return kubernetesClusterInspectCertificateRenewal{
			Provider:               "custom",
			Supported:              false,
			RequiresCustomCommands: true,
			Reason:                 "distribution is unknown from kubectl-visible data; configure custom certificate inspect/renew commands if Torque should manage certificates.",
		}
	}
}
