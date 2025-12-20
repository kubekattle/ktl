// File: internal/ui/deploy_console.go
// Brief: Internal ui package implementation for 'deploy console'.

// Package ui provides ui helpers.

package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/deploy"
	"github.com/fatih/color"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var phaseTitleCaser = cases.Title(language.Und, cases.NoLower)

type DeployConsoleOptions struct {
	Enabled         bool
	Wide            bool
	Width           int
	DetailsExpanded bool
}

type DeployMetadata struct {
	Release         string
	Namespace       string
	Chart           string
	ChartVersion    string
	ValuesFiles     []string
	SetValues       []string
	SetStringValues []string
}

type DeployConsole struct {
	out  io.Writer
	opts DeployConsoleOptions

	mu         sync.Mutex
	metadata   DeployMetadata
	phases     map[string]phaseBadge
	resources  []deploy.ResourceStatus
	warning    *consoleWarning
	sections   []consoleSection
	totalLines int
	details    bool
}

type phaseBadge struct {
	Name    string
	State   string
	Message string
}

type consoleWarning struct {
	Severity string
	Message  string
	IssuedAt time.Time
}

type consoleSection struct {
	name  string
	lines []string
}

var phaseOrder = []string{"render", "diff", "upgrade", "install", "wait", "post-hooks", "destroy"}

func NewDeployConsole(out io.Writer, meta DeployMetadata, opts DeployConsoleOptions) *DeployConsole {
	phases := make(map[string]phaseBadge, len(phaseOrder))
	for _, name := range phaseOrder {
		phases[name] = phaseBadge{Name: name, State: "pending"}
	}
	return &DeployConsole{
		out:      out,
		opts:     opts,
		metadata: meta,
		phases:   phases,
		details:  opts.DetailsExpanded,
	}
}

func (c *DeployConsole) UpdateMetadata(meta DeployMetadata) {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	c.metadata = meta
	c.renderLocked()
	c.mu.Unlock()
}

func (c *DeployConsole) UpdateResources(rows []deploy.ResourceStatus) {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	c.resources = cloneStatusRows(rows)
	c.renderLocked()
	c.mu.Unlock()
}

func (c *DeployConsole) PhaseStarted(name string) {
	c.updatePhase(name, "running", "")
}

func (c *DeployConsole) PhaseCompleted(name, status, message string) {
	c.updatePhase(name, normalizePhaseState(status), message)
}

func (c *DeployConsole) EmitEvent(level, message string) {
	lvl := normalizeSeverity(level)
	if lvl == "warn" || lvl == "error" {
		c.setWarning(lvl, message)
		return
	}
	if lvl == "info" {
		c.clearWarning()
	}
}

func (c *DeployConsole) SetDiff(string) {}

func (c *DeployConsole) Done() {
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

func (c *DeployConsole) updatePhase(name, state, message string) {
	if c == nil || !c.opts.Enabled {
		return
	}
	key := strings.ToLower(strings.TrimSpace(name))
	if key == "" {
		return
	}
	c.mu.Lock()
	badge := c.phases[key]
	badge.Name = key
	badge.State = state
	badge.Message = strings.TrimSpace(message)
	c.phases[key] = badge
	c.renderLocked()
	c.mu.Unlock()
}

func (c *DeployConsole) setWarning(severity, message string) {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	c.warning = &consoleWarning{Severity: severity, Message: message, IssuedAt: time.Now()}
	c.renderLocked()
	c.mu.Unlock()
}

func (c *DeployConsole) clearWarning() {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	if c.warning != nil {
		c.warning = nil
		c.renderLocked()
	}
	c.mu.Unlock()
}

func (c *DeployConsole) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *DeployConsole) buildSectionsLocked() []consoleSection {
	sections := []consoleSection{
		{name: "metadata", lines: c.renderMetadataLinesLocked()},
		{name: "phases", lines: []string{formatPhases(c.phases)}},
	}
	if c.warning != nil {
		sections = append(sections, consoleSection{name: "warning", lines: []string{renderWarning(*c.warning)}})
	}
	sections = append(sections, consoleSection{name: "resources", lines: c.renderResourceLines()})
	return sections
}

func (c *DeployConsole) applyDiffLocked(newSections []consoleSection) {
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

func (c *DeployConsole) writeSections(sections []consoleSection) {
	for _, section := range sections {
		for _, line := range section.lines {
			fmt.Fprintf(c.out, "%s\x1b[K\n", line)
		}
	}
	if len(sections) == 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
	}
}

