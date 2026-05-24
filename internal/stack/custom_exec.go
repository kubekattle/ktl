package stack

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type dispatchExecutor struct {
	helm   *helmExecutor
	custom *customNodeExecutor
}

func (d *dispatchExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	if isHelmNode(node.ResolvedRelease) {
		return d.helm.RunNode(ctx, node, command)
	}
	return d.custom.RunNode(ctx, node, command)
}

type customNodeExecutor struct {
	run    *runState
	out    io.Writer
	errOut io.Writer
	dryRun bool
	diff   bool
}

func (e *customNodeExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	switch normalizeNodeKind(node.Kind) {
	case NodeKindAction:
		return e.runScriptNode(ctx, node, command)
	case NodeKindActionPlugin:
		return e.runActionPluginNode(ctx, node, command)
	case NodeKindDBRestorePoint:
		return e.runDBRestorePointNode(ctx, node, command)
	case NodeKindDBSchemaExpand:
		return e.runDBSchemaExpandNode(ctx, node, command)
	case NodeKindDBBackfill:
		return e.runDBBackfillNode(ctx, node, command)
	case NodeKindDBVerify:
		return e.runDBVerifyNode(ctx, node, command)
	case NodeKindDBCutover:
		return e.runDBCutoverNode(ctx, node, command)
	case NodeKindDBSchemaContract:
		return e.runDBSchemaContractNode(ctx, node, command)
	case NodeKindHostCommandRun:
		return e.runHostCommandNode(ctx, node, command)
	case NodeKindK8sClusterInspect:
		return e.runKubernetesClusterInspectNode(ctx, node, command)
	case NodeKindK8sCertInspect:
		return e.runKubernetesCertInspectNode(ctx, node, command)
	case NodeKindK8sCertRenew:
		return e.runKubernetesCertRenewNode(ctx, node, command)
	case NodeKindK8sClusterVerify:
		return e.runKubernetesClusterVerifyNode(ctx, node, command)
	default:
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("unsupported node kind %q", normalizeNodeKind(node.Kind)))
	}
}

func (e *customNodeExecutor) runScriptNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Action.Apply
	phase := "script"
	if strings.EqualFold(command, "delete") {
		spec = node.Action.Delete
		phase = "delete-script"
	}
	if spec == nil {
		return nil
	}
	cursor := map[string]any{
		"kind":  normalizeNodeKind(node.Kind),
		"phase": phase,
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	cmd := exec.CommandContext(ctx, spec.Command[0], spec.Command[1:]...)
	workDir := strings.TrimSpace(spec.WorkDir)
	if workDir == "" {
		workDir = node.Dir
	}
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), flattenEnv(spec.Env)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		e.recordActionNodeArtifacts(node, phase, spec, workDir, "failure", msg)
		runErr := &RunError{Class: "SCRIPT_FAILED", Message: msg, Digest: computeRunErrorDigest("SCRIPT_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "failure",
			"message": msg,
			"cursor":  cursor,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("script phase %s: %w", phase, err))
	}
	msg := strings.TrimSpace(string(out))
	if msg == "" {
		msg = "success"
	}
	e.recordActionNodeArtifacts(node, phase, spec, workDir, "success", msg)
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":   phase,
		"status":  "success",
		"message": msg,
		"cursor":  cursor,
	}, nil)
	return nil
}

