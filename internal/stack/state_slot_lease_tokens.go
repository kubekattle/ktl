package stack

import (
	"context"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/ops/slotledger"
)

type StackSlotLeaseToken struct {
	RunID          string `json:"runId"`
	NodeID         string `json:"nodeId"`
	TargetID       string `json:"targetId"`
	LeaseID        string `json:"leaseId"`
	Tenant         string `json:"tenant,omitempty"`
	Token          string `json:"-"`
	TokenDigest    string `json:"tokenDigest,omitempty"`
	LedgerStore    string `json:"ledgerStore,omitempty"`
	LedgerStoreKey string `json:"ledgerStoreKey,omitempty"`
	Status         string `json:"status,omitempty"`
	AcquiredAt     string `json:"acquiredAt,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	ReleasedAt     string `json:"releasedAt,omitempty"`
	UpdatedAt      string `json:"updatedAt,omitempty"`
}

func (s *stackStateStore) UpsertSlotLeaseToken(ctx context.Context, token StackSlotLeaseToken) error {
	if s == nil || s.db == nil || s.readOnly {
		return nil
	}
	runID := strings.TrimSpace(token.RunID)
	nodeID := strings.TrimSpace(token.NodeID)
	targetID := strings.TrimSpace(token.TargetID)
	leaseID := strings.TrimSpace(token.LeaseID)
	if runID == "" || nodeID == "" || targetID == "" || leaseID == "" {
		return nil
	}
	status := strings.TrimSpace(token.Status)
	if status == "" {
		status = slotledger.StatusHeld
	}
	rawToken := strings.TrimSpace(token.Token)
	tokenDigest := strings.TrimSpace(token.TokenDigest)
	if tokenDigest == "" {
		tokenDigest = slotledger.TokenDigest(rawToken)
	}
	updatedAt := strings.TrimSpace(token.UpdatedAt)
	if updatedAt == "" {
		updatedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if status == slotledger.StatusReleased {
		rawToken = ""
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO torque_stack_slot_lease_tokens (
  run_id, node_id, target_id, lease_id, tenant, token, token_digest,
  ledger_store, ledger_store_key, status, acquired_at_ns, expires_at_ns,
  released_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, node_id, target_id, lease_id) DO UPDATE SET
  tenant = CASE WHEN excluded.tenant != '' THEN excluded.tenant ELSE torque_stack_slot_lease_tokens.tenant END,
  token = excluded.token,
  token_digest = CASE WHEN excluded.token_digest != '' THEN excluded.token_digest ELSE torque_stack_slot_lease_tokens.token_digest END,
  ledger_store = CASE WHEN excluded.ledger_store != '' THEN excluded.ledger_store ELSE torque_stack_slot_lease_tokens.ledger_store END,
  ledger_store_key = CASE WHEN excluded.ledger_store_key != '' THEN excluded.ledger_store_key ELSE torque_stack_slot_lease_tokens.ledger_store_key END,
  status = CASE WHEN excluded.status != '' THEN excluded.status ELSE torque_stack_slot_lease_tokens.status END,
  acquired_at_ns = CASE WHEN excluded.acquired_at_ns > 0 THEN excluded.acquired_at_ns ELSE torque_stack_slot_lease_tokens.acquired_at_ns END,
  expires_at_ns = CASE WHEN excluded.expires_at_ns > 0 THEN excluded.expires_at_ns ELSE torque_stack_slot_lease_tokens.expires_at_ns END,
  released_at_ns = CASE WHEN excluded.released_at_ns > 0 THEN excluded.released_at_ns ELSE torque_stack_slot_lease_tokens.released_at_ns END,
  updated_at_ns = CASE WHEN excluded.updated_at_ns > torque_stack_slot_lease_tokens.updated_at_ns THEN excluded.updated_at_ns ELSE torque_stack_slot_lease_tokens.updated_at_ns END
`, runID, nodeID, targetID, leaseID,
		strings.TrimSpace(token.Tenant), rawToken, tokenDigest,
		strings.TrimSpace(token.LedgerStore), strings.TrimSpace(token.LedgerStoreKey), status,
		parseRFC3339NanoToNS(token.AcquiredAt), parseRFC3339NanoToNS(token.ExpiresAt),
		parseRFC3339NanoToNS(token.ReleasedAt), parseRFC3339NanoToNS(updatedAt))
	return err
}

func (s *stackStateStore) ListSlotLeaseTokens(ctx context.Context, runID string, nodeID string) ([]StackSlotLeaseToken, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	runID = strings.TrimSpace(runID)
	nodeID = strings.TrimSpace(nodeID)
	if runID == "" {
		return nil, nil
	}
	query := `
SELECT run_id, node_id, target_id, lease_id, tenant, token, token_digest,
  ledger_store, ledger_store_key, status, acquired_at_ns, expires_at_ns,
  released_at_ns, updated_at_ns
FROM torque_stack_slot_lease_tokens
WHERE run_id = ?
`
	args := []any{runID}
	if nodeID != "" {
		query += ` AND node_id = ?`
		args = append(args, nodeID)
	}
	query += ` ORDER BY node_id ASC, target_id ASC, lease_id ASC`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []StackSlotLeaseToken
	for rows.Next() {
		var token StackSlotLeaseToken
		var acquiredNS, expiresNS, releasedNS, updatedNS int64
		if err := rows.Scan(
			&token.RunID,
			&token.NodeID,
			&token.TargetID,
			&token.LeaseID,
			&token.Tenant,
			&token.Token,
			&token.TokenDigest,
			&token.LedgerStore,
			&token.LedgerStoreKey,
			&token.Status,
			&acquiredNS,
			&expiresNS,
			&releasedNS,
			&updatedNS,
		); err != nil {
			return nil, err
		}
		token.AcquiredAt = nsToRFC3339Nano(acquiredNS)
		token.ExpiresAt = nsToRFC3339Nano(expiresNS)
		token.ReleasedAt = nsToRFC3339Nano(releasedNS)
		token.UpdatedAt = nsToRFC3339Nano(updatedNS)
		out = append(out, token)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}
