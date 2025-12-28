// File: cmd/ktl/stack_run_cmd.go
// Brief: `ktl stack apply/delete` command wiring (runner lives in internal/stack).

package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/stack"
	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/internal/ui"
	"github.com/spf13/cobra"
)

type stackCommandCommon struct {
	rootDir              *string
	profile              *string
	clusters             *[]string
	output               *string
	planOnly             *bool
	inferDeps            *bool
	inferConfigRefs      *bool
	tags                 *[]string
	fromPaths            *[]string
	releases             *[]string
	gitRange             *string
	gitIncludeDeps       *bool
	gitIncludeDependents *bool
	includeDeps          *bool
	includeDependents    *bool
	allowMissingDeps     *bool

	kubeconfig  *string
	kubeContext *string
	logLevel    *string
	remoteAgent *string
}

type stackRunKind string

const (
	stackRunApply  stackRunKind = "apply"
	stackRunDelete stackRunKind = "delete"
)

func newStackApplyCommand(common stackCommandCommon) *cobra.Command {
	return newStackRunCommand(stackRunApply, common)
}

func newStackDeleteCommand(common stackCommandCommon) *cobra.Command {
	return newStackRunCommand(stackRunDelete, common)
}

func newStackRunCommand(kind stackRunKind, common stackCommandCommon) *cobra.Command {
	opts := stackRunCLIOptions{
		FailFast:               true,
		Retry:                  1,
		RunnerKubeQPS:          0,
		RunnerKubeBurst:        0,
		DeleteConfirmThreshold: 20,
		Lock:                   true,
		LockTTL:                30 * time.Minute,
		VerifyBundle:           true,
	}

	short := "Apply the selected stack releases in DAG order"
	if kind == stackRunDelete {
		short = "Delete the selected stack releases in reverse DAG order"
	}

	cmd := &cobra.Command{
		Use:   string(kind),
		Short: short,
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			var p *stack.Plan
			var cleanup func()

			runSelector := buildRunSelector(common)
			planOutput := strings.ToLower(strings.TrimSpace(*common.output))
			cfg, cfgErr := resolveStackCommandConfig(cmd, common)
			if cfgErr != nil && !isNoStackRootError(cfgErr) {
				return cfgErr
			}
			if cfgErr == nil {
				printStackConfigWarnings(cmd, cfg.Warnings)
				applyStackRunDefaults(cmd, kind, cfg, &opts)
				runSelector = stack.RunSelector{
					Clusters:             cfg.Clusters,
					Tags:                 cfg.Selector.Tags,
					FromPaths:            cfg.Selector.FromPaths,
					Releases:             cfg.Selector.Releases,
					GitRange:             strings.TrimSpace(cfg.Selector.GitRange),
					GitIncludeDeps:       cfg.Selector.GitIncludeDeps,
					GitIncludeDependents: cfg.Selector.GitIncludeDependents,
					IncludeDeps:          cfg.Selector.IncludeDeps,
					IncludeDependents:    cfg.Selector.IncludeDependents,
					AllowMissingDeps:     cfg.Selector.AllowMissingDeps,
				}
				planOutput = cfg.Output
			}

			runWithViews := func(p *stack.Plan, runOpts stack.RunOptions) error {
				out := cmd.OutOrStdout()
				errOut := cmd.ErrOrStderr()

				outFormat := strings.ToLower(strings.TrimSpace(planOutput))
				verbose := strings.ToLower(strings.TrimSpace(derefString(common.logLevel))) == "debug"

				var observers []stack.RunEventObserver
				var encMu sync.Mutex

				var console *stack.RunConsole
				if outFormat == "json" {
					enc := json.NewEncoder(out)
					enc.SetEscapeHTML(false)
					observers = append(observers, stack.RunEventObserverFunc(func(ev stack.RunEvent) {
						encMu.Lock()
						defer encMu.Unlock()
						_ = enc.Encode(ev)
					}))
				} else if isTerminalWriter(errOut) {
					width, _ := ui.TerminalWidth(errOut)
					console = stack.NewRunConsole(errOut, p, string(kind), stack.RunConsoleOptions{
						Enabled:      true,
						Verbose:      verbose,
						Width:        width,
						Color:        true,
						ShowHelmLogs: strings.TrimSpace(opts.HelmLogs) != "" && strings.ToLower(strings.TrimSpace(opts.HelmLogs)) != "off",
						HelmLogsMode: strings.TrimSpace(opts.HelmLogs),
					})
					observers = append(observers, console)
				} else {
					observers = append(observers, stack.RunEventObserverFunc(func(ev stack.RunEvent) {
						encMu.Lock()
						defer encMu.Unlock()
						node := strings.TrimSpace(ev.NodeID)
						if node == "" {
							node = "-"
						}
						msg := strings.TrimSpace(ev.Message)
						if msg != "" {
							fmt.Fprintf(errOut, "%s\t%s\t%s\t%d\t%s\n", ev.TS, ev.Type, node, ev.Attempt, msg)
							return
						}
						fmt.Fprintf(errOut, "%s\t%s\t%s\t%d\n", ev.TS, ev.Type, node, ev.Attempt)
					}))
				}

				if addr := strings.TrimSpace(opts.WSListenAddr); addr != "" {
					logger, err := buildLogger(derefString(common.logLevel))
					if err != nil {
						return err
					}
					label := "ktl stack"
					if p != nil && strings.TrimSpace(p.StackName) != "" {
						label = fmt.Sprintf("ktl stack %s", strings.TrimSpace(p.StackName))
					}
					wsServer := caststream.New(addr, caststream.ModeWS, label, logger.WithName("stack-ws"))
					if err := castutil.StartCastServer(cmd.Context(), wsServer, "ktl stack websocket stream", logger.WithName("stack-ws"), errOut); err != nil {
						return err
					}
					observers = append(observers, stack.RunEventObserverFunc(func(ev stack.RunEvent) {
						raw, err := json.Marshal(ev)
						if err != nil {
							return
						}
						rec := tailer.LogRecord{
							Timestamp: time.Now().UTC(),
							Raw:       string(raw),
							Rendered:  string(raw),
							Source:    "stack",
						}
						wsServer.ObserveLog(rec)
					}))
					fmt.Fprintf(errOut, "Serving ktl websocket stack stream on %s\n", addr)
				}

				runOpts.EventObservers = append(runOpts.EventObservers, observers...)
				if console != nil {
					defer console.Done()
				}
				return stack.Run(cmd.Context(), runOpts, out, errOut)
			}

			if strings.TrimSpace(opts.SealedDir) != "" || strings.TrimSpace(opts.FromBundle) != "" {
				var trustedPub []byte
				if opts.RequireSigned && strings.TrimSpace(opts.BundlePub) != "" {
					_, pub, _, err := stack.LoadBundleKey(strings.TrimSpace(opts.BundlePub))
					if err != nil {
						return err
					}
					trustedPub = pub
				}

				pp, c, err := stack.LoadSealedPlan(cmd.Context(), stack.LoadSealedPlanOptions{
					StateStoreRoot: *common.rootDir,
					SealedDir:      strings.TrimSpace(opts.SealedDir),
					BundlePath:     strings.TrimSpace(opts.FromBundle),
					VerifyBundle:   opts.VerifyBundle,
					RequireSigned:  opts.RequireSigned,
					TrustedPubKey:  trustedPub,
				})
				if err != nil {
					return err
				}
				p = pp
				cleanup = c
				if cleanup != nil {
					defer cleanup()
				}
			} else if opts.Resume && !opts.Replan {
				resumeFromRunID := strings.TrimSpace(opts.RunID)
				if resumeFromRunID == "" {
					var err error
					resumeFromRunID, err = stack.LoadMostRecentRun(*common.rootDir)
					if err != nil {
						return err
					}
				}
				loaded, err := stack.LoadRun(*common.rootDir, resumeFromRunID)
				if err != nil {
					return err
				}
				p = loaded.Plan
				if !opts.AllowDrift {
					drift, err := stack.DriftReport(p)
					if err != nil {
						return err
					}
					if len(drift) > 0 {
						return fmt.Errorf("cannot resume: inputs changed (rerun with --allow-drift or --replan)\n%s", strings.Join(drift, "\n"))
					}
				}
				if opts.RerunFailed {
					p = stack.FilterByNodeStatus(p, loaded.StatusByID, []string{"failed"})
				}
				initialAttempts := loaded.AttemptByID

				effective, adaptiveOpts, err := resolveRunnerFromFlags(cmd, p.Runner, opts.runnerOverrides())
				if err != nil {
					return err
				}

				runOpts := buildRunOptions(kind, common, p, opts, effective, adaptiveOpts)
				runOpts.RunID = ""
				runOpts.ResumeStatusByID = loaded.StatusByID
				runOpts.ResumeAttemptByID = loaded.AttemptByID
				runOpts.ResumeFromRunID = resumeFromRunID
				runOpts.InitialAttempts = initialAttempts
				runOpts.Selector = runSelector
				return runWithViews(p, runOpts)
			} else {
				var pp *stack.Plan
				var err error
				if cfgErr == nil {
					_, pp, _, err = compileInferSelectWithConfig(cmd, common, cfg)
				} else {
					_, pp, _, err = compileInferSelect(cmd, common)
				}
				if err != nil {
					return err
				}
				p = pp
			}

			if common.planOnly != nil && *common.planOnly {
				switch strings.ToLower(strings.TrimSpace(planOutput)) {
				case "json":
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				default:
					return stack.PrintPlanTable(cmd.OutOrStdout(), p)
				}
			}

			if kind == stackRunDelete && !opts.Yes {
				if opts.DeleteConfirmThreshold <= 0 {
					opts.DeleteConfirmThreshold = 20
				}
				if len(p.Nodes) >= opts.DeleteConfirmThreshold {
					dec, err := approvalMode(cmd, false, false)
					if err != nil {
						return err
					}
					prompt := fmt.Sprintf("About to delete %d releases. Only 'yes' will be accepted:", len(p.Nodes))
					if err := confirmAction(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), dec, prompt, confirmModeYes, ""); err != nil {
						return err
					}
				}
			}

			effective, adaptiveOpts, err := resolveRunnerFromFlags(cmd, p.Runner, opts.runnerOverrides())
			if err != nil {
				return err
			}

			runOpts := buildRunOptions(kind, common, p, opts, effective, adaptiveOpts)
			runOpts.Selector = runSelector
			return runWithViews(p, runOpts)
		},
	}

	addStackRunFlags(cmd, kind, &opts)
	addStackRunnerFlags(cmd, &opts)
	addStackSealFlags(cmd, &opts)

	cmd.MarkFlagsMutuallyExclusive("fail-fast", "continue-on-error")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "from-bundle")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "resume")
	cmd.MarkFlagsMutuallyExclusive("from-bundle", "resume")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "replan")
	cmd.MarkFlagsMutuallyExclusive("from-bundle", "replan")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "require-signed")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "bundle-pub")
	cmd.MarkFlagsMutuallyExclusive("sealed-dir", "verify-bundle")

	return cmd
}

