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
