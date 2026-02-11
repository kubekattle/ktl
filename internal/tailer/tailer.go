// File: internal/tailer/tailer.go
// Brief: Internal tailer package implementation for 'tailer'.

// Package tailer implements ktl's high-performance, color-aware log streamer
// used by the 'ktl logs' family of commands, coordinating informers, filters,
// and observer hooks.
package tailer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"io"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/example/ktl/internal/config"
	"github.com/fatih/color"
	"github.com/go-logr/logr"
	"golang.org/x/sync/errgroup"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/cache"
)

// Tailer coordinates pod discovery via informers and streams logs to stdout.
type logSource string

const (
	sourcePod   logSource = "pod"
	sourceNode  logSource = "node"
	sourceEvent logSource = "event"
)

const (
	logScannerInitial = 64 * 1024
	logScannerMax     = 1024 * 1024
)

// Tailer coordinates pod discovery via informers and streams logs to stdout.
type Tailer struct {
	client             kubernetes.Interface
	opts               *config.Options
	log                logr.Logger
	writer             io.Writer
	podRegex           *regexp.Regexp
	template           *template.Template
	ctx                context.Context
	cancel             context.CancelFunc
	mu                 sync.Mutex
	tails              map[containerKey]*tailState
	podFilter          func(*corev1.Pod) bool
	podDisplayOverride map[containerKey]string
	podColors          []*color.Color
	containerColors    []*color.Color
	highlight          *color.Color
	eventCols          map[string]*color.Color
	bufferPool         sync.Pool
	scannerBuffers     sync.Pool
	observers          []LogObserver
	selectionObservers []SelectionObserver
	nodeLogs           *nodeLogManager
	defaultTemplate    bool
	jsonFilter         map[string]string
}

// LogRecord captures a single log line emitted by the tailer along with contextual metadata.
type LogRecord struct {
	Timestamp          time.Time
	FormattedTimestamp string
	Namespace          string
	Pod                string
	Container          string
	Raw                string
	Rendered           string
	Source             string
	SourceGlyph        string
	RenderedEqualsRaw  bool
}

// LogObserver receives callbacks whenever the tailer renders a log line.
type LogObserver interface {
	ObserveLog(LogRecord)
}

// SelectionSnapshot captures a change in the set of active log tails.
type SelectionSnapshot struct {
	Timestamp    time.Time         `json:"timestamp"`
	ChangeKind   string            `json:"changeKind"`
	Reason       string            `json:"reason,omitempty"`
	Namespace    string            `json:"namespace,omitempty"`
	Pod          string            `json:"pod,omitempty"`
	Container    string            `json:"container,omitempty"`
	RestartCount int32             `json:"restartCount,omitempty"`
	Selected     []SelectionTarget `json:"selected"`
}

type SelectionTarget struct {
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
}

// SelectionObserver receives callbacks whenever the tailer changes its active selection.
type SelectionObserver interface {
	ObserveSelection(SelectionSnapshot)
}

// Option configures optional Tailer behavior.
type Option func(*Tailer)

// WithLogObserver registers an observer that sees every rendered log line.
func WithLogObserver(observer LogObserver) Option {
	return func(t *Tailer) {
		if observer == nil {
			return
		}
		t.observers = append(t.observers, observer)
	}
}

// WithSelectionObserver registers an observer that sees active selection changes (pods/containers tailed).
func WithSelectionObserver(observer SelectionObserver) Option {
	return func(t *Tailer) {
		if observer == nil {
			return
		}
		t.selectionObservers = append(t.selectionObservers, observer)
	}
}

// WithOutput overrides the writer used for rendered output.
func WithOutput(w io.Writer) Option {
	return func(t *Tailer) {
		if w != nil {
			t.writer = w
		}
	}
}

// WithPodFilter registers an optional callback that further restricts which pods are tailed.
// It is evaluated after name/condition filters.
func WithPodFilter(filter func(*corev1.Pod) bool) Option {
	return func(t *Tailer) {
		if filter != nil {
			t.podFilter = filter
		}
	}
}

// WithJSONFilter sets key-value pairs to filter structured logs.
func WithJSONFilter(filters map[string]string) Option {
	return func(t *Tailer) {
		t.jsonFilter = filters
	}
}

type containerKey struct {
	Namespace string
	Pod       string
	Container string
}

type tailState struct {
	cancel       context.CancelFunc
	restartCount int32
	podUID       string
}

type logEntry struct {
	Timestamp        string
	Namespace        string
	NamespaceDisplay string
	PodName          string
	PodDisplay       string
	ContainerName    string
	ContainerTag     string
	Message          string
	Raw              string
	SourceGlyph      string
	SourceLabel      string
}

