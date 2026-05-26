package agentappliance

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunWritesEvidenceBundle(t *testing.T) {
	repoDir := t.TempDir()
	mustWrite(t, filepath.Join(repoDir, "go.mod"), "module example.test/appliance\n")
	mustWrite(t, filepath.Join(repoDir, "main.go"), "package main\n")
	mustWrite(t, filepath.Join(repoDir, "main_test.go"), "package main\n")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	outDir := filepath.Join(t.TempDir(), "evidence")
	report, err := Run(context.Background(), Options{
		RepoDir:        repoDir,
		OutDir:         outDir,
		Actor:          "codex",
		Task:           "smoke evidence",
		APIURLs:        []string{server.URL + "/health"},
		Checks:         []string{"printf ok && printf warn >&2"},
		Timeout:        5 * time.Second,
		MaxOutputBytes: 1024,
		Now: func() time.Time {
			return time.Date(2026, 5, 26, 12, 0, 0, 0, time.UTC)
		},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !report.Passed {
		t.Fatalf("expected passed report: %+v", report.Summary)
	}
	if report.Actor != "codex" || report.Task != "smoke evidence" {
		t.Fatalf("unexpected actor/task: %q %q", report.Actor, report.Task)
	}
	if report.API.Passed != 1 || report.Checks.Passed != 1 {
		t.Fatalf("unexpected probe/check counts: api=%+v checks=%+v", report.API, report.Checks)
	}
	if !containsString(report.Repo.DependencyManifests, "go.mod") {
		t.Fatalf("go.mod not detected as dependency manifest: %#v", report.Repo.DependencyManifests)
	}
	if !containsString(report.Repo.TestFiles, "main_test.go") {
		t.Fatalf("main_test.go not detected as test file: %#v", report.Repo.TestFiles)
	}
	for _, rel := range []string{"repo.json", "api.json", "checks.json", "browser.json", "summary.md", "manifest.json", "manifest.sha256"} {
		if _, err := os.Stat(filepath.Join(outDir, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}
	raw, err := os.ReadFile(filepath.Join(outDir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var manifest Report
	if err := json.Unmarshal(raw, &manifest); err != nil {
		t.Fatalf("parse manifest: %v", err)
	}
	if manifest.Tool != ToolName || len(manifest.Evidence) == 0 {
		t.Fatalf("manifest missing tool/evidence: %+v", manifest)
	}
	checksum, err := os.ReadFile(filepath.Join(outDir, "manifest.sha256"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(checksum), "manifest.json") {
		t.Fatalf("manifest checksum does not name manifest.json: %s", checksum)
	}
}

func TestRunRecordsFailedCheck(t *testing.T) {
	repoDir := t.TempDir()
	mustWrite(t, filepath.Join(repoDir, "README.md"), "# test\n")
	outDir := filepath.Join(t.TempDir(), "evidence")
	report, err := Run(context.Background(), Options{
		RepoDir: repoDir,
		OutDir:  outDir,
		Checks:  []string{"exit 7"},
		Timeout: 5 * time.Second,
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.Passed {
		t.Fatalf("expected failed report")
	}
	if report.Checks.Failed != 1 || report.Checks.Results[0].ExitCode != 7 {
		t.Fatalf("unexpected check report: %+v", report.Checks)
	}
}

func mustWrite(t *testing.T, path, value string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(value), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
