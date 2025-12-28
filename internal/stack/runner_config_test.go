package stack

import "testing"

func pint(v int) *int         { return &v }
func pbool(v bool) *bool      { return &v }
func pf32(v float32) *float32 { return &v }
func pf64(v float64) *float64 { return &v }

func TestResolveRunnerConfig_ProfileOverridesBase(t *testing.T) {
	root := "/tmp/ktl-root"
	u := &Universe{
		RootDir: root,
		Stacks: map[string]StackFile{
			root: {
				Runner: RunnerConfig{
					Concurrency:            pint(5),
					ProgressiveConcurrency: pbool(true),
					KubeQPS:                pf32(25),
					KubeBurst:              pint(50),
					Adaptive: RunnerAdaptive{
						Mode: "conservative",
					},
					Limits: RunnerLimits{
						MaxParallelPerNamespace: pint(2),
						MaxParallelKind:         map[string]int{"Deployment": 1},
						ParallelismGroupLimit:   pint(2),
					},
				},
				Profiles: map[string]StackProfile{
					"ci": {
						Runner: RunnerConfig{
							Concurrency: pint(10),
							Adaptive: RunnerAdaptive{
								RampMaxFailureRate: pf64(0.25),
							},
						},
					},
				},
			},
		},
	}

	got, err := ResolveRunnerConfig(u, "ci")
	if err != nil {
		t.Fatalf("ResolveRunnerConfig: %v", err)
	}
	if got.Concurrency != 10 {
		t.Fatalf("expected concurrency=10, got %d", got.Concurrency)
	}
	if !got.ProgressiveConcurrency {
		t.Fatalf("expected progressiveConcurrency=true")
	}
	if got.KubeQPS != 25 || got.KubeBurst != 50 {
		t.Fatalf("expected kubeQPS=25 kubeBurst=50, got kubeQPS=%v kubeBurst=%v", got.KubeQPS, got.KubeBurst)
	}
	if got.Adaptive.Mode != "conservative" {
		t.Fatalf("expected mode=conservative, got %q", got.Adaptive.Mode)
	}
	if got.Adaptive.RampMaxFailureRate != 0.25 {
		t.Fatalf("expected rampMaxFailureRate=0.25, got %v", got.Adaptive.RampMaxFailureRate)
	}
	if got.Limits.MaxParallelPerNamespace != 2 {
		t.Fatalf("expected maxParallelPerNamespace=2, got %d", got.Limits.MaxParallelPerNamespace)
	}
	if got.Limits.ParallelismGroupLimit != 2 {
		t.Fatalf("expected parallelismGroupLimit=2, got %d", got.Limits.ParallelismGroupLimit)
	}
	if got.Limits.MaxParallelKind["Deployment"] != 1 {
		t.Fatalf("expected maxParallelKind[Deployment]=1, got %d", got.Limits.MaxParallelKind["Deployment"])
	}
}

func TestValidateRunnerResolved_CatchesBadValues(t *testing.T) {
	_, err := ResolveRunnerConfig(&Universe{
		RootDir: "/tmp/ktl-root2",
		Stacks: map[string]StackFile{
			"/tmp/ktl-root2": {
				Runner: RunnerConfig{
					Concurrency: pint(2),
					Adaptive: RunnerAdaptive{
						Min:                pint(3),
						RampMaxFailureRate: pf64(1.5),
						Window:             pint(2),
					},
				},
			},
		},
	}, "")
	if err == nil {
		t.Fatalf("expected validation error")
	}
}
