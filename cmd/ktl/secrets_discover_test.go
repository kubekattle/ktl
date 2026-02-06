package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

var _, discoverTestFile, _, _ = runtime.Caller(0)
var discoverRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(discoverTestFile), "..", ".."))

func discoverTestdata(parts ...string) string {
	base := append([]string{discoverRepoRoot, "testdata", "secrets", "discover"}, parts...)
	return filepath.Join(base...)
}

func TestSecretsDiscoverChart(t *testing.T) {
	chartPath := discoverTestdata("chart")
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"secrets", "discover", "--scope", "chart", "--chart", chartPath, "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload secretDiscoverOutput
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}

	foundLocal := false
	foundVault := false
	for _, ref := range payload.Refs {
		if ref.Provider == "local" && strings.HasPrefix(ref.Path, "db/") {
			foundLocal = true
		}
		if ref.Provider == "vault" && strings.HasPrefix(ref.Path, "app/token") {
			foundVault = true
		}
	}
	if !foundLocal || !foundVault {
		t.Fatalf("expected local and vault refs, got: %+v", payload.Refs)
	}
}

func TestSecretsDiscoverRepo(t *testing.T) {
	repoRoot := discoverTestdata("")
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"secrets", "discover", "--scope", "repo", "--root", repoRoot, "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload secretDiscoverOutput
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	if len(payload.Refs) < 2 {
		t.Fatalf("expected multiple refs, got: %+v", payload.Refs)
	}
}

func TestSecretsDiscoverStack(t *testing.T) {
	stackRoot := discoverTestdata("stack")
	root := newRootCommand()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"secrets", "discover", "--scope", "stack", "--config", stackRoot, "--output", "json"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}

	var payload secretDiscoverOutput
	if err := json.Unmarshal(out.Bytes(), &payload); err != nil {
		t.Fatalf("parse json: %v", err)
	}
	foundOwner := false
	for _, ref := range payload.Refs {
		for _, owner := range ref.Owners {
			if owner == "local/default/app" {
				foundOwner = true
				break
			}
		}
	}
	if !foundOwner {
		t.Fatalf("expected stack release owner local/default/app, got: %+v", payload.Refs)
	}
}
