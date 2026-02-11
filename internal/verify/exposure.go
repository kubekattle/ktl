package verify

import (
	"encoding/json"
	"sort"
	"strconv"
	"strings"
)

type ExposureReport struct {
	PublicSurfaces []PublicSurface `json:"publicSurfaces,omitempty"`
	Graph          ExposureGraph   `json:"graph,omitempty"`
}

type ExposureGraph struct {
	Nodes []ExposureNode `json:"nodes,omitempty"`
	Edges []ExposureEdge `json:"edges,omitempty"`
}

type ExposureNode struct {
	ID    string            `json:"id"`
	Kind  string            `json:"kind"`
	Name  string            `json:"name"`
	Meta  map[string]string `json:"meta,omitempty"`
	Score int               `json:"score,omitempty"`
}

type ExposureEdge struct {
	From string            `json:"from"`
	To   string            `json:"to"`
	Kind string            `json:"kind"` // ingress->service, service->pod, service->workload
	Meta map[string]string `json:"meta,omitempty"`
}

type PublicSurface struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"` // ingress|service
	Namespace string            `json:"namespace,omitempty"`
	Name      string            `json:"name"`
	Score     int               `json:"score"`
	Evidence  map[string]any    `json:"evidence,omitempty"`
	Targets   []TargetReference `json:"targets,omitempty"`
}

type TargetReference struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name"`
}

