package inventory

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/ops/targetgraph"
)

const SourceSnapshotKind = "InventorySourceSnapshot"

type SourceSnapshotOptions struct {
	Type       string
	Source     string
	Path       string
	Ref        string
	Timeout    time.Duration
	HTTPClient *http.Client
}

type SourceSnapshot struct {
	APIVersion   string              `json:"apiVersion"`
	Kind         string              `json:"kind"`
	SourceType   string              `json:"sourceType"`
	SourceRef    string              `json:"sourceRef"`
	Path         string              `json:"path,omitempty"`
	Revision     string              `json:"revision,omitempty"`
	DirtyState   string              `json:"dirtyState,omitempty"`
	FetchedAt    string              `json:"fetchedAt"`
	SourceBytes  int                 `json:"sourceBytes"`
	SourceDigest string              `json:"sourceDigest"`
	GraphDigest  string              `json:"graphDigest"`
	Digest       string              `json:"digest"`
	Summary      targetgraph.Summary `json:"summary"`
}

type sourceReadResult struct {
	Type       string
	Ref        string
	Path       string
	Revision   string
	DirtyState string
	Raw        []byte
}

func SnapshotSource(ctx context.Context, opts SourceSnapshotOptions) (*SourceSnapshot, error) {
	source := strings.TrimSpace(opts.Source)
	if source == "" {
		return nil, fmt.Errorf("source is required")
	}
	timeout := opts.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	var cancel context.CancelFunc
	ctx, cancel = context.WithTimeout(ctx, timeout)
	defer cancel()

	result, err := readInventorySource(ctx, opts)
	if err != nil {
		return nil, err
	}
	graph, err := targetgraph.Load(bytes.NewReader(result.Raw))
	if err != nil {
		return nil, fmt.Errorf("load source TargetGraph: %w", err)
	}
	summary := redactSummary(graph.Summary())
	now := time.Now().UTC()
	snapshot := &SourceSnapshot{
		APIVersion:   APIVersion,
		Kind:         SourceSnapshotKind,
		SourceType:   result.Type,
		SourceRef:    redactSourceRef(result.Ref),
		Path:         RedactSecretRefs(result.Path),
		Revision:     RedactSecretRefs(result.Revision),
		DirtyState:   result.DirtyState,
		FetchedAt:    now.Format(time.RFC3339),
		SourceBytes:  len(result.Raw),
		SourceDigest: digestBytes(result.Raw),
		GraphDigest:  digestSummary(summary),
		Summary:      summary,
	}
	snapshot.Digest = snapshot.StableDigest()
	return snapshot, nil
}

func (s SourceSnapshot) StableDigest() string {
	doc := struct {
		SourceType   string              `json:"sourceType"`
		SourceRef    string              `json:"sourceRef"`
		Path         string              `json:"path,omitempty"`
		Revision     string              `json:"revision,omitempty"`
		DirtyState   string              `json:"dirtyState,omitempty"`
		SourceDigest string              `json:"sourceDigest"`
		GraphDigest  string              `json:"graphDigest"`
		Summary      targetgraph.Summary `json:"summary"`
	}{
		SourceType:   s.SourceType,
		SourceRef:    s.SourceRef,
		Path:         s.Path,
		Revision:     s.Revision,
		DirtyState:   s.DirtyState,
		SourceDigest: s.SourceDigest,
		GraphDigest:  s.GraphDigest,
		Summary:      s.Summary,
	}
	raw, _ := json.Marshal(doc)
	return digestBytes(raw)
}

func readInventorySource(ctx context.Context, opts SourceSnapshotOptions) (sourceReadResult, error) {
	sourceType := normalizeSourceType(opts.Type, opts.Source)
	switch sourceType {
	case "file":
		return readFileInventorySource(opts)
	case "script":
		return readScriptInventorySource(ctx, opts)
	case "http":
		return readHTTPInventorySource(ctx, opts)
	case "git":
		return readGitInventorySource(ctx, opts)
	default:
		return sourceReadResult{}, fmt.Errorf("unsupported source type %q", sourceType)
	}
}

func normalizeSourceType(value, source string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if value != "" {
		return value
	}
	source = strings.TrimSpace(source)
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		return "http"
	}
	return "file"
}

