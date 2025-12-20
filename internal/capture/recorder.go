package capture

import (
	"bytes"
	"compress/gzip"
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
	"sync/atomic"
	"time"

	_ "modernc.org/sqlite"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/tailer"
)

type Entities struct {
	Cluster      string
	KubeContext  string
	Namespace    string
	Release      string
	Chart        string
	ImageRef     string
	ImageDigest  string
	BuildContext string
}

type SessionMeta struct {
	Command     string            `json:"command"`
	Args        []string          `json:"args,omitempty"`
	RunID       string            `json:"runId,omitempty"`
	ParentRunID string            `json:"parentRunId,omitempty"`
	StartedAt   time.Time         `json:"startedAt"`
	Host        string            `json:"host,omitempty"`
	User        string            `json:"user,omitempty"`
	Extra       map[string]string `json:"extra,omitempty"`
	Tags        map[string]string `json:"tags,omitempty"`
	Entities    Entities          `json:"entities,omitempty"`
}

type Recorder struct {
	db        *sql.DB
	sessionID string
	runID     string

	now func() time.Time

	seq uint64

	queue    chan writeRequest
	wg       sync.WaitGroup
	writeMu  sync.Mutex
	writeErr error

	mu     sync.Mutex
	closed bool
}

type writeRequest struct {
	ctx context.Context
	fn  func(context.Context, *sql.Tx) error
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

	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, fmt.Errorf("create capture dir: %w", err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	r := &Recorder{
		db:    db,
		now:   func() time.Time { return time.Now().UTC() },
		queue: make(chan writeRequest, 4096),
	}
	if err := migrate(context.Background(), db); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := r.startSession(context.Background(), meta); err != nil {
		_ = db.Close()
		return nil, err
	}

	r.wg.Add(1)
	go r.writerLoop()

	return r, nil
}

func (r *Recorder) SessionID() string {
	if r == nil {
		return ""
	}
	return r.sessionID
}

func (r *Recorder) RunID() string {
	if r == nil {
		return ""
	}
	return r.runID
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
	close(r.queue)
	r.mu.Unlock()

	r.wg.Wait()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ended := r.now()
	_, _ = r.db.ExecContext(ctx, `
UPDATE ktl_capture_sessions
SET ended_at = ?, ended_at_ns = ?
WHERE session_id = ?
`, ended.Format(time.RFC3339Nano), ended.UnixNano(), r.sessionID)

	closeErr := r.db.Close()
	writeErr := r.firstWriteErr()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}

func (r *Recorder) ObserveLog(rec tailer.LogRecord) {
	_ = r.RecordLog(context.Background(), rec)
}

func (r *Recorder) ObserveSelection(sel tailer.SelectionSnapshot) {
	_ = r.RecordSelection(context.Background(), sel)
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
	now := r.now()
	seq := r.nextSeq()
	return r.enqueue(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO ktl_capture_artifacts(session_id, seq, ts, ts_ns, name, text)
VALUES(?, ?, ?, ?, ?, ?)
`, r.sessionID, seq, now.Format(time.RFC3339Nano), now.UnixNano(), name, text)
		return err
	})
}

func (r *Recorder) RecordLog(ctx context.Context, rec tailer.LogRecord) error {
	if r == nil {
		return nil
	}
	ts := rec.Timestamp.UTC()
	if ts.IsZero() {
		ts = r.now()
	}
	seq := r.nextSeq()
	payloadType, payloadBlob, payloadJSON := encodePayload(mustJSON(rec))
	message := firstNonEmpty(rec.Rendered, rec.Raw)
	return r.enqueue(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO ktl_capture_events(
  session_id, seq, ts, ts_ns, kind,
  level, source, namespace, pod, container,
  message, payload_type, payload_blob, payload_json
)
VALUES(?, ?, ?, ?, 'log', ?, ?, ?, ?, ?, ?, ?, ?, ?)
`,
			r.sessionID,
			seq,
			ts.Format(time.RFC3339Nano),
			ts.UnixNano(),
			"",
			rec.Source,
			rec.Namespace,
			rec.Pod,
			rec.Container,
			message,
			payloadType,
			payloadBlob,
			payloadJSON,
		)
		return err
	})
}

