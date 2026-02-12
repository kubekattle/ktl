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

	"github.com/fatih/color"
	"github.com/kubekattle/ktl/internal/tailer"
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

	mu               sync.Mutex
	meta             BuildMetadata
	warning          *consoleWarning
	phases           map[string]phaseBadge
	graph            *buildGraphSnapshot
	cacheHits        int
	cacheMiss        int
	finished         bool
	success          bool
	events           []string
	lastGraphEventAt time.Time
	lastRenderAt     time.Time
	sections         []consoleSection
	totalLines       int
	startedAt        time.Time
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
	now := time.Now()
	// Debounce repaints to reduce flicker and CPU churn under high-frequency progress updates.
	if strings.EqualFold(strings.TrimSpace(rec.Source), "result") || strings.TrimSpace(rec.SourceGlyph) == "✖" {
		c.renderLocked()
		c.lastRenderAt = now
		return
	}
	if !c.lastRenderAt.IsZero() && now.Sub(c.lastRenderAt) < 100*time.Millisecond {
		return
	}
	c.renderLocked()
	c.lastRenderAt = now
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
			// Graph updates can arrive very frequently; avoid swamping the event tail.
			if c.lastGraphEventAt.IsZero() || time.Since(c.lastGraphEventAt) > 2*time.Second {
				c.pushEventLocked("Build graph updated")
				c.lastGraphEventAt = time.Now()
			}
		}
	case "diagnostic":
		c.consumeDiagnosticLocked(rec)
	case "build":
		raw := strings.TrimSpace(rec.Raw)
		if strings.HasPrefix(raw, "Summary:") {
			c.consumeSummaryLocked(strings.TrimSpace(strings.TrimPrefix(raw, "Summary:")))
			c.pushEventLocked("Summary updated")
		}
	case "phase":
		c.consumePhaseLocked(rec)
	case "result":
		c.consumeResultLocked(rec)
	}
	if sev := buildSeverity(rec); sev != "" {
		c.warning = &consoleWarning{Severity: sev, Message: clipConsoleLine(strings.TrimSpace(rec.Rendered), 240), IssuedAt: time.Now()}
	} else if strings.TrimSpace(rec.SourceGlyph) == "ℹ" || strings.TrimSpace(rec.SourceGlyph) == "ⓘ" {
		// Clear the banner on explicit "info" events so older errors don't stick forever.
		c.warning = nil
	}
	if msg := strings.TrimSpace(rec.Rendered); msg != "" && source != "graph" && source != "phase" {
		if shouldIncludeBuildEvent(rec, msg) {
			c.pushEventLocked(clipConsoleLine(msg, 240))
		}
	}
}

func (c *BuildConsole) consumeResultLocked(rec tailer.LogRecord) {
	type payload struct {
		Success bool   `json:"success"`
		Error   string `json:"error,omitempty"`
	}
	var p payload
	_ = json.Unmarshal([]byte(strings.TrimSpace(rec.Raw)), &p)
	c.finished = true
	c.success = p.Success || strings.TrimSpace(rec.SourceGlyph) == "✔"
	state := "failed"
	if c.success {
		state = "completed"
	}
	c.updatePhaseLocked("done", state, strings.TrimSpace(p.Error))
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

func shouldIncludeBuildEvent(rec tailer.LogRecord, msg string) bool {
	if strings.EqualFold(strings.TrimSpace(rec.Source), "diagnostic") {
		return false
	}
	container := strings.ToLower(strings.TrimSpace(rec.Container))
	lower := strings.ToLower(msg)
	if container == "status" && (strings.Contains(lower, "cache miss") || strings.Contains(lower, "cache hit")) {
		return false
	}
	if container == "diagnostic" && (strings.Contains(lower, "cache miss") || strings.Contains(lower, "cache hit")) {
		return false
	}
	if strings.Contains(lower, "cache miss") || strings.Contains(lower, "cache hit") {
		// Heuristic: avoid spamming the tail with cache intel.
		return false
	}
	return true
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
	if strings.EqualFold(strings.TrimSpace(rec.Source), "build") && strings.EqualFold(strings.TrimSpace(rec.Container), "stderr") {
		return "warn"
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
	cached := make([]buildGraphNode, 0)
	failed := make([]buildGraphNode, 0)
	for _, n := range nodes {
		switch strings.ToLower(strings.TrimSpace(n.Status)) {
		case "failed":
			failed = append(failed, n)
		case "running":
			running = append(running, n)
		case "cached":
			cached = append(cached, n)
		case "completed":
			done = append(done, n)
		default:
			pending = append(pending, n)
		}
	}
	sort.Slice(running, func(i, j int) bool { return graphNodeOrder(running[i], running[j]) })
	sort.Slice(failed, func(i, j int) bool { return graphNodeOrder(failed[i], failed[j]) })
	lines := []string{}
	lines = append(lines, fmt.Sprintf("Steps: %d running · %d pending · %d done · %d cached · %d failed", len(running), len(pending), len(done), len(cached), len(failed)))
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
	if c.finished {
		lines = append(lines, fmt.Sprintf("Done: %s", map[bool]string{true: "success", false: "failed"}[c.success]))
		// Keep the final screen focused; pending vertices often remain in snapshots.
		return lines
	}
	// Pending vertices are often not helpful (they can linger in snapshots); keep them as a count only.
	return lines
}

func graphNodeOrder(a, b buildGraphNode) bool {
	aTS := a.StartedUnix
	if aTS == 0 {
		aTS = a.FirstSeenUnix
	}
	bTS := b.StartedUnix
	if bTS == 0 {
		bTS = b.FirstSeenUnix
	}
	if aTS != bTS {
		return aTS > bTS
	}
	return a.Label < b.Label
}

type buildGraphSnapshot struct {
	Nodes []buildGraphNode `json:"nodes"`
}

type buildGraphNode struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	Cached        bool   `json:"cached"`
	FirstSeenUnix int64  `json:"firstSeenUnix,omitempty"`
	StartedUnix   int64  `json:"startedUnix,omitempty"`
	CompletedUnix int64  `json:"completedUnix,omitempty"`
	Current       int64  `json:"current,omitempty"`
	Total         int64  `json:"total,omitempty"`
	Error         string `json:"error,omitempty"`
}
