package analyze

import (
	"context"
	"fmt"
	"os"
	"strings"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Evidence collects all relevant data for analysis
type Evidence struct {
	Pod             *corev1.Pod
	Node            *corev1.Node      // Details of the node running the pod
	Events          []corev1.Event    // Events related to the pod
	NamespaceEvents []corev1.Event    // Recent events in the namespace (for context)
	Logs            map[string]string // ContainerName -> Last N lines of logs
	PreviousLogs    map[string]string // ContainerName -> Last N lines of previous logs (if crashed)
	ConfigWarnings  []string          // Missing ConfigMaps, Secrets, etc.
	NetworkContext  []string          // Services, Endpoints status
	ResourceInfo    []string          // QoS, Limits, OOMKilled history
	ImageAnalysis   []string          // Tag validation, known vulnerabilities
	SecurityAudit   []string          // ServiceAccount, SecurityContext
	Availability    []string          // PDB status
	ChangeDiff      []string          // Diff vs previous revision
	LocalDocs       string            // Content of local troubleshooting docs
	SourceSnippets  []Snippet         // Code snippets linked from stack traces
	Manifest        string            // YAML representation (optional)
}

// Diagnosis represents the result of an analysis
type Diagnosis struct {
	RootCause       string
	Suggestion      string
	Explanation     string
	ConfidenceScore float64 // 0.0 to 1.0
	Patch           string  // Optional: Patch to apply
}

type Analyzer interface {
	Analyze(ctx context.Context, evidence *Evidence) (*Diagnosis, error)
}

// GatherEvidence collects data from the cluster
func GatherEvidence(ctx context.Context, client kubernetes.Interface, namespace, podName string) (*Evidence, error) {
	pod, err := client.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to get pod: %w", err)
	}

	// 1. Get Pod Events
	events, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", podName),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get events: %w", err)
	}

	// 2. Get Node Info (if scheduled)
	var node *corev1.Node
	if pod.Spec.NodeName != "" {
		n, err := client.CoreV1().Nodes().Get(ctx, pod.Spec.NodeName, metav1.GetOptions{})
		if err == nil {
			node = n
		}
	}

	// 3. Get Recent Namespace Events (Contextual Clues)
	// We grab the last 20 warning events in the namespace to see if there's a wider issue
	// (e.g. Quota exceeded, PVC binding issues unrelated to this specific pod object ref)
	nsEventsList, err := client.CoreV1().Events(namespace).List(ctx, metav1.ListOptions{
		Limit: 20,
	})
	var nsEvents []corev1.Event
	if err == nil {
		// Simple filter for Warnings or recent stuff could be done here.
		// For now, we just pass them all, and let the analyzer filter.
		nsEvents = nsEventsList.Items
	}

	// 3.5 Config/Secret Validation (Iteration 2)
	configWarnings := validateConfigs(ctx, client, namespace, pod)

	// 3.6 Network Reachability Check (Iteration 3)
	networkContext := checkNetwork(ctx, client, namespace, pod)

	// 3.8 Resource Analysis (Iteration 6)
	resourceInfo := checkResources(pod)

	// 3.9 Image Analysis (Iteration 7)
	imageAnalysis := checkImages(pod)

	// 3.10 Security Audit (Iteration 8)
	securityAudit := checkSecurity(ctx, client, namespace, pod)

	// 3.11 Availability Check (PDB) (Iteration 9)
	availability := checkAvailability(ctx, client, namespace, pod)

	// 3.12 Change Detection (Iteration 10)
	changeDiff := checkChanges(ctx, client, namespace, pod)

	// 3.7 Local Knowledge Base (Iteration 4)
	localDocs := findLocalDocs()

	logs := make(map[string]string)
	prevLogs := make(map[string]string)
	// Tail logs for all containers (init and regular)
	allContainers := append(pod.Spec.InitContainers, pod.Spec.Containers...)
	for _, c := range allContainers {
		// 1. Current Logs
		opts := &corev1.PodLogOptions{
			Container: c.Name,
			TailLines: int64Ptr(50),
		}
		req := client.CoreV1().Pods(namespace).GetLogs(podName, opts)
		podLogs, err := req.DoRaw(ctx)
		if err == nil {
			logs[c.Name] = string(podLogs)
		} else {
			logs[c.Name] = fmt.Sprintf("<failed to fetch logs: %v>", err)
		}

		// 2. Previous Logs (Time Travel)
		// Check if the container has restarted
		status := getContainerStatus(pod, c.Name)
		if status != nil && status.RestartCount > 0 {
			prevOpts := &corev1.PodLogOptions{
				Container: c.Name,
				TailLines: int64Ptr(50),
				Previous:  true,
			}
			reqPrev := client.CoreV1().Pods(namespace).GetLogs(podName, prevOpts)
			prevPodLogs, err := reqPrev.DoRaw(ctx)
			if err == nil {
				prevLogs[c.Name] = string(prevPodLogs)
			}
		}
	}

	// 4. Source Code Correlation (The Magic)
	// We scan the collected logs for stack traces and try to find matching code in the CWD
	var snippets []Snippet
	for _, log := range logs {
		s := FindSourceSnippets(log)
		snippets = append(snippets, s...)
	}
	for _, log := range prevLogs {
		s := FindSourceSnippets(log)
		snippets = append(snippets, s...)
	}

	return &Evidence{
		Pod:             pod,
		Node:            node,
		Events:          events.Items,
		NamespaceEvents: nsEvents,
		Logs:            logs,
		PreviousLogs:    prevLogs,
		ConfigWarnings:  configWarnings,
		NetworkContext:  networkContext,
		ResourceInfo:    resourceInfo,
		ImageAnalysis:   imageAnalysis,
		SecurityAudit:   securityAudit,
		Availability:    availability,
		ChangeDiff:      changeDiff,
		LocalDocs:       localDocs,
		SourceSnippets:  snippets,
	}, nil
}

