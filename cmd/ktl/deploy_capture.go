package main

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"helm.sh/helm/v3/pkg/release"
)

type artifactRecorder interface {
	RecordArtifact(ctx context.Context, name, text string) error
}

type capturedHelmRelease struct {
	Name      string `json:"name,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Revision  int    `json:"revision,omitempty"`
	Status    string `json:"status,omitempty"`
	Chart     string `json:"chart,omitempty"`
	Version   string `json:"version,omitempty"`
	UpdatedAt string `json:"updatedAt,omitempty"`
}

func captureHelmRelease(ctx context.Context, rec artifactRecorder, rel *release.Release) {
	if rec == nil || rel == nil {
		return
	}

	if strings.TrimSpace(rel.Manifest) != "" {
		_ = rec.RecordArtifact(ctx, "apply.release.manifest", rel.Manifest)
	}

	summary := capturedHelmRelease{
		Name:      strings.TrimSpace(rel.Name),
		Namespace: strings.TrimSpace(rel.Namespace),
		Revision:  rel.Version,
	}
	if rel.Info != nil {
		if !rel.Info.LastDeployed.IsZero() {
			summary.UpdatedAt = rel.Info.LastDeployed.Format(time.RFC3339Nano)
		}
		summary.Status = rel.Info.Status.String()
	}
	if rel.Chart != nil && rel.Chart.Metadata != nil {
		summary.Chart = strings.TrimSpace(rel.Chart.Metadata.Name)
		summary.Version = strings.TrimSpace(rel.Chart.Metadata.Version)
	}

	raw, err := json.Marshal(summary)
	if err == nil {
		_ = rec.RecordArtifact(ctx, "apply.release.json", string(raw))
	}
}
