// render.go prints the priority/preemption tables exposed by 'ktl diag priorities'.
package priorities

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
)

// Render prints PriorityClass details plus pod priority usage.
func Render(summary *Summary) {
	if summary == nil {
		fmt.Println("No priority data available.")
		return
	}
	printPriorityClasses(summary.Classes)
	printPodPriorities(summary.Pods)
}

func printPriorityClasses(classes []PriorityClassSummary) {
	if len(classes) == 0 {
		fmt.Println("No PriorityClasses found.")
		return
	}
	fmt.Println("PriorityClasses:")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "NAME\tVALUE\tPOLICY\tDEFAULT\tDESCRIPTION")
	for _, class := range classes {
		defaultText := ""
		if class.GlobalDefault {
			defaultText = "yes"
		}
		fmt.Fprintf(tw, "%s\t%d\t%s\t%s\t%s\n",
			class.Name,
			class.Value,
			class.PreemptionPolicy,
			defaultText,
			class.Description)
	}
	_ = tw.Flush()
}

func printPodPriorities(pods []PodPriority) {
	if len(pods) == 0 {
		fmt.Println("\nNo pods matched the namespace filter.")
		return
	}
	fmt.Println("\nPods (sorted by priority):")
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "POD\tPRIORITY\tCLASS\tPHASE\tPOLICY\tNOMINATED\tSTATUS")
	for _, pod := range pods {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pod.Namespace,
			pod.Name,
			formatPriorityValue(pod.Priority, pod.Deleting),
			emptyIf(pod.PriorityClass),
			pod.Phase,
			emptyIf(pod.PreemptionPolicy),
			emptyIf(pod.NominatedNode),
			formatStatus(pod),
		)
	}
	_ = tw.Flush()
	fmt.Println("\nStatus combines pod.Status.Reason/Message, DisruptionTarget notes, and termination state to explain preemption cascades.")
}

func formatPriorityValue(value int32, deleting bool) string {
	text := fmt.Sprintf("%d", value)
	if deleting && !color.NoColor {
		return color.New(color.FgHiRed).Sprint(text + " (terminating)")
	}
	return text
}

func emptyIf(val string) string {
	if val == "" {
		return "-"
	}
	return val
}

func formatStatus(pod PodPriority) string {
	var parts []string
	if pod.Reason != "" {
		parts = append(parts, pod.Reason)
	}
	if pod.Message != "" {
		parts = append(parts, pod.Message)
	}
	if pod.DisruptionNote != "" {
		parts = append(parts, pod.DisruptionNote)
	}
	if pod.Deleting {
		parts = append(parts, "terminating")
	}
	if pod.NominatedNode != "" {
		parts = append(parts, fmt.Sprintf("nominated for %s", pod.NominatedNode))
	}
	if len(parts) == 0 {
		return "OK"
	}
	status := strings.Join(parts, "; ")
	if strings.Contains(strings.ToLower(status), "preempt") && !color.NoColor {
		return color.New(color.FgYellow).Sprint(status)
	}
	return status
}
