package main

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/example/ktl/internal/appconfig"
)

// TestPackageChartsSmoke walks the bundled test charts and ensures the package
// CLI can archive and verify each one that contains a Chart.yaml.
func TestPackageChartsSmoke(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("failed to resolve caller path")
	}
	repoRoot := appconfig.FindRepoRoot(file)
	if repoRoot == "" {
		t.Fatalf("failed to locate repo root")
	}
	chartsDir := filepath.Join(repoRoot, "testdata", "charts")

	entries, err := os.ReadDir(chartsDir)
	if err != nil {
		t.Fatalf("read charts dir: %v", err)
	}

	var packaged int
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		chartDir := filepath.Join(chartsDir, entry.Name())
		if _, err := os.Stat(filepath.Join(chartDir, "Chart.yaml")); err != nil {
			t.Logf("skip %s: no Chart.yaml", entry.Name())
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			outPath := filepath.Join(t.TempDir(), entry.Name()+".sqlite")

			// Package the chart.
			var pkgOut, pkgErr bytes.Buffer
			pkgCmd := newPackageCommand()
			pkgCmd.SetOut(&pkgOut)
			pkgCmd.SetErr(&pkgErr)
			pkgCmd.SetArgs([]string{chartDir, "--output", outPath, "--quiet"})
			if err := pkgCmd.Execute(); err != nil {
				t.Fatalf("package %s: %v\nstdout:\n%s\nstderr:\n%s", entry.Name(), err, pkgOut.String(), pkgErr.String())
			}
			if _, err := os.Stat(outPath); err != nil {
				t.Fatalf("expected archive at %s: %v", outPath, err)
			}

			// Verify the archive using the CLI as well.
			var verifyOut, verifyErr bytes.Buffer
			verifyCmd := newPackageCommand()
			verifyCmd.SetOut(&verifyOut)
			verifyCmd.SetErr(&verifyErr)
			verifyCmd.SetArgs([]string{"--verify", outPath, "--quiet", "--print-sha"})
			if err := verifyCmd.Execute(); err != nil {
				t.Fatalf("verify %s: %v\nstdout:\n%s\nstderr:\n%s", entry.Name(), err, verifyOut.String(), verifyErr.String())
			}

			// Unpack the archive.
			destDir := filepath.Join(t.TempDir(), entry.Name()+"-unpacked")
			var unpackOut, unpackErr bytes.Buffer
			unpackCmd := newPackageCommand()
			unpackCmd.SetOut(&unpackOut)
			unpackCmd.SetErr(&unpackErr)
			unpackCmd.SetArgs([]string{"--unpack", outPath, "--destination", destDir, "--quiet"})
			if err := unpackCmd.Execute(); err != nil {
				t.Fatalf("unpack %s: %v\nstdout:\n%s\nstderr:\n%s", entry.Name(), err, unpackOut.String(), unpackErr.String())
			}
			if _, err := os.Stat(filepath.Join(destDir, "Chart.yaml")); err != nil {
				t.Fatalf("expected Chart.yaml after unpack: %v\nstdout:\n%s\nstderr:\n%s", err, unpackOut.String(), unpackErr.String())
			}
		})
		packaged++
	}

	if packaged == 0 {
		t.Fatalf("no charts were packaged (check testdata/charts contents)")
	}
}