func checkChanges(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var changes []string

	// Only works if pod is owned by a ReplicaSet (Deployment)
	var rsName string
	for _, owner := range pod.OwnerReferences {
		if owner.Kind == "ReplicaSet" {
			rsName = owner.Name
			break
		}
	}
	if rsName == "" {
		return changes
	}

	// Get current RS
	currentRS, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, rsName, metav1.GetOptions{})
	if err != nil {
		return changes
	}

	// Find all RS for this deployment (by matching labels)
	// Heuristic: assume RS name is dep-hash.
	// Better: List RS with same owner? No, RS owner is Deployment.
	// Best: List all RS, filter by same OwnerReferences as currentRS

	allRS, err := client.AppsV1().ReplicaSets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return changes
	}

	var siblings []metav1.Object
	for _, rs := range allRS.Items {
		if isSibling(currentRS, &rs) {
			siblings = append(siblings, &rs)
		}
	}

	// Sort by creation timestamp
	// ... (Simplification: just look for one created just before current)
	var prevRS *appsv1.ReplicaSet
	for _, rsObj := range siblings {
		rs := rsObj.(*appsv1.ReplicaSet)
		if rs.Name == currentRS.Name {
			continue
		}
		if rs.CreationTimestamp.Before(&currentRS.CreationTimestamp) {
			if prevRS == nil || rs.CreationTimestamp.After(prevRS.CreationTimestamp.Time) {
				prevRS = rs
			}
		}
	}

	if prevRS != nil {
		changes = append(changes, fmt.Sprintf("Previous Revision found: %s", prevRS.Name))
		// Diff Images
		currImages := getImages(currentRS.Spec.Template.Spec)
		prevImages := getImages(prevRS.Spec.Template.Spec)
		for c, img := range currImages {
			if prevImg, ok := prevImages[c]; ok {
				if img != prevImg {
					changes = append(changes, fmt.Sprintf("CHANGE: Container '%s' image changed: %s -> %s", c, prevImg, img))
				}
			} else {
				changes = append(changes, fmt.Sprintf("CHANGE: New container '%s' added.", c))
			}
		}
	}

	return changes
}

func isSibling(a, b metav1.Object) bool {
	if len(a.GetOwnerReferences()) == 0 || len(b.GetOwnerReferences()) == 0 {
		return false
	}
	return a.GetOwnerReferences()[0].UID == b.GetOwnerReferences()[0].UID
}

func getImages(spec corev1.PodSpec) map[string]string {
	m := make(map[string]string)
	for _, c := range spec.Containers {
		m[c.Name] = c.Image
	}
	return m
}