type stackRunCLIOptions struct {
	Concurrency            int
	ProgressiveConcurrency bool
	FailFast               bool
	ContinueOnError        bool
	Yes                    bool
	DryRun                 bool
	Diff                   bool
	HelmLogs               string
	Resume                 bool
	RunID                  string
	Replan                 bool
	AllowDrift             bool
	RerunFailed            bool
	Retry                  int

	RunnerKubeQPS                 float32
	RunnerKubeBurst               int
	RunnerMaxParallelPerNamespace int
	RunnerMaxParallelKind         []string
	RunnerParallelismGroupLimit   int
	RunnerAdaptiveMin             int
	RunnerAdaptiveWindow          int
	RunnerAdaptiveRampSuccesses   int
	RunnerAdaptiveRampFailureRate float64
	RunnerAdaptiveCooldownSevere  int

	Lock      bool
	Takeover  bool
	LockTTL   time.Duration
	LockOwner string

	SealedDir     string
	FromBundle    string
	VerifyBundle  bool
	RequireSigned bool
	BundlePub     string

	DeleteConfirmThreshold int

	WSListenAddr string
}

func (o stackRunCLIOptions) runnerOverrides() stackRunnerOverrides {
	return stackRunnerOverrides{
		Concurrency:             o.Concurrency,
		ProgressiveConcurrency:  o.ProgressiveConcurrency,
		KubeQPS:                 o.RunnerKubeQPS,
		KubeBurst:               o.RunnerKubeBurst,
		MaxParallelPerNamespace: o.RunnerMaxParallelPerNamespace,
		MaxParallelKind:         o.RunnerMaxParallelKind,
		ParallelismGroupLimit:   o.RunnerParallelismGroupLimit,
		AdaptiveMin:             o.RunnerAdaptiveMin,
		AdaptiveWindow:          o.RunnerAdaptiveWindow,
		AdaptiveRampSuccesses:   o.RunnerAdaptiveRampSuccesses,
		AdaptiveRampFailureRate: o.RunnerAdaptiveRampFailureRate,
		AdaptiveCooldownSevere:  o.RunnerAdaptiveCooldownSevere,
	}
}

