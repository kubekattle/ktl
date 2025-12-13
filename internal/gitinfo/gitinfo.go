// gitinfo.go reads Git metadata to stamp artifacts and version output.
package gitinfo

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// Head returns the current git commit hash and dirty state if the repository is available.
func Head(ctx context.Context) (commit string, dirty bool, err error) {
	cmd := exec.CommandContext(ctx, "git", "rev-parse", "HEAD")
	output, err := cmd.Output()
	if err != nil {
		return "", false, err
	}
	commit = strings.TrimSpace(string(output))
	statusCmd := exec.CommandContext(ctx, "git", "status", "--porcelain")
	statusOut, err := statusCmd.Output()
	if err != nil {
		return commit, false, fmt.Errorf("git status: %w", err)
	}
	dirty = len(strings.TrimSpace(string(statusOut))) > 0
	return commit, dirty, nil
}
