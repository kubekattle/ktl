package main

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	helmtime "helm.sh/helm/v3/pkg/time"
)

type fakeArtifactRecorder struct {
	artifacts map[string]string
}

func (f *fakeArtifactRecorder) RecordArtifact(_ context.Context, name, text string) error {
	if f.artifacts == nil {
		f.artifacts = map[string]string{}
	}
	f.artifacts[name] = text
	return nil
}

func TestCaptureHelmRelease_WritesManifestAndJSON(t *testing.T) {
	rec := &fakeArtifactRecorder{}
	now := time.Date(2025, 12, 20, 12, 0, 0, 123, time.UTC)
	rel := &release.Release{
		Name:      "monitoring",
		Namespace: "default",
		Version:   7,
		Manifest:  "kind: ConfigMap\n",
		Info: &release.Info{
			Status:       release.StatusDeployed,
			LastDeployed: helmtime.Time{Time: now},
		},
		Chart: &chart.Chart{Metadata: &chart.Metadata{Name: "tempo", Version: "1.2.3"}},
	}

	captureHelmRelease(context.Background(), rec, rel)

	if got := rec.artifacts["apply.release.manifest"]; got != "kind: ConfigMap\n" {
		t.Fatalf("apply.release.manifest = %q", got)
	}

	raw := rec.artifacts["apply.release.json"]
	if raw == "" {
		t.Fatal("apply.release.json is empty")
	}

	var decoded capturedHelmRelease
	if err := json.Unmarshal([]byte(raw), &decoded); err != nil {
		t.Fatalf("unmarshal apply.release.json: %v", err)
	}
	if decoded.Name != "monitoring" || decoded.Namespace != "default" || decoded.Revision != 7 {
		t.Fatalf("decoded summary = %#v", decoded)
	}
	if decoded.Status != "deployed" {
		t.Fatalf("decoded status = %q", decoded.Status)
	}
	if decoded.Chart != "tempo" || decoded.Version != "1.2.3" {
		t.Fatalf("decoded chart/version = %#v", decoded)
	}
	if decoded.UpdatedAt != now.Format(time.RFC3339Nano) {
		t.Fatalf("decoded updatedAt = %q", decoded.UpdatedAt)
	}
}
