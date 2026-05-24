package stack

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompile_AllowsActionPluginNode(t *testing.T) {
	root := t.TempDir()
	stackYAML := `apiVersion: torque.dev/v1
kind: Stack
name: plugin
defaults:
  cluster:
    name: dev
releases:
  - name: os-patch
    kind: action.plugin
    action:
      idempotent: true
      plugin:
        command: ["sh", "-c", "cat >/dev/null; printf '{\"status\":\"planned\",\"safeToRun\":true}'"]
        phases: [plan, apply]
        config:
          host: web-1
`
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), []byte(stackYAML), 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}
	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	n := p.ByID["dev/default/os-patch"]
	if n == nil {
		t.Fatalf("missing plugin node")
	}
	if got := normalizeNodeKind(n.Kind); got != NodeKindActionPlugin {
		t.Fatalf("kind=%q", got)
	}
	if n.Action.Plugin == nil || n.Action.Plugin.Config["host"] != "web-1" {
		t.Fatalf("missing plugin config: %#v", n.Action.Plugin)
	}
}

func TestRun_ActionPluginNode_RecordsDecisionAndArtifacts(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "plugin-applied.txt")
	script := `
cat >/dev/null
case "$TORQUE_STACK_PHASE" in
  plan)
    printf '{"status":"planned","safeToRun":true,"risk":"medium","message":"ready","artifacts":{"plan-receipt.json":{"ok":true}}}'
    ;;
  apply)
    printf applied > "$OUT_FILE"
    printf '{"status":"succeeded","changed":true,"message":"applied","evidence":{"target":"host-a"},"artifacts":{"apply-receipt.json":{"changed":true}}}'
    ;;
  *)
    printf '{"status":"succeeded"}'
    ;;
esac
`
	node := &ResolvedRelease{
		ID:        "local/default/plugin",
		Kind:      NodeKindActionPlugin,
		Name:      "plugin",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Action: ActionSpec{
			Idempotent: true,
			Plugin: &ActionPluginSpec{
				Command: []string{"sh", "-c", script},
				Env:     map[string]string{"OUT_FILE": outFile},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}
	raw, err := os.ReadFile(outFile)
	if err != nil {
		t.Fatalf("read plugin output: %v", err)
	}
	if got := string(raw); got != "applied" {
		t.Fatalf("plugin output=%q", got)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	gotArtifacts := map[string]string{}
	for _, artifact := range audit.Artifacts {
		if artifact.NodeID == node.ID {
			gotArtifacts[artifact.Name] = artifact.Body
		}
	}
	for _, name := range []string{"plugin-plan.json", "plugin-apply.json", "decision.json", "plan-receipt.json", "apply-receipt.json"} {
		if _, ok := gotArtifacts[name]; !ok {
			t.Fatalf("missing artifact %s in %v", name, gotArtifacts)
		}
	}
	if !strings.Contains(gotArtifacts["decision.json"], `"phase": "apply"`) {
		t.Fatalf("decision missing apply phase: %s", gotArtifacts["decision.json"])
	}
	if !strings.Contains(gotArtifacts["plugin-apply.json"], `"target": "host-a"`) {
		t.Fatalf("plugin apply artifact missing evidence: %s", gotArtifacts["plugin-apply.json"])
	}
}

func TestRun_ActionPluginNode_BlocksUnsafePlan(t *testing.T) {
	root := t.TempDir()
	outFile := filepath.Join(root, "should-not-exist.txt")
	script := `
cat >/dev/null
case "$TORQUE_STACK_PHASE" in
  plan)
    printf '{"status":"planned","safeToRun":false,"message":"missing approval"}'
    ;;
  apply)
    printf applied > "$OUT_FILE"
    printf '{"status":"succeeded","changed":true}'
    ;;
esac
`
	node := &ResolvedRelease{
		ID:        "local/default/plugin",
		Kind:      NodeKindActionPlugin,
		Name:      "plugin",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Action: ActionSpec{
			Idempotent: true,
			Plugin: &ActionPluginSpec{
				Command: []string{"sh", "-c", script},
				Env:     map[string]string{"OUT_FILE": outFile},
			},
		},
	}
	plan := planForTest(root, node)
	var out, errOut bytes.Buffer
	err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        plan,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut)
	if err == nil {
		t.Fatalf("expected blocked run")
	}
	if !strings.Contains(err.Error(), "plugin blocked") {
		t.Fatalf("expected plugin blocked error, got %v", err)
	}
	if _, statErr := os.Stat(outFile); !os.IsNotExist(statErr) {
		t.Fatalf("apply phase should not have executed, stat err=%v", statErr)
	}
}