func checkAvailability(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string

	// Check PDBs
	// Note: We use the discovery client to check if PDB API exists, but for brevity we assume v1.
	// If the cluster is very old, this might fail, but we'll wrap it.

	pdbs, err := client.PolicyV1().PodDisruptionBudgets(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		// Fallback to v1beta1? No, let's just ignore or log error
		return info
	}

	for _, pdb := range pdbs.Items {
		match := true
		for k, v := range pdb.Spec.Selector.MatchLabels {
			if pod.Labels[k] != v {
				match = false
				break
			}
		}
		if match && len(pdb.Spec.Selector.MatchLabels) > 0 {
			if pdb.Status.DisruptionsAllowed == 0 {
				info = append(info, fmt.Sprintf("WARNING: PDB '%s' blocking eviction. DisruptionsAllowed: 0, CurrentHealthy: %d, DesiredHealthy: %d", pdb.Name, pdb.Status.CurrentHealthy, pdb.Status.DesiredHealthy))
			} else {
				info = append(info, fmt.Sprintf("PDB '%s' allows %d disruptions.", pdb.Name, pdb.Status.DisruptionsAllowed))
			}
		}
	}
	return info
}

func checkSecurity(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string

	// 1. Service Account
	saName := pod.Spec.ServiceAccountName
	if saName == "" {
		saName = "default"
	}
	_, err := client.CoreV1().ServiceAccounts(namespace).Get(ctx, saName, metav1.GetOptions{})
	if err != nil {
		info = append(info, fmt.Sprintf("CRITICAL: ServiceAccount '%s' does not exist. Pod cannot authenticate.", saName))
	} else {
		if saName == "default" {
			info = append(info, "NOTE: Pod uses 'default' ServiceAccount. Ensure it has necessary permissions.")
		}
	}

	// 2. Security Context
	if pod.Spec.SecurityContext != nil {
		if pod.Spec.SecurityContext.RunAsNonRoot != nil && *pod.Spec.SecurityContext.RunAsNonRoot {
			info = append(info, "Security: RunAsNonRoot is enabled (Good).")
		}
	}

	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil {
			if c.SecurityContext.Privileged != nil && *c.SecurityContext.Privileged {
				info = append(info, fmt.Sprintf("WARNING: Container '%s' is PRIVILEGED. This is a security risk.", c.Name))
			}
			if c.SecurityContext.RunAsUser != nil && *c.SecurityContext.RunAsUser == 0 {
				info = append(info, fmt.Sprintf("WARNING: Container '%s' explicitly runs as ROOT (UID 0).", c.Name))
			}
		}
	}

	return info
}

func checkImages(pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		if strings.HasSuffix(c.Image, ":latest") {
			info = append(info, fmt.Sprintf("WARNING: Container '%s' uses ':latest' tag (%s). This is unstable for production.", c.Name, c.Image))
		}
		if !strings.Contains(c.Image, ":") {
			info = append(info, fmt.Sprintf("WARNING: Container '%s' uses image '%s' without explicit tag (defaults to latest).", c.Name, c.Image))
		}
		// Basic heuristics for "heavy" images
		if strings.Contains(c.Image, "ubuntu") || strings.Contains(c.Image, "debian") || strings.Contains(c.Image, "centos") {
			info = append(info, fmt.Sprintf("NOTE: Container '%s' uses a full OS image (%s). Consider using distroless or alpine for security/size.", c.Name, c.Image))
		}
	}
	return info
}

func checkResources(pod *corev1.Pod) []string {
	var info []string

	// QoS Class
	info = append(info, fmt.Sprintf("QoS Class: %s", pod.Status.QOSClass))
	if pod.Status.QOSClass == corev1.PodQOSBestEffort {
		info = append(info, "WARNING: Pod is BestEffort (no requests/limits). It will be evicted first under node pressure.")
	}

	// Container Limits
	for _, c := range pod.Spec.Containers {
		req := c.Resources.Requests
		lim := c.Resources.Limits
		if len(lim) == 0 {
			info = append(info, fmt.Sprintf("Container '%s' has NO limits set.", c.Name))
		} else {
			info = append(info, fmt.Sprintf("Container '%s' Limits: CPU=%s, Mem=%s", c.Name, lim.Cpu(), lim.Memory()))
		}
		if len(req) == 0 {
			info = append(info, fmt.Sprintf("Container '%s' has NO requests set.", c.Name))
		}
	}

	// OOMKilled Check
	for _, s := range pod.Status.ContainerStatuses {
		if s.State.Terminated != nil && s.State.Terminated.Reason == "OOMKilled" {
			info = append(info, fmt.Sprintf("CRITICAL: Container '%s' was OOMKilled. Memory limit (%s) is too low.", s.Name, pod.Spec.Containers[0].Resources.Limits.Memory()))
		}
		if s.LastTerminationState.Terminated != nil && s.LastTerminationState.Terminated.Reason == "OOMKilled" {
			info = append(info, fmt.Sprintf("CRITICAL: Container '%s' was previously OOMKilled.", s.Name))
		}
	}

	return info
}

