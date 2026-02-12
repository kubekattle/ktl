// File: cmd/ktl/logs_deploy_lens.go
// Brief: Live Deployment Lens for 'ktl logs deploy/<name>'.

package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kubekattle/ktl/internal/config"
	"github.com/kubekattle/ktl/internal/tailer"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

type deployMode int

const (
	deployModeActive deployMode = iota
	deployModeStable
	deployModeCanary
	deployModeStableCanary
)

func parseDeployLogsTarget(arg string) (kind string, name string, ok bool) {
	raw := strings.TrimSpace(arg)
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return "", "", false
	}
	parts := strings.SplitN(raw, "/", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	k := strings.ToLower(strings.TrimSpace(parts[0]))
	n := strings.TrimSpace(parts[1])
	switch k {
	case "deploy", "deployment":
		if n == "" {
			return "", "", false
		}
		return k, n, true
	default:
		return "", "", false
	}
}

func prepareDeployLogsLens(ctx context.Context, stderr io.Writer, client kubernetes.Interface, opts *config.Options, kind, name, pinRaw string, modeRaw string, refresh time.Duration, pruneGrace time.Duration) (tailer.Option, func(*tailer.Tailer) error, error) {
	if client == nil || opts == nil {
		return nil, nil, fmt.Errorf("deploy logs lens: missing dependencies")
	}
	if opts.AllNamespaces {
		return nil, nil, fmt.Errorf("deploy logs lens does not support --all-namespaces")
	}
	if len(opts.Namespaces) != 1 || strings.TrimSpace(opts.Namespaces[0]) == "" {
		return nil, nil, fmt.Errorf("deploy logs lens requires exactly one namespace (use -n)")
	}
	if strings.TrimSpace(opts.LabelSelector) != "" {
		return nil, nil, fmt.Errorf("deploy logs lens does not support --selector (it derives the selector from the deployment)")
	}
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	mode, err := parseDeployMode(modeRaw, pinRaw)
	if err != nil {
		return nil, nil, err
	}
	if pruneGrace < 0 {
		pruneGrace = 0
	}

	ns := strings.TrimSpace(opts.Namespaces[0])
	deploy, selector, err := getDeploymentAndSelector(ctx, client, ns, name)
	if err != nil {
		return nil, nil, err
	}

	opts.PodQuery = ".*"
	opts.LabelSelector = selector.String()

	var (
		mu             sync.RWMutex
		allowed        = map[string]string{} // ns/pod -> role
		grace          = map[string]time.Time{}
		selectorLabels = selector
	)

	update := func(reason string, t *tailer.Tailer) error {
		pods, err := computeDeploymentLens(ctx, client, deploy, selectorLabels, mode)
		if err != nil {
			return err
		}
		reconcile(reason, t, pods, pruneGrace, &mu, &allowed, &grace)
		return nil
	}

	if err := update("deploy_initial", nil); err != nil {
		return nil, nil, err
	}
	fmt.Fprintf(stderr, "Deployment lens: %s/%s (selector %q)\n", ns, deploy.Name, opts.LabelSelector)

	podFilter := func(pod *corev1.Pod) bool {
		if pod == nil {
			return false
		}
		mu.RLock()
		_, ok := allowed[pod.Namespace+"/"+pod.Name]
		if !ok {
			if until, ok := grace[pod.Namespace+"/"+pod.Name]; ok && time.Now().Before(until) {
				mu.RUnlock()
				return true
			}
		}
		mu.RUnlock()
		return ok
	}

	start := func(t *tailer.Tailer) error {
		if t == nil {
			return fmt.Errorf("deploy logs lens: tailer unavailable")
		}
		if err := update("deploy_start", t); err != nil {
			return err
		}
		go runDeployLensInformers(ctx, client, deploy.Namespace, deploy.Name, selectorLabels.String(), refresh, func(reason string, deployObj *appsv1.Deployment, rsObjs []*appsv1.ReplicaSet, podObjs []*corev1.Pod) {
			if deployObj == nil {
				return
			}
			selected := computeDeploymentLensFromObjects(deployObj, rsObjs, podObjs, mode)
			reconcile(reason, t, selected, pruneGrace, &mu, &allowed, &grace)
		})
		return nil
	}

	return tailer.WithPodFilter(podFilter), start, nil
}

