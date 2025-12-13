// report.go assembles namespace snapshots into ASCII/HTML posture reports.
package report

import (
	"context"
	"fmt"
	"html/template"
	"io"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/resourceutil"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Options controls HTML report generation.
type Options struct {
	Namespaces     []string
	AllNamespaces  bool
	OutputPath     string
	IncludeIngress bool
}

// NewOptions returns report options with defaults.
func NewOptions() *Options {
	return &Options{IncludeIngress: true}
}

// ResolveNamespaces figures out which namespaces to inspect.
func (o *Options) ResolveNamespaces(defaultNamespace string) []string {
	if o.AllNamespaces {
		return nil
	}
	if len(o.Namespaces) > 0 {
		out := make([]string, 0, len(o.Namespaces))
		for _, ns := range o.Namespaces {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				out = append(out, ns)
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if defaultNamespace != "" {
		return []string{defaultNamespace}
	}
	return []string{"default"}
}

// ResolveOutputPath picks the final HTML path.
func (o *Options) ResolveOutputPath(ts time.Time) string {
	if strings.TrimSpace(o.OutputPath) != "" {
		return o.OutputPath
	}
	name := fmt.Sprintf("ktl-report-%s.html", ts.UTC().Format("20060102-150405"))
	return name
}

// Collect gathers pod data for the requested namespaces.
func Collect(ctx context.Context, client *kube.Client, opts *Options) (Data, []string, error) {
	namespaces := opts.ResolveNamespaces(client.Namespace)
	if opts.AllNamespaces {
		list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return Data{}, nil, fmt.Errorf("list namespaces: %w", err)
		}
		namespaces = make([]string, 0, len(list.Items))
		for _, item := range list.Items {
			namespaces = append(namespaces, item.Name)
		}
	}
	if len(namespaces) == 0 {
		return Data{}, nil, fmt.Errorf("no namespaces resolved for report")
	}
	sort.Strings(namespaces)
	reportData, err := collectNamespaces(ctx, client, namespaces, opts)
	if err != nil {
		return Data{}, nil, err
	}
	reportData.GeneratedAt = time.Now().UTC()
	reportData.ClusterServer = client.RESTConfig.Host
	reportData.Context = client.Namespace
	reportData.Scorecard = buildScorecard(ctx, client, namespaces, reportData.Namespaces)
	applyScorecardInsights(&reportData)
	return reportData, namespaces, nil
}

type Data struct {
	GeneratedAt   time.Time
	ClusterServer string
	Context       string
	Namespaces    []namespaceSection
	Summary       summaryStats
	Scorecard     Scorecard
	RecentEvents  []EventEntry
	ArchiveDiff   *ArchiveDiff
}

type summaryStats struct {
	TotalNamespaces int
	TotalPods       int
	ReadyPods       int
	TotalContainers int
	TotalRestarts   int32
}

type namespaceSection struct {
	Name                 string
	Pods                 []podRow
	Ingresses            []ingressRow
	PodSecurityFindings  int
	HasPodSecurityIssues bool
}

type podRow struct {
	Name            string
	Phase           corev1.PodPhase
	Node            string
	Age             string
	ReadyCount      string
	ReadyContainers int
	TotalContainers int
	ReadyRatio      float64
	Restarts        int32
	MaxRestarts     int32
	Containers      []containerRow
	Labels          string
	FilterText      string
	CPUSeries       []float64
	MemSeries       []float64
	RestartSeries   []float64
}

type containerRow struct {
	Name       string
	Image      string
	Ready      bool
	Restarts   int32
	State      string
	Requests   string
	Limits     string
	CPUUsage   string
	MemUsage   string
	CPUPercent float64
	MemPercent float64
}

type ingressRow struct {
	Name       string
	Class      string
	Hosts      string
	Services   string
	TLS        string
	Age        string
	FilterText string
}

// EventEntry represents a summarized Kubernetes event for the report sidebar.
type EventEntry struct {
	Namespace string
	Type      string
	TypeClass string
	Reason    string
	Message   string
	Object    string
	Count     int32
	Timestamp time.Time
	Age       string
}

func collectNamespaces(ctx context.Context, client *kube.Client, namespaces []string, opts *Options) (Data, error) {
	data := Data{}
	data.Namespaces = make([]namespaceSection, 0, len(namespaces))
	summary := summaryStats{}
	metrics := map[string]map[string]usageSample{}
	if client.Metrics != nil {
		metrics = fetchUsage(ctx, client, namespaces)
	}
	for _, ns := range namespaces {
		list, err := client.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return Data{}, fmt.Errorf("list pods in %s: %w", ns, err)
		}
		section := namespaceSection{Name: ns}
		sort.Slice(list.Items, func(i, j int) bool { return list.Items[i].Name < list.Items[j].Name })
		for _, pod := range list.Items {
			row := buildPodRow(pod, metrics)
			section.Pods = append(section.Pods, row)
			summary.TotalPods++
			summary.TotalContainers += len(row.Containers)
			summary.TotalRestarts += row.Restarts
			if strings.HasPrefix(row.ReadyCount, "✅") {
				summary.ReadyPods++
			}
		}
		if opts == nil || opts.IncludeIngress {
			ingList, err := client.Clientset.NetworkingV1().Ingresses(ns).List(ctx, metav1.ListOptions{})
			if err == nil {
				section.Ingresses = buildIngressRows(ingList.Items)
			}
		}
		data.Namespaces = append(data.Namespaces, section)
	}
	summary.TotalNamespaces = len(namespaces)
	data.Summary = summary
	return data, nil
}

// CollectRecentEvents returns up to limit most recent events in the supplied namespaces.
func CollectRecentEvents(ctx context.Context, client *kube.Client, namespaces []string, limit int) ([]EventEntry, error) {
	if limit <= 0 {
		limit = 50
	}
	events := make([]EventEntry, 0, limit)
	for _, ns := range namespaces {
		list, err := client.Clientset.CoreV1().Events(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list events in %s: %w", ns, err)
		}
		for _, ev := range list.Items {
			ts := resolveEventTimestamp(ev)
			entry := EventEntry{
				Namespace: ns,
				Type:      ev.Type,
				TypeClass: strings.ToLower(ev.Type),
				Reason:    ev.Reason,
				Message:   strings.TrimSpace(ev.Message),
				Object:    formatEventObject(ev),
				Count:     ev.Count,
				Timestamp: ts,
			}
			events = append(events, entry)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Timestamp.After(events[j].Timestamp)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return events, nil
}

func resolveEventTimestamp(ev corev1.Event) time.Time {
	switch {
	case !ev.EventTime.IsZero():
		return ev.EventTime.Time
	case ev.Series != nil && !ev.Series.LastObservedTime.IsZero():
		return ev.Series.LastObservedTime.Time
	case !ev.LastTimestamp.IsZero():
		return ev.LastTimestamp.Time
	case !ev.CreationTimestamp.IsZero():
		return ev.CreationTimestamp.Time
	default:
		return time.Time{}
	}
}

func formatEventObject(ev corev1.Event) string {
	kind := strings.ToLower(ev.InvolvedObject.Kind)
	if kind == "" {
		kind = "object"
	}
	if ev.InvolvedObject.Name == "" {
		return kind
	}
	if ev.InvolvedObject.Namespace != "" && !strings.EqualFold(ev.InvolvedObject.Namespace, ev.Namespace) {
		return fmt.Sprintf("%s/%s/%s", ev.InvolvedObject.Namespace, kind, ev.InvolvedObject.Name)
	}
	return fmt.Sprintf("%s/%s", kind, ev.InvolvedObject.Name)
}

func buildPodRow(pod corev1.Pod, metrics map[string]map[string]usageSample) podRow {
	readyContainers := 0
	totalContainers := len(pod.Spec.Containers)
	restarts := int32(0)
	statusMap := map[string]corev1.ContainerStatus{}
	for _, cs := range pod.Status.ContainerStatuses {
		statusMap[cs.Name] = cs
		restarts += cs.RestartCount
		if cs.Ready {
			readyContainers++
		}
	}
	containers := make([]containerRow, 0, totalContainers)
	seriesCPU := []float64{}
	seriesMem := []float64{}
	seriesRestarts := []float64{}
	metricMap := metrics[keyForPod(pod.Namespace, pod.Name)]
	maxRestarts := int32(0)
	for _, container := range pod.Spec.Containers {
		cs := statusMap[container.Name]
		usage := metricMap[container.Name]
		cpuUsage := humanCPU(usage.CPU)
		memUsage := humanMemory(usage.Memory)
		cpuPct := percentOf(usage.CPU, resourceutil.FromResourceList(container.Resources.Requests, corev1.ResourceCPU, resourceutil.QuantityMilli))
		memPct := percentOf(usage.Memory, resourceutil.FromResourceList(container.Resources.Requests, corev1.ResourceMemory, resourceutil.QuantityInt))
		currentRestarts := cs.RestartCount
		if currentRestarts > maxRestarts {
			maxRestarts = currentRestarts
		}
		containers = append(containers, containerRow{
			Name:       container.Name,
			Image:      container.Image,
			Ready:      cs.Ready,
			Restarts:   currentRestarts,
			State:      describeState(cs.State),
			Requests:   formatResources(container.Resources.Requests),
			Limits:     formatResources(container.Resources.Limits),
			CPUUsage:   cpuUsage,
			MemUsage:   memUsage,
			CPUPercent: cpuPct,
			MemPercent: memPct,
		})
		if cpuPct > 0 {
			seriesCPU = append(seriesCPU, cpuPct)
		}
		if memPct > 0 {
			seriesMem = append(seriesMem, memPct)
		}
		seriesRestarts = append(seriesRestarts, float64(cs.RestartCount))
	}
	labels := formatLabels(pod.Labels)
	filterParts := []string{pod.Name, pod.Namespace, pod.Spec.NodeName, labels, string(pod.Status.Phase)}
	for _, c := range containers {
		filterParts = append(filterParts, c.Name, c.Image)
	}
	readyRatio := 1.0
	if totalContainers > 0 {
		readyRatio = float64(readyContainers) / float64(totalContainers)
	}
	return podRow{
		Name:            pod.Name,
		Phase:           pod.Status.Phase,
		Node:            pod.Spec.NodeName,
		Age:             HumanDuration(time.Since(pod.CreationTimestamp.Time)),
		ReadyCount:      fmt.Sprintf("%s %d/%d", readinessEmoji(readyContainers, totalContainers), readyContainers, totalContainers),
		ReadyContainers: readyContainers,
		TotalContainers: totalContainers,
		ReadyRatio:      readyRatio,
		Restarts:        restarts,
		MaxRestarts:     maxRestarts,
		Containers:      containers,
		Labels:          labels,
		FilterText:      strings.ToLower(strings.Join(filterParts, " ")),
		CPUSeries:       seriesCPU,
		MemSeries:       seriesMem,
		RestartSeries:   seriesRestarts,
	}
}

type usageSample struct {
	CPU    int64
	Memory int64
}

func buildIngressRows(items []networkingv1.Ingress) []ingressRow {
	rows := make([]ingressRow, 0, len(items))
	for _, ing := range items {
		hosts := collectIngressHosts(ing)
		services := collectIngressServices(ing)
		className := ""
		if ing.Spec.IngressClassName != nil {
			className = *ing.Spec.IngressClassName
		} else if v := ing.Annotations["kubernetes.io/ingress.class"]; v != "" {
			className = v
		}
		tls := "No"
		if len(ing.Spec.TLS) > 0 {
			tls = fmt.Sprintf("Yes (%d)", len(ing.Spec.TLS))
		}
		filter := strings.ToLower(strings.Join([]string{
			ing.Name,
			ing.Namespace,
			className,
			hosts,
			services,
			tls,
		}, " "))
		rows = append(rows, ingressRow{
			Name:       ing.Name,
			Class:      emptyDash(className),
			Hosts:      hosts,
			Services:   services,
			TLS:        tls,
			Age:        HumanDuration(time.Since(ing.CreationTimestamp.Time)),
			FilterText: filter,
		})
	}
	return rows
}

func fetchUsage(ctx context.Context, client *kube.Client, namespaces []string) map[string]map[string]usageSample {
	results := make(map[string]map[string]usageSample)
	for _, ns := range namespaces {
		metrics, err := client.Metrics.MetricsV1beta1().PodMetricses(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			continue
		}
		for _, item := range metrics.Items {
			key := keyForPod(item.Namespace, item.Name)
			if _, ok := results[key]; !ok {
				results[key] = make(map[string]usageSample)
			}
			for _, container := range item.Containers {
				cpu := resourceutil.QuantityMilli(container.Usage[corev1.ResourceCPU])
				mem := resourceutil.QuantityInt(container.Usage[corev1.ResourceMemory])
				results[key][container.Name] = usageSample{CPU: cpu, Memory: mem}
			}
		}
	}
	return results
}

func keyForPod(ns, pod string) string {
	return ns + "/" + pod
}

func readinessEmoji(ready, total int) string {
	if total == 0 {
		return "⚪"
	}
	if ready == total {
		return "✅"
	}
	if ready == 0 {
		return "⚠️"
	}
	return "⚡"
}

func describeState(state corev1.ContainerState) string {
	switch {
	case state.Running != nil:
		return "running"
	case state.Waiting != nil:
		if state.Waiting.Reason != "" {
			return "waiting: " + state.Waiting.Reason
		}
		return "waiting"
	case state.Terminated != nil:
		if state.Terminated.Reason != "" {
			return "terminated: " + state.Terminated.Reason
		}
		return "terminated"
	default:
		return "unknown"
	}
}

func formatResources(list corev1.ResourceList) string {
	if len(list) == 0 {
		return "-"
	}
	parts := []string{}
	if qty, ok := list[corev1.ResourceCPU]; ok && !qty.IsZero() {
		parts = append(parts, "cpu="+qty.String())
	}
	if qty, ok := list[corev1.ResourceMemory]; ok && !qty.IsZero() {
		parts = append(parts, "mem="+qty.String())
	}
	if qty, ok := list[corev1.ResourceEphemeralStorage]; ok && !qty.IsZero() {
		parts = append(parts, "ephem="+qty.String())
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ", ")
}

func formatLabels(labels map[string]string) string {
	if len(labels) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", k, labels[k]))
	}
	return strings.Join(parts, ", ")
}

// HumanDuration converts a duration into a compact, human-readable string (e.g. "5m 3s").
func HumanDuration(d time.Duration) string {
	if d < 0 {
		d = -d
	}
	if d < time.Minute {
		return "<1m"
	}
	units := []struct {
		dur  time.Duration
		name string
	}{
		{24 * time.Hour, "d"},
		{time.Hour, "h"},
		{time.Minute, "m"},
	}
	var parts []string
	remain := d
	for _, unit := range units {
		if remain >= unit.dur {
			value := remain / unit.dur
			remain -= value * unit.dur
			parts = append(parts, fmt.Sprintf("%d%s", value, unit.name))
			if len(parts) == 2 {
				break
			}
		}
	}
	if len(parts) == 0 {
		return "<1m"
	}
	return strings.Join(parts, " ")
}

func collectIngressHosts(ing networkingv1.Ingress) string {
	set := map[string]struct{}{}
	for _, rule := range ing.Spec.Rules {
		host := rule.Host
		if host == "" {
			host = "*"
		}
		set[host] = struct{}{}
	}
	if len(set) == 0 {
		return "*"
	}
	hosts := make([]string, 0, len(set))
	for host := range set {
		hosts = append(hosts, host)
	}
	sort.Strings(hosts)
	return strings.Join(hosts, ", ")
}

func collectIngressServices(ing networkingv1.Ingress) string {
	set := map[string]struct{}{}
	addService := func(svc *networkingv1.IngressServiceBackend) {
		if svc == nil {
			return
		}
		name := svc.Name
		port := ""
		if svc.Port.Name != "" {
			port = svc.Port.Name
		} else if svc.Port.Number != 0 {
			port = fmt.Sprintf("%d", svc.Port.Number)
		}
		key := name
		if port != "" {
			key = fmt.Sprintf("%s:%s", name, port)
		}
		set[key] = struct{}{}
	}
	if ing.Spec.DefaultBackend != nil {
		addService(ing.Spec.DefaultBackend.Service)
	}
	for _, rule := range ing.Spec.Rules {
		if rule.HTTP == nil {
			continue
		}
		for _, path := range rule.HTTP.Paths {
			addService(path.Backend.Service)
		}
	}
	if len(set) == 0 {
		return "-"
	}
	services := make([]string, 0, len(set))
	for svc := range set {
		services = append(services, svc)
	}
	sort.Strings(services)
	return strings.Join(services, ", ")
}

func emptyDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func printSimpleTable(w io.Writer, headers []string, rows [][]string) {
	if len(headers) == 0 {
		return
	}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for j, col := range row {
			if len(col) > widths[j] {
				widths[j] = len(col)
			}
		}
	}
	for i, h := range headers {
		fmt.Fprintf(w, "%-*s  ", widths[i], strings.ToUpper(h))
	}
	fmt.Fprintln(w)
	for i := range headers {
		fmt.Fprintf(w, "%s  ", strings.Repeat("-", widths[i]))
	}
	fmt.Fprintln(w)
	for _, row := range rows {
		for j := range headers {
			val := ""
			if j < len(row) {
				val = row[j]
			}
			fmt.Fprintf(w, "%-*s  ", widths[j], val)
		}
		fmt.Fprintln(w)
	}
	fmt.Fprintln(w)
}

func RenderHTML(data Data) (string, error) {
	tmpl := template.Must(template.New("report").Parse(htmlTemplate))
	var builder strings.Builder
	if err := tmpl.Execute(&builder, data); err != nil {
		return "", err
	}
	return builder.String(), nil
}

func humanCPU(milli int64) string {
	if milli <= 0 {
		return "-"
	}
	if milli >= 1000 {
		return fmt.Sprintf("%.2f", float64(milli)/1000) + " cores"
	}
	return fmt.Sprintf("%dm", milli)
}

func humanMemory(bytes int64) string {
	if bytes <= 0 {
		return "-"
	}
	units := []struct {
		name  string
		value float64
	}{
		{"GiB", 1024 * 1024 * 1024},
		{"MiB", 1024 * 1024},
		{"KiB", 1024},
	}
	b := float64(bytes)
	for _, unit := range units {
		if b >= unit.value {
			return fmt.Sprintf("%.2f %s", b/unit.value, unit.name)
		}
	}
	return fmt.Sprintf("%d B", bytes)
}

func percentOf(value int64, baseline int64) float64 {
	if value <= 0 || baseline <= 0 {
		return 0
	}
	return math.Min(100, (float64(value)/float64(baseline))*100)
}

const htmlTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <title>ktl Namespace Report</title>
  <style>
    :root {
      color-scheme: light;
      --surface: rgba(255,255,255,0.9);
      --surface-soft: rgba(255,255,255,0.82);
      --border: rgba(15,23,42,0.12);
      --text: #0f172a;
      --muted: rgba(15,23,42,0.65);
      --accent: #2563eb;
      --chip-bg: rgba(37,99,235,0.08);
      --chip-text: #1d4ed8;
      --sparkline-color: #0ea5e9;
      --warn: #fbbf24;
      --fail: #ef4444;
    }
    * { box-sizing: border-box; }
    body {
      font-family: "SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0;
      min-height: 100vh;
      padding: 48px 56px 72px;
      background: radial-gradient(circle at 20% 20%, #ffffff, #e9edf5 45%, #dce3f1);
      color: var(--text);
    }
    body.print-mode {
      background: #ffffff !important;
      color: #000 !important;
    }
    .chrome { max-width: 1600px; margin: 0 auto; }
    header { margin-bottom: 32px; }
    h1 { font-size: 2.8rem; font-weight: 600; letter-spacing: -0.04em; margin: 0 0 0.3rem; }
    .subtitle { font-size: 1rem; color: var(--muted); letter-spacing: 0.02em; }
    .subtitle strong { color: var(--text); }
    .layout { display:flex; gap:22px; align-items:flex-start; }
    .main-column { flex:1 1 auto; min-width:0; }
    .insight-stack { width:462px; position:sticky; top:32px; display:flex; flex-direction:column; gap:24px; }
    @media (max-width: 1400px) {
      .layout { flex-direction:column; }
      .insight-stack { width:100%; position:static; }
    }
    .panel {
      border-radius: 28px;
      padding: 32px;
      background: var(--surface);
      border: 1px solid var(--border);
      backdrop-filter: blur(18px);
      box-shadow: 0 40px 80px rgba(16,23,36,0.12);
      margin-bottom: 32px;
    }
    .toolbar { display:flex; flex-wrap:wrap; gap:16px; align-items:flex-start; }
    .toolbar input {
      flex: 1 1 320px;
      border-radius: 999px;
      border: 1px solid rgba(15,23,42,0.18);
      padding: 0.75rem 1.25rem;
      font-size: 1rem;
      background: rgba(255,255,255,0.9);
      box-shadow: inset 0 1px 3px rgba(11,15,20,0.08);
      transition: border 0.2s ease, box-shadow 0.2s ease;
    }
    .toolbar input:focus { outline: none; border-color: var(--accent); box-shadow: 0 0 0 3px rgba(37,99,235,0.18); }
    .chip-row { display:flex; flex-wrap:wrap; gap:8px; align-items:center; }
    .chip {
      border-radius: 999px;
      border: 1px solid transparent;
      background: var(--chip-bg);
      color: var(--chip-text);
      padding: 0.35rem 0.9rem;
      font-size: 0.85rem;
      font-weight: 600;
      cursor: pointer;
      transition: transform 0.15s ease, box-shadow 0.2s ease;
    }
    .chip.active {
      border-color: var(--chip-text);
      box-shadow: 0 0 0 2px rgba(37,99,235,0.2);
      transform: translateY(-1px);
    }
    .grid { display:grid; gap:1.1rem; grid-template-columns: repeat(auto-fit, minmax(180px,1fr)); }
    .card {
      padding: 1.25rem 1.5rem;
      border-radius: 24px;
      background: rgba(255,255,255,0.92);
      border: 1px solid rgba(15,23,42,0.08);
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.6);
    }
    .card span { display:block; font-size:0.78rem; letter-spacing:0.2em; text-transform:uppercase; color:var(--muted); }
    .card strong { display:block; font-size:2.2rem; font-weight:570; margin-top:0.35rem; letter-spacing:-0.04em; }
    .diff-panel h2 { margin:0; }
    .diff-header { display:flex; flex-wrap:wrap; justify-content:space-between; gap:12px; align-items:center; }
    .diff-summary { margin-top:1rem; }
    .diff-summary .card { border-left:4px solid var(--border); }
    .diff-summary .card.added { border-left-color:#22c55e; }
    .diff-summary .card.removed { border-left-color:var(--fail); }
    .diff-summary .card.changed { border-left-color:var(--warn); }
    .diff-list { margin-top:1.5rem; display:flex; flex-direction:column; gap:18px; }
    .diff-item {
      border:1px solid var(--border);
      border-radius:24px;
      padding:1.5rem;
      background:var(--surface-soft);
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.4);
    }
    .diff-item header { display:flex; justify-content:space-between; flex-wrap:wrap; gap:0.5rem; }
    .diff-item.added { border-left:4px solid #22c55e; }
    .diff-item.removed { border-left:4px solid var(--fail); }
    .diff-item.changed { border-left:4px solid var(--warn); }
    .diff-meta-chip {
      font-size:0.8rem;
      text-transform:uppercase;
      letter-spacing:0.12em;
      color:var(--muted);
    }
    .diff-highlights {
      list-style:none;
      padding:0;
      margin:0.75rem 0;
      display:flex;
      flex-direction:column;
      gap:0.35rem;
    }
    .diff-highlights li {
      display:flex;
      flex-wrap:wrap;
      gap:0.4rem;
      font-size:0.9rem;
      color:var(--text);
    }
    .diff-highlights .label {
      font-weight:600;
      color:var(--muted);
      min-width:120px;
    }
    .diff-highlights .arrow { color:var(--muted); }
    .diff-snippet {
      width:100%;
      overflow:auto;
      background:#0f172a;
      color:#e2e8f0;
      border-radius:16px;
      padding:0.75rem 1rem;
      font-size:0.85rem;
      line-height:1.3;
      margin:0.75rem 0 0;
    }
    .diff-empty { margin-top:1rem; }
    .score-grid { display:grid; gap:1rem; grid-template-columns: repeat(auto-fit, minmax(260px,1fr)); }
    .score-card {
      padding: 1.25rem 1.5rem;
      border-radius: 24px;
      border: 1px solid rgba(15,23,42,0.18);
      background: var(--surface-soft);
      display:flex;
      flex-direction:column;
      gap:0.75rem;
    }
    .score-meta { display:flex; justify-content:space-between; align-items:center; gap:1rem; }
    .score-delta { margin:0; font-size:0.85rem; color:var(--muted); }
    .score-card strong { font-size:2rem; letter-spacing:-0.03em; }
    .sparkline { width:100%; height:36px; }
    .budget-bar { height:6px; background:rgba(15,23,42,0.15); border-radius:999px; overflow:hidden; }
    .budget-bar span { display:block; height:100%; background:linear-gradient(90deg,#22c55e,#16a34a); }
    .score-card.warn .budget-bar span { background:linear-gradient(90deg,#fbbf24,#f97316); }
    .score-card.fail .budget-bar span { background:linear-gradient(90deg,#fb7185,#ef4444); }
    .cta-row { display:flex; gap:8px; flex-wrap:wrap; }
    .cta {
      border-radius:12px;
      border:1px solid var(--border);
      background:rgba(0,0,0,0.02);
      color:var(--text);
      padding:0.35rem 0.8rem;
      font-size:0.85rem;
      cursor:pointer;
    }
    .score-drilldown summary { font-size:1.4rem; }
    .drill-summary {
      border:none;
      background:none;
      padding:0;
      margin:0 auto;
      display:flex;
      align-items:center;
      justify-content:center;
      line-height:1;
      color:#4b5563;
      cursor:pointer;
    }
    .drill-summary::-webkit-details-marker { display:none; }
    .drill-summary::marker { content: none; }
    .drill-summary:focus-visible {
      outline:2px solid #111;
      outline-offset:4px;
    }
    .glyph-chevron {
      width:32px;
      height:32px;
      border-radius:999px;
      border:1px solid rgba(37,99,235,0.2);
      background:rgba(37,99,235,0.07);
      box-shadow: inset 0 1px 0 rgba(255,255,255,0.6);
      display:inline-flex;
      align-items:center;
      justify-content:center;
      transition:background 0.2s ease, border-color 0.2s ease;
    }
    .glyph-chevron::before {
      content:"";
      width:18px;
      height:3px;
      border-radius:999px;
      background:linear-gradient(90deg, #2563eb, #0ea5e9);
      box-shadow:0 0 6px rgba(14,165,233,0.35);
      transition:background 0.2s ease, box-shadow 0.2s ease;
    }
    details[open] .glyph-chevron {
      border-color:rgba(15,23,42,0.15);
      background:rgba(15,23,42,0.04);
    }
    details[open] .glyph-chevron::before {
      background:rgba(15,23,42,0.35);
      box-shadow:none;
    }
    .sr-only {
      position:absolute;
      width:1px;
      height:1px;
      padding:0;
      margin:-1px;
      overflow:hidden;
      clip:rect(0,0,0,0);
      border:0;
    }
    details.namespace { margin-bottom: 16px; border-radius: 28px; border: 1px solid var(--border); background: var(--surface); backdrop-filter: blur(18px); }
    details.namespace[open] { box-shadow: 0 25px 60px rgba(15,23,42,0.18); }
    details.namespace summary {
      list-style:none;
      cursor:pointer;
      padding: 24px 32px;
      display:flex;
      align-items:center;
      justify-content:space-between;
      gap:16px;
    }
    details.namespace summary::marker,
    details.namespace summary::-webkit-details-marker {
      display:none;
    }
    details.namespace .summary-meta { color: var(--muted); font-size:0.9rem; margin:4px 0 0; }
    .chevron {
      width:22px;
      height:22px;
      display:inline-flex;
      align-items:center;
      justify-content:center;
      transition:transform 0.2s ease;
    }
    .chevron svg {
      width:14px;
      height:14px;
      stroke:#111;
      stroke-width:2.2;
      fill:none;
      stroke-linecap:round;
      stroke-linejoin:round;
    }
    details[open] .chevron {
      transform:rotate(90deg);
    }
    details.namespace > div.content { padding:0 32px 28px 32px; display:flex; flex-direction:column; gap:24px; }
    .section-label { font-size:0.9rem; letter-spacing:0.18em; text-transform:uppercase; color:var(--muted); margin:0; }
    .table-wrap { border-radius:28px; overflow:hidden; border:1px solid rgba(15,23,42,0.08); }
    table { width:100%; border-collapse:separate; border-spacing:0; }
    thead th {
      font-size:0.75rem;
      letter-spacing:0.18em;
      text-transform:uppercase;
      font-weight:500;
      color:var(--muted);
      padding:0.85rem 1rem;
      background:rgba(255,255,255,0.8);
      border-bottom:1px solid rgba(15,23,42,0.08);
    }
    tbody td {
      padding:1.05rem 1rem;
      border-bottom:1px solid rgba(15,23,42,0.06);
      background:rgba(255,255,255,0.95);
    }
    tbody tr.hidden { display:none; }
    .badge {
      display:inline-flex;
      align-items:center;
      font-size:0.72rem;
      border-radius:999px;
      padding:0.15rem 0.65rem;
      margin-right:0.35rem;
      border:1px solid rgba(15,23,42,0.08);
    }
    .badge.ready { background:rgba(7,186,115,0.12); color:#047857; border-color:rgba(7,186,115,0.25); }
    .badge.pending { background:rgba(253,186,116,0.18); color:#92400e; border-color:rgba(253,186,116,0.35); }
    .insight-panel {
      border-radius:20px;
      border:1px solid rgba(15,23,42,0.1);
      background:rgba(255,255,255,0.95);
      padding:20px;
      box-shadow:0 18px 40px rgba(15,23,42,0.12);
    }
    .insight-panel h3 { margin:0 0 0.75rem; font-size:1rem; }
    .timeline { list-style:none; margin:0; padding:0; display:flex; flex-direction:column; gap:16px; }
    .timeline li { display:flex; gap:12px; align-items:flex-start; }
    .timeline .dot { width:10px; height:10px; border-radius:999px; margin-top:6px; background:var(--accent); box-shadow:0 0 0 4px rgba(37,99,235,0.15); }
    .timeline li.warn .dot { background: var(--warn); box-shadow:0 0 0 4px rgba(251,191,36,0.25); }
    .timeline li.fail .dot { background: var(--fail); box-shadow:0 0 0 4px rgba(239,68,68,0.25); }
    .budget-grid { display:flex; flex-direction:column; gap:12px; }
    .budget-widget {
      border:1px solid rgba(15,23,42,0.1);
      border-radius:16px;
      padding:12px;
      display:flex;
      gap:12px;
      align-items:center;
      background:#fff;
      cursor:pointer;
    }
    .budget-widget.active { border-color: var(--accent); box-shadow:0 0 0 2px rgba(37,99,235,0.15); }
    .donut {
      width:54px;
      height:54px;
      border-radius:50%;
      background: conic-gradient(var(--accent) calc(var(--usage) * 1%), rgba(15,23,42,0.08) 0);
      position:relative;
    }
    .donut::after {
      content:"";
      position:absolute;
      inset:12px;
      border-radius:50%;
      background:#fff;
    }
    .donut span {
      position:absolute;
      inset:0;
      display:flex;
      align-items:center;
      justify-content:center;
      font-size:0.75rem;
      font-weight:600;
    }
    .runbook-card {
      border:1px solid rgba(15,23,42,0.1);
      border-radius:16px;
      padding:12px 14px;
      margin-bottom:12px;
      background:#fff;
    }
    .runbook-card:last-child { margin-bottom:0; }
    .runbook-card p { margin:0 0 0.6rem; font-size:0.9rem; color:var(--muted); }
    .runbook-card .cta { margin-top:8px; }
    .runbook-card a { font-size:0.85rem; color:var(--accent); text-decoration:none; }
    .events-note { margin:0 0 0.5rem; font-size:0.9rem; letter-spacing:0.18em; text-transform:uppercase; color:var(--muted); }
    .events-scroll {
      max-height:676px;
      overflow-y:auto;
      display:flex;
      flex-direction:column;
      gap:14px;
      padding-right:8px;
    }
    .event-item {
      border-left:3px solid rgba(15,23,42,0.12);
      padding-left:12px;
      display:flex;
      flex-direction:column;
      gap:0.3rem;
    }
    .event-meta {
      display:flex;
      justify-content:space-between;
      font-size:0.72rem;
      letter-spacing:0.15em;
      text-transform:uppercase;
      color:var(--muted);
    }
    .event-type { font-weight:600; }
    .event-type.normal { color:#16a34a; }
    .event-type.warning { color:var(--warn); }
    .event-type.error { color:var(--fail); }
    .event-reason { margin:0; font-weight:600; font-size:0.95rem; }
    .event-object { font-size:0.82rem; color:var(--muted); }
    .event-message { margin:0; font-size:0.9rem; color:var(--text); }
    .toast {
      position:fixed;
      bottom:24px;
      right:24px;
      padding:0.6rem 1.2rem;
      border-radius:12px;
      background:var(--surface);
      border:1px solid var(--border);
      box-shadow:0 12px 30px rgba(0,0,0,0.15);
      opacity:0;
      transform: translateY(10px);
      transition: opacity 0.2s ease, transform 0.2s ease;
      pointer-events:none;
    }
    .toast.visible { opacity:1; transform: translateY(0); }
    @media print {
      .layout { flex-direction:column; }
      .insight-stack { display:none; }
      .panel, details.namespace { box-shadow:none !important; border-color:#000 !important; }
      .toolbar, .chip-row, .cta-row, #copyToast { display:none !important; }
    }
  </style>
</head>
<body>
  <div class="chrome">
    <header>
      <h1>ktl Namespace Report</h1>
      <div class="subtitle"><strong>{{.GeneratedAt.Format "02 Jan 2006"}}</strong> at {{.GeneratedAt.Format "15:04 MST"}} · Cluster endpoint <strong>{{.ClusterServer}}</strong></div>
    </header>
    <div class="layout">
      <div class="main-column">
        <div class="panel">
          <div class="toolbar">
            <input id="podFilter" type="search" placeholder="Filter pods, containers, labels, nodes" />
            <div class="chip-row"></div>
          </div>
          <div class="grid" style="margin-top:24px;">
            <div class="card"><span>Namespaces</span><strong>{{.Summary.TotalNamespaces}}</strong></div>
            <div class="card"><span>Total Pods</span><strong>{{.Summary.TotalPods}}</strong></div>
            <div class="card"><span>Ready Pods</span><strong>{{.Summary.ReadyPods}}</strong></div>
            <div class="card"><span>Containers</span><strong>{{.Summary.TotalContainers}}</strong></div>
            <div class="card"><span>Total Restarts</span><strong>{{.Summary.TotalRestarts}}</strong></div>
          </div>
        </div>
        {{if .ArchiveDiff}}
        <div class="panel diff-panel">
          <div class="diff-header">
            <div>
              <h2>Snapshot Drift</h2>
              <p class="summary-meta">{{.ArchiveDiff.Left.Snapshot}} → {{.ArchiveDiff.Right.Snapshot}}</p>
            </div>
            <div class="diff-meta-chip">{{.ArchiveDiff.Left.ManifestCount}} → {{.ArchiveDiff.Right.ManifestCount}} manifests</div>
          </div>
          <div class="grid diff-summary">
            <div class="card added">
              <span>Added</span>
              <strong>{{.ArchiveDiff.Summary.Added}}</strong>
            </div>
            <div class="card removed">
              <span>Removed</span>
              <strong>{{.ArchiveDiff.Summary.Removed}}</strong>
            </div>
            <div class="card changed">
              <span>Changed</span>
              <strong>{{.ArchiveDiff.Summary.Changed}}</strong>
            </div>
          </div>
          {{if .ArchiveDiff.Resources}}
          <div class="diff-list">
            {{range .ArchiveDiff.Resources}}
            <article class="diff-item {{.Change}}">
              <header>
                <div>
                  <h3 style="margin:0;">{{.Kind}} · {{.Name}}</h3>
                  <p class="summary-meta">{{if .Namespace}}{{.Namespace}}{{else}}cluster-wide{{end}}</p>
                </div>
                <span class="diff-meta-chip">{{.Change}}</span>
              </header>
              {{if .Highlights}}
              <ul class="diff-highlights">
                {{range .Highlights}}
                <li>
                  <span class="label">{{.Label}}</span>
                  <span class="before">{{.Before}}</span>
                  <span class="arrow">→</span>
                  <span class="after">{{.After}}</span>
                </li>
                {{end}}
              </ul>
              {{end}}
              {{if .Diff}}
              <pre class="diff-snippet">{{.Diff}}</pre>
              {{end}}
              {{if .RollbackCommand}}
              <div class="cta-row">
                <button class="cta copy" type="button" data-command="{{.RollbackCommand}}">Copy rollback</button>
              </div>
              {{end}}
            </article>
            {{end}}
          </div>
          {{else}}
          <p class="summary-meta diff-empty">No manifest drift detected between the selected snapshots.</p>
          {{end}}
        </div>
        {{end}}
        {{if .Scorecard.Checks}}
        <div class="panel scorecard">
          <div style="margin-bottom:1rem;">
            <h2 style="margin:0;">Health Scorecard</h2>
            <p class="summary-meta">Composite average {{printf "%.1f" .Scorecard.Average}}%</p>
          </div>
          <div class="score-grid">
            {{range .Scorecard.Checks}}
            <div class="score-card {{.Status}}" data-score-key="{{.Key}}">
              <div class="score-meta">
                <div>
                  <h3>{{.Name}}</h3>
                  {{if ne .Delta 0.0}}<p class="score-delta">{{if gt .Delta 0.0}}▲{{else}}▼{{end}} {{printf "%+.1f" .Delta}} pts vs last</p>{{else}}<p class="score-delta">No change</p>{{end}}
                </div>
                <strong>{{printf "%.0f%%" .Score}}</strong>
              </div>
              {{if .TrendEncoded}}<canvas class="sparkline" width="140" height="36" data-trend="{{.TrendEncoded}}"></canvas>{{end}}
              <div class="budget-bar"><span style="width: {{printf "%.0f" .Score}}%;"></span></div>
              <p>{{.Summary}}</p>
          {{if .Details}}
          <div class="cta-row">
            <details id="drilldown-{{.Key}}" class="score-drilldown">
	              <summary class="drill-summary" title="Toggle details" aria-label="Toggle details">
	                <span class="glyph-chevron" aria-hidden="true"></span>
                <span class="sr-only">Toggle details</span>
              </summary>
              <ul>
                {{range .Details}}
                <li>{{.}}</li>
                {{end}}
              </ul>
            </details>
          </div>
          {{end}}
            </div>
            {{end}}
          </div>
        </div>
        {{end}}
      {{range $ns := .Namespaces}}
      <details class="panel namespace" data-namespace="{{$ns.Name}}">
          <summary>
            <div>
        <h2>{{$ns.Name}}</h2>
        <p class="summary-meta">{{len $ns.Pods}} pods</p>
            </div>
            <span class="chevron" aria-hidden="true">
              <svg viewBox="0 0 24 24" aria-hidden="true" focusable="false">
                <path d="M9 6l6 6-6 6"/>
              </svg>
            </span>
          </summary>
          <div class="content">
          {{if $ns.Pods}}
          <div class="table-wrap">
          <table>
            <thead>
              <tr>
                <th>Pod</th>
                <th>Phase</th>
                <th>Node</th>
                <th>Ready</th>
                <th>Restarts</th>
                <th>Age</th>
                <th>Containers</th>
              </tr>
            </thead>
            <tbody>
              {{range $pod := $ns.Pods}}
              <tr data-filter="{{$pod.FilterText}}" data-ready="{{printf "%.2f" $pod.ReadyRatio}}" data-max-restart="{{$pod.MaxRestarts}}">
                <td>
                  <div class="pod-name">{{$pod.Name}}</div>
                  <div class="labels">{{$pod.Labels}}</div>
                </td>
                <td class="phase">{{$pod.Phase}}</td>
                <td>{{$pod.Node}}</td>
                <td>{{$pod.ReadyCount}}</td>
                <td>{{$pod.Restarts}}</td>
                <td>{{$pod.Age}}</td>
                <td class="containers">
                  <ul>
                    {{range $pod.Containers}}
                    <li>
                      <strong>{{.Name}}</strong> <span class="badge {{if .Ready}}ready{{else}}pending{{end}}">{{if .Ready}}Ready{{else}}Not Ready{{end}}</span>
                      <div>State: {{.State}}, Restarts: {{.Restarts}}</div>
                      <div>Image: {{.Image}}</div>
                      <div>Requests: {{.Requests}}</div>
                      <div>Limits: {{.Limits}}</div>
                      <div>Usage: CPU {{.CPUUsage}} ({{printf "%.0f" .CPUPercent}}%), Mem {{.MemUsage}} ({{printf "%.0f" .MemPercent}}%)</div>
                    </li>
                    {{end}}
                  </ul>
                </td>
              </tr>
              {{end}}
            </tbody>
          </table>
          </div>
          {{else}}
            <p>No pods found.</p>
          {{end}}
          {{if $ns.Ingresses}}
          <div>
            <h3 class="section-label">Ingress</h3>
            <div class="table-wrap">
              <table>
                <thead>
                  <tr>
                    <th>Name</th>
                    <th>Class</th>
                    <th>Hosts</th>
                    <th>Services</th>
                    <th>TLS</th>
                    <th>Age</th>
                  </tr>
                </thead>
                <tbody>
                  {{range $ns.Ingresses}}
                  <tr data-filter="{{.FilterText}}">
                    <td>{{.Name}}</td>
                    <td>{{.Class}}</td>
                    <td>{{.Hosts}}</td>
                    <td>{{.Services}}</td>
                    <td>{{.TLS}}</td>
                    <td>{{.Age}}</td>
                  </tr>
                  {{end}}
                </tbody>
              </table>
            </div>
          </div>
          {{end}}
          </div>
        </details>
        {{end}}
      </div>
      <aside class="insight-stack">
        <div class="insight-panel events-panel">
          <p class="events-note">Last 50 events</p>
          <div class="events-scroll">
            {{if .RecentEvents}}
              {{range .RecentEvents}}
              <article class="event-item">
                <div class="event-meta">
                  <span class="event-type {{.TypeClass}}">{{.Type}}</span>
                  <span class="event-age">{{if .Age}}{{.Age}} ago{{else}}-{{end}}</span>
                </div>
                <div class="event-reason">{{.Reason}}</div>
                <div class="event-object">{{.Namespace}} · {{.Object}}{{if gt .Count 1}} (x{{.Count}}){{end}}</div>
                <p class="event-message">{{.Message}}</p>
              </article>
              {{end}}
            {{else}}
              <p class="summary-meta">No recent events captured.</p>
            {{end}}
          </div>
        </div>
      </aside>
    </div>
      
  </div>
  <div id="copyToast" class="toast">Copied!</div>
</body>
<script>
(function(){
  const body = document.body;
  const search = document.getElementById('podFilter');
  const chips = document.querySelectorAll('[data-filter-chip]');
  const rows = Array.from(document.querySelectorAll('tbody tr'));
  const sections = Array.from(document.querySelectorAll('details.namespace'));
  const filterState = new Set();
  const toast = document.getElementById('copyToast');
  const budgetWidgets = document.querySelectorAll('[data-budget-widget]');
  const budgetNamespaces = new Set();

  if (window.location.search.indexOf('print') !== -1) {
    body.classList.add('print-mode');
  }

  function matchesFilters(row){
    for (const filter of filterState) {
      if (filter === 'under-ready' && Number(row.dataset.ready || 1) >= 0.8) { return false; }
      if (filter === 'high-restart' && Number(row.dataset.maxRestart || 0) < 5) { return false; }
    }
    return true;
  }

  function namespaceAllowed(ns){
    if (budgetNamespaces.size === 0) {
      return true;
    }
    return budgetNamespaces.has(ns);
  }

  function applyFilters(){
    const needle = (search.value || '').trim().toLowerCase();
    rows.forEach(row => {
      const hay = row.getAttribute('data-filter') || '';
      const matchesSearch = !needle || hay.indexOf(needle) >= 0;
      const matches = matchesSearch && matchesFilters(row);
      row.classList.toggle('hidden', !matches);
      if (matches) {
        const parent = row.closest('details');
        if (parent) { parent.setAttribute('open','open'); }
      }
    });
    sections.forEach(section => {
      const ns = section.dataset.namespace || '';
      if (!namespaceAllowed(ns)) {
        section.style.display = 'none';
        return;
      }
      const visible = section.querySelector('tbody tr:not(.hidden)');
      section.style.display = visible ? '' : 'none';
    });
  }

  search && search.addEventListener('input', applyFilters);
  chips.forEach(chip => {
    chip.addEventListener('click', () => {
      chip.classList.toggle('active');
      const key = chip.dataset.filterChip;
      if (!key) { return; }
      if (chip.classList.contains('active')) {
        filterState.add(key);
      } else {
        filterState.delete(key);
      }
      applyFilters();
    });
  });

  budgetWidgets.forEach(widget => {
    widget.addEventListener('click', () => {
      const names = (widget.dataset.namespaces || '').split(',').map(s => s.trim()).filter(Boolean);
      const isActive = widget.classList.contains('active');
      budgetNamespaces.clear();
      budgetWidgets.forEach(w => w.classList.remove('active'));
      if (!isActive && names.length) {
        names.forEach(ns => budgetNamespaces.add(ns));
        widget.classList.add('active');
      }
      applyFilters();
    });
  });

  function showToast(message){
    if (!toast) { return; }
    toast.textContent = message;
    toast.classList.add('visible');
    clearTimeout(showToast._timer);
    showToast._timer = setTimeout(() => toast.classList.remove('visible'), 1400);
  }

  document.querySelectorAll('.cta.copy').forEach(button => {
    button.addEventListener('click', async () => {
      const cmd = button.dataset.command;
      if (!cmd) { return; }
      try {
        await navigator.clipboard.writeText(cmd);
        showToast('Command copied');
      } catch (err) {
        showToast('Unable to copy');
      }
    });
  });

  function drawSparklines(){
    document.querySelectorAll('canvas.sparkline').forEach(canvas => {
      const data = (canvas.dataset.trend || '').split(',').map(Number).filter(n => !isNaN(n));
      if (data.length < 2) { canvas.style.display = 'none'; return; }
      const ctx = canvas.getContext('2d');
      const w = canvas.width;
      const h = canvas.height;
      ctx.clearRect(0,0,w,h);
      const min = Math.min.apply(null, data);
      const max = Math.max.apply(null, data);
      const range = max - min || 1;
      ctx.beginPath();
      data.forEach((val, idx) => {
        const x = (idx / (data.length - 1)) * (w - 2) + 1;
        const y = h - ((val - min) / range) * (h - 2) - 1;
        if (idx === 0) { ctx.moveTo(x,y); } else { ctx.lineTo(x,y); }
      });
      ctx.strokeStyle = getComputedStyle(canvas).getPropertyValue('--sparkline-color') || '#0ea5e9';
      ctx.lineWidth = 2;
      ctx.stroke();
    });
  }

  applyFilters();
  drawSparklines();
  setupLiveUpdates();

  function setupLiveUpdates(){
    if (typeof EventSource === 'undefined') { return; }
    if (!/^https?:/.test(window.location.protocol)) { return; }
    const url = new URL('/events/live', window.location.origin);
    url.search = window.location.search;
    let lastHash = null;
    let inflight = false;
    const source = new EventSource(url);
    source.onmessage = event => {
      if (!event.data || event.data === lastHash || inflight) { return; }
      lastHash = event.data;
      inflight = true;
      fetch(window.location.href, {
        headers: { 'X-Requested-With': 'ktl-live' },
        cache: 'no-store',
        credentials: 'same-origin'
      })
        .then(resp => resp.text())
        .then(html => {
          document.open();
          document.write(html);
          document.close();
        })
        .catch(() => window.location.reload())
        .finally(() => { inflight = false; });
    };
  }
})();
</script>
</html>`

func RenderTable(w io.Writer, data Data) {
	if len(data.Scorecard.Checks) > 0 {
		fmt.Fprintf(w, "Scorecard (generated %s, avg %.1f%%)\n", data.Scorecard.GeneratedAt.Format(time.RFC3339), data.Scorecard.Average)
		for _, check := range data.Scorecard.Checks {
			fmt.Fprintf(w, "  - %-24s %6.1f%% %-8s %s\n", check.Name, check.Score, strings.ToUpper(string(check.Status)), check.Summary)
		}
		fmt.Fprintln(w)
	}

	fmt.Fprintf(w, "Summary (generated %s)\n", data.GeneratedAt.Format(time.RFC3339))
	fmt.Fprintf(w, "  Namespaces : %d\n", data.Summary.TotalNamespaces)
	fmt.Fprintf(w, "  Pods       : %d (ready %d)\n", data.Summary.TotalPods, data.Summary.ReadyPods)
	fmt.Fprintf(w, "  Containers : %d\n", data.Summary.TotalContainers)
	fmt.Fprintf(w, "  Restarts   : %d\n\n", data.Summary.TotalRestarts)

	for _, ns := range data.Namespaces {
		renderNamespaceSection(w, ns)
	}
}

func renderNamespaceSection(w io.Writer, ns namespaceSection) {
	fmt.Fprintf(w, "=== Namespace: %s ===\n", ns.Name)
	if ns.PodSecurityFindings > 0 {
		fmt.Fprintf(w, "  PodSecurity: %d violation(s)\n", ns.PodSecurityFindings)
	}
	if len(ns.Pods) == 0 {
		fmt.Fprintln(w, "  Pods: (none)")
	} else {
		fmt.Fprintln(w, "  Pods:")
		for _, pod := range ns.Pods {
			renderPodBlock(w, pod)
		}
	}
	if len(ns.Ingresses) > 0 {
		fmt.Fprintln(w, "  Ingresses:")
		for _, ing := range ns.Ingresses {
			renderIngressBlock(w, ing)
		}
	}
	fmt.Fprintln(w)
}

func renderPodBlock(w io.Writer, pod podRow) {
	fmt.Fprintf(w, "    - %s\n", pod.Name)
	fmt.Fprintf(w, "      Phase: %-10s Node: %-15s Ready: %-7s Restarts: %-4d Age: %s\n",
		string(pod.Phase), emptyDash(pod.Node), pod.ReadyCount, pod.Restarts, pod.Age)
	if len(pod.Containers) == 0 {
		return
	}
	fmt.Fprintln(w, "      Containers:")
	for _, c := range pod.Containers {
		status := "not-ready"
		if c.Ready {
			status = "ready"
		}
		fmt.Fprintf(w, "        • %-18s status=%-10s state=%-12s restarts=%-4d cpu=%-10s(%s) mem=%-10s(%s)\n",
			c.Name,
			status,
			emptyDash(c.State),
			c.Restarts,
			emptyDash(c.CPUUsage),
			formatPercent(c.CPUPercent),
			emptyDash(c.MemUsage),
			formatPercent(c.MemPercent),
		)
	}
}

func renderIngressBlock(w io.Writer, ing ingressRow) {
	fmt.Fprintf(w, "    - %s\n", ing.Name)
	fmt.Fprintf(w, "      Class: %-12s Hosts: %s\n", emptyDash(ing.Class), emptyDash(ing.Hosts))
	fmt.Fprintf(w, "      Services: %s\n", emptyDash(ing.Services))
	fmt.Fprintf(w, "      TLS: %s | Age: %s\n", emptyDash(ing.TLS), ing.Age)
}

func formatPercent(val float64) string {
	if val <= 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", val)
}

func applyScorecardInsights(data *Data) {
	if data == nil {
		return
	}
	violations := data.Scorecard.Insights.PodSecurityViolations
	if len(violations) == 0 {
		return
	}
	for i := range data.Namespaces {
		name := data.Namespaces[i].Name
		if count, ok := violations[name]; ok {
			data.Namespaces[i].PodSecurityFindings = count
			data.Namespaces[i].HasPodSecurityIssues = count > 0
		}
	}
}
