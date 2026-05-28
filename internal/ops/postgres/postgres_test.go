package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestDefaultLockPolicyScopesMutableResources(t *testing.T) {
	policy := DefaultLockPolicy("lab", "postgres.role.ensure", Spec{
		Database: "keycloak",
		Role:     RoleSpec{Name: "torque_auditor"},
	})
	if policy == nil || !policy.Enabled {
		t.Fatalf("expected enabled lock policy, got %#v", policy)
	}
	if policy.TimeoutMillis != defaultLockTimeoutMillis || policy.OnFailure != "block" {
		t.Fatalf("lock defaults = %#v", policy)
	}
	for _, want := range []string{
		"tenant=lab",
		"database=postgres",
		"kind=postgres.role.ensure",
		"identity=torque_auditor",
	} {
		if !strings.Contains(policy.Key, want) {
			t.Fatalf("lock key %q missing %q", policy.Key, want)
		}
	}
	if got := digestValue(policy.Key); !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("lock digest = %q", got)
	}
	if advisoryLockID(policy.Key) == 0 {
		t.Fatalf("advisory lock id should be stable and non-zero")
	}
}

func TestDefaultLockPolicySkipsReadOnlyVerifyResources(t *testing.T) {
	for _, kind := range []string{"postgres.replication.verify", "postgres.backup.verify"} {
		if policy := DefaultLockPolicy("lab", kind, Spec{}); policy != nil {
			t.Fatalf("%s lock policy = %#v, want nil", kind, policy)
		}
	}
}

func TestTransactionUnsupportedReceiptExplainsCrossTransactionResources(t *testing.T) {
	cases := map[string]string{
		"postgres.database.ensure": "CREATE DATABASE",
		"postgres.config.ensure":   "ALTER SYSTEM",
		"postgres.backup.run":      "pg_dump",
		"postgres.restore.drill":   "pg_restore",
	}
	for kind, want := range cases {
		receipt := transactionUnsupportedReceipt(kind)
		if receipt == nil || receipt.Supported {
			t.Fatalf("%s receipt = %#v, want unsupported transaction", kind, receipt)
		}
		if !strings.Contains(receipt.Reason, want) {
			t.Fatalf("%s reason = %q, want %q", kind, receipt.Reason, want)
		}
	}
}

func TestExecuteBackupRunWritesDurableCatalog(t *testing.T) {
	root := t.TempDir()
	pgDump := writeFakePGDump(t, root, "torque-backup-fixture\n")
	file := filepath.Join(root, "keycloak.dump")
	manifest := filepath.Join(root, "keycloak.manifest.json")
	catalog := filepath.Join(root, "keycloak.catalog.json")
	result := Result{RunID: "run-catalog", NodeID: "postgres.backup.run/keycloak"}
	err := executeBackupRun(context.Background(), &result, Spec{
		Database:      "keycloak",
		PGDumpCommand: pgDump,
		Backup: BackupSpec{
			Database:     "keycloak",
			ID:           "keycloak/manual",
			Path:         root,
			File:         file,
			ManifestPath: manifest,
			CatalogPath:  catalog,
		},
	})
	if err != nil {
		t.Fatalf("backup run: %v", err)
	}
	if result.Backup == nil || result.Backup.ID != "keycloak/manual" || result.Backup.CatalogPath != catalog {
		t.Fatalf("backup result missing catalog identity: %#v", result.Backup)
	}
	raw, err := os.ReadFile(catalog)
	if err != nil {
		t.Fatalf("read catalog: %v", err)
	}
	var record BackupCatalogRecord
	if err := json.Unmarshal(raw, &record); err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if record.ID != "keycloak/manual" || record.Status != "succeeded" || record.Sha256 == "" || record.Bytes == 0 {
		t.Fatalf("catalog record = %#v", record)
	}
	if record.RunID != "run-catalog" || record.NodeID != "postgres.backup.run/keycloak" {
		t.Fatalf("catalog source = %#v", record)
	}
}

