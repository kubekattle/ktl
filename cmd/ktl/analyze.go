package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
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
	var drift bool
	var cost bool
	var fix bool
	var cluster bool

	cmd := &cobra.Command{
		Use:   "analyze [POD_NAME]",
		Short: "Analyze a Kubernetes pod for failures using heuristic or AI-powered diagnostics",
		Long: `Analyze a pod to determine why it is failing.
It fetches the pod status, recent events, and logs, then runs them through a diagnostic engine.

Examples:
  # Analyze a specific pod in the current namespace
  ktl analyze my-app-pod-123

  # Analyze a pod in a different namespace with AI enabled
  ktl analyze my-app-pod-123 -n prod --ai
  
  # Check for configuration drift (manual changes)
  ktl analyze my-app-pod-123 --drift
  
  # Estimate monthly cost
  ktl analyze my-app-pod-123 --cost
  
  # Automatically apply AI-suggested fixes (Use with caution)
  ktl analyze my-app-pod-123 --ai --fix
  
  # Analyze Cluster Health (Nodes, Global Events)
  ktl analyze --cluster`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				targetPod = args[0]
			}
			return runAnalyze(cmd.Context(), kubeconfig, kubeContext, targetPod, namespace, useAI, aiProvider, drift, cost, fix, cluster)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (defaults to context)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "Use AI-powered analysis (requires API key)")
	cmd.Flags().StringVar(&aiProvider, "provider", "heuristic", "Analysis provider: heuristic (default), openai, qwen, deepseek, or mock")
	cmd.Flags().BoolVar(&drift, "drift", false, "Check for configuration drift against 'kubectl.kubernetes.io/last-applied-configuration'")
	cmd.Flags().BoolVar(&cost, "cost", false, "Estimate monthly cost of the pod based on resource requests")
	cmd.Flags().BoolVar(&fix, "fix", false, "Automatically apply the suggested patch if available")
	cmd.Flags().BoolVar(&cluster, "cluster", false, "Run cluster-wide health checks (nodes, system pods)")

	return cmd
}

