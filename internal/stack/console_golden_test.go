package stack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeClock struct {
	t time.Time
}

func (c *fakeClock) Now() time.Time { return c.t }

func TestRunConsole_TTYSpec_Golden(t *testing.T) {
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	run := func(t *testing.T, goldenRelPath string, command string, feed func(clock *fakeClock, emit func(RunEvent))) {
		t.Helper()
		clock := &fakeClock{t: t0}
		console := NewRunConsole(nil, testPlan(), command, RunConsoleOptions{
			Enabled: true,
			Verbose: false,
			Width:   120,
			Now:     clock.Now,
			Color:   true,
		})
		emit := func(ev RunEvent) {
			console.mu.Lock()
			console.applyEventLocked(ev)
			console.mu.Unlock()
		}
		feed(clock, emit)

		got := strings.Join(console.SnapshotLines(), "\n") + "\n"
		got = strings.ReplaceAll(got, "\x1b", "\\x1b")
		want := readGolden(t, goldenRelPath)
		if got != want {
			t.Fatalf("golden mismatch\n--- want\n%s--- got\n%s", want, got)
		}
	}

	t.Run("apply_with_failure_rail", func(t *testing.T) {
		run(t, "../../testdata/stack/console/tty_apply.golden", "apply", func(clock *fakeClock, emit func(RunEvent)) {
			emit(RunEvent{
				TS:    t0.Format(time.RFC3339Nano),
				RunID: "r-123",
				Type:  string(RunStarted),
				Fields: map[string]any{
					"command":     "apply",
					"concurrency": 4,
				},
			})

			clock.t = t0.Add(900 * time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/a", Type: string(NodeRunning), Attempt: 1})
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/a", Type: string(PhaseStarted), Attempt: 1, Fields: map[string]any{"phase": "upgrade"}})

			clock.t = t0.Add(2*time.Second + 100*time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/a", Type: string(NodeSucceeded), Attempt: 1})

			clock.t = t0.Add(3*time.Second + 400*time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/b", Type: string(NodeRunning), Attempt: 1})
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/b", Type: string(PhaseStarted), Attempt: 1, Fields: map[string]any{"phase": "upgrade"}})
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/b", Type: string(BudgetWait), Attempt: 1, Message: "budget wait: cluster/dev ns/ns-b limit=1 active=1"})

			clock.t = t0.Add(7*time.Second + 300*time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-123", NodeID: "dev/c", Type: string(NodeRunning), Attempt: 1})
			emit(RunEvent{
				TS:      clock.t.Format(time.RFC3339Nano),
				RunID:   "r-123",
				NodeID:  "dev/c",
				Type:    string(NodeFailed),
				Attempt: 1,
				Message: "helm upgrade failed: context deadline exceeded while waiting for resources",
				Error: &RunError{
					Class:   "RATE_LIMIT",
					Message: "context deadline exceeded",
					Digest:  "0123456789abcdef0123456789abcdef",
				},
			})

			clock.t = t0.Add(12*time.Second + 300*time.Millisecond)
		})
	})

	t.Run("delete_without_failures", func(t *testing.T) {
		run(t, "../../testdata/stack/console/tty_delete.golden", "delete", func(clock *fakeClock, emit func(RunEvent)) {
			emit(RunEvent{
				TS:    t0.Format(time.RFC3339Nano),
				RunID: "r-999",
				Type:  string(RunStarted),
				Fields: map[string]any{
					"command":     "delete",
					"concurrency": 2,
				},
			})

			clock.t = t0.Add(1*time.Second + 200*time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-999", NodeID: "dev/b", Type: string(NodeRunning), Attempt: 1})
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-999", NodeID: "dev/b", Type: string(PhaseStarted), Attempt: 1, Fields: map[string]any{"phase": "delete"}})

			clock.t = t0.Add(2*time.Second + 600*time.Millisecond)
			emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-999", NodeID: "dev/c", Type: string(NodeBlocked), Attempt: 1, Message: "blocked by dependency failure: dev/b"})

			clock.t = t0.Add(9*time.Second + 900*time.Millisecond)
		})
	})

	t.Run("apply_with_helm_logs", func(t *testing.T) {
		t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
		clock := &fakeClock{t: t0}
		console := NewRunConsole(nil, testPlan(), "apply", RunConsoleOptions{
			Enabled:      true,
			Verbose:      false,
			Width:        120,
			Now:          clock.Now,
			Color:        true,
			ShowHelmLogs: true,
			HelmLogTail:  3,
		})
		emit := func(ev RunEvent) {
			console.mu.Lock()
			console.applyEventLocked(ev)
			console.mu.Unlock()
		}

		emit(RunEvent{
			TS:    t0.Format(time.RFC3339Nano),
			RunID: "r-helm-1",
			Type:  string(RunStarted),
			Fields: map[string]any{
				"command":     "apply",
				"concurrency": 2,
			},
		})

		clock.t = t0.Add(800 * time.Millisecond)
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/a", Type: string(NodeRunning), Attempt: 1})
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/a", Type: string(HelmLog), Attempt: 1, Message: "getting history for release a"})
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/a", Type: string(HelmLog), Attempt: 1, Message: "performing update"})

		clock.t = t0.Add(1*time.Second + 900*time.Millisecond)
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/a", Type: string(NodeSucceeded), Attempt: 1})

		clock.t = t0.Add(2*time.Second + 200*time.Millisecond)
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/b", Type: string(NodeRunning), Attempt: 1})
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/b", Type: string(HelmLog), Attempt: 1, Message: "getting history for release b"})
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/b", Type: string(HelmLog), Attempt: 1, Message: "performing update"})
		emit(RunEvent{TS: clock.t.Format(time.RFC3339Nano), RunID: "r-helm-1", NodeID: "dev/b", Type: string(HelmLog), Attempt: 1, Message: "waiting for resources"})

		got := strings.Join(console.SnapshotLines(), "\n") + "\n"
		got = strings.ReplaceAll(got, "\x1b", "\\x1b")
		want := readGolden(t, "../../testdata/stack/console/tty_apply_helm_logs.golden")
		if got != want {
			t.Fatalf("golden mismatch\n--- want\n%s--- got\n%s", want, got)
		}
	})
}

func readGolden(t *testing.T, relPath string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Clean(relPath))
	if err != nil {
		t.Fatalf("read golden %s: %v", relPath, err)
	}
	return string(b)
}

func testPlan() *Plan {
	return &Plan{
		StackName: "payments",
		StackRoot: "/repo/testdata/stack",
		Nodes: []*ResolvedRelease{
			{
				ID:             "dev/a",
				Name:           "a",
				Cluster:        ClusterTarget{Name: "dev"},
				Namespace:      "ns-a",
				ExecutionGroup: 0,
			},
			{
				ID:             "dev/b",
				Name:           "b",
				Cluster:        ClusterTarget{Name: "dev"},
				Namespace:      "ns-b",
				Needs:          []string{"a"},
				ExecutionGroup: 1,
			},
			{
				ID:             "dev/c",
				Name:           "c",
				Cluster:        ClusterTarget{Name: "dev"},
				Namespace:      "ns-c",
				Needs:          []string{"b"},
				ExecutionGroup: 2,
			},
		},
	}
}
