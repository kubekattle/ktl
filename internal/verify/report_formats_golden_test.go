package verify

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteReport_HTML_Golden(t *testing.T) {
	rep := &Report{
		Tool:        "ktl-verify",
		Engine:      EngineMeta{Name: "builtin", Ruleset: "builtin@test"},
		Mode:        ModeWarn,
		FailOn:      SeverityHigh,
		Passed:      true,
		Blocked:     false,
		EvaluatedAt: time.Date(2026, 2, 6, 0, 0, 0, 0, time.UTC),
		Inputs: []Input{
			{Kind: "manifest", Source: "rendered.yaml", RenderedSHA256: "abc123"},
		},
		Findings: []Finding{
			{
				RuleID:    "k8s/container_image_tag_latest",
				Severity:  SeverityMedium,
				Category:  "Supply Chain",
				Message:   "Image tag should not be latest",
				FieldPath: "spec.template.spec.containers.0.image",
				Location:  "metadata.name={{web}}.spec.template.spec.containers.name={{web}}.image",
				Subject:   Subject{Kind: "Deployment", Namespace: "default", Name: "web"},
				Expected:  "pin to tag/digest",
				Observed:  "nginx:latest",
				HelpURL:   "https://example.invalid/docs",
			},
		},
	}
	rep.Summary = BuildSummary(rep.Findings, rep.Blocked)

	var out bytes.Buffer
	if err := WriteReport(&out, rep, OutputHTML); err != nil {
		t.Fatalf("WriteReport(html): %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "<title>ktl verify report</title>") {
		t.Fatalf("expected HTML title, got:\n%s", got[:min2(400, len(got))])
	}

	goldenPath := filepath.Join("testdata", "verify", "golden", "verify_report.html")
	if os.Getenv("KTL_UPDATE_GOLDENS") == "1" {
		if err := os.MkdirAll(filepath.Dir(goldenPath), 0o755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(goldenPath, []byte(got), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
	}
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden %s: %v (set KTL_UPDATE_GOLDENS=1 to generate)", goldenPath, err)
	}
	if string(want) != got {
		t.Fatalf("golden mismatch (%s). set KTL_UPDATE_GOLDENS=1 to update", goldenPath)
	}
}

func min2(a, b int) int {
	if a < b {
		return a
	}
	return b
}
