package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/ui"
	"github.com/example/ktl/internal/verify"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

func newVerifyCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	var explain string
	var rulesDir string
	var explained bool

	cmd := &cobra.Command{
		Use:   "verify <chart|namespace>",
		Short: "Verify Kubernetes configuration for security and policy issues",
		Example: strings.TrimSpace(`
  # Verify a chart render (warn-only by default)
  ktl verify chart --chart ./chart --release myapp --namespace default

  # Enforce regression gating vs a baseline
  ktl verify chart --chart ./chart --release myapp --baseline verify-baseline.json --exit-on-delta --mode block

  # Verify a live namespace
  ktl verify namespace default --mode warn
`),
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			if strings.TrimSpace(rulesDir) == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			if strings.TrimSpace(explain) == "" {
				return nil
			}
			rs, err := verify.LoadRuleset(rulesDir)
			if err != nil {
				return err
			}
			want := strings.TrimSpace(explain)
			for _, r := range rs.Rules {
				if r.ID == want {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\nSeverity: %s\nCategory: %s\nHelp: %s\n\n%s\n", r.ID, r.Severity, r.Category, r.HelpURL, r.Description)
					explained = true
					return nil
				}
			}
			return fmt.Errorf("unknown rule %q", want)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if explained {
				return nil
			}
			if len(args) == 0 {
				fmt.Fprintln(cmd.ErrOrStderr(), "Choose one:")
				fmt.Fprintln(cmd.ErrOrStderr(), "  ktl verify chart --chart <path> --release <name> [flags]")
				fmt.Fprintln(cmd.ErrOrStderr(), "  ktl verify namespace <name> [flags]")
				fmt.Fprintln(cmd.ErrOrStderr())
			}
			return cmd.Help()
		},
	}

	cmd.AddCommand(
		newVerifyChartCommand(kubeconfigPath, kubeContext, logLevel),
		newVerifyNamespaceCommand(kubeconfigPath, kubeContext, logLevel),
	)

	cmd.PersistentFlags().StringVar(&explain, "explain", "", "Explain a rule ID (example: k8s/container_is_privileged)")
	cmd.PersistentFlags().StringVar(&rulesDir, "rules-dir", "", "Rules directory (defaults to the pinned builtin rules)")

	decorateCommandHelp(cmd, "Verify Flags")

	// Cobra always registers a `help` child command, which makes generic "Subcommands" rendering noisy.
	// Keep `ktl verify --help` focused on the two operational modes users care about.
	cmd.SetHelpTemplate(`{{with or .Long .Short}}{{. | trimTrailingWhitespaces}}{{end}}

Usage:
  {{.UseLine}}

Modes:
{{- with (indexCommand .Commands "chart") }}
  {{rpad .Name .NamePadding}} {{.Short}}
{{- end }}
{{- with (indexCommand .Commands "namespace") }}
  {{rpad .Name .NamePadding}} {{.Short}}
{{- end }}

Verify Flags:
{{flagUsages .LocalFlags}}

{{ if .HasAvailableInheritedFlags}}
Global Flags:
{{flagUsages .InheritedFlags}}
{{ end}}
`)

	return cmd
}

func newVerifyChartCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	var chartRef string
	var release string
	var namespace string
	var valuesFiles []string
	var setValues []string
	var format string
	var mode string
	var rulesDir string
	var failOn string
	var outputPath string
	var baselinePath string
	var policyRef string
	var policyMode string
	var fixPlan bool
	var exposure bool
	var exposureOutput string
	var exitOnDelta bool
	var baselineWrite string

	cmd := &cobra.Command{
		Use:   "chart --chart <path> --release <name>",
		Short: "Verify a Helm chart by rendering and scanning namespaced resources",
		Long:  "Verify a Helm chart by rendering and scanning namespaced resources.\n\nRequired flags: --chart, --release.",
		Example: strings.TrimSpace(`
  # Verify a local chart render
  ktl verify chart --chart ./chart --release myapp --namespace default

  # Compare against a baseline and fail on regressions
  ktl verify chart --chart ./chart --release myapp --baseline verify-baseline.json --exit-on-delta --mode block
`),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			_ = args
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			if strings.TrimSpace(chartRef) == "" || strings.TrimSpace(release) == "" {
				return fmt.Errorf("--chart and --release are required")
			}

			modeValue := verify.Mode(strings.ToLower(strings.TrimSpace(mode)))
			failOnValue := verify.Severity(strings.ToLower(strings.TrimSpace(failOn)))
			target := fmt.Sprintf("chart %s (release=%s ns=%s)", strings.TrimSpace(chartRef), strings.TrimSpace(release), strings.TrimSpace(namespace))

			errOut := cmd.ErrOrStderr()
			var console *verify.Console
			finishConsole := func() {
				if console == nil {
					return
				}
				console.Done()
				console = nil
			}
			if isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
				console = verify.NewConsole(errOut, verify.ConsoleMeta{
					Target:     target,
					Mode:       modeValue,
					FailOn:     failOnValue,
					PolicyRef:  strings.TrimSpace(policyRef),
					PolicyMode: strings.TrimSpace(policyMode),
				}, verify.ConsoleOptions{
					Enabled: true,
					Width:   width,
					Color:   !noColor,
					Now:     func() time.Time { return time.Now().UTC() },
				})
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Target: target, Phase: "render"})
			}
			var emit verify.Emitter
			if console != nil {
				emit = func(ev verify.Event) error {
					console.Observe(ev)
					return nil
				}
			}

			settings := cli.New()
			settings.KubeConfig = strings.TrimSpace(deref(kubeconfigPath))
			if settings.KubeConfig == "" {
				settings.KubeConfig = os.Getenv("KUBECONFIG")
			}
			if v := strings.TrimSpace(deref(kubeContext)); v != "" {
				settings.KubeContext = v
			}

			actionCfg := new(action.Configuration)
			if err := actionCfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(format string, args ...interface{}) {
				_ = logLevel
			}); err != nil {
				return err
			}

			result, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
				Chart:       chartRef,
				ReleaseName: release,
				Namespace:   namespace,
				ValuesFiles: valuesFiles,
				SetValues:   setValues,
				// Match ktl apply: cluster-aware rendering and CRDs included so
				// verify-before-apply continuity can compare digests reliably.
				UseCluster:  true,
				IncludeCRDs: true,
			})
			if err != nil {
				return err
			}
			if console != nil {
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "decode"})
			}
			objs, err := verify.DecodeK8SYAML(result.Manifest)
			if err != nil {
				return err
			}
			if strings.TrimSpace(rulesDir) == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			if console != nil {
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
			}
			rep, err := verify.VerifyObjectsWithEmitter(ctx, target, objs, verify.Options{
				Mode:     modeValue,
				FailOn:   failOnValue,
				Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(format))),
				RulesDir: rulesDir,
			}, emit)
			if err != nil {
				return err
			}

			rep.Inputs = append(rep.Inputs, verify.Input{
				Kind:           "chart",
				Chart:          strings.TrimSpace(chartRef),
				Release:        strings.TrimSpace(release),
				Namespace:      strings.TrimSpace(namespace),
				RenderedSHA256: verify.ManifestDigestSHA256(result.Manifest),
			})

			if strings.TrimSpace(policyRef) != "" {
				if console != nil {
					console.Observe(verify.Event{
						Type:       verify.EventProgress,
						When:       time.Now().UTC(),
						Phase:      "policy",
						PolicyRef:  strings.TrimSpace(policyRef),
						PolicyMode: strings.TrimSpace(policyMode),
					})
				}
				pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: policyRef, Mode: policyMode}, objs)
				if err != nil {
					return err
				}
				policyFindings := verify.PolicyReportToFindings(pol)
				rep.Findings = append(rep.Findings, policyFindings...)
				if console != nil {
					for i := range policyFindings {
						f := policyFindings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
				}
				if strings.EqualFold(strings.TrimSpace(policyMode), "enforce") && pol != nil && pol.DenyCount > 0 {
					rep.Blocked = true
					rep.Passed = false
				}
			}

			if strings.TrimSpace(baselinePath) != "" {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "baseline"})
				}
				base, err := verify.LoadReport(baselinePath)
				if err != nil {
					return err
				}
				delta := verify.ComputeDelta(rep, base)
				if exitOnDelta && len(delta.NewOrChanged) > 0 {
					// Enforce regression gate regardless of warn/block modes.
					rep.Blocked = true
					rep.Passed = false
				}
				rep.Findings = delta.NewOrChanged
				rep.Summary = verify.Summary{Total: len(rep.Findings), BySev: map[verify.Severity]int{}}
				for _, f := range rep.Findings {
					rep.Summary.BySev[f.Severity]++
				}
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventReset, When: time.Now().UTC()})
					for i := range rep.Findings {
						f := rep.Findings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
					s := rep.Summary
					console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				}
			}

			if exposure {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "exposure"})
				}
				ex := verify.AnalyzeExposure(objs)
				rep.Exposure = &ex
				if strings.TrimSpace(exposureOutput) != "" {
					if w, c, err := openOutput(cmd.ErrOrStderr(), exposureOutput); err == nil {
						_ = verify.WriteExposureJSON(w, &ex)
						if c != nil {
							_ = c.Close()
						}
					}
				}
			}

			if strings.TrimSpace(baselineWrite) != "" {
				// Write the current report JSON for use as a future baseline.
				if w, c, err := openOutput(cmd.ErrOrStderr(), baselineWrite); err == nil {
					_ = verify.WriteReport(w, rep, verify.OutputJSON)
					if c != nil {
						_ = c.Close()
					}
				}
			}

			if console != nil {
				s := rep.Summary
				console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				console.Observe(verify.Event{Type: verify.EventDone, When: time.Now().UTC(), Passed: rep.Passed, Blocked: rep.Blocked})
			}
			finishConsole()
			out, closer, err := openOutput(cmd.OutOrStdout(), outputPath)
			if err != nil {
				return err
			}
			defer func() {
				if closer != nil {
					_ = closer.Close()
				}
			}()
			if err := verify.WriteReport(out, rep, repModeFormat(format)); err != nil {
				return err
			}
			if fixPlan && (strings.TrimSpace(outputPath) == "" || strings.TrimSpace(outputPath) == "-") && repModeFormat(format) == verify.OutputTable {
				plan := verify.BuildFixPlan(rep.Findings)
				if text := verify.RenderFixPlanText(plan); text != "" {
					fmt.Fprint(cmd.OutOrStdout(), text)
				}
			}
			if rep.Blocked {
				return fmt.Errorf("verify blocked (fail-on=%s)", failOn)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&chartRef, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&release, "release", "", "Release name")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace (defaults to active context)")
	cmd.Flags().StringSliceVarP(&valuesFiles, "values", "f", nil, "Values files (repeatable)")
	cmd.Flags().StringArrayVar(&setValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, sarif")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Mode: warn, block, off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Block threshold when --mode=block: info, low, medium, high, critical")
	cmd.Flags().StringVar(&rulesDir, "rules-dir", "", "Rules directory (defaults to the pinned builtin rules)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write the report to this path (use '-' for stdout)")
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "Only report new/changed findings vs this baseline report JSON")
	cmd.Flags().StringVar(&policyRef, "policy", "", "Policy bundle ref (dir/tar/https) to evaluate against rendered objects")
	cmd.Flags().StringVar(&policyMode, "policy-mode", "warn", "Policy mode: warn or enforce")
	cmd.Flags().BoolVar(&fixPlan, "fix-plan", false, "Print suggested patch snippets for known findings (table output only)")
	cmd.Flags().BoolVar(&exposure, "exposure", false, "Include exposure analysis (Ingress/Service to pods/workloads) in the report")
	cmd.Flags().StringVar(&exposureOutput, "exposure-output", "", "Write exposure graph JSON to this path (use '-' for stdout)")
	cmd.Flags().BoolVar(&exitOnDelta, "exit-on-delta", false, "When using --baseline, fail if any new/changed findings exist")
	cmd.Flags().StringVar(&baselineWrite, "baseline-write", "", "Write the current report JSON to this path for use as a future baseline")
	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")
	decorateCommandHelp(cmd, "Verify Chart Flags")
	return cmd
}

