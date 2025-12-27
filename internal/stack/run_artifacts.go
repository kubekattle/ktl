// File: internal/stack/run_artifacts.go
// Brief: Durable run artifacts (plan/events/summary) for resume/debugging.

package stack

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
)

type RunPlan struct {
	APIVersion  string             `json:"apiVersion"`
	RunID       string             `json:"runId"`
	PlanHash    string             `json:"planHash,omitempty"`
	StackRoot   string             `json:"stackRoot"`
	StackName   string             `json:"stackName"`
	Command     string             `json:"command"`
	Profile     string             `json:"profile"`
	Concurrency int                `json:"concurrency"`
	FailMode    string             `json:"failMode"`
	Selector    RunSelector        `json:"selector,omitempty"`
	Nodes       []*ResolvedRelease `json:"nodes"`

	StackGitCommit string `json:"stackGitCommit,omitempty"`
	StackGitDirty  bool   `json:"stackGitDirty,omitempty"`

	KtlVersion   string `json:"ktlVersion,omitempty"`
	KtlGitCommit string `json:"ktlGitCommit,omitempty"`
}

type RunSelector struct {
	Clusters             []string `json:"clusters,omitempty"`
	Tags                 []string `json:"tags,omitempty"`
	FromPaths            []string `json:"fromPaths,omitempty"`
	Releases             []string `json:"releases,omitempty"`
	GitRange             string   `json:"gitRange,omitempty"`
	GitIncludeDeps       bool     `json:"gitIncludeDeps,omitempty"`
	GitIncludeDependents bool     `json:"gitIncludeDependents,omitempty"`
	IncludeDeps          bool     `json:"includeDeps,omitempty"`
	IncludeDependents    bool     `json:"includeDependents,omitempty"`
	AllowMissingDeps     bool     `json:"allowMissingDeps,omitempty"`
}

type RunEvent struct {
	Seq     int64     `json:"seq,omitempty"`
	TS      string    `json:"ts"`
	RunID   string    `json:"runId"`
	NodeID  string    `json:"nodeId,omitempty"`
	Type    string    `json:"type"`
	Attempt int       `json:"attempt"`
	Message string    `json:"message,omitempty"`
	Error   *RunError `json:"error,omitempty"`

	PrevDigest string `json:"prevDigest,omitempty"`
	Digest     string `json:"digest,omitempty"`
	CRC32      string `json:"crc32,omitempty"`
}

type RunError struct {
	Class   string `json:"class,omitempty"`
	Message string `json:"message,omitempty"`
	Digest  string `json:"digest,omitempty"`
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
		APIVersion:  "ktl.dev/stack-run/v1",
		RunID:       run.RunID,
		StackRoot:   p.StackRoot,
		StackName:   p.StackName,
		Command:     run.Command,
		Profile:     p.Profile,
		Concurrency: run.Concurrency,
		FailMode:    run.FailMode,
		Selector:    run.Selector,
		Nodes:       nodes,
	}
}

// ComputeRunPlanHash returns a stable sha256 hash of the run plan content.
// The hash is computed over the JSON form with PlanHash itself cleared.
func ComputeRunPlanHash(p *RunPlan) (string, error) {
	if p == nil {
		return "", nil
	}
	clone := *p
	clone.PlanHash = ""
	raw, err := json.Marshal(&clone)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:]), nil
}

func computeRunEventIntegrity(ev RunEvent) (digest string, crc string) {
	h := sha256.New()
	c := crc32.NewIEEE()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = c.Write([]byte(s))
		_, _ = h.Write([]byte{0})
		_, _ = c.Write([]byte{0})
	}
	write("ktl.stack-event.v1")
	write(fmt.Sprintf("seq=%d", ev.Seq))
	write(ev.TS)
	write(ev.RunID)
	write(ev.NodeID)
	write(ev.Type)
	write(fmt.Sprintf("attempt=%d", ev.Attempt))
	write(ev.Message)
	if ev.Error != nil {
		write(ev.Error.Class)
		write(ev.Error.Message)
		write(ev.Error.Digest)
	} else {
		write("")
		write("")
		write("")
	}
	write(ev.PrevDigest)

	return "sha256:" + hex.EncodeToString(h.Sum(nil)), fmt.Sprintf("crc32:%08x", c.Sum32())
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
