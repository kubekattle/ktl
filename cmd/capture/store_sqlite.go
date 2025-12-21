package main

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

type sqliteStore struct {
	db   *sql.DB
	path string
}

type SessionRow struct {
	SessionID     string `json:"session_id"`
	Command       string `json:"command"`
	StartedAtNS   int64  `json:"started_at_ns"`
	EndedAtNS     int64  `json:"ended_at_ns"`
	Cluster       string `json:"cluster,omitempty"`
	KubeContext   string `json:"kube_context,omitempty"`
	Namespace     string `json:"namespace,omitempty"`
	Release       string `json:"release,omitempty"`
	Chart         string `json:"chart,omitempty"`
	DroppedEvents int64  `json:"dropped_events,omitempty"`
}

type SessionMeta struct {
	SessionID   string `json:"session_id"`
	Command     string `json:"command"`
	MetaJSON    string `json:"meta_json"`
	StartedAtNS int64  `json:"started_at_ns"`
	EndedAtNS   int64  `json:"ended_at_ns"`
	Cluster     string `json:"cluster,omitempty"`
	KubeContext string `json:"kube_context,omitempty"`
	Namespace   string `json:"namespace,omitempty"`
	Release     string `json:"release,omitempty"`
	Chart       string `json:"chart,omitempty"`
	RunID       string `json:"run_id,omitempty"`
}

type TimelineRow struct {
	BucketNS  int64 `json:"bucket_ns"`
	LogsTotal int64 `json:"logs_total"`
	LogsWarn  int64 `json:"logs_warn"`
	LogsFail  int64 `json:"logs_fail"`
	LogsInfo  int64 `json:"logs_info"`
	Deploy    int64 `json:"deploy"`
	Selection int64 `json:"selection"`
	Artifacts int64 `json:"artifacts"`
	AnyEvents int64 `json:"any_events"`
}

type LogsPage struct {
	Cursor  int64     `json:"cursor"`
	Lines   []LogLine `json:"lines"`
	HasMore bool      `json:"has_more"`
}

type EventsPage struct {
	Cursor int64       `json:"cursor"`
	Events []EventLine `json:"events"`
}

type LogLine struct {
	Key       int64  `json:"key"`
	Seq       int64  `json:"seq"`
	ID        int64  `json:"id"`
	TSNS      int64  `json:"ts_ns"`
	Kind      string `json:"kind"`
	Level     string `json:"level,omitempty"`
	Source    string `json:"source,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	Message   string `json:"message"`
}

type EventLine struct {
	Key       int64  `json:"key"`
	Seq       int64  `json:"seq"`
	ID        int64  `json:"id"`
	TSNS      int64  `json:"ts_ns"`
	Kind      string `json:"kind"`
	Level     string `json:"level,omitempty"`
	Source    string `json:"source,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
	Message   string `json:"message"`
}

func openSQLiteStore(path string, readOnly bool) (*sqliteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("capture db path is required")
	}
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve capture db path: %w", err)
	}
	if _, err := os.Stat(absPath); err != nil {
		return nil, fmt.Errorf("open capture db: %w", err)
	}
	dsn := absPath
	if readOnly {
		// modernc.org/sqlite understands URI parameters in a "file:" DSN.
		u := url.URL{Scheme: "file", Path: absPath}
		q := u.Query()
		q.Set("mode", "ro")
		q.Set("_busy_timeout", "5000")
		u.RawQuery = q.Encode()
		// Avoid relative "file:foo.db" edge cases by forcing an absolute path.
		// url.URL{Scheme:"file",Path:"/abs"} formats as "file:/abs" which SQLite accepts.
		dsn = u.String()
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	return &sqliteStore{db: db, path: absPath}, nil
}

