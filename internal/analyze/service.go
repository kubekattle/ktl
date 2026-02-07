package analyze

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// Evidence collects all relevant data for analysis
type Evidence struct {
	Pod               *corev1.Pod
	Node              *corev1.Node      // Details of the node running the pod
	Events            []corev1.Event    // Events related to the pod
	NamespaceEvents   []corev1.Event    // Recent events in the namespace (for context)
	Logs              map[string]string // ContainerName -> Last N lines of logs
	PreviousLogs      map[string]string // ContainerName -> Last N lines of previous logs (if crashed)
	ConfigWarnings    []string          // Missing ConfigMaps, Secrets, etc.
	NetworkContext    []string          // Services, Endpoints status
	ResourceInfo      []string          // QoS, Limits, OOMKilled history
	ImageAnalysis     []string          // Tag validation, known vulnerabilities
	SecurityAudit     []string          // ServiceAccount, SecurityContext
	Availability      []string          // PDB status
	ChangeDiff        []string          // Diff vs previous revision
	IngressInfo       []string          // Ingress details
	ScalingInfo       []string          // HPA details
	StorageInfo       []string          // PVC details
	SchedulingInfo    []string          // Taints/Affinity
	LifecycleInfo     []string          // Hooks
	ProbeInfo         []string          // Liveness/Readiness issues
	SecretValidation  []string          // Content checks (trailing newlines)
	MeshInfo          []string          // Sidecar status
	InitExitInfo      []string          // Init container exit codes
	OwnerChain        []string          // Hierarchy
	NetPolicyInfo     []string          // Network Policies
	CertInfo          []string          // Certificate expiration
	QuotaInfo         []string          // ResourceQuotas
	LogInsights       []string          // Pattern matching results
	AffinityInfo      []string          // Node/Pod Affinity
	PSAInfo           []string          // Pod Security Admission
	SpreadInfo        []string          // Topology Spread
	PriorityInfo      []string          // PriorityClass
	FinalizerInfo     []string          // Finalizers
	DNSInfo           []string          // CoreDNS status
	NodeExtInfo       []string          // Kernel, Runtime, Pressure
	SecurityExtInfo   []string          // Caps, Seccomp, AppArmor
	VolumeExtInfo     []string          // HostPath, HugePages, DownwardAPI
	ServiceExtInfo    []string          // Port mismatch, LB status
	IngressExtInfo    []string          // TLS, Class
	ConfigSyntaxInfo  []string          // JSON/YAML validation
	PodStateExtInfo   []string          // Zombie, Backoff
	ControllerExtInfo []string          // Deployment strategy, CronJob
	LocalDocs         string            // Content of local troubleshooting docs
	SourceSnippets    []Snippet         // Code snippets linked from stack traces
	Manifest          string            // YAML representation (optional)
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

	// Iteration 11: Ingress
	ingressInfo := checkIngress(ctx, client, namespace, pod)

	// Iteration 12: HPA
	scalingInfo := checkHPA(ctx, client, namespace, pod)

	// Iteration 13: PVC
	storageInfo := checkStorage(ctx, client, namespace, pod)

	// Iteration 14: Scheduling
	schedulingInfo := checkScheduling(pod, node)

	// Iteration 15: Lifecycle
	lifecycleInfo := checkLifecycle(pod)

	// Iteration 16: Probes
	probeInfo := checkProbes(pod)

	// Iteration 17: Secret Validation (Deep)
	secretVal := checkSecretsDeep(ctx, client, namespace, pod)

	// Iteration 18: Mesh
	meshInfo := checkMesh(pod)

	// Iteration 19: Init Exit Codes
	initExit := checkInitExit(pod)

	// Iteration 20: Owner Chain
	ownerChain := checkOwnerChain(ctx, client, namespace, pod)

	// Iteration 21: Network Policies
	netPolicyInfo := checkNetworkPolicies(ctx, client, namespace, pod)

	// Iteration 22: Certificates
	certInfo := checkCertificates(ctx, client, namespace, pod)

	// Iteration 23: Quotas
	quotaInfo := checkResourceQuotas(ctx, client, namespace)

	// Iteration 24: Ephemeral Storage
	ephemeralInfo := checkEphemeralStorage(pod)

	// Iteration 26: Affinity
	affinityInfo := checkAffinity(pod)

	// Iteration 27: PSA
	psaInfo := checkPSA(ctx, client, namespace, pod)

	// Iteration 28: Topology Spread
	spreadInfo := checkTopologySpread(pod)

	// Iteration 29: Priority
	priorityInfo := checkPriority(pod)

	// Iteration 30: Finalizers
	finalizerInfo := checkFinalizers(pod)

	// Iteration 31: DNS Health
	dnsInfo := checkDNS(ctx, client)

	// Iteration 32-33: Node Extended
	nodeExtInfo := checkNodeExt(node)

	// Iteration 34, 36, 37: Security Extended
	secExtInfo := checkSecurityExt(pod)

	// Iteration 35, 38, 39, 40: Volume/Resource Extended
	volExtInfo := checkVolumeExt(pod)

	// Iteration 41-42: Service Extended
	svcExtInfo := checkServiceExt(ctx, client, namespace, pod)

	// Iteration 43-44: Ingress Extended
	ingExtInfo := checkIngressExt(ctx, client, namespace, pod)

	// Iteration 45: Config Syntax
	confSynInfo := checkConfigSyntax(ctx, client, namespace, pod)

	// Iteration 46-48: Pod State Extended
	podStateInfo := checkPodStateExt(pod)

	// Iteration 49-50: Controller Extended
	ctrlExtInfo := checkControllerExt(ctx, client, namespace, pod)

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

	resourceInfo = append(resourceInfo, ephemeralInfo...)

	// Iteration 25: Log Insights (Must be done after logs are fetched)
	logInsights := analyzeLogPatterns(logs, prevLogs)

	return &Evidence{
		Pod:               pod,
		Node:              node,
		Events:            events.Items,
		NamespaceEvents:   nsEvents,
		Logs:              logs,
		PreviousLogs:      prevLogs,
		ConfigWarnings:    configWarnings,
		NetworkContext:    networkContext,
		ResourceInfo:      resourceInfo, // Includes ephemeral
		ImageAnalysis:     imageAnalysis,
		SecurityAudit:     securityAudit,
		Availability:      availability,
		ChangeDiff:        changeDiff,
		IngressInfo:       ingressInfo,
		ScalingInfo:       scalingInfo,
		StorageInfo:       storageInfo,
		SchedulingInfo:    schedulingInfo,
		LifecycleInfo:     lifecycleInfo,
		ProbeInfo:         probeInfo,
		SecretValidation:  secretVal,
		MeshInfo:          meshInfo,
		InitExitInfo:      initExit,
		OwnerChain:        ownerChain,
		NetPolicyInfo:     netPolicyInfo,
		CertInfo:          certInfo,
		QuotaInfo:         quotaInfo,
		LogInsights:       logInsights,
		AffinityInfo:      affinityInfo,
		PSAInfo:           psaInfo,
		SpreadInfo:        spreadInfo,
		PriorityInfo:      priorityInfo,
		FinalizerInfo:     finalizerInfo,
		DNSInfo:           dnsInfo,
		NodeExtInfo:       nodeExtInfo,
		SecurityExtInfo:   secExtInfo,
		VolumeExtInfo:     volExtInfo,
		ServiceExtInfo:    svcExtInfo,
		IngressExtInfo:    ingExtInfo,
		ConfigSyntaxInfo:  confSynInfo,
		PodStateExtInfo:   podStateInfo,
		ControllerExtInfo: ctrlExtInfo,
		LocalDocs:         localDocs,
		SourceSnippets:    snippets,
	}, nil
}

