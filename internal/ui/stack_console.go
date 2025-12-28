package ui

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/stack"
	"github.com/fatih/color"
)

type StackRunConsoleOptions struct {
	Enabled bool
	Verbose bool
	Width   int
}

type StackRunConsole struct {
	out  io.Writer
	opts StackRunConsoleOptions

	mu         sync.Mutex
	plan       *stack.Plan
	nodeOrder  []string
	nodes      map[string]*stackNodeState
	failures   []stackFailure
	logTail    []string
	startedAt  time.Time
	runID      string
	command    string
	concurrent string
	sections   []consoleSection
	totalLines int
}

type stackNodeState struct {
	id        string
	status    string
	attempt   int
	phase     string
	wait      string
	lastError *stack.RunError

	startedAt time.Time
	updatedAt time.Time
}

type stackFailure struct {
	nodeID  string
	attempt int
	err     *stack.RunError
	msg     string
}

func NewStackRunConsole(out io.Writer, plan *stack.Plan, command string, opts StackRunConsoleOptions) *StackRunConsole {
	c := &StackRunConsole{
		out:       out,
		opts:      opts,
		plan:      plan,
		command:   strings.TrimSpace(command),
		startedAt: time.Now(),
		nodes:     map[string]*stackNodeState{},
	}
	if plan != nil {
		c.nodeOrder = stackRunConsoleOrder(plan)
		for _, n := range plan.Nodes {
			if n == nil {
				continue
			}
			c.nodes[n.ID] = &stackNodeState{id: n.ID, status: "planned"}
		}
	}
	return c
}

func (c *StackRunConsole) ObserveRunEvent(ev stack.RunEvent) {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	c.applyEventLocked(ev)
	c.renderLocked()
	c.mu.Unlock()
}

func (c *StackRunConsole) Done() {
	if c == nil || !c.opts.Enabled {
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

func (c *StackRunConsole) applyEventLocked(ev stack.RunEvent) {
	ts, ok := parseRFC3339(ev.TS)
	if ok && c.startedAt.IsZero() {
		c.startedAt = ts
	}
	if ev.Type == string(stack.RunStarted) && ok {
		c.startedAt = ts
	}
	if strings.TrimSpace(ev.RunID) != "" {
		c.runID = strings.TrimSpace(ev.RunID)
	}
	switch ev.Type {
	case string(stack.RunConcurrency):
		c.concurrent = strings.TrimSpace(ev.Message)
	case string(stack.NodeQueued):
		c.setNodeLocked(ev.NodeID, "queued", ev.Attempt, "", "", nil, ts)
	case string(stack.NodeRunning):
		c.setNodeLocked(ev.NodeID, "running", ev.Attempt, c.getPhase(ev.NodeID), "", nil, ts)
	case string(stack.BudgetWait):
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, c.getPhase(ev.NodeID), strings.TrimSpace(ev.Message), nil, ts)
	case string(stack.PhaseStarted):
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, strings.TrimSpace(ev.Message), "", nil, ts)
	case string(stack.PhaseCompleted):
		// Keep the completed phase visible briefly; it will be overwritten by the next phase start.
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, strings.TrimSpace(ev.Message), "", nil, ts)
	case string(stack.HookFailed):
		c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), true)
	case string(stack.HookStarted), string(stack.HookSucceeded):
		if c.opts.Verbose {
			c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
		}
	case string(stack.RetryScheduled):
		c.setNodeLocked(ev.NodeID, "retrying", ev.Attempt, c.getPhase(ev.NodeID), strings.TrimSpace(ev.Message), ev.Error, ts)
		c.appendLogLocked(fmt.Sprintf("[%s] retry scheduled: %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
	case string(stack.NodeSucceeded):
		c.setNodeLocked(ev.NodeID, "succeeded", ev.Attempt, "", "", nil, ts)
	case string(stack.NodeBlocked):
		c.setNodeLocked(ev.NodeID, "blocked", ev.Attempt, "", strings.TrimSpace(ev.Message), nil, ts)
		c.appendLogLocked(fmt.Sprintf("[%s] blocked: %s", ev.NodeID, strings.TrimSpace(ev.Message)), true)
	case string(stack.NodeFailed):
		c.setNodeLocked(ev.NodeID, "failed", ev.Attempt, c.getPhase(ev.NodeID), "", ev.Error, ts)
		c.addFailureLocked(stackFailure{nodeID: ev.NodeID, attempt: ev.Attempt, err: ev.Error, msg: strings.TrimSpace(ev.Message)})
	case string(stack.NodeLog):
		if c.opts.Verbose {
			c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
		}
	case string(stack.RunCompleted):
		if msg := strings.TrimSpace(ev.Message); msg != "" {
			c.appendLogLocked(fmt.Sprintf("run completed: %s", msg), true)
		}
	}
}

