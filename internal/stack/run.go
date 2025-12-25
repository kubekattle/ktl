// File: internal/stack/run.go
// Brief: Stack runner (apply/delete orchestration).

package stack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type RunOptions struct {
	Command     string
	Plan        *Plan
	Concurrency int
	FailFast    bool
	AutoApprove bool

	ProgressiveConcurrency bool

	Kubeconfig      *string
	KubeContext     *string
	LogLevel        *string
	RemoteAgentAddr *string

	RunID   string
	RunRoot string

	Selector        RunSelector
	FailMode        string
	MaxAttempts     int
	InitialAttempts map[string]int
}

func Run(ctx context.Context, opts RunOptions, out io.Writer, errOut io.Writer) error {
	if opts.Plan == nil {
		return fmt.Errorf("plan is required")
	}
	cmd := strings.ToLower(strings.TrimSpace(opts.Command))
	if cmd != "apply" && cmd != "delete" {
		return fmt.Errorf("unknown stack command %q", opts.Command)
	}
	if opts.RemoteAgentAddr != nil && strings.TrimSpace(*opts.RemoteAgentAddr) != "" {
		return fmt.Errorf("ktl stack %s: --remote-agent is not supported yet", cmd)
	}
	concurrency := opts.Concurrency
	if concurrency <= 0 {
		concurrency = 1
	}

	run := newRunState(opts.Plan, cmd)
	if opts.RunID != "" {
		run.RunID = opts.RunID
	}
	if opts.RunRoot != "" {
		run.RunRoot = opts.RunRoot
	}
	run.Concurrency = concurrency
	if strings.TrimSpace(opts.FailMode) != "" {
		run.FailMode = opts.FailMode
	} else if opts.FailFast {
		run.FailMode = "fail-fast"
	} else {
		run.FailMode = "continue"
	}
	run.Selector = opts.Selector
	if opts.InitialAttempts != nil {
		for _, n := range run.Nodes {
			if a, ok := opts.InitialAttempts[n.ID]; ok {
				n.Attempt = a
			}
		}
	}
	maxAttempts := opts.MaxAttempts
	if maxAttempts < 1 {
		maxAttempts = 1
	}
	if err := run.InitFiles(); err != nil {
		return err
	}
	if run.store != nil {
		defer run.store.Close()
	}
	if err := run.WritePlan(); err != nil {
		return err
	}

	fmt.Fprintf(errOut, "ktl stack %s: %d releases (runId=%s)\n", cmd, len(run.Nodes), run.RunID)

	exec := &helmExecutor{
		kubeconfig:  opts.Kubeconfig,
		kubeContext: opts.KubeContext,
		out:         out,
		errOut:      errOut,
	}

	start := time.Now()
	s := newScheduler(run.Nodes, cmd)
	var mu sync.Mutex
	var firstErr error
	var poolMu sync.Mutex
	targetWorkers := concurrency
	runningWorkers := 0
	consecutiveFailures := 0
	if opts.ProgressiveConcurrency && concurrency > 1 {
		targetWorkers = 1
	}

	var worker func()
	var wg sync.WaitGroup
	spawnWorker := func() {
		wg.Add(1)
		go func() {
			defer wg.Done()
			worker()
		}()
	}
	maybeSpawn := func() {
		if !(opts.ProgressiveConcurrency && concurrency > 1) {
			return
		}
		poolMu.Lock()
		want := targetWorkers
		have := runningWorkers
		poolMu.Unlock()
		for have < want {
			spawnWorker()
			have++
		}
	}

	worker = func() {
		poolMu.Lock()
		runningWorkers++
		poolMu.Unlock()
		defer func() {
			poolMu.Lock()
			runningWorkers--
			poolMu.Unlock()
		}()
		for {
			node := s.NextReady()
			if node == nil {
				return
			}
			for {
				node.Attempt++
				run.AppendEvent(node.ID, "NODE_RUNNING", node.Attempt, "", nil)
				err := exec.RunNode(ctx, node, cmd)
				if err == nil {
					s.MarkSucceeded(node.ID)
					run.AppendEvent(node.ID, "NODE_SUCCEEDED", node.Attempt, "", nil)
					if opts.ProgressiveConcurrency && concurrency > 1 {
						poolMu.Lock()
						consecutiveFailures = 0
						if targetWorkers < concurrency {
							targetWorkers++
						}
						poolMu.Unlock()
						maybeSpawn()
					}
					break
				}
				class := classifyError(err)
				retryable := isRetryableClass(class)
				run.AppendEvent(node.ID, "NODE_FAILED", node.Attempt, err.Error(), &RunError{Class: class, Message: err.Error()})
				if opts.ProgressiveConcurrency && concurrency > 1 {
					poolMu.Lock()
					consecutiveFailures++
					if consecutiveFailures >= 2 {
						targetWorkers = 1
					} else if targetWorkers > 1 {
						targetWorkers--
					}
					poolMu.Unlock()
					maybeSpawn()
				}
				if retryable && node.Attempt < maxAttempts {
					backoff := retryBackoff(node.Attempt)
					run.AppendEvent(node.ID, "NODE_RETRY_SCHEDULED", node.Attempt+1, fmt.Sprintf("backoff=%s", backoff), &RunError{Class: class, Message: err.Error()})
					time.Sleep(backoff)
					continue
				}

				mu.Lock()
				if firstErr == nil {
					firstErr = err
				}
				mu.Unlock()

				s.MarkFailed(node.ID, err)
				if opts.FailFast {
					s.Stop()
					return
				}
				break
			}
		}
	}

	run.AppendEvent("", "RUN_STARTED", 0, "", nil)
	run.WriteSummarySnapshot(run.BuildSummary("running", start, s.Snapshot()))

	for i := 0; i < targetWorkers; i++ {
		spawnWorker()
	}
	wg.Wait()

	s.FinalizeBlocked()
	status := "succeeded"
	if firstErr != nil {
		status = "failed"
	}
	run.AppendEvent("", "RUN_COMPLETED", 0, status, nil)
	run.WriteSummarySnapshot(run.BuildSummary(status, start, s.Snapshot()))

	printRunSummary(errOut, run, start)
	if firstErr != nil {
		return firstErr
	}
	return nil
}

