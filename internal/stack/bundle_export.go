package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type RunBundleManifest struct {
	APIVersion  string `json:"apiVersion"`
	Kind        string `json:"kind"`
	CreatedAt   string `json:"createdAt"`
	RunID       string `json:"runId"`
	RunDigest   string `json:"runDigest,omitempty"`
	StateSHA256 string `json:"stateSha256"`
}

func ExportRunBundle(ctx context.Context, root string, runID string, outPath string) (string, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return "", fmt.Errorf("run id is required")
	}
	if strings.TrimSpace(outPath) == "" {
		outPath = filepath.Join(root, ".ktl", "stack", "exports", runID+".tgz")
	}

	src, err := openStackStateStore(root, true)
	if err != nil {
		return "", err
	}
	defer src.Close()

	tmpRoot, err := os.MkdirTemp("", "ktl-stack-export-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpRoot)

	dst, err := openStackStateStore(tmpRoot, false)
	if err != nil {
		return "", err
	}
	defer dst.Close()

	if err := copyRunRow(ctx, src.db, dst.db, runID); err != nil {
		return "", err
	}
	if err := copyRunNodes(ctx, src.db, dst.db, runID); err != nil {
		return "", err
	}
	if err := copyRunEvents(ctx, src.db, dst.db, runID); err != nil {
		return "", err
	}
	_ = dst.CheckpointPortable(ctx)

	statePath := filepath.Join(tmpRoot, stackStateSQLiteRelPath)
	stateSHA, err := sha256File(statePath)
	if err != nil {
		return "", err
	}

	// Read run_digest for convenience.
	var runDigest string
	_ = dst.db.QueryRowContext(ctx, `SELECT run_digest FROM ktl_stack_runs WHERE run_id = ?`, runID).Scan(&runDigest)
	runDigest = strings.TrimSpace(runDigest)

	manifest := RunBundleManifest{
		APIVersion:  "ktl.dev/stack-bundle/v1",
		Kind:        "StackRunBundle",
		CreatedAt:   time.Now().UTC().Format(time.RFC3339Nano),
		RunID:       runID,
		RunDigest:   runDigest,
		StateSHA256: stateSHA,
	}

	manifestPath := filepath.Join(tmpRoot, "manifest.json")
	if err := writeJSONAtomicFile(manifestPath, manifest); err != nil {
		return "", err
	}

	if err := writeDeterministicTarGz(outPath, []tarFile{
		{Name: "state.sqlite", Path: statePath, Mode: 0o644},
		{Name: "manifest.json", Path: manifestPath, Mode: 0o644},
	}); err != nil {
		return "", err
	}
	return outPath, nil
}

func copyRunRow(ctx context.Context, src *sql.DB, dst *sql.DB, runID string) error {
	var (
		stackRoot, stackName, profile, command, failMode, status   string
		createdAtNS, updatedAtNS, completedAtNS                    int64
		concurrency, pid                                           int
		createdBy, host, ciURL, gitAuthor, kubeconfig, kubeContext string
		selectorJSON, planJSON, summaryJSON                        string
		lastEventDigest, runDigest                                 string
	)
	err := src.QueryRowContext(ctx, `
SELECT stack_root, stack_name, profile, command, concurrency, fail_mode, status,
  created_at_ns, updated_at_ns, completed_at_ns,
  created_by, host, pid, ci_run_url, git_author, kubeconfig, kube_context,
  selector_json, plan_json, summary_json, last_event_digest, run_digest
FROM ktl_stack_runs WHERE run_id = ?
`, runID).Scan(&stackRoot, &stackName, &profile, &command, &concurrency, &failMode, &status,
		&createdAtNS, &updatedAtNS, &completedAtNS,
		&createdBy, &host, &pid, &ciURL, &gitAuthor, &kubeconfig, &kubeContext,
		&selectorJSON, &planJSON, &summaryJSON, &lastEventDigest, &runDigest)
	if err != nil {
		return err
	}
	_, err = dst.ExecContext(ctx, `
INSERT INTO ktl_stack_runs (
  run_id, stack_root, stack_name, profile, command, concurrency, fail_mode, status,
  created_at_ns, updated_at_ns, completed_at_ns,
  created_by, host, pid, ci_run_url, git_author, kubeconfig, kube_context,
  selector_json, plan_json, summary_json, last_event_digest, run_digest
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, stackRoot, stackName, profile, command, concurrency, failMode, status,
		createdAtNS, updatedAtNS, completedAtNS,
		createdBy, host, pid, ciURL, gitAuthor, kubeconfig, kubeContext,
		selectorJSON, planJSON, summaryJSON, lastEventDigest, runDigest)
	return err
}

func copyRunNodes(ctx context.Context, src *sql.DB, dst *sql.DB, runID string) error {
	rows, err := src.QueryContext(ctx, `
SELECT node_id, status, attempt, error, last_error_class, last_error_digest, updated_at_ns
FROM ktl_stack_nodes WHERE run_id = ?
ORDER BY node_id ASC
`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var nodeID, status, nodeErr, lastClass, lastDigest string
		var attempt int
		var updatedAt int64
		if err := rows.Scan(&nodeID, &status, &attempt, &nodeErr, &lastClass, &lastDigest, &updatedAt); err != nil {
			return err
		}
		_, err := dst.ExecContext(ctx, `
INSERT INTO ktl_stack_nodes (run_id, node_id, status, attempt, error, last_error_class, last_error_digest, updated_at_ns)
VALUES (?, ?, ?, ?, ?, ?, ?, ?)
`, runID, nodeID, status, attempt, nodeErr, lastClass, lastDigest, updatedAt)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func copyRunEvents(ctx context.Context, src *sql.DB, dst *sql.DB, runID string) error {
	rows, err := src.QueryContext(ctx, `
SELECT ts_ns, node_id, type, attempt, message, error_class, error_message, error_digest, seq, prev_digest, digest, crc32
FROM ktl_stack_events
WHERE run_id = ?
ORDER BY id ASC
`, runID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var tsNS int64
		var nodeID, typ, msg, errClass, errMsg, errDigest, prevDigest, digest, crc32 string
		var attempt int
		var seq int64
		if err := rows.Scan(&tsNS, &nodeID, &typ, &attempt, &msg, &errClass, &errMsg, &errDigest, &seq, &prevDigest, &digest, &crc32); err != nil {
			return err
		}
		_, err := dst.ExecContext(ctx, `
INSERT INTO ktl_stack_events (run_id, ts_ns, node_id, type, attempt, message, error_class, error_message, error_digest, seq, prev_digest, digest, crc32)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`, runID, tsNS, nodeID, typ, attempt, msg, errClass, errMsg, errDigest, seq, prevDigest, digest, crc32)
		if err != nil {
			return err
		}
	}
	return rows.Err()
}

func ExtractBundleToTempDir(bundlePath string) (string, error) {
	// Uses `tar` for simplicity and speed; bundle format is controlled by ktl.
	bundlePath = strings.TrimSpace(bundlePath)
	if bundlePath == "" {
		return "", fmt.Errorf("bundle path is required")
	}
	if _, err := os.Stat(bundlePath); err != nil {
		return "", err
	}
	tmp, err := os.MkdirTemp("", "ktl-bundle-*")
	if err != nil {
		return "", err
	}
	// We'll create a synthetic root containing extracted files.
	// Implementation uses Go's tar reader in sign/verify; for apply we can use same.
	if err := extractTarGz(bundlePath, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return "", err
	}
	return tmp, nil
}

func canonicalizeJSON(raw []byte) ([]byte, error) {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return nil, err
	}
	return json.Marshal(v)
}