func (c *StackRunConsole) setNodeLocked(id, status string, attempt int, phase string, wait string, runErr *stack.RunError, ts time.Time) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ns := c.nodes[id]
	if ns == nil {
		ns = &stackNodeState{id: id}
		c.nodes[id] = ns
	}
	if ns.startedAt.IsZero() && status == "running" {
		ns.startedAt = ts
	}
	ns.updatedAt = ts
	if strings.TrimSpace(status) != "" {
		ns.status = strings.TrimSpace(status)
	}
	if attempt > 0 {
		ns.attempt = attempt
	}
	if strings.TrimSpace(phase) != "" {
		ns.phase = strings.TrimSpace(phase)
	}
	if strings.TrimSpace(wait) != "" {
		ns.wait = strings.TrimSpace(wait)
	} else if status != "retrying" && status != "blocked" {
		ns.wait = ""
	}
	if runErr != nil {
		ns.lastError = runErr
	}
}

func (c *StackRunConsole) addFailureLocked(f stackFailure) {
	for _, existing := range c.failures {
		if existing.nodeID == f.nodeID && existing.attempt == f.attempt {
			return
		}
	}
	c.failures = append(c.failures, f)
}

func (c *StackRunConsole) appendLogLocked(line string, important bool) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	if !important && !c.opts.Verbose {
		return
	}
	const max = 16
	c.logTail = append(c.logTail, line)
	if len(c.logTail) > max {
		c.logTail = c.logTail[len(c.logTail)-max:]
	}
}

func (c *StackRunConsole) getStatus(id string) string {
	if ns := c.nodes[strings.TrimSpace(id)]; ns != nil && ns.status != "" {
		return ns.status
	}
	return "planned"
}

func (c *StackRunConsole) getPhase(id string) string {
	if ns := c.nodes[strings.TrimSpace(id)]; ns != nil {
		return ns.phase
	}
	return ""
}

func (c *StackRunConsole) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *StackRunConsole) buildSectionsLocked() []consoleSection {
	var sections []consoleSection
	sections = append(sections, consoleSection{name: "header", lines: c.renderHeaderLocked()})
	if len(c.failures) > 0 {
		sections = append(sections, consoleSection{name: "failures", lines: c.renderFailuresLocked()})
	}
	sections = append(sections, consoleSection{name: "nodes", lines: c.renderNodesLocked()})
	if c.opts.Verbose || len(c.failures) > 0 {
		sections = append(sections, consoleSection{name: "log", lines: c.renderLogLocked()})
	}
	sections = append(sections, consoleSection{name: "footer", lines: c.renderFooterLocked()})
	return sections
}

func (c *StackRunConsole) applyDiffLocked(newSections []consoleSection) {
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

func (c *StackRunConsole) writeSections(sections []consoleSection) {
	for _, section := range sections {
		for _, line := range section.lines {
			fmt.Fprintf(c.out, "%s\x1b[K\n", line)
		}
	}
	if len(sections) == 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
	}
}

