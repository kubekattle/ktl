package verify

import (
	"encoding/json"
	"os"
	"sort"
	"strings"
)

type Delta struct {
	NewOrChanged []Finding
	Fixed        []Finding
	Unchanged    int

	NewOrChangedDetails []DeltaDetail
	FixedDetails        []DeltaDetail
}

// DeltaDetail describes why a finding is considered new/changed/fixed when
// comparing the current report against a baseline.
//
// It is designed for UX consumers (HTML report, PR comments) to show a concise
// change narrative without having to re-derive it client-side.
type DeltaDetail struct {
	Kind     string   `json:"kind,omitempty"` // new|changed|fixed
	Changes  []string `json:"changes,omitempty"`
	Current  *Finding `json:"current,omitempty"`
	Baseline *Finding `json:"baseline,omitempty"`
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
	baseByFP := map[string]Finding{}
	baseByID := map[string]Finding{}
	if baseline != nil {
		for _, f := range baseline.Findings {
			fp := strings.TrimSpace(f.Fingerprint)
			if fp == "" {
				fp = fallbackFingerprint(f)
			}
			baseByFP[fp] = f
			id := identityKey(f)
			if id != "" {
				// First wins for determinism.
				if _, ok := baseByID[id]; !ok {
					baseByID[id] = f
				}
			}
		}
	}
	curByFP := map[string]Finding{}
	curByID := map[string]Finding{}
	for _, f := range current.Findings {
		fp := strings.TrimSpace(f.Fingerprint)
		if fp == "" {
			fp = fallbackFingerprint(f)
		}
		curByFP[fp] = f
		id := identityKey(f)
		if id != "" {
			if _, ok := curByID[id]; !ok {
				curByID[id] = f
			}
		}
	}
	var changed []Finding
	var changedDetails []DeltaDetail
	unchanged := 0
	for _, f := range current.Findings {
		fp := strings.TrimSpace(f.Fingerprint)
		if fp == "" {
			fp = fallbackFingerprint(f)
		}
		if prev, ok := baseByFP[fp]; ok && prev.Severity == f.Severity {
			unchanged++
			continue
		}

		kind := "new"
		var prev *Finding
		if b, ok := baseByID[identityKey(f)]; ok {
			kind = "changed"
			tmp := b
			prev = &tmp
		}

		changes := []string{}
		if kind == "changed" && prev != nil {
			changes = append(changes, diffFinding(*prev, f)...)
			if len(changes) == 0 {
				// Fallback: identity matches but we couldn't pinpoint which field
				// changed (maybe only fingerprint logic changed).
				changes = []string{"content"}
			}
		}

		curCopy := f
		changed = append(changed, f)
		changedDetails = append(changedDetails, DeltaDetail{
			Kind:     kind,
			Changes:  changes,
			Current:  &curCopy,
			Baseline: prev,
		})
	}
	var fixed []Finding
	var fixedDetails []DeltaDetail
	if baseline != nil {
		for fp, f := range baseByFP {
			if _, ok := curByFP[fp]; ok {
				continue
			}
			fixed = append(fixed, f)
		}
		// Include identity-based fixed details so the UI can explain what was
		// removed even if fingerprinting changes in the future.
		for id, bf := range baseByID {
			if _, ok := curByID[id]; ok {
				continue
			}
			tmp := bf
			fixedDetails = append(fixedDetails, DeltaDetail{
				Kind:     "fixed",
				Changes:  []string{"removed"},
				Current:  nil,
				Baseline: &tmp,
			})
		}
	}
	sort.Slice(changed, func(i, j int) bool {
		if changed[i].Severity != changed[j].Severity {
			return severityRank(changed[i].Severity) < severityRank(changed[j].Severity)
		}
		return changed[i].RuleID < changed[j].RuleID
	})
	sort.Slice(fixed, func(i, j int) bool {
		if fixed[i].Severity != fixed[j].Severity {
			return severityRank(fixed[i].Severity) < severityRank(fixed[j].Severity)
		}
		return fixed[i].RuleID < fixed[j].RuleID
	})
	// Keep details in the same order as the changed slice for predictable UX.
	sort.Slice(changedDetails, func(i, j int) bool {
		ci, cj := changedDetails[i].Current, changedDetails[j].Current
		if ci != nil && cj != nil && ci.Severity != cj.Severity {
			return severityRank(ci.Severity) < severityRank(cj.Severity)
		}
		ri, rj := "", ""
		if ci != nil {
			ri = ci.RuleID
		}
		if cj != nil {
			rj = cj.RuleID
		}
		return ri < rj
	})
	sort.Slice(fixedDetails, func(i, j int) bool {
		bi, bj := fixedDetails[i].Baseline, fixedDetails[j].Baseline
		if bi != nil && bj != nil && bi.Severity != bj.Severity {
			return severityRank(bi.Severity) < severityRank(bj.Severity)
		}
		ri, rj := "", ""
		if bi != nil {
			ri = bi.RuleID
		}
		if bj != nil {
			rj = bj.RuleID
		}
		return ri < rj
	})

	return Delta{
		NewOrChanged:        changed,
		Fixed:               fixed,
		Unchanged:           unchanged,
		NewOrChangedDetails: changedDetails,
		FixedDetails:        fixedDetails,
	}
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

func identityKey(f Finding) string {
	return strings.Join([]string{
		strings.TrimSpace(f.RuleID),
		strings.TrimSpace(f.Subject.Kind),
		strings.TrimSpace(f.Subject.Namespace),
		strings.TrimSpace(f.Subject.Name),
		strings.TrimSpace(nonEmpty(strings.TrimSpace(f.FieldPath), strings.TrimSpace(f.Location))),
	}, "|")
}

func diffFinding(prev Finding, cur Finding) []string {
	var out []string
	if prev.Severity != cur.Severity {
		out = append(out, "severity")
	}
	if strings.TrimSpace(prev.Message) != strings.TrimSpace(cur.Message) {
		out = append(out, "message")
	}
	if strings.TrimSpace(prev.Expected) != strings.TrimSpace(cur.Expected) {
		out = append(out, "expected")
	}
	if strings.TrimSpace(prev.Observed) != strings.TrimSpace(cur.Observed) {
		out = append(out, "observed")
	}
	if strings.TrimSpace(prev.HelpURL) != strings.TrimSpace(cur.HelpURL) {
		out = append(out, "help")
	}
	if strings.TrimSpace(prev.Path) != strings.TrimSpace(cur.Path) || prev.Line != cur.Line {
		out = append(out, "source")
	}
	return out
}
