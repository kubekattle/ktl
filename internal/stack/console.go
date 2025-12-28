package stack

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/mattn/go-runewidth"
)

type RunConsoleOptions struct {
	Enabled bool
	Verbose bool
	Width   int

	// ShowNoisyPhases forces the Phase column to include phases that are normally
	// suppressed in non-verbose mode (e.g. pre-apply/post-apply).
	ShowNoisyPhases bool

	// NodeFilter controls which nodes are shown in the node table.
	// Supported values: all|running|failed.
	NodeFilter string

	// ShowHooks renders a recent-hook activity panel below the node table.
	ShowHooks bool

	// HookTail caps stored hook events in the HOOKS panel (0 uses a default).
	HookTail int

	// ShowDetails renders an expanded per-node details panel (hooks + helm tail).
	ShowDetails bool

	// DetailsTail caps lines shown per node in the details panel (0 uses a default).
	DetailsTail int

	// Now returns the current time for elapsed calculations. Defaults to time.Now.
	Now func() time.Time

	// Color toggles ANSI styling for the TTY surface.
	Color bool

	// ShowHelmLogs renders HELM_LOG events under each node.
	ShowHelmLogs bool

	// HelmLogsMode controls which nodes are included in the HELM LOGS section.
	// Supported values: off|on|all. "on" shows only non-succeeded nodes.
	HelmLogsMode string

	// HelmLogTail caps stored log lines per node (0 uses a default).
	HelmLogTail int
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
	helmLogs   map[string][]runConsoleHelmLogEntry
	hookEvents []runConsoleHookEntry
	failures   []runConsoleFailure
	startedAt  time.Time
	runID      string
	command    string
	targetConc int
	runStage   string
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

	hooksOK      int
	hooksFailed  int
	hooksSkipped int

	lastHookNote        string
	lastHookSkippedNote string
	lastHelmLine        string

	phasesSeen map[string]bool
	phasesDone map[string]bool

	lastPhaseBeforeHook string

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

type runConsoleHookEntry struct {
	ts       time.Time
	nodeID   string
	attempt  int
	phase    string
	hook     string
	when     string
	runOnce  bool
	hookType string
	summary  string
	status   string // started|succeeded|failed|skipped
}

type runConsoleSection struct {
	name  string
	lines []string
}

type runConsoleHelmLogEntry struct {
	seq     int64
	offset  int
	ts      time.Time
	attempt int
	line    string
}

const runConsoleStackNodeID = "stack"

func NewRunConsole(out io.Writer, plan *Plan, command string, opts RunConsoleOptions) *RunConsole {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	c := &RunConsole{
		out:        out,
		opts:       opts,
		plan:       plan,
		command:    strings.TrimSpace(command),
		startedAt:  opts.Now(),
		nodes:      map[string]*runConsoleNodeState{},
		metaByID:   map[string]runConsoleNodeMeta{},
		helmLogs:   map[string][]runConsoleHelmLogEntry{},
		hookEvents: []runConsoleHookEntry{},
	}
	if plan != nil {
		c.nodeOrder = runConsoleOrder(plan)
		for _, n := range plan.Nodes {
			if n == nil {
				continue
			}
			c.nodes[n.ID] = &runConsoleNodeState{id: n.ID, status: "planned"}
			c.metaByID[n.ID] = runConsoleNodeMeta{
				cluster:          strings.TrimSpace(n.Cluster.Name),
				namespace:        strings.TrimSpace(n.Namespace),
				name:             strings.TrimSpace(n.Name),
				executionGroup:   n.ExecutionGroup,
				parallelismGroup: strings.TrimSpace(n.Parallelism),
				primaryKind:      strings.TrimSpace(n.InferredPrimaryKind),
				critical:         n.Critical,
			}
		}
		if planHasStackHooks(plan) {
			c.ensureStackNodeLocked()
		}
	}
	return c
}

func planHasStackHooks(plan *Plan) bool {
	if plan == nil {
		return false
	}
	return len(plan.Hooks.PreApply) > 0 || len(plan.Hooks.PostApply) > 0 || len(plan.Hooks.PreDelete) > 0 || len(plan.Hooks.PostDelete) > 0
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

// SnapshotLines returns the current console surface as plain lines (no cursor movement).
// It is intended for tests and debugging.
func (c *RunConsole) SnapshotLines() []string {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	sections := c.buildSectionsLocked()
	out := make([]string, 0, runConsoleCountLines(sections))
	for _, section := range sections {
		out = append(out, section.lines...)
	}
	return out
}

func (c *RunConsole) now() time.Time {
	if c == nil || c.opts.Now == nil {
		return time.Now()
	}
	return c.opts.Now()
}

func (c *RunConsole) ensureStackNodeLocked() {
	if c == nil {
		return
	}
	if _, ok := c.nodes[runConsoleStackNodeID]; !ok {
		c.nodes[runConsoleStackNodeID] = &runConsoleNodeState{id: runConsoleStackNodeID, status: "planned"}
	}
	if _, ok := c.metaByID[runConsoleStackNodeID]; !ok {
		c.metaByID[runConsoleStackNodeID] = runConsoleNodeMeta{
			cluster:     "stack",
			name:        "hooks",
			primaryKind: "Hooks",
		}
	}
}

func hookNoteFromEvent(ev RunEvent) string {
	hook := strings.TrimSpace(fieldString(ev.Fields, "hook"))
	when := strings.TrimSpace(fieldString(ev.Fields, "when"))
	runOnce := fieldBool(ev.Fields, "runOnce")
	summary := strings.TrimSpace(fieldString(ev.Fields, "summary"))
	phase := strings.TrimSpace(fieldString(ev.Fields, "phase"))
	if phase == "" {
		phase = strings.TrimSpace(ev.Message)
	}

	parts := []string{}
	if phase != "" {
		parts = append(parts, displayHookPhase(strings.TrimSpace(ev.NodeID), phase))
	}
	if hook != "" {
		parts = append(parts, hook)
	} else {
		parts = append(parts, "hook")
	}
	if when != "" {
		parts = append(parts, "when="+when)
	}
	if runOnce {
		parts = append(parts, "runOnce")
	}
	if summary != "" {
		parts = append(parts, summary)
	}
	return strings.Join(parts, " · ")
}

func displayHookPhase(nodeID string, phase string) string {
	nodeID = strings.TrimSpace(nodeID)
	phase = strings.ToLower(strings.TrimSpace(phase))
	if nodeID == "" || nodeID == runConsoleStackNodeID {
		switch phase {
		case "pre-apply":
			return "stack-pre-hooks"
		case "post-apply":
			return "stack-post-hooks"
		case "pre-delete":
			return "stack-pre-hooks"
		case "post-delete":
			return "stack-post-hooks"
		default:
			return phase
		}
	}
	return phase
}

func (c *RunConsole) appendHookEventLocked(ev RunEvent, status string) {
	if c == nil {
		return
	}
	tail := c.opts.HookTail
	if tail <= 0 {
		tail = 24
	}
	ts, _ := parseRFC3339(ev.TS)
	entry := runConsoleHookEntry{
		ts:       ts,
		nodeID:   strings.TrimSpace(ev.NodeID),
		attempt:  ev.Attempt,
		phase:    strings.TrimSpace(fieldString(ev.Fields, "phase")),
		hook:     strings.TrimSpace(fieldString(ev.Fields, "hook")),
		when:     strings.TrimSpace(fieldString(ev.Fields, "when")),
		runOnce:  fieldBool(ev.Fields, "runOnce"),
		hookType: strings.TrimSpace(fieldString(ev.Fields, "type")),
		summary:  strings.TrimSpace(fieldString(ev.Fields, "summary")),
		status:   strings.ToLower(strings.TrimSpace(status)),
	}
	if entry.nodeID == "" {
		entry.nodeID = runConsoleStackNodeID
	}
	if entry.phase == "" {
		entry.phase = strings.TrimSpace(ev.Message)
	}
	c.hookEvents = append(c.hookEvents, entry)
	if len(c.hookEvents) > tail {
		c.hookEvents = c.hookEvents[len(c.hookEvents)-tail:]
	}
}

func (c *RunConsole) appendMarkerLocked(ts time.Time, text string) {
	if c == nil {
		return
	}
	tail := c.opts.HookTail
	if tail <= 0 {
		tail = 24
	}
	c.hookEvents = append(c.hookEvents, runConsoleHookEntry{
		ts:      ts,
		status:  "marker",
		summary: strings.TrimSpace(text),
	})
	if len(c.hookEvents) > tail {
		c.hookEvents = c.hookEvents[len(c.hookEvents)-tail:]
	}
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
		if to, ok := ev.Fields["to"]; ok {
			switch v := to.(type) {
			case float64:
				c.targetConc = int(v)
			case int:
				c.targetConc = v
			case string:
				if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
					c.targetConc = n
				}
			}
		}
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
		id := strings.TrimSpace(ev.NodeID)
		ns := c.nodes[id]
		if ns == nil && id != "" {
			ns = &runConsoleNodeState{id: id}
			c.nodes[id] = ns
		}
		if ns != nil {
			if ns.phasesSeen == nil {
				ns.phasesSeen = map[string]bool{}
			}
			ns.phasesSeen[strings.TrimSpace(phase)] = true
		}
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, phase, "", nil, ts)
	case string(PhaseCompleted):
		phase := fieldString(ev.Fields, "phase")
		if phase == "" {
			phase = strings.TrimSpace(ev.Message)
		}
		id := strings.TrimSpace(ev.NodeID)
		ns := c.nodes[id]
		if ns == nil && id != "" {
			ns = &runConsoleNodeState{id: id}
			c.nodes[id] = ns
		}
		if ns != nil {
			if ns.phasesSeen == nil {
				ns.phasesSeen = map[string]bool{}
			}
			if ns.phasesDone == nil {
				ns.phasesDone = map[string]bool{}
			}
			p := strings.TrimSpace(phase)
			ns.phasesSeen[p] = true
			ns.phasesDone[p] = true
		}
		c.setNodeLocked(ev.NodeID, c.getStatus(ev.NodeID), ev.Attempt, phase, "", nil, ts)
	case string(HookStarted), string(HookSucceeded):
		nodeID := strings.TrimSpace(ev.NodeID)
		if nodeID == "" {
			nodeID = runConsoleStackNodeID
			c.ensureStackNodeLocked()
		}
		phase := strings.TrimSpace(fieldString(ev.Fields, "phase"))
		note := hookNoteFromEvent(ev)
		if ev.Type == string(HookStarted) {
			c.appendHookEventLocked(ev, "started")
		} else {
			c.appendHookEventLocked(ev, "succeeded")
		}

		ns := c.nodes[nodeID]
		if ns == nil {
			ns = &runConsoleNodeState{id: nodeID}
			c.nodes[nodeID] = ns
		}
		ns.lastHookNote = note
		if nodeID != runConsoleStackNodeID && ev.Type == string(HookStarted) {
			if strings.TrimSpace(ns.phase) != "" && strings.TrimSpace(ns.phase) != "hook" {
				ns.lastPhaseBeforeHook = strings.TrimSpace(ns.phase)
			}
			phase = "hook"
		}
		if nodeID != runConsoleStackNodeID && ev.Type == string(HookSucceeded) && strings.TrimSpace(ns.lastPhaseBeforeHook) != "" {
			phase = strings.TrimSpace(ns.lastPhaseBeforeHook)
			ns.lastPhaseBeforeHook = ""
		}
		if ev.Type == string(HookStarted) {
			c.setNodeLocked(nodeID, "running", ev.Attempt, strings.TrimSpace(phase), "", nil, ts)
		} else {
			if ns := c.nodes[nodeID]; ns != nil {
				ns.hooksOK++
			}
			c.setNodeLocked(nodeID, c.getStatus(nodeID), ev.Attempt, strings.TrimSpace(phase), "", nil, ts)
		}
	case string(HookFailed):
		nodeID := strings.TrimSpace(ev.NodeID)
		if nodeID == "" {
			nodeID = runConsoleStackNodeID
			c.ensureStackNodeLocked()
		}
		phase := strings.TrimSpace(fieldString(ev.Fields, "phase"))
		note := hookNoteFromEvent(ev)
		c.appendHookEventLocked(ev, "failed")

		ns := c.nodes[nodeID]
		if ns == nil {
			ns = &runConsoleNodeState{id: nodeID}
			c.nodes[nodeID] = ns
		}
		ns.lastHookNote = note
		if nodeID != runConsoleStackNodeID {
			phase = "hook"
		}
		if ns := c.nodes[nodeID]; ns != nil {
			ns.hooksFailed++
		}
		c.setNodeLocked(nodeID, "failed", ev.Attempt, strings.TrimSpace(phase), "", ev.Error, ts)
		c.addFailureLocked(runConsoleFailure{nodeID: nodeID, attempt: ev.Attempt, err: ev.Error, msg: note})
	case string(HookSkipped):
		nodeID := strings.TrimSpace(ev.NodeID)
		if nodeID == "" {
			nodeID = runConsoleStackNodeID
			c.ensureStackNodeLocked()
		}
		phase := strings.TrimSpace(fieldString(ev.Fields, "phase"))
		note := hookNoteFromEvent(ev)
		c.appendHookEventLocked(ev, "skipped")

		ns := c.nodes[nodeID]
		if ns == nil {
			ns = &runConsoleNodeState{id: nodeID}
			c.nodes[nodeID] = ns
		}
		ns.lastHookSkippedNote = note
		if nodeID != runConsoleStackNodeID && strings.TrimSpace(ns.lastPhaseBeforeHook) != "" {
			phase = strings.TrimSpace(ns.lastPhaseBeforeHook)
			ns.lastPhaseBeforeHook = ""
		}
		if ns := c.nodes[nodeID]; ns != nil {
			ns.hooksSkipped++
		}
		c.setNodeLocked(nodeID, c.getStatus(nodeID), ev.Attempt, strings.TrimSpace(phase), "", nil, ts)
	case string(RetryScheduled):
		c.setNodeLocked(ev.NodeID, "retrying", ev.Attempt, c.getPhase(ev.NodeID), strings.TrimSpace(ev.Message), ev.Error, ts)
	case string(NodeSucceeded):
		id := strings.TrimSpace(ev.NodeID)
		if id != "" {
			ns := c.nodes[id]
			if ns == nil {
				ns = &runConsoleNodeState{id: id}
				c.nodes[id] = ns
			}
			if strings.TrimSpace(ns.phase) != "" {
				if ns.phasesSeen == nil {
					ns.phasesSeen = map[string]bool{}
				}
				if ns.phasesDone == nil {
					ns.phasesDone = map[string]bool{}
				}
				p := strings.TrimSpace(ns.phase)
				ns.phasesSeen[p] = true
				ns.phasesDone[p] = true
			}
		}
		c.setNodeLocked(ev.NodeID, "succeeded", ev.Attempt, "", "", nil, ts)
	case string(NodeBlocked):
		c.setNodeLocked(ev.NodeID, "blocked", ev.Attempt, "", strings.TrimSpace(ev.Message), nil, ts)
	case string(NodeFailed):
		c.setNodeLocked(ev.NodeID, "failed", ev.Attempt, c.getPhase(ev.NodeID), "", ev.Error, ts)
		c.addFailureLocked(runConsoleFailure{nodeID: ev.NodeID, attempt: ev.Attempt, err: ev.Error, msg: strings.TrimSpace(ev.Message)})
	case string(NodeLog):
	case string(HelmLog):
		c.appendHelmLogLocked(ev)
	case string(StackHooksStarted):
		stage := strings.TrimSpace(fieldString(ev.Fields, "stage"))
		if stage == "" {
			stage = strings.TrimSpace(ev.Message)
		}
		c.appendMarkerLocked(ts, fmt.Sprintf("stack hooks started: %s", stage))
	case string(StackHooksCompleted):
		stage := strings.TrimSpace(fieldString(ev.Fields, "stage"))
		st := strings.TrimSpace(fieldString(ev.Fields, "status"))
		if stage == "" {
			stage = strings.TrimSpace(ev.Message)
		}
		if st == "" {
			st = "completed"
		}
		c.appendMarkerLocked(ts, fmt.Sprintf("stack hooks %s: %s", st, stage))
	case string(RunFinalizing):
		c.runStage = "finalizing"
		c.appendMarkerLocked(ts, "finalizing (post hooks)")
	case string(RunFinalized):
		c.runStage = ""
	case string(RunCompleted):
		if ns := c.nodes[runConsoleStackNodeID]; ns != nil {
			if strings.ToLower(strings.TrimSpace(ns.status)) != "failed" {
				c.setNodeLocked(runConsoleStackNodeID, "succeeded", ns.attempt, ns.phase, ns.wait, nil, ts)
			}
		}
	case string(RunStarted):
		if strings.TrimSpace(c.command) == "" {
			if cmd := fieldString(ev.Fields, "command"); cmd != "" {
				c.command = cmd
			}
		}
		if c.targetConc <= 0 {
			if conc, ok := ev.Fields["concurrency"]; ok {
				switch v := conc.(type) {
				case float64:
					c.targetConc = int(v)
				case int:
					c.targetConc = v
				case string:
					if n, err := strconv.Atoi(strings.TrimSpace(v)); err == nil {
						c.targetConc = n
					}
				}
			}
		}
	}
}

