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
		q.Set("_busy_timeout", "5000")
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
  selector_json TEXT NOT NULL,
  plan_json TEXT NOT NULL,
  summary_json TEXT NOT NULL
);`,
		`
CREATE TABLE IF NOT EXISTS ktl_stack_nodes (
  run_id TEXT NOT NULL,
  node_id TEXT NOT NULL,
  status TEXT NOT NULL,
  attempt INTEGER NOT NULL,
  error TEXT NOT NULL,
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
  error_class TEXT NOT NULL,
  error_message TEXT NOT NULL,
  error_digest TEXT NOT NULL,
  FOREIGN KEY (run_id) REFERENCES ktl_stack_runs(run_id) ON DELETE CASCADE
);`,
		`CREATE INDEX IF NOT EXISTS idx_ktl_stack_events_run_id_id ON ktl_stack_events(run_id, id);`,
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("init schema: %w", err)
		}
	}
	return nil
}

func (s *stackStateStore) CreateRun(ctx context.Context, run *runState, p *Plan) error {
	now := time.Now().UTC()

	payload := buildRunPlanPayload(run, p)
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

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.ExecContext(ctx, `
INSERT INTO ktl_stack_runs (
  run_id, stack_root, stack_name, profile, command, concurrency, fail_mode, status,
  created_at_ns, updated_at_ns, selector_json, plan_json, summary_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, run.RunID, p.StackRoot, p.StackName, p.Profile, run.Command, run.Concurrency, run.FailMode, "running",
		now.UnixNano(), now.UnixNano(), string(selectorJSON), string(planJSON), string(summaryJSON))
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
	_, err = s.db.ExecContext(ctx, `
INSERT INTO ktl_stack_events (run_id, ts_ns, node_id, type, attempt, message, error_class, error_message, error_digest)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, ts.UnixNano(), nodeID, ev.Type, ev.Attempt, msg, errClass, errMsg, errDigest)
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
			if status == "failed" {
				nodeErr = errMsg
			}
			_, _ = s.db.ExecContext(ctx, `
UPDATE ktl_stack_nodes
SET status = ?, attempt = CASE WHEN ? > attempt THEN ? ELSE attempt END, error = ?
WHERE run_id = ? AND node_id = ?
`, status, ev.Attempt, ev.Attempt, nodeErr, runID, nodeID)
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
		_, err := tx.ExecContext(ctx, `
UPDATE ktl_stack_nodes
SET status = ?, attempt = ?, error = ?
WHERE run_id = ? AND node_id = ?
`, ns.Status, ns.Attempt, nodeErr, runID, nodeID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
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
	p := &Plan{
		StackRoot: rp.StackRoot,
		StackName: rp.StackName,
		Profile:   rp.Profile,
		Nodes:     rp.Nodes,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range p.Nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}
	if err := assignExecutionGroups(p); err != nil {
		return nil, err
	}
	return p, nil
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
