package stack

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type backfillState struct {
	ObjectID          string
	IntentDigest      string
	CheckpointKey     string
	StartCursor       int64
	CurrentCursor     int64
	EndCursor         int64
	BatchSize         int64
	BatchesCompleted  int
	TotalRowsAffected int64
	PhaseStatus       string
	UpdatedAtNS       int64
}

func (e *customNodeExecutor) runDBRestorePointNode(ctx context.Context, node *runNode, command string) error {
	return e.runDBStatementNode(ctx, node, command, "restore-point", strings.TrimSpace(node.Database.RestorePointSQL), "restore-point.json")
}

func (e *customNodeExecutor) runDBSchemaExpandNode(ctx context.Context, node *runNode, command string) error {
	return e.runDBStatementNode(ctx, node, command, "schema-expand", strings.TrimSpace(node.Database.ExpandSQL), "schema-expand.json")
}

func (e *customNodeExecutor) runDBVerifyNode(ctx context.Context, node *runNode, command string) error {
	if !strings.EqualFold(command, "apply") {
		return nil
	}
	db, _, err := openNodeDatabase(ctx, node)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	defer db.Close()

	phase := "verify"
	cursor := map[string]any{"phase": phase, "kind": normalizeNodeKind(node.Kind)}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	msg, err := verifyDatabaseCondition(ctx, db, node.Database)
	if err != nil {
		e.recordDBNodeArtifacts(node, "verify.json", map[string]any{
			"apiVersion": "torque.dev/db-node/v1",
			"kind":       "DBNodeArtifact",
			"nodeId":     node.ID,
			"nodeKind":   normalizeNodeKind(node.Kind),
			"driver":     strings.TrimSpace(node.Database.Driver),
			"status":     "failure",
			"phase":      phase,
			"message":    err.Error(),
		})
		return e.cutoverPhaseFailure(node, phase, cursor, "DB_VERIFY_FAILED", err)
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":   phase,
		"status":  "success",
		"message": msg,
		"cursor":  cursor,
	}, nil)
	e.recordDBNodeArtifacts(node, "verify.json", map[string]any{
		"apiVersion":    "torque.dev/db-node/v1",
		"kind":          "DBNodeArtifact",
		"nodeId":        node.ID,
		"nodeKind":      normalizeNodeKind(node.Kind),
		"driver":        strings.TrimSpace(node.Database.Driver),
		"status":        "success",
		"phase":         phase,
		"verifyMessage": msg,
	})
	return nil
}

func (e *customNodeExecutor) runDBSchemaContractNode(ctx context.Context, node *runNode, command string) error {
	return e.runDBStatementNode(ctx, node, command, "schema-contract", strings.TrimSpace(node.Database.ContractSQL), "schema-contract.json")
}

func (e *customNodeExecutor) runDBStatementNode(ctx context.Context, node *runNode, command string, phase string, sqlText string, artifactName string) error {
	if !strings.EqualFold(command, "apply") {
		return nil
	}
	db, _, err := openNodeDatabase(ctx, node)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	defer db.Close()

	cursor := map[string]any{"phase": phase, "kind": normalizeNodeKind(node.Kind)}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	rowsAffected, err := execSQLScriptWithStats(ctx, db, sqlText)
	if err != nil {
		e.recordDBNodeArtifacts(node, artifactName, map[string]any{
			"apiVersion": "torque.dev/db-node/v1",
			"kind":       "DBNodeArtifact",
			"nodeId":     node.ID,
			"nodeKind":   normalizeNodeKind(node.Kind),
			"driver":     strings.TrimSpace(node.Database.Driver),
			"status":     "failure",
			"phase":      phase,
			"message":    err.Error(),
		})
		return e.cutoverPhaseFailure(node, phase, cursor, "DB_NODE_FAILED", err)
	}

	verifyMessage := ""
	if strings.TrimSpace(node.Database.VerifySQL) != "" {
		verifyMessage, err = verifyDatabaseCondition(ctx, db, node.Database)
		if err != nil {
			e.recordDBNodeArtifacts(node, artifactName, map[string]any{
				"apiVersion": "torque.dev/db-node/v1",
				"kind":       "DBNodeArtifact",
				"nodeId":     node.ID,
				"nodeKind":   normalizeNodeKind(node.Kind),
				"driver":     strings.TrimSpace(node.Database.Driver),
				"status":     "failure",
				"phase":      phase,
				"message":    err.Error(),
			})
			return e.cutoverPhaseFailure(node, phase, cursor, "DB_NODE_VERIFY_FAILED", err)
		}
	}

	msg := "success"
	if verifyMessage != "" {
		msg = verifyMessage
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":        phase,
		"status":       "success",
		"message":      msg,
		"rowsAffected": rowsAffected,
		"cursor":       cursor,
	}, nil)
	e.recordDBNodeArtifacts(node, artifactName, map[string]any{
		"apiVersion":    "torque.dev/db-node/v1",
		"kind":          "DBNodeArtifact",
		"nodeId":        node.ID,
		"nodeKind":      normalizeNodeKind(node.Kind),
		"driver":        strings.TrimSpace(node.Database.Driver),
		"status":        "success",
		"phase":         phase,
		"rowsAffected":  rowsAffected,
		"verifyMessage": verifyMessage,
	})
	return nil
}

