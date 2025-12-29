package verify

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

type ConsoleOptions struct {
	Enabled bool
	Width   int
	Color   bool

	// Tail limits the number of recent findings shown (0 uses a default).
	Tail int

	// Now returns the current time for elapsed calculations. Defaults to time.Now.
	Now func() time.Time
}

type ConsoleMeta struct {
	Target     string
	Mode       Mode
	FailOn     Severity
	PolicyRef  string
	PolicyMode string
}

// Console renders verify events into a single in-place updating TTY view.
// It is event-driven: callers should feed Event values via Observe.
type Console struct {
	out  io.Writer
	opts ConsoleOptions

	mu        sync.Mutex
	meta      ConsoleMeta
	startedAt time.Time

	phase   string
	ruleset string
	counts  map[Severity]int
	total   int

	findings []Finding

	passed  bool
	blocked bool
	done    bool

	sections   []consoleSection
	totalLines int
}

type consoleSection struct {
	name  string
	lines []string
}

func NewConsole(out io.Writer, meta ConsoleMeta, opts ConsoleOptions) *Console {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Console{
		out:       out,
		opts:      opts,
		meta:      meta,
		startedAt: opts.Now(),
		counts:    map[Severity]int{},
	}
}

func (c *Console) Observe(ev Event) {
	if c == nil || !c.opts.Enabled || c.out == nil {
		return
	}
	c.mu.Lock()
	c.applyLocked(ev)
	c.renderLocked()
	c.mu.Unlock()
}

func (c *Console) Done() {
	if c == nil || !c.opts.Enabled || c.out == nil {
		return
	}
	c.mu.Lock()
	c.renderLocked()
	if c.totalLines > 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
		c.totalLines++
	}
	c.mu.Unlock()
}

// SnapshotLines returns the current console surface as plain lines (no cursor movement).
// It is intended for tests and debugging.
func (c *Console) SnapshotLines() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sections := c.buildSectionsLocked()
	out := make([]string, 0, countLines(sections))
	for _, section := range sections {
		out = append(out, section.lines...)
	}
	return out
}

func (c *Console) now() time.Time {
	if c == nil || c.opts.Now == nil {
		return time.Now()
	}
	return c.opts.Now()
}

func (c *Console) applyLocked(ev Event) {
	if ev.Type == EventReset {
		c.findings = nil
		c.counts = map[Severity]int{}
		c.total = 0
	}
	if strings.TrimSpace(ev.Target) != "" {
		c.meta.Target = strings.TrimSpace(ev.Target)
	}
	if strings.TrimSpace(ev.Ruleset) != "" {
		c.ruleset = strings.TrimSpace(ev.Ruleset)
	}
	if strings.TrimSpace(ev.Phase) != "" {
		c.phase = strings.TrimSpace(ev.Phase)
	}
	if strings.TrimSpace(ev.PolicyRef) != "" {
		c.meta.PolicyRef = strings.TrimSpace(ev.PolicyRef)
	}
	if strings.TrimSpace(ev.PolicyMode) != "" {
		c.meta.PolicyMode = strings.TrimSpace(ev.PolicyMode)
	}
	if ev.Finding != nil {
		f := *ev.Finding
		c.findings = append(c.findings, f)
		c.total++
		c.counts[f.Severity]++
	}
	if ev.Summary != nil {
		s := *ev.Summary
		c.total = s.Total
		c.counts = map[Severity]int{}
		for k, v := range s.BySev {
			c.counts[k] = v
		}
		c.passed = s.Passed
		c.blocked = s.Blocked
	}
	switch ev.Type {
	case EventDone:
		c.done = true
		c.passed = ev.Passed
		c.blocked = ev.Blocked
	}
}

func (c *Console) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *Console) buildSectionsLocked() []consoleSection {
	width := c.opts.Width
	lines := []string{c.renderHeaderLocked(width)}
	if detail := c.renderDetailLocked(width); detail != "" {
		lines = append(lines, ansiDim(c.opts.Color, detail))
	}
	if counts := c.renderCountsLocked(width); counts != "" {
		lines = append(lines, ansiDim(c.opts.Color, counts))
	}
	sections := []consoleSection{{name: "header", lines: lines}}
	if fl := c.renderFindingsLocked(width); len(fl) > 0 {
		sections = append(sections, consoleSection{name: "findings", lines: fl})
	}
	return sections
}

func (c *Console) renderHeaderLocked(width int) string {
	target := strings.TrimSpace(c.meta.Target)
	if target == "" {
		target = "-"
	}
	elapsed := c.now().Sub(c.startedAt).Round(100 * time.Millisecond)
	phase := strings.TrimSpace(c.phase)
	if phase == "" {
		phase = "idle"
	}
	state := "running"
	if c.done {
		if c.blocked {
			state = "blocked"
		} else if c.passed {
			state = "passed"
		} else {
			state = "done"
		}
	}
	line := trimToWidth(fmt.Sprintf("ktl verify · %s · %s · phase=%s · findings=%d · %s", target, state, phase, c.total, elapsed), width)
	if c.opts.Color {
		switch state {
		case "blocked":
			return ansiRedBold(true, line)
		case "passed":
			return ansiGreenBold(true, line)
		}
	}
	return line
}

