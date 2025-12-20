package main

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestRenderDeployPlanHTML(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName:  "demo",
		Namespace:    "prod",
		ChartVersion: "1.2.3",
		ChartRef:     "./chart",
		Summary:      planSummary{Creates: 1, Updates: 0, Deletes: 0, Unchanged: 0},
		Changes: []planResourceChange{
			{Key: resourceKey{Kind: "ConfigMap", Name: "cfg", Namespace: "prod"}, Kind: changeCreate, Diff: "apiVersion: v1"},
		},
		Warnings:    []string{"Updating demo"},
		ClusterHost: "https://cluster",
		InstallCmd:  "ktl apply --chart ./chart --release demo",
		GeneratedAt: time.Now(),
	}

	html, err := renderDeployPlanHTML(result)
	if err != nil {
		t.Fatalf("render HTML: %v", err)
	}
	if !strings.Contains(html, "Release demo") {
		t.Fatalf("expected release title in html, got %q", html)
	}
	if !strings.Contains(html, "ConfigMap") {
		t.Fatalf("missing change details: %q", html)
	}
}

func TestPlanHTMLEmbedsJSON(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName:  "demo",
		Namespace:    "prod",
		ChartVersion: "1.0.0",
		ChartRef:     "./chart",
		GeneratedAt:  time.Now(),
	}
	html, err := renderDeployPlanHTML(result)
	if err != nil {
		t.Fatalf("render HTML: %v", err)
	}
	if !strings.Contains(html, `id="ktlPlanData"`) {
		t.Fatalf("expected embedded plan JSON block")
	}
	parsed, err := parsePlanHTML([]byte(html))
	if err != nil {
		t.Fatalf("parse plan HTML: %v", err)
	}
	if parsed.ReleaseName != "demo" || parsed.Namespace != "prod" {
		t.Fatalf("unexpected parsed plan: %+v", parsed)
	}
}

func TestRenderDeployVisualizeHTML(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName:   "demo",
		Namespace:     "prod",
		ChartRef:      "./chart",
		GraphNodes:    []deployGraphNode{{ID: "prod|deployment|web", Kind: "Deployment", Name: "web", Namespace: "prod"}},
		GraphEdges:    []deployGraphEdge{{From: "prod|deployment|web", To: "prod|configmap|cfg"}},
		ManifestBlobs: map[string]string{"prod|deployment|web": "kind: Deployment\nmetadata:\n  name: web"},
		LiveManifests: map[string]string{"prod|deployment|web": "kind: Deployment\nmetadata:\n  name: web"},
		ManifestDiffs: map[string]string{"prod|deployment|web": "--- live\n+++ rendered\n"},
		Summary:       planSummary{Creates: 1, Updates: 2, Deletes: 0, Unchanged: 3},
		GeneratedAt:   time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Warnings:      []string{"image cache stale"},
		InstallCmd:    "ktl apply --chart ./chart --release demo",
		Changes: []planResourceChange{
			{Key: resourceKey{Namespace: "prod", Kind: "Deployment", Name: "web"}, Kind: changeUpdate},
		},
		OfflineFallback: false,
	}
	html, err := renderDeployVisualizeHTML(result, nil)
	if err != nil {
		t.Fatalf("render viz html: %v", err)
	}
	if !strings.Contains(html, "ktl Deploy Visualize") {
		t.Fatalf("missing visualize heading")
	}
	if !strings.Contains(html, "Load comparison") {
		t.Fatalf("missing comparison controls")
	}
	if !strings.Contains(html, `id="dataErrorBanner"`) {
		t.Fatalf("missing data error banner")
	}
	if !strings.Contains(html, `id="emptyState"`) {
		t.Fatalf("missing empty state panel")
	}
	if !strings.Contains(html, `id="impactSummary"`) {
		t.Fatalf("missing impact summary")
	}
	if !strings.Contains(html, `id="preflightList"`) {
		t.Fatalf("missing preflight list")
	}
	if !strings.Contains(html, `id="diffToolbar"`) {
		t.Fatalf("missing diff toolbar")
	}
	if !strings.Contains(html, `id="vizData"`) {
		t.Fatalf("missing viz data block")
	}
	vizData := extractVizData(t, html)
	var payload deployVisualizePayload
	if err := json.Unmarshal([]byte(vizData), &payload); err != nil {
		t.Fatalf("parse viz payload: %v", err)
	}
	if payload.Release != "demo" || len(payload.Nodes) != 1 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if payload.LiveManifests == nil || payload.ManifestDiffs == nil {
		t.Fatalf("expected live manifests and diffs in payload: %+v", payload)
	}
	if payload.ChangeKinds == nil || payload.ChangeKinds["prod|deployment|web"] != string(changeUpdate) {
		t.Fatalf("expected changeKinds to include update: %+v", payload.ChangeKinds)
	}
	if payload.Summary.Creates != 1 || payload.Summary.Updates != 2 {
		t.Fatalf("expected summary data round-tripped: %+v", payload.Summary)
	}
	if len(payload.Warnings) != 1 || payload.Warnings[0] != "image cache stale" {
		t.Fatalf("expected warnings to round-trip: %+v", payload.Warnings)
	}
	if payload.InstallCommand == "" {
		t.Fatalf("expected install command in payload")
	}
}

func TestRenderDeployVisualizeHTMLWithCompare(t *testing.T) {
	base := &deployPlanResult{
		ReleaseName:   "demo",
		Namespace:     "prod",
		ChartRef:      "./chart",
		GraphNodes:    []deployGraphNode{{ID: "prod|configmap|cfg", Kind: "ConfigMap", Name: "cfg", Namespace: "prod"}},
		GraphEdges:    nil,
		ManifestBlobs: map[string]string{"prod|configmap|cfg": "kind: ConfigMap\nmetadata:\n  name: cfg"},
		Summary:       planSummary{Creates: 1, Updates: 0, Deletes: 0, Unchanged: 0},
		Changes: []planResourceChange{
			{Key: resourceKey{Namespace: "prod", Kind: "ConfigMap", Name: "cfg"}, Kind: changeCreate},
		},
	}
	compare := &deployPlanResult{
		ReleaseName:   "demo-prev",
		Namespace:     "prod",
		ChartRef:      "./chart",
		ManifestBlobs: map[string]string{"prod|configmap|cfg": "kind: ConfigMap\nmetadata:\n  name: cfg-old"},
		GeneratedAt:   time.Date(2025, 1, 2, 3, 4, 0, 0, time.UTC),
	}
	html, err := renderDeployVisualizeHTML(base, compare)
	if err != nil {
		t.Fatalf("render viz html: %v", err)
	}
	vizData := extractVizData(t, html)
	var payload deployVisualizePayload
	if err := json.Unmarshal([]byte(vizData), &payload); err != nil {
		t.Fatalf("parse payload: %v", err)
	}
	if payload.CompareManifests == nil || payload.CompareManifests["prod|configmap|cfg"] == "" {
		t.Fatalf("expected compare manifests embedded: %+v", payload.CompareManifests)
	}
	if payload.CompareSummary == "" || !strings.Contains(payload.CompareSummary, "demo-prev") {
		t.Fatalf("expected compare summary, got %q", payload.CompareSummary)
	}
}

var vizDataScriptRegex = regexp.MustCompile(`(?s)<script[^>]+id=["']vizData["'][^>]*>(.*?)</script>`)

func extractVizData(t *testing.T, html string) string {
	t.Helper()
	match := vizDataScriptRegex.FindStringSubmatch(html)
	if len(match) < 2 {
		t.Fatalf("viz html missing embedded data block")
	}
	return match[1]
}
