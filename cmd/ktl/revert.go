package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/kube"
	"github.com/spf13/cobra"
	"go.uber.org/zap/zapcore"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/release"
	"helm.sh/helm/v3/pkg/storage/driver"
)

func newRevertCommand(kubeconfig *string, kubeContext *string, logLevel *string) *cobra.Command {
	var namespace string
	var releaseName string
	var revision int
	var wait bool
	var timeout time.Duration
	var dryRun bool
	var yes bool
	var nonInteractive bool
	var verbose bool

	wait = true
	timeout = 5 * time.Minute

	cmd := &cobra.Command{
		Use:   "revert",
		Short: "Revert a Helm release to a previous revision",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateVerboseLogLevel(cmd, verbose, logLevel); err != nil {
				return err
			}
			if err := validateNonInteractive(cmd, nonInteractive, yes); err != nil {
				return err
			}
			if timeout <= 0 {
				return fmt.Errorf("--timeout must be > 0")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) (runErr error) {
			currentLogLevel := effectiveLogLevel(logLevel)
			errOut := cmd.ErrOrStderr()
			startedAt := time.Now()
			report := reportLine{
				Kind:    "revert",
				Release: releaseName,
				DryRun:  dryRun,
			}
			defer func() {
				report.Result = "success"
				if runErr != nil {
					report.Result = "fail"
				}
				report.ElapsedMS = time.Since(startedAt).Milliseconds()
				writeReportTable(errOut, report)
			}()

			dec, err := approvalMode(cmd, yes, nonInteractive)
			if err != nil {
				return err
			}

			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			resolvedNamespace := strings.TrimSpace(namespace)
			if resolvedNamespace == "" {
				resolvedNamespace = kubeClient.Namespace
			}
			if resolvedNamespace == "" {
				resolvedNamespace = "default"
			}
			report.Namespace = resolvedNamespace

			settings := cli.New()
			if kubeconfig != nil && *kubeconfig != "" {
				settings.KubeConfig = *kubeconfig
			}
			if kubeContext != nil && *kubeContext != "" {
				settings.KubeContext = *kubeContext
			}
			settings.SetNamespace(resolvedNamespace)
			settings.Debug = shouldLogAtLevel(currentLogLevel, zapcore.DebugLevel)

			actionCfg := new(action.Configuration)
			if err := actionCfg.Init(settings.RESTClientGetter(), resolvedNamespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
				return fmt.Errorf("init helm action config: %w", err)
			}

			if strings.TrimSpace(releaseName) == "" {
				return fmt.Errorf("--release is required")
			}

			historyAction := action.NewHistory(actionCfg)
			historyAction.Max = 50
			revisions, err := historyAction.Run(releaseName)
			if err != nil {
				if errors.Is(err, driver.ErrReleaseNotFound) {
					return fmt.Errorf("release %s not found", releaseName)
				}
				return fmt.Errorf("helm history: %w", err)
			}

			selection, err := selectRevertTarget(releaseName, revisions, revision)
			if err != nil {
				return err
			}
			report.Namespace = resolvedNamespace
			fmt.Fprintln(errOut, selection.RationaleLine())
			if len(selection.Candidates) > 0 {
				fmt.Fprintf(errOut, "Candidates: %s\n", strings.Join(selection.Candidates, ", "))
			}

			if err := confirmAction(ctx, cmd.InOrStdin(), errOut, dec, fmt.Sprintf("Revert release %s from revision %d to %d? Only 'yes' will be accepted:", releaseName, selection.FromRevision, selection.ToRevision), confirmModeYes, ""); err != nil {
				return err
			}

			_ = currentLogLevel

			if dryRun {
				fmt.Fprintf(cmd.OutOrStdout(), "Revert plan: would revert %s from revision %d to %d\n", releaseName, selection.FromRevision, selection.ToRevision)
				return nil
			}

			rollback := action.NewRollback(actionCfg)
			rollback.Version = selection.ToRevision
			rollback.Wait = wait
			rollback.Timeout = timeout
			if err := rollback.Run(releaseName); err != nil {
				return fmt.Errorf("helm rollback: %w", err)
			}

			// Helm rollback doesn't return the new release object; fetch both the new and target revisions.
			getCurrent := action.NewGet(actionCfg)
			rel, _ := getCurrent.Run(releaseName)
			getTarget := action.NewGet(actionCfg)
			getTarget.Version = selection.ToRevision
			targetRel, _ := getTarget.Run(releaseName)

			trackerManifest := ""
			if targetRel != nil && strings.TrimSpace(targetRel.Manifest) != "" {
				trackerManifest = targetRel.Manifest
			}

			// Render a single tracker snapshot for the reverted-to manifest to show final state.
			if shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) && isTerminalWriter(errOut) && strings.TrimSpace(trackerManifest) != "" {
				rows := make(chan []deploy.ResourceStatus, 1)
				update := func(r []deploy.ResourceStatus) {
					if r != nil {
						select {
						case rows <- r:
						default:
						}
					}
				}
				snapCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				defer cancel()
				tracker := deploy.NewResourceTracker(kubeClient, resolvedNamespace, releaseName, trackerManifest, update).WithInterval(250 * time.Millisecond)
				go tracker.Run(snapCtx)
				select {
				case r := <-rows:
					fmt.Fprintln(errOut, "Resource                                 Action   Status       Message")
					fmt.Fprintln(errOut, strings.Repeat("-", 100))
					for _, row := range r {
						fmt.Fprintf(errOut, "%-40s  %-7s  %-10s  %s\n", fmt.Sprintf("%s %s/%s", row.Kind, row.Namespace, row.Name), "-", row.Status, row.Message)
					}
				case <-snapCtx.Done():
				}
			}

			status := "unknown"
			newRev := selection.ToRevision
			chart := ""
			versionStr := ""
			if rel != nil {
				newRev = rel.Version
				if rel.Info != nil {
					status = rel.Info.Status.String()
				}
				if rel.Chart != nil && rel.Chart.Metadata != nil {
					chart = rel.Chart.Metadata.Name
					versionStr = rel.Chart.Metadata.Version
				}
			}
			report.Chart = chart
			report.Version = versionStr
			report.Revision = newRev
			fmt.Fprintf(cmd.OutOrStdout(), "Release %s %s (revision %d)\n", releaseName, status, newRev)
			return nil
		},
	}

	cmd.Flags().StringVar(&releaseName, "release", "", "Helm release name")
	cmd.Flags().IntVar(&revision, "revision", 0, "Target revision to revert to (default: last known-good)")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for resources to become ready")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "How long to wait for the rollback")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Show the target revision and exit without changing the cluster")
	cmd.Flags().BoolVar(&yes, "yes", false, "Auto-approve confirmation prompts")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (equivalent to --log-level=debug)")
	_ = cmd.MarkFlagRequired("release")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	decorateCommandHelp(cmd, "Deploy Operations")
	return cmd
}