func printRunSummary(w io.Writer, run *runState, startedAt time.Time) {
	summary := run.BuildSummary("final", startedAt, run.lastSnapshot)
	sort.Strings(summary.Order)
	fmt.Fprintln(w)
	fmt.Fprintf(w, "RESULT\t%s\t(planned=%d succeeded=%d failed=%d blocked=%d)\n",
		summary.Status, summary.Totals.Planned, summary.Totals.Succeeded, summary.Totals.Failed, summary.Totals.Blocked)
	for _, id := range summary.Order {
		ns := summary.Nodes[id]
		note := ns.Error
		if len(note) > 140 {
			note = note[:140] + "…"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\n", id, strings.ToUpper(ns.Status), note)
	}
}

type runState struct {
	RunID   string
	RunRoot string
	store   *stackStateStore

	Plan        *Plan
	Command     string
	Nodes       []*runNode
	Concurrency int
	FailMode    string
	Selector    RunSelector

	mu sync.Mutex

	lastSnapshot schedulerSnapshot
}

type runNode struct {
	*ResolvedRelease
	Attempt int
}

func newRunState(p *Plan, command string) *runState {
	// Include sub-second precision to avoid collisions when commands re-run quickly
	// (e.g. rerun-failed immediately after apply).
	runID := time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
	return &runState{
		RunID:   runID,
		RunRoot: filepath.Join(p.StackRoot, ".ktl", "stack", "runs", runID),
		Plan:    p,
		Command: command,
		Nodes:   wrapRunNodes(p.Nodes),
	}
}

func wrapRunNodes(nodes []*ResolvedRelease) []*runNode {
	out := make([]*runNode, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, &runNode{ResolvedRelease: n})
	}
	return out
}

func (r *runState) InitFiles() error {
	// Prefer durable sqlite state store; keep legacy RunRoot only as a logical identifier.
	s, err := openStackStateStore(r.Plan.StackRoot, false)
	if err != nil {
		return err
	}
	r.store = s
	return nil
}

func (r *runState) WritePlan() error {
	for _, n := range r.Plan.Nodes {
		hash, err := ComputeEffectiveInputHash(n, true)
		if err != nil {
			return err
		}
		n.EffectiveInputHash = hash
	}
	if r.store == nil {
		return fmt.Errorf("internal error: state store not initialized")
	}
	return r.store.CreateRun(context.Background(), r, r.Plan)
}

func (r *runState) AppendEvent(nodeID, typ string, attempt int, message string, runErr *RunError) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := RunEvent{
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		RunID:   r.RunID,
		NodeID:  nodeID,
		Type:    typ,
		Attempt: attempt,
		Message: message,
		Error:   runErr,
	}
	if r.store != nil {
		_ = r.store.AppendEvent(context.Background(), r.RunID, ev)
		return
	}
	_ = appendJSONLine(filepath.Join(r.RunRoot, "events.jsonl"), ev)
}

func (r *runState) WriteSummarySnapshot(s *RunSummary) {
	if r.store != nil {
		_ = r.store.WriteSummary(context.Background(), r.RunID, s)
		return
	}
	_ = writeJSONAtomic(filepath.Join(r.RunRoot, "summary.json"), s)
}

