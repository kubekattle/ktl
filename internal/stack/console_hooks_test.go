package stack

import (
	"testing"
	"time"
)

func TestRunConsole_CollapsesHookStartedAfterTerminalEvent(t *testing.T) {
	clock := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	c := NewRunConsole(nil, nil, "apply", RunConsoleOptions{
		Enabled:   true,
		ShowHooks: true,
		Now:       func() time.Time { return clock },
	})

	emit := func(ev RunEvent) {
		c.mu.Lock()
		c.applyEventLocked(ev)
		c.mu.Unlock()
	}

	startTS := clock.Format(time.RFC3339Nano)
	emit(RunEvent{
		TS:    startTS,
		RunID: "r-1",
		Type:  string(HookStarted),
		Fields: map[string]any{
			"phase": "pre-apply",
			"hook":  "stack-pre",
		},
	})
	emit(RunEvent{
		TS:    startTS,
		RunID: "r-1",
		Type:  string(HookSucceeded),
		Fields: map[string]any{
			"phase": "pre-apply",
			"hook":  "stack-pre",
		},
	})

	c.mu.Lock()
	defer c.mu.Unlock()

	started := 0
	succeeded := 0
	for _, e := range c.hookEvents {
		if e.hook != "stack-pre" {
			continue
		}
		switch e.status {
		case "started":
			started++
		case "succeeded":
			succeeded++
		}
	}
	if started != 0 {
		t.Fatalf("expected HOOK_STARTED to be collapsed, got started=%d", started)
	}
	if succeeded != 1 {
		t.Fatalf("expected exactly 1 terminal hook event, got succeeded=%d", succeeded)
	}
}
