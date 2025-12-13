// render.go prints node capacity/allocatable summaries for 'ktl diag nodes'.
package nodes

import (
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RenderOptions tunes status thresholds when printing node data.
type RenderOptions struct {
	WarnThreshold  float64
	BlockThreshold float64
}

// PrintTable renders node summaries in a tabular format.
func PrintTable(summaries []Summary, opts RenderOptions) {
	if len(summaries) == 0 {
		fmt.Println("No nodes matched the selector.")
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
	fmt.Fprintln(tw, "NODE\tREADY\tSCHED\tCPU req/alloc/cap\tMEM req/alloc/cap\tEPHEMERAL req/alloc/cap\tPODS used/alloc\tNOTES")
	for _, summary := range summaries {
		row := renderNodeRow(summary, warn, block)
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			summary.Name,
			row.ready,
			row.schedulable,
			row.cpu,
			row.memory,
			row.ephemeral,
			row.pods,
			row.notes,
		)
	}
	_ = tw.Flush()
	printLegend(os.Stdout)
}

type nodeRow struct {
	ready       string
	schedulable string
	cpu         string
	memory      string
	ephemeral   string
	pods        string
	notes       string
}

func renderNodeRow(summary Summary, warn, block float64) nodeRow {
	row := nodeRow{
		ready:       boolText(summary.Ready, "Ready", summary.ReadyReason),
		schedulable: boolText(summary.Schedulable, "Sched", "cordoned"),
		notes:       summary.FormatNotes(),
	}

	row.cpu = renderResource(summary.Requested.CPU, summary.Allocatable.CPU, summary.Capacity.CPU, warn, block, formatCPU)
	row.memory = renderResource(summary.Requested.Memory, summary.Allocatable.Memory, summary.Capacity.Memory, warn, block, formatBinary)
	row.ephemeral = renderResource(summary.Requested.Ephemeral, summary.Allocatable.Ephemeral, summary.Capacity.Ephemeral, warn, block, formatBinary)
	row.pods = renderPods(summary.PodCount, summary.PodAllocatable, summary.PodCapacity, warn, block)

	return row
}

func boolText(ok bool, positive string, negative string) string {
	if ok {
		return positive
	}
	if color.NoColor {
		return negative
	}
	return color.New(color.FgHiRed).Sprint(negative)
}

func renderResource(used, alloc, capacity int64, warn, block float64, formatter func(int64) string) string {
	if alloc <= 0 || capacity <= 0 {
		return "-"
	}
	pct := float64(used) / float64(alloc)
	text := fmt.Sprintf("%s/%s/%s (%.0f%%)", formatter(used), formatter(alloc), formatter(capacity), pct*100)
	switch {
	case pct >= block:
		return highlight(text, levelBlock)
	case pct >= warn:
		return highlight(text, levelWarn)
	default:
		return text
	}
}

func renderPods(used int, allocatable int64, capacity int64, warn, block float64) string {
	if allocatable <= 0 || capacity <= 0 {
		return "-"
	}
	pct := float64(used) / float64(allocatable)
	text := fmt.Sprintf("%d/%d/%d (%.0f%%)", used, allocatable, capacity, pct*100)
	switch {
	case pct >= block:
		return highlight(text, levelBlock)
	case pct >= warn:
		return highlight(text, levelWarn)
	default:
		return text
	}
}

func formatCPU(val int64) string {
	return fmt.Sprintf("%dm", val)
}

func formatBinary(val int64) string {
	q := resource.NewQuantity(val, resource.BinarySI)
	return q.String()
}

type statusLevel int

const (
	levelOK statusLevel = iota
	levelWarn
	levelBlock
)

func highlight(text string, lvl statusLevel) string {
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

func printLegend(w io.Writer) {
	if _, err := fmt.Fprintln(w, "\nLegend: req/alloc/cap compare pod requests to allocatable and node capacity; WARN at >=80%, BLOCK at >=100%."); err != nil {
		return
	}
}
