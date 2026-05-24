package k8sfacts

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	APIVersion = "torque.dev/ops/facts/v1alpha1"
	Kind       = "K8sFactSnapshot"
)

type CollectRequest struct {
	TargetID        string
	TargetDigest    string
	ObservedAt      time.Time
	Namespaces      []string
	AllNamespaces   bool
	EventLimit      int
	WorkloadSamples int
}

type Snapshot struct {
	APIVersion   string          `json:"apiVersion"`
	Kind         string          `json:"kind"`
	TargetID     string          `json:"targetId"`
	TargetDigest string          `json:"targetDigest"`
	ObservedAt   string          `json:"observedAt"`
	Digest       string          `json:"digest"`
	Cluster      ClusterFacts    `json:"cluster"`
	Namespaces   NamespaceFacts  `json:"namespaces"`
	Nodes        NodeFacts       `json:"nodes"`
	Workloads    WorkloadFacts   `json:"workloads"`
	Events       EventFacts      `json:"events"`
	APICalls     []APICallRecord `json:"apiCalls,omitempty"`
}

type ClusterFacts struct {
	GitVersion string `json:"gitVersion,omitempty"`
	Major      string `json:"major,omitempty"`
	Minor      string `json:"minor,omitempty"`
	Platform   string `json:"platform,omitempty"`
}

type NamespaceFacts struct {
	Count    int             `json:"count"`
	Selected []NamespaceInfo `json:"selected,omitempty"`
	Sample   []NamespaceInfo `json:"sample,omitempty"`
}

type NamespaceInfo struct {
	Name   string `json:"name"`
	Status string `json:"status,omitempty"`
}

type NodeFacts struct {
	Count       int            `json:"count"`
	ReadyCount  int            `json:"readyCount"`
	Sample      []NodeInfo     `json:"sample,omitempty"`
	Kubelets    map[string]int `json:"kubelets,omitempty"`
	OSImages    map[string]int `json:"osImages,omitempty"`
	Arch        map[string]int `json:"arch,omitempty"`
	Unavailable []string       `json:"unavailable,omitempty"`
}

type NodeInfo struct {
	Name       string `json:"name"`
	Ready      bool   `json:"ready"`
	Kubelet    string `json:"kubelet,omitempty"`
	OSImage    string `json:"osImage,omitempty"`
	Kernel     string `json:"kernel,omitempty"`
	Arch       string `json:"arch,omitempty"`
	ProviderID string `json:"providerId,omitempty"`
}

type WorkloadFacts struct {
	Namespaces   []string       `json:"namespaces"`
	Deployments  WorkloadCount  `json:"deployments"`
	StatefulSets WorkloadCount  `json:"statefulSets"`
	DaemonSets   WorkloadCount  `json:"daemonSets"`
	Jobs         WorkloadCount  `json:"jobs"`
	CronJobs     WorkloadCount  `json:"cronJobs"`
	Pods         PodCount       `json:"pods"`
	Sample       []WorkloadInfo `json:"sample,omitempty"`
}

type WorkloadCount struct {
	Count           int `json:"count"`
	Ready           int `json:"ready,omitempty"`
	Unavailable     int `json:"unavailable,omitempty"`
	DesiredReplicas int `json:"desiredReplicas,omitempty"`
	ReadyReplicas   int `json:"readyReplicas,omitempty"`
}

type PodCount struct {
	Count     int            `json:"count"`
	ByPhase   map[string]int `json:"byPhase,omitempty"`
	ReadyPods int            `json:"readyPods"`
}

type WorkloadInfo struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Ready     string `json:"ready,omitempty"`
	Status    string `json:"status,omitempty"`
}

type EventFacts struct {
	Namespaces   []string    `json:"namespaces"`
	Count        int         `json:"count"`
	WarningCount int         `json:"warningCount"`
	Sample       []EventInfo `json:"sample,omitempty"`
}

type EventInfo struct {
	Namespace string `json:"namespace"`
	Type      string `json:"type,omitempty"`
	Reason    string `json:"reason,omitempty"`
	Object    string `json:"object,omitempty"`
	Message   string `json:"message,omitempty"`
}

type APICallRecord struct {
	Resource  string `json:"resource"`
	Namespace string `json:"namespace,omitempty"`
	Status    string `json:"status"`
	Error     string `json:"error,omitempty"`
}