func (s *sqliteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *sqliteStore) ListSessions(ctx context.Context) ([]SessionRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT
  session_id,
  command,
  COALESCE(started_at_ns, CAST(strftime('%s', started_at) AS INTEGER) * 1000000000),
  COALESCE(ended_at_ns, CAST(strftime('%s', ended_at) AS INTEGER) * 1000000000),
  COALESCE(cluster, ''),
  COALESCE(kube_context, ''),
  COALESCE(namespace, ''),
  COALESCE(release, ''),
  COALESCE(chart, ''),
  COALESCE(dropped_events, 0)
FROM ktl_capture_sessions
ORDER BY COALESCE(started_at_ns, 0) DESC
LIMIT 200
`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionRow
	for rows.Next() {
		var r SessionRow
		if err := rows.Scan(&r.SessionID, &r.Command, &r.StartedAtNS, &r.EndedAtNS, &r.Cluster, &r.KubeContext, &r.Namespace, &r.Release, &r.Chart, &r.DroppedEvents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *sqliteStore) GetSessionMeta(ctx context.Context, sessionID string) (SessionMeta, error) {
	var m SessionMeta
	err := s.db.QueryRowContext(ctx, `
SELECT
  session_id,
  command,
  meta_json,
  COALESCE(started_at_ns, CAST(strftime('%s', started_at) AS INTEGER) * 1000000000),
  COALESCE(ended_at_ns, CAST(strftime('%s', ended_at) AS INTEGER) * 1000000000),
  COALESCE(cluster, ''),
  COALESCE(kube_context, ''),
  COALESCE(namespace, ''),
  COALESCE(release, ''),
  COALESCE(chart, ''),
  COALESCE(run_id, '')
FROM ktl_capture_sessions
WHERE session_id = ?
`, sessionID).Scan(&m.SessionID, &m.Command, &m.MetaJSON, &m.StartedAtNS, &m.EndedAtNS, &m.Cluster, &m.KubeContext, &m.Namespace, &m.Release, &m.Chart, &m.RunID)
	return m, err
}

func (s *sqliteStore) Timeline(ctx context.Context, sessionID string, bucket time.Duration, startNS, endNS int64, search string) ([]TimelineRow, error) {
	if bucket <= 0 {
		bucket = time.Second
	}
	bucketNS := int64(bucket)
	tsExpr := `COALESCE(ts_ns, CAST(strftime('%s', ts) AS INTEGER) * 1000000000)`
	keyExpr := fmt.Sprintf("(%s / %d) * %d", tsExpr, bucketNS, bucketNS)

	where := "session_id = ?"
	args := []any{sessionID}
	if startNS > 0 {
		where += " AND " + tsExpr + " >= ?"
		args = append(args, startNS)
	}
	if endNS > 0 {
		where += " AND " + tsExpr + " <= ?"
		args = append(args, endNS)
	}
	if strings.TrimSpace(search) != "" {
		where += " AND (message LIKE ? OR namespace LIKE ? OR pod LIKE ? OR container LIKE ?)"
		pat := "%" + search + "%"
		args = append(args, pat, pat, pat, pat)
	}

	// Severity is inferred from level/message for logs; deploy uses level if present.
	query := fmt.Sprintf(`
SELECT
  %s AS bucket_ns,
  SUM(CASE WHEN kind = 'log' THEN 1 ELSE 0 END) AS logs_total,
  SUM(CASE WHEN kind = 'log' AND (lower(COALESCE(level, '')) = 'warn' OR lower(COALESCE(message, '')) LIKE '%%warn%%') THEN 1 ELSE 0 END) AS logs_warn,
  SUM(CASE WHEN kind = 'log' AND (lower(COALESCE(level, '')) IN ('error','fatal') OR lower(COALESCE(message, '')) LIKE '%%error%%' OR lower(COALESCE(message, '')) LIKE '%%fatal%%' OR lower(COALESCE(message, '')) LIKE '%%panic%%') THEN 1 ELSE 0 END) AS logs_fail,
  SUM(CASE WHEN kind = 'log' AND NOT (lower(COALESCE(level, '')) = 'warn' OR lower(COALESCE(level, '')) IN ('error','fatal') OR lower(COALESCE(message, '')) LIKE '%%warn%%' OR lower(COALESCE(message, '')) LIKE '%%error%%' OR lower(COALESCE(message, '')) LIKE '%%fatal%%' OR lower(COALESCE(message, '')) LIKE '%%panic%%') THEN 1 ELSE 0 END) AS logs_info,
  SUM(CASE WHEN kind = 'deploy' THEN 1 ELSE 0 END) AS deploy,
  SUM(CASE WHEN kind = 'selection' THEN 1 ELSE 0 END) AS selection,
  0 AS artifacts,
  COUNT(1) AS any_events
FROM ktl_capture_events
WHERE %s
GROUP BY bucket_ns
ORDER BY bucket_ns
LIMIT 4000
`, keyExpr, where)

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]TimelineRow, 0, 512)
	for rows.Next() {
		var r TimelineRow
		if err := rows.Scan(&r.BucketNS, &r.LogsTotal, &r.LogsWarn, &r.LogsFail, &r.LogsInfo, &r.Deploy, &r.Selection, &r.Artifacts, &r.AnyEvents); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Best-effort overlay: bucket artifacts too (apply captures write lots of artifacts).
	artRows, err := s.db.QueryContext(ctx, fmt.Sprintf(`
SELECT
  (%s / %d) * %d AS bucket_ns,
  COUNT(1) AS artifacts
FROM ktl_capture_artifacts
WHERE session_id = ?
GROUP BY bucket_ns
ORDER BY bucket_ns
LIMIT 4000
`, `COALESCE(ts_ns, CAST(strftime('%s', ts) AS INTEGER) * 1000000000)`, bucketNS, bucketNS), sessionID)
	if err == nil {
		defer artRows.Close()
		byBucket := map[int64]int{}
		for i := range out {
			byBucket[out[i].BucketNS] = i
		}
		for artRows.Next() {
			var bucket int64
			var n int64
			if err := artRows.Scan(&bucket, &n); err != nil {
				break
			}
			if idx, ok := byBucket[bucket]; ok {
				out[idx].Artifacts = n
				continue
			}
			out = append(out, TimelineRow{BucketNS: bucket, Artifacts: n})
		}
		_ = artRows.Err()
		sort.Slice(out, func(i, j int) bool { return out[i].BucketNS < out[j].BucketNS })
	}
	return out, nil
}

func (s *sqliteStore) Logs(ctx context.Context, sessionID string, cursor int64, limit int, search string, startNS, endNS int64, dir string) (LogsPage, error) {
	if limit <= 0 {
		limit = 300
	}
	keyExpr := `COALESCE(seq, id)`
	tsExpr := `COALESCE(ts_ns, CAST(strftime('%s', ts) AS INTEGER) * 1000000000)`

	where := "session_id = ? AND kind = 'log'"
	args := []any{sessionID}
	if startNS > 0 {
		where += " AND " + tsExpr + " >= ?"
		args = append(args, startNS)
	}
	if endNS > 0 {
		where += " AND " + tsExpr + " <= ?"
		args = append(args, endNS)
	}
	if strings.TrimSpace(search) != "" {
		where += " AND (message LIKE ? OR namespace LIKE ? OR pod LIKE ? OR container LIKE ?)"
		pat := "%" + search + "%"
		args = append(args, pat, pat, pat, pat)
	}

	order := "ORDER BY k"
	cursorOp := ">"
	if strings.EqualFold(strings.TrimSpace(dir), "prev") {
		order = "ORDER BY k DESC"
		cursorOp = "<"
	}
	if cursor > 0 {
		where += " AND " + keyExpr + " " + cursorOp + " ?"
		args = append(args, cursor)
	}

	query := fmt.Sprintf(`
SELECT
  %s AS k,
  COALESCE(seq, 0),
  id,
  %s AS ts_ns,
  kind,
  COALESCE(level, ''),
  COALESCE(source, ''),
  COALESCE(namespace, ''),
  COALESCE(pod, ''),
  COALESCE(container, ''),
  COALESCE(message, '')
FROM ktl_capture_events
WHERE %s
%s
LIMIT %d
`, keyExpr, tsExpr, where, order, limit+1)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return LogsPage{}, err
	}
	defer rows.Close()
	out := LogsPage{Cursor: cursor}
	for rows.Next() {
		var l LogLine
		if err := rows.Scan(&l.Key, &l.Seq, &l.ID, &l.TSNS, &l.Kind, &l.Level, &l.Source, &l.Namespace, &l.Pod, &l.Container, &l.Message); err != nil {
			return LogsPage{}, err
		}
		out.Lines = append(out.Lines, l)
		out.Cursor = l.Key
		if len(out.Lines) == limit+1 {
			out.HasMore = true
			break
		}
	}
	if err := rows.Err(); err != nil {
		return LogsPage{}, err
	}
	if out.HasMore {
		out.Lines = out.Lines[:limit]
	}
	if strings.EqualFold(strings.TrimSpace(dir), "prev") {
		for i, j := 0, len(out.Lines)-1; i < j; i, j = i+1, j-1 {
			out.Lines[i], out.Lines[j] = out.Lines[j], out.Lines[i]
		}
		if len(out.Lines) > 0 {
			out.Cursor = out.Lines[0].Key
		}
	}
	return out, nil
}

func (s *sqliteStore) Events(ctx context.Context, sessionID string, cursor int64, limit int, search string, startNS, endNS int64) (EventsPage, error) {
	if limit <= 0 {
		limit = 200
	}
	keyExpr := `COALESCE(seq, id)`
	tsExpr := `COALESCE(ts_ns, CAST(strftime('%s', ts) AS INTEGER) * 1000000000)`

	where := "session_id = ? AND kind != 'log'"
	args := []any{sessionID}
	if startNS > 0 {
		where += " AND " + tsExpr + " >= ?"
		args = append(args, startNS)
	}
	if endNS > 0 {
		where += " AND " + tsExpr + " <= ?"
		args = append(args, endNS)
	}
	if cursor > 0 {
		where += " AND " + keyExpr + " > ?"
		args = append(args, cursor)
	}
	if strings.TrimSpace(search) != "" {
		where += " AND (message LIKE ? OR kind LIKE ? OR source LIKE ? OR namespace LIKE ? OR pod LIKE ?)"
		pat := "%" + search + "%"
		args = append(args, pat, pat, pat, pat, pat)
	}

	query := fmt.Sprintf(`
SELECT
  %s AS k,
  COALESCE(seq, 0),
  id,
  %s AS ts_ns,
  COALESCE(kind, ''),
  COALESCE(level, ''),
  COALESCE(source, ''),
  COALESCE(namespace, ''),
  COALESCE(pod, ''),
  COALESCE(container, ''),
  COALESCE(message, '')
FROM ktl_capture_events
WHERE %s
ORDER BY k
LIMIT %d
`, keyExpr, tsExpr, where, limit)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return EventsPage{}, err
	}
	defer rows.Close()
	out := EventsPage{Cursor: cursor}
	for rows.Next() {
		var e EventLine
		if err := rows.Scan(&e.Key, &e.Seq, &e.ID, &e.TSNS, &e.Kind, &e.Level, &e.Source, &e.Namespace, &e.Pod, &e.Container, &e.Message); err != nil {
			return EventsPage{}, err
		}
		out.Events = append(out.Events, e)
		out.Cursor = e.Key
	}
	return out, rows.Err()
}
