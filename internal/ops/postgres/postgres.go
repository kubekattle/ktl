package postgres

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lib/pq"
)

const (
	RequestAPIVersion = "torque.dev/postgres-resource-request/v1"
	RequestKind       = "PostgresResourceRequest"
	ResultAPIVersion  = "torque.dev/postgres-resource-result/v1"
	ResultKind        = "PostgresResourceResult"
)

type ResourceRequest struct {
	APIVersion string          `json:"apiVersion,omitempty"`
	Kind       string          `json:"kind,omitempty"`
	Tenant     string          `json:"tenant,omitempty"`
	NodeID     string          `json:"nodeId,omitempty"`
	RunID      string          `json:"runId,omitempty"`
	NodeKind   string          `json:"nodeKind"`
	Lock       *LockPolicy     `json:"lock,omitempty"`
	Spec       json.RawMessage `json:"spec"`
}

type LockPolicy struct {
	Enabled       bool   `json:"enabled"`
	Key           string `json:"key,omitempty"`
	TimeoutMillis int64  `json:"timeoutMillis,omitempty"`
	OnFailure     string `json:"onFailure,omitempty"`
}

type Spec struct {
	Database         string          `json:"database,omitempty"`
	EnvFile          string          `json:"envFile,omitempty"`
	Host             string          `json:"host,omitempty"`
	HostEnv          string          `json:"hostEnv,omitempty"`
	Port             int             `json:"port,omitempty"`
	PortEnv          string          `json:"portEnv,omitempty"`
	User             string          `json:"user,omitempty"`
	PasswordEnv      string          `json:"passwordEnv,omitempty"`
	SSLMode          string          `json:"sslMode,omitempty"`
	PSQLCommand      string          `json:"psqlCommand,omitempty"`
	PGDumpCommand    string          `json:"pgDumpCommand,omitempty"`
	PGRestoreCommand string          `json:"pgRestoreCommand,omitempty"`
	RunAsUser        string          `json:"runAsUser,omitempty"`
	Role             RoleSpec        `json:"role,omitempty"`
	DatabaseRef      DatabaseSpec    `json:"databaseRef,omitempty"`
	Grant            GrantSpec       `json:"grant,omitempty"`
	Schema           SchemaSpec      `json:"schema,omitempty"`
	Extension        ExtensionSpec   `json:"extension,omitempty"`
	Replication      ReplicationSpec `json:"replication,omitempty"`
	Backup           BackupSpec      `json:"backup,omitempty"`
	Restore          RestoreSpec     `json:"restore,omitempty"`
	Config           ConfigSpec      `json:"config,omitempty"`
	Maintenance      MaintenanceSpec `json:"maintenance,omitempty"`
}

type RoleSpec struct {
	Name        string `json:"name,omitempty"`
	PasswordEnv string `json:"passwordEnv,omitempty"`
	Login       *bool  `json:"login,omitempty"`
	Superuser   *bool  `json:"superuser,omitempty"`
}

type DatabaseSpec struct {
	Name  string `json:"name,omitempty"`
	Owner string `json:"owner,omitempty"`
}

type GrantSpec struct {
	Role       string   `json:"role,omitempty"`
	Database   string   `json:"database,omitempty"`
	Schema     string   `json:"schema,omitempty"`
	ObjectType string   `json:"objectType,omitempty"`
	Privileges []string `json:"privileges,omitempty"`
}

type SchemaSpec struct {
	Name     string `json:"name,omitempty"`
	Database string `json:"database,omitempty"`
	Owner    string `json:"owner,omitempty"`
}

type ExtensionSpec struct {
	Name     string `json:"name,omitempty"`
	Database string `json:"database,omitempty"`
	Schema   string `json:"schema,omitempty"`
}

type ReplicationSpec struct {
	ExpectedReplicas int  `json:"expectedReplicas,omitempty"`
	RequireStreaming bool `json:"requireStreaming,omitempty"`
}

type BackupSpec struct {
	Database         string          `json:"database,omitempty"`
	ID               string          `json:"id,omitempty"`
	Path             string          `json:"path,omitempty"`
	File             string          `json:"file,omitempty"`
	Format           string          `json:"format,omitempty"`
	ManifestPath     string          `json:"manifestPath,omitempty"`
	CatalogPath      string          `json:"catalogPath,omitempty"`
	Compress         int             `json:"compress,omitempty"`
	ExpectedSha256   string          `json:"expectedSha256,omitempty"`
	SimulateDuration *time.Duration  `json:"simulateDuration,omitempty"`
	Store            BackupStoreSpec `json:"store,omitempty"`
}

type BackupStoreSpec struct {
	Type               string `json:"type,omitempty"`
	Ref                string `json:"ref,omitempty"`
	RefEnv             string `json:"refEnv,omitempty"`
	Bucket             string `json:"bucket,omitempty"`
	BucketEnv          string `json:"bucketEnv,omitempty"`
	Prefix             string `json:"prefix,omitempty"`
	PrefixEnv          string `json:"prefixEnv,omitempty"`
	Region             string `json:"region,omitempty"`
	RegionEnv          string `json:"regionEnv,omitempty"`
	Endpoint           string `json:"endpoint,omitempty"`
	EndpointEnv        string `json:"endpointEnv,omitempty"`
	PathStyle          bool   `json:"pathStyle,omitempty"`
	PartSizeBytes      int64  `json:"partSizeBytes,omitempty"`
	SessionPath        string `json:"sessionPath,omitempty"`
	AccessKeyIDEnv     string `json:"accessKeyIdEnv,omitempty"`
	SecretAccessKeyEnv string `json:"secretAccessKeyEnv,omitempty"`
	SessionTokenEnv    string `json:"sessionTokenEnv,omitempty"`
}

type RestoreSpec struct {
	BackupFile string `json:"backupFile,omitempty"`
	Database   string `json:"database,omitempty"`
	VerifySQL  string `json:"verifySQL,omitempty"`
	Expect     string `json:"expect,omitempty"`
	Cleanup    bool   `json:"cleanup,omitempty"`
}

type ConfigSpec struct {
	Settings map[string]string `json:"settings,omitempty"`
	Reload   bool              `json:"reload,omitempty"`
}

type MaintenanceSpec struct {
	Action   string `json:"action,omitempty"`
	Database string `json:"database,omitempty"`
	Table    string `json:"table,omitempty"`
}

type Result struct {
	APIVersion        string              `json:"apiVersion"`
	Kind              string              `json:"kind"`
	NodeID            string              `json:"nodeId,omitempty"`
	RunID             string              `json:"runId,omitempty"`
	NodeKind          string              `json:"nodeKind"`
	Status            string              `json:"status"`
	Changed           bool                `json:"changed"`
	Database          string              `json:"database,omitempty"`
	Message           string              `json:"message,omitempty"`
	Observed          map[string]any      `json:"observed,omitempty"`
	Desired           map[string]any      `json:"desired,omitempty"`
	Diff              []Diff              `json:"diff,omitempty"`
	Plan              Plan                `json:"plan,omitempty"`
	PlannedSQLDigest  string              `json:"plannedSqlDigest,omitempty"`
	ExecutedSQLDigest string              `json:"executedSqlDigest,omitempty"`
	Lock              *LockReceipt        `json:"lock,omitempty"`
	Transaction       *TransactionReceipt `json:"transaction,omitempty"`
	Verify            Verify              `json:"verify,omitempty"`
	Backup            *BackupResult       `json:"backup,omitempty"`
	Restore           *RestoreResult      `json:"restore,omitempty"`
	StartedAt         string              `json:"startedAt"`
	CompletedAt       string              `json:"completedAt"`
}

type LockReceipt struct {
	Key           string `json:"lockKey"`
	Digest        string `json:"lockDigest"`
	Acquired      bool   `json:"lockAcquired"`
	WaitMillis    int64  `json:"lockWaitMillis"`
	TimeoutMillis int64  `json:"timeoutMillis,omitempty"`
	Released      bool   `json:"released,omitempty"`
	ReleaseError  string `json:"releaseError,omitempty"`
	Blocked       bool   `json:"blocked,omitempty"`
}

type TransactionReceipt struct {
	Supported  bool   `json:"supported"`
	Started    bool   `json:"transactionStarted"`
	Committed  bool   `json:"transactionCommitted"`
	RolledBack bool   `json:"transactionRolledBack"`
	Reason     string `json:"reason,omitempty"`
}

