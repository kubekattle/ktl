package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/ingresslabs/torque/internal/agentappliance"
	"github.com/spf13/cobra"
)

type agentApplianceOptions struct {
	OutDir         string
	Task           string
	Actor          string
	BrowserMode    string
	BrowserURLs    []string
	APIURLs        []string
	Checks         []string
	Timeout        time.Duration
	MaxOutputBytes int
	Format         string
}

func newAgentApplianceCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "appliance",
		Short: "Collect agent workbench evidence",
		Long:  "Collect a file-first evidence bundle for AI agent work: repo intelligence, API probes, browser captures, and command checks.",
	}
	cmd.AddCommand(newAgentApplianceRunCommand())
	decorateCommandHelp(cmd, "Agent Appliance Commands")
	return cmd
}

func newAgentApplianceRunCommand() *cobra.Command {
	opts := agentApplianceOptions{
		Actor:          "agent",
		BrowserMode:    "headless",
		Timeout:        2 * time.Minute,
		MaxOutputBytes: 65536,
		Format:         "text",
	}
	cmd := &cobra.Command{
		Use:   "run [repo-dir]",
		Short: "Run repo, browser, API, and check evidence collectors",
		Long:  "Run the agent appliance workbenches and write a deterministic evidence directory that Codex, Claude, OpenCode, CI, and humans can inspect.",
		Args:  cobra.MaximumNArgs(1),
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateAgentApplianceOptions(opts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			repoDir := "."
			if len(args) > 0 {
				repoDir = args[0]
			}
			report, err := agentappliance.Run(cmd.Context(), agentappliance.Options{
				RepoDir:        repoDir,
				OutDir:         opts.OutDir,
				Task:           opts.Task,
				Actor:          opts.Actor,
				BrowserMode:    opts.BrowserMode,
				BrowserURLs:    opts.BrowserURLs,
				APIURLs:        opts.APIURLs,
				Checks:         opts.Checks,
				Timeout:        opts.Timeout,
				MaxOutputBytes: opts.MaxOutputBytes,
			})
			if err != nil {
				return err
			}
			if strings.EqualFold(strings.TrimSpace(opts.Format), "json") {
				raw, err := json.MarshalIndent(report, "", "  ")
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "%s\n", raw)
			} else {
				renderAgentApplianceText(cmd.OutOrStdout(), report)
			}
			if !report.Passed {
				return fmt.Errorf("agent appliance evidence failed")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&opts.OutDir, "out-dir", "", "Evidence output directory (default .torque/agent-appliance/<timestamp> under the repo)")
	cmd.Flags().StringVar(&opts.Task, "task", "", "Task description recorded in the evidence manifest")
	cmd.Flags().StringVar(&opts.Actor, "actor", opts.Actor, "Actor identity recorded in the evidence manifest")
	cmd.Flags().StringArrayVar(&opts.BrowserURLs, "browser-url", nil, "Browser URL to capture with Playwright (repeatable or comma-separated)")
	cmd.Flags().StringArrayVar(&opts.APIURLs, "api-url", nil, "HTTP API URL to probe with GET (repeatable or comma-separated)")
	cmd.Flags().StringArrayVar(&opts.Checks, "check", nil, "Shell command to run from the repo root and capture as redacted evidence (repeatable)")
	cmd.Flags().Var(newEnumStringValue(&opts.BrowserMode, "auto", "headless", "visible"), "browser-mode", "Browser mode: auto, headless, or visible")
	cmd.Flags().DurationVar(&opts.Timeout, "timeout", opts.Timeout, "Timeout per API, browser, or command check")
	cmd.Flags().IntVar(&opts.MaxOutputBytes, "max-output-bytes", opts.MaxOutputBytes, "Maximum captured bytes per command stream, API body, or DOM artifact")
	cmd.Flags().StringVar(&opts.Format, "format", opts.Format, "Output format: text or json")
	decorateCommandHelp(cmd, "Agent Appliance Run Flags")
	return cmd
}

func validateAgentApplianceOptions(opts agentApplianceOptions) error {
	if err := validateAgentFormat(opts.Format); err != nil {
		return err
	}
	switch strings.ToLower(strings.TrimSpace(opts.BrowserMode)) {
	case "", "auto", "headless", "visible":
	default:
		return fmt.Errorf("unsupported --browser-mode %q (expected auto, headless, or visible)", opts.BrowserMode)
	}
	if opts.Timeout <= 0 {
		return fmt.Errorf("--timeout must be greater than zero")
	}
	if opts.MaxOutputBytes <= 0 {
		return fmt.Errorf("--max-output-bytes must be greater than zero")
	}
	return nil
}

func renderAgentApplianceText(out io.Writer, report *agentappliance.Report) {
	fmt.Fprintf(out, "Agent appliance: %s\n", strings.ToUpper(passFail(report.Passed)))
	fmt.Fprintf(out, "Evidence: %s\n", report.OutDir)
	fmt.Fprintf(out, "Repo: %d files, %d changed, %d dependency manifests, %d tests\n",
		report.Summary.RepoFiles,
		report.Summary.ChangedFiles,
		report.Summary.DependencyManifests,
		report.Summary.TestFiles,
	)
	fmt.Fprintf(out, "API: %d passed, %d failed\n", report.Summary.APIPassed, report.Summary.APIFailed)
	fmt.Fprintf(out, "Browser: %d captured, %d skipped, %d failed\n", report.Summary.BrowserCaptured, report.Summary.BrowserSkipped, report.Summary.BrowserFailed)
	fmt.Fprintf(out, "Checks: %d passed, %d failed, %d timed out\n", report.Summary.CommandChecksPassed, report.Summary.CommandChecksFailed, report.Summary.CommandChecksTimedOut)
	fmt.Fprintf(out, "Manifest: %s\n", report.ManifestPath)
	if report.ManifestSHA256 != "" {
		fmt.Fprintf(out, "Manifest SHA256: %s\n", report.ManifestSHA256)
	}
	for _, warning := range report.Warnings {
		fmt.Fprintf(out, "Warning: %s\n", warning)
	}
}
