package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ingresslabs/torque/internal/ops/locks"
	opspolicy "github.com/ingresslabs/torque/internal/ops/policy"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	"github.com/ingresslabs/torque/internal/stack"
)

func TestStackPlan_AttachesOpsInputs(t *testing.T) {
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("TORQUE_CONFIG", cfgPath)

	root := writeStackWithCliDefaults(t)
	t.Setenv("TORQUE_STACK_ROOT", root)
	targetsPath := writeStackOpsPlanTargetGraph(t)
	factsDir := writeStackOpsPlanFactsEvidence(t)

	lockDir := filepath.Join(t.TempDir(), "locks")
	if _, err := (locks.FileStore{Dir: lockDir}).Acquire(context.Background(), locks.AcquireRequest{
		Scope:     "target/host/web-01",
		TargetID:  "host/web-01",
		Holder:    "test-operator",
		Operation: "host.command.run",
	}); err != nil {
		t.Fatalf("acquire lock fixture: %v", err)
	}

	policyPath := filepath.Join(t.TempDir(), "policy.json")
	writeJSONFixture(t, policyPath, opspolicy.Evaluate(opspolicy.Request{
		Mode:      opspolicy.ModeObserveOnly,
		Operation: "host.command.run",
		TargetID:  "host/web-01",
		Mutating:  true,
	}))

	cmd := newRootCommand()
	var out bytes.Buffer
	var errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetArgs([]string{
		"stack", "plan",
		"--output", "json",
		"--ops-targets", targetsPath,
		"--ops-selector", "role=web",
		"--ops-facts", factsDir,
		"--ops-lock-dir", lockDir,
		"--ops-policy-decision", policyPath,
	})
	if err := cmd.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v (stderr=%q stdout=%q)", err, errOut.String(), out.String())
	}

	var plan stack.Plan
	if err := json.Unmarshal(out.Bytes(), &plan); err != nil {
		t.Fatalf("decode plan: %v\n%s", err, out.String())
	}
	if plan.Ops == nil {
		t.Fatal("plan.Ops = nil, want ops plan inputs")
	}
	if got := plan.Ops.TargetGraph.Selection.MatchedTargetIDs; len(got) != 1 || got[0] != "host/web-01" {
		t.Fatalf("matched targets = %#v", got)
	}
	if plan.Ops.Summary.SelectedTargets != 1 || plan.Ops.Summary.TargetCount != 2 {
		t.Fatalf("target summary = %#v", plan.Ops.Summary)
	}
	if plan.Ops.Summary.FactBlocked != 1 || plan.Ops.Summary.LockHeld != 1 || plan.Ops.Summary.PolicyBlocked != 1 {
		t.Fatalf("ops summary did not include blocked facts, held lock, and blocked policy: %#v", plan.Ops.Summary)
	}
	if len(plan.Ops.Blockers) != 3 {
		t.Fatalf("blockers = %#v, want facts + lock + policy", plan.Ops.Blockers)
	}
}

func writeStackOpsPlanTargetGraph(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "targetgraph.yaml")
	if err := os.WriteFile(path, []byte(`
apiVersion: torque.dev/v1alpha1
kind: TargetGraph
metadata:
  name: ops-plan-lab
targets:
  - id: host/web-01
    type: host
    transportRef: local/web-01
    labels:
      role: web
    facts:
      ttl: 15m
  - id: host/db-01
    type: host
    transportRef: local/db-01
    labels:
      role: db
transports:
  - id: local/web-01
    kind: local
  - id: local/db-01
    kind: local
`), 0o600); err != nil {
		t.Fatalf("write target graph: %v", err)
	}
	return path
}

func writeStackOpsPlanFactsEvidence(t *testing.T) string {
	t.Helper()
	outDir := filepath.Join(t.TempDir(), "facts")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir facts evidence: %v", err)
	}
	result := opsFactsCollectResult{
		APIVersion: opsFactsAPIVersion,
		Kind:       opsFactsCollectionKind,
		GraphName:  "ops-plan-lab",
		Selection: targetgraph.SelectionResult{
			MatchedTargetIDs: []string{"host/web-01"},
			BeforeLimitCount: 1,
			AfterLimitCount:  1,
		},
		Results: []opsFactsTargetResult{
			{
				TargetID:   "host/web-01",
				TargetType: "host",
				Status:     "blocked",
				Source:     "cache",
				Error:      "stale facts",
			},
		},
		Summary: opsFactsCollectSummary{Selected: 1, Blocked: 1},
	}
	writeJSONFixture(t, filepath.Join(outDir, "facts.json"), result)
	writeJSONFixture(t, filepath.Join(outDir, "freshness.json"), opsFactsEvidenceFreshness{
		APIVersion:  opsFactsAPIVersion,
		Kind:        "FactFreshnessEvidence",
		GeneratedAt: "2026-05-24T00:00:00Z",
		Summary: opsFactsEvidenceFreshnessSummary{
			Blocked: 1,
		},
	})
	return outDir
}
