// File: internal/stack/git.go
// Brief: Git helpers for selection.

package stack

import (
	"bytes"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

func GitChangedFiles(root string, gitRange string) ([]string, error) {
	gr := strings.TrimSpace(gitRange)
	if gr == "" {
		return nil, nil
	}
	cmd := exec.Command("git", "-C", root, "diff", "--name-only", gr)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return nil, fmt.Errorf("git diff %s: %s", gr, msg)
		}
		return nil, fmt.Errorf("git diff %s: %w", gr, err)
	}
	var files []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		files = append(files, filepath.Clean(line))
	}
	return files, nil
}