type Diff struct {
	Path   string `json:"path"`
	From   any    `json:"from,omitempty"`
	To     any    `json:"to,omitempty"`
	Action string `json:"action"`
}

type Plan struct {
	Action    string   `json:"action,omitempty"`
	SQLDigest string   `json:"sqlDigest,omitempty"`
	SQL       []string `json:"sql,omitempty"`
}

type Verify struct {
	Status string         `json:"status,omitempty"`
	Checks map[string]any `json:"checks,omitempty"`
}

type BackupResult struct {
	File         string             `json:"file,omitempty"`
	ManifestPath string             `json:"manifestPath,omitempty"`
	CatalogPath  string             `json:"catalogPath,omitempty"`
	ID           string             `json:"id,omitempty"`
	Sha256       string             `json:"sha256,omitempty"`
	Bytes        int64              `json:"bytes,omitempty"`
	Store        *BackupStoreResult `json:"store,omitempty"`
}

type BackupStoreResult struct {
	Type          string `json:"type,omitempty"`
	Bucket        string `json:"bucket,omitempty"`
	Key           string `json:"key,omitempty"`
	URI           string `json:"uri,omitempty"`
	Region        string `json:"region,omitempty"`
	Endpoint      string `json:"endpoint,omitempty"`
	Uploaded      bool   `json:"uploaded"`
	Resumed       bool   `json:"resumed,omitempty"`
	Multipart     bool   `json:"multipart,omitempty"`
	UploadID      string `json:"uploadId,omitempty"`
	Parts         int    `json:"parts,omitempty"`
	PartSizeBytes int64  `json:"partSizeBytes,omitempty"`
	ETag          string `json:"etag,omitempty"`
	Bytes         int64  `json:"bytes,omitempty"`
	Sha256        string `json:"sha256,omitempty"`
	ManifestKey   string `json:"manifestKey,omitempty"`
	ManifestURI   string `json:"manifestUri,omitempty"`
	CatalogKey    string `json:"catalogKey,omitempty"`
	CatalogURI    string `json:"catalogUri,omitempty"`
	SessionPath   string `json:"sessionPath,omitempty"`
}

type RestoreResult struct {
	Database     string `json:"database,omitempty"`
	BackupFile   string `json:"backupFile,omitempty"`
	VerifyOutput string `json:"verifyOutput,omitempty"`
}

type Runner struct {
	Executable string
	Stdout     io.Writer
	Stderr     io.Writer
}

func Execute(ctx context.Context, req ResourceRequest) (Result, error) {
	return Runner{}.Execute(ctx, req)
}

func (r Runner) Execute(ctx context.Context, req ResourceRequest) (Result, error) {
	ctx = withProgressReporter(ctx, r.Stdout, r.Stderr)
	req = normalizeRequest(req)
	spec, err := decodeSpec(req.Spec)
	if err != nil {
		return failureResult(req, spec, err), err
	}
	if shouldReexecAsUser(spec.RunAsUser) {
		return r.reexecAsUser(ctx, req, strings.TrimSpace(spec.RunAsUser))
	}
	return executeNative(ctx, req, spec)
}

func ExecuteFromBase64(ctx context.Context, encoded string) (Result, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return Result{}, fmt.Errorf("decode postgres resource request: %w", err)
	}
	var req ResourceRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return Result{}, fmt.Errorf("parse postgres resource request: %w", err)
	}
	return Execute(ctx, req)
}

func normalizeRequest(req ResourceRequest) ResourceRequest {
	if strings.TrimSpace(req.APIVersion) == "" {
		req.APIVersion = RequestAPIVersion
	}
	if strings.TrimSpace(req.Kind) == "" {
		req.Kind = RequestKind
	}
	req.Tenant = first(req.Tenant, "default")
	req.NodeID = strings.TrimSpace(req.NodeID)
	req.RunID = strings.TrimSpace(req.RunID)
	req.NodeKind = strings.TrimSpace(req.NodeKind)
	return req
}

func decodeSpec(raw json.RawMessage) (Spec, error) {
	var spec Spec
	if len(raw) == 0 {
		return spec, nil
	}
	if err := json.Unmarshal(raw, &spec); err != nil {
		return spec, fmt.Errorf("parse postgres spec: %w", err)
	}
	spec.Database = first(spec.Database, "postgres")
	spec.User = first(spec.User, "postgres")
	spec.PGDumpCommand = first(spec.PGDumpCommand, "pg_dump")
	spec.PGRestoreCommand = first(spec.PGRestoreCommand, "pg_restore")
	if strings.TrimSpace(spec.RunAsUser) == "" && postgresHost(spec) == "" && strings.TrimSpace(spec.PasswordEnv) == "" {
		spec.RunAsUser = "postgres"
	}
	return spec, nil
}

func executeNative(ctx context.Context, req ResourceRequest, spec Spec) (Result, error) {
	started := time.Now().UTC()
	result := Result{
		APIVersion: ResultAPIVersion,
		Kind:       ResultKind,
		NodeID:     req.NodeID,
		RunID:      req.RunID,
		NodeKind:   req.NodeKind,
		Status:     "succeeded",
		Database:   spec.Database,
		StartedAt:  started.Format(time.RFC3339Nano),
	}
	err := withAdvisoryLock(ctx, req, spec, &result, func() error {
		return executeResourceKind(ctx, req, &result, spec)
	})
	result.PlannedSQLDigest = strings.TrimSpace(result.Plan.SQLDigest)
	switch req.NodeKind {
	case "postgres.database.ensure", "postgres.config.ensure", "postgres.maintenance.run", "postgres.backup.run", "postgres.restore.drill":
		if result.Transaction == nil {
			result.Transaction = transactionUnsupportedReceipt(req.NodeKind)
		}
	}
	if strings.TrimSpace(result.ExecutedSQLDigest) == "" && result.Changed && strings.TrimSpace(result.Plan.SQLDigest) != "" {
		result.ExecutedSQLDigest = strings.TrimSpace(result.Plan.SQLDigest)
	}
	if err != nil {
		var blocked blockedError
		if errors.As(err, &blocked) {
			result.Status = "blocked"
			result.Message = err.Error()
			result.Verify = Verify{Status: "blocked", Checks: map[string]any{"error": err.Error()}}
		} else {
			result.Status = "failed"
			result.Message = err.Error()
			result.Verify = Verify{Status: "failed", Checks: map[string]any{"error": err.Error()}}
		}
	}
	if result.Message == "" {
		if result.Changed {
			result.Message = "changed"
		} else {
			result.Message = "already current"
		}
	}
	if result.Verify.Status == "" {
		result.Verify.Status = "succeeded"
	}
	result.CompletedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, err
}

func executeResourceKind(ctx context.Context, req ResourceRequest, result *Result, spec Spec) error {
	switch req.NodeKind {
	case "postgres.role.ensure":
		return executeRoleEnsure(ctx, result, spec)
	case "postgres.database.ensure":
		return executeDatabaseEnsure(ctx, result, spec)
	case "postgres.grant.ensure":
		return executeGrantEnsure(ctx, result, spec)
	case "postgres.schema.ensure":
		return executeSchemaEnsure(ctx, result, spec)
	case "postgres.extension.ensure":
		return executeExtensionEnsure(ctx, result, spec)
	case "postgres.replication.verify":
		return executeReplicationVerify(ctx, result, spec)
	case "postgres.backup.run":
		return executeBackupRun(ctx, result, spec)
	case "postgres.backup.verify":
		return executeBackupVerify(ctx, result, spec)
	case "postgres.restore.drill":
		return executeRestoreDrill(ctx, result, spec)
	case "postgres.config.ensure":
		return executeConfigEnsure(ctx, result, spec)
	case "postgres.maintenance.run":
		return executeMaintenanceRun(ctx, result, spec)
	default:
		return fmt.Errorf("unsupported PostgreSQL resource kind %q", req.NodeKind)
	}
}

type blockedError struct {
	message string
}

func (e blockedError) Error() string {
	return e.message
}

type sqlExecer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

const defaultLockTimeoutMillis int64 = 30_000

