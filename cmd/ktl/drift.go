// drift.go adds the 'ktl drift watch' workflow, periodically snapshotting pods and flagging changes between generations.
package main

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/drift"
	"github.com/example/ktl/internal/grpcutil"
	"github.com/example/ktl/internal/kube"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newDriftCommand(kubeconfig *string, kubeContext *string, remoteAgent *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "drift",
		Short: "Observe workload drift over time",
	}
	cmd.AddCommand(newDriftWatchCommand(kubeconfig, kubeContext, remoteAgent))
	return cmd
}

func newDriftWatchCommand(kubeconfig *string, kubeContext *string, remoteAgent *string) *cobra.Command {
	var namespaces []string
	var allNamespaces bool
	interval := 30 * time.Second
	history := 20
	iterations := 0
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Continuously snapshot pods and highlight drift",
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				return runRemoteDriftWatch(cmd, strings.TrimSpace(*remoteAgent), namespaces, allNamespaces, interval, history, iterations, kubeconfig, kubeContext)
			}
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			targetNamespaces, err := determineDriftNamespaces(ctx, kubeClient, namespaces, allNamespaces)
			if err != nil {
				return err
			}
			collector := drift.NewCollector(kubeClient.Clientset, targetNamespaces, history)
			collector.SetInterval(interval)
			prev, err := collector.Snapshot(ctx)
			if err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Watching drift across %d namespace(s) every %s\n", len(targetNamespaces), interval)
			renderSnapshotHeader(cmd.OutOrStdout(), prev)
			iteration := 0
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for {
				if iterations > 0 && iteration >= iterations {
					return nil
				}
				iteration++
				select {
				case <-ctx.Done():
					return ctx.Err()
				case <-ticker.C:
					curr, err := collector.Snapshot(ctx)
					if err != nil {
						return err
					}
					diff := drift.DiffSnapshots(&prev, &curr)
					renderDiffBlock(cmd.OutOrStdout(), prev.Timestamp, curr, diff)
					prev = curr
				}
			}
		},
	}
	cmd.Flags().StringSliceVarP(&namespaces, "namespace", "n", nil, "Namespaces to monitor (defaults to current context namespace)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Monitor every namespace (cluster-wide)")
	cmd.Flags().DurationVar(&interval, "interval", interval, "Snapshot interval (e.g. 15s, 1m)")
	cmd.Flags().IntVar(&history, "history", history, "Number of snapshots to retain in memory")
	cmd.Flags().IntVar(&iterations, "iterations", 0, "Stop after N iterations (0 = run until Ctrl+C)")
	cmd.Example = `  # Watch drift in prod-payments every 15 seconds
  ktl logs drift watch -n prod-payments --interval 15s

  # Monitor the whole cluster with a longer cadence
  ktl logs drift watch -A --interval 1m`
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Drift Flags")
	return cmd
}

func determineDriftNamespaces(ctx context.Context, client *kube.Client, requested []string, all bool) ([]string, error) {
	if all {
		list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		names := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			names = append(names, ns.Name)
		}
		sort.Strings(names)
		return names, nil
	}
	if len(requested) > 0 {
		out := make([]string, 0, len(requested))
		for _, ns := range requested {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				out = append(out, ns)
			}
		}
		if len(out) > 0 {
			return out, nil
		}
	}
	if client.Namespace != "" {
		return []string{client.Namespace}, nil
	}
	return []string{metav1.NamespaceDefault}, nil
}

func empty(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}

func renderSnapshotHeader(out io.Writer, snap drift.Snapshot) {
	fmt.Fprintf(out, "Initial snapshot captured at %s (%d pods)\n", snap.Timestamp.Format(time.RFC3339), len(snap.Pods))
}

func renderDiffBlock(out io.Writer, prevTime time.Time, curr drift.Snapshot, diff drift.Diff) {
	fmt.Fprintf(out, "\n=== Drift at %s (Δ %s) ===\n", curr.Timestamp.Format(time.RFC3339), curr.Timestamp.Sub(prevTime))
	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Changed) == 0 {
		fmt.Fprintln(out, "No drift detected.")
		return
	}
	for _, pod := range diff.Added {
		fmt.Fprintf(out, "+ %s/%s phase=%s node=%s\n", pod.Namespace, pod.Name, pod.Phase, empty(pod.Node, "<none>"))
	}
	for _, pod := range diff.Removed {
		fmt.Fprintf(out, "- %s/%s (was phase %s)\n", pod.Namespace, pod.Name, pod.Phase)
	}
	for _, change := range diff.Changed {
		fmt.Fprintf(out, "~ %s/%s %s\n", change.Namespace, change.Name, strings.Join(change.Reasons, "; "))
	}
}

func runRemoteDriftWatch(cmd *cobra.Command, remoteAddr string, namespaces []string, all bool, interval time.Duration, history int, iterations int, kubeconfig *string, kubeContext *string) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, remoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	cfg := convert.DriftWatchConfig{
		Namespaces:    append([]string(nil), namespaces...),
		AllNamespaces: all,
		Interval:      interval,
		History:       history,
		Iterations:    iterations,
	}
	if kubeconfig != nil {
		cfg.KubeConfig = *kubeconfig
	}
	if kubeContext != nil {
		cfg.KubeContext = *kubeContext
	}

	client := apiv1.NewDriftServiceClient(conn)
	stream, err := client.Watch(ctx, convert.DriftToProto(cfg))
	if err != nil {
		return err
	}
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		text := event.GetText()
		if strings.TrimSpace(text) == "" {
			continue
		}
		fmt.Fprint(cmd.OutOrStdout(), text)
	}
}