func (e *customNodeExecutor) runDBBackfillNode(ctx context.Context, node *runNode, command string) error {
	if !strings.EqualFold(command, "apply") {
		return nil
	}
	db, dialect, err := openNodeDatabase(ctx, node)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	defer db.Close()

	table, err := sanitizeSQLIdent(node.Database.Backfill.CheckpointTable)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("backfill checkpoint table: %w", err))
	}
	if err := ensureBackfillTable(ctx, db, dialect, table); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	intent := strings.TrimSpace(node.EffectiveInputHash)
	if intent == "" {
		intent = strings.TrimSpace(node.Name)
	}
	state, err := loadBackfillState(ctx, db, dialect, table, node.ID)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	startCursor, err := queryScalarInt64(ctx, db, node.Database.Backfill.StartSQL)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("db.backfill startSQL: %w", err))
	}
	endCursor, err := queryScalarInt64(ctx, db, node.Database.Backfill.EndSQL)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("db.backfill endSQL: %w", err))
	}
	if endCursor < startCursor {
		endCursor = startCursor
	}
	if state == nil {
		state = &backfillState{
			ObjectID:      node.ID,
			IntentDigest:  intent,
			CheckpointKey: strings.TrimSpace(node.Database.Backfill.CheckpointKey),
			StartCursor:   startCursor,
			CurrentCursor: startCursor,
			EndCursor:     endCursor,
			BatchSize:     node.Database.Backfill.BatchSize,
			PhaseStatus:   "running",
			UpdatedAtNS:   time.Now().UTC().UnixNano(),
		}
	} else {
		if strings.TrimSpace(state.IntentDigest) != "" && state.IntentDigest != intent && !backfillComplete(state) {
			return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("backfill intent changed while previous checkpoint %s is incomplete", state.CheckpointKey))
		}
		if strings.TrimSpace(state.IntentDigest) == "" {
			state.IntentDigest = intent
		}
		if state.StartCursor == 0 && startCursor != 0 {
			state.StartCursor = startCursor
		}
		if state.BatchSize == 0 {
			state.BatchSize = node.Database.Backfill.BatchSize
		}
	}
	if state.CurrentCursor < state.StartCursor {
		state.CurrentCursor = state.StartCursor
	}
	if endCursor > state.EndCursor {
		state.EndCursor = endCursor
	}
	if state.BatchSize <= 0 {
		state.BatchSize = node.Database.Backfill.BatchSize
	}
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if err := upsertBackfillState(ctx, db, dialect, table, state); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	batchesThisRun := 0
	for state.CurrentCursor < state.EndCursor {
		if node.Database.Backfill.MaxBatches > 0 && batchesThisRun >= node.Database.Backfill.MaxBatches {
			err := fmt.Errorf("backfill reached maxBatches=%d before completion", node.Database.Backfill.MaxBatches)
			e.recordDBNodeArtifacts(node, "backfill.json", map[string]any{
				"apiVersion":       "torque.dev/db-node/v1",
				"kind":             "DBNodeArtifact",
				"nodeId":           node.ID,
				"nodeKind":         normalizeNodeKind(node.Kind),
				"driver":           strings.TrimSpace(node.Database.Driver),
				"status":           "failure",
				"phase":            "backfill",
				"message":          err.Error(),
				"currentCursor":    state.CurrentCursor,
				"targetCursor":     state.EndCursor,
				"batchesCompleted": state.BatchesCompleted,
			})
			return e.cutoverPhaseFailure(node, "backfill", map[string]any{
				"phase":         "backfill",
				"currentCursor": state.CurrentCursor,
				"targetCursor":  state.EndCursor,
				"batchesRun":    batchesThisRun,
				"checkpointKey": state.CheckpointKey,
				"intentDigest":  state.IntentDigest,
				"batchSize":     state.BatchSize,
			}, "DB_BACKFILL_INCOMPLETE", err)
		}

		batchStart := state.CurrentCursor
		batchEnd := batchStart + state.BatchSize
		if batchEnd > state.EndCursor {
			batchEnd = state.EndCursor
		}
		phase := fmt.Sprintf("backfill[%d,%d]", batchStart, batchEnd)
		cursor := map[string]any{
			"phase":         "backfill",
			"window":        phase,
			"currentCursor": batchStart,
			"targetCursor":  state.EndCursor,
			"checkpointKey": state.CheckpointKey,
		}
		e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
		sqlText := renderBackfillSQL(node.Database.Backfill.BatchSQL, batchStart, batchEnd, state.BatchSize)
		rowsAffected, err := execSQLScriptWithStats(ctx, db, sqlText)
		if err != nil {
			e.recordDBNodeArtifacts(node, "backfill.json", map[string]any{
				"apiVersion":       "torque.dev/db-node/v1",
				"kind":             "DBNodeArtifact",
				"nodeId":           node.ID,
				"nodeKind":         normalizeNodeKind(node.Kind),
				"driver":           strings.TrimSpace(node.Database.Driver),
				"status":           "failure",
				"phase":            phase,
				"message":          err.Error(),
				"currentCursor":    batchStart,
				"targetCursor":     state.EndCursor,
				"batchesCompleted": state.BatchesCompleted,
				"totalRows":        state.TotalRowsAffected,
			})
			return e.cutoverPhaseFailure(node, phase, cursor, "DB_BACKFILL_FAILED", err)
		}
		state.CurrentCursor = batchEnd
		state.BatchesCompleted++
		state.TotalRowsAffected += rowsAffected
		state.PhaseStatus = "running"
		state.UpdatedAtNS = time.Now().UTC().UnixNano()
		if err := upsertBackfillState(ctx, db, dialect, table, state); err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, fmt.Sprintf("rows=%d", rowsAffected), map[string]any{
			"phase":        phase,
			"status":       "success",
			"message":      fmt.Sprintf("rows=%d", rowsAffected),
			"rowsAffected": rowsAffected,
			"cursor": map[string]any{
				"phase":         "backfill",
				"currentCursor": state.CurrentCursor,
				"targetCursor":  state.EndCursor,
				"checkpointKey": state.CheckpointKey,
				"batchesDone":   state.BatchesCompleted,
				"rowsAffected":  state.TotalRowsAffected,
			},
		}, nil)
		batchesThisRun++
		if node.Database.Backfill.BatchSleep != nil && *node.Database.Backfill.BatchSleep > 0 && state.CurrentCursor < state.EndCursor {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(*node.Database.Backfill.BatchSleep):
			}
		}
	}

	verifyMessage := ""
	if strings.TrimSpace(node.Database.VerifySQL) != "" {
		verifyMessage, err = verifyDatabaseCondition(ctx, db, node.Database)
		if err != nil {
			e.recordDBNodeArtifacts(node, "backfill.json", map[string]any{
				"apiVersion":       "torque.dev/db-node/v1",
				"kind":             "DBNodeArtifact",
				"nodeId":           node.ID,
				"nodeKind":         normalizeNodeKind(node.Kind),
				"driver":           strings.TrimSpace(node.Database.Driver),
				"status":           "failure",
				"phase":            "verify",
				"message":          err.Error(),
				"currentCursor":    state.CurrentCursor,
				"targetCursor":     state.EndCursor,
				"batchesCompleted": state.BatchesCompleted,
				"totalRows":        state.TotalRowsAffected,
			})
			return e.cutoverPhaseFailure(node, "verify", map[string]any{
				"phase":         "verify",
				"currentCursor": state.CurrentCursor,
				"targetCursor":  state.EndCursor,
				"checkpointKey": state.CheckpointKey,
			}, "DB_BACKFILL_VERIFY_FAILED", err)
		}
	}

	state.PhaseStatus = "success"
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if err := upsertBackfillState(ctx, db, dialect, table, state); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	e.recordDBNodeArtifacts(node, "backfill.json", map[string]any{
		"apiVersion":       "torque.dev/db-node/v1",
		"kind":             "DBNodeArtifact",
		"nodeId":           node.ID,
		"nodeKind":         normalizeNodeKind(node.Kind),
		"driver":           strings.TrimSpace(node.Database.Driver),
		"status":           "success",
		"phase":            "backfill",
		"currentCursor":    state.CurrentCursor,
		"targetCursor":     state.EndCursor,
		"batchesCompleted": state.BatchesCompleted,
		"totalRows":        state.TotalRowsAffected,
		"verifyMessage":    verifyMessage,
	})
	return nil
}

