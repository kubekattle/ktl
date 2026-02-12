// File: internal/workflows/buildsvc/progress_test.go
// Brief: Internal buildsvc package implementation for 'progress'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"bytes"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/kubekattle/ktl/internal/tailer"
	"github.com/kubekattle/ktl/pkg/buildkit"
	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
)

type observerFunc func(tailer.LogRecord)

func (f observerFunc) ObserveLog(rec tailer.LogRecord) {
	f(rec)
}

func TestBuildProgressBroadcasterEmitsEvents(t *testing.T) {
	b := newBuildProgressBroadcaster("ctx")
	var (
		mu     sync.Mutex
		lines  []string
		target = digest.FromString("step-1")
		now    = time.Now()
	)
	b.addObserver(observerFunc(func(rec tailer.LogRecord) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, rec.Rendered)
	}))

	b.HandleStatus(&client.SolveStatus{
		Vertexes: []*client.Vertex{{
			Digest:  target,
			Name:    "load base image",
			Started: &now,
		}},
	})

	b.HandleStatus(&client.SolveStatus{
		Logs: []*client.VertexLog{{
			Vertex: target,
			Data:   []byte("RUN apk add curl\n"),
		}},
	})

	b.HandleStatus(&client.SolveStatus{
		Vertexes: []*client.Vertex{{
			Digest:    target,
			Completed: &now,
		}},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(lines) < 2 {
		t.Fatalf("expected at least two mirrored entries, got %d", len(lines))
	}
	assertContains := func(substr string) {
		for _, line := range lines {
			if strings.Contains(line, substr) {
				return
			}
		}
		t.Fatalf("expected to find %q in %v", substr, lines)
	}
	assertContains("Started load base image")
	assertContains("RUN apk add curl")
	assertContains("Completed load base image")
}

func TestBuildDiagnosticObserverPrefersStreamOverWriter(t *testing.T) {
	stream := newBuildProgressBroadcaster("ctx")
	var buf bytes.Buffer
	obs := &buildDiagnosticObserver{stream: stream, writer: &buf}

	obs.HandleDiagnostic(buildkit.BuildDiagnostic{
		Type:   buildkit.DiagnosticCacheMiss,
		Name:   "load dockerfile",
		Vertex: digest.FromString("v1"),
	})

	if got := buf.String(); got != "" {
		t.Fatalf("expected no direct writer output when stream is present, got %q", got)
	}
}
