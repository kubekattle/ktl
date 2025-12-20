// File: internal/sqlitewriter/viewer_schema.go
// Brief: Internal sqlitewriter package implementation for 'viewer schema'.

// Package sqlitewriter provides sqlitewriter helpers.

package sqlitewriter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

const (
	createLogsFTSStmt = `
CREATE VIRTUAL TABLE IF NOT EXISTS logs_fts
USING fts5(raw, rendered, content='logs', content_rowid='id');`
	createLogsFTSTriggersStmt = `
CREATE TRIGGER IF NOT EXISTS logs_ai AFTER INSERT ON logs BEGIN
  INSERT INTO logs_fts(rowid, raw, rendered) VALUES (new.id, new.raw, new.rendered);
END;
CREATE TRIGGER IF NOT EXISTS logs_ad AFTER DELETE ON logs BEGIN
  INSERT INTO logs_fts(logs_fts, rowid, raw, rendered) VALUES('delete', old.id, old.raw, old.rendered);
END;
CREATE TRIGGER IF NOT EXISTS logs_au AFTER UPDATE ON logs BEGIN
  INSERT INTO logs_fts(logs_fts, rowid, raw, rendered) VALUES('delete', old.id, old.raw, old.rendered);
  INSERT INTO logs_fts(rowid, raw, rendered) VALUES(new.id, new.raw, new.rendered);
END;`

	createManifestsTablesStmt = `
CREATE TABLE IF NOT EXISTS manifest_resources (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  api_version TEXT,
  kind TEXT NOT NULL,
  namespace TEXT,
  name TEXT NOT NULL,
  yaml TEXT NOT NULL,
  path TEXT,
  uid TEXT
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_manifest_resources_unique ON manifest_resources(kind, COALESCE(namespace, ''), name);
CREATE INDEX IF NOT EXISTS idx_manifest_resources_kind_ns_name ON manifest_resources(kind, namespace, name);

CREATE TABLE IF NOT EXISTS manifest_edges (
  parent_id INTEGER NOT NULL,
  child_id INTEGER NOT NULL,
  PRIMARY KEY(parent_id, child_id),
  FOREIGN KEY(parent_id) REFERENCES manifest_resources(id) ON DELETE CASCADE,
  FOREIGN KEY(child_id) REFERENCES manifest_resources(id) ON DELETE CASCADE
);`
)

// EnsureViewerSchema applies optional schema pieces that power the capture viewer
// (FTS, manifest tables). It is safe to call on any logs.sqlite produced by ktl.
func EnsureViewerSchema(ctx context.Context, db *sql.DB) error {
	if db == nil {
		return errors.New("sqlite db is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	// Best-effort: FTS5 may be unavailable depending on the SQLite build. If it
	// fails, keep the viewer functional with LIKE-based search.
	if _, err := db.ExecContext(ctx, createLogsFTSStmt); err == nil {
		_, _ = db.ExecContext(ctx, createLogsFTSTriggersStmt)
		// If the external-content FTS table is empty, build it once.
		var ftsCount int64
		if err := db.QueryRowContext(ctx, `SELECT COUNT(1) FROM logs_fts`).Scan(&ftsCount); err == nil && ftsCount == 0 {
			_, _ = db.ExecContext(ctx, `INSERT INTO logs_fts(logs_fts) VALUES('rebuild')`)
		}
	}

	if _, err := db.ExecContext(ctx, createManifestsTablesStmt); err != nil {
		return fmt.Errorf("ensure manifest tables: %w", err)
	}
	return nil
}