func (e *customNodeExecutor) recordDBNodeArtifacts(node *runNode, name string, payload any) {
	if e == nil || e.run == nil || node == nil {
		return
	}
	e.run.RecordJSONArtifact(node.ID, name, payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func openNodeDatabase(ctx context.Context, node *runNode) (*sql.DB, sqlDialect, error) {
	dialect, err := dialectFor(node.Database.Driver)
	if err != nil {
		return nil, sqlDialect{}, err
	}
	dsn, err := resolveDatabaseDSN(node.Database)
	if err != nil {
		return nil, sqlDialect{}, err
	}
	db, err := sql.Open(dialect.driver, dsn)
	if err != nil {
		return nil, sqlDialect{}, fmt.Errorf("open database: %w", err)
	}
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, sqlDialect{}, fmt.Errorf("ping database: %w", err)
	}
	return db, dialect, nil
}

func verifyDatabaseCondition(ctx context.Context, db *sql.DB, spec DatabaseSpec) (string, error) {
	ok, msg, err := cutoverVerify(ctx, db, spec.VerifySQL)
	if err != nil {
		return "", err
	}
	if expect := strings.TrimSpace(spec.VerifyExpectMessage); expect != "" && msg != expect && !strings.Contains(msg, expect) {
		return "", fmt.Errorf("verification message mismatch: got %s want %s", msg, expect)
	}
	if !ok {
		return "", fmt.Errorf("verification failed: %s", msg)
	}
	return msg, nil
}

func execSQLScriptWithStats(ctx context.Context, execer sqlExecer, script string) (int64, error) {
	stmts := splitSQLStatements(script)
	var totalRows int64
	for _, stmt := range stmts {
		res, err := execer.ExecContext(ctx, stmt)
		if err != nil {
			return totalRows, err
		}
		if res != nil {
			if rows, err := res.RowsAffected(); err == nil && rows > 0 {
				totalRows += rows
			}
		}
	}
	return totalRows, nil
}

func queryScalarInt64(ctx context.Context, db *sql.DB, query string) (int64, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return 0, fmt.Errorf("scalar query is required")
	}
	var raw any
	if err := db.QueryRowContext(ctx, query).Scan(&raw); err != nil {
		return 0, err
	}
	return coerceInt64(raw)
}

