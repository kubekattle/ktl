package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/kubekattle/ktl/internal/stack"
	"github.com/spf13/cobra"
)

type stackRunDefaultOverrides struct {
	FailFast *bool
	Retry    *int

	Lock      *bool
	Takeover  *bool
	LockTTL   *time.Duration
	LockOwner *string

	DryRun *bool
	Diff   *bool

	DeleteConfirmThreshold *int
}

type stackResumeDefaultOverrides struct {
	AllowDrift  *bool
	RerunFailed *bool
}

type stackCommandConfig struct {
	RootDir  string
	Profile  string
	Universe *stack.Universe

	StackCLI stack.StackCLIResolved

	Clusters []string
	Selector stack.Selector

	Output          string
	InferDeps       bool
	InferConfigRefs bool

	ApplyDefaults  stackRunDefaultOverrides
	DeleteDefaults stackRunDefaultOverrides
	ResumeDefaults stackResumeDefaultOverrides

	Warnings []string
}

func applyStackRunDefaults(cmd *cobra.Command, kind stackRunKind, cfg stackCommandConfig, opts *stackRunCLIOptions) {
	if cmd == nil || opts == nil {
		return
	}
	var runCfg stackRunDefaultOverrides
	switch kind {
	case stackRunApply:
		runCfg = cfg.ApplyDefaults
	case stackRunDelete:
		runCfg = cfg.DeleteDefaults
	default:
		return
	}

	if !flagChanged(cmd, "fail-fast") && !flagChanged(cmd, "continue-on-error") && runCfg.FailFast != nil {
		opts.FailFast = *runCfg.FailFast
	}
	if !flagChanged(cmd, "retry") && runCfg.Retry != nil {
		opts.Retry = *runCfg.Retry
	}
	if !flagChanged(cmd, "lock") && runCfg.Lock != nil {
		opts.Lock = *runCfg.Lock
	}
	if !flagChanged(cmd, "takeover") && runCfg.Takeover != nil {
		opts.Takeover = *runCfg.Takeover
	}
	if !flagChanged(cmd, "lock-ttl") && runCfg.LockTTL != nil {
		opts.LockTTL = *runCfg.LockTTL
	}
	if !flagChanged(cmd, "lock-owner") && runCfg.LockOwner != nil {
		opts.LockOwner = *runCfg.LockOwner
	}

	if kind == stackRunApply {
		if !flagChanged(cmd, "dry-run") && runCfg.DryRun != nil {
			opts.DryRun = *runCfg.DryRun
		}
		if !flagChanged(cmd, "diff") && runCfg.Diff != nil {
			opts.Diff = *runCfg.Diff
		}
	}
	if kind == stackRunDelete {
		if !flagChanged(cmd, "delete-confirm-threshold") && runCfg.DeleteConfirmThreshold != nil {
			opts.DeleteConfirmThreshold = *runCfg.DeleteConfirmThreshold
		}
	}

	if !flagChanged(cmd, "allow-drift") && cfg.ResumeDefaults.AllowDrift != nil {
		opts.AllowDrift = *cfg.ResumeDefaults.AllowDrift
	}
	if !flagChanged(cmd, "rerun-failed") && cfg.ResumeDefaults.RerunFailed != nil {
		opts.RerunFailed = *cfg.ResumeDefaults.RerunFailed
	}
}