func (c *StackRunConsole) renderHeaderLocked() []string {
	stackName := ""
	stackRoot := ""
	if c.plan != nil {
		stackName = strings.TrimSpace(c.plan.StackName)
		stackRoot = strings.TrimSpace(c.plan.StackRoot)
	}
	elapsed := time.Since(c.startedAt).Round(100 * time.Millisecond)
	runID := c.runID
	if runID == "" {
		runID = "…"
	}
	title := fmt.Sprintf("ktl stack %s • %s • runId=%s • elapsed=%s", c.command, stackName, runID, elapsed)
	if stackRoot != "" {
		title += " • root=" + stackRoot
	}
	lines := []string{title}
	if strings.TrimSpace(c.concurrent) != "" {
		lines = append(lines, strings.TrimSpace(c.concurrent))
	}
	return lines
}

func (c *StackRunConsole) renderFailuresLocked() []string {
	lines := []string{color.New(color.FgRed, color.Bold).Sprint("FAILURES (sticky)")}
	for _, f := range c.failures {
		msg := f.msg
		class := ""
		digest := ""
		if f.err != nil {
			class = strings.TrimSpace(f.err.Class)
			digest = strings.TrimSpace(f.err.Digest)
		}
		if len(digest) > 16 {
			digest = digest[:16] + "…"
		}
		if len(msg) > 140 {
			msg = msg[:140] + "…"
		}
		line := fmt.Sprintf("  %s attempt=%d class=%s digest=%s %s", f.nodeID, f.attempt, class, digest, msg)
		lines = append(lines, color.New(color.FgRed).Sprint(line))
	}
	return lines
}

func (c *StackRunConsole) renderNodesLocked() []string {
	order := c.nodeOrder
	if len(order) == 0 {
		for id := range c.nodes {
			order = append(order, id)
		}
		sort.Strings(order)
	}

	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	lines := make([]string, 0, len(order)+3)
	lines = append(lines, fmt.Sprintf("%-44s %-9s %-7s %-24s %s", "Release", "Status", "Attempt", "Phase", "Note"))
	lines = append(lines, strings.Repeat("-", minInt(width, 110)))
	now := time.Now()
	for _, id := range order {
		ns := c.nodes[id]
		if ns == nil {
			ns = &stackNodeState{id: id, status: "planned"}
		}
		status := strings.ToUpper(ns.status)
		status = colorizeStackStatus(status)
		attempt := ns.attempt
		if attempt == 0 {
			attempt = 0
		}
		phase := strings.TrimSpace(ns.phase)
		if phase == "" {
			phase = "-"
		}
		note := strings.TrimSpace(ns.wait)
		if note == "" && ns.lastError != nil {
			note = strings.TrimSpace(ns.lastError.Class)
		}
		elapsed := ""
		if !ns.startedAt.IsZero() && (ns.status == "running" || ns.status == "retrying") {
			elapsed = now.Sub(ns.startedAt).Round(100 * time.Millisecond).String()
		}
		if elapsed != "" {
			phase = fmt.Sprintf("%s (%s)", phase, elapsed)
		}
		lines = append(lines, fmt.Sprintf("%-44s %-9s %-7d %-24s %s", trimTo(id, 44), status, attempt, trimTo(phase, 24), trimTo(note, maxInt(width-44-9-7-24-4, 0))))
	}
	return lines
}

func (c *StackRunConsole) renderLogLocked() []string {
	if len(c.logTail) == 0 {
		return []string{"LOG (tail) • (empty)"}
	}
	lines := []string{"LOG (tail)"}
	for _, line := range c.logTail {
		lines = append(lines, "  "+line)
	}
	return lines
}

func (c *StackRunConsole) renderFooterLocked() []string {
	if c.plan == nil || strings.TrimSpace(c.plan.StackRoot) == "" || strings.TrimSpace(c.runID) == "" {
		return nil
	}
	root := strings.TrimSpace(c.plan.StackRoot)
	runID := strings.TrimSpace(c.runID)
	return []string{
		fmt.Sprintf("AUDIT  ktl stack --root %s audit --run-id %s", root, runID),
		fmt.Sprintf("FOLLOW ktl stack --root %s status --run-id %s --follow", root, runID),
	}
}

