package worker

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	_ "modernc.org/sqlite"
)

const (
	ledgerStatusReceived         = "received"
	ledgerStatusRunning          = "running"
	ledgerStatusRetrying         = "retrying"
	ledgerStatusSucceeded        = "succeeded"
	ledgerStatusFailed           = "failed"
	ledgerStatusBlocked          = "blocked"
	ledgerStatusTimeout          = "timeout"
	ledgerStatusReceiptPublished = "receiptPublished"
)

type assignmentLedger struct {
	db *sql.DB
}

type ledgerDecision struct {
	Replay       bool
	UnsafeReplay bool
	Receipt      transport.OperationResult
	Status       string
	Attempt      int
}

func openAssignmentLedger(ctx context.Context, path string) (*assignmentLedger, error) {
	path = defaultLedgerPath(path)
	if strings.TrimSpace(path) == "" {
		return nil, fmt.Errorf("assignment ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create assignment ledger directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open assignment ledger: %w", err)
	}
	ledger := &assignmentLedger{db: db}
	if err := ledger.init(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return ledger, nil
}

func defaultLedgerPath(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		return path
	}
	if os.Getuid() == 0 {
		return "/var/lib/torque/agent/assignments.sqlite"
	}
	if home, err := os.UserHomeDir(); err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, ".torque", "agent", "assignments.sqlite")
	}
	return filepath.Join(os.TempDir(), "torque-agent", "assignments.sqlite")
}

func (l *assignmentLedger) Close() error {
	if l == nil || l.db == nil {
		return nil
	}
	return l.db.Close()
}

