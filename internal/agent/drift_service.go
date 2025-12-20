// File: internal/agent/drift_service.go
// Brief: Internal agent package implementation for 'drift service'.

// Package agent provides agent helpers.

package agent

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/drift"
	"github.com/example/ktl/internal/kube"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DriftServer exposes drift watch over gRPC.
type DriftServer struct {
	apiv1.UnimplementedDriftServiceServer
}

// Watch streams drift snapshots/diffs to the caller.
func (s *DriftServer) Watch(req *apiv1.DriftWatchRequest, stream apiv1.DriftService_WatchServer) error {
	if req == nil {
		return fmt.Errorf("drift request is required")
	}
	cfg := convert.DriftFromProto(req)
	ctx := stream.Context()

	kubeClient, err := kube.New(ctx, cfg.KubeConfig, cfg.KubeContext)
	if err != nil {
		return err
	}
	namespaces, err := resolveDriftNamespaces(ctx, kubeClient, cfg.Namespaces, cfg.AllNamespaces)
	if err != nil {
		return err
	}

	collector := drift.NewCollector(kubeClient.Clientset, namespaces, cfg.History)
	collector.SetInterval(cfg.Interval)

	prev, err := collector.Snapshot(ctx)
	if err != nil {
		return err
	}
	if err := stream.Send(&apiv1.DriftEvent{Text: formatSnapshotHeader(prev)}); err != nil {
		return err
	}

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()
	iterations := 0
	for {
		if cfg.Iterations > 0 && iterations >= cfg.Iterations {
			return nil
		}
		iterations++
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			curr, err := collector.Snapshot(ctx)
			if err != nil {
				return err
			}
			diff := drift.DiffSnapshots(&prev, &curr)
			text := formatDiff(prev.Timestamp, curr, diff)
			if err := stream.Send(&apiv1.DriftEvent{Text: text}); err != nil {
				return err
			}
			prev = curr
		}
	}
}

func resolveDriftNamespaces(ctx context.Context, client *kube.Client, requested []string, all bool) ([]string, error) {
	if all {
		list, err := client.Clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
		if err != nil {
			return nil, fmt.Errorf("list namespaces: %w", err)
		}
		out := make([]string, 0, len(list.Items))
		for _, ns := range list.Items {
			out = append(out, ns.Name)
		}
		sort.Strings(out)
		return out, nil
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

func formatSnapshotHeader(snap drift.Snapshot) string {
	return fmt.Sprintf("Initial snapshot captured at %s (%d pods)\n", snap.Timestamp.Format(time.RFC3339), len(snap.Pods))
}

func formatDiff(prevTime time.Time, curr drift.Snapshot, diff drift.Diff) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "\n=== Drift at %s (Δ %s) ===\n", curr.Timestamp.Format(time.RFC3339), curr.Timestamp.Sub(prevTime))
	if len(diff.Added) == 0 && len(diff.Removed) == 0 && len(diff.Changed) == 0 {
		buf.WriteString("No drift detected.\n")
		return buf.String()
	}
	for _, pod := range diff.Added {
		fmt.Fprintf(&buf, "+ %s/%s phase=%s node=%s\n", pod.Namespace, pod.Name, pod.Phase, nonEmpty(pod.Node, "<none>"))
	}
	for _, pod := range diff.Removed {
		fmt.Fprintf(&buf, "- %s/%s (was phase %s)\n", pod.Namespace, pod.Name, pod.Phase)
	}
	for _, change := range diff.Changed {
		fmt.Fprintf(&buf, "~ %s/%s %s\n", change.Namespace, change.Name, strings.Join(change.Reasons, "; "))
	}
	return buf.String()
}

func nonEmpty(val, fallback string) string {
	if strings.TrimSpace(val) == "" {
		return fallback
	}
	return val
}
