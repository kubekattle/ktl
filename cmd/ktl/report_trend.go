// report_trend.go handles 'ktl diag report trend', charting historical score data for recurring runs stored in S3 or disk.
package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/report"
	"github.com/spf13/cobra"
)

func newReportTrendCommand() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "trend",
		Short: "Summarize historical scorecard snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			entries, err := report.LoadScorecardTrend(days)
			if err != nil {
				return err
			}
			if len(entries) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No scorecard history recorded yet.")
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Scorecard trend (last %s)\n", windowLabel(days))
			printTrend(cmd, entries)
			return nil
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "Lookback window in days (0 shows all history)")
	return cmd
}

func printTrend(cmd *cobra.Command, entries []report.TrendEntry) {
	headers := []string{"Generated", "Average", "Worst Check", "Summary"}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		generated := entry.GeneratedAt.Local().Format(time.RFC3339)
		avg := fmt.Sprintf("%.1f%%", entry.Average)
		worst := summarizeWorst(entry.Checks)
		summary := summarizeChecks(entry.Checks)
		rows = append(rows, []string{generated, avg, worst, summary})
	}
	colWidths := make([]int, len(headers))
	for i, h := range headers {
		colWidths[i] = len(h)
	}
	for _, row := range rows {
		for i, col := range row {
			if len(col) > colWidths[i] {
				colWidths[i] = len(col)
			}
		}
	}
	out := cmd.OutOrStdout()
	for i, h := range headers {
		fmt.Fprintf(out, "%-*s  ", colWidths[i], strings.ToUpper(h))
	}
	fmt.Fprintln(out)
	for i := range headers {
		fmt.Fprintf(out, "%s  ", strings.Repeat("-", colWidths[i]))
	}
	fmt.Fprintln(out)
	for _, row := range rows {
		for i, col := range row {
			fmt.Fprintf(out, "%-*s  ", colWidths[i], col)
		}
		fmt.Fprintln(out)
	}
}

func summarizeWorst(checks []report.ScoreCheck) string {
	if len(checks) == 0 {
		return "-"
	}
	sorted := append([]report.ScoreCheck(nil), checks...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Score < sorted[j].Score })
	worst := sorted[0]
	return fmt.Sprintf("%s (%.1f%%)", worst.Name, worst.Score)
}

func summarizeChecks(checks []report.ScoreCheck) string {
	if len(checks) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(checks))
	for _, check := range checks {
		parts = append(parts, fmt.Sprintf("%s:%s", check.Key, strings.ToUpper(string(check.Status))))
	}
	return strings.Join(parts, " ")
}

func windowLabel(days int) string {
	if days <= 0 {
		return "all history"
	}
	if days == 1 {
		return "24h"
	}
	return fmt.Sprintf("%dd", days)
}
