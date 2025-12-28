package stack

import (
	"os"
	"strings"
)

func ciRunURLFromEnv() string {
	// GitHub Actions: build a stable link if possible.
	ghServer := strings.TrimSpace(os.Getenv("GITHUB_SERVER_URL"))
	ghRepo := strings.TrimSpace(os.Getenv("GITHUB_REPOSITORY"))
	ghRunID := strings.TrimSpace(os.Getenv("GITHUB_RUN_ID"))
	if ghServer != "" && ghRepo != "" && ghRunID != "" {
		return ghServer + "/" + ghRepo + "/actions/runs/" + ghRunID
	}
	if v := strings.TrimSpace(os.Getenv("GITHUB_RUN_URL")); v != "" {
		return v
	}

	// GitLab CI.
	if v := strings.TrimSpace(os.Getenv("CI_PIPELINE_URL")); v != "" {
		return v
	}

	// Buildkite.
	if v := strings.TrimSpace(os.Getenv("BUILDKITE_BUILD_URL")); v != "" {
		return v
	}

	// CircleCI.
	if v := strings.TrimSpace(os.Getenv("CIRCLE_BUILD_URL")); v != "" {
		return v
	}

	return ""
}
