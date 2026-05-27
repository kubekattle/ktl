package stack

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
)

type StackReceiptOffsetCheckpoint struct {
	RunID         string                      `json:"runId"`
	ReceiptRunID  string                      `json:"receiptRunId,omitempty"`
	NodeID        string                      `json:"nodeId"`
	TargetID      string                      `json:"targetId"`
	AssignmentID  string                      `json:"assignmentId"`
	AgentID       string                      `json:"agentId,omitempty"`
	WorkerSubject string                      `json:"workerSubject,omitempty"`
	Offset        *natstransport.StreamOffset `json:"offset,omitempty"`
	LastSeenAt    string                      `json:"lastSeenAt"`
	Receipt       transport.OperationResult   `json:"receipt"`
	ReceiptDigest string                      `json:"receiptDigest,omitempty"`
}

func (s *stackStateStore) UpsertReceiptOffset(ctx context.Context, checkpoint StackReceiptOffsetCheckpoint) error {
	if s == nil || s.db == nil || s.readOnly {
		return nil
	}
	runID := strings.TrimSpace(checkpoint.RunID)
	nodeID := strings.TrimSpace(checkpoint.NodeID)
	targetID := strings.TrimSpace(checkpoint.TargetID)
	assignmentID := strings.TrimSpace(checkpoint.AssignmentID)
	if assignmentID == "" {
		assignmentID = strings.TrimSpace(checkpoint.Receipt.Metadata["assignmentId"])
	}
	if targetID == "" {
		targetID = firstNonEmptyString(checkpoint.Receipt.Metadata["assignmentTargetId"], checkpoint.Receipt.Metadata["targetId"])
	}
	if runID == "" || nodeID == "" || targetID == "" || assignmentID == "" {
		return nil
	}
	receiptRunID := firstNonEmptyString(checkpoint.ReceiptRunID, checkpoint.Receipt.Metadata["runId"], runID)
	agentID := firstNonEmptyString(checkpoint.AgentID, checkpoint.Receipt.Metadata["agentId"])
	workerSubject := firstNonEmptyString(checkpoint.WorkerSubject, checkpoint.Receipt.Metadata["workerSubject"])
	offset := checkpoint.Offset
	stream := ""
	subject := ""
	consumer := ""
	var sequence, delivered, pending uint64
	receivedAt := ""
	if offset != nil {
		stream = strings.TrimSpace(offset.Stream)
		subject = strings.TrimSpace(offset.Subject)
		consumer = strings.TrimSpace(offset.Consumer)
		sequence = offset.Sequence
		delivered = offset.NumDelivered
		pending = offset.NumPending
		receivedAt = strings.TrimSpace(offset.ReceivedAt)
	}
	if stream == "" {
		stream = strings.TrimSpace(checkpoint.Receipt.Metadata["receiptStream"])
	}
	if subject == "" {
		subject = strings.TrimSpace(checkpoint.Receipt.Metadata["receiptSubject"])
	}
	if receivedAt == "" {
		receivedAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	lastSeenAt := strings.TrimSpace(checkpoint.LastSeenAt)
	if lastSeenAt == "" {
		lastSeenAt = time.Now().UTC().Format(time.RFC3339Nano)
	}
	raw, err := json.Marshal(checkpoint.Receipt)
	if err != nil {
		return err
	}
	receiptDigest := "sha256:" + hashBytes(raw)
	if strings.TrimSpace(checkpoint.ReceiptDigest) != "" {
		receiptDigest = strings.TrimSpace(checkpoint.ReceiptDigest)
	}
	_, err = s.db.ExecContext(ctx, `
INSERT INTO torque_stack_receipt_offsets (
  run_id, receipt_run_id, node_id, target_id, assignment_id,
  agent_id, worker_subject, receipt_stream, subject, consumer,
  sequence, num_delivered, num_pending, received_at_ns, last_seen_at_ns,
  receipt_json, receipt_sha256
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(run_id, node_id, target_id, assignment_id) DO UPDATE SET
  receipt_run_id = CASE WHEN excluded.receipt_run_id != '' THEN excluded.receipt_run_id ELSE torque_stack_receipt_offsets.receipt_run_id END,
  agent_id = CASE WHEN excluded.agent_id != '' THEN excluded.agent_id ELSE torque_stack_receipt_offsets.agent_id END,
  worker_subject = CASE WHEN excluded.worker_subject != '' THEN excluded.worker_subject ELSE torque_stack_receipt_offsets.worker_subject END,
  receipt_stream = CASE WHEN excluded.receipt_stream != '' THEN excluded.receipt_stream ELSE torque_stack_receipt_offsets.receipt_stream END,
  subject = CASE WHEN excluded.subject != '' THEN excluded.subject ELSE torque_stack_receipt_offsets.subject END,
  consumer = CASE WHEN excluded.consumer != '' THEN excluded.consumer ELSE torque_stack_receipt_offsets.consumer END,
  sequence = CASE WHEN excluded.sequence > 0 THEN excluded.sequence ELSE torque_stack_receipt_offsets.sequence END,
  num_delivered = CASE WHEN excluded.num_delivered > 0 THEN excluded.num_delivered ELSE torque_stack_receipt_offsets.num_delivered END,
  num_pending = excluded.num_pending,
  received_at_ns = CASE WHEN excluded.received_at_ns > 0 THEN excluded.received_at_ns ELSE torque_stack_receipt_offsets.received_at_ns END,
  last_seen_at_ns = CASE WHEN excluded.last_seen_at_ns > torque_stack_receipt_offsets.last_seen_at_ns THEN excluded.last_seen_at_ns ELSE torque_stack_receipt_offsets.last_seen_at_ns END,
  receipt_json = CASE WHEN excluded.receipt_json != '' THEN excluded.receipt_json ELSE torque_stack_receipt_offsets.receipt_json END,
  receipt_sha256 = CASE WHEN excluded.receipt_sha256 != '' THEN excluded.receipt_sha256 ELSE torque_stack_receipt_offsets.receipt_sha256 END
`, runID, receiptRunID, nodeID, targetID, assignmentID,
		agentID, workerSubject, stream, subject, consumer,
		uint64ToSQLiteInt(sequence), uint64ToSQLiteInt(delivered), uint64ToSQLiteInt(pending),
		parseRFC3339NanoToNS(receivedAt), parseRFC3339NanoToNS(lastSeenAt),
		string(raw), receiptDigest)
	return err
}

func (s *stackStateStore) ListReceiptOffsets(ctx context.Context, runID string, nodeID string) ([]StackReceiptOffsetCheckpoint, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	runID = strings.TrimSpace(runID)
	nodeID = strings.TrimSpace(nodeID)
	if runID == "" {
		return nil, nil
	}
	query := `
SELECT
  run_id, receipt_run_id, node_id, target_id, assignment_id,
  agent_id, worker_subject, receipt_stream, subject, consumer,
  sequence, num_delivered, num_pending, received_at_ns, last_seen_at_ns,
  receipt_json, receipt_sha256
FROM torque_stack_receipt_offsets
WHERE run_id = ?
`
	args := []any{runID}
	if nodeID != "" {
		query += ` AND node_id = ?`
		args = append(args, nodeID)
	}
	query += `
ORDER BY node_id ASC, target_id ASC, sequence ASC
`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "no such table") {
			return nil, nil
		}
		return nil, err
	}
	defer rows.Close()
	var out []StackReceiptOffsetCheckpoint
	for rows.Next() {
		var checkpoint StackReceiptOffsetCheckpoint
		var stream, subject, consumer, receiptJSON string
		var sequence, delivered, pending, receivedNS, seenNS int64
		if err := rows.Scan(
			&checkpoint.RunID,
			&checkpoint.ReceiptRunID,
			&checkpoint.NodeID,
			&checkpoint.TargetID,
			&checkpoint.AssignmentID,
			&checkpoint.AgentID,
			&checkpoint.WorkerSubject,
			&stream,
			&subject,
			&consumer,
			&sequence,
			&delivered,
			&pending,
			&receivedNS,
			&seenNS,
			&receiptJSON,
			&checkpoint.ReceiptDigest,
		); err != nil {
			return nil, err
		}
		if strings.TrimSpace(receiptJSON) != "" {
			_ = json.Unmarshal([]byte(receiptJSON), &checkpoint.Receipt)
		}
		checkpoint.Offset = &natstransport.StreamOffset{
			Stream:       strings.TrimSpace(stream),
			Consumer:     strings.TrimSpace(consumer),
			Subject:      strings.TrimSpace(subject),
			Sequence:     sqliteIntToUint64(sequence),
			NumDelivered: sqliteIntToUint64(delivered),
			NumPending:   sqliteIntToUint64(pending),
			ReceivedAt:   nsToRFC3339Nano(receivedNS),
		}
		checkpoint.LastSeenAt = nsToRFC3339Nano(seenNS)
		out = append(out, checkpoint)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return out, nil
}

func parseRFC3339NanoToNS(value string) int64 {
	value = strings.TrimSpace(value)
	if value == "" {
		return 0
	}
	ts, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return 0
	}
	return ts.UTC().UnixNano()
}

func nsToRFC3339Nano(value int64) string {
	if value <= 0 {
		return ""
	}
	return time.Unix(0, value).UTC().Format(time.RFC3339Nano)
}

func uint64ToSQLiteInt(value uint64) int64 {
	const maxInt64 = uint64(^uint64(0) >> 1)
	if value > maxInt64 {
		return int64(maxInt64)
	}
	return int64(value)
}

func sqliteIntToUint64(value int64) uint64 {
	if value <= 0 {
		return 0
	}
	return uint64(value)
}
