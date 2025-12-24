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
	"golang.org/x/term"
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
	phases     map[string]phaseBadge
	graph      *buildGraphSnapshot
	cacheHits  int
	cacheMiss  int
	finished   bool
	success    bool
	events     []string
	sections   []consoleSection
	totalLines int
	startedAt  time.Time
}

var buildPhaseOrder = []string{"policy-pre", "solve", "export", "policy-post", "attest", "push", "load", "done"}

func NewBuildConsole(out io.Writer, meta BuildMetadata, opts BuildConsoleOptions) *BuildConsole {
	phases := make(map[string]phaseBadge, len(buildPhaseOrder))
	for _, name := range buildPhaseOrder {
		phases[name] = phaseBadge{Name: name, State: "pending"}
	}
	return &BuildConsole{
		out:       out,
		opts:      opts,
		meta:      meta,
		phases:    phases,
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
	source := strings.ToLower(strings.TrimSpace(rec.Source))
	switch strings.ToLower(strings.TrimSpace(rec.Source)) {
	case "graph":
		var snap buildGraphSnapshot
		if err := json.Unmarshal([]byte(rec.Raw), &snap); err == nil {
			c.graph = &snap
			c.pushEventLocked("Build graph updated")
		}
	case "diagnostic":
		c.consumeDiagnosticLocked(rec)
	case "build":
		raw := strings.TrimSpace(rec.Raw)
		if strings.HasPrefix(raw, "Summary:") {
			c.consumeSummaryLocked(strings.TrimSpace(strings.TrimPrefix(raw, "Summary:")))
			c.pushEventLocked("Summary updated")
		}
		if strings.TrimSpace(rec.SourceGlyph) == "✔" || strings.TrimSpace(rec.SourceGlyph) == "✖" {
			c.finished = true
			c.success = strings.TrimSpace(rec.SourceGlyph) == "✔"
			c.updatePhaseLocked("done", map[bool]string{true: "completed", false: "failed"}[c.success], "")
		}
	case "phase":
		c.consumePhaseLocked(rec)
	}
	if sev := buildSeverity(rec); sev != "" {
		c.warning = &consoleWarning{Severity: sev, Message: clipConsoleLine(strings.TrimSpace(rec.Rendered), 240), IssuedAt: time.Now()}
	} else if strings.TrimSpace(rec.SourceGlyph) == "ℹ" || strings.TrimSpace(rec.SourceGlyph) == "ⓘ" {
		// Clear the banner on explicit "info" events so older errors don't stick forever.
		c.warning = nil
	}
	if msg := strings.TrimSpace(rec.Rendered); msg != "" && source != "graph" && source != "phase" {
		c.pushEventLocked(clipConsoleLine(msg, 240))
	}
}

func (c *BuildConsole) consumeDiagnosticLocked(rec tailer.LogRecord) {
	switch strings.TrimSpace(rec.SourceGlyph) {
	case "✔":
		c.cacheHits++
	case "⚠":
		c.cacheMiss++
		// Cache misses are normal; keep them out of the warning banner.
	}
}

func (c *BuildConsole) consumePhaseLocked(rec tailer.LogRecord) {
	name := strings.ToLower(strings.TrimSpace(rec.Pod))
	if name == "" {
		name = strings.ToLower(strings.TrimSpace(rec.Namespace))
	}
	if name == "" {
		return
	}
	state := strings.ToLower(strings.TrimSpace(rec.Container))
	if state == "" {
		state = "running"
	}
	c.updatePhaseLocked(name, state, strings.TrimSpace(rec.Rendered))
}

func (c *BuildConsole) updatePhaseLocked(name, state, message string) {
	if c == nil || c.phases == nil {
		return
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	badge := c.phases[key]
	badge.Name = key
	badge.State = state
	badge.Message = strings.TrimSpace(message)
	c.phases[key] = badge
}

func (c *BuildConsole) consumeSummaryLocked(payload string) {
	type summary struct {
		CacheHits   int `json:"cacheHits"`
		CacheMisses int `json:"cacheMisses"`
	}
	var parsed summary
	if err := json.Unmarshal([]byte(payload), &parsed); err != nil {
		return
	}
	if parsed.CacheHits > 0 {
		c.cacheHits = parsed.CacheHits
	}
	if parsed.CacheMisses > 0 {
		c.cacheMiss = parsed.CacheMisses
	}
}

func buildSeverity(rec tailer.LogRecord) string {
	switch strings.TrimSpace(rec.SourceGlyph) {
	case "✖":
		return "error"
	}
	return ""
}

func clipConsoleLine(s string, max int) string {
	s = strings.TrimSpace(s)
	if max <= 0 || len(s) <= max {
		return s
	}
	if max <= 3 {
		return s[:max]
	}
	return s[:max-3] + "..."
}

func (c *BuildConsole) pushEventLocked(message string) {
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	if len(c.events) > 0 && c.events[len(c.events)-1] == message {
		return
	}
	c.events = append(c.events, message)
	if len(c.events) > 5 {
		c.events = c.events[len(c.events)-5:]
	}
}

func (c *BuildConsole) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	c.opts.Width = c.terminalWidthLocked()
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *BuildConsole) terminalWidthLocked() int {
	type fdProvider interface {
		Fd() uintptr
	}
	if c.out == nil {
		return c.opts.Width
	}
	if v, ok := c.out.(fdProvider); ok {
		if cols, _, err := term.GetSize(int(v.Fd())); err == nil && cols > 0 {
			return cols
		}
	}
	return c.opts.Width
}

func (c *BuildConsole) applyDiffLocked(newSections []consoleSection) {
	newTotal := countLines(newSections)
	if len(c.sections) == 0 {
		c.writeSections(newSections)
		c.sections = cloneSections(newSections)
		c.totalLines = newTotal
		return
	}
	idx := diffIndex(c.sections, newSections)
	if idx == -1 && newTotal == c.totalLines {
		return
	}
	if idx == -1 {
		idx = len(newSections)
	}
	startLine := sumLines(c.sections[:idx])
	linesBelow := c.totalLines - startLine
	if linesBelow > 0 {
		fmt.Fprintf(c.out, "\x1b[%dF", linesBelow)
	}
	fmt.Fprint(c.out, "\x1b[J")
	c.writeSections(newSections[idx:])
	c.sections = cloneSections(newSections)
	c.totalLines = newTotal
}

func (c *BuildConsole) writeSections(sections []consoleSection) {
	for _, section := range sections {
		for _, line := range section.lines {
			fmt.Fprintf(c.out, "%s\x1b[K\n", line)
		}
	}
	if len(sections) == 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
	}
}

