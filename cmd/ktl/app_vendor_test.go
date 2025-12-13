package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestAppVendorLocalDirectory(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "vendir.lock.yml")
	configPath := filepath.Join(tmpDir, "vendir.yml")
	cacheDir := filepath.Join(tmpDir, ".vendir-cache")

	chartSrc := filepath.Join("..", "..", "testdata", "charts", "grafana")
	absChart, err := filepath.Abs(chartSrc)
	if err != nil {
		t.Fatalf("resolve chart path: %v", err)
	}

	config := fmt.Sprintf(`apiVersion: vendir.k14s.io/v1alpha1
kind: Config
directories:
- path: vendor
  contents:
  - path: grafana
    directory:
      path: %s
`, absChart)

	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatalf("write vendir config: %v", err)
	}

	t.Setenv("VENDIR_CACHE_DIR", cacheDir)

	cmd := newAppVendorCommand()
	var stdout, stderr bytes.Buffer
	cmd.SetOut(&stdout)
	cmd.SetErr(&stderr)
	cmd.SetArgs([]string{
		"sync",
		"--file", configPath,
		"--lock-file", lockPath,
		"--chdir", tmpDir,
	})

	if err := cmd.Execute(); err != nil {
		t.Fatalf("vendor command failed: %v\nstdout: %s\nstderr: %s", err, stdout.String(), stderr.String())
	}

	vendoredChart := filepath.Join(tmpDir, "vendor", "grafana")
	if _, err := os.Stat(filepath.Join(vendoredChart, "Chart.yaml")); err != nil {
		t.Fatalf("vendored Chart.yaml missing: %v", err)
	}

	lockBytes, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatalf("read lock file: %v", err)
	}
	if !bytes.Contains(lockBytes, []byte("grafana")) {
		t.Fatalf("lock file did not record grafana contents:\n%s", lockBytes)
	}
}