// New creates a Tailer instance.
func New(client kubernetes.Interface, opts *config.Options, logger logr.Logger, tailerOptions ...Option) (*Tailer, error) {
	podRegex, err := regexp.Compile(opts.PodQuery)
	if err != nil {
		return nil, err
	}
	tmpl, err := template.New("ktl").Parse(opts.Template)
	if err != nil {
		return nil, fmt.Errorf("parse template: %w", err)
	}
	switch opts.ColorMode {
	case "always":
		color.NoColor = false
	case "never":
		color.NoColor = true
	}
	defaultPalette := DefaultColorPalette()
	podPalette := defaultPalette
	if podColorValues := normalizeColorList(opts.PodColorStrings); len(podColorValues) > 0 {
		custom, err := buildCustomPalette(podColorValues, "--pod-colors")
		if err != nil {
			return nil, err
		}
		podPalette = custom
	}
	containerPalette := podPalette
	if containerColorValues := normalizeColorList(opts.ContainerColorStrings); len(containerColorValues) > 0 {
		custom, err := buildCustomPalette(containerColorValues, "--container-colors")
		if err != nil {
			return nil, err
		}
		containerPalette = custom
	}
	highlight := color.New(color.BgYellow, color.FgBlack)
	defaultTemplate := !opts.JSONOutput && strings.TrimSpace(opts.Template) == config.DefaultTemplate()
	t := &Tailer{
		client:             client,
		opts:               opts,
		log:                logger.WithName("tailer"),
		writer:             os.Stdout,
		podRegex:           podRegex,
		template:           tmpl,
		tails:              make(map[containerKey]*tailState),
		podDisplayOverride: make(map[containerKey]string),
		podColors:          podPalette,
		containerColors:    containerPalette,
		highlight:          highlight,
		eventCols: map[string]*color.Color{
			"Normal":  color.New(color.FgCyan),
			"Warning": color.New(color.FgYellow),
			"Error":   color.New(color.FgRed),
		},
		bufferPool: sync.Pool{
			New: func() interface{} {
				buf := &bytes.Buffer{}
				buf.Grow(512)
				return buf
			},
		},
		scannerBuffers: sync.Pool{
			New: func() interface{} { return make([]byte, logScannerInitial) },
		},
		defaultTemplate: defaultTemplate,
	}
	for _, opt := range tailerOptions {
		opt(t)
	}
	if len(opts.NodeLogFiles) > 0 {
		t.nodeLogs = newNodeLogManager(t)
	}
	return t, nil
}

// Run launches the tailer until the context is cancelled.
func (t *Tailer) Run(ctx context.Context) error {
	t.ctx, t.cancel = context.WithCancel(ctx)
	defer t.cancel()

	if len(t.selectionObservers) > 0 {
		go t.sampleSelection(t.ctx, 10*time.Second)
	}
	t.log.V(1).Info("starting tailer run", "follow", t.opts.Follow, "events", t.opts.Events, "eventsOnly", t.opts.EventsOnly)
	defer t.log.V(1).Info("tailer run finished")
	if t.nodeLogs != nil && t.opts.NodeLogAll {
		if err := t.nodeLogs.ensureAllNodes(t.ctx); err != nil {
			t.log.Error(err, "enable node log streaming for all nodes")
		}
	}

	if t.opts.Follow {
		eg, ctx := errgroup.WithContext(t.ctx)
		if t.opts.Events {
			eg.Go(func() error { return t.followEvents(ctx) })
		}
		if !t.opts.EventsOnly {
			eg.Go(func() error { return t.runFollow(ctx) })
		}
		return eg.Wait()
	}

	if t.opts.Events {
		if err := t.printEventsSnapshot(t.ctx); err != nil {
			return err
		}
		if t.opts.EventsOnly {
			return nil
		}
	}

	return t.runOnce(t.ctx)
}

func (t *Tailer) runFollow(ctx context.Context) error {
	namespaces := t.resolveNamespaces()
	t.log.V(1).Info("starting follow mode", "namespaces", namespaces, "events", t.opts.Events, "eventsOnly", t.opts.EventsOnly)
	informers := t.createInformers(namespaces)
	if len(informers) == 0 {
		return fmt.Errorf("no informers could be configured")
	}

	for _, informer := range informers {
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc:    t.handlePodAdd,
			UpdateFunc: func(oldObj, newObj interface{}) { t.handlePodAdd(newObj) },
			DeleteFunc: t.handlePodDelete,
		})
		go informer.Run(ctx.Done())
	}

	synced := make([]cache.InformerSynced, 0, len(informers))
	for _, informer := range informers {
		synced = append(synced, informer.HasSynced)
	}
	if !cache.WaitForCacheSync(ctx.Done(), synced...) {
		return fmt.Errorf("failed to sync informers before context cancellation")
	}
	t.log.V(1).Info("informers synced, waiting for context cancellation", "namespaceCount", len(informers))

	<-ctx.Done()
	t.log.V(1).Info("follow context done, stopping all tails")
	t.stopAllTails()
	return nil
}

func (t *Tailer) runOnce(ctx context.Context) error {
	namespaces := t.resolveNamespaces()
	t.log.V(1).Info("running once without informers", "namespaces", namespaces)
	listOpts := metav1.ListOptions{}
	if t.opts.LabelSelector != "" {
		listOpts.LabelSelector = t.opts.LabelSelector
	}
	if t.opts.FieldSelector != "" {
		listOpts.FieldSelector = t.opts.FieldSelector
	}

	var (
		podsMu sync.Mutex
		pods   = make([]*corev1.Pod, 0, len(namespaces)*4)
	)
	eg, egCtx := errgroup.WithContext(ctx)
	for _, ns := range namespaces {
		ns := ns
		localOpts := listOpts
		eg.Go(func() error {
			list, err := t.client.CoreV1().Pods(ns).List(egCtx, localOpts)
			if err != nil {
				return fmt.Errorf("list pods in %s: %w", ns, err)
			}
			matched := make([]*corev1.Pod, 0, len(list.Items))
			for i := range list.Items {
				pod := list.Items[i]
				if !t.podRegex.MatchString(pod.Name) {
					continue
				}
				if t.podExcluded(pod.Name) {
					continue
				}
				if !t.podMatchesConditions(&pod) {
					continue
				}
				if t.podFilter != nil && !t.podFilter(&pod) {
					continue
				}
				matched = append(matched, pod.DeepCopy())
			}
			if len(matched) == 0 {
				return nil
			}
			podsMu.Lock()
			pods = append(pods, matched...)
			podsMu.Unlock()
			return nil
		})
	}
	if err := eg.Wait(); err != nil {
		return err
	}
	t.log.V(1).Info("resolved pods for one-shot run", "count", len(pods))
	if t.nodeLogs != nil {
		t.nodeLogs.ensureForPods(pods)
	}

	var wg sync.WaitGroup
	for _, pod := range pods {
		for _, container := range t.allPodContainers(pod) {
			if !t.containerIncluded(container.Name) {
				continue
			}
			restart := t.restartCountFor(pod, container.Name)
			wg.Add(1)
			go func(p *corev1.Pod, containerName string, count int32) {
				defer wg.Done()
				t.streamContainer(ctx, p, containerName, count)
			}(pod, container.Name, restart)
		}
	}
	wg.Wait()
	return nil
}

