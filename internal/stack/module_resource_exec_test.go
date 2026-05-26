package stack

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestCompile_AllowsExternalTypedModuleKind(t *testing.T) {
	root := t.TempDir()
	stackYAML := `apiVersion: torque.dev/v1
kind: Stack
name: module-demo
defaults:
  cluster:
    name: local
nodes:
  - name: counter
    kind: demo.counter.ensure
    module:
      source: oci://example.test/torque-modules/demo
      version: 0.1.0
      command: ["sh", "-c", "cat >/dev/null; printf '{\"status\":\"planned\",\"safeToRun\":true}'"]
      phases: [observe, diff, plan, apply, verify]
    input:
      path: /tmp/counter
      value: ready
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
	n := p.ByID["local/default/counter"]
	if n == nil {
		t.Fatalf("missing module node")
	}
	if got := normalizeNodeKind(n.Kind); got != "demo.counter.ensure" {
		t.Fatalf("kind=%q", got)
	}
	if len(n.Module.Command) == 0 || n.Module.Input["value"] != "ready" {
		t.Fatalf("missing module command/input: %#v", n.Module)
	}
	if _, input, err := ComputeEffectiveInputHash(root, n, true); err != nil {
		t.Fatalf("ComputeEffectiveInputHash: %v", err)
	} else if input.ModuleDigest == "" {
		t.Fatalf("missing module digest in effective input: %#v", input)
	}
}

func TestRun_ModuleResourceNode_RecordsLifecycleReceipts(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "state.txt")
	script := `
cat >/dev/null
case "$TORQUE_MODULE_PHASE" in
  observe)
    printf '{"status":"succeeded","message":"observed","before":{"exists":false}}'
    ;;
  diff)
    printf '{"status":"planned","changed":true,"diff":{"create":true}}'
    ;;
  plan)
    printf '{"status":"planned","safeToRun":true,"risk":"low","message":"ready"}'
    ;;
  apply)
    printf ready > "$STATE_FILE"
    printf '{"status":"succeeded","changed":true,"after":{"value":"ready"},"artifacts":{"apply-receipt.json":{"changed":true}}}'
    ;;
  verify)
    test "$(cat "$STATE_FILE")" = ready
    printf '{"status":"succeeded","message":"verified","receipt":{"value":"ready"},"evidence":{"path":"ok"}}'
    ;;
  *)
    printf '{"status":"failed","message":"unexpected phase"}'
    exit 1
    ;;
esac
`
	node := &ResolvedRelease{
		ID:        "local/default/counter",
		Kind:      "demo.counter.ensure",
		Name:      "counter",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Module: ModuleSpec{
			Source:  "oci://example.test/torque-modules/demo",
			Version: "0.1.0",
			Command: []string{"sh", "-c", script},
			Env:     map[string]string{"STATE_FILE": stateFile},
			Input:   map[string]any{"value": "ready"},
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
	raw, err := os.ReadFile(stateFile)
	if err != nil {
		t.Fatalf("read module state: %v", err)
	}
	if got := string(raw); got != "ready" {
		t.Fatalf("state=%q", got)
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
	for _, name := range []string{"module-observe.json", "module-diff.json", "module-plan.json", "module-apply.json", "module-verify.json", "module-resource.json", "decision.json", "apply-receipt.json"} {
		if body := auditArtifactBody(t, audit.Artifacts, node.ID, name); body == "" {
			t.Fatalf("missing artifact %s", name)
		}
	}
	if body := auditArtifactBody(t, audit.Artifacts, node.ID, "module-resource.json"); !strings.Contains(body, `"status": "succeeded"`) || !strings.Contains(body, `"phase": "verify"`) {
		t.Fatalf("module resource receipt missing success/verify:\n%s", body)
	}
	if body := auditArtifactBody(t, audit.Artifacts, node.ID, "module-verify.json"); !strings.Contains(body, `"value": "ready"`) || !strings.Contains(body, `"path": "ok"`) {
		t.Fatalf("module verify receipt missing proof:\n%s", body)
	}
}

func TestRun_ModuleResourceNode_BlocksUnsafePlan(t *testing.T) {
	root := t.TempDir()
	stateFile := filepath.Join(root, "should-not-exist.txt")
	script := `
cat >/dev/null
case "$TORQUE_MODULE_PHASE" in
  observe)
    printf '{"status":"succeeded"}'
    ;;
  diff)
    printf '{"status":"planned","changed":true}'
    ;;
  plan)
    printf '{"status":"planned","safeToRun":false,"message":"missing approval"}'
    ;;
  apply)
    printf applied > "$STATE_FILE"
    printf '{"status":"succeeded","changed":true}'
    ;;
  verify)
    printf '{"status":"succeeded"}'
    ;;
esac
`
	node := &ResolvedRelease{
		ID:        "local/default/counter",
		Kind:      "demo.counter.ensure",
		Name:      "counter",
		Dir:       root,
		Namespace: "default",
		Cluster:   ClusterTarget{Name: "local"},
		Module: ModuleSpec{
			Command: []string{"sh", "-c", script},
			Env:     map[string]string{"STATE_FILE": stateFile},
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
	if !strings.Contains(err.Error(), "module blocked") {
		t.Fatalf("expected module blocked error, got %v", err)
	}
	if _, statErr := os.Stat(stateFile); !os.IsNotExist(statErr) {
		t.Fatalf("apply phase should not have executed, stat err=%v", statErr)
	}
}
