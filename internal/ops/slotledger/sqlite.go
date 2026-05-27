package slotledger

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

type SQLiteStore struct {
	db   *sql.DB
	path string
}

func NewSQLiteStore(ctx context.Context, path string) (*SQLiteStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("slot ledger path is required")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create slot ledger directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open slot ledger: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	store := &SQLiteStore{db: db, path: path}
	if err := store.init(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return store, nil
}

func (s *SQLiteStore) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *SQLiteStore) init(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
PRAGMA busy_timeout = 5000;
PRAGMA journal_mode = WAL;
PRAGMA synchronous = NORMAL;
CREATE TABLE IF NOT EXISTS target_slot_leases (
  lease_id TEXT PRIMARY KEY,
  tenant TEXT NOT NULL,
  target_id TEXT NOT NULL,
  slot_index INTEGER NOT NULL,
  slots INTEGER NOT NULL,
  max_slots INTEGER NOT NULL,
  holder TEXT NOT NULL,
  run_id TEXT,
  node_id TEXT,
  token_digest TEXT NOT NULL,
  status TEXT NOT NULL,
  acquired_at TEXT NOT NULL,
  expires_at TEXT NOT NULL,
  released_at TEXT,
  updated_at TEXT NOT NULL,
  store TEXT,
  store_key TEXT,
  store_scope TEXT,
  metadata_json TEXT
);
CREATE INDEX IF NOT EXISTS idx_target_slot_leases_target_status ON target_slot_leases(tenant, target_id, status);
CREATE UNIQUE INDEX IF NOT EXISTS idx_target_slot_leases_held_slot ON target_slot_leases(tenant, target_id, slot_index) WHERE status = 'held';
`)
	if err != nil {
		return fmt.Errorf("init slot ledger: %w", err)
	}
	return nil
}

func (s *SQLiteStore) Reserve(ctx context.Context, req ReserveRequest) (ReserveResult, error) {
	if s == nil || s.db == nil {
		return ReserveResult{}, fmt.Errorf("slot ledger is not open")
	}
	req.Store = firstNonEmpty(req.Store, StoreFile)
	req.StoreScope = firstNonEmpty(req.StoreScope, s.path)
	normalized, err := req.normalized(time.Now())
	if err != nil {
		return ReserveResult{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return ReserveResult{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	reclaimed, err := sqliteExpireHeld(ctx, tx, normalized)
	if err != nil {
		return ReserveResult{}, err
	}
	existing, err := sqliteListHeld(ctx, tx, normalized.Tenant, normalized.TargetID)
	if err != nil {
		return ReserveResult{}, err
	}
	heldSlots := map[int]struct{}{}
	for _, record := range existing {
		heldSlots[record.SlotIndex] = struct{}{}
	}
	capacity := normalized.MaxSlots
	available := capacity - len(existing)
	if available < 0 {
		available = 0
	}
	result := ReserveResult{
		APIVersion: APIVersion,
		Kind:       ReserveResultKind,
		Capacity:   capacity,
		Held:       len(existing),
		Available:  available,
		Existing:   existing,
		Reclaimed:  reclaimed,
	}
	if len(existing) >= capacity {
		result.Decision = "blocked"
		result.Reason = fmt.Sprintf("target %s has no available ledger slots (held=%d capacity=%d)", normalized.TargetID, len(existing), capacity)
		if err := tx.Commit(); err != nil {
			return ReserveResult{}, err
		}
		tx = nil
		return result, nil
	}
	slotIndex := normalized.SlotIndex
	if slotIndex < 1 {
		for idx := 1; idx <= capacity; idx++ {
			if _, ok := heldSlots[idx]; !ok {
				slotIndex = idx
				break
			}
		}
	}
	if slotIndex < 1 || slotIndex > capacity {
		return ReserveResult{}, fmt.Errorf("slot index %d is outside capacity %d", slotIndex, capacity)
	}
	storeKey := sqliteStoreKey(normalized.Tenant, normalized.TargetID, normalized.LeaseID)
	lease := NewLeaseRecord(normalized, slotIndex, storeKey)
	metadataJSON, err := MarshalMetadata(lease.Metadata)
	if err != nil {
		return ReserveResult{}, err
	}
	insert, err := tx.ExecContext(ctx, `
INSERT OR IGNORE INTO target_slot_leases (
  lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id, node_id,
  token_digest, status, acquired_at, expires_at, released_at, updated_at, store,
  store_key, store_scope, metadata_json
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		lease.LeaseID,
		lease.Tenant,
		lease.TargetID,
		lease.SlotIndex,
		lease.Slots,
		lease.MaxSlots,
		lease.Holder,
		lease.RunID,
		lease.NodeID,
		lease.TokenDigest,
		lease.Status,
		lease.AcquiredAt,
		lease.ExpiresAt,
		lease.ReleasedAt,
		lease.UpdatedAt,
		lease.Store,
		lease.StoreKey,
		lease.StoreScope,
		metadataJSON,
	)
	if err != nil {
		return ReserveResult{}, fmt.Errorf("insert slot lease: %w", err)
	}
	inserted, _ := insert.RowsAffected()
	if inserted == 0 {
		existing, err = sqliteListHeld(ctx, tx, normalized.Tenant, normalized.TargetID)
		if err != nil {
			return ReserveResult{}, err
		}
		result.Decision = "blocked"
		result.Reason = fmt.Sprintf("target %s has no available ledger slots (held=%d capacity=%d)", normalized.TargetID, len(existing), capacity)
		result.Existing = existing
		result.Held = len(existing)
		result.Available = capacity - result.Held
		if result.Available < 0 {
			result.Available = 0
		}
		if err := tx.Commit(); err != nil {
			return ReserveResult{}, err
		}
		tx = nil
		return result, nil
	}
	if err := tx.Commit(); err != nil {
		return ReserveResult{}, err
	}
	tx = nil
	result.Decision = "acquired"
	result.Reason = "slot lease acquired"
	result.Lease = &lease
	result.Held++
	result.Available = capacity - result.Held
	if result.Available < 0 {
		result.Available = 0
	}
	return result, nil
}

