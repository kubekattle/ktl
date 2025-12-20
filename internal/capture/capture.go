package capture

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/tailer"
)

type SessionMeta struct {
	Command   string            `json:"command"`
	Args      []string          `json:"args,omitempty"`
	StartedAt time.Time         `json:"startedAt"`
	Host      string            `json:"host,omitempty"`
	User      string            `json:"user,omitempty"`
	Extra     map[string]string `json:"extra,omitempty"`
}

type Recorder struct {
	db        *sql.DB
	sessionID string
	now       func() time.Time

	mu     sync.Mutex
	closed bool
}

func Open(path string, meta SessionMeta) (*Recorder, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("capture path is required")
	}
	if strings.TrimSpace(meta.Command) == "" {
		return nil, errors.New("capture command is required")
	}
	if meta.StartedAt.IsZero() {
		meta.StartedAt = time.Now().UTC()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil && filepath.Dir(path) != "." {
		return nil, fmt.Errorf("create capture dir: %w", err)
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	r := &Recorder{
		db:  db,
		now: func() time.Time { return time.Now().UTC() },
	}
	if err := r.initSchema(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := r.startSession(context.Background(), meta); err != nil {
		_ = db.Close()
		return nil, err
	}
	return r, nil
}

func (r *Recorder) SessionID() string {
	if r == nil {
		return ""
	}
	return r.sessionID
}

func (r *Recorder) Close() error {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	if r.closed {
		r.mu.Unlock()
		return nil
	}
	r.closed = true
	r.mu.Unlock()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = r.db.ExecContext(ctx, `UPDATE ktl_capture_sessions SET ended_at = ? WHERE session_id = ?`, r.now().Format(time.RFC3339Nano), r.sessionID)
	return r.db.Close()
}

func (r *Recorder) ObserveLog(rec tailer.LogRecord) {
	_ = r.RecordLog(context.Background(), rec)
}

func (r *Recorder) HandleDeployEvent(evt deploy.StreamEvent) {
	_ = r.RecordDeployEvent(context.Background(), evt)
}

func (r *Recorder) RecordArtifact(ctx context.Context, name, text string) error {
	if r == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return errors.New("artifact name is required")
	}
	now := r.now().Format(time.RFC3339Nano)
	_, err := r.db.ExecContext(ctx, `
INSERT INTO ktl_capture_artifacts(session_id, ts, name, text)
VALUES(?, ?, ?, ?)
`, r.sessionID, now, name, text)
	return err
}

func (r *Recorder) RecordLog(ctx context.Context, rec tailer.LogRecord) error {
	if r == nil {
		return nil
	}
	ts := rec.Timestamp.UTC()
	if ts.IsZero() {
		ts = r.now()
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO ktl_capture_events(session_id, ts, kind, level, source, namespace, pod, container, stream, message, raw_json)
VALUES(?, ?, 'log', ?, ?, ?, ?, ?, ?, ?, ?)
`, r.sessionID, ts.Format(time.RFC3339Nano), "", rec.Source, rec.Namespace, rec.Pod, rec.Container, "", firstNonEmpty(rec.Rendered, rec.Raw), mustJSON(rec))
	return err
}

func (r *Recorder) RecordDeployEvent(ctx context.Context, evt deploy.StreamEvent) error {
	if r == nil {
		return nil
	}
	ts := strings.TrimSpace(evt.Timestamp)
	if ts == "" {
		ts = r.now().Format(time.RFC3339Nano)
	}
	level := ""
	message := ""
	if evt.Log != nil {
		level = evt.Log.Level
		message = evt.Log.Message
	}
	_, err := r.db.ExecContext(ctx, `
INSERT INTO ktl_capture_events(session_id, ts, kind, level, source, namespace, pod, container, stream, message, raw_json)
VALUES(?, ?, 'deploy', ?, ?, ?, '', '', '', ?, ?)
`, r.sessionID, ts, level, string(evt.Kind), eventNamespace(evt), message, mustJSON(evt))
	return err
}

func (r *Recorder) initSchema(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA foreign_keys=ON;`,
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
		if _, err := r.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init capture schema: %w", err)
		}
	}
	return nil
}

func (r *Recorder) startSession(ctx context.Context, meta SessionMeta) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal session meta: %w", err)
	}
	now := meta.StartedAt.UTC().Format(time.RFC3339Nano)
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO ktl_capture_sessions(session_id, command, meta_json, started_at)
VALUES(?, ?, ?, ?)
`, id, meta.Command, string(metaJSON), now); err != nil {
		return fmt.Errorf("insert capture session: %w", err)
	}
	r.sessionID = id
	return nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func mustJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func eventNamespace(evt deploy.StreamEvent) string {
	if evt.Summary != nil {
		return strings.TrimSpace(evt.Summary.Namespace)
	}
	if evt.Phase != nil {
		return ""
	}
	if evt.Log != nil {
		return strings.TrimSpace(evt.Log.Namespace)
	}
	return ""
}
