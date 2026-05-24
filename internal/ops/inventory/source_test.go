package inventory

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSnapshotSourceAdapters(t *testing.T) {
	graph := sourceTargetGraph("source-lab")
	dir := t.TempDir()
	filePath := filepath.Join(dir, "targetgraph.yaml")
	if err := os.WriteFile(filePath, []byte(graph), 0o600); err != nil {
		t.Fatalf("write file source: %v", err)
	}
	scriptPath := filepath.Join(dir, "inventory.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/sh\ncat "+shellQuoteForTest(filePath)+"\n"), 0o700); err != nil {
		t.Fatalf("write script source: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(graph))
	}))
	defer server.Close()

	cases := []SourceSnapshotOptions{
		{Type: "file", Source: filePath},
		{Type: "script", Source: scriptPath},
		{Type: "http", Source: server.URL + "?token=secret"},
	}
	for _, tc := range cases {
		t.Run(tc.Type, func(t *testing.T) {
			snapshot, err := SnapshotSource(context.Background(), tc)
			if err != nil {
				t.Fatalf("SnapshotSource() error = %v", err)
			}
			assertSourceSnapshot(t, snapshot, tc.Type)
		})
	}

	changedPath := filepath.Join(dir, "changed.yaml")
	if err := os.WriteFile(changedPath, []byte(sourceTargetGraph("source-lab-changed")), 0o600); err != nil {
		t.Fatalf("write changed file source: %v", err)
	}
	before, err := SnapshotSource(context.Background(), SourceSnapshotOptions{Type: "file", Source: filePath})
	if err != nil {
		t.Fatalf("SnapshotSource(before) error = %v", err)
	}
	after, err := SnapshotSource(context.Background(), SourceSnapshotOptions{Type: "file", Source: changedPath})
	if err != nil {
		t.Fatalf("SnapshotSource(after) error = %v", err)
	}
	if before.SourceDigest == after.SourceDigest || before.GraphDigest == after.GraphDigest {
		t.Fatalf("changed source did not change digests: before=%#v after=%#v", before, after)
	}
}

func TestSnapshotSourceGitAdapter(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := t.TempDir()
	runGitForTest(t, repo, "init")
	runGitForTest(t, repo, "config", "user.email", "torque@example.invalid")
	runGitForTest(t, repo, "config", "user.name", "Torque Tests")
	if err := os.WriteFile(filepath.Join(repo, "targetgraph.yaml"), []byte(sourceTargetGraph("git-lab")), 0o600); err != nil {
		t.Fatalf("write git source: %v", err)
	}
	runGitForTest(t, repo, "add", "targetgraph.yaml")
	runGitForTest(t, repo, "commit", "-m", "add target graph")

	snapshot, err := SnapshotSource(context.Background(), SourceSnapshotOptions{
		Type:    "git",
		Source:  repo,
		Path:    "targetgraph.yaml",
		Ref:     "HEAD",
		Timeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatalf("SnapshotSource(git) error = %v", err)
	}
	assertSourceSnapshot(t, snapshot, "git")
	if snapshot.Revision == "" || snapshot.Path != "targetgraph.yaml" || snapshot.DirtyState != "clean" {
		t.Fatalf("git snapshot missing revision/path: %#v", snapshot)
	}
}

func assertSourceSnapshot(t *testing.T, snapshot *SourceSnapshot, sourceType string) {
	t.Helper()
	if snapshot.APIVersion != APIVersion || snapshot.Kind != SourceSnapshotKind || snapshot.SourceType != sourceType {
		t.Fatalf("unexpected snapshot identity: %#v", snapshot)
	}
	if snapshot.SourceDigest == "" || snapshot.GraphDigest == "" || snapshot.Digest == "" {
		t.Fatalf("missing digest fields: %#v", snapshot)
	}
	if snapshot.Summary.TargetCount != 1 || snapshot.Summary.SecretReferenceCount != 1 {
		t.Fatalf("summary = %#v", snapshot.Summary)
	}
	if snapshot.Digest != snapshot.StableDigest() {
		t.Fatalf("unstable digest: %q vs %q", snapshot.Digest, snapshot.StableDigest())
	}
	raw, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	if strings.Contains(string(raw), "secret://") {
		t.Fatalf("source snapshot leaked secret refs: %s", raw)
	}
	if strings.Contains(string(raw), "token=secret") {
		t.Fatalf("source snapshot leaked HTTP query credentials: %s", raw)
	}
}

func sourceTargetGraph(name string) string {
	return `apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ` + name + `
targets:
  - id: host/app-01
    type: host
    transportRef: local/app-01
    labels:
      role: app
transports:
  - id: local/app-01
    kind: local
variables:
  - id: global
    values:
      token: secret://ops/source#token
`
}

func runGitForTest(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	raw, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, raw)
	}
}

func shellQuoteForTest(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\\''") + "'"
}