func TestExecuteBackupRunEmitsProgressLines(t *testing.T) {
	root := t.TempDir()
	pgDump := writeFakePGDump(t, root, "torque-progress-fixture\n")
	file := filepath.Join(root, "keycloak.dump")
	manifest := filepath.Join(root, "keycloak.manifest.json")
	catalog := filepath.Join(root, "keycloak.catalog.json")
	result := Result{RunID: "run-progress", NodeID: "postgres.backup.run/keycloak"}
	var progress bytes.Buffer
	ctx := withProgressReporter(context.Background(), nil, &progress)
	if err := executeBackupRun(ctx, &result, Spec{
		Database:      "keycloak",
		PGDumpCommand: pgDump,
		Backup: BackupSpec{
			Database:     "keycloak",
			ID:           "keycloak/progress",
			Path:         root,
			File:         file,
			ManifestPath: manifest,
			CatalogPath:  catalog,
		},
	}); err != nil {
		t.Fatalf("backup run: %v", err)
	}
	logs := progress.String()
	for _, want := range []string{
		"backup: running pg_dump",
		"backup: wrote " + file,
		"backup: manifest " + manifest,
		"backup: catalog " + catalog,
	} {
		if !strings.Contains(logs, want) {
			t.Fatalf("expected progress log %q in:\n%s", want, logs)
		}
	}
}

func TestNormalizeBackupStoreUsesEnvBackedTarget(t *testing.T) {
	t.Setenv("TORQUE_TEST_S3_REF", "s3://torque-env-bucket/backups/")
	t.Setenv("TORQUE_TEST_S3_PREFIX", "override-prefix")
	t.Setenv("TORQUE_TEST_S3_REGION", "eu-west-1")
	t.Setenv("TORQUE_TEST_S3_ENDPOINT", "https://s3.example.test")
	store, err := normalizeBackupStore(BackupStoreSpec{
		Type:        "s3",
		RefEnv:      "TORQUE_TEST_S3_REF",
		PrefixEnv:   "TORQUE_TEST_S3_PREFIX",
		RegionEnv:   "TORQUE_TEST_S3_REGION",
		EndpointEnv: "TORQUE_TEST_S3_ENDPOINT",
	})
	if err != nil {
		t.Fatalf("normalize env store: %v", err)
	}
	if store.Bucket != "torque-env-bucket" || store.Prefix != "override-prefix/" || store.Region != "eu-west-1" || store.Endpoint != "https://s3.example.test" {
		t.Fatalf("env store = %#v", store)
	}
}

func TestNormalizeBackupStoreUsesEnvFileBackedTarget(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "manual.env")
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		"export TORQUE_TEST_S3_BUCKET=env-file-bucket",
		"export TORQUE_TEST_S3_PREFIX='env-file-prefix'",
		"export TORQUE_TEST_S3_REGION=eu-west-1",
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("TORQUE_TEST_S3_BUCKET", "process-bucket")
	store, err := normalizeBackupStore(BackupStoreSpec{
		Type:      "s3",
		BucketEnv: "TORQUE_TEST_S3_BUCKET",
		PrefixEnv: "TORQUE_TEST_S3_PREFIX",
		RegionEnv: "TORQUE_TEST_S3_REGION",
	}, envPath)
	if err != nil {
		t.Fatalf("normalize env-file store: %v", err)
	}
	if store.Bucket != "env-file-bucket" || store.Prefix != "env-file-prefix/" || store.Region != "eu-west-1" {
		t.Fatalf("env-file store = %#v", store)
	}
}

func TestPostgresHostAndPortUseEnvBackedTarget(t *testing.T) {
	t.Setenv("TORQUE_TEST_PGHOST", "rds.example.test")
	t.Setenv("TORQUE_TEST_PGPORT", "6543")
	spec := Spec{HostEnv: "TORQUE_TEST_PGHOST", PortEnv: "TORQUE_TEST_PGPORT", User: "postgres"}
	if got := postgresHost(spec); got != "rds.example.test" {
		t.Fatalf("postgres host = %q", got)
	}
	if got := postgresPort(spec); got != 6543 {
		t.Fatalf("postgres port = %d", got)
	}
	if got := conninfo(spec, "keycloak"); !strings.Contains(got, "host='rds.example.test'") || !strings.Contains(got, "port=6543") {
		t.Fatalf("conninfo = %q", got)
	}
}

