package main

import (
	"testing"

	"helm.sh/helm/v3/pkg/release"
)

func rel(version int, status release.Status) *release.Release {
	return &release.Release{
		Name:    "r",
		Version: version,
		Info:    &release.Info{Status: status},
	}
}

func TestSelectRevertTarget_ExplicitRevision(t *testing.T) {
	sel, err := selectRevertTarget("r", []*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusDeployed),
	}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.FromRevision != 2 || sel.ToRevision != 1 {
		t.Fatalf("got from=%d to=%d", sel.FromRevision, sel.ToRevision)
	}
}

func TestSelectRevertTarget_PicksLastKnownGoodSuperseded(t *testing.T) {
	sel, err := selectRevertTarget("r", []*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusSuperseded),
		rel(3, release.StatusDeployed),
	}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.FromRevision != 3 || sel.ToRevision != 2 {
		t.Fatalf("got from=%d to=%d", sel.FromRevision, sel.ToRevision)
	}
}

func TestSelectRevertTarget_CurrentFailedUsesLastDeployed(t *testing.T) {
	sel, err := selectRevertTarget("r", []*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusDeployed),
		rel(3, release.StatusFailed),
	}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sel.FromRevision != 2 || sel.ToRevision != 1 {
		t.Fatalf("got from=%d to=%d", sel.FromRevision, sel.ToRevision)
	}
}

func TestSelectRevertTarget_NoPreviousSuccess(t *testing.T) {
	_, err := selectRevertTarget("r", []*release.Release{
		rel(1, release.StatusDeployed),
	}, 0)
	if err == nil {
		t.Fatalf("expected error")
	}
}
