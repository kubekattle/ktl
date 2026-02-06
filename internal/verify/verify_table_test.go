package verify

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestWriteReport_Table_IncludesMessage(t *testing.T) {
	rep := &Report{
		Tool:        "ktl-verify",
		Engine:      EngineMeta{Name: "builtin", Ruleset: "builtin@test"},
		Mode:        ModeBlock,
		Passed:      false,
		Blocked:     true,
		EvaluatedAt: time.Unix(0, 0).UTC(),
		Summary:     BuildSummary([]Finding{{RuleID: "k8s/container_is_privileged", Severity: SeverityHigh, Message: "container is privileged"}}, true),
		Findings: []Finding{{
			RuleID:   "k8s/container_is_privileged",
			Severity: SeverityHigh,
			Message:  "container is privileged",
			Subject:  Subject{Kind: "Deployment", Namespace: "default", Name: "api"},
		}},
	}

	var out bytes.Buffer
	if err := WriteReport(&out, rep, OutputTable); err != nil {
		t.Fatalf("WriteReport: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "k8s/container_is_privileged") {
		t.Fatalf("expected rule id, got:\n%s", got)
	}
	if !strings.Contains(got, "container is privileged") {
		t.Fatalf("expected message, got:\n%s", got)
	}
}