func (c *DeployConsole) renderResourceLines() []string {
	rows := c.resources
	if len(rows) == 0 {
		return []string{"Waiting for release resources..."}
	}
	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	narrow := !c.opts.Wide && width < 100
	lines := make([]string, 0, len(rows)*2+2)
	if narrow {
		for _, row := range rows {
			lines = append(lines, fmt.Sprintf("%s %s/%s", row.Kind, row.Namespace, row.Name))
			status := colorizeStatus(row.Status)
			lines = append(lines, fmt.Sprintf("  • %s (%s)", status, row.Message))
		}
		return lines
	}
	lines = append(lines, fmt.Sprintf("%-40s %-8s %-12s %s", "Resource", "Action", "Status", "Message"))
	lines = append(lines, strings.Repeat("-", 100))
	for _, row := range rows {
		resource := fmt.Sprintf("%s %s/%s", row.Kind, row.Namespace, row.Name)
		lines = append(lines, fmt.Sprintf("%-40s %-8s %-12s %s", resource, row.Action, colorizeStatus(row.Status), row.Message))
	}
	return lines
}

func (c *DeployConsole) renderMetadataLinesLocked() []string {
	lines := []string{formatMetadataSummary(c.metadata)}
	detailLines := formatMetadataDetails(c.metadata)
	if len(detailLines) == 0 {
		return lines
	}
	if c.shouldRenderDetails() {
		for _, line := range detailLines {
			lines = append(lines, "  "+line)
		}
		return lines
	}
	lines = append(lines, fmt.Sprintf("  Details ▸ %s (add --console-details to expand)", summarizeDetailCounts(c.metadata)))
	return lines
}

func (c *DeployConsole) shouldRenderDetails() bool {
	const detailWidthThreshold = 100
	return c.opts.Wide || c.details || c.opts.Width <= 0 || c.opts.Width >= detailWidthThreshold
}

func formatMetadataSummary(meta DeployMetadata) string {
	parts := []string{}
	if meta.Release != "" {
		parts = append(parts, fmt.Sprintf("Release %s", meta.Release))
	}
	if meta.Namespace != "" {
		parts = append(parts, fmt.Sprintf("ns/%s", meta.Namespace))
	}
	if meta.Chart != "" {
		chart := meta.Chart
		if meta.ChartVersion != "" {
			chart = fmt.Sprintf("%s@%s", chart, meta.ChartVersion)
		}
		parts = append(parts, fmt.Sprintf("Chart %s", chart))
	}
	if len(parts) == 0 {
		return "Deploying release"
	}
	return strings.Join(parts, " | ")
}

func formatMetadataDetails(meta DeployMetadata) []string {
	lines := []string{}
	if vals := sanitizeList(meta.ValuesFiles); len(vals) > 0 {
		lines = append(lines, fmt.Sprintf("Values: %s", strings.Join(vals, ", ")))
	}
	if sets := sanitizeList(meta.SetValues); len(sets) > 0 {
		lines = append(lines, fmt.Sprintf("Set: %s", strings.Join(sets, ", ")))
	}
	if setStrings := sanitizeList(meta.SetStringValues); len(setStrings) > 0 {
		lines = append(lines, fmt.Sprintf("SetString: %s", strings.Join(setStrings, ", ")))
	}
	return lines
}

func summarizeDetailCounts(meta DeployMetadata) string {
	counts := []string{}
	if vals := sanitizeList(meta.ValuesFiles); len(vals) > 0 {
		counts = append(counts, fmt.Sprintf("values:%d", len(vals)))
	}
	if sets := sanitizeList(meta.SetValues); len(sets) > 0 {
		counts = append(counts, fmt.Sprintf("set:%d", len(sets)))
	}
	if str := sanitizeList(meta.SetStringValues); len(str) > 0 {
		counts = append(counts, fmt.Sprintf("set-string:%d", len(str)))
	}
	if len(counts) == 0 {
		return "nothing to show"
	}
	return strings.Join(counts, ", ")
}

func sanitizeList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := make(map[string]struct{})
	for _, value := range values {
		trimmed := strings.TrimSpace(value)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	out := make([]string, 0, len(set))
	for val := range set {
		out = append(out, val)
	}
	sort.Strings(out)
	return out
}

