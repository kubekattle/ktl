package ui

import (
	"bytes"
	"testing"
	"time"

	"github.com/example/ktl/internal/tailer"
)

func TestBuildConsoleEventTailDedupAndClamp(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewBuildConsole(buf, BuildMetadata{ContextDir: "ctx"}, BuildConsoleOptions{Enabled: true, Width: 120})

	rec := func(msg string) tailer.LogRecord {
		return tailer.LogRecord{
			Timestamp:   time.Now(),
			Namespace:   "build",
			Pod:         "ctx",
			Container:   "info",
			Source:      "build",
			SourceGlyph: "ℹ",
			Rendered:    msg,
			Raw:         msg,
		}
	}

	c.ObserveLog(rec("a"))
	c.ObserveLog(rec("a")) // dedup
	c.ObserveLog(rec("b"))
	c.ObserveLog(rec("c"))
	c.ObserveLog(rec("d"))
	c.ObserveLog(rec("e"))
	c.ObserveLog(rec("f")) // should clamp to last 5: b..f

	c.mu.Lock()
	got := append([]string(nil), c.events...)
	c.mu.Unlock()

	if len(got) != 5 {
		t.Fatalf("expected 5 events, got %d (%v)", len(got), got)
	}
	if got[0] != "b" || got[4] != "f" {
		t.Fatalf("unexpected tail: %v", got)
	}
}

func TestBuildConsoleDoesNotBannerOnCacheMiss(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewBuildConsole(buf, BuildMetadata{ContextDir: "ctx"}, BuildConsoleOptions{Enabled: true, Width: 120})

	c.ObserveLog(tailer.LogRecord{
		Timestamp:   time.Now(),
		Source:      "diagnostic",
		SourceGlyph: "⚠",
		Rendered:    "cache miss",
		Raw:         "cache miss",
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.warning != nil {
		t.Fatalf("expected no warning banner for cache miss, got %+v", *c.warning)
	}
	if c.cacheMiss != 1 {
		t.Fatalf("expected cache miss count=1, got %d", c.cacheMiss)
	}
}

func TestBuildConsoleParsesPhases(t *testing.T) {
	buf := &bytes.Buffer{}
	c := NewBuildConsole(buf, BuildMetadata{ContextDir: "ctx"}, BuildConsoleOptions{Enabled: true, Width: 120})

	c.ObserveLog(tailer.LogRecord{
		Timestamp: time.Now(),
		Source:    "phase",
		Namespace: "phase",
		Pod:       "solve",
		Container: "running",
		Rendered:  "Building Dockerfile",
		Raw:       "Building Dockerfile",
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	if c.phases == nil {
		t.Fatalf("expected phases map to be initialized")
	}
	if got := c.phases["solve"].State; got != "running" {
		t.Fatalf("expected solve phase state=running, got %q", got)
	}
}