func withAdvisoryLock(ctx context.Context, req ResourceRequest, spec Spec, result *Result, fn func() error) error {
	policy := normalizeLockPolicy(req, spec)
	if policy == nil || !policy.Enabled {
		return fn()
	}
	lockDB := postgresLockDatabase(req.NodeKind, spec)
	db, err := openDB(ctx, spec, lockDB)
	if err != nil {
		return err
	}
	defer db.Close()
	lockKey := strings.TrimSpace(policy.Key)
	lockDigest := digestValue(lockKey)
	receipt := &LockReceipt{
		Key:           lockKey,
		Digest:        lockDigest,
		TimeoutMillis: policy.TimeoutMillis,
	}
	reportProgress(ctx, "lock: waiting for advisory lock %s", lockDigest)
	result.Lock = receipt
	lockID := advisoryLockID(lockKey)
	started := time.Now()
	timeout := time.Duration(policy.TimeoutMillis) * time.Millisecond
	if timeout <= 0 {
		timeout = time.Duration(defaultLockTimeoutMillis) * time.Millisecond
	}
	deadline := started.Add(timeout)
	for {
		var acquired bool
		if err := db.QueryRowContext(ctx, `SELECT pg_try_advisory_lock($1)`, lockID).Scan(&acquired); err != nil {
			return err
		}
		receipt.WaitMillis = time.Since(started).Milliseconds()
		if acquired {
			receipt.Acquired = true
			reportProgress(ctx, "lock: acquired advisory lock %s after %dms", lockDigest, receipt.WaitMillis)
			break
		}
		if time.Now().After(deadline) {
			receipt.Blocked = true
			return blockedError{message: fmt.Sprintf("postgres advisory lock %s was not acquired within %s", lockDigest, timeout)}
		}
		select {
		case <-ctx.Done():
			receipt.Blocked = true
			return blockedError{message: fmt.Sprintf("postgres advisory lock %s was not acquired before context ended: %v", lockDigest, ctx.Err())}
		case <-time.After(100 * time.Millisecond):
		}
	}
	defer func() {
		var released bool
		if err := db.QueryRowContext(context.Background(), `SELECT pg_advisory_unlock($1)`, lockID).Scan(&released); err != nil {
			receipt.ReleaseError = err.Error()
			return
		}
		receipt.Released = released
	}()
	return fn()
}

func normalizeLockPolicy(req ResourceRequest, spec Spec) *LockPolicy {
	policy := req.Lock
	if policy == nil {
		policy = DefaultLockPolicy(req.Tenant, req.NodeKind, spec)
	} else {
		copied := *policy
		policy = &copied
		if strings.TrimSpace(policy.Key) == "" {
			defaultPolicy := DefaultLockPolicy(req.Tenant, req.NodeKind, spec)
			if defaultPolicy != nil {
				policy.Key = defaultPolicy.Key
			}
		}
	}
	if policy == nil || !policy.Enabled {
		return policy
	}
	if strings.TrimSpace(policy.Key) == "" {
		policy.Enabled = false
		return policy
	}
	if policy.TimeoutMillis <= 0 {
		policy.TimeoutMillis = defaultLockTimeoutMillis
	}
	if strings.TrimSpace(policy.OnFailure) == "" {
		policy.OnFailure = "block"
	}
	return policy
}

func DefaultLockPolicy(tenant string, nodeKind string, spec Spec) *LockPolicy {
	nodeKind = strings.TrimSpace(nodeKind)
	if !postgresLockRequired(nodeKind) {
		return nil
	}
	return &LockPolicy{
		Enabled:       true,
		Key:           postgresLockKey(tenant, nodeKind, spec),
		TimeoutMillis: defaultLockTimeoutMillis,
		OnFailure:     "block",
	}
}

func postgresLockRequired(nodeKind string) bool {
	switch strings.TrimSpace(nodeKind) {
	case "postgres.role.ensure",
		"postgres.database.ensure",
		"postgres.grant.ensure",
		"postgres.schema.ensure",
		"postgres.extension.ensure",
		"postgres.config.ensure",
		"postgres.maintenance.run",
		"postgres.backup.run",
		"postgres.restore.drill":
		return true
	default:
		return false
	}
}

func postgresTransactionSupported(nodeKind string) bool {
	switch strings.TrimSpace(nodeKind) {
	case "postgres.role.ensure",
		"postgres.grant.ensure",
		"postgres.schema.ensure",
		"postgres.extension.ensure":
		return true
	default:
		return false
	}
}

func transactionUnsupportedReceipt(nodeKind string) *TransactionReceipt {
	reason := "resource does not require a transaction"
	switch strings.TrimSpace(nodeKind) {
	case "postgres.database.ensure":
		reason = "CREATE DATABASE and ALTER DATABASE owner changes are coordinated with an advisory lock outside a transaction"
	case "postgres.config.ensure":
		reason = "ALTER SYSTEM cannot run inside a PostgreSQL transaction block; advisory lock protects config mutation"
	case "postgres.maintenance.run":
		reason = "maintenance commands can require execution outside a transaction block; advisory lock protects maintenance"
	case "postgres.backup.run":
		reason = "pg_dump is an external backup primitive coordinated by advisory lock"
	case "postgres.restore.drill":
		reason = "restore drill uses database drop/create and pg_restore outside a transaction; advisory lock protects restore"
	}
	return &TransactionReceipt{Supported: false, Reason: reason}
}

func postgresLockDatabase(nodeKind string, spec Spec) string {
	switch strings.TrimSpace(nodeKind) {
	case "postgres.schema.ensure":
		return first(spec.Schema.Database, spec.Database, "postgres")
	case "postgres.extension.ensure":
		return first(spec.Extension.Database, spec.Database, "postgres")
	case "postgres.grant.ensure":
		return grantObservationDatabase(strings.ToLower(first(spec.Grant.ObjectType, "database")), first(spec.Grant.Database, spec.Database, "postgres"))
	case "postgres.maintenance.run":
		return first(spec.Maintenance.Database, spec.Database, "postgres")
	case "postgres.backup.run":
		return first(spec.Backup.Database, spec.Database, "postgres")
	default:
		return "postgres"
	}
}

func postgresLockKey(tenant string, nodeKind string, spec Spec) string {
	tenant = first(tenant, "default")
	nodeKind = strings.TrimSpace(nodeKind)
	database := postgresLockDatabase(nodeKind, spec)
	identity := postgresResourceIdentity(nodeKind, spec)
	return strings.Join([]string{
		"tenant=" + tenant,
		"database=" + database,
		"kind=" + nodeKind,
		"identity=" + identity,
	}, "|")
}

func postgresResourceIdentity(nodeKind string, spec Spec) string {
	switch strings.TrimSpace(nodeKind) {
	case "postgres.role.ensure":
		return strings.TrimSpace(spec.Role.Name)
	case "postgres.database.ensure":
		return strings.TrimSpace(spec.DatabaseRef.Name)
	case "postgres.schema.ensure":
		return first(spec.Schema.Database, spec.Database, "postgres") + "/" + strings.TrimSpace(spec.Schema.Name)
	case "postgres.extension.ensure":
		return first(spec.Extension.Database, spec.Database, "postgres") + "/" + strings.TrimSpace(spec.Extension.Name)
	case "postgres.grant.ensure":
		privs := normalizedPrivileges(spec.Grant.Privileges, "CONNECT")
		return strings.Join([]string{
			strings.ToLower(first(spec.Grant.ObjectType, "database")),
			strings.TrimSpace(spec.Grant.Role),
			first(spec.Grant.Database, spec.Database, "postgres"),
			strings.TrimSpace(spec.Grant.Schema),
			strings.Join(privs, ","),
		}, "/")
	case "postgres.config.ensure":
		return strings.Join(sortedMapKeys(spec.Config.Settings), ",")
	case "postgres.maintenance.run":
		return strings.Join([]string{first(spec.Maintenance.Action, "analyze"), first(spec.Maintenance.Database, spec.Database, "postgres"), strings.TrimSpace(spec.Maintenance.Table)}, "/")
	case "postgres.backup.run":
		return strings.Join([]string{first(spec.Backup.Database, spec.Database, "postgres"), strings.TrimSpace(spec.Backup.File)}, "/")
	case "postgres.restore.drill":
		return strings.Join([]string{strings.TrimSpace(spec.Restore.Database), first(spec.Restore.BackupFile, spec.Backup.File)}, "/")
	default:
		return first(spec.Database, "postgres")
	}
}

func advisoryLockID(key string) int64 {
	sum := sha256.Sum256([]byte(strings.TrimSpace(key)))
	return int64(binary.BigEndian.Uint64(sum[:8]))
}

