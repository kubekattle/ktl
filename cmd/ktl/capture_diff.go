// File: cmd/ktl/capture_diff.go
// Brief: CLI command wiring and implementation for 'capture diff'.

// capture_diff.go implements 'ktl logs capture diff', comparing archived snapshots (or live namespaces) to surface drift between captures.
package main

import (
	"context"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/kube"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newCaptureDiffCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var live bool
	var liveNamespaces []string
	var liveAll bool
	var livePodQuery string
	cmd := &cobra.Command{
		Use:   "diff <CAPTURE_A> [<CAPTURE_B>]",
		Short: "Show metadata differences between two capture artifacts",
		Example: `  # Compare two capture artifacts
  ktl logs capture diff dist/before.tar.gz dist/after.tar.gz

  # Compare a capture against the live cluster
  ktl logs capture diff dist/incident.tar.gz --live -n prod-payments`,
		Args: func(cmd *cobra.Command, args []string) error {
			if live {
				if len(args) != 1 {
					return fmt.Errorf("--live requires exactly one capture artifact path")
				}
				return nil
			}
			if len(args) != 2 {
				return fmt.Errorf("diff requires two capture artifacts (got %d)", len(args))
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			leftPath := args[0]
			if live {
				leftMeta, err := capture.LoadMetadata(leftPath)
				if err != nil {
					return err
				}
				kubeClient, err := kube.New(cmd.Context(), *kubeconfig, *kubeContext)
				if err != nil {
					return err
				}
				namespaces, err := resolveLiveNamespaces(cmd.Context(), kubeClient, liveNamespaces, liveAll)
				if err != nil {
					return err
				}
				rightMeta, err := buildLiveMetadata(cmd.Context(), kubeClient, namespaces, liveAll, livePodQuery, *kubeconfig, *kubeContext)
				if err != nil {
					return err
				}
				diff := capture.DiffMetadata(*leftMeta, rightMeta)
				diff.LeftPath = leftPath
				diff.RightPath = "live cluster"
				printCaptureSummary(cmd, "Capture", leftPath, diff.Left)
				printCaptureSummary(cmd, "Live", "current cluster", diff.Right)
				fmt.Fprintln(cmd.OutOrStdout())
				if diff.Empty() {
					cmd.Println("Captures share the same metadata (no differences detected)")
					return nil
				}
				printDiffReport(cmd, &diff)
				return nil
			}
			report, err := capture.CompareMetadataFiles(args[0], args[1])
			if err != nil {
				return err
			}
			printCaptureSummary(cmd, "Capture A", report.LeftPath, report.Left)
			printCaptureSummary(cmd, "Capture B", report.RightPath, report.Right)
			fmt.Fprintln(cmd.OutOrStdout())
			if report.Empty() {
				cmd.Println("Captures share the same metadata (no differences detected)")
				return nil
			}
			printDiffReport(cmd, report)
			return nil
		},
	}
	cmd.Flags().BoolVar(&live, "live", false, "Compare the capture against the current cluster")
	cmd.Flags().StringSliceVarP(&liveNamespaces, "namespace", "n", nil, "Namespaces to inspect when using --live (defaults to the kube context namespace)")
	cmd.Flags().BoolVarP(&liveAll, "all-namespaces", "A", false, "Inspect every namespace when using --live")
	cmd.Flags().StringVar(&livePodQuery, "pod-query", ".*", "Regex used to count pods when using --live")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Diff Flags")
	return cmd
}

func printDiffReport(cmd *cobra.Command, report *capture.MetadataDiff) {
	if len(report.AddedNamespaces) > 0 || len(report.RemovedNamespaces) > 0 {
		cmd.Println("Namespace delta:")
		if len(report.AddedNamespaces) > 0 {
			cmd.Printf("  Added:   %s\n", joinOr(report.AddedNamespaces))
		}
		if len(report.RemovedNamespaces) > 0 {
			cmd.Printf("  Removed: %s\n", joinOr(report.RemovedNamespaces))
		}
	}
	if len(report.FieldDiffs) > 0 {
		cmd.Println("Field differences:")
		for _, fd := range report.FieldDiffs {
			cmd.Printf("  %s: %s -> %s\n", fd.Name, fd.Left, fd.Right)
		}
	}
}

func printCaptureSummary(cmd *cobra.Command, label, path string, meta capture.Metadata) {
	duration := time.Duration(meta.DurationSeconds * float64(time.Second))
	cmd.Printf("%s (%s)\n", label, path)
	cmd.Printf("  Session:   %s\n", emptyOr(meta.SessionName, "<unnamed>"))
	cmd.Printf("  Window:    %s → %s (%s)\n", meta.StartedAt.UTC().Format(time.RFC3339), meta.EndedAt.UTC().Format(time.RFC3339), duration)
	cmd.Printf("  Namespaces:%s\n", formatList(meta.Namespaces))
	cmd.Printf("  Pod query: %s\n", emptyOr(meta.PodQuery, ".*"))
	cmd.Printf("  Pods seen: %d\n", meta.PodCount)
	cmd.Printf("  Tail:      %d lines, since=%s\n", meta.TailLines, emptyOr(meta.Since, "<none>"))
	cmd.Printf("  Flags:     events=%t follow=%t sqlite=%t\n", meta.EventsEnabled, meta.Follow, meta.SQLitePath != "")
}

func emptyOr(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func formatList(values []string) string {
	if len(values) == 0 {
		return " <none>"
	}
	return " " + strings.Join(values, ", ")
}

func joinOr(values []string) string {
	if len(values) == 0 {
		return "<none>"
	}
	return strings.Join(values, ", ")
}

func resolveLiveNamespaces(ctx context.Context, client *kube.Client, requested []string, all bool) ([]string, error) {
	if all {
		list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, err
		}
		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		sort.Strings(names)
		return names, nil
	}
	names := make([]string, 0, len(requested))
	for _, ns := range requested {
		ns = strings.TrimSpace(ns)
		if ns != "" {
			names = append(names, ns)
		}
	}
	if len(names) > 0 {
		sort.Strings(names)
		return names, nil
	}
	if client.Namespace != "" {
		return []string{client.Namespace}, nil
	}
	return []string{"default"}, nil
}

func buildLiveMetadata(ctx context.Context, client *kube.Client, namespaces []string, all bool, podExpr string, kubeconfig string, kubeCtx string) (capture.Metadata, error) {
	re, err := regexp.Compile(podExpr)
	if err != nil {
		return capture.Metadata{}, fmt.Errorf("compile pod regex: %w", err)
	}
	count := 0
	for _, ns := range namespaces {
		pods, err := client.Clientset.CoreV1().Pods(ns).List(ctx, metav1.ListOptions{})
		if err != nil {
			return capture.Metadata{}, fmt.Errorf("list pods in %s: %w", ns, err)
		}
		for _, pod := range pods.Items {
			if re.MatchString(pod.Name) {
				count++
			}
		}
	}
	now := time.Now().UTC()
	meta := capture.Metadata{
		SessionName:     "live-cluster",
		StartedAt:       now,
		EndedAt:         now,
		DurationSeconds: 0,
		Namespaces:      append([]string{}, namespaces...),
		AllNamespaces:   all,
		PodQuery:        podExpr,
		TailLines:       0,
		Since:           "",
		Context:         kubeCtx,
		Kubeconfig:      kubeconfig,
		PodCount:        count,
		EventsEnabled:   false,
		Follow:          true,
	}
	return meta, nil
}
