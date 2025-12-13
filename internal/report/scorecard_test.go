// scorecard_test.go covers score calculations and breach detection logic.
package report

import "testing"

func TestStatusForScore(t *testing.T) {
	tests := []struct {
		score float64
		want  ScoreStatus
	}{
		{95, ScoreStatusPass},
		{80, ScoreStatusWarn},
		{50, ScoreStatusFail},
		{-1, ScoreStatusUnknown},
	}
	for _, tt := range tests {
		if got := statusForScore(tt.score); got != tt.want {
			t.Fatalf("statusForScore(%v)=%v want %v", tt.score, got, tt.want)
		}
	}
}

func TestScorecardBreaches(t *testing.T) {
	card := Scorecard{
		Checks: []ScoreCheck{
			{Name: "ok", Score: 90, Status: ScoreStatusPass},
			{Name: "warn", Score: 70, Status: ScoreStatusFail},
			{Name: "unknown", Score: 0, Status: ScoreStatusUnknown},
		},
	}
	breaches := card.Breaches(80)
	if len(breaches) != 1 || breaches[0].Name != "warn" {
		t.Fatalf("unexpected breaches: %+v", breaches)
	}
}

func TestCommandFor(t *testing.T) {
	cmd := commandFor("ktl diag podsecurity", []string{"prod", "dev"})
	want := "ktl diag podsecurity -n prod -n dev"
	if cmd != want {
		t.Fatalf("commandFor mismatch: %s != %s", cmd, want)
	}
}

func TestEncodeTrend(t *testing.T) {
	got := encodeTrend([]float64{10, 12.345, 9})
	if got != "10.00,12.35,9.00" {
		t.Fatalf("encodeTrend unexpected: %s", got)
	}
}

func TestRound1(t *testing.T) {
	if round1(1.26) != 1.3 {
		t.Fatalf("round1 failed for 1.26")
	}
	if round1(-1.24) != -1.2 {
		t.Fatalf("round1 failed for -1.24")
	}
}
