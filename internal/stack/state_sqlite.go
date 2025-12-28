package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/example/ktl/internal/version"
)

const stackStateSQLiteRelPath = ".ktl/stack/state.sqlite"

type stackStateStore struct {
	db       *sql.DB
	path     string
	readOnly bool
}

func openStackStateStore(root string, readOnly bool) (*stackStateStore, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(absRoot, stackStateSQLiteRelPath)
	if readOnly {
		if _, err := os.Stat(path); err != nil {
			return nil, err
		}
	} else {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
	}

	dsn := path
	if readOnly {
		u := url.URL{Scheme: "file", Path: path}
		q := u.Query()
		q.Set("mode", "ro")
		u.RawQuery = q.Encode()
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
	// Make lock contention tolerable for concurrent readers/writers (status --follow
	// while a run is executing). This is safe in both ro and rw modes.
	_, _ = db.ExecContext(ctx, `PRAGMA busy_timeout=5000;`)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	s := &stackStateStore{db: db, path: path, readOnly: readOnly}
	if !readOnly {
		if err := s.initSchema(ctx); err != nil {
			_ = s.Close()
			return nil, err
		}
	}
	return s, nil
}

func (s *stackStateStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *stackStateStore) CheckpointPortable(ctx context.Context) error {
	if s == nil || s.db == nil || s.readOnly {
		return nil
	}
	// Fold WAL back into the main DB file so it can be moved/copied as a single .sqlite.
	// TRUNCATE also removes/empties the -wal file on success.
	if _, err := s.db.ExecContext(ctx, `PRAGMA wal_checkpoint(TRUNCATE);`); err != nil {
		return fmt.Errorf("wal checkpoint: %w", err)
	}
	return nil
}

func (s *stackStateStore) initSchema(ctx context.Context) error {
	stmts := []string{
		`PRAGMA journal_mode=WAL;`,
		`PRAGMA synchronous=NORMAL;`,
		`PRAGMA foreign_keys=ON;`,
		`PRAGMA busy_timeout=5000;`,
		`
CREATE TABLE IF NOT EXISTS ktl_stack_runs (
  run_id TEXT PRIMARY KEY,
  stack_root TEXT NOT NULL,
  stack_name TEXT NOT NULL,
  profile TEXT NOT NULL,
  command TEXT NOT NULL,
  concurrency INTEGER NOT NULL,
  fail_mode TEXT NOT NULL,
  status TEXT NOT NULL,
  created_at_ns INTEGER NOT NULL,
  updated_at_ns INTEGER NOT NULL,
  completed_at_ns INTEGER NOT NULL DEFAULT 0,
  created_by TEXT NOT NULL DEFAULT '',
  host TEXT NOT NULL DEFAULT '',
  pid INTEGER NOT NULL DEFAULT 0,
  ci_run_url TEXT NOT NULL DEFAULT '',
  git_author TEXT NOT NULL DEFAULT '',
  kubeconfig TEXT NOT NULL DEFAULT '',
  kube_context TEXT NOT NULL DEFAULT '',
  selector_json TEXT NOT NULL,
  plan_json TEXT NOT NULL,
  summary_json TEXT NOT NULL,
  last_event_digest TEXT NOT NULL DEFAULT '',
  run_digest TEXT NOT NULL DEFAULT ''
);`,
		`
CREATE TABLE IF NOT EXISTS ktl_stack_nodes (
  run_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  error TEXT NOT NULL,
  last_error_class TEXT NOT NULL DEFAULT '',
  last_error_digest TEXT NOT NULL DEFAULT '',
  updated_at_ns INTEGER NOT NULL DEFAULT 0,
  PRIMARY KEY (run_id, node_id),
  FOREIGN KEY (run_id) REFERENCES ktl_stack_runs(run_id) ON DELETE CASCADE
);`,
		`
CREATE TABLE IF NOT EXISTS ktl_stack_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  run_id TEXT NOT NULL,
  ts_ns INTEGER NOT NULL,
  node_id TEXT NOT NULL,
  type TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  message TEXT NOT NULL,
  fields_json TEXT NOT NULL DEFAULT '',
  error_class TEXT NOT NULL,
  error_message TEXT NOT NULL,
  error_digest TEXT NOT NULL,
  seq INTEGER NOT NULL DEFAULT 0,
  prev_digest TEXT NOT NULL DEFAULT '',
  digest TEXT NOT NULL DEFAULT '',
  crc32 TEXT NOT NULL DEFAULT '',
  FOREIGN KEY (run_id) REFERENCES ktl_stack_runs(run_id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_ktl_stack_events_run_id_id ON ktl_stack_events(run_id, id);`,
		`CREATE INDEX IF NOT EXISTS idx_ktl_stack_events_run_id_error_digest ON ktl_stack_events(run_id, error_digest);`,
		`CREATE INDEX IF NOT EXISTS idx_ktl_stack_nodes_run_id_status ON ktl_stack_nodes(run_id, status);`,
		`
CREATE TABLE IF NOT EXISTS ktl_stack_lock (
  id INTEGER PRIMARY KEY CHECK (id = 1),
  owner TEXT NOT NULL,
  run_id TEXT NOT NULL,
  created_at_ns INTEGER NOT NULL,
  ttl_ns INTEGER NOT NULL
);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	if err := s.ensureEventsIntegrityColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureRunColumns(ctx); err != nil {
		return err
	}
	if err := s.ensureNodeColumns(ctx); err != nil {
		return err
	}
	return nil
}

func (s *stackStateStore) ensureEventsIntegrityColumns(ctx context.Context) error {
	cols, err := s.tableColumns(ctx, "ktl_stack_events")
	if err != nil {
		return err
	}
	want := map[string]string{
		"fields_json": "TEXT NOT NULL DEFAULT ''",
		"seq":         "INTEGER NOT NULL DEFAULT 0",
		"prev_digest": "TEXT NOT NULL DEFAULT ''",
		"digest":      "TEXT NOT NULL DEFAULT ''",
		"crc32":       "TEXT NOT NULL DEFAULT ''",
	}
	for name, ddl := range want {
		if _, ok := cols[name]; ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE ktl_stack_events ADD COLUMN %s %s;", name, ddl)); err != nil {
			return fmt.Errorf("add column ktl_stack_events.%s: %w", name, err)
		}
	}
	return nil
}

func (s *stackStateStore) ensureRunColumns(ctx context.Context) error {
	cols, err := s.tableColumns(ctx, "ktl_stack_runs")
	if err != nil {
		return err
	}
	want := map[string]string{
		"completed_at_ns":   "INTEGER NOT NULL DEFAULT 0",
		"created_by":        "TEXT NOT NULL DEFAULT ''",
		"host":              "TEXT NOT NULL DEFAULT ''",
		"pid":               "INTEGER NOT NULL DEFAULT 0",
		"ci_run_url":        "TEXT NOT NULL DEFAULT ''",
		"git_author":        "TEXT NOT NULL DEFAULT ''",
		"kubeconfig":        "TEXT NOT NULL DEFAULT ''",
		"kube_context":      "TEXT NOT NULL DEFAULT ''",
		"last_event_digest": "TEXT NOT NULL DEFAULT ''",
		"run_digest":        "TEXT NOT NULL DEFAULT ''",
	}
	for name, ddl := range want {
		if _, ok := cols[name]; ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE ktl_stack_runs ADD COLUMN %s %s;", name, ddl)); err != nil {
			return fmt.Errorf("add column ktl_stack_runs.%s: %w", name, err)
		}
	}
	return nil
}

func (s *stackStateStore) ensureNodeColumns(ctx context.Context) error {
	cols, err := s.tableColumns(ctx, "ktl_stack_nodes")
	if err != nil {
		return err
	}
	want := map[string]string{
		"last_error_class":  "TEXT NOT NULL DEFAULT ''",
		"last_error_digest": "TEXT NOT NULL DEFAULT ''",
		"updated_at_ns":     "INTEGER NOT NULL DEFAULT 0",
	}
	for name, ddl := range want {
		if _, ok := cols[name]; ok {
			continue
		}
		if _, err := s.db.ExecContext(ctx, fmt.Sprintf("ALTER TABLE ktl_stack_nodes ADD COLUMN %s %s;", name, ddl)); err != nil {
			return fmt.Errorf("add column ktl_stack_nodes.%s: %w", name, err)
		}
	}
	return nil
}

func (s *stackStateStore) tableColumns(ctx context.Context, table string) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s);", table))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	cols := map[string]struct{}{}
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dfltValue any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dfltValue, &pk); err != nil {
			return nil, err
		}
		cols[name] = struct{}{}
	}
	return cols, rows.Err()
}

func (s *stackStateStore) CreateRun(ctx context.Context, run *runState, p *Plan) error {
	now := time.Now().UTC()

	payload := buildRunPlanPayload(run, p)
	gid, err := GitIdentityForRoot(p.StackRoot)
	if err != nil {
		return err
	}
	payload.StackGitCommit = gid.Commit
	payload.StackGitDirty = gid.Dirty
	payload.KtlVersion = version.Version
	payload.KtlGitCommit = version.GitCommit
	planHash, err := ComputeRunPlanHash(payload)
	if err != nil {
		return err
	}
	payload.PlanHash = planHash
	planJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	selectorJSON, err := json.Marshal(payload.Selector)
	if err != nil {
		return err
	}

	emptySummary := RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      run.RunID,
		Status:     "created",
		StartedAt:  now.Format(time.RFC3339Nano),
		UpdatedAt:  now.Format(time.RFC3339Nano),
		Totals:     RunTotals{Planned: len(run.Nodes)},
		Nodes:      map[string]RunNodeSummary{},
		Order:      payloadNodesOrder(p),
	}
	for _, n := range run.Nodes {
		emptySummary.Nodes[n.ID] = RunNodeSummary{Status: "planned", Attempt: 0, Error: ""}
	}
	summaryJSON, err := json.Marshal(emptySummary)
	if err != nil {
		return err
	}

	createdBy := strings.TrimSpace(run.lockOwner)
	if createdBy == "" {
		createdBy = defaultLockOwner()
	}
	host, _ := os.Hostname()
	host = strings.TrimSpace(host)
	if host == "" {
		host = "unknown-host"
	}
	pid := os.Getpid()
	ciRunURL := strings.TrimSpace(ciRunURLFromEnv())
	gitAuthor := strings.TrimSpace(gitAuthorForRoot(p.StackRoot))
	kubeconfig := strings.TrimSpace(run.Kubeconfig)
	kubeContext := strings.TrimSpace(run.KubeContext)

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
INSERT INTO ktl_stack_runs (
  run_id, stack_root, stack_name, profile, command, concurrency, fail_mode, status,
  created_at_ns, updated_at_ns, completed_at_ns, created_by, host, pid,
  ci_run_url, git_author, kubeconfig, kube_context,
  selector_json, plan_json, summary_json, last_event_digest, run_digest
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, run.RunID, p.StackRoot, p.StackName, p.Profile, run.Command, run.Concurrency, run.FailMode, "running",
		now.UnixNano(), now.UnixNano(), int64(0), createdBy, host, pid,
		ciRunURL, gitAuthor, kubeconfig, kubeContext,
		string(selectorJSON), string(planJSON), string(summaryJSON), "", "")
	if err != nil {
		return err
	}

	for _, n := range run.Nodes {
		_, err := tx.ExecContext(ctx, `
INSERT INTO ktl_stack_nodes (run_id, node_id, status, attempt, error)
VALUES (?, ?, ?, ?, ?)
`, run.RunID, n.ID, "planned", 0, "")
		if err != nil {
			return err
		}
	}

	if err := tx.Commit(); err != nil {
		return err
	}
	return nil
}

func payloadNodesOrder(p *Plan) []string {
	var order []string
	for _, n := range p.Nodes {
		order = append(order, n.ID)
	}
	return order
}

func (s *stackStateStore) AppendEvent(ctx context.Context, runID string, ev RunEvent) error {
	ts, err := time.Parse(time.RFC3339Nano, ev.TS)
	if err != nil {
		ts = time.Now().UTC()
	}
	nodeID := strings.TrimSpace(ev.NodeID)
	errClass := ""
	errMsg := ""
	errDigest := ""
	if ev.Error != nil {
		errClass = strings.TrimSpace(ev.Error.Class)
		errMsg = strings.TrimSpace(ev.Error.Message)
		errDigest = strings.TrimSpace(ev.Error.Digest)
	}
	if nodeID == "" {
		nodeID = ""
	}
	msg := strings.TrimSpace(ev.Message)
	fieldsJSON := ""
	if raw, err := json.Marshal(ev.Fields); err == nil {
		fieldsJSON = string(raw)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ktl_stack_events (run_id, ts_ns, node_id, type, attempt, message, fields_json, error_class, error_message, error_digest, seq, prev_digest, digest, crc32)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, ts.UnixNano(), nodeID, ev.Type, ev.Attempt, msg, fieldsJSON, errClass, errMsg, errDigest, ev.Seq, ev.PrevDigest, ev.Digest, ev.CRC32)
	if err != nil {
		return err
	}

	updatedAt := time.Now().UTC().UnixNano()
	_, _ = s.db.ExecContext(ctx, `UPDATE ktl_stack_runs SET updated_at_ns = ? WHERE run_id = ?`, updatedAt, runID)

	switch ev.Type {
	case "NODE_RUNNING", "NODE_SUCCEEDED", "NODE_FAILED", "NODE_BLOCKED":
		status := ""
		switch ev.Type {
		case "NODE_RUNNING":
			status = "running"
		case "NODE_SUCCEEDED":
			status = "succeeded"
		case "NODE_FAILED":
			status = "failed"
		case "NODE_BLOCKED":
			status = "blocked"
		}
		if status != "" && nodeID != "" {
			nodeErr := ""
			lastErrClass := ""
			lastErrDigest := ""
			if status == "failed" {
				nodeErr = errMsg
				lastErrClass = errClass
				lastErrDigest = errDigest
			}
			updatedNodeAt := time.Now().UTC().UnixNano()
			_, _ = s.db.ExecContext(ctx, `
UPDATE ktl_stack_nodes
SET status = ?, attempt = CASE WHEN ? > attempt THEN ? ELSE attempt END, error = ?, last_error_class = ?, last_error_digest = ?, updated_at_ns = ?
WHERE run_id = ? AND node_id = ?
`, status, ev.Attempt, ev.Attempt, nodeErr, lastErrClass, lastErrDigest, updatedNodeAt, runID, nodeID)
		}
	}

	if ev.Type == "RUN_COMPLETED" {
		status := strings.TrimSpace(ev.Message)
		if status == "" {
			status = "completed"
		}
		_, _ = s.db.ExecContext(ctx, `UPDATE ktl_stack_runs SET status = ?, updated_at_ns = ? WHERE run_id = ?`, status, updatedAt, runID)
	}
	return nil
}

func (s *stackStateStore) WriteSummary(ctx context.Context, runID string, summary *RunSummary) error {
	raw, err := json.Marshal(summary)
	if err != nil {
		return err
	}
	updatedAt := time.Now().UTC().UnixNano()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `UPDATE ktl_stack_runs SET summary_json = ?, status = ?, updated_at_ns = ? WHERE run_id = ?`, string(raw), summary.Status, updatedAt, runID)
	if err != nil {
		return err
	}
	for nodeID, ns := range summary.Nodes {
		nodeErr := strings.TrimSpace(ns.Error)
		updatedNodeAt := updatedAt
		_, err := tx.ExecContext(ctx, `
UPDATE ktl_stack_nodes
SET status = ?, attempt = ?, error = ?, updated_at_ns = ?
WHERE run_id = ? AND node_id = ?
`, ns.Status, ns.Attempt, nodeErr, updatedNodeAt, runID, nodeID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *stackStateStore) FinalizeRun(ctx context.Context, runID string, completedAtNS int64, lastEventDigest string) (string, error) {
	if s == nil || s.db == nil {
		return "", fmt.Errorf("state store not initialized")
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "", fmt.Errorf("run id is required")
	}
	if completedAtNS <= 0 {
		completedAtNS = time.Now().UTC().UnixNano()
	}
	lastEventDigest = strings.TrimSpace(lastEventDigest)

	var planJSON string
	var summaryJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT plan_json, summary_json FROM ktl_stack_runs WHERE run_id = ?`, runID).Scan(&planJSON, &summaryJSON); err != nil {
		return "", err
	}
	digest := computeRunDigest(planJSON, summaryJSON, lastEventDigest)
	_, err := s.db.ExecContext(ctx, `
UPDATE ktl_stack_runs
SET completed_at_ns = ?, last_event_digest = ?, run_digest = ?, updated_at_ns = CASE WHEN updated_at_ns < ? THEN ? ELSE updated_at_ns END
WHERE run_id = ?
`, completedAtNS, lastEventDigest, digest, completedAtNS, completedAtNS, runID)
	if err != nil {
		return "", err
	}
	return digest, nil
}

func (s *stackStateStore) GetRunSummary(ctx context.Context, runID string) (*RunSummary, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT summary_json FROM ktl_stack_runs WHERE run_id = ?`, runID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var ssum RunSummary
	if err := json.Unmarshal([]byte(raw), &ssum); err != nil {
		return nil, err
	}
	return &ssum, nil
}

func (s *stackStateStore) GetRunPlan(ctx context.Context, runID string) (*Plan, error) {
	var raw string
	err := s.db.QueryRowContext(ctx, `SELECT plan_json FROM ktl_stack_runs WHERE run_id = ?`, runID).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var rp RunPlan
	if err := json.Unmarshal([]byte(raw), &rp); err != nil {
		return nil, err
	}
	return PlanFromRunPlan(&rp)
}

func (s *stackStateStore) VerifyEventsIntegrity(ctx context.Context, runID string) error {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, ts_ns, node_id, type, attempt, message, fields_json, error_class, error_message, error_digest, seq, prev_digest, digest, crc32
FROM ktl_stack_events
WHERE run_id = ?
ORDER BY id ASC
`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()

	prev := ""
	checked := false
	for rows.Next() {
		var r sqliteEventRow
		if err := rows.Scan(&r.id, &r.tsNS, &r.nodeID, &r.typ, &r.attempt, &r.message, &r.fieldsJSON, &r.errClass, &r.errMessage, &r.errDigest, &r.seq, &r.prevDigest, &r.digest, &r.crc32); err != nil {
			return err
		}
		ev := sqliteRowToRunEvent(runID, r)
		if !checked {
			if strings.TrimSpace(ev.Digest) == "" || strings.TrimSpace(ev.CRC32) == "" {
				return nil
			}
			checked = true
		}
		if strings.TrimSpace(ev.PrevDigest) != strings.TrimSpace(prev) {
			return fmt.Errorf("event prevDigest mismatch (want %q got %q)", prev, ev.PrevDigest)
		}
		wantDigest, wantCRC := computeRunEventIntegrity(ev)
		if strings.TrimSpace(ev.Digest) != strings.TrimSpace(wantDigest) {
			return fmt.Errorf("event digest mismatch (want %q got %q)", wantDigest, ev.Digest)
		}
		if strings.TrimSpace(ev.CRC32) != strings.TrimSpace(wantCRC) {
			return fmt.Errorf("event crc32 mismatch (want %q got %q)", wantCRC, ev.CRC32)
		}
		prev = ev.Digest
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

func (s *stackStateStore) GetNodeStatus(ctx context.Context, runID string) (map[string]string, map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT node_id, status, attempt FROM ktl_stack_nodes WHERE run_id = ?`, runID)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	status := map[string]string{}
	attempt := map[string]int{}
	for rows.Next() {
		var id, st string
		var a int
		if err := rows.Scan(&id, &st, &a); err != nil {
			return nil, nil, err
		}
		status[id] = st
		attempt[id] = a
	}
	return status, attempt, rows.Err()
}

type sqliteEventRow struct {
	id         int64
	tsNS       int64
	nodeID     string
	typ        string
	attempt    int
	message    string
	fieldsJSON string
	errClass   string
	errMessage string
	errDigest  string
	seq        int64
	prevDigest string
	digest     string
	crc32      string
}

func (s *stackStateStore) TailEvents(ctx context.Context, runID string, limit int) ([]RunEvent, int64, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, ts_ns, node_id, type, attempt, message, fields_json, error_class, error_message, error_digest, seq, prev_digest, digest, crc32
FROM ktl_stack_events
WHERE run_id = ?
ORDER BY id DESC
LIMIT ?
`, runID, limit)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	var raw []sqliteEventRow
	var maxID int64
	for rows.Next() {
		var r sqliteEventRow
		if err := rows.Scan(&r.id, &r.tsNS, &r.nodeID, &r.typ, &r.attempt, &r.message, &r.fieldsJSON, &r.errClass, &r.errMessage, &r.errDigest, &r.seq, &r.prevDigest, &r.digest, &r.crc32); err != nil {
			return nil, 0, err
		}
		if r.id > maxID {
			maxID = r.id
		}
		raw = append(raw, r)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, err
	}

	// Reverse to chronological order.
	for i, j := 0, len(raw)-1; i < j; i, j = i+1, j-1 {
		raw[i], raw[j] = raw[j], raw[i]
	}

	out := make([]RunEvent, 0, len(raw))
	for _, r := range raw {
		out = append(out, sqliteRowToRunEvent(runID, r))
	}
	return out, maxID, nil
}

func (s *stackStateStore) EventsAfter(ctx context.Context, runID string, afterID int64, limit int) ([]RunEvent, int64, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT id, ts_ns, node_id, type, attempt, message, fields_json, error_class, error_message, error_digest, seq, prev_digest, digest, crc32
FROM ktl_stack_events
WHERE run_id = ? AND id > ?
ORDER BY id ASC
LIMIT ?
`, runID, afterID, limit)
	if err != nil {
		return nil, afterID, err
	}
	defer rows.Close()

	var out []RunEvent
	maxID := afterID
	for rows.Next() {
		var r sqliteEventRow
		if err := rows.Scan(&r.id, &r.tsNS, &r.nodeID, &r.typ, &r.attempt, &r.message, &r.fieldsJSON, &r.errClass, &r.errMessage, &r.errDigest, &r.seq, &r.prevDigest, &r.digest, &r.crc32); err != nil {
			return nil, afterID, err
		}
		if r.id > maxID {
			maxID = r.id
		}
		out = append(out, sqliteRowToRunEvent(runID, r))
	}
	if err := rows.Err(); err != nil {
		return nil, afterID, err
	}
	return out, maxID, nil
}

func (s *stackStateStore) ListEvents(ctx context.Context, runID string, limit int) ([]RunEvent, error) {
	if limit < 0 {
		limit = 0
	}
	query := `
SELECT id, ts_ns, node_id, type, attempt, message, fields_json, error_class, error_message, error_digest, seq, prev_digest, digest, crc32
FROM ktl_stack_events
WHERE run_id = ?
ORDER BY id ASC
`
	args := []any{runID}
	if limit > 0 {
		query += "\nLIMIT ?"
		args = append(args, limit)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunEvent
	for rows.Next() {
		var r sqliteEventRow
		if err := rows.Scan(&r.id, &r.tsNS, &r.nodeID, &r.typ, &r.attempt, &r.message, &r.fieldsJSON, &r.errClass, &r.errMessage, &r.errDigest, &r.seq, &r.prevDigest, &r.digest, &r.crc32); err != nil {
			return nil, err
		}
		out = append(out, sqliteRowToRunEvent(runID, r))
	}
	return out, rows.Err()
}

func sqliteRowToRunEvent(runID string, r sqliteEventRow) RunEvent {
	ev := RunEvent{
		Seq:     r.seq,
		TS:      time.Unix(0, r.tsNS).UTC().Format(time.RFC3339Nano),
		RunID:   runID,
		NodeID:  strings.TrimSpace(r.nodeID),
		Type:    strings.TrimSpace(r.typ),
		Attempt: r.attempt,
		Message: strings.TrimSpace(r.message),

		PrevDigest: strings.TrimSpace(r.prevDigest),
		Digest:     strings.TrimSpace(r.digest),
		CRC32:      strings.TrimSpace(r.crc32),
	}
	if strings.TrimSpace(r.fieldsJSON) != "" {
		var fields map[string]any
		_ = json.Unmarshal([]byte(r.fieldsJSON), &fields)
		if len(fields) > 0 {
			ev.Fields = fields
		}
	}
	if strings.TrimSpace(r.errClass) != "" || strings.TrimSpace(r.errMessage) != "" || strings.TrimSpace(r.errDigest) != "" {
		ev.Error = &RunError{
			Class:   strings.TrimSpace(r.errClass),
			Message: strings.TrimSpace(r.errMessage),
			Digest:  strings.TrimSpace(r.errDigest),
		}
	}
	return ev
}

func (s *stackStateStore) MostRecentRunID(ctx context.Context) (string, error) {
	var runID string
	err := s.db.QueryRowContext(ctx, `SELECT run_id FROM ktl_stack_runs ORDER BY created_at_ns DESC LIMIT 1`).Scan(&runID)
	return runID, err
}

func (s *stackStateStore) ListRuns(ctx context.Context, limit int) ([]RunIndexEntry, error) {
	if limit <= 0 {
		limit = 20
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT run_id, summary_json
FROM ktl_stack_runs
ORDER BY created_at_ns DESC
LIMIT ?
`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RunIndexEntry
	for rows.Next() {
		var id string
		var raw string
		if err := rows.Scan(&id, &raw); err != nil {
			return nil, err
		}
		var ssum RunSummary
		if err := json.Unmarshal([]byte(raw), &ssum); err != nil {
			out = append(out, RunIndexEntry{RunID: id, HasSummary: false})
			continue
		}
		out = append(out, RunIndexEntry{
			RunID:      id,
			RunRoot:    s.path,
			Status:     ssum.Status,
			StartedAt:  ssum.StartedAt,
			UpdatedAt:  ssum.UpdatedAt,
			Totals:     ssum.Totals,
			HasSummary: true,
		})
	}
	return out, rows.Err()
}

type StackLock struct {
	Owner     string
	RunID     string
	CreatedAt time.Time
	TTL       time.Duration
}

func (s *stackStateStore) GetLock(ctx context.Context) (*StackLock, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	var owner, runID string
	var createdAtNS, ttlNS int64
	err := s.db.QueryRowContext(ctx, `SELECT owner, run_id, created_at_ns, ttl_ns FROM ktl_stack_lock WHERE id = 1`).Scan(&owner, &runID, &createdAtNS, &ttlNS)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &StackLock{
		Owner:     owner,
		RunID:     runID,
		CreatedAt: time.Unix(0, createdAtNS).UTC(),
		TTL:       time.Duration(ttlNS),
	}, nil
}

func (s *stackStateStore) AcquireLock(ctx context.Context, owner string, ttl time.Duration, takeover bool, runID string) (*StackLock, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("state store not initialized")
	}
	if s.readOnly {
		return nil, fmt.Errorf("cannot acquire lock in read-only mode")
	}
	owner = strings.TrimSpace(owner)
	if owner == "" {
		owner = defaultLockOwner()
	}
	if ttl <= 0 {
		ttl = 30 * time.Minute
	}
	now := time.Now().UTC()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	var curOwner, curRunID string
	var createdAtNS, ttlNS int64
	err = tx.QueryRowContext(ctx, `SELECT owner, run_id, created_at_ns, ttl_ns FROM ktl_stack_lock WHERE id = 1`).Scan(&curOwner, &curRunID, &createdAtNS, &ttlNS)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}

	expired := false
	if err == nil {
		created := time.Unix(0, createdAtNS).UTC()
		curTTL := time.Duration(ttlNS)
		if curTTL <= 0 {
			curTTL = 30 * time.Minute
		}
		if now.After(created.Add(curTTL)) {
			expired = true
		}
		if !expired && !takeover {
			return nil, fmt.Errorf("stack state is locked by %q (runId=%s, createdAt=%s, ttl=%s); rerun with --takeover to steal the lock",
				curOwner, curRunID, created.Format(time.RFC3339), curTTL.String())
		}
		_, err := tx.ExecContext(ctx, `UPDATE ktl_stack_lock SET owner = ?, run_id = ?, created_at_ns = ?, ttl_ns = ? WHERE id = 1`,
			owner, strings.TrimSpace(runID), now.UnixNano(), ttl.Nanoseconds())
		if err != nil {
			return nil, err
		}
	} else {
		_, err := tx.ExecContext(ctx, `INSERT INTO ktl_stack_lock (id, owner, run_id, created_at_ns, ttl_ns) VALUES (1, ?, ?, ?, ?)`,
			owner, strings.TrimSpace(runID), now.UnixNano(), ttl.Nanoseconds())
		if err != nil {
			return nil, err
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return &StackLock{
		Owner:     owner,
		RunID:     strings.TrimSpace(runID),
		CreatedAt: now,
		TTL:       ttl,
	}, nil
}

func (s *stackStateStore) ReleaseLock(ctx context.Context, owner string, runID string) error {
	if s == nil || s.db == nil || s.readOnly {
		return nil
	}
	owner = strings.TrimSpace(owner)
	runID = strings.TrimSpace(runID)
	if owner == "" {
		return nil
	}
	_, _ = s.db.ExecContext(ctx, `DELETE FROM ktl_stack_lock WHERE id = 1 AND owner = ? AND run_id = ?`, owner, runID)
	return nil
}
