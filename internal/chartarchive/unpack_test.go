package chartarchive

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestUnpackArchive_DefaultDestination(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	chartYAML := `apiVersion: v2
name: demo
version: 0.2.1
`
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "service.yaml"), []byte("kind: Service\n"), 0o755); err != nil {
		t.Fatalf("write template: %v", err)
	}

	outDir := t.TempDir()
	pkgRes, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	// Run in an isolated working directory so the default destination is clean.
	cwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	tempCwd := t.TempDir()
	if err := os.Chdir(tempCwd); err != nil {
		t.Fatalf("chdir temp: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(cwd) })

	res, err := UnpackArchive(context.Background(), pkgRes.ArchivePath, UnpackOptions{DestinationPath: ""})
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}
	if res.FileCount == 0 {
		t.Fatalf("expected files to be unpacked")
	}
	if filepath.Base(res.DestinationPath) != "demo-0.2.1" {
		t.Fatalf("expected default destination demo-0.2.1, got %s", filepath.Base(res.DestinationPath))
	}
	if _, err := os.Stat(filepath.Join(res.DestinationPath, "Chart.yaml")); err != nil {
		t.Fatalf("expected Chart.yaml in destination: %v", err)
	}
}

func TestUnpackArchive_RequiresForceForExistingDir(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	outDir := t.TempDir()
	pkgRes, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	dest := t.TempDir()
	if _, err := UnpackArchive(context.Background(), pkgRes.ArchivePath, UnpackOptions{DestinationPath: dest}); err == nil {
		t.Fatalf("expected unpack to fail without force when dest exists")
	}
	if _, err := UnpackArchive(context.Background(), pkgRes.ArchivePath, UnpackOptions{DestinationPath: dest, Force: true}); err != nil {
		t.Fatalf("expected unpack with force to succeed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "Chart.yaml")); err != nil {
		t.Fatalf("expected Chart.yaml after unpack: %v", err)
	}
}
