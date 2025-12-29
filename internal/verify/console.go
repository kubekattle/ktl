package verify

import (
	"fmt"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/ui"
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
	phases  map[string]string
	ruleset string
	counts  map[Severity]int
	total   int

	findings []Finding

	passed  bool
	blocked bool
	done    bool

	byRule    map[string]int
	bySubject map[string]int

	lastRenderAt time.Time

	sections   []consoleSection
	totalLines int
}

type consoleSection struct {
	name  string
	lines []string
}

var verifyPhaseOrder = []string{"collect", "render", "decode", "evaluate", "policy", "baseline", "exposure", "write"}

func NewConsole(out io.Writer, meta ConsoleMeta, opts ConsoleOptions) *Console {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	phases := make(map[string]string, len(verifyPhaseOrder))
	for _, name := range verifyPhaseOrder {
		phases[name] = "pending"
	}
	return &Console{
		out:       out,
		opts:      opts,
		meta:      meta,
		startedAt: opts.Now(),
		counts:    map[Severity]int{},
		phases:    phases,
		byRule:    map[string]int{},
		bySubject: map[string]int{},
	}
}

func (c *Console) Observe(ev Event) {
	if c == nil || !c.opts.Enabled || c.out == nil {
		return
	}
	c.mu.Lock()
	prevPhase := c.phase
	prevTotal := c.total
	prevBlocked := c.blocked
	prevPassed := c.passed
	prevDone := c.done

	c.applyLocked(ev)

	meaningful := false
	switch ev.Type {
	case EventFinding, EventSummary, EventDone, EventReset:
		meaningful = true
	default:
		if c.phase != prevPhase || c.total != prevTotal || c.blocked != prevBlocked || c.passed != prevPassed || c.done != prevDone {
			meaningful = true
		}
	}
	c.renderMaybeLocked(ev, meaningful)
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
		c.byRule = map[string]int{}
		c.bySubject = map[string]int{}
	}
	if strings.TrimSpace(ev.Target) != "" {
		c.meta.Target = strings.TrimSpace(ev.Target)
	}
	if strings.TrimSpace(ev.Ruleset) != "" {
		c.ruleset = strings.TrimSpace(ev.Ruleset)
	}
	if strings.TrimSpace(ev.Phase) != "" {
		c.phase = strings.ToLower(strings.TrimSpace(ev.Phase))
		c.setPhaseLocked(c.phase)
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
		ruleID := strings.TrimSpace(f.RuleID)
		if ruleID != "" {
			c.byRule[ruleID]++
		}
		subKey := strings.TrimSpace(f.Subject.Kind) + "/" + strings.TrimSpace(f.Subject.Namespace) + "/" + strings.TrimSpace(f.Subject.Name)
		subKey = strings.Trim(subKey, "/")
		if subKey != "" {
			c.bySubject[subKey]++
		}
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
		if strings.TrimSpace(c.phase) != "" {
			c.setPhaseTerminalLocked(c.phase)
		}
	}
}

func (c *Console) setPhaseLocked(phase string) {
	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase == "" || c.phases == nil {
		return
	}
	for k, v := range c.phases {
		if v == "running" && k != phase {
			c.phases[k] = "done"
		}
	}
	for i, name := range verifyPhaseOrder {
		if name == phase {
			for j := 0; j < i; j++ {
				if c.phases[verifyPhaseOrder[j]] == "pending" {
					c.phases[verifyPhaseOrder[j]] = "done"
				}
			}
			c.phases[name] = "running"
			return
		}
	}
}

func (c *Console) setPhaseTerminalLocked(phase string) {
	phase = strings.ToLower(strings.TrimSpace(phase))
	if phase == "" || c.phases == nil {
		return
	}
	if _, ok := c.phases[phase]; ok {
		c.phases[phase] = "done"
	}
}

func (c *Console) renderMaybeLocked(ev Event, meaningful bool) {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	now := time.Now()
	force := false
	switch ev.Type {
	case EventDone, EventSummary, EventReset:
		force = true
	}
	if !force && !meaningful {
		return
	}
	if !force && !c.lastRenderAt.IsZero() && now.Sub(c.lastRenderAt) < 90*time.Millisecond {
		return
	}
	c.renderLocked()
	c.lastRenderAt = now
}

