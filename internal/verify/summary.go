package verify

import "sort"

// BuildSummary computes summary stats for findings.
func BuildSummary(findings []Finding, blocked bool) Summary {
	summary := Summary{
		Total:          len(findings),
		BySev:          map[Severity]int{},
		ByRule:         map[string]int{},
		ByRuleSeverity: map[string]map[Severity]int{},
		Passed:         !blocked,
		Blocked:        blocked,
	}
	for _, f := range findings {
		summary.BySev[f.Severity]++
		if f.RuleID != "" {
			summary.ByRule[f.RuleID]++
			if summary.ByRuleSeverity[f.RuleID] == nil {
				summary.ByRuleSeverity[f.RuleID] = map[Severity]int{}
			}
			summary.ByRuleSeverity[f.RuleID][f.Severity]++
		}
	}
	return summary
}

func sortFindings(findings []Finding) {
	if len(findings) < 2 {
		return
	}
	sort.Slice(findings, func(i, j int) bool {
		fi, fj := findings[i], findings[j]
		if fi.Severity != fj.Severity {
			return severityRank(fi.Severity) < severityRank(fj.Severity)
		}
		if fi.RuleID != fj.RuleID {
			return fi.RuleID < fj.RuleID
		}
		if fi.ResourceKey != fj.ResourceKey {
			return fi.ResourceKey < fj.ResourceKey
		}
		if fi.Location != fj.Location {
			return fi.Location < fj.Location
		}
		return fi.Message < fj.Message
	})
}
