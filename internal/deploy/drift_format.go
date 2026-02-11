package deploy

import (
	"fmt"
	"strings"
)

func FormatDriftReport(report DriftReport, maxItems int, maxDiffLines int) string {
	if len(report.Items) == 0 {
		return ""
	}
	if maxItems <= 0 {
		maxItems = 6
	}
	if maxDiffLines <= 0 {
		maxDiffLines = 80
	}
	changed := 0
	missing := 0
	errors := 0
	for _, it := range report.Items {
		switch strings.TrimSpace(it.Reason) {
		case "changed":
			changed++
		case "missing":
			missing++
		default:
			if strings.HasPrefix(strings.TrimSpace(it.Reason), "fetch_error:") {
				errors++
			} else {
				changed++
			}
		}
	}
	items := report.Items
	if len(items) > maxItems {
		items = items[:maxItems]
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Summary: %d changed, %d missing, %d errors\n", changed, missing, errors)
	for _, it := range items {
		ns := strings.TrimSpace(it.Namespace)
		if ns == "" {
			ns = "-"
		}
		fmt.Fprintf(&b, "- %s/%s (ns: %s): %s\n", strings.TrimSpace(it.Kind), strings.TrimSpace(it.Name), ns, strings.TrimSpace(it.Reason))
		diff := strings.TrimSpace(it.Diff)
		if diff == "" {
			continue
		}
		lines := strings.Split(diff, "\n")
		if len(lines) > maxDiffLines {
			lines = lines[:maxDiffLines]
		}
		for _, line := range lines {
			fmt.Fprintf(&b, "  %s\n", line)
		}
		if strings.Count(diff, "\n")+1 > maxDiffLines {
			fmt.Fprintf(&b, "  (diff truncated; showing first %d lines)\n", maxDiffLines)
		}
	}
	if len(report.Items) > len(items) {
		fmt.Fprintf(&b, "(and %d more)\n", len(report.Items)-len(items))
	}
	return strings.TrimRight(b.String(), "\n")
}
