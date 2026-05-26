package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ingresslabs/torque/internal/agentappliance"
)

func TestAgentApplianceRunCommandWritesManifest(t *testing.T) {
	repoDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("# appliance\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "evidence")
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs([]string{
		"agent", "appliance", "run", repoDir,
		"--out-dir", outDir,
		"--actor", "codex",
		"--task", "cli smoke",
		"--check", "printf ok",
		"--format", "json",
	})
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("agent appliance run: %v\n%s", err, out.String())
	}
	var report agentappliance.Report
	if err := json.Unmarshal(out.Bytes(), &report); err != nil {
		t.Fatalf("parse output: %v\n%s", err, out.String())
	}
	if !report.Passed || report.Tool != agentappliance.ToolName {
		t.Fatalf("unexpected report: %+v", report)
	}
	if _, err := os.Stat(filepath.Join(outDir, "manifest.json")); err != nil {
		t.Fatalf("manifest missing: %v", err)
	}
}
