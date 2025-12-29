package verify

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestConsoleSnapshotLines(t *testing.T) {
	t0 := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	now := t0

	buf := &bytes.Buffer{}
	c := NewConsole(buf, ConsoleMeta{
		Target: "chart ./chart (release=demo ns=default)",
		Mode:   ModeWarn,
		FailOn: SeverityHigh,
	}, ConsoleOptions{
		Enabled: true,
		Width:   200,
		Color:   true,
		Now:     func() time.Time { return now },
		Tail:    3,
	})

	c.Observe(Event{Type: EventProgress, When: t0, Phase: "evaluate"})
	c.Observe(Event{Type: EventFinding, When: t0, Finding: &Finding{
		RuleID:   "k8s/container_is_privileged",
		Severity: SeverityHigh,
		Message:  "Container should not run privileged",
		Subject:  Subject{Kind: "Deployment", Namespace: "default", Name: "api"},
		Location: "spec.template.spec.containers[0].securityContext.privileged",
	}})
	c.Observe(Event{Type: EventFinding, When: t0, Finding: &Finding{
		RuleID:   "k8s/memory_limits_not_defined",
		Severity: SeverityMedium,
		Message:  "Memory limits should be set",
		Subject:  Subject{Kind: "Deployment", Namespace: "default", Name: "api"},
		Location: "spec.template.spec.containers[0].resources.limits.memory",
	}})

	now = t0.Add(2 * time.Second)
	s := Summary{
		Total:   2,
		BySev:   map[Severity]int{SeverityHigh: 1, SeverityMedium: 1},
		Passed:  true,
		Blocked: false,
	}
	c.Observe(Event{Type: EventSummary, When: now, Summary: &s})
	c.Observe(Event{Type: EventDone, When: now, Passed: true, Blocked: false})

	got := strings.Join(c.SnapshotLines(), "\n") + "\n"
	got = strings.ReplaceAll(got, "\x1b", "\\x1b")
	if !strings.Contains(got, "KTL VERIFY") {
		t.Fatalf("expected header, got:\n%s", got)
	}
	if !strings.Contains(got, "[PASS]") {
		t.Fatalf("expected pass status, got:\n%s", got)
	}
	if !strings.Contains(got, "severity:") {
		t.Fatalf("expected severity breakdown, got:\n%s", got)
	}
	if !strings.Contains(got, "top rules:") {
		t.Fatalf("expected top rules section, got:\n%s", got)
	}
	if !strings.Contains(got, "RECENT FINDINGS") {
		t.Fatalf("expected findings header, got:\n%s", got)
	}
	if !strings.Contains(got, "Container should not run privileged") {
		t.Fatalf("expected finding message to be visible, got:\n%s", got)
	}
}