func (r *runState) BuildSummary(status string, startedAt time.Time, snap schedulerSnapshot) *RunSummary {
	r.lastSnapshot = snap
	s := &RunSummary{
		APIVersion: "ktl.dev/stack-run/v1",
		RunID:      r.RunID,
		Status:     status,
		StartedAt:  startedAt.UTC().Format(time.RFC3339Nano),
		UpdatedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		Totals:     RunTotals{Planned: len(r.Nodes)},
		Nodes:      map[string]RunNodeSummary{},
	}
	for _, n := range r.Nodes {
		nodeStatus := snap.Status[n.ID]
		if nodeStatus == "" {
			nodeStatus = "planned"
		}
		ns := RunNodeSummary{Status: nodeStatus, Attempt: n.Attempt}
		if err := snap.Errors[n.ID]; err != nil {
			ns.Error = err.Error()
		}
		s.Nodes[n.ID] = ns
		s.Order = append(s.Order, n.ID)
		switch nodeStatus {
		case "succeeded":
			s.Totals.Succeeded++
		case "failed":
			s.Totals.Failed++
		case "blocked":
			s.Totals.Blocked++
		case "running":
			s.Totals.Running++
		}
	}
	return s
}

type scheduler struct {
	mu sync.Mutex

	nodes map[string]*runNode
	order []string

	inDegree   map[string]int
	deps       map[string][]string
	dependents map[string][]string

	ready []string

	status map[string]string // planned, running, succeeded, failed, blocked
	errs   map[string]error

	stopped bool
}

type schedulerSnapshot struct {
	Status map[string]string
	Errors map[string]error
}

func newScheduler(nodes []*runNode, command string) *scheduler {
	s := &scheduler{
		nodes:      map[string]*runNode{},
		inDegree:   map[string]int{},
		deps:       map[string][]string{},
		dependents: map[string][]string{},
		status:     map[string]string{},
		errs:       map[string]error{},
	}

	byName := map[string]*runNode{}
	for _, n := range nodes {
		s.nodes[n.ID] = n
		s.order = append(s.order, n.ID)
		s.status[n.ID] = "planned"
		byName[n.Name] = n
	}

	if command == "apply" {
		for _, n := range nodes {
			for _, depName := range n.Needs {
				dep := byName[depName]
				s.deps[n.ID] = append(s.deps[n.ID], dep.ID)
				s.dependents[dep.ID] = append(s.dependents[dep.ID], n.ID)
			}
		}
	} else {
		// delete: reverse edges, so that dependents run before dependencies.
		for _, n := range nodes {
			for _, depName := range n.Needs {
				dep := byName[depName]
				s.deps[dep.ID] = append(s.deps[dep.ID], n.ID)
				s.dependents[n.ID] = append(s.dependents[n.ID], dep.ID)
			}
		}
	}

	for id := range s.nodes {
		s.inDegree[id] = len(s.deps[id])
		if s.inDegree[id] == 0 {
			s.ready = append(s.ready, id)
		}
	}
	sort.Strings(s.ready)
	sort.Strings(s.order)
	for id := range s.dependents {
		sort.Strings(s.dependents[id])
	}
	for id := range s.deps {
		sort.Strings(s.deps[id])
	}
	return s
}

func (s *scheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.stopped = true
}

func (s *scheduler) NextReady() *runNode {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return nil
	}
	for len(s.ready) > 0 {
		id := s.ready[0]
		s.ready = s.ready[1:]
		if s.status[id] != "planned" {
			continue
		}
		// Ensure all deps succeeded.
		ok := true
		for _, depID := range s.deps[id] {
			if s.status[depID] != "succeeded" {
				ok = false
				break
			}
		}
		if !ok {
			s.status[id] = "blocked"
			continue
		}
		s.status[id] = "running"
		return s.nodes[id]
	}
	return nil
}

func (s *scheduler) MarkSucceeded(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status[id] != "running" {
		return
	}
	s.status[id] = "succeeded"
	for _, depID := range s.dependents[id] {
		s.inDegree[depID]--
		if s.inDegree[depID] == 0 {
			s.ready = append(s.ready, depID)
		}
	}
	sort.Strings(s.ready)
}

func (s *scheduler) MarkFailed(id string, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status[id] != "running" {
		return
	}
	s.status[id] = "failed"
	s.errs[id] = err
	for _, depID := range s.dependents[id] {
		// Still decrement so graph progresses, but the dependent will be blocked
		// when we check predecessor status.
		s.inDegree[depID]--
		if s.inDegree[depID] == 0 {
			s.ready = append(s.ready, depID)
		}
	}
	sort.Strings(s.ready)
}

func (s *scheduler) FinalizeBlocked() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, id := range s.order {
		if s.status[id] != "planned" {
			continue
		}
		for _, depID := range s.deps[id] {
			if s.status[depID] == "failed" || s.status[depID] == "blocked" {
				s.status[id] = "blocked"
				break
			}
		}
	}
}

func (s *scheduler) Snapshot() schedulerSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	status := map[string]string{}
	for k, v := range s.status {
		status[k] = v
	}
	errs := map[string]error{}
	for k, v := range s.errs {
		errs[k] = v
	}
	return schedulerSnapshot{Status: status, Errors: errs}
}

// run errors should stay actionable; prefer returning the first error but attach
// a clear prefix.
func wrapNodeErr(node *ResolvedRelease, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.Canceled) {
		return err
	}
	return fmt.Errorf("%s: %w", node.ID, err)
}
