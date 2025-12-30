package chartarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	_ "modernc.org/sqlite"
)

type UnpackOptions struct {
	// DestinationPath is the directory to write chart files into. If empty,
	// it is derived from the chart metadata (e.g., <name>-<version>).
	DestinationPath string
	Force           bool
	Workers         int
	MaxBytes        int64 // 0 means unlimited; only used for streaming stdin by callers.
}

type UnpackResult struct {
	ArchivePath     string `json:"archivePath"`
	ChartName       string `json:"chartName,omitempty"`
	ChartVersion    string `json:"chartVersion,omitempty"`
	DestinationPath string `json:"destinationPath"`
	FileCount       int    `json:"fileCount"`
	TotalBytes      int64  `json:"totalBytes"`
	ContentSHA256   string `json:"contentSha256,omitempty"`
}

func UnpackArchive(ctx context.Context, archivePath string, opts UnpackOptions) (*UnpackResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	archivePath = strings.TrimSpace(archivePath)
	if archivePath == "" {
		return nil, errors.New("archive path is required")
	}

	db, err := sql.Open("sqlite", archivePath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	defer db.Close()

	meta, err := readMeta(ctx, db)
	if err != nil {
		return nil, err
	}
	if meta["ktl_archive_type"] != archiveType {
		return nil, fmt.Errorf("unexpected archive type %q (want %q)", meta["ktl_archive_type"], archiveType)
	}

	destPath, err := resolveDestPath(opts.DestinationPath, archivePath, meta)
	if err != nil {
		return nil, err
	}
	destPath, err = filepath.Abs(destPath)
	if err != nil {
		return nil, fmt.Errorf("resolve destination: %w", err)
	}

	if info, err := os.Stat(destPath); err == nil {
		if !info.IsDir() {
			return nil, fmt.Errorf("destination exists and is not a directory: %s", destPath)
		}
		if !opts.Force {
			return nil, fmt.Errorf("destination already exists: %s (use --force to overwrite)", destPath)
		}
	} else if errors.Is(err, os.ErrNotExist) {
		if err := os.MkdirAll(destPath, 0o755); err != nil {
			return nil, fmt.Errorf("create destination: %w", err)
		}
	} else {
		return nil, fmt.Errorf("stat destination: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT path, mode, sha256, data FROM ktl_chart_files ORDER BY path ASC`)
	if err != nil {
		return nil, fmt.Errorf("read chart files: %w", err)
	}
	defer rows.Close()

	var (
		files int
		bytes int64
	)

	var jobs []struct {
		path string
		mode int64
		data []byte
	}
	digest := sha256.New()
	for rows.Next() {
		var (
			relPath string
			mode    int64
			sha     string
			data    []byte
		)
		if err := rows.Scan(&relPath, &mode, &sha, &data); err != nil {
			return nil, fmt.Errorf("scan chart file: %w", err)
		}
		clean := filepath.Clean(relPath)
		if clean == "." || strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return nil, fmt.Errorf("invalid file path in archive: %s", relPath)
		}
		actual := sha256.Sum256(data)
		actualHex := fmt.Sprintf("%x", actual[:])
		if strings.TrimSpace(sha) != actualHex {
			return nil, fmt.Errorf("sha256 mismatch for %s", relPath)
		}
		recordDigest(digest, relPath, actualHex)
		jobs = append(jobs, struct {
			path string
			mode int64
			data []byte
		}{path: clean, mode: mode, data: append([]byte(nil), data...)})
		files++
		bytes += int64(len(data))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chart files: %w", err)
	}
	contentSHA := fmt.Sprintf("%x", digest.Sum(nil))
	if expected := strings.TrimSpace(meta["content_sha256"]); expected != "" && expected != contentSHA {
		return nil, fmt.Errorf("content_sha256 mismatch (expected %s got %s)", expected, contentSHA)
	}

	workerCount := opts.Workers
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	if workerCount < 1 {
		workerCount = 1
	}
	workCh := make(chan struct {
		path string
		mode int64
		data []byte
	}, workerCount)
	var wg sync.WaitGroup
	var writeErr error
	var once sync.Once
	for i := 0; i < workerCount; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for job := range workCh {
				target := filepath.Join(destPath, job.path)
				if !strings.HasPrefix(target, destPath+string(os.PathSeparator)) && target != destPath {
					once.Do(func() { writeErr = fmt.Errorf("refusing to write outside destination: %s", target) })
					return
				}
				if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
					once.Do(func() { writeErr = fmt.Errorf("mkdir for %s: %w", target, err) })
					return
				}
				if !opts.Force {
					if _, err := os.Stat(target); err == nil {
						once.Do(func() { writeErr = fmt.Errorf("file already exists: %s (use --force)", target) })
						return
					}
				}
				if err := os.WriteFile(target, job.data, os.FileMode(job.mode)); err != nil {
					once.Do(func() { writeErr = fmt.Errorf("write %s: %w", target, err) })
					return
				}
			}
		}()
	}
	for _, job := range jobs {
		if writeErr != nil {
			break
		}
		workCh <- job
	}
	close(workCh)
	wg.Wait()
	if writeErr != nil {
		return nil, writeErr
	}

	return &UnpackResult{
		ArchivePath:     archivePath,
		ChartName:       strings.TrimSpace(meta["chart_name"]),
		ChartVersion:    strings.TrimSpace(meta["chart_version"]),
		DestinationPath: destPath,
		FileCount:       files,
		TotalBytes:      bytes,
		ContentSHA256:   strings.TrimSpace(firstNonEmpty(meta["content_sha256"], contentSHA)),
	}, nil
}

func resolveDestPath(requested, archivePath string, meta map[string]string) (string, error) {
	requested = strings.TrimSpace(requested)
	if requested != "" {
		return requested, nil
	}
	name := sanitizeFilenameToken(meta["chart_name"])
	version := sanitizeFilenameToken(meta["chart_version"])
	switch {
	case name != "" && version != "":
		return fmt.Sprintf("%s-%s", name, version), nil
	case name != "":
		return name, nil
	default:
		base := strings.TrimSuffix(filepath.Base(archivePath), filepath.Ext(archivePath))
		if base == "" {
			return "", errors.New("destination path is empty")
		}
		return base, nil
	}
}
