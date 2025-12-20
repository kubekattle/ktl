// File: internal/chartarchive/chartarchive.go
// Brief: SQLite-backed archive format for packaging Helm charts.

package chartarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/chart/loader"
)

const (
	archiveType    = "chart"
	archiveVersion = "1"
)

const (
	createMetaTableStmt = `
CREATE TABLE IF NOT EXISTS ktl_archive_meta (
  key TEXT PRIMARY KEY,
  value TEXT NOT NULL
);`
	createFilesTableStmt = `
CREATE TABLE IF NOT EXISTS ktl_chart_files (
  path TEXT PRIMARY KEY,
  mode INTEGER NOT NULL,
  size INTEGER NOT NULL,
  sha256 TEXT NOT NULL,
  data BLOB NOT NULL
);`
)

type PackageOptions struct {
	// OutputPath is the desired archive file path. If empty, a name is derived
	// from chart metadata and written to the current working directory.
	//
	// If OutputPath points to an existing directory, the archive filename will
	// be derived and placed within that directory.
	OutputPath string
	Force      bool
}

type PackageResult struct {
	ArchivePath  string
	ChartName    string
	ChartVersion string
	FileCount    int
	TotalBytes   int64
}

func PackageDir(ctx context.Context, chartDir string, opts PackageOptions) (*PackageResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	chartDir = strings.TrimSpace(chartDir)
	if chartDir == "" {
		chartDir = "."
	}

	ch, err := loader.LoadDir(chartDir)
	if err != nil {
		return nil, fmt.Errorf("load chart: %w", err)
	}
	name := chartName(ch)
	version := chartVersion(ch)

	outputPath, err := resolveOutputPath(opts.OutputPath, name, version)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(outputPath) == "" {
		return nil, errors.New("resolved output path is empty")
	}
	if strings.TrimSpace(outputPath) == "-" {
		return nil, errors.New("output path cannot be '-' for sqlite archives")
	}

	if !opts.Force {
		if _, err := os.Stat(outputPath); err == nil {
			return nil, fmt.Errorf("output already exists: %s (rerun with --force to overwrite)", outputPath)
		}
	}

	outDir := filepath.Dir(outputPath)
	if outDir != "" && outDir != "." {
		if err := os.MkdirAll(outDir, 0o755); err != nil {
			return nil, fmt.Errorf("create output dir: %w", err)
		}
	}

	tmpFile, err := os.CreateTemp(outDir, "ktl-chart-*.sqlite")
	if err != nil {
		return nil, fmt.Errorf("create temp sqlite: %w", err)
	}
	tmpPath := tmpFile.Name()
	_ = tmpFile.Close()
	cleanupTmp := func() { _ = os.Remove(tmpPath) }

	result, err := writeArchive(ctx, tmpPath, chartDir, ch)
	if err != nil {
		cleanupTmp()
		return nil, err
	}

	if opts.Force {
		_ = os.Remove(outputPath)
	}
	if err := os.Rename(tmpPath, outputPath); err != nil {
		cleanupTmp()
		return nil, fmt.Errorf("finalize archive: %w", err)
	}
	result.ArchivePath = outputPath
	return result, nil
}

func resolveOutputPath(requested, chartName, chartVersion string) (string, error) {
	requested = strings.TrimSpace(requested)
	filename := archiveFilename(chartName, chartVersion)
	if requested == "" {
		wd, err := os.Getwd()
		if err != nil {
			return "", fmt.Errorf("get working dir: %w", err)
		}
		return filepath.Join(wd, filename), nil
	}

	info, err := os.Stat(requested)
	if err == nil && info.IsDir() {
		return filepath.Join(requested, filename), nil
	}
	if err != nil && errors.Is(err, os.ErrNotExist) && strings.HasSuffix(requested, string(os.PathSeparator)) {
		return filepath.Join(strings.TrimRight(requested, string(os.PathSeparator)), filename), nil
	}
	return requested, nil
}

func archiveFilename(chartName, chartVersion string) string {
	name := sanitizeFilenameToken(chartName)
	version := sanitizeFilenameToken(chartVersion)
	if name == "" {
		name = "chart"
	}
	if version == "" {
		version = "unknown"
	}
	return fmt.Sprintf("%s-%s.sqlite", name, version)
}

func sanitizeFilenameToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	var b strings.Builder
	b.Grow(len(token))
	lastDash := false
	for _, r := range token {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
			lastDash = false
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
			lastDash = false
		case r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '.' || r == '_' || r == '-':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func chartName(ch *chart.Chart) string {
	if ch != nil && ch.Metadata != nil {
		if name := strings.TrimSpace(ch.Metadata.Name); name != "" {
			return name
		}
	}
	return "chart"
}

func chartVersion(ch *chart.Chart) string {
	if ch != nil && ch.Metadata != nil {
		if version := strings.TrimSpace(ch.Metadata.Version); version != "" {
			return version
		}
	}
	return ""
}

func writeArchive(ctx context.Context, path string, chartDir string, ch *chart.Chart) (*PackageResult, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	if _, err := db.ExecContext(ctx, createMetaTableStmt); err != nil {
		return nil, fmt.Errorf("create meta table: %w", err)
	}
	if _, err := db.ExecContext(ctx, createFilesTableStmt); err != nil {
		return nil, fmt.Errorf("create files table: %w", err)
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC().Format(time.RFC3339Nano)
	if err := insertMeta(tx, ctx, map[string]string{
		"ktl_archive_type":    archiveType,
		"ktl_archive_version": archiveVersion,
		"created_at":          now,
		"chart_name":          chartName(ch),
		"chart_version":       chartVersion(ch),
		"chart_app_version":   chartAppVersion(ch),
		"chart_api_version":   chartAPIVersion(ch),
	}); err != nil {
		return nil, err
	}

	stmt, err := tx.PrepareContext(ctx, `INSERT INTO ktl_chart_files(path, mode, size, sha256, data) VALUES(?, ?, ?, ?, ?)`)
	if err != nil {
		return nil, fmt.Errorf("prepare insert: %w", err)
	}
	defer stmt.Close()

	var (
		fileCount  int
		totalBytes int64
	)
	for _, f := range chartFiles(ch) {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
		if strings.TrimSpace(f.Name) == "" {
			continue
		}
		mode, size := fileModeAndSize(chartDir, f.Name, int64(len(f.Data)))
		hash := sha256.Sum256(f.Data)
		if _, err := stmt.ExecContext(ctx, f.Name, mode, size, fmt.Sprintf("%x", hash[:]), f.Data); err != nil {
			return nil, fmt.Errorf("insert file %s: %w", f.Name, err)
		}
		fileCount++
		totalBytes += int64(len(f.Data))
	}

	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return &PackageResult{
		ChartName:    chartName(ch),
		ChartVersion: chartVersion(ch),
		FileCount:    fileCount,
		TotalBytes:   totalBytes,
	}, nil
}

func insertMeta(tx *sql.Tx, ctx context.Context, values map[string]string) error {
	if tx == nil {
		return errors.New("meta transaction is required")
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT OR REPLACE INTO ktl_archive_meta(key, value) VALUES(?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare meta insert: %w", err)
	}
	defer stmt.Close()
	for k, v := range values {
		if strings.TrimSpace(k) == "" {
			continue
		}
		if _, err := stmt.ExecContext(ctx, k, v); err != nil {
			return fmt.Errorf("insert meta %s: %w", k, err)
		}
	}
	return nil
}

func chartFiles(ch *chart.Chart) []*chart.File {
	if ch == nil {
		return nil
	}
	return ch.Raw
}

func chartAppVersion(ch *chart.Chart) string {
	if ch != nil && ch.Metadata != nil {
		return strings.TrimSpace(ch.Metadata.AppVersion)
	}
	return ""
}

func chartAPIVersion(ch *chart.Chart) string {
	if ch != nil && ch.Metadata != nil {
		return strings.TrimSpace(ch.Metadata.APIVersion)
	}
	return ""
}

func fileModeAndSize(chartDir string, relative string, fallbackSize int64) (int64, int64) {
	if chartDir == "" || relative == "" {
		return 0o644, fallbackSize
	}
	candidate := filepath.Join(chartDir, filepath.FromSlash(relative))
	info, err := os.Stat(candidate)
	if err != nil {
		return 0o644, fallbackSize
	}
	return int64(info.Mode().Perm()), info.Size()
}
