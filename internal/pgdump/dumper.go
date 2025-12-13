// dumper.go orchestrates pg_dump executions inside pods for 'ktl db backup'.
package pgdump

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
)

type Options struct {
	Namespace    string
	Pod          string
	Container    string
	User         string
	Password     string
	OutputDir    string
	ArchiveName  string
	Compress     bool
	Databases    []string
	Timestamp    time.Time
	ProgressHook func(current, total int, database string)
}

type Result struct {
	ArchivePath string
	Databases   []string
}

func DumpAll(ctx context.Context, client *kube.Client, opts Options) (*Result, error) {
	if client == nil {
		return nil, fmt.Errorf("kube client is required")
	}
	if opts.Pod == "" {
		return nil, fmt.Errorf("pod is required")
	}
	namespace := opts.Namespace
	if namespace == "" {
		namespace = client.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}
	user := opts.User
	if user == "" {
		user = "postgres"
	}
	outputDir := opts.OutputDir
	if outputDir == "" {
		outputDir = "."
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return nil, fmt.Errorf("create output dir: %w", err)
	}
	compress := opts.Compress
	ts := opts.Timestamp
	if ts.IsZero() {
		ts = time.Now().UTC()
	}

	dbs := dedupe(opts.Databases)
	if len(dbs) == 0 {
		var err error
		dbs, err = discoverDatabases(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password)
		if err != nil {
			return nil, err
		}
	}
	if len(dbs) == 0 {
		return nil, fmt.Errorf("no databases found to dump")
	}

	tempDir, err := os.MkdirTemp("", "ktl-pgdump-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	tempPaths := make(map[string]string, len(dbs))
	for i, db := range dbs {
		if opts.ProgressHook != nil {
			opts.ProgressHook(i+1, len(dbs), db)
		}
		tempPath := filepath.Join(tempDir, fmt.Sprintf("%s.sql", db))
		if err := dumpDatabase(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password, db, tempPath); err != nil {
			return nil, err
		}
		tempPaths[db] = tempPath
	}

	archiveName := opts.ArchiveName
	if archiveName == "" {
		archiveName = fmt.Sprintf("db_backup_%s.tar", ts.Format("20060102_150405"))
		if compress {
			archiveName += ".gz"
		}
	}
	archivePath := filepath.Join(outputDir, archiveName)
	archiveFile, err := os.Create(archivePath)
	if err != nil {
		return nil, fmt.Errorf("create archive: %w", err)
	}
	defer archiveFile.Close()

	var writer io.Writer = archiveFile
	var gz *gzip.Writer
	if compress {
		gz = gzip.NewWriter(archiveFile)
		writer = gz
	}
	tarWriter := tar.NewWriter(writer)
	defer tarWriter.Close()
	if gz != nil {
		defer gz.Close()
	}

	for _, db := range dbs {
		dumpPath := tempPaths[db]
		info, err := os.Stat(dumpPath)
		if err != nil {
			return nil, fmt.Errorf("stat dump for %s: %w", db, err)
		}
		header := &tar.Header{
			Name:    fmt.Sprintf("%s.sql", db),
			Size:    info.Size(),
			Mode:    0o600,
			ModTime: ts,
		}
		if err := tarWriter.WriteHeader(header); err != nil {
			return nil, fmt.Errorf("write tar header for %s: %w", db, err)
		}
		file, err := os.Open(dumpPath)
		if err != nil {
			return nil, fmt.Errorf("open dump for %s: %w", db, err)
		}
		if _, err := io.Copy(tarWriter, file); err != nil {
			file.Close()
			return nil, fmt.Errorf("copy dump for %s: %w", db, err)
		}
		file.Close()
	}

	return &Result{ArchivePath: archivePath, Databases: dbs}, nil
}

func dumpDatabase(ctx context.Context, client *kube.Client, namespace, pod, container, user, password, database, path string) (err error) {
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create dump file: %w", err)
	}
	defer func() {
		if cerr := file.Close(); err == nil && cerr != nil {
			err = fmt.Errorf("close dump file: %w", cerr)
		}
	}()

	var stderr bytes.Buffer
	base := []string{"pg_dump", "-U", user, "--dbname", database}
	cmd := withPassword(password, base)
	if err := client.Exec(ctx, namespace, pod, container, cmd, nil, file, &stderr); err != nil {
		return fmt.Errorf("dump %s: %w: %s", database, err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func withPassword(password string, base []string) []string {
	buf := make([]string, len(base))
	copy(buf, base)
	if password == "" {
		return buf
	}
	cmd := make([]string, 0, len(base)+2)
	cmd = append(cmd, "env", fmt.Sprintf("PGPASSWORD=%s", password))
	cmd = append(cmd, buf...)
	return cmd
}

func dedupe(values []string) []string {
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func discoverDatabases(ctx context.Context, client *kube.Client, namespace, pod, container, user, password string) ([]string, error) {
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	query := "SELECT datname FROM pg_database WHERE datistemplate = false ORDER BY datname"
	base := []string{"psql", "-U", user, "-tAc", query}
	cmd := withPassword(password, base)
	if err := client.Exec(ctx, namespace, pod, container, cmd, nil, &stdout, &stderr); err != nil {
		return nil, fmt.Errorf("list databases: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	lines := strings.Split(stdout.String(), "\n")
	var names []string
	for _, line := range lines {
		name := strings.TrimSpace(line)
		if name == "" {
			continue
		}
		names = append(names, name)
	}
	return dedupe(names), nil
}
