package chartarchive

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"errors"
	"fmt"
	"strings"

	_ "modernc.org/sqlite"
)

type VerifyResult struct {
	ArchivePath   string `json:"archivePath"`
	ChartName     string `json:"chartName,omitempty"`
	ChartVersion  string `json:"chartVersion,omitempty"`
	ChartDir      string `json:"chartDir,omitempty"`
	FileCount     int    `json:"fileCount"`
	TotalBytes    int64  `json:"totalBytes"`
	ContentSHA256 string `json:"contentSha256,omitempty"`
}

func VerifyArchive(ctx context.Context, path string) (*VerifyResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("archive path is required")
	}

	db, err := sql.Open("sqlite", path)
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
	if meta["ktl_archive_version"] != archiveVersion {
		return nil, fmt.Errorf("unexpected archive version %q (want %q)", meta["ktl_archive_version"], archiveVersion)
	}

	rows, err := db.QueryContext(ctx, `SELECT path, sha256, size, data FROM ktl_chart_files ORDER BY path ASC`)
	if err != nil {
		return nil, fmt.Errorf("read chart files: %w", err)
	}
	defer rows.Close()

	digest := sha256.New()
	var (
		fileCount  int
		totalBytes int64
	)
	for rows.Next() {
		var (
			p    string
			sha  string
			size int64
			data []byte
		)
		if err := rows.Scan(&p, &sha, &size, &data); err != nil {
			return nil, fmt.Errorf("scan chart file: %w", err)
		}
		actual := sha256.Sum256(data)
		actualHex := fmt.Sprintf("%x", actual[:])
		if strings.TrimSpace(sha) != actualHex {
			return nil, fmt.Errorf("sha256 mismatch for %s", p)
		}
		recordDigest(digest, p, actualHex)
		fileCount++
		totalBytes += int64(len(data))
		_ = size
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chart files: %w", err)
	}

	contentSHA := fmt.Sprintf("%x", digest.Sum(nil))
	if expected := strings.TrimSpace(meta["content_sha256"]); expected != "" && expected != contentSHA {
		return nil, fmt.Errorf("content_sha256 mismatch (expected %s got %s)", expected, contentSHA)
	}

	return &VerifyResult{
		ArchivePath:   path,
		ChartName:     strings.TrimSpace(meta["chart_name"]),
		ChartVersion:  strings.TrimSpace(meta["chart_version"]),
		ChartDir:      strings.TrimSpace(meta["chart_dir"]),
		FileCount:     fileCount,
		TotalBytes:    totalBytes,
		ContentSHA256: firstNonEmpty(strings.TrimSpace(meta["content_sha256"]), contentSHA),
	}, nil
}

func readMeta(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, err := db.QueryContext(ctx, `SELECT key, value FROM ktl_archive_meta`)
	if err != nil {
		return nil, fmt.Errorf("read archive meta: %w", err)
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, fmt.Errorf("scan archive meta: %w", err)
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate archive meta: %w", err)
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
