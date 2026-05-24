package inventory

import (
	"strings"
	"testing"

	"github.com/ingresslabs/torque/internal/ops/targetgraph"
)

func TestShowFiltersTargetsAndPreservesSummary(t *testing.T) {
	graph := inventoryFixture(t)
	selector, err := ParseSelector([]string{"role=db", "env=lab"})
	if err != nil {
		t.Fatalf("ParseSelector() error = %v", err)
	}
	result, err := Show(graph, ShowRequest{Selector: selector, Groups: []string{"gitlab"}, Limit: 1})
	if err != nil {
		t.Fatalf("Show() error = %v", err)
	}
	if result.APIVersion != APIVersion || result.Kind != ShowKind {
		t.Fatalf("result type = %s/%s", result.APIVersion, result.Kind)
	}
	if result.Summary.TargetCount != 3 || result.Summary.GroupCount != 2 {
		t.Fatalf("summary = %#v", result.Summary)
	}
	if !result.Selection.Limited || result.Selection.BeforeLimitCount != 2 || result.Selection.AfterLimitCount != 1 {
		t.Fatalf("selection = %#v", result.Selection)
	}
	if len(result.Targets) != 1 || result.Targets[0].ID != "host/gitlab-db-01" {
		t.Fatalf("targets = %#v", result.Targets)
	}
	if result.Targets[0].FactsTTL != "15m" || result.Targets[0].TransportRef != "ssh/gitlab-db-01" {
		t.Fatalf("target view = %#v", result.Targets[0])
	}
	raw, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	if strings.Contains(string(raw), "secret://") {
		t.Fatalf("inventory JSON leaked secret refs: %s", raw)
	}
}

func TestGraphExportsJSONAndHTMLWithoutSecretRefs(t *testing.T) {
	graph := inventoryFixture(t)
	selector, err := ParseSelector([]string{"role=db"})
	if err != nil {
		t.Fatalf("ParseSelector() error = %v", err)
	}
	result, err := Graph(graph, ShowRequest{Selector: selector})
	if err != nil {
		t.Fatalf("Graph() error = %v", err)
	}
	if result.APIVersion != APIVersion || result.Kind != GraphKind {
		t.Fatalf("result type = %s/%s", result.APIVersion, result.Kind)
	}
	if len(result.Nodes) == 0 || len(result.Edges) == 0 {
		t.Fatalf("graph nodes/edges = %d/%d", len(result.Nodes), len(result.Edges))
	}
	if result.SelectedTargetIDs[0] != "host/gitlab-db-01" || result.SelectedTargetIDs[1] != "host/gitlab-db-02" {
		t.Fatalf("selected target IDs = %#v", result.SelectedTargetIDs)
	}
	raw, err := result.JSON()
	if err != nil {
		t.Fatalf("JSON() error = %v", err)
	}
	html, err := RenderGraphHTML(result)
	if err != nil {
		t.Fatalf("RenderGraphHTML() error = %v", err)
	}
	combined := string(raw) + string(html)
	if !strings.Contains(combined, "target/host/gitlab-db-01") || !strings.Contains(combined, "transport/ssh/gitlab-db-01") {
		t.Fatalf("graph output missing expected nodes:\n%s", combined)
	}
	if strings.Contains(combined, "secret://") {
		t.Fatalf("graph output leaked secret refs: %s", combined)
	}
}

func TestParseSelectorRejectsInvalidValues(t *testing.T) {
	if _, err := ParseSelector([]string{"role"}); err == nil {
		t.Fatal("ParseSelector() error = nil, want invalid selector")
	}
	if got := FormatLabels(map[string]string{"role": "db", "env": "lab"}); got != "env=lab,role=db" {
		t.Fatalf("FormatLabels() = %q", got)
	}
	if got := FormatList([]string{"b", "a"}); got != "a,b" {
		t.Fatalf("FormatList() = %q", got)
	}
}

func inventoryFixture(t *testing.T) *targetgraph.TargetGraph {
	t.Helper()
	graph, err := targetgraph.Load(strings.NewReader(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: gitlab-hybrid-lab
targets:
  - id: host/gitlab-app-01
    type: host
    transportRef: ssh/gitlab-app-01
    labels:
      app: gitlab
      env: lab
      role: app
    facts:
      ttl: 15m
  - id: host/gitlab-db-01
    type: host
    transportRef: ssh/gitlab-db-01
    labels:
      app: gitlab
      env: lab
      role: db
    facts:
      ttl: 15m
  - id: host/gitlab-db-02
    type: host
    transportRef: ssh/gitlab-db-02
    labels:
      app: gitlab
      env: lab
      role: db
    facts:
      ttl: 15m
groups:
  - id: gitlab
    selector:
      app: gitlab
  - id: db
    selector:
      role: db
transports:
  - id: ssh/gitlab-app-01
    kind: ssh
    host: 141.105.65.227
    user: root
  - id: ssh/gitlab-db-01
    kind: ssh
    host: 172.31.245.13
    user: root
  - id: ssh/gitlab-db-02
    kind: ssh
    host: 172.31.245.14
    user: root
`))
	if err != nil {
		t.Fatalf("load fixture: %v", err)
	}
	return graph
}