func runInTransaction(ctx context.Context, db *sql.DB, result *Result, plannedSQL []string, fn func(sqlExecer) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	receipt := &TransactionReceipt{Supported: true, Started: true}
	result.Transaction = receipt
	committed := false
	defer func() {
		if committed {
			return
		}
		if rollbackErr := tx.Rollback(); rollbackErr == nil {
			receipt.RolledBack = true
		}
	}()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	receipt.Committed = true
	result.ExecutedSQLDigest = digestStrings(plannedSQL)
	return nil
}

func executeRoleEnsure(ctx context.Context, result *Result, spec Spec) error {
	name := strings.TrimSpace(spec.Role.Name)
	if name == "" {
		return fmt.Errorf("role.name is required")
	}
	db, err := openDB(ctx, spec, "postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	observed, err := observeRole(ctx, db, name)
	if err != nil {
		return err
	}
	desired := map[string]any{"name": name}
	if spec.Role.Login != nil {
		desired["login"] = *spec.Role.Login
	}
	if spec.Role.Superuser != nil {
		desired["superuser"] = *spec.Role.Superuser
	}
	result.Observed = observed
	result.Desired = desired
	var sqls []string
	if exists, _ := observed["exists"].(bool); !exists {
		sqls = append(sqls, "CREATE ROLE "+pq.QuoteIdentifier(name))
		result.Diff = append(result.Diff, Diff{Path: "role.exists", From: false, To: true, Action: "create"})
	}
	if spec.Role.Login != nil && observed["login"] != *spec.Role.Login {
		value := "NOLOGIN"
		if *spec.Role.Login {
			value = "LOGIN"
		}
		sqls = append(sqls, "ALTER ROLE "+pq.QuoteIdentifier(name)+" "+value)
		result.Diff = append(result.Diff, Diff{Path: "role.login", From: observed["login"], To: *spec.Role.Login, Action: "update"})
	}
	if spec.Role.Superuser != nil && observed["superuser"] != *spec.Role.Superuser {
		value := "NOSUPERUSER"
		if *spec.Role.Superuser {
			value = "SUPERUSER"
		}
		sqls = append(sqls, "ALTER ROLE "+pq.QuoteIdentifier(name)+" "+value)
		result.Diff = append(result.Diff, Diff{Path: "role.superuser", From: observed["superuser"], To: *spec.Role.Superuser, Action: "update"})
	}
	if pass := envValueFrom(spec.EnvFile, spec.Role.PasswordEnv); pass != "" {
		sqls = append(sqls, "ALTER ROLE "+pq.QuoteIdentifier(name)+" PASSWORD [REDACTED]")
		result.Diff = append(result.Diff, Diff{Path: "role.password", From: "[unknown]", To: "[redacted]", Action: "update"})
	}
	result.Plan = planFromSQL(sqls)
	if len(sqls) == 0 {
		result.Message = "role already current"
		result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"role": name, "exists": true}}
		return nil
	}
	if err := runInTransaction(ctx, db, result, sqls, func(exec sqlExecer) error {
		if exists, _ := observed["exists"].(bool); !exists {
			if _, err := exec.ExecContext(ctx, "CREATE ROLE "+pq.QuoteIdentifier(name)); err != nil {
				return err
			}
		}
		if spec.Role.Login != nil {
			value := "NOLOGIN"
			if *spec.Role.Login {
				value = "LOGIN"
			}
			if _, err := exec.ExecContext(ctx, "ALTER ROLE "+pq.QuoteIdentifier(name)+" "+value); err != nil {
				return err
			}
		}
		if spec.Role.Superuser != nil {
			value := "NOSUPERUSER"
			if *spec.Role.Superuser {
				value = "SUPERUSER"
			}
			if _, err := exec.ExecContext(ctx, "ALTER ROLE "+pq.QuoteIdentifier(name)+" "+value); err != nil {
				return err
			}
		}
		if pass := envValueFrom(spec.EnvFile, spec.Role.PasswordEnv); pass != "" {
			if _, err := exec.ExecContext(ctx, "ALTER ROLE "+pq.QuoteIdentifier(name)+" PASSWORD "+pq.QuoteLiteral(pass)); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	result.Changed = true
	verified, err := observeRole(ctx, db, name)
	if err != nil {
		return err
	}
	result.Verify = Verify{Status: "succeeded", Checks: verified}
	result.Message = "role ensured"
	return nil
}

func observeRole(ctx context.Context, db *sql.DB, name string) (map[string]any, error) {
	var login, super bool
	err := db.QueryRowContext(ctx, `SELECT rolcanlogin, rolsuper FROM pg_roles WHERE rolname = $1`, name).Scan(&login, &super)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{"exists": false, "name": name}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "name": name, "login": login, "superuser": super}, nil
}

func executeDatabaseEnsure(ctx context.Context, result *Result, spec Spec) error {
	name := strings.TrimSpace(spec.DatabaseRef.Name)
	if name == "" {
		return fmt.Errorf("databaseRef.name is required")
	}
	db, err := openDB(ctx, spec, "postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	observed, err := observeDatabase(ctx, db, name)
	if err != nil {
		return err
	}
	desired := map[string]any{"name": name}
	if strings.TrimSpace(spec.DatabaseRef.Owner) != "" {
		desired["owner"] = strings.TrimSpace(spec.DatabaseRef.Owner)
	}
	result.Observed = observed
	result.Desired = desired
	var sqls []string
	if exists, _ := observed["exists"].(bool); !exists {
		stmt := "CREATE DATABASE " + pq.QuoteIdentifier(name)
		if strings.TrimSpace(spec.DatabaseRef.Owner) != "" {
			stmt += " OWNER " + pq.QuoteIdentifier(strings.TrimSpace(spec.DatabaseRef.Owner))
		}
		sqls = append(sqls, stmt)
		result.Diff = append(result.Diff, Diff{Path: "database.exists", From: false, To: true, Action: "create"})
	} else if owner := strings.TrimSpace(spec.DatabaseRef.Owner); owner != "" && observed["owner"] != owner {
		sqls = append(sqls, "ALTER DATABASE "+pq.QuoteIdentifier(name)+" OWNER TO "+pq.QuoteIdentifier(owner))
		result.Diff = append(result.Diff, Diff{Path: "database.owner", From: observed["owner"], To: owner, Action: "update"})
	}
	result.Plan = planFromSQL(sqls)
	if len(sqls) == 0 {
		result.Message = "database already current"
		return nil
	}
	for _, stmt := range sqls {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	result.ExecutedSQLDigest = digestStrings(sqls)
	result.Changed = true
	verified, err := observeDatabase(ctx, db, name)
	if err != nil {
		return err
	}
	result.Verify = Verify{Status: "succeeded", Checks: verified}
	result.Message = "database ensured"
	return nil
}

func observeDatabase(ctx context.Context, db *sql.DB, name string) (map[string]any, error) {
	var owner string
	err := db.QueryRowContext(ctx, `SELECT pg_catalog.pg_get_userbyid(datdba) FROM pg_database WHERE datname = $1`, name).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{"exists": false, "name": name}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "name": name, "owner": owner}, nil
}

func executeSchemaEnsure(ctx context.Context, result *Result, spec Spec) error {
	dbName := first(spec.Schema.Database, spec.Database)
	name := strings.TrimSpace(spec.Schema.Name)
	if name == "" {
		return fmt.Errorf("schema.name is required")
	}
	db, err := openDB(ctx, spec, dbName)
	if err != nil {
		return err
	}
	defer db.Close()
	observed, err := observeSchema(ctx, db, name)
	if err != nil {
		return err
	}
	result.Database = dbName
	result.Observed = observed
	result.Desired = map[string]any{"name": name, "database": dbName, "owner": strings.TrimSpace(spec.Schema.Owner)}
	var sqls []string
	if exists, _ := observed["exists"].(bool); !exists {
		stmt := "CREATE SCHEMA IF NOT EXISTS " + pq.QuoteIdentifier(name)
		if owner := strings.TrimSpace(spec.Schema.Owner); owner != "" {
			stmt += " AUTHORIZATION " + pq.QuoteIdentifier(owner)
		}
		sqls = append(sqls, stmt)
		result.Diff = append(result.Diff, Diff{Path: "schema.exists", From: false, To: true, Action: "create"})
	} else if owner := strings.TrimSpace(spec.Schema.Owner); owner != "" && observed["owner"] != owner {
		sqls = append(sqls, "ALTER SCHEMA "+pq.QuoteIdentifier(name)+" OWNER TO "+pq.QuoteIdentifier(owner))
		result.Diff = append(result.Diff, Diff{Path: "schema.owner", From: observed["owner"], To: owner, Action: "update"})
	}
	result.Plan = planFromSQL(sqls)
	if len(sqls) == 0 {
		result.Message = "schema already current"
		return nil
	}
	if err := runInTransaction(ctx, db, result, sqls, func(exec sqlExecer) error {
		for _, stmt := range sqls {
			if _, err := exec.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	result.Changed = true
	verified, err := observeSchema(ctx, db, name)
	if err != nil {
		return err
	}
	result.Verify = Verify{Status: "succeeded", Checks: verified}
	result.Message = "schema ensured"
	return nil
}

func observeSchema(ctx context.Context, db *sql.DB, name string) (map[string]any, error) {
	var owner string
	err := db.QueryRowContext(ctx, `SELECT pg_catalog.pg_get_userbyid(nspowner) FROM pg_namespace WHERE nspname = $1`, name).Scan(&owner)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{"exists": false, "name": name}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "name": name, "owner": owner}, nil
}

func executeExtensionEnsure(ctx context.Context, result *Result, spec Spec) error {
	dbName := first(spec.Extension.Database, spec.Database)
	name := strings.TrimSpace(spec.Extension.Name)
	if name == "" {
		return fmt.Errorf("extension.name is required")
	}
	db, err := openDB(ctx, spec, dbName)
	if err != nil {
		return err
	}
	defer db.Close()
	observed, err := observeExtension(ctx, db, name)
	if err != nil {
		return err
	}
	result.Database = dbName
	result.Observed = observed
	result.Desired = map[string]any{"name": name, "database": dbName, "schema": strings.TrimSpace(spec.Extension.Schema)}
	var sqls []string
	if exists, _ := observed["exists"].(bool); !exists {
		stmt := "CREATE EXTENSION IF NOT EXISTS " + pq.QuoteIdentifier(name)
		if schema := strings.TrimSpace(spec.Extension.Schema); schema != "" {
			stmt += " SCHEMA " + pq.QuoteIdentifier(schema)
		}
		sqls = append(sqls, stmt)
		result.Diff = append(result.Diff, Diff{Path: "extension.exists", From: false, To: true, Action: "create"})
	}
	result.Plan = planFromSQL(sqls)
	if len(sqls) == 0 {
		result.Message = "extension already current"
		return nil
	}
	if err := runInTransaction(ctx, db, result, sqls, func(exec sqlExecer) error {
		for _, stmt := range sqls {
			if _, err := exec.ExecContext(ctx, stmt); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}
	result.Changed = true
	verified, err := observeExtension(ctx, db, name)
	if err != nil {
		return err
	}
	result.Verify = Verify{Status: "succeeded", Checks: verified}
	result.Message = "extension ensured"
	return nil
}

func observeExtension(ctx context.Context, db *sql.DB, name string) (map[string]any, error) {
	var schema string
	err := db.QueryRowContext(ctx, `SELECT n.nspname FROM pg_extension e JOIN pg_namespace n ON n.oid = e.extnamespace WHERE e.extname = $1`, name).Scan(&schema)
	if errors.Is(err, sql.ErrNoRows) {
		return map[string]any{"exists": false, "name": name}, nil
	}
	if err != nil {
		return nil, err
	}
	return map[string]any{"exists": true, "name": name, "schema": schema}, nil
}

func executeGrantEnsure(ctx context.Context, result *Result, spec Spec) error {
	role := strings.TrimSpace(spec.Grant.Role)
	dbName := first(spec.Grant.Database, spec.Database)
	objectType := strings.ToLower(first(spec.Grant.ObjectType, "database"))
	privs := normalizedPrivileges(spec.Grant.Privileges, "CONNECT")
	if role == "" {
		return fmt.Errorf("grant.role is required")
	}
	db, err := openDB(ctx, spec, grantObservationDatabase(objectType, dbName))
	if err != nil {
		return err
	}
	defer db.Close()
	observed, err := observeGrant(ctx, db, objectType, role, dbName, spec.Grant.Schema, privs)
	if err != nil {
		return err
	}
	result.Database = dbName
	result.Observed = observed
	result.Desired = map[string]any{"role": role, "database": dbName, "schema": strings.TrimSpace(spec.Grant.Schema), "objectType": objectType, "privileges": privs}
	if granted, _ := observed["granted"].(bool); granted {
		result.Plan = planFromSQL(nil)
		result.Message = "grant already current"
		return nil
	}
	stmt, err := grantSQL(objectType, role, dbName, spec.Grant.Schema, privs)
	if err != nil {
		return err
	}
	result.Diff = append(result.Diff, Diff{Path: "grant.granted", From: false, To: true, Action: "grant"})
	result.Plan = planFromSQL([]string{stmt})
	if err := runInTransaction(ctx, db, result, []string{stmt}, func(exec sqlExecer) error {
		_, err := exec.ExecContext(ctx, stmt)
		return err
	}); err != nil {
		return err
	}
	result.Changed = true
	verified, err := observeGrant(ctx, db, objectType, role, dbName, spec.Grant.Schema, privs)
	if err != nil {
		return err
	}
	result.Verify = Verify{Status: "succeeded", Checks: verified}
	result.Message = "grant ensured"
	return nil
}

func grantObservationDatabase(objectType, dbName string) string {
	if objectType == "database" || strings.TrimSpace(objectType) == "" {
		return "postgres"
	}
	return dbName
}

func observeGrant(ctx context.Context, db *sql.DB, objectType, role, dbName, schema string, privs []string) (map[string]any, error) {
	priv := strings.Join(privs, ",")
	var granted bool
	switch objectType {
	case "database", "":
		if err := db.QueryRowContext(ctx, `SELECT has_database_privilege($1, $2, $3)`, role, dbName, priv).Scan(&granted); err != nil {
			return nil, err
		}
	case "schema":
		if err := db.QueryRowContext(ctx, `SELECT has_schema_privilege($1, $2, $3)`, role, schema, priv).Scan(&granted); err != nil {
			return nil, err
		}
	case "tables":
		granted = false
	default:
		return nil, fmt.Errorf("unsupported grant objectType %q", objectType)
	}
	return map[string]any{"role": role, "database": dbName, "schema": strings.TrimSpace(schema), "objectType": objectType, "privileges": privs, "granted": granted}, nil
}

func grantSQL(objectType, role, dbName, schema string, privs []string) (string, error) {
	priv := strings.Join(privs, ", ")
	switch objectType {
	case "database", "":
		return "GRANT " + priv + " ON DATABASE " + pq.QuoteIdentifier(dbName) + " TO " + pq.QuoteIdentifier(role), nil
	case "schema":
		if strings.TrimSpace(schema) == "" {
			return "", fmt.Errorf("grant.schema is required for schema grants")
		}
		return "GRANT " + priv + " ON SCHEMA " + pq.QuoteIdentifier(schema) + " TO " + pq.QuoteIdentifier(role), nil
	case "tables":
		if strings.TrimSpace(schema) == "" {
			return "", fmt.Errorf("grant.schema is required for table grants")
		}
		return "GRANT " + priv + " ON ALL TABLES IN SCHEMA " + pq.QuoteIdentifier(schema) + " TO " + pq.QuoteIdentifier(role), nil
	default:
		return "", fmt.Errorf("unsupported grant objectType %q", objectType)
	}
}

func executeReplicationVerify(ctx context.Context, result *Result, spec Spec) error {
	db, err := openDB(ctx, spec, "postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	var primary bool
	if err := db.QueryRowContext(ctx, `SELECT NOT pg_is_in_recovery()`).Scan(&primary); err != nil {
		return err
	}
	var replicas int
	query := `SELECT count(*) FROM pg_stat_replication`
	if spec.Replication.RequireStreaming {
		query += ` WHERE state = 'streaming'`
	}
	if err := db.QueryRowContext(ctx, query).Scan(&replicas); err != nil {
		return err
	}
	observed := map[string]any{"primary": primary, "replicas": replicas, "requireStreaming": spec.Replication.RequireStreaming}
	desired := map[string]any{"primary": true, "expectedReplicas": spec.Replication.ExpectedReplicas, "requireStreaming": spec.Replication.RequireStreaming}
	result.Observed = observed
	result.Desired = desired
	result.Plan = Plan{Action: "verify", SQLDigest: digestStrings([]string{query})}
	if !primary {
		return fmt.Errorf("postgres replication verify must run on primary")
	}
	if replicas < spec.Replication.ExpectedReplicas {
		return fmt.Errorf("replicas %d < expected %d", replicas, spec.Replication.ExpectedReplicas)
	}
	result.Message = "replication verified"
	result.Verify = Verify{Status: "succeeded", Checks: observed}
	return nil
}

func executeConfigEnsure(ctx context.Context, result *Result, spec Spec) error {
	if len(spec.Config.Settings) == 0 {
		result.Message = "config already current"
		result.Plan = planFromSQL(nil)
		return nil
	}
	db, err := openDB(ctx, spec, "postgres")
	if err != nil {
		return err
	}
	defer db.Close()
	keys := sortedMapKeys(spec.Config.Settings)
	observed := map[string]any{}
	desired := map[string]any{}
	var sqls []string
	for _, key := range keys {
		if !settingNamePattern.MatchString(key) {
			return fmt.Errorf("unsupported postgres setting name %s", key)
		}
		value := strings.TrimSpace(spec.Config.Settings[key])
		current := ""
		_ = db.QueryRowContext(ctx, `SELECT setting FROM pg_settings WHERE name = $1`, key).Scan(&current)
		observed[key] = current
		desired[key] = value
		if current != value {
			sqls = append(sqls, "ALTER SYSTEM SET "+key+" TO "+pq.QuoteLiteral(value))
			result.Diff = append(result.Diff, Diff{Path: "config." + key, From: current, To: value, Action: "update"})
		}
	}
	if len(sqls) > 0 && spec.Config.Reload {
		sqls = append(sqls, "SELECT pg_reload_conf()")
	}
	result.Observed = observed
	result.Desired = desired
	result.Plan = planFromSQL(sqls)
	if len(sqls) == 0 {
		result.Message = "config already current"
		return nil
	}
	for _, stmt := range sqls {
		if _, err := db.ExecContext(ctx, stmt); err != nil {
			return err
		}
	}
	result.ExecutedSQLDigest = digestStrings(sqls)
	result.Changed = true
	result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"settings": desired, "reload": spec.Config.Reload}}
	result.Message = "config ensured"
	return nil
}

func executeMaintenanceRun(ctx context.Context, result *Result, spec Spec) error {
	dbName := first(spec.Maintenance.Database, spec.Database)
	action := strings.ToLower(first(spec.Maintenance.Action, "analyze"))
	db, err := openDB(ctx, spec, dbName)
	if err != nil {
		return err
	}
	defer db.Close()
	stmt := ""
	table := strings.TrimSpace(spec.Maintenance.Table)
	tableClause := ""
	if table != "" {
		tableClause = " " + quoteQualifiedIdent(table)
	}
	switch action {
	case "analyze":
		stmt = "ANALYZE" + tableClause
	case "vacuum":
		stmt = "VACUUM (ANALYZE)" + tableClause
	case "reindex":
		if table != "" {
			stmt = "REINDEX TABLE " + quoteQualifiedIdent(table)
		} else {
			stmt = "REINDEX DATABASE " + pq.QuoteIdentifier(dbName)
		}
	default:
		return fmt.Errorf("unsupported maintenance action %s", action)
	}
	result.Database = dbName
	result.Observed = map[string]any{"database": dbName, "table": table}
	result.Desired = map[string]any{"action": action, "database": dbName, "table": table}
	result.Plan = planFromSQL([]string{stmt})
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return err
	}
	result.ExecutedSQLDigest = digestStrings([]string{stmt})
	result.Changed = true
	result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"action": action, "database": dbName, "table": table}}
	result.Message = "maintenance completed"
	return nil
}

func executeBackupRun(ctx context.Context, result *Result, spec Spec) error {
	dbName := first(spec.Backup.Database, spec.Database)
	file := strings.TrimSpace(spec.Backup.File)
	if file == "" {
		safeRun := safeFileToken(result.RunID)
		file = filepath.Join(strings.TrimSpace(spec.Backup.Path), dbName+"-"+safeRun+".dump")
	}
	manifest := first(spec.Backup.ManifestPath, file+".manifest.json")
	catalogPath := backupCatalogPath(spec, manifest)
	if strings.TrimSpace(spec.Backup.Path) == "" {
		spec.Backup.Path = filepath.Dir(file)
	}
	if err := os.MkdirAll(strings.TrimSpace(spec.Backup.Path), 0o755); err != nil {
		return err
	}
	if spec.Backup.SimulateDuration != nil && *spec.Backup.SimulateDuration > 0 {
		select {
		case <-time.After(*spec.Backup.SimulateDuration):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	tmp := file + ".tmp." + strconv.Itoa(os.Getpid())
	_ = os.Remove(tmp)
	args := []string{"-Fc", "-d", dbName, "-f", tmp}
	if spec.Backup.Compress > 0 {
		args = append(args, "-Z", strconv.Itoa(spec.Backup.Compress))
	}
	reportProgress(ctx, "backup: running pg_dump for %s -> %s", dbName, file)
	result.Database = dbName
	result.Observed = map[string]any{"backupFile": fileExists(file)}
	result.Desired = map[string]any{"database": dbName, "file": file, "manifestPath": manifest}
	result.Plan = Plan{Action: "pg_dump", SQLDigest: digestStrings([]string{strings.Join(args, " ")})}
	if err := runPostgresCommand(ctx, spec, spec.PGDumpCommand, args...); err != nil {
		return err
	}
	result.ExecutedSQLDigest = strings.TrimSpace(result.Plan.SQLDigest)
	if err := os.Rename(tmp, file); err != nil {
		return err
	}
	bytes, sha, err := fileDigest(file)
	if err != nil {
		return err
	}
	reportProgress(ctx, "backup: wrote %s (%d bytes, %s)", file, bytes, sha)
	backupID := backupID(result, spec, dbName, file)
	backup := BackupResult{ID: backupID, File: file, ManifestPath: manifest, CatalogPath: catalogPath, Sha256: sha, Bytes: bytes}
	result.Changed = true
	if err := writeBackupManifest(manifest, result.RunID, result.NodeID, dbName, backup); err != nil {
		return err
	}
	reportProgress(ctx, "backup: manifest %s", manifest)
	catalog := backupCatalogRecord(result, dbName, backup, "pending-upload")
	if err := writeBackupCatalog(catalogPath, catalog); err != nil {
		return err
	}
	reportProgress(ctx, "backup: catalog %s", catalogPath)
	if backupStoreEnabled(spec.Backup.Store) {
		reportProgress(ctx, "backup: uploading %s to durable store", file)
		store, err := uploadBackupArtifacts(ctx, spec.Backup.Store, spec.EnvFile, backupID, file, manifest, catalogPath, sha, bytes)
		if err != nil {
			return err
		}
		backup.Store = store
		if err := writeBackupManifest(manifest, result.RunID, result.NodeID, dbName, backup); err != nil {
			return err
		}
		catalog = backupCatalogRecord(result, dbName, backup, "succeeded")
		if err := writeBackupCatalog(catalogPath, catalog); err != nil {
			return err
		}
		if _, err := uploadBackupArtifacts(ctx, spec.Backup.Store, spec.EnvFile, backupID, file, manifest, catalogPath, sha, bytes); err != nil {
			return err
		}
		if backup.Store != nil {
			reportProgress(ctx, "backup: durable copy ready at %s", backup.Store.URI)
		}
	} else {
		catalog = backupCatalogRecord(result, dbName, backup, "succeeded")
		if err := writeBackupCatalog(catalogPath, catalog); err != nil {
			return err
		}
	}
	result.Backup = &backup
	result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"backupId": backupID, "file": file, "sha256": sha, "bytes": bytes, "catalogPath": catalogPath}}
	if backup.Store != nil {
		result.Verify.Checks["storeUri"] = backup.Store.URI
		result.Verify.Checks["storeType"] = backup.Store.Type
	}
	result.Message = "backup completed"
	return nil
}

func executeBackupVerify(ctx context.Context, result *Result, spec Spec) error {
	file := strings.TrimSpace(spec.Backup.File)
	if file == "" && strings.TrimSpace(spec.Backup.ManifestPath) != "" {
		file = manifestBackupFile(strings.TrimSpace(spec.Backup.ManifestPath))
	}
	reportProgress(ctx, "backup verify: checking %s", first(file, strings.TrimSpace(spec.Backup.ManifestPath)))
	if _, err := ensureBackupLocalFromStore(ctx, spec, file); err != nil {
		return err
	}
	if file == "" || !fileExists(file) {
		return fmt.Errorf("backup file not found: %s", file)
	}
	bytes, sha, err := fileDigest(file)
	if err != nil {
		return err
	}
	if expected := strings.TrimSpace(spec.Backup.ExpectedSha256); expected != "" && expected != sha {
		return fmt.Errorf("backup sha256 %s != expected %s", sha, expected)
	}
	reportProgress(ctx, "backup verify: digest %s", sha)
	if err := runPostgresCommand(ctx, spec, spec.PGRestoreCommand, "--list", file); err != nil {
		return err
	}
	reportProgress(ctx, "backup verify: pg_restore --list succeeded for %s", file)
	backupID := backupID(result, spec, first(spec.Backup.Database, spec.Database), file)
	catalogPath := backupCatalogPath(spec, strings.TrimSpace(spec.Backup.ManifestPath))
	backup := BackupResult{ID: backupID, File: file, ManifestPath: strings.TrimSpace(spec.Backup.ManifestPath), CatalogPath: catalogPath, Sha256: sha, Bytes: bytes}
	result.Observed = map[string]any{"file": file, "sha256": sha, "bytes": bytes}
	result.Desired = map[string]any{"backupId": backupID, "file": file, "expectedSha256": strings.TrimSpace(spec.Backup.ExpectedSha256)}
	result.Plan = Plan{Action: "pg_restore --list", SQLDigest: digestStrings([]string{file})}
	result.ExecutedSQLDigest = strings.TrimSpace(result.Plan.SQLDigest)
	result.Backup = &backup
	result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"backupId": backupID, "file": file, "sha256": sha, "bytes": bytes, "catalogPath": catalogPath}}
	result.Message = "backup verified"
	return nil
}

