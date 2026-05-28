package stack

import (
	"errors"
	"testing"
)

func TestScheduler_EmitsBlockedOnNextReady(t *testing.T) {
	a := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/a",
		Name:      "a",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
	}}
	b := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/b",
		Name:      "b",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
		Needs:     []string{"a"},
	}}

	s := newScheduler([]*runNode{a, b}, "apply")

	got := s.NextReady()
	if got == nil || got.ID != "c1/ns/a" {
		t.Fatalf("expected first node a, got %#v", got)
	}
	s.MarkFailed("c1/ns/a", errors.New("boom"))

	_ = s.NextReady()
	blocked := s.TakeNewlyBlocked()
	if blocked == nil {
		t.Fatalf("expected blocked nodes")
	}
	reason := blocked["c1/ns/b"]
	if reason == "" {
		t.Fatalf("expected blocked reason for b, got %+v", blocked)
	}
}

func TestScheduler_EmitsBlockedOnFinalizeBlocked(t *testing.T) {
	a := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/a",
		Name:      "a",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
	}}
	b := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/b",
		Name:      "b",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
		Needs:     []string{"a"},
	}}

	s := newScheduler([]*runNode{a, b}, "apply")
	got := s.NextReady()
	if got == nil || got.ID != "c1/ns/a" {
		t.Fatalf("expected first node a, got %#v", got)
	}
	s.MarkFailed("c1/ns/a", errors.New("boom"))
	s.Stop()

	s.FinalizeBlocked()
	blocked := s.TakeNewlyBlocked()
	if blocked == nil || blocked["c1/ns/b"] == "" {
		t.Fatalf("expected blocked b after finalize, got %+v", blocked)
	}
}

func TestScheduler_MarkBlockedPropagatesToDependents(t *testing.T) {
	a := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/a",
		Name:      "a",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
	}}
	b := &runNode{ResolvedRelease: &ResolvedRelease{
		ID:        "c1/ns/b",
		Name:      "b",
		Namespace: "ns",
		Cluster:   ClusterTarget{Name: "c1"},
		Needs:     []string{"a"},
	}}

	s := newScheduler([]*runNode{a, b}, "apply")
	got := s.NextReady()
	if got == nil || got.ID != "c1/ns/a" {
		t.Fatalf("expected first node a, got %#v", got)
	}
	s.MarkBlocked("c1/ns/a", "advisory lock blocked", newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", "advisory lock blocked", nil))

	_ = s.NextReady()
	blocked := s.TakeNewlyBlocked()
	if blocked == nil || blocked["c1/ns/b"] == "" {
		t.Fatalf("expected dependent blocked after parent block, got %+v", blocked)
	}
	snap := s.Snapshot()
	if snap.Status["c1/ns/a"] != "blocked" || snap.Status["c1/ns/b"] != "blocked" {
		t.Fatalf("snapshot statuses = %#v, want both blocked", snap.Status)
	}
	if runStatusFromSnapshot(newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", "blocked", nil), snap) != "blocked" {
		t.Fatalf("run status = %q, want blocked", runStatusFromSnapshot(newBlockedRunError("POSTGRES_RESOURCE_BLOCKED", "blocked", nil), snap))
	}
}
