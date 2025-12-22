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
	from, to, err := selectRevertTarget([]*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusDeployed),
	}, 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from != 2 || to != 1 {
		t.Fatalf("got from=%d to=%d", from, to)
	}
}

func TestSelectRevertTarget_PicksLastKnownGoodSuperseded(t *testing.T) {
	from, to, err := selectRevertTarget([]*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusSuperseded),
		rel(3, release.StatusDeployed),
	}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from != 3 || to != 2 {
		t.Fatalf("got from=%d to=%d", from, to)
	}
}

func TestSelectRevertTarget_CurrentFailedUsesLastDeployed(t *testing.T) {
	from, to, err := selectRevertTarget([]*release.Release{
		rel(1, release.StatusSuperseded),
		rel(2, release.StatusDeployed),
		rel(3, release.StatusFailed),
	}, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if from != 2 || to != 1 {
		t.Fatalf("got from=%d to=%d", from, to)
	}
}

