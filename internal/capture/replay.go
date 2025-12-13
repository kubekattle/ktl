// replay.go rehydrates captured logs/events back to stdout or JSON for offline analysis.
package capture

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/template"
	"time"

	_ "modernc.org/sqlite"
)

const defaultReplayTemplate = "{{.Rendered}}"

// ReplayOptions governs how capture artifacts are replayed.
type ReplayOptions struct {
	Namespaces []string
	Pods       []string
	Containers []string
	Grep       []string
	Since      time.Time
	Until      time.Time
	Limit      int
	PreferJSON bool
	Raw        bool
	JSON       bool
	Desc       bool
	Template   string
	Follow     bool
}

// Replay renders the logs from the given capture artifact to the writer.
func Replay(ctx context.Context, artifactPath string, opts ReplayOptions, out io.Writer) error {
	baseDir, cleanup, err := prepareCaptureDir(artifactPath)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
		if opts.Follow {
			return fmt.Errorf("--follow requires pointing replay at a capture directory (not a tarball)")
		}
	}
	jsonPath := filepath.Join(baseDir, "logs.jsonl")
	sqlitePath := filepath.Join(baseDir, "logs.sqlite")
	if opts.Follow {
		opts.PreferJSON = true
	}
	useJSON := opts.PreferJSON || !fileExists(sqlitePath)
	if useJSON && !fileExists(jsonPath) {
		return fmt.Errorf("capture artifact missing logs.jsonl at %s", jsonPath)
	}
	if opts.Follow && !useJSON {
		return fmt.Errorf("--follow currently streams from logs.jsonl; rerun with --prefer-json")
	}
	if opts.Template == "" {
		opts.Template = defaultReplayTemplate
	}
	render, err := template.New("capture-replay").Parse(opts.Template)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	filters := newReplayFilter(opts)
	emit := func(entry Entry) error {
		if !filters.Allow(entry) {
			return nil
		}
		if opts.Limit > 0 && filters.count >= opts.Limit {
			return io.EOF
		}
		filters.count++
		return writeEntry(out, entry, opts, render)
	}
	if useJSON {
		return replayFromJSON(ctx, jsonPath, emit, opts.Follow)
	}
	return replayFromSQLite(ctx, sqlitePath, emit, opts)
}

func replayFromJSON(ctx context.Context, path string, emit func(Entry) error, follow bool) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open logs.jsonl: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := reader.ReadBytes('\n')
		if err != nil {
			if errors.Is(err, io.EOF) {
				if follow {
					time.Sleep(200 * time.Millisecond)
					continue
				}
				return nil
			}
			return fmt.Errorf("read logs.jsonl: %w", err)
		}
		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 {
			continue
		}
		var entry Entry
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return fmt.Errorf("unmarshal capture entry: %w", err)
		}
		if err := emit(entry); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
}

func replayFromSQLite(ctx context.Context, path string, emit func(Entry) error, opts ReplayOptions) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open sqlite: %w", err)
	}
	defer db.Close()
	query, args := buildSQLiteQuery(opts)
	rows, err := db.QueryContext(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("query sqlite logs: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var logTS, collected string
		var ns, pod, container, raw, rendered string
		if err := rows.Scan(&logTS, &collected, &ns, &pod, &container, &raw, &rendered); err != nil {
			return fmt.Errorf("scan sqlite row: %w", err)
		}
		entry := Entry{
			Namespace: ns,
			Pod:       pod,
			Container: container,
			Raw:       raw,
			Rendered:  rendered,
		}
		entry.Timestamp = parseFirstTime(logTS, collected)
		entry.FormattedTimestamp = entry.Timestamp.Format(time.RFC3339)
		if err := emit(entry); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
	}
	return rows.Err()
}

func buildSQLiteQuery(opts ReplayOptions) (string, []interface{}) {
	clauses := make([]string, 0, 6)
	args := make([]interface{}, 0, 6)
	if len(opts.Namespaces) > 0 {
		clauses = append(clauses, inClause("namespace", len(opts.Namespaces)))
		for _, ns := range opts.Namespaces {
			args = append(args, ns)
		}
	}
	if len(opts.Pods) > 0 {
		clauses = append(clauses, inClause("pod", len(opts.Pods)))
		for _, pod := range opts.Pods {
			args = append(args, pod)
		}
	}
	if len(opts.Containers) > 0 {
		clauses = append(clauses, inClause("container", len(opts.Containers)))
		for _, c := range opts.Containers {
			args = append(args, c)
		}
	}
	if !opts.Since.IsZero() {
		clauses = append(clauses, "COALESCE(log_timestamp, collected_at) >= ?")
		args = append(args, opts.Since.Format(time.RFC3339Nano))
	}
	if !opts.Until.IsZero() {
		clauses = append(clauses, "COALESCE(log_timestamp, collected_at) <= ?")
		args = append(args, opts.Until.Format(time.RFC3339Nano))
	}
	query := "SELECT log_timestamp, collected_at, namespace, pod, container, raw, rendered FROM logs"
	if len(clauses) > 0 {
		query += " WHERE " + strings.Join(clauses, " AND ")
	}
	order := "ASC"
	if opts.Desc {
		order = "DESC"
	}
	query += " ORDER BY COALESCE(log_timestamp, collected_at) " + order
	if opts.Limit > 0 {
		query += fmt.Sprintf(" LIMIT %d", opts.Limit)
	}
	return query, args
}

