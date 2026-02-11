// File: cmd/ktl/list_test.go
// Brief: Tests for 'ktl list' helpers.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"bytes"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/release"
	helmtime "helm.sh/helm/v3/pkg/time"
)

func TestReleaseListElementsFormatsFields(t *testing.T) {
	deployedAt := time.Date(2025, 1, 2, 3, 4, 5, 0, time.UTC)
	rel := &release.Release{
		Name:      "web-prod",
		Namespace: "prod",
		Version:   7,
		Info: &release.Info{
			Status:       release.StatusDeployed,
			LastDeployed: helmtime.Time{Time: deployedAt},
		},
		Chart: &chart.Chart{
			Metadata: &chart.Metadata{
				Name:       "web",
				Version:    "1.2.3",
				AppVersion: "2025.01.02",
			},
		},
	}

	els := releaseListElements([]*release.Release{rel}, "2006-01-02")
	if len(els) != 1 {
		t.Fatalf("expected 1 element, got %d", len(els))
	}
	got := els[0]
	if got.Name != "web-prod" || got.Namespace != "prod" {
		t.Fatalf("unexpected element identity: %+v", got)
	}
	if got.Revision != "7" {
		t.Fatalf("expected revision 7, got %q", got.Revision)
	}
	if got.Updated != "2025-01-02" {
		t.Fatalf("expected updated date, got %q", got.Updated)
	}
	if got.Status != "deployed" {
		t.Fatalf("expected status deployed, got %q", got.Status)
	}
	if got.Chart != "web-1.2.3" {
		t.Fatalf("expected chart web-1.2.3, got %q", got.Chart)
	}
	if got.AppVersion != "2025.01.02" {
		t.Fatalf("expected app version, got %q", got.AppVersion)
	}
}

func TestWriteReleaseListTableDoesNotColorWhenDisabled(t *testing.T) {
	rel := &release.Release{
		Name:      "web-prod",
		Namespace: "prod",
		Version:   1,
		Info: &release.Info{
			Status: release.StatusFailed,
		},
	}
	var buf bytes.Buffer
	if err := writeReleaseListTable(&buf, []*release.Release{rel}, "", false, false); err != nil {
		t.Fatalf("write table: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "NAME") || !strings.Contains(out, "web-prod") {
		t.Fatalf("expected headers and row, got: %q", out)
	}
	if strings.Contains(out, "\x1b[") {
		t.Fatalf("expected no ANSI escapes, got: %q", out)
	}
}

func TestColorizeReleaseStatusAddsANSI(t *testing.T) {
	if os.Getenv("NO_COLOR") != "" {
		t.Skip("NO_COLOR set; skip ANSI assertions")
	}
	prev := color.NoColor
	color.NoColor = false
	t.Cleanup(func() { color.NoColor = prev })

	colored := colorizeReleaseStatus("deployed")
	if colored == "deployed" {
		t.Fatalf("expected colorized string, got %q", colored)
	}
	if !strings.Contains(colored, "\x1b[") {
		t.Fatalf("expected ANSI escape sequence, got %q", colored)
	}
}
