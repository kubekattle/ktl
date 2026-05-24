package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestRun_OpsApprovedReplayRequiresYes(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "ops-replay-approval")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Ops = eligibleHostCommandOpsForTest(t, root, "host/web-01")
	p.Sealed = sealedPlanMetadataForReplayTest()

	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
		OpsReplay: OpsApplyReplayOptions{
			Required: true,
			Approved: false,
		},
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "ops replay blocked") {
		t.Fatalf("Run error = %v, want ops replay blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite blocked replay: %v", calls)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeEvents:    true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if audit.Status != "blocked" {
		t.Fatalf("audit status = %s, want blocked", audit.Status)
	}
	if !auditHasEvent(audit.Events, string(OpsReplay), "blocked") {
		t.Fatalf("missing blocked ops replay event: %#v", audit.Events)
	}
	replay := opsReplayArtifact(t, audit.Artifacts)
	if replay.Status != "blocked" || !opsReplayHasBlocker(replay, "ops.replay.approval_required") {
		t.Fatalf("replay artifact = %#v", replay)
	}
}

func TestRun_OpsApprovedReplayBlocksFactDigestDrift(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "ops-replay-fact-drift")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	p.Ops = eligibleHostCommandOpsForTest(t, root, "host/web-01")
	p.Sealed = sealedPlanMetadataForReplayTest()
	if len(p.Ops.FactEvidence) == 0 {
		t.Fatal("missing fact evidence")
	}
	if err := os.WriteFile(p.Ops.FactEvidence[0].Source, []byte(`{"changed":true}`+"\n"), 0o600); err != nil {
		t.Fatalf("modify fact evidence: %v", err)
	}

	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
		OpsReplay: OpsApplyReplayOptions{
			Required: true,
			Approved: true,
		},
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "ops preflight blocked") {
		t.Fatalf("Run error = %v, want ops preflight blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite fact drift: %v", calls)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeEvents:    true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	replay := opsReplayArtifact(t, audit.Artifacts)
	if replay.Status != "eligible" {
		t.Fatalf("replay status = %s, want eligible", replay.Status)
	}
	preflight := opsPreflightArtifact(t, audit.Artifacts)
	if preflight.Status != "blocked" || !opsPreflightHasBlocker(preflight, "ops.facts.changed") {
		t.Fatalf("preflight artifact = %#v", preflight)
	}
}

func sealedPlanMetadataForReplayTest() *SealedPlanMetadata {
	return &SealedPlanMetadata{
		APIVersion:                 "torque.dev/stack-sealed-plan/v1",
		Kind:                       "SealedPlanMetadata",
		SourceKind:                 "bundle",
		SourcePath:                 "test-plan.tgz",
		PlanHash:                   "sha256:plan",
		ComputedPlanHash:           "sha256:plan",
		InputsBundle:               "inputs.tar.gz",
		InputsBundleDigest:         "sha256:inputs",
		ObservedInputsBundleDigest: "sha256:inputs",
		Verified:                   true,
	}
}

func opsReplayArtifact(t *testing.T, artifacts []RunArtifact) OpsApplyReplayResult {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Name != "ops-replay.json" {
			continue
		}
		var result OpsApplyReplayResult
		if err := json.Unmarshal([]byte(artifact.Body), &result); err != nil {
			t.Fatalf("decode ops-replay.json: %v\n%s", err, artifact.Body)
		}
		return result
	}
	t.Fatalf("missing ops-replay.json in artifacts: %#v", artifacts)
	return OpsApplyReplayResult{}
}

func opsReplayHasBlocker(result OpsApplyReplayResult, code string) bool {
	for _, blocker := range result.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}

func opsPreflightHasBlocker(result OpsApplyPreflightResult, code string) bool {
	for _, blocker := range result.Blockers {
		if blocker.Code == code {
			return true
		}
	}
	return false
}
