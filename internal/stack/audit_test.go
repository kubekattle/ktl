package stack

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestRunAudit_IncludesMetaAndDigest(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "audit-test")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 2,
		Lock:        true,
		LockTTL:     2 * time.Second,
		Executor:    &fakeExecutor{},
	}, &out, &errOut); err != nil {
		t.Fatalf("run: %v\nstderr:\n%s", err, errOut.String())
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}

	a, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir: root,
		RunID:   runID,
		Verify:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if a.RunID != runID {
		t.Fatalf("runId=%s want %s", a.RunID, runID)
	}
	if a.CreatedBy == "" {
		t.Fatalf("expected createdBy to be set")
	}
	if a.Host == "" {
		t.Fatalf("expected host to be set")
	}
	if a.PID <= 0 {
		t.Fatalf("expected pid to be set, got %d", a.PID)
	}
	if a.StatePath == "" {
		t.Fatalf("expected statePath to be set")
	}
	if a.FollowCommand == "" {
		t.Fatalf("expected followCommand to be set")
	}
	if a.RunDigest == "" {
		t.Fatalf("expected runDigest to be set")
	}
	if !a.Integrity.EventsOK {
		t.Fatalf("expected eventsOK=true, got false (%s)", a.Integrity.EventsError)
	}
	if !a.Integrity.RunDigestOK {
		t.Fatalf("expected runDigestOK=true, got false (%s)", a.Integrity.RunDigestError)
	}
}

func TestRunAudit_FailureClusters(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "audit-fail")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}

	failExec := &recordingExecutor{failOn: map[string]error{"app2": context.DeadlineExceeded}}
	var out, errOut bytes.Buffer
	_ = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
		LockTTL:     2 * time.Second,
		Executor:    failExec,
		FailFast:    false,
	}, &out, &errOut)

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	a, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir: root,
		RunID:   runID,
		Verify:  false,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(a.FailureClusters) == 0 {
		t.Fatalf("expected failure clusters, got none")
	}
	if a.FailureClusters[0].ErrorDigest == "" || a.FailureClusters[0].ErrorClass == "" {
		t.Fatalf("expected cluster error class/digest, got %+v", a.FailureClusters[0])
	}
	if a.FailureClusters[0].AffectedNodes < 1 || a.FailureClusters[0].FailedEvents < 1 {
		t.Fatalf("expected cluster counts, got %+v", a.FailureClusters[0])
	}
}