func (c *RunConsole) appendHelmLogLocked(ev RunEvent) {
	if c == nil || !c.opts.ShowHelmLogs {
		return
	}
	id := strings.TrimSpace(ev.NodeID)
	if id == "" {
		return
	}
	msg := strings.TrimSpace(ev.Message)
	if msg == "" {
		return
	}
	tail := c.opts.HelmLogTail
	if tail <= 0 {
		tail = 18
	}
	ts, _ := parseRFC3339(ev.TS)
	lines := strings.Split(msg, "\n")
	for i, line := range lines {
		line = strings.TrimRight(line, "\r\t ")
		if strings.TrimSpace(line) == "" {
			continue
		}
		ns := c.nodes[id]
		if ns == nil {
			ns = &runConsoleNodeState{id: id}
			c.nodes[id] = ns
		}
		ns.lastHelmLine = strings.TrimSpace(line)
		c.helmLogs[id] = append(c.helmLogs[id], runConsoleHelmLogEntry{
			seq:     ev.Seq,
			offset:  i,
			ts:      ts,
			attempt: ev.Attempt,
			line:    line,
		})
		if len(c.helmLogs[id]) > tail {
			c.helmLogs[id] = c.helmLogs[id][len(c.helmLogs[id])-tail:]
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
	if ns.phasesSeen == nil {
		ns.phasesSeen = map[string]bool{}
	}
	if ns.phasesDone == nil {
		ns.phasesDone = map[string]bool{}
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
	if lines := c.renderStackRailLocked(); len(lines) > 0 {
		sections = append(sections, runConsoleSection{name: "stack-rail", lines: lines})
	}
	if len(c.failures) > 0 {
		sections = append(sections, runConsoleSection{name: "failures", lines: c.renderFailuresLocked()})
	}
	sections = append(sections, runConsoleSection{name: "nodes", lines: c.renderNodesLocked()})
	if c.opts.ShowHooks {
		if lines := c.renderHooksLocked(); len(lines) > 0 {
			sections = append(sections, runConsoleSection{name: "hooks", lines: lines})
		}
	}
	if c.opts.ShowDetails {
		if lines := c.renderDetailsLocked(); len(lines) > 0 {
			sections = append(sections, runConsoleSection{name: "details", lines: lines})
		}
	}
	if c.opts.ShowHelmLogs {
		if lines := c.renderHelmLogsLocked(); len(lines) > 0 {
			sections = append(sections, runConsoleSection{name: "helm-logs", lines: lines})
		}
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
	if c.plan != nil {
		stackName = strings.TrimSpace(c.plan.StackName)
	}
	if stackName == "" {
		stackName = "-"
	}
	elapsed := c.now().Sub(c.startedAt).Round(100 * time.Millisecond)
	runID := c.runID
	if runID == "" {
		runID = "…"
	}
	cmd := strings.TrimSpace(c.command)
	if cmd == "" {
		cmd = "-"
	}

	ok, fail, blocked, running := 0, 0, 0, 0
	active := 0
	for id, ns := range c.nodes {
		if strings.TrimSpace(id) == runConsoleStackNodeID {
			continue
		}
		if ns == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(ns.status)) {
		case "succeeded":
			ok++
		case "failed":
			fail++
		case "blocked":
			blocked++
		case "running", "retrying":
			running++
			active++
		}
	}

	target := c.targetConc
	if target <= 0 {
		target = active
	}

	helmNodes, helmLines := 0, 0
	if c.opts.ShowHelmLogs {
		for _, id := range c.nodeOrder {
			if len(c.helmLogs[id]) == 0 {
				continue
			}
			helmNodes++
			helmLines += len(c.helmLogs[id])
		}
		if helmNodes == 0 {
			for id, logs := range c.helmLogs {
				if len(logs) == 0 {
					continue
				}
				helmNodes++
				helmLines += len(logs)
				_ = id
			}
		}
	}

	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	totalsPart := fmt.Sprintf("totals ok=%d fail=%d blocked=%d running=%d", ok, fail, blocked, running)
	concPart := fmt.Sprintf("concurrency %d/%d", target, active)
	if c.opts.ShowHelmLogs {
		totalsPart = fmt.Sprintf("totals o=%d f=%d b=%d r=%d", ok, fail, blocked, running)
		concPart = fmt.Sprintf("conc %d/%d", target, active)
	}

	parts := []string{
		stackName,
		cmd,
		"runId=" + runID,
		totalsPart,
		concPart,
	}
	if strings.TrimSpace(c.runStage) != "" {
		parts = append(parts, "stage "+strings.TrimSpace(c.runStage))
	}
	parts = append(parts, "elapsed "+elapsed.String())
	if c.opts.ShowHelmLogs {
		tail := c.opts.HelmLogTail
		if tail <= 0 {
			tail = 18
		}
		parts = append(parts, fmt.Sprintf("hl n=%d l=%d t=%d", helmNodes, helmLines, tail))
	}
	title := strings.Join(parts, " • ")
	title = runConsoleTrimToWidth(title, width)
	return []string{runConsoleAnsiBold(c.opts.Color, title)}
}

func (c *RunConsole) renderStackRailLocked() []string {
	if c == nil || c.plan == nil || !planHasStackHooks(c.plan) {
		return nil
	}

	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	ns := c.nodes[runConsoleStackNodeID]
	if ns == nil {
		return []string{runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("STACK HOOKS", width))}
	}

	exp := expectedStackHooksForCommand(c.plan, c.command)
	done := ns.hooksOK + ns.hooksFailed + ns.hooksSkipped
	progress := "-"
	if exp > 0 {
		progress = fmt.Sprintf("%d/%d", done, exp)
	}

	status := strings.ToLower(strings.TrimSpace(ns.status))
	if status == "" {
		status = "planned"
	}
	phase := strings.TrimSpace(ns.phase)
	if phase == "" {
		phase = "-"
	}

	note := strings.TrimSpace(ns.lastHookNote)
	if note == "" {
		note = strings.TrimSpace(ns.wait)
	}
	if note == "" {
		note = fmt.Sprintf("hooks %s (ok=%d failed=%d skipped=%d)", progress, ns.hooksOK, ns.hooksFailed, ns.hooksSkipped)
	} else {
		note = note + " · hooks " + progress
	}

	statusCell := runConsoleStatusCell(strings.ToUpper(status))
	statusTag := runConsoleFormatStatusTag(c.opts.Color, statusCell)
	statusPlain := strings.TrimSpace(statusCell.text)

	prefixPlain := "stack " + statusPlain + " "
	remaining := width - runewidth.StringWidth(prefixPlain)
	if remaining < 0 {
		remaining = 0
	}
	rest := runConsoleTrimToWidth(fmt.Sprintf("a%d · %s · %s", ns.attempt, phase, note), remaining)
	line := "stack " + statusTag + " " + runConsoleAnsiDim(c.opts.Color, rest)

	return []string{
		runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("STACK HOOKS", width)),
		runConsoleTrimToWidth(line, width),
	}
}

func (c *RunConsole) renderFailuresLocked() []string {
	width := c.opts.Width
	if width <= 0 {
		width = 120
	}
	const maxLines = 8

	header := fmt.Sprintf("FAILURES (%d)", len(c.failures))
	lines := []string{runConsoleAnsiRedBold(c.opts.Color, runConsoleTrimToWidth(header, width))}

	// Most recent failures first for the sticky rail.
	shown := 0
	for i := len(c.failures) - 1; i >= 0; i-- {
		if shown >= maxLines {
			break
		}
		f := c.failures[i]

		class := "-"
		digest := "-"
		if f.err != nil {
			if v := strings.TrimSpace(f.err.Class); v != "" {
				class = v
			}
			if v := strings.TrimSpace(f.err.Digest); v != "" {
				digest = v
			}
		}
		digestShort := runConsoleShortDigest(digest)
		msg := strings.TrimSpace(f.msg)
		if msg == "" && f.err != nil {
			msg = strings.TrimSpace(f.err.Message)
		}
		if msg == "" {
			msg = "-"
		}

		if ns := c.nodes[strings.TrimSpace(f.nodeID)]; ns != nil {
			if strings.TrimSpace(ns.lastHookNote) != "" && !strings.Contains(msg, strings.TrimSpace(ns.lastHookNote)) {
				msg = msg + " · hook " + strings.TrimSpace(ns.lastHookNote)
			}
			if strings.TrimSpace(ns.lastHelmLine) != "" {
				msg = msg + " · helm " + strings.TrimSpace(ns.lastHelmLine)
			}
		}
		if hint := remediationHint(class); hint != "" {
			msg = msg + " · hint " + hint
		}

		line := runConsoleBulletFit(width, []string{
			runConsoleTrimToWidth(f.nodeID, 28),
			fmt.Sprintf("a%d", f.attempt),
			runConsoleTrimToWidth(class, 18),
			digestShort,
			msg,
		})
		lines = append(lines, runConsoleAnsiRed(c.opts.Color, line))
		shown++
	}
	if extra := len(c.failures) - shown; extra > 0 {
		lines = append(lines, runConsoleAnsiRed(c.opts.Color, runConsoleTrimToWidth(fmt.Sprintf("… +%d more", extra), width)))
	}
	return lines
}

func expectedStackHooksForCommand(plan *Plan, command string) int {
	if plan == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "apply":
		return len(plan.Hooks.PreApply) + len(plan.Hooks.PostApply)
	case "delete":
		return len(plan.Hooks.PreDelete) + len(plan.Hooks.PostDelete)
	default:
		return len(plan.Hooks.PreApply) + len(plan.Hooks.PostApply) + len(plan.Hooks.PreDelete) + len(plan.Hooks.PostDelete)
	}
}

