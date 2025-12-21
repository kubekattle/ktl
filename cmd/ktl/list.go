// File: cmd/ktl/list.go
// Brief: CLI command wiring and implementation for 'list'/'ls'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/fatih/color"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/chart"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/cli/output"
	"helm.sh/helm/v3/pkg/release"
)

var (
	listStatusDeployed = color.New(color.FgGreen).SprintFunc()
	listStatusFailed   = color.New(color.FgRed).SprintFunc()
	listStatusPending  = color.New(color.FgYellow).SprintFunc()
)

func newListCommand(kubeconfig *string, kubeContext *string) *cobra.Command {
	var namespace string
	var outShort bool
	var noHeaders bool
	var timeFormat string
	var outputFormat string
	var byDate bool
	var reverse bool

	var all bool
	var uninstalled bool
	var superseded bool
	var uninstalling bool
	var deployed bool
	var failed bool
	var pending bool
	var allNamespaces bool
	var limit int
	var offset int
	var filter string
	var selector string

	cmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List Helm releases",
		Args:    cobra.NoArgs,
		Example: `  # List releases in the current namespace
  ktl list

  # List across all namespaces
  ktl list -A

  # Emit structured output
  ktl list --format json`,
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, _ []string) error {
			selectedFormat := strings.ToLower(strings.TrimSpace(outputFormat))
			if selectedFormat == "" {
				selectedFormat = "table"
			}
			switch selectedFormat {
			case "table", "json", "yaml":
			default:
				return fmt.Errorf("unsupported format %q (expected table, json, or yaml)", selectedFormat)
			}

			settings := cli.New()
			if kubeconfig != nil && strings.TrimSpace(*kubeconfig) != "" {
				settings.KubeConfig = *kubeconfig
			}
			if kubeContext != nil && strings.TrimSpace(*kubeContext) != "" {
				settings.KubeContext = *kubeContext
			}

			resolvedNamespace := strings.TrimSpace(namespace)
			if allNamespaces {
				if resolvedNamespace != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), "Warning: --namespace is ignored when --all-namespaces is set.")
					resolvedNamespace = ""
				}
			} else {
				if resolvedNamespace == "" {
					resolvedNamespace = settings.Namespace()
				}
				if resolvedNamespace != "" {
					settings.SetNamespace(resolvedNamespace)
				}
			}

			actionCfg := new(action.Configuration)
			initNamespace := resolvedNamespace
			if allNamespaces {
				initNamespace = ""
			}
			if err := actionCfg.Init(settings.RESTClientGetter(), initNamespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
				return fmt.Errorf("init helm action config: %w", err)
			}

			client := action.NewList(actionCfg)
			client.Short = outShort
			client.NoHeaders = noHeaders
			client.TimeFormat = timeFormat
			client.ByDate = byDate
			client.SortReverse = reverse
			client.All = all
			client.Uninstalled = uninstalled
			client.Superseded = superseded
			client.Uninstalling = uninstalling
			client.Deployed = deployed
			client.Failed = failed
			client.Pending = pending
			client.AllNamespaces = allNamespaces
			client.Limit = limit
			client.Offset = offset
			client.Filter = filter
			client.Selector = selector
			client.SetStateMask()

			results, err := runWithCancel(cmd.Context(), func() ([]*release.Release, error) {
				return client.Run()
			})
			if err != nil {
				return err
			}

			if client.Short {
				names := releaseNames(results)
				switch selectedFormat {
				case "json":
					return output.EncodeJSON(cmd.OutOrStdout(), names)
				case "yaml":
					return output.EncodeYAML(cmd.OutOrStdout(), names)
				default:
					for _, name := range names {
						fmt.Fprintln(cmd.OutOrStdout(), name)
					}
					return nil
				}
			}

			switch selectedFormat {
			case "json":
				return output.EncodeJSON(cmd.OutOrStdout(), releaseListElements(results, client.TimeFormat))
			case "yaml":
				return output.EncodeYAML(cmd.OutOrStdout(), releaseListElements(results, client.TimeFormat))
			default:
				colorize := isTerminalWriter(cmd.OutOrStdout()) && !color.NoColor
				return writeReleaseListTable(cmd.OutOrStdout(), results, client.TimeFormat, client.NoHeaders, colorize)
			}
		},
	}

	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace scope for listing releases (defaults to active context)")
	cmd.Flags().BoolVarP(&allNamespaces, "all-namespaces", "A", false, "List releases across all namespaces")
	cmd.Flags().BoolVarP(&outShort, "short", "q", false, "Output short (quiet) listing format")
	cmd.Flags().BoolVar(&noHeaders, "no-headers", false, "Don't print headers when using the default output format")
	cmd.Flags().StringVar(&outputFormat, "format", "table", "Output format: table, json, or yaml")
	cmd.Flags().StringVar(&timeFormat, "time-format", "", `Format time using Go time layout. Example: --time-format "2006-01-02 15:04:05Z0700"`)
	cmd.Flags().BoolVarP(&byDate, "date", "d", false, "Sort by release date")
	cmd.Flags().BoolVarP(&reverse, "reverse", "r", false, "Reverse the sort order")
	cmd.Flags().BoolVarP(&all, "all", "a", false, "Show all releases without any filter applied")
	cmd.Flags().BoolVar(&uninstalled, "uninstalled", false, "Show uninstalled releases (if uninstall --keep-history was used)")
	cmd.Flags().BoolVar(&superseded, "superseded", false, "Show superseded releases")
	cmd.Flags().BoolVar(&uninstalling, "uninstalling", false, "Show releases that are currently being uninstalled")
	cmd.Flags().BoolVar(&deployed, "deployed", false, "Show deployed releases. If no other is specified, this will be automatically enabled")
	cmd.Flags().BoolVar(&failed, "failed", false, "Show failed releases")
	cmd.Flags().BoolVar(&pending, "pending", false, "Show pending releases")
	cmd.Flags().IntVarP(&limit, "max", "m", 256, "Maximum number of releases to fetch")
	cmd.Flags().IntVar(&offset, "offset", 0, "Next release index in the list, used to offset from start")
	cmd.Flags().StringVarP(&filter, "filter", "f", "", "A regular expression (Perl compatible) to filter releases by name")
	cmd.Flags().StringVarP(&selector, "selector", "l", "", "Label selector to filter releases by label query (works only for secret/configmap backends)")

	decorateCommandHelp(cmd, "List Flags")
	return cmd
}

