// render.go prints PVC health rows, including node pressure annotations, for 'ktl diag storage'.
package pvcstatus

import (
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/fatih/color"
	corev1 "k8s.io/api/core/v1"
)

// RenderOptions toggles details.
type RenderOptions struct{}

// Print renders PVC summaries.
func Print(summaries []Summary, _ RenderOptions) {
	if len(summaries) == 0 {
		fmt.Println("No PersistentVolumeClaims matched the filter.")
		return
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "PVC\tPHASE\tCLASS\tVOLUME MODE\tACCESS MODES\tCAPACITY\tPODS\tNOTES")
	for _, pvc := range summaries {
		fmt.Fprintf(tw, "%s/%s\t%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			pvc.Namespace,
			pvc.Name,
			pvc.Phase,
			dashIfEmpty(pvc.StorageClass),
			dashIfEmpty(pvc.VolumeMode),
			formatAccessModes(pvc.AccessModes),
			dashIfEmpty(pvc.Capacity),
			formatPods(pvc.Pods),
			formatNotes(pvc.Notes),
		)
	}
	_ = tw.Flush()
}

func formatAccessModes(modes []corev1.PersistentVolumeAccessMode) string {
	if len(modes) == 0 {
		return "-"
	}
	var labels []string
	for _, mode := range modes {
		labels = append(labels, string(mode))
	}
	return strings.Join(labels, ",")
}

func formatPods(pods []PodUsage) string {
	if len(pods) == 0 {
		return "-"
	}
	var parts []string
	for _, pod := range pods {
		entry := pod.PodName
		if pod.NodeName != "" {
			entry = fmt.Sprintf("%s@%s", entry, pod.NodeName)
		}
		parts = append(parts, entry)
	}
	return strings.Join(parts, ",")
}

func formatNotes(notes []string) string {
	if len(notes) == 0 {
		return "-"
	}
	var formatted []string
	for _, note := range notes {
		if strings.Contains(note, "Pressure") && !color.NoColor {
			formatted = append(formatted, color.New(color.FgHiRed).Sprint(note))
		} else {
			formatted = append(formatted, note)
		}
	}
	return strings.Join(formatted, "; ")
}

func dashIfEmpty(val string) string {
	if val == "" {
		return "-"
	}
	return val
}