func (t *Tailer) followEvents(ctx context.Context) error {
	informers := t.createEventInformers(t.resolveNamespaces())
	if len(informers) == 0 {
		return nil
	}
	t.log.V(1).Info("starting event follow", "namespaceCount", len(informers))
	for _, informer := range informers {
		informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
			AddFunc: t.handleEventAdd,
			UpdateFunc: func(oldObj, newObj interface{}) {
				oldEvent, _ := oldObj.(*corev1.Event)
				newEvent, _ := newObj.(*corev1.Event)
				if newEvent == nil {
					return
				}
				if oldEvent != nil && newEvent.Count == oldEvent.Count && newEvent.Message == oldEvent.Message {
					return
				}
				t.handleEventAdd(newObj)
			},
		})
		go informer.Run(ctx.Done())
	}
	synced := make([]cache.InformerSynced, 0, len(informers))
	for _, informer := range informers {
		synced = append(synced, informer.HasSynced)
	}
	if !cache.WaitForCacheSync(ctx.Done(), synced...) {
		return fmt.Errorf("failed to sync event informers before context cancellation")
	}
	t.log.V(1).Info("event informers synced")
	<-ctx.Done()
	t.log.V(1).Info("event follow context done")
	return nil
}

func (t *Tailer) resolveNamespaces() []string {
	if t.opts.AllNamespaces {
		return []string{metav1.NamespaceAll}
	}
	if len(t.opts.Namespaces) == 0 {
		return []string{"default"}
	}
	return t.opts.Namespaces
}

func (t *Tailer) createInformers(namespaces []string) []cache.SharedIndexInformer {
	informers := make([]cache.SharedIndexInformer, 0, len(namespaces))
	for _, ns := range namespaces {
		namespace := ns
		lw := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				if t.opts.LabelSelector != "" {
					options.LabelSelector = t.opts.LabelSelector
				}
				if t.opts.FieldSelector != "" {
					options.FieldSelector = t.opts.FieldSelector
				}
				return t.client.CoreV1().Pods(namespace).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				if t.opts.LabelSelector != "" {
					options.LabelSelector = t.opts.LabelSelector
				}
				if t.opts.FieldSelector != "" {
					options.FieldSelector = t.opts.FieldSelector
				}
				return t.client.CoreV1().Pods(namespace).Watch(context.Background(), options)
			},
		}
		informer := cache.NewSharedIndexInformer(
			lw,
			&corev1.Pod{},
			0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
		informers = append(informers, informer)
		if namespace == metav1.NamespaceAll {
			break
		}
	}
	return informers
}

func (t *Tailer) createEventInformers(namespaces []string) []cache.SharedIndexInformer {
	informers := make([]cache.SharedIndexInformer, 0, len(namespaces))
	for _, ns := range namespaces {
		namespace := ns
		lw := &cache.ListWatch{
			ListFunc: func(options metav1.ListOptions) (runtime.Object, error) {
				return t.client.CoreV1().Events(namespace).List(context.Background(), options)
			},
			WatchFunc: func(options metav1.ListOptions) (watch.Interface, error) {
				return t.client.CoreV1().Events(namespace).Watch(context.Background(), options)
			},
		}
		informer := cache.NewSharedIndexInformer(
			lw,
			&corev1.Event{},
			0,
			cache.Indexers{cache.NamespaceIndex: cache.MetaNamespaceIndexFunc},
		)
		informers = append(informers, informer)
		if namespace == metav1.NamespaceAll {
			break
		}
	}
	return informers
}

func (t *Tailer) handlePodAdd(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		return
	}
	if !t.podRegex.MatchString(pod.Name) {
		return
	}
	if t.podExcluded(pod.Name) {
		return
	}
	if !t.podMatchesConditions(pod) {
		return
	}
	if t.podFilter != nil && !t.podFilter(pod) {
		return
	}
	if t.nodeLogs != nil {
		t.nodeLogs.ensureForPod(pod)
	}
	for _, container := range t.allPodContainers(pod) {
		if !t.containerIncluded(container.Name) {
			continue
		}
		count := t.restartCountFor(pod, container.Name)
		t.ensureTail(pod, container.Name, count)
	}
}