func reconcile(reason string, t *tailer.Tailer, selected map[string]string, pruneGrace time.Duration, mu *sync.RWMutex, allowed *map[string]string, grace *map[string]time.Time) {
	if selected == nil {
		selected = map[string]string{}
	}
	now := time.Now()
	var removed []string
	var expired []string

	mu.Lock()
	prev := *allowed
	next := selected
	*allowed = next
	for k := range prev {
		if _, ok := next[k]; ok {
			continue
		}
		if pruneGrace > 0 {
			(*grace)[k] = now.Add(pruneGrace)
		} else {
			removed = append(removed, k)
		}
	}
	for k, until := range *grace {
		if now.Before(until) {
			continue
		}
		if _, ok := next[k]; ok {
			delete(*grace, k)
			continue
		}
		expired = append(expired, k)
		delete(*grace, k)
	}
	mu.Unlock()

	if t == nil {
		return
	}
	for key, role := range selected {
		nsPod := strings.SplitN(key, "/", 2)
		if len(nsPod) != 2 {
			continue
		}
		podName := nsPod[1]
		display := fmt.Sprintf("[%s] %s", role, podName)
		t.SetPodDisplayOverride(nsPod[0], podName, display)
	}
	for _, key := range removed {
		nsPod := strings.SplitN(key, "/", 2)
		if len(nsPod) != 2 {
			continue
		}
		t.SetPodDisplayOverride(nsPod[0], nsPod[1], "")
	}
	for _, key := range expired {
		nsPod := strings.SplitN(key, "/", 2)
		if len(nsPod) != 2 {
			continue
		}
		t.SetPodDisplayOverride(nsPod[0], nsPod[1], "")
	}
	t.PruneTails(func(namespace, pod, container string) bool {
		mu.RLock()
		_, ok := (*allowed)[namespace+"/"+pod]
		if !ok {
			if until, ok := (*grace)[namespace+"/"+pod]; ok && time.Now().Before(until) {
				mu.RUnlock()
				return true
			}
		}
		mu.RUnlock()
		return ok
	}, reason)
}

func runDeployLensInformers(ctx context.Context, client kubernetes.Interface, namespace, deploymentName, selector string, refresh time.Duration, onChange func(reason string, deployObj *appsv1.Deployment, rsObjs []*appsv1.ReplicaSet, podObjs []*corev1.Pod)) {
	if client == nil || strings.TrimSpace(namespace) == "" || strings.TrimSpace(deploymentName) == "" || onChange == nil {
		return
	}
	if refresh <= 0 {
		refresh = 2 * time.Second
	}
	deployLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.FieldSelector = "metadata.name=" + deploymentName
			return client.AppsV1().Deployments(namespace).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.FieldSelector = "metadata.name=" + deploymentName
			return client.AppsV1().Deployments(namespace).Watch(context.Background(), options)
		},
	}
	rsLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector
			return client.AppsV1().ReplicaSets(namespace).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector
			return client.AppsV1().ReplicaSets(namespace).Watch(context.Background(), options)
		},
	}
	podLW := &cache.ListWatch{
		ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
			options.LabelSelector = selector
			return client.CoreV1().Pods(namespace).List(context.Background(), options)
		},
		WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
			options.LabelSelector = selector
			return client.CoreV1().Pods(namespace).Watch(context.Background(), options)
		},
	}

	deployInf := cache.NewSharedIndexInformer(deployLW, &appsv1.Deployment{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	rsInf := cache.NewSharedIndexInformer(rsLW, &appsv1.ReplicaSet{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})
	podInf := cache.NewSharedIndexInformer(podLW, &corev1.Pod{}, 0, cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc})

	trigger := make(chan string, 1)
	handler := cache.ResourceEventHandlerFuncs{
		AddFunc: func(obj interface{}) { nonBlockingSignal(trigger, "watch_add") },
		UpdateFunc: func(oldObj, newObj interface{}) {
			nonBlockingSignal(trigger, "watch_update")
		},
		DeleteFunc: func(obj interface{}) { nonBlockingSignal(trigger, "watch_delete") },
	}
	deployInf.AddEventHandler(handler)
	rsInf.AddEventHandler(handler)
	podInf.AddEventHandler(handler)

	go deployInf.Run(ctx.Done())
	go rsInf.Run(ctx.Done())
	go podInf.Run(ctx.Done())

	if !cache.WaitForCacheSync(ctx.Done(), deployInf.HasSynced, rsInf.HasSynced, podInf.HasSynced) {
		return
	}

	reconcileFromStores := func(reason string) {
		var deployObj *appsv1.Deployment
		for _, obj := range deployInf.GetStore().List() {
			if d, ok := obj.(*appsv1.Deployment); ok {
				deployObj = d
				break
			}
		}
		rsObjs := make([]*appsv1.ReplicaSet, 0)
		for _, obj := range rsInf.GetStore().List() {
			if rs, ok := obj.(*appsv1.ReplicaSet); ok {
				rsObjs = append(rsObjs, rs)
			}
		}
		podObjs := make([]*corev1.Pod, 0)
		for _, obj := range podInf.GetStore().List() {
			if p, ok := obj.(*corev1.Pod); ok {
				podObjs = append(podObjs, p)
			}
		}
		onChange(reason, deployObj, rsObjs, podObjs)
	}

	reconcileFromStores("watch_sync")
	ticker := time.NewTicker(refresh)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case reason := <-trigger:
			reconcileFromStores(reason)
		case <-ticker.C:
			reconcileFromStores("watch_refresh")
		}
	}
}