func resolveStackCommandConfig(cmd *cobra.Command, common stackCommandCommon) (stackCommandConfig, error) {
	root := strings.TrimSpace(derefString(common.rootDir))
	if cmd != nil && !flagChanged(cmd, "root") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_ROOT")); v != "" {
			root = v
		}
	}
	if root == "" {
		root = "."
	}

	profile := strings.TrimSpace(derefString(common.profile))
	if cmd != nil && !flagChanged(cmd, "profile") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_PROFILE")); v != "" {
			profile = v
		}
	}

	u, err := stack.Discover(root)
	if err != nil {
		return stackCommandConfig{}, err
	}
	if strings.TrimSpace(profile) == "" {
		profile = strings.TrimSpace(u.DefaultProfile)
	}

	stackCLI, err := stack.ResolveStackCLIConfig(u, profile)
	if err != nil {
		return stackCommandConfig{}, err
	}

	cfg := stackCommandConfig{
		RootDir:  root,
		Profile:  profile,
		Universe: u,
		StackCLI: stackCLI,

		InferDeps:       stackCLI.InferDeps,
		InferConfigRefs: stackCLI.InferConfigRefs,
		Selector:        stackCLI.Selector,
		Clusters:        append([]string(nil), stackCLI.Clusters...),
		Output:          strings.ToLower(strings.TrimSpace(stackCLI.Output)),
	}

	inferDeps, inferConfigRefs, err := resolveInferDefaults(cmd, common, stackCLI)
	if err != nil {
		return stackCommandConfig{}, err
	}
	cfg.InferDeps = inferDeps
	cfg.InferConfigRefs = inferConfigRefs

	clusters, selector, warnings, err := resolveSelectorDefaults(cmd, common, stackCLI)
	if err != nil {
		return stackCommandConfig{}, err
	}
	cfg.Clusters = clusters
	cfg.Selector = selector
	cfg.Warnings = append(cfg.Warnings, warnings...)

	cfg.Output, err = resolveOutputDefault(cmd, common, stackCLI)
	if err != nil {
		return stackCommandConfig{}, err
	}

	applyDefaults, deleteDefaults, resumeDefaults, err := resolveRunDefaults(cmd, stackCLI)
	if err != nil {
		return stackCommandConfig{}, err
	}
	cfg.ApplyDefaults = applyDefaults
	cfg.DeleteDefaults = deleteDefaults
	cfg.ResumeDefaults = resumeDefaults

	if strings.TrimSpace(cfg.Selector.GitRange) == "" && (cfg.Selector.GitIncludeDeps || cfg.Selector.GitIncludeDependents) {
		return stackCommandConfig{}, fmt.Errorf("invalid selector: gitIncludeDeps/gitIncludeDependents requires selector.gitRange (set cli.selector.gitRange or KTL_STACK_GIT_RANGE)")
	}

	return cfg, nil
}

func printStackConfigWarnings(cmd *cobra.Command, warnings []string) {
	if cmd == nil || len(warnings) == 0 {
		return
	}
	for _, w := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "warning: %s\n", w)
	}
}

func isNoStackRootError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "no stack.yaml found at stack root")
}

func resolveInferDefaults(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) (inferDeps bool, inferConfigRefs bool, err error) {
	inferDeps = cfg.InferDeps
	inferConfigRefs = cfg.InferConfigRefs

	if flagChanged(cmd, "infer-deps") {
		inferDeps = common.inferDeps != nil && *common.inferDeps
	} else if v, ok, err := envBool("KTL_STACK_INFER_DEPS"); err != nil {
		return false, false, err
	} else if ok {
		inferDeps = v
	}

	if flagChanged(cmd, "infer-config-refs") {
		inferConfigRefs = common.inferConfigRefs != nil && *common.inferConfigRefs
	} else if v, ok, err := envBool("KTL_STACK_INFER_CONFIG_REFS"); err != nil {
		return false, false, err
	} else if ok {
		inferConfigRefs = v
	}

	return inferDeps, inferConfigRefs, nil
}

func resolveOutputDefault(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) (string, error) {
	out := strings.ToLower(strings.TrimSpace(derefString(common.output)))
	if flagChanged(cmd, "output") {
		return validateStackOutput(out)
	}
	if v := strings.TrimSpace(os.Getenv("KTL_STACK_OUTPUT")); v != "" {
		return validateStackOutput(strings.ToLower(v))
	}
	if strings.TrimSpace(cfg.Output) != "" {
		return validateStackOutput(strings.ToLower(strings.TrimSpace(cfg.Output)))
	}
	return validateStackOutput(out)
}

func validateStackOutput(out string) (string, error) {
	switch out {
	case "", "table":
		return "table", nil
	case "json":
		return "json", nil
	default:
		return "", fmt.Errorf("unknown output %q (expected table|json)", out)
	}
}