func runAnalyze(ctx context.Context, kubeconfig, kubeContext *string, podName, namespace string, useAI bool, provider string, drift bool, cost bool, fix bool, cluster bool) error {
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
		if namespace == "" {
			namespace = "default"
		}
	}

	// Cluster Analysis Mode
	if cluster {
		fmt.Println("Analyzing Cluster Health...")
		nodes, err := kClient.Clientset.CoreV1().Nodes().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		fmt.Printf("Checked %d nodes.\n", len(nodes.Items))
		for _, n := range nodes.Items {
			ready := false
			for _, c := range n.Status.Conditions {
				if c.Type == "Ready" && c.Status == "True" {
					ready = true
					break
				}
			}
			if !ready {
				color.New(color.FgRed).Printf("Node %s is NOT READY\n", n.Name)
			}

			// Check Pressure
			for _, c := range n.Status.Conditions {
				if c.Status == "True" && c.Type != "Ready" {
					color.New(color.FgYellow).Printf("Node %s has %s\n", n.Name, c.Type)
				}
			}
		}
		return nil
	}

	if podName == "" {
		return fmt.Errorf("pod name required (or use --cluster)")
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

	if drift {
		driftReport := analyze.CheckDrift(evidence.Pod)
		if len(driftReport) > 0 {
			color.New(color.FgRed, color.Bold).Println("\n DRIFT DETECTED ")
			for _, line := range driftReport {
				fmt.Println(line)
			}
		} else {
			color.New(color.FgGreen).Println("\nNo configuration drift detected.")
		}
	}

	if cost {
		monthlyCost := analyze.EstimateCost(evidence.Pod)
		color.New(color.FgCyan, color.Bold).Println("\n COST ESTIMATION ")
		fmt.Printf("Estimated Monthly Cost: $%.2f\n", monthlyCost)
		fmt.Println("(Based on generic cloud pricing: $0.04/vCPU/hr, $0.004/GB/hr)")
	}

	// 6. Interactive Fix
	if diagnosis.Patch != "" {
		fmt.Println("\n--- Auto-Remediation ---")
		fmt.Printf("Suggested Patch:\n%s\n", diagnosis.Patch)

		apply := fix
		if !fix {
			fmt.Print("Apply this patch? [y/N]: ")
			reader := bufio.NewReader(os.Stdin)
			input, _ := reader.ReadString('\n')
			if strings.ToLower(strings.TrimSpace(input)) == "y" {
				apply = true
			}
		}

		if apply {
			fmt.Println("Applying patch...")
			// Use kubectl patch
			// We need to know the resource kind. Assuming Pod for now, but usually we patch the owner (Deployment).
			// If we patch the Pod, it might be ephemeral if owned by RS.
			// Ideally we patch the owner.
			// Let's check owner references from evidence.

			targetKind := "Pod"
			targetName := podName

			if len(evidence.Pod.OwnerReferences) > 0 {
				owner := evidence.Pod.OwnerReferences[0]
				if owner.Kind == "ReplicaSet" {
					// Need to find Deployment
					// We don't have RS object handy here easily unless we fetch it.
					// But we have kClient.
					rs, err := kClient.Clientset.AppsV1().ReplicaSets(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
					if err == nil && len(rs.OwnerReferences) > 0 {
						targetKind = rs.OwnerReferences[0].Kind
						targetName = rs.OwnerReferences[0].Name
					} else {
						// Patch RS? Usually bad idea.
						fmt.Println("Warning: Pod is owned by ReplicaSet but could not find Deployment. Patching Pod directly (might be lost).")
					}
				} else {
					targetKind = owner.Kind
					targetName = owner.Name
				}
			}

			fmt.Printf("Targeting %s/%s\n", targetKind, targetName)

			// kubectl patch kind name --patch '...'
			cmd := exec.Command("kubectl", "patch", targetKind, targetName, "-n", namespace, "--patch", diagnosis.Patch)
			out, err := cmd.CombinedOutput()
			if err != nil {
				color.New(color.FgRed).Printf("Patch failed: %v\n%s\n", err, string(out))
			} else {
				color.New(color.FgGreen).Println("Patch applied successfully!")
				fmt.Println(string(out))
			}
		}
	}

	// 7. Interactive Chat (Iteration 5)
	if aiAnalyzer, ok := analyzer.(*analyze.AIAnalyzer); ok && (useAI || provider != "heuristic") {
		startChatLoop(ctx, aiAnalyzer, diagnosis)
	}

	return nil
}

func startChatLoop(ctx context.Context, ai *analyze.AIAnalyzer, initialDiagnosis *analyze.Diagnosis) {
	fmt.Println()
	color.New(color.FgMagenta, color.Bold).Println(" INTERACTIVE CHAT MODE ")
	fmt.Println("Ask follow-up questions about the pod, logs, or diagnosis. Type 'exit' to quit.")
	fmt.Println(strings.Repeat("-", 40))

	// Initial history
	history := []analyze.Message{
		{Role: "system", Content: "You are a Kubernetes Assistant. You have just provided a diagnosis for a failing pod. Answer user follow-up questions based on the evidence provided earlier."},
		{Role: "assistant", Content: fmt.Sprintf("Diagnosis: %s. %s", initialDiagnosis.RootCause, initialDiagnosis.Explanation)},
	}

	scanner := bufio.NewScanner(os.Stdin)
	for {
		color.New(color.FgCyan).Print("\n> ")
		if !scanner.Scan() {
			break
		}
		query := strings.TrimSpace(scanner.Text())
		if query == "" {
			continue
		}
		if query == "exit" || query == "quit" {
			break
		}

		// User message
		history = append(history, analyze.Message{Role: "user", Content: query})

		// Call AI
		fmt.Print("Thinking...")
		response, err := ai.Chat(ctx, history)
		fmt.Print("\r") // Clear "Thinking..."
		if err != nil {
			color.New(color.FgRed).Printf("Error: %v\n", err)
			continue
		}

		// Assistant response
		fmt.Println(response)
		history = append(history, analyze.Message{Role: "assistant", Content: response})
	}
}

func confirmFix() bool {
	fmt.Print("Do you want to apply the suggested fix? [y/N]: ")
	var response string
	fmt.Scanln(&response)
	return strings.ToLower(response) == "y" || strings.ToLower(response) == "yes"
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
