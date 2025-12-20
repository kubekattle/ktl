package capture

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSliceWritesFilteredArtifact(t *testing.T) {
	dir := t.TempDir()
	entries := []Entry{
		{Timestamp: time.Date(2025, 12, 16, 1, 0, 0, 0, time.UTC), Namespace: "a", Pod: "p1", Container: "c", Raw: "r1", Rendered: "one"},
		{Timestamp: time.Date(2025, 12, 16, 1, 0, 1, 0, time.UTC), Namespace: "b", Pod: "p2", Container: "c", Raw: "r2", Rendered: "two"},
		{Timestamp: time.Date(2025, 12, 16, 1, 0, 2, 0, time.UTC), Namespace: "a", Pod: "p3", Container: "c", Raw: "r3", Rendered: "three"},
	}
	logPath := filepath.Join(dir, "logs.jsonl")
	f, err := os.Create(logPath)
	if err != nil {
		t.Fatalf("create logs.jsonl: %v", err)
	}
	for _, e := range entries {
		line, _ := json.Marshal(e)
		f.Write(append(line, '\n'))
	}
	f.Close()
	meta := Metadata{
		SessionName: "test",
		StartedAt:   entries[0].Timestamp,
		EndedAt:     entries[2].Timestamp,
		Namespaces:  []string{"a", "b"},
		PodQuery:    ".*",
		PodCount:    3,
	}
	metaFile, _ := os.Create(filepath.Join(dir, "metadata.json"))
	enc := json.NewEncoder(metaFile)
	enc.Encode(meta)
	metaFile.Close()

	out := filepath.Join(t.TempDir(), "slice.tar.gz")
	_, err = Slice(context.Background(), dir, out, ReplayOptions{Namespaces: []string{"a"}})
	if err != nil {
		t.Fatalf("Slice returned error: %v", err)
	}
	info, err := os.Stat(out)
	if err != nil {
		t.Fatalf("stat slice: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("expected non-empty slice file")
	}

	// Cheap sanity check that it's a gzip stream.
	r, err := os.Open(out)
	if err != nil {
		t.Fatalf("open slice: %v", err)
	}
	defer r.Close()
	gzr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatalf("gzip reader: %v", err)
	}
	gzr.Close()

	var got []Entry
	err = ReplayEntries(context.Background(), out, ReplayOptions{PreferJSON: true}, func(e Entry) error {
		got = append(got, e)
		return nil
	})
	if err != nil {
		t.Fatalf("ReplayEntries on slice: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("unexpected entry count %d (want 2)", len(got))
	}
	for _, e := range got {
		if e.Namespace != "a" {
			t.Fatalf("unexpected namespace %q", e.Namespace)
		}
	}
}
