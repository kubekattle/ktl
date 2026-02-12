package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"

	"github.com/kubekattle/ktl/internal/envcatalog"
	"github.com/spf13/cobra"
	"sigs.k8s.io/yaml"
)

type envRow struct {
	Category    string `json:"category" yaml:"category"`
	Variable    string `json:"variable" yaml:"variable"`
	Value       string `json:"value,omitempty" yaml:"value,omitempty"`
	Description string `json:"description" yaml:"description"`
}

func envRows(showAll bool) []envRow {
	rows := envcatalog.Catalog()
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
