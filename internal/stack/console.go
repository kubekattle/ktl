package stack

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/fatih/color"
)

type RunConsoleOptions struct {
	Enabled bool
	Verbose bool
	Width   int
}

// RunConsole renders stack run events into a single in-place updating TTY view.
// It is event-driven: callers should feed RunEvent values via ObserveRunEvent.
type RunConsole struct {
	out  io.Writer
	opts RunConsoleOptions

	mu         sync.Mutex
	plan       *Plan
	nodeOrder  []string
	nodes      map[string]*runConsoleNodeState
	metaByID   map[string]runConsoleNodeMeta
	failures   []runConsoleFailure
	logTail    []string
	startedAt  time.Time
	runID      string
	command    string
	concurrent string
	sections   []runConsoleSection
	totalLines int
}

type runConsoleNodeState struct {
	id        string
	status    string
	attempt   int
	phase     string
	wait      string
	lastError *RunError

	startedAt time.Time
	updatedAt time.Time
}

type runConsoleNodeMeta struct {
	cluster          string
	namespace        string
	name             string
	executionGroup   int
	parallelismGroup string
	primaryKind      string
	critical         bool
}

type runConsoleFailure struct {
	nodeID  string
	attempt int
	err     *RunError
	msg     string
}

type runConsoleSection struct {
	name  string
	lines []string
}

func NewRunConsole(out io.Writer, plan *Plan, command string, opts RunConsoleOptions) *RunConsole {
	c := &RunConsole{
		out:       out,
		opts:      opts,
		plan:      plan,
		command:   strings.TrimSpace(command),
		startedAt: time.Now(),
		nodes:     map[string]*runConsoleNodeState{},
		metaByID:  map[string]runConsoleNodeMeta{},
	}
	if plan != nil {
		c.nodeOrder = runConsoleOrder(plan)
		for _, n := range plan.Nodes {
			if n == nil {
				continue
			}
			c.nodes[n.ID] = &runConsoleNodeState{id: n.ID, status: "planned"}
		}
	}
	return c
}

func (c *RunConsole) ObserveRunEvent(ev RunEvent) {
	if c == nil || !c.opts.Enabled {
		return
	}
	c.mu.Lock()
	c.applyEventLocked(ev)
	c.renderLocked()
	c.mu.Unlock()
}

func (c *RunConsole) Done() {
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

func (c *RunConsole) applyEventLocked(ev RunEvent) {
	ts, ok := parseRFC3339(ev.TS)
	if ok && c.startedAt.IsZero() {
		c.startedAt = ts
	}
	if ev.Type == string(RunStarted) && ok {
		c.startedAt = ts
	}
	if strings.TrimSpace(ev.RunID) != "" {
		c.runID = strings.TrimSpace(ev.RunID)
	}
	switch ev.Type {
	case string(NodeMeta):
		c.applyNodeMetaLocked(ev)
	case string(RunConcurrency):
		c.concurrent = strings.TrimSpace(ev.Message)
	case string(NodeQueued):
		c.setNodeLocked(ev.NodeID, "queued", ev.Attempt, "", "", nil, ts)
	case string(NodeRunning):
		c.setNodeLocked(ev.NodeID, "running", ev.Attempt, c.getPhase(ev.NodeID), "", nil, ts)
	case string(BudgetWait):
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, c.getPhase(ev.NodeID), strings.TrimSpace(ev.Message), nil, ts)
	case string(PhaseStarted):
		phase := strings.TrimSpace(ev.Message)
		if v := fieldString(ev.Fields, "phase"); v != "" {
			phase = v
		}
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, phase, "", nil, ts)
	case string(PhaseCompleted):
		phase := fieldString(ev.Fields, "phase")
		if phase == "" {
			phase = strings.TrimSpace(ev.Message)
		}
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, phase, "", nil, ts)
	case string(HookFailed):
		c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), true)
	case string(HookStarted), string(HookSucceeded):
		if c.opts.Verbose {
			c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
		}
	case string(RetryScheduled):
		c.setNodeLocked(ev.NodeID, "retrying", ev.Attempt, c.getPhase(ev.NodeID), strings.TrimSpace(ev.Message), ev.Error, ts)
		c.appendLogLocked(fmt.Sprintf("[%s] retry scheduled: %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
	case string(NodeSucceeded):
		c.setNodeLocked(ev.NodeID, "succeeded", ev.Attempt, "", "", nil, ts)
	case string(NodeBlocked):
		c.setNodeLocked(ev.NodeID, "blocked", ev.Attempt, "", strings.TrimSpace(ev.Message), nil, ts)
		c.appendLogLocked(fmt.Sprintf("[%s] blocked: %s", ev.NodeID, strings.TrimSpace(ev.Message)), true)
	case string(NodeFailed):
		c.setNodeLocked(ev.NodeID, "failed", ev.Attempt, c.getPhase(ev.NodeID), "", ev.Error, ts)
		c.addFailureLocked(runConsoleFailure{nodeID: ev.NodeID, attempt: ev.Attempt, err: ev.Error, msg: strings.TrimSpace(ev.Message)})
	case string(NodeLog):
		if c.opts.Verbose {
			c.appendLogLocked(fmt.Sprintf("[%s] %s", ev.NodeID, strings.TrimSpace(ev.Message)), false)
		}
	case string(RunCompleted):
		if msg := strings.TrimSpace(ev.Message); msg != "" {
			c.appendLogLocked(fmt.Sprintf("run completed: %s", msg), true)
		}
	}
}