func remediationHint(class string) string {
	c := strings.ToUpper(strings.TrimSpace(class))
	switch c {
	case "HOOK_FAILED":
		return "check hook summary/output (and when=)"
	case "WAIT_TIMEOUT":
		return "inspect blockers; consider increasing apply.timeout"
	case "HELM_RATE_LIMIT", "KUBE_RATE_LIMIT":
		return "try lower concurrency or rerun"
	case "HELM_ERROR":
		return "check helm logs; rerun with --helm-logs=all"
	default:
		return ""
	}
}

func (c *RunConsole) renderNodesLocked() []string {
	order := c.filteredNodeOrderLocked()
	if len(order) == 0 {
		order = []string{}
	}

	width := c.opts.Width
	if width <= 0 {
		width = 120
	}

	col := runConsoleColumnWidths(width)
	lines := make([]string, 0, len(order)+3)
	lines = append(lines, strings.TrimRight(runConsoleJoinRow(width,
		runConsoleFormatCell("Node", col.node, runConsoleAlignLeft),
		runConsoleFormatCell("Status", col.status, runConsoleAlignLeft),
		runConsoleFormatCell("Att", col.attempt, runConsoleAlignRight),
		runConsoleFormatCell("Phase", col.phase, runConsoleAlignLeft),
		runConsoleFormatCell("Note", col.note, runConsoleAlignLeft),
	), " "))
	lines = append(lines, strings.Repeat("-", width))

	now := c.now()
	for _, id := range order {
		ns := c.nodes[id]
		if ns == nil {
			ns = &runConsoleNodeState{id: id, status: "planned"}
		}
		statusCell := runConsoleStatusCell(strings.ToUpper(ns.status))
		attempt := ns.attempt
		phase := strings.TrimSpace(ns.phase)
		if phase == "" {
			phase = "-"
		}
		if id != runConsoleStackNodeID && !c.opts.Verbose && !c.opts.ShowNoisyPhases && ns.status != "failed" && isNoisyPhase(phase) {
			phase = "-"
		}
		note := c.nodeProgressNoteLocked(id, ns)
		if strings.TrimSpace(ns.wait) != "" {
			note = strings.TrimSpace(ns.wait)
		} else if ns.lastError != nil && strings.TrimSpace(ns.lastError.Class) != "" {
			cls := strings.TrimSpace(ns.lastError.Class)
			if strings.TrimSpace(note) == "" {
				note = cls
			} else {
				note = fmt.Sprintf("%s · %s", note, cls)
			}
		}
		elapsed := ""
		if !ns.startedAt.IsZero() && (ns.status == "running" || ns.status == "retrying") {
			elapsed = now.Sub(ns.startedAt).Round(100 * time.Millisecond).String()
		}
		if elapsed != "" {
			phase = fmt.Sprintf("%s (%s)", phase, elapsed)
		}

		lines = append(lines, strings.TrimRight(runConsoleJoinRow(width,
			runConsoleFormatCell(id, col.node, runConsoleAlignLeft),
			runConsoleFormatStatusCell(c.opts.Color, col.status, statusCell),
			runConsoleFormatCell(fmt.Sprintf("%d", attempt), col.attempt, runConsoleAlignRight),
			runConsoleFormatCell(phase, col.phase, runConsoleAlignLeft),
			runConsoleFormatCell(note, col.note, runConsoleAlignLeft),
		), " "))
	}
	return lines
}

