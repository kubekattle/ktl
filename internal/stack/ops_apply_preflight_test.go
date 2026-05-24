package stack

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ingresslabs/torque/internal/ops/locks"
)

func TestRun_OpsPreflightBlocksBeforeMutation(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "ops-preflight-block")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	targetGraphPath := writeOpsPreflightTargetGraph(t, root, "role: web\n")
	p.Ops = &OpsPlanInputs{
		APIVersion: "torque.dev/ops/plan-inputs/v1alpha1",
		Kind:       "OpsPlanInputs",
		TargetGraph: &OpsTargetGraphInput{
			Path:         targetGraphPath,
			Name:         "ops-preflight",
			SourceDigest: opsApplyPreflightFileDigest(t, targetGraphPath),
			Selection: OpsTargetSelectionInput{
				MatchedTargetIDs: []string{"host/web-01"},
			},
			Summary: OpsTargetGraphSummary{TargetCount: 1},
		},
		FactEvidence: []OpsFactEvidenceInput{
			{
				Source: "facts.json",
				Kind:   "FactCollection",
				Summary: OpsFactEvidenceSummary{
					Selected: 1,
					Blocked:  1,
				},
			},
		},
		Summary: OpsPlanInputSummary{
			TargetCount:     1,
			SelectedTargets: 1,
			FactEvidence:    1,
			FactBlocked:     1,
		},
	}

	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	err = Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut)
	if err == nil || !strings.Contains(err.Error(), "ops preflight blocked") {
		t.Fatalf("Run error = %v, want ops preflight blocked", err)
	}
	if calls := exec.calledNames(); len(calls) != 0 {
		t.Fatalf("executor was called despite blocked preflight: %v", calls)
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
	if audit.Status != "blocked" || audit.Summary == nil || audit.Summary.Totals.Blocked != len(p.Nodes) {
		t.Fatalf("audit status/summary = %s %#v", audit.Status, audit.Summary)
	}
	if !auditHasEvent(audit.Events, string(OpsPreflight), "blocked") {
		t.Fatalf("missing blocked ops preflight event: %#v", audit.Events)
	}
	preflight := opsPreflightArtifact(t, audit.Artifacts)
	if preflight.Status != "blocked" || len(preflight.Blockers) == 0 {
		t.Fatalf("preflight artifact = %#v", preflight)
	}
}

