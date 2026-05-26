package agentappliance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	defaultTimeout        = 2 * time.Minute
	defaultMaxOutputBytes = 65536
)

// DefaultOutDir returns the conventional evidence directory for a run.
func DefaultOutDir(repoDir string, now time.Time) string {
	if strings.TrimSpace(repoDir) == "" {
		repoDir = "."
	}
	return filepath.Join(repoDir, ".torque", "agent-appliance", now.UTC().Format("20060102-150405"))
}

// Run collects repo, API, browser, and command-check evidence into an output directory.
func Run(ctx context.Context, opts Options) (*Report, error) {
	opts = normalizeOptions(opts)
	now := opts.Now().UTC()
	repoDir, err := filepath.Abs(opts.RepoDir)
	if err != nil {
		return nil, fmt.Errorf("resolve repo dir: %w", err)
	}
	if info, err := os.Stat(repoDir); err != nil {
		return nil, fmt.Errorf("stat repo dir: %w", err)
	} else if !info.IsDir() {
		return nil, fmt.Errorf("repo dir is not a directory: %s", repoDir)
	}

	if strings.TrimSpace(opts.OutDir) == "" {
		opts.OutDir = DefaultOutDir(repoDir, now)
	}
	outDir, err := filepath.Abs(opts.OutDir)
	if err != nil {
		return nil, fmt.Errorf("resolve out dir: %w", err)
	}

	repo := collectRepo(ctx, repoDir, opts)
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return nil, fmt.Errorf("create evidence dir: %w", err)
	}

	report := &Report{
		Version:          Version,
		GeneratedAt:      now.Format(time.RFC3339Nano),
		Tool:             ToolName,
		Actor:            opts.Actor,
		Task:             strings.TrimSpace(opts.Task),
		OutDir:           outDir,
		RedactionEnabled: true,
		Repo:             repo,
	}

	if err := writeJSON(filepath.Join(outDir, "repo.json"), repo); err != nil {
		return nil, fmt.Errorf("write repo evidence: %w", err)
	}

	api := probeAPI(ctx, opts)
	report.API = api
	if err := writeJSON(filepath.Join(outDir, "api.json"), api); err != nil {
		return nil, fmt.Errorf("write api evidence: %w", err)
	}

	checks := runChecks(ctx, repoDir, outDir, opts)
	report.Checks = checks
	if err := writeJSON(filepath.Join(outDir, "checks.json"), checks); err != nil {
		return nil, fmt.Errorf("write command check evidence: %w", err)
	}

	browser := runBrowser(ctx, repoDir, outDir, opts)
	report.Browser = browser
	if err := writeJSON(filepath.Join(outDir, "browser.json"), browser); err != nil {
		return nil, fmt.Errorf("write browser evidence: %w", err)
	}

	report.Summary = summarize(*report)
	report.Passed = report.Summary.APIFailed == 0 &&
		report.Summary.BrowserFailed == 0 &&
		report.Summary.CommandChecksFailed == 0 &&
		report.Summary.CommandChecksTimedOut == 0
	report.RawSecretStored = browser.Captured > 0
	report.Warnings = collectWarnings(repo, api, browser, checks)
	if report.RawSecretStored {
		report.Warnings = append(report.Warnings, "browser screenshots and DOM artifacts may contain page-rendered sensitive data")
	}

	summaryPath := filepath.Join(outDir, "summary.md")
	if err := os.WriteFile(summaryPath, []byte(renderSummaryMarkdown(*report)), 0o644); err != nil {
		return nil, fmt.Errorf("write summary: %w", err)
	}
	report.SummaryPath = summaryPath

	evidence, err := collectEvidenceFiles(outDir)
	if err != nil {
		return nil, fmt.Errorf("collect evidence hashes: %w", err)
	}
	report.Evidence = evidence

	manifestPath := filepath.Join(outDir, "manifest.json")
	report.ManifestPath = manifestPath
	if err := writeJSON(manifestPath, report); err != nil {
		return nil, fmt.Errorf("write manifest: %w", err)
	}
	manifestHash, _, err := hashFile(manifestPath)
	if err != nil {
		return nil, fmt.Errorf("hash manifest: %w", err)
	}
	report.ManifestSHA256 = manifestHash
	if err := os.WriteFile(filepath.Join(outDir, "manifest.sha256"), []byte(manifestHash+"  manifest.json\n"), 0o644); err != nil {
		return nil, fmt.Errorf("write manifest checksum: %w", err)
	}
	return report, nil
}