func runWithCancel[T any](ctx context.Context, fn func() (T, error)) (T, error) {
	var zero T
	if ctx == nil {
		return fn()
	}
	done := make(chan struct{})
	var out T
	var err error
	go func() {
		defer close(done)
		out, err = fn()
	}()
	select {
	case <-ctx.Done():
		return zero, ctx.Err()
	case <-done:
		return out, err
	}
}

type releaseListElement struct {
	Name       string `json:"name" yaml:"name"`
	Namespace  string `json:"namespace" yaml:"namespace"`
	Revision   string `json:"revision" yaml:"revision"`
	Updated    string `json:"updated" yaml:"updated"`
	Status     string `json:"status" yaml:"status"`
	Chart      string `json:"chart" yaml:"chart"`
	AppVersion string `json:"app_version" yaml:"app_version"`
}

func releaseNames(releases []*release.Release) []string {
	names := make([]string, 0, len(releases))
	for _, rel := range releases {
		if rel != nil && strings.TrimSpace(rel.Name) != "" {
			names = append(names, rel.Name)
		}
	}
	return names
}

func releaseListElements(releases []*release.Release, timeFormat string) []releaseListElement {
	elements := make([]releaseListElement, 0, len(releases))
	for _, rel := range releases {
		if rel == nil {
			continue
		}
		updated, status := releaseTimingAndStatus(rel, timeFormat)
		elements = append(elements, releaseListElement{
			Name:       rel.Name,
			Namespace:  rel.Namespace,
			Revision:   strconv.Itoa(rel.Version),
			Updated:    updated,
			Status:     status,
			Chart:      formatChartName(rel.Chart),
			AppVersion: formatAppVersion(rel.Chart),
		})
	}
	return elements
}

