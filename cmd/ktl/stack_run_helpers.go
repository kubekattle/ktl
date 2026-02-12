package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/kubekattle/ktl/internal/stack"
	"github.com/spf13/cobra"
)

const (
	stackFlagConcurrency             = "concurrency"
	stackFlagProgressiveConcurrency  = "progressive-concurrency"
	stackFlagKubeQPS                 = "kube-qps"
	stackFlagKubeBurst               = "kube-burst"
	stackFlagMaxParallelPerNamespace = "max-parallel-per-namespace"
	stackFlagMaxParallelKind         = "max-parallel-kind"
	stackFlagParallelismGroupLimit   = "parallelism-group-limit"
	stackFlagAdaptiveMin             = "adaptive-min"
	stackFlagAdaptiveWindow          = "adaptive-window"
	stackFlagAdaptiveRampSuccesses   = "adaptive-ramp-successes"
	stackFlagAdaptiveRampFailureRate = "adaptive-ramp-max-failure-rate"
	stackFlagAdaptiveCooldownSevere  = "adaptive-cooldown-severe"
)

func parseMaxParallelKind(args []string) (map[string]int, error) {
	out := map[string]int{}
	for _, raw := range args {
		for _, part := range strings.Split(raw, ",") {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			kv := strings.SplitN(part, "=", 2)
			if len(kv) != 2 {
				return nil, fmt.Errorf("invalid --max-parallel-kind entry %q (expected Kind=N)", part)
			}
			kind := strings.TrimSpace(kv[0])
			if kind == "" {
				return nil, fmt.Errorf("invalid --max-parallel-kind entry %q (empty kind)", part)
			}
			n, err := strconv.Atoi(strings.TrimSpace(kv[1]))
			if err != nil || n <= 0 {
				return nil, fmt.Errorf("invalid --max-parallel-kind entry %q (expected Kind=N where N>=1)", part)
			}
			out[kind] = n
		}
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

type stackRunnerOverrides struct {
	Concurrency             int
	ProgressiveConcurrency  bool
	KubeQPS                 float32
	KubeBurst               int
	MaxParallelPerNamespace int
	MaxParallelKind         []string
	ParallelismGroupLimit   int

	AdaptiveMin             int
	AdaptiveWindow          int
	AdaptiveRampSuccesses   int
	AdaptiveRampFailureRate float64
	AdaptiveCooldownSevere  int
}

func resolveRunnerFromFlags(cmd *cobra.Command, base stack.RunnerResolved, overrides stackRunnerOverrides) (stack.RunnerResolved, *stack.AdaptiveConcurrencyOptions, error) {
	effective := base
	if cmd.Flags().Changed(stackFlagConcurrency) {
		effective.Concurrency = overrides.Concurrency
	}
	if cmd.Flags().Changed(stackFlagProgressiveConcurrency) {
		effective.ProgressiveConcurrency = overrides.ProgressiveConcurrency
	}
	if cmd.Flags().Changed(stackFlagKubeQPS) {
		effective.KubeQPS = overrides.KubeQPS
	}
	if cmd.Flags().Changed(stackFlagKubeBurst) {
		effective.KubeBurst = overrides.KubeBurst
	}
	if cmd.Flags().Changed(stackFlagMaxParallelPerNamespace) {
		effective.Limits.MaxParallelPerNamespace = overrides.MaxParallelPerNamespace
	}
	if cmd.Flags().Changed(stackFlagMaxParallelKind) {
		m, err := parseMaxParallelKind(overrides.MaxParallelKind)
		if err != nil {
			return stack.RunnerResolved{}, nil, err
		}
		effective.Limits.MaxParallelKind = m
	}
	if cmd.Flags().Changed(stackFlagParallelismGroupLimit) {
		effective.Limits.ParallelismGroupLimit = overrides.ParallelismGroupLimit
	}
	if cmd.Flags().Changed(stackFlagAdaptiveMin) {
		effective.Adaptive.Min = overrides.AdaptiveMin
	}
	if cmd.Flags().Changed(stackFlagAdaptiveWindow) {
		effective.Adaptive.Window = overrides.AdaptiveWindow
	}
	if cmd.Flags().Changed(stackFlagAdaptiveRampSuccesses) {
		effective.Adaptive.RampAfterSuccesses = overrides.AdaptiveRampSuccesses
	}
	if cmd.Flags().Changed(stackFlagAdaptiveRampFailureRate) {
		effective.Adaptive.RampMaxFailureRate = overrides.AdaptiveRampFailureRate
	}
	if cmd.Flags().Changed(stackFlagAdaptiveCooldownSevere) {
		effective.Adaptive.CooldownSevere = overrides.AdaptiveCooldownSevere
	}
	if err := stack.ValidateRunnerResolved(effective); err != nil {
		return stack.RunnerResolved{}, nil, err
	}
	return effective, adaptiveConcurrencyOptions(effective), nil
}

func adaptiveConcurrencyOptions(r stack.RunnerResolved) *stack.AdaptiveConcurrencyOptions {
	if !r.ProgressiveConcurrency || r.Concurrency <= 1 {
		return nil
	}
	return &stack.AdaptiveConcurrencyOptions{
		Min:                r.Adaptive.Min,
		WindowSize:         r.Adaptive.Window,
		RampAfterSuccesses: r.Adaptive.RampAfterSuccesses,
		RampMaxFailureRate: r.Adaptive.RampMaxFailureRate,
		CooldownSuccessesByClass: map[string]int{
			"RATE_LIMIT":  r.Adaptive.CooldownSevere,
			"SERVER_5XX":  r.Adaptive.CooldownSevere,
			"UNAVAILABLE": r.Adaptive.CooldownSevere,
		},
	}
}

func chooseFailMode(failFast bool) string {
	if failFast {
		return "fail-fast"
	}
	return "continue"
}

func maxAttemptsFromRetry(retry int) int {
	if retry <= 0 {
		return 1
	}
	return retry
}
