// File: internal/sqlitewriter/writer_test.go
// Brief: Internal sqlitewriter package implementation for 'writer'.

// writer_test.go validates the SQLite writer's schema and durability behavior.
package sqlitewriter

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestWriterPersistsEntry(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "logs.db")
	writer, err := New(path)
	if err != nil {
		t.Fatalf("New returned error: %v", err)
	}
	t.Cleanup(func() {
		if err := writer.Close(); err != nil {
			t.Fatalf("Close returned error: %v", err)
		}
	})

	entry := Entry{
		CollectedAt:  time.Unix(1700000000, 0),
		LogTimestamp: "2023-11-14T12:10:00Z",
		Namespace:    "default",
		Pod:          "demo-abc",
		Container:    "app",
		Raw:          "hello world",
		Rendered:     "rendered line",
	}

	if err := writer.Write(context.Background(), entry); err != nil {
		t.Fatalf("Write returned error: %v", err)
	}

	verifyDB, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open verification db: %v", err)
	}
	defer verifyDB.Close()

	var (
		gotCollected string
		gotTimestamp string
		gotPod       string
		gotRendered  string
	)
	row := verifyDB.QueryRow(`SELECT collected_at, log_timestamp, pod, rendered FROM logs LIMIT 1`)
	if err := row.Scan(&gotCollected, &gotTimestamp, &gotPod, &gotRendered); err != nil {
		t.Fatalf("Scan failed: %v", err)
	}
	if gotTimestamp != entry.LogTimestamp {
		t.Fatalf("unexpected log timestamp: want %s got %s", entry.LogTimestamp, gotTimestamp)
	}
	if gotPod != entry.Pod {
		t.Fatalf("unexpected pod value: want %s got %s", entry.Pod, gotPod)
	}
	if gotRendered != entry.Rendered {
		t.Fatalf("unexpected rendered value: want %s got %s", entry.Rendered, gotRendered)
	}
	if gotCollected == "" {
		t.Fatalf("collected_at should not be empty")
	}
}
