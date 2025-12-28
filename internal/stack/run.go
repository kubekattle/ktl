// File: internal/stack/run.go
// Brief: Stack runner (apply/delete orchestration).

package stack

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"
)

type RunOptions struct {
	Command     string
	Plan        *Plan
	Concurrency int
	FailFast    bool
	AutoApprove bool
	DryRun      bool
	Diff        bool
	Executor    NodeExecutor

	KubeQPS   float32
	KubeBurst int

	MaxConcurrencyPerNamespace int
	MaxConcurrencyByKind       map[string]int
	ParallelismGroupLimit      int
	Adaptive                   *AdaptiveConcurrencyOptions

	ResumeStatusByID  map[string]string
	ResumeFromRunID   string
	ResumeAttemptByID map[string]int

	ProgressiveConcurrency bool
	Lock                   bool
	LockOwner              string
	LockTTL                time.Duration
	TakeoverLock           bool

	Kubeconfig      *string
	KubeContext     *string
	LogLevel        *string
	RemoteAgentAddr *string

	RunID string

	Selector        RunSelector
	FailMode        string
	MaxAttempts     int
	InitialAttempts map[string]int

	EventObservers []RunEventObserver
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
	run.observers = append([]RunEventObserver(nil), opts.EventObservers...)
	if opts.Kubeconfig != nil {
		run.Kubeconfig = strings.TrimSpace(*opts.Kubeconfig)
	}
	if opts.KubeContext != nil {
		run.KubeContext = strings.TrimSpace(*opts.KubeContext)
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
	if err := run.InitFiles(opts.Lock, opts.LockOwner, opts.LockTTL, opts.TakeoverLock); err != nil {
		return err
	}
	if run.store != nil {
		defer func() {
			if run.lockHeld {
				_ = run.store.ReleaseLock(context.Background(), run.lockOwner, run.lockRunID)
			}
			_ = run.store.Close()
		}()
	}
	if err := run.WritePlan(); err != nil {
		return err
	}

	exec := opts.Executor
	if exec == nil {
		exec = &helmExecutor{
			kubeconfig:  opts.Kubeconfig,
			kubeContext: opts.KubeContext,
			run:         run,
			out:         out,
			errOut:      errOut,
			dryRun:      opts.DryRun,
			diff:        opts.Diff,
			kubeQPS:     opts.KubeQPS,
			kubeBurst:   opts.KubeBurst,
		}
	}
	exec = &hookedExecutor{base: exec, run: run, opts: opts, out: out, errOut: errOut}

	start := time.Now()
	s := newScheduler(run.Nodes, cmd)
	nodesByID := map[string]*runNode{}
	for _, n := range run.Nodes {
		nodesByID[n.ID] = n
	}
	var mu sync.Mutex
	var firstErr error
	var poolMu sync.Mutex
	targetWorkers := concurrency
	runningWorkers := 0
	var adaptive *AdaptiveConcurrency
	if opts.ProgressiveConcurrency && concurrency > 1 {
		if opts.Adaptive != nil {
			adaptive = NewAdaptiveConcurrencyWithOptions(concurrency, *opts.Adaptive)
		} else {
			adaptive = NewAdaptiveConcurrency(concurrency)
		}
		targetWorkers = adaptive.Target
	}

	var semMu sync.Mutex
	type budgetSem struct {
		sem   *semaphore.Weighted
		limit int64
		inUse atomic.Int64
	}
	getBudgetSem := func(m map[string]*budgetSem, key string, limit int64) *budgetSem {
		semMu.Lock()
		defer semMu.Unlock()
		if v, ok := m[key]; ok {
			return v
		}
		if limit < 1 {
			limit = 1
		}
		b := &budgetSem{sem: semaphore.NewWeighted(limit), limit: limit}
		m[key] = b
		return b
	}

	nsSem := map[string]*budgetSem{}
	getNSSem := func(namespace string) *budgetSem {
		limit := int64(opts.MaxConcurrencyPerNamespace)
		if limit < 1 {
			limit = 1
		}
		return getBudgetSem(nsSem, namespace, limit)
	}

	kindSem := map[string]*budgetSem{}
	getKindSem := func(kind string) *budgetSem {
		kind = strings.TrimSpace(kind)
		if kind == "" {
			return nil
		}
		limit := int64(0)
		if opts.MaxConcurrencyByKind != nil {
			if v, ok := opts.MaxConcurrencyByKind[kind]; ok {
				limit = int64(v)
			}
		}
		if limit < 1 {
			limit = 1
		}
		return getBudgetSem(kindSem, kind, limit)
	}

	groupSem := map[string]*budgetSem{}
	getGroupSem := func(group string) *budgetSem {
		group = strings.TrimSpace(group)
		if group == "" {
			return nil
		}
		limit := int64(opts.ParallelismGroupLimit)
		if limit < 1 {
			limit = 1
		}
		return getBudgetSem(groupSem, group, limit)
	}

	acquireBudget := func(ctx context.Context, node *runNode, b *budgetSem, waitType string, waitKey string, waited *bool) error {
		if b == nil {
			return nil
		}
		for {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			if b.sem.TryAcquire(1) {
				b.inUse.Add(1)
				return nil
			}
			if waited != nil && !*waited {
				*waited = true
				used := b.inUse.Load()
				msg := fmt.Sprintf("waiting: %s budget %s (limit=%d used=%d)", waitType, waitKey, b.limit, used)
				run.AppendEvent(node.ID, BudgetWait, node.Attempt, msg, map[string]any{
					"budgetType": waitType,
					"budgetKey":  waitKey,
					"limit":      b.limit,
					"used":       used,
				}, nil)
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	releaseBudget := func(b *budgetSem) {
		if b == nil {
			return
		}
		b.inUse.Add(-1)
		b.sem.Release(1)
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
			if err := ctx.Err(); err != nil {
				s.Stop()
				return
			}
			// Allow shrinking the pool while work is still pending by having idle workers
			// self-terminate once the target drops below the current worker count.
			if adaptive != nil {
				poolMu.Lock()
				shouldExit := runningWorkers > targetWorkers
				poolMu.Unlock()
				if shouldExit {
					return
				}
			}
			node := s.NextReady()
			if newlyReady := s.TakeNewlyReady(); len(newlyReady) > 0 {
				ids := append([]string(nil), newlyReady...)
				sort.Strings(ids)
				for _, id := range ids {
					run.AppendEvent(id, NodeQueued, 0, "ready", nil, nil)
				}
			}
			if blocked := s.TakeNewlyBlocked(); len(blocked) > 0 {
				ids := make([]string, 0, len(blocked))
				for id := range blocked {
					ids = append(ids, id)
				}
				sort.Strings(ids)
				for _, id := range ids {
					attempt := 0
					if n := nodesByID[id]; n != nil {
						attempt = n.Attempt
					}
					run.AppendEvent(id, NodeBlocked, attempt, blocked[id], nil, nil)
				}
			}
			if node == nil {
				return
			}
			for {
				node.Attempt++
				run.AppendEvent(node.ID, NodeRunning, node.Attempt, "", nil, nil)
				releaseNS := strings.TrimSpace(node.Namespace)
				if releaseNS == "" {
					releaseNS = "default"
				}
				var (
					semNS    *budgetSem
					semKind  *budgetSem
					semGroup *budgetSem
				)
				if node.Parallelism != "" {
					semGroup = getGroupSem(node.Parallelism)
					waited := false
					if err := acquireBudget(ctx, node, semGroup, "group", node.Parallelism, &waited); err != nil {
						s.MarkFailed(node.ID, err)
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
						s.Stop()
						return
					}
				}
				if node.InferredPrimaryKind != "" && opts.MaxConcurrencyByKind != nil {
					if _, ok := opts.MaxConcurrencyByKind[node.InferredPrimaryKind]; ok {
						semKind = getKindSem(node.InferredPrimaryKind)
						waited := false
						if err := acquireBudget(ctx, node, semKind, "kind", node.InferredPrimaryKind, &waited); err != nil {
							releaseBudget(semGroup)
							s.MarkFailed(node.ID, err)
							mu.Lock()
							if firstErr == nil {
								firstErr = err
							}
							mu.Unlock()
							s.Stop()
							return
						}
					}
				}
				if opts.MaxConcurrencyPerNamespace > 0 {
					semNS = getNSSem(releaseNS)
					waited := false
					if err := acquireBudget(ctx, node, semNS, "namespace", releaseNS, &waited); err != nil {
						releaseBudget(semKind)
						releaseBudget(semGroup)
						s.MarkFailed(node.ID, err)
						mu.Lock()
						if firstErr == nil {
							firstErr = err
						}
						mu.Unlock()
						s.Stop()
						return
					}
				}
				err := exec.RunNode(ctx, node, cmd)
				if semNS != nil {
					releaseBudget(semNS)
				}
				if semKind != nil {
					releaseBudget(semKind)
				}
				if semGroup != nil {
					releaseBudget(semGroup)
				}
				if err == nil {
					s.MarkSucceeded(node.ID)
					run.AppendEvent(node.ID, NodeSucceeded, node.Attempt, "", nil, nil)
					if adaptive != nil {
						poolMu.Lock()
						before := adaptive.Target
						changed, reason := adaptive.OnSuccess()
						if changed {
							targetWorkers = adaptive.Target
							msg := fmt.Sprintf("concurrency: %d -> %d reason=%s window=%d failRate=%.2f", before, adaptive.Target, reason, len(adaptive.window), adaptive.failureRate())
							run.AppendEvent("", RunConcurrency, 0, msg, map[string]any{
								"from":     before,
								"to":       adaptive.Target,
								"reason":   reason,
								"window":   len(adaptive.window),
								"failRate": adaptive.failureRate(),
							}, nil)
						}
						poolMu.Unlock()
						maybeSpawn()
					}
					break
				}
				if errors.Is(err, context.Canceled) {
					s.MarkFailed(node.ID, err)
					mu.Lock()
					if firstErr == nil {
						firstErr = err
					}
					mu.Unlock()
					s.Stop()
					return
				}
				class := classifyError(err)
				retryable := isRetryableClass(class)
				run.AppendEvent(node.ID, NodeFailed, node.Attempt, err.Error(), nil, &RunError{Class: class, Message: err.Error(), Digest: computeRunErrorDigest(class, err.Error())})
				if adaptive != nil {
					poolMu.Lock()
					before := adaptive.Target
					changed, reason := adaptive.OnFailure(class)
					if changed {
						targetWorkers = adaptive.Target
						msg := fmt.Sprintf("concurrency: %d -> %d reason=%s window=%d failRate=%.2f", before, adaptive.Target, reason, len(adaptive.window), adaptive.failureRate())
						run.AppendEvent("", RunConcurrency, 0, msg, map[string]any{
							"from":     before,
							"to":       adaptive.Target,
							"reason":   reason,
							"class":    class,
							"window":   len(adaptive.window),
							"failRate": adaptive.failureRate(),
						}, nil)
					}
					poolMu.Unlock()
					maybeSpawn()
				}
				if retryable && node.Attempt < maxAttempts {
					backoff := retryBackoff(node.Attempt)
					run.AppendEvent(node.ID, RetryScheduled, node.Attempt+1, fmt.Sprintf("backoff=%s", backoff), map[string]any{
						"backoff": backoff.String(),
					}, &RunError{Class: class, Message: err.Error(), Digest: computeRunErrorDigest(class, err.Error())})
					select {
					case <-ctx.Done():
						s.MarkFailed(node.ID, ctx.Err())
						mu.Lock()
						if firstErr == nil {
							firstErr = ctx.Err()
						}
						mu.Unlock()
						s.Stop()
						return
					case <-time.After(backoff):
					}
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

	for _, n := range run.Nodes {
		run.AppendEvent(n.ID, NodeMeta, 0, "", map[string]any{
			"cluster":          strings.TrimSpace(n.Cluster.Name),
			"namespace":        strings.TrimSpace(n.Namespace),
			"name":             strings.TrimSpace(n.Name),
			"executionGroup":   n.ExecutionGroup,
			"parallelismGroup": strings.TrimSpace(n.Parallelism),
			"critical":         n.Critical,
			"primaryKind":      strings.TrimSpace(n.InferredPrimaryKind),
		}, nil)
	}
	run.AppendEvent("", RunStarted, 0, fmt.Sprintf("command=%s planned=%d", cmd, len(run.Nodes)), map[string]any{
		"command":     cmd,
		"planned":     len(run.Nodes),
		"stackName":   strings.TrimSpace(run.Plan.StackName),
		"stackRoot":   strings.TrimSpace(run.Plan.StackRoot),
		"profile":     strings.TrimSpace(run.Plan.Profile),
		"concurrency": run.Concurrency,
		"failMode":    strings.TrimSpace(run.FailMode),
	}, nil)

	// Stack-level runOnce hooks (pre).
	if err := runHookList(ctx, hookRunContext{
		run:     run,
		opts:    opts,
		errOut:  errOut,
		phase:   "pre-" + cmd,
		status:  "success",
		baseDir: run.Plan.StackRoot,
	}, hooksForRunOnce(run.Plan, cmd, true)); err != nil {
		firstErr = err
		run.AppendEvent("", RunCompleted, 0, "failed", map[string]any{"status": "failed"}, nil)
		run.WriteSummarySnapshot(run.BuildSummary("failed", start, s.Snapshot()))
		if run.store != nil {
			_, _ = run.store.FinalizeRun(context.Background(), run.RunID, time.Now().UTC().UnixNano(), run.eventPrevHash)
			_ = run.store.CheckpointPortable(context.Background())
		}
		return firstErr
	}

	// Seed the scheduler with already-completed nodes from a previous run.
	// Emit NODE_SUCCEEDED events so the new run's sqlite summary matches the resumed state.
	if len(opts.ResumeStatusByID) > 0 {
		for _, n := range run.Nodes {
			if strings.TrimSpace(opts.ResumeStatusByID[n.ID]) != "succeeded" {
				continue
			}
			if opts.ResumeAttemptByID != nil {
				if a, ok := opts.ResumeAttemptByID[n.ID]; ok && a > n.Attempt {
					n.Attempt = a
				}
			}
			s.SeedSucceeded(n.ID)

			msg := "resume: already succeeded"
			if strings.TrimSpace(opts.ResumeFromRunID) != "" {
				msg = fmt.Sprintf("resume: already succeeded in run %s", strings.TrimSpace(opts.ResumeFromRunID))
			}
			run.AppendEvent(n.ID, NodeSucceeded, n.Attempt, msg, nil, nil)
		}
	}
	run.WriteSummarySnapshot(run.BuildSummary("running", start, s.Snapshot()))

	for i := 0; i < targetWorkers; i++ {
		spawnWorker()
	}
	wg.Wait()

	s.FinalizeBlocked()
	if blocked := s.TakeNewlyBlocked(); len(blocked) > 0 {
		ids := make([]string, 0, len(blocked))
		for id := range blocked {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		for _, id := range ids {
			attempt := 0
			if n := nodesByID[id]; n != nil {
				attempt = n.Attempt
			}
			run.AppendEvent(id, NodeBlocked, attempt, blocked[id], nil, nil)
		}
	}
	status := "succeeded"
	if firstErr != nil {
		status = "failed"
	}

	// Stack-level runOnce hooks (post). If the run already failed, keep the original error but still emit hook events/output.
	postStatus := "success"
	if status == "failed" {
		postStatus = "failure"
	}
	if err := runHookList(ctx, hookRunContext{
		run:     run,
		opts:    opts,
		errOut:  errOut,
		phase:   "post-" + cmd,
		status:  postStatus,
		baseDir: run.Plan.StackRoot,
	}, hooksForRunOnce(run.Plan, cmd, false)); err != nil && firstErr == nil {
		firstErr = err
		status = "failed"
	}
	run.AppendEvent("", RunCompleted, 0, status, map[string]any{"status": status}, nil)
	run.WriteSummarySnapshot(run.BuildSummary(status, start, s.Snapshot()))
	if run.store != nil {
		_, _ = run.store.FinalizeRun(context.Background(), run.RunID, time.Now().UTC().UnixNano(), run.eventPrevHash)
		_ = run.store.CheckpointPortable(context.Background())
	}

	if firstErr != nil {
		return firstErr
	}
	return nil
}

type runState struct {
	RunID string
	store *stackStateStore

	Plan        *Plan
	Command     string
	Nodes       []*runNode
	Concurrency int
	FailMode    string
	Selector    RunSelector
	Kubeconfig  string
	KubeContext string

	mu sync.Mutex

	lastSnapshot  schedulerSnapshot
	eventSeq      int64
	eventPrevHash string
	observers     []RunEventObserver

	lockOwner string
	lockRunID string
	lockHeld  bool
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

func (r *runState) InitFiles(lock bool, lockOwner string, lockTTL time.Duration, takeover bool) error {
	// Use durable sqlite state store for all run artifacts.
	s, err := openStackStateStore(r.Plan.StackRoot, false)
	if err != nil {
		return err
	}
	if lock {
		owner := strings.TrimSpace(lockOwner)
		if owner == "" {
			owner = defaultLockOwner()
		}
		ttl := lockTTL
		if ttl <= 0 {
			ttl = 30 * time.Minute
		}
		l, err := s.AcquireLock(context.Background(), owner, ttl, takeover, r.RunID)
		if err != nil {
			_ = s.Close()
			return err
		}
		r.lockOwner = l.Owner
		r.lockRunID = l.RunID
		r.lockHeld = true
	}
	r.store = s
	return nil
}

func (r *runState) WritePlan() error {
	for _, n := range r.Plan.Nodes {
		hash, input, err := ComputeEffectiveInputHash(r.Plan.StackRoot, n, true)
		if err != nil {
			return err
		}
		n.EffectiveInputHash = hash
		n.EffectiveInput = input
	}
	if r.store == nil {
		return fmt.Errorf("internal error: state store not initialized")
	}
	return r.store.CreateRun(context.Background(), r, r.Plan)
}

func (r *runState) AppendEvent(nodeID string, typ RunEventType, attempt int, message string, fields any, runErr *RunError) {
	r.emitEvent(nodeID, typ, attempt, message, fields, runErr, true)
}

func (r *runState) EmitEphemeralEvent(nodeID string, typ RunEventType, attempt int, message string, fields any) {
	r.emitEvent(nodeID, typ, attempt, message, fields, nil, false)
}

func (r *runState) emitEvent(nodeID string, typ RunEventType, attempt int, message string, fields any, runErr *RunError, persist bool) {
	r.mu.Lock()
	r.eventSeq++
	ev := RunEvent{
		Seq:     r.eventSeq,
		TS:      time.Now().UTC().Format(time.RFC3339Nano),
		RunID:   r.RunID,
		NodeID:  nodeID,
		Type:    string(typ),
		Attempt: attempt,
		Message: message,
		Fields:  fields,
		Error:   runErr,
	}
	observers := append([]RunEventObserver(nil), r.observers...)
	if persist {
		ev.PrevDigest = r.eventPrevHash
		ev.Digest, ev.CRC32 = computeRunEventIntegrity(ev)
		r.eventPrevHash = ev.Digest
		if r.store != nil {
			_ = r.store.AppendEvent(context.Background(), r.RunID, ev)
		}
	}
	r.mu.Unlock()

	for _, obs := range observers {
		if obs == nil {
			continue
		}
		obs.ObserveRunEvent(ev)
	}
}

func (r *runState) WriteSummarySnapshot(s *RunSummary) {
	if r.store == nil {
		return
	}
	_ = r.store.WriteSummary(context.Background(), r.RunID, s)
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

	ready      []string
	newlyReady []string

	status map[string]string // planned, running, succeeded, failed, blocked
	errs   map[string]error

	newlyBlocked []string
	blockedBy    map[string]string

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
		blockedBy:  map[string]string{},
	}

	byKey := map[string]*runNode{}
	for _, n := range nodes {
		s.nodes[n.ID] = n
		s.order = append(s.order, n.ID)
		s.status[n.ID] = "planned"
		byKey[schedulerKey(n.Cluster.Name, n.Name)] = n
	}

	if command == "apply" {
		for _, n := range nodes {
			for _, depName := range n.Needs {
				dep := byKey[schedulerKey(n.Cluster.Name, depName)]
				if dep == nil {
					continue
				}
				s.deps[n.ID] = append(s.deps[n.ID], dep.ID)
				s.dependents[dep.ID] = append(s.dependents[dep.ID], n.ID)
			}
		}
	} else {
		// delete: reverse edges, so that dependents run before dependencies.
		for _, n := range nodes {
			for _, depName := range n.Needs {
				dep := byKey[schedulerKey(n.Cluster.Name, depName)]
				if dep == nil {
					continue
				}
				s.deps[dep.ID] = append(s.deps[dep.ID], n.ID)
				s.dependents[n.ID] = append(s.dependents[n.ID], dep.ID)
			}
		}
	}

	for id := range s.nodes {
		s.inDegree[id] = len(s.deps[id])
		if s.inDegree[id] == 0 {
			s.ready = append(s.ready, id)
			s.newlyReady = append(s.newlyReady, id)
		}
	}
	s.sortReady()
	sort.Strings(s.order)
	for id := range s.dependents {
		sort.Strings(s.dependents[id])
	}
	for id := range s.deps {
		sort.Strings(s.deps[id])
	}
	return s
}

func schedulerKey(cluster string, name string) string {
	return strings.TrimSpace(cluster) + "\n" + strings.TrimSpace(name)
}

func (s *scheduler) sortReady() {
	sort.Slice(s.ready, func(i, j int) bool {
		return releaseReadyKey(s.nodes[s.ready[i]].ResolvedRelease) < releaseReadyKey(s.nodes[s.ready[j]].ResolvedRelease)
	})
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
		blockedReason := ""
		for _, depID := range s.deps[id] {
			if s.status[depID] != "succeeded" {
				ok = false
				blockedReason = fmt.Sprintf("blocked by %s (%s)", depID, s.status[depID])
				break
			}
		}
		if !ok {
			s.setBlocked(id, blockedReason)
			continue
		}
		s.status[id] = "running"
		return s.nodes[id]
	}
	return nil
}

func (s *scheduler) TakeNewlyReady() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.newlyReady) == 0 {
		return nil
	}
	out := append([]string(nil), s.newlyReady...)
	s.newlyReady = s.newlyReady[:0]
	return out
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
			s.newlyReady = append(s.newlyReady, depID)
		}
	}
	s.sortReady()
}

func (s *scheduler) SeedSucceeded(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status[id] == "succeeded" {
		return
	}
	// Allow seeding from "planned" (fresh run) or "running" (defensive), but never
	// override failed/blocked states.
	switch s.status[id] {
	case "planned", "running":
	default:
		return
	}
	s.status[id] = "succeeded"
	for _, depID := range s.dependents[id] {
		if s.inDegree[depID] > 0 {
			s.inDegree[depID]--
		}
		if s.inDegree[depID] == 0 && s.status[depID] == "planned" {
			s.ready = append(s.ready, depID)
			s.newlyReady = append(s.newlyReady, depID)
		}
	}
	s.sortReady()
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
			s.newlyReady = append(s.newlyReady, depID)
		}
	}
	s.sortReady()
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
				s.setBlocked(id, fmt.Sprintf("blocked by %s (%s)", depID, s.status[depID]))
				break
			}
		}
	}
}

func (s *scheduler) TakeNewlyBlocked() map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.newlyBlocked) == 0 {
		return nil
	}
	out := make(map[string]string, len(s.newlyBlocked))
	for _, id := range s.newlyBlocked {
		out[id] = s.blockedBy[id]
	}
	s.newlyBlocked = nil
	return out
}

func (s *scheduler) setBlocked(id string, reason string) {
	if s.status[id] == "blocked" {
		return
	}
	if s.status[id] != "planned" {
		return
	}
	s.status[id] = "blocked"
	s.blockedBy[id] = reason
	s.newlyBlocked = append(s.newlyBlocked, id)
	sort.Strings(s.newlyBlocked)
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
