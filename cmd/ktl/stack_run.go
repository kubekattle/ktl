// File: cmd/ktl/stack_run.go
// Brief: `ktl stack apply/delete` command wiring (runner lives in internal/stack).

package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackApplyCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var failFast bool
	var continueOnError bool
	var yes bool
	var resume bool
	var runID string
	var replan bool
	var allowDrift bool
	var rerunFailed bool
	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply the selected stack releases in DAG order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueOnError && failFast {
				return fmt.Errorf("cannot combine --fail-fast and --continue-on-error")
			}
			var p *stack.Plan
			runRoot := ""
			if resume && !replan {
				var err error
				if strings.TrimSpace(runID) != "" {
					runRoot = filepath.Join(*rootDir, ".ktl", "stack", "runs", strings.TrimSpace(runID))
				} else {
					runRoot, err = stack.LoadMostRecentRun(*rootDir)
					if err != nil {
						return err
					}
				}
				loaded, err := stack.LoadRun(runRoot)
				if err != nil {
					return err
				}
				p = loaded.Plan
				if !allowDrift {
					drift, err := stack.DriftReport(p)
					if err != nil {
						return err
					}
					if len(drift) > 0 {
						return fmt.Errorf("cannot resume: inputs changed (rerun with --allow-drift or --replan)\n%s", strings.Join(drift, "\n"))
					}
				}
				if rerunFailed {
					p = stack.FilterByNodeStatus(p, loaded.StatusByID, []string{"failed"})
				}
			} else {
				u, err := stack.Discover(*rootDir)
				if err != nil {
					return err
				}
				pp, err := stack.Compile(u, stack.CompileOptions{Profile: *profile})
				if err != nil {
					return err
				}
				pp, err = stack.Select(u, pp, splitCSV(*clusters), stack.Selector{
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
				p = pp
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
				RunID:           strings.TrimSpace(runID),
				RunRoot:         runRoot,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume the most recent run (uses its frozen plan.json unless --replan is set)")
	cmd.Flags().StringVar(&runID, "run-id", "", "Resume a specific run ID (directory name under .ktl/stack/runs)")
	cmd.Flags().BoolVar(&replan, "replan", false, "Recompute the plan from current config when resuming")
	cmd.Flags().BoolVar(&allowDrift, "allow-drift", false, "Allow resume even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().BoolVar(&rerunFailed, "rerun-failed", false, "When resuming, schedule only failed nodes")
	return cmd
}

func newStackDeleteCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var failFast bool
	var continueOnError bool
	var yes bool
	var resume bool
	var runID string
	var replan bool
	var allowDrift bool
	var rerunFailed bool
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the selected stack releases in reverse DAG order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueOnError && failFast {
				return fmt.Errorf("cannot combine --fail-fast and --continue-on-error")
			}
			var p *stack.Plan
			runRoot := ""
			if resume && !replan {
				var err error
				if strings.TrimSpace(runID) != "" {
					runRoot = filepath.Join(*rootDir, ".ktl", "stack", "runs", strings.TrimSpace(runID))
				} else {
					runRoot, err = stack.LoadMostRecentRun(*rootDir)
					if err != nil {
						return err
					}
				}
				loaded, err := stack.LoadRun(runRoot)
				if err != nil {
					return err
				}
				p = loaded.Plan
				if !allowDrift {
					drift, err := stack.DriftReport(p)
					if err != nil {
						return err
					}
					if len(drift) > 0 {
						return fmt.Errorf("cannot resume: inputs changed (rerun with --allow-drift or --replan)\n%s", strings.Join(drift, "\n"))
					}
				}
				if rerunFailed {
					p = stack.FilterByNodeStatus(p, loaded.StatusByID, []string{"failed"})
				}
			} else {
				u, err := stack.Discover(*rootDir)
				if err != nil {
					return err
				}
				pp, err := stack.Compile(u, stack.CompileOptions{Profile: *profile})
				if err != nil {
					return err
				}
				pp, err = stack.Select(u, pp, splitCSV(*clusters), stack.Selector{
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
				p = pp
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
				RunID:           strings.TrimSpace(runID),
				RunRoot:         runRoot,
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume the most recent run (uses its frozen plan.json unless --replan is set)")
	cmd.Flags().StringVar(&runID, "run-id", "", "Resume a specific run ID (directory name under .ktl/stack/runs)")
	cmd.Flags().BoolVar(&replan, "replan", false, "Recompute the plan from current config when resuming")
	cmd.Flags().BoolVar(&allowDrift, "allow-drift", false, "Allow resume even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().BoolVar(&rerunFailed, "rerun-failed", false, "When resuming, schedule only failed nodes")
	return cmd
}
