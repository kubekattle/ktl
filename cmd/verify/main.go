package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/example/ktl/internal/ui"
	"github.com/example/ktl/internal/verify"
	cfgpkg "github.com/example/ktl/internal/verify/config"
	"github.com/example/ktl/internal/verify/engine"
	"github.com/spf13/cobra"
)

func main() {
	root := newRootCommand()
	if err := root.Execute(); err != nil {
		if errors.Is(err, errUsage) {
			os.Exit(2)
		}
		os.Exit(1)
	}
}

var errUsage = errors.New("usage")

func newRootCommand() *cobra.Command {
	var kubeconfigPath string
	var kubeContext string
	logLevel := "info"
	var noColor bool
	var showVersion bool
	var rulesPath string

	cmd := newVerifyCommand(&kubeconfigPath, &kubeContext, &logLevel, &noColor, &rulesPath)
	cmd.SilenceUsage = true
	cmd.SilenceErrors = false
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for CLI requests")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "c", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().StringVar(&logLevel, "log-level", logLevel, "Log level for output (debug, info, warn, error)")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.PersistentFlags().BoolVar(&showVersion, "version", false, "Print version and exit")
	cmd.PersistentFlags().StringVar(&rulesPath, "rules-path", "", "Extra rules.d search paths (comma/colon-separated)")
	cmd.SetHelpCommand(newHelpCommand(cmd))
	return cmd
}