func (t *Tailer) handlePodDelete(obj interface{}) {
	pod, ok := obj.(*corev1.Pod)
	if !ok {
		tombstone, cast := obj.(cache.DeletedFinalStateUnknown)
		if !cast {
			return
		}
		pod, _ = tombstone.Obj.(*corev1.Pod)
		if pod == nil {
			return
		}
	}
	for _, container := range t.allPodContainers(pod) {
		t.stopTail(pod.Namespace, pod.Name, container.Name, "pod_deleted")
	}
}

// SetPodDisplayOverride overrides the rendered pod label for the given pod.
// The override affects only output formatting, not Kubernetes API requests.
func (t *Tailer) SetPodDisplayOverride(namespace, pod, display string) {
	key := containerKey{Namespace: namespace, Pod: pod}
	t.mu.Lock()
	if strings.TrimSpace(display) == "" {
		delete(t.podDisplayOverride, key)
	} else {
		t.podDisplayOverride[key] = display
	}
	t.mu.Unlock()
}

// PruneTails stops any active tails that do not satisfy keep.
func (t *Tailer) PruneTails(keep func(namespace, pod, container string) bool, reason string) {
	if keep == nil {
		return
	}
	var toStop []containerKey
	t.mu.Lock()
	for k := range t.tails {
		if !keep(k.Namespace, k.Pod, k.Container) {
			toStop = append(toStop, k)
		}
	}
	t.mu.Unlock()
	for _, k := range toStop {
		t.stopTail(k.Namespace, k.Pod, k.Container, reason)
	}
}

func (t *Tailer) handleEventAdd(obj interface{}) {
	event, ok := obj.(*corev1.Event)
	if !ok {
		return
	}
	if !t.shouldPrintEvent(event) {
		return
	}
	t.printEvent(event)
}

func (t *Tailer) containerIncluded(name string) bool {
	if len(t.opts.ContainerRegex) > 0 {
		ok := false
		for _, re := range t.opts.ContainerRegex {
			if re.MatchString(name) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	for _, re := range t.opts.ExcludeRegex {
		if re.MatchString(name) {
			return false
		}
	}
	return true
}

func (t *Tailer) podExcluded(name string) bool {
	for _, re := range t.opts.ExcludePodRegex {
		if re.MatchString(name) {
			return true
		}
	}
	return false
}

func (t *Tailer) podMatchesConditions(pod *corev1.Pod) bool {
	if len(t.opts.ConditionFilters) == 0 {
		return true
	}
	condMap := make(map[corev1.PodConditionType]corev1.ConditionStatus, len(pod.Status.Conditions))
	for _, cond := range pod.Status.Conditions {
		condMap[cond.Type] = cond.Status
	}
	for condType, expected := range t.opts.ConditionFilters {
		actual, ok := condMap[condType]
		if !ok {
			actual = corev1.ConditionUnknown
		}
		if actual != expected {
			return false
		}
	}
	return true
}

func (t *Tailer) ensureTail(pod *corev1.Pod, container string, restartCount int32) {
	key := containerKey{Namespace: pod.Namespace, Pod: pod.Name, Container: container}
	var cancel context.CancelFunc
	var selection SelectionSnapshot
	emitSelection := false
	t.mu.Lock()
	if state, ok := t.tails[key]; ok {
		if state.podUID == string(pod.UID) && state.restartCount == restartCount {
			t.mu.Unlock()
			return
		}
		cancel = state.cancel
		delete(t.tails, key)
	}
	ctx, ctxCancel := context.WithCancel(t.ctx)
	t.tails[key] = &tailState{cancel: ctxCancel, restartCount: restartCount, podUID: string(pod.UID)}
	if len(t.selectionObservers) > 0 {
		reason := "add"
		if cancel != nil {
			reason = "replace"
		}
		selection = t.selectionSnapshotLocked("add", reason, pod.Namespace, pod.Name, container, restartCount)
		emitSelection = true
	}
	t.mu.Unlock()
	t.log.V(1).Info("ensuring tail", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "restartCount", restartCount)
	if emitSelection {
		t.notifySelection(selection)
	}
	if cancel != nil {
		t.log.V(1).Info("stopping replaced tail", "namespace", pod.Namespace, "pod", pod.Name, "container", container)
		cancel()
	}
	go t.streamContainer(ctx, pod, container, restartCount)
}

func (t *Tailer) stopTail(namespace, pod, container string, reason string) {
	key := containerKey{Namespace: namespace, Pod: pod, Container: container}
	var selection SelectionSnapshot
	emitSelection := false
	t.mu.Lock()
	state, ok := t.tails[key]
	if ok {
		delete(t.tails, key)
		if len(t.selectionObservers) > 0 {
			selection = t.selectionSnapshotLocked("remove", reason, namespace, pod, container, state.restartCount)
			emitSelection = true
		}
	}
	t.mu.Unlock()
	if ok {
		t.log.V(1).Info("stopping tail", "namespace", namespace, "pod", pod, "container", container)
		state.cancel()
		if emitSelection {
			t.notifySelection(selection)
		}
	}
}

func (t *Tailer) stopAllTails() {
	t.mu.Lock()
	states := make([]*tailState, 0, len(t.tails))
	for _, state := range t.tails {
		states = append(states, state)
	}
	t.tails = map[containerKey]*tailState{}
	var selection SelectionSnapshot
	emitSelection := false
	if len(t.selectionObservers) > 0 {
		selection = SelectionSnapshot{
			Timestamp:  time.Now().UTC(),
			ChangeKind: "reset",
			Reason:     "stop_all",
			Selected:   nil,
		}
		emitSelection = true
	}
	t.mu.Unlock()
	t.log.V(1).Info("cancelling active tails", "count", len(states))
	if emitSelection {
		t.notifySelection(selection)
	}
	for _, state := range states {
		state.cancel()
	}
}

func (t *Tailer) selectionSnapshotLocked(kind, reason, namespace, pod, container string, restartCount int32) SelectionSnapshot {
	selected := make([]SelectionTarget, 0, len(t.tails))
	for k := range t.tails {
		selected = append(selected, SelectionTarget(k))
	}
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].Namespace != selected[j].Namespace {
			return selected[i].Namespace < selected[j].Namespace
		}
		if selected[i].Pod != selected[j].Pod {
			return selected[i].Pod < selected[j].Pod
		}
		return selected[i].Container < selected[j].Container
	})
	return SelectionSnapshot{
		Timestamp:    time.Now().UTC(),
		ChangeKind:   kind,
		Reason:       strings.TrimSpace(reason),
		Namespace:    namespace,
		Pod:          pod,
		Container:    container,
		RestartCount: restartCount,
		Selected:     selected,
	}
}

