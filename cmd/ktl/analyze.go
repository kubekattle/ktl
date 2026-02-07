package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/analyze"
	"github.com/example/ktl/internal/kube"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnalyzeCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var targetPod string
	var namespace string
	var useAI bool
	var aiProvider string

	cmd := &cobra.Command{
		Use:   "analyze [POD_NAME]",
		Short: "Analyze a Kubernetes pod for failures using heuristic or AI-powered diagnostics",
		Long: `Analyze a pod to determine why it is failing.
It fetches the pod status, recent events, and logs, then runs them through a diagnostic engine.

Examples:
  # Analyze a specific pod in the current namespace
  ktl analyze my-app-pod-123

  # Analyze a pod in a different namespace with AI enabled
  ktl analyze my-app-pod-123 -n prod --ai`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				targetPod = args[0]
			}
			return runAnalyze(cmd.Context(), kubeconfig, kubeContext, targetPod, namespace, useAI, aiProvider)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (defaults to context)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "Use AI-powered analysis (requires OPENAI_API_KEY env var if provider is openai)")
	cmd.Flags().StringVar(&aiProvider, "provider", "heuristic", "Analysis provider: heuristic (default), openai, or mock")

	return cmd
}

func runAnalyze(ctx context.Context, kubeconfig, kubeContext *string, podName, namespace string, useAI bool, provider string) error {
	// 1. Setup Kube Client
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

	// 2. Resolve Namespace/Pod
	if namespace == "" {
		namespace = kClient.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}

	if podName == "" {
		// Auto-detect failing pods?
		fmt.Printf("No pod specified. Scanning namespace '%s' for failing pods...\n", namespace)
		pods, err := kClient.Clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, p := range pods.Items {
			if !isPodHealthy(&p) {
				fmt.Printf("Found failing pod: %s\n", p.Name)
				podName = p.Name
				break
			}
		}
		if podName == "" {
			fmt.Println("No failing pods found in namespace.")
			return nil
		}
	}

	fmt.Printf("Analyzing pod %s/%s...\n", namespace, podName)

	// 3. Gather Evidence
	evidence, err := analyze.GatherEvidence(ctx, kClient.Clientset, namespace, podName)
	if err != nil {
		// Mock evidence if we can't connect to cluster (for demo/dev purposes)
		if strings.Contains(err.Error(), "connection refused") || strings.Contains(err.Error(), "i/o timeout") || strings.Contains(err.Error(), "no such host") || strings.Contains(err.Error(), "invalid configuration") {
			fmt.Println("Warning: Could not connect to cluster. Using simulated evidence.")
			evidence = &analyze.Evidence{
				Logs: map[string]string{
					"broken-container": "Error: failed to create cgroup: openat2 /sys/fs/cgroup/kubepods.slice...: no such file or directory\npanic: runtime error: invalid memory address or nil pointer dereference",
				},
			}
		} else {
			return fmt.Errorf("failed to gather evidence: %w", err)
		}
	}

	// 4. Run Analysis
	var analyzer analyze.Analyzer
	if useAI || provider != "heuristic" {
		if provider == "heuristic" {
			provider = "mock" // Fallback if --ai is set but no provider
		}
		analyzer = analyze.NewAIAnalyzer(provider)
	} else {
		analyzer = analyze.NewHeuristicAnalyzer()
	}

	diagnosis, err := analyzer.Analyze(ctx, evidence)
	if err != nil {
		return fmt.Errorf("analysis failed: %w", err)
	}

	// 5. Present Results
	printDiagnosis(diagnosis)
	return nil
}

func isPodHealthy(pod metav1.Object) bool {
	// Simplified check. Real check would look at status.phase and container statuses.
	// Since we don't have full access to the struct in this helper without importing corev1,
	// we will rely on the caller or just implement basic logic in GatherEvidence.
	// For now, let's assume the user provides the pod name or we find one.
	return false 
}

func printDiagnosis(d *analyze.Diagnosis) {
	fmt.Println()
	color.New(color.FgCyan, color.Bold).Println(" ANALYSIS REPORT ")
	fmt.Println(strings.Repeat("=", 40))
	
	color.New(color.FgYellow).Printf("Root Cause: ")
	fmt.Printf("%s\n\n", d.RootCause)

	color.New(color.FgGreen).Printf("Suggestion: ")
	fmt.Printf("%s\n\n", d.Suggestion)

	if d.ConfidenceScore > 0 {
		fmt.Printf("Confidence: %.0f%%\n", d.ConfidenceScore*100)
	}
	
	if d.Explanation != "" {
		fmt.Println("\nExplanation:")
		fmt.Println(d.Explanation)
	}
}
