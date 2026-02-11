package verify

import (
	"fmt"
	"io"
	"sort"
	"strings"
	"time"
)

func writeMarkdown(w io.Writer, rep *Report) error {
	if w == nil || rep == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("# ktl verify report\n\n")
	b.WriteString(fmt.Sprintf("- Evaluated: %s\n", rep.EvaluatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Mode: %s\n", rep.Mode))
	b.WriteString(fmt.Sprintf("- Passed: %t\n", rep.Passed))
	b.WriteString(fmt.Sprintf("- Blocked: %t\n", rep.Blocked))
	if strings.TrimSpace(rep.Engine.Ruleset) != "" {
		b.WriteString(fmt.Sprintf("- Ruleset: %s\n", strings.TrimSpace(rep.Engine.Ruleset)))
	}
	b.WriteString(fmt.Sprintf("- Total findings: %d\n", rep.Summary.Total))

	if len(rep.Summary.BySev) > 0 {
		b.WriteString("\n## By severity\n\n")
		writeMarkdownSeverityTable(&b, rep.Summary.BySev)
	}

	if len(rep.Findings) == 0 {
		b.WriteString("\n_No findings._\n")
		_, _ = w.Write([]byte(b.String()))
		return nil
	}

	b.WriteString("\n## Findings\n\n")
	b.WriteString("| Severity | Rule | Resource | Message | Location | Expected | Observed | Help |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range rep.Findings {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s | %s |\n",
			strings.ToUpper(string(f.Severity)),
			mdEscape(f.RuleID),
			mdEscape(findingResource(f)),
			mdEscape(f.Message),
			mdEscape(findingLocation(f)),
			mdEscape(f.Expected),
			mdEscape(f.Observed),
			mdEscape(f.HelpURL),
		))
	}
	_, _ = w.Write([]byte(b.String()))
	return nil
}

func writeMarkdownSeverityTable(b *strings.Builder, bySev map[Severity]int) {
	if b == nil {
		return
	}
	sevs := make([]Severity, 0, len(bySev))
	for sev := range bySev {
		sevs = append(sevs, sev)
	}
	sort.Slice(sevs, func(i, j int) bool { return severityRank(sevs[i]) < severityRank(sevs[j]) })
	b.WriteString("| Severity | Count |\n")
	b.WriteString("| --- | --- |\n")
	for _, sev := range sevs {
		b.WriteString(fmt.Sprintf("| %s | %d |\n", strings.ToUpper(string(sev)), bySev[sev]))
	}
}

func mdEscape(value string) string {
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}

func findingResource(f Finding) string {
	if f.ResourceKey != "" {
		return f.ResourceKey
	}
	return resourceKey(f.Subject)
}

func findingLocation(f Finding) string {
	if f.Path != "" {
		return f.Path
	}
	if strings.TrimSpace(f.FieldPath) != "" {
		return strings.TrimSpace(f.FieldPath)
	}
	return f.Location
}
