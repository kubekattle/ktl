// File: cmd/ktl/stack_graph.go
// Brief: `ktl stack graph` / `ktl stack explain` UX.

package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackGraphCommand(rootDir, profile *string, clusters *[]string, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "graph",
		Short: "Render the selected stack DAG (dot or mermaid)",
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
			p, err = stack.Select(u, p, splitCSV(*clusters), stack.Selector{
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
			switch strings.ToLower(strings.TrimSpace(format)) {
			case "", "dot":
				return stack.PrintGraphDOT(cmd.OutOrStdout(), p)
			case "mermaid":
				return stack.PrintGraphMermaid(cmd.OutOrStdout(), p)
			default:
				return fmt.Errorf("unknown --format %q (expected dot|mermaid)", format)
			}
		},
	}
	cmd.Flags().StringVar(&format, "format", "dot", "Graph format: dot|mermaid")
	return cmd
}

func newStackExplainCommand(rootDir, profile *string, clusters *[]string, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool) *cobra.Command {
	var why bool
	cmd := &cobra.Command{
		Use:   "explain <id|name>",
		Short: "Explain why a release was selected",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			u, err := stack.Discover(*rootDir)
			if err != nil {
				return err
			}
			p, err := stack.Compile(u, stack.CompileOptions{Profile: *profile})
			if err != nil {
				return err
			}
			p, err = stack.Select(u, p, splitCSV(*clusters), stack.Selector{
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
			target := args[0]
			var node *stack.ResolvedRelease
			if strings.Count(target, "/") >= 2 {
				node = p.ByID[target]
				if node == nil {
					return fmt.Errorf("unknown id %q", target)
				}
			} else {
				var matches []*stack.ResolvedRelease
				for _, n := range p.Nodes {
					if n.Name == target {
						matches = append(matches, n)
					}
				}
				if len(matches) == 0 {
					return fmt.Errorf("unknown release name %q", target)
				}
				if len(matches) > 1 {
					return fmt.Errorf("ambiguous name %q (use full id)", target)
				}
				node = matches[0]
			}
			if why {
				for _, r := range node.SelectedBy {
					fmt.Fprintln(cmd.OutOrStdout(), r)
				}
				return nil
			}
			fmt.Fprintf(cmd.OutOrStdout(), "ID: %s\n", node.ID)
			fmt.Fprintf(cmd.OutOrStdout(), "Chart: %s\n", node.Chart)
			fmt.Fprintf(cmd.OutOrStdout(), "Values: %v\n", node.Values)
			fmt.Fprintf(cmd.OutOrStdout(), "Tags: %v\n", node.Tags)
			fmt.Fprintf(cmd.OutOrStdout(), "Needs: %v\n", node.Needs)
			fmt.Fprintf(cmd.OutOrStdout(), "SelectedBy: %v\n", node.SelectedBy)
			return nil
		},
	}
	cmd.Flags().BoolVar(&why, "why", false, "Print only the selection reasons")
	return cmd
}
