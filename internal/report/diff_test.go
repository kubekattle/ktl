// diff_test.go ensures diff rendering highlights changes as intended.
package report

import (
	"path/filepath"
	"testing"

	apparchive "github.com/example/ktl/internal/apparchive"
)

func TestBuildArchiveDiffHighlightsEnvChanges(t *testing.T) {
	dir := t.TempDir()
	leftPath := filepath.Join(dir, "left.k8s")
	rightPath := filepath.Join(dir, "right.k8s")

	leftBuilder, err := apparchive.NewBuilder(leftPath, apparchive.SnapshotMetadata{
		Name:      "left",
		Release:   "demo",
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("left builder: %v", err)
	}
	defer leftBuilder.Close()
	rightBuilder, err := apparchive.NewBuilder(rightPath, apparchive.SnapshotMetadata{
		Name:      "right",
		Release:   "demo",
		Namespace: "default",
	})
	if err != nil {
		t.Fatalf("right builder: %v", err)
	}
	defer rightBuilder.Close()

	leftManifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: demo:v1
        env:
        - name: MODE
          value: blue
`
	rightManifest := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
  namespace: default
spec:
  template:
    spec:
      containers:
      - name: app
        image: demo:v2
        env:
        - name: MODE
          value: green
`
	if err := leftBuilder.AddManifest(apparchive.ManifestRecord{
		ID:         "deployment-web",
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "web",
		Body:       leftManifest,
	}); err != nil {
		t.Fatalf("add left manifest: %v", err)
	}
	if err := rightBuilder.AddManifest(apparchive.ManifestRecord{
		ID:         "deployment-web",
		APIVersion: "apps/v1",
		Kind:       "Deployment",
		Namespace:  "default",
		Name:       "web",
		Body:       rightManifest,
	}); err != nil {
		t.Fatalf("add right manifest: %v", err)
	}
	leftBuilder.Close()
	rightBuilder.Close()

	diff, err := BuildArchiveDiff(
		ArchiveSource{Path: leftPath},
		ArchiveSource{Path: rightPath},
	)
	if err != nil {
		t.Fatalf("BuildArchiveDiff: %v", err)
	}
	if diff.Summary.Changed != 1 {
		t.Fatalf("expected 1 changed resource, got %+v", diff.Summary)
	}
	if len(diff.Resources) != 1 {
		t.Fatalf("expected 1 resource diff, got %d", len(diff.Resources))
	}
	res := diff.Resources[0]
	if res.Kind != "Deployment" || res.Name != "web" {
		t.Fatalf("unexpected resource %+v", res)
	}
	foundEnv := false
	for _, hl := range res.Highlights {
		if hl.Change == "env" && hl.Label == "Env · app/MODE" {
			foundEnv = true
			break
		}
	}
	if !foundEnv {
		t.Fatalf("expected env highlight, got %+v", res.Highlights)
	}
	if res.RollbackCommand == "" {
		t.Fatalf("expected rollback command, got empty string")
	}
}

func TestBuildArchiveDiffHandlesMissingArchives(t *testing.T) {
	if _, err := BuildArchiveDiff(ArchiveSource{}, ArchiveSource{}); err == nil {
		t.Fatalf("expected error for missing archive paths")
	}
}