func coerceInt64(v any) (int64, error) {
	switch x := v.(type) {
	case int64:
		return x, nil
	case int32:
		return int64(x), nil
	case int:
		return int64(x), nil
	case int16:
		return int64(x), nil
	case int8:
		return int64(x), nil
	case uint64:
		return int64(x), nil
	case uint32:
		return int64(x), nil
	case uint:
		return int64(x), nil
	case float64:
		return int64(x), nil
	case float32:
		return int64(x), nil
	case []byte:
		return strconvParseInt64(string(x))
	case string:
		return strconvParseInt64(x)
	default:
		return 0, fmt.Errorf("unsupported scalar type %T", v)
	}
}

func strconvParseInt64(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	v, err := strconv.ParseInt(raw, 10, 64)
	if err == nil {
		return v, nil
	}
	f, ferr := strconv.ParseFloat(raw, 64)
	if ferr == nil {
		return int64(f), nil
	}
	return 0, err
}

func renderBackfillSQL(tpl string, start int64, end int64, batchSize int64) string {
	replacer := strings.NewReplacer(
		"{{.cursor_start}}", fmt.Sprintf("%d", start),
		"{{.cursor_end}}", fmt.Sprintf("%d", end),
		"{{.batch_size}}", fmt.Sprintf("%d", batchSize),
		"{{.cursorStart}}", fmt.Sprintf("%d", start),
		"{{.cursorEnd}}", fmt.Sprintf("%d", end),
		"{{.batchSize}}", fmt.Sprintf("%d", batchSize),
	)
	return replacer.Replace(tpl)
}