func (e *customNodeExecutor) recordActionNodeArtifacts(node *runNode, phase string, spec *ScriptHookConfig, workDir string, status string, output string) {
	if e == nil || e.run == nil || node == nil {
		return
	}
	artifactName := "script-output.json"
	if strings.TrimSpace(phase) != "" && phase != "script" {
		artifactName = strings.TrimSpace(phase) + "-output.json"
	}
	payload := map[string]any{
		"apiVersion": "torque.dev/action-node/v1",
		"kind":       "ActionNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"phase":      strings.TrimSpace(phase),
		"status":     strings.TrimSpace(status),
		"workDir":    strings.TrimSpace(workDir),
		"output":     strings.TrimSpace(output),
	}
	if spec != nil && len(spec.Command) > 0 {
		payload["command"] = append([]string(nil), spec.Command...)
	}
	trimmed := strings.TrimSpace(output)
	if trimmed != "" {
		var decoded any
		if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil {
			payload["outputFormat"] = "json"
			payload["result"] = decoded
		} else {
			payload["outputFormat"] = "text"
		}
	}
	e.run.RecordJSONArtifact(node.ID, artifactName, payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
}

func (e *customNodeExecutor) runHostCommandNode(ctx context.Context, node *runNode, command string) error {
	spec := node.Host
	remoteCommand := strings.TrimSpace(spec.Command)
	phase := "host-command"
	if strings.EqualFold(command, "delete") {
		remoteCommand = strings.TrimSpace(spec.DeleteCommand)
		phase = "delete-host-command"
	}
	if remoteCommand == "" {
		return nil
	}
	cursor := map[string]any{
		"kind":      normalizeNodeKind(node.Kind),
		"phase":     phase,
		"transport": strings.TrimSpace(spec.Transport),
	}
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)

	if e.dryRun || e.diff {
		reason := "preview"
		if e.dryRun {
			reason = "dry-run"
		} else if e.diff {
			reason = "diff"
		}
		payload := map[string]any{
			"apiVersion": "torque.dev/host-command-node/v1",
			"kind":       "HostCommandNodeArtifact",
			"nodeId":     node.ID,
			"nodeKind":   normalizeNodeKind(node.Kind),
			"phase":      phase,
			"status":     "skipped",
			"reason":     reason,
		}
		e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
		e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
		e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "skipped: "+reason, map[string]any{
			"phase":  phase,
			"status": "skipped",
			"reason": reason,
			"cursor": cursor,
		}, nil)
		return nil
	}

	transportClient, err := hostCommandTransport(spec)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	receipt := transportClient.Run(ctx, remoteCommand)
	payload := map[string]any{
		"apiVersion":   "torque.dev/host-command-node/v1",
		"kind":         "HostCommandNodeArtifact",
		"nodeId":       node.ID,
		"nodeKind":     normalizeNodeKind(node.Kind),
		"phase":        phase,
		"status":       receipt.Status,
		"targetDigest": receipt.TargetDigest,
		"receipt":      receipt,
	}
	e.run.RecordJSONArtifact(node.ID, phase+".json", payload)
	e.run.RecordJSONArtifact(node.ID, "decision.json", payload)
	if !nodeStepSucceeded(receipt.Status) {
		msg := strings.TrimSpace(receipt.Error)
		if msg == "" {
			msg = strings.TrimSpace(receipt.Stderr)
		}
		if msg == "" {
			msg = fmt.Sprintf("host command status %s", receipt.Status)
		}
		runErr := &RunError{Class: "HOST_COMMAND_FAILED", Message: msg, Digest: computeRunErrorDigest("HOST_COMMAND_FAILED", msg)}
		e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
			"phase":   phase,
			"status":  "failure",
			"cursor":  cursor,
			"receipt": receipt,
		}, runErr, true)
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("host command phase %s: %s", phase, msg))
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"cursor":  cursor,
		"receipt": receipt,
	}, nil)
	return nil
}

type hostCommandRunner interface {
	TargetDigest() string
	Run(ctx context.Context, command string) transport.OperationResult
}