func (c *Console) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	if width, ok := ui.TerminalWidth(c.out); ok && width > 0 {
		// Avoid printing into the last terminal column: many terminals auto-wrap
		// when the last column is filled, which breaks cursor-based repaints and
		// results in duplicated frames.
		if width > 1 {
			width--
		}
		c.opts.Width = width
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *Console) buildSectionsLocked() []consoleSection {
	width := c.opts.Width
	lines := []string{
		c.renderHeaderLocked(width),
		c.renderStatusRailLocked(width),
		c.renderPhaseRailLocked(width),
	}
	if detail := c.renderDetailLocked(width); detail != "" {
		lines = append(lines, ansiDim(c.opts.Color, detail))
	}
	if tops := c.renderTopLocked(width); len(tops) > 0 {
		lines = append(lines, "")
		lines = append(lines, tops...)
	}
	lines = append(lines, "")
	lines = append(lines, ansiDim(c.opts.Color, trimToWidth(strings.Repeat("─", dividerWidth(width)), dividerWidth(width))))
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
	state := "RUN"
	if c.done {
		if c.blocked {
			state = "BLOCK"
		} else if c.passed {
			state = "PASS"
		} else {
			state = "DONE"
		}
	}
	tag := "[" + state + "]"
	switch state {
	case "BLOCK":
		tag = ansiRedBold(c.opts.Color, tag)
	case "PASS":
		tag = ansiGreenBold(c.opts.Color, tag)
	case "RUN":
		tag = ansiCyan(c.opts.Color, tag)
	default:
		tag = ansiDim(c.opts.Color, tag)
	}
	left := ansiBold(c.opts.Color, "KTL VERIFY") + " " + tag + " " + target
	right := elapsed.String()
	return trimToWidthANSI(joinLeftRightANSI(left, right, width), width)
}

