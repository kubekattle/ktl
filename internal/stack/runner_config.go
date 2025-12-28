package stack

import (
	"fmt"
	"maps"
	"sort"
	"strings"
)

func ResolveRunnerConfig(u *Universe, profile string) (RunnerResolved, error) {
	// Defaults keep behavior stable when runner is omitted.
	base := RunnerResolved{
		Concurrency:            1,
		ProgressiveConcurrency: false,
		Limits: RunnerLimitsResolved{
			ParallelismGroupLimit: 1,
		},
		Adaptive: RunnerAdaptiveResolved{
			Mode:               "balanced",
			Min:                1,
			Window:             20,
			RampAfterSuccesses: 2,
			RampMaxFailureRate: 0.30,
			CooldownSevere:     4,
		},
	}
	if u == nil {
		return base, nil
	}
	root := u.RootDir
	sf, ok := u.Stacks[root]
	if !ok {
		return base, nil
	}
	merged := RunnerConfig{}
	mergeRunner(&merged, sf.Runner)
	if strings.TrimSpace(profile) != "" {
		if sp, ok := sf.Profiles[strings.TrimSpace(profile)]; ok {
			mergeRunner(&merged, sp.Runner)
		}
	}

	// Apply mode presets first (so explicit fields can override).
	mode := strings.ToLower(strings.TrimSpace(merged.Adaptive.Mode))
	if mode == "" {
		mode = base.Adaptive.Mode
	}
	switch mode {
	case "conservative":
		base.Adaptive.Mode = "conservative"
		base.Adaptive.Min = 1
		base.Adaptive.Window = 30
		base.Adaptive.RampAfterSuccesses = 3
		base.Adaptive.RampMaxFailureRate = 0.10
		base.Adaptive.CooldownSevere = 6
	case "aggressive":
		base.Adaptive.Mode = "aggressive"
		base.Adaptive.Min = 1
		base.Adaptive.Window = 12
		base.Adaptive.RampAfterSuccesses = 1
		base.Adaptive.RampMaxFailureRate = 0.40
		base.Adaptive.CooldownSevere = 2
	default:
		base.Adaptive.Mode = "balanced"
	}

	applyRunnerResolved(&base, merged)
	if err := ValidateRunnerResolved(base); err != nil {
		return RunnerResolved{}, err
	}
	return base, nil
}

func mergeRunner(dst *RunnerConfig, src RunnerConfig) {
	if dst == nil {
		return
	}
	if src.Concurrency != nil {
		dst.Concurrency = src.Concurrency
	}
	if src.ProgressiveConcurrency != nil {
		dst.ProgressiveConcurrency = src.ProgressiveConcurrency
	}
	if src.KubeQPS != nil {
		dst.KubeQPS = src.KubeQPS
	}
	if src.KubeBurst != nil {
		dst.KubeBurst = src.KubeBurst
	}
	if src.Limits.MaxParallelPerNamespace != nil {
		dst.Limits.MaxParallelPerNamespace = src.Limits.MaxParallelPerNamespace
	}
	if src.Limits.ParallelismGroupLimit != nil {
		dst.Limits.ParallelismGroupLimit = src.Limits.ParallelismGroupLimit
	}
	if src.Limits.MaxParallelKind != nil {
		if dst.Limits.MaxParallelKind == nil {
			dst.Limits.MaxParallelKind = map[string]int{}
		}
		maps.Copy(dst.Limits.MaxParallelKind, src.Limits.MaxParallelKind)
	}
	if strings.TrimSpace(src.Adaptive.Mode) != "" {
		dst.Adaptive.Mode = src.Adaptive.Mode
	}
	if src.Adaptive.Min != nil {
		dst.Adaptive.Min = src.Adaptive.Min
	}
	if src.Adaptive.Window != nil {
		dst.Adaptive.Window = src.Adaptive.Window
	}
	if src.Adaptive.RampAfterSuccesses != nil {
		dst.Adaptive.RampAfterSuccesses = src.Adaptive.RampAfterSuccesses
	}
	if src.Adaptive.RampMaxFailureRate != nil {
		dst.Adaptive.RampMaxFailureRate = src.Adaptive.RampMaxFailureRate
	}
	if src.Adaptive.CooldownSevere != nil {
		dst.Adaptive.CooldownSevere = src.Adaptive.CooldownSevere
	}
}

