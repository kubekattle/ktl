// archive_test.go verifies archive creation and integrity routines.
package apparchive

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestBuilderCreatesSnapshots(t *testing.T) {
	tmp := t.TempDir()
	path := filepath.Join(tmp, "app.k8s")

	base, err := NewBuilder(path, SnapshotMetadata{Name: "base", Release: "demo"})
	if err != nil {
		t.Fatalf("new builder: %v", err)
	}
	err = base.AddManifest(ManifestRecord{
		ID:         "cm-demo",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Namespace:  "default",
		Name:       "demo",
		Body:       "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\ndata:\n  key: value\n",
	})
	if err != nil {
		t.Fatalf("add manifest: %v", err)
	}
	err = base.AddAttachment(Attachment{Name: "notes.txt", MediaType: "text/plain", Data: []byte("hello")})
	if err != nil {
		t.Fatalf("add attachment: %v", err)
	}
	if err := base.Close(); err != nil {
		t.Fatalf("close base: %v", err)
	}

	patch, err := NewBuilder(path, SnapshotMetadata{Name: "patch", Parent: "base", Release: "demo"})
	if err != nil {
		t.Fatalf("new patch builder: %v", err)
	}
	err = patch.AddManifest(ManifestRecord{
		ID:         "cm-demo",
		APIVersion: "v1",
		Kind:       "ConfigMap",
		Namespace:  "default",
		Name:       "demo",
		Body:       "apiVersion: v1\nkind: ConfigMap\nmetadata:\n  name: demo\ndata:\n  key: updated\n",
	})
	if err != nil {
		t.Fatalf("add manifest patch: %v", err)
	}
	if err := patch.Close(); err != nil {
		t.Fatalf("close patch: %v", err)
	}

	infos, err := ListSnapshots(path)
	if err != nil {
		t.Fatalf("list snapshots: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("expected 2 snapshots, got %d", len(infos))
	}
	if infos[1].ParentID == nil || *infos[1].ParentID != infos[0].ID {
		t.Fatalf("expected patch parent to be base")
	}

	blobCount := countRows(t, path, "blobs")
	if blobCount != 3 { // two manifests + attachment
		t.Fatalf("expected 3 blobs, got %d", blobCount)
	}
}

func countRows(t *testing.T, path, table string) int {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&count); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	return count
}