func (t *Tailer) notifySelection(s SelectionSnapshot) {
	if len(t.selectionObservers) == 0 {
		return
	}
	observers := append([]SelectionObserver(nil), t.selectionObservers...)
	for _, obs := range observers {
		if obs != nil {
			obs.ObserveSelection(s)
		}
	}
}

func (t *Tailer) sampleSelection(ctx context.Context, interval time.Duration) {
	if interval <= 0 {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			t.mu.Lock()
			if len(t.selectionObservers) == 0 {
				t.mu.Unlock()
				continue
			}
			snap := t.selectionSnapshotLocked("sample", "periodic", "", "", "", 0)
			t.mu.Unlock()
			t.notifySelection(snap)
		}
	}
}

func (t *Tailer) streamContainer(ctx context.Context, pod *corev1.Pod, container string, restartCount int32) {
	logOpts := &corev1.PodLogOptions{
		Container: container,
		Follow:    t.opts.Follow,
	}
	if t.opts.Since > 0 {
		seconds := int64(t.opts.Since.Seconds())
		logOpts.SinceSeconds = &seconds
	}
	if t.opts.TailLines >= 0 {
		tail := t.opts.TailLines
		logOpts.TailLines = &tail
	}

	backoff := 250 * time.Millisecond
	for {
		if ctx.Err() != nil {
			return
		}
		t.log.V(1).Info("starting container stream", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "restartCount", restartCount, "follow", t.opts.Follow, "tailLines", t.opts.TailLines, "since", t.opts.Since.String())
		stream, err := t.client.CoreV1().Pods(pod.Namespace).GetLogs(pod.Name, logOpts).Stream(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			if isRetryableLogStreamErr(err) {
				t.log.V(1).Info("log stream unavailable yet; retrying", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "error", err.Error(), "backoff", backoff.String())
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 2*time.Second {
					backoff *= 2
					if backoff > 2*time.Second {
						backoff = 2 * time.Second
					}
				}
				continue
			}
			t.log.Error(err, "stream logs failed", "namespace", pod.Namespace, "pod", pod.Name, "container", container)
			return
		}

		backoff = 250 * time.Millisecond
		scanner := bufio.NewScanner(stream)
		buf := t.getScannerBuffer()
		scanner.Buffer(buf, logScannerMax)
		for scanner.Scan() {
			select {
			case <-ctx.Done():
				t.putScannerBuffer(buf)
				_ = stream.Close()
				return
			default:
			}
			line := scanner.Text()
			if t.opts.ExcludeLineRegex != nil && t.opts.ExcludeLineRegex.MatchString(line) {
				continue
			}

			if len(t.jsonFilter) > 0 {
				if !matchJSONFilter(line, t.jsonFilter) {
					continue
				}
			}

			t.outputLine(sourcePod, pod.Namespace, pod.Name, container, line)
		}
		scanErr := scanner.Err()
		t.putScannerBuffer(buf)
		_ = stream.Close()
		switch {
		case scanErr != nil && scanErr != io.EOF && ctx.Err() == nil && !isContextErr(scanErr):
			if isRetryableLogStreamErr(scanErr) {
				t.log.V(1).Info("log stream ended transiently; retrying", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "error", scanErr.Error(), "backoff", backoff.String())
				select {
				case <-ctx.Done():
					return
				case <-time.After(backoff):
				}
				if backoff < 2*time.Second {
					backoff *= 2
					if backoff > 2*time.Second {
						backoff = 2 * time.Second
					}
				}
				continue
			}
			t.log.Error(scanErr, "scanner error", "namespace", pod.Namespace, "pod", pod.Name, "container", container)
			return
		case ctx.Err() != nil:
			t.log.V(1).Info("container stream stopped by context", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "reason", ctx.Err())
			return
		default:
			reason := "drained"
			if scanErr == io.EOF {
				reason = "eof"
			} else if isContextErr(scanErr) {
				reason = "context"
			}
			t.log.V(1).Info("container stream finished", "namespace", pod.Namespace, "pod", pod.Name, "container", container, "reason", reason)
			return
		}
	}
}

func matchJSONFilter(line string, filters map[string]string) bool {
	// Fast check: must look like JSON
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{") {
		return false
	}

	var data map[string]interface{}
	if err := json.Unmarshal([]byte(trimmed), &data); err != nil {
		return false // Not JSON
	}

	for k, v := range filters {
		val, ok := data[k]
		if !ok {
			return false
		}

		// String comparison
		if strVal, ok := val.(string); ok {
			if strVal != v {
				return false
			}
		} else {
			// Try basic string conversion for numbers/bools
			if fmt.Sprintf("%v", val) != v {
				return false
			}
		}
	}
	return true
}

