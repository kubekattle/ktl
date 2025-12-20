// File: cmd/ktl/deploy_plan_test.go
// Brief: CLI command wiring and implementation for 'deploy plan'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"strings"
	"testing"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"sigs.k8s.io/yaml"
)

func TestBuildPlanChanges(t *testing.T) {
	desiredManifest := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-env
  namespace: default
data:
  FOO: bar
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  replicas: 2
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: fresh
  namespace: default
data:
  NEW: value
`
	previousManifest := `---
apiVersion: v1
kind: ConfigMap
metadata:
  name: web-env
  namespace: default
data:
  FOO: old
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: legacy
  namespace: default
data:
  OLD: value
`

	desired := docsToMap(parseManifestDocs(desiredManifest))
	previous := docsToMap(parseManifestDocs(previousManifest))
	live := map[resourceKey]*unstructured.Unstructured{}

	for key, doc := range desired {
		switch key.Name {
		case "web-env":
			live[key] = mustUnstructured(t, `apiVersion: v1
kind: ConfigMap
metadata:
  name: web-env
  namespace: default
data:
  FOO: old
`)
		case "web":
			live[key] = doc.Obj.DeepCopy()
		}
	}

	changes, summary := buildPlanChanges(desired, previous, live)

	if summary.Creates != 1 || summary.Updates != 1 || summary.Deletes != 1 || summary.Unchanged != 1 {
		t.Fatalf("unexpected summary: %+v", summary)
	}

	assertChange := func(kind planChangeKind, name string) *planResourceChange {
		for _, ch := range changes {
			if ch.Kind == kind && ch.Key.Name == name {
				return &ch
			}
		}
		t.Fatalf("missing %s change for %s", kind, name)
		return nil
	}

	if diff := assertChange(changeCreate, "fresh").Diff; diff == "" {
		t.Fatalf("expected diff for create")
	}
	if diff := assertChange(changeUpdate, "web-env").Diff; diff == "" || !contains(diff, "FOO") {
		t.Fatalf("expected diff to mention FOO, got %q", diff)
	}
	if diff := assertChange(changeDelete, "legacy").Diff; diff == "" {
		t.Fatalf("expected diff for delete")
	}
}

func TestPlanWarnings(t *testing.T) {
	changes := []planResourceChange{
		{Key: resourceKey{Name: "api", Namespace: "default", Kind: "Deployment"}, Kind: changeUpdate},
		{Key: resourceKey{Name: "pdb", Namespace: "default", Kind: "PodDisruptionBudget"}, Kind: changeDelete},
		{Key: resourceKey{Name: "jobs", Namespace: "default", Kind: "Job"}, Kind: changeDelete},
	}

	warnings := planWarnings(changes)
	if len(warnings) < 3 {
		t.Fatalf("expected at least 3 warnings, got %v", warnings)
	}

	expectContains(t, warnings, "Deployment")
	expectContains(t, warnings, "PodDisruptionBudget")
	expectContains(t, warnings, "Job")
}

func TestBuildPlanChangesFallsBackToPrevious(t *testing.T) {
	desiredManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: default
data:
  FOO: new
`
	previousManifest := `apiVersion: v1
kind: ConfigMap
metadata:
  name: api-config
  namespace: default
data:
  FOO: old
`
	desired := docsToMap(parseManifestDocs(desiredManifest))
	previous := docsToMap(parseManifestDocs(previousManifest))

	changes, summary := buildPlanChanges(desired, previous, nil)
	if summary.Updates != 1 || summary.Creates != 0 {
		t.Fatalf("expected one update when falling back, got summary %+v", summary)
	}
	if len(changes) != 1 || changes[0].Kind != changeUpdate {
		t.Fatalf("expected single update change, got %+v", changes)
	}
	if !strings.Contains(changes[0].Diff, "FOO") {
		t.Fatalf("expected diff to mention key change, got %q", changes[0].Diff)
	}
}

func mustUnstructured(t *testing.T, body string) *unstructured.Unstructured {
	t.Helper()
	var obj map[string]interface{}
	if err := yaml.Unmarshal([]byte(body), &obj); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return &unstructured.Unstructured{Object: obj}
}

func expectContains(t *testing.T, items []string, needle string) {
	t.Helper()
	for _, item := range items {
		if strings.Contains(item, needle) {
			return
		}
	}
	t.Fatalf("expected to find %q inside %v", needle, items)
}

func contains(haystack, needle string) bool {
	return strings.Contains(haystack, needle)
}