func checkServiceExt(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	svcs, err := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
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
			// Check ports
			for _, port := range svc.Spec.Ports {
				foundPort := false
				for _, c := range pod.Spec.Containers {
					for _, cp := range c.Ports {
						if cp.ContainerPort == port.TargetPort.IntVal || port.TargetPort.StrVal == cp.Name {
							foundPort = true
						}
					}
					// Also check if targetPort is not set, defaults to port
					if port.TargetPort.IntVal == 0 && port.TargetPort.StrVal == "" {
						if port.Port == 0 {
							continue
						} // Should not happen
						// Assuming container listens on same port (heuristic)
					}
				}
				if !foundPort && port.TargetPort.IntVal != 0 {
					info = append(info, fmt.Sprintf("WARNING: Service '%s' targets port %s, but no container exposes it explicitly.", svc.Name, port.TargetPort.String()))
				}
			}

			// Check LoadBalancer
			if svc.Spec.Type == corev1.ServiceTypeLoadBalancer {
				if len(svc.Status.LoadBalancer.Ingress) == 0 {
					info = append(info, fmt.Sprintf("WARNING: Service '%s' is LoadBalancer but has no IP allocated yet.", svc.Name))
				}
			}
		}
	}
	return info
}

func checkIngressExt(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	// Reuse ingress check logic or fetch new
	ingresses, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	for _, ing := range ingresses.Items {
		// TLS Check
		for _, tls := range ing.Spec.TLS {
			if tls.SecretName != "" {
				_, err := client.CoreV1().Secrets(namespace).Get(ctx, tls.SecretName, metav1.GetOptions{})
				if err != nil {
					info = append(info, fmt.Sprintf("CRITICAL: Ingress '%s' refers to missing TLS secret '%s'.", ing.Name, tls.SecretName))
				}
			}
		}
		// Class Check
		if ing.Spec.IngressClassName != nil {
			// List classes (cluster scoped) - need cluster permissions, might fail
			// Just skipping for now to avoid permission errors in restricted envs
		}
	}
	return info
}