func Collect(ctx context.Context, client kubernetes.Interface, req CollectRequest) (*Snapshot, error) {
	if client == nil {
		return nil, fmt.Errorf("kubernetes client is required")
	}
	targetID := strings.TrimSpace(req.TargetID)
	if targetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	observedAt := req.ObservedAt.UTC()
	if observedAt.IsZero() {
		observedAt = time.Now().UTC()
	}
	eventLimit := req.EventLimit
	if eventLimit <= 0 {
		eventLimit = 25
	}
	workloadSamples := req.WorkloadSamples
	if workloadSamples <= 0 {
		workloadSamples = 25
	}

	snapshot := &Snapshot{
		APIVersion:   APIVersion,
		Kind:         Kind,
		TargetID:     targetID,
		TargetDigest: strings.TrimSpace(req.TargetDigest),
		ObservedAt:   observedAt.Format(time.RFC3339),
	}

	if version, err := client.Discovery().ServerVersion(); err == nil && version != nil {
		snapshot.Cluster = ClusterFacts{
			GitVersion: version.GitVersion,
			Major:      version.Major,
			Minor:      version.Minor,
			Platform:   version.Platform,
		}
		snapshot.APICalls = append(snapshot.APICalls, APICallRecord{Resource: "serverVersion", Status: "succeeded"})
	} else if err != nil {
		snapshot.APICalls = append(snapshot.APICalls, APICallRecord{Resource: "serverVersion", Status: "failed", Error: err.Error()})
	}

	namespaces, selected, err := collectNamespaces(ctx, client, req)
	if err != nil {
		return nil, err
	}
	snapshot.Namespaces = namespaces
	snapshot.Workloads.Namespaces = selected
	snapshot.Events.Namespaces = selected

	nodes, nodeCall := collectNodes(ctx, client)
	snapshot.Nodes = nodes
	snapshot.APICalls = append(snapshot.APICalls, nodeCall)

	workloads, workloadCalls := collectWorkloads(ctx, client, selected, workloadSamples)
	snapshot.Workloads = workloads
	snapshot.APICalls = append(snapshot.APICalls, workloadCalls...)

	events, eventCalls := collectEvents(ctx, client, selected, eventLimit)
	snapshot.Events = events
	snapshot.APICalls = append(snapshot.APICalls, eventCalls...)

	sortAPICalls(snapshot.APICalls)
	snapshot.Digest = snapshot.StableDigest()
	return snapshot, nil
}

