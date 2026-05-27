package stack

import (
	"testing"
	"time"
)

func pint(v int) *int         { return &v }
func pbool(v bool) *bool      { return &v }
func pf32(v float32) *float32 { return &v }
func pf64(v float64) *float64 { return &v }

func pduration(v time.Duration) *time.Duration { return &v }

func TestResolveRunnerConfig_ProfileOverridesBase(t *testing.T) {
	root := "/tmp/torque-root"
	u := &Universe{
		RootDir: root,
		Stacks: map[string]StackFile{
			root: {
				Runner: RunnerConfig{
					Mode:                   RunnerModeFleet,
					Concurrency:            pint(5),
					ProgressiveConcurrency: pbool(true),
					KubeQPS:                pf32(25),
					KubeBurst:              pint(50),
					Readiness: RunnerReadiness{
						Store:           RunnerReadinessStoreFile,
						StorePath:       "/tmp/base-registry.json",
						Tenant:          "lab",
						Selector:        map[string]string{"role": "mysql"},
						MinReadyPercent: pint(95),
						FailureBudget:   pint(5),
					},
					Fanout: RunnerFanout{
						MaxParallel:         pint(12),
						MaxFailed:           pint(2),
						MinSucceededPercent: pint(90),
						OnPartialFailure:    RunnerFanoutOnContinue,
						Delivery:            RunnerFanoutDeliveryJetStream,
						Retry: RunnerFanoutRetry{
							MaxDeliver:  pint(4),
							AckWait:     pduration(15 * time.Second),
							Backoff:     []time.Duration{time.Second, 5 * time.Second},
							OnExhausted: RunnerFanoutRetryOnBlock,
						},
					},
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
							Readiness: RunnerReadiness{
								Selector: map[string]string{"role": "db"},
							},
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
	if got.Mode != RunnerModeFleet || !got.Readiness.Enabled || !got.Readiness.RequireAgents {
		t.Fatalf("expected fleet readiness enabled, got %#v", got.Readiness)
	}
	if got.Readiness.Tenant != "lab" || got.Readiness.Selector["role"] != "db" || got.Readiness.MinReadyPercent != 95 || got.Readiness.FailureBudget != 5 {
		t.Fatalf("readiness override/defaults = %#v", got.Readiness)
	}
	if got.Fanout.MaxParallel != 12 || got.Fanout.MaxFailed != 2 || got.Fanout.MinSucceededPercent != 90 || got.Fanout.OnPartialFailure != RunnerFanoutOnContinue || got.Fanout.Delivery != RunnerFanoutDeliveryJetStream {
		t.Fatalf("fanout = %#v", got.Fanout)
	}
	if got.Fanout.Retry.MaxDeliver != 4 || got.Fanout.Retry.AckWait != 15*time.Second || len(got.Fanout.Retry.Backoff) != 2 || got.Fanout.Retry.OnExhausted != RunnerFanoutRetryOnBlock {
		t.Fatalf("fanout retry = %#v", got.Fanout.Retry)
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
		RootDir: "/tmp/torque-root2",
		Stacks: map[string]StackFile{
			"/tmp/torque-root2": {
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