func hostCommandTransport(spec HostCommandSpec) (hostCommandRunner, error) {
	transportKind := strings.ToLower(strings.TrimSpace(spec.Transport))
	if transportKind == "" {
		transportKind = "local"
	}
	timeout := 30 * time.Second
	if spec.Timeout != nil && *spec.Timeout > 0 {
		timeout = *spec.Timeout
	}
	target := strings.TrimSpace(spec.Target)
	if envName := strings.TrimSpace(spec.TargetEnv); envName != "" {
		target = strings.TrimSpace(os.Getenv(envName))
		if target == "" {
			return nil, fmt.Errorf("host command target env %s is empty", envName)
		}
	}
	switch transportKind {
	case "local", "localhost":
		if target == "" {
			target = "local://localhost"
		}
		return localtransport.New(localtransport.Config{
			Target:       target,
			Timeout:      timeout,
			RedactValues: []string{target},
		})
	case "ssh":
		if target == "" {
			return nil, fmt.Errorf("host.command.run ssh transport requires host.target or host.targetEnv")
		}
		return sshtransport.New(sshtransport.Config{
			Target:       target,
			IdentityFile: strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_IDENTITY")),
			ExtraArgs:    strings.Fields(strings.TrimSpace(os.Getenv("TORQUE_LAB_SSH_OPTS"))),
			Timeout:      timeout,
			RedactValues: []string{target},
		})
	default:
		return nil, fmt.Errorf("unsupported host.command.run transport %q", transportKind)
	}
}

type cutoverState struct {
	ObjectID                  string
	CutoverEpoch              string
	IntentDigest              string
	Phase                     string
	PhaseStatus               string
	FenceToken                string
	CommitMarker              string
	StabilizationStartedAtNS  int64
	StabilizationDeadlineAtNS int64
	AmbiguityStatus           string
	UpdatedAtNS               int64
}

type sqlDialect struct {
	name        string
	driver      string
	placeholder func(int) string
}

func dialectFor(driver string) (sqlDialect, error) {
	switch strings.ToLower(strings.TrimSpace(driver)) {
	case "postgres", "postgresql":
		return sqlDialect{
			name:   "postgres",
			driver: "postgres",
			placeholder: func(i int) string {
				return fmt.Sprintf("$%d", i)
			},
		}, nil
	case "mysql", "mariadb":
		return sqlDialect{
			name:   "mysql",
			driver: "mysql",
			placeholder: func(i int) string {
				return "?"
			},
		}, nil
	case "sqlite", "":
		return sqlDialect{
			name:   "sqlite",
			driver: "sqlite",
			placeholder: func(i int) string {
				return "?"
			},
		}, nil
	default:
		return sqlDialect{}, fmt.Errorf("unsupported database driver %q", driver)
	}
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (e *customNodeExecutor) runDBCutoverNode(ctx context.Context, node *runNode, command string) error {
	if !strings.EqualFold(command, "apply") {
		return nil
	}
	dialect, err := dialectFor(node.Database.Driver)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	dsn, err := resolveDatabaseDSN(node.Database)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	table, err := sanitizeSQLIdent(node.Database.MetadataTable)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("metadata table: %w", err))
	}
	db, err := sql.Open(dialect.driver, dsn)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("open database: %w", err))
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("ping database: %w", err))
	}
	if err := ensureCutoverTable(ctx, db, dialect, table); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	objectID := node.ID
	intent := strings.TrimSpace(node.EffectiveInputHash)
	if intent == "" {
		intent = strings.TrimSpace(node.Name)
	}
	state, err := loadCutoverState(ctx, db, dialect, table, objectID)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	if state == nil {
		state = &cutoverState{
			ObjectID:     objectID,
			CutoverEpoch: fmt.Sprintf("%s/%s", e.run.RunID, node.ID),
			IntentDigest: intent,
			Phase:        "",
			PhaseStatus:  "",
			FenceToken:   fmt.Sprintf("fence:%s/%d", e.run.RunID, node.Attempt),
			UpdatedAtNS:  time.Now().UTC().UnixNano(),
		}
	} else if strings.TrimSpace(state.IntentDigest) != "" && state.IntentDigest != intent && !cutoverComplete(state) {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("cutover intent changed while previous epoch %s is incomplete", state.CutoverEpoch))
	}
	if strings.TrimSpace(state.IntentDigest) == "" {
		state.IntentDigest = intent
	}
	if strings.TrimSpace(state.CutoverEpoch) == "" {
		state.CutoverEpoch = fmt.Sprintf("%s/%s", e.run.RunID, node.ID)
	}
	if strings.TrimSpace(state.FenceToken) == "" {
		state.FenceToken = fmt.Sprintf("fence:%s/%d", e.run.RunID, node.Attempt)
	}
	if cutoverComplete(state) {
		e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
			"apiVersion": "torque.dev/db-node/v1",
			"kind":       "DBNodeArtifact",
			"nodeId":     node.ID,
			"nodeKind":   normalizeNodeKind(node.Kind),
			"driver":     strings.TrimSpace(node.Database.Driver),
			"status":     "success",
			"phase":      "finalize",
			"state":      state,
		})
		return nil
	}

	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "prepare", strings.TrimSpace(node.Database.PrepareSQL), false); err != nil {
		return err
	}
	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "arm", strings.TrimSpace(node.Database.ArmSQL), false); err != nil {
		return err
	}
	if err := e.runCutoverCommit(ctx, db, dialect, table, node, state); err != nil {
		return err
	}
	if err := e.runCutoverStabilize(ctx, db, dialect, table, node, state); err != nil {
		return err
	}
	if err := e.runCutoverPhase(ctx, db, dialect, table, node, state, "finalize", strings.TrimSpace(node.Database.FinalizeSQL), true); err != nil {
		return err
	}
	e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
		"apiVersion": "torque.dev/db-node/v1",
		"kind":       "DBNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"driver":     strings.TrimSpace(node.Database.Driver),
		"status":     "success",
		"phase":      "finalize",
		"state":      state,
	})
	return nil
}