func checkConfigSyntax(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	// Check ConfigMaps referenced
	for _, c := range pod.Spec.Containers {
		for _, envFrom := range c.EnvFrom {
			if envFrom.ConfigMapRef != nil {
				cm, err := client.CoreV1().ConfigMaps(namespace).Get(ctx, envFrom.ConfigMapRef.Name, metav1.GetOptions{})
				if err == nil {
					for k, v := range cm.Data {
						if strings.HasSuffix(k, ".json") {
							if !json.Valid([]byte(v)) {
								info = append(info, fmt.Sprintf("WARNING: ConfigMap '%s' key '%s' has invalid JSON.", cm.Name, k))
							}
						}
						// Simple YAML check (heuristic)
						if strings.HasSuffix(k, ".yaml") || strings.HasSuffix(k, ".yml") {
							if strings.HasPrefix(strings.TrimSpace(v), "\t") {
								info = append(info, fmt.Sprintf("WARNING: ConfigMap '%s' key '%s' seems to use tabs. YAML forbids tabs.", cm.Name, k))
							}
						}
					}
				}
			}
		}
	}
	return info
}

func checkPodStateExt(pod *corev1.Pod) []string {
	var info []string
	// Zombie check
	if pod.DeletionTimestamp != nil {
		if time.Since(pod.DeletionTimestamp.Time) > 1*time.Hour {
			info = append(info, fmt.Sprintf("CRITICAL: Pod has been Terminating for > 1 hour (Zombie). Force delete might be needed."))
		}
	}

	// Restart Backoff
	for _, s := range pod.Status.ContainerStatuses {
		if s.State.Waiting != nil && s.State.Waiting.Reason == "CrashLoopBackOff" {
			// Can't easily get backoff duration from status, but we can infer from restart count vs age?
			// Just generic warning
			if s.RestartCount > 10 {
				info = append(info, fmt.Sprintf("Container '%s' has restarted %d times. Backoff is likely at max.", s.Name, s.RestartCount))
			}
		}
	}
	return info
}