func newVerifyNamespaceCommand(kubeconfigPath *string, kubeContext *string, logLevel *string) *cobra.Command {
	var format string
	var mode string
	var rulesDir string
	var failOn string
	var outputPath string
	var baselinePath string
	var policyRef string
	var policyMode string
	var fixPlan bool
	var exposure bool
	var exposureOutput string
	var exitOnDelta bool
	var baselineWrite string

	cmd := &cobra.Command{
		Use:   "namespace <name>",
		Short: "Verify a live namespace by scanning namespaced resources only",
		Example: strings.TrimSpace(`
  # Verify a live namespace
  ktl verify namespace default --mode warn

  # Enforce policy and block at the chosen severity threshold
  ktl verify namespace default --policy ./policy-bundle --policy-mode enforce --mode block --fail-on high
`),
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}
			modeValue := verify.Mode(strings.ToLower(strings.TrimSpace(mode)))
			failOnValue := verify.Severity(strings.ToLower(strings.TrimSpace(failOn)))
			client, err := kube.New(ctx, strings.TrimSpace(deref(kubeconfigPath)), strings.TrimSpace(deref(kubeContext)))
			if err != nil {
				return err
			}
			namespace := strings.TrimSpace(args[0])
			if namespace == "" {
				return fmt.Errorf("namespace is required")
			}
			target := fmt.Sprintf("namespace %s", namespace)

			errOut := cmd.ErrOrStderr()
			var console *verify.Console
			finishConsole := func() {
				if console == nil {
					return
				}
				console.Done()
				console = nil
			}
			if isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				noColor, _ := cmd.Root().PersistentFlags().GetBool("no-color")
				console = verify.NewConsole(errOut, verify.ConsoleMeta{
					Target:     target,
					Mode:       modeValue,
					FailOn:     failOnValue,
					PolicyRef:  strings.TrimSpace(policyRef),
					PolicyMode: strings.TrimSpace(policyMode),
				}, verify.ConsoleOptions{
					Enabled: true,
					Width:   width,
					Color:   !noColor,
					Now:     func() time.Time { return time.Now().UTC() },
				})
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Target: target, Phase: "collect"})
			}
			var emit verify.Emitter
			if console != nil {
				emit = func(ev verify.Event) error {
					console.Observe(ev)
					return nil
				}
			}

			objs, err := collectNamespacedObjects(ctx, client, namespace)
			if err != nil {
				return err
			}
			if strings.TrimSpace(rulesDir) == "" {
				rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
			}
			if console != nil {
				console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
			}
			rep, err := verify.VerifyObjectsWithEmitter(ctx, target, objs, verify.Options{
				Mode:     modeValue,
				FailOn:   failOnValue,
				Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(format))),
				RulesDir: rulesDir,
			}, emit)
			if err != nil {
				return err
			}
			rep.Inputs = append(rep.Inputs, verify.Input{
				Kind:            "namespace",
				Namespace:       namespace,
				CollectedAtHint: "live",
			})

			if strings.TrimSpace(policyRef) != "" {
				if console != nil {
					console.Observe(verify.Event{
						Type:       verify.EventProgress,
						When:       time.Now().UTC(),
						Phase:      "policy",
						PolicyRef:  strings.TrimSpace(policyRef),
						PolicyMode: strings.TrimSpace(policyMode),
					})
				}
				pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: policyRef, Mode: policyMode}, objs)
				if err != nil {
					return err
				}
				policyFindings := verify.PolicyReportToFindings(pol)
				rep.Findings = append(rep.Findings, policyFindings...)
				if console != nil {
					for i := range policyFindings {
						f := policyFindings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
				}
				if strings.EqualFold(strings.TrimSpace(policyMode), "enforce") && pol != nil && pol.DenyCount > 0 {
					rep.Blocked = true
					rep.Passed = false
				}
			}

			if strings.TrimSpace(baselinePath) != "" {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "baseline"})
				}
				base, err := verify.LoadReport(baselinePath)
				if err != nil {
					return err
				}
				delta := verify.ComputeDelta(rep, base)
				if exitOnDelta && len(delta.NewOrChanged) > 0 {
					rep.Blocked = true
					rep.Passed = false
				}
				rep.Findings = delta.NewOrChanged
				rep.Summary = verify.Summary{Total: len(rep.Findings), BySev: map[verify.Severity]int{}}
				for _, f := range rep.Findings {
					rep.Summary.BySev[f.Severity]++
				}
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventReset, When: time.Now().UTC()})
					for i := range rep.Findings {
						f := rep.Findings[i]
						console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
					}
					s := rep.Summary
					console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				}
			}

			if exposure {
				if console != nil {
					console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "exposure"})
				}
				ex := verify.AnalyzeExposure(objs)
				rep.Exposure = &ex
				if strings.TrimSpace(exposureOutput) != "" {
					if w, c, err := openOutput(cmd.ErrOrStderr(), exposureOutput); err == nil {
						_ = verify.WriteExposureJSON(w, &ex)
						if c != nil {
							_ = c.Close()
						}
					}
				}
			}

			if strings.TrimSpace(baselineWrite) != "" {
				// Write the current report JSON for use as a future baseline.
				if w, c, err := openOutput(cmd.ErrOrStderr(), baselineWrite); err == nil {
					_ = verify.WriteReport(w, rep, verify.OutputJSON)
					if c != nil {
						_ = c.Close()
					}
				}
			}

			if console != nil {
				s := rep.Summary
				console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
				console.Observe(verify.Event{Type: verify.EventDone, When: time.Now().UTC(), Passed: rep.Passed, Blocked: rep.Blocked})
			}
			finishConsole()
			out, closer, err := openOutput(cmd.OutOrStdout(), outputPath)
			if err != nil {
				return err
			}
			defer func() {
				if closer != nil {
					_ = closer.Close()
				}
			}()
			if err := verify.WriteReport(out, rep, repModeFormat(format)); err != nil {
				return err
			}
			if fixPlan && (strings.TrimSpace(outputPath) == "" || strings.TrimSpace(outputPath) == "-") && repModeFormat(format) == verify.OutputTable {
				plan := verify.BuildFixPlan(rep.Findings)
				if text := verify.RenderFixPlanText(plan); text != "" {
					fmt.Fprint(cmd.OutOrStdout(), text)
				}
			}
			if rep.Blocked {
				return fmt.Errorf("verify blocked (fail-on=%s)", failOn)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, sarif")
	cmd.Flags().StringVar(&mode, "mode", "warn", "Mode: warn, block, off")
	cmd.Flags().StringVar(&failOn, "fail-on", "high", "Block threshold when --mode=block: info, low, medium, high, critical")
	cmd.Flags().StringVar(&rulesDir, "rules-dir", "", "Rules directory (defaults to the pinned builtin rules)")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write the report to this path (use '-' for stdout)")
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "Only report new/changed findings vs this baseline report JSON")
	cmd.Flags().StringVar(&policyRef, "policy", "", "Policy bundle ref (dir/tar/https) to evaluate against live objects")
	cmd.Flags().StringVar(&policyMode, "policy-mode", "warn", "Policy mode: warn or enforce")
	cmd.Flags().BoolVar(&fixPlan, "fix-plan", false, "Print suggested patch snippets for known findings (table output only)")
	cmd.Flags().BoolVar(&exposure, "exposure", false, "Include exposure analysis (Ingress/Service to pods/workloads) in the report")
	cmd.Flags().StringVar(&exposureOutput, "exposure-output", "", "Write exposure graph JSON to this path (use '-' for stdout)")
	cmd.Flags().BoolVar(&exitOnDelta, "exit-on-delta", false, "When using --baseline, fail if any new/changed findings exist")
	cmd.Flags().StringVar(&baselineWrite, "baseline-write", "", "Write the current report JSON to this path for use as a future baseline")
	_ = logLevel
	decorateCommandHelp(cmd, "Verify Namespace Flags")
	return cmd
}

func repModeFormat(v string) verify.OutputFormat {
	v = strings.ToLower(strings.TrimSpace(v))
	switch v {
	case "", "table":
		return verify.OutputTable
	case "json":
		return verify.OutputJSON
	case "sarif":
		return verify.OutputSARIF
	default:
		return verify.OutputTable
	}
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func openOutput(defaultWriter io.Writer, path string) (io.Writer, io.Closer, error) {
	path = strings.TrimSpace(path)
	if path == "" || path == "-" {
		return defaultWriter, nil, nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, nil, err
	}
	f, err := os.Create(path)
	if err != nil {
		return nil, nil, err
	}
	return f, f, nil
}
