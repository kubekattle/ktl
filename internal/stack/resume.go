// File: internal/stack/resume.go
// Brief: Resume and rerun-failed support from on-disk run artifacts.

package stack

import (
	"bufio"
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
	planPath := filepath.Join(runRoot, "plan.json")
	raw, err := os.ReadFile(planPath)
	if err != nil {
		return nil, err
	}
	var rp RunPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, fmt.Errorf("parse %s: %w", planPath, err)
	}

	p := &Plan{
		StackRoot: rp.StackRoot,
		StackName: rp.StackName,
		Profile:   rp.Profile,
		Nodes:     rp.Nodes,
		ByID:      map[string]*ResolvedRelease{},
		ByCluster: map[string][]*ResolvedRelease{},
	}
	for _, n := range p.Nodes {
		p.ByID[n.ID] = n
		p.ByCluster[n.Cluster.Name] = append(p.ByCluster[n.Cluster.Name], n)
	}
	if err := assignExecutionGroups(p); err != nil {
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
	for sc.Scan() {
		var ev RunEvent
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			return nil, nil, fmt.Errorf("parse %s: %w", path, err)
		}
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
	return status, attempt, nil
}

func DriftReport(p *Plan) ([]string, error) {
	var drift []string
	for _, n := range p.Nodes {
		want := strings.TrimSpace(n.EffectiveInputHash)
		if want == "" {
			continue
		}
		got, err := ComputeEffectiveInputHash(n, true)
		if err != nil {
			return nil, err
		}
		if got != want {
			drift = append(drift, fmt.Sprintf("%s inputs changed (%s -> %s)", n.ID, want, got))
		}
	}
	sort.Strings(drift)
	return drift, nil
}
