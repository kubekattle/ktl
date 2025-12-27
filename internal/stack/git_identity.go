// File: internal/stack/git_identity.go
// Brief: Git identity (commit + dirty state) for sealed stack plans.

package stack

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

type GitIdentity struct {
	Commit string
	Dirty  bool
}

// GitIdentityForRoot returns the current git HEAD commit and whether the working tree
// is dirty. If root is not a git work tree, it returns an empty commit and Dirty=false.
func GitIdentityForRoot(root string) (GitIdentity, error) {
	if strings.TrimSpace(root) == "" {
		root = "."
	}

	if ok, err := isGitWorkTree(root); err != nil {
		return GitIdentity{}, err
	} else if !ok {
		return GitIdentity{}, nil
	}

	commit, err := gitStdout(root, "rev-parse", "HEAD")
	if err != nil {
		return GitIdentity{}, err
	}
	dirtyOut, err := gitStdout(root, "status", "--porcelain")
	if err != nil {
		return GitIdentity{}, err
	}
	return GitIdentity{
		Commit: commit,
		Dirty:  strings.TrimSpace(dirtyOut) != "",
	}, nil
}

func isGitWorkTree(root string) (bool, error) {
	out, err := gitStdout(root, "rev-parse", "--is-inside-work-tree")
	if err != nil {
		// Not a git repo (or git missing) should not be fatal for stack use.
		return false, nil
	}
	return strings.TrimSpace(out) == "true", nil
}

func gitStdout(root string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", root}, args...)...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg != "" {
			return "", fmt.Errorf("git %s: %s", strings.Join(args, " "), msg)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return strings.TrimSpace(stdout.String()), nil
}