func checkControllerExt(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" {
			rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
			if err == nil {
				for _, rsRef := range rs.OwnerReferences {
					if rsRef.Kind == "Deployment" {
						deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, rsRef.Name, metav1.GetOptions{})
						if err == nil {
							if deploy.Spec.Strategy.Type == appsv1.RollingUpdateDeploymentStrategyType {
								if deploy.Spec.Strategy.RollingUpdate != nil {
									info = append(info, fmt.Sprintf("Deployment Strategy: RollingUpdate (MaxUnavail: %s, MaxSurge: %s)", deploy.Spec.Strategy.RollingUpdate.MaxUnavailable.String(), deploy.Spec.Strategy.RollingUpdate.MaxSurge.String()))
								}
							}
						}
					}
				}
			}
		}
	}
	return info
}

func checkDNS(ctx context.Context, client kubernetes.Interface) []string {
	var info []string
	// Check kube-system for kube-dns or coredns
	svcs, err := client.CoreV1().Services("kube-system").List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	found := false
	for _, svc := range svcs.Items {
		if svc.Name == "kube-dns" || svc.Name == "coredns" {
			found = true
			// Check endpoints
			eps, err := client.CoreV1().Endpoints("kube-system").Get(ctx, svc.Name, metav1.GetOptions{})
			if err == nil {
				ready := 0
				for _, sub := range eps.Subsets {
					ready += len(sub.Addresses)
				}
				if ready == 0 {
					info = append(info, fmt.Sprintf("CRITICAL: Cluster DNS (%s) has 0 endpoints! Cluster-wide resolution might fail.", svc.Name))
				} else {
					info = append(info, fmt.Sprintf("Cluster DNS (%s) is healthy (%d endpoints).", svc.Name, ready))
				}
			}
		}
	}
	if !found {
		info = append(info, "WARNING: Could not find 'kube-dns' or 'coredns' service in kube-system.")
	}
	return info
}

func checkNodeExt(node *corev1.Node) []string {
	var info []string
	if node == nil {
		return info
	}

	info = append(info, fmt.Sprintf("Node Info: Kernel=%s, Runtime=%s, OS=%s", node.Status.NodeInfo.KernelVersion, node.Status.NodeInfo.ContainerRuntimeVersion, node.Status.NodeInfo.OSImage))

	// Extended Pressure Checks
	for _, cond := range node.Status.Conditions {
		if cond.Status == "True" {
			switch cond.Type {
			case corev1.NodePIDPressure:
				info = append(info, "CRITICAL: Node has PIDPressure. Too many processes.")
			case corev1.NodeMemoryPressure:
				info = append(info, "CRITICAL: Node has MemoryPressure. Eviction imminent.")
			case corev1.NodeDiskPressure:
				info = append(info, "CRITICAL: Node has DiskPressure. Garbage collection active.")
			}
		}
	}
	return info
}

func checkSecurityExt(pod *corev1.Pod) []string {
	var info []string

	if pod.Spec.AutomountServiceAccountToken != nil && !*pod.Spec.AutomountServiceAccountToken {
		info = append(info, "Security: AutomountServiceAccountToken is disabled (Good).")
	}

	for _, c := range pod.Spec.Containers {
		if c.SecurityContext != nil {
			if c.SecurityContext.Capabilities != nil {
				if len(c.SecurityContext.Capabilities.Add) > 0 {
					info = append(info, fmt.Sprintf("WARNING: Container '%s' adds capabilities: %v", c.Name, c.SecurityContext.Capabilities.Add))
				}
				if len(c.SecurityContext.Capabilities.Drop) > 0 {
					info = append(info, fmt.Sprintf("Security: Container '%s' drops capabilities: %v", c.Name, c.SecurityContext.Capabilities.Drop))
				}
			}
			if c.SecurityContext.AllowPrivilegeEscalation != nil && *c.SecurityContext.AllowPrivilegeEscalation {
				info = append(info, fmt.Sprintf("WARNING: Container '%s' allows privilege escalation.", c.Name))
			}
			if c.SecurityContext.ReadOnlyRootFilesystem != nil && *c.SecurityContext.ReadOnlyRootFilesystem {
				info = append(info, fmt.Sprintf("Security: Container '%s' has ReadOnlyRootFilesystem (Good).", c.Name))
			}
		}
	}
	return info
}

