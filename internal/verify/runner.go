package verify

import (
	"context"
	"strings"
	"time"
)

type EventType string

const (
	EventReset    EventType = "reset"
	EventStarted  EventType = "started"
	EventProgress EventType = "progress"
	EventFinding  EventType = "finding"
	EventSummary  EventType = "summary"
	EventDone     EventType = "done"
)

type Event struct {
	Type       EventType
	When       time.Time
	Phase      string
	Counts     map[string]int
	Finding    *Finding
	Summary    *Summary
	Passed     bool
	Blocked    bool
	Target     string
	Ruleset    string
	PolicyRef  string
	PolicyMode string
}

type Emitter func(Event) error

type Runner struct {
	RulesDir string
}

func (r Runner) Verify(ctx context.Context, target string, objects []map[string]any, opts Options, emit Emitter) (*Report, error) {
	if opts.Mode == ModeOff {
		rep := &Report{
			Tool:        "ktl-verify",
			Engine:      EngineMeta{Name: "builtin", Ruleset: "off"},
			Mode:        opts.Mode,
			Passed:      true,
			EvaluatedAt: time.Now().UTC(),
			Summary:     Summary{Total: 0, BySev: map[Severity]int{}, Passed: true, Blocked: false},
		}
		return rep, nil
	}
	rulesDir := strings.TrimSpace(opts.RulesDir)
	if rulesDir == "" {
		rulesDir = strings.TrimSpace(r.RulesDir)
	}
	opts.RulesDir = rulesDir

	rulesetHash, _ := RulesetDigest(rulesDir)
	rulesetLabel := nonEmpty("builtin@"+rulesetHash, "builtin")
	if emit != nil {
		_ = emit(Event{
			Type:    EventStarted,
			When:    time.Now().UTC(),
			Target:  strings.TrimSpace(target),
			Ruleset: rulesetLabel,
		})
	}

	rules, err := LoadRuleset(rulesDir)
	if err != nil {
		return nil, err
	}
	commonDir := strings.TrimSpace(rulesDir)
	commonDir = strings.TrimSuffix(commonDir, "/")
	commonDir = strings.TrimSuffix(commonDir, "\\")
	commonDir = strings.TrimSpace(commonDir)
	commonDir = commonDir + "/lib"

	rep := &Report{
		Tool:        "ktl-verify",
		Engine:      EngineMeta{Name: "builtin", Ruleset: rulesetLabel},
		Mode:        opts.Mode,
		EvaluatedAt: time.Now().UTC(),
		Summary:     Summary{BySev: map[Severity]int{}},
	}

	var findings []Finding
	emitFinding := func(f Finding) {
		findings = append(findings, f)
		rep.Summary.Total++
		rep.Summary.BySev[f.Severity]++
		if emit != nil {
			ff := f
			_ = emit(Event{Type: EventFinding, When: time.Now().UTC(), Finding: &ff})
		}
	}

	if emit != nil {
		_ = emit(Event{Type: EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
	}

	ruleFindings, err := EvaluateRules(ctx, rules, objects, commonDir)
	if err != nil {
		return nil, err
	}
	for _, f := range filterNamespacedFindings(ruleFindings) {
		emitFinding(f)
	}

	blocked := opts.Mode == ModeBlock && hasAtLeast(findings, opts.FailOn)
	rep.Blocked = blocked
	rep.Passed = !blocked
	rep.Summary.Blocked = rep.Blocked
	rep.Summary.Passed = rep.Passed
	rep.Findings = findings

	if emit != nil {
		s := rep.Summary
		_ = emit(Event{Type: EventSummary, When: time.Now().UTC(), Summary: &s})
		_ = emit(Event{Type: EventDone, When: time.Now().UTC(), Passed: rep.Passed, Blocked: rep.Blocked})
	}
	return rep, nil
}
