package main

import "testing"

func TestRepositoryFrom(t *testing.T) {
	repo, err := repositoryFrom("registry.example.com/app:dev")
	if err != nil {
		t.Fatalf("repositoryFrom error: %v", err)
	}
	if repo != "registry.example.com/app" {
		t.Fatalf("expected registry.example.com/app, got %s", repo)
	}
}
