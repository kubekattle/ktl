package verify

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type Delta struct {
	NewOrChanged []Finding
	Unchanged    int
}

func LoadReport(path string) (*Report, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rep Report
	if err := json.Unmarshal(raw, &rep); err != nil {
		return nil, err
	}
	return &rep, nil
}

func ComputeDelta(current *Report, baseline *Report) Delta {
	if current == nil {
		return Delta{}
	}
	base := map[string]Finding{}
	if baseline != nil {
		for _, f := range baseline.Findings {
			fp := strings.TrimSpace(f.Fingerprint)
			if fp == "" {
				fp = fallbackFingerprint(f)
			}
			base[fp] = f
		}
	}
	var changed []Finding
	unchanged := 0
	for _, f := range current.Findings {
		fp := strings.TrimSpace(f.Fingerprint)
		if fp == "" {
			fp = fallbackFingerprint(f)
		}
		prev, ok := base[fp]
		if !ok || prev.Severity != f.Severity {
			changed = append(changed, f)
			continue
		}
		unchanged++
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Severity != changed[j].Severity {
			return severityRank(changed[i].Severity) < severityRank(changed[j].Severity)
		}
		return changed[i].RuleID < changed[j].RuleID
	})
	return Delta{NewOrChanged: changed, Unchanged: unchanged}
}

func fallbackFingerprint(f Finding) string {
	return strings.Join([]string{
		f.RuleID,
		f.Subject.Kind,
		f.Subject.Namespace,
		f.Subject.Name,
		f.Location,
	}, "|")
}