func (c *RunConsole) filteredNodeOrderLocked() []string {
	order := c.nodeOrder
	if len(order) == 0 {
		for id := range c.nodes {
			order = append(order, id)
		}
		sort.Strings(order)
	}

	filter := strings.ToLower(strings.TrimSpace(c.opts.NodeFilter))
	if filter == "" {
		filter = "all"
	}

	out := make([]string, 0, len(order))
	for _, id := range order {
		if strings.TrimSpace(id) == "" || id == runConsoleStackNodeID {
			continue
		}
		status := strings.ToLower(strings.TrimSpace(c.getStatus(id)))
		switch filter {
		case "all":
		case "running":
			switch status {
			case "queued", "running", "retrying", "blocked":
			default:
				continue
			}
		case "failed":
			if status != "failed" {
				continue
			}
		default:
			// Unknown filter: behave like "all".
		}
		out = append(out, id)
	}
	return out
}

func (c *RunConsole) nodeProgressNoteLocked(id string, ns *runConsoleNodeState) string {
	status := strings.ToLower(strings.TrimSpace(ns.status))
	switch status {
	case "running", "retrying", "blocked", "failed":
	default:
		return ""
	}

	// Hooks progress.
	hExp := expectedHooksForNodeLocked(c.plan, id, c.command)
	hDone := ns.hooksOK + ns.hooksFailed + ns.hooksSkipped
	hooksPart := ""
	if hExp > 0 || hDone > 0 {
		hProg := fmt.Sprintf("%d", hDone)
		if hExp > 0 {
			hProg = fmt.Sprintf("%d/%d", hDone, hExp)
		}
		hooksPart = "hooks " + hProg
		if ns.hooksFailed > 0 {
			hooksPart += fmt.Sprintf(" fail=%d", ns.hooksFailed)
		}
	}

	// Phase progress.
	pDone, pTotal := phaseProgressLocked(c.plan, id, c.command, ns)
	phasePart := "-"
	if pTotal > 0 {
		phasePart = fmt.Sprintf("phases %d/%d", pDone, pTotal)
	}

	if phasePart == "-" && hooksPart == "" {
		return ""
	}
	if phasePart == "-" {
		return hooksPart
	}
	if hooksPart == "" {
		return phasePart
	}
	return phasePart + " · " + hooksPart
}

