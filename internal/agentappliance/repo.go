package agentappliance

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

func collectRepo(ctx context.Context, repoDir string, opts Options) RepoReport {
	report := RepoReport{
		Root:           repoDir,
		LanguageCounts: map[string]int{},
	}
	git := collectGit(ctx, repoDir, opts)
	report.Git = git
	if git.InRepo {
		report.VCS = "git"
		report.ChangedFiles = git.ChangedFiles
	}

	files, tool, warnings := listRepoFiles(ctx, repoDir, git.InRepo, opts)
	report.SearchTool = tool
	report.Warnings = append(report.Warnings, warnings...)
	report.FileCount = len(files)
	report.LanguageCounts = countLanguages(files)
	report.DependencyManifests = findDependencyManifests(files)
	report.TestFiles = findTestFiles(files)
	report.Impact = inferImpact(report.ChangedFiles, report.TestFiles)
	return report
}

func collectGit(ctx context.Context, repoDir string, opts Options) GitReport {
	root, err := commandOutput(ctx, repoDir, opts.Timeout, "git", "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(root) == "" {
		return GitReport{InRepo: false}
	}
	branch, _ := commandOutput(ctx, repoDir, opts.Timeout, "git", "branch", "--show-current")
	commit, _ := commandOutput(ctx, repoDir, opts.Timeout, "git", "rev-parse", "--short=12", "HEAD")
	status, _ := commandOutput(ctx, repoDir, opts.Timeout, "git", "status", "--porcelain=v1", "-uall")
	changed := parseGitStatusFiles(status)
	return GitReport{
		InRepo:       true,
		Root:         strings.TrimSpace(root),
		Branch:       strings.TrimSpace(branch),
		Commit:       strings.TrimSpace(commit),
		Dirty:        strings.TrimSpace(status) != "",
		Status:       strings.TrimSpace(status),
		ChangedFiles: changed,
	}
}

func parseGitStatusFiles(status string) []string {
	var files []string
	for _, line := range strings.Split(status, "\n") {
		line = strings.TrimRight(line, "\r")
		if len(line) < 4 {
			continue
		}
		path := strings.TrimSpace(line[3:])
		if strings.Contains(path, " -> ") {
			parts := strings.Split(path, " -> ")
			path = strings.TrimSpace(parts[len(parts)-1])
		}
		path = strings.Trim(path, `"`)
		if path != "" {
			files = append(files, filepath.ToSlash(path))
		}
	}
	sort.Strings(files)
	return uniqueStrings(files)
}

func listRepoFiles(ctx context.Context, repoDir string, inGitRepo bool, opts Options) ([]string, string, []string) {
	if _, err := exec.LookPath("rg"); err == nil {
		out, err := commandOutput(ctx, repoDir, opts.Timeout, "rg", "--files", "--hidden", "-g", "!.git", "-g", "!.torque/agent-appliance")
		if err == nil {
			return normalizeFileList(out), "rg", nil
		}
	}
	if inGitRepo {
		out, err := commandOutput(ctx, repoDir, opts.Timeout, "git", "ls-files")
		if err == nil {
			return normalizeFileList(out), "git-ls-files", nil
		}
	}
	files, err := walkFiles(repoDir)
	if err != nil {
		return files, "walk", []string{"repo walk was partial: " + err.Error()}
	}
	return files, "walk", nil
}

func normalizeFileList(out string) []string {
	var files []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, filepath.ToSlash(line))
	}
	sort.Strings(files)
	return uniqueStrings(files)
}

func walkFiles(root string) ([]string, error) {
	var files []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() {
			switch name {
			case ".git", "node_modules", "vendor", "dist", "bin":
				return filepath.SkipDir
			}
			if path != root && filepath.ToSlash(path) == filepath.ToSlash(filepath.Join(root, ".torque", "agent-appliance")) {
				return filepath.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return uniqueStrings(files), err
}

func countLanguages(files []string) map[string]int {
	counts := map[string]int{}
	for _, file := range files {
		key := languageKey(file)
		counts[key]++
	}
	return counts
}

func languageKey(path string) string {
	base := filepath.Base(path)
	switch base {
	case "Dockerfile":
		return "dockerfile"
	case "Makefile":
		return "makefile"
	case "justfile":
		return "justfile"
	}
	if strings.HasPrefix(base, "Dockerfile.") {
		return "dockerfile"
	}
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(base)), ".")
	if ext == "" {
		return "no-ext"
	}
	return ext
}

func findDependencyManifests(files []string) []string {
	var out []string
	for _, file := range files {
		if isDependencyManifest(file) {
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

func isDependencyManifest(path string) bool {
	base := filepath.Base(path)
	switch base {
	case "go.mod", "go.sum", "package.json", "package-lock.json", "pnpm-lock.yaml", "yarn.lock",
		"Cargo.toml", "Cargo.lock", "pyproject.toml", "requirements.txt", "poetry.lock", "Pipfile",
		"Pipfile.lock", "pom.xml", "build.gradle", "settings.gradle", "build.gradle.kts",
		"settings.gradle.kts", "composer.json", "composer.lock", "Gemfile", "Gemfile.lock",
		"Dockerfile", "docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml",
		"Chart.yaml", "values.yaml", "Makefile", "Taskfile.yml", "Taskfile.yaml", "justfile":
		return true
	}
	switch {
	case strings.HasSuffix(base, ".csproj"):
		return true
	case strings.HasPrefix(base, "Dockerfile."):
		return true
	default:
		return false
	}
}

func findTestFiles(files []string) []string {
	var out []string
	for _, file := range files {
		if isTestFile(file) {
			out = append(out, file)
		}
	}
	sort.Strings(out)
	return out
}

func isTestFile(path string) bool {
	slash := filepath.ToSlash(path)
	base := filepath.Base(slash)
	lower := strings.ToLower(base)
	if strings.HasSuffix(lower, "_test.go") ||
		strings.HasPrefix(lower, "test_") ||
		strings.HasSuffix(lower, "_test.py") ||
		strings.Contains(lower, ".test.") ||
		strings.Contains(lower, ".spec.") {
		return true
	}
	return strings.Contains("/"+strings.ToLower(slash)+"/", "/test/") ||
		strings.Contains("/"+strings.ToLower(slash)+"/", "/tests/")
}

func inferImpact(changedFiles, testFiles []string) []ImpactEntry {
	if len(changedFiles) == 0 {
		return nil
	}
	var out []ImpactEntry
	for _, changed := range changedFiles {
		tests := relatedTests(changed, testFiles)
		entry := ImpactEntry{ChangedFile: changed, TestFiles: tests}
		if len(tests) > 0 {
			entry.Reason = "same directory or matching basename"
		} else {
			entry.Reason = "no nearby test file detected"
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ChangedFile < out[j].ChangedFile })
	return out
}

func relatedTests(changed string, testFiles []string) []string {
	changed = filepath.ToSlash(changed)
	dir := filepath.ToSlash(filepath.Dir(changed))
	stem := strings.TrimSuffix(filepath.Base(changed), filepath.Ext(changed))
	var matches []string
	for _, test := range testFiles {
		testDir := filepath.ToSlash(filepath.Dir(test))
		testStem := strings.TrimSuffix(filepath.Base(test), filepath.Ext(test))
		switch {
		case testDir == dir:
			matches = append(matches, test)
		case stem != "" && strings.Contains(testStem, stem):
			matches = append(matches, test)
		}
	}
	sort.Strings(matches)
	if len(matches) > 20 {
		matches = matches[:20]
	}
	return uniqueStrings(matches)
}
