// File: internal/caststream/capture_store.go
// Brief: Internal caststream package implementation for 'capture store'.

// Package caststream provides caststream helpers.

package caststream

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/sqlitewriter"
)

type storedCapture struct {
	id        string
	dir       string
	sqlite    string
	meta      capture.Metadata
	db        *sql.DB
	hasFTS    bool
	hasMeta   bool
	hasEvents bool
	hasManif  bool
}

func captureClusterInfo(meta capture.Metadata, fallbackName string) string {
	start := meta.StartedAt.UTC()
	end := meta.EndedAt.UTC()
	if !start.IsZero() && !end.IsZero() {
		return fmt.Sprintf("Capture %s · %s → %s · %d pods", strings.TrimSpace(meta.SessionName), start.Format(time.RFC3339), end.Format(time.RFC3339), meta.PodCount)
	}
	if strings.TrimSpace(fallbackName) != "" {
		return fmt.Sprintf("Capture replay · %s", strings.TrimSpace(fallbackName))
	}
	return "Capture replay"
}

func (s *Server) importCaptureArtifact(ctx context.Context, id string, artifactPath string) error {
	if s == nil {
		return errors.New("server is required")
	}
	captureID := strings.TrimSpace(id)
	if captureID == "" {
		return errors.New("capture id is required")
	}
	src := strings.TrimSpace(artifactPath)
	if src == "" {
		return errors.New("artifact path is required")
	}
	if s.captureRoot == "" {
		root, err := os.MkdirTemp("", "ktl-capture-store-")
		if err != nil {
			return fmt.Errorf("create capture store dir: %w", err)
		}
		s.captureRoot = root
	}
	destDir := filepath.Join(s.captureRoot, captureID)
	if err := os.RemoveAll(destDir); err != nil {
		return fmt.Errorf("clear capture dir: %w", err)
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return fmt.Errorf("create capture dir: %w", err)
	}

	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("stat artifact: %w", err)
	}
	switch {
	case info.IsDir():
		// Copy a minimal subset into our store so the viewer can mutate logs.sqlite
		// for indexes/FTS without touching the original capture directory.
		if err := copyCaptureSubset(src, destDir); err != nil {
			return err
		}
	default:
		ext := strings.ToLower(filepath.Ext(src))
		if ext == ".sqlite" || ext == ".sqlite3" || ext == ".db" {
			if err := copyFile(src, filepath.Join(destDir, "logs.sqlite")); err != nil {
				return err
			}
		} else {
			if err := extractCaptureArchiveToDir(src, destDir); err != nil {
				return err
			}
		}
	}

	meta, hasMeta := readCaptureMetadata(destDir)
	sqlitePath := filepath.Join(destDir, "logs.sqlite")
	if _, err := os.Stat(sqlitePath); err != nil {
		return fmt.Errorf("capture missing logs.sqlite")
	}

	db, err := sql.Open("sqlite", sqlitePath)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	if err := sqlitewriter.EnsureViewerSchema(ctx, db); err != nil {
		_ = db.Close()
		return err
	}
	if err := capture.IngestManifestsToSQLite(ctx, db, destDir); err != nil {
		_ = db.Close()
		return err
	}

	hasFTS := sqliteTableExists(ctx, db, "logs_fts")
	hasEvents := sqliteTableExists(ctx, db, "events")
	hasManif := sqliteTableExists(ctx, db, "manifest_resources")

	s.captureStoreMu.Lock()
	if prev := s.captureStore[captureID]; prev != nil && prev.db != nil {
		_ = prev.db.Close()
	}
	s.captureStore[captureID] = &storedCapture{
		id:        captureID,
		dir:       destDir,
		sqlite:    sqlitePath,
		meta:      meta,
		db:        db,
		hasFTS:    hasFTS,
		hasMeta:   hasMeta,
		hasEvents: hasEvents,
		hasManif:  hasManif,
	}
	s.captureStoreMu.Unlock()
	return nil
}

func readCaptureMetadata(dir string) (capture.Metadata, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "metadata.json"))
	if err != nil {
		return capture.Metadata{}, false
	}
	var meta capture.Metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return capture.Metadata{}, false
	}
	return meta, true
}

func sqliteTableExists(ctx context.Context, db *sql.DB, name string) bool {
	if db == nil || strings.TrimSpace(name) == "" {
		return false
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	var found string
	err := db.QueryRowContext(ctx, `SELECT name FROM sqlite_master WHERE type IN ('table','view') AND name = ? LIMIT 1`, name).Scan(&found)
	return err == nil && found == name
}

func copyCaptureSubset(srcDir, destDir string) error {
	if err := copyFile(filepath.Join(srcDir, "logs.sqlite"), filepath.Join(destDir, "logs.sqlite")); err != nil {
		return err
	}
	_ = copyFile(filepath.Join(srcDir, "metadata.json"), filepath.Join(destDir, "metadata.json"))
	// Manifests can be large but are still bounded; copy them for offline browsing.
	_ = copyDir(filepath.Join(srcDir, "manifests"), filepath.Join(destDir, "manifests"))
	return nil
}

func copyFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func copyDir(src, dest string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("source is not a directory: %s", src)
	}
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dest, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(path, target)
	})
}

func extractCaptureArchiveToDir(path, destDir string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open capture artifact: %w", err)
	}
	defer file.Close()

	var reader io.Reader = file
	header := make([]byte, 2)
	if _, err := io.ReadFull(file, header); err == nil {
		_, _ = file.Seek(0, io.SeekStart)
		if header[0] == 0x1f && header[1] == 0x8b {
			gz, err := gzip.NewReader(file)
			if err != nil {
				return fmt.Errorf("create gzip reader: %w", err)
			}
			defer gz.Close()
			reader = gz
		}
	} else {
		_, _ = file.Seek(0, io.SeekStart)
	}

	tr := tar.NewReader(reader)
	for {
		hdr, err := tr.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return fmt.Errorf("read archive: %w", err)
		}
		cleanName := filepath.Clean(hdr.Name)
		if cleanName == "." || strings.HasPrefix(cleanName, ".."+string(filepath.Separator)) || cleanName == ".." {
			continue
		}
		target := filepath.Join(destDir, cleanName)
		if !strings.HasPrefix(filepath.Clean(target)+string(filepath.Separator), filepath.Clean(destDir)+string(filepath.Separator)) {
			continue
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				return fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg, '\x00':
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				return fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(outFile, tr); err != nil {
				_ = outFile.Close()
				return fmt.Errorf("extract file %s: %w", target, err)
			}
			_ = outFile.Close()
		}
	}
}