func expectedHooksForNodeLocked(plan *Plan, nodeID string, command string) int {
	if plan == nil || plan.ByID == nil {
		return 0
	}
	n := plan.ByID[strings.TrimSpace(nodeID)]
	if n == nil {
		return 0
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "apply":
		return len(n.Hooks.PreApply) + len(n.Hooks.PostApply)
	case "delete":
		return len(n.Hooks.PreDelete) + len(n.Hooks.PostDelete)
	default:
		return len(n.Hooks.PreApply) + len(n.Hooks.PostApply) + len(n.Hooks.PreDelete) + len(n.Hooks.PostDelete)
	}
}

func phaseProgressLocked(plan *Plan, nodeID string, command string, ns *runConsoleNodeState) (done int, total int) {
	if ns == nil {
		return 0, 0
	}
	var want []string
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "apply":
		want = []string{"render", "diff", "upgrade", "install", "wait", "post-hooks"}
		if plan != nil && plan.ByID != nil {
			if n := plan.ByID[strings.TrimSpace(nodeID)]; n != nil && verifyEnabled(n.Verify) {
				want = append(want, "verify")
			}
		}
	case "delete":
		want = []string{"destroy"}
	default:
		want = []string{"render", "diff", "upgrade", "install", "wait", "post-hooks", "verify", "destroy"}
	}

	seen := ns.phasesSeen
	doneMap := ns.phasesDone
	if seen == nil {
		seen = map[string]bool{}
	}
	if doneMap == nil {
		doneMap = map[string]bool{}
	}

	for _, p := range want {
		if seen[p] || doneMap[p] {
			total++
			if doneMap[p] {
				done++
			}
		}
	}
	return done, total
}