func cutoverComplete(state *cutoverState) bool {
	return state != nil && state.Phase == "finalize" && nodeStepSucceeded(state.PhaseStatus)
}

func (e *customNodeExecutor) runCutoverPhase(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState, phase string, sqlText string, terminal bool) error {
	if state == nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("cutover state is required"))
	}
	if cutoverPhaseDone(state, phase) {
		return nil
	}
	cursor := cutoverCursor(state, phase)
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, phase, map[string]any{"phase": phase, "cursor": cursor}, nil)
	if strings.TrimSpace(sqlText) != "" {
		if err := execSQLScript(ctx, db, sqlText); err != nil {
			return e.cutoverPhaseFailure(node, phase, cursor, "DB_CUTOVER_FAILED", err)
		}
	}
	state.Phase = phase
	state.PhaseStatus = "success"
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if terminal {
		state.FenceToken = ""
	}
	if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   phase,
		"status":  "success",
		"cursor":  cursor,
		"message": "success",
	}, nil)
	return nil
}

func (e *customNodeExecutor) runCutoverCommit(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState) error {
	if cutoverPhaseDone(state, "commit") {
		return nil
	}
	cursor := cutoverCursor(state, "commit")
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "commit", map[string]any{"phase": "commit", "cursor": cursor}, nil)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("begin cutover commit: %w", err))
	}
	if err := execSQLScript(ctx, tx, node.Database.CommitSQL); err != nil {
		_ = tx.Rollback()
		return e.cutoverPhaseFailure(node, "commit", cursor, "DB_CUTOVER_FAILED", err)
	}
	state.Phase = "commit"
	state.PhaseStatus = "success"
	if strings.TrimSpace(state.CommitMarker) == "" {
		state.CommitMarker = fmt.Sprintf("commit:%s", state.CutoverEpoch)
	}
	state.UpdatedAtNS = time.Now().UTC().UnixNano()
	if err := upsertCutoverStateTx(ctx, tx, dialect, table, state); err != nil {
		_ = tx.Rollback()
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	if err := tx.Commit(); err != nil {
		state.AmbiguityStatus = "commit-uncertain"
		state.PhaseStatus = "ambiguous"
		state.UpdatedAtNS = time.Now().UTC().UnixNano()
		_ = upsertCutoverState(ctx, db, dialect, table, state)
		return e.cutoverPhaseFailure(node, "commit", cursor, "DB_CUTOVER_AMBIGUOUS", err)
	}
	e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "success", map[string]any{
		"phase":   "commit",
		"status":  "success",
		"cursor":  cutoverCursor(state, "commit"),
		"message": "success",
	}, nil)
	return nil
}

