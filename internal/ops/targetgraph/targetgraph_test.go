package targetgraph

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadValidTargetGraph(t *testing.T) {
	graph, err := Load(strings.NewReader(validGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if graph.Metadata.Name != "lab-foundation" {
		t.Fatalf("metadata.name = %q", graph.Metadata.Name)
	}
	summary := graph.Summary()
	if summary.TargetCount != 2 {
		t.Fatalf("TargetCount = %d, want 2", summary.TargetCount)
	}
	if summary.TargetTypes["host"] != 1 || summary.TargetTypes["local"] != 1 {
		t.Fatalf("TargetTypes = %#v", summary.TargetTypes)
	}
	if summary.TransportKinds["ssh"] != 1 || summary.TransportKinds["local"] != 1 {
		t.Fatalf("TransportKinds = %#v", summary.TransportKinds)
	}
	if summary.PrivilegeProfileCount != 2 {
		t.Fatalf("PrivilegeProfileCount = %d, want 2", summary.PrivilegeProfileCount)
	}
	if summary.SecretReferenceCount != 2 {
		t.Fatalf("SecretReferenceCount = %d, want 2", summary.SecretReferenceCount)
	}
	if len(summary.HostReachabilityRefs) != 1 || summary.HostReachabilityRefs[0] != "host/lab-ssh" {
		t.Fatalf("HostReachabilityRefs = %#v", summary.HostReachabilityRefs)
	}
}

func TestSelectTargetsMatchesLabels(t *testing.T) {
	graph, err := Load(strings.NewReader(selectionGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got := graph.SelectTargets(map[string]string{"role": "web"})
	want := []string{"host/web-01", "host/web-02", "host/web-03"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("SelectTargets() = %#v, want %#v", got, want)
	}
}

func TestExpandGroupCombinesExplicitAndSelectorTargets(t *testing.T) {
	graph, err := Load(strings.NewReader(selectionGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	got, err := graph.ExpandGroup("group/web")
	if err != nil {
		t.Fatalf("ExpandGroup() error = %v", err)
	}
	want := []string{"host/web-01", "host/web-02", "host/web-03"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("ExpandGroup() = %#v, want %#v", got, want)
	}
}

func TestResolveSelectionFiltersGroupsReportsConflictsAndLimits(t *testing.T) {
	graph, err := Load(strings.NewReader(selectionGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	result, err := graph.ResolveSelection(SelectionRequest{
		Groups:   []string{"group/mixed"},
		Selector: map[string]string{"role": "web"},
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("ResolveSelection() error = %v", err)
	}
	if got, want := strings.Join(result.MatchedTargetIDs, ","), "host/web-01"; got != want {
		t.Fatalf("MatchedTargetIDs = %q, want %q", got, want)
	}
	if got, want := strings.Join(result.OmittedTargetIDs, ","), "host/web-02"; got != want {
		t.Fatalf("OmittedTargetIDs = %q, want %q", got, want)
	}
	if !result.Limited || result.BeforeLimitCount != 2 || result.AfterLimitCount != 1 {
		t.Fatalf("limit fields = limited:%v before:%d after:%d", result.Limited, result.BeforeLimitCount, result.AfterLimitCount)
	}
	if result.ConflictCount != 1 || result.Conflicts[0].TargetID != "host/db-01" {
		t.Fatalf("Conflicts = %#v, want db conflict", result.Conflicts)
	}
}

func TestResolveSelectionRejectsUnknownGroup(t *testing.T) {
	graph, err := Load(strings.NewReader(selectionGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	_, err = graph.ResolveSelection(SelectionRequest{Groups: []string{"group/missing"}})
	if err == nil {
		t.Fatal("ResolveSelection() error = nil, want unknown group")
	}
	if !strings.Contains(err.Error(), "unknown group") {
		t.Fatalf("ResolveSelection() error = %v, want unknown group detail", err)
	}
}

func TestResolveVariablesAppliesPrecedenceAndProvenance(t *testing.T) {
	graph, err := Load(strings.NewReader(variableGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	result, err := graph.ResolveVariables(VariableResolutionRequest{
		TargetID: "host/web-01",
		Environment: map[string]any{
			"package":           "env-nginx",
			"maintenanceWindow": "night",
			"apiToken":          "secret://ops/env#api-token",
		},
		CLI: map[string]any{
			"package": "cli-nginx",
			"dryRun":  true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveVariables() error = %v", err)
	}
	if got := result.Values["package"].ActualValue; got != "cli-nginx" {
		t.Fatalf("package = %v, want cli-nginx", got)
	}
	if got := result.Values["replicas"].ActualValue; got != 3 {
		t.Fatalf("replicas = %v, want 3", got)
	}
	if got := result.Values["region"].ActualValue; got != "global" {
		t.Fatalf("region = %v, want global", got)
	}
	if got := result.Values["maintenanceWindow"].ActualValue; got != "night" {
		t.Fatalf("maintenanceWindow = %v, want night", got)
	}
	if result.FinalSources["package"].Type != "cli" {
		t.Fatalf("package source = %#v, want cli", result.FinalSources["package"])
	}
	if result.FinalSources["replicas"].Type != "group" {
		t.Fatalf("replicas source = %#v, want group", result.FinalSources["replicas"])
	}
	if len(result.OverriddenKeys["package"]) != 4 {
		t.Fatalf("package overridden history = %#v, want 4 overridden sources", result.OverriddenKeys["package"])
	}
	if result.Redaction.SecretRefCount != 3 {
		t.Fatalf("SecretRefCount = %d, want 3", result.Redaction.SecretRefCount)
	}
	if result.Values["apiToken"].Value != "[REDACTED:sensitive-key]" {
		t.Fatalf("apiToken redacted value = %#v", result.Values["apiToken"].Value)
	}
	if result.Values["tlsCert"].Value != "[REDACTED:secret-ref]" {
		t.Fatalf("tlsCert redacted value = %#v", result.Values["tlsCert"].Value)
	}
	raw, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal result: %v", err)
	}
	if strings.Contains(string(raw), "secret://") {
		t.Fatalf("redacted resolution leaked secret ref: %s", raw)
	}
}

func TestResolveVariablesRejectsUnknownTarget(t *testing.T) {
	graph, err := Load(strings.NewReader(variableGraphYAML()))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	_, err = graph.ResolveVariables(VariableResolutionRequest{TargetID: "host/missing"})
	if err == nil {
		t.Fatal("ResolveVariables() error = nil, want unknown target")
	}
	if !strings.Contains(err.Error(), "unknown target") {
		t.Fatalf("ResolveVariables() error = %v, want unknown target detail", err)
	}
}

func TestLoadRejectsDuplicateTargetID(t *testing.T) {
	_, err := Load(strings.NewReader(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: duplicate
targets:
  - id: host/a
    type: host
    transportRef: ssh/a
  - id: host/a
    type: host
    transportRef: ssh/a
transports:
  - id: ssh/a
    kind: ssh
`))
	if err == nil {
		t.Fatal("Load() error = nil, want duplicate target validation error")
	}
	if !strings.Contains(err.Error(), "duplicates host/a") {
		t.Fatalf("Load() error = %v, want duplicate target detail", err)
	}
}

func TestLoadRejectsUnknownHostTransport(t *testing.T) {
	_, err := Load(strings.NewReader(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: missing-transport
targets:
  - id: host/a
    type: host
    transportRef: ssh/missing
`))
	if err == nil {
		t.Fatal("Load() error = nil, want missing transport validation error")
	}
	if !strings.Contains(err.Error(), "references unknown transport") {
		t.Fatalf("Load() error = %v, want unknown transport detail", err)
	}
}

func TestLoadRejectsBadFactTTL(t *testing.T) {
	_, err := Load(strings.NewReader(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: bad-ttl
targets:
  - id: local/controller
    type: local
    facts:
      ttl: soon
`))
	if err == nil {
		t.Fatal("Load() error = nil, want bad ttl validation error")
	}
	if !strings.Contains(err.Error(), "facts.ttl") {
		t.Fatalf("Load() error = %v, want ttl detail", err)
	}
}

func TestLoadRejectsUnknownFields(t *testing.T) {
	_, err := Load(strings.NewReader(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: unknown-field
targets:
  - id: local/controller
    type: local
    surprise: true
`))
	if err == nil {
		t.Fatal("Load() error = nil, want unknown field error")
	}
	if !strings.Contains(err.Error(), "field surprise not found") {
		t.Fatalf("Load() error = %v, want unknown field detail", err)
	}
}

func TestE2EEnvLoadTargetGraph(t *testing.T) {
	input := os.Getenv("TORQUE_OPS_TG_E2E_INPUT")
	output := os.Getenv("TORQUE_OPS_TG_E2E_OUTPUT")
	if input == "" && output == "" {
		t.Skip("set TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_OUTPUT to run the E2E loader proof")
	}
	if input == "" || output == "" {
		t.Fatal("TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_OUTPUT must be set together")
	}
	graph, err := LoadFile(input)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", input, err)
	}
	summary := graph.Summary()
	raw, err := json.MarshalIndent(summary, "", "  ")
	if err != nil {
		t.Fatalf("marshal summary: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}
}

func TestE2EEnvResolveSelection(t *testing.T) {
	input := os.Getenv("TORQUE_OPS_TG_E2E_INPUT")
	output := os.Getenv("TORQUE_OPS_TG_E2E_SELECTION_OUTPUT")
	if input == "" && output == "" {
		t.Skip("set TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_SELECTION_OUTPUT to run the E2E selection proof")
	}
	if input == "" || output == "" {
		t.Fatal("TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_SELECTION_OUTPUT must be set together")
	}
	graph, err := LoadFile(input)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", input, err)
	}
	groupWeb, err := graph.ResolveSelection(SelectionRequest{Groups: []string{"group/web"}})
	if err != nil {
		t.Fatalf("resolve group/web: %v", err)
	}
	zoneA, err := graph.ResolveSelection(SelectionRequest{
		Groups:   []string{"group/web"},
		Selector: map[string]string{"zone": "a"},
	})
	if err != nil {
		t.Fatalf("resolve group/web zone=a: %v", err)
	}
	limited, err := graph.ResolveSelection(SelectionRequest{
		Groups: []string{"group/web"},
		Limit:  2,
	})
	if err != nil {
		t.Fatalf("resolve group/web limit: %v", err)
	}
	conflict, err := graph.ResolveSelection(SelectionRequest{
		Groups:   []string{"group/mixed"},
		Selector: map[string]string{"role": "web"},
	})
	if err != nil {
		t.Fatalf("resolve group/mixed role=web: %v", err)
	}
	doc := map[string]any{
		"apiVersion": "torque.dev/e2e/v1",
		"kind":       "OpsTargetGraphSelectionProof",
		"graphName":  graph.Metadata.Name,
		"selections": map[string]SelectionResult{
			"groupWeb":         groupWeb,
			"groupWebZoneA":    zoneA,
			"groupWebLimitTwo": limited,
			"groupMixedWeb":    conflict,
		},
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal selection proof: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write selection proof: %v", err)
	}
}

func TestE2EEnvResolveVariables(t *testing.T) {
	input := os.Getenv("TORQUE_OPS_TG_E2E_INPUT")
	output := os.Getenv("TORQUE_OPS_TG_E2E_VARIABLE_OUTPUT")
	if input == "" && output == "" {
		t.Skip("set TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_VARIABLE_OUTPUT to run the E2E variable proof")
	}
	if input == "" || output == "" {
		t.Fatal("TORQUE_OPS_TG_E2E_INPUT and TORQUE_OPS_TG_E2E_VARIABLE_OUTPUT must be set together")
	}
	graph, err := LoadFile(input)
	if err != nil {
		t.Fatalf("LoadFile(%q) error = %v", input, err)
	}
	result, err := graph.ResolveVariables(VariableResolutionRequest{
		TargetID: "host/web-01",
		Environment: map[string]any{
			"package":           "env-nginx",
			"maintenanceWindow": "night",
			"apiToken":          "secret://ops/env#api-token",
		},
		CLI: map[string]any{
			"package": "cli-nginx",
			"dryRun":  true,
		},
	})
	if err != nil {
		t.Fatalf("ResolveVariables() error = %v", err)
	}
	doc := map[string]any{
		"apiVersion": "torque.dev/e2e/v1",
		"kind":       "OpsTargetGraphVariableProof",
		"graphName":  graph.Metadata.Name,
		"resolution": result,
	}
	raw, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		t.Fatalf("marshal variable proof: %v", err)
	}
	if strings.Contains(string(raw), "secret://") {
		t.Fatal("variable proof leaked secret reference")
	}
	if err := os.MkdirAll(filepath.Dir(output), 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	if err := os.WriteFile(output, append(raw, '\n'), 0o644); err != nil {
		t.Fatalf("write variable proof: %v", err)
	}
}

func validGraphYAML() string {
	return `
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: lab-foundation
  labels:
    env: lab
variables:
  - id: environment
    values:
      region: local
targets:
  - id: local/controller
    type: local
    labels:
      role: controller
    privilegeProfile: local-readonly
    facts:
      ttl: 5m
  - id: host/lab-ssh
    type: host
    transportRef: ssh/lab
    labels:
      role: web
    groups:
      - group/web
    privilegeProfile: ssh-readonly
    facts:
      ttl: 15m
    variables:
      - id: host
        values:
          package: curl
groups:
  - id: group/web
    selector:
      role: web
transports:
  - id: local/controller
    kind: local
  - id: ssh/lab
    kind: ssh
    host: secret://ops/lab/ssh#host
    user: root
    keyRef: secret://ops/lab/ssh#identity
privilegeProfiles:
  - id: local-readonly
    kind: none
  - id: ssh-readonly
    kind: sudo
    commands:
      - /usr/bin/uname
      - /bin/mkdir
`
}

func selectionGraphYAML() string {
	return `
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: selection-foundation
targets:
  - id: host/web-01
    type: host
    transportRef: ssh/web-01
    labels:
      role: web
      zone: a
  - id: host/web-02
    type: host
    transportRef: ssh/web-02
    labels:
      role: web
      zone: b
  - id: host/web-03
    type: host
    transportRef: ssh/web-03
    labels:
      role: web
      zone: c
  - id: host/db-01
    type: host
    transportRef: ssh/db-01
    labels:
      role: db
      zone: a
groups:
  - id: group/web
    selector:
      role: web
  - id: group/mixed
    targets:
      - host/web-01
      - host/web-02
      - host/db-01
transports:
  - id: ssh/web-01
    kind: ssh
  - id: ssh/web-02
    kind: ssh
  - id: ssh/web-03
    kind: ssh
  - id: ssh/db-01
    kind: ssh
`
}

func variableGraphYAML() string {
	return `
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: variable-foundation
variables:
  - id: defaults
    values:
      package: default-nginx
      region: global
      dbPassword: secret://ops/db#password
targets:
  - id: host/web-01
    type: host
    transportRef: ssh/web-01
    labels:
      role: web
      zone: a
    variables:
      - id: host
        values:
          package: host-nginx
          hostRole: primary
  - id: host/db-01
    type: host
    transportRef: ssh/db-01
    labels:
      role: db
      zone: a
groups:
  - id: group/web
    selector:
      role: web
    variables:
      - id: web
        values:
          package: group-nginx
          replicas: 3
          tlsCert: secret://ops/tls#cert
transports:
  - id: ssh/web-01
    kind: ssh
  - id: ssh/db-01
    kind: ssh
`
}
