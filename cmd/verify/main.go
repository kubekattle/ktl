package main

import (
	"context"
	"errors"
	"fmt"
	"os"
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

	cmd := &cobra.Command{
		Use:   "verify <config.yaml>",
		Short: "Verify Kubernetes configuration",
		Long: strings.TrimSpace(`
Input must be a YAML file. Generate a config with 'verify init' (chart|manifest|namespace), then run:
  verify verify.yaml
`),
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expected exactly one verify config file\n\nHint: generate one with 'verify init chart|manifest|namespace --write verify.yaml' then run 'verify verify.yaml'")
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

			var cfg *cfgpkg.Config
			var baseDir string
			var err error

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

			if err := cfg.Validate(baseDir); err != nil {
				return err
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

			err = engine.Run(ctx, cfg, baseDir, engine.Options{
				Kubeconfig:  *kubeconfigPath,
				KubeContext: *kubeContext,
				LogLevel:    logLevel,
				Console:     console,
				ErrOut:      cmd.ErrOrStderr(),
				Out:         out,
				RulesPath:   splitListLocal(*rulesPath),
			})
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

	return cmd
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