func newVerifyCommand(kubeconfigPath *string, kubeContext *string, logLevel *string, noColor *bool, rulesPath *string) *cobra.Command {
	var baselineWrite string
	var compareTo string
	var compareExit bool
	var fixPlan bool
	var openReport bool

	// Shortcut flags (no YAML required).
	var chartPath string
	var release string
	var namespace string
	var manifestPath string
	var valuesFiles []string
	var setValues []string
	var useCluster bool
	var includeCRDs bool
	var mode string
	var failOn string
	var format string
	var reportPath string
	var policyRef string
	var policyMode string
	var exposure bool
	var evaluatedAt string

	cmd := &cobra.Command{
		Use:   "verify [config.yaml]",
		Short: "Verify Kubernetes configuration",
		Long: strings.TrimSpace(`
Verify can run from a YAML config file, or directly from flags.

YAML config (recommended for CI):
  verify init chart|manifest|namespace --write verify.yaml
  verify verify.yaml

Shortcut flags (no YAML):
  verify --manifest ./rendered.yaml
  verify --chart ./chart --release foo -n default
  verify --namespace default
`),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) > 1 {
				return fmt.Errorf("expected at most one verify config file")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			if ctx == nil {
				ctx = context.Background()
			}

			var (
				cfg     *cfgpkg.Config
				baseDir string
				err     error
			)
			if len(args) == 1 {
				// Avoid ambiguous mode: config file plus shortcut flags.
				if strings.TrimSpace(chartPath) != "" || strings.TrimSpace(release) != "" || strings.TrimSpace(namespace) != "" || strings.TrimSpace(manifestPath) != "" {
					return fmt.Errorf("provide either a config file or shortcut flags (--chart/--manifest/--namespace), not both")
				}
				cfgPath := strings.TrimSpace(args[0])
				if cfgPath == "" {
					return fmt.Errorf("verify config path is required")
				}
				if _, statErr := os.Stat(cfgPath); statErr != nil {
					return fmt.Errorf("verify config not found: %s", cfgPath)
				}
				cfg, baseDir, err = cfgpkg.Load(cfgPath)
				if err != nil {
					return err
				}
			} else {
				// Build config from flags, using cwd as base dir for relative paths.
				cwd, _ := os.Getwd()
				if strings.TrimSpace(cwd) == "" {
					cwd = "."
				}
				baseDir = cwd
				built, berr := cfgpkg.BuildFromParams(cfgpkg.Params{
					Kind:        "",
					ChartPath:   chartPath,
					Release:     release,
					Namespace:   namespace,
					Manifest:    manifestPath,
					ValuesFiles: valuesFiles,
					SetValues:   setValues,
					UseCluster:  useCluster,
					IncludeCRDs: includeCRDs,
					Mode:        mode,
					FailOn:      failOn,
					Format:      format,
					Report:      reportPath,
					KubeContext: "",
				})
				if berr != nil {
					return berr
				}
				cfg = &built
				// Apply extra options that the param builder doesn't cover.
				cfg.Verify.Policy.Ref = strings.TrimSpace(policyRef)
				cfg.Verify.Policy.Mode = strings.TrimSpace(policyMode)
				cfg.Verify.Exposure.Enabled = exposure
				cfg.ResolvePaths(baseDir)
			}

			if err := cfg.Validate(baseDir); err != nil {
				return err
			}
			if fixPlan {
				cfg.Verify.FixPlan = true
			}

			if strings.TrimSpace(baselineWrite) != "" {
				cfg.Verify.Baseline.Write = cfgpkg.ResolveRelPath(baseDir, baselineWrite)
			}
			if strings.TrimSpace(compareTo) != "" {
				cfg.Verify.Baseline.Read = cfgpkg.ResolveRelPath(baseDir, compareTo)
				if cmd.Flags().Changed("compare-exit") {
					cfg.Verify.Baseline.ExitOnDelta = compareExit
				} else if !cfg.Verify.Baseline.ExitOnDelta {
					cfg.Verify.Baseline.ExitOnDelta = true
				}
			} else if cmd.Flags().Changed("compare-exit") {
				cfg.Verify.Baseline.ExitOnDelta = compareExit
			}

			flagKubeconfigSet := cmd.Flags().Changed("kubeconfig")
			flagContextSet := cmd.Flags().Changed("context")
			if flagKubeconfigSet && strings.TrimSpace(*kubeconfigPath) == "" {
				return fmt.Errorf("--kubeconfig was provided but empty; set a path or drop the flag")
			}
			if flagContextSet && strings.TrimSpace(*kubeContext) == "" {
				return fmt.Errorf("--context was provided but empty; set a name or drop the flag")
			}

			var console *verify.Console
			errOut := cmd.ErrOrStderr()
			if isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				noColorVal := false
				if noColor != nil {
					noColorVal = *noColor
				}
				console = verify.NewConsole(errOut, verify.ConsoleMeta{
					Target:     cfg.TargetLabel(),
					Mode:       verify.Mode(strings.ToLower(strings.TrimSpace(cfg.Verify.Mode))),
					FailOn:     verify.Severity(strings.ToLower(strings.TrimSpace(cfg.Verify.FailOn))),
					PolicyRef:  strings.TrimSpace(cfg.Verify.Policy.Ref),
					PolicyMode: strings.TrimSpace(cfg.Verify.Policy.Mode),
				}, verify.ConsoleOptions{
					Enabled: true,
					Width:   width,
					Color:   !noColorVal,
					Now:     func() time.Time { return time.Now().UTC() },
				})
			}

			out, closer, err := cfgpkg.OpenOutput(cmd.OutOrStdout(), cfg.Output.Report)
			if err != nil {
				return err
			}
			if closer != nil {
				defer closer.Close()
			}

			var nowFn func() time.Time
			if strings.TrimSpace(evaluatedAt) != "" {
				tm, perr := time.Parse(time.RFC3339, strings.TrimSpace(evaluatedAt))
				if perr != nil {
					return fmt.Errorf("--evaluated-at must be RFC3339 (e.g. 2026-02-06T00:00:00Z): %w", perr)
				}
				fixed := tm.UTC()
				nowFn = func() time.Time { return fixed }
			}

			err = engine.Run(ctx, cfg, baseDir, engine.Options{
				Kubeconfig:  *kubeconfigPath,
				KubeContext: *kubeContext,
				LogLevel:    logLevel,
				Console:     console,
				ErrOut:      cmd.ErrOrStderr(),
				Out:         out,
				RulesPath:   splitListLocal(*rulesPath),
				Now:         nowFn,
			})
			if err == nil && openReport {
				// Best effort: only for file reports.
				rp := strings.TrimSpace(cfg.Output.Report)
				if rp != "" && rp != "-" && strings.EqualFold(strings.TrimSpace(cfg.Output.Format), "html") {
					_ = openLocalFile(rp)
				}
			}
			return err
		},
	}

	cmd.Example = strings.TrimSpace(`
  # Generate a starter config then run it
  verify init chart --chart ./chart --release foo -n default --write verify.yaml
  verify verify.yaml
`)

	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\nHint: generate a config with 'verify init chart|manifest|namespace --write verify.yaml' then run 'verify verify.yaml'\n", err)
		}
		return errUsage
	})

	cmd.AddCommand(newVerifyInitCommand())
	cmd.AddCommand(newVerifyRulesCommand(rulesPath))

	// Shortcut flags.
	cmd.Flags().StringVar(&chartPath, "chart", "", "Helm chart path (shortcut mode)")
	cmd.Flags().StringVar(&release, "release", "", "Helm release name (shortcut mode)")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace (shortcut mode: namespace target; or chart namespace)")
	cmd.Flags().StringVar(&manifestPath, "manifest", "", "Manifest YAML path (shortcut mode)")
	cmd.Flags().StringSliceVar(&valuesFiles, "values", nil, "Values file(s) (shortcut mode, kind=chart)")
	cmd.Flags().StringSliceVar(&setValues, "set", nil, "Set value(s) (shortcut mode, kind=chart)")
	cmd.Flags().BoolVar(&useCluster, "use-cluster", false, "Allow cluster lookups while rendering (shortcut mode, kind=chart)")
	cmd.Flags().BoolVar(&includeCRDs, "include-crds", false, "Include CRDs in rendered output (shortcut mode, kind=chart)")
	cmd.Flags().StringVar(&mode, "mode", "", "Verify mode: warn|block|off (shortcut mode)")
	cmd.Flags().StringVar(&failOn, "fail-on", "", "Fail threshold: info|low|medium|high|critical (shortcut mode)")
	cmd.Flags().StringVar(&format, "format", "", "Output format: table|json|sarif|html|md (shortcut mode)")
	cmd.Flags().StringVar(&reportPath, "report", "-", `Report path ("-" for stdout) (shortcut mode)`)
	cmd.Flags().StringVar(&policyRef, "policy-ref", "", "Policy bundle ref (path or URL) (shortcut mode)")
	cmd.Flags().StringVar(&policyMode, "policy-mode", "warn", "Policy mode: warn|enforce (shortcut mode)")
	cmd.Flags().BoolVar(&exposure, "exposure", false, "Enable exposure analysis (shortcut mode)")
	cmd.Flags().BoolVar(&openReport, "open", false, "Open the report after success (HTML file reports only)")
	cmd.Flags().StringVar(&evaluatedAt, "evaluated-at", "", "Override evaluation time (RFC3339) for deterministic reports/tests")

	cmd.Flags().StringVar(&baselineWrite, "baseline", "", "Write a baseline report (JSON) to this path")
	cmd.Flags().StringVar(&compareTo, "compare-to", "", "Compare against a baseline report (JSON) and show only new/changed findings")
	cmd.Flags().BoolVar(&compareExit, "compare-exit", true, "Exit non-zero when --compare-to detects new or changed findings")
	cmd.Flags().BoolVar(&fixPlan, "fix", false, "Print suggested patch snippets for fixable findings (table output only)")

	return cmd
}

func openLocalFile(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	// macOS: open. Linux: xdg-open. Windows not supported here.
	if err := tryExec("open", path); err == nil {
		return nil
	}
	_ = tryExec("xdg-open", path)
	return nil
}

func tryExec(name string, arg string) error {
	cmd := exec.Command(name, arg)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func splitListLocal(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ':' })
	var out []string
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			out = append(out, s)
		}
	}
	return out
}
