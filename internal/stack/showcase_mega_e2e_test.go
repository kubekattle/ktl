package stack

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

func TestShowcaseMega_PlanInferAndRun(t *testing.T) {
	srcRoot := filepath.Clean(filepath.Join("..", "..", "testdata", "stack", "showcase", "mega"))
	root := t.TempDir()
	copyDir(t, srcRoot, root)
	// Never trust developer-local artifacts under testdata (e.g. sqlite state from a real run).
	_ = os.RemoveAll(filepath.Join(root, ".ktl"))

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}
	if err := InferDependencies(context.Background(), p, "", "", InferDepsOptions{IncludeConfigRefs: true}); err != nil {
		t.Fatalf("InferDependencies: %v", err)
	}
	if err := RecomputeExecutionGroups(p); err != nil {
		t.Fatalf("RecomputeExecutionGroups: %v", err)
	}

	getByName := func(cluster, name string) *ResolvedRelease {
		for _, n := range p.ByCluster[cluster] {
			if n.Name == name {
				return n
			}
		}
		return nil
	}

	api := getByName("c1", "api")
	if api == nil {
		t.Fatalf("missing c1/api")
	}
	needSet := map[string]struct{}{}
	for _, dep := range api.Needs {
		needSet[dep] = struct{}{}
	}
	for _, want := range []string{"crds", "rbac", "storage", "config"} {
		if _, ok := needSet[want]; !ok {
			t.Fatalf("expected c1/api to need %q; needs=%v", want, api.Needs)
		}
	}

	frontend := getByName("c1", "frontend")
	if frontend == nil {
		t.Fatalf("missing c1/frontend")
	}
	if !contains(frontend.Needs, "api") || !contains(frontend.Needs, "worker") {
		t.Fatalf("expected c1/frontend to need api+worker; needs=%v", frontend.Needs)
	}

	apiC2 := getByName("c1", "api-c2")
	if apiC2 == nil {
		t.Fatalf("missing c1/api-c2")
	}
	for _, want := range []string{"crds", "rbac-c2", "storage-c2", "config-c2"} {
		if !contains(apiC2.Needs, want) {
			t.Fatalf("expected c1/api-c2 to need %q; needs=%v", want, apiC2.Needs)
		}
	}

	// Run the scheduler end-to-end (no Kubernetes) using the existing fake executor.
	exec := &fakeExecutor{}
	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 4,
		Lock:        true,
		Executor:    exec,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr:\n%s", err, errOut.String())
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	loaded, err := LoadRun(root, runID)
	if err != nil {
		t.Fatalf("LoadRun: %v", err)
	}
	// Ensure we recorded a deterministic order and every node completed.
	if len(loaded.Plan.Nodes) == 0 {
		t.Fatalf("expected nodes in loaded plan")
	}
	var ids []string
	for _, n := range loaded.Plan.Nodes {
		ids = append(ids, n.ID)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if strings.TrimSpace(loaded.StatusByID[id]) != "succeeded" {
			t.Fatalf("expected %s succeeded; status=%q", id, loaded.StatusByID[id])
		}
	}
}

func contains(list []string, v string) bool {
	for _, s := range list {
		if strings.TrimSpace(s) == strings.TrimSpace(v) {
			return true
		}
	}
	return false
}
