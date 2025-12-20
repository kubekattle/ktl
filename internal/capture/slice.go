package capture

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Slice writes a new capture artifact containing only the filtered entries from
// the source artifact.
func Slice(ctx context.Context, artifactPath string, outputPath string, opts ReplayOptions) (string, error) {
	if strings.TrimSpace(artifactPath) == "" {
		return "", fmt.Errorf("artifact path is required")
	}
	if strings.TrimSpace(outputPath) == "" {
		return "", fmt.Errorf("output path is required")
	}
	if !filepath.IsAbs(outputPath) {
		abs, err := filepath.Abs(outputPath)
		if err == nil {
			outputPath = abs
		}
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return "", fmt.Errorf("ensure output dir: %w", err)
	}

	srcMeta, _ := LoadMetadata(artifactPath)
	tempDir, err := os.MkdirTemp("", "ktl-capture-slice-")
	if err != nil {
		return "", fmt.Errorf("create slice temp dir: %w", err)
	}
	defer os.RemoveAll(tempDir)

	logsPath := filepath.Join(tempDir, "logs.jsonl")
	logFile, err := os.Create(logsPath)
	if err != nil {
		return "", fmt.Errorf("create slice logs: %w", err)
	}
	writer := bufio.NewWriterSize(logFile, 64*1024)

	opts.PreferJSON = true
	var (
		firstTS time.Time
		lastTS  time.Time
		pods    = make(map[string]struct{})
		nsSet   = make(map[string]struct{})
		lines   int
	)
	if err := ReplayEntries(ctx, artifactPath, opts, func(entry Entry) error {
		data, err := json.Marshal(entry)
		if err != nil {
			return fmt.Errorf("marshal entry: %w", err)
		}
		if _, err := writer.Write(data); err != nil {
			return err
		}
		if err := writer.WriteByte('\n'); err != nil {
			return err
		}
		lines++
		if !entry.Timestamp.IsZero() {
			if firstTS.IsZero() || entry.Timestamp.Before(firstTS) {
				firstTS = entry.Timestamp
			}
			if lastTS.IsZero() || entry.Timestamp.After(lastTS) {
				lastTS = entry.Timestamp
			}
		}
		if entry.Namespace != "" && entry.Pod != "" {
			key := entry.Namespace + "/" + entry.Pod
			pods[key] = struct{}{}
		}
		if entry.Namespace != "" {
			nsSet[entry.Namespace] = struct{}{}
		}
		return nil
	}); err != nil {
		writer.Flush()
		logFile.Close()
		return "", err
	}
	if err := writer.Flush(); err != nil {
		logFile.Close()
		return "", fmt.Errorf("flush slice logs: %w", err)
	}
	if err := logFile.Close(); err != nil {
		return "", fmt.Errorf("close slice logs: %w", err)
	}

	// Derive metadata for the slice.
	var namespaces []string
	for ns := range nsSet {
		namespaces = append(namespaces, ns)
	}
	sort.Strings(namespaces)
	sessionName := "slice"
	contextName := ""
	kubeconfig := ""
	podQuery := ""
	allNamespaces := false
	tailLines := int64(0)
	since := ""
	eventsEnabled := false
	follow := false
	if srcMeta != nil {
		if strings.TrimSpace(srcMeta.SessionName) != "" {
			sessionName = strings.TrimSpace(srcMeta.SessionName) + " (slice)"
		}
		contextName = srcMeta.Context
		kubeconfig = srcMeta.Kubeconfig
		podQuery = srcMeta.PodQuery
		allNamespaces = srcMeta.AllNamespaces
		tailLines = srcMeta.TailLines
		since = srcMeta.Since
		eventsEnabled = srcMeta.EventsEnabled
		follow = false
	}
	start := firstTS
	end := lastTS
	if start.IsZero() {
		start = time.Now().UTC()
	}
	if end.IsZero() {
		end = start
	}
	meta := Metadata{
		SessionName:     sessionName,
		StartedAt:       start.UTC(),
		EndedAt:         end.UTC(),
		DurationSeconds: end.Sub(start).Seconds(),
		Namespaces:      namespaces,
		AllNamespaces:   allNamespaces,
		PodQuery:        podQuery,
		TailLines:       tailLines,
		Since:           since,
		Context:         contextName,
		Kubeconfig:      kubeconfig,
		PodCount:        len(pods),
		EventsEnabled:   eventsEnabled,
		Follow:          follow,
		SQLitePath:      "",
	}
	metaPath := filepath.Join(tempDir, "metadata.json")
	metaFile, err := os.Create(metaPath)
	if err != nil {
		return "", fmt.Errorf("create slice metadata: %w", err)
	}
	enc := json.NewEncoder(metaFile)
	enc.SetIndent("", "  ")
	if err := enc.Encode(meta); err != nil {
		metaFile.Close()
		return "", fmt.Errorf("write slice metadata: %w", err)
	}
	if err := metaFile.Close(); err != nil {
		return "", fmt.Errorf("close slice metadata: %w", err)
	}

	// Archive tempDir into outputPath (tar.gz).
	outFile, err := os.Create(outputPath)
	if err != nil {
		return "", fmt.Errorf("create slice archive: %w", err)
	}
	defer outFile.Close()
	gz := gzip.NewWriter(outFile)
	defer gz.Close()
	tw := tar.NewWriter(gz)
	defer tw.Close()
	walkErr := filepath.WalkDir(tempDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(tempDir, path)
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		hdr := &tar.Header{
			Name:    rel,
			Mode:    int64(info.Mode().Perm()),
			Size:    info.Size(),
			ModTime: info.ModTime(),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			return err
		}
		f, err := os.Open(path)
		if err != nil {
			return err
		}
		if _, err := io.Copy(tw, f); err != nil {
			f.Close()
			return err
		}
		return f.Close()
	})
	if walkErr != nil {
		return "", fmt.Errorf("archive slice contents: %w", walkErr)
	}
	if lines == 0 {
		// Avoid surprising empty artifacts when filters excluded everything.
		// The archive still contains metadata + empty logs.jsonl; callers can decide policy.
	}
	return outputPath, nil
}