func (s Snapshot) StableDigest() string {
	doc := struct {
		TargetID     string         `json:"targetId"`
		TargetDigest string         `json:"targetDigest"`
		Cluster      ClusterFacts   `json:"cluster"`
		Namespaces   NamespaceFacts `json:"namespaces"`
		Nodes        NodeFacts      `json:"nodes"`
		Workloads    WorkloadFacts  `json:"workloads"`
		Events       EventFacts     `json:"events"`
	}{
		TargetID:     s.TargetID,
		TargetDigest: s.TargetDigest,
		Cluster:      s.Cluster,
		Namespaces:   s.Namespaces,
		Nodes:        s.Nodes,
		Workloads:    s.Workloads,
		Events:       s.Events,
	}
	raw, _ := json.Marshal(doc)
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func TargetDigest(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func collectNamespaces(ctx context.Context, client kubernetes.Interface, req CollectRequest) (NamespaceFacts, []string, error) {
	list, err := client.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return NamespaceFacts{}, nil, fmt.Errorf("list namespaces: %w", err)
	}
	all := make([]NamespaceInfo, 0, len(list.Items))
	byName := map[string]NamespaceInfo{}
	for _, ns := range list.Items {
		info := NamespaceInfo{Name: ns.Name, Status: string(ns.Status.Phase)}
		all = append(all, info)
		byName[ns.Name] = info
	}
	sort.Slice(all, func(i, j int) bool { return all[i].Name < all[j].Name })

	selectedNames := normalizeNamespaces(req.Namespaces)
	if req.AllNamespaces {
		selectedNames = make([]string, 0, len(all))
		for _, ns := range all {
			selectedNames = append(selectedNames, ns.Name)
		}
	} else if len(selectedNames) == 0 && len(all) > 0 {
		selectedNames = []string{all[0].Name}
	}

	selected := make([]NamespaceInfo, 0, len(selectedNames))
	for _, name := range selectedNames {
		info, ok := byName[name]
		if !ok {
			return NamespaceFacts{}, nil, fmt.Errorf("namespace %q not found", name)
		}
		selected = append(selected, info)
	}
	sample := all
	if len(sample) > 50 {
		sample = sample[:50]
	}
	return NamespaceFacts{
		Count:    len(all),
		Selected: selected,
		Sample:   sample,
	}, selectedNames, nil
}

func collectNodes(ctx context.Context, client kubernetes.Interface) (NodeFacts, APICallRecord) {
	list, err := client.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
	if err != nil {
		return NodeFacts{}, APICallRecord{Resource: "nodes", Status: "failed", Error: err.Error()}
	}
	facts := NodeFacts{
		Count:    len(list.Items),
		Kubelets: map[string]int{},
		OSImages: map[string]int{},
		Arch:     map[string]int{},
	}
	for _, node := range list.Items {
		ready := nodeReady(node)
		if ready {
			facts.ReadyCount++
		} else {
			facts.Unavailable = append(facts.Unavailable, node.Name)
		}
		facts.Kubelets[node.Status.NodeInfo.KubeletVersion]++
		facts.OSImages[node.Status.NodeInfo.OSImage]++
		facts.Arch[node.Status.NodeInfo.Architecture]++
		facts.Sample = append(facts.Sample, NodeInfo{
			Name:       node.Name,
			Ready:      ready,
			Kubelet:    node.Status.NodeInfo.KubeletVersion,
			OSImage:    node.Status.NodeInfo.OSImage,
			Kernel:     node.Status.NodeInfo.KernelVersion,
			Arch:       node.Status.NodeInfo.Architecture,
			ProviderID: node.Spec.ProviderID,
		})
	}
	sort.Strings(facts.Unavailable)
	sort.Slice(facts.Sample, func(i, j int) bool { return facts.Sample[i].Name < facts.Sample[j].Name })
	if len(facts.Sample) > 50 {
		facts.Sample = facts.Sample[:50]
	}
	return facts, APICallRecord{Resource: "nodes", Status: "succeeded"}
}

func collectWorkloads(ctx context.Context, client kubernetes.Interface, namespaces []string, sampleLimit int) (WorkloadFacts, []APICallRecord) {
	facts := WorkloadFacts{Namespaces: append([]string(nil), namespaces...), Pods: PodCount{ByPhase: map[string]int{}}}
	var calls []APICallRecord
	for _, ns := range namespaces {
		if list, err := client.AppsV1().Deployments(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "deployments", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.Deployments.Count++
				facts.Deployments.DesiredReplicas += int(deploymentDesired(item))
				facts.Deployments.ReadyReplicas += int(item.Status.ReadyReplicas)
				if item.Status.AvailableReplicas >= deploymentDesired(item) {
					facts.Deployments.Ready++
				} else {
					facts.Deployments.Unavailable++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "Deployment", ns, item.Name, fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, deploymentDesired(item)), "")
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "deployments", Namespace: ns, Status: "failed", Error: err.Error()})
		}
		if list, err := client.AppsV1().StatefulSets(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "statefulSets", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.StatefulSets.Count++
				facts.StatefulSets.DesiredReplicas += int(statefulSetDesired(item))
				facts.StatefulSets.ReadyReplicas += int(item.Status.ReadyReplicas)
				if item.Status.ReadyReplicas >= statefulSetDesired(item) {
					facts.StatefulSets.Ready++
				} else {
					facts.StatefulSets.Unavailable++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "StatefulSet", ns, item.Name, fmt.Sprintf("%d/%d", item.Status.ReadyReplicas, statefulSetDesired(item)), "")
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "statefulSets", Namespace: ns, Status: "failed", Error: err.Error()})
		}
		if list, err := client.AppsV1().DaemonSets(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "daemonSets", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.DaemonSets.Count++
				facts.DaemonSets.DesiredReplicas += int(item.Status.DesiredNumberScheduled)
				facts.DaemonSets.ReadyReplicas += int(item.Status.NumberReady)
				if item.Status.NumberUnavailable == 0 {
					facts.DaemonSets.Ready++
				} else {
					facts.DaemonSets.Unavailable++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "DaemonSet", ns, item.Name, fmt.Sprintf("%d/%d", item.Status.NumberReady, item.Status.DesiredNumberScheduled), "")
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "daemonSets", Namespace: ns, Status: "failed", Error: err.Error()})
		}
		if list, err := client.BatchV1().Jobs(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "jobs", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.Jobs.Count++
				status := "active"
				if item.Status.Succeeded > 0 {
					status = "succeeded"
					facts.Jobs.Ready++
				} else if item.Status.Failed > 0 {
					status = "failed"
					facts.Jobs.Unavailable++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "Job", ns, item.Name, "", status)
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "jobs", Namespace: ns, Status: "failed", Error: err.Error()})
		}
		if list, err := client.BatchV1().CronJobs(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "cronJobs", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.CronJobs.Count++
				if item.Spec.Suspend != nil && *item.Spec.Suspend {
					facts.CronJobs.Unavailable++
				} else {
					facts.CronJobs.Ready++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "CronJob", ns, item.Name, "", cronJobStatus(item))
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "cronJobs", Namespace: ns, Status: "failed", Error: err.Error()})
		}
		if list, err := client.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{}); err == nil {
			calls = append(calls, APICallRecord{Resource: "pods", Namespace: ns, Status: "succeeded"})
			for _, item := range list.Items {
				facts.Pods.Count++
				facts.Pods.ByPhase[string(item.Status.Phase)]++
				if podReady(item) {
					facts.Pods.ReadyPods++
				}
				appendWorkloadSample(&facts.Sample, sampleLimit, "Pod", ns, item.Name, "", string(item.Status.Phase))
			}
		} else {
			calls = append(calls, APICallRecord{Resource: "pods", Namespace: ns, Status: "failed", Error: err.Error()})
		}
	}
	sort.Strings(facts.Namespaces)
	sort.Slice(facts.Sample, func(i, j int) bool {
		left := facts.Sample[i].Namespace + "/" + facts.Sample[i].Kind + "/" + facts.Sample[i].Name
		right := facts.Sample[j].Namespace + "/" + facts.Sample[j].Kind + "/" + facts.Sample[j].Name
		return left < right
	})
	return facts, calls
}

