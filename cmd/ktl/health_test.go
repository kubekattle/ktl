package main

import (
	"testing"

	"github.com/example/ktl/internal/report"
)

func TestParseFailOnMode(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    failOnMode
		wantErr bool
	}{
		{"empty defaults to fail", "", failFail, false},
		{"fail explicit", "fail", failFail, false},
		{"warn", "warn", failWarn, false},
		{"never", "never", failNever, false},
		{"none alias", "none", failNever, false},
		{"unknown", "nope", failFail, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseFailOnMode(tt.input)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %q", tt.input)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if err == nil && got != tt.want {
				t.Fatalf("parseFailOnMode(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}

func TestFailOnModeShouldFail(t *testing.T) {
	tests := []struct {
		name     string
		mode     failOnMode
		failures int
		warnings int
		want     bool
	}{
		{"never never fails", failNever, 2, 3, false},
		{"warn trips on warning", failWarn, 0, 1, true},
		{"warn trips on failure", failWarn, 1, 0, true},
		{"fail ignores warnings", failFail, 0, 2, false},
		{"fail trips on failure", failFail, 1, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.mode.shouldFail(tt.failures, tt.warnings)
			if got != tt.want {
				t.Fatalf("mode %v shouldFail(%d,%d) = %v, want %v", tt.mode, tt.failures, tt.warnings, got, tt.want)
			}
		})
	}
}

func TestStatusBadge(t *testing.T) {
	if got := statusBadge(report.ScoreStatusPass); got != "PASS" {
		t.Fatalf("PASS badge mismatch: %s", got)
	}
	if got := statusBadge(report.ScoreStatusWarn); got != "WARN" {
		t.Fatalf("WARN badge mismatch: %s", got)
	}
	if got := statusBadge(report.ScoreStatusFail); got != "FAIL" {
		t.Fatalf("FAIL badge mismatch: %s", got)
	}
	if got := statusBadge(report.ScoreStatus("mystery")); got != "UNKNOWN" {
		t.Fatalf("UNKNOWN badge mismatch: %s", got)
	}
}

func TestScoreText(t *testing.T) {
	check := report.ScoreCheck{Score: 87.432, Status: report.ScoreStatusPass}
	if got := scoreText(check); got != "87.4%" {
		t.Fatalf("unexpected score text: %s", got)
	}
	check.Status = report.ScoreStatusUnknown
	if got := scoreText(check); got != "-" {
		t.Fatalf("unknown score text mismatch: %s", got)
	}
}