func checkVolumeExt(pod *corev1.Pod) []string {
	var info []string

	for _, vol := range pod.Spec.Volumes {
		if vol.HostPath != nil {
			info = append(info, fmt.Sprintf("WARNING: Volume '%s' uses HostPath (%s). Security risk & node dependency.", vol.Name, vol.HostPath.Path))
		}
		if vol.EmptyDir != nil {
			if vol.EmptyDir.SizeLimit == nil {
				info = append(info, fmt.Sprintf("NOTE: Volume '%s' (emptyDir) has no SizeLimit.", vol.Name))
			}
		}
		if vol.DownwardAPI != nil {
			info = append(info, fmt.Sprintf("Info: Volume '%s' uses DownwardAPI.", vol.Name))
		}
	}

	for _, c := range pod.Spec.Containers {
		for _, res := range c.Resources.Limits {
			if res.Format == "nvidia.com/gpu" {
				info = append(info, fmt.Sprintf("Resource: Container '%s' requests GPU.", c.Name))
			}
		}
	}

	return info
}

func analyzeLogPatterns(logs, prevLogs map[string]string) []string {
	var info []string

	check := func(source, log string) {
		if strings.Contains(log, "java.lang.OutOfMemoryError") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Java OOM detected. Tune -Xmx matches container memory.", source))
		}
		if strings.Contains(log, "FATAL: password authentication failed") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Postgres Auth Failure. Check DB_PASSWORD.", source))
		}
		if strings.Contains(log, "Connection refused") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Connection refused. Target service might be down.", source))
		}
		if strings.Contains(log, "permission denied") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Filesystem permission error. Check RunAsUser/fsGroup.", source))
		}
		if strings.Contains(log, "ModuleNotFoundError") || strings.Contains(log, "ImportError") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Python missing dependency. Check requirements.txt.", source))
		}
		if strings.Contains(log, "Error: Cannot find module") {
			info = append(info, fmt.Sprintf("Pattern Match (%s): Node.js missing module. Check package.json/npm install.", source))
		}
	}

	for c, l := range logs {
		check("Current-"+c, l)
	}
	for c, l := range prevLogs {
		check("Previous-"+c, l)
	}
	return info
}

func checkAffinity(pod *corev1.Pod) []string {
	var info []string
	if pod.Spec.Affinity != nil {
		if pod.Spec.Affinity.NodeAffinity != nil {
			info = append(info, "Note: Pod has NodeAffinity configured.")
		}
		if pod.Spec.Affinity.PodAntiAffinity != nil {
			info = append(info, "Note: Pod has PodAntiAffinity configured (Spread).")
		}
	}
	return info
}

func checkPSA(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	ns, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		return info
	}

	mode := ns.Labels["pod-security.kubernetes.io/enforce"]
	if mode != "" {
		info = append(info, fmt.Sprintf("Namespace enforcement: %s. Pod must comply.", mode))
		// Basic check
		if mode == "restricted" {
			if pod.Spec.SecurityContext == nil || pod.Spec.SecurityContext.RunAsNonRoot == nil || !*pod.Spec.SecurityContext.RunAsNonRoot {
				info = append(info, "WARNING: PSA 'restricted' requires RunAsNonRoot=true.")
			}
		}
	}
	return info
}

func checkTopologySpread(pod *corev1.Pod) []string {
	var info []string
	if len(pod.Spec.TopologySpreadConstraints) > 0 {
		info = append(info, fmt.Sprintf("Pod has %d TopologySpreadConstraints.", len(pod.Spec.TopologySpreadConstraints)))
	}
	return info
}

func checkPriority(pod *corev1.Pod) []string {
	var info []string
	if pod.Spec.PriorityClassName != "" {
		info = append(info, fmt.Sprintf("PriorityClass: %s (Priority: %d)", pod.Spec.PriorityClassName, *pod.Spec.Priority))
	}
	return info
}

func checkFinalizers(pod *corev1.Pod) []string {
	var info []string
	if len(pod.Finalizers) > 0 {
		info = append(info, fmt.Sprintf("Finalizers preventing deletion: %v", pod.Finalizers))
		if pod.DeletionTimestamp != nil {
			info = append(info, "CRITICAL: Pod is Terminating but stuck on Finalizers.")
		}
	}
	return info
}

