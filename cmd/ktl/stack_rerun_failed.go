// File: cmd/ktl/stack_rerun_failed.go
// Brief: `ktl stack rerun-failed` convenience command.

package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackRerunFailedCommand(rootDir, profile *string, clusters *[]string, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var yes bool
	var runID string
	var allowDrift bool
	var retry int
	var concurrency int
	var progressiveConcurrency bool
	cmd := &cobra.Command{
		Use:   "rerun-failed",
		Short: "Resume the most recent run and schedule only failed nodes",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			runRoot := ""
			if strings.TrimSpace(runID) != "" {
				runRoot = filepath.Join(*rootDir, ".ktl", "stack", "runs", strings.TrimSpace(runID))
			} else {
				var err error
				runRoot, err = stack.LoadMostRecentRun(*rootDir)
				if err != nil {
					return err
				}
			}
			loaded, err := stack.LoadRun(runRoot)
			if err != nil {
				return err
			}
			p := loaded.Plan
			if !allowDrift {
				drift, err := stack.DriftReport(p)
				if err != nil {
					return err
				}
				if len(drift) > 0 {
					return fmt.Errorf("cannot resume: inputs changed (rerun with --allow-drift)\n%s", strings.Join(drift, "\n"))
				}
			}
			p = stack.FilterByNodeStatus(p, loaded.StatusByID, []string{"failed"})
			return stack.Run(cmd.Context(), stack.RunOptions{
				Command:                "apply",
				Plan:                   p,
				Concurrency:            concurrency,
				ProgressiveConcurrency: progressiveConcurrency,
				FailFast:               true,
				AutoApprove:            yes,
				Kubeconfig:             kubeconfig,
				KubeContext:            kubeContext,
				LogLevel:               logLevel,
				RemoteAgentAddr:        remoteAgent,
				RunID:                  strings.TrimSpace(runID),
				RunRoot:                runRoot,
				FailMode:               chooseFailMode(true),
				MaxAttempts:            maxAttemptsFromRetry(retry),
				InitialAttempts:        loaded.AttemptByID,
				Selector: stack.RunSelector{
					Clusters:             splitCSV(*clusters),
					Tags:                 splitCSV(*tags),
					FromPaths:            splitCSV(*fromPaths),
					Releases:             splitCSV(*releases),
					GitRange:             strings.TrimSpace(*gitRange),
					GitIncludeDeps:       *gitIncludeDeps,
					GitIncludeDependents: *gitIncludeDependents,
					IncludeDeps:          *includeDeps,
					IncludeDependents:    *includeDependents,
				},
			}, cmd.OutOrStdout(), cmd.ErrOrStderr())
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	cmd.Flags().StringVar(&runID, "run-id", "", "Resume a specific run ID (directory name under .ktl/stack/runs)")
	cmd.Flags().BoolVar(&allowDrift, "allow-drift", false, "Allow rerun even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().IntVar(&retry, "retry", 1, "Maximum attempts per release (includes the initial attempt)")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&progressiveConcurrency, "progressive-concurrency", false, "Start at 1 worker, then ramp up/down based on successes/failures")
	return cmd
}
