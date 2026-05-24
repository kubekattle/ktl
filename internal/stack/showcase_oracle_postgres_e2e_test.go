package stack

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

func TestShowcaseOraclePostgresK8s_RunAndExport(t *testing.T) {
	srcRoot := filepath.Clean(filepath.Join("..", "..", "docs", "showcase", "oracle-postgres-k8s"))
	root := t.TempDir()
	copyDir(t, srcRoot, root)
	_ = os.RemoveAll(filepath.Join(root, ".torque"))

	stackBytes, err := os.ReadFile(filepath.Join(root, "stack.sqlite.yaml"))
	if err != nil {
		t.Fatalf("read sqlite stack fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "stack.yaml"), stackBytes, 0o644); err != nil {
		t.Fatalf("write stack.yaml: %v", err)
	}

	runtimeDir := filepath.Join(root, "runtime")
	if err := os.MkdirAll(runtimeDir, 0o755); err != nil {
		t.Fatalf("mkdir runtime: %v", err)
	}
	dbPath := filepath.Join(runtimeDir, "oracle-pg.sqlite")
	t.Setenv("TORQUE_ORACLE_PG_DSN", dbPath)

	u, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	p, err := Compile(u, CompileOptions{})
	if err != nil {
		t.Fatalf("Compile: %v", err)
	}

	var out, errOut bytes.Buffer
	if err := Run(context.Background(), RunOptions{
		Command:     "apply",
		Plan:        p,
		Concurrency: 1,
		Lock:        true,
	}, &out, &errOut); err != nil {
		t.Fatalf("Run: %v\nstderr=%s", err, errOut.String())
	}

	for _, rel := range []string{
		"runtime/oracle-export.json",
		"runtime/oracle-export.csv",
		"runtime/app-route-promote.json",
		"runtime/post-cutover-check.json",
	} {
		if _, err := os.Stat(filepath.Join(root, rel)); err != nil {
			t.Fatalf("expected %s: %v", rel, err)
		}
	}

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()

	var stageCount, shadowCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM oracle_stage_accounts`).Scan(&stageCount); err != nil {
		t.Fatalf("count oracle_stage_accounts: %v", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM accounts_shadow`).Scan(&shadowCount); err != nil {
		t.Fatalf("count accounts_shadow: %v", err)
	}
	if stageCount != 3 || shadowCount != 3 {
		t.Fatalf("unexpected row counts stage=%d shadow=%d", stageCount, shadowCount)
	}

	var routeState string
	if err := db.QueryRow(`SELECT CAST(live AS TEXT) || ',' || CAST(verified AS TEXT) || ',' || CAST(contracted AS TEXT) FROM route_flags WHERE name = 'apex'`).Scan(&routeState); err != nil {
		t.Fatalf("query route_flags: %v", err)
	}
	if routeState != "1,1,1" {
		t.Fatalf("unexpected route_flags state %q", routeState)
	}

	var auditPhase string
	if err := db.QueryRow(`SELECT phase FROM migration_audit WHERE entry = 'oracle-apex-to-postgres'`).Scan(&auditPhase); err != nil {
		t.Fatalf("query migration_audit: %v", err)
	}
	if auditPhase != "contracted" {
		t.Fatalf("unexpected migration audit phase %q", auditPhase)
	}

	runID, err := LoadMostRecentRun(root)
	if err != nil {
		t.Fatalf("LoadMostRecentRun: %v", err)
	}
	audit, err := GetRunAudit(context.Background(), RunAuditOptions{
		RootDir:          root,
		RunID:            runID,
		Verify:           true,
		IncludePlan:      true,
		IncludeArtifacts: true,
	})
	if err != nil {
		t.Fatalf("GetRunAudit: %v", err)
	}
	if !audit.Integrity.EventsOK || !audit.Integrity.RunDigestOK {
		t.Fatalf("unexpected audit integrity: %#v", audit.Integrity)
	}
	if len(audit.Artifacts) < 30 {
		t.Fatalf("expected rich artifact set, got %d", len(audit.Artifacts))
	}

	wantArtifacts := []string{
		"target-k8s/data-platform/cnpg-ready:script-output.json",
		"target-k8s/data-platform/change-window-approved:script-output.json",
		"target-k8s/data-platform/oracle-export:script-output.json",
		"target-k8s/data-platform/pg-restore:restore-point.json",
		"target-k8s/data-platform/pg-expand:schema-expand.json",
		"target-k8s/data-platform/pg-backfill:backfill.json",
		"target-k8s/data-platform/pg-verify:verify.json",
		"target-k8s/data-platform/pg-cutover:cutover.json",
		"target-k8s/data-platform/pg-contract:schema-contract.json",
		"target-k8s/data-platform/post-cutover-check:script-output.json",
	}
	joined := make([]string, 0, len(audit.Artifacts))
	for _, artifact := range audit.Artifacts {
		joined = append(joined, artifact.NodeID+":"+artifact.Name)
	}
	for _, want := range wantArtifacts {
		found := false
		for _, got := range joined {
			if got == want {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing artifact %s in %v", want, joined)
		}
	}

	bundlePath := filepath.Join(root, "runtime", "oracle-postgres-run.tgz")
	if _, err := ExportRunBundle(context.Background(), root, runID, bundlePath); err != nil {
		t.Fatalf("ExportRunBundle: %v", err)
	}
	extracted, err := ExtractBundleToTempDir(bundlePath)
	if err != nil {
		t.Fatalf("ExtractBundleToTempDir: %v", err)
	}
	defer os.RemoveAll(extracted)

	exportDB, err := sql.Open("sqlite", filepath.Join(extracted, "state.sqlite"))
	if err != nil {
		t.Fatalf("open exported sqlite: %v", err)
	}
	defer exportDB.Close()

	var artifactCount int
	if err := exportDB.QueryRow(`SELECT COUNT(*) FROM torque_stack_run_artifacts WHERE run_id = ?`, runID).Scan(&artifactCount); err != nil {
		t.Fatalf("count exported artifacts: %v", err)
	}
	if artifactCount != len(audit.Artifacts) {
		t.Fatalf("exported artifact count=%d want=%d", artifactCount, len(audit.Artifacts))
	}

	var manifest string
	if raw, err := os.ReadFile(filepath.Join(root, "runtime", "oracle-export.json")); err == nil {
		manifest = string(raw)
	}
	if !strings.Contains(manifest, `"sourceSystem":"apex-prod"`) {
		t.Fatalf("unexpected oracle export manifest %q", manifest)
	}
}