func isRetryableLogStreamErr(err error) bool {
	if err == nil {
		return false
	}

	// Prefer structured status payloads when available, since k8s client errors may be wrapped
	// in a way that loses the original message in err.Error().
	for e := err; e != nil; e = errors.Unwrap(e) {
		apiStatus, ok := e.(apierrors.APIStatus)
		if !ok {
			continue
		}
		msg := strings.ToLower(apiStatus.Status().Message)
		if strings.Contains(msg, "is waiting to start") {
			return true
		}
		if strings.Contains(msg, "containercreating") || strings.Contains(msg, "podinitializing") {
			return true
		}
	}

	// Fallback: string matching against the fully formatted error.
	// The apiserver returns BadRequest with messages like:
	// "container \"X\" in pod \"Y\" is waiting to start: ContainerCreating"
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "is waiting to start") {
		return true
	}
	if strings.Contains(msg, "containercreating") || strings.Contains(msg, "podinitializing") {
		return true
	}
	return false
}

func (t *Tailer) getScannerBuffer() []byte {
	if buf, ok := t.scannerBuffers.Get().(*[]byte); ok && buf != nil {
		return (*buf)[:logScannerInitial]
	}
	return make([]byte, logScannerInitial)
}

func (t *Tailer) putScannerBuffer(buf []byte) {
	if buf == nil {
		return
	}
	if cap(buf) < logScannerInitial {
		buf = make([]byte, logScannerInitial)
	}
	buf = buf[:logScannerInitial]
	t.scannerBuffers.Put(&buf)
}

func isContextErr(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func (t *Tailer) outputLine(src logSource, namespace, pod, container, line string) {
	if t.opts.NodeLogsOnly && src == sourcePod {
		return
	}
	wallClock := time.Now()
	timestamp := ""
	if t.opts.ShowTimestamp {
		formatTime := wallClock
		if t.opts.TimeLocation != nil {
			formatTime = formatTime.In(t.opts.TimeLocation)
		}
		if strings.EqualFold(t.opts.TimestampFormat, config.TimestampFormatYouTube) {
			timestamp = youtubeTimestamp(formatTime)
		} else {
			timestamp = formatTime.Format(t.opts.TimestampFormat)
		}
	}
	message := line
	if len(t.opts.SearchRegex) > 0 && !t.colorsDisabled() {
		for _, re := range t.opts.SearchRegex {
			if !re.MatchString(message) {
				continue
			}
			message = re.ReplaceAllStringFunc(message, func(m string) string {
				return t.highlight.Sprint(m)
			})
		}
	}
	displayNamespace := namespace
	displayPod := pod
	if src == sourceNode && namespace != "" {
		displayPod = fmt.Sprintf("%s/%s", namespace, pod)
	}
	if src == sourcePod {
		key := containerKey{Namespace: namespace, Pod: pod}
		t.mu.Lock()
		override := t.podDisplayOverride[key]
		t.mu.Unlock()
		if override != "" {
			displayPod = override
		}
	}
	containerTag := formatContainerTag(container)
	timestampToken := ""
	if t.opts.ShowTimestamp {
		timestampToken = fmt.Sprintf("[%s]", timestamp)
	}
	podToken := displayPod
	entry := logEntry{
		Timestamp:        timestamp,
		Namespace:        namespace,
		NamespaceDisplay: displayNamespace,
		PodName:          pod,
		PodDisplay:       podToken,
		ContainerName:    container,
		ContainerTag:     containerTag,
		Message:          message,
		Raw:              line,
		SourceGlyph:      "",
		SourceLabel:      src.label(),
	}
	rendered := line
	if t.opts.JSONOutput {
		t.notifyLogObservers(entry, line, rendered, wallClock)
		fmt.Fprintln(t.writer, line)
		return
	}
	if t.defaultTemplate {
		rendered = t.formatDefaultLine(timestamp, podToken, containerTag, message)
	} else {
		buf := t.bufferPool.Get().(*bytes.Buffer)
		buf.Reset()
		defer func() {
			buf.Reset()
			t.bufferPool.Put(buf)
		}()
		if err := t.template.Execute(buf, entry); err != nil {
			t.log.Error(err, "execute template")
			return
		}
		rendered = buf.String()
	}
	t.notifyLogObservers(entry, line, rendered, wallClock)
	colored := t.applyColors(timestampToken, podToken, containerTag, rendered)
	fmt.Fprintln(t.writer, colored)
}

func (t *Tailer) notifyLogObservers(entry logEntry, raw, rendered string, ts time.Time) {
	if len(t.observers) == 0 {
		return
	}
	record := LogRecord{
		Timestamp:          ts,
		FormattedTimestamp: entry.Timestamp,
		Namespace:          entry.Namespace,
		Pod:                entry.PodName,
		Container:          entry.ContainerName,
		Raw:                raw,
		Rendered:           rendered,
		Source:             entry.SourceLabel,
		SourceGlyph:        entry.SourceGlyph,
		RenderedEqualsRaw:  rendered == raw,
	}
	for _, observer := range t.observers {
		observer.ObserveLog(record)
	}
}

func (t *Tailer) applyColors(timestampToken, podToken, containerTag, text string) string {
	if t.colorsDisabled() {
		return text
	}
	podColor := t.colorFor(podToken, t.podColors)
	colored := colorizeToken(text, podToken, podColor)
	var timestampColor *color.Color
	if timestampToken != "" && strings.Contains(text, timestampToken) {
		timestampColor = t.distinctColorFor(timestampToken, t.containerColors, podColor)
		colored = colorizeToken(colored, timestampToken, timestampColor)
	}
	tag := containerTag
	if tag != "" {
		containerColor := t.distinctColorFor(tag, t.containerColors, podColor, timestampColor)
		colored = colorizeToken(colored, tag, containerColor)
	}
	return colored
}

func colorizeToken(text, token string, color *color.Color) string {
	if token == "" || color == nil {
		return text
	}
	idx := strings.Index(text, token)
	if idx == -1 {
		return text
	}
	var b strings.Builder
	b.Grow(len(text) + len(token))
	b.WriteString(text[:idx])
	b.WriteString(color.Sprint(token))
	b.WriteString(text[idx+len(token):])
	return b.String()
}

func formatContainerTag(name string) string {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return ""
	}
	return fmt.Sprintf("[%s]", trimmed)
}

