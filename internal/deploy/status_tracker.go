package deploy

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	appsv1 "k8s.io/api/apps/v1"
	autoscalingv2 "k8s.io/api/autoscaling/v2"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	meta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
)

// StatusUpdateFunc consumes resource status snapshots.
type StatusUpdateFunc func([]ResourceStatus)

// ResourceTracker periodically lists release resources and reports their status.
type ResourceTracker struct {
	client           *kube.Client
	releaseName      string
	defaultNamespace string
	interval         time.Duration
	updateFn         StatusUpdateFunc
	targets          []resourceTarget
	namespaces       []string
}

// NewResourceTracker constructs a tracker for the given release.
func NewResourceTracker(client *kube.Client, namespace, release, manifest string, update StatusUpdateFunc) *ResourceTracker {
	targets := targetsFromManifest(manifest)
	nsSet := map[string]struct{}{}
	for _, target := range targets {
		if ns := strings.TrimSpace(target.Namespace); ns != "" {
			nsSet[ns] = struct{}{}
		}
	}
	if len(nsSet) == 0 && strings.TrimSpace(namespace) != "" {
		nsSet[strings.TrimSpace(namespace)] = struct{}{}
	}
	nsList := make([]string, 0, len(nsSet))
	for ns := range nsSet {
		nsList = append(nsList, ns)
	}
	sort.Strings(nsList)
	return &ResourceTracker{
		client:           client,
		releaseName:      strings.TrimSpace(release),
		defaultNamespace: strings.TrimSpace(namespace),
		interval:         2 * time.Second,
		updateFn:         update,
		targets:          targets,
		namespaces:       nsList,
	}
}

// WithInterval overrides the polling interval.
func (t *ResourceTracker) WithInterval(interval time.Duration) *ResourceTracker {
	t.interval = interval
	return t
}

// Run starts the tracker loop until the context is canceled.
func (t *ResourceTracker) Run(ctx context.Context) {
	if t.updateFn == nil || strings.TrimSpace(t.releaseName) == "" {
		return
	}
	ticker := time.NewTicker(t.interval)
	defer ticker.Stop()
	t.updateFn(nil)
	for {
		select {
		case <-ctx.Done():
			t.updateFn(nil)
			return
		case <-ticker.C:
			t.updateFn(t.collect(ctx))
		}
	}
}

func (t *ResourceTracker) collect(ctx context.Context) []ResourceStatus {
	seen := make(map[string]struct{})
	rows := make([]ResourceStatus, 0, len(t.targets)+8)
	if len(t.targets) > 0 && t.client != nil && t.client.Dynamic != nil && t.client.RESTMapper != nil {
		rows = append(rows, t.collectFromTargets(ctx, seen)...)
	}
	if len(rows) == 0 {
		rows = append(rows, t.collectByLabel(ctx, seen)...)
	} else {
		rows = append(rows, t.collectDependents(ctx, seen)...)
	}
	sort.Slice(rows, func(i, j int) bool {
		return sortKey(rows[i]) < sortKey(rows[j])
	})
	return rows
}

func (t *ResourceTracker) collectFromTargets(ctx context.Context, seen map[string]struct{}) []ResourceStatus {
	rows := make([]ResourceStatus, 0, len(t.targets))
	for _, target := range t.targets {
		status := t.statusForTarget(ctx, target)
		if status == nil {
			continue
		}
		t.appendIfNew(&rows, seen, *status)
	}
	return rows
}

func (t *ResourceTracker) statusForTarget(ctx context.Context, target resourceTarget) *ResourceStatus {
	mapper := t.client.RESTMapper
	dyn := t.client.Dynamic
	if mapper == nil || dyn == nil {
		return nil
	}
	if strings.TrimSpace(target.Name) == "" || strings.TrimSpace(target.Kind) == "" {
		return nil
	}
	gvk := schemaFromTarget(target)
	mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		meta := metav1.ObjectMeta{Name: target.Name, Namespace: target.Namespace}
		rs := genericStatus(target.Kind, meta, fmt.Sprintf("REST mapping unavailable: %v", err))
		return &rs
	}
	resource := dyn.Resource(mapping.Resource)
	ns := target.Namespace
	if ns == "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns = t.effectiveNamespace()
	}
	if ns == "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		ns = metav1.NamespaceDefault
	}
	var obj *unstructured.Unstructured
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		obj, err = resource.Namespace(ns).Get(ctx, target.Name, metav1.GetOptions{})
	} else {
		obj, err = resource.Get(ctx, target.Name, metav1.GetOptions{})
	}
	if err != nil {
		meta := metav1.ObjectMeta{Name: target.Name, Namespace: ns}
		switch {
		case apierrors.IsNotFound(err):
			rs := genericStatus(target.Kind, meta, "Not yet created")
			return &rs
		case apierrors.IsForbidden(err):
			rs := genericStatus(target.Kind, meta, "Access forbidden")
			rs.Status = "Unknown"
			rs.Message = err.Error()
			return &rs
		default:
			rs := genericStatus(target.Kind, meta, err.Error())
			rs.Status = "Unknown"
			return &rs
		}
	}
	return statusFromUnstructured(obj)
}

