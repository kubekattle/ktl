package main

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/tailer"
)

func TestSQLiteStore_ReadsKtlCaptureDB(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "capture.sqlite")

	rec, err := capture.Open(path, capture.SessionMeta{
		Command:   "ktl logs",
		Args:      []string{"-A", "--capture"},
		StartedAt: time.Now().UTC(),
		Host:      "test-host",
		Tags:      map[string]string{"env": "test"},
	})
	if err != nil {
		t.Fatalf("open recorder: %v", err)
	}

	now := time.Now().UTC()
	rec.ObserveLog(tailer.LogRecord{
		Timestamp: now,
		Namespace: "prod",
		Pod:       "api-123",
		Container: "api",
		Raw:       "hello",
		Rendered:  "hello",
		Source:    "pod",
	})
	rec.ObserveLog(tailer.LogRecord{
		Timestamp: now.Add(50 * time.Millisecond),
		Namespace: "prod",
		Pod:       "api-123",
		Container: "api",
		Raw:       "ERROR boom",
		Rendered:  "ERROR boom",
		Source:    "pod",
	})
	rec.ObserveSelection(tailer.SelectionSnapshot{
		Timestamp:  now.Add(100 * time.Millisecond),
		ChangeKind: "add",
		Reason:     "test",
		Namespace:  "prod",
		Pod:        "api-123",
		Container:  "api",
	})
	rec.HandleDeployEvent(deploy.StreamEvent{
		Kind:      deploy.StreamEventLog,
		Timestamp: now.Add(150 * time.Millisecond).Format(time.RFC3339Nano),
		Log: &deploy.LogPayload{
			Level:     "warn",
			Message:   "waiting for rollout",
			Namespace: "prod",
		},
	})
	_ = rec.RecordArtifact(context.Background(), "apply.manifest", "kind: Deployment\n")

	if err := rec.Close(); err != nil {
		t.Fatalf("close recorder: %v", err)
	}

	st, err := openSQLiteStore(path, true)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	sessions, err := st.ListSessions(context.Background())
	if err != nil {
		t.Fatalf("ListSessions: %v", err)
	}
	if len(sessions) != 1 {
		t.Fatalf("sessions=%d, want 1", len(sessions))
	}
	sessionID := sessions[0].SessionID

	tl, err := st.Timeline(context.Background(), sessionID, time.Second, 0, 0, "")
	if err != nil {
		t.Fatalf("Timeline: %v", err)
	}
	var logsTotal, logsFail, deployTotal, selectionTotal, artifactTotal int64
	for _, r := range tl {
		logsTotal += r.LogsTotal
		logsFail += r.LogsFail
		deployTotal += r.Deploy
		selectionTotal += r.Selection
		artifactTotal += r.Artifacts
	}
	if logsTotal != 2 {
		t.Fatalf("logsTotal=%d, want 2", logsTotal)
	}
	if logsFail != 1 {
		t.Fatalf("logsFail=%d, want 1", logsFail)
	}
	if deployTotal != 1 {
		t.Fatalf("deployTotal=%d, want 1", deployTotal)
	}
	if selectionTotal != 1 {
		t.Fatalf("selectionTotal=%d, want 1", selectionTotal)
	}
	if artifactTotal < 1 {
		t.Fatalf("artifactTotal=%d, want >=1", artifactTotal)
	}

	logsPage, err := st.Logs(context.Background(), sessionID, 0, 10, "", 0, 0)
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(logsPage.Lines) != 2 {
		t.Fatalf("Logs lines=%d, want 2", len(logsPage.Lines))
	}

	eventsPage, err := st.Events(context.Background(), sessionID, 0, 10, "", 0, 0)
	if err != nil {
		t.Fatalf("Events: %v", err)
	}
	if len(eventsPage.Events) < 2 {
		t.Fatalf("Events=%d, want >=2", len(eventsPage.Events))
	}
}