func (t *Tailer) formatDefaultLine(timestamp, podToken, containerTag, message string) string {
	var b strings.Builder
	b.Grow(len(timestamp) + len(podToken) + len(containerTag) + len(message) + 6)
	b.WriteByte('[')
	b.WriteString(timestamp)
	b.WriteString("] ")
	b.WriteString(podToken)
	b.WriteByte(' ')
	b.WriteString(containerTag)
	b.WriteByte(' ')
	b.WriteString(message)
	return b.String()
}

func (s logSource) label() string {
	switch s {
	case sourceNode:
		return "node"
	case sourceEvent:
		return "event"
	case sourcePod:
		return "pod"
	default:
		return "unknown"
	}
}

func youtubeTimestamp(ts time.Time) string {
	hour, min, sec := ts.Clock()
	return fmt.Sprintf("%d:%02d:%02d", hour, min, sec)
}

func (t *Tailer) printEventsSnapshot(ctx context.Context) error {
	events, err := t.listEvents(ctx)
	if err != nil {
		return err
	}
	sort.Slice(events, func(i, j int) bool {
		return eventTimestamp(events[i]).Before(eventTimestamp(events[j]))
	})
	for _, ev := range events {
		t.printEvent(ev)
	}
	return nil
}

func (t *Tailer) listEvents(ctx context.Context) ([]*corev1.Event, error) {
	namespaces := t.resolveNamespaces()
	events := make([]*corev1.Event, 0, len(namespaces)*4)
	for _, ns := range namespaces {
		list, err := t.client.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list events in %s: %w", ns, err)
		}
		for i := range list.Items {
			ev := list.Items[i]
			if !t.shouldPrintEvent(&ev) {
				continue
			}
			events = append(events, ev.DeepCopy())
		}
	}
	return events, nil
}

func (t *Tailer) shouldPrintEvent(ev *corev1.Event) bool {
	if ev == nil {
		return false
	}
	kind := strings.ToLower(ev.InvolvedObject.Kind)
	if !t.eventKindAllowed(kind, ev.InvolvedObject.Name) {
		return false
	}
	if kind == "pod" && !t.podRegex.MatchString(ev.InvolvedObject.Name) {
		return false
	}
	if kind == "pod" && t.podExcluded(ev.InvolvedObject.Name) {
		return false
	}
	return t.namespaceAllowed(ev.InvolvedObject.Namespace)
}

var defaultEventKinds = map[string]struct{}{
	"pod":         {},
	"deployment":  {},
	"replicaset":  {},
	"statefulset": {},
	"daemonset":   {},
	"job":         {},
	"cronjob":     {},
}

func (t *Tailer) eventKindAllowed(kind, name string) bool {
	if _, ok := defaultEventKinds[kind]; ok {
		return true
	}
	// Allow PersistentVolumeClaim and Service events when the name matches the pod regex.
	if (kind == "persistentvolumeclaim" || kind == "service") && t.podRegex.MatchString(name) {
		return true
	}
	return false
}

func (t *Tailer) namespaceAllowed(ns string) bool {
	if t.opts.AllNamespaces {
		return true
	}
	if len(t.opts.Namespaces) == 0 {
		return ns == "default"
	}
	for _, allowed := range t.opts.Namespaces {
		if allowed == ns {
			return true
		}
	}
	return false
}

func (t *Tailer) printEvent(ev *corev1.Event) {
	if ev == nil {
		return
	}
	message, container := t.formatEventMessage(ev)
	t.outputLine(sourceEvent, ev.InvolvedObject.Namespace, ev.InvolvedObject.Name, container, message)
}

func (t *Tailer) formatEventMessage(ev *corev1.Event) (string, string) {
	eventType := ev.Type
	if eventType == "" {
		eventType = "Normal"
	}
	typeText := eventType
	if !t.colorsDisabled() {
		if col, ok := t.eventCols[eventType]; ok {
			typeText = col.Sprint(eventType)
		}
	}
	age := humanizeAge(eventTimestamp(ev))
	target := fmt.Sprintf("%s/%s", strings.ToLower(ev.InvolvedObject.Kind), ev.InvolvedObject.Name)
	source := formatEventSource(ev)
	message := fmt.Sprintf("%-7s %-18s %-8s %s -> %s", typeText, ev.Reason, age, target, source)
	return message, ""
}