func (r *Recorder) RecordDeployEvent(ctx context.Context, evt deploy.StreamEvent) error {
	if r == nil {
		return nil
	}
	ts, tsNS := parseEventTimestamp(evt.Timestamp, r.now)
	seq := r.nextSeq()
	level := ""
	message := ""
	if evt.Log != nil {
		level = evt.Log.Level
		message = evt.Log.Message
	}
	payloadType, payloadBlob, payloadJSON := encodePayload(mustJSON(evt))
	source := string(evt.Kind)
	namespace := eventNamespace(evt)
	return r.enqueue(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO ktl_capture_events(
  session_id, seq, ts, ts_ns, kind,
  level, source, namespace, pod, container,
  message, payload_type, payload_blob, payload_json
)
VALUES(?, ?, ?, ?, 'deploy', ?, ?, ?, '', '', ?, ?, ?, ?)
`,
			r.sessionID,
			seq,
			ts,
			tsNS,
			level,
			source,
			namespace,
			message,
			payloadType,
			payloadBlob,
			payloadJSON,
		)
		return err
	})
}

func (r *Recorder) RecordSelection(ctx context.Context, sel tailer.SelectionSnapshot) error {
	if r == nil {
		return nil
	}
	ts := sel.Timestamp.UTC()
	if ts.IsZero() {
		ts = r.now()
	}
	seq := r.nextSeq()
	payloadType, payloadBlob, payloadJSON := encodePayload(mustJSON(sel))
	msg := fmt.Sprintf("%s %s/%s (%s)", strings.TrimSpace(sel.ChangeKind), strings.TrimSpace(sel.Namespace), strings.TrimSpace(sel.Pod), strings.TrimSpace(sel.Container))
	if strings.TrimSpace(sel.ChangeKind) == "reset" {
		msg = "selection reset"
	}
	return r.enqueue(ctx, func(ctx context.Context, tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `
INSERT INTO ktl_capture_events(
  session_id, seq, ts, ts_ns, kind,
  level, source, namespace, pod, container,
  message, payload_type, payload_blob, payload_json
)
VALUES(?, ?, ?, ?, 'selection', '', 'tailer', ?, ?, ?, ?, ?, ?, ?)
`,
			r.sessionID,
			seq,
			ts.Format(time.RFC3339Nano),
			ts.UnixNano(),
			strings.TrimSpace(sel.Namespace),
			strings.TrimSpace(sel.Pod),
			strings.TrimSpace(sel.Container),
			msg,
			payloadType,
			payloadBlob,
			payloadJSON,
		)
		return err
	})
}

func (r *Recorder) enqueue(ctx context.Context, fn func(context.Context, *sql.Tx) error) error {
	if r == nil {
		return nil
	}
	if err := r.firstWriteErr(); err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
		return errors.New("capture recorder is closed")
	}
	select {
	case r.queue <- writeRequest{ctx: ctx, fn: fn}:
		return nil
	default:
		return errors.New("capture queue is full")
	}
}

func (r *Recorder) writerLoop() {
	defer r.wg.Done()

	batch := make([]writeRequest, 0, 256)
	flush := func() {
		if len(batch) == 0 || r.firstWriteErr() != nil {
			batch = batch[:0]
			return
		}
		ctx := context.Background()
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			r.setWriteErr(err)
			batch = batch[:0]
			return
		}
		for _, req := range batch {
			reqCtx := req.ctx
			if reqCtx == nil {
				reqCtx = ctx
			}
			if err := req.fn(reqCtx, tx); err != nil {
				_ = tx.Rollback()
				r.setWriteErr(err)
				batch = batch[:0]
				return
			}
		}
		if err := tx.Commit(); err != nil {
			r.setWriteErr(err)
		}
		batch = batch[:0]
	}

	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case req, ok := <-r.queue:
			if !ok {
				flush()
				return
			}
			batch = append(batch, req)
			if len(batch) >= cap(batch) {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

func (r *Recorder) nextSeq() int64 {
	return int64(atomic.AddUint64(&r.seq, 1))
}

func (r *Recorder) setWriteErr(err error) {
	if err == nil {
		return
	}
	r.writeMu.Lock()
	if r.writeErr == nil {
		r.writeErr = err
	}
	r.writeMu.Unlock()
}

func (r *Recorder) firstWriteErr() error {
	r.writeMu.Lock()
	defer r.writeMu.Unlock()
	return r.writeErr
}

func (r *Recorder) startSession(ctx context.Context, meta SessionMeta) error {
	id, err := randomID()
	if err != nil {
		return err
	}
	runID := strings.TrimSpace(meta.RunID)
	if runID == "" {
		runID, err = randomRunID()
		if err != nil {
			return err
		}
		meta.RunID = runID
	}
	metaJSON, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("marshal session meta: %w", err)
	}
	start := meta.StartedAt.UTC()
	if _, err := r.db.ExecContext(ctx, `
INSERT INTO ktl_capture_sessions(
  session_id, run_id, parent_run_id,
  command, meta_json,
  started_at, started_at_ns,
  ended_at, ended_at_ns,
  cluster, kube_context, namespace, release, chart,
  image_ref, image_digest, build_context
)
VALUES(?, ?, ?, ?, ?, ?, ?, NULL, NULL, ?, ?, ?, ?, ?, ?, ?, ?)
`,
		id,
		runID,
		strings.TrimSpace(meta.ParentRunID),
		meta.Command,
		string(metaJSON),
		start.Format(time.RFC3339Nano),
		start.UnixNano(),
		strings.TrimSpace(meta.Entities.Cluster),
		strings.TrimSpace(meta.Entities.KubeContext),
		strings.TrimSpace(meta.Entities.Namespace),
		strings.TrimSpace(meta.Entities.Release),
		strings.TrimSpace(meta.Entities.Chart),
		strings.TrimSpace(meta.Entities.ImageRef),
		strings.TrimSpace(meta.Entities.ImageDigest),
		strings.TrimSpace(meta.Entities.BuildContext),
	); err != nil {
		return fmt.Errorf("insert capture session: %w", err)
	}
	if len(meta.Tags) > 0 {
		tx, err := r.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin tags tx: %w", err)
		}
		for k, v := range meta.Tags {
			k = strings.TrimSpace(k)
			v = strings.TrimSpace(v)
			if k == "" || v == "" {
				continue
			}
			if _, err := tx.ExecContext(ctx, `
INSERT INTO ktl_capture_tags(session_id, key, value)
VALUES(?, ?, ?)
ON CONFLICT(session_id, key) DO UPDATE SET value = excluded.value
`, id, k, v); err != nil {
				_ = tx.Rollback()
				return fmt.Errorf("insert tag %s: %w", k, err)
			}
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit tags: %w", err)
		}
	}
	r.sessionID = id
	r.runID = runID
	return nil
}

func randomID() (string, error) {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", fmt.Errorf("rand: %w", err)
	}
	return hex.EncodeToString(b[:]), nil
}

func randomRunID() (string, error) {
	var b [6]byte
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
	if evt.Log != nil {
		return strings.TrimSpace(evt.Log.Namespace)
	}
	return ""
}

func parseEventTimestamp(raw string, now func() time.Time) (string, int64) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		t := now().UTC()
		return t.Format(time.RFC3339Nano), t.UnixNano()
	}
	if ts, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return ts.UTC().Format(time.RFC3339Nano), ts.UTC().UnixNano()
	}
	if ts, err := time.Parse(time.RFC3339, raw); err == nil {
		return ts.UTC().Format(time.RFC3339Nano), ts.UTC().UnixNano()
	}
	t := now().UTC()
	return raw, t.UnixNano()
}

func encodePayload(jsonText string) (string, []byte, string) {
	jsonText = strings.TrimSpace(jsonText)
	if jsonText == "" {
		return "", nil, ""
	}
	// Always store a gzipped blob; payload_json stays populated for compatibility and quick inspection.
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	_, _ = zw.Write([]byte(jsonText))
	_ = zw.Close()
	return "json+gzip", buf.Bytes(), jsonText
}