func executeRestoreDrill(ctx context.Context, result *Result, spec Spec) error {
	file := first(spec.Restore.BackupFile, spec.Backup.File)
	if file == "" && strings.TrimSpace(spec.Backup.ManifestPath) != "" {
		file = manifestBackupFile(strings.TrimSpace(spec.Backup.ManifestPath))
	}
	reportProgress(ctx, "restore: preparing backup %s", first(file, strings.TrimSpace(spec.Backup.ManifestPath)))
	if _, err := ensureBackupLocalFromStore(ctx, spec, file); err != nil {
		return err
	}
	if file == "" || !fileExists(file) {
		return fmt.Errorf("restore backup file not found: %s", file)
	}
	dbName := strings.TrimSpace(spec.Restore.Database)
	if dbName == "" {
		return fmt.Errorf("restore.database is required")
	}
	reportProgress(ctx, "restore: opening admin connection for %s", dbName)
	admin, err := openDB(ctx, spec, "postgres")
	if err != nil {
		return err
	}
	defer admin.Close()
	before, observeErr := observeDatabase(ctx, admin, dbName)
	if observeErr != nil {
		before = map[string]any{"name": dbName, "observeError": observeErr.Error()}
	}
	if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(dbName)+" WITH (FORCE)"); err != nil {
		return err
	}
	reportProgress(ctx, "restore: dropped database %s", dbName)
	if _, err := admin.ExecContext(ctx, "CREATE DATABASE "+pq.QuoteIdentifier(dbName)); err != nil {
		return err
	}
	reportProgress(ctx, "restore: created database %s", dbName)
	if err := runPostgresCommand(ctx, spec, spec.PGRestoreCommand, "--no-owner", "-d", dbName, file); err != nil {
		return err
	}
	reportProgress(ctx, "restore: pg_restore completed for %s", dbName)
	verifyOutput := ""
	if strings.TrimSpace(spec.Restore.VerifySQL) != "" {
		restoreDB, err := openDB(ctx, spec, dbName)
		if err != nil {
			return err
		}
		defer restoreDB.Close()
		verifyOutput, err = querySingleValueString(ctx, restoreDB, spec.Restore.VerifySQL)
		if err != nil {
			return err
		}
		reportProgress(ctx, "restore: verify query returned %s", verifyOutput)
		if expected := strings.TrimSpace(spec.Restore.Expect); expected != "" && verifyOutput != expected {
			return fmt.Errorf("restore verify output %s != expected %s", verifyOutput, expected)
		}
	}
	if spec.Restore.Cleanup {
		if _, err := admin.ExecContext(ctx, "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(dbName)+" WITH (FORCE)"); err != nil {
			return err
		}
		reportProgress(ctx, "restore: cleaned up database %s", dbName)
	}
	restore := RestoreResult{Database: dbName, BackupFile: file, VerifyOutput: verifyOutput}
	result.Changed = true
	result.Restore = &restore
	result.Observed = map[string]any{"databaseBefore": before}
	result.Desired = map[string]any{"database": dbName, "backupFile": file, "verifySQL": strings.TrimSpace(spec.Restore.VerifySQL), "expect": strings.TrimSpace(spec.Restore.Expect), "cleanup": spec.Restore.Cleanup}
	result.Plan = Plan{Action: "restore-drill", SQLDigest: digestStrings([]string{"DROP DATABASE", "CREATE DATABASE", "pg_restore", spec.Restore.VerifySQL})}
	result.ExecutedSQLDigest = strings.TrimSpace(result.Plan.SQLDigest)
	result.Verify = Verify{Status: "succeeded", Checks: map[string]any{"database": dbName, "verifyOutput": verifyOutput}}
	result.Message = "restore drill verified"
	return nil
}