func (e *customNodeExecutor) runCutoverStabilize(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, node *runNode, state *cutoverState) error {
	if cutoverPhaseDone(state, "stabilize") {
		return nil
	}
	cursor := cutoverCursor(state, "stabilize")
	e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "stabilize", map[string]any{"phase": "stabilize", "cursor": cursor}, nil)
	now := time.Now().UTC()
	if state.StabilizationStartedAtNS == 0 {
		state.StabilizationStartedAtNS = now.UnixNano()
		window := time.Duration(0)
		if node.Database.StabilizationWindow != nil {
			window = *node.Database.StabilizationWindow
		}
		state.StabilizationDeadlineAtNS = now.Add(window).UnixNano()
		state.Phase = "stabilize"
		state.PhaseStatus = "running"
		state.UpdatedAtNS = now.UnixNano()
		if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
	}
	deadline := time.Unix(0, state.StabilizationDeadlineAtNS)
	if state.StabilizationDeadlineAtNS == 0 {
		deadline = now
	}
	for {
		ok, msg, err := cutoverVerify(ctx, db, node.Database.VerifySQL)
		if err != nil {
			return e.cutoverPhaseFailure(node, "stabilize", cursor, "DB_CUTOVER_FAILED", err)
		}
		if ok && !time.Now().UTC().Before(deadline) {
			state.Phase = "stabilize"
			state.PhaseStatus = "success"
			state.UpdatedAtNS = time.Now().UTC().UnixNano()
			if err := upsertCutoverState(ctx, db, dialect, table, state); err != nil {
				return wrapNodeErr(node.ResolvedRelease, err)
			}
			e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
				"phase":   "stabilize",
				"status":  "success",
				"cursor":  cutoverCursor(state, "stabilize"),
				"message": msg,
			}, nil)
			return nil
		}
		if !ok && !time.Now().UTC().Before(deadline) {
			return e.cutoverPhaseFailure(node, "stabilize", cursor, "DB_CUTOVER_FAILED", fmt.Errorf("stabilization verification failed: %s", msg))
		}
		wait := 500 * time.Millisecond
		if rem := time.Until(deadline); rem > 0 && rem < wait {
			wait = rem
		}
		if wait <= 0 {
			wait = 50 * time.Millisecond
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(wait):
		}
	}
}

func (e *customNodeExecutor) cutoverPhaseFailure(node *runNode, phase string, cursor map[string]any, class string, err error) error {
	msg := err.Error()
	runErr := &RunError{Class: class, Message: msg, Digest: computeRunErrorDigest(class, msg)}
	e.recordDBNodeArtifacts(node, "cutover.json", map[string]any{
		"apiVersion": "torque.dev/db-node/v1",
		"kind":       "DBNodeArtifact",
		"nodeId":     node.ID,
		"nodeKind":   normalizeNodeKind(node.Kind),
		"driver":     strings.TrimSpace(node.Database.Driver),
		"status":     "failure",
		"phase":      phase,
		"message":    msg,
		"cursor":     cursor,
		"errorClass": class,
	})
	e.run.emitEvent(node.ID, PhaseCompleted, node.Attempt, msg, map[string]any{
		"phase":   phase,
		"status":  "failure",
		"cursor":  cursor,
		"message": msg,
	}, runErr, true)
	return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("%s phase %s: %w", normalizeNodeKind(node.Kind), phase, err))
}