func (c *BuildConsole) buildSectionsLocked() []consoleSection {
	sections := []consoleSection{
		{name: "metadata", lines: c.renderMetadataLinesLocked()},
		{name: "phases", lines: []string{c.renderPhasesLocked()}},
		{name: "summary", lines: c.renderSummaryLinesLocked()},
	}
	if c.warning != nil {
		sections = append(sections, consoleSection{name: "warning", lines: []string{renderWarning(*c.warning)}})
	}
	sections = append(sections, consoleSection{name: "graph", lines: c.renderGraphLinesLocked()})
	if len(c.events) > 0 {
		lines := make([]string, 0, len(c.events))
		for _, evt := range c.events {
			lines = append(lines, "• "+evt)
		}
		sections = append(sections, consoleSection{name: "events", lines: lines})
	}
	return sections
}

func (c *BuildConsole) renderPhasesLocked() string {
	if len(c.phases) == 0 {
		return ""
	}
	parts := make([]string, 0, len(buildPhaseOrder))
	for _, name := range buildPhaseOrder {
		badge, ok := c.phases[name]
		if !ok {
			continue
		}
		state := strings.ToLower(strings.TrimSpace(badge.State))
		label := name
		switch state {
		case "running":
			label = color.New(color.FgHiBlue).Sprintf("%s*", name)
		case "completed", "success":
			label = color.New(color.FgHiGreen).Sprint(name)
		case "failed", "error":
			label = color.New(color.FgHiRed).Sprint(name)
		}
		parts = append(parts, label)
	}
	if len(parts) == 0 {
		return ""
	}
	return "Phases: " + strings.Join(parts, " → ")
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
	sort.Slice(running, func(i, j int) bool { return running[i].Label < running[j].Label })
	sort.Slice(pending, func(i, j int) bool { return pending[i].Label < pending[j].Label })
	sort.Slice(done, func(i, j int) bool { return done[i].Label < done[j].Label })
	sort.Slice(failed, func(i, j int) bool { return failed[i].Label < failed[j].Label })
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
			if n.Total > 0 {
				pct := float64(0)
				if n.Current > 0 {
					pct = (float64(n.Current) / float64(n.Total)) * 100
				}
				progress = fmt.Sprintf(" (%.0f%%)", pct)
			}
			lines = append(lines, fmt.Sprintf("%s %s%s · %s", prefix, token, progress, label))
		}
	}
	emit("•", failed, 3)
	emit("•", running, maxLines-len(lines))
	if !c.finished && len(lines) < maxLines {
		emit("•", pending, maxLines-len(lines))
	} else if c.finished {
		// Keep the final screen focused; pending vertices often remain in snapshots.
		lines = append(lines, fmt.Sprintf("Done: %s", map[bool]string{true: "success", false: "failed"}[c.success]))
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
