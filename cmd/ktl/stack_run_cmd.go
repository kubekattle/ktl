// File: cmd/ktl/stack_run_cmd.go
// Brief: `ktl stack apply/delete` command wiring (runner lives in internal/stack).

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/example/ktl/internal/stack"
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
			if resolvedSelector, resolvedOutput, ok := resolveStackRunDefaults(cmd, common, kind, &opts); ok {
				runSelector = resolvedSelector
				planOutput = resolvedOutput
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
				return stack.Run(cmd.Context(), runOpts, cmd.OutOrStdout(), cmd.ErrOrStderr())
			} else {
				_, pp, effective, err := compileInferSelect(cmd, common)
				if err != nil {
					return err
				}
				p = pp
				runSelector = stack.RunSelector{
					Clusters:             effective.Clusters,
					Tags:                 effective.Selector.Tags,
					FromPaths:            effective.Selector.FromPaths,
					Releases:             effective.Selector.Releases,
					GitRange:             strings.TrimSpace(effective.Selector.GitRange),
					GitIncludeDeps:       effective.Selector.GitIncludeDeps,
					GitIncludeDependents: effective.Selector.GitIncludeDependents,
					IncludeDeps:          effective.Selector.IncludeDeps,
					IncludeDependents:    effective.Selector.IncludeDependents,
					AllowMissingDeps:     effective.Selector.AllowMissingDeps,
				}
				planOutput = strings.ToLower(strings.TrimSpace(effective.Output))
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
			return stack.Run(cmd.Context(), runOpts, cmd.OutOrStdout(), cmd.ErrOrStderr())
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
}

func buildRunOptions(kind stackRunKind, common stackCommandCommon, plan *stack.Plan, opts stackRunCLIOptions, effective stack.RunnerResolved, adaptive *stack.AdaptiveConcurrencyOptions) stack.RunOptions {
	failFast := opts.FailFast && !opts.ContinueOnError
	return stack.RunOptions{
		Command:                    string(kind),
		Plan:                       plan,
		Concurrency:                effective.Concurrency,
		ProgressiveConcurrency:     effective.ProgressiveConcurrency,
		FailFast:                   failFast,
		AutoApprove:                opts.Yes,
		DryRun:                     kind == stackRunApply && opts.DryRun,
		Diff:                       kind == stackRunApply && opts.Diff,
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

func resolveStackRunDefaults(cmd *cobra.Command, common stackCommandCommon, kind stackRunKind, opts *stackRunCLIOptions) (selector stack.RunSelector, output string, ok bool) {
	effective, err := resolveStackEffectiveConfig(cmd, common)
	if err != nil {
		return stack.RunSelector{}, "", false
	}
	u, err := stack.Discover(effective.RootDir)
	if err != nil {
		return stack.RunSelector{}, "", false
	}
	profile := strings.TrimSpace(effective.Profile)
	if profile == "" {
		profile = strings.TrimSpace(u.DefaultProfile)
	}
	cfg, err := stack.ResolveStackCLIConfig(u, profile)
	if err != nil {
		return stack.RunSelector{}, "", false
	}

	// Defaults for selection + output.
	clusters, sel, err := resolveSelectorDefaults(cmd, common, cfg)
	if err != nil {
		return stack.RunSelector{}, "", false
	}
	selector = stack.RunSelector{
		Clusters:             clusters,
		Tags:                 sel.Tags,
		FromPaths:            sel.FromPaths,
		Releases:             sel.Releases,
		GitRange:             strings.TrimSpace(sel.GitRange),
		GitIncludeDeps:       sel.GitIncludeDeps,
		GitIncludeDependents: sel.GitIncludeDependents,
		IncludeDeps:          sel.IncludeDeps,
		IncludeDependents:    sel.IncludeDependents,
		AllowMissingDeps:     sel.AllowMissingDeps,
	}
	output = resolveOutputDefault(cmd, common, cfg)

	// Defaults for run behavior (env overrides stack.yaml when flag is not set).
	if kind == stackRunApply && !cmd.Flags().Changed("dry-run") {
		if v, ok, _ := envBool("KTL_STACK_APPLY_DRY_RUN"); ok {
			opts.DryRun = v
		} else if cfg.ApplyDryRun != nil {
			opts.DryRun = *cfg.ApplyDryRun
		}
	}
	if kind == stackRunApply && !cmd.Flags().Changed("diff") {
		if v, ok, _ := envBool("KTL_STACK_APPLY_DIFF"); ok {
			opts.Diff = v
		} else if cfg.ApplyDiff != nil {
			opts.Diff = *cfg.ApplyDiff
		}
	}
	if kind == stackRunDelete && !cmd.Flags().Changed("delete-confirm-threshold") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_DELETE_CONFIRM_THRESHOLD")); v != "" {
			if n, err := strconv.Atoi(v); err == nil {
				opts.DeleteConfirmThreshold = n
			}
		} else if cfg.DeleteConfirmThreshold != nil {
			opts.DeleteConfirmThreshold = *cfg.DeleteConfirmThreshold
		}
	}
	if !cmd.Flags().Changed("allow-drift") {
		if v, ok, _ := envBool("KTL_STACK_RESUME_ALLOW_DRIFT"); ok {
			opts.AllowDrift = v
		} else if cfg.ResumeAllowDrift != nil {
			opts.AllowDrift = *cfg.ResumeAllowDrift
		}
	}
	if !cmd.Flags().Changed("rerun-failed") {
		if v, ok, _ := envBool("KTL_STACK_RESUME_RERUN_FAILED"); ok {
			opts.RerunFailed = v
		} else if cfg.ResumeRerunFailed != nil {
			opts.RerunFailed = *cfg.ResumeRerunFailed
		}
	}

	return selector, output, true
}
