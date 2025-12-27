// File: cmd/ktl/stack_rerun_failed.go
// Brief: `ktl stack rerun-failed` convenience command.

package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackRerunFailedCommand(rootDir, profile *string, clusters *[]string, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, allowMissingDeps *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var yes bool
	var runID string
	var allowDrift bool
	var retry int
	var concurrency int
	var progressiveConcurrency bool
	var lock bool
	var takeover bool
	var lockTTL time.Duration
	var lockOwner string
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
				Lock:                   lock,
				LockOwner:              lockOwner,
				LockTTL:                lockTTL,
				TakeoverLock:           takeover,
				Kubeconfig:             kubeconfig,
				KubeContext:            kubeContext,
				LogLevel:               logLevel,
				RemoteAgentAddr:        remoteAgent,
				// Always create a new runId; --run-id selects which previous run to resume from.
				RunID:           "",
				RunRoot:         "",
				FailMode:        chooseFailMode(true),
				MaxAttempts:     maxAttemptsFromRetry(retry),
				InitialAttempts: loaded.AttemptByID,
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
					AllowMissingDeps:     *allowMissingDeps,
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
	cmd.Flags().BoolVar(&lock, "lock", true, "Acquire a stack state lock for this run")
	cmd.Flags().BoolVar(&takeover, "takeover", false, "Take over the stack state lock if held (unsafe)")
	cmd.Flags().DurationVar(&lockTTL, "lock-ttl", 30*time.Minute, "How long before the lock is considered stale")
	cmd.Flags().StringVar(&lockOwner, "lock-owner", "", "Lock owner string (defaults to user@host:pid)")
	return cmd
}
