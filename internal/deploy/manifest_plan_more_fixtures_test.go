// File: internal/deploy/manifest_plan_more_fixtures_test.go
// Brief: More fixture coverage for immutables and hooks.

package deploy

import (
	"os"
	"testing"
)

func TestSummarizeManifestPlan_Fixtures_StatefulSetSelectorReplace(t *testing.T) {
	prev := readFixture(t, "../../testdata/deploy/plan/statefulset_selector_previous.yaml")
	next := readFixture(t, "../../testdata/deploy/plan/statefulset_selector_proposed.yaml")
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Replace != 1 {
		t.Fatalf("expected 1 replace, got %#v", summary)
	}
}

func TestSummarizeManifestPlan_Fixtures_PVCStorageClassReplace(t *testing.T) {
	prev := readFixture(t, "../../testdata/deploy/plan/pvc_storageclass_previous.yaml")
	next := readFixture(t, "../../testdata/deploy/plan/pvc_storageclass_proposed.yaml")
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Replace != 1 {
		t.Fatalf("expected 1 replace, got %#v", summary)
	}
}

func TestSummarizeManifestPlan_Fixtures_IngressClassReplace(t *testing.T) {
	prev := readFixture(t, "../../testdata/deploy/plan/ingress_class_previous.yaml")
	next := readFixture(t, "../../testdata/deploy/plan/ingress_class_proposed.yaml")
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Replace != 1 {
		t.Fatalf("expected 1 replace, got %#v", summary)
	}
}

func TestSummarizeManifestPlan_Fixtures_HookDeletePolicyIsHookChange(t *testing.T) {
	prev := readFixture(t, "../../testdata/deploy/plan/hook_delete_policy_previous.yaml")
	next := readFixture(t, "../../testdata/deploy/plan/hook_delete_policy_proposed.yaml")
	summary, err := SummarizeManifestPlan(prev, next)
	if err != nil {
		t.Fatalf("summarize: %v", err)
	}
	if summary.Hooks.Change != 1 {
		t.Fatalf("expected 1 hook change, got %#v", summary.Hooks)
	}
	if summary.Add != 0 || summary.Change != 0 || summary.Replace != 0 || summary.Destroy != 0 {
		t.Fatalf("expected hook-only change, got %#v", summary)
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}
	return string(data)
}