func querySingleValueString(ctx context.Context, db *sql.DB, query string) (string, error) {
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return "", err
	}
	defer rows.Close()
	if !rows.Next() {
		return "", sql.ErrNoRows
	}
	var raw any
	if err := rows.Scan(&raw); err != nil {
		return "", err
	}
	if err := rows.Err(); err != nil {
		return "", err
	}
	switch value := raw.(type) {
	case nil:
		return "", nil
	case []byte:
		return string(value), nil
	case string:
		return value, nil
	case time.Time:
		return value.Format(time.RFC3339Nano), nil
	default:
		return fmt.Sprint(value), nil
	}
}

func openDB(ctx context.Context, spec Spec, dbName string) (*sql.DB, error) {
	db, err := sql.Open("postgres", conninfo(spec, dbName))
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return db, nil
}

func conninfo(spec Spec, dbName string) string {
	parts := []string{"dbname=" + pqQuoteValue(first(dbName, spec.Database, "postgres"))}
	if host := postgresHost(spec); host != "" {
		parts = append(parts, "host="+pqQuoteValue(host))
	}
	if port := postgresPort(spec); port > 0 {
		parts = append(parts, "port="+strconv.Itoa(port))
	}
	if strings.TrimSpace(spec.User) != "" {
		parts = append(parts, "user="+pqQuoteValue(strings.TrimSpace(spec.User)))
	}
	if pass := envValueFrom(spec.EnvFile, spec.PasswordEnv); pass != "" {
		parts = append(parts, "password="+pqQuoteValue(pass))
	}
	if strings.TrimSpace(spec.SSLMode) != "" {
		parts = append(parts, "sslmode="+pqQuoteValue(strings.TrimSpace(spec.SSLMode)))
	}
	return strings.Join(parts, " ")
}

