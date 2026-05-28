package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func newHelmerRootCommand() *cobra.Command {
	var kubeconfigPath string
	var kubeContext string
	var noColor bool
	var showVersion bool

	cmd := &cobra.Command{
		Use:           "helmer <command>",
		Short:         "Standalone Helm review and archive tool",
		Long:          "helmer renders charts, previews live changes, writes shareable HTML reports, and packages chart archives without applying them.",
		Args:          cobra.ArbitraryArgs,
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if noColor {
				color.NoColor = true
				_ = os.Setenv("NO_COLOR", "1")
			} else if os.Getenv("NO_COLOR") != "" {
				color.NoColor = true
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			if showVersion {
				return newVersionCommand().RunE(cmd, nil)
			}
			if len(args) > 0 && looksLikeSubcommandToken(args[0]) {
				fmt.Fprintf(cmd.ErrOrStderr(), "unknown command %q for %q\n\n", args[0], cmd.Name())
			}
			return pflag.ErrHelp
		},
	}
	cmd.SetFlagErrorFunc(func(cmd *cobra.Command, err error) error {
		if err != nil {
			fmt.Fprintf(cmd.ErrOrStderr(), "Error: %v\n\n", err)
		}
		return pflag.ErrHelp
	})
	cmd.CompletionOptions.DisableDefaultCmd = true
	cmd.SetHelpCommand(newHelpCommand(cmd))
	cmd.SetHelpTemplate(helmerRootHelpTemplate())
	cmd.PersistentFlags().StringVarP(&kubeconfigPath, "kubeconfig", "k", "", "Path to the kubeconfig file to use for Kubernetes lookups")
	cmd.PersistentFlags().StringVarP(&kubeContext, "context", "c", "", "Name of the kubeconfig context to use")
	cmd.PersistentFlags().BoolVar(&noColor, "no-color", false, "Disable colored output")
	cmd.Flags().BoolVar(&showVersion, "version", false, "Print version and exit")

	planCmd := newDeployPlanCommand(nil, &kubeconfigPath, &kubeContext, "Plan Flags")
	reportCmd := newHelmerReportCommand(&kubeconfigPath, &kubeContext)
	archiveCmd := newHelmerArchiveCommand()
	verifyArchiveCmd := newHelmerVerifyArchiveCommand()
	unpackCmd := newHelmerUnpackCommand()
	versionCmd := newVersionCommand()

	cmd.AddCommand(planCmd, reportCmd, archiveCmd, verifyArchiveCmd, unpackCmd, versionCmd)
	cmd.Example = strings.TrimSpace(`
  # Preview chart changes
  helmer plan --chart ./chart --release api --namespace prod

  # Write the interactive HTML report
  helmer report --chart ./chart --release api --namespace prod --output ./plan.html

  # Archive a chart for transport or review
  helmer archive ./chart --output ./chart.sqlite
`)
	return cmd
}

func newHelmerReportCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	cmd := newDeployPlanCommand(nil, kubeconfig, kubeContext, "Report Flags")
	origPreRunE := cmd.PreRunE
	cmd.Use = "report"
	cmd.Short = "Render a shareable HTML Helm plan report"
	cmd.Long = "Run the Helm plan engine and emit the interactive HTML report used for review and comparisons."
	cmd.PreRunE = func(cmd *cobra.Command, args []string) error {
		if err := cmd.Flags().Set("visualize", "true"); err != nil {
			return err
		}
		if !cmd.Flags().Changed("format") {
			if err := cmd.Flags().Set("format", "html"); err != nil {
				return err
			}
		}
		if origPreRunE != nil {
			return origPreRunE(cmd, args)
		}
		return nil
	}
	cmd.Example = strings.TrimSpace(`
  # Write a reviewable HTML report
  helmer report --chart ./chart --release api --namespace prod --output ./plan.html

  # Emit the same visualize payload as JSON
  helmer report --chart ./chart --release api --namespace prod --format json --output ./plan.json
`)
	return cmd
}

func helmerRootHelpTemplate() string {
	return `{{with (or .Long .Short)}}{{. | trimTrailingWhitespaces}}
{{end}}
Usage:
  {{.UseLine}}

Subcommands:
{{- range $i, $n := (list "plan" "report" "archive" "verify-archive" "unpack" "version") }}
{{- with (indexCommand $.Commands $n) }}
  {{rpad .Name .NamePadding }} {{.Short}}
{{- end }}
{{- end }}

Flags:
{{flagUsages .LocalFlags}}

{{ if .HasAvailableInheritedFlags}}
Global Flags:
{{flagUsages .InheritedFlags}}
{{ end}}
`
}
