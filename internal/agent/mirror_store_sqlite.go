package agent

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/protobuf/proto"
)

type sqliteMirrorStore struct {
	path string

	writeDB *sql.DB
	readDB  *sql.DB

	maxSessions         int
	maxFramesPerSession uint64
	maxBytes            int64
	pruneInterval       time.Duration
	lastPruneUnixNano   int64

	queueSize     int
	batchSize     int
	flushInterval time.Duration

	queue    chan writeRequest
	wg       sync.WaitGroup
	writeMu  sync.Mutex
	writeErr error
	dropped  uint64

	mu     sync.Mutex
	closed bool
}

type MirrorStoreOptions struct {
	MaxSessions         int
	MaxFramesPerSession uint64
	MaxBytes            int64
	PruneInterval       time.Duration
}

// OpenMirrorStore opens a SQLite-backed flight recorder database.
// If path is empty, it returns (nil, nil) and the mirror bus stays in-memory only.
func OpenMirrorStore(path string, opts MirrorStoreOptions) (MirrorStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve mirror store path: %w", err)
	}
	dir := filepath.Dir(absPath)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create mirror store dir: %w", err)
		}
	}

	writeDB, err := sql.Open("sqlite", absPath)
	if err != nil {
		return nil, fmt.Errorf("open mirror sqlite: %w", err)
	}
	writeDB.SetMaxOpenConns(1)
	writeDB.SetMaxIdleConns(1)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := initMirrorSQLite(ctx, writeDB); err != nil {
		_ = writeDB.Close()
		return nil, err
	}

	// Read-only connection so long exports don't block the writer.
	readDSN := absPath
	{
		u := url.URL{Scheme: "file", Path: absPath}
		q := u.Query()
		q.Set("mode", "ro")
		q.Set("_busy_timeout", "5000")
		u.RawQuery = q.Encode()
		readDSN = u.String()
	}
	readDB, err := sql.Open("sqlite", readDSN)
	if err != nil {
		_ = writeDB.Close()
		return nil, fmt.Errorf("open mirror sqlite (ro): %w", err)
	}
	readDB.SetMaxOpenConns(1)
	readDB.SetMaxIdleConns(1)
	{
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := readDB.PingContext(ctx); err != nil {
			_ = readDB.Close()
			_ = writeDB.Close()
			return nil, fmt.Errorf("ping mirror sqlite: %w", err)
		}
	}

	queueSize := envInt("KTL_MIRROR_STORE_QUEUE_SIZE", 4096)
	if queueSize < 128 {
		queueSize = 128
	}
	batchSize := envInt("KTL_MIRROR_STORE_BATCH_SIZE", 256)
	if batchSize < 16 {
		batchSize = 16
	}
	flushMS := envInt("KTL_MIRROR_STORE_FLUSH_MS", 250)
	if flushMS < 25 {
		flushMS = 25
	}

	maxSessions := opts.MaxSessions
	if maxSessions < 0 {
		maxSessions = 0
	}
	maxFrames := opts.MaxFramesPerSession
	maxBytes := opts.MaxBytes
	if maxBytes < 0 {
		maxBytes = 0
	}
	pruneInterval := opts.PruneInterval
	if pruneInterval <= 0 && (maxSessions > 0 || maxBytes > 0) {
		pruneInterval = 5 * time.Second
	}

	s := &sqliteMirrorStore{
		path:                absPath,
		writeDB:             writeDB,
		readDB:              readDB,
		maxSessions:         maxSessions,
		maxFramesPerSession: maxFrames,
		maxBytes:            maxBytes,
		pruneInterval:       pruneInterval,
		queueSize:           queueSize,
		batchSize:           batchSize,
		flushInterval:       time.Duration(flushMS) * time.Millisecond,
		queue:               make(chan writeRequest, queueSize),
	}
	s.wg.Add(1)
	go s.writerLoop()
	return s, nil
}

