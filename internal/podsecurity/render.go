// render.go formats PodSecurity assessment data into the table output used by 'ktl diag podsecurity'.
package podsecurity

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
)

// RenderOptions controls thresholding (currently unused but reserved for future tuning).
type RenderOptions struct{}

// PrintReport renders namespace PodSecurity labels and violation summaries.
func PrintReport(summaries []NamespaceSummary, _ RenderOptions) {
	if len(summaries) == 0 {
		fmt.Println("No namespaces matched the supplied filters.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tENFORCE\tAUDIT\tWARN\tVIOLATIONS")
	for _, summary := range summaries {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n",
			summary.Namespace,
			formatPolicy(summary.Labels.Enforce, summary.Labels.EnforceVersion),
			formatPolicy(summary.Labels.Audit, summary.Labels.AuditVersion),
			formatPolicy(summary.Labels.Warn, summary.Labels.WarnVersion),
			formatViolations(summary.Findings),
		)
	}
	_ = tw.Flush()
	printFindingsDetail(summaries)
}

func formatPolicy(level, version string) string {
	if level == "" {
		return "-"
	}
	if version == "" {
		return level
	}
	return fmt.Sprintf("%s (%s)", level, version)
}

func formatViolations(findings []Finding) string {
	if len(findings) == 0 {
		return "OK"
	}
	counts := map[string]int{
		"ENFORCE": 0,
		"WARN":    0,
		"AUDIT":   0,
		"INFO":    0,
	}
	for _, finding := range findings {
		counts[strings.ToUpper(finding.Action)]++
	}
	var parts []string
	if counts["ENFORCE"] > 0 {
		parts = append(parts, colorize(fmt.Sprintf("BLOCK %d", counts["ENFORCE"]), "ENFORCE"))
	}
	if counts["WARN"] > 0 {
		parts = append(parts, colorize(fmt.Sprintf("WARN %d", counts["WARN"]), "WARN"))
	}
	if counts["AUDIT"] > 0 {
		parts = append(parts, colorize(fmt.Sprintf("AUDIT %d", counts["AUDIT"]), "AUDIT"))
	}
	if counts["INFO"] > 0 {
		parts = append(parts, fmt.Sprintf("INFO %d", counts["INFO"]))
	}
	return strings.Join(parts, ", ")
}

func colorize(text, category string) string {
	if color.NoColor {
		return text
	}
	switch category {
	case "ENFORCE":
		return color.New(color.FgHiRed).Sprint(text)
	case "WARN":
		return color.New(color.FgYellow).Sprint(text)
	case "AUDIT":
		return color.New(color.FgCyan).Sprint(text)
	default:
		return text
	}
}

func printFindingsDetail(summaries []NamespaceSummary) {
	printedHeader := false
	for _, summary := range summaries {
		if len(summary.Findings) == 0 {
			continue
		}
		if !printedHeader {
			fmt.Println("\nFindings:")
			printedHeader = true
		}
		fmt.Printf("\nNamespace %s\n", summary.Namespace)
		for _, finding := range summary.Findings {
			target := fmt.Sprintf("%s/%s", summary.Namespace, finding.Pod)
			if finding.Container != "" {
				target = fmt.Sprintf("%s:%s", target, finding.Container)
			}
			fmt.Printf("  - [%s/%s] %s -> %s\n", strings.ToUpper(finding.Action), finding.Level, target, finding.Reason)
		}
	}
	if printedHeader {
		fmt.Println("\nLegend: BLOCK=will be denied by enforce policy, WARN=surfaced to users, AUDIT=recorded in audit log, INFO=no label would catch it yet.")
	}
}