func (s *SQLiteStore) Release(ctx context.Context, req ReleaseRequest) (LeaseRecord, error) {
	if s == nil || s.db == nil {
		return LeaseRecord{}, fmt.Errorf("slot ledger is not open")
	}
	normalized, err := req.normalized(time.Now())
	if err != nil {
		return LeaseRecord{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return LeaseRecord{}, err
	}
	defer func() {
		if tx != nil {
			_ = tx.Rollback()
		}
	}()
	record, found, err := sqliteGetByLeaseID(ctx, tx, normalized.LeaseID)
	if err != nil {
		return LeaseRecord{}, err
	}
	if !found {
		return LeaseRecord{}, fmt.Errorf("slot lease %q was not found", normalized.LeaseID)
	}
	if record.Tenant != normalized.Tenant || record.TargetID != normalized.TargetID {
		return LeaseRecord{}, fmt.Errorf("slot lease %q target mismatch", normalized.LeaseID)
	}
	if record.TokenDigest != TokenDigest(normalized.Token) {
		return LeaseRecord{}, fmt.Errorf("slot lease %q token mismatch", normalized.LeaseID)
	}
	if record.Status == StatusReleased {
		if err := tx.Commit(); err != nil {
			return LeaseRecord{}, err
		}
		tx = nil
		return record, nil
	}
	now := normalized.Now.UTC().Format(time.RFC3339Nano)
	if _, err := tx.ExecContext(ctx, `
UPDATE target_slot_leases
SET status = ?, released_at = ?, updated_at = ?
WHERE lease_id = ?`, StatusReleased, now, now, normalized.LeaseID); err != nil {
		return LeaseRecord{}, fmt.Errorf("release slot lease: %w", err)
	}
	record.Status = StatusReleased
	record.ReleasedAt = now
	record.UpdatedAt = now
	if err := tx.Commit(); err != nil {
		return LeaseRecord{}, err
	}
	tx = nil
	return record, nil
}

func (s *SQLiteStore) List(ctx context.Context, req ListRequest) ([]LeaseRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("slot ledger is not open")
	}
	req = req.normalized()
	query := `SELECT lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id, node_id, token_digest, status, acquired_at, expires_at, released_at, updated_at, store, store_key, store_scope, metadata_json FROM target_slot_leases WHERE tenant = ?`
	args := []any{req.Tenant}
	if req.TargetID != "" {
		query += ` AND target_id = ?`
		args = append(args, req.TargetID)
	}
	if req.Include == "" || req.Include == StatusHeld {
		query += ` AND status = ?`
		args = append(args, StatusHeld)
	} else if req.Include != "all" {
		query += ` AND status = ?`
		args = append(args, req.Include)
	}
	query += ` ORDER BY tenant, target_id, slot_index, lease_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseRecord
	for rows.Next() {
		record, err := scanSQLiteLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func sqliteExpireHeld(ctx context.Context, tx *sql.Tx, req ReserveRequest) ([]LeaseRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id, node_id, token_digest, status, acquired_at, expires_at, released_at, updated_at, store, store_key, store_scope, metadata_json
FROM target_slot_leases
WHERE tenant = ? AND target_id = ? AND status = ? AND expires_at <= ?
ORDER BY slot_index, lease_id`, req.Tenant, req.TargetID, StatusHeld, req.Now.UTC().Format(time.RFC3339Nano))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var expired []LeaseRecord
	for rows.Next() {
		record, err := scanSQLiteLease(rows)
		if err != nil {
			return nil, err
		}
		record.Status = StatusExpired
		record.UpdatedAt = req.Now.UTC().Format(time.RFC3339Nano)
		expired = append(expired, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, record := range expired {
		if _, err := tx.ExecContext(ctx, `
UPDATE target_slot_leases SET status = ?, updated_at = ? WHERE lease_id = ?`,
			StatusExpired, req.Now.UTC().Format(time.RFC3339Nano), record.LeaseID); err != nil {
			return nil, err
		}
	}
	return expired, nil
}

func sqliteListHeld(ctx context.Context, tx *sql.Tx, tenant string, targetID string) ([]LeaseRecord, error) {
	rows, err := tx.QueryContext(ctx, `
SELECT lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id, node_id, token_digest, status, acquired_at, expires_at, released_at, updated_at, store, store_key, store_scope, metadata_json
FROM target_slot_leases
WHERE tenant = ? AND target_id = ? AND status = ?
ORDER BY slot_index, lease_id`, tenant, targetID, StatusHeld)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LeaseRecord
	for rows.Next() {
		record, err := scanSQLiteLease(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, record)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func sqliteGetByLeaseID(ctx context.Context, tx *sql.Tx, leaseID string) (LeaseRecord, bool, error) {
	row := tx.QueryRowContext(ctx, `
SELECT lease_id, tenant, target_id, slot_index, slots, max_slots, holder, run_id, node_id, token_digest, status, acquired_at, expires_at, released_at, updated_at, store, store_key, store_scope, metadata_json
FROM target_slot_leases
WHERE lease_id = ?`, strings.TrimSpace(leaseID))
	record, err := scanSQLiteLease(row)
	if errors.Is(err, sql.ErrNoRows) {
		return LeaseRecord{}, false, nil
	}
	if err != nil {
		return LeaseRecord{}, false, err
	}
	return record, true, nil
}

type sqliteScanner interface {
	Scan(dest ...any) error
}

func scanSQLiteLease(row sqliteScanner) (LeaseRecord, error) {
	var record LeaseRecord
	var releasedAt sql.NullString
	var metadataJSON sql.NullString
	err := row.Scan(
		&record.LeaseID,
		&record.Tenant,
		&record.TargetID,
		&record.SlotIndex,
		&record.Slots,
		&record.MaxSlots,
		&record.Holder,
		&record.RunID,
		&record.NodeID,
		&record.TokenDigest,
		&record.Status,
		&record.AcquiredAt,
		&record.ExpiresAt,
		&releasedAt,
		&record.UpdatedAt,
		&record.Store,
		&record.StoreKey,
		&record.StoreScope,
		&metadataJSON,
	)
	if err != nil {
		return LeaseRecord{}, err
	}
	record.APIVersion = APIVersion
	record.Kind = LeaseKind
	if releasedAt.Valid {
		record.ReleasedAt = releasedAt.String
	}
	if metadataJSON.Valid {
		record.Metadata = UnmarshalMetadata(metadataJSON.String)
	}
	return record, nil
}

func sqliteStoreKey(tenant string, targetID string, leaseID string) string {
	return "sqlite://" + strings.Join([]string{
		NormalizeTenant(tenant),
		EncodeKeyPart(targetID),
		EncodeKeyPart(leaseID),
	}, "/")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
