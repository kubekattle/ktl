// File: internal/stack/resume.go
// Brief: Resume and rerun-failed support for stack runs.

package stack

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type LoadedRun struct {
	RootDir     string
	RunID       string
	Plan        *Plan
	StatusByID  map[string]string
	AttemptByID map[string]int
}

func LoadMostRecentRun(root string) (string, error) {
	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err == nil {
		s, err := openStackStateStore(root, true)
		if err != nil {
			return "", err
		}
		defer s.Close()
		return s.MostRecentRunID(context.Background())
	}
	return "", fmt.Errorf("no stack state found (expected %s)", statePath)
}

func LoadRun(root string, runID string) (*LoadedRun, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		root = "."
	}
	runID = strings.TrimSpace(runID)
	if runID == "" {
		return nil, fmt.Errorf("run id is required")
	}

	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err != nil {
		return nil, fmt.Errorf("missing stack state (expected %s)", statePath)
	}
	s, err := openStackStateStore(root, true)
	if err != nil {
		return nil, err
	}
	defer s.Close()
	if err := s.VerifyEventsIntegrity(context.Background(), runID); err != nil {
		return nil, fmt.Errorf("run %s events integrity: %w", runID, err)
	}

	p, err := s.GetRunPlan(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	// Runs can be moved/copied to a different directory; always treat the current
	// root (where state.sqlite lives) as the stack root for resume/drift checks.
	p.StackRoot = root
	statusByID, attemptByID, err := s.GetNodeStatus(context.Background(), runID)
	if err != nil {
		return nil, err
	}
	return &LoadedRun{
		RootDir:     root,
		RunID:       runID,
		Plan:        p,
		StatusByID:  statusByID,
		AttemptByID: attemptByID,
	}, nil
}