func nonBlockingSignal(ch chan string, reason string) {
	select {
	case ch <- reason:
	default:
	}
}

func parseDeployMode(modeRaw string, pinRaw string) (deployMode, error) {
	mode := strings.ToLower(strings.TrimSpace(modeRaw))
	switch mode {
	case "", "active":
		return deployModeActive, nil
	case "stable":
		return deployModeStable, nil
	case "canary":
		return deployModeCanary, nil
	case "stable+canary", "stable,canary", "canary,stable":
		return deployModeStableCanary, nil
	default:
	}

	// Back-compat: map the deprecated --deploy-pin onto a mode if it was used alone.
	pin := strings.TrimSpace(pinRaw)
	if pin == "" {
		return 0, fmt.Errorf("unknown --deploy-mode value %q (supported: active, stable, canary, stable+canary)", modeRaw)
	}
	parts := map[string]struct{}{}
	for _, p := range strings.Split(pin, ",") {
		parts[strings.ToLower(strings.TrimSpace(p))] = struct{}{}
	}
	_, wantStable := parts["stable"]
	_, wantCanary := parts["canary"]
	switch {
	case wantStable && wantCanary:
		return deployModeStableCanary, nil
	case wantStable:
		return deployModeStable, nil
	case wantCanary:
		return deployModeCanary, nil
	default:
		return 0, fmt.Errorf("unknown --deploy-mode value %q (supported: active, stable, canary, stable+canary)", modeRaw)
	}
}

func getDeploymentAndSelector(ctx context.Context, client kubernetes.Interface, namespace, name string) (*appsv1.Deployment, labels.Selector, error) {
	deploy, err := client.AppsV1().Deployments(namespace).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		if apierrors.IsNotFound(err) {
			return nil, nil, fmt.Errorf("deployment %s/%s not found", namespace, name)
		}
		return nil, nil, err
	}
	if deploy.Spec.Selector == nil {
		return nil, nil, fmt.Errorf("deployment %s/%s has no selector", namespace, name)
	}
	selector, err := metav1.LabelSelectorAsSelector(deploy.Spec.Selector)
	if err != nil {
		return nil, nil, fmt.Errorf("deployment selector invalid: %w", err)
	}
	return deploy, selector, nil
}

func computeDeploymentLens(ctx context.Context, client kubernetes.Interface, deploy *appsv1.Deployment, selector labels.Selector, mode deployMode) (map[string]string, error) {
	rsList, err := client.AppsV1().ReplicaSets(deploy.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("list replicasets: %w", err)
	}
	podList, err := client.CoreV1().Pods(deploy.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return nil, fmt.Errorf("list pods: %w", err)
	}
	rsObjs := make([]*appsv1.ReplicaSet, 0, len(rsList.Items))
	for i := range rsList.Items {
		rsObjs = append(rsObjs, &rsList.Items[i])
	}
	podObjs := make([]*corev1.Pod, 0, len(podList.Items))
	for i := range podList.Items {
		podObjs = append(podObjs, &podList.Items[i])
	}
	return computeDeploymentLensFromObjects(deploy, rsObjs, podObjs, mode), nil
}

