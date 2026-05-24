package inventory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"html/template"
	"sort"
	"strings"

	"github.com/ingresslabs/torque/internal/ops/targetgraph"
)

const GraphKind = "InventoryGraph"

type GraphResult struct {
	APIVersion        string                      `json:"apiVersion"`
	Kind              string                      `json:"kind"`
	GraphName         string                      `json:"graphName"`
	Summary           targetgraph.Summary         `json:"summary"`
	Selection         targetgraph.SelectionResult `json:"selection"`
	SelectedTargetIDs []string                    `json:"selectedTargetIds"`
	Nodes             []GraphNode                 `json:"nodes"`
	Edges             []GraphEdge                 `json:"edges"`
}

type GraphNode struct {
	ID       string            `json:"id"`
	Type     string            `json:"type"`
	Label    string            `json:"label"`
	Selected bool              `json:"selected,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Metadata map[string]string `json:"metadata,omitempty"`
}

type GraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
	Kind string `json:"kind"`
}

func Graph(graph *targetgraph.TargetGraph, request ShowRequest) (GraphResult, error) {
	if graph == nil {
		return GraphResult{}, fmt.Errorf("target graph is required")
	}
	show, err := Show(graph, request)
	if err != nil {
		return GraphResult{}, err
	}
	graphNodeID := "graph/" + RedactSecretRefs(graph.Metadata.Name)
	selected := map[string]struct{}{}
	for _, targetID := range show.Selection.MatchedTargetIDs {
		selected[targetID] = struct{}{}
	}
	result := GraphResult{
		APIVersion:        APIVersion,
		Kind:              GraphKind,
		GraphName:         graph.Metadata.Name,
		Summary:           show.Summary,
		Selection:         show.Selection,
		SelectedTargetIDs: redactedStrings(show.Selection.MatchedTargetIDs),
		Nodes: []GraphNode{
			{
				ID:    graphNodeID,
				Type:  "graph",
				Label: RedactSecretRefs(graph.Metadata.Name),
				Metadata: map[string]string{
					"apiVersion": RedactSecretRefs(graph.APIVersion),
					"kind":       RedactSecretRefs(graph.Kind),
				},
			},
		},
	}

	for _, group := range graph.Groups {
		groupNodeID := "group/" + RedactSecretRefs(group.ID)
		result.Nodes = append(result.Nodes, GraphNode{
			ID:    groupNodeID,
			Type:  "group",
			Label: RedactSecretRefs(group.ID),
			Metadata: map[string]string{
				"selector": FormatLabels(group.Selector),
			},
		})
		result.Edges = append(result.Edges, GraphEdge{From: graphNodeID, To: groupNodeID, Kind: "contains"})
		expanded, err := graph.ExpandGroup(group.ID)
		if err != nil {
			return GraphResult{}, err
		}
		for _, targetID := range expanded {
			result.Edges = append(result.Edges, GraphEdge{From: groupNodeID, To: "target/" + RedactSecretRefs(targetID), Kind: "selects"})
		}
	}
	for _, transport := range graph.Transports {
		nodeID := "transport/" + RedactSecretRefs(transport.ID)
		result.Nodes = append(result.Nodes, GraphNode{
			ID:    nodeID,
			Type:  "transport",
			Label: RedactSecretRefs(transport.ID),
			Metadata: map[string]string{
				"kind": RedactSecretRefs(transport.Kind),
			},
		})
		result.Edges = append(result.Edges, GraphEdge{From: graphNodeID, To: nodeID, Kind: "contains"})
	}
	for _, profile := range graph.PrivilegeProfiles {
		nodeID := "privilege/" + RedactSecretRefs(profile.ID)
		result.Nodes = append(result.Nodes, GraphNode{
			ID:    nodeID,
			Type:  "privilegeProfile",
			Label: RedactSecretRefs(profile.ID),
			Metadata: map[string]string{
				"kind": RedactSecretRefs(profile.Kind),
			},
		})
		result.Edges = append(result.Edges, GraphEdge{From: graphNodeID, To: nodeID, Kind: "contains"})
	}
	for _, target := range graph.Targets {
		nodeID := "target/" + RedactSecretRefs(target.ID)
		_, isSelected := selected[target.ID]
		result.Nodes = append(result.Nodes, GraphNode{
			ID:       nodeID,
			Type:     "target",
			Label:    RedactSecretRefs(target.ID),
			Selected: isSelected,
			Labels:   redactedLabels(target.Labels),
			Metadata: map[string]string{
				"type":             RedactSecretRefs(target.Type),
				"transportRef":     RedactSecretRefs(target.TransportRef),
				"factsTtl":         RedactSecretRefs(target.Facts.TTL),
				"factsDiscovery":   RedactSecretRefs(target.Facts.Discovery),
				"privilegeProfile": RedactSecretRefs(target.PrivilegeProfile),
				"lockScope":        RedactSecretRefs(target.LockScope),
			},
		})
		result.Edges = append(result.Edges, GraphEdge{From: graphNodeID, To: nodeID, Kind: "contains"})
		if target.TransportRef != "" {
			result.Edges = append(result.Edges, GraphEdge{From: nodeID, To: "transport/" + RedactSecretRefs(target.TransportRef), Kind: "uses"})
		}
		if target.PrivilegeProfile != "" {
			result.Edges = append(result.Edges, GraphEdge{From: nodeID, To: "privilege/" + RedactSecretRefs(target.PrivilegeProfile), Kind: "uses"})
		}
	}
	sortGraph(result.Nodes, result.Edges)
	return result, nil
}

func (r GraphResult) JSON() ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

func RenderGraphHTML(result GraphResult) ([]byte, error) {
	const page = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8">
  <title>{{.GraphName}} inventory graph</title>
  <style>
    body { font-family: system-ui, -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; margin: 24px; color: #17202a; background: #f7f8fb; }
    h1 { margin: 0 0 6px; font-size: 26px; }
    .meta { color: #52606d; margin-bottom: 22px; }
    .layout { display: grid; grid-template-columns: minmax(0, 1fr) minmax(320px, 0.7fr); gap: 18px; align-items: start; }
    table { width: 100%; border-collapse: collapse; background: white; border: 1px solid #d9dee7; }
    th, td { padding: 8px 10px; border-bottom: 1px solid #e6eaf0; text-align: left; font-size: 13px; vertical-align: top; }
    th { background: #eef2f7; font-weight: 650; }
    .selected { font-weight: 700; color: #075985; }
    .pill { display: inline-block; border: 1px solid #c5ccd8; border-radius: 999px; padding: 1px 7px; background: #fff; }
    pre { margin: 0; white-space: pre-wrap; overflow-wrap: anywhere; }
  </style>
</head>
<body>
  <h1>{{.GraphName}} inventory graph</h1>
  <div class="meta">{{len .Nodes}} nodes, {{len .Edges}} edges, {{len .SelectedTargetIDs}} selected targets</div>
  <div class="layout">
    <section>
      <h2>Nodes</h2>
      <table>
        <thead><tr><th>ID</th><th>Type</th><th>Selected</th><th>Labels</th></tr></thead>
        <tbody>
        {{range .Nodes}}
          <tr>
            <td>{{.ID}}</td>
            <td><span class="pill">{{.Type}}</span></td>
            <td>{{if .Selected}}<span class="selected">yes</span>{{else}}-{{end}}</td>
            <td>{{formatLabels .Labels}}</td>
          </tr>
        {{end}}
        </tbody>
      </table>
    </section>
    <section>
      <h2>Edges</h2>
      <table>
        <thead><tr><th>From</th><th>Kind</th><th>To</th></tr></thead>
        <tbody>
        {{range .Edges}}
          <tr><td>{{.From}}</td><td>{{.Kind}}</td><td>{{.To}}</td></tr>
        {{end}}
        </tbody>
      </table>
      <h2>Selected Targets</h2>
      <pre>{{range .SelectedTargetIDs}}{{.}}
{{end}}</pre>
    </section>
  </div>
</body>
</html>
`
	tmpl, err := template.New("inventory-graph").Funcs(template.FuncMap{
		"formatLabels": FormatLabels,
	}).Parse(page)
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := tmpl.Execute(&out, result); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}

func sortGraph(nodes []GraphNode, edges []GraphEdge) {
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Type == nodes[j].Type {
			return nodes[i].ID < nodes[j].ID
		}
		return nodes[i].Type < nodes[j].Type
	})
	sort.Slice(edges, func(i, j int) bool {
		left := strings.Join([]string{edges[i].From, edges[i].Kind, edges[i].To}, "\x00")
		right := strings.Join([]string{edges[j].From, edges[j].Kind, edges[j].To}, "\x00")
		return left < right
	})
}