func normalizeOptions(opts Options) Options {
	if strings.TrimSpace(opts.RepoDir) == "" {
		opts.RepoDir = "."
	}
	if strings.TrimSpace(opts.Actor) == "" {
		opts.Actor = "agent"
	}
	opts.BrowserMode = strings.ToLower(strings.TrimSpace(opts.BrowserMode))
	if opts.BrowserMode == "" || opts.BrowserMode == "auto" {
		opts.BrowserMode = "headless"
	}
	if opts.Timeout <= 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.MaxOutputBytes <= 0 {
		opts.MaxOutputBytes = defaultMaxOutputBytes
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	opts.BrowserURLs = cleanList(opts.BrowserURLs)
	opts.APIURLs = cleanList(opts.APIURLs)
	opts.Checks = cleanList(opts.Checks)
	return opts
}

func summarize(report Report) Summary {
	return Summary{
		RepoFiles:             report.Repo.FileCount,
		ChangedFiles:          len(report.Repo.ChangedFiles),
		DependencyManifests:   len(report.Repo.DependencyManifests),
		TestFiles:             len(report.Repo.TestFiles),
		APIProbes:             report.API.Total,
		APIPassed:             report.API.Passed,
		APIFailed:             report.API.Failed,
		BrowserProbes:         report.Browser.Total,
		BrowserCaptured:       report.Browser.Captured,
		BrowserSkipped:        report.Browser.Skipped,
		BrowserFailed:         report.Browser.Failed,
		CommandChecks:         report.Checks.Total,
		CommandChecksPassed:   report.Checks.Passed,
		CommandChecksFailed:   report.Checks.Failed,
		CommandChecksTimedOut: report.Checks.TimedOut,
	}
}

func collectWarnings(repo RepoReport, api APIReport, browser BrowserReport, checks ChecksReport) []string {
	var warnings []string
	warnings = append(warnings, repo.Warnings...)
	for _, probe := range api.Probes {
		if probe.Error != "" {
			warnings = append(warnings, fmt.Sprintf("api probe failed for %s: %s", probe.URL, probe.Error))
		}
	}
	for _, capture := range browser.Captures {
		if capture.Skipped && capture.Reason != "" {
			warnings = append(warnings, fmt.Sprintf("browser probe skipped for %s: %s", capture.URL, capture.Reason))
		}
		if capture.Error != "" {
			warnings = append(warnings, fmt.Sprintf("browser probe failed for %s: %s", capture.URL, capture.Error))
		}
	}
	for _, result := range checks.Results {
		if result.Error != "" {
			warnings = append(warnings, fmt.Sprintf("check failed for %q: %s", result.Command, result.Error))
		}
	}
	sort.Strings(warnings)
	return uniqueStrings(warnings)
}

func writeJSON(path string, value any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return os.WriteFile(path, raw, 0o644)
}

func collectEvidenceFiles(outDir string) ([]EvidenceFile, error) {
	var files []EvidenceFile
	err := filepath.WalkDir(outDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		name := filepath.Base(path)
		if name == "manifest.json" || name == "manifest.sha256" {
			return nil
		}
		sum, size, err := hashFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(outDir, path)
		if err != nil {
			return err
		}
		files = append(files, EvidenceFile{
			Path:   filepath.ToSlash(rel),
			SHA256: sum,
			Bytes:  size,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool { return files[i].Path < files[j].Path })
	return files, nil
}

func hashFile(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return "", 0, err
	}
	return hex.EncodeToString(h.Sum(nil)), n, nil
}

func cleanList(values []string) []string {
	var out []string
	for _, value := range values {
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func uniqueStrings(values []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, value := range values {
		if value == "" || seen[value] {
			continue
		}
		seen[value] = true
		out = append(out, value)
	}
	return out
}