func (c *RunConsole) applyNodeMetaLocked(ev RunEvent) {
	id := strings.TrimSpace(ev.NodeID)
	if id == "" {
		return
	}
	meta := runConsoleNodeMeta{}
	meta.cluster = fieldString(ev.Fields, "cluster")
	meta.namespace = fieldString(ev.Fields, "namespace")
	meta.name = fieldString(ev.Fields, "name")
	meta.parallelismGroup = fieldString(ev.Fields, "parallelismGroup")
	meta.primaryKind = fieldString(ev.Fields, "primaryKind")
	meta.executionGroup = fieldInt(ev.Fields, "executionGroup")
	meta.critical = fieldBool(ev.Fields, "critical")
	c.metaByID[id] = meta

	if _, ok := c.nodes[id]; !ok {
		c.nodes[id] = &runConsoleNodeState{id: id, status: "planned"}
	}
	// If the console was created without a plan, build a deterministic order.
	if c.plan == nil {
		c.rebuildOrderFromMetaLocked()
	}
}

func (c *RunConsole) rebuildOrderFromMetaLocked() {
	ids := make([]string, 0, len(c.nodes))
	for id := range c.nodes {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		mi := c.metaByID[ids[i]]
		mj := c.metaByID[ids[j]]
		if mi.executionGroup != mj.executionGroup {
			return mi.executionGroup < mj.executionGroup
		}
		if mi.parallelismGroup != mj.parallelismGroup {
			return mi.parallelismGroup < mj.parallelismGroup
		}
		return ids[i] < ids[j]
	})
	c.nodeOrder = ids
}