func applyRunnerResolved(dst *RunnerResolved, cfg RunnerConfig) {
	if dst == nil {
		return
	}
	if cfg.Concurrency != nil {
		dst.Concurrency = *cfg.Concurrency
	}
	if cfg.ProgressiveConcurrency != nil {
		dst.ProgressiveConcurrency = *cfg.ProgressiveConcurrency
	}
	if cfg.KubeQPS != nil {
		dst.KubeQPS = *cfg.KubeQPS
	}
	if cfg.KubeBurst != nil {
		dst.KubeBurst = *cfg.KubeBurst
	}
	if cfg.Limits.MaxParallelPerNamespace != nil {
		dst.Limits.MaxParallelPerNamespace = *cfg.Limits.MaxParallelPerNamespace
	}
	if cfg.Limits.ParallelismGroupLimit != nil {
		dst.Limits.ParallelismGroupLimit = *cfg.Limits.ParallelismGroupLimit
	}
	if cfg.Limits.MaxParallelKind != nil {
		dst.Limits.MaxParallelKind = maps.Clone(cfg.Limits.MaxParallelKind)
	}
	if cfg.Adaptive.Min != nil {
		dst.Adaptive.Min = *cfg.Adaptive.Min
	}
	if cfg.Adaptive.Window != nil {
		dst.Adaptive.Window = *cfg.Adaptive.Window
	}
	if cfg.Adaptive.RampAfterSuccesses != nil {
		dst.Adaptive.RampAfterSuccesses = *cfg.Adaptive.RampAfterSuccesses
	}
	if cfg.Adaptive.RampMaxFailureRate != nil {
		dst.Adaptive.RampMaxFailureRate = *cfg.Adaptive.RampMaxFailureRate
	}
	if cfg.Adaptive.CooldownSevere != nil {
		dst.Adaptive.CooldownSevere = *cfg.Adaptive.CooldownSevere
	}
	if strings.TrimSpace(cfg.Adaptive.Mode) != "" {
		dst.Adaptive.Mode = strings.ToLower(strings.TrimSpace(cfg.Adaptive.Mode))
	}
}

func ValidateRunnerResolved(r RunnerResolved) error {
	if r.Concurrency < 1 {
		return fmt.Errorf("runner.concurrency must be >= 1 (got %d)", r.Concurrency)
	}
	if r.Limits.ParallelismGroupLimit < 1 {
		return fmt.Errorf("runner.limits.parallelismGroupLimit must be >= 1 (got %d)", r.Limits.ParallelismGroupLimit)
	}
	if r.Limits.MaxParallelPerNamespace < 0 {
		return fmt.Errorf("runner.limits.maxParallelPerNamespace must be >= 0 (got %d)", r.Limits.MaxParallelPerNamespace)
	}
	if r.Adaptive.Window < 4 {
		return fmt.Errorf("runner.adaptive.window must be >= 4 (got %d)", r.Adaptive.Window)
	}
	if r.Adaptive.Min < 1 {
		return fmt.Errorf("runner.adaptive.min must be >= 1 (got %d)", r.Adaptive.Min)
	}
	if r.Adaptive.Min > r.Concurrency {
		return fmt.Errorf("runner.adaptive.min must be <= runner.concurrency (%d > %d)", r.Adaptive.Min, r.Concurrency)
	}
	if r.Adaptive.RampAfterSuccesses < 1 {
		return fmt.Errorf("runner.adaptive.rampAfterSuccesses must be >= 1 (got %d)", r.Adaptive.RampAfterSuccesses)
	}
	if r.Adaptive.RampMaxFailureRate < 0 || r.Adaptive.RampMaxFailureRate > 1 {
		return fmt.Errorf("runner.adaptive.rampMaxFailureRate must be in [0,1] (got %.3f)", r.Adaptive.RampMaxFailureRate)
	}
	if r.Adaptive.CooldownSevere < 0 {
		return fmt.Errorf("runner.adaptive.cooldownSevere must be >= 0 (got %d)", r.Adaptive.CooldownSevere)
	}
	for kind, v := range r.Limits.MaxParallelKind {
		if strings.TrimSpace(kind) == "" {
			return fmt.Errorf("runner.limits.maxParallelKind has empty kind")
		}
		if v < 1 {
			return fmt.Errorf("runner.limits.maxParallelKind[%s] must be >= 1 (got %d)", kind, v)
		}
	}
	return nil
}

func runnerMaxParallelKindFlagString(m map[string]int) string {
	if len(m) == 0 {
		return ""
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var parts []string
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s=%d", k, m[k]))
	}
	return strings.Join(parts, ",")
}
