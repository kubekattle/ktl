package chartarchive

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestVerifyArchive_Succeeds(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("replicaCount: 1\n"), 0o644); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}

	outDir := t.TempDir()
	res, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	verified, err := VerifyArchive(context.Background(), res.ArchivePath)
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if verified.ContentSHA256 == "" || verified.FileCount == 0 {
		t.Fatalf("unexpected verify result: %#v", verified)
	}
}

func TestVerifyArchive_DetectsTamper(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("replicaCount: 1\n"), 0o644); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}

	outDir := t.TempDir()
	res, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	db, err := sql.Open("sqlite", res.ArchivePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`UPDATE ktl_chart_files SET data = ? WHERE path = 'values.yaml'`, []byte("tampered\n")); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if _, err := VerifyArchive(context.Background(), res.ArchivePath); err == nil {
		t.Fatalf("expected verify to fail")
	}
}
