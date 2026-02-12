package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kubekattle/ktl/internal/appconfig"
	"github.com/kubekattle/ktl/internal/verify"
	"github.com/spf13/cobra"
)

func newVerifyRulesCommand(rulesPath *string) *cobra.Command {
	var rulesDir string
	cmd := &cobra.Command{
		Use:   "rules",
		Short: "Browse available verify rules",
		Long: strings.TrimSpace(`
List or inspect the rule metadata shipped with ktl verify.

This is a discovery tool: it helps you understand rule meaning and severity,
and it gives you stable rule IDs to use in selectors.
`),
		Args: cobra.NoArgs,
	}

	cmd.PersistentFlags().StringVar(&rulesDir, "rules-dir", "", "Rules directory root (defaults to the builtin rules)")
	cmd.AddCommand(newVerifyRulesListCommand(&rulesDir, rulesPath))
	cmd.AddCommand(newVerifyRulesShowCommand(&rulesDir, rulesPath))
	cmd.AddCommand(newVerifyRulesExplainCommand(&rulesDir, rulesPath))
	return cmd
}

func newVerifyRulesListCommand(rulesDir *string, rulesPath *string) *cobra.Command {
	var format string
	var query string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List rules",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			rs, err := loadRulesForCLI(*rulesDir, rulesPath)
			if err != nil {
				return err
			}
			q := strings.ToLower(strings.TrimSpace(query))
			rules := rs.Rules
			if q != "" {
				filtered := rules[:0]
				for _, r := range rules {
					hay := strings.ToLower(r.ID + " " + r.Title + " " + r.Category)
					if strings.Contains(hay, q) {
						filtered = append(filtered, r)
					}
				}
				rules = filtered
			}
			sort.Slice(rules, func(i, j int) bool { return rules[i].ID < rules[j].ID })

			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "table":
				sevCounts := map[verify.Severity]int{}
				for _, r := range rules {
					sevCounts[r.Severity]++
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Rules: %d\n", len(rules))
				fmt.Fprintf(cmd.OutOrStdout(), "By severity: CRIT %d | HIGH %d | MED %d | LOW %d | INFO %d\n\n",
					sevCounts[verify.SeverityCritical],
					sevCounts[verify.SeverityHigh],
					sevCounts[verify.SeverityMedium],
					sevCounts[verify.SeverityLow],
					sevCounts[verify.SeverityInfo],
				)
				fmt.Fprintln(cmd.OutOrStdout(), "ID\tSEV\tCATEGORY\tTITLE")
				for _, r := range rules {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\t%s\n",
						r.ID,
						strings.ToUpper(string(r.Severity)),
						r.Category,
						r.Title,
					)
				}
				return nil
			case "json":
				raw, err := json.MarshalIndent(rules, "", "  ")
				if err != nil {
					return err
				}
				_, _ = cmd.OutOrStdout().Write(append(raw, '\n'))
				return nil
			default:
				return fmt.Errorf("unknown --format %q (expected table|json)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "table", "Output format: table|json")
	cmd.Flags().StringVarP(&query, "query", "q", "", "Filter rules by substring match over id/title/category")
	return cmd
}

func newVerifyRulesShowCommand(rulesDir *string, rulesPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "show <rule-id>",
		Short: "Show rule details",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expected exactly one rule id")
			}
			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("rule id is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			rs, err := loadRulesForCLI(*rulesDir, rulesPath)
			if err != nil {
				return err
			}
			needle := strings.TrimSpace(args[0])
			for _, r := range rs.Rules {
				if r.ID != needle {
					continue
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "ID: %s\n", r.ID)
				if r.Title != "" {
					fmt.Fprintf(out, "Title: %s\n", r.Title)
				}
				if r.Severity != "" {
					fmt.Fprintf(out, "Severity: %s\n", strings.ToUpper(string(r.Severity)))
				}
				if r.Category != "" {
					fmt.Fprintf(out, "Category: %s\n", r.Category)
				}
				if r.HelpURL != "" {
					fmt.Fprintf(out, "Help: %s\n", r.HelpURL)
				}
				if r.Dir != "" {
					fmt.Fprintf(out, "Dir: %s\n", r.Dir)
				}
				if r.Description != "" {
					fmt.Fprintln(out, "")
					fmt.Fprintln(out, strings.TrimSpace(r.Description))
				}
				return nil
			}
			return fmt.Errorf("unknown rule id %q", needle)
		},
	}
	return cmd
}

func newVerifyRulesExplainCommand(rulesDir *string, rulesPath *string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "explain <rule-id>",
		Short: "Explain a rule and show example fixtures (if present)",
		Args: func(cmd *cobra.Command, args []string) error {
			if len(args) != 1 {
				return fmt.Errorf("expected exactly one rule id")
			}
			if strings.TrimSpace(args[0]) == "" {
				return fmt.Errorf("rule id is required")
			}
			return nil
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			rs, err := loadRulesForCLI(*rulesDir, rulesPath)
			if err != nil {
				return err
			}
			needle := strings.TrimSpace(args[0])
			for _, r := range rs.Rules {
				if r.ID != needle {
					continue
				}
				out := cmd.OutOrStdout()
				fmt.Fprintf(out, "ID: %s\n", r.ID)
				if r.Title != "" {
					fmt.Fprintf(out, "Title: %s\n", r.Title)
				}
				if r.Severity != "" {
					fmt.Fprintf(out, "Severity: %s\n", strings.ToUpper(string(r.Severity)))
				}
				if r.Category != "" {
					fmt.Fprintf(out, "Category: %s\n", r.Category)
				}
				if r.HelpURL != "" {
					fmt.Fprintf(out, "Help: %s\n", r.HelpURL)
				}
				if r.Description != "" {
					fmt.Fprintln(out, "\n"+strings.TrimSpace(r.Description))
				}

				// Show local fixtures if present.
				if strings.TrimSpace(r.Dir) != "" {
					testDir := filepath.Join(r.Dir, "test")
					if ents, err := os.ReadDir(testDir); err == nil {
						var files []string
						for _, e := range ents {
							if e.IsDir() {
								continue
							}
							name := strings.TrimSpace(e.Name())
							if name == "" {
								continue
							}
							files = append(files, filepath.Join(testDir, name))
						}
						sort.Strings(files)
						if len(files) > 0 {
							fmt.Fprintln(out, "\nFixtures:")
							for _, p := range files {
								fmt.Fprintf(out, "- %s\n", p)
							}
						}
					}
				}
				return nil
			}
			return fmt.Errorf("unknown rule id %q", needle)
		},
	}
	return cmd
}

func loadRulesForCLI(rulesDir string, rulesPath *string) (verify.Ruleset, error) {
	base := strings.TrimSpace(rulesDir)
	if base == "" {
		base = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
	}
	paths := []string{base}
	if rulesPath != nil {
		paths = append(paths, splitListLocal(*rulesPath)...)
	}
	if env := strings.TrimSpace(os.Getenv("KTL_VERIFY_RULES_PATH")); env != "" {
		paths = append(paths, splitListLocal(env)...)
	}
	return verify.LoadRuleset(paths...)
}
