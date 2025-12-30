package chartarchive

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"helm.sh/helm/v3/pkg/chart/loader"
	"helm.sh/helm/v3/pkg/chartutil"
	"helm.sh/helm/v3/pkg/engine"
)

// Ensures package -> unpack preserves rendered manifests.
func TestPackageUnpackRenderRoundTrip(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(chartDir, "templates"), 0o755); err != nil {
		t.Fatalf("mkdir templates: %v", err)
	}
	chartYAML := `apiVersion: v2
name: demo
version: 0.3.0
appVersion: "1.2.3"
`
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte(chartYAML), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	tmpl := `apiVersion: v1
kind: ConfigMap
metadata:
  name: {{ .Release.Name }}-{{ .Chart.Name }}
data:
  message: {{ .Values.message | default "hello" | quote }}
`
	if err := os.WriteFile(filepath.Join(chartDir, "templates", "configmap.yaml"), []byte(tmpl), 0o644); err != nil {
		t.Fatalf("write template: %v", err)
	}
	if err := os.WriteFile(filepath.Join(chartDir, "values.yaml"), []byte("message: world\n"), 0o644); err != nil {
		t.Fatalf("write values.yaml: %v", err)
	}

	pkgRes, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: filepath.Join(t.TempDir(), "demo.sqlite")})
	if err != nil {
		t.Fatalf("package: %v", err)
	}
	unpackDir := t.TempDir()
	_, err = UnpackArchive(context.Background(), pkgRes.ArchivePath, UnpackOptions{DestinationPath: unpackDir, Force: true})
	if err != nil {
		t.Fatalf("unpack: %v", err)
	}

	origChart, err := loader.LoadDir(chartDir)
	if err != nil {
		t.Fatalf("load original: %v", err)
	}
	unpackedChart, err := loader.LoadDir(unpackDir)
	if err != nil {
		t.Fatalf("load unpacked: %v", err)
	}

	rel := chartutil.ReleaseOptions{Name: "demo", Namespace: "default", Revision: 1, IsInstall: true}
	renderValsOrig, err := chartutil.ToRenderValues(origChart, chartutil.Values{}, rel, nil)
	if err != nil {
		t.Fatalf("render vals original: %v", err)
	}
	renderValsUnpacked, err := chartutil.ToRenderValues(unpackedChart, chartutil.Values{}, rel, nil)
	if err != nil {
		t.Fatalf("render vals unpacked: %v", err)
	}

	eng := engine.Engine{}
	origRendered, err := eng.Render(origChart, renderValsOrig)
	if err != nil {
		t.Fatalf("render original: %v", err)
	}
	unpackedRendered, err := eng.Render(unpackedChart, renderValsUnpacked)
	if err != nil {
		t.Fatalf("render unpacked: %v", err)
	}

	if !reflect.DeepEqual(origRendered, unpackedRendered) {
		t.Fatalf("rendered manifests differ after round-trip")
	}
}

func TestUnpackArchiveDetectsTamper(t *testing.T) {
	chartDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(chartDir, "Chart.yaml"), []byte("apiVersion: v2\nname: demo\nversion: 0.1.0\n"), 0o644); err != nil {
		t.Fatalf("write Chart.yaml: %v", err)
	}
	outDir := t.TempDir()
	pkgRes, err := PackageDir(context.Background(), chartDir, PackageOptions{OutputPath: outDir})
	if err != nil {
		t.Fatalf("package: %v", err)
	}

	// Tamper with stored bytes so SHA mismatch is detected.
	db, err := openSQLite(pkgRes.ArchivePath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE ktl_chart_files SET data = x'00' || data WHERE path = 'Chart.yaml'`); err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if _, err := UnpackArchive(context.Background(), pkgRes.ArchivePath, UnpackOptions{DestinationPath: t.TempDir()}); err == nil {
		t.Fatalf("expected unpack to fail after tamper")
	}
}

// openSQLite is a tiny helper to keep tests terse.
func openSQLite(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	return db, nil
}