func cutoverPhaseDone(state *cutoverState, phase string) bool {
	if state == nil || !nodeStepSucceeded(state.PhaseStatus) {
		return false
	}
	order := map[string]int{"prepare": 1, "arm": 2, "commit": 3, "stabilize": 4, "finalize": 5}
	return order[strings.TrimSpace(state.Phase)] >= order[phase]
}

func cutoverCursor(state *cutoverState, phase string) map[string]any {
	if state == nil {
		return map[string]any{"phase": phase}
	}
	return map[string]any{
		"phase":          phase,
		"cutoverEpoch":   state.CutoverEpoch,
		"intentDigest":   state.IntentDigest,
		"fenceToken":     state.FenceToken,
		"commitMarker":   state.CommitMarker,
		"ambiguityState": state.AmbiguityStatus,
	}
}

func resolveDatabaseDSN(spec DatabaseSpec) (string, error) {
	if strings.TrimSpace(spec.DSN) != "" {
		return strings.TrimSpace(spec.DSN), nil
	}
	if env := strings.TrimSpace(spec.DSNEnv); env != "" {
		if v := strings.TrimSpace(os.Getenv(env)); v != "" {
			return v, nil
		}
		return "", fmt.Errorf("environment variable %s is not set", env)
	}
	return "", fmt.Errorf("database dsn is required")
}

func sanitizeSQLIdent(v string) (string, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return "", fmt.Errorf("identifier is required")
	}
	for _, part := range strings.Split(v, ".") {
		part = strings.TrimSpace(part)
		if part == "" {
			return "", fmt.Errorf("empty identifier segment")
		}
		for _, r := range part {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
				continue
			}
			return "", fmt.Errorf("invalid identifier %q", v)
		}
	}
	return v, nil
}

func ensureCutoverTable(ctx context.Context, db *sql.DB, dialect sqlDialect, table string) error {
	stmt := cutoverTableDDL(dialect, table)
	if err := execSQLScript(ctx, db, stmt); err != nil {
		return fmt.Errorf("ensure cutover table %s: %w", table, err)
	}
	return nil
}

func loadCutoverState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, objectID string) (*cutoverState, error) {
	q := fmt.Sprintf(`SELECT object_id, cutover_epoch, intent_digest, phase, phase_status, fence_token, commit_marker,
stabilization_started_at_ns, stabilization_deadline_at_ns, ambiguity_status, updated_at_ns
FROM %s WHERE object_id = %s`, table, dialect.placeholder(1))
	var state cutoverState
	err := db.QueryRowContext(ctx, q, objectID).Scan(
		&state.ObjectID,
		&state.CutoverEpoch,
		&state.IntentDigest,
		&state.Phase,
		&state.PhaseStatus,
		&state.FenceToken,
		&state.CommitMarker,
		&state.StabilizationStartedAtNS,
		&state.StabilizationDeadlineAtNS,
		&state.AmbiguityStatus,
		&state.UpdatedAtNS,
	)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load cutover state: %w", err)
	}
	return &state, nil
}

func upsertCutoverState(ctx context.Context, db *sql.DB, dialect sqlDialect, table string, state *cutoverState) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin cutover state tx: %w", err)
	}
	if err := upsertCutoverStateTx(ctx, tx, dialect, table, state); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit cutover state tx: %w", err)
	}
	return nil
}

