package buildkit

import (
	"testing"
	"time"

	"github.com/moby/buildkit/client"
)

type stubDiagnosticObserver struct {
	diags []BuildDiagnostic
}

func (s *stubDiagnosticObserver) HandleDiagnostic(diag BuildDiagnostic) {
	s.diags = append(s.diags, diag)
}

func TestEmitDiagnosticsEmitsCacheEvents(t *testing.T) {
	ch := make(chan *client.SolveStatus)
	observer := &stubDiagnosticObserver{}
	done := make(chan struct{})

	go func() {
		emitDiagnostics(ch, []BuildDiagnosticObserver{observer})
		close(done)
	}()

	now := time.Now()
	ch <- &client.SolveStatus{
		Vertexes: []*client.Vertex{
			{Digest: "sha256:aaaa", Name: "LOAD docker.io/library/alpine", Cached: true},
			{Digest: "sha256:bbbb", Name: "RUN apk add curl", Completed: &now},
		},
	}
	close(ch)
	<-done

	if len(observer.diags) != 2 {
		t.Fatalf("expected 2 diagnostics, got %d", len(observer.diags))
	}

	if observer.diags[0].Type != DiagnosticCacheHit {
		t.Fatalf("expected first diagnostic to be cache hit, got %s", observer.diags[0].Type)
	}
	if observer.diags[1].Type != DiagnosticCacheMiss {
		t.Fatalf("expected second diagnostic to be cache miss, got %s", observer.diags[1].Type)
	}
}
