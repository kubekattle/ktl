package stack

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"
)

func TestRun_BlockedErrorMarksRunBlockedAndDependents(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "blocked-run")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	exec := &recordingExecutor{failOn: map[string]error{
		"app2": newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", "postgres advisory lock blocked", nil),
	}}
	var out, errOut bytes.Buffer
	err = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
		LockTTL:     2 * time.Second,
		Executor:    exec,
		FailFast:    false,
	}, &out, &errOut)
	if err == nil || !isBlockedRunError(err) {
		t.Fatalf("Run error = %v, want blocked error", err)
	}
	if !strings.Contains(err.Error(), "postgres advisory lock blocked") {
		t.Fatalf("Run error = %v, want advisory lock reason", err)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:       root,
		RunID:         runID,
		Verify:        false,
		IncludeEvents: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != "blocked" {
		t.Fatalf("audit status = %s, want blocked", audit.Status)
	}
	if audit.Summary == nil || audit.Summary.Totals.Failed != 0 || audit.Summary.Totals.Blocked != 2 || audit.Summary.Totals.Succeeded != 1 {
		t.Fatalf("audit summary = %#v, want one success and two blocked", audit.Summary)
	}
	if got := audit.Summary.Nodes["c1/ns/app2"].Status; got != "blocked" {
		t.Fatalf("app2 status = %s, want blocked", got)
	}
	if got := audit.Summary.Nodes["c1/ns/app3"].Status; got != "blocked" {
		t.Fatalf("app3 status = %s, want dependency blocked", got)
	}
	if !auditHasEvent(audit.Events, string(NodeBlocked), "postgres advisory lock blocked") {
		t.Fatalf("missing node blocked event with advisory lock reason: %#v", audit.Events)
	}
}