func TestPostgresHostAndPortUseEnvFileBackedTarget(t *testing.T) {
	root := t.TempDir()
	envPath := filepath.Join(root, "manual.env")
	if err := os.WriteFile(envPath, []byte(strings.Join([]string{
		"export TORQUE_TEST_PGHOST=env-file-rds.example.test",
		"export TORQUE_TEST_PGPORT=6544",
		"export TORQUE_TEST_PGPASSWORD='secret-value'",
		"",
	}, "\n")), 0o600); err != nil {
		t.Fatalf("write env file: %v", err)
	}
	t.Setenv("TORQUE_TEST_PGHOST", "process-rds.example.test")
	spec := Spec{EnvFile: envPath, HostEnv: "TORQUE_TEST_PGHOST", PortEnv: "TORQUE_TEST_PGPORT", User: "postgres", PasswordEnv: "TORQUE_TEST_PGPASSWORD"}
	if got := postgresHost(spec); got != "env-file-rds.example.test" {
		t.Fatalf("postgres host = %q", got)
	}
	if got := postgresPort(spec); got != 6544 {
		t.Fatalf("postgres port = %d", got)
	}
	got := conninfo(spec, "keycloak")
	if !strings.Contains(got, "host='env-file-rds.example.test'") || !strings.Contains(got, "port=6544") || !strings.Contains(got, "password='secret-value'") {
		t.Fatalf("conninfo = %q", got)
	}
}

func TestExecuteBackupRunUploadsToS3WithMultipartSession(t *testing.T) {
	root := t.TempDir()
	fake := newFakeS3Server(t)
	t.Setenv("TORQUE_TEST_AWS_ACCESS_KEY_ID", "test")
	t.Setenv("TORQUE_TEST_AWS_SECRET_ACCESS_KEY", "test")
	pgDump := writeFakePGDump(t, root, "0123456789abcdef")
	file := filepath.Join(root, "keycloak.dump")
	manifest := filepath.Join(root, "keycloak.manifest.json")
	catalog := filepath.Join(root, "keycloak.catalog.json")
	session := filepath.Join(root, "upload-session.json")
	spec := Spec{
		Database:      "keycloak",
		PGDumpCommand: pgDump,
		Backup: BackupSpec{
			Database:     "keycloak",
			ID:           "keycloak/s3-proof",
			Path:         root,
			File:         file,
			ManifestPath: manifest,
			CatalogPath:  catalog,
			Store: BackupStoreSpec{
				Type:               "s3",
				Bucket:             "torque-test",
				Prefix:             "postgres",
				Region:             "us-east-1",
				Endpoint:           fake.URL,
				PathStyle:          true,
				PartSizeBytes:      5,
				SessionPath:        session,
				AccessKeyIDEnv:     "TORQUE_TEST_AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "TORQUE_TEST_AWS_SECRET_ACCESS_KEY",
			},
		},
	}
	first := Result{RunID: "run-s3", NodeID: "postgres.backup.run/keycloak"}
	if err := executeBackupRun(context.Background(), &first, spec); err != nil {
		t.Fatalf("first backup run: %v", err)
	}
	if first.Backup == nil || first.Backup.Store == nil {
		t.Fatalf("missing backup store result: %#v", first.Backup)
	}
	if !first.Backup.Store.Multipart || first.Backup.Store.Parts != 4 || first.Backup.Store.URI != "s3://torque-test/postgres/base/keycloak/s3-proof/keycloak.dump" {
		t.Fatalf("store result = %#v", first.Backup.Store)
	}
	if _, err := os.Stat(session); !os.IsNotExist(err) {
		t.Fatalf("session file should be removed after complete, stat err=%v", err)
	}
	for _, key := range []string{
		"postgres/base/keycloak/s3-proof/keycloak.dump",
		"postgres/base/keycloak/s3-proof/keycloak.manifest.json",
		"postgres/catalog/keycloak/s3-proof/keycloak.catalog.json",
	} {
		if !fake.HasObject("torque-test", key) {
			t.Fatalf("fake S3 missing object %s", key)
		}
	}
	second := Result{RunID: "run-s3", NodeID: "postgres.backup.run/keycloak"}
	if err := executeBackupRun(context.Background(), &second, spec); err != nil {
		t.Fatalf("second backup run: %v", err)
	}
	if second.Backup == nil || second.Backup.Store == nil || !second.Backup.Store.Resumed || second.Backup.Store.Uploaded {
		t.Fatalf("second run should reuse existing durable object, got %#v", second.Backup)
	}
	if err := os.Remove(file); err != nil {
		t.Fatalf("remove local backup before verify: %v", err)
	}
	verifySpec := spec
	verifySpec.PGRestoreCommand = writeFakePGRestoreList(t, root)
	verify := Result{RunID: "run-s3-verify", NodeID: "postgres.backup.verify/keycloak"}
	if err := executeBackupVerify(context.Background(), &verify, verifySpec); err != nil {
		t.Fatalf("backup verify from S3: %v", err)
	}
	if verify.Backup == nil || verify.Backup.Sha256 != first.Backup.Sha256 || !fileExists(file) {
		t.Fatalf("verify should download and prove durable backup, got %#v", verify.Backup)
	}
}

