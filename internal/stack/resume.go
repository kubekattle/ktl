// File: internal/stack/resume.go
// Brief: Resume and rerun-failed support from on-disk run artifacts.

package stack

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type ResumeOptions struct {
	RunID       string
	StackRunDir string // default .ktl/stack/runs

	AllowDrift        bool
	RerunFailed       bool
	IncludeDependents bool
}

type LoadedRun struct {
	RunRoot     string
	Plan        *Plan
	StatusByID  map[string]string
	AttemptByID map[string]int
}

func LoadMostRecentRun(root string) (string, error) {
	// Prefer sqlite state store when present.
	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err == nil {
		s, err := openStackStateStore(root, true)
		if err != nil {
			return "", err
		}
		defer s.Close()
		runID, err := s.MostRecentRunID(context.Background())
		if err != nil {
			return "", err
		}
		return filepath.Join(root, ".ktl", "stack", "runs", runID), nil
	}

	base := filepath.Join(root, ".ktl", "stack", "runs")
	entries, err := os.ReadDir(base)
	if err != nil {
		return "", err
	}
	var dirs []string
	for _, e := range entries {
		if e.IsDir() {
			dirs = append(dirs, e.Name())
		}
	}
	if len(dirs) == 0 {
		return "", fmt.Errorf("no previous runs found under %s", base)
	}
	sort.Strings(dirs)
	return filepath.Join(base, dirs[len(dirs)-1]), nil
}

func LoadRun(runRoot string) (*LoadedRun, error) {
	runID := filepath.Base(runRoot)
	// runRoot is <stackRoot>/.ktl/stack/runs/<run-id>
	root := filepath.Clean(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(runRoot)))))
	statePath := filepath.Join(root, stackStateSQLiteRelPath)
	if _, err := os.Stat(statePath); err == nil {
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
			RunRoot:     runRoot,
			Plan:        p,
			StatusByID:  statusByID,
			AttemptByID: attemptByID,
		}, nil
	}

	planPath := filepath.Join(runRoot, "plan.json")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	var rp RunPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, fmt.Errorf("parse %s: %w", planPath, err)
	}

	p, err := PlanFromRunPlan(&rp)
	if err != nil {
		return nil, err
	}

	statusByID, attemptByID, err := replayEvents(filepath.Join(runRoot, "events.jsonl"))
	if err != nil {
		return nil, err
	}

	return &LoadedRun{
		RunRoot:     runRoot,
		Plan:        p,
		StatusByID:  statusByID,
		AttemptByID: attemptByID,
	}, nil
}

func replayEvents(path string) (map[string]string, map[string]int, error) {
	status := map[string]string{}
	attempt := map[string]int{}
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return status, attempt, nil
		}
		return nil, nil, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	var events []RunEvent
	for sc.Scan() {
		var ev RunEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
		events = append(events, ev)
		if strings.TrimSpace(ev.NodeID) == "" {
			continue
		}
		switch ev.Type {
		case "NODE_RUNNING":
			status[ev.NodeID] = "running"
		case "NODE_SUCCEEDED":
			status[ev.NodeID] = "succeeded"
		case "NODE_FAILED":
			status[ev.NodeID] = "failed"
		case "NODE_BLOCKED":
			status[ev.NodeID] = "blocked"
		}
		if ev.Attempt > attempt[ev.NodeID] {
			attempt[ev.NodeID] = ev.Attempt
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, err
	}
	if err := VerifyRunEventChain(events); err != nil {
		return nil, nil, fmt.Errorf("events integrity: %w", err)
	}
	return status, attempt, nil
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
				drift = append(drift, fmt.Sprintf("  apply options: atomic=%t wait=%t timeout=%s -> atomic=%t wait=%t timeout=%s",
					n.EffectiveInput.Apply.Atomic, n.EffectiveInput.Apply.Wait, n.EffectiveInput.Apply.Timeout,
					gotInput.Apply.Atomic, gotInput.Apply.Wait, gotInput.Apply.Timeout,
				))
			}
			if n.EffectiveInput.Delete.Digest != gotInput.Delete.Digest {
				drift = append(drift, fmt.Sprintf("  delete options: timeout=%s -> timeout=%s",
					n.EffectiveInput.Delete.Timeout, gotInput.Delete.Timeout,
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
