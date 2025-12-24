// File: internal/ui/build_console.go
// Brief: Internal ui package implementation for 'build console'.

package ui

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/tailer"
	"github.com/fatih/color"
)

type BuildConsoleOptions struct {
	Enabled bool
	Width   int
}

type BuildMetadata struct {
	ContextDir string
	Dockerfile string
	Tags       []string
	Platforms  []string
	Mode       string
	Push       bool
	Load       bool
}

type BuildConsole struct {
	out  io.Writer
	opts BuildConsoleOptions

	mu         sync.Mutex
	meta       BuildMetadata
	warning    *consoleWarning
	graph      *buildGraphSnapshot
	cacheHits  int
	cacheMiss  int
	lastEvent  string
	sections   []consoleSection
	totalLines int
	startedAt  time.Time
}

func NewBuildConsole(out io.Writer, meta BuildMetadata, opts BuildConsoleOptions) *BuildConsole {
	return &BuildConsole{
		out:       out,
		opts:      opts,
		meta:      meta,
		startedAt: time.Now(),
	}
}

func (c *BuildConsole) ObserveLog(rec tailer.LogRecord) {
	if c == nil || !c.opts.Enabled || c.out == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.consumeLocked(rec)
	c.renderLocked()
}

func (c *BuildConsole) Done() {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.totalLines > 0 {
		fmt.Fprintf(c.out, "\x1b[%dF\x1b[J", c.totalLines)
		c.totalLines = 0
		c.sections = nil
	}
}

func (c *BuildConsole) consumeLocked(rec tailer.LogRecord) {
	switch strings.ToLower(strings.TrimSpace(rec.Source)) {
	case "graph":
		var snap buildGraphSnapshot
		if err := json.Unmarshal([]byte(rec.Raw), &snap); err == nil {
			c.graph = &snap
		}
	case "diagnostic":
		switch strings.TrimSpace(rec.SourceGlyph) {
		case "✔":
			c.cacheHits++
		case "⚠":
			c.cacheMiss++
		}
	}
	level := normalizeSeverity(rec.Container)
	if level == "warn" || level == "error" {
		c.warning = &consoleWarning{Severity: level, Message: strings.TrimSpace(rec.Rendered), IssuedAt: time.Now()}
	} else if level == "info" {
		c.warning = nil
	}
	if msg := strings.TrimSpace(rec.Rendered); msg != "" {
		c.lastEvent = msg
	}
}

func (c *BuildConsole) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *BuildConsole) buildSectionsLocked() []consoleSection {
	sections := []consoleSection{
		{name: "metadata", lines: c.renderMetadataLinesLocked()},
		{name: "summary", lines: c.renderSummaryLinesLocked()},
	}
	if c.warning != nil {
		sections = append(sections, consoleSection{name: "warning", lines: []string{renderWarning(*c.warning)}})
	}
	sections = append(sections, consoleSection{name: "graph", lines: c.renderGraphLinesLocked()})
	if strings.TrimSpace(c.lastEvent) != "" {
		sections = append(sections, consoleSection{name: "last", lines: []string{fmt.Sprintf("Last: %s", c.lastEvent)}})
	}
	return sections
}

func (c *BuildConsole) renderMetadataLinesLocked() []string {
	meta := c.meta
	var lines []string
	title := "Building"
	if ctx := strings.TrimSpace(meta.ContextDir); ctx != "" {
		title = fmt.Sprintf("Building %s", ctx)
	}
	lines = append(lines, title)
	sub := []string{}
	if df := strings.TrimSpace(meta.Dockerfile); df != "" {
		sub = append(sub, fmt.Sprintf("Dockerfile=%s", df))
	}
	if len(meta.Platforms) > 0 {
		sub = append(sub, fmt.Sprintf("Platforms=%s", strings.Join(meta.Platforms, ",")))
	}
	if len(meta.Tags) > 0 {
		sub = append(sub, fmt.Sprintf("Tags=%s", strings.Join(meta.Tags, ",")))
	}
	if mode := strings.TrimSpace(meta.Mode); mode != "" {
		sub = append(sub, fmt.Sprintf("Mode=%s", mode))
	}
	if meta.Push {
		sub = append(sub, "Push=on")
	}
	if meta.Load {
		sub = append(sub, "Load=on")
	}
	if len(sub) > 0 {
		lines = append(lines, strings.Join(sub, " · "))
	}
	return lines
}

func (c *BuildConsole) renderSummaryLinesLocked() []string {
	elapsed := time.Since(c.startedAt).Round(time.Second)
	if elapsed < 0 {
		elapsed = 0
	}
	return []string{
		fmt.Sprintf("Elapsed: %s · Cache: %d hit / %d miss", elapsed, c.cacheHits, c.cacheMiss),
	}
}

func (c *BuildConsole) renderGraphLinesLocked() []string {
	if c.graph == nil || len(c.graph.Nodes) == 0 {
		return []string{"Waiting for BuildKit progress..."}
	}
	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	maxLines := 16
	nodes := append([]buildGraphNode(nil), c.graph.Nodes...)
	sort.Slice(nodes, func(i, j int) bool { return nodes[i].Label < nodes[j].Label })
	running := make([]buildGraphNode, 0)
	pending := make([]buildGraphNode, 0)
	done := make([]buildGraphNode, 0)
	failed := make([]buildGraphNode, 0)
	for _, n := range nodes {
		switch strings.ToLower(strings.TrimSpace(n.Status)) {
		case "failed":
			failed = append(failed, n)
		case "running":
			running = append(running, n)
		case "completed", "cached":
			done = append(done, n)
		default:
			pending = append(pending, n)
		}
	}
	lines := []string{}
	lines = append(lines, fmt.Sprintf("Steps: %d running · %d pending · %d done · %d failed", len(running), len(pending), len(done), len(failed)))
	emit := func(prefix string, list []buildGraphNode, limit int) {
		for i, n := range list {
			if i >= limit {
				break
			}
			label := n.Label
			if width < 100 && len(label) > 72 {
				label = label[:69] + "..."
			}
			state := strings.ToLower(strings.TrimSpace(n.Status))
			token := state
			switch state {
			case "running":
				token = color.New(color.FgHiBlue).Sprint("running")
			case "failed":
				token = color.New(color.FgHiRed).Sprint("failed")
			case "completed", "cached":
				token = color.New(color.FgHiGreen).Sprint(state)
			}
			progress := ""
			if n.Total > 0 || n.Current > 0 {
				progress = fmt.Sprintf(" (%d/%d)", n.Current, n.Total)
			}
			lines = append(lines, fmt.Sprintf("%s %s%s · %s", prefix, token, progress, label))
		}
	}
	emit("•", failed, 3)
	emit("•", running, maxLines-len(lines))
	if len(lines) < maxLines {
		emit("•", pending, maxLines-len(lines))
	}
	return lines
}

type buildGraphSnapshot struct {
	Nodes []buildGraphNode `json:"nodes"`
}

type buildGraphNode struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Cached  bool   `json:"cached"`
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Error   string `json:"error,omitempty"`
}