func (s *sqliteMirrorStore) Close() error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	close(s.queue)
	s.mu.Unlock()

	s.wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _ = s.writeDB.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`)

	writeErr := s.firstWriteErr()
	closeErr := s.writeDB.Close()
	if s.readDB != nil {
		_ = s.readDB.Close()
	}
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (s *sqliteMirrorStore) Append(frame *apiv1.MirrorFrame) error {
	if s == nil {
		return nil
	}
	if err := s.firstWriteErr(); err != nil {
		return err
	}
	if frame == nil {
		return nil
	}
	sessionID := strings.TrimSpace(frame.GetSessionId())
	if sessionID == "" {
		return nil
	}
	seq := frame.GetSequence()
	received := frame.GetReceivedUnixNano()
	if seq == 0 || received == 0 {
		// The server assigns these; if we got here without them, treat it as a programming error
		// and avoid recording misleading data.
		return errors.New("mirror frame missing assigned sequence/timestamp")
	}
	blob, err := proto.Marshal(frame)
	if err != nil {
		return fmt.Errorf("marshal mirror frame: %w", err)
	}
	maxFramesPerSession := s.maxFramesPerSession

	return s.enqueue(context.Background(), func(ctx context.Context, tx *sql.Tx) error {
		// Ensure the session exists.
		if _, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO ktl_mirror_sessions(
  session_id,
  created_at_ns,
  last_seen_ns,
  last_sequence,
  command,
  args_json,
  requester,
  cluster,
  kube_context,
  namespace,
  release,
  chart,
  tags_json
)
VALUES(?, ?, ?, ?, '', '[]', '', '', '', '', '', '', '{}')
`, sessionID, received, received, seq); err != nil {
			return err
		}
		// Update session cursor.
		if _, err := tx.ExecContext(ctx, `
UPDATE ktl_mirror_sessions
SET last_seen_ns = max(last_seen_ns, ?),
    last_sequence = max(last_sequence, ?),
    state = max(state, 1)
WHERE session_id = ?
`, received, seq, sessionID); err != nil {
			return err
		}
		// Insert the frame.
		_, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO ktl_mirror_frames(session_id, sequence, received_ns, frame_proto)
VALUES(?, ?, ?, ?)
`, sessionID, seq, received, blob)
		if err != nil {
			return err
		}
		if maxFramesPerSession > 0 && seq > maxFramesPerSession {
			// Keep the last N frames for the session.
			threshold := int64(seq - maxFramesPerSession)
			_, _ = tx.ExecContext(ctx, `
DELETE FROM ktl_mirror_frames
WHERE session_id = ? AND sequence <= ?
`, sessionID, threshold)
		}
		return nil
	})
}

func (s *sqliteMirrorStore) UpsertSessionMeta(ctx context.Context, sessionID string, meta MirrorSessionMeta, tags map[string]string) error {
	if s == nil || s.writeDB == nil {
		return nil
	}
	if err := s.firstWriteErr(); err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC().UnixNano()

	// Ensure the session exists.
	if _, err := s.writeDB.ExecContext(ctx, `
INSERT OR IGNORE INTO ktl_mirror_sessions(
  session_id,
  created_at_ns,
  last_seen_ns,
  last_sequence,
  command,
  args_json,
  requester,
  cluster,
  kube_context,
  namespace,
  release,
  chart,
  tags_json
)
VALUES(?, ?, ?, 0, '', '[]', '', '', '', '', '', '', '{}')
`, sessionID, now, now); err != nil {
		return err
	}

	// Load existing values so we can merge tags and only overwrite non-empty meta fields.
	loaded, _, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	merged := loaded
	merged.Meta = mergeMeta(merged.Meta, meta)
	merged.Tags = mergeTags(merged.Tags, tags)
	if merged.Tags == nil {
		merged.Tags = map[string]string{}
	}
	argsJSON, _ := json.Marshal(merged.Meta.Args)
	tagsJSON, _ := json.Marshal(merged.Tags)

	_, err = s.writeDB.ExecContext(ctx, `
UPDATE ktl_mirror_sessions
SET last_seen_ns = max(last_seen_ns, ?),
    state = max(state, 1),
    command = ?,
    args_json = ?,
    requester = ?,
    cluster = ?,
    kube_context = ?,
    namespace = ?,
    release = ?,
    chart = ?,
    tags_json = ?
WHERE session_id = ?
`, now,
		strings.TrimSpace(merged.Meta.Command),
		string(argsJSON),
		strings.TrimSpace(merged.Meta.Requester),
		strings.TrimSpace(merged.Meta.Cluster),
		strings.TrimSpace(merged.Meta.KubeContext),
		strings.TrimSpace(merged.Meta.Namespace),
		strings.TrimSpace(merged.Meta.Release),
		strings.TrimSpace(merged.Meta.Chart),
		string(tagsJSON),
		sessionID,
	)
	return err
}

func (s *sqliteMirrorStore) UpsertSessionStatus(ctx context.Context, sessionID string, st MirrorSessionStatus) error {
	if s == nil || s.writeDB == nil {
		return nil
	}
	if err := s.firstWriteErr(); err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	now := time.Now().UTC().UnixNano()

	// Ensure the session exists.
	if _, err := s.writeDB.ExecContext(ctx, `