func upsertCutoverStateTx(ctx context.Context, tx *sql.Tx, dialect sqlDialect, table string, state *cutoverState) error {
	if state == nil {
		return fmt.Errorf("cutover state is required")
	}
	cols := []string{
		"object_id", "cutover_epoch", "intent_digest", "phase", "phase_status",
		"fence_token", "commit_marker", "stabilization_started_at_ns",
		"stabilization_deadline_at_ns", "ambiguity_status", "updated_at_ns",
	}
	ph := make([]string, 0, len(cols))
	for i := range cols {
		ph = append(ph, dialect.placeholder(i+1))
	}
	updateCols := []string{
		"cutover_epoch", "intent_digest", "phase", "phase_status", "fence_token",
		"commit_marker", "stabilization_started_at_ns", "stabilization_deadline_at_ns",
		"ambiguity_status", "updated_at_ns",
	}
	stmt := cutoverUpsertStmt(dialect, table, cols, ph, updateCols)
	_, err := tx.ExecContext(ctx, stmt,
		state.ObjectID,
		state.CutoverEpoch,
		state.IntentDigest,
		state.Phase,
		state.PhaseStatus,
		state.FenceToken,
		state.CommitMarker,
		state.StabilizationStartedAtNS,
		state.StabilizationDeadlineAtNS,
		state.AmbiguityStatus,
		state.UpdatedAtNS,
	)
	if err != nil {
		return fmt.Errorf("upsert cutover state: %w", err)
	}
	return nil
}

func cutoverVerify(ctx context.Context, db *sql.DB, query string) (bool, string, error) {
	query = strings.TrimSpace(query)
	if query == "" {
		return true, "no verify query configured", nil
	}
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return false, "", err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return false, "", err
	}
	if !rows.Next() {
		return false, "verify query returned no rows", nil
	}
	raw := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range raw {
		ptrs[i] = &raw[i]
	}
	if err := rows.Scan(ptrs...); err != nil {
		return false, "", err
	}
	if len(raw) == 0 {
		return false, "verify query returned no columns", nil
	}
	ok := truthyDBValue(raw[0])
	msg := fmt.Sprintf("verify=%v", ok)
	if len(raw) == 1 {
		if payload, err := json.Marshal(raw[0]); err == nil {
			msg = string(payload)
		}
	} else if payload, err := json.Marshal(raw); err == nil {
		msg = string(payload)
	}
	return ok, msg, nil
}

func truthyDBValue(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int64:
		return x != 0
	case int32:
		return x != 0
	case int:
		return x != 0
	case int8:
		return x != 0
	case int16:
		return x != 0
	case float64:
		return x != 0
	case float32:
		return x != 0
	case uint:
		return x != 0
	case uint8:
		return x != 0
	case uint16:
		return x != 0
	case uint32:
		return x != 0
	case uint64:
		return x != 0
	case []byte:
		return truthyString(string(x))
	case string:
		return truthyString(x)
	default:
		return false
	}
}

func truthyString(v string) bool {
	if n, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
		return n != 0
	}
	switch strings.TrimSpace(strings.ToLower(v)) {
	case "1", "t", "true", "y", "yes", "ready", "ok", "success":
		return true
	default:
		return false
	}
}

func cutoverTableDDL(dialect sqlDialect, table string) string {
	switch dialect.name {
	case "mysql":
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id VARCHAR(255) PRIMARY KEY,
cutover_epoch VARCHAR(255) NOT NULL,
intent_digest VARCHAR(255) NOT NULL,
phase VARCHAR(64) NOT NULL,
phase_status VARCHAR(64) NOT NULL,
fence_token VARCHAR(255) NOT NULL DEFAULT '',
commit_marker VARCHAR(255) NOT NULL DEFAULT '',
stabilization_started_at_ns BIGINT NOT NULL DEFAULT 0,
stabilization_deadline_at_ns BIGINT NOT NULL DEFAULT 0,
ambiguity_status VARCHAR(64) NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	default:
		return fmt.Sprintf(`CREATE TABLE IF NOT EXISTS %s (
object_id TEXT PRIMARY KEY,
cutover_epoch TEXT NOT NULL,
intent_digest TEXT NOT NULL,
phase TEXT NOT NULL,
phase_status TEXT NOT NULL,
fence_token TEXT NOT NULL DEFAULT '',
commit_marker TEXT NOT NULL DEFAULT '',
stabilization_started_at_ns BIGINT NOT NULL DEFAULT 0,
stabilization_deadline_at_ns BIGINT NOT NULL DEFAULT 0,
ambiguity_status TEXT NOT NULL DEFAULT '',
updated_at_ns BIGINT NOT NULL DEFAULT 0
)`, table)
	}
}