func AnalyzeExposure(objects []map[string]any) ExposureReport {
	idx := indexK8sObjects(objects)
	graph := ExposureGraph{}
	nodes := map[string]ExposureNode{}
	addNode := func(kind, ns, name string, score int, meta map[string]string) string {
		id := nodeID(kind, ns, name)
		if existing, ok := nodes[id]; ok {
			if score > existing.Score {
				existing.Score = score
				nodes[id] = existing
			}
			return id
		}
		nodes[id] = ExposureNode{ID: id, Kind: kind, Name: name, Meta: meta, Score: score}
		return id
	}
	addEdge := func(from, to, kind string, meta map[string]string) {
		graph.Edges = append(graph.Edges, ExposureEdge{From: from, To: to, Kind: kind, Meta: meta})
	}

	var surfaces []PublicSurface

	// Ingress -> Service -> Pods/Workloads
	for _, ing := range idx.ingresses {
		ns := objNamespace(ing)
		name := objName(ing)
		ingNode := addNode("Ingress", ns, name, 0, nil)

		rules := toSlice(toMap(ing["spec"])["rules"])
		var hostCount int
		for _, r := range rules {
			rm := toMap(r)
			host := toString(rm["host"])
			if host != "" {
				hostCount++
			}
			http := toMap(rm["http"])
			paths := toSlice(http["paths"])
			for _, p := range paths {
				pm := toMap(p)
				path := toString(pm["path"])
				backend := toMap(pm["backend"])
				service := toMap(backend["service"])
				svcName := toString(service["name"])
				if svcName == "" {
					continue
				}
				svcNode := addNode("Service", ns, svcName, 0, nil)
				addEdge(ingNode, svcNode, "ingress->service", map[string]string{"host": host, "path": path})
			}
		}
		if len(rules) == 0 {
			// Default backend
			spec := toMap(ing["spec"])
			backend := toMap(spec["defaultBackend"])
			service := toMap(backend["service"])
			svcName := toString(service["name"])
			if svcName != "" {
				svcNode := addNode("Service", ns, svcName, 0, nil)
				addEdge(ingNode, svcNode, "ingress->service", nil)
			}
		}
		score := 80
		score += minInt(40, hostCount*10)
		surfaces = append(surfaces, PublicSurface{
			ID:        "ingress:" + ns + "/" + name,
			Kind:      "ingress",
			Namespace: ns,
			Name:      name,
			Score:     score,
			Evidence: map[string]any{
				"hosts": hostCount,
			},
		})
	}

	// Service type LB/NodePort -> Pods/Workloads
	for _, svc := range idx.services {
		ns := objNamespace(svc)
		name := objName(svc)
		spec := toMap(svc["spec"])
		svcType := strings.TrimSpace(toString(spec["type"]))
		if !strings.EqualFold(svcType, "LoadBalancer") && !strings.EqualFold(svcType, "NodePort") {
			continue
		}
		score := 70
		if strings.EqualFold(svcType, "LoadBalancer") {
			score = 90
		}
		ports := extractServicePorts(spec)
		score += minInt(30, len(ports)*5)
		surfaces = append(surfaces, PublicSurface{
			ID:        "service:" + ns + "/" + name,
			Kind:      "service",
			Namespace: ns,
			Name:      name,
			Score:     score,
			Evidence: map[string]any{
				"type":  svcType,
				"ports": ports,
			},
		})
		addNode("Service", ns, name, score, map[string]string{"type": svcType})
	}

	// Join Services to Pods/Workloads via selectors
	for _, svc := range idx.services {
		ns := objNamespace(svc)
		name := objName(svc)
		spec := toMap(svc["spec"])
		selector := toStringMap(spec["selector"])
		if len(selector) == 0 {
			continue
		}
		svcNode := addNode("Service", ns, name, 0, nil)
		pods := idx.podsByNS[ns]
		for _, pod := range pods {
			if labelsMatch(selector, toStringMap(toMap(pod["metadata"])["labels"])) {
				podName := objName(pod)
				podNode := addNode("Pod", ns, podName, 0, nil)
				addEdge(svcNode, podNode, "service->pod", nil)
			}
		}

		// Best-effort workload mapping for chart-mode or when pods are absent: match selector against workload template labels.
		for _, wl := range idx.workloadsByNS[ns] {
			if labelsMatch(selector, workloadTemplateLabels(wl)) {
				wlKind := toString(wl["kind"])
				wlName := objName(wl)
				wlNode := addNode(wlKind, ns, wlName, 0, nil)
				addEdge(svcNode, wlNode, "service->workload", nil)
			}
		}
	}

	for _, n := range nodes {
		graph.Nodes = append(graph.Nodes, n)
	}
	sort.Slice(graph.Nodes, func(i, j int) bool { return graph.Nodes[i].ID < graph.Nodes[j].ID })
	sort.Slice(graph.Edges, func(i, j int) bool {
		if graph.Edges[i].From != graph.Edges[j].From {
			return graph.Edges[i].From < graph.Edges[j].From
		}
		if graph.Edges[i].To != graph.Edges[j].To {
			return graph.Edges[i].To < graph.Edges[j].To
		}
		return graph.Edges[i].Kind < graph.Edges[j].Kind
	})

	// Attach targets to public surfaces from graph.
	targetsBySurface := map[string][]TargetReference{}
	for _, e := range graph.Edges {
		if e.Kind != "ingress->service" && e.Kind != "service->pod" && e.Kind != "service->workload" {
			continue
		}
		// If edge comes from ingress, surface is ingress.
		if strings.HasPrefix(e.From, "Ingress/") {
			ns, name := parseNodeID(e.From)
			key := "ingress:" + ns + "/" + name
			kind, tns, tname := parseNodeKindNSName(e.To)
			targetsBySurface[key] = append(targetsBySurface[key], TargetReference{Kind: kind, Namespace: tns, Name: tname})
			continue
		}
		// If edge comes from service, surface is service.
		if strings.HasPrefix(e.From, "Service/") {
			ns, name := parseNodeID(e.From)
			key := "service:" + ns + "/" + name
			kind, tns, tname := parseNodeKindNSName(e.To)
			targetsBySurface[key] = append(targetsBySurface[key], TargetReference{Kind: kind, Namespace: tns, Name: tname})
		}
	}
	for i := range surfaces {
		s := &surfaces[i]
		s.Targets = uniqTargets(targetsBySurface[s.ID])
	}
	sort.Slice(surfaces, func(i, j int) bool {
		if surfaces[i].Score != surfaces[j].Score {
			return surfaces[i].Score > surfaces[j].Score
		}
		return surfaces[i].ID < surfaces[j].ID
	})
	return ExposureReport{PublicSurfaces: surfaces, Graph: graph}
}

type exposureIndex struct {
	ingresses     []map[string]any
	services      []map[string]any
	podsByNS      map[string][]map[string]any
	workloadsByNS map[string][]map[string]any
}