func postgresHost(spec Spec) string {
	if strings.TrimSpace(spec.Host) != "" {
		return strings.TrimSpace(spec.Host)
	}
	if host := envValueFrom(spec.EnvFile, spec.HostEnv); host != "" {
		return host
	}
	if strings.TrimSpace(spec.PasswordEnv) == "" {
		if info, err := os.Stat("/var/run/postgresql"); err == nil && info.IsDir() {
			return "/var/run/postgresql"
		}
	}
	return ""
}

func postgresPort(spec Spec) int {
	if spec.Port > 0 {
		return spec.Port
	}
	value := envValueFrom(spec.EnvFile, spec.PortEnv)
	if value == "" {
		return 0
	}
	port, err := strconv.Atoi(value)
	if err != nil || port <= 0 || port > 65535 {
		return 0
	}
	return port
}

func runPostgresCommand(ctx context.Context, spec Spec, name string, args ...string) error {
	name = first(name, "pg_restore")
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = append(os.Environ(), pgEnv(spec)...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return fmt.Errorf("%s: %w: %s", name, err, msg)
		}
		return fmt.Errorf("%s: %w", name, err)
	}
	return nil
}

func pgEnv(spec Spec) []string {
	var env []string
	if strings.TrimSpace(spec.Host) != "" {
		env = append(env, "PGHOST="+strings.TrimSpace(spec.Host))
	} else if host := envValueFrom(spec.EnvFile, spec.HostEnv); host != "" {
		env = append(env, "PGHOST="+host)
	}
	if port := postgresPort(spec); port > 0 {
		env = append(env, "PGPORT="+strconv.Itoa(port))
	}
	if strings.TrimSpace(spec.User) != "" {
		env = append(env, "PGUSER="+strings.TrimSpace(spec.User))
	}
	if strings.TrimSpace(spec.SSLMode) != "" {
		env = append(env, "PGSSLMODE="+strings.TrimSpace(spec.SSLMode))
	}
	if pass := envValueFrom(spec.EnvFile, spec.PasswordEnv); pass != "" {
		env = append(env, "PGPASSWORD="+pass)
	}
	return env
}