func findLocalDocs() string {
	candidates := []string{
		".ktl/knowledge.md",
		"TROUBLESHOOTING.md",
		"docs/troubleshooting.md",
		"docs/runbook.md",
	}

	for _, path := range candidates {
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
	}
	return ""
}

func checkNetwork(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var netInfo []string

	if pod.Status.PodIP == "" {
		netInfo = append(netInfo, "Pod has no IP address assigned yet.")
	} else {
		netInfo = append(netInfo, fmt.Sprintf("Pod IP: %s", pod.Status.PodIP))
	}

	// Find services selecting this pod
	svcs, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		netInfo = append(netInfo, fmt.Sprintf("Failed to list services: %v", err))
		return netInfo
	}

	for _, svc := range svcs.Items {
		match := true
		for k, v := range svc.Spec.Selector {
			if pod.Labels[k] != v {
				match = false
				break
			}
		}
		if match && len(svc.Spec.Selector) > 0 {
			// Found a service targeting this pod
			// Check endpoints
			eps, err := client.CoreV1().Endpoints(namespace).Get(ctx, svc.Name, metav1.GetOptions{})
			if err != nil {
				netInfo = append(netInfo, fmt.Sprintf("Service '%s' selects this pod but failed to get endpoints: %v", svc.Name, err))
				continue
			}

			// Check if pod is in endpoints
			foundInEp := false
			ready := false
			for _, subset := range eps.Subsets {
				for _, addr := range subset.Addresses {
					if addr.TargetRef != nil && addr.TargetRef.UID == pod.UID {
						foundInEp = true
						ready = true
					}
				}
				for _, addr := range subset.NotReadyAddresses {
					if addr.TargetRef != nil && addr.TargetRef.UID == pod.UID {
						foundInEp = true
						ready = false
					}
				}
			}

			if !foundInEp {
				netInfo = append(netInfo, fmt.Sprintf("WARNING: Service '%s' selects this pod labels, but Pod is NOT in Endpoints. Check Readiness Probes.", svc.Name))
			} else if !ready {
				netInfo = append(netInfo, fmt.Sprintf("WARNING: Service '%s' has this pod in Endpoints, but it is NOT Ready.", svc.Name))
			} else {
				netInfo = append(netInfo, fmt.Sprintf("Service '%s' correctly routes to this pod (Ready).", svc.Name))
			}
		}
	}
	return netInfo
}

func validateConfigs(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var warnings []string

	// Helper to check ConfigMap
	checkCM := func(name string) {
		_, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Missing ConfigMap: %s (Error: %v)", name, err))
		}
	}

	// Helper to check Secret
	checkSecret := func(name string) {
		_, err := client.CoreV1().Secrets(namespace).Get(ctx, name, metav1.GetOptions{})
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("Missing Secret: %s (Error: %v)", name, err))
		}
	}

	for _, c := range pod.Spec.Containers {
		for _, env := range c.Env {
			if env.ValueFrom != nil {
				if env.ValueFrom.ConfigMapKeyRef != nil {
					checkCM(env.ValueFrom.ConfigMapKeyRef.Name)
				}
				if env.ValueFrom.SecretKeyRef != nil {
					checkSecret(env.ValueFrom.SecretKeyRef.Name)
				}
			}
		}
		for _, envFrom := range c.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				checkCM(envFrom.ConfigMapRef.Name)
			}
			if envFrom.SecretRef != nil {
				checkSecret(envFrom.SecretRef.Name)
			}
		}
	}

	for _, vol := range pod.Spec.Volumes {
		if vol.ConfigMap != nil {
			checkCM(vol.ConfigMap.Name)
		}
		if vol.Secret != nil {
			checkSecret(vol.Secret.SecretName)
		}
		if vol.PersistentVolumeClaim != nil {
			_, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, vol.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
			if err != nil {
				warnings = append(warnings, fmt.Sprintf("Missing PVC: %s (Error: %v)", vol.PersistentVolumeClaim.ClaimName, err))
			}
		}
	}

	return warnings
}

func getContainerStatus(pod *corev1.Pod, name string) *corev1.ContainerStatus {
	for _, s := range pod.Status.ContainerStatuses {
		if s.Name == name {
			return &s
		}
	}
	for _, s := range pod.Status.InitContainerStatuses {
		if s.Name == name {
			return &s
		}
	}
	return nil
}

func int64Ptr(i int64) *int64 { return &i }
