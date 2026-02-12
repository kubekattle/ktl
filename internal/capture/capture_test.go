package capture

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/tailer"
)

func TestRecorderWritesSessionAndEvents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "cap.sqlite")
	rec, err := Open(dbPath, SessionMeta{
		Command:   "ktl logs",
		Args:      []string{"foo"},
		StartedAt: time.Now().UTC(),
		Extra:     map[string]string{"x": "y"},
	})
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	rec.ObserveLog(tailer.LogRecord{
		Timestamp: time.Now().UTC(),
		Source:    "test",
		Namespace: "default",
		Pod:       "p",
		Container: "c",
		Rendered:  "hello",
	})
	rec.HandleDeployEvent(deploy.StreamEvent{
		Kind:      deploy.StreamEventLog,
		Timestamp: time.Now().UTC().Format(time.RFC3339Nano),
		Log:       &deploy.LogPayload{Level: "info", Message: "deploy"},
		Summary:   &deploy.SummaryPayload{Release: "r", Namespace: "default", Status: "pending"},
	})
	if err := rec.RecordArtifact(context.Background(), "rendered_manifest", "kind: ConfigMap\n"); err != nil {
		t.Fatalf("RecordArtifact: %v", err)
	}

	if err := rec.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := os.Stat(dbPath + "-wal"); err == nil {
		t.Fatalf("expected no -wal sidecar after Close")
	}
	if _, err := os.Stat(dbPath + "-shm"); err == nil {
		t.Fatalf("expected no -shm sidecar after Close")
	}

	// Validate via a separate connection.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var sessionCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ktl_capture_sessions`).Scan(&sessionCount); err != nil {
		t.Fatalf("count sessions: %v", err)
	}
	if sessionCount != 1 {
		t.Fatalf("expected 1 session, got %d", sessionCount)
	}
	var eventCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ktl_capture_events`).Scan(&eventCount); err != nil {
		t.Fatalf("count events: %v", err)
	}
	if eventCount != 2 {
		t.Fatalf("expected 2 events, got %d", eventCount)
	}
	var minSeq sql.NullInt64
	var maxSeq sql.NullInt64
	if err := db.QueryRow(`SELECT MIN(seq), MAX(seq) FROM ktl_capture_events`).Scan(&minSeq, &maxSeq); err != nil {
		t.Fatalf("seq range: %v", err)
	}
	if !minSeq.Valid || !maxSeq.Valid || minSeq.Int64 <= 0 || maxSeq.Int64 < minSeq.Int64 {
		t.Fatalf("unexpected seq range: min=%v max=%v", minSeq, maxSeq)
	}
	var artifactCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM ktl_capture_artifacts`).Scan(&artifactCount); err != nil {
		t.Fatalf("count artifacts: %v", err)
	}
	if artifactCount != 1 {
		t.Fatalf("expected 1 artifact, got %d", artifactCount)
	}
}
