package buildsvc

import (
	"bytes"
	"testing"

	"github.com/kubekattle/ktl/internal/tailer"
)

func TestConsoleObserver_LogLevelFiltersGraph(t *testing.T) {
	rec := tailer.LogRecord{
		Namespace:   "graph",
		Source:      "graph",
		SourceGlyph: "◆",
		Rendered:    "graph payload",
		Raw:         "graph payload",
	}

	var buf bytes.Buffer
	obsInfo := NewConsoleObserverWithLevel(&buf, "info")
	obsInfo.ObserveLog(rec)
	if got := buf.String(); got != "" {
		t.Fatalf("expected graph suppressed at info, got %q", got)
	}

	buf.Reset()
	obsDebug := NewConsoleObserverWithLevel(&buf, "debug")
	obsDebug.ObserveLog(rec)
	if got := buf.String(); got == "" {
		t.Fatalf("expected graph printed at debug")
	}
}

func TestConsoleObserver_LogLevelWarnKeepsCacheMiss(t *testing.T) {
	rec := tailer.LogRecord{
		Namespace:   "diagnostic",
		Pod:         "x",
		Container:   "diagnostic",
		Source:      "build",
		SourceGlyph: "⚠",
		Rendered:    "cache miss (no reusable layer found)",
		Raw:         "cache miss (no reusable layer found)",
	}

	var buf bytes.Buffer
	obsWarn := NewConsoleObserverWithLevel(&buf, "warn")
	obsWarn.ObserveLog(rec)
	if got := buf.String(); got == "" {
		t.Fatalf("expected warn to print cache miss")
	}

	buf.Reset()
	obsErr := NewConsoleObserverWithLevel(&buf, "error")
	obsErr.ObserveLog(rec)
	if got := buf.String(); got != "" {
		t.Fatalf("expected error to suppress warn diagnostics, got %q", got)
	}
}