func TestRun_OpsPreflightEligibleWritesEvidence(t *testing.T) {
	root := t.TempDir()
	writeMinimalStackFixture(t, root, "ops-preflight-eligible")

	u, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	targetGraphPath := writeOpsPreflightTargetGraph(t, root, "role: web\n")
	factsPath := writeOpsPreflightFactsFile(t, root, "host/web-01", "collected")
	policyPath := writeOpsPreflightPolicyFile(t, root, "host/web-01", "allow")
	lockDir := filepath.Join(root, "ops-locks")
	if _, err := (locks.FileStore{Dir: lockDir}).Acquire(context.Background(), locks.AcquireRequest{
		Scope:     "target/host/web-01",
		TargetID:  "host/web-01",
		Holder:    "test-operator",
		Operation: "host.command.run",
		TTL:       time.Minute,
	}); err != nil {
		t.Fatalf("acquire lock: %v", err)
	}
	p.Ops = &OpsPlanInputs{
		APIVersion: "torque.dev/ops/plan-inputs/v1alpha1",
		Kind:       "OpsPlanInputs",
		TargetGraph: &OpsTargetGraphInput{
			Path:         targetGraphPath,
			Name:         "ops-preflight",
			SourceDigest: opsApplyPreflightFileDigest(t, targetGraphPath),
			Selection: OpsTargetSelectionInput{
				MatchedTargetIDs: []string{"host/web-01"},
			},
			Summary: OpsTargetGraphSummary{TargetCount: 1},
		},
		FactEvidence: []OpsFactEvidenceInput{
			{
				Source: factsPath,
				Kind:   "FactCollection",
				Digest: opsApplyPreflightFileDigest(t, factsPath),
				Targets: []OpsFactTargetInput{
					{TargetID: "host/web-01", Status: "collected", Digest: "sha256:facts"},
				},
				Summary: OpsFactEvidenceSummary{
					Selected:  1,
					Targets:   1,
					Snapshots: 1,
					Collected: 1,
				},
			},
		},
		Locks: []OpsLockInput{
			{
				Source:   lockDir,
				Scope:    "target/host/web-01",
				Found:    true,
				TargetID: "host/web-01",
				Status:   "held",
				Holder:   "test-operator",
			},
		},
		PolicyDecisions: []OpsPolicyDecisionInput{
			{
				Source:    policyPath,
				Digest:    opsApplyPreflightFileDigest(t, policyPath),
				Decision:  "allow",
				Reason:    "guarded policy satisfied",
				Operation: "host.command.run",
				TargetID:  "host/web-01",
				Mutating:  true,
			},
		},
		Summary: OpsPlanInputSummary{
			TargetCount:     1,
			SelectedTargets: 1,
			FactEvidence:    1,
			FactSnapshots:   1,
			Locks:           1,
			PolicyDecisions: 1,
		},
	}

	exec := &recordingExecutor{}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Executor:    exec,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}
	if calls := exec.calledNames(); len(calls) != len(p.Nodes) {
		t.Fatalf("executor calls = %v, want %d nodes", calls, len(p.Nodes))
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatal(err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	preflight := opsPreflightArtifact(t, audit.Artifacts)
	if preflight.Status != "eligible" || preflight.Summary.Blocked != 0 {
		t.Fatalf("preflight artifact = %#v", preflight)
	}
}

func writeOpsPreflightTargetGraph(t *testing.T, root string, label string) string {
	t.Helper()
	path := filepath.Join(root, "targetgraph.yaml")
	body := `apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-preflight
targets:
  - id: host/web-01
    type: host
    transportRef: local/web-01
    labels:
      ` + label + `transports:
  - id: local/web-01
    kind: local
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write target graph: %v", err)
	}
	return path
}

func writeOpsPreflightFactsFile(t *testing.T, root string, targetID string, status string) string {
	t.Helper()
	path := filepath.Join(root, "facts-"+strings.NewReplacer("/", "-").Replace(targetID)+".json")
	body := `{
  "apiVersion": "torque.dev/ops/facts/v1alpha1",
  "kind": "FactCollection",
  "graphName": "ops-preflight",
  "results": [
    {
      "targetId": "` + targetID + `",
      "targetType": "host",
      "transportKind": "local",
      "status": "` + status + `",
      "source": "test"
    }
  ],
  "summary": {
    "selected": 1,
    "collected": 1
  }
}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write facts: %v", err)
	}
	return path
}

func writeOpsPreflightPolicyFile(t *testing.T, root string, targetID string, decision string) string {
	t.Helper()
	path := filepath.Join(root, "policy-"+strings.NewReplacer("/", "-").Replace(targetID)+".json")
	body := `{
  "mode": "guarded",
  "decision": "` + decision + `",
  "reason": "guarded policy satisfied",
  "operation": "host.command.run",
  "targetId": "` + targetID + `",
  "mutating": true
}
`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write policy: %v", err)
	}
	return path
}

func opsApplyPreflightFileDigest(t *testing.T, path string) string {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return opsApplyPreflightDigest(raw)
}

func opsPreflightArtifact(t *testing.T, artifacts []RunArtifact) OpsApplyPreflightResult {
	t.Helper()
	for _, artifact := range artifacts {
		if artifact.Name != "ops-preflight.json" {
			continue
		}
		var result OpsApplyPreflightResult
		if err := json.Unmarshal([]byte(artifact.Body), &result); err != nil {
			t.Fatalf("decode ops-preflight.json: %v\n%s", err, artifact.Body)
		}
		return result
	}
	t.Fatalf("missing ops-preflight.json in artifacts: %#v", artifacts)
	return OpsApplyPreflightResult{}
}

func auditHasEvent(events []RunEvent, typ string, message string) bool {
	for _, event := range events {
		if event.Type == typ && strings.Contains(event.Message, message) {
			return true
		}
	}
	return false
}
