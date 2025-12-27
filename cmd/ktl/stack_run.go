// File: cmd/ktl/stack_run.go
// Brief: `ktl stack apply/delete` command wiring (runner lives in internal/stack).

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackApplyCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, allowMissingDeps *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var progressiveConcurrency bool
	var failFast bool
	var continueOnError bool
	var yes bool
	var dryRun bool
	var diff bool
	var resume bool
	var runID string
	var replan bool
	var allowDrift bool
	var rerunFailed bool
	var retry int
	var lock bool
	var takeover bool
	var lockTTL time.Duration
	var lockOwner string
	var sealedDir string
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
			if strings.TrimSpace(sealedDir) != "" {
				if resume || replan {
					return fmt.Errorf("cannot combine --sealed-dir with --resume/--replan")
				}
				dir := strings.TrimSpace(sealedDir)
				rp, err := readRunPlanFile(filepath.Join(dir, "plan.json"))
				if err != nil {
					return err
				}
				wantPlanHash := strings.TrimSpace(rp.PlanHash)
				bundleFile := "inputs.tar.gz"
				attPath := filepath.Join(dir, "attestation.json")
				if _, err := os.Stat(attPath); err == nil {
					att, err := readSealAttestation(attPath)
					if err != nil {
						return err
					}
					if strings.TrimSpace(att.PlanHash) != "" {
						if wantPlanHash != "" && att.PlanHash != wantPlanHash {
							return fmt.Errorf("attestation planHash mismatch (%s != %s)", att.PlanHash, wantPlanHash)
						}
						if wantPlanHash == "" {
							wantPlanHash = att.PlanHash
						}
					}
					if strings.TrimSpace(att.InputsBundle) != "" {
						bundleFile = strings.TrimSpace(att.InputsBundle)
					}
					if strings.TrimSpace(att.InputsBundleSH) != "" {
						got, err := sha256File(filepath.Join(dir, bundleFile))
						if err != nil {
							return err
						}
						if got != strings.TrimSpace(att.InputsBundleSH) {
							return fmt.Errorf("attestation bundle digest mismatch (%s != %s)", got, strings.TrimSpace(att.InputsBundleSH))
						}
					}
				}
				gotPlanHash, err := stack.ComputeRunPlanHash(rp)
				if err != nil {
					return err
				}
				if wantPlanHash != "" && gotPlanHash != wantPlanHash {
					return fmt.Errorf("sealed plan hash mismatch (%s != %s)", gotPlanHash, wantPlanHash)
				}
				pp, err := stack.PlanFromRunPlan(rp)
				if err != nil {
					return err
				}
				tmpDir, err := os.MkdirTemp("", "ktl-stack-inputs-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)
				manifest, err := stack.ExtractInputBundle(cmd.Context(), filepath.Join(dir, bundleFile), tmpDir)
				if err != nil {
					return err
				}
				if strings.TrimSpace(manifest.PlanHash) != "" && wantPlanHash != "" && manifest.PlanHash != wantPlanHash {
					return fmt.Errorf("bundle planHash mismatch (%s != %s)", manifest.PlanHash, wantPlanHash)
				}
				if err := stack.ApplyInputBundleToPlan(pp, tmpDir, manifest); err != nil {
					return err
				}
				gid := &stack.GitIdentity{Commit: rp.StackGitCommit, Dirty: rp.StackGitDirty}
				for _, n := range pp.Nodes {
					got, _, err := stack.ComputeEffectiveInputHashWithOptions(n, stack.EffectiveInputHashOptions{
						StackRoot:             tmpDir,
						IncludeValuesContents: true,
						StackGitIdentity:      gid,
					})
					if err != nil {
						return err
					}
					want := strings.TrimSpace(n.EffectiveInputHash)
					if want != "" && got != want {
						return fmt.Errorf("%s inputs mismatch (want %s got %s)", n.ID, want, got)
					}
				}
				// Use the current root as the state store location for this run.
				pp.StackRoot = *rootDir
				p = pp
			} else if resume && !replan {
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
				initialAttempts := loaded.AttemptByID
				return stack.Run(cmd.Context(), stack.RunOptions{
					Command:                "apply",
					Plan:                   p,
					Concurrency:            concurrency,
					ProgressiveConcurrency: progressiveConcurrency,
					FailFast:               failFast || !continueOnError,
					AutoApprove:            yes,
					DryRun:                 dryRun,
					Diff:                   diff,
					Lock:                   lock,
					LockOwner:              lockOwner,
					LockTTL:                lockTTL,
					TakeoverLock:           takeover,
					Kubeconfig:             kubeconfig,
					KubeContext:            kubeContext,
					LogLevel:               logLevel,
					RemoteAgentAddr:        remoteAgent,
					// When resuming, always create a new runId; --run-id selects which previous
					// run to resume from, not the id of the new run.
					RunID:           "",
					RunRoot:         "",
					FailMode:        chooseFailMode(failFast || !continueOnError),
					MaxAttempts:     maxAttemptsFromRetry(retry),
					InitialAttempts: initialAttempts,
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
					AllowMissingDeps:     *allowMissingDeps,
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
				Command:                "apply",
				Plan:                   p,
				Concurrency:            concurrency,
				ProgressiveConcurrency: progressiveConcurrency,
				FailFast:               failFast || !continueOnError,
				AutoApprove:            yes,
				DryRun:                 dryRun,
				Diff:                   diff,
				Lock:                   lock,
				LockOwner:              lockOwner,
				LockTTL:                lockTTL,
				TakeoverLock:           takeover,
				Kubeconfig:             kubeconfig,
				KubeContext:            kubeContext,
				LogLevel:               logLevel,
				RemoteAgentAddr:        remoteAgent,
				RunID:                  strings.TrimSpace(runID),
				RunRoot:                runRoot,
				FailMode:               chooseFailMode(failFast || !continueOnError),
				MaxAttempts:            maxAttemptsFromRetry(retry),
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
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&progressiveConcurrency, "progressive-concurrency", false, "Start at 1 worker, then ramp up/down based on successes/failures")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview changes without applying them")
	cmd.Flags().BoolVar(&diff, "diff", false, "Print a manifest diff during apply")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume the most recent run (uses its frozen plan.json unless --replan is set)")
	cmd.Flags().StringVar(&runID, "run-id", "", "Resume a specific run ID (directory name under .ktl/stack/runs)")
	cmd.Flags().BoolVar(&replan, "replan", false, "Recompute the plan from current config when resuming")
	cmd.Flags().BoolVar(&allowDrift, "allow-drift", false, "Allow resume even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().BoolVar(&rerunFailed, "rerun-failed", false, "When resuming, schedule only failed nodes")
	cmd.Flags().IntVar(&retry, "retry", 1, "Maximum attempts per release (includes the initial attempt)")
	cmd.Flags().BoolVar(&lock, "lock", true, "Acquire a stack state lock for this run")
	cmd.Flags().BoolVar(&takeover, "takeover", false, "Take over the stack state lock if held (unsafe)")
	cmd.Flags().DurationVar(&lockTTL, "lock-ttl", 30*time.Minute, "How long before the lock is considered stale")
	cmd.Flags().StringVar(&lockOwner, "lock-owner", "", "Lock owner string (defaults to user@host:pid)")
	cmd.Flags().StringVar(&sealedDir, "sealed-dir", "", "Run from a sealed plan/bundle directory (expects plan.json + inputs.tar.gz)")
	return cmd
}

func newStackDeleteCommand(rootDir, profile *string, clusters *[]string, output *string, planOnly *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, allowMissingDeps *bool, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var concurrency int
	var progressiveConcurrency bool
	var failFast bool
	var continueOnError bool
	var yes bool
	var dryRun bool
	var resume bool
	var runID string
	var replan bool
	var allowDrift bool
	var rerunFailed bool
	var largeDeletePromptThreshold int
	var retry int
	var lock bool
	var takeover bool
	var lockTTL time.Duration
	var lockOwner string
	var sealedDir string
	cmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete the selected stack releases in reverse DAG order",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if continueOnError && failFast {
				return fmt.Errorf("cannot combine --fail-fast and --continue-on-error")
			}
			if dryRun {
				return fmt.Errorf("ktl stack delete: --dry-run is not supported")
			}
			var p *stack.Plan
			runRoot := ""
			if strings.TrimSpace(sealedDir) != "" {
				if resume || replan {
					return fmt.Errorf("cannot combine --sealed-dir with --resume/--replan")
				}
				dir := strings.TrimSpace(sealedDir)
				rp, err := readRunPlanFile(filepath.Join(dir, "plan.json"))
				if err != nil {
					return err
				}
				wantPlanHash := strings.TrimSpace(rp.PlanHash)
				bundleFile := "inputs.tar.gz"
				attPath := filepath.Join(dir, "attestation.json")
				if _, err := os.Stat(attPath); err == nil {
					att, err := readSealAttestation(attPath)
					if err != nil {
						return err
					}
					if strings.TrimSpace(att.PlanHash) != "" {
						if wantPlanHash != "" && att.PlanHash != wantPlanHash {
							return fmt.Errorf("attestation planHash mismatch (%s != %s)", att.PlanHash, wantPlanHash)
						}
						if wantPlanHash == "" {
							wantPlanHash = att.PlanHash
						}
					}
					if strings.TrimSpace(att.InputsBundle) != "" {
						bundleFile = strings.TrimSpace(att.InputsBundle)
					}
					if strings.TrimSpace(att.InputsBundleSH) != "" {
						got, err := sha256File(filepath.Join(dir, bundleFile))
						if err != nil {
							return err
						}
						if got != strings.TrimSpace(att.InputsBundleSH) {
							return fmt.Errorf("attestation bundle digest mismatch (%s != %s)", got, strings.TrimSpace(att.InputsBundleSH))
						}
					}
				}
				gotPlanHash, err := stack.ComputeRunPlanHash(rp)
				if err != nil {
					return err
				}
				if wantPlanHash != "" && gotPlanHash != wantPlanHash {
					return fmt.Errorf("sealed plan hash mismatch (%s != %s)", gotPlanHash, wantPlanHash)
				}
				pp, err := stack.PlanFromRunPlan(rp)
				if err != nil {
					return err
				}
				tmpDir, err := os.MkdirTemp("", "ktl-stack-inputs-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)
				manifest, err := stack.ExtractInputBundle(cmd.Context(), filepath.Join(dir, bundleFile), tmpDir)
				if err != nil {
					return err
				}
				if strings.TrimSpace(manifest.PlanHash) != "" && wantPlanHash != "" && manifest.PlanHash != wantPlanHash {
					return fmt.Errorf("bundle planHash mismatch (%s != %s)", manifest.PlanHash, wantPlanHash)
				}
				if err := stack.ApplyInputBundleToPlan(pp, tmpDir, manifest); err != nil {
					return err
				}
				gid := &stack.GitIdentity{Commit: rp.StackGitCommit, Dirty: rp.StackGitDirty}
				for _, n := range pp.Nodes {
					got, _, err := stack.ComputeEffectiveInputHashWithOptions(n, stack.EffectiveInputHashOptions{
						StackRoot:             tmpDir,
						IncludeValuesContents: true,
						StackGitIdentity:      gid,
					})
					if err != nil {
						return err
					}
					want := strings.TrimSpace(n.EffectiveInputHash)
					if want != "" && got != want {
						return fmt.Errorf("%s inputs mismatch (want %s got %s)", n.ID, want, got)
					}
				}
				pp.StackRoot = *rootDir
				p = pp
			} else if resume && !replan {
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
				initialAttempts := loaded.AttemptByID
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
				if !yes {
					if largeDeletePromptThreshold <= 0 {
						largeDeletePromptThreshold = 20
					}
					if len(p.Nodes) >= largeDeletePromptThreshold {
						dec, err := approvalMode(cmd, false, false)
						if err != nil {
							return err
						}
						prompt := fmt.Sprintf("About to delete %d releases. Only 'yes' will be accepted:", len(p.Nodes))
						if err := confirmAction(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), dec, prompt, confirmModeYes, ""); err != nil {
							return err
						}
					}
				}
				return stack.Run(cmd.Context(), stack.RunOptions{
					Command:                "delete",
					Plan:                   p,
					Concurrency:            concurrency,
					ProgressiveConcurrency: progressiveConcurrency,
					FailFast:               failFast || !continueOnError,
					AutoApprove:            yes,
					DryRun:                 false,
					Diff:                   false,
					Lock:                   lock,
					LockOwner:              lockOwner,
					LockTTL:                lockTTL,
					TakeoverLock:           takeover,
					Kubeconfig:             kubeconfig,
					KubeContext:            kubeContext,
					LogLevel:               logLevel,
					RemoteAgentAddr:        remoteAgent,
					// When resuming, always create a new runId; --run-id selects which previous
					// run to resume from, not the id of the new run.
					RunID:           "",
					RunRoot:         "",
					FailMode:        chooseFailMode(failFast || !continueOnError),
					MaxAttempts:     maxAttemptsFromRetry(retry),
					InitialAttempts: initialAttempts,
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
					AllowMissingDeps:     *allowMissingDeps,
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
			if !yes {
				if largeDeletePromptThreshold <= 0 {
					largeDeletePromptThreshold = 20
				}
				if len(p.Nodes) >= largeDeletePromptThreshold {
					dec, err := approvalMode(cmd, false, false)
					if err != nil {
						return err
					}
					prompt := fmt.Sprintf("About to delete %d releases. Only 'yes' will be accepted:", len(p.Nodes))
					if err := confirmAction(cmd.Context(), cmd.InOrStdin(), cmd.ErrOrStderr(), dec, prompt, confirmModeYes, ""); err != nil {
						return err
					}
				}
			}
			return stack.Run(cmd.Context(), stack.RunOptions{
				Command:                "delete",
				Plan:                   p,
				Concurrency:            concurrency,
				ProgressiveConcurrency: progressiveConcurrency,
				FailFast:               failFast || !continueOnError,
				AutoApprove:            yes,
				DryRun:                 false,
				Diff:                   false,
				Lock:                   lock,
				LockOwner:              lockOwner,
				LockTTL:                lockTTL,
				TakeoverLock:           takeover,
				Kubeconfig:             kubeconfig,
				KubeContext:            kubeContext,
				LogLevel:               logLevel,
				RemoteAgentAddr:        remoteAgent,
				RunID:                  strings.TrimSpace(runID),
				RunRoot:                runRoot,
				FailMode:               chooseFailMode(failFast || !continueOnError),
				MaxAttempts:            maxAttemptsFromRetry(retry),
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
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Maximum number of concurrent releases to run")
	cmd.Flags().BoolVar(&progressiveConcurrency, "progressive-concurrency", false, "Start at 1 worker, then ramp up/down based on successes/failures")
	cmd.Flags().BoolVar(&failFast, "fail-fast", true, "Stop scheduling new releases on first error")
	cmd.Flags().BoolVar(&continueOnError, "continue-on-error", false, "Continue scheduling independent releases after failures")
	cmd.Flags().BoolVar(&yes, "yes", false, "Skip confirmation prompts")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Preview deletes without applying them (not supported)")
	cmd.Flags().BoolVar(&resume, "resume", false, "Resume the most recent run (uses its frozen plan.json unless --replan is set)")
	cmd.Flags().StringVar(&runID, "run-id", "", "Resume a specific run ID (directory name under .ktl/stack/runs)")
	cmd.Flags().BoolVar(&replan, "replan", false, "Recompute the plan from current config when resuming")
	cmd.Flags().BoolVar(&allowDrift, "allow-drift", false, "Allow resume even when inputs changed since the plan was written (unsafe)")
	cmd.Flags().BoolVar(&rerunFailed, "rerun-failed", false, "When resuming, schedule only failed nodes")
	cmd.Flags().IntVar(&largeDeletePromptThreshold, "delete-confirm-threshold", 20, "Prompt when deleting at least this many releases (0 disables)")
	cmd.Flags().IntVar(&retry, "retry", 1, "Maximum attempts per release (includes the initial attempt)")
	cmd.Flags().BoolVar(&lock, "lock", true, "Acquire a stack state lock for this run")
	cmd.Flags().BoolVar(&takeover, "takeover", false, "Take over the stack state lock if held (unsafe)")
	cmd.Flags().DurationVar(&lockTTL, "lock-ttl", 30*time.Minute, "How long before the lock is considered stale")
	cmd.Flags().StringVar(&lockOwner, "lock-owner", "", "Lock owner string (defaults to user@host:pid)")
	cmd.Flags().StringVar(&sealedDir, "sealed-dir", "", "Run from a sealed plan/bundle directory (expects plan.json + inputs.tar.gz)")
	return cmd
}

func chooseFailMode(failFast bool) string {
	if failFast {
		return "fail-fast"
	}
	return "continue"
}

func maxAttemptsFromRetry(retry int) int {
	if retry <= 0 {
		return 1
	}
	return retry
}

func readRunPlanFile(path string) (*stack.RunPlan, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var rp stack.RunPlan
	if err := json.Unmarshal(raw, &rp); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &rp, nil
}

type stackSealAttestationFile struct {
	PlanHash         string `json:"planHash"`
	InputsBundle     string `json:"inputsBundle,omitempty"`
	InputsBundleSH   string `json:"inputsBundleDigest,omitempty"`
	StackGitCommit   string `json:"stackGitCommit,omitempty"`
	StackGitDirty    bool   `json:"stackGitDirty,omitempty"`
	KtlVersion       string `json:"ktlVersion,omitempty"`
	KtlGitCommit     string `json:"ktlGitCommit,omitempty"`
	AttestationVer   string `json:"apiVersion,omitempty"`
	AttestationStamp string `json:"createdAt,omitempty"`
}

func readSealAttestation(path string) (*stackSealAttestationFile, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var a stackSealAttestationFile
	if err := json.Unmarshal(raw, &a); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &a, nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return "sha256:" + hex.EncodeToString(h.Sum(nil)), nil
}