func (c *Console) renderDetailLocked(width int) string {
	parts := []string{}
	if m := strings.TrimSpace(string(c.meta.Mode)); m != "" {
		parts = append(parts, "mode="+m)
	}
	if f := strings.TrimSpace(string(c.meta.FailOn)); f != "" {
		parts = append(parts, "fail-on="+f)
	}
	if r := strings.TrimSpace(c.ruleset); r != "" {
		parts = append(parts, "ruleset="+r)
	}
	if strings.TrimSpace(c.meta.PolicyRef) != "" {
		p := "policy=" + strings.TrimSpace(c.meta.PolicyRef)
		if strings.TrimSpace(c.meta.PolicyMode) != "" {
			p += " (" + strings.TrimSpace(c.meta.PolicyMode) + ")"
		}
		parts = append(parts, p)
	}
	if len(parts) == 0 {
		return ""
	}
	return trimToWidth(strings.Join(parts, " · "), width)
}

func (c *Console) renderCountsLocked(width int) string {
	total := c.total
	if c.done {
		total = c.total
	}
	if total == 0 && len(c.counts) == 0 {
		return ""
	}
	crit := c.counts[SeverityCritical]
	high := c.counts[SeverityHigh]
	med := c.counts[SeverityMedium]
	low := c.counts[SeverityLow]
	info := c.counts[SeverityInfo]
	return trimToWidth(fmt.Sprintf("severity: critical=%d high=%d medium=%d low=%d info=%d", crit, high, med, low, info), width)
}

func (c *Console) renderFindingsLocked(width int) []string {
	tail := c.opts.Tail
	if tail <= 0 {
		tail = 8
	}
	list := c.findings
	if len(list) > tail {
		list = list[len(list)-tail:]
	}
	if len(list) == 0 {
		return nil
	}
	lines := make([]string, 0, len(list)+1)
	lines = append(lines, ansiBold(c.opts.Color, trimToWidth("Recent findings:", width)))
	for _, f := range list {
		sub := strings.TrimSpace(f.Subject.Kind)
		if ns := strings.TrimSpace(f.Subject.Namespace); ns != "" {
			sub += "/" + ns
		}
		if name := strings.TrimSpace(f.Subject.Name); name != "" {
			if sub != "" {
				sub += "/"
			}
			sub += name
		}
		loc := strings.TrimSpace(f.Location)
		if loc == "" {
			loc = strings.TrimSpace(f.Path)
		}
		msg := strings.TrimSpace(f.Message)
		if msg == "" {
			msg = strings.TrimSpace(f.RuleID)
		}
		line := fmt.Sprintf("- [%s] %s · %s", strings.ToUpper(string(f.Severity)), strings.TrimSpace(f.RuleID), strings.TrimSpace(sub))
		if loc != "" {
			line += " · " + loc
		}
		if msg != "" {
			line += " · " + msg
		}
		line = trimToWidth(line, width)
		switch f.Severity {
		case SeverityCritical, SeverityHigh:
			line = ansiRed(c.opts.Color, line)
		case SeverityMedium:
			line = ansiYellow(c.opts.Color, line)
		case SeverityLow:
			line = ansiCyan(c.opts.Color, line)
		default:
			line = ansiDim(c.opts.Color, line)
		}
		lines = append(lines, line)
	}
	return lines
}

func (c *Console) applyDiffLocked(newSections []consoleSection) {
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

func (c *Console) writeSections(sections []consoleSection) {
	for _, section := range sections {
		for _, line := range section.lines {
			fmt.Fprintf(c.out, "%s\x1b[K\n", line)
		}
	}
	if len(sections) == 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
	}
}

func countLines(sections []consoleSection) int {
	total := 0
	for _, s := range sections {
		total += len(s.lines)
	}
	if total == 0 {
		return 1
	}
	return total
}

func sumLines(sections []consoleSection) int {
	total := 0
	for _, s := range sections {
		total += len(s.lines)
	}
	return total
}

func cloneSections(sections []consoleSection) []consoleSection {
	out := make([]consoleSection, 0, len(sections))
	for _, s := range sections {
		lines := make([]string, 0, len(s.lines))
		lines = append(lines, s.lines...)
		out = append(out, consoleSection{name: s.name, lines: lines})
	}
	return out
}

func diffIndex(prev []consoleSection, next []consoleSection) int {
	max := len(prev)
	if len(next) < max {
		max = len(next)
	}
	for i := 0; i < max; i++ {
		if prev[i].name != next[i].name {
			return i
		}
		if !sameLines(prev[i].lines, next[i].lines) {
			return i
		}
	}
	if len(prev) != len(next) {
		return max
	}
	return -1
}

func sameLines(a []string, b []string) bool {
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

func trimToWidth(s string, width int) string {
	s = strings.TrimSpace(s)
	if width <= 0 {
		return ""
	}
	if runewidth.StringWidth(s) <= width {
		return s
	}
	if width <= 1 {
		out := []rune(s)
		if len(out) == 0 {
			return ""
		}
		return string(out[:1])
	}
	limit := width - 1
	var out []rune
	w := 0
	for _, r := range s {
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		if w+rw > limit {
			break
		}
		out = append(out, r)
		w += rw
	}
	return string(out) + "…"
}

func ansi(enabled bool, code string, s string) string {
	if !enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func ansiBold(enabled bool, s string) string      { return ansi(enabled, "1", s) }
func ansiDim(enabled bool, s string) string       { return ansi(enabled, "2", s) }
func ansiRed(enabled bool, s string) string       { return ansi(enabled, "31", s) }
func ansiRedBold(enabled bool, s string) string   { return ansi(enabled, "31;1", s) }
func ansiGreenBold(enabled bool, s string) string { return ansi(enabled, "32;1", s) }
func ansiYellow(enabled bool, s string) string    { return ansi(enabled, "33", s) }
func ansiCyan(enabled bool, s string) string      { return ansi(enabled, "36", s) }
