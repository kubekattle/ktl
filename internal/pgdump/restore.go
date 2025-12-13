// restore.go runs pg_restore/psql flows to support 'ktl db restore'.
package pgdump

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/kube"
)

type RestoreOptions struct {
	Namespace            string
	Pod                  string
	Container            string
	User                 string
	Password             string
	InputPath            string
	DropAllBeforeRestore bool
	ProgressHook         func(current, total int, database string)
}

type RestoreResult struct {
	Databases []string
}

func Restore(ctx context.Context, client *kube.Client, opts RestoreOptions) (*RestoreResult, error) {
	if client == nil {
		return nil, fmt.Errorf("kube client is required")
	}
	if opts.Pod == "" {
		return nil, fmt.Errorf("pod is required")
	}
	if strings.TrimSpace(opts.InputPath) == "" {
		return nil, fmt.Errorf("input archive path is required")
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
	file, err := os.Open(opts.InputPath)
	if err != nil {
		return nil, fmt.Errorf("open archive: %w", err)
	}
	defer file.Close()

	reader := bufio.NewReader(file)
	var tarReader *tar.Reader
	if isGzipPath(opts.InputPath) {
		gz, err := gzip.NewReader(reader)
		if err != nil {
			return nil, fmt.Errorf("open gzip: %w", err)
		}
		defer gz.Close()
		tarReader = tar.NewReader(gz)
	} else {
		tarReader = tar.NewReader(reader)
	}

	tempDir, err := os.MkdirTemp("", "ktl-pgrestore-*")
	if err != nil {
		return nil, fmt.Errorf("create temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	order := make([]string, 0)
	files := make(map[string]string)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read archive: %w", err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		base := filepath.Base(header.Name)
		if !strings.HasSuffix(base, ".sql") {
			continue
		}
		db := strings.TrimSuffix(base, ".sql")
		target := filepath.Join(tempDir, base)
		out, err := os.Create(target)
		if err != nil {
			return nil, fmt.Errorf("create temp file for %s: %w", db, err)
		}
		if _, err := io.Copy(out, tarReader); err != nil {
			out.Close()
			return nil, fmt.Errorf("extract %s: %w", db, err)
		}
		out.Close()
		order = append(order, db)
		files[db] = target
	}
	if len(order) == 0 {
		return nil, fmt.Errorf("archive contains no *.sql dumps")
	}

	if opts.DropAllBeforeRestore {
		existing, err := discoverDatabases(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password)
		if err != nil {
			return nil, err
		}
		for _, db := range existing {
			if err := runSQL(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password, fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", db)); err != nil {
				return nil, err
			}
		}
	}

	for i, db := range order {
		if opts.ProgressHook != nil {
			opts.ProgressHook(i+1, len(order), db)
		}
		if err := runSQL(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password, fmt.Sprintf("DROP DATABASE IF EXISTS \"%s\";", db)); err != nil {
			return nil, err
		}
		if err := runSQL(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password, fmt.Sprintf("CREATE DATABASE \"%s\";", db)); err != nil {
			return nil, err
		}
		if err := restoreDatabase(ctx, client, namespace, opts.Pod, opts.Container, user, opts.Password, db, files[db]); err != nil {
			return nil, err
		}
	}

	return &RestoreResult{Databases: order}, nil
}

func restoreDatabase(ctx context.Context, client *kube.Client, namespace, pod, container, user, password, database, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open dump %s: %w", database, err)
	}
	defer file.Close()
	cmd := withPassword(password, []string{"psql", "-U", user, "-d", database})
	if err := client.Exec(ctx, namespace, pod, container, cmd, file, io.Discard, io.Discard); err != nil {
		return fmt.Errorf("restore %s: %w", database, err)
	}
	return nil
}

func runSQL(ctx context.Context, client *kube.Client, namespace, pod, container, user, password, statement string) error {
	cmd := withPassword(password, []string{"psql", "-U", user, "-tAc", statement})
	var stderr strings.Builder
	if err := client.Exec(ctx, namespace, pod, container, cmd, nil, io.Discard, &stderr); err != nil {
		return fmt.Errorf("psql: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

func isGzipPath(path string) bool {
	return strings.HasSuffix(path, ".gz")
}