func computeDeploymentLensFromObjects(deploy *appsv1.Deployment, replicaSets []*appsv1.ReplicaSet, pods []*corev1.Pod, mode deployMode) map[string]string {
	if deploy == nil {
		return map[string]string{}
	}
	owned := make([]*appsv1.ReplicaSet, 0, len(replicaSets))
	for _, rs := range replicaSets {
		if rs == nil {
			continue
		}
		if !ownedBy(rs.OwnerReferences, "Deployment", deploy.Name, string(deploy.UID)) {
			continue
		}
		owned = append(owned, rs)
	}
	newRS, stableRS := pickDeploymentReplicaSets(deploy, owned)
	activeRS := map[string]string{} // rsName -> role
	switch mode {
	case deployModeStable:
		if stableRS != nil {
			activeRS[stableRS.Name] = "stable"
		}
	case deployModeCanary:
		if newRS != nil {
			activeRS[newRS.Name] = "canary"
		}
	case deployModeStableCanary:
		if stableRS != nil {
			activeRS[stableRS.Name] = "stable"
		}
		if newRS != nil {
			activeRS[newRS.Name] = "canary"
		}
	default: // active
		for _, rs := range owned {
			if !replicasActive(rs) {
				continue
			}
			role := "rs"
			if newRS != nil && rs.Name == newRS.Name {
				role = "canary"
			} else if stableRS != nil && rs.Name == stableRS.Name {
				role = "stable"
			}
			activeRS[rs.Name] = role
		}
	}

	out := map[string]string{}
	for _, pod := range pods {
		if pod == nil {
			continue
		}
		rsName := owningReplicaSet(pod.OwnerReferences)
		if rsName == "" {
			continue
		}
		role, ok := activeRS[rsName]
		if !ok {
			continue
		}
		out[pod.Namespace+"/"+pod.Name] = role
	}
	return out
}

func ownedBy(refs []metav1.OwnerReference, kind, name, uid string) bool {
	for _, r := range refs {
		if r.Kind == kind && r.Name == name {
			if uid == "" || string(r.UID) == uid {
				return true
			}
		}
	}
	return false
}

func owningReplicaSet(refs []metav1.OwnerReference) string {
	for _, r := range refs {
		if r.Kind == "ReplicaSet" && strings.TrimSpace(r.Name) != "" {
			return r.Name
		}
	}
	return ""
}

func replicasActive(rs *appsv1.ReplicaSet) bool {
	if rs == nil {
		return false
	}
	if rs.Spec.Replicas != nil && *rs.Spec.Replicas > 0 {
		return true
	}
	if rs.Status.Replicas > 0 || rs.Status.ReadyReplicas > 0 || rs.Status.AvailableReplicas > 0 {
		return true
	}
	return false
}

func pickDeploymentReplicaSets(deploy *appsv1.Deployment, owned []*appsv1.ReplicaSet) (newRS *appsv1.ReplicaSet, stableRS *appsv1.ReplicaSet) {
	if deploy == nil || len(owned) == 0 {
		return nil, nil
	}
	desiredHash := strings.TrimSpace(deploy.Spec.Template.Labels["pod-template-hash"])
	if desiredHash != "" {
		for _, rs := range owned {
			if strings.TrimSpace(rs.Spec.Template.Labels["pod-template-hash"]) == desiredHash {
				newRS = rs
				break
			}
		}
	}

	if newRS == nil {
		newRS = highestRevision(owned, nil)
	}

	stableRS = highestRevision(owned, func(rs *appsv1.ReplicaSet) bool {
		if newRS != nil && rs.Name == newRS.Name {
			return false
		}
		return replicasActive(rs)
	})
	if stableRS == nil {
		stableRS = highestRevision(owned, func(rs *appsv1.ReplicaSet) bool {
			if newRS != nil && rs.Name == newRS.Name {
				return false
			}
			return true
		})
	}
	return newRS, stableRS
}

func highestRevision(owned []*appsv1.ReplicaSet, allow func(*appsv1.ReplicaSet) bool) *appsv1.ReplicaSet {
	type scored struct {
		rs       *appsv1.ReplicaSet
		revision int64
		avail    int32
		ready    int32
	}
	list := make([]scored, 0, len(owned))
	for _, rs := range owned {
		if allow != nil && !allow(rs) {
			continue
		}
		list = append(list, scored{
			rs:       rs,
			revision: deploymentRevision(rs),
			avail:    rs.Status.AvailableReplicas,
			ready:    rs.Status.ReadyReplicas,
		})
	}
	if len(list) == 0 {
		return nil
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].avail != list[j].avail {
			return list[i].avail > list[j].avail
		}
		if list[i].ready != list[j].ready {
			return list[i].ready > list[j].ready
		}
		if list[i].revision != list[j].revision {
			return list[i].revision > list[j].revision
		}
		return list[i].rs.CreationTimestamp.After(list[j].rs.CreationTimestamp.Time)
	})
	return list[0].rs
}

func deploymentRevision(rs *appsv1.ReplicaSet) int64 {
	if rs == nil {
		return 0
	}
	raw := strings.TrimSpace(rs.Annotations["deployment.kubernetes.io/revision"])
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
}
