// File: cmd/ktl/stack_run.go
// Brief: `ktl stack apply/delete` command wiring (runner lives in internal/stack).

package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackApplyCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var failFast bool
	var continueOnError bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the selected stack releases in DAG order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueOnError && failFast {
				return fmt.Errorf("cannot combine --fail-fast and --continue-on-error")
			}
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
			if *planOnly {
				switch strings.ToLower(strings.TrimSpace(*output)) {
				case "json":
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				default:
					return stack.PrintPlanTable(cmd.OutOrStdout(), p)
				}
			}
			return stack.Run(cmd.Context(), stack.RunOptions{
				Command:         "apply",
				Plan:            p,
				Concurrency:     concurrency,
				FailFast:        failFast || !continueOnError,
				AutoApprove:     yes,
				Kubeconfig:      kubeconfig,
				KubeContext:     kubeContext,
				LogLevel:        logLevel,
				RemoteAgentAddr: remoteAgent,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	return cmd
}

func newStackDeleteCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var failFast bool
	var continueOnError bool
	var yes bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the selected stack releases in reverse DAG order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueOnError && failFast {
				return fmt.Errorf("cannot combine --fail-fast and --continue-on-error")
			}
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
			if *planOnly {
				switch strings.ToLower(strings.TrimSpace(*output)) {
				case "json":
					enc := json.NewEncoder(cmd.OutOrStdout())
					enc.SetIndent("", "  ")
					return enc.Encode(p)
				default:
					return stack.PrintPlanTable(cmd.OutOrStdout(), p)
				}
			}
			return stack.Run(cmd.Context(), stack.RunOptions{
				Command:         "delete",
				Plan:            p,
				Concurrency:     concurrency,
				FailFast:        failFast || !continueOnError,
				AutoApprove:     yes,
				Kubeconfig:      kubeconfig,
				KubeContext:     kubeContext,
				LogLevel:        logLevel,
				RemoteAgentAddr: remoteAgent,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	return cmd
}