func checkEphemeralStorage(pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		lim := c.Resources.Limits
		req := c.Resources.Requests
		if _, ok := lim[corev1.ResourceEphemeralStorage]; !ok {
			info = append(info, fmt.Sprintf("WARNING: Container '%s' has NO ephemeral-storage limit. It can consume all node disk.", c.Name))
		}
		if _, ok := req[corev1.ResourceEphemeralStorage]; !ok {
			info = append(info, fmt.Sprintf("NOTE: Container '%s' has NO ephemeral-storage request.", c.Name))
		}
	}
	return info
}

func checkProbes(pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		if c.LivenessProbe != nil {
			if c.LivenessProbe.TimeoutSeconds > c.LivenessProbe.PeriodSeconds {
				info = append(info, fmt.Sprintf("WARNING: Container '%s' LivenessProbe Timeout (%ds) > Period (%ds).", c.Name, c.LivenessProbe.TimeoutSeconds, c.LivenessProbe.PeriodSeconds))
			}
		} else {
			info = append(info, fmt.Sprintf("NOTE: Container '%s' has NO LivenessProbe.", c.Name))
		}
		if c.ReadinessProbe == nil {
			info = append(info, fmt.Sprintf("NOTE: Container '%s' has NO ReadinessProbe.", c.Name))
		}
	}
	return info
}

func checkSecretsDeep(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	// Check env vars referencing secrets
	for _, c := range pod.Spec.Containers {
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				secretName := env.ValueFrom.SecretKeyRef.Name
				key := env.ValueFrom.SecretKeyRef.Key

				secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					continue // Already handled by validateConfigs
				}

				valBytes, ok := secret.Data[key]
				if !ok {
					continue
				}
				val := string(valBytes)

				if strings.HasSuffix(val, "\n") || strings.HasSuffix(val, "\r") {
					info = append(info, fmt.Sprintf("WARNING: Secret '%s' key '%s' has a trailing newline. This often breaks apps.", secretName, key))
				}
				if strings.Contains(val, " ") && (strings.Contains(strings.ToLower(key), "key") || strings.Contains(strings.ToLower(key), "pass")) {
					info = append(info, fmt.Sprintf("NOTE: Secret '%s' key '%s' contains spaces. Verify this is intended.", secretName, key))
				}
			}
		}
	}
	return info
}

func checkMesh(pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		if c.Name == "istio-proxy" || c.Name == "linkerd-proxy" {
			info = append(info, fmt.Sprintf("Service Mesh Sidecar detected: %s", c.Name))
			// Find status
			status := getContainerStatus(pod, c.Name)
			if status != nil {
				if !status.Ready {
					info = append(info, fmt.Sprintf("CRITICAL: Mesh Sidecar '%s' is NOT Ready.", c.Name))
				}
				if status.RestartCount > 0 {
					info = append(info, fmt.Sprintf("WARNING: Mesh Sidecar '%s' has restarted %d times.", c.Name, status.RestartCount))
				}
			}
		}
	}
	return info
}

func checkInitExit(pod *corev1.Pod) []string {
	var info []string
	for _, s := range pod.Status.InitContainerStatuses {
		if s.State.Terminated != nil && s.State.Terminated.ExitCode != 0 {
			info = append(info, fmt.Sprintf("CRITICAL: InitContainer '%s' failed with ExitCode %d (Reason: %s).", s.Name, s.State.Terminated.ExitCode, s.State.Terminated.Reason))
		}
	}
	return info
}

func checkOwnerChain(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var chain []string
	current := metav1.Object(pod)

	for {
		owners := current.GetOwnerReferences()
		if len(owners) == 0 {
			break
		}
		// Follow first owner
		ref := owners[0]
		chain = append(chain, fmt.Sprintf("%s/%s", ref.Kind, ref.Name))

		if ref.Kind == "ReplicaSet" {
			rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
			if err == nil {
				current = rs
				continue
			}
		} else if ref.Kind == "Deployment" {
			// Stop at deployment usually
			break
		} else if ref.Kind == "Job" {
			// Could go to CronJob
			break
		}
		break
	}
	return chain
}