INSERT OR IGNORE INTO ktl_mirror_sessions(
  session_id,
  created_at_ns,
  last_seen_ns,
  last_sequence,
  command,
  args_json,
  requester,
  cluster,
  kube_context,
  namespace,
  release,
  chart,
  tags_json
)
VALUES(?, ?, ?, 0, '', '[]', '', '', '', '', '', '', '{}')
`, sessionID, now, now); err != nil {
		return err
	}

	loaded, _, err := s.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	merged := loaded
	merged.Status = mergeStatus(merged.Status, st)

	_, err = s.writeDB.ExecContext(ctx, `
UPDATE ktl_mirror_sessions
SET last_seen_ns = max(last_seen_ns, ?),
    state = ?,
    exit_code = ?,
    error_message = ?,
    completed_at_ns = ?
WHERE session_id = ?
`, now,
		int64(merged.Status.State),
		int64(merged.Status.ExitCode),
		strings.TrimSpace(merged.Status.ErrorMessage),
		merged.Status.CompletedUnixNano,
		sessionID,
	)
	return err
}

func (s *sqliteMirrorStore) DeleteSession(ctx context.Context, sessionID string) (bool, error) {
	if s == nil || s.writeDB == nil {
		return false, nil
	}
	if err := s.firstWriteErr(); err != nil {
		return false, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	res, err := s.writeDB.ExecContext(ctx, `DELETE FROM ktl_mirror_sessions WHERE session_id = ?`, sessionID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (s *sqliteMirrorStore) GetSession(ctx context.Context, sessionID string) (MirrorSession, bool, error) {
	if s == nil || s.readDB == nil {
		return MirrorSession{}, false, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return MirrorSession{}, false, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var out MirrorSession
	var outMetaArgsJSON string
	var outTagsJSON string
	var state int64
	var exitCode int64
	err := s.readDB.QueryRowContext(ctx, `
SELECT
  session_id,
  created_at_ns,
  last_seen_ns,
  last_sequence,
  COALESCE(command, ''),
  COALESCE(args_json, '[]'),
  COALESCE(requester, ''),
  COALESCE(cluster, ''),
  COALESCE(kube_context, ''),
  COALESCE(namespace, ''),
  COALESCE(release, ''),
  COALESCE(chart, ''),
  COALESCE(tags_json, '{}'),
  COALESCE(state, 0),
  COALESCE(exit_code, 0),
  COALESCE(error_message, ''),
  COALESCE(completed_at_ns, 0)
