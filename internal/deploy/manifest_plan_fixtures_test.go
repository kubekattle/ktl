// File: internal/deploy/manifest_plan_fixtures_test.go
// Brief: Fixture-based plan summarization tests.

package deploy

import (
	"os"
	"testing"
)

func TestSummarizeManifestPlan_Fixtures_HooksAndNormalization(t *testing.T) {
	prev, err := os.ReadFile("../../testdata/deploy/plan/previous.yaml")
	if err != nil {
		t.Fatalf("read previous: %v", err)
	}
	next, err := os.ReadFile("../../testdata/deploy/plan/proposed.yaml")
	if err != nil {
		t.Fatalf("read proposed: %v", err)
	}
	summary, err := SummarizeManifestPlan(string(prev), string(next))
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	// Service clusterIP change is immutable => replace.
	if summary.Replace != 1 {
		t.Fatalf("expected 1 replace, got %#v", summary)
	}
	// Hook Job image change should be tracked separately (excluded from totals).
	if summary.Hooks.Change != 1 {
		t.Fatalf("expected 1 hook change, got %#v", summary.Hooks)
	}
	// checksum/* and helm.sh/chart annotations are stripped; ports order is normalized by name.
	if summary.Change != 0 {
		t.Fatalf("expected no non-hook change besides replace, got %#v", summary)
	}
}
