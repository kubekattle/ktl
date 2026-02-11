package stack

import (
	"fmt"
	"strings"
)

type AdaptiveConcurrencyOptions struct {
	Min int

	// WindowSize controls how many recent outcomes influence shrink/ramp.
	WindowSize int

	// RampAfterSuccesses controls how many clean successes are required before
	// increasing concurrency by 1.
	RampAfterSuccesses int

	// RampMaxFailureRate blocks ramp-up when the recent failure rate exceeds this.
	RampMaxFailureRate float64

	// CooldownSuccessesByClass controls how many subsequent successes must pass
	// (without ramping) after a failure of a given class.
	CooldownSuccessesByClass map[string]int
}

func (o AdaptiveConcurrencyOptions) withDefaults() AdaptiveConcurrencyOptions {
	if o.Min < 1 {
		o.Min = 1
	}
	if o.WindowSize < 4 {
		o.WindowSize = 20
	}
	if o.RampAfterSuccesses < 1 {
		o.RampAfterSuccesses = 2
	}
	if o.RampMaxFailureRate <= 0 {
		o.RampMaxFailureRate = 0.30
	}
	if o.CooldownSuccessesByClass == nil {
		o.CooldownSuccessesByClass = map[string]int{
			"RATE_LIMIT":  4,
			"SERVER_5XX":  4,
			"UNAVAILABLE": 4,
			"TIMEOUT":     3,
			"TRANSPORT":   3,
			"CONFLICT":    1,
			"OTHER":       1,
		}
	}
	return o
}

type AdaptiveConcurrency struct {
	Target int
	Max    int
	Min    int

	opts AdaptiveConcurrencyOptions

	cooldownSuccesses int
	successStreak     int

	window       []string
	windowIndex  int
	windowFilled bool
}

func NewAdaptiveConcurrency(max int) *AdaptiveConcurrency {
	return NewAdaptiveConcurrencyWithOptions(max, AdaptiveConcurrencyOptions{})
}

func NewAdaptiveConcurrencyWithOptions(max int, opts AdaptiveConcurrencyOptions) *AdaptiveConcurrency {
	if max < 1 {
		max = 1
	}
	opts = opts.withDefaults()
	min := opts.Min
	if min > max {
		min = max
	}
	return &AdaptiveConcurrency{
		Target: min,
		Max:    max,
		Min:    min,
		opts:   opts,
		window: make([]string, opts.WindowSize),
	}
}

func (a *AdaptiveConcurrency) OnSuccess() (changed bool, reason string) {
	if a == nil {
		return false, ""
	}
	a.pushOutcome("SUCCESS")
	if a.cooldownSuccesses > 0 {
		a.cooldownSuccesses--
		a.successStreak = 0
		return false, "cooldown"
	}
	a.successStreak++
	if a.successStreak < a.opts.RampAfterSuccesses {
		return false, "success"
	}
	a.successStreak = 0

	if a.Target >= a.Max {
		return false, "at-max"
	}
	if a.failureRate() > a.opts.RampMaxFailureRate {
		return false, "dirty-window"
	}
	if a.windowCount("RATE_LIMIT") > 0 || a.windowCount("SERVER_5XX") > 0 {
		return false, "dirty-window"
	}
	a.Target++
	return true, "ramp-up"
}

func (a *AdaptiveConcurrency) OnFailure(class string) (changed bool, reason string) {
	if a == nil {
		return false, ""
	}
	class = strings.TrimSpace(class)
	if class == "" {
		class = "OTHER"
	}
	a.pushOutcome(class)
	a.successStreak = 0

	old := a.Target
	switch class {
	case "RATE_LIMIT", "SERVER_5XX", "UNAVAILABLE":
		a.Target = maxInt(a.Min, a.Target/2)
		a.cooldownSuccesses = a.cooldownFor(class)
		reason = fmt.Sprintf("shrink:%s", class)
	case "TIMEOUT", "TRANSPORT":
		// Shrink only when the window shows repeated turbulence.
		if a.windowCount(class) >= 2 || a.failureRate() >= 0.30 {
			a.Target = maxInt(a.Min, a.Target-1)
			reason = fmt.Sprintf("shrink:%s", class)
		} else {
			reason = fmt.Sprintf("no-change:%s", class)
		}
		a.cooldownSuccesses = a.cooldownFor(class)
	case "CONFLICT":
		// Shrink mildly only if conflicts repeat.
		if a.windowCount(class) >= 2 {
			a.Target = maxInt(a.Min, a.Target-1)
			reason = fmt.Sprintf("shrink:%s", class)
		} else {
			reason = fmt.Sprintf("no-change:%s", class)
		}
		a.cooldownSuccesses = a.cooldownFor(class)
	default:
		a.cooldownSuccesses = maxInt(a.cooldownSuccesses, a.cooldownFor("OTHER"))
		reason = fmt.Sprintf("no-change:%s", class)
	}
	return a.Target != old, reason
}

func (a *AdaptiveConcurrency) cooldownFor(class string) int {
	if a == nil {
		return 0
	}
	if a.opts.CooldownSuccessesByClass == nil {
		return 0
	}
	if v, ok := a.opts.CooldownSuccessesByClass[class]; ok {
		return v
	}
	if v, ok := a.opts.CooldownSuccessesByClass["OTHER"]; ok {
		return v
	}
	return 0
}

func (a *AdaptiveConcurrency) pushOutcome(outcome string) {
	if a == nil || len(a.window) == 0 {
		return
	}
	a.window[a.windowIndex] = outcome
	a.windowIndex++
	if a.windowIndex >= len(a.window) {
		a.windowIndex = 0
		a.windowFilled = true
	}
}

func (a *AdaptiveConcurrency) windowCount(class string) int {
	if a == nil || len(a.window) == 0 {
		return 0
	}
	n := len(a.window)
	if !a.windowFilled {
		n = a.windowIndex
	}
	total := 0
	for i := 0; i < n; i++ {
		if a.window[i] == class {
			total++
		}
	}
	return total
}

func (a *AdaptiveConcurrency) failureRate() float64 {
	if a == nil || len(a.window) == 0 {
		return 0
	}
	n := len(a.window)
	if !a.windowFilled {
		n = a.windowIndex
	}
	if n == 0 {
		return 0
	}
	fail := 0
	for i := 0; i < n; i++ {
		if a.window[i] != "" && a.window[i] != "SUCCESS" {
			fail++
		}
	}
	return float64(fail) / float64(n)
}

func (a *AdaptiveConcurrency) SnapshotString() string {
	if a == nil {
		return ""
	}
	return fmt.Sprintf("target=%d min=%d max=%d cooldown=%d failureRate=%.2f window(rate_limit=%d conflict=%d timeout=%d transport=%d)",
		a.Target, a.Min, a.Max, a.cooldownSuccesses, a.failureRate(),
		a.windowCount("RATE_LIMIT"), a.windowCount("CONFLICT"), a.windowCount("TIMEOUT"), a.windowCount("TRANSPORT"))
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