FROM ktl_mirror_sessions
WHERE session_id = ?
`, sessionID).Scan(
		&out.SessionID,
		&out.CreatedUnixNano,
		&out.LastSeenUnixNano,
		&out.LastSequence,
		&out.Meta.Command,
		&outMetaArgsJSON,
		&out.Meta.Requester,
		&out.Meta.Cluster,
		&out.Meta.KubeContext,
		&out.Meta.Namespace,
		&out.Meta.Release,
		&out.Meta.Chart,
		&outTagsJSON,
		&state,
		&exitCode,
		&out.Status.ErrorMessage,
		&out.Status.CompletedUnixNano,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return MirrorSession{}, false, nil
	}
	if err != nil {
		return MirrorSession{}, false, err
	}
	out.Meta.Args = nil
	if strings.TrimSpace(outMetaArgsJSON) != "" {
		var args []string
		_ = json.Unmarshal([]byte(outMetaArgsJSON), &args)
		out.Meta.Args = args
	}
	out.Tags = nil
	if strings.TrimSpace(outTagsJSON) != "" {
		var tags map[string]string
		_ = json.Unmarshal([]byte(outTagsJSON), &tags)
		out.Tags = tags
	}
	out.Status.State = MirrorSessionState(state)
	out.Status.ExitCode = int32(exitCode)
	out.Status.ErrorMessage = strings.TrimSpace(out.Status.ErrorMessage)
	return out, true, nil
}

func (s *sqliteMirrorStore) ListSessions(ctx context.Context, limit int) ([]MirrorSession, error) {
	if s == nil || s.readDB == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if limit <= 0 {
		limit = 200
	}
	if limit > 1000 {
		limit = 1000
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT
  session_id,
  created_at_ns,
  last_seen_ns,
  last_sequence,
  COALESCE(command, ''),
  COALESCE(args_json, '[]'),
  COALESCE(requester, ''),
  COALESCE(cluster, ''),
  COALESCE(kube_context, ''),
  COALESCE(namespace, ''),
  COALESCE(release, ''),
  COALESCE(chart, ''),
  COALESCE(tags_json, '{}'),
  COALESCE(state, 0),
  COALESCE(exit_code, 0),
  COALESCE(error_message, ''),
  COALESCE(completed_at_ns, 0)
FROM ktl_mirror_sessions
ORDER BY last_seen_ns DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := make([]MirrorSession, 0, min(limit, 256))
	for rows.Next() {
		var m MirrorSession
		var argsJSON string
		var tagsJSON string
		var state int64
		var exitCode int64
		if err := rows.Scan(
			&m.SessionID,
			&m.CreatedUnixNano,
			&m.LastSeenUnixNano,
			&m.LastSequence,
			&m.Meta.Command,
			&argsJSON,
			&m.Meta.Requester,
			&m.Meta.Cluster,
			&m.Meta.KubeContext,
			&m.Meta.Namespace,
			&m.Meta.Release,
			&m.Meta.Chart,
			&tagsJSON,
			&state,
			&exitCode,
			&m.Status.ErrorMessage,
			&m.Status.CompletedUnixNano,
		); err != nil {
			return nil, err
		}
		m.Meta.Args = nil
		if strings.TrimSpace(argsJSON) != "" {
			var args []string
			_ = json.Unmarshal([]byte(argsJSON), &args)
			m.Meta.Args = args
		}
		m.Tags = nil
		if strings.TrimSpace(tagsJSON) != "" {
			var tags map[string]string
			_ = json.Unmarshal([]byte(tagsJSON), &tags)
			m.Tags = tags
		}
		m.Status.State = MirrorSessionState(state)
		m.Status.ExitCode = int32(exitCode)
		m.Status.ErrorMessage = strings.TrimSpace(m.Status.ErrorMessage)
		out = append(out, m)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func (s *sqliteMirrorStore) Replay(ctx context.Context, sessionID string, fromSequence uint64, send func(*apiv1.MirrorFrame) error) (uint64, error) {
	if s == nil || s.readDB == nil {
		return 0, nil
	}
	if send == nil {
		return 0, nil
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return 0, nil
	}
	if fromSequence == 0 {
		fromSequence = 1
	}
	if ctx == nil {
		ctx = context.Background()
	}
	rows, err := s.readDB.QueryContext(ctx, `