func backfillComplete(state *backfillState) bool {
	return state != nil && nodeStepSucceeded(state.PhaseStatus) && state.CurrentCursor >= state.EndCursor
}

func ensureBackfillTable(ctx context.Context, db *sql.DB, dialect sqlDialect, table string) error {
	stmt := backfillTableDDL(dialect, table)
	if err := execSQLScript(ctx, db, stmt); err != nil {
		return fmt.Errorf("ensure backfill table %s: %w", table, err)
	}
	return nil
}

func backfillTableDDL(dialect sqlDialect, table string) string {
	switch dialect.name {
	case "mysql":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id VARCHAR(255) PRIMARY KEY,
intent_digest VARCHAR(255) NOT NULL,
checkpoint_key VARCHAR(255) NOT NULL,
start_cursor BIGINT NOT NULL DEFAULT 0,
current_cursor BIGINT NOT NULL DEFAULT 0,
end_cursor BIGINT NOT NULL DEFAULT 0,
batch_size BIGINT NOT NULL DEFAULT 0,
batches_completed INTEGER NOT NULL DEFAULT 0,
total_rows_affected BIGINT NOT NULL DEFAULT 0,
phase_status VARCHAR(64) NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id TEXT PRIMARY KEY,
intent_digest TEXT NOT NULL,
checkpoint_key TEXT NOT NULL,
start_cursor BIGINT NOT NULL DEFAULT 0,
current_cursor BIGINT NOT NULL DEFAULT 0,
end_cursor BIGINT NOT NULL DEFAULT 0,
batch_size BIGINT NOT NULL DEFAULT 0,
batches_completed INTEGER NOT NULL DEFAULT 0,
total_rows_affected BIGINT NOT NULL DEFAULT 0,
phase_status TEXT NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	}
}

func loadBackfillState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, objectID string) (*backfillState, error) {
	q := fmt.Sprintf(`SELECT object_id, intent_digest, checkpoint_key, start_cursor, current_cursor, end_cursor,
batch_size, batches_completed, total_rows_affected, phase_status, updated_at_ns
FROM %s WHERE object_id = %s`, table, dialect.placeholder(1))
	var state backfillState
	err := db.QueryRowContext(ctx, q, objectID).Scan(
		&state.ObjectID,
		&state.IntentDigest,
		&state.CheckpointKey,
		&state.StartCursor,
		&state.CurrentCursor,
		&state.EndCursor,
		&state.BatchSize,
		&state.BatchesCompleted,
		&state.TotalRowsAffected,
		&state.PhaseStatus,
		&state.UpdatedAtNS,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load backfill state: %w", err)
	}
	return &state, nil
}

func upsertBackfillState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, state *backfillState) error {
	if state == nil {
		return fmt.Errorf("backfill state is required")
	}
	cols := []string{
		"object_id", "intent_digest", "checkpoint_key", "start_cursor", "current_cursor",
		"end_cursor", "batch_size", "batches_completed", "total_rows_affected", "phase_status", "updated_at_ns",
	}
	ph := make([]string, 0, len(cols))
	for i := range cols {
		ph = append(ph, dialect.placeholder(i+1))
	}
	assignments := make([]string, 0, len(cols)-1)
	switch dialect.name {
	case "mysql":
		for _, col := range cols[1:] {
			assignments = append(assignments, fmt.Sprintf("%s = VALUES(%s)", col, col))
		}
	default:
		for _, col := range cols[1:] {
			assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
		}
	}
	stmt := fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)`,
		table,
		strings.Join(cols, ", "),
		strings.Join(ph, ", "),
	)
	if dialect.name == "mysql" {
		stmt += "\nON DUPLICATE KEY UPDATE " + strings.Join(assignments, ", ")
	} else {
		stmt += "\nON CONFLICT (object_id) DO UPDATE SET " + strings.Join(assignments, ", ")
	}
	_, err := db.ExecContext(ctx, stmt,
		state.ObjectID,
		state.IntentDigest,
		state.CheckpointKey,
		state.StartCursor,
		state.CurrentCursor,
		state.EndCursor,
		state.BatchSize,
		state.BatchesCompleted,
		state.TotalRowsAffected,
		state.PhaseStatus,
		state.UpdatedAtNS,
	)
	if err != nil {
		return fmt.Errorf("upsert backfill state: %w", err)
	}
	return nil
}