func TestExecuteBackupRunResumesInterruptedMultipartSession(t *testing.T) {
	root := t.TempDir()
	fake := newFakeS3Server(t)
	fake.FailUploadPart(2)
	t.Setenv("TORQUE_TEST_AWS_ACCESS_KEY_ID", "test")
	t.Setenv("TORQUE_TEST_AWS_SECRET_ACCESS_KEY", "test")
	spec := Spec{
		Database:      "keycloak",
		PGDumpCommand: writeFakePGDump(t, root, "0123456789abcdef"),
		Backup: BackupSpec{
			Database:     "keycloak",
			ID:           "keycloak/resume-proof",
			Path:         root,
			File:         filepath.Join(root, "keycloak.dump"),
			ManifestPath: filepath.Join(root, "keycloak.manifest.json"),
			CatalogPath:  filepath.Join(root, "keycloak.catalog.json"),
			Store: BackupStoreSpec{
				Type:               "s3",
				Bucket:             "torque-test",
				Prefix:             "postgres",
				Region:             "us-east-1",
				Endpoint:           fake.URL,
				PathStyle:          true,
				PartSizeBytes:      5,
				SessionPath:        filepath.Join(root, "upload-session.json"),
				AccessKeyIDEnv:     "TORQUE_TEST_AWS_ACCESS_KEY_ID",
				SecretAccessKeyEnv: "TORQUE_TEST_AWS_SECRET_ACCESS_KEY",
			},
		},
	}
	first := Result{RunID: "run-s3-resume", NodeID: "postgres.backup.run/keycloak"}
	if err := executeBackupRun(context.Background(), &first, spec); err == nil {
		t.Fatalf("first backup run should fail on injected part upload error")
	}
	raw, err := os.ReadFile(spec.Backup.Store.SessionPath)
	if err != nil {
		t.Fatalf("read interrupted upload session: %v", err)
	}
	var session s3UploadSession
	if err := json.Unmarshal(raw, &session); err != nil {
		t.Fatalf("parse interrupted upload session: %v", err)
	}
	if session.UploadID == "" || len(session.Parts) != 1 || session.Parts[0].Number != 1 {
		t.Fatalf("interrupted session = %#v", session)
	}

	fake.ClearUploadPartFailure()
	second := Result{RunID: "run-s3-resume", NodeID: "postgres.backup.run/keycloak"}
	if err := executeBackupRun(context.Background(), &second, spec); err != nil {
		t.Fatalf("resumed backup run: %v", err)
	}
	if second.Backup == nil || second.Backup.Store == nil || !second.Backup.Store.Multipart || !second.Backup.Store.Resumed || second.Backup.Store.Parts != 4 {
		t.Fatalf("resumed store result = %#v", second.Backup)
	}
	if _, err := os.Stat(spec.Backup.Store.SessionPath); !os.IsNotExist(err) {
		t.Fatalf("session file should be removed after resumed complete, stat err=%v", err)
	}
	if !fake.HasObject("torque-test", "postgres/base/keycloak/resume-proof/keycloak.dump") {
		t.Fatalf("fake S3 missing resumed backup object")
	}
}