func cutoverUpsertStmt(dialect sqlDialect, table string, cols []string, ph []string, updateCols []string) string {
	assignments := make([]string, 0, len(updateCols))
	switch dialect.name {
	case "mysql":
		for _, col := range updateCols {
			assignments = append(assignments, fmt.Sprintf("%s = VALUES(%s)", col, col))
		}
		return fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
ON DUPLICATE KEY UPDATE %s`,
			table,
			strings.Join(cols, ", "),
			strings.Join(ph, ", "),
			strings.Join(assignments, ", "),
		)
	default:
		for _, col := range updateCols {
			assignments = append(assignments, fmt.Sprintf("%s = EXCLUDED.%s", col, col))
		}
		return fmt.Sprintf(`INSERT INTO %s (%s) VALUES (%s)
ON CONFLICT (object_id) DO UPDATE SET %s`,
			table,
			strings.Join(cols, ", "),
			strings.Join(ph, ", "),
			strings.Join(assignments, ", "),
		)
	}
}

func execSQLScript(ctx context.Context, execer sqlExecer, script string) error {
	stmts := splitSQLStatements(script)
	for _, stmt := range stmts {
		if _, err := execer.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

func splitSQLStatements(script string) []string {
	script = strings.TrimSpace(script)
	if script == "" {
		return nil
	}
	var out []string
	var buf strings.Builder
	inSingle := false
	inDouble := false
	inBacktick := false
	inLineComment := false
	inBlockComment := false
	runes := []rune(script)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		var next rune
		if i+1 < len(runes) {
			next = runes[i+1]
		}
		if inLineComment {
			buf.WriteRune(r)
			if r == '\n' {
				inLineComment = false
			}
			continue
		}
		if inBlockComment {
			buf.WriteRune(r)
			if r == '*' && next == '/' {
				i++
				buf.WriteRune(next)
				inBlockComment = false
			}
			continue
		}
		if !inSingle && !inDouble && !inBacktick {
			if r == '-' && next == '-' {
				buf.WriteRune(r)
				i++
				buf.WriteRune(next)
				inLineComment = true
				continue
			}
			if r == '#' {
				buf.WriteRune(r)
				inLineComment = true
				continue
			}
			if r == '/' && next == '*' {
				buf.WriteRune(r)
				i++
				buf.WriteRune(next)
				inBlockComment = true
				continue
			}
		}
		switch r {
		case '\'':
			buf.WriteRune(r)
			if !inDouble && !inBacktick {
				if inSingle && next == '\'' {
					i++
					buf.WriteRune(next)
					continue
				}
				inSingle = !inSingle
			}
		case '"':
			buf.WriteRune(r)
			if !inSingle && !inBacktick {
				if inDouble && next == '"' {
					i++
					buf.WriteRune(next)
					continue
				}
				inDouble = !inDouble
			}
		case '`':
			buf.WriteRune(r)
			if !inSingle && !inDouble {
				inBacktick = !inBacktick
			}
		case ';':
			if inSingle || inDouble || inBacktick {
				buf.WriteRune(r)
				continue
			}
			stmt := strings.TrimSpace(buf.String())
			if stmt != "" {
				out = append(out, stmt)
			}
			buf.Reset()
		default:
			buf.WriteRune(r)
		}
	}
	stmt := strings.TrimSpace(buf.String())
	if stmt != "" {
		out = append(out, stmt)
	}
	return out
}

func flattenEnv(env map[string]string) []string {
	if len(env) == 0 {
		return nil
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, env[k]))
	}
	return out
}
