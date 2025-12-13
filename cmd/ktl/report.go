// report.go powers 'ktl diag report', rendering ASCII or HTML health reports for namespaces and optional drift comparisons.
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/report"
	"github.com/example/ktl/internal/ui"
	"github.com/spf13/cobra"
)

func newReportCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	opts := report.NewOptions()
	var renderHTML bool
	var liveMode bool
	var listenAddr string
	var scoreThreshold float64
	var notifyMode string
	var compareLeft string
	var compareRight string
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Summarize namespaces as an ASCII table (use --html for a full report)",
		Example: `  # Print a terminal-friendly summary
  ktl diag report -n prod-payments

  # Generate the HTML report and open it
  ktl diag report -n prod-payments --html --output dist/prod-report.html && open dist/prod-report.html`,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			if liveMode {
				if renderHTML {
					return fmt.Errorf("--html cannot be combined with --live")
				}
				if strings.TrimSpace(opts.OutputPath) != "" {
					return fmt.Errorf("--output cannot be used with --live")
				}
				if scoreThreshold > 0 {
					return fmt.Errorf("--threshold is not supported with --live (per-request notifications only)")
				}
				return serveLiveReports(ctx, cmd, kubeClient, opts, listenAddr)
			}
			spinnerStop := ui.StartSpinner(cmd.ErrOrStderr(), "Collecting report data")
			stopped := false
			stop := func(ok bool) {
				if stopped {
					return
				}
				spinnerStop(ok)
				stopped = true
			}
			defer stop(false)
			data, namespaces, err := report.Collect(ctx, kubeClient, opts)
			if err != nil {
				return err
			}
			events, err := report.CollectRecentEvents(ctx, kubeClient, namespaces, 50)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warn: unable to load recent events: %v\n", err)
			} else {
				for i := range events {
					if events[i].Timestamp.IsZero() {
						continue
					}
					events[i].Age = report.HumanDuration(data.GeneratedAt.Sub(events[i].Timestamp))
				}
				data.RecentEvents = events
			}
			stop(true)
			if err := report.AppendScorecardHistory(data.Scorecard); err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "warn: unable to persist score history: %v\n", err)
			}
			if scoreThreshold > 0 {
				if err := maybeNotifyScorecard(cmd, notifyMode, scoreThreshold, data.Scorecard); err != nil {
					return err
				}
			}
			if !renderHTML && (strings.TrimSpace(compareLeft) != "" || strings.TrimSpace(compareRight) != "") {
				return fmt.Errorf("--compare-left/--compare-right require --html")
			}
			if renderHTML && (strings.TrimSpace(compareLeft) != "" || strings.TrimSpace(compareRight) != "") {
				if strings.TrimSpace(compareLeft) == "" || strings.TrimSpace(compareRight) == "" {
					return fmt.Errorf("both --compare-left and --compare-right must be provided")
				}
				leftSource, err := parseArchiveSource(compareLeft)
				if err != nil {
					return fmt.Errorf("parse --compare-left: %w", err)
				}
				rightSource, err := parseArchiveSource(compareRight)
				if err != nil {
					return fmt.Errorf("parse --compare-right: %w", err)
				}
				archiveDiff, err := report.BuildArchiveDiff(leftSource, rightSource)
				if err != nil {
					return err
				}
				data.ArchiveDiff = archiveDiff
			}
			if renderHTML {
				html, err := report.RenderHTML(data)
				if err != nil {
					return err
				}
				path := opts.ResolveOutputPath(time.Now())
				if !filepath.IsAbs(path) {
					if abs, err := filepath.Abs(path); err == nil {
						path = abs
					}
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return fmt.Errorf("ensure report directory: %w", err)
				}
				if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
					return fmt.Errorf("write report: %w", err)
				}
				cmd.Printf("Report written to %s (namespaces: %s)\n", path, strings.Join(namespaces, ", "))
				return nil
			}
			if strings.TrimSpace(opts.OutputPath) != "" {
				return fmt.Errorf("--output can only be used with --html")
			}
			report.RenderTable(cmd.OutOrStdout(), data)
			cmd.Printf("\nNamespaces: %s\n", strings.Join(namespaces, ", "))
			return nil
		},
	}
	cmd.Flags().StringSliceVarP(&opts.Namespaces, "namespace", "n", nil, "Namespaces to include (defaults to the current context namespace)")
	cmd.Flags().BoolVarP(&opts.AllNamespaces, "all-namespaces", "A", false, "Include every namespace")
	cmd.Flags().StringVar(&opts.OutputPath, "output", "", "Path for the generated HTML (defaults to ./ktl-report-<timestamp>.html)")
	cmd.Flags().BoolVar(&renderHTML, "html", false, "Render the polished HTML report (default prints an ASCII table)")
	cmd.Flags().BoolVar(&liveMode, "live", false, "Serve the HTML report via a continuously updating HTTP endpoint")
	cmd.Flags().StringVar(&listenAddr, "listen", ":8080", "HTTP listen address to use with --live")
	cmd.Flags().Float64Var(&scoreThreshold, "threshold", 0, "Emit notifications when any score falls below this percentage (0 disables)")
	cmd.Flags().StringVar(&notifyMode, "notify", "json", "Notification sink used when --threshold triggers (json|stdout|none)")
	cmd.Flags().StringVar(&compareLeft, "compare-left", "", "Path to the baseline .k8s archive (append @snapshot to select a specific layer)")
	cmd.Flags().StringVar(&compareRight, "compare-right", "", "Path to the target .k8s archive (append @snapshot to select a specific layer)")
	registerNamespaceCompletion(cmd, "namespace", kubeconfig, kubeContext)
	decorateCommandHelp(cmd, "Report Flags")
	cmd.AddCommand(newReportTrendCommand())
	return cmd
}

func maybeNotifyScorecard(cmd *cobra.Command, mode string, threshold float64, card report.Scorecard) error {
	failures := card.Breaches(threshold)
	if len(failures) == 0 {
		return nil
	}
	switch strings.ToLower(mode) {
	case "", "json":
		payload := card.NotificationPayload(threshold, failures)
		body, err := json.MarshalIndent(payload, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(body))
	case "stdout":
		fmt.Fprintf(cmd.OutOrStdout(), "Scorecard threshold %.1f%% breached (%d checks)\n", threshold, len(failures))
		for _, check := range failures {
			fmt.Fprintf(cmd.OutOrStdout(), "- %s: %.1f%% (%s)\n", check.Name, check.Score, check.Summary)
		}
	case "none":
		// intentional no-op
	default:
		return fmt.Errorf("unsupported notify mode %q", mode)
	}
	return nil
}

func parseArchiveSource(spec string) (report.ArchiveSource, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return report.ArchiveSource{}, fmt.Errorf("archive spec is empty")
	}
	source := report.ArchiveSource{}
	parts := strings.SplitN(spec, "@", 2)
	source.Path = strings.TrimSpace(parts[0])
	if source.Path == "" {
		return report.ArchiveSource{}, fmt.Errorf("archive path is empty")
	}
	if len(parts) == 2 {
		source.Snapshot = strings.TrimSpace(parts[1])
	}
	if _, err := os.Stat(source.Path); err != nil {
		return report.ArchiveSource{}, err
	}
	return source, nil
}