func formatPhases(phases map[string]phaseBadge) string {
	if len(phases) == 0 {
		return "Phases: pending"
	}
	chips := make([]string, 0, len(phaseOrder))
	for _, name := range phaseOrder {
		badge, ok := phases[name]
		if !ok {
			badge = phaseBadge{Name: name, State: "pending"}
		}
		if strings.TrimSpace(badge.Name) == "" {
			badge.Name = name
		}
		chips = append(chips, renderPhaseChip(badge))
	}
	return fmt.Sprintf("Phases: %s", strings.Join(chips, "  "))
}

func renderPhaseChip(badge phaseBadge) string {
	state := strings.ToLower(strings.TrimSpace(badge.State))
	label := phaseTitleCaser.String(strings.TrimSpace(badge.Name))
	if label == "" {
		label = "Phase"
	}
	var glyph string
	painter := color.New(color.FgHiBlack)
	switch state {
	case "succeeded", "success", "skipped":
		glyph = "●"
		painter = color.New(color.FgGreen)
	case "running":
		glyph = "⟳"
		painter = color.New(color.FgYellow)
	case "failed":
		glyph = "✖"
		painter = color.New(color.FgRed)
	default:
		glyph = "○"
	}
	text := painter.Sprintf("%s %s", glyph, label)
	if badge.Message != "" && (state == "running" || state == "failed") {
		text = fmt.Sprintf("%s - %s", text, strings.TrimSpace(badge.Message))
	}
	return text
}

func renderWarning(w consoleWarning) string {
	prefix := color.New(color.FgHiYellow).Sprint("Attention")
	if w.Severity == "error" {
		prefix = color.New(color.FgHiRed).Sprint("Attention")
	}
	age := humanizeAge(time.Since(w.IssuedAt))
	return fmt.Sprintf("%s (%s): %s", prefix, age, w.Message)
}

func colorizeStatus(status string) string {
	switch strings.ToLower(status) {
	case "ready", "succeeded":
		return color.New(color.FgGreen).Sprint(status)
	case "failed", "error":
		return color.New(color.FgRed).Sprint(status)
	case "progressing", "running":
		return color.New(color.FgYellow).Sprint(status)
	default:
		return color.New(color.FgHiBlack).Sprint(status)
	}
}

func normalizeSeverity(level string) string {
	lvl := strings.ToLower(strings.TrimSpace(level))
	switch lvl {
	case "warning":
		return "warn"
	case "err":
		return "error"
	case "warn", "error", "info":
		return lvl
	default:
		return "info"
	}
}

func normalizePhaseState(state string) string {
	s := strings.ToLower(strings.TrimSpace(state))
	switch s {
	case "running", "pending", "succeeded", "failed", "skipped":
		return s
	default:
		return "pending"
	}
}

func humanizeAge(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	switch {
	case d < time.Second:
		return "-0s"
	case d < time.Minute:
		return fmt.Sprintf("-%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("-%dm", int(d.Minutes()))
	default:
		return fmt.Sprintf("-%dh", int(d.Hours()))
	}
}

func cloneStatusRows(rows []deploy.ResourceStatus) []deploy.ResourceStatus {
	if len(rows) == 0 {
		return nil
	}
	cp := make([]deploy.ResourceStatus, len(rows))
	copy(cp, rows)
	return cp
}

func cloneSections(sections []consoleSection) []consoleSection {
	if len(sections) == 0 {
		return nil
	}
	out := make([]consoleSection, len(sections))
	for i, sec := range sections {
		lines := make([]string, len(sec.lines))
		copy(lines, sec.lines)
		out[i] = consoleSection{name: sec.name, lines: lines}
	}
	return out
}

func countLines(sections []consoleSection) int {
	total := 0
	for _, sec := range sections {
		total += len(sec.lines)
	}
	return total
}

func diffIndex(oldSections, newSections []consoleSection) int {
	max := len(oldSections)
	if len(newSections) < max {
		max = len(newSections)
	}
	for i := 0; i < max; i++ {
		if !equalLines(oldSections[i].lines, newSections[i].lines) {
			return i
		}
	}
	if len(oldSections) != len(newSections) {
		return max
	}
	return -1
}

func equalLines(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func sumLines(sections []consoleSection) int {
	total := 0
	for _, sec := range sections {
		total += len(sec.lines)
	}
	return total
}
