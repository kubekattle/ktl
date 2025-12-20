package capture

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 2

func migrate(ctx context.Context, db *sql.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA foreign_keys=ON;`,
		`PRAGMA synchronous=NORMAL;`,
		`PRAGMA busy_timeout=5000;`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init capture pragma: %w", err)
		}
	}

	if _, err := db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS ktl_capture_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`); err != nil {
		return fmt.Errorf("create capture meta: %w", err)
	}

	current, err := currentSchemaVersion(ctx, db)
	if err != nil {
		return err
	}
	for current < schemaVersion {
		next := current + 1
		if err := applyMigration(ctx, db, next); err != nil {
			return err
		}
		if err := setSchemaVersion(ctx, db, next); err != nil {
			return err
		}
		current = next
	}
	return nil
}

func currentSchemaVersion(ctx context.Context, db *sql.DB) (int, error) {
	var v string
	err := db.QueryRowContext(ctx, `SELECT value FROM ktl_capture_meta WHERE key = 'schema_version'`).Scan(&v)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	if err != nil {
		return 0, fmt.Errorf("read schema_version: %w", err)
	}
	var n int
	_, _ = fmt.Sscanf(v, "%d", &n)
	return n, nil
}

func setSchemaVersion(ctx context.Context, db *sql.DB, v int) error {
	if _, err := db.ExecContext(ctx, `
INSERT INTO ktl_capture_meta(key, value) VALUES('schema_version', ?)
ON CONFLICT(key) DO UPDATE SET value = excluded.value
`, fmt.Sprintf("%d", v)); err != nil {
		return fmt.Errorf("write schema_version: %w", err)
	}
	return nil
}

func applyMigration(ctx context.Context, db *sql.DB, toVersion int) error {
	switch toVersion {
	case 1:
		// Baseline schema for initial capture.
		stmts := []string{
			`CREATE TABLE IF NOT EXISTS ktl_capture_sessions (
  session_id TEXT PRIMARY KEY,
  command TEXT NOT NULL,
  meta_json TEXT NOT NULL,
  started_at TEXT NOT NULL,
  ended_at TEXT
);`,
			`CREATE TABLE IF NOT EXISTS ktl_capture_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  ts TEXT NOT NULL,
  kind TEXT NOT NULL,
  level TEXT,
  source TEXT,
  namespace TEXT,
  pod TEXT,
  container TEXT,
  stream TEXT,
  message TEXT,
  raw_json TEXT,
  FOREIGN KEY(session_id) REFERENCES ktl_capture_sessions(session_id) ON DELETE CASCADE
);`,
			`CREATE INDEX IF NOT EXISTS idx_capture_events_session_ts ON ktl_capture_events(session_id, ts);`,
			`CREATE INDEX IF NOT EXISTS idx_capture_events_kind ON ktl_capture_events(session_id, kind);`,
			`CREATE TABLE IF NOT EXISTS ktl_capture_artifacts (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  ts TEXT NOT NULL,
  name TEXT NOT NULL,
  text TEXT NOT NULL,
  FOREIGN KEY(session_id) REFERENCES ktl_capture_sessions(session_id) ON DELETE CASCADE
);`,
			`CREATE INDEX IF NOT EXISTS idx_capture_artifacts_session_name ON ktl_capture_artifacts(session_id, name);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				return fmt.Errorf("migration v1: %w", err)
			}
		}
		return nil
	case 2:
		// UI-first schema improvements:
		// - run_id + parent_run_id
		// - entity columns
		// - typed timestamps (epoch ns)
		// - monotonic seq per session
		// - payload blob (future compression) + keep payload_json for compatibility
		stmts := []string{
			`ALTER TABLE ktl_capture_sessions ADD COLUMN run_id TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN parent_run_id TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN started_at_ns INTEGER;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN ended_at_ns INTEGER;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN cluster TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN kube_context TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN namespace TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN release TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN chart TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN image_ref TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN image_digest TEXT;`,
			`ALTER TABLE ktl_capture_sessions ADD COLUMN build_context TEXT;`,
			`CREATE INDEX IF NOT EXISTS idx_capture_sessions_run_id ON ktl_capture_sessions(run_id);`,

			`ALTER TABLE ktl_capture_events ADD COLUMN seq INTEGER;`,
			`ALTER TABLE ktl_capture_events ADD COLUMN ts_ns INTEGER;`,
			`ALTER TABLE ktl_capture_events ADD COLUMN payload_type TEXT;`,
			`ALTER TABLE ktl_capture_events ADD COLUMN payload_blob BLOB;`,
			`ALTER TABLE ktl_capture_events ADD COLUMN payload_json TEXT;`,
			`CREATE INDEX IF NOT EXISTS idx_capture_events_session_seq ON ktl_capture_events(session_id, seq);`,

			`ALTER TABLE ktl_capture_artifacts ADD COLUMN seq INTEGER;`,
			`ALTER TABLE ktl_capture_artifacts ADD COLUMN ts_ns INTEGER;`,
			`CREATE INDEX IF NOT EXISTS idx_capture_artifacts_session_seq ON ktl_capture_artifacts(session_id, seq);`,
		}
		for _, stmt := range stmts {
			if _, err := db.ExecContext(ctx, stmt); err != nil {
				// SQLite doesn't support IF NOT EXISTS on ALTER TABLE ADD COLUMN; ignore duplicate-column errors.
				// modernc sqlite error messages are driver-specific; safest is to continue on any error that looks like "duplicate column".
				msg := err.Error()
				if containsDuplicateColumn(msg) {
					continue
				}
				return fmt.Errorf("migration v2 (%s): %w", stmt, err)
			}
		}
		return nil
	default:
		return fmt.Errorf("unknown capture schema version %d", toVersion)
	}
}

func containsDuplicateColumn(msg string) bool {
	if msg == "" {
		return false
	}
	// Covers both "duplicate column name" and driver variants.
	return contains(msg, "duplicate column") || contains(msg, "duplicate column name")
}

func contains(s, sub string) bool {
	// small local helper to avoid importing strings in this file
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			if s[i+j] != sub[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
