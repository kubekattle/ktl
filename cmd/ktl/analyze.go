package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/charmbracelet/glamour"
	"github.com/example/ktl/internal/analyze"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/ui"
	"github.com/fatih/color"
	"github.com/spf13/cobra"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func newAnalyzeCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var targetPod string
	var namespace string
	var useAI bool
	var aiProvider string
	var aiModel string
	var drift bool
	var cost bool
	var fix bool
	var cluster bool
	var profile bool
	var rbac bool

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
  
  # Profile resource usage vs requests
  ktl analyze my-app-pod-123 --profile
  
  # Audit RBAC permissions for the pod
  ktl analyze my-app-pod-123 --rbac
  
  # Automatically apply AI-suggested fixes (Use with caution)
  ktl analyze my-app-pod-123 --ai --fix
  
  # Analyze Cluster Health (Nodes, Global Events)
  ktl analyze --cluster`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) > 0 {
				targetPod = args[0]
			}
			return runAnalyze(cmd.Context(), kubeconfig, kubeContext, targetPod, namespace, useAI, aiProvider, aiModel, drift, cost, fix, cluster, profile, rbac)
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Kubernetes namespace (defaults to context)")
	cmd.Flags().BoolVar(&useAI, "ai", false, "Use AI-powered analysis (requires API key)")
	cmd.Flags().StringVar(&aiProvider, "provider", "heuristic", "Analysis provider: heuristic (default), openai, qwen, deepseek, or mock")
	cmd.Flags().StringVar(&aiModel, "model", "", "Override AI model (e.g., gpt-4-turbo, gpt-4o)")
	cmd.Flags().BoolVar(&drift, "drift", false, "Check for configuration drift against 'kubectl.kubernetes.io/last-applied-configuration'")
	cmd.Flags().BoolVar(&cost, "cost", false, "Estimate monthly cost of the pod based on resource requests")
	cmd.Flags().BoolVar(&fix, "fix", false, "Automatically apply the suggested patch if available")
	cmd.Flags().BoolVar(&cluster, "cluster", false, "Run cluster-wide health checks (nodes, system pods)")
	cmd.Flags().BoolVar(&profile, "profile", false, "Profile resource usage (requires metrics-server)")
	cmd.Flags().BoolVar(&rbac, "rbac", false, "Audit RBAC permissions for the pod's ServiceAccount")

	return cmd
}

func runAnalyze(ctx context.Context, kubeconfig, kubeContext *string, podName, namespace string, useAI bool, provider string, model string, drift bool, cost bool, fix bool, cluster bool, profile bool, rbac bool) error {
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
	if useAI || provider != "heuristic" {
		modelDisplay := model
		if modelDisplay == "" {
			modelDisplay = os.Getenv("KTL_AI_MODEL")
			if modelDisplay == "" {
				modelDisplay = "default"
			}
		}
		fmt.Printf("Using AI Provider: %s (Model: %s)\n", provider, modelDisplay)
	}

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

	// RBAC Audit
	if rbac {
		if evidence.Pod == nil {
			fmt.Println("Error: Cannot audit RBAC without pod details.")
			return nil
		}
		saName := evidence.Pod.Spec.ServiceAccountName
		fmt.Printf("Auditing RBAC for ServiceAccount %s/%s...\n", namespace, saName)

		// List RoleBindings
		rbs, err := kClient.Clientset.RbacV1().RoleBindings(namespace).List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}

		var roles []string
		for _, rb := range rbs.Items {
			for _, s := range rb.Subjects {
				if s.Kind == "ServiceAccount" && s.Name == saName && (s.Namespace == "" || s.Namespace == namespace) {
					roles = append(roles, fmt.Sprintf("Role/%s", rb.RoleRef.Name))

					// Fetch Role to show rules
					role, err := kClient.Clientset.RbacV1().Roles(namespace).Get(ctx, rb.RoleRef.Name, metav1.GetOptions{})
					if err == nil {
						fmt.Printf("  Role: %s\n", rb.RoleRef.Name)
						for _, rule := range role.Rules {
							fmt.Printf("    - %v %v\n", rule.Verbs, rule.Resources)
						}
					}
				}
			}
		}

		// List ClusterRoleBindings
		crbs, err := kClient.Clientset.RbacV1().ClusterRoleBindings().List(ctx, metav1.ListOptions{})
		if err != nil {
			return err
		}
		for _, crb := range crbs.Items {
			for _, s := range crb.Subjects {
				if s.Kind == "ServiceAccount" && s.Name == saName && (s.Namespace == "" || s.Namespace == namespace) {
					roles = append(roles, fmt.Sprintf("ClusterRole/%s", crb.RoleRef.Name))

					// Fetch ClusterRole
					role, err := kClient.Clientset.RbacV1().ClusterRoles().Get(ctx, crb.RoleRef.Name, metav1.GetOptions{})
					if err == nil {
						fmt.Printf("  ClusterRole: %s\n", crb.RoleRef.Name)
						for _, rule := range role.Rules {
							fmt.Printf("    - %v %v\n", rule.Verbs, rule.Resources)
						}
					}
				}
			}
		}

		if len(roles) == 0 {
			fmt.Println("No explicit roles found (ServiceAccount might have no permissions).")
		}
		return nil
	}

	// Profiler
	if profile {
		if evidence.Pod == nil {
			fmt.Println("Error: Cannot profile without pod details.")
			return nil
		}
		fmt.Println("Profiling resource usage (fetching metrics)...")
		path := fmt.Sprintf("/apis/metrics.k8s.io/v1beta1/namespaces/%s/pods/%s", namespace, podName)
		data, err := kClient.Clientset.CoreV1().RESTClient().Get().AbsPath(path).DoRaw(ctx)
		if err != nil {
			if strings.Contains(err.Error(), "the server could not find the requested resource") || strings.Contains(err.Error(), "NotFound") {
				fmt.Println("Warning: No metrics available for this pod (it might be crashing, too new, or metrics-server is lagging).")
				return nil
			}
			return fmt.Errorf("failed to fetch metrics (is metrics-server installed?): %w", err)
		}

		type PodMetrics struct {
			Containers []struct {
				Name  string `json:"name"`
				Usage struct {
					CPU    string `json:"cpu"`
					Memory string `json:"memory"`
				} `json:"usage"`
			} `json:"containers"`
		}

		var metrics PodMetrics
		if err := json.Unmarshal(data, &metrics); err != nil {
			return fmt.Errorf("failed to parse metrics: %w", err)
		}

		for _, c := range metrics.Containers {
			fmt.Printf("Container: %s\n", c.Name)

			// Parse Usage
			cpuUsage, _ := resource.ParseQuantity(c.Usage.CPU)
			memUsage, _ := resource.ParseQuantity(c.Usage.Memory)

			fmt.Printf("  Usage:    CPU: %s, Mem: %s\n", c.Usage.CPU, c.Usage.Memory)

			// Find Spec
			for _, specC := range evidence.Pod.Spec.Containers {
				if specC.Name == c.Name {
					// Requests
					if req, ok := specC.Resources.Requests[corev1.ResourceCPU]; ok {
						fmt.Printf("  Request:  CPU: %s (Usage: %.0f%%)\n", req.String(), float64(cpuUsage.MilliValue())/float64(req.MilliValue())*100)
					}
					if req, ok := specC.Resources.Requests[corev1.ResourceMemory]; ok {
						fmt.Printf("  Request:  Mem: %s (Usage: %.0f%%)\n", req.String(), float64(memUsage.Value())/float64(req.Value())*100)
					}
					// Limits
					if lim, ok := specC.Resources.Limits[corev1.ResourceCPU]; ok {
						fmt.Printf("  Limit:    CPU: %s (Usage: %.0f%%)\n", lim.String(), float64(cpuUsage.MilliValue())/float64(lim.MilliValue())*100)
					}
					if lim, ok := specC.Resources.Limits[corev1.ResourceMemory]; ok {
						fmt.Printf("  Limit:    Mem: %s (Usage: %.0f%%)\n", lim.String(), float64(memUsage.Value())/float64(lim.Value())*100)
					}
				}
			}
		}
		return nil
	}

	// 4. Run Analysis
	var analyzer analyze.Analyzer
	if useAI || provider != "heuristic" {
		if provider == "heuristic" {
			provider = "mock" // Fallback if --ai is set but no provider
		}
		analyzer = analyze.NewAIAnalyzer(provider, model)
	} else {
		analyzer = analyze.NewHeuristicAnalyzer()
	}

	stop := ui.StartSpinner(os.Stdout, "Running analysis...")
	diagnosis, err := analyzer.Analyze(ctx, evidence)
	if err != nil {
		if errors.Is(err, analyze.ErrQuotaExceeded) {
			stop(false)
			fmt.Println("AI provider quota exceeded. Falling back to heuristic analysis.")
			analyzer = analyze.NewHeuristicAnalyzer()
			stop = ui.StartSpinner(os.Stdout, "Running heuristic analysis...")
			diagnosis, err = analyzer.Analyze(ctx, evidence)
			stop(err == nil)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}
		} else {
			stop(false)
			return fmt.Errorf("analysis failed: %w", err)
		}
	} else {
		stop(true)
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
				// Try merging patch strategy if default fails (often needed for arrays)
				cmdMerge := exec.Command("kubectl", "patch", targetKind, targetName, "-n", namespace, "--type=json", "--patch", diagnosis.Patch)
				outMerge, errMerge := cmdMerge.CombinedOutput()
				if errMerge == nil {
					color.New(color.FgGreen).Println("Patch applied successfully (using JSON patch type)!")
					fmt.Println(string(outMerge))
				} else {
					// Fallback to merge patch if JSON patch failed
					cmdStrategic := exec.Command("kubectl", "patch", targetKind, targetName, "-n", namespace, "--type=strategic", "--patch", diagnosis.Patch)
					outStrategic, errStrategic := cmdStrategic.CombinedOutput()
					if errStrategic == nil {
						color.New(color.FgGreen).Println("Patch applied successfully (using Strategic patch type)!")
						fmt.Println(string(outStrategic))
					} else {
						color.New(color.FgRed).Printf("Patch failed: %v\n%s\n", err, string(out))
						color.New(color.FgRed).Printf("JSON Patch failed: %v\n%s\n", errMerge, string(outMerge))
					}
				}
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
		// Use streaming chat for better UX
		fmt.Print("\n")

		var responseBuilder strings.Builder
		response, err := ai.StreamChat(ctx, history, func(chunk string) {
			fmt.Print(chunk)
			responseBuilder.WriteString(chunk)
		})

		fmt.Println() // Newline after stream finishes

		if err != nil {
			color.New(color.FgRed).Printf("Error: %v\n", err)
			continue
		}

		// Render markdown nicely using Glamour
		// If the response contains markdown features, we re-print the styled version
		// This might be a bit duplicated, but it's much easier to read for code blocks.
		if strings.Contains(response, "```") || strings.Contains(response, "# ") {
			r, _ := glamour.NewTermRenderer(
				glamour.WithAutoStyle(),
				glamour.WithWordWrap(100),
			)
			out, err := r.Render(response)
			if err == nil {
				fmt.Println()
				color.New(color.FgHiBlack).Println("--- Formatted View ---")
				fmt.Println(out)
			}
		}

		// Assistant response
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
