// File: cmd/ktl/stack.go
// Brief: CLI wiring for `ktl stack` (multi-release orchestration).

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var rootDir string
	var profile string
	var clusters []string
	var tags []string
	var fromPaths []string
	var releases []string
	var gitRange string
	var gitIncludeDeps bool
	var gitIncludeDependents bool
	var includeDeps bool
	var includeDependents bool
	var output string
	var planOnly bool

	cmd := &cobra.Command{
		Use:   "stack",
		Short: "Compile and orchestrate many Helm releases as a dependency graph",
		Long:  "ktl stack discovers stack.yaml/release.yaml, compiles them with inheritance into a DAG, and runs ktl apply/delete per release.",
	}
	cmd.PersistentFlags().StringVar(&rootDir, "root", ".", "Stack root directory")
	cmd.PersistentFlags().StringVar(&profile, "profile", "", "Profile overlay name (defaults to stack.yaml.defaultProfile when present)")
	cmd.PersistentFlags().StringSliceVar(&clusters, "cluster", nil, "Filter the universe by cluster name (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&tags, "tag", nil, "Select releases by tag (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&fromPaths, "from-path", nil, "Select releases under a directory subtree (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&releases, "release", nil, "Select releases by name (repeatable or comma-separated)")
	cmd.PersistentFlags().StringVar(&gitRange, "git-range", "", "Select releases affected by a git diff range (example: origin/main...HEAD)")
	cmd.PersistentFlags().BoolVar(&gitIncludeDeps, "git-include-deps", false, "When using --git-range, expand selection to include dependencies")
	cmd.PersistentFlags().BoolVar(&gitIncludeDependents, "git-include-dependents", false, "When using --git-range, expand selection to include dependents")
	cmd.PersistentFlags().BoolVar(&includeDeps, "include-deps", false, "Expand selection to include dependencies")
	cmd.PersistentFlags().BoolVar(&includeDependents, "include-dependents", false, "Expand selection to include dependents")
	cmd.PersistentFlags().StringVar(&output, "output", "table", "Output format: table|json")
	cmd.PersistentFlags().BoolVar(&planOnly, "plan-only", false, "Compile and print the plan, but do not execute")

	cmd.AddCommand(newStackPlanCommand(&rootDir, &profile, &clusters, &output, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents))
	cmd.AddCommand(newStackGraphCommand(&rootDir, &profile, &clusters, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents))
	cmd.AddCommand(newStackExplainCommand(&rootDir, &profile, &clusters, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents))
	cmd.AddCommand(newStackApplyCommand(&rootDir, &profile, &clusters, &output, &planOnly, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents, kubeconfig, kubeContext, logLevel, remoteAgent))
	cmd.AddCommand(newStackDeleteCommand(&rootDir, &profile, &clusters, &output, &planOnly, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents, kubeconfig, kubeContext, logLevel, remoteAgent))
	return cmd
}

func newStackPlanCommand(rootDir, profile *string, clusters *[]string, output *string, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool) *cobra.Command {
	return &cobra.Command{
		Use:   "plan",
		Short: "Compile stack configs into an execution plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := stack.Discover(*rootDir)
			if err != nil {
				return err
			}
			p, err := stack.Compile(u, stack.CompileOptions{Profile: *profile})
			if err != nil {
				return err
			}
			selected, err := stack.Select(u, p, splitCSV(*clusters), stack.Selector{
				Tags:                 *tags,
				FromPaths:            *fromPaths,
				Releases:             *releases,
				GitRange:             *gitRange,
				GitIncludeDeps:       *gitIncludeDeps,
				GitIncludeDependents: *gitIncludeDependents,
				IncludeDeps:          *includeDeps,
				IncludeDependents:    *includeDependents,
			})
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(*output)) {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(selected)
			case "", "table":
				return stack.PrintPlanTable(cmd.OutOrStdout(), selected)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json)", *output)
			}
		},
	}
}

func splitCSV(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