func readFileInventorySource(opts SourceSnapshotOptions) (sourceReadResult, error) {
	path := strings.TrimSpace(opts.Source)
	if subpath := strings.TrimSpace(opts.Path); subpath != "" {
		path = filepath.Join(path, subpath)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return sourceReadResult{}, fmt.Errorf("read file source: %w", err)
	}
	return sourceReadResult{Type: "file", Ref: path, Path: strings.TrimSpace(opts.Path), Raw: raw}, nil
}

func readScriptInventorySource(ctx context.Context, opts SourceSnapshotOptions) (sourceReadResult, error) {
	script := strings.TrimSpace(opts.Source)
	cmd := exec.CommandContext(ctx, script)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return sourceReadResult{}, fmt.Errorf("run script source: %s", msg)
	}
	return sourceReadResult{Type: "script", Ref: script, Raw: raw}, nil
}

func readHTTPInventorySource(ctx context.Context, opts SourceSnapshotOptions) (sourceReadResult, error) {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(opts.Source), nil)
	if err != nil {
		return sourceReadResult{}, fmt.Errorf("build HTTP source request: %w", err)
	}
	resp, err := client.Do(req)
	if err != nil {
		return sourceReadResult{}, fmt.Errorf("fetch HTTP source: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return sourceReadResult{}, fmt.Errorf("fetch HTTP source: status %s", resp.Status)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 10<<20))
	if err != nil {
		return sourceReadResult{}, fmt.Errorf("read HTTP source: %w", err)
	}
	return sourceReadResult{Type: "http", Ref: opts.Source, Raw: raw}, nil
}

func readGitInventorySource(ctx context.Context, opts SourceSnapshotOptions) (sourceReadResult, error) {
	repo := strings.TrimSpace(opts.Source)
	ref := firstInventoryNonEmpty(opts.Ref, "HEAD")
	path := firstInventoryNonEmpty(opts.Path, "targetgraph.yaml")
	revision, err := runGit(ctx, repo, "rev-parse", "--verify", ref+"^{commit}")
	if err != nil {
		return sourceReadResult{}, err
	}
	raw, err := runGitBytes(ctx, repo, "show", strings.TrimSpace(revision)+":"+path)
	if err != nil {
		return sourceReadResult{}, err
	}
	dirtyState := "clean"
	if status, err := runGit(ctx, repo, "status", "--porcelain", "--", path); err == nil && strings.TrimSpace(status) != "" {
		dirtyState = "dirty"
	} else if err != nil {
		dirtyState = "unknown"
	}
	return sourceReadResult{
		Type:       "git",
		Ref:        repo,
		Path:       path,
		Revision:   strings.TrimSpace(revision),
		DirtyState: dirtyState,
		Raw:        raw,
	}, nil
}

func runGit(ctx context.Context, repo string, args ...string) (string, error) {
	raw, err := runGitBytes(ctx, repo, args...)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(raw)), nil
}

func runGitBytes(ctx context.Context, repo string, args ...string) ([]byte, error) {
	fullArgs := append([]string{"-C", repo}, args...)
	cmd := exec.CommandContext(ctx, "git", fullArgs...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	raw, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
	}
	return raw, nil
}

func digestSummary(summary targetgraph.Summary) string {
	raw, _ := json.Marshal(summary)
	return digestBytes(raw)
}

func digestBytes(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func redactSummary(summary targetgraph.Summary) targetgraph.Summary {
	summary.Name = RedactSecretRefs(summary.Name)
	summary.GroupIDs = redactAndSort(summary.GroupIDs)
	summary.TargetIDs = redactAndSort(summary.TargetIDs)
	summary.HostReachabilityRefs = redactAndSort(summary.HostReachabilityRefs)
	summary.TargetTypes = copyStringIntMap(summary.TargetTypes)
	summary.TransportKinds = copyStringIntMap(summary.TransportKinds)
	return summary
}

func redactSourceRef(value string) string {
	value = RedactSecretRefs(strings.TrimSpace(value))
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" {
		return value
	}
	switch parsed.Scheme {
	case "http", "https":
		if parsed.User != nil {
			parsed.User = url.User("[REDACTED]")
		}
		parsed.RawQuery = ""
		parsed.Fragment = ""
		return parsed.String()
	default:
		return value
	}
}

func redactAndSort(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, RedactSecretRefs(value))
	}
	sort.Strings(out)
	return out
}

func copyStringIntMap(values map[string]int) map[string]int {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]int, len(values))
	for key, value := range values {
		out[RedactSecretRefs(key)] = value
	}
	return out
}

func firstInventoryNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