func addStackRunFlags(cmd *cobra.Command, kind stackRunKind, opts *stackRunCLIOptions) {
	cmd.Flags().BoolVar(&opts.FailFast, "fail-fast", opts.FailFast, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&opts.ContinueOnError, "continue-on-error", opts.ContinueOnError, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&opts.Yes, "yes", opts.Yes, "Skip confirmation prompts")

	cmd.Flags().StringVar(&opts.HelmLogs, "helm-logs", opts.HelmLogs, "Helm log capture + TTY rendering mode: off|on|all (default off)")
	cmd.Flags().Lookup("helm-logs").NoOptDefVal = "on"

	cmd.Flags().BoolVar(&opts.Resume, "resume", opts.Resume, "Resume the most recent run (uses its frozen plan unless --replan is set)")
	cmd.Flags().StringVar(&opts.RunID, "run-id", opts.RunID, "With --resume: resume a specific run ID; otherwise: set the new run ID")
	cmd.Flags().BoolVar(&opts.Replan, "replan", opts.Replan, "Recompute the plan from current config when resuming")
	cmd.Flags().BoolVar(&opts.AllowDrift, "allow-drift", opts.AllowDrift, "Allow resume even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().BoolVar(&opts.RerunFailed, "rerun-failed", opts.RerunFailed, "When resuming, schedule only failed nodes")
	cmd.Flags().IntVar(&opts.Retry, "retry", opts.Retry, "Maximum attempts per release (includes the initial attempt)")

	cmd.Flags().IntVar(&opts.Concurrency, "concurrency", opts.Concurrency, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&opts.ProgressiveConcurrency, "progressive-concurrency", opts.ProgressiveConcurrency, "Start at 1 worker, then ramp up/down based on successes/failures")

	cmd.Flags().BoolVar(&opts.Lock, "lock", opts.Lock, "Acquire a stack state lock for this run")
	cmd.Flags().BoolVar(&opts.Takeover, "takeover", opts.Takeover, "Take over the stack state lock if held (unsafe)")
	cmd.Flags().DurationVar(&opts.LockTTL, "lock-ttl", opts.LockTTL, "How long before the lock is considered stale")
	cmd.Flags().StringVar(&opts.LockOwner, "lock-owner", opts.LockOwner, "Lock owner string (defaults to user@host:pid)")

	if kind == stackRunApply {
		cmd.Flags().BoolVar(&opts.DryRun, "dry-run", opts.DryRun, "Preview changes without applying them")
		cmd.Flags().BoolVar(&opts.Diff, "diff", opts.Diff, "Print a manifest diff during apply")
	}
	if kind == stackRunDelete {
		cmd.Flags().IntVar(&opts.DeleteConfirmThreshold, "delete-confirm-threshold", opts.DeleteConfirmThreshold, "Prompt when deleting at least this many releases (0 disables)")
	}
	cmd.Flags().Var(&validatedStringValue{dest: &opts.WSListenAddr, name: "--ws-listen", allowEmpty: true, validator: validateWSListenAddr}, "ws-listen", "Expose the stack run event stream over WebSocket at this address (e.g. :9090)")

	// Minimal-flag UX: keep knobs configurable via stack.yaml/env; hide overrides but keep them working.
	_ = cmd.Flags().MarkHidden("fail-fast")
	_ = cmd.Flags().MarkHidden("continue-on-error")
	_ = cmd.Flags().MarkHidden("retry")
	_ = cmd.Flags().MarkHidden("concurrency")
	_ = cmd.Flags().MarkHidden("progressive-concurrency")
	_ = cmd.Flags().MarkHidden("lock")
	_ = cmd.Flags().MarkHidden("takeover")
	_ = cmd.Flags().MarkHidden("lock-ttl")
	_ = cmd.Flags().MarkHidden("lock-owner")
	_ = cmd.Flags().MarkHidden("allow-drift")
	_ = cmd.Flags().MarkHidden("rerun-failed")
	if kind == stackRunApply {
		_ = cmd.Flags().MarkHidden("dry-run")
		_ = cmd.Flags().MarkHidden("diff")
	}
	if kind == stackRunDelete {
		_ = cmd.Flags().MarkHidden("delete-confirm-threshold")
	}
	_ = cmd.Flags().MarkHidden("ws-listen")
}

func addStackRunnerFlags(cmd *cobra.Command, opts *stackRunCLIOptions) {
	cmd.Flags().Float32Var(&opts.RunnerKubeQPS, "kube-qps", opts.RunnerKubeQPS, "Override Kubernetes client QPS for Helm operations (0 uses defaults)")
	cmd.Flags().IntVar(&opts.RunnerKubeBurst, "kube-burst", opts.RunnerKubeBurst, "Override Kubernetes client burst for Helm operations (0 uses defaults)")
	cmd.Flags().IntVar(&opts.RunnerMaxParallelPerNamespace, "max-parallel-per-namespace", opts.RunnerMaxParallelPerNamespace, "Limit concurrent releases per target namespace (0 disables)")
	cmd.Flags().StringSliceVar(&opts.RunnerMaxParallelKind, "max-parallel-kind", opts.RunnerMaxParallelKind, "Limit concurrent releases by inferred primary kind (repeatable, format Kind=N)")
	cmd.Flags().IntVar(&opts.RunnerParallelismGroupLimit, "parallelism-group-limit", opts.RunnerParallelismGroupLimit, "Concurrency limit for releases sharing the same parallelismGroup")
	cmd.Flags().IntVar(&opts.RunnerAdaptiveMin, "adaptive-min", 1, "Minimum worker target when using --progressive-concurrency")
	cmd.Flags().IntVar(&opts.RunnerAdaptiveWindow, "adaptive-window", 20, "Outcome window size for adaptive concurrency when using --progressive-concurrency")
	cmd.Flags().IntVar(&opts.RunnerAdaptiveRampSuccesses, "adaptive-ramp-successes", 2, "Successes required before increasing worker target when using --progressive-concurrency")
	cmd.Flags().Float64Var(&opts.RunnerAdaptiveRampFailureRate, "adaptive-ramp-max-failure-rate", 0.30, "Block ramp-up when recent failure rate exceeds this (0-1)")
	cmd.Flags().IntVar(&opts.RunnerAdaptiveCooldownSevere, "adaptive-cooldown-severe", 4, "Successes to wait before ramp-up after RATE_LIMIT/5xx when using --progressive-concurrency")

	_ = cmd.Flags().MarkHidden("kube-qps")
	_ = cmd.Flags().MarkHidden("kube-burst")
	_ = cmd.Flags().MarkHidden("max-parallel-per-namespace")
	_ = cmd.Flags().MarkHidden("max-parallel-kind")
	_ = cmd.Flags().MarkHidden("parallelism-group-limit")
	_ = cmd.Flags().MarkHidden("adaptive-min")
	_ = cmd.Flags().MarkHidden("adaptive-window")
	_ = cmd.Flags().MarkHidden("adaptive-ramp-successes")
	_ = cmd.Flags().MarkHidden("adaptive-ramp-max-failure-rate")
	_ = cmd.Flags().MarkHidden("adaptive-cooldown-severe")
}

func addStackSealFlags(cmd *cobra.Command, opts *stackRunCLIOptions) {
	cmd.Flags().StringVar(&opts.SealedDir, "sealed-dir", opts.SealedDir, "Run from a sealed plan/bundle directory (expects plan.json + inputs.tar.gz)")
	cmd.Flags().StringVar(&opts.FromBundle, "from-bundle", opts.FromBundle, "Run from a sealed plan bundle (.tgz)")
	cmd.Flags().BoolVar(&opts.VerifyBundle, "verify-bundle", opts.VerifyBundle, "Verify bundle manifest digests before running")
	cmd.Flags().BoolVar(&opts.RequireSigned, "require-signed", opts.RequireSigned, "Require a valid signature.json inside the bundle")
	cmd.Flags().StringVar(&opts.BundlePub, "bundle-pub", opts.BundlePub, "Optional trusted public key (ed25519 key JSON) when verifying a signed bundle")

	_ = cmd.Flags().MarkHidden("verify-bundle")
	_ = cmd.Flags().MarkHidden("require-signed")
	_ = cmd.Flags().MarkHidden("bundle-pub")
}

func buildRunOptions(kind stackRunKind, common stackCommandCommon, plan *stack.Plan, opts stackRunCLIOptions, effective stack.RunnerResolved, adaptive *stack.AdaptiveConcurrencyOptions) stack.RunOptions {
	failFast := opts.FailFast && !opts.ContinueOnError
	helmLogsMode := strings.ToLower(strings.TrimSpace(opts.HelmLogs))
	switch helmLogsMode {
	case "", "false", "0":
		helmLogsMode = "off"
	case "true", "1":
		helmLogsMode = "on"
	}
	return stack.RunOptions{
		Command:                    string(kind),
		Plan:                       plan,
		Concurrency:                effective.Concurrency,
		ProgressiveConcurrency:     effective.ProgressiveConcurrency,
		FailFast:                   failFast,
		AutoApprove:                opts.Yes,
		DryRun:                     kind == stackRunApply && opts.DryRun,
		Diff:                       kind == stackRunApply && opts.Diff,
		HelmLogs:                   helmLogsMode != "off",
		KubeQPS:                    effective.KubeQPS,
		KubeBurst:                  effective.KubeBurst,
		MaxConcurrencyPerNamespace: effective.Limits.MaxParallelPerNamespace,
		MaxConcurrencyByKind:       effective.Limits.MaxParallelKind,
		ParallelismGroupLimit:      effective.Limits.ParallelismGroupLimit,
		Adaptive:                   adaptive,
		Lock:                       opts.Lock,
		LockOwner:                  opts.LockOwner,
		LockTTL:                    opts.LockTTL,
		TakeoverLock:               opts.Takeover,
		Kubeconfig:                 common.kubeconfig,
		KubeContext:                common.kubeContext,
		LogLevel:                   common.logLevel,
		RemoteAgentAddr:            common.remoteAgent,
		RunID:                      strings.TrimSpace(opts.RunID),
		FailMode:                   chooseFailMode(failFast),
		MaxAttempts:                maxAttemptsFromRetry(opts.Retry),
		Selector:                   buildRunSelector(common),
	}
}

func buildRunSelector(common stackCommandCommon) stack.RunSelector {
	return stack.RunSelector{
		Clusters:             splitCSV(*common.clusters),
		Tags:                 splitCSV(*common.tags),
		FromPaths:            splitCSV(*common.fromPaths),
		Releases:             splitCSV(*common.releases),
		GitRange:             strings.TrimSpace(*common.gitRange),
		GitIncludeDeps:       *common.gitIncludeDeps,
		GitIncludeDependents: *common.gitIncludeDependents,
		IncludeDeps:          *common.includeDeps,
		IncludeDependents:    *common.includeDependents,
		AllowMissingDeps:     *common.allowMissingDeps,
	}
}
