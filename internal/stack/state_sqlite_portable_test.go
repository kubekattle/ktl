package stack

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func TestStackStateSQLite_CheckpointPortable_SingleFileCopyOpens(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	s, err := openStackStateStore(root, false)
	if err != nil {
		t.Fatalf("open stack state store: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	runID := "run-1"
	now := time.Now().UTC().UnixNano()
	selectorJSON := `{"release":["foo"]}`
	planJSON := `{"apiVersion":"ktl.dev/stack-plan/v1"}`
	summaryJSON := `{"apiVersion":"ktl.dev/stack-run/v1"}`

	_, err = s.db.ExecContext(ctx, `
INSERT INTO ktl_stack_runs (
  run_id, stack_root, stack_name, profile, command, concurrency, fail_mode, status,
  created_at_ns, updated_at_ns, selector_json, plan_json, summary_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, root, "stack", "default", "apply", 1, "fail-fast", "running",
		now, now, selectorJSON, planJSON, summaryJSON)
	if err != nil {
		_ = s.Close()
		t.Fatalf("insert run: %v", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ktl_stack_nodes (run_id, node_id, status, attempt, error)
VALUES (?, ?, ?, ?, ?)
`, runID, "node-1", "planned", 0, "")
	if err != nil {
		_ = s.Close()
		t.Fatalf("insert node: %v", err)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ktl_stack_events (run_id, ts_ns, node_id, type, attempt, message, error_class, error_message, error_digest)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, now, "node-1", "NODE_RUNNING", 1, "", "", "", "")
	if err != nil {
		_ = s.Close()
		t.Fatalf("insert event: %v", err)
	}

	if err := s.CheckpointPortable(ctx); err != nil {
		_ = s.Close()
		t.Fatalf("checkpoint portable: %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	dbPath := filepath.Join(root, stackStateSQLiteRelPath)
	walPath := dbPath + "-wal"
	if st, err := os.Stat(walPath); err == nil {
		// Some SQLite builds may leave an empty WAL file behind; portability still holds.
		if st.Size() > 0 {
			t.Fatalf("expected WAL to be absent or empty after checkpoint, got %d bytes at %s", st.Size(), walPath)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("stat wal: %v", err)
	}

	dstDir := t.TempDir()
	dstDB := filepath.Join(dstDir, "state.sqlite")
	srcBytes, err := os.ReadFile(dbPath)
	if err != nil {
		t.Fatalf("read db: %v", err)
	}
	if err := os.WriteFile(dstDB, srcBytes, 0o600); err != nil {
		t.Fatalf("write copied db: %v", err)
	}

	db, err := sql.Open("sqlite", dstDB)
	if err != nil {
		t.Fatalf("open copied sqlite: %v", err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	var runs int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ktl_stack_runs;`).Scan(&runs); err != nil {
		t.Fatalf("query copied runs: %v", err)
	}
	if runs != 1 {
		t.Fatalf("expected 1 run in copied db, got %d", runs)
	}

	var events int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM ktl_stack_events;`).Scan(&events); err != nil {
		t.Fatalf("query copied events: %v", err)
	}
	if events != 1 {
		t.Fatalf("expected 1 event in copied db, got %d", events)
	}
}
