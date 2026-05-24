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
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/fake"
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

func TestOpsInventorySnapshotJSON(t *testing.T) {
	path := writeOpsInventoryFixture(t)
	out, errOut, err := runRootForOpsInventory(t, "ops", "inventory", "snapshot", "--source", path, "--type", "file", "--format", "json")
	if err != nil {
		t.Fatalf("execute snapshot failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var snapshot inventory.SourceSnapshot
	if err := json.Unmarshal([]byte(out), &snapshot); err != nil {
		t.Fatalf("decode snapshot output: %v\n%s", err, out)
	}
	if snapshot.Kind != inventory.SourceSnapshotKind || snapshot.SourceType != "file" {
		t.Fatalf("snapshot identity = %#v", snapshot)
	}
	if snapshot.SourceDigest == "" || snapshot.GraphDigest == "" || snapshot.Summary.TargetCount != 3 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("source snapshot leaked secret refs: %s", out)
	}
}

func TestOpsFactsCollectJSONUsesCache(t *testing.T) {
	path := writeOpsFactsLocalFixture(t)
	cacheDir := filepath.Join(t.TempDir(), "facts-cache")
	out, errOut, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--selector", "role=controller", "--cache-dir", cacheDir, "--format", "json", "--timeout", "10s")
	if err != nil {
		t.Fatalf("execute collect failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result opsFactsCollectResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode collect output: %v\n%s", err, out)
	}
	if result.Kind != opsFactsCollectionKind || result.Summary.Collected != 1 || len(result.Results) != 1 {
		t.Fatalf("collect result = %#v", result)
	}
	if result.Results[0].Snapshot == nil || result.Results[0].Snapshot.Digest == "" {
		t.Fatalf("collect snapshot missing digest: %#v", result.Results[0])
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("facts output leaked secret refs: %s", out)
	}

	cachedOut, errOut, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--selector", "role=controller", "--cache-dir", cacheDir, "--cache-only", "--format", "json", "--timeout", "10s")
	if err != nil {
		t.Fatalf("execute cache-only collect failed: %v\nstderr=%s\nstdout=%s", err, errOut, cachedOut)
	}
	var cached opsFactsCollectResult
	if err := json.Unmarshal([]byte(cachedOut), &cached); err != nil {
		t.Fatalf("decode cache output: %v\n%s", err, cachedOut)
	}
	if cached.Summary.Cached != 1 || cached.Results[0].Status != "cached" {
		t.Fatalf("cache-only result = %#v", cached)
	}
}

func TestOpsFactsCollectWritesEvidenceBundle(t *testing.T) {
	path := writeOpsFactsLocalFixture(t)
	dir := t.TempDir()
	cacheDir := filepath.Join(dir, "facts-cache")
	outDir := filepath.Join(dir, "evidence")
	out, errOut, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--selector", "role=controller", "--cache-dir", cacheDir, "--out-dir", outDir, "--format", "json", "--timeout", "10s")
	if err != nil {
		t.Fatalf("execute evidence collect failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result opsFactsCollectResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode collect output: %v\n%s", err, out)
	}
	if result.EvidenceDir != outDir {
		t.Fatalf("evidence dir = %q, want %q", result.EvidenceDir, outDir)
	}

	for _, name := range []string{"facts.json", "selection.json", "targets.json", "freshness.json", "redaction.proof.json", "manifest.json"} {
		raw, err := os.ReadFile(filepath.Join(outDir, name))
		if err != nil {
			t.Fatalf("read evidence %s: %v", name, err)
		}
		if bytes.Contains(raw, []byte("secret://")) {
			t.Fatalf("evidence %s leaked secret refs: %s", name, raw)
		}
	}

	var manifest opsFactsEvidenceManifest
	readOpsEvidenceJSON(t, outDir, "manifest.json", &manifest)
	if manifest.Kind != "FactEvidenceManifest" || manifest.Summary.Collected != 1 || manifest.Redaction.SecretRefFindings != 0 {
		t.Fatalf("manifest = %#v", manifest)
	}
	if !opsEvidenceManifestHasFile(manifest, "facts.json") || !opsEvidenceManifestHasFile(manifest, "redaction.proof.json") {
		t.Fatalf("manifest missing expected files: %#v", manifest.Files)
	}

	var redaction opsFactsRedactionProof
	readOpsEvidenceJSON(t, outDir, "redaction.proof.json", &redaction)
	if redaction.Status != "passed" || redaction.Summary.Files < 4 || redaction.Summary.SecretRefFindings != 0 {
		t.Fatalf("redaction proof = %#v", redaction)
	}

	var freshness opsFactsEvidenceFreshness
	readOpsEvidenceJSON(t, outDir, "freshness.json", &freshness)
	if freshness.Summary.HostDecisions != 1 || len(freshness.HostDecisions) != 1 {
		t.Fatalf("freshness evidence = %#v", freshness)
	}

	var targets opsFactsEvidenceTargets
	readOpsEvidenceJSON(t, outDir, "targets.json", &targets)
	if len(targets.Targets) != 1 || targets.Targets[0].Digest == "" || targets.Targets[0].SnapshotKind == "" {
		t.Fatalf("target evidence = %#v", targets)
	}
}

func TestOpsFactsCollectRedactsSecretRefErrors(t *testing.T) {
	path := writeOpsFactsSecretTransportFixture(t)
	out, _, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--format", "json", "--timeout", "10s")
	if err == nil {
		t.Fatal("execute collect error = nil, want incomplete collect")
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("collect output leaked secret ref: %s", out)
	}
	if !strings.Contains(out, "[REDACTED:secret-ref]") {
		t.Fatalf("collect output missing redacted marker: %s", out)
	}
}

func TestOpsFactsCollectKubernetesJSON(t *testing.T) {
	oldClient := newOpsKubeFactClient
	defer func() { newOpsKubeFactClient = oldClient }()
	replicas := int32(1)
	newOpsKubeFactClient = func(ctx context.Context, kubeconfigPath, kubeContext string) (opsKubeFactClient, error) {
		return opsKubeFactClient{
			Namespace: "apps",
			Clientset: fake.NewClientset(
				&corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: "apps"}, Status: corev1.NamespaceStatus{Phase: corev1.NamespaceActive}},
				&corev1.Node{
					ObjectMeta: metav1.ObjectMeta{Name: "node-a"},
					Status: corev1.NodeStatus{
						Conditions: []corev1.NodeCondition{{Type: corev1.NodeReady, Status: corev1.ConditionTrue}},
						NodeInfo:   corev1.NodeSystemInfo{KubeletVersion: "v1.30.0", OSImage: "Ubuntu 24.04", Architecture: "amd64"},
					},
				},
				&appsv1.Deployment{
					ObjectMeta: metav1.ObjectMeta{Name: "api", Namespace: "apps"},
					Spec:       appsv1.DeploymentSpec{Replicas: &replicas},
					Status:     appsv1.DeploymentStatus{ReadyReplicas: 1, AvailableReplicas: 1},
				},
				&corev1.Pod{
					ObjectMeta: metav1.ObjectMeta{Name: "api-0", Namespace: "apps"},
					Status:     corev1.PodStatus{Phase: corev1.PodRunning, Conditions: []corev1.PodCondition{{Type: corev1.PodReady, Status: corev1.ConditionTrue}}},
				},
				&corev1.Event{
					ObjectMeta:     metav1.ObjectMeta{Name: "api-warning", Namespace: "apps"},
					Type:           corev1.EventTypeWarning,
					Reason:         "BackOff",
					Message:        "container restart backoff",
					InvolvedObject: corev1.ObjectReference{Kind: "Pod", Name: "api-0", Namespace: "apps"},
				},
			),
		}, nil
	}

	path := writeOpsFactsKubernetesFixture(t)
	out, errOut, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--selector", "role=cluster", "--namespace", "apps", "--format", "json")
	if err != nil {
		t.Fatalf("execute k8s collect failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var result opsFactsCollectResult
	if err := json.Unmarshal([]byte(out), &result); err != nil {
		t.Fatalf("decode k8s collect output: %v\n%s", err, out)
	}
	if result.Summary.Collected != 1 || len(result.Results) != 1 {
		t.Fatalf("collect summary = %#v", result.Summary)
	}
	snapshot := result.Results[0].K8sSnapshot
	if snapshot == nil || snapshot.Digest == "" {
		t.Fatalf("k8s snapshot missing: %#v", result.Results[0])
	}
	if snapshot.Nodes.ReadyCount != 1 || snapshot.Workloads.Deployments.Count != 1 || snapshot.Events.WarningCount != 1 {
		t.Fatalf("unexpected k8s facts: %#v", snapshot)
	}
	if strings.Contains(out, "secret://") {
		t.Fatalf("k8s facts output leaked secret refs: %s", out)
	}

	var after opsFactsCollectResult
	cloned, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("clone k8s collect result: %v", err)
	}
	if err := json.Unmarshal(cloned, &after); err != nil {
		t.Fatalf("decode cloned k8s collect result: %v", err)
	}
	after.Results[0].K8sSnapshot.Nodes.ReadyCount = 0
	after.Results[0].K8sSnapshot.Digest = after.Results[0].K8sSnapshot.StableDigest()
	dir := t.TempDir()
	fromPath := filepath.Join(dir, "before.json")
	toPath := filepath.Join(dir, "after.json")
	writeJSONFixture(t, fromPath, result)
	writeJSONFixture(t, toPath, after)
	diffOut, errOut, err := runRootForOpsInventory(t, "ops", "facts", "diff", "--from", fromPath, "--to", toPath, "--format", "json")
	if err != nil {
		t.Fatalf("execute k8s diff failed: %v\nstderr=%s\nstdout=%s", err, errOut, diffOut)
	}
	var diff opsFactsDiffResult
	if err := json.Unmarshal([]byte(diffOut), &diff); err != nil {
		t.Fatalf("decode k8s diff output: %v\n%s", err, diffOut)
	}
	if diff.Summary.Changed != 1 || !stringSliceContains(diff.Changes[0].ChangedFields, "nodes") {
		t.Fatalf("k8s diff result = %#v", diff)
	}
}

