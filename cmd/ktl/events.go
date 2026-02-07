package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newEventsCommand(kubeconfig, kubeContext *string) *cobra.Command {
	var namespace string
	var watchMode bool
	var warningsOnly bool
	var allNamespaces bool

	cmd := &cobra.Command{
		Use:   "events",
		Short: "Stream cluster events in real-time with smart formatting",
		Long: `A better 'kubectl get events'.
Streams events in real-time, color-coded by severity, and formatted for readability.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runEvents(cmd.Context(), kubeconfig, kubeContext, namespace, allNamespaces, watchMode, warningsOnly)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (defaults to current)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "Watch events in all namespaces")
	cmd.Flags().BoolVarP(&watchMode, "watch", "w", false, "Watch for new events after listing (default false, unlike kubectl)")
	cmd.Flags().BoolVar(&warningsOnly, "warnings-only", false, "Show only warning events")

	return cmd
}

func runEvents(ctx context.Context, kubeconfig, kubeContext *string, namespace string, allNamespaces, watchMode, warningsOnly bool) error {
	var kc, kctx string
	if kubeconfig != nil {
		kc = *kubeconfig
	}
	if kubeContext != nil {
		kctx = *kubeContext
	}

	kClient, err := kube.New(ctx, kc, kctx)
	if err != nil {
		return fmt.Errorf("failed to init kube client: %w", err)
	}

	if allNamespaces {
		namespace = ""
	} else if namespace == "" {
		namespace = kClient.Namespace
		if namespace == "" {
			namespace = "default"
		}
	}

	// 1. List existing events
	listOpts := metav1.ListOptions{}
	if warningsOnly {
		listOpts.FieldSelector = "type=Warning"
	}

	fmt.Println(header())

	// We'll track printed UIDs to avoid dupes if watch overlaps, 
	// though usually we just rely on ResourceVersion
	printed := make(map[string]bool)

	events, err := kClient.Clientset.CoreV1().Events(namespace).List(ctx, listOpts)
	if err != nil {
		return err
	}

	// Sort by LastTimestamp
	sort.Slice(events.Items, func(i, j int) bool {
		t1 := events.Items[i].LastTimestamp.Time
		if t1.IsZero() {
			t1 = events.Items[i].EventTime.Time
		}
		t2 := events.Items[j].LastTimestamp.Time
		if t2.IsZero() {
			t2 = events.Items[j].EventTime.Time
		}
		return t1.Before(t2)
	})

	lastRV := events.ResourceVersion

	for _, e := range events.Items {
		printEvent(e, allNamespaces)
		printed[string(e.UID)] = true
	}

	if !watchMode {
		return nil
	}

	// 2. Watch
	listOpts.ResourceVersion = lastRV
	watcher, err := kClient.Clientset.CoreV1().Events(namespace).Watch(ctx, listOpts)
	if err != nil {
		return err
	}
	defer watcher.Stop()

	for {
		select {
		case <-ctx.Done():
			return nil
		case event, ok := <-watcher.ResultChan():
			if !ok {
				return nil
			}
			if e, ok := event.Object.(*corev1.Event); ok {
				// Dedup check (unlikely needed with RV, but good hygiene)
				if !printed[string(e.UID)] {
					printEvent(*e, allNamespaces)
					printed[string(e.UID)] = true
				}
			}
		}
	}
}

func header() string {
	return color.New(color.FgWhite, color.Underline).Sprintf("%-20s %-10s %-15s %-30s %s", "LAST SEEN", "TYPE", "REASON", "OBJECT", "MESSAGE")
}

func printEvent(e corev1.Event, showNS bool) {
	ts := e.LastTimestamp.Time
	if ts.IsZero() {
		ts = e.EventTime.Time
	}
	if ts.IsZero() {
		ts = e.FirstTimestamp.Time
	}

	timeStr := ts.Format("15:04:05")
	if time.Since(ts) > 24*time.Hour {
		timeStr = ts.Format("Jan02 15:04")
	}

	typeColor := color.New(color.FgGreen)
	if e.Type == "Warning" {
		typeColor = color.New(color.FgRed, color.Bold)
	}

	obj := fmt.Sprintf("%s/%s", strings.ToLower(e.InvolvedObject.Kind), e.InvolvedObject.Name)
	if showNS {
		obj = fmt.Sprintf("%s/%s", e.InvolvedObject.Namespace, obj)
	}
	// Truncate object if too long
	if len(obj) > 30 {
		obj = "..." + obj[len(obj)-27:]
	}

	reason := e.Reason
	if len(reason) > 15 {
		reason = reason[:15]
	}

	msg := e.Message
	
	// Format: Time | Type | Reason | Object | Message
	fmt.Printf("%s %s %s %s %s\n",
		color.New(color.FgHiBlack).Sprint(timeStr),
		typeColor.Sprintf("%-10s", e.Type),
		color.New(color.FgYellow).Sprintf("%-15s", reason),
		color.New(color.FgCyan).Sprintf("%-30s", obj),
		msg,
	)
}
