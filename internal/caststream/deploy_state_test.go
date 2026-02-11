// File: internal/caststream/deploy_state_test.go
// Brief: Internal caststream package implementation for 'deploy state'.

// Package caststream provides caststream helpers.

package caststream

import (
	"encoding/json"
	"testing"

	"github.com/example/ktl/internal/deploy"
)

func TestDeployStateReplayOrdering(t *testing.T) {
	state := newDeployState()
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventSummary,
		Summary: &deploy.SummaryPayload{
			Release:   "web",
			Namespace: "prod",
			Status:    "pending",
		},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventPhase,
		Phase: &deploy.PhasePayload{
			Name:   deploy.PhaseUpgrade,
			Status: "running",
		},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventPhase,
		Phase: &deploy.PhasePayload{
			Name:   deploy.PhaseRender,
			Status: "succeeded",
		},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventResources,
		Resources: []deploy.ResourceStatus{{
			Kind:      "Deployment",
			Namespace: "prod",
			Name:      "web",
		}},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventHealth,
		Health: &deploy.HealthSnapshot{
			Ready: 1,
		},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventDiff,
		Diff: &deploy.DiffPayload{
			Text: "diff",
		},
	})
	state.Record(deploy.StreamEvent{
		Kind: deploy.StreamEventLog,
		Log: &deploy.LogPayload{
			Level:   "info",
			Message: "deploying",
		},
	})

	out := make(chan []byte, 16)
	state.Replay(out)
	close(out)

	var kinds []deploy.StreamEventKind
	for payload := range out {
		var event deploy.StreamEvent
		if err := json.Unmarshal(payload, &event); err != nil {
			t.Fatalf("unmarshal replay payload: %v", err)
		}
		kinds = append(kinds, event.Kind)
	}

	expected := []deploy.StreamEventKind{
		deploy.StreamEventSummary,
		deploy.StreamEventPhase,
		deploy.StreamEventPhase,
		deploy.StreamEventResources,
		deploy.StreamEventHealth,
		deploy.StreamEventDiff,
		deploy.StreamEventLog,
	}
	if len(kinds) != len(expected) {
		t.Fatalf("unexpected replay count %d (want %d)", len(kinds), len(expected))
	}
	for i, want := range expected {
		if kinds[i] != want {
			t.Fatalf("unexpected replay order at %d: got %s want %s", i, kinds[i], want)
		}
	}
}

func TestDeployStateTrimsLogs(t *testing.T) {
	state := newDeployState()
	total := maxCachedLogs + 25
	for i := 0; i < total; i++ {
		state.Record(deploy.StreamEvent{
			Kind: deploy.StreamEventLog,
			Log: &deploy.LogPayload{
				Level:   "info",
				Message: "log",
			},
		})
	}
	if got := len(state.logs); got != maxCachedLogs {
		t.Fatalf("expected %d cached logs, got %d", maxCachedLogs, got)
	}
}
