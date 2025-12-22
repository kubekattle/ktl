package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type envVarInfo struct {
	Category    string
	Name        string
	Description string
	Dynamic     bool
	Internal    bool
}

func envVarCatalog() []envVarInfo {
	return []envVarInfo{
		{
			Category:    "Config",
			Name:        "KTL_CONFIG",
			Description: "Path to the ktl config file.",
		},
		{
			Category:    "Config",
			Name:        "KTL_<FLAG>",
			Dynamic:     true,
			Description: "Set any ktl CLI flag via environment (hyphens become underscores). Example: KTL_NAMESPACE=default.",
		},
		{
			Category:    "Output",
			Name:        "NO_COLOR",
			Description: "Disable ANSI color output (any non-empty value).",
		},
		{
			Category:    "Logging",
			Name:        "KTL_KUBE_LOG_LEVEL",
			Description: "Kubernetes client-go verbosity (klog -v). At >=6 enables HTTP request/response tracing.",
		},
		{
			Category:    "Profiling",
			Name:        "KTL_PROFILE",
			Description: "Enable profiling modes for ktl itself (e.g. startup writes CPU/heap profiles to the working directory).",
		},
		{
			Category:    "Features",
			Name:        "KTL_FEATURE_<FLAG>",
			Dynamic:     true,
			Description: "Enable an experimental feature flag (repeatable via env). Example: KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1.",
		},
		{
			Category:    "Build",
			Name:        "KTL_BUILDKIT_HOST",
			Description: "Override the BuildKit address used by `ktl build`.",
		},
		{
			Category:    "Build",
			Name:        "KTL_BUILDKIT_CACHE",
			Description: "Configure BuildKit cache import/export for `ktl build`.",
		},
		{
			Category:    "Build",
			Name:        "KTL_DOCKER_CONTEXT",
			Description: "Docker context to use for Buildx fallback (when provisioning a Docker-backed BuildKit builder).",
		},
		{
			Category:    "Build",
			Name:        "KTL_DOCKER_CONFIG",
			Description: "Override Docker config directory for Buildx fallback (equivalent to DOCKER_CONFIG).",
		},
		{
			Category:    "Registry",
			Name:        "KTL_AUTHFILE",
			Description: "Path to a container registry auth file for `ktl build` (containers-auth.json).",
		},
		{
			Category:    "Registry",
			Name:        "KTL_REGISTRY_AUTH_FILE",
			Description: "Alternate registry auth file path for `ktl build`.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_DISABLE",
			Description: "Disable sandbox execution where supported (set to 1).",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CONFIG",
			Description: "Path to the sandbox policy configuration file.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_ACTIVE",
			Internal:    true,
			Description: "Internal marker set inside the sandbox runtime.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_LOG_PATH",
			Internal:    true,
			Description: "Internal path used by the sandbox to mirror diagnostics/logs.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CONTEXT",
			Internal:    true,
			Description: "Internal sandbox context marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CACHE",
			Internal:    true,
			Description: "Internal sandbox cache marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_BUILDER",
			Internal:    true,
			Description: "Internal sandbox builder marker.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_DISABLE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_DISABLE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_ACTIVE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_ACTIVE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_LOG_PATH",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_LOG_PATH.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_CONTEXT",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_CONTEXT.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_CACHE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_CACHE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_BUILDER",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_BUILDER.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_QUEUE_SIZE",
			Description: "Capture recorder in-memory queue size.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_BATCH_SIZE",
			Description: "Capture recorder flush batch size.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_FLUSH_MS",
			Description: "Capture recorder flush interval in milliseconds.",
		},
	}
}

type envRow struct {
	Category    string `json:"category" yaml:"category"`
	Variable    string `json:"variable" yaml:"variable"`
	Value       string `json:"value,omitempty" yaml:"value,omitempty"`
	Description string `json:"description" yaml:"description"`
}

func envRows(showAll bool) []envRow {
	rows := envVarCatalog()
	out := make([]envRow, 0, len(rows))
	for _, row := range rows {
		if row.Internal && !showAll {
			continue
		}
		value := ""
		if !row.Dynamic {
			value = strings.TrimSpace(os.Getenv(row.Name))
		}
		out = append(out, envRow{
			Category:    row.Category,
			Variable:    row.Name,
			Value:       value,
			Description: row.Description,
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Category != out[j].Category {
			return out[i].Category < out[j].Category
		}
		return out[i].Variable < out[j].Variable
	})
	return out
}

func filterEnvRows(rows []envRow, category string, match string, onlySet bool) []envRow {
	category = strings.TrimSpace(category)
	match = strings.ToLower(strings.TrimSpace(match))
	out := rows[:0]
	for _, row := range rows {
		if category != "" && !strings.EqualFold(row.Category, category) {
			continue
		}
		if match != "" {
			haystack := strings.ToLower(row.Variable + "\n" + row.Description + "\n" + row.Category)
			if !strings.Contains(haystack, match) {
				continue
			}
		}
		if onlySet && strings.TrimSpace(row.Value) == "" {
			continue
		}
		out = append(out, row)
	}
	return out
}

func newEnvCommand() *cobra.Command {
	var format string
	var showAll bool
	var onlySet bool
	var category string
	var match string

	cmd := &cobra.Command{
		Use:   "env",
		Short: "Show environment variables used by ktl",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rows := filterEnvRows(envRows(showAll), category, match, onlySet)

			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "table":
				out := cmd.OutOrStdout()
				tw := tabwriter.NewWriter(out, 0, 4, 2, ' ', 0)
				fmt.Fprintln(tw, "CATEGORY\tVARIABLE\tVALUE\tDESCRIPTION")
				for _, row := range rows {
					fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", row.Category, row.Variable, row.Value, row.Description)
				}
				return tw.Flush()
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			case "yaml", "yml":
				b, err := yaml.Marshal(rows)
				if err != nil {
					return err
				}
				_, err = cmd.OutOrStdout().Write(b)
				return err
			default:
				return fmt.Errorf("unsupported --format %q (expected table, json, or yaml)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table, json, yaml")
	cmd.Flags().BoolVar(&showAll, "all", false, "Include internal/legacy variables")
	cmd.Flags().BoolVar(&onlySet, "set", false, "Show only variables with a non-empty value")
	cmd.Flags().StringVar(&category, "category", "", "Filter to a category (case-insensitive)")
	cmd.Flags().StringVar(&match, "match", "", "Filter by substring match against name/category/description")
	decorateCommandHelp(cmd, "Diagnostics")
	return cmd
}