func eventTimestamp(ev *corev1.Event) time.Time {
	switch {
	case !ev.EventTime.IsZero():
		return ev.EventTime.Time
	case ev.Series != nil && !ev.Series.LastObservedTime.IsZero():
		return ev.Series.LastObservedTime.Time
	case !ev.LastTimestamp.IsZero():
		return ev.LastTimestamp.Time
	default:
		return ev.CreationTimestamp.Time
	}
}

func humanizeAge(ts time.Time) string {
	if ts.IsZero() {
		return "n/a"
	}
	diff := time.Since(ts)
	if diff < time.Second {
		return "now"
	}
	switch {
	case diff < time.Minute:
		return fmt.Sprintf("%ds ago", int(diff.Seconds()))
	case diff < time.Hour:
		return fmt.Sprintf("%dm ago", int(diff.Minutes()))
	case diff < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(diff.Hours()))
	default:
		return fmt.Sprintf("%dd ago", int(diff.Hours()/24))
	}
}

func formatEventSource(ev *corev1.Event) string {
	component := ev.Source.Component
	if component == "" {
		component = ev.ReportingController
	}
	if component == "" {
		component = "unknown"
	}
	if ev.Source.Host != "" {
		component = fmt.Sprintf("%s/%s", component, ev.Source.Host)
	}
	message := strings.TrimSpace(ev.Message)
	if message == "" {
		return component
	}
	return fmt.Sprintf("%s: %s", component, message)
}

// DefaultColorPalette returns the vibrant color rotation used when rendering ktl log streams.
func DefaultColorPalette() []*color.Color {
	return []*color.Color{
		color.New(color.Bold, color.FgHiCyan),
		color.New(color.Bold, color.FgHiMagenta),
		color.New(color.Bold, color.FgHiGreen),
		color.New(color.Bold, color.FgHiYellow),
		color.New(color.Bold, color.FgHiBlue),
		color.New(color.Bold, color.FgHiRed),
		color.New(color.FgHiMagenta, color.BgBlack),
		color.New(color.FgHiBlue, color.BgBlack),
		color.New(color.FgHiGreen, color.BgBlack),
		color.New(color.FgHiCyan, color.BgBlack),
		color.New(color.FgHiYellow, color.BgBlack),
	}
}

func buildCustomPalette(values []string, origin string) ([]*color.Color, error) {
	var palette []*color.Color
	for _, entry := range values {
		for _, seq := range strings.Split(entry, ",") {
			seq = strings.TrimSpace(seq)
			if seq == "" {
				continue
			}
			parts := strings.Split(seq, ";")
			var attrs []color.Attribute
			for _, part := range parts {
				part = strings.TrimSpace(part)
				if part == "" {
					continue
				}
				attrVal, err := strconv.Atoi(part)
				if err != nil {
					return nil, fmt.Errorf("invalid SGR value %q in %s: %w", part, origin, err)
				}
				attrs = append(attrs, color.Attribute(attrVal))
			}
			if len(attrs) == 0 {
				continue
			}
			palette = append(palette, color.New(attrs...))
		}
	}
	if len(palette) == 0 {
		return nil, fmt.Errorf("no valid SGR values provided for %s", origin)
	}
	return palette, nil
}

func normalizeColorList(values []string) []string {
	var filtered []string
	for _, val := range values {
		trimmed := strings.TrimSpace(val)
		if trimmed == "" {
			continue
		}
		filtered = append(filtered, trimmed)
	}
	return filtered
}

func (t *Tailer) colorFor(seed string, palette []*color.Color) *color.Color {
	if len(palette) == 0 {
		return color.New()
	}
	idx := paletteIndex(seed, len(palette))
	return palette[idx]
}

func (t *Tailer) distinctColorFor(seed string, palette []*color.Color, avoid ...*color.Color) *color.Color {
	if len(palette) == 0 {
		return color.New()
	}
	start := paletteIndex(seed, len(palette))
	for offset := 0; offset < len(palette); offset++ {
		idx := (start + offset) % len(palette)
		candidate := palette[idx]
		if !containsColor(avoid, candidate) {
			return candidate
		}
	}
	return palette[start]
}

func paletteIndex(seed string, length int) int {
	if length == 0 {
		return 0
	}
	hasher := fnv.New32a()
	_, _ = hasher.Write([]byte(seed))
	return int(hasher.Sum32()) % length
}

func containsColor(colors []*color.Color, candidate *color.Color) bool {
	if candidate == nil {
		return false
	}
	for _, c := range colors {
		if c == nil {
			continue
		}
		if c == candidate {
			return true
		}
	}
	return false
}

func (t *Tailer) colorsDisabled() bool {
	switch t.opts.ColorMode {
	case "always":
		return false
	case "never":
		return true
	default:
		return color.NoColor
	}
}

func (t *Tailer) restartCountFor(pod *corev1.Pod, container string) int32 {
	for _, status := range pod.Status.ContainerStatuses {
		if status.Name == container {
			return status.RestartCount
		}
	}
	for _, status := range pod.Status.InitContainerStatuses {
		if status.Name == container {
			return status.RestartCount
		}
	}
	return 0
}

func (t *Tailer) allPodContainers(pod *corev1.Pod) []corev1.Container {
	containers := make([]corev1.Container, 0, len(pod.Spec.Containers)+len(pod.Spec.InitContainers))
	containers = append(containers, pod.Spec.InitContainers...)
	containers = append(containers, pod.Spec.Containers...)
	return containers
}