func indexK8sObjects(objects []map[string]any) exposureIndex {
	idx := exposureIndex{
		podsByNS:      map[string][]map[string]any{},
		workloadsByNS: map[string][]map[string]any{},
	}
	for _, obj := range objects {
		kind := strings.TrimSpace(toString(obj["kind"]))
		ns := objNamespace(obj)
		switch kind {
		case "Ingress":
			idx.ingresses = append(idx.ingresses, obj)
		case "Service":
			idx.services = append(idx.services, obj)
		case "Pod":
			idx.podsByNS[ns] = append(idx.podsByNS[ns], obj)
		case "Deployment", "StatefulSet", "DaemonSet", "Job", "CronJob", "ReplicaSet":
			idx.workloadsByNS[ns] = append(idx.workloadsByNS[ns], obj)
		default:
			// ignore
		}
	}
	return idx
}

func nodeID(kind, ns, name string) string {
	kind = strings.TrimSpace(kind)
	ns = strings.TrimSpace(ns)
	name = strings.TrimSpace(name)
	return kind + "/" + ns + "/" + name
}

func parseNodeID(id string) (ns, name string) {
	parts := strings.Split(id, "/")
	if len(parts) < 3 {
		return "", ""
	}
	return parts[1], parts[2]
}

func parseNodeKindNSName(id string) (kind, ns, name string) {
	parts := strings.Split(id, "/")
	if len(parts) < 3 {
		return "", "", ""
	}
	return parts[0], parts[1], parts[2]
}

func objName(obj map[string]any) string {
	return toString(toMap(obj["metadata"])["name"])
}

func objNamespace(obj map[string]any) string {
	return toString(toMap(obj["metadata"])["namespace"])
}

func toMap(v any) map[string]any {
	if v == nil {
		return nil
	}
	m, _ := v.(map[string]any)
	return m
}

func toSlice(v any) []any {
	if v == nil {
		return nil
	}
	s, _ := v.([]any)
	return s
}

func toString(v any) string {
	if v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(jsonString(v)), "\""), "\""))
	}
}

func jsonString(v any) string {
	raw, _ := json.Marshal(v)
	return string(raw)
}

func toStringMap(v any) map[string]string {
	out := map[string]string{}
	m, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for k, vv := range m {
		ks := strings.TrimSpace(k)
		vs := toString(vv)
		if ks != "" && vs != "" {
			out[ks] = vs
		}
	}
	return out
}

func labelsMatch(selector map[string]string, labels map[string]string) bool {
	if len(selector) == 0 {
		return false
	}
	for k, v := range selector {
		if labels[k] != v {
			return false
		}
	}
	return true
}

func workloadTemplateLabels(obj map[string]any) map[string]string {
	spec := toMap(obj["spec"])
	if spec == nil {
		return nil
	}
	switch strings.TrimSpace(toString(obj["kind"])) {
	case "CronJob":
		spec = toMap(toMap(toMap(spec["jobTemplate"])["spec"])["template"])
	default:
		spec = toMap(spec["template"])
	}
	meta := toMap(spec["metadata"])
	if meta == nil {
		return nil
	}
	return toStringMap(meta["labels"])
}

func extractServicePorts(spec map[string]any) []string {
	var out []string
	for _, p := range toSlice(spec["ports"]) {
		pm := toMap(p)
		port := intFrom(pm["port"])
		proto := strings.ToUpper(toString(pm["protocol"]))
		if proto == "" {
			proto = "TCP"
		}
		if port > 0 {
			out = append(out, strconv.Itoa(port)+"/"+proto)
		}
	}
	return out
}

func intFrom(v any) int {
	switch t := v.(type) {
	case int:
		return t
	case int64:
		return int(t)
	case float64:
		return int(t)
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(t))
		return n
	default:
		return 0
	}
}

func uniqTargets(list []TargetReference) []TargetReference {
	seen := map[string]bool{}
	var out []TargetReference
	for _, t := range list {
		k := t.Kind + "/" + t.Namespace + "/" + t.Name
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		if out[i].Namespace != out[j].Namespace {
			return out[i].Namespace < out[j].Namespace
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
