package buildkit

import (
	"context"
	"runtime"
	"testing"

	"github.com/moby/buildkit/client"
	ocispecs "github.com/opencontainers/image-spec/specs-go/v1"
)

func TestDefaultPlatformPreservesRuntimeOS(t *testing.T) {
	got := defaultPlatform("windows", "arm64")
	if got != "windows/arm64" {
		t.Fatalf("expected windows/arm64, got %s", got)
	}
}

func TestDefaultPlatformFallsBackToRuntimeArch(t *testing.T) {
	want := "linux/" + runtime.GOARCH
	if got := defaultPlatform("linux", ""); got != want {
		t.Fatalf("expected %s, got %s", want, got)
	}
}

func TestCollectWorkerPlatformsDeduplicates(t *testing.T) {
	workers := []*client.WorkerInfo{
		{Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "amd64"}, {OS: "linux", Architecture: "arm64"}}},
		{Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "amd64"}}},
	}
	got := collectWorkerPlatforms(workers)
	if len(got) != 2 {
		t.Fatalf("expected 2 platforms, got %v", got)
	}
	if got[0] != "linux/amd64" || got[1] != "linux/arm64" {
		t.Fatalf("unexpected platforms order/content: %v", got)
	}
}

func TestDetectBuilderPlatformsUsesWorkers(t *testing.T) {
	workers := []*client.WorkerInfo{{Platforms: []ocispecs.Platform{{OS: "linux", Architecture: "amd64"}}}}
	got, err := detectBuilderPlatforms(context.Background(), &fakeWorkerLister{workers: workers})
	if err != nil {
		t.Fatalf("detectBuilderPlatforms returned error: %v", err)
	}
	if len(got) != 1 || got[0] != "linux/amd64" {
		t.Fatalf("unexpected platforms: %v", got)
	}
}

type fakeWorkerLister struct {
	workers []*client.WorkerInfo
	err     error
}

func (f *fakeWorkerLister) ListWorkers(context.Context, ...client.ListWorkersOption) ([]*client.WorkerInfo, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.workers, nil
}