func (r Runner) reexecAsUser(ctx context.Context, req ResourceRequest, runAs string) (Result, error) {
	if err := prepareRunAsPath(req); err != nil {
		return failureResult(req, Spec{}, err), err
	}
	raw, err := json.Marshal(req)
	if err != nil {
		return failureResult(req, Spec{}, err), err
	}
	exe := strings.TrimSpace(r.Executable)
	if exe == "" {
		exe, err = os.Executable()
		if err != nil {
			return failureResult(req, Spec{}, err), err
		}
	}
	encoded := base64.StdEncoding.EncodeToString(raw)
	cmd := exec.CommandContext(ctx, "runuser", "-u", runAs, "--", exe, "postgres-resource-exec", "--request-b64", encoded)
	cmd.Env = os.Environ()
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	if r.Stderr != nil {
		cmd.Stderr = io.MultiWriter(&stderr, r.Stderr)
	} else {
		cmd.Stderr = &stderr
	}
	if err := cmd.Run(); err != nil {
		parsed, parseErr := parseResult(stdout.Bytes())
		if parseErr == nil && parsed.Status != "" {
			return parsed, err
		}
		return failureResult(req, Spec{}, fmt.Errorf("run as %s: %w: %s", runAs, err, strings.TrimSpace(stderr.String()))), err
	}
	parsed, err := parseResult(stdout.Bytes())
	if err != nil {
		return failureResult(req, Spec{}, err), err
	}
	return parsed, nil
}

func prepareRunAsPath(req ResourceRequest) error {
	var spec Spec
	_ = json.Unmarshal(req.Spec, &spec)
	if req.NodeKind != "postgres.backup.run" {
		return nil
	}
	path := strings.TrimSpace(spec.Backup.Path)
	if path == "" && strings.TrimSpace(spec.Backup.File) != "" {
		path = filepath.Dir(strings.TrimSpace(spec.Backup.File))
	}
	if path == "" {
		return nil
	}
	if err := os.MkdirAll(path, 0o755); err != nil {
		return err
	}
	u, err := user.Lookup(strings.TrimSpace(spec.RunAsUser))
	if err != nil {
		return nil
	}
	uid, uidErr := strconv.Atoi(u.Uid)
	gid, gidErr := strconv.Atoi(u.Gid)
	if uidErr == nil && gidErr == nil {
		_ = os.Chown(path, uid, gid)
	}
	return nil
}

func shouldReexecAsUser(runAs string) bool {
	runAs = strings.TrimSpace(runAs)
	if runAs == "" {
		return false
	}
	current, err := user.Current()
	if err != nil {
		return true
	}
	return current.Username != runAs
}

func parseResult(raw []byte) (Result, error) {
	lines := bytes.Split(bytes.TrimSpace(raw), []byte("\n"))
	for i := len(lines) - 1; i >= 0; i-- {
		line := bytes.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		var result Result
		if err := json.Unmarshal(line, &result); err == nil {
			return result, nil
		}
	}
	return Result{}, fmt.Errorf("postgres resource helper returned no result")
}

func failureResult(req ResourceRequest, spec Spec, err error) Result {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	return Result{
		APIVersion:  ResultAPIVersion,
		Kind:        ResultKind,
		NodeID:      strings.TrimSpace(req.NodeID),
		RunID:       strings.TrimSpace(req.RunID),
		NodeKind:    strings.TrimSpace(req.NodeKind),
		Status:      "failed",
		Database:    strings.TrimSpace(spec.Database),
		Message:     err.Error(),
		Verify:      Verify{Status: "failed", Checks: map[string]any{"error": err.Error()}},
		StartedAt:   now,
		CompletedAt: now,
	}
}

func WriteResult(w io.Writer, result Result) error {
	enc := json.NewEncoder(w)
	return enc.Encode(result)
}

func planFromSQL(sqls []string) Plan {
	if len(sqls) == 0 {
		return Plan{Action: "noop", SQLDigest: digestStrings(nil)}
	}
	return Plan{Action: "apply", SQLDigest: digestStrings(sqls), SQL: sqls}
}

func digestStrings(values []string) string {
	normalized := append([]string(nil), values...)
	sort.Strings(normalized)
	raw := strings.Join(normalized, "\n")
	sum := sha256.Sum256([]byte(raw))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func digestValue(value string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(value)))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func fileDigest(path string) (int64, string, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, "", err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return 0, "", err
	}
	return n, hex.EncodeToString(h.Sum(nil)), nil
}

func writeBackupManifest(path string, runID string, nodeID string, dbName string, backup BackupResult) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	payload := map[string]any{
		"apiVersion":   "torque.dev/postgres-backup-manifest/v1",
		"kind":         "PostgresBackupManifest",
		"id":           backup.ID,
		"runId":        runID,
		"nodeId":       nodeID,
		"database":     dbName,
		"file":         backup.File,
		"manifestPath": backup.ManifestPath,
		"catalogPath":  backup.CatalogPath,
		"sha256":       backup.Sha256,
		"bytes":        backup.Bytes,
		"createdAt":    time.Now().UTC().Format(time.RFC3339Nano),
	}
	if backup.Store != nil {
		payload["store"] = backup.Store
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(path, append(raw, '\n'), 0o644)
}

func manifestBackupFile(path string) string {
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var payload struct {
		File string `json:"file"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}
	return strings.TrimSpace(payload.File)
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func normalizedPrivileges(values []string, fallback string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.ToUpper(strings.TrimSpace(part))
			if part == "" {
				continue
			}
			if !privilegePattern.MatchString(part) {
				continue
			}
			out = append(out, part)
		}
	}
	if len(out) == 0 {
		out = []string{strings.ToUpper(strings.TrimSpace(fallback))}
	}
	sort.Strings(out)
	return out
}

func sortedMapKeys(values map[string]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, strings.TrimSpace(key))
	}
	sort.Strings(keys)
	return keys
}

func quoteQualifiedIdent(value string) string {
	parts := strings.Split(strings.TrimSpace(value), ".")
	for i := range parts {
		parts[i] = pq.QuoteIdentifier(parts[i])
	}
	return strings.Join(parts, ".")
}

func pqQuoteValue(value string) string {
	if value == "" {
		return "''"
	}
	return "'" + strings.ReplaceAll(value, "'", `\'`) + "'"
}

func envValue(name string) string {
	return envValueFrom("", name)
}

func envValueFrom(envFile string, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if values, err := readEnvFile(strings.TrimSpace(envFile)); err == nil {
		if value, ok := values[name]; ok {
			return strings.TrimSpace(value)
		}
	}
	return os.Getenv(name)
}

func first(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func safeFileToken(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Now().UTC().Format("20060102T150405Z")
	}
	return safeFilePattern.ReplaceAllString(value, "_")
}

var (
	privilegePattern   = regexp.MustCompile(`^[A-Z_ ]+$`)
	settingNamePattern = regexp.MustCompile(`^[A-Za-z0-9_.]+$`)
	safeFilePattern    = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)
)
