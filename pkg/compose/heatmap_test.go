package compose

import (
	"errors"
	"testing"
	"time"

	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"

	"github.com/example/ktl/pkg/buildkit"
)

func TestServiceHeatmapCollectorSummary(t *testing.T) {
	collector := newServiceHeatmapCollector("checkout")
	start := time.Now()
	mid := start.Add(6 * time.Second)
	end := start.Add(12 * time.Second)

	collector.HandleStatus(&client.SolveStatus{
		Vertexes: []*client.Vertex{
			{
				Digest:    digest.FromString("layer-run"),
				Name:      "RUN npm install",
				Started:   &start,
				Completed: &mid,
			},
			{
				Digest:    digest.FromString("layer-copy"),
				Name:      "COPY . .",
				Started:   &mid,
				Completed: &end,
			},
		},
	})
	collector.HandleDiagnostic(buildkit.BuildDiagnostic{Type: buildkit.DiagnosticCacheMiss})
	collector.HandleDiagnostic(buildkit.BuildDiagnostic{Type: buildkit.DiagnosticCacheHit})

	summary := collector.snapshotSummary(nil)
	if summary.Service != "checkout" {
		t.Fatalf("unexpected service: %s", summary.Service)
	}
	if summary.StepsTotal != 2 {
		t.Fatalf("expected 2 steps, got %d", summary.StepsTotal)
	}
	if len(summary.Hotspots) == 0 {
		t.Fatalf("expected hotspots to be populated")
	}
	if summary.Status != heatmapStatusWarn {
		t.Fatalf("expected warn status, got %s", summary.Status)
	}
	if summary.DurationMillis <= 0 {
		t.Fatalf("expected duration to be recorded")
	}
}

func TestServiceHeatmapCollectorFailure(t *testing.T) {
	collector := newServiceHeatmapCollector("api")
	start := time.Now()
	collector.HandleStatus(&client.SolveStatus{
		Vertexes: []*client.Vertex{
			{
				Digest:  digest.FromString("layer"),
				Name:    "RUN test",
				Started: &start,
				Error:   "exit 1",
			},
		},
	})
	summary := collector.snapshotSummary(errors.New("boom"))
	if summary.Status != heatmapStatusFail {
		t.Fatalf("expected fail status, got %s", summary.Status)
	}
	if summary.FailedStep == "" {
		t.Fatalf("expected failed step to be recorded")
	}
}