func DriftReport(p *Plan) ([]string, error) {
	var drift []string
	for _, n := range p.Nodes {
		want := strings.TrimSpace(n.EffectiveInputHash)
		if want == "" {
			continue
		}
		got, gotInput, err := ComputeEffectiveInputHash(p.StackRoot, n, true)
		if err != nil {
			return nil, err
		}
		if got != want {
			if n.EffectiveInput == nil || gotInput == nil {
				drift = append(drift, fmt.Sprintf("%s inputs changed (%s -> %s)", n.ID, want, got))
				continue
			}

			// Include a stable header so multiple diffs per node are easy to scan.
			drift = append(drift, fmt.Sprintf("%s inputs changed (%s -> %s):", n.ID, want, got))

			if n.EffectiveInput.KtlVersion != gotInput.KtlVersion || n.EffectiveInput.KtlGitCommit != gotInput.KtlGitCommit {
				drift = append(drift, fmt.Sprintf("  ktl: %s (%s) -> %s (%s)",
					n.EffectiveInput.KtlVersion, n.EffectiveInput.KtlGitCommit,
					gotInput.KtlVersion, gotInput.KtlGitCommit,
				))
			}
			if n.EffectiveInput.StackGitCommit != gotInput.StackGitCommit || n.EffectiveInput.StackGitDirty != gotInput.StackGitDirty {
				drift = append(drift, fmt.Sprintf("  stack git: %s dirty=%t -> %s dirty=%t",
					n.EffectiveInput.StackGitCommit, n.EffectiveInput.StackGitDirty,
					gotInput.StackGitCommit, gotInput.StackGitDirty,
				))
			}

			if n.EffectiveInput.Chart.Digest != gotInput.Chart.Digest {
				drift = append(drift, fmt.Sprintf("  chart digest: %s -> %s", n.EffectiveInput.Chart.Digest, gotInput.Chart.Digest))
			}
			if strings.TrimSpace(n.EffectiveInput.Chart.Version) != strings.TrimSpace(gotInput.Chart.Version) {
				drift = append(drift, fmt.Sprintf("  chart version: %q -> %q", n.EffectiveInput.Chart.Version, gotInput.Chart.Version))
			}
			if strings.TrimSpace(n.EffectiveInput.Chart.ResolvedVersion) != strings.TrimSpace(gotInput.Chart.ResolvedVersion) {
				drift = append(drift, fmt.Sprintf("  chart resolvedVersion: %q -> %q", n.EffectiveInput.Chart.ResolvedVersion, gotInput.Chart.ResolvedVersion))
			}

			if n.EffectiveInput.SetDigest != gotInput.SetDigest {
				drift = append(drift, fmt.Sprintf("  set digest: %s -> %s", n.EffectiveInput.SetDigest, gotInput.SetDigest))
			}
			if n.EffectiveInput.ClusterDigest != gotInput.ClusterDigest {
				drift = append(drift, fmt.Sprintf("  cluster digest: %s -> %s", n.EffectiveInput.ClusterDigest, gotInput.ClusterDigest))
			}

			if n.EffectiveInput.Apply.Digest != gotInput.Apply.Digest {
				drift = append(drift, fmt.Sprintf("  apply options: atomic=%t wait=%t createNamespace=%t timeout=%s -> atomic=%t wait=%t createNamespace=%t timeout=%s",
					n.EffectiveInput.Apply.Atomic, n.EffectiveInput.Apply.Wait, n.EffectiveInput.Apply.CreateNamespace, n.EffectiveInput.Apply.Timeout,
					gotInput.Apply.Atomic, gotInput.Apply.Wait, gotInput.Apply.CreateNamespace, gotInput.Apply.Timeout,
				))
			}
			if n.EffectiveInput.Delete.Digest != gotInput.Delete.Digest {
				drift = append(drift, fmt.Sprintf("  delete options: timeout=%s -> timeout=%s",
					n.EffectiveInput.Delete.Timeout, gotInput.Delete.Timeout,
				))
			}
			if n.EffectiveInput.Verify.Digest != gotInput.Verify.Digest {
				drift = append(drift, fmt.Sprintf("  verify: enabled=%t failOnWarnings=%t eventsWindow=%s -> enabled=%t failOnWarnings=%t eventsWindow=%s",
					n.EffectiveInput.Verify.Enabled, n.EffectiveInput.Verify.FailOnWarnings, n.EffectiveInput.Verify.EventsWindow,
					gotInput.Verify.Enabled, gotInput.Verify.FailOnWarnings, gotInput.Verify.EventsWindow,
				))
			}

			for _, line := range diffFileDigests("values", n.EffectiveInput.Values, gotInput.Values) {
				drift = append(drift, "  "+line)
			}
		}
	}
	return drift, nil
}

func diffFileDigests(label string, oldList []FileDigest, newList []FileDigest) []string {
	oldByPath := map[string]string{}
	for _, d := range oldList {
		oldByPath[d.Path] = d.Digest
	}
	newByPath := map[string]string{}
	for _, d := range newList {
		newByPath[d.Path] = d.Digest
	}

	paths := make([]string, 0, len(oldByPath)+len(newByPath))
	seen := map[string]struct{}{}
	for p := range oldByPath {
		paths = append(paths, p)
		seen[p] = struct{}{}
	}
	for p := range newByPath {
		if _, ok := seen[p]; ok {
			continue
		}
		paths = append(paths, p)
	}
	sort.Strings(paths)

	var out []string
	for _, p := range paths {
		od, okOld := oldByPath[p]
		nd, okNew := newByPath[p]
		switch {
		case okOld && okNew && od != nd:
			out = append(out, fmt.Sprintf("%s %s: %s -> %s", label, p, od, nd))
		case okOld && !okNew:
			out = append(out, fmt.Sprintf("%s %s: removed (was %s)", label, p, od))
		case !okOld && okNew:
			out = append(out, fmt.Sprintf("%s %s: added (%s)", label, p, nd))
		}
	}
	return out
}