func (c *Console) renderPhaseRailLocked(width int) string {
	if c.phases == nil {
		return ""
	}
	parts := make([]string, 0, len(verifyPhaseOrder))
	for _, name := range verifyPhaseOrder {
		state := strings.ToLower(strings.TrimSpace(c.phases[name]))
		token := strings.ToUpper(name)
		switch state {
		case "running":
			token = ansiCyan(c.opts.Color, "▶ "+token)
		case "done":
			token = ansiGreenBold(c.opts.Color, "✓ "+token)
		default:
			token = ansiDim(c.opts.Color, "· "+token)
		}
		parts = append(parts, token)
	}
	return trimToWidthANSI(strings.Join(parts, "  "), width)
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

func (c *Console) renderStatusRailLocked(width int) string {
	if c.total == 0 && len(c.counts) == 0 {
		return ""
	}
	crit := c.counts[SeverityCritical]
	high := c.counts[SeverityHigh]
	med := c.counts[SeverityMedium]
	low := c.counts[SeverityLow]
	info := c.counts[SeverityInfo]

	findings := ansiBold(c.opts.Color, fmt.Sprintf("Findings %d", c.total))
	chips := []string{
		ansiRed(c.opts.Color, fmt.Sprintf("CRIT %d", crit)),
		ansiRed(c.opts.Color, fmt.Sprintf("HIGH %d", high)),
		ansiYellow(c.opts.Color, fmt.Sprintf("MED %d", med)),
		ansiCyan(c.opts.Color, fmt.Sprintf("LOW %d", low)),
		ansiDim(c.opts.Color, fmt.Sprintf("INFO %d", info)),
	}
	line := findings + "  |  " + strings.Join(chips, "  |  ")
	return trimToWidthANSI(line, width)
}

func (c *Console) renderTopLocked(width int) []string {
	if len(c.byRule) == 0 && len(c.bySubject) == 0 {
		return nil
	}
	limit := 4
	lines := []string{ansiDimBold(c.opts.Color, trimToWidth("TOP", width))}

	if len(c.byRule) > 0 {
		lines = append(lines, ansiDim(c.opts.Color, trimToWidth("top rules:", width)))
		for _, item := range topN(c.byRule, limit) {
			left := "• " + item.Key
			right := fmt.Sprintf("%d", item.Count)
			lines = append(lines, ansiDim(c.opts.Color, trimToWidth(joinLeftRight(left, right, width), width)))
		}
	}
	if len(c.bySubject) > 0 {
		lines = append(lines, ansiDim(c.opts.Color, trimToWidth("top subjects:", width)))
		for _, item := range topN(c.bySubject, limit) {
			left := "• " + item.Key
			right := fmt.Sprintf("%d", item.Count)
			lines = append(lines, ansiDim(c.opts.Color, trimToWidth(joinLeftRight(left, right, width), width)))
		}
	}
	return lines
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
	lines := make([]string, 0, len(list)+4)
	lines = append(lines, ansiDimBold(c.opts.Color, trimToWidth("RECENT FINDINGS", width)))

	sevW := 6
	// Bias width towards MESSAGE for 100/120-col terminals.
	ruleW := clamp(18, width/4, 36)
	subW := clamp(14, width/5, 28)
	gaps := 3
	msgW := width - (sevW + ruleW + subW + gaps)
	if msgW < 16 {
		need := 16 - msgW
		shrink := min(need, max(0, subW-10))
		subW -= shrink
		need -= shrink
		shrink = min(need, max(0, ruleW-16))
		ruleW -= shrink
		msgW = width - (sevW + ruleW + subW + gaps)
	}

	header := strings.Join([]string{
		ansiDim(c.opts.Color, formatCell("SEV", sevW, alignLeft)),
		ansiDim(c.opts.Color, formatCell("RULE", ruleW, alignLeft)),
		ansiDim(c.opts.Color, formatCell("SUBJECT", subW, alignLeft)),
		ansiDim(c.opts.Color, formatCell("MESSAGE", max(0, msgW), alignLeft)),
	}, " ")
	lines = append(lines, header)

	for _, f := range list {
		sev := strings.ToUpper(string(f.Severity))

		rule := strings.TrimSpace(f.RuleID)
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
		msg := strings.TrimSpace(f.Message)
		if msg == "" {
			msg = strings.TrimSpace(f.RuleID)
		}
		loc := strings.TrimSpace(f.Location)
		if loc == "" {
			loc = strings.TrimSpace(f.Path)
		}
		// Default: keep MESSAGE clean (no long metadata path). Users can still
		// see the full location in JSON/SARIF outputs.

		sevCell := formatCell(sev, sevW, alignLeft)
		switch f.Severity {
		case SeverityCritical, SeverityHigh:
			sevCell = ansiRed(c.opts.Color, sevCell)
		case SeverityMedium:
			sevCell = ansiYellow(c.opts.Color, sevCell)
		case SeverityLow:
			sevCell = ansiCyan(c.opts.Color, sevCell)
		default:
			sevCell = ansiDim(c.opts.Color, sevCell)
		}
		ruleCell := formatCell(rule, ruleW, alignLeft)
		subCell := formatCell(sub, subW, alignLeft)
		msgCell := formatCell(msg, max(0, msgW), alignLeft)
		row := strings.Join([]string{sevCell, ruleCell, subCell, msgCell}, " ")
		lines = append(lines, row)
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
func ansiDimBold(enabled bool, s string) string   { return ansi(enabled, "2;1", s) }
func ansiRed(enabled bool, s string) string       { return ansi(enabled, "31", s) }
func ansiRedBold(enabled bool, s string) string   { return ansi(enabled, "31;1", s) }
func ansiGreenBold(enabled bool, s string) string { return ansi(enabled, "32;1", s) }
func ansiYellow(enabled bool, s string) string    { return ansi(enabled, "33", s) }
func ansiCyan(enabled bool, s string) string      { return ansi(enabled, "36", s) }

type cellAlign int

const (
	alignLeft cellAlign = iota
	alignRight
)

func formatCell(text string, width int, align cellAlign) string {
	text = strings.TrimSpace(text)
	if width <= 0 {
		return ""
	}
	out := trimToWidth(text, width)
	pad := width - runewidth.StringWidth(out)
	if pad <= 0 {
		return out
	}
	switch align {
	case alignRight:
		return strings.Repeat(" ", pad) + out
	default:
		return out + strings.Repeat(" ", pad)
	}
}

func joinLeftRight(left string, right string, width int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if width <= 0 {
		return ""
	}
	gap := "  "
	if runewidth.StringWidth(left)+runewidth.StringWidth(right)+runewidth.StringWidth(gap) >= width {
		return left + gap + right
	}
	spaces := width - runewidth.StringWidth(left) - runewidth.StringWidth(right)
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + right
}

func joinLeftRightANSI(left string, right string, width int) string {
	left = strings.TrimSpace(left)
	right = strings.TrimSpace(right)
	if width <= 0 {
		return ""
	}
	gap := "  "
	if visibleWidthANSI(left)+visibleWidthANSI(right)+visibleWidthANSI(gap) >= width {
		return left + gap + right
	}
	spaces := width - visibleWidthANSI(left) - visibleWidthANSI(right)
	if spaces < 1 {
		spaces = 1
	}
	return left + strings.Repeat(" ", spaces) + right
}

func trimToWidthANSI(s string, width int) string {
	s = strings.TrimSpace(s)
	if width <= 0 {
		return ""
	}
	if visibleWidthANSI(s) <= width {
		return s
	}
	if width <= 1 {
		return "…"
	}
	limit := width - 1
	var b strings.Builder
	b.Grow(len(s))

	seenANSI := false
	visible := 0
	for i := 0; i < len(s); {
		// ESC [ ... m
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			seenANSI = true
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++
			}
			b.WriteString(s[i:j])
			i = j
			continue
		}

		r, size := rune(s[i]), 1
		if r >= 0x80 {
			r, size = decodeRune(s[i:])
		}
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		if visible+rw > limit {
			break
		}
		b.WriteRune(r)
		visible += rw
		i += size
	}
	b.WriteRune('…')
	if seenANSI {
		// Ensure we don't leave the terminal in a styled state if we truncated mid-line.
		b.WriteString("\x1b[0m")
	}
	return b.String()
}

func visibleWidthANSI(s string) int {
	w := 0
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				j++
			}
			i = j
			continue
		}
		r, size := rune(s[i]), 1
		if r >= 0x80 {
			r, size = decodeRune(s[i:])
		}
		rw := runewidth.RuneWidth(r)
		if rw == 0 {
			rw = 1
		}
		w += rw
		i += size
	}
	return w
}