SELECT sequence, received_ns, frame_proto
FROM ktl_mirror_frames
WHERE session_id = ? AND sequence >= ?
ORDER BY sequence
`, sessionID, fromSequence)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	var last uint64
	for rows.Next() {
		var seq uint64
		var received int64
		var blob []byte
		if err := rows.Scan(&seq, &received, &blob); err != nil {
			return last, err
		}
		var frame apiv1.MirrorFrame
		if err := proto.Unmarshal(blob, &frame); err != nil {
			return last, fmt.Errorf("unmarshal mirror frame: %w", err)
		}
		frame.Sequence = seq
		frame.ReceivedUnixNano = received
		if err := send(&frame); err != nil {
			return last, err
		}
		last = seq
	}
	if err := rows.Err(); err != nil {
		return last, err
	}
	return last, nil
}

type writeRequest struct {
	ctx context.Context
	fn  func(context.Context, *sql.Tx) error
}

func (s *sqliteMirrorStore) enqueue(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	if s == nil {
		return nil
	}
	if err := s.firstWriteErr(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if closed {
		return errors.New("mirror store is closed")
	}
	select {
	case s.queue <- writeRequest{ctx: ctx, fn: fn}:
		return nil
	default:
		atomic.AddUint64(&s.dropped, 1)
		return errors.New("mirror store queue is full")
	}
}

func (s *sqliteMirrorStore) writerLoop() {
	defer s.wg.Done()

	batch := make([]writeRequest, 0, s.batchSize)
	flush := func() bool {
		if len(batch) == 0 || s.firstWriteErr() != nil {
			batch = batch[:0]
			return false
		}
		ctx := context.Background()
		tx, err := s.writeDB.BeginTx(ctx, nil)
		if err != nil {
			s.setWriteErr(err)
			batch = batch[:0]
			return true
		}
		for _, req := range batch {
			reqCtx := req.ctx
			if reqCtx == nil {
				reqCtx = ctx
			}
			if err := req.fn(reqCtx, tx); err != nil {
				_ = tx.Rollback()
				s.setWriteErr(err)
				batch = batch[:0]
				return true
			}
		}
		if err := tx.Commit(); err != nil {
			s.setWriteErr(err)
		}
		batch = batch[:0]
		return true
	}

	interval := s.flushInterval
	if interval <= 0 {
		interval = 250 * time.Millisecond
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case req, ok := <-s.queue:
			if !ok {
				_ = flush()
				return
			}
			batch = append(batch, req)
			if len(batch) >= cap(batch) {
				_ = flush()
			}
		case <-ticker.C:
			didFlush := flush()
			if didFlush {
				s.maybePrune()
			}
		}
	}
}

func (s *sqliteMirrorStore) setWriteErr(err error) {
	if err == nil {
		return
	}
	s.writeMu.Lock()
	if s.writeErr == nil {
		s.writeErr = err
	}
	s.writeMu.Unlock()
}

func (s *sqliteMirrorStore) firstWriteErr() error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	return s.writeErr
}

func initMirrorSQLite(ctx context.Context, db *sql.DB) error {
	if ctx == nil {
		ctx = context.Background()
	}
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA foreign_keys=ON;`,
		`PRAGMA synchronous=NORMAL;`,
		`PRAGMA busy_timeout=5000;`,
		`CREATE TABLE IF NOT EXISTS ktl_mirror_sessions (
  session_id TEXT PRIMARY KEY,
  created_at_ns INTEGER NOT NULL,
  last_seen_ns INTEGER NOT NULL,
  last_sequence INTEGER NOT NULL,
  command TEXT,
  args_json TEXT,
  requester TEXT,
  cluster TEXT,
  kube_context TEXT,
  namespace TEXT,
  release TEXT,
  chart TEXT,
  tags_json TEXT,
  state INTEGER NOT NULL DEFAULT 0,
  exit_code INTEGER NOT NULL DEFAULT 0,
  error_message TEXT NOT NULL DEFAULT '',
  completed_at_ns INTEGER NOT NULL DEFAULT 0
);`,
		`CREATE INDEX IF NOT EXISTS idx_mirror_sessions_last_seen ON ktl_mirror_sessions(last_seen_ns DESC);`,
		`CREATE TABLE IF NOT EXISTS ktl_mirror_frames (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  session_id TEXT NOT NULL,
  sequence INTEGER NOT NULL,
  received_ns INTEGER NOT NULL,
  frame_proto BLOB NOT NULL,
  FOREIGN KEY(session_id) REFERENCES ktl_mirror_sessions(session_id) ON DELETE CASCADE,
  UNIQUE(session_id, sequence)
);`,
		`CREATE INDEX IF NOT EXISTS idx_mirror_frames_session_seq ON ktl_mirror_frames(session_id, sequence);`,
		`CREATE INDEX IF NOT EXISTS idx_mirror_frames_session_received ON ktl_mirror_frames(session_id, received_ns);`,
	}
	for _, stmt := range stmts {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init mirror sqlite: %w", err)
		}
	}

	// Backfill new columns for older databases (SQLite doesn't support IF NOT EXISTS for ADD COLUMN).
	alter := []string{
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN command TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN args_json TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN requester TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN cluster TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN kube_context TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN namespace TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN release TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN chart TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN tags_json TEXT;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN state INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN exit_code INTEGER NOT NULL DEFAULT 0;`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN error_message TEXT NOT NULL DEFAULT '';`,
		`ALTER TABLE ktl_mirror_sessions ADD COLUMN completed_at_ns INTEGER NOT NULL DEFAULT 0;`,
	}
	for _, stmt := range alter {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			if containsDuplicateColumn(err.Error()) {
				continue
			}
			return fmt.Errorf("init mirror sqlite (%s): %w", stmt, err)
		}
	}
	return nil
}

func (s *sqliteMirrorStore) maybePrune() {
	if s == nil {
		return
	}
	if s.pruneInterval <= 0 {
		return
	}
	now := time.Now().UTC()
	last := atomic.LoadInt64(&s.lastPruneUnixNano)
	if last > 0 && now.UnixNano()-last < int64(s.pruneInterval) {
		return
	}
	if !atomic.CompareAndSwapInt64(&s.lastPruneUnixNano, last, now.UnixNano()) {
		return
	}
	if s.maxSessions <= 0 && s.maxBytes <= 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.prune(ctx); err != nil {
		s.setWriteErr(err)
	}
}

