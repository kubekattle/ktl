// render.go formats quota results into human-readable tables.
package quota

import (
	"fmt"
	"io"
	"math"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RenderOptions tune the rendering thresholds.
type RenderOptions struct {
	WarnThreshold  float64
	BlockThreshold float64
}

// PrintTable writes a human friendly table of quota summaries.
func PrintTable(summaries []Summary, opts RenderOptions) {
	if len(summaries) == 0 {
		fmt.Println("No namespaces matched the supplied filters.")
		return
	}
	warn := opts.WarnThreshold
	if warn <= 0 {
		warn = 0.80
	}
	block := opts.BlockThreshold
	if block <= 0 {
		block = 1.0
	}

	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAMESPACE\tPODS\tCPU (requests)\tMEMORY (requests)\tPVCS\tSTATUS")
	for _, summary := range summaries {
		row := renderRow(summary, warn, block)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\n",
			summary.Namespace,
			row.Pods,
			row.CPU,
			row.Memory,
			row.PVCs,
			row.Status,
		)
	}
	_ = tw.Flush()
	printLimitRanges(summaries)
}

type renderedRow struct {
	Pods   string
	CPU    string
	Memory string
	PVCs   string
	Status string
}

func renderRow(summary Summary, warn, block float64) renderedRow {
	var row renderedRow
	statusLevel := levelOK
	var reasons []string

	pods := renderMetric(summary.Pods, warn, block, formatCount)
	row.Pods = pods.Text
	statusLevel, reasons = aggregateStatus(statusLevel, reasons, pods, "pods")

	cpu := renderMetric(summary.CPU, warn, block, formatCPU)
	row.CPU = cpu.Text
	statusLevel, reasons = aggregateStatus(statusLevel, reasons, cpu, "cpu")

	memory := renderMetric(summary.Memory, warn, block, formatMemory)
	row.Memory = memory.Text
	statusLevel, reasons = aggregateStatus(statusLevel, reasons, memory, "memory")

	pvcs := renderMetric(summary.PVCs, warn, block, formatCount)
	row.PVCs = pvcs.Text
	statusLevel, reasons = aggregateStatus(statusLevel, reasons, pvcs, "pvcs")

	row.Status = formatStatus(statusLevel, reasons)
	return row
}

type level int

const (
	levelOK level = iota
	levelWarn
	levelBlock
)

type metricRender struct {
	Text  string
	Level level
	Pct   float64
}

func aggregateStatus(current level, reasons []string, metric metricRender, label string) (level, []string) {
	if metric.Level > current {
		current = metric.Level
	}
	if metric.Level >= levelWarn && metric.Pct >= 0 {
		reasons = append(reasons, fmt.Sprintf("%s %.0f%%", label, math.Round(metric.Pct*100)))
	}
	return current, reasons
}

func formatStatus(lvl level, reasons []string) string {
	switch lvl {
	case levelBlock:
		base := "BLOCK"
		if !color.NoColor {
			base = color.New(color.FgHiRed, color.Bold).Sprint(base)
		}
		return fmt.Sprintf("%s (%s)", base, strings.Join(reasons, ", "))
	case levelWarn:
		base := "WARN"
		if !color.NoColor {
			base = color.New(color.FgYellow).Sprint(base)
		}
		return fmt.Sprintf("%s (%s)", base, strings.Join(reasons, ", "))
	default:
		return "OK"
	}
}

func renderMetric(metric Metric, warn, block float64, formatter func(int64) string) metricRender {
	if !metric.HasLimit {
		if metric.Used == 0 {
			return metricRender{Text: "-", Level: levelOK, Pct: -1}
		}
		return metricRender{
			Text:  fmt.Sprintf("%s/∞", formatter(metric.Used)),
			Level: levelOK,
			Pct:   -1,
		}
	}
	if metric.Limit <= 0 {
		text := fmt.Sprintf("%s/0", formatter(metric.Used))
		lvl := levelOK
		if metric.Used > 0 {
			lvl = levelBlock
		}
		return metricRender{Text: highlight(text, lvl), Level: lvl, Pct: 1}
	}
	pct := float64(metric.Used) / float64(metric.Limit)
	text := fmt.Sprintf("%s/%s (%.0f%%)", formatter(metric.Used), formatter(metric.Limit), pct*100)
	var lvl level
	switch {
	case pct >= block:
		lvl = levelBlock
	case pct >= warn:
		lvl = levelWarn
	default:
		lvl = levelOK
	}
	return metricRender{
		Text:  highlight(text, lvl),
		Level: lvl,
		Pct:   pct,
	}
}

func highlight(text string, lvl level) string {
	if color.NoColor {
		return text
	}
	switch lvl {
	case levelBlock:
		return color.New(color.FgHiRed).Sprint(text)
	case levelWarn:
		return color.New(color.FgYellow).Sprint(text)
	default:
		return text
	}
}

func formatCount(value int64) string {
	return fmt.Sprintf("%d", value)
}

func formatCPU(value int64) string {
	return fmt.Sprintf("%dm", value)
}

func formatMemory(value int64) string {
	q := resource.NewQuantity(value, resource.BinarySI)
	return q.String()
}

func printLimitRanges(summaries []Summary) {
	var sections []string
	for _, summary := range summaries {
		if len(summary.LimitRanges) == 0 {
			continue
		}
		var b strings.Builder
		fmt.Fprintf(&b, "\nLimitRanges for %s:\n", summary.Namespace)
		for _, lr := range summary.LimitRanges {
			fmt.Fprintf(&b, "  - %s\n", lr.String())
		}
		sections = append(sections, b.String())
	}
	if len(sections) == 0 {
		return
	}
	_, _ = io.WriteString(os.Stdout, strings.Join(sections, ""))
}