func collectEvents(ctx context.Context, client kubernetes.Interface, namespaces []string, limit int) (EventFacts, []APICallRecord) {
	facts := EventFacts{Namespaces: append([]string(nil), namespaces...)}
	var calls []APICallRecord
	for _, ns := range namespaces {
		list, err := client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			calls = append(calls, APICallRecord{Resource: "events", Namespace: ns, Status: "failed", Error: err.Error()})
			continue
		}
		calls = append(calls, APICallRecord{Resource: "events", Namespace: ns, Status: "succeeded"})
		for _, event := range list.Items {
			facts.Count++
			if strings.EqualFold(event.Type, corev1.EventTypeWarning) {
				facts.WarningCount++
			}
			facts.Sample = append(facts.Sample, EventInfo{
				Namespace: ns,
				Type:      event.Type,
				Reason:    event.Reason,
				Object:    event.InvolvedObject.Kind + "/" + event.InvolvedObject.Name,
				Message:   truncate(event.Message, 180),
			})
		}
	}
	sort.Strings(facts.Namespaces)
	sort.Slice(facts.Sample, func(i, j int) bool {
		left := facts.Sample[i].Namespace + "/" + facts.Sample[i].Object + "/" + facts.Sample[i].Reason
		right := facts.Sample[j].Namespace + "/" + facts.Sample[j].Object + "/" + facts.Sample[j].Reason
		return left < right
	})
	if len(facts.Sample) > limit {
		facts.Sample = facts.Sample[:limit]
	}
	return facts, calls
}

func normalizeNamespaces(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func nodeReady(node corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func podReady(pod corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func deploymentDesired(item appsv1.Deployment) int32 {
	if item.Spec.Replicas == nil {
		return 1
	}
	return *item.Spec.Replicas
}

func statefulSetDesired(item appsv1.StatefulSet) int32 {
	if item.Spec.Replicas == nil {
		return 1
	}
	return *item.Spec.Replicas
}

func cronJobStatus(item batchv1.CronJob) string {
	if item.Spec.Suspend != nil && *item.Spec.Suspend {
		return "suspended"
	}
	return "active"
}

func appendWorkloadSample(sample *[]WorkloadInfo, limit int, kind, namespace, name, ready, status string) {
	if len(*sample) >= limit {
		return
	}
	*sample = append(*sample, WorkloadInfo{
		Kind:      kind,
		Namespace: namespace,
		Name:      name,
		Ready:     ready,
		Status:    status,
	})
}

func sortAPICalls(calls []APICallRecord) {
	sort.Slice(calls, func(i, j int) bool {
		left := calls[i].Resource + "/" + calls[i].Namespace
		right := calls[j].Resource + "/" + calls[j].Namespace
		return left < right
	})
}

func truncate(value string, max int) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= max {
		return value
	}
	return value[:max] + "..."
}
