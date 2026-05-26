package agentappliance

import (
	"fmt"
	"strings"
)

func renderSummaryMarkdown(report Report) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# Agent Appliance Evidence\n\n")
	fmt.Fprintf(&b, "- Generated: %s\n", report.GeneratedAt)
	fmt.Fprintf(&b, "- Tool: %s\n", report.Tool)
	fmt.Fprintf(&b, "- Actor: %s\n", report.Actor)
	if report.Task != "" {
		fmt.Fprintf(&b, "- Task: %s\n", report.Task)
	}
	fmt.Fprintf(&b, "- Result: %s\n", upperPass(report.Passed))
	fmt.Fprintf(&b, "- Evidence directory: %s\n\n", report.OutDir)

	fmt.Fprintf(&b, "## Repo Intelligence\n\n")
	fmt.Fprintf(&b, "- Root: %s\n", report.Repo.Root)
	if report.Repo.Git.InRepo {
		fmt.Fprintf(&b, "- Git: %s @ %s (dirty: %t)\n", firstNonEmpty(report.Repo.Git.Branch, "detached"), firstNonEmpty(report.Repo.Git.Commit, "unknown"), report.Repo.Git.Dirty)
	}
	fmt.Fprintf(&b, "- Files: %d\n", report.Summary.RepoFiles)
	fmt.Fprintf(&b, "- Changed files: %d\n", report.Summary.ChangedFiles)
	fmt.Fprintf(&b, "- Dependency manifests: %d\n", report.Summary.DependencyManifests)
	fmt.Fprintf(&b, "- Test files: %d\n\n", report.Summary.TestFiles)

	fmt.Fprintf(&b, "## Workbenches\n\n")
	fmt.Fprintf(&b, "- API probes: %d passed, %d failed\n", report.Summary.APIPassed, report.Summary.APIFailed)
	fmt.Fprintf(&b, "- Browser probes: %d captured, %d skipped, %d failed\n", report.Summary.BrowserCaptured, report.Summary.BrowserSkipped, report.Summary.BrowserFailed)
	fmt.Fprintf(&b, "- Command checks: %d passed, %d failed, %d timed out\n\n", report.Summary.CommandChecksPassed, report.Summary.CommandChecksFailed, report.Summary.CommandChecksTimedOut)

	if len(report.Repo.Impact) > 0 {
		fmt.Fprintf(&b, "## Test Impact\n\n")
		limit := len(report.Repo.Impact)
		if limit > 20 {
			limit = 20
		}
		for _, entry := range report.Repo.Impact[:limit] {
			tests := "none detected"
			if len(entry.TestFiles) > 0 {
				tests = strings.Join(entry.TestFiles, ", ")
			}
			fmt.Fprintf(&b, "- `%s`: %s\n", entry.ChangedFile, tests)
		}
		fmt.Fprintf(&b, "\n")
	}

	if len(report.Warnings) > 0 {
		fmt.Fprintf(&b, "## Warnings\n\n")
		for _, warning := range report.Warnings {
			fmt.Fprintf(&b, "- %s\n", warning)
		}
		fmt.Fprintf(&b, "\n")
	}

	fmt.Fprintf(&b, "## Evidence Files\n\n")
	if len(report.Evidence) == 0 {
		fmt.Fprintf(&b, "- Evidence file hashes are listed in manifest.json.\n")
	} else {
		for _, file := range report.Evidence {
			fmt.Fprintf(&b, "- `%s` %s (%d bytes)\n", file.Path, file.SHA256, file.Bytes)
		}
	}
	return b.String()
}

func upperPass(ok bool) string {
	if ok {
		return "PASSED"
	}
	return "FAILED"
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}
