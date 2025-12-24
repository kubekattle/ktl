// File: internal/stack/run_artifacts.go
// Brief: Durable run artifacts (plan/events/summary) for resume/debugging.

package stack

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
)

type RunPlan struct {
	APIVersion string             `json:"apiVersion"`
	RunID      string             `json:"runId"`
	StackRoot  string             `json:"stackRoot"`
	StackName  string             `json:"stackName"`
	Command    string             `json:"command"`
	Profile    string             `json:"profile"`
	Nodes      []*ResolvedRelease `json:"nodes"`
}

type RunEvent struct {
	TS      string `json:"ts"`
	RunID   string `json:"runId"`
	NodeID  string `json:"nodeId,omitempty"`
	Type    string `json:"type"`
	Attempt int    `json:"attempt"`
	Message string `json:"message,omitempty"`
}

type RunTotals struct {
	Planned   int `json:"planned"`
	Succeeded int `json:"succeeded"`
	Failed    int `json:"failed"`
	Blocked   int `json:"blocked"`
	Running   int `json:"running"`
}

type RunNodeSummary struct {
	Status  string `json:"status"`
	Attempt int    `json:"attempt,omitempty"`
	Error   string `json:"error,omitempty"`
}

type RunSummary struct {
	APIVersion string                    `json:"apiVersion"`
	RunID      string                    `json:"runId"`
	Status     string                    `json:"status"`
	StartedAt  string                    `json:"startedAt"`
	UpdatedAt  string                    `json:"updatedAt"`
	Totals     RunTotals                 `json:"totals"`
	Nodes      map[string]RunNodeSummary `json:"nodes"`
	Order      []string                  `json:"order,omitempty"`
}

func buildRunPlanPayload(run *runState, p *Plan) *RunPlan {
	nodes := append([]*ResolvedRelease(nil), p.Nodes...)
	return &RunPlan{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      run.RunID,
		StackRoot:  p.StackRoot,
		StackName:  p.StackName,
		Command:    run.Command,
		Profile:    p.Profile,
		Nodes:      nodes,
	}
}

func writeJSONAtomic(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}

func appendJSONLine(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	if err := enc.Encode(v); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	return f.Sync()
}