func (l *assignmentLedger) init(ctx context.Context) error {
	_, err := l.db.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS nats_assignment_ledger (
  assignment_id TEXT PRIMARY KEY,
  status TEXT NOT NULL,
  result_status TEXT,
  assignment_json TEXT NOT NULL,
  receipt_json TEXT,
  attempts INTEGER NOT NULL DEFAULT 0,
  received_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  receipt_published_at TEXT,
  last_error TEXT
);
CREATE INDEX IF NOT EXISTS idx_nats_assignment_ledger_status ON nats_assignment_ledger(status);
`)
	if err != nil {
		return fmt.Errorf("init assignment ledger: %w", err)
	}
	return nil
}

func (l *assignmentLedger) Begin(ctx context.Context, assignment natstransport.CommandAssignment) (ledgerDecision, error) {
	if l == nil || l.db == nil {
		return ledgerDecision{Attempt: 1}, nil
	}
	assignmentID := strings.TrimSpace(assignment.AssignmentID)
	if assignmentID == "" {
		return ledgerDecision{}, fmt.Errorf("assignmentId is required for durable execution")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	rawAssignment, err := json.Marshal(assignment)
	if err != nil {
		return ledgerDecision{}, fmt.Errorf("marshal assignment for ledger: %w", err)
	}
	tx, err := l.db.BeginTx(ctx, nil)
	if err != nil {
		return ledgerDecision{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	row := tx.QueryRowContext(ctx, `SELECT status, receipt_json, attempts FROM nats_assignment_ledger WHERE assignment_id = ?`, assignmentID)
	var status string
	var receiptJSON sql.NullString
	var attempts int
	switch err := row.Scan(&status, &receiptJSON, &attempts); {
	case err == nil:
		if receiptJSON.Valid && strings.TrimSpace(receiptJSON.String) != "" && terminalLedgerStatus(status) {
			var receipt transport.OperationResult
			if err := json.Unmarshal([]byte(receiptJSON.String), &receipt); err != nil {
				return ledgerDecision{}, fmt.Errorf("parse stored assignment receipt: %w", err)
			}
			if err := tx.Commit(); err != nil {
				return ledgerDecision{}, err
			}
			tx = nil
			return ledgerDecision{Replay: true, Receipt: receipt, Status: status, Attempt: attempts}, nil
		}
		if status == ledgerStatusRunning {
			if err := tx.Commit(); err != nil {
				return ledgerDecision{}, err
			}
			tx = nil
			return ledgerDecision{UnsafeReplay: true, Status: status, Attempt: attempts}, nil
		}
	case errors.Is(err, sql.ErrNoRows):
		if _, err := tx.ExecContext(ctx, `
INSERT INTO nats_assignment_ledger (assignment_id, status, assignment_json, attempts, received_at, updated_at)
VALUES (?, ?, ?, 0, ?, ?)`, assignmentID, ledgerStatusReceived, string(rawAssignment), now, now); err != nil {
			return ledgerDecision{}, err
		}
	default:
		return ledgerDecision{}, err
	}
	attempts++
	if _, err := tx.ExecContext(ctx, `
UPDATE nats_assignment_ledger
SET status = ?, assignment_json = ?, attempts = ?, updated_at = ?, last_error = NULL
WHERE assignment_id = ?`, ledgerStatusRunning, string(rawAssignment), attempts, now, assignmentID); err != nil {
		return ledgerDecision{}, err
	}
	if err := tx.Commit(); err != nil {
		return ledgerDecision{}, err
	}
	tx = nil
	return ledgerDecision{Status: ledgerStatusRunning, Attempt: attempts}, nil
}

func (l *assignmentLedger) SaveReceipt(ctx context.Context, assignmentID string, receipt transport.OperationResult) error {
	if l == nil || l.db == nil {
		return nil
	}
	assignmentID = strings.TrimSpace(assignmentID)
	if assignmentID == "" {
		return fmt.Errorf("assignmentId is required for ledger receipt")
	}
	rawReceipt, err := json.Marshal(receipt)
	if err != nil {
		return fmt.Errorf("marshal assignment receipt for ledger: %w", err)
	}
	status := ledgerStatusForReceipt(receipt)
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err = l.db.ExecContext(ctx, `
UPDATE nats_assignment_ledger
SET status = ?, result_status = ?, receipt_json = ?, updated_at = ?, last_error = ?
WHERE assignment_id = ?`, status, strings.TrimSpace(receipt.Status), string(rawReceipt), now, strings.TrimSpace(receipt.Error), assignmentID)
	if err != nil {
		return fmt.Errorf("save assignment receipt to ledger: %w", err)
	}
	return nil
}

func (l *assignmentLedger) MarkRetry(ctx context.Context, assignmentID string, receipt transport.OperationResult) error {
	if l == nil || l.db == nil {
		return nil
	}
	assignmentID = strings.TrimSpace(assignmentID)
	if assignmentID == "" {
		return fmt.Errorf("assignmentId is required for ledger retry mark")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := l.db.ExecContext(ctx, `
UPDATE nats_assignment_ledger
SET status = ?, result_status = ?, receipt_json = NULL, updated_at = ?, last_error = ?
WHERE assignment_id = ?`, ledgerStatusRetrying, strings.TrimSpace(receipt.Status), now, strings.TrimSpace(receipt.Error), assignmentID)
	if err != nil {
		return fmt.Errorf("mark assignment retry in ledger: %w", err)
	}
	return nil
}

func (l *assignmentLedger) MarkReceiptPublished(ctx context.Context, assignmentID string) error {
	if l == nil || l.db == nil {
		return nil
	}
	assignmentID = strings.TrimSpace(assignmentID)
	if assignmentID == "" {
		return fmt.Errorf("assignmentId is required for ledger publish mark")
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := l.db.ExecContext(ctx, `
UPDATE nats_assignment_ledger
SET status = ?, receipt_published_at = ?, updated_at = ?
WHERE assignment_id = ?`, ledgerStatusReceiptPublished, now, now, assignmentID)
	if err != nil {
		return fmt.Errorf("mark assignment receipt published: %w", err)
	}
	return nil
}

func terminalLedgerStatus(status string) bool {
	switch strings.TrimSpace(status) {
	case ledgerStatusSucceeded, ledgerStatusFailed, ledgerStatusBlocked, ledgerStatusTimeout, ledgerStatusReceiptPublished:
		return true
	default:
		return false
	}
}

func ledgerStatusForReceipt(receipt transport.OperationResult) string {
	switch strings.ToLower(strings.TrimSpace(receipt.Status)) {
	case "succeeded", "success", "skipped":
		return ledgerStatusSucceeded
	case "blocked":
		return ledgerStatusBlocked
	case "timeout":
		return ledgerStatusTimeout
	default:
		return ledgerStatusFailed
	}
}
