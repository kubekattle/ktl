package verify

import (
	"context"
	"os"
	"path/filepath"
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
	baseDir := strings.TrimSpace(opts.RulesDir)
	if baseDir == "" {
		baseDir = strings.TrimSpace(r.RulesDir)
	}
	paths := []string{baseDir}
	for _, p := range opts.ExtraRules {
		p = strings.TrimSpace(p)
		if p != "" {
			paths = append(paths, p)
		}
	}
	if env := strings.TrimSpace(os.Getenv("KTL_VERIFY_RULES_PATH")); env != "" {
		for _, p := range splitList(env) {
			if p != "" {
				paths = append(paths, p)
			}
		}
	}
	paths = dedupeStrings(paths)
	opts.RulesDir = baseDir

	rulesetHash, _ := RulesetDigestMulti(paths)
	rulesetLabel := nonEmpty("builtin@"+rulesetHash, "builtin")
	if emit != nil {
		_ = emit(Event{
			Type:    EventStarted,
			When:    time.Now().UTC(),
			Target:  strings.TrimSpace(target),
			Ruleset: rulesetLabel,
		})
	}

	rules, err := LoadRuleset(paths...)
	if err != nil {
		return nil, err
	}
	commonDirs := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		p = strings.TrimSuffix(p, "/")
		p = strings.TrimSuffix(p, "\\")
		p = strings.TrimSpace(p)
		commonDirs = append(commonDirs, filepath.Join(p, "lib"))
	}

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

	ruleFindings, err := EvaluateRules(ctx, rules, objects, commonDirs)
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