func checkNetworkPolicies(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	pols, err := client.NetworkingV1().NetworkPolicies(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	deniedIngress := false
	deniedEgress := false

	for _, pol := range pols.Items {
		match := true
		for k, v := range pol.Spec.PodSelector.MatchLabels {
			if pod.Labels[k] != v {
				match = false
				break
			}
		}
		if match {
			// This policy applies to the pod
			// Simplified analysis: just listing them
			types := []string{}
			for _, t := range pol.Spec.PolicyTypes {
				types = append(types, string(t))
				if t == networkingv1.PolicyTypeIngress {
					deniedIngress = true
				} // Default deny unless allowed
				if t == networkingv1.PolicyTypeEgress {
					deniedEgress = true
				}
			}
			info = append(info, fmt.Sprintf("NetworkPolicy '%s' applies to this pod (Types: %v).", pol.Name, types))
		}
	}

	if deniedIngress {
		info = append(info, "NOTE: At least one NetworkPolicy selects this pod. Ingress traffic is restricted unless explicitly allowed.")
	}
	if deniedEgress {
		info = append(info, "NOTE: At least one NetworkPolicy selects this pod. Egress traffic is restricted unless explicitly allowed.")
	}

	return info
}

func checkCertificates(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		for _, env := range c.Env {
			if env.ValueFrom != nil && env.ValueFrom.SecretKeyRef != nil {
				// Check if secret looks like a cert
				secretName := env.ValueFrom.SecretKeyRef.Name
				key := env.ValueFrom.SecretKeyRef.Key

				// Optimization: avoid re-fetching if possible, but for now just fetch
				secret, err := client.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
				if err != nil {
					continue
				}

				valBytes := secret.Data[key]
				if strings.Contains(string(valBytes), "-----BEGIN CERTIFICATE-----") {
					block, _ := pem.Decode(valBytes)
					if block != nil {
						cert, err := x509.ParseCertificate(block.Bytes)
						if err == nil {
							if time.Now().After(cert.NotAfter) {
								info = append(info, fmt.Sprintf("CRITICAL: Certificate in Secret '%s' (key '%s') EXPIRED on %s.", secretName, key, cert.NotAfter.Format(time.RFC3339)))
							} else if time.Now().Add(24 * 7 * time.Hour).After(cert.NotAfter) {
								info = append(info, fmt.Sprintf("WARNING: Certificate in Secret '%s' (key '%s') expires soon (%s).", secretName, key, cert.NotAfter.Format(time.RFC3339)))
							}
						}
					}
				}
			}
		}
	}
	// TODO: Check volumes too
	return info
}