type revertSelection struct {
	ReleaseName       string
	CurrentRevision   int
	CurrentStatus     string
	EffectiveRevision int
	EffectiveStatus   string

	FromRevision int
	ToRevision   int
	ToStatus     string

	Candidates []string
}

func (s revertSelection) RationaleLine() string {
	cur := fmt.Sprintf("current=%d", s.CurrentRevision)
	if s.CurrentStatus != "" {
		cur += fmt.Sprintf(" (%s)", s.CurrentStatus)
	}
	eff := fmt.Sprintf("effective=%d", s.EffectiveRevision)
	if s.EffectiveStatus != "" {
		eff += fmt.Sprintf(" (%s)", s.EffectiveStatus)
	}
	sel := fmt.Sprintf("selected=%d", s.ToRevision)
	if s.ToStatus != "" {
		sel += fmt.Sprintf(" (%s)", s.ToStatus)
	}
	return fmt.Sprintf("Revert selection: %s, %s, %s", cur, eff, sel)
}

func selectRevertTarget(releaseName string, revisions []*release.Release, requested int) (revertSelection, error) {
	name := strings.TrimSpace(releaseName)
	if name == "" && len(revisions) > 0 && revisions[0] != nil {
		name = strings.TrimSpace(revisions[0].Name)
	}
	if name == "" {
		name = "release"
	}
	type revInfo struct {
		version int
		status  release.Status
	}
	list := make([]revInfo, 0, len(revisions))
	seen := map[int]struct{}{}
	for _, rel := range revisions {
		if rel == nil || rel.Version == 0 || rel.Info == nil {
			continue
		}
		if _, ok := seen[rel.Version]; ok {
			continue
		}
		seen[rel.Version] = struct{}{}
		list = append(list, revInfo{version: rel.Version, status: rel.Info.Status})
	}
	if len(list) == 0 {
		return revertSelection{}, fmt.Errorf("no release revisions found for %s", name)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].version < list[j].version })

	current := list[len(list)-1]
	effective := current
	if current.status != release.StatusDeployed {
		// If the latest revision isn't deployed (failed/pending), treat the last deployed as effective.
		for i := len(list) - 1; i >= 0; i-- {
			if list[i].status == release.StatusDeployed {
				effective = list[i]
				break
			}
		}
	}
	if effective.version == 0 {
		effective = current
	}

	selection := revertSelection{
		ReleaseName:       name,
		CurrentRevision:   current.version,
		CurrentStatus:     current.status.String(),
		EffectiveRevision: effective.version,
		EffectiveStatus:   effective.status.String(),
		FromRevision:      effective.version,
	}

	// Build candidate list (< effective) preferring superseded over deployed.
	var superseded []revInfo
	var deployed []revInfo
	for _, r := range list {
		if r.version >= effective.version {
			continue
		}
		switch r.status {
		case release.StatusSuperseded:
			superseded = append(superseded, r)
		case release.StatusDeployed:
			deployed = append(deployed, r)
		}
	}
	sort.Slice(superseded, func(i, j int) bool { return superseded[i].version > superseded[j].version })
	sort.Slice(deployed, func(i, j int) bool { return deployed[i].version > deployed[j].version })

	for _, r := range append(append([]revInfo(nil), superseded...), deployed...) {
		if len(selection.Candidates) >= 3 {
			break
		}
		selection.Candidates = append(selection.Candidates, fmt.Sprintf("%d(%s)", r.version, strings.ToLower(r.status.String())))
	}

	if requested > 0 {
		found := false
		var requestedStatus release.Status
		for _, r := range list {
			if r.version == requested {
				found = true
				requestedStatus = r.status
				break
			}
		}
		if !found {
			return revertSelection{}, fmt.Errorf("target revision %d not found for %s", requested, name)
		}
		if requested >= effective.version {
			return revertSelection{}, fmt.Errorf("target revision %d must be < effective revision %d", requested, effective.version)
		}
		selection.ToRevision = requested
		selection.ToStatus = strings.ToLower(requestedStatus.String())
		return selection, nil
	}

	if len(superseded) > 0 {
		selection.ToRevision = superseded[0].version
		selection.ToStatus = strings.ToLower(superseded[0].status.String())
		return selection, nil
	}
	if len(deployed) > 0 {
		selection.ToRevision = deployed[0].version
		selection.ToStatus = strings.ToLower(deployed[0].status.String())
		return selection, nil
	}
	return revertSelection{}, fmt.Errorf("no previous successful revision found for %s", name)
}