func TestExecuteBackupRunUploadsToLiveS3(t *testing.T) {
	if os.Getenv("TORQUE_POSTGRES_S3_E2E") != "1" {
		t.Skip("set TORQUE_POSTGRES_S3_E2E=1 and TORQUE_POSTGRES_S3_E2E_BUCKET to run live S3 cleanup E2E")
	}
	bucket := strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_BUCKET"))
	if bucket == "" {
		t.Skip("TORQUE_POSTGRES_S3_E2E_BUCKET is required for live S3 cleanup E2E")
	}
	ctx := context.Background()
	root := t.TempDir()
	id := "live-s3/" + safeFileToken(strconv.FormatInt(time.Now().UnixNano(), 10))
	prefix := strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_PREFIX"))
	if prefix == "" {
		prefix = "torque-postgres-e2e"
	}
	spec := Spec{
		Database:      "keycloak",
		PGDumpCommand: writeFakePGDumpBytes(t, root, 6*1024*1024),
		Backup: BackupSpec{
			Database:     "keycloak",
			ID:           id,
			Path:         root,
			File:         filepath.Join(root, "keycloak.dump"),
			ManifestPath: filepath.Join(root, "keycloak.manifest.json"),
			CatalogPath:  filepath.Join(root, "keycloak.catalog.json"),
			Store: BackupStoreSpec{
				Type:               "s3",
				Bucket:             bucket,
				Prefix:             prefix,
				Region:             first(strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_REGION")), strings.TrimSpace(os.Getenv("AWS_REGION")), strings.TrimSpace(os.Getenv("AWS_DEFAULT_REGION")), "us-east-1"),
				Endpoint:           strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_ENDPOINT")),
				PathStyle:          strings.EqualFold(strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_PATH_STYLE")), "true"),
				PartSizeBytes:      5 * 1024 * 1024,
				SessionPath:        filepath.Join(root, "live-upload-session.json"),
				AccessKeyIDEnv:     strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_ACCESS_KEY_ID_ENV")),
				SecretAccessKeyEnv: strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_SECRET_ACCESS_KEY_ENV")),
				SessionTokenEnv:    strings.TrimSpace(os.Getenv("TORQUE_POSTGRES_S3_E2E_SESSION_TOKEN_ENV")),
			},
		},
	}
	store, err := normalizeBackupStore(spec.Backup.Store)
	if err != nil {
		t.Fatalf("normalize live S3 store: %v", err)
	}
	client, err := newBackupS3Client(ctx, store)
	if err != nil {
		t.Fatalf("create live S3 client: %v", err)
	}
	keys := []string{
		backupObjectKey(store, id, spec.Backup.File),
		backupManifestKey(store, id, spec.Backup.ManifestPath),
		backupCatalogKey(store, id, spec.Backup.CatalogPath),
	}
	t.Cleanup(func() {
		for _, key := range keys {
			_, _ = client.DeleteObject(context.Background(), &s3.DeleteObjectInput{Bucket: aws.String(bucket), Key: aws.String(key)})
		}
	})
	result := Result{RunID: "run-live-s3", NodeID: "postgres.backup.run/keycloak"}
	if err := executeBackupRun(ctx, &result, spec); err != nil {
		t.Fatalf("live S3 backup run: %v", err)
	}
	if result.Backup == nil || result.Backup.Store == nil {
		t.Fatalf("missing live S3 backup store result: %#v", result.Backup)
	}
	if !result.Backup.Store.Multipart || result.Backup.Store.Parts != 2 || result.Backup.Store.URI == "" {
		t.Fatalf("live S3 backup store result = %#v", result.Backup.Store)
	}
	head, err := client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(keys[0])})
	if err != nil {
		t.Fatalf("head live S3 backup object: %v", err)
	}
	if head.ContentLength == nil || *head.ContentLength != result.Backup.Bytes {
		t.Fatalf("live S3 object size = %v, want %d", head.ContentLength, result.Backup.Bytes)
	}
	if got := strings.TrimSpace(head.Metadata["sha256"]); got != result.Backup.Sha256 {
		t.Fatalf("live S3 object sha metadata = %q, want %q", got, result.Backup.Sha256)
	}
}