func writeReleaseListTable(out io.Writer, releases []*release.Release, timeFormat string, noHeaders bool, colorize bool) error {
	headers := []string{"NAME", "NAMESPACE", "REVISION", "UPDATED", "STATUS", "CHART", "APP VERSION"}
	if noHeaders {
		headers = nil
	}

	rows := releaseListElements(releases, timeFormat)

	widths := make([]int, len(headers))
	for i, header := range headers {
		widths[i] = utf8.RuneCountInString(header)
	}
	if len(headers) == 0 {
		widths = make([]int, 7)
	}
	for _, row := range rows {
		cols := []string{row.Name, row.Namespace, row.Revision, row.Updated, row.Status, row.Chart, row.AppVersion}
		for i, col := range cols {
			if w := utf8.RuneCountInString(col); w > widths[i] {
				widths[i] = w
			}
		}
	}

	renderRow := func(cols []string, plainCols []string) {
		for i, col := range cols {
			plain := col
			if i < len(plainCols) {
				plain = plainCols[i]
			}
			io.WriteString(out, col)
			if i == len(cols)-1 {
				io.WriteString(out, "\n")
				continue
			}
			padding := widths[i] - utf8.RuneCountInString(plain)
			if padding < 0 {
				padding = 0
			}
			io.WriteString(out, strings.Repeat(" ", padding+2))
		}
	}

	if len(headers) > 0 {
		renderRow(headers, headers)
	}
	for _, row := range rows {
		status := row.Status
		if colorize {
			status = colorizeReleaseStatus(status)
		}
		renderRow(
			[]string{row.Name, row.Namespace, row.Revision, row.Updated, status, row.Chart, row.AppVersion},
			[]string{row.Name, row.Namespace, row.Revision, row.Updated, row.Status, row.Chart, row.AppVersion},
		)
	}
	return nil
}

func releaseTimingAndStatus(rel *release.Release, timeFormat string) (string, string) {
	updated := "-"
	status := "-"
	if rel == nil || rel.Info == nil {
		return updated, status
	}
	status = rel.Info.Status.String()
	if rel.Info.LastDeployed.IsZero() {
		return updated, status
	}
	if strings.TrimSpace(timeFormat) != "" {
		return rel.Info.LastDeployed.Format(timeFormat), status
	}
	return rel.Info.LastDeployed.String(), status
}

func colorizeReleaseStatus(status string) string {
	switch status {
	case release.StatusDeployed.String():
		return listStatusDeployed(status)
	case release.StatusFailed.String():
		return listStatusFailed(status)
	case release.StatusPendingInstall.String(), release.StatusPendingUpgrade.String(), release.StatusPendingRollback.String():
		return listStatusPending(status)
	default:
		return status
	}
}

func formatChartName(chart *chart.Chart) string {
	if chart == nil || chart.Metadata == nil {
		return "-"
	}
	name := strings.TrimSpace(chart.Metadata.Name)
	version := strings.TrimSpace(chart.Metadata.Version)
	if name == "" && version == "" {
		return "-"
	}
	if name == "" {
		return version
	}
	if version == "" {
		return name
	}
	return fmt.Sprintf("%s-%s", name, version)
}

func formatAppVersion(chart *chart.Chart) string {
	if chart == nil || chart.Metadata == nil {
		return "-"
	}
	appVersion := strings.TrimSpace(chart.Metadata.AppVersion)
	if appVersion == "" {
		return "-"
	}
	return appVersion
}