func decodeRune(s string) (rune, int) {
	// Minimal UTF-8 decoder (avoids importing unicode/utf8 just for this file).
	// Falls back to raw byte on invalid sequences.
	b0 := s[0]
	switch {
	case b0 < 0x80:
		return rune(b0), 1
	case b0 < 0xE0 && len(s) >= 2:
		return rune(b0&0x1F)<<6 | rune(s[1]&0x3F), 2
	case b0 < 0xF0 && len(s) >= 3:
		return rune(b0&0x0F)<<12 | rune(s[1]&0x3F)<<6 | rune(s[2]&0x3F), 3
	case b0 < 0xF8 && len(s) >= 4:
		return rune(b0&0x07)<<18 | rune(s[1]&0x3F)<<12 | rune(s[2]&0x3F)<<6 | rune(s[3]&0x3F), 4
	default:
		return rune(b0), 1
	}
}

type topItem struct {
	Key   string
	Count int
}

func topN(m map[string]int, n int) []topItem {
	if len(m) == 0 || n <= 0 {
		return nil
	}
	out := make([]topItem, 0, len(m))
	for k, v := range m {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		out = append(out, topItem{Key: k, Count: v})
	}
	// simple selection sort for small N; stable-ish and no extra deps.
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j].Count > out[i].Count || (out[j].Count == out[i].Count && out[j].Key < out[i].Key) {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	if len(out) > n {
		out = out[:n]
	}
	return out
}

func clamp(minV, v, maxV int) int {
	if v < minV {
		return minV
	}
	if v > maxV {
		return maxV
	}
	return v
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func dividerWidth(width int) int {
	if width <= 0 {
		return 0
	}
	if width > 96 {
		return 96
	}
	return width
}