func writeFakePGDump(t *testing.T, dir string, payload string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-pg-dump.sh")
	script := `#!/bin/sh
set -eu
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-f" ]; then
    out="$2"
    shift 2
  else
    shift
  fi
done
if [ -z "$out" ]; then
  echo "missing -f" >&2
  exit 2
fi
printf '%s' ` + shellSingleQuote(payload) + ` > "$out"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pg_dump: %v", err)
	}
	return path
}

func writeFakePGDumpBytes(t *testing.T, dir string, bytes int64) string {
	t.Helper()
	path := filepath.Join(dir, "fake-pg-dump-bytes.sh")
	script := `#!/bin/sh
set -eu
out=""
while [ "$#" -gt 0 ]; do
  if [ "$1" = "-f" ]; then
    out="$2"
    shift 2
  else
    shift
  fi
done
if [ -z "$out" ]; then
  echo "missing -f" >&2
  exit 2
fi
head -c ` + strconv.FormatInt(bytes, 10) + ` /dev/zero > "$out"
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake byte pg_dump: %v", err)
	}
	return path
}

func writeFakePGRestoreList(t *testing.T, dir string) string {
	t.Helper()
	path := filepath.Join(dir, "fake-pg-restore.sh")
	script := `#!/bin/sh
set -eu
if [ "$#" -eq 2 ] && [ "$1" = "--list" ] && [ -s "$2" ]; then
  echo "toc"
  exit 0
fi
echo "expected --list <backup>" >&2
exit 2
`
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake pg_restore: %v", err)
	}
	return path
}

func shellSingleQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'"'"'`) + "'"
}

type fakeS3Server struct {
	URL      string
	server   *httptest.Server
	mu       sync.Mutex
	objects  map[string]fakeS3Object
	uploads  map[string]*fakeS3Upload
	nextID   int
	failPart int
}

type fakeS3Object struct {
	body     []byte
	etag     string
	metadata map[string]string
}

type fakeS3Upload struct {
	bucket   string
	key      string
	metadata map[string]string
	parts    map[int][]byte
	etags    map[int]string
}

func newFakeS3Server(t *testing.T) *fakeS3Server {
	t.Helper()
	fake := &fakeS3Server{
		objects: map[string]fakeS3Object{},
		uploads: map[string]*fakeS3Upload{},
	}
	fake.server = httptest.NewServer(http.HandlerFunc(fake.ServeHTTP))
	fake.URL = fake.server.URL
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeS3Server) HasObject(bucket string, key string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	_, ok := f.objects[bucket+"/"+key]
	return ok
}

func (f *fakeS3Server) FailUploadPart(number int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPart = number
}

func (f *fakeS3Server) ClearUploadPartFailure() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.failPart = 0
}

func (f *fakeS3Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	bucket, key := fakeS3BucketKey(r)
	query := r.URL.Query()
	switch {
	case r.Method == http.MethodPost && query.Get("uploadId") != "":
		f.completeMultipart(w, bucket, key, query.Get("uploadId"))
	case r.Method == http.MethodPost:
		if _, ok := query["uploads"]; ok {
			f.createMultipart(w, r, bucket, key)
			return
		}
		http.Error(w, "unsupported post", http.StatusBadRequest)
	case r.Method == http.MethodPut && query.Get("uploadId") != "" && query.Get("partNumber") != "":
		f.uploadPart(w, r, query.Get("uploadId"), query.Get("partNumber"))
	case r.Method == http.MethodPut:
		f.putObject(w, r, bucket, key)
	case r.Method == http.MethodHead:
		f.headObject(w, bucket, key)
	case r.Method == http.MethodGet && query.Get("uploadId") != "":
		f.listParts(w, query.Get("uploadId"))
	case r.Method == http.MethodGet:
		f.getObject(w, bucket, key)
	default:
		http.Error(w, "unsupported", http.StatusBadRequest)
	}
}

func fakeS3BucketKey(r *http.Request) (string, string) {
	trimmed := strings.TrimPrefix(r.URL.Path, "/")
	bucket, key, _ := strings.Cut(trimmed, "/")
	return bucket, key
}

