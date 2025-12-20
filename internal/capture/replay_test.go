package capture

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestReplayEntriesRequiresEmit(t *testing.T) {
	if err := ReplayEntries(context.Background(), "/does/not/matter", ReplayOptions{}, nil); err == nil {
		t.Fatalf("expected error for nil emit")
	}
}

func TestReplayEntriesFiltersAndLimits(t *testing.T) {
	dir := t.TempDir()
	entries := []Entry{
		{
			Timestamp: time.Date(2025, 12, 16, 1, 2, 3, 0, time.UTC),
			Namespace: "prod",
			Pod:       "api-1",
			Container: "api",
			Raw:       "raw a",
			Rendered:  "INFO a",
		},
		{
			Timestamp: time.Date(2025, 12, 16, 1, 2, 4, 0, time.UTC),
			Namespace: "prod",
			Pod:       "api-2",
			Container: "proxy",
			Raw:       "raw b",
			Rendered:  "ERROR b",
		},
		{
			Timestamp: time.Date(2025, 12, 16, 1, 2, 5, 0, time.UTC),
			Namespace: "dev",
			Pod:       "api-3",
			Container: "api",
			Raw:       "raw c",
			Rendered:  "INFO c",
		},
	}
	dataPath := filepath.Join(dir, "logs.jsonl")
	f, err := os.Create(dataPath)
	if err != nil {
		t.Fatalf("create logs.jsonl: %v", err)
	}
	for _, entry := range entries {
		line, err := json.Marshal(entry)
		if err != nil {
			t.Fatalf("marshal entry: %v", err)
		}
		if _, err := f.Write(append(line, '\n')); err != nil {
			t.Fatalf("write entry: %v", err)
		}
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close logs.jsonl: %v", err)
	}

	opts := ReplayOptions{
		PreferJSON: true,
		Namespaces: []string{"prod"},
		Grep:       []string{"error"},
		Limit:      1,
	}
	var got []Entry
	if err := ReplayEntries(context.Background(), dir, opts, func(e Entry) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatalf("ReplayEntries returned error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("unexpected entry count %d (want 1)", len(got))
	}
	if got[0].Pod != "api-2" {
		t.Fatalf("unexpected entry pod %q (want api-2)", got[0].Pod)
	}
	if got[0].FormattedTimestamp == "" {
		t.Fatalf("expected FormattedTimestamp to be populated")
	}
}
