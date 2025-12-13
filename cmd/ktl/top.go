// top.go wires the 'ktl diag top' helpers that render top-like CPU/memory tables for pods and namespaces.
package main

import (
	"fmt"
	"io"
	"text/tabwriter"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/top"
	"github.com/spf13/cobra"
)

func newTopCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var (
		namespaces    []string
		allNamespaces bool
		labelSelector string
		sortByCPU     bool
	)
	cmd := &cobra.Command{
		Use:   "top",
		Short: "Show CPU and memory usage for pods (kubectl top equivalent)",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			opts := top.PodOptions{
				Namespaces:    namespaces,
				AllNamespaces: allNamespaces,
				LabelSelector: labelSelector,
				SortByCPU:     sortByCPU,
			}
			if len(opts.Namespaces) == 0 && !opts.AllNamespaces && kubeClient.Namespace != "" {
				opts.Namespaces = []string{kubeClient.Namespace}
			}
			stats, err := top.ListPodUsage(ctx, kubeClient.Metrics, opts)
			if err != nil {
				return err
			}
			if len(stats) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "No pod metrics found (metrics-server installed?)")
				return nil
			}
			includeNamespace := opts.AllNamespaces || hasMultipleNamespaces(stats)
			renderPodStats(cmd.OutOrStdout(), stats, includeNamespace)
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to query (defaults to current context)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Include pods across all namespaces")
	cmd.Flags().StringVarP(&labelSelector, "selector", "l", "", "Label selector to filter pods")
	cmd.Flags().BoolVar(&sortByCPU, "sort-cpu", false, "Sort output by CPU usage instead of namespace/name")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "top Flags")
	return cmd
}

func renderPodStats(out io.Writer, stats []top.PodUsage, includeNamespace bool) {
	w := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
	if includeNamespace {
		fmt.Fprintln(w, "NAMESPACE\tNAME\tCPU(m)\tMEMORY")
	} else {
		fmt.Fprintln(w, "NAME\tCPU(m)\tMEMORY")
	}
	for _, stat := range stats {
		mem := formatBytesMi(stat.MemoryBytes)
		if includeNamespace {
			fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", stat.Namespace, stat.Pod, stat.CPUm, mem)
		} else {
			fmt.Fprintf(w, "%s\t%d\t%s\n", stat.Pod, stat.CPUm, mem)
		}
	}
	w.Flush()
}

func formatBytesMi(bytes int64) string {
	if bytes <= 0 {
		return "0Mi"
	}
	mi := float64(bytes) / (1024.0 * 1024.0)
	return fmt.Sprintf("%.0fMi", mi)
}

func hasMultipleNamespaces(stats []top.PodUsage) bool {
	if len(stats) == 0 {
		return false
	}
	first := stats[0].Namespace
	for _, s := range stats[1:] {
		if s.Namespace != first {
			return true
		}
	}
	return false
}
