package main

import (
	"testing"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func TestResolveRunnerFromFlags_NoChanges(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int(stackFlagConcurrency, 1, "")
	cmd.Flags().Bool(stackFlagProgressiveConcurrency, false, "")
	cmd.Flags().Float32(stackFlagKubeQPS, 0, "")
	cmd.Flags().Int(stackFlagKubeBurst, 0, "")
	cmd.Flags().Int(stackFlagMaxParallelPerNamespace, 0, "")
	cmd.Flags().StringSlice(stackFlagMaxParallelKind, nil, "")
	cmd.Flags().Int(stackFlagParallelismGroupLimit, 1, "")
	cmd.Flags().Int(stackFlagAdaptiveMin, 1, "")
	cmd.Flags().Int(stackFlagAdaptiveWindow, 20, "")
	cmd.Flags().Int(stackFlagAdaptiveRampSuccesses, 2, "")
	cmd.Flags().Float64(stackFlagAdaptiveRampFailureRate, 0.3, "")
	cmd.Flags().Int(stackFlagAdaptiveCooldownSevere, 4, "")

	base := stack.RunnerResolved{
		Concurrency:            3,
		ProgressiveConcurrency: true,
		KubeQPS:                123,
		KubeBurst:              7,
		Limits: stack.RunnerLimitsResolved{
			MaxParallelPerNamespace: 2,
			MaxParallelKind:         map[string]int{"Deployment": 1},
			ParallelismGroupLimit:   5,
		},
		Adaptive: stack.RunnerAdaptiveResolved{
			Min:                1,
			Window:             20,
			RampAfterSuccesses: 2,
			RampMaxFailureRate: 0.3,
			CooldownSevere:     4,
		},
	}

	got, adaptive, err := resolveRunnerFromFlags(cmd, base, stackRunnerOverrides{})
	if err != nil {
		t.Fatalf("resolveRunnerFromFlags: %v", err)
	}
	if got.Concurrency != base.Concurrency || got.KubeBurst != base.KubeBurst || got.Limits.ParallelismGroupLimit != base.Limits.ParallelismGroupLimit {
		t.Fatalf("unexpected runner mutation: %#v", got)
	}
	if adaptive == nil {
		t.Fatalf("expected adaptive options for progressive + concurrency>1")
	}
}

func TestResolveRunnerFromFlags_Overrides(t *testing.T) {
	t.Parallel()

	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().Int(stackFlagConcurrency, 1, "")
	cmd.Flags().Bool(stackFlagProgressiveConcurrency, false, "")
	cmd.Flags().Float32(stackFlagKubeQPS, 0, "")
	cmd.Flags().Int(stackFlagKubeBurst, 0, "")
	cmd.Flags().Int(stackFlagMaxParallelPerNamespace, 0, "")
	cmd.Flags().StringSlice(stackFlagMaxParallelKind, nil, "")
	cmd.Flags().Int(stackFlagParallelismGroupLimit, 1, "")
	cmd.Flags().Int(stackFlagAdaptiveMin, 1, "")
	cmd.Flags().Int(stackFlagAdaptiveWindow, 20, "")
	cmd.Flags().Int(stackFlagAdaptiveRampSuccesses, 2, "")
	cmd.Flags().Float64(stackFlagAdaptiveRampFailureRate, 0.3, "")
	cmd.Flags().Int(stackFlagAdaptiveCooldownSevere, 4, "")

	_ = cmd.Flags().Set(stackFlagConcurrency, "4")
	_ = cmd.Flags().Set(stackFlagProgressiveConcurrency, "true")
	_ = cmd.Flags().Set(stackFlagMaxParallelPerNamespace, "3")
	_ = cmd.Flags().Set(stackFlagMaxParallelKind, "Deployment=2,StatefulSet=1")
	_ = cmd.Flags().Set(stackFlagParallelismGroupLimit, "2")
	_ = cmd.Flags().Set(stackFlagAdaptiveMin, "2")

	base := stack.RunnerResolved{
		Concurrency:            1,
		ProgressiveConcurrency: false,
		Limits: stack.RunnerLimitsResolved{
			ParallelismGroupLimit: 1,
		},
		Adaptive: stack.RunnerAdaptiveResolved{
			Min:                1,
			Window:             20,
			RampAfterSuccesses: 2,
			RampMaxFailureRate: 0.3,
			CooldownSevere:     4,
		},
	}

	got, adaptive, err := resolveRunnerFromFlags(cmd, base, stackRunnerOverrides{
		Concurrency:             4,
		ProgressiveConcurrency:  true,
		MaxParallelPerNamespace: 3,
		MaxParallelKind:         []string{"Deployment=2,StatefulSet=1"},
		ParallelismGroupLimit:   2,
		AdaptiveMin:             2,
	})
	if err != nil {
		t.Fatalf("resolveRunnerFromFlags: %v", err)
	}
	if got.Concurrency != 4 || !got.ProgressiveConcurrency {
		t.Fatalf("unexpected runner: %#v", got)
	}
	if got.Limits.MaxParallelPerNamespace != 3 || got.Limits.ParallelismGroupLimit != 2 {
		t.Fatalf("unexpected limits: %#v", got.Limits)
	}
	if got.Limits.MaxParallelKind["Deployment"] != 2 || got.Limits.MaxParallelKind["StatefulSet"] != 1 {
		t.Fatalf("unexpected maxParallelKind: %#v", got.Limits.MaxParallelKind)
	}
	if adaptive == nil || adaptive.Min != 2 {
		t.Fatalf("unexpected adaptive opts: %#v", adaptive)
	}
}
