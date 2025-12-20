// writer.go streams log lines into on-disk SQLite databases for captures and replay.
package sqlitewriter

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

const (
	createTableStmt = `
CREATE TABLE IF NOT EXISTS logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    collected_at TEXT NOT NULL,
    log_timestamp TEXT,
    namespace TEXT,
    pod TEXT,
    container TEXT,
    raw TEXT,
    rendered TEXT
);`
	createIndexesStmt = `
CREATE INDEX IF NOT EXISTS idx_logs_ns_pod_c ON logs(namespace, pod, container);
CREATE INDEX IF NOT EXISTS idx_logs_log_timestamp ON logs(log_timestamp);
CREATE INDEX IF NOT EXISTS idx_logs_ts ON logs(COALESCE(log_timestamp, collected_at));`
	insertStmt = `INSERT INTO logs(collected_at, log_timestamp, namespace, pod, container, raw, rendered) VALUES(?, ?, ?, ?, ?, ?, ?)`
)

// Entry represents a single log line persisted to SQLite.
type Entry struct {
	CollectedAt  time.Time
	LogTimestamp string
	Namespace    string
	Pod          string
	Container    string
	Raw          string
	Rendered     string
}

// Writer persists log entries into a SQLite database.
type Writer struct {
	db     *sql.DB
	insert *sql.Stmt
}

// New initializes a Writer pointing at the given on-disk SQLite file.
func New(path string) (*Writer, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return nil, errors.New("sqlite path cannot be empty")
	}
	dir := filepath.Dir(p)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create sqlite directory: %w", err)
		}
	}
	db, err := sql.Open("sqlite", p)
	if err != nil {
		return nil, fmt.Errorf("open sqlite database: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := db.ExecContext(ctx, createTableStmt); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure logs table: %w", err)
	}
	if _, err := db.ExecContext(ctx, createIndexesStmt); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure log indexes: %w", err)
	}
	if err := ensureContextTables(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure context tables: %w", err)
	}
	if err := EnsureViewerSchema(ctx, db); err != nil {
		db.Close()
		return nil, fmt.Errorf("ensure viewer schema: %w", err)
	}
	stmt, err := db.PrepareContext(ctx, insertStmt)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("prepare insert statement: %w", err)
	}
	return &Writer{
		db:     db,
		insert: stmt,
	}, nil
}

// Close releases database resources.
func (w *Writer) Close() error {
	var err error
	if w.insert != nil {
		err = errors.Join(err, w.insert.Close())
	}
	if w.db != nil {
		err = errors.Join(err, w.db.Close())
	}
	return err
}

// Write stores the provided entry using the prepared insert statement.
func (w *Writer) Write(ctx context.Context, entry Entry) error {
	if w == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	collected := entry.CollectedAt
	if collected.IsZero() {
		collected = time.Now()
	}
	_, err := w.insert.ExecContext(
		ctx,
		collected.UTC().Format(time.RFC3339Nano),
		entry.LogTimestamp,
		entry.Namespace,
		entry.Pod,
		entry.Container,
		entry.Raw,
		entry.Rendered,
	)
	return err
}