func resolveSelectorDefaults(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) ([]string, stack.Selector, []string, error) {
	var warnings []string

	clusters := resolveStringSliceDefault(cmd, "cluster", splitCSV(derefStringSlice(common.clusters)), "KTL_STACK_CLUSTER", cfg.Clusters)
	selector := cfg.Selector

	selector.Tags = resolveStringSliceDefault(cmd, "tag", splitCSV(derefStringSlice(common.tags)), "KTL_STACK_TAG", cfg.Selector.Tags)
	selector.FromPaths = resolveStringSliceDefault(cmd, "from-path", splitCSV(derefStringSlice(common.fromPaths)), "KTL_STACK_FROM_PATH", cfg.Selector.FromPaths)
	selector.Releases = resolveStringSliceDefault(cmd, "release", splitCSV(derefStringSlice(common.releases)), "KTL_STACK_RELEASE", cfg.Selector.Releases)

	if flagChanged(cmd, "git-range") {
		selector.GitRange = strings.TrimSpace(derefString(common.gitRange))
	} else if v := strings.TrimSpace(os.Getenv("KTL_STACK_GIT_RANGE")); v != "" {
		if strings.TrimSpace(cfg.Selector.GitRange) != "" {
			warnings = append(warnings, "both stack.yaml cli.selector.gitRange and KTL_STACK_GIT_RANGE are set; using KTL_STACK_GIT_RANGE")
		}
		selector.GitRange = v
	} else {
		selector.GitRange = strings.TrimSpace(cfg.Selector.GitRange)
	}

	selector.GitIncludeDeps = resolveBoolDefault(cmd, "git-include-deps", common.gitIncludeDeps, "KTL_STACK_GIT_INCLUDE_DEPS", cfg.Selector.GitIncludeDeps)
	selector.GitIncludeDependents = resolveBoolDefault(cmd, "git-include-dependents", common.gitIncludeDependents, "KTL_STACK_GIT_INCLUDE_DEPENDENTS", cfg.Selector.GitIncludeDependents)
	selector.IncludeDeps = resolveBoolDefault(cmd, "include-deps", common.includeDeps, "KTL_STACK_INCLUDE_DEPS", cfg.Selector.IncludeDeps)
	selector.IncludeDependents = resolveBoolDefault(cmd, "include-dependents", common.includeDependents, "KTL_STACK_INCLUDE_DEPENDENTS", cfg.Selector.IncludeDependents)
	selector.AllowMissingDeps = resolveBoolDefault(cmd, "allow-missing-deps", common.allowMissingDeps, "KTL_STACK_ALLOW_MISSING_DEPS", cfg.Selector.AllowMissingDeps)

	return clusters, selector, warnings, nil
}

func resolveStringSliceDefault(cmd *cobra.Command, flagName string, flagValue []string, envName string, yamlValue []string) []string {
	if flagChanged(cmd, flagName) {
		return flagValue
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return splitCSV([]string{v})
	}
	if len(yamlValue) > 0 {
		return splitCSV(yamlValue)
	}
	return flagValue
}

func resolveBoolDefault(cmd *cobra.Command, flagName string, flagPtr *bool, envName string, yamlValue bool) bool {
	if flagChanged(cmd, flagName) && flagPtr != nil {
		return *flagPtr
	}
	if v, ok, err := envBool(envName); err == nil && ok {
		return v
	}
	return yamlValue
}

