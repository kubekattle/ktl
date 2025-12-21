// File: cmd/ktl/deploy_plan_html_test.go
// Brief: CLI command wiring and implementation for 'deploy plan html'.

// Package main provides the ktl CLI entrypoints.

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
	html, err := renderDeployVisualizeHTML(result, nil, deployVisualizeFeatures{})
	if err != nil {
		t.Fatalf("render viz html: %v", err)
	}
	if !strings.Contains(html, "ktl Plan Visualize") {
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
	if !strings.Contains(html, `data-manifest-mode="quota"`) {
		t.Fatalf("missing quota manifest mode")
	}
	if !strings.Contains(html, `id="vizData"`) {
		t.Fatalf("missing viz data block")
	}
	if strings.Contains(html, `helm\\.sh`) {
		t.Fatalf("expected helm hook heuristic to avoid regex literals, got %q", `helm\\.sh`)
	}
	if !strings.Contains(html, `helm.sh/hook:`) {
		t.Fatalf("expected helm hook heuristic to match on literal key, got %q", `helm.sh/hook:`)
	}
	if strings.Contains(html, "__FEATURES__") {
		t.Fatalf("expected features placeholder to be replaced")
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

func TestBuildDeployVisualizePayloadMatchesEmbeddedJSON(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName:   "demo",
		Namespace:     "prod",
		ChartRef:      "./chart",
		GraphNodes:    []deployGraphNode{{ID: "prod|deployment|web", Kind: "Deployment", Name: "web", Namespace: "prod"}},
		GraphEdges:    []deployGraphEdge{{From: "prod|deployment|web", To: "prod|configmap|cfg"}},
		ManifestBlobs: map[string]string{"prod|deployment|web": "kind: Deployment\nmetadata:\n  name: web"},
		ManifestDiffs: map[string]string{"prod|deployment|web": "--- live\n+++ rendered\n"},
		Changes: []planResourceChange{
			{Key: resourceKey{Namespace: "prod", Kind: "Deployment", Name: "web"}, Kind: changeUpdate},
		},
		GeneratedAt: time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
	}

	embeddedHTML, err := renderDeployVisualizeHTML(result, nil, deployVisualizeFeatures{})
	if err != nil {
		t.Fatalf("render viz html: %v", err)
	}
	var embedded deployVisualizePayload
	if err := json.Unmarshal([]byte(extractVizData(t, embeddedHTML)), &embedded); err != nil {
		t.Fatalf("parse embedded payload: %v", err)
	}

	built, err := buildDeployVisualizePayload(result, nil)
	if err != nil {
		t.Fatalf("build payload: %v", err)
	}
	builtJSON, err := json.Marshal(built)
	if err != nil {
		t.Fatalf("marshal built payload: %v", err)
	}
	var roundTripped deployVisualizePayload
	if err := json.Unmarshal(builtJSON, &roundTripped); err != nil {
		t.Fatalf("round trip built payload: %v", err)
	}

	if roundTripped.Release != embedded.Release || roundTripped.Namespace != embedded.Namespace || roundTripped.Chart != embedded.Chart {
		t.Fatalf("mismatched payload identity: built=%+v embedded=%+v", roundTripped, embedded)
	}
	if len(roundTripped.Nodes) != len(embedded.Nodes) || len(roundTripped.Manifests) != len(embedded.Manifests) {
		t.Fatalf("mismatched payload shape: built=%+v embedded=%+v", roundTripped, embedded)
	}
	if roundTripped.ChangeKinds["prod|deployment|web"] != embedded.ChangeKinds["prod|deployment|web"] {
		t.Fatalf("mismatched changeKinds: built=%+v embedded=%+v", roundTripped.ChangeKinds, embedded.ChangeKinds)
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
	html, err := renderDeployVisualizeHTML(base, compare, deployVisualizeFeatures{})
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

func TestRenderDeployVisualizeHTMLWithExplainDiffFeature(t *testing.T) {
	result := &deployPlanResult{
		ReleaseName:   "demo",
		Namespace:     "prod",
		ChartRef:      "./chart",
		GraphNodes:    []deployGraphNode{{ID: "prod|deployment|web", Kind: "Deployment", Name: "web", Namespace: "prod"}},
		ManifestBlobs: map[string]string{"prod|deployment|web": "kind: Deployment\nmetadata:\n  name: web"},
		ManifestDiffs: map[string]string{"prod|deployment|web": "--- live\n+++ rendered\n+  image: nginx:2\n-  image: nginx:1\n"},
		Changes: []planResourceChange{
			{Key: resourceKey{Namespace: "prod", Kind: "Deployment", Name: "web"}, Kind: changeUpdate},
		},
	}
	html, err := renderDeployVisualizeHTML(result, nil, deployVisualizeFeatures{ExplainDiff: true})
	if err != nil {
		t.Fatalf("render viz html: %v", err)
	}
	if !strings.Contains(html, `id="ktlVizFeatures"`) {
		t.Fatalf("expected viz features block")
	}
	if !strings.Contains(html, `"explainDiff":true`) {
		t.Fatalf("expected explainDiff feature enabled")
	}
	if !strings.Contains(html, `id="manifestModeExplain"`) {
		t.Fatalf("expected explain button present in template")
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