func (f *fakeS3Server) createMultipart(w http.ResponseWriter, r *http.Request, bucket string, key string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.nextID++
	uploadID := fmt.Sprintf("upload-%d", f.nextID)
	f.uploads[uploadID] = &fakeS3Upload{
		bucket:   bucket,
		key:      key,
		metadata: fakeS3Metadata(r),
		parts:    map[int][]byte{},
		etags:    map[int]string{},
	}
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<InitiateMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, bucket, key, uploadID)
}

func (f *fakeS3Server) uploadPart(w http.ResponseWriter, r *http.Request, uploadID string, partNumber string) {
	number, _ := strconv.Atoi(partNumber)
	f.mu.Lock()
	failPart := f.failPart
	f.mu.Unlock()
	if failPart == number {
		http.Error(w, "injected upload part failure", http.StatusInternalServerError)
		return
	}
	body, _ := io.ReadAll(r.Body)
	etag := fmt.Sprintf(`"part-%d-%d"`, number, len(body))
	f.mu.Lock()
	defer f.mu.Unlock()
	upload := f.uploads[uploadID]
	if upload == nil {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}
	upload.parts[number] = body
	upload.etags[number] = etag
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3Server) completeMultipart(w http.ResponseWriter, bucket string, key string, uploadID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	upload := f.uploads[uploadID]
	if upload == nil {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}
	var numbers []int
	for number := range upload.parts {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	var body []byte
	for _, number := range numbers {
		body = append(body, upload.parts[number]...)
	}
	etag := fmt.Sprintf(`"complete-%d"`, len(body))
	f.objects[bucket+"/"+key] = fakeS3Object{body: body, etag: etag, metadata: upload.metadata}
	delete(f.uploads, uploadID)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprintf(w, `<CompleteMultipartUploadResult><Bucket>%s</Bucket><Key>%s</Key><ETag>%s</ETag></CompleteMultipartUploadResult>`, bucket, key, etag)
}

func (f *fakeS3Server) listParts(w http.ResponseWriter, uploadID string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	upload := f.uploads[uploadID]
	if upload == nil {
		http.Error(w, "upload not found", http.StatusNotFound)
		return
	}
	var numbers []int
	for number := range upload.parts {
		numbers = append(numbers, number)
	}
	sort.Ints(numbers)
	w.Header().Set("Content-Type", "application/xml")
	_, _ = fmt.Fprint(w, `<ListPartsResult><IsTruncated>false</IsTruncated>`)
	for _, number := range numbers {
		_, _ = fmt.Fprintf(w, `<Part><PartNumber>%d</PartNumber><ETag>%s</ETag><Size>%d</Size></Part>`, number, upload.etags[number], len(upload.parts[number]))
	}
	_, _ = fmt.Fprint(w, `</ListPartsResult>`)
}

func (f *fakeS3Server) putObject(w http.ResponseWriter, r *http.Request, bucket string, key string) {
	body, _ := io.ReadAll(r.Body)
	etag := fmt.Sprintf(`"put-%d"`, len(body))
	f.mu.Lock()
	f.objects[bucket+"/"+key] = fakeS3Object{body: body, etag: etag, metadata: fakeS3Metadata(r)}
	f.mu.Unlock()
	w.Header().Set("ETag", etag)
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3Server) headObject(w http.ResponseWriter, bucket string, key string) {
	f.mu.Lock()
	obj, ok := f.objects[bucket+"/"+key]
	f.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", obj.etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
	for key, value := range obj.metadata {
		w.Header().Set("x-amz-meta-"+key, value)
	}
	w.WriteHeader(http.StatusOK)
}

func (f *fakeS3Server) getObject(w http.ResponseWriter, bucket string, key string) {
	f.mu.Lock()
	obj, ok := f.objects[bucket+"/"+key]
	f.mu.Unlock()
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("ETag", obj.etag)
	w.Header().Set("Content-Length", strconv.Itoa(len(obj.body)))
	_, _ = w.Write(obj.body)
}

func fakeS3Metadata(r *http.Request) map[string]string {
	out := map[string]string{}
	for key, values := range r.Header {
		lower := strings.ToLower(key)
		if strings.HasPrefix(lower, "x-amz-meta-") && len(values) > 0 {
			out[strings.TrimPrefix(lower, "x-amz-meta-")] = values[0]
		}
	}
	return out
}