func checkResourceQuotas(ctx context.Context, client kubernetes.Interface, namespace string) []string {
	var info []string
	quotas, err := client.CoreV1().ResourceQuotas(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	for _, q := range quotas.Items {
		for resName, qty := range q.Status.Hard {
			used := q.Status.Used[resName]
			if used.Cmp(qty) >= 0 {
				info = append(info, fmt.Sprintf("WARNING: ResourceQuota '%s' hit limit for %s (Used: %s, Limit: %s).", q.Name, resName, used.String(), qty.String()))
			}
		}
	}
	return info
}

func checkIngress(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	ingresses, err := client.NetworkingV1().Ingresses(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	// Find Service name first (simplified from checkNetwork)
	var svcName string
	svcs, _ := client.CoreV1().Services(namespace).List(ctx, metav1.ListOptions{})
	for _, svc := range svcs.Items {
		match := true
		for k, v := range svc.Spec.Selector {
			if pod.Labels[k] != v {
				match = false
				break
			}
		}
		if match && len(svc.Spec.Selector) > 0 {
			svcName = svc.Name
			break
		}
	}

	if svcName == "" {
		return info
	}

	for _, ing := range ingresses.Items {
		for _, rule := range ing.Spec.Rules {
			if rule.HTTP == nil {
				continue
			}
			for _, path := range rule.HTTP.Paths {
				if path.Backend.Service != nil && path.Backend.Service.Name == svcName {
					info = append(info, fmt.Sprintf("Ingress '%s' routes host '%s' path '%s' to service '%s'.", ing.Name, rule.Host, path.Path, svcName))
				}
			}
		}
	}
	return info
}

func checkHPA(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	// Assume Pod -> RS -> Deploy
	var deployName string
	for _, ref := range pod.OwnerReferences {
		if ref.Kind == "ReplicaSet" {
			rs, err := client.AppsV1().ReplicaSets(namespace).Get(ctx, ref.Name, metav1.GetOptions{})
			if err == nil {
				for _, rsRef := range rs.OwnerReferences {
					if rsRef.Kind == "Deployment" {
						deployName = rsRef.Name
					}
				}
			}
		}
	}

	if deployName == "" {
		return info
	}

	hpas, err := client.AutoscalingV1().HorizontalPodAutoscalers(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return info
	}

	for _, hpa := range hpas.Items {
		if hpa.Spec.ScaleTargetRef.Kind == "Deployment" && hpa.Spec.ScaleTargetRef.Name == deployName {
			info = append(info, fmt.Sprintf("HPA '%s' targets this deployment. Min: %d, Max: %d, Current: %d.", hpa.Name, *hpa.Spec.MinReplicas, hpa.Spec.MaxReplicas, hpa.Status.CurrentReplicas))
			if hpa.Status.CurrentReplicas >= hpa.Spec.MaxReplicas {
				info = append(info, "WARNING: HPA is at MAX replicas. Scaling might be capped.")
			}
		}
	}
	return info
}

func checkStorage(ctx context.Context, client kubernetes.Interface, namespace string, pod *corev1.Pod) []string {
	var info []string
	for _, vol := range pod.Spec.Volumes {
		if vol.PersistentVolumeClaim != nil {
			pvc, err := client.CoreV1().PersistentVolumeClaims(namespace).Get(ctx, vol.PersistentVolumeClaim.ClaimName, metav1.GetOptions{})
			if err != nil {
				continue
			}
			status := pvc.Status.Phase
			info = append(info, fmt.Sprintf("PVC '%s' is %s.", pvc.Name, status))
			if status != corev1.ClaimBound {
				info = append(info, fmt.Sprintf("CRITICAL: PVC '%s' is NOT Bound.", pvc.Name))
			}
			// Capacity check requires metrics, but we can list capacity
			if cap, ok := pvc.Status.Capacity[corev1.ResourceStorage]; ok {
				info = append(info, fmt.Sprintf("PVC Capacity: %s", cap.String()))
			}
		}
	}
	return info
}

func checkScheduling(pod *corev1.Pod, node *corev1.Node) []string {
	var info []string
	if pod.Status.Phase == corev1.PodPending {
		info = append(info, "Pod is PENDING.")
		// Check Tolerations vs Node Taints (if node is known, but usually pending means no node)
		// If Node is nil, we can't check specific node taints, but we can list general issues.
	}
	if node != nil {
		for _, taint := range node.Spec.Taints {
			tolerated := false
			for _, tol := range pod.Spec.Tolerations {
				if tol.ToleratesTaint(&taint) {
					tolerated = true
					break
				}
			}
			if !tolerated {
				info = append(info, fmt.Sprintf("WARNING: Node has taint '%s=%s:%s' which is NOT tolerated by pod.", taint.Key, taint.Value, taint.Effect))
			}
		}
	}
	return info
}

func checkLifecycle(pod *corev1.Pod) []string {
	var info []string
	for _, c := range pod.Spec.Containers {
		if c.Lifecycle != nil {
			if c.Lifecycle.PostStart != nil {
				info = append(info, fmt.Sprintf("Container '%s' has PostStart hook.", c.Name))
			}
			if c.Lifecycle.PreStop != nil {
				info = append(info, fmt.Sprintf("Container '%s' has PreStop hook.", c.Name))
			}
		}
	}
	return info
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