func (c *RunConsole) renderHelmLogsLocked() []string {
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

	hasAny := false
	mode := strings.ToLower(strings.TrimSpace(c.opts.HelmLogsMode))
	switch mode {
	case "", "true", "1":
		mode = "on"
	case "false", "0":
		mode = "off"
	}
	for _, id := range order {
		if len(c.helmLogs[id]) == 0 {
			continue
		}
		if mode != "all" {
			status := strings.ToLower(strings.TrimSpace(c.getStatus(id)))
			switch status {
			case "failed", "running", "retrying", "blocked":
			default:
				continue
			}
		}
		if len(c.helmLogs[id]) > 0 {
			hasAny = true
			break
		}
	}
	if !hasAny {
		return nil
	}

	lines := make([]string, 0, len(order)*6)
	lines = append(lines, runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("HELM LOGS", width)))

	for _, id := range order {
		entries := c.helmLogs[id]
		if len(entries) == 0 {
			continue
		}
		if mode != "all" {
			status := strings.ToLower(strings.TrimSpace(c.getStatus(id)))
			switch status {
			case "failed", "running", "retrying", "blocked":
			default:
				continue
			}
		}
		meta := c.metaByID[id]
		headerParts := []string{}
		if strings.TrimSpace(meta.cluster) != "" && strings.TrimSpace(meta.name) != "" {
			if strings.TrimSpace(meta.namespace) != "" {
				headerParts = append(headerParts, fmt.Sprintf("%s/ns/%s/%s", meta.cluster, meta.namespace, meta.name))
			} else {
				headerParts = append(headerParts, fmt.Sprintf("%s/%s", meta.cluster, meta.name))
			}
		} else {
			headerParts = append(headerParts, id)
			if strings.TrimSpace(meta.namespace) != "" {
				headerParts = append(headerParts, "ns/"+meta.namespace)
			}
		}
		var statusTag string
		var statusPlain string
		if ns := c.nodes[id]; ns != nil {
			status := runConsoleStatusCell(strings.ToUpper(ns.status))
			statusTag = runConsoleFormatStatusTag(c.opts.Color, status)
			statusPlain = strings.TrimSpace(status.text)

			if ns.attempt > 0 {
				headerParts = append(headerParts, fmt.Sprintf("a%d", ns.attempt))
			}
			phase := strings.TrimSpace(ns.phase)
			if phase != "" && (c.opts.Verbose || ns.status == "failed" || !isNoisyPhase(phase)) {
				headerParts = append(headerParts, phase)
			}
		}
		headerRest := strings.Join(headerParts, " · ")
		if strings.TrimSpace(statusTag) == "" {
			lines = append(lines, runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("─ "+headerRest, width)))
		} else {
			prefixPlain := statusPlain + " "
			remaining := width - runewidth.StringWidth(prefixPlain)
			if remaining < 0 {
				remaining = 0
			}
			rest := runConsoleTrimToWidth("─ "+headerRest, remaining)
			lines = append(lines, statusTag+" "+runConsoleAnsiDimBold(c.opts.Color, rest))
		}

		sort.SliceStable(entries, func(i, j int) bool {
			if entries[i].seq != entries[j].seq {
				return entries[i].seq < entries[j].seq
			}
			return entries[i].offset < entries[j].offset
		})
		for _, entry := range entries {
			ts := "--:--:--.---"
			if !entry.ts.IsZero() {
				ts = entry.ts.UTC().Format("15:04:05.000")
			}
			line := fmt.Sprintf("  │ %s %s", ts, strings.TrimSpace(entry.line))
			line = runConsoleTrimToWidthKeepLeft(line, width)
			lines = append(lines, runConsoleAnsiDim(c.opts.Color, line))
		}
	}

	return lines
}

func (c *RunConsole) renderHooksLocked() []string {
	if c == nil || len(c.hookEvents) == 0 {
		return nil
	}

	width := c.opts.Width
	if width <= 0 {
		width = 120
	}

	lines := []string{runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("HOOKS", width))}

	maxLines := 14
	tail := c.opts.HookTail
	if tail <= 0 {
		tail = 24
	}
	if tail < maxLines {
		maxLines = tail
	}

	shown := 0
	for i := len(c.hookEvents) - 1; i >= 0; i-- {
		if shown >= maxLines {
			break
		}
		e := c.hookEvents[i]

		if strings.ToLower(strings.TrimSpace(e.status)) == "marker" {
			text := strings.TrimSpace(e.summary)
			if text == "" {
				text = "-"
			}
			lines = append(lines, runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("─ "+text, width)))
			shown++
			continue
		}

		ts := "--:--:--.---"
		if !e.ts.IsZero() {
			ts = e.ts.UTC().Format("15:04:05.000")
		}

		status := strings.ToLower(strings.TrimSpace(e.status))
		switch status {
		case "started":
			status = "running"
		case "succeeded":
			status = "succeeded"
		case "failed":
			status = "failed"
		case "skipped":
			status = "skipped"
		default:
			status = "-"
		}
		statusCell := runConsoleStatusCell(strings.ToUpper(status))
		statusTag := runConsoleFormatStatusTag(c.opts.Color, statusCell)

		nodeLabel := e.nodeID
		if e.nodeID == runConsoleStackNodeID {
			nodeLabel = "stack"
		} else if meta, ok := c.metaByID[e.nodeID]; ok && strings.TrimSpace(meta.name) != "" {
			if strings.TrimSpace(meta.namespace) != "" && strings.TrimSpace(meta.cluster) != "" {
				nodeLabel = fmt.Sprintf("%s/ns/%s/%s", meta.cluster, meta.namespace, meta.name)
			} else if strings.TrimSpace(meta.namespace) != "" {
				nodeLabel = fmt.Sprintf("ns/%s/%s", meta.namespace, meta.name)
			} else {
				nodeLabel = meta.name
			}
		}

		parts := []string{
			ts,
			runConsoleTrimToWidth(nodeLabel, 34),
			fmt.Sprintf("a%d", e.attempt),
		}
		phase := strings.TrimSpace(e.phase)
		if phase != "" {
			parts = append(parts, phase)
		}
		hook := strings.TrimSpace(e.hook)
		if hook != "" {
			parts = append(parts, hook)
		}
		if strings.TrimSpace(e.when) != "" {
			parts = append(parts, "when="+strings.TrimSpace(e.when))
		}
		if e.runOnce {
			parts = append(parts, "runOnce")
		}
		if strings.TrimSpace(e.summary) != "" {
			parts = append(parts, strings.TrimSpace(e.summary))
		}

		line := strings.Join(parts, " · ")
		line = runConsoleTrimToWidthKeepLeft(line, width-runewidth.StringWidth(statusCell.text)-1)
		lines = append(lines, statusTag+" "+runConsoleAnsiDim(c.opts.Color, line))
		shown++
	}
	return lines
}