func inClause(column string, count int) string {
	pl := make([]string, count)
	for i := range pl {
		pl[i] = "?"
	}
	return fmt.Sprintf("%s IN (%s)", column, strings.Join(pl, ","))
}

func parseFirstTime(values ...string) time.Time {
	for _, val := range values {
		if val == "" {
			continue
		}
		if ts, err := time.Parse(time.RFC3339Nano, val); err == nil {
			return ts
		}
	}
	return time.Time{}
}

type replayFilter struct {
	ns         map[string]struct{}
	pods       map[string]struct{}
	containers map[string]struct{}
	grep       []string
	since      time.Time
	until      time.Time
	count      int
}

func newReplayFilter(opts ReplayOptions) *replayFilter {
	return &replayFilter{
		ns:         toSet(opts.Namespaces),
		pods:       toSet(opts.Pods),
		containers: toSet(opts.Containers),
		grep:       normalizeGrep(opts.Grep),
		since:      opts.Since,
		until:      opts.Until,
	}
}

func (f *replayFilter) Allow(entry Entry) bool {
	if len(f.ns) > 0 && !f.contains(f.ns, entry.Namespace) {
		return false
	}
	if len(f.pods) > 0 && !f.contains(f.pods, entry.Pod) {
		return false
	}
	if len(f.containers) > 0 && !f.contains(f.containers, entry.Container) {
		return false
	}
	if !f.since.IsZero() {
		ts := entry.Timestamp
		if ts.IsZero() && entry.FormattedTimestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, entry.FormattedTimestamp); err == nil {
				ts = parsed
			}
		}
		if ts.IsZero() || ts.Before(f.since) {
			return false
		}
	}
	if !f.until.IsZero() {
		ts := entry.Timestamp
		if ts.IsZero() && entry.FormattedTimestamp != "" {
			if parsed, err := time.Parse(time.RFC3339, entry.FormattedTimestamp); err == nil {
				ts = parsed
			}
		}
		if !ts.IsZero() && ts.After(f.until) {
			return false
		}
	}
	if len(f.grep) > 0 {
		text := strings.ToLower(entry.Rendered)
		raw := strings.ToLower(entry.Raw)
		for _, needle := range f.grep {
			if !strings.Contains(text, needle) && !strings.Contains(raw, needle) {
				return false
			}
		}
	}
	return true
}

func (f *replayFilter) contains(set map[string]struct{}, value string) bool {
	_, ok := set[value]
	return ok
}

func toSet(values []string) map[string]struct{} {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	return set
}

func normalizeGrep(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	norm := make([]string, 0, len(values))
	for _, v := range values {
		trim := strings.TrimSpace(v)
		if trim == "" {
			continue
		}
		norm = append(norm, strings.ToLower(trim))
	}
	return norm
}

func writeEntry(out io.Writer, entry Entry, opts ReplayOptions, tmpl *template.Template) error {
	var line string
	switch {
	case opts.JSON:
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal entry json: %w", err)
		}
		line = string(data)
	case opts.Raw:
		line = entry.Raw
	default:
		var buf bytes.Buffer
		if err := tmpl.Execute(&buf, entry); err != nil {
			return fmt.Errorf("render template: %w", err)
		}
		line = buf.String()
	}
	if line == "" {
		return nil
	}
	_, err := fmt.Fprintln(out, line)
	return err
}

func prepareCaptureDir(path string) (string, func(), error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", nil, fmt.Errorf("stat capture artifact: %w", err)
	}
	if info.IsDir() {
		return path, nil, nil
	}
	dir, err := extractCaptureArchive(path)
	if err != nil {
		return "", nil, err
	}
	cleanup := func() {
		_ = os.RemoveAll(dir)
	}
	return dir, cleanup, nil
}

func extractCaptureArchive(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", fmt.Errorf("open capture artifact: %w", err)
	}
	defer file.Close()
	gz, err := gzip.NewReader(file)
	if err != nil {
		return "", fmt.Errorf("create gzip reader: %w", err)
	}
	defer gz.Close()
	tarReader := tar.NewReader(gz)
	tempDir, err := os.MkdirTemp("", "ktl-replay-")
	if err != nil {
		return "", fmt.Errorf("create replay temp dir: %w", err)
	}
	for {
		hdr, err := tarReader.Next()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return tempDir, nil
			}
			os.RemoveAll(tempDir)
			return "", fmt.Errorf("read archive: %w", err)
		}
		target := filepath.Join(tempDir, hdr.Name)
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, os.FileMode(hdr.Mode)); err != nil {
				os.RemoveAll(tempDir)
				return "", fmt.Errorf("create dir %s: %w", target, err)
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				os.RemoveAll(tempDir)
				return "", err
			}
			outFile, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode))
			if err != nil {
				os.RemoveAll(tempDir)
				return "", fmt.Errorf("create file %s: %w", target, err)
			}
			if _, err := io.Copy(outFile, tarReader); err != nil {
				outFile.Close()
				os.RemoveAll(tempDir)
				return "", fmt.Errorf("extract file %s: %w", target, err)
			}
			outFile.Close()
		}
	}
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