func (t *ResourceTracker) collectByLabel(ctx context.Context, seen map[string]struct{}) []ResourceStatus {
	clientset := t.typedClient()
	if clientset == nil || t.releaseName == "" {
		return nil
	}
	namespaces := t.trackedNamespaces()
	selector := t.selector()
	if selector == "" {
		return nil
	}
	opts := metav1.ListOptions{LabelSelector: selector}
	var rows []ResourceStatus
	for _, ns := range namespaces {
		if depList, err := clientset.AppsV1().Deployments(ns).List(ctx, opts); err == nil {
			for i := range depList.Items {
				t.appendIfNew(&rows, seen, deploymentStatus(&depList.Items[i]))
			}
		}
		if stsList, err := clientset.AppsV1().StatefulSets(ns).List(ctx, opts); err == nil {
			for i := range stsList.Items {
				t.appendIfNew(&rows, seen, statefulSetStatus(&stsList.Items[i]))
			}
		}
		if dsList, err := clientset.AppsV1().DaemonSets(ns).List(ctx, opts); err == nil {
			for i := range dsList.Items {
				t.appendIfNew(&rows, seen, daemonSetStatus(&dsList.Items[i]))
			}
		}
		if jobList, err := clientset.BatchV1().Jobs(ns).List(ctx, opts); err == nil {
			for i := range jobList.Items {
				t.appendIfNew(&rows, seen, jobStatus(&jobList.Items[i]))
			}
		}
		if cjList, err := clientset.BatchV1().CronJobs(ns).List(ctx, opts); err == nil {
			for i := range cjList.Items {
				t.appendIfNew(&rows, seen, cronJobStatus(&cjList.Items[i]))
			}
		}
	}
	return rows
}

func (t *ResourceTracker) collectDependents(ctx context.Context, seen map[string]struct{}) []ResourceStatus {
	clientset := t.typedClient()
	if clientset == nil || t.releaseName == "" {
		return nil
	}
	selector := t.selector()
	if selector == "" {
		return nil
	}
	opts := metav1.ListOptions{LabelSelector: selector}
	namespaces := t.trackedNamespaces()
	var rows []ResourceStatus
	for _, ns := range namespaces {
		if podList, err := clientset.CoreV1().Pods(ns).List(ctx, opts); err == nil {
			for i := range podList.Items {
				t.appendIfNew(&rows, seen, podStatus(&podList.Items[i]))
			}
		}
		if pdbList, err := clientset.PolicyV1().PodDisruptionBudgets(ns).List(ctx, opts); err == nil {
			for i := range pdbList.Items {
				t.appendIfNew(&rows, seen, pdbStatus(&pdbList.Items[i]))
			}
		}
		if hpaList, err := clientset.AutoscalingV2().HorizontalPodAutoscalers(ns).List(ctx, opts); err == nil {
			for i := range hpaList.Items {
				t.appendIfNew(&rows, seen, hpaStatus(&hpaList.Items[i]))
			}
		}
	}
	return rows
}

func (t *ResourceTracker) appendIfNew(rows *[]ResourceStatus, seen map[string]struct{}, rs ResourceStatus) {
	key := sortKey(rs)
	if _, ok := seen[key]; ok {
		return
	}
	seen[key] = struct{}{}
	*rows = append(*rows, normalizeStatus(rs))
}

func (t *ResourceTracker) selector() string {
	if strings.TrimSpace(t.releaseName) == "" {
		return ""
	}
	return labels.Set(map[string]string{
		"app.kubernetes.io/instance": t.releaseName,
	}).AsSelector().String()
}

func (t *ResourceTracker) effectiveNamespace() string {
	if t.defaultNamespace != "" {
		return t.defaultNamespace
	}
	if t.client != nil && t.client.Namespace != "" {
		return t.client.Namespace
	}
	return metav1.NamespaceDefault
}

func (t *ResourceTracker) trackedNamespaces() []string {
	if len(t.namespaces) > 0 {
		return t.namespaces
	}
	return []string{t.effectiveNamespace()}
}

func (t *ResourceTracker) typedClient() kubernetes.Interface {
	if t.client == nil {
		return nil
	}
	return t.client.Clientset
}

func statusFromUnstructured(obj *unstructured.Unstructured) *ResourceStatus {
	if obj == nil {
		return nil
	}
	switch strings.ToLower(obj.GetKind()) {
	case "deployment":
		var dep appsv1.Deployment
		if convert(obj, &dep) == nil {
			rs := deploymentStatus(&dep)
			return &rs
		}
	case "statefulset":
		var sts appsv1.StatefulSet
		if convert(obj, &sts) == nil {
			rs := statefulSetStatus(&sts)
			return &rs
		}
	case "daemonset":
		var ds appsv1.DaemonSet
		if convert(obj, &ds) == nil {
			rs := daemonSetStatus(&ds)
			return &rs
		}
	case "job":
		var job batchv1.Job
		if convert(obj, &job) == nil {
			rs := jobStatus(&job)
			return &rs
		}
	case "cronjob":
		var cj batchv1.CronJob
		if convert(obj, &cj) == nil {
			rs := cronJobStatus(&cj)
			return &rs
		}
	case "pod":
		var pod corev1.Pod
		if convert(obj, &pod) == nil {
			rs := podStatus(&pod)
			return &rs
		}
	case "poddisruptionbudget":
		var pdb policyv1.PodDisruptionBudget
		if convert(obj, &pdb) == nil {
			rs := pdbStatus(&pdb)
			return &rs
		}
	case "horizontalpodautoscaler":
		var hpa autoscalingv2.HorizontalPodAutoscaler
		if convert(obj, &hpa) == nil {
			rs := hpaStatus(&hpa)
			return &rs
		}
	}
	meta := metav1.ObjectMeta{
		Name:      obj.GetName(),
		Namespace: obj.GetNamespace(),
	}
	rs := genericStatus(obj.GetKind(), meta, "")
	rs.Status = "Ready"
	return &rs
}

func convert(obj *unstructured.Unstructured, out interface{}) error {
	if obj == nil {
		return fmt.Errorf("object is nil")
	}
	return runtime.DefaultUnstructuredConverter.FromUnstructured(obj.Object, out)
}
func schemaFromTarget(target resourceTarget) schema.GroupVersionKind {
	return schema.GroupVersionKind{Group: target.Group, Version: target.Version, Kind: target.Kind}
}