func (c *RunConsole) renderDetailsLocked() []string {
	if c == nil {
		return nil
	}
	width := c.opts.Width
	if width <= 0 {
		width = 120
	}

	order := c.filteredNodeOrderLocked()
	if len(order) == 0 {
		return nil
	}

	detailTail := c.opts.DetailsTail
	if detailTail <= 0 {
		detailTail = 8
	}

	// Focus details on the most actionable nodes.
	type target struct {
		id     string
		status string
	}
	var targets []target
	for _, id := range order {
		status := strings.ToLower(strings.TrimSpace(c.getStatus(id)))
		switch status {
		case "failed", "running", "retrying", "blocked":
			targets = append(targets, target{id: id, status: status})
		}
	}
	if len(targets) == 0 {
		return nil
	}
	if len(targets) > 2 {
		targets = targets[:2]
	}

	lines := []string{runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth("DETAILS", width))}
	for _, t := range targets {
		id := t.id
		ns := c.nodes[id]
		if ns == nil {
			continue
		}
		meta := c.metaByID[id]
		label := id
		if strings.TrimSpace(meta.cluster) != "" && strings.TrimSpace(meta.namespace) != "" && strings.TrimSpace(meta.name) != "" {
			label = fmt.Sprintf("%s/ns/%s/%s", meta.cluster, meta.namespace, meta.name)
		}
		title := fmt.Sprintf("─ %s · %s · phase=%s", label, strings.ToLower(strings.TrimSpace(ns.status)), strings.TrimSpace(ns.phase))
		lines = append(lines, runConsoleAnsiDimBold(c.opts.Color, runConsoleTrimToWidth(title, width)))

		if strings.TrimSpace(ns.lastHookNote) != "" {
			lines = append(lines, runConsoleAnsiDim(c.opts.Color, runConsoleTrimToWidth("  hook: "+strings.TrimSpace(ns.lastHookNote), width)))
		}

		// Recent hook events for this node.
		shownHooks := 0
		for i := len(c.hookEvents) - 1; i >= 0 && shownHooks < detailTail; i-- {
			e := c.hookEvents[i]
			if e.nodeID != id {
				continue
			}
			ts := "--:--:--.---"
			if !e.ts.IsZero() {
				ts = e.ts.UTC().Format("15:04:05.000")
			}
			msg := fmt.Sprintf("  hook[%s]: %s", strings.ToLower(e.status), hookLine(e))
			lines = append(lines, runConsoleAnsiDim(c.opts.Color, runConsoleTrimToWidthKeepLeft(fmt.Sprintf("  │ %s %s", ts, msg), width)))
			shownHooks++
		}

		// Helm tail (if captured).
		if logs := c.helmLogs[id]; len(logs) > 0 {
			lines = append(lines, runConsoleAnsiDim(c.opts.Color, runConsoleTrimToWidth("  helm:", width)))
			start := 0
			if len(logs) > detailTail {
				start = len(logs) - detailTail
			}
			for _, entry := range logs[start:] {
				ts := "--:--:--.---"
				if !entry.ts.IsZero() {
					ts = entry.ts.UTC().Format("15:04:05.000")
				}
				line := fmt.Sprintf("  │ %s %s", ts, strings.TrimSpace(entry.line))
				lines = append(lines, runConsoleAnsiDim(c.opts.Color, runConsoleTrimToWidthKeepLeft(line, width)))
			}
		} else if strings.TrimSpace(ns.lastHelmLine) != "" {
			lines = append(lines, runConsoleAnsiDim(c.opts.Color, runConsoleTrimToWidth("  helm: "+strings.TrimSpace(ns.lastHelmLine), width)))
		}
	}
	return lines
}

func hookLine(e runConsoleHookEntry) string {
	parts := []string{}
	if strings.TrimSpace(e.phase) != "" {
		parts = append(parts, displayHookPhase(e.nodeID, strings.TrimSpace(e.phase)))
	}
	if strings.TrimSpace(e.hook) != "" {
		parts = append(parts, strings.TrimSpace(e.hook))
	}
	if strings.TrimSpace(e.when) != "" {
		parts = append(parts, "when="+strings.TrimSpace(e.when))
	}
	if e.runOnce {
		parts = append(parts, "runOnce")
	}
	if strings.TrimSpace(e.summary) != "" {
		parts = append(parts, strings.TrimSpace(e.summary))
	}
	return strings.Join(parts, " · ")
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

type runConsoleAlign int

const (
	runConsoleAlignLeft runConsoleAlign = iota
	runConsoleAlignRight
)

type runConsoleCols struct {
	node    int
	status  int
	attempt int
	phase   int
	note    int
}

func runConsoleColumnWidths(total int) runConsoleCols {
	// Fixed columns with a flexible Note says "no wrapping": always fit within total.
	// Minimums are chosen to keep the surface readable in narrow terminals.
	node := 42
	status := 12
	attempt := 3
	phase := 20
	minNote := 10

	// 4 inter-column single spaces.
	used := node + status + attempt + phase + 4
	note := total - used
	for note < minNote && node > 20 {
		node--
		used--
		note = total - used
	}
	for note < minNote && phase > 10 {
		phase--
		used--
		note = total - used
	}
	for note < minNote && status > 10 {
		status--
		used--
		note = total - used
	}
	if note < 0 {
		note = 0
	}
	return runConsoleCols{node: node, status: status, attempt: attempt, phase: phase, note: note}
}

type runConsoleStatus struct {
	text  string
	color string
	bold  bool
}

func runConsoleStatusCell(status string) runConsoleStatus {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "PLANNED":
		return runConsoleStatus{text: "· PLANNED", color: "dim", bold: false}
	case "SKIPPED":
		return runConsoleStatus{text: "↷ SKIPPED", color: "dim", bold: false}
	case "QUEUED":
		return runConsoleStatus{text: "⧗ QUEUED", color: "cyan", bold: false}
	case "RUNNING":
		return runConsoleStatus{text: "▶ RUNNING", color: "blue", bold: true}
	case "RETRYING":
		return runConsoleStatus{text: "↻ RETRYING", color: "yellow", bold: true}
	case "SUCCEEDED":
		return runConsoleStatus{text: "✓ SUCCEEDED", color: "green", bold: true}
	case "FAILED":
		return runConsoleStatus{text: "✖ FAILED", color: "red", bold: true}
	case "BLOCKED":
		return runConsoleStatus{text: "⏸ BLOCKED", color: "yellow", bold: false}
	default:
		if strings.TrimSpace(status) == "" {
			status = "-"
		}
		return runConsoleStatus{text: status, color: "", bold: false}
	}
}