func (c *RunConsole) setNodeLocked(id, status string, attempt int, phase string, wait string, runErr *RunError, ts time.Time) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	ns := c.nodes[id]
	if ns == nil {
		ns = &runConsoleNodeState{id: id}
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

func (c *RunConsole) addFailureLocked(f runConsoleFailure) {
	for _, existing := range c.failures {
		if existing.nodeID == f.nodeID && existing.attempt == f.attempt {
			return
		}
	}
	c.failures = append(c.failures, f)
}

func (c *RunConsole) appendLogLocked(line string, important bool) {
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

func (c *RunConsole) getStatus(id string) string {
	if ns := c.nodes[strings.TrimSpace(id)]; ns != nil && ns.status != "" {
		return ns.status
	}
	return "planned"
}

func (c *RunConsole) getPhase(id string) string {
	if ns := c.nodes[strings.TrimSpace(id)]; ns != nil {
		return ns.phase
	}
	return ""
}

func (c *RunConsole) renderLocked() {
	if !c.opts.Enabled || c.out == nil {
		return
	}
	newSections := c.buildSectionsLocked()
	c.applyDiffLocked(newSections)
}

func (c *RunConsole) buildSectionsLocked() []runConsoleSection {
	var sections []runConsoleSection
	sections = append(sections, runConsoleSection{name: "header", lines: c.renderHeaderLocked()})
	if len(c.failures) > 0 {
		sections = append(sections, runConsoleSection{name: "failures", lines: c.renderFailuresLocked()})
	}
	sections = append(sections, runConsoleSection{name: "nodes", lines: c.renderNodesLocked()})
	if c.opts.Verbose || len(c.failures) > 0 {
		sections = append(sections, runConsoleSection{name: "log", lines: c.renderLogLocked()})
	}
	return sections
}

func (c *RunConsole) applyDiffLocked(newSections []runConsoleSection) {
	newTotal := runConsoleCountLines(newSections)
	if len(c.sections) == 0 {
		c.writeSections(newSections)
		c.sections = runConsoleCloneSections(newSections)
		c.totalLines = newTotal
		return
	}
	idx := runConsoleDiffIndex(c.sections, newSections)
	if idx == -1 && newTotal == c.totalLines {
		return
	}
	if idx == -1 {
		idx = len(newSections)
	}
	startLine := runConsoleSumLines(c.sections[:idx])
	linesBelow := c.totalLines - startLine
	if linesBelow > 0 {
		fmt.Fprintf(c.out, "\x1b[%dF", linesBelow)
	}
	fmt.Fprint(c.out, "\x1b[J")
	c.writeSections(newSections[idx:])
	c.sections = runConsoleCloneSections(newSections)
	c.totalLines = newTotal
}

func (c *RunConsole) writeSections(sections []runConsoleSection) {
	for _, section := range sections {
		for _, line := range section.lines {
			fmt.Fprintf(c.out, "%s\x1b[K\n", line)
		}
	}
	if len(sections) == 0 {
		fmt.Fprint(c.out, "\x1b[K\n")
	}
}

func (c *RunConsole) renderHeaderLocked() []string {
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

	if focus := c.pickFocusLocked(); focus != "" {
		meta := c.metaByID[focus]
		ns := c.nodes[focus]
		target := focus
		if meta.cluster != "" || meta.namespace != "" || meta.name != "" {
			target = fmt.Sprintf("%s/%s/%s", meta.cluster, meta.namespace, meta.name)
		}
		attempt := 0
		phase := ""
		nodeElapsed := ""
		if ns != nil {
			attempt = ns.attempt
			phase = strings.TrimSpace(ns.phase)
			if !ns.startedAt.IsZero() && (ns.status == "running" || ns.status == "retrying") {
				nodeElapsed = time.Since(ns.startedAt).Round(100 * time.Millisecond).String()
			}
		}
		if !c.opts.Verbose && isNoisyPhase(phase) && ns != nil && ns.status != "failed" {
			phase = ""
		}
		focusLine := fmt.Sprintf("focus: %s attempt=%d", target, attempt)
		if phase != "" {
			focusLine += " phase=" + phase
		}
		if nodeElapsed != "" {
			focusLine += " elapsed=" + nodeElapsed
		}
		lines = append(lines, focusLine)
	}

	return lines
}

func (c *RunConsole) pickFocusLocked() string {
	// Prefer a running/retrying node (most recently updated), otherwise a failed node.
	best := ""
	bestTS := time.Time{}
	for id, ns := range c.nodes {
		if ns == nil {
			continue
		}
		if ns.status != "running" && ns.status != "retrying" {
			continue
		}
		if best == "" || ns.updatedAt.After(bestTS) {
			best = id
			bestTS = ns.updatedAt
		}
	}
	if best != "" {
		return best
	}
	for id, ns := range c.nodes {
		if ns == nil {
			continue
		}
		if ns.status != "failed" {
			continue
		}
		if best == "" || ns.updatedAt.After(bestTS) {
			best = id
			bestTS = ns.updatedAt
		}
	}
	return best
}

func (c *RunConsole) renderFailuresLocked() []string {
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

func (c *RunConsole) renderNodesLocked() []string {
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
	lines = append(lines, strings.Repeat("-", runConsoleMinInt(width, 110)))
	now := time.Now()
	for _, id := range order {
		ns := c.nodes[id]
		if ns == nil {
			ns = &runConsoleNodeState{id: id, status: "planned"}
		}
		status := strings.ToUpper(ns.status)
		status = colorizeRunConsoleStatus(status)
		attempt := ns.attempt
		phase := strings.TrimSpace(ns.phase)
		if phase == "" {
			phase = "-"
		}
		if !c.opts.Verbose && ns.status != "failed" && isNoisyPhase(phase) {
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
		lines = append(lines, fmt.Sprintf("%-44s %-9s %-7d %-24s %s", runConsoleTrimTo(id, 44), status, attempt, runConsoleTrimTo(phase, 24), runConsoleTrimTo(note, runConsoleMaxInt(width-44-9-7-24-4, 0))))
	}
	return lines
}

func isNoisyPhase(phase string) bool {
	p := strings.ToLower(strings.TrimSpace(phase))
	switch p {
	case "render", "wait", "pre-apply", "post-apply", "pre-delete", "post-delete":
		return true
	default:
		return false
	}
}

func fieldString(fields any, key string) string {
	m, ok := fields.(map[string]any)
	if !ok || m == nil {
		return ""
	}
	v, ok := m[key]
	if !ok || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	default:
		return strings.TrimSpace(fmt.Sprint(t))
	}
}

func fieldInt(fields any, key string) int {
	m, ok := fields.(map[string]any)
	if !ok || m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok || v == nil {
		return 0
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	default:
		return 0
	}
}

func fieldBool(fields any, key string) bool {
	m, ok := fields.(map[string]any)
	if !ok || m == nil {
		return false
	}
	v, ok := m[key]
	if !ok || v == nil {
		return false
	}
	switch t := v.(type) {
	case bool:
		return t
	default:
		return false
	}
}

func (c *RunConsole) renderLogLocked() []string {
	if len(c.logTail) == 0 {
		return []string{"LOG (tail) • (empty)"}
	}
	lines := []string{"LOG (tail)"}
	for _, line := range c.logTail {
		lines = append(lines, "  "+line)
	}
	return lines
}

func colorizeRunConsoleStatus(status string) string {
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

func runConsoleOrder(p *Plan) []string {
	if p == nil || len(p.Nodes) == 0 {
		return nil
	}
	critical := runConsoleCriticalPathIDs(p)
	criticalSet := map[string]struct{}{}
	for _, id := range critical {
		criticalSet[id] = struct{}{}
	}

	var rest []*ResolvedRelease
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

func runConsoleCriticalPathIDs(p *Plan) []string {
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
		n := (*ResolvedRelease)(nil)
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

func runConsoleTrimTo(s string, n int) string {
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

func runConsoleMinInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

func runConsoleMaxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func runConsoleCountLines(sections []runConsoleSection) int {
	total := 0
	for _, section := range sections {
		total += len(section.lines)
	}
	return total
}

func runConsoleSumLines(sections []runConsoleSection) int {
	total := 0
	for _, section := range sections {
		total += len(section.lines)
	}
	return total
}

func runConsoleDiffIndex(oldSections, newSections []runConsoleSection) int {
	max := len(oldSections)
	if len(newSections) < max {
		max = len(newSections)
	}
	for i := 0; i < max; i++ {
		if oldSections[i].name != newSections[i].name {
			return i
		}
		if len(oldSections[i].lines) != len(newSections[i].lines) {
			return i
		}
		for j := range oldSections[i].lines {
			if oldSections[i].lines[j] != newSections[i].lines[j] {
				return i
			}
		}
	}
	if len(oldSections) != len(newSections) {
		return max
	}
	return -1
}

func runConsoleCloneSections(sections []runConsoleSection) []runConsoleSection {
	out := make([]runConsoleSection, 0, len(sections))
	for _, section := range sections {
		lines := append([]string(nil), section.lines...)
		out = append(out, runConsoleSection{name: section.name, lines: lines})
	}
	return out
}