func (s *sqliteMirrorStore) prune(ctx context.Context) error {
	if s == nil || s.writeDB == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	tx, err := s.writeDB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	commit := false
	defer func() {
		if !commit {
			_ = tx.Rollback()
		}
	}()

	if s.maxSessions > 0 {
		var count int
		if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM ktl_mirror_sessions`).Scan(&count); err != nil {
			return err
		}
		excess := count - s.maxSessions
		if excess > 0 {
			if _, err := tx.ExecContext(ctx, `
DELETE FROM ktl_mirror_sessions
WHERE session_id IN (
  SELECT session_id FROM ktl_mirror_sessions ORDER BY last_seen_ns ASC LIMIT ?
)`, excess); err != nil {
				return err
			}
		}
	}

	if s.maxBytes > 0 {
		for i := 0; i < 25; i++ {
			used, err := usedBytesTx(ctx, tx)
			if err != nil {
				return err
			}
			if used <= s.maxBytes {
				break
			}
			// Delete a few oldest sessions and re-check.
			res, err := tx.ExecContext(ctx, `
DELETE FROM ktl_mirror_sessions
WHERE session_id IN (
  SELECT session_id FROM ktl_mirror_sessions ORDER BY last_seen_ns ASC LIMIT 10
)`)
			if err != nil {
				return err
			}
			n, _ := res.RowsAffected()
			if n == 0 {
				break
			}
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	commit = true
	return nil
}

func usedBytesTx(ctx context.Context, tx *sql.Tx) (int64, error) {
	if tx == nil {
		return 0, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var pageSize int64
	var pageCount int64
	var freeList int64
	if err := tx.QueryRowContext(ctx, `PRAGMA page_size;`).Scan(&pageSize); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `PRAGMA page_count;`).Scan(&pageCount); err != nil {
		return 0, err
	}
	if err := tx.QueryRowContext(ctx, `PRAGMA freelist_count;`).Scan(&freeList); err != nil {
		return 0, err
	}
	if pageCount < freeList {
		freeList = pageCount
	}
	return pageSize * (pageCount - freeList), nil
}

func mergeMeta(base, update MirrorSessionMeta) MirrorSessionMeta {
	out := base
	if v := strings.TrimSpace(update.Command); v != "" {
		out.Command = v
	}
	if len(update.Args) > 0 {
		out.Args = append([]string(nil), update.Args...)
	}
	if v := strings.TrimSpace(update.Requester); v != "" {
		out.Requester = v
	}
	if v := strings.TrimSpace(update.Cluster); v != "" {
		out.Cluster = v
	}
	if v := strings.TrimSpace(update.KubeContext); v != "" {
		out.KubeContext = v
	}
	if v := strings.TrimSpace(update.Namespace); v != "" {
		out.Namespace = v
	}
	if v := strings.TrimSpace(update.Release); v != "" {
		out.Release = v
	}
	if v := strings.TrimSpace(update.Chart); v != "" {
		out.Chart = v
	}
	return out
}

func mergeTags(base, update map[string]string) map[string]string {
	if len(base) == 0 && len(update) == 0 {
		return nil
	}
	out := map[string]string{}
	for k, v := range base {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	for k, v := range update {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out[k] = v
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeStatus(base, update MirrorSessionStatus) MirrorSessionStatus {
	out := base
	if update.State > out.State {
		out.State = update.State
	}
	if update.State == MirrorSessionStateDone || update.State == MirrorSessionStateError {
		out.ExitCode = update.ExitCode
		if v := strings.TrimSpace(update.ErrorMessage); v != "" {
			out.ErrorMessage = v
		}
		if update.CompletedUnixNano > 0 && update.CompletedUnixNano > out.CompletedUnixNano {
			out.CompletedUnixNano = update.CompletedUnixNano
		}
	}
	return out
}

func containsDuplicateColumn(msg string) bool {
	if msg == "" {
		return false
	}
	return strings.Contains(strings.ToLower(msg), "duplicate column")
}

func envInt(key string, def int) int {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return def
	}
	var n int
	if _, err := fmt.Sscanf(raw, "%d", &n); err != nil {
		return def
	}
	return n
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