func runConsoleFormatStatusCell(colorEnabled bool, width int, s runConsoleStatus) string {
	cell := runConsoleFormatCell(s.text, width, runConsoleAlignLeft)
	switch s.color {
	case "dim":
		if s.bold {
			return runConsoleAnsiDimBold(colorEnabled, cell)
		}
		return runConsoleAnsiDim(colorEnabled, cell)
	case "cyan":
		if s.bold {
			return runConsoleAnsiCyanBold(colorEnabled, cell)
		}
		return runConsoleAnsiCyan(colorEnabled, cell)
	case "blue":
		if s.bold {
			return runConsoleAnsiBlueBold(colorEnabled, cell)
		}
		return runConsoleAnsiBlue(colorEnabled, cell)
	case "yellow":
		if s.bold {
			return runConsoleAnsiYellowBold(colorEnabled, cell)
		}
		return runConsoleAnsiYellow(colorEnabled, cell)
	case "green":
		if s.bold {
			return runConsoleAnsiGreenBold(colorEnabled, cell)
		}
		return runConsoleAnsiGreen(colorEnabled, cell)
	case "red":
		if s.bold {
			return runConsoleAnsiRedBold(colorEnabled, cell)
		}
		return runConsoleAnsiRed(colorEnabled, cell)
	default:
		if s.bold {
			return runConsoleAnsiBold(colorEnabled, cell)
		}
		return cell
	}
}

func runConsoleFormatStatusTag(colorEnabled bool, s runConsoleStatus) string {
	text := strings.TrimSpace(s.text)
	switch s.color {
	case "dim":
		if s.bold {
			return runConsoleAnsiDimBold(colorEnabled, text)
		}
		return runConsoleAnsiDim(colorEnabled, text)
	case "cyan":
		if s.bold {
			return runConsoleAnsiCyanBold(colorEnabled, text)
		}
		return runConsoleAnsiCyan(colorEnabled, text)
	case "blue":
		if s.bold {
			return runConsoleAnsiBlueBold(colorEnabled, text)
		}
		return runConsoleAnsiBlue(colorEnabled, text)
	case "yellow":
		if s.bold {
			return runConsoleAnsiYellowBold(colorEnabled, text)
		}
		return runConsoleAnsiYellow(colorEnabled, text)
	case "green":
		if s.bold {
			return runConsoleAnsiGreenBold(colorEnabled, text)
		}
		return runConsoleAnsiGreen(colorEnabled, text)
	case "red":
		if s.bold {
			return runConsoleAnsiRedBold(colorEnabled, text)
		}
		return runConsoleAnsiRed(colorEnabled, text)
	default:
		if s.bold {
			return runConsoleAnsiBold(colorEnabled, text)
		}
		return text
	}
}

func runConsoleJoinRow(totalWidth int, cells ...string) string {
	_ = totalWidth
	return strings.Join(cells, " ")
}

func runConsoleFormatCell(text string, width int, align runConsoleAlign) string {
	if width <= 0 {
		return ""
	}
	trimmed := runConsoleTrimToWidth(text, width)
	pad := width - runewidth.StringWidth(trimmed)
	if pad <= 0 {
		return trimmed
	}
	switch align {
	case runConsoleAlignRight:
		return strings.Repeat(" ", pad) + trimmed
	default:
		return trimmed + strings.Repeat(" ", pad)
	}
}

func runConsoleShortDigest(d string) string {
	d = strings.TrimSpace(d)
	if d == "" || d == "-" {
		return "-"
	}
	const max = 12
	if len(d) <= max {
		return d
	}
	return d[:max] + "…"
}

func runConsoleBulletFit(width int, segments []string) string {
	const sep = " • "
	if width <= 0 {
		width = 120
	}
	seg := append([]string(nil), segments...)
	for i := range seg {
		seg[i] = strings.TrimSpace(seg[i])
		if seg[i] == "" {
			seg[i] = "-"
		}
	}
	base := strings.Join(seg, sep)
	if runewidth.StringWidth(base) <= width {
		return base
	}
	if len(seg) == 0 {
		return ""
	}
	// Prefer truncating the last segment (message) to fit the line.
	prefix := strings.Join(seg[:len(seg)-1], sep)
	if prefix != "" {
		prefix += sep
	}
	avail := width - runewidth.StringWidth(prefix)
	if avail <= 0 {
		return runConsoleTrimToWidth(prefix, width)
	}
	seg[len(seg)-1] = runConsoleTrimToWidth(seg[len(seg)-1], avail)
	return runConsoleTrimToWidth(prefix+seg[len(seg)-1], width)
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

func runConsoleTrimToWidth(s string, width int) string {
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
	// Trim by rune width.
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

func runConsoleTrimToWidthKeepLeft(s string, width int) string {
	s = strings.TrimRight(s, " \t\r\n")
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

func runConsoleAnsi(enabled bool, code string, s string) string {
	if !enabled {
		return s
	}
	return "\x1b[" + code + "m" + s + "\x1b[0m"
}

func runConsoleAnsiBold(enabled bool, s string) string    { return runConsoleAnsi(enabled, "1", s) }
func runConsoleAnsiDim(enabled bool, s string) string     { return runConsoleAnsi(enabled, "2", s) }
func runConsoleAnsiDimBold(enabled bool, s string) string { return runConsoleAnsi(enabled, "2;1", s) }
func runConsoleAnsiRed(enabled bool, s string) string     { return runConsoleAnsi(enabled, "31", s) }
func runConsoleAnsiRedBold(enabled bool, s string) string { return runConsoleAnsi(enabled, "31;1", s) }
func runConsoleAnsiGreen(enabled bool, s string) string   { return runConsoleAnsi(enabled, "32", s) }
func runConsoleAnsiGreenBold(enabled bool, s string) string {
	return runConsoleAnsi(enabled, "32;1", s)
}
func runConsoleAnsiYellow(enabled bool, s string) string { return runConsoleAnsi(enabled, "33", s) }
func runConsoleAnsiYellowBold(enabled bool, s string) string {
	return runConsoleAnsi(enabled, "33;1", s)
}
func runConsoleAnsiBlue(enabled bool, s string) string     { return runConsoleAnsi(enabled, "34", s) }
func runConsoleAnsiBlueBold(enabled bool, s string) string { return runConsoleAnsi(enabled, "34;1", s) }
func runConsoleAnsiCyan(enabled bool, s string) string     { return runConsoleAnsi(enabled, "36", s) }
func runConsoleAnsiCyanBold(enabled bool, s string) string { return runConsoleAnsi(enabled, "36;1", s) }