func resolveRunDefaults(cmd *cobra.Command, cfg stack.StackCLIResolved) (apply stackRunDefaultOverrides, del stackRunDefaultOverrides, resume stackResumeDefaultOverrides, err error) {
	apply = stackRunDefaultOverrides{}
	del = stackRunDefaultOverrides{}
	resume = stackResumeDefaultOverrides{}

	if !flagChanged(cmd, "dry-run") {
		if v, ok, err := envBool("KTL_STACK_APPLY_DRY_RUN"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.DryRun = &v
		} else if cfg.ApplyDryRun != nil {
			apply.DryRun = cfg.ApplyDryRun
		}
	}
	if !flagChanged(cmd, "diff") {
		if v, ok, err := envBool("KTL_STACK_APPLY_DIFF"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.Diff = &v
		} else if cfg.ApplyDiff != nil {
			apply.Diff = cfg.ApplyDiff
		}
	}

	if !flagChanged(cmd, "fail-fast") && !flagChanged(cmd, "continue-on-error") {
		if v, ok, err := envBool("KTL_STACK_APPLY_FAIL_FAST"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.FailFast = &v
		} else if cfg.ApplyFailFast != nil {
			apply.FailFast = cfg.ApplyFailFast
		}

		if v, ok, err := envBool("KTL_STACK_DELETE_FAIL_FAST"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.FailFast = &v
		} else if cfg.DeleteFailFast != nil {
			del.FailFast = cfg.DeleteFailFast
		}
	}

	if !flagChanged(cmd, "retry") {
		if v, ok, err := envInt("KTL_STACK_APPLY_RETRY"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.Retry = &v
		} else if cfg.ApplyRetry != nil {
			apply.Retry = cfg.ApplyRetry
		}

		if v, ok, err := envInt("KTL_STACK_DELETE_RETRY"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.Retry = &v
		} else if cfg.DeleteRetry != nil {
			del.Retry = cfg.DeleteRetry
		}
	}

	if !flagChanged(cmd, "lock") {
		if v, ok, err := envBool("KTL_STACK_APPLY_LOCK"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.Lock = &v
		} else if cfg.ApplyLock != nil {
			apply.Lock = cfg.ApplyLock
		}

		if v, ok, err := envBool("KTL_STACK_DELETE_LOCK"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.Lock = &v
		} else if cfg.DeleteLock != nil {
			del.Lock = cfg.DeleteLock
		}
	}

	if !flagChanged(cmd, "takeover") {
		if v, ok, err := envBool("KTL_STACK_APPLY_TAKEOVER"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.Takeover = &v
		} else if cfg.ApplyTakeover != nil {
			apply.Takeover = cfg.ApplyTakeover
		}

		if v, ok, err := envBool("KTL_STACK_DELETE_TAKEOVER"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.Takeover = &v
		} else if cfg.DeleteTakeover != nil {
			del.Takeover = cfg.DeleteTakeover
		}
	}

	if !flagChanged(cmd, "lock-ttl") {
		if v, ok, err := envDuration("KTL_STACK_APPLY_LOCK_TTL"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			apply.LockTTL = &v
		} else if cfg.ApplyLockTTL != nil {
			apply.LockTTL = cfg.ApplyLockTTL
		}

		if v, ok, err := envDuration("KTL_STACK_DELETE_LOCK_TTL"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.LockTTL = &v
		} else if cfg.DeleteLockTTL != nil {
			del.LockTTL = cfg.DeleteLockTTL
		}
	}

	if !flagChanged(cmd, "lock-owner") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_APPLY_LOCK_OWNER")); v != "" {
			apply.LockOwner = &v
		} else if cfg.ApplyLockOwner != nil && strings.TrimSpace(*cfg.ApplyLockOwner) != "" {
			apply.LockOwner = cfg.ApplyLockOwner
		}

		if v := strings.TrimSpace(os.Getenv("KTL_STACK_DELETE_LOCK_OWNER")); v != "" {
			del.LockOwner = &v
		} else if cfg.DeleteLockOwner != nil && strings.TrimSpace(*cfg.DeleteLockOwner) != "" {
			del.LockOwner = cfg.DeleteLockOwner
		}
	}

	if !flagChanged(cmd, "delete-confirm-threshold") {
		if v, ok, err := envInt("KTL_STACK_DELETE_CONFIRM_THRESHOLD"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			del.DeleteConfirmThreshold = &v
		} else if cfg.DeleteConfirmThreshold != nil {
			del.DeleteConfirmThreshold = cfg.DeleteConfirmThreshold
		}
	}

	if !flagChanged(cmd, "allow-drift") {
		if v, ok, err := envBool("KTL_STACK_RESUME_ALLOW_DRIFT"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			resume.AllowDrift = &v
		} else if cfg.ResumeAllowDrift != nil {
			resume.AllowDrift = cfg.ResumeAllowDrift
		}
	}
	if !flagChanged(cmd, "rerun-failed") {
		if v, ok, err := envBool("KTL_STACK_RESUME_RERUN_FAILED"); err != nil {
			return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, err
		} else if ok {
			resume.RerunFailed = &v
		} else if cfg.ResumeRerunFailed != nil {
			resume.RerunFailed = cfg.ResumeRerunFailed
		}
	}

	if apply.Retry != nil && *apply.Retry < 0 {
		return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, fmt.Errorf("invalid apply retry %d (expected >= 0)", *apply.Retry)
	}
	if del.Retry != nil && *del.Retry < 0 {
		return stackRunDefaultOverrides{}, stackRunDefaultOverrides{}, stackResumeDefaultOverrides{}, fmt.Errorf("invalid delete retry %d (expected >= 0)", *del.Retry)
	}

	return apply, del, resume, nil
}

func envInt(name string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0, true, fmt.Errorf("invalid %s %q (expected integer)", name, raw)
	}
	return n, true, nil
}

func envDuration(name string) (time.Duration, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	d, err := time.ParseDuration(raw)
	if err != nil {
		return 0, true, fmt.Errorf("invalid %s %q (expected duration like 30s, 5m, 1h)", name, raw)
	}
	return d, true, nil
}

func derefStringSlice(ptr *[]string) []string {
	if ptr == nil {
		return nil
	}
	return *ptr
}

func envBool(name string) (bool, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "t", "yes", "y", "on":
		return true, true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, true, nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n != 0, true, nil
	}
	return false, true, fmt.Errorf("invalid %s %q (expected boolean)", name, raw)
}