func colorizeStackStatus(status string) string {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PLANNED":
		return color.New(color.FgHiBlack).Sprint(status)
	case "QUEUED":
		return color.New(color.FgCyan).Sprint(status)
	case "RUNNING":
		return color.New(color.FgBlue, color.Bold).Sprint(status)
	case "RETRYING":
		return color.New(color.FgYellow, color.Bold).Sprint(status)
	case "SUCCEEDED":
		return color.New(color.FgGreen, color.Bold).Sprint(status)
	case "FAILED":
		return color.New(color.FgRed, color.Bold).Sprint(status)
	case "BLOCKED":
		return color.New(color.FgYellow).Sprint(status)
	default:
		return status
	}
}

func stackRunConsoleOrder(p *stack.Plan) []string {
	if p == nil || len(p.Nodes) == 0 {
		return nil
	}
	critical := stackCriticalPathIDs(p)
	criticalSet := map[string]struct{}{}
	for _, id := range critical {
		criticalSet[id] = struct{}{}
	}

	var rest []*stack.ResolvedRelease
	for _, n := range p.Nodes {
		if n == nil {
			continue
		}
		if _, ok := criticalSet[n.ID]; ok {
			continue
		}
		rest = append(rest, n)
	}
	sort.Slice(rest, func(i, j int) bool {
		if rest[i].ExecutionGroup != rest[j].ExecutionGroup {
			return rest[i].ExecutionGroup < rest[j].ExecutionGroup
		}
		if rest[i].Parallelism != rest[j].Parallelism {
			return rest[i].Parallelism < rest[j].Parallelism
		}
		return rest[i].ID < rest[j].ID
	})

	var out []string
	out = append(out, critical...)
	for _, n := range rest {
		out = append(out, n.ID)
	}
	return out
}

func stackCriticalPathIDs(p *stack.Plan) []string {
	// Map cluster+name -> id for needs resolution.
	byKey := map[string]string{}
	for _, n := range p.Nodes {
		if n == nil {
			continue
		}
		byKey[strings.TrimSpace(n.Cluster.Name)+"\n"+strings.TrimSpace(n.Name)] = n.ID
	}
	order := p.Order
	if len(order) == 0 {
		for _, n := range p.Nodes {
			if n == nil {
				continue
			}
			order = append(order, n.ID)
		}
		sort.Strings(order)
	}
	dist := map[string]int{}
	prev := map[string]string{}
	for _, id := range order {
		n := (*stack.ResolvedRelease)(nil)
		if p.ByID != nil {
			n = p.ByID[id]
		}
		if n == nil {
			for _, cand := range p.Nodes {
				if cand != nil && cand.ID == id {
					n = cand
					break
				}
			}
		}
		if n == nil {
			continue
		}
		best := 0
		bestPrev := ""
		for _, depName := range n.Needs {
			depID := byKey[strings.TrimSpace(n.Cluster.Name)+"\n"+strings.TrimSpace(depName)]
			if depID == "" {
				continue
			}
			if d := dist[depID]; d > best {
				best = d
				bestPrev = depID
			}
		}
		dist[id] = best + 1
		if bestPrev != "" {
			prev[id] = bestPrev
		}
	}
	end := ""
	maxD := 0
	for id, d := range dist {
		if d > maxD {
			maxD = d
			end = id
		}
	}
	if end == "" {
		return nil
	}
	var path []string
	cur := end
	for cur != "" {
		path = append(path, cur)
		cur = prev[cur]
	}
	// Reverse to start->end.
	for i, j := 0, len(path)-1; i < j; i, j = i+1, j-1 {
		path[i], path[j] = path[j], path[i]
	}
	return path
}

func parseRFC3339(raw string) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, false
	}
	ts, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, false
	}
	return ts, true
}

func trimTo(s string, n int) string {
	s = strings.TrimSpace(s)
	if n <= 0 {
		return ""
	}
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
