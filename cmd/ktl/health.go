// health.go powers 'ktl diag health', a scorecard-driven automation command.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/report"
	"github.com/example/ktl/internal/ui"
	"github.com/spf13/cobra"
)

func newHealthCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := report.NewOptions()
	opts.IncludeIngress = false
	var outputJSON bool
	var failOn string
	cmd := &cobra.Command{
		Use:   "health",
		Short: "Run automated scorecard checks and emit machine-friendly results",
		Example: `  # Run the default namespace scorecard
  ktl diag health

  # Scan all namespaces, emit JSON, and fail on warnings
  ktl diag health -A --json --fail-on warn`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			mode, err := parseFailOnMode(failOn)
			if err != nil {
				return err
			}
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			spinnerStop := ui.StartSpinner(cmd.ErrOrStderr(), "Collecting health scorecard")
			finished := false
			stop := func(ok bool) {
				if finished {
					return
				}
				spinnerStop(ok)
				finished = true
			}
			defer stop(false)
			data, namespaces, err := report.Collect(ctx, kubeClient, opts)
			if err != nil {
				return err
			}
			stop(true)
			card := data.Scorecard
			failures, warnings := summarizeScorecard(card)
			if outputJSON {
				payload := healthPayload{
					GeneratedAt: card.GeneratedAt,
					Cluster:     kubeClient.RESTConfig.Host,
					Namespaces:  namespaces,
					Scorecard:   card,
					Failures:    failures,
					Warnings:    warnings,
				}
				body, err := json.MarshalIndent(payload, "", "  ")
				if err != nil {
					return fmt.Errorf("encode health payload: %w", err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), string(body))
			} else {
				renderHealthTable(cmd.OutOrStdout(), card)
				fmt.Fprintf(cmd.OutOrStdout(), "\nAverage: %.1f%% across %d checks\n", card.Average, len(card.Checks))
				fmt.Fprintf(cmd.OutOrStdout(), "Namespaces: %s\n", strings.Join(namespaces, ", "))
			}
			if mode.shouldFail(failures, warnings) {
				return fmt.Errorf("health checks reported %d failure(s) and %d warning(s)", failures, warnings)
			}
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&opts.Namespaces, "namespace", "n", nil, "Namespaces to include (defaults to the current context namespace)")
	cmd.Flags().BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", false, "Include every namespace")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output the scorecard payload as JSON for automation pipelines")
	cmd.Flags().StringVar(&failOn, "fail-on", "fail", "Exit with status 1 when health checks reach this severity (never|warn|fail)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Health Flags")
	return cmd
}

type healthPayload struct {
	GeneratedAt time.Time        `json:"generatedAt"`
	Cluster     string           `json:"cluster"`
	Namespaces  []string         `json:"namespaces"`
	Scorecard   report.Scorecard `json:"scorecard"`
	Failures    int              `json:"failures"`
	Warnings    int              `json:"warnings"`
}

func renderHealthTable(w io.Writer, card report.Scorecard) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "STATUS\tCHECK\tSCORE\tSUMMARY")
	for _, check := range card.Checks {
		status := statusBadge(check.Status)
		score := scoreText(check)
		summary := strings.TrimSpace(check.Summary)
		if summary == "" {
			summary = "(no summary available)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", status, check.Name, score, summary)
		for _, detail := range check.Details {
			detail = strings.TrimSpace(detail)
			if detail == "" {
				continue
			}
			fmt.Fprintf(tw, "\t\t\t- %s\n", detail)
		}
		if strings.TrimSpace(check.Command) != "" {
			fmt.Fprintf(tw, "\t\t\tRun: %s\n", check.Command)
		}
	}
	_ = tw.Flush()
}

func summarizeScorecard(card report.Scorecard) (failures int, warnings int) {
	for _, check := range card.Checks {
		switch check.Status {
		case report.ScoreStatusFail:
			failures++
		case report.ScoreStatusWarn:
			warnings++
		}
	}
	return failures, warnings
}

func scoreText(check report.ScoreCheck) string {
	if check.Status == report.ScoreStatusUnknown {
		return "-"
	}
	return fmt.Sprintf("%.1f%%", check.Score)
}

func statusBadge(status report.ScoreStatus) string {
	switch status {
	case report.ScoreStatusPass:
		return "PASS"
	case report.ScoreStatusWarn:
		return "WARN"
	case report.ScoreStatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

type failOnMode int

const (
	failNever failOnMode = iota
	failWarn
	failFail
)

func parseFailOnMode(value string) (failOnMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "fail":
		return failFail, nil
	case "warn":
		return failWarn, nil
	case "never", "none":
		return failNever, nil
	default:
		return failFail, fmt.Errorf("unsupported fail-on value %q (use never|warn|fail)", value)
	}
}

func (m failOnMode) shouldFail(failures, warnings int) bool {
	switch m {
	case failNever:
		return false
	case failWarn:
		return failures > 0 || warnings > 0
	case failFail:
		return failures > 0
	default:
		return failures > 0
	}
}
