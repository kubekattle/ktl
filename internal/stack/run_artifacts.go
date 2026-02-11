// File: internal/stack/run_artifacts.go
// Brief: Durable run artifacts (plan/events/summary) for resume/debugging.

package stack

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"hash/crc32"
	"strings"
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
	Runner      RunnerResolved     `json:"runner,omitempty"`

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
	Seq     int64          `json:"seq,omitempty"`
	TS      string         `json:"ts"`
	RunID   string         `json:"runId"`
	NodeID  string         `json:"nodeId,omitempty"`
	Type    string         `json:"type"`
	Attempt int            `json:"attempt"`
	Message string         `json:"message,omitempty"`
	Fields  map[string]any `json:"fields,omitempty"`
	Error   *RunError      `json:"error,omitempty"`

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
		Runner:      p.Runner,
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
	raw, _ := json.Marshal(ev.Fields)
	write(string(raw))
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

// VerifyRunEventChain checks the per-event digest/crc chain produced by computeRunEventIntegrity.
// If the chain is absent (digest/crc empty on the first event), it returns nil for backward compatibility.
func VerifyRunEventChain(events []RunEvent) error {
	if len(events) == 0 {
		return nil
	}
	if strings.TrimSpace(events[0].Digest) == "" || strings.TrimSpace(events[0].CRC32) == "" {
		return nil
	}
	prev := ""
	for i := range events {
		ev := events[i]
		if strings.TrimSpace(ev.PrevDigest) != strings.TrimSpace(prev) {
			return fmt.Errorf("event[%d] prevDigest mismatch (want %q got %q)", i, prev, ev.PrevDigest)
		}
		wantDigest, wantCRC := computeRunEventIntegrity(ev)
		if strings.TrimSpace(ev.Digest) != strings.TrimSpace(wantDigest) {
			return fmt.Errorf("event[%d] digest mismatch (want %q got %q)", i, wantDigest, ev.Digest)
		}
		if strings.TrimSpace(ev.CRC32) != strings.TrimSpace(wantCRC) {
			return fmt.Errorf("event[%d] crc32 mismatch (want %q got %q)", i, wantCRC, ev.CRC32)
		}
		prev = ev.Digest
	}
	return nil
}

func computeRunDigest(planJSON string, summaryJSON string, lastEventDigest string) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-run.v1")
	write(planJSON)
	write(summaryJSON)
	write(strings.TrimSpace(lastEventDigest))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}

func computeRunErrorDigest(class string, message string) string {
	h := sha256.New()
	write := func(s string) {
		_, _ = h.Write([]byte(s))
		_, _ = h.Write([]byte{0})
	}
	write("ktl.stack-error.v1")
	write(strings.TrimSpace(class))
	write(strings.TrimSpace(message))
	return "sha256:" + hex.EncodeToString(h.Sum(nil))
}