func TestOpsFactsDiffReportsChangedFields(t *testing.T) {
	path := writeOpsFactsLocalFixture(t)
	out, errOut, err := runRootForOpsInventory(t, "ops", "facts", "collect", "--targets", path, "--format", "json", "--timeout", "10s")
	if err != nil {
		t.Fatalf("execute collect failed: %v\nstderr=%s\nstdout=%s", err, errOut, out)
	}
	var before opsFactsCollectResult
	if err := json.Unmarshal([]byte(out), &before); err != nil {
		t.Fatalf("decode collect output: %v\n%s", err, out)
	}
	var after opsFactsCollectResult
	cloned, err := json.Marshal(before)
	if err != nil {
		t.Fatalf("clone collect result: %v", err)
	}
	if err := json.Unmarshal(cloned, &after); err != nil {
		t.Fatalf("decode cloned collect result: %v", err)
	}
	if after.Results[0].Snapshot == nil {
		t.Fatalf("collect result missing snapshot: %#v", after.Results[0])
	}
	after.Results[0].Snapshot.Kernel.Release = "torque-test-kernel"
	after.Results[0].Snapshot.Digest = after.Results[0].Snapshot.StableDigest()

	dir := t.TempDir()
	fromPath := filepath.Join(dir, "before.json")
	toPath := filepath.Join(dir, "after.json")
	writeJSONFixture(t, fromPath, before)
	writeJSONFixture(t, toPath, after)

	diffOut, errOut, err := runRootForOpsInventory(t, "ops", "facts", "diff", "--from", fromPath, "--to", toPath, "--format", "json")
	if err != nil {
		t.Fatalf("execute diff failed: %v\nstderr=%s\nstdout=%s", err, errOut, diffOut)
	}
	var diff opsFactsDiffResult
	if err := json.Unmarshal([]byte(diffOut), &diff); err != nil {
		t.Fatalf("decode diff output: %v\n%s", err, diffOut)
	}
	if diff.Summary.Changed != 1 || len(diff.Changes) != 1 || !stringSliceContains(diff.Changes[0].ChangedFields, "kernel") {
		t.Fatalf("diff result = %#v", diff)
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

func writeOpsFactsLocalFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: local-facts-lab
targets:
  - id: local/controller
    type: local
    transportRef: local/controller
    labels:
      role: controller
    facts:
      ttl: 15m
    variables:
      - id: local
        values:
          credential: secret://ops/local#token
transports:
  - id: local/controller
    kind: local
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func writeOpsFactsKubernetesFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: k8s-facts-lab
targets:
  - id: k8s/lab
    type: kubernetes.cluster
    transportRef: kube/lab
    labels:
      role: cluster
      env: lab
transports:
  - id: kube/lab
    kind: kubernetes
    config:
      kubeconfig: ./test-kubeconfig.yaml
      context: lab
variables:
  - id: global
    values:
      credential: secret://ops/k8s#token
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func writeOpsFactsSecretTransportFixture(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: secret-transport-lab
targets:
  - id: host/secret
    type: host
    transportRef: secret://ops/ssh#target
    labels:
      role: secret
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
	return path
}

func writeJSONFixture(t *testing.T, path string, value any) {
	t.Helper()
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}
}

func readOpsEvidenceJSON(t *testing.T, outDir, name string, value any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(outDir, name))
	if err != nil {
		t.Fatalf("read evidence %s: %v", name, err)
	}
	if err := json.Unmarshal(raw, value); err != nil {
		t.Fatalf("decode evidence %s: %v\n%s", name, err, raw)
	}
}

func opsEvidenceManifestHasFile(manifest opsFactsEvidenceManifest, name string) bool {
	for _, file := range manifest.Files {
		if file.Path == name && file.Digest != "" && file.Bytes > 0 {
			return true
		}
	}
	return false
}

func stringSliceContains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
