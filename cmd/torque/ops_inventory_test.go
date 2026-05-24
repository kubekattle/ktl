package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ingresslabs/torque/internal/ops/inventory"
)

func TestOpsInventoryShowJSON(t *testing.T) {
	path := writeOpsInventoryFixture(t)
	out, errOut, err := runRootForOpsInventory(t, "ops", "inventory", "show", "--targets", path, "--selector", "role=db", "--format", "json")
	if err != nil {
		t.Fatalf("execute failed: %v\nstderr=%s", err, errOut)
	}
	var result inventory.ShowResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if len(result.Targets) != 2 {
		t.Fatalf("target count = %d, want 2", len(result.Targets))
	}
	if result.Selection.BeforeLimitCount != 2 || result.Selection.AfterLimitCount != 2 {
		t.Fatalf("selection = %#v", result.Selection)
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("inventory output leaked secret refs: %s", out)
	}
}

func TestOpsInventoryShowTable(t *testing.T) {
	path := writeOpsInventoryFixture(t)
	out, errOut, err := runRootForOpsInventory(t, "ops", "inventory", "show", "--targets", path, "--group", "gitlab", "--limit", "1")
	if err != nil {
		t.Fatalf("execute failed: %v\nstderr=%s", err, errOut)
	}
	for _, want := range []string{"TARGET", "TYPE", "TRANSPORT", "host/gitlab-app-01"} {
		if !strings.Contains(out, want) {
			t.Fatalf("table output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("inventory table leaked secret refs: %s", out)
	}
}

func TestOpsInventoryGraphJSONAndHTML(t *testing.T) {
	path := writeOpsInventoryFixture(t)
	jsonOut, errOut, err := runRootForOpsInventory(t, "ops", "inventory", "graph", "--targets", path, "--selector", "role=db", "--format", "json")
	if err != nil {
		t.Fatalf("execute JSON failed: %v\nstderr=%s", err, errOut)
	}
	if !strings.Contains(jsonOut, `"kind": "InventoryGraph"`) || !strings.Contains(jsonOut, `"selectedTargetIds"`) {
		t.Fatalf("graph JSON missing expected fields:\n%s", jsonOut)
	}
	if strings.Contains(jsonOut, "secret://") {
		t.Fatalf("graph JSON leaked secret refs: %s", jsonOut)
	}

	outputPath := filepath.Join(t.TempDir(), "inventory.html")
	stdout, errOut, err := runRootForOpsInventory(t, "ops", "inventory", "graph", "--targets", path, "--group", "gitlab", "--limit", "1", "--output", outputPath)
	if err != nil {
		t.Fatalf("execute HTML failed: %v\nstderr=%s", err, errOut)
	}
	if stdout != "" {
		t.Fatalf("expected no stdout with --output, got %q", stdout)
	}
	html, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("read graph HTML: %v", err)
	}
	if !bytes.Contains(html, []byte("<!doctype html>")) || !bytes.Contains(html, []byte("target/host/gitlab-app-01")) {
		t.Fatalf("graph HTML missing expected content:\n%s", html)
	}
	if bytes.Contains(html, []byte("secret://")) {
		t.Fatalf("graph HTML leaked secret refs: %s", html)
	}
}

func runRootForOpsInventory(t *testing.T, args ...string) (string, string, error) {
	t.Helper()
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("TORQUE_CONFIG", cfgPath)
	root := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&errOut)
	root.SetArgs(args)
	err := root.ExecuteContext(context.Background())
	return out.String(), errOut.String(), err
}

func writeOpsInventoryFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
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
variables:
  - id: global
    values:
      credential: secret://ops/gitlab#token
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}
