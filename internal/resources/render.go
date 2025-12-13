// render.go outputs namespace workload tables (deployments, pods, images) for 'ktl diag resources'.
package resources

import (
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/fatih/color"
	"k8s.io/apimachinery/pkg/api/resource"
)

// RenderOptions control formatting of the resources table.
type RenderOptions struct{}

// Print outputs the container usage table.
func Print(summary *Summary, _ RenderOptions) {
	if summary == nil || len(summary.Containers) == 0 {
		fmt.Println("No containers matched the namespace filter.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "CONTAINER\tCPU req/limit/usage\tMEM req/limit/usage\tPHASE\tNODE")
	for _, container := range summary.Containers {
		fmt.Fprintf(tw, "%s/%s:%s\t%s\t%s\t%s\t%s\n",
			container.Namespace,
			container.Pod,
			container.Container,
			formatCPU(container.RequestCPU, container.LimitCPU, container.UsageCPU),
			formatMemory(container.RequestMemory, container.LimitMemory, container.UsageMemory),
			container.Phase,
			emptyIf(container.NodeName),
		)
	}
	_ = tw.Flush()
	if !summary.MetricsEnabled && summary.MetricsError != "" {
		fmt.Printf("\nWarning: live usage unavailable (%s). Showing requests/limits only.\n", summary.MetricsError)
	} else if !summary.MetricsEnabled {
		fmt.Println("\nWarning: metrics client unavailable; showing requests/limits only.")
	} else {
		fmt.Println("\nRows are sorted by live memory usage so the greediest container (e.g., 32 GiB) rises to the top.")
	}
}

func formatCPU(req, limit, usage int64) string {
	return fmt.Sprintf("%s/%s/%s", milliString(req), milliString(limit), milliStringColored(usage, limit))
}

func formatMemory(req, limit, usage int64) string {
	return fmt.Sprintf("%s/%s/%s", bytesString(req), bytesString(limit), bytesStringColored(usage, limit))
}

func milliString(value int64) string {
	if value <= 0 {
		return "-"
	}
	return fmt.Sprintf("%dm", value)
}

func milliStringColored(value int64, limit int64) string {
	text := milliString(value)
	if value <= 0 || limit <= 0 || color.NoColor {
		return text
	}
	pct := float64(value) / float64(limit)
	switch {
	case pct >= 1.0:
		return color.New(color.FgHiRed).Sprint(text)
	case pct >= 0.8:
		return color.New(color.FgYellow).Sprint(text)
	default:
		return text
	}
}

func bytesString(value int64) string {
	if value <= 0 {
		return "-"
	}
	q := resource.NewQuantity(value, resource.BinarySI)
	return q.String()
}

func bytesStringColored(value int64, limit int64) string {
	text := bytesString(value)
	if value <= 0 || limit <= 0 || color.NoColor {
		return text
	}
	pct := float64(value) / float64(limit)
	switch {
	case pct >= 1.0:
		return color.New(color.FgHiRed).Sprint(text)
	case pct >= 0.8:
		return color.New(color.FgYellow).Sprint(text)
	default:
		return text
	}
}

func emptyIf(val string) string {
	if val == "" {
		return "-"
	}
	return val
}
