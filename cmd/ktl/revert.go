package main

import (
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/ui"
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
	var yes bool
	var nonInteractive bool
	var verbose bool

	wait = true
	timeout = 5 * time.Minute

	cmd := &cobra.Command{
		Use:   "revert",
		Short: "Rollback a Helm release to a previous revision",
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

			fromRev, toRev, err := selectRevertTarget(revisions, revision)
			if err != nil {
				return err
			}

			if err := confirmAction(ctx, cmd.InOrStdin(), errOut, dec, fmt.Sprintf("Revert release %s from revision %d to %d? Only 'yes' will be accepted:", releaseName, fromRev, toRev), confirmModeYes, ""); err != nil {
				return err
			}

			if shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) && isTerminalWriter(errOut) {
				width, _ := terminalWidth(errOut)
				meta := ui.DeployMetadata{Release: releaseName, Namespace: resolvedNamespace}
				console := ui.NewDeployConsole(errOut, meta, ui.DeployConsoleOptions{Enabled: true, Width: width})
				defer console.Done()
			}

			rollback := action.NewRollback(actionCfg)
			rollback.Version = toRev
			rollback.Wait = wait
			rollback.Timeout = timeout
			if err := rollback.Run(releaseName); err != nil {
				return fmt.Errorf("helm rollback: %w", err)
			}

			// Helm rollback doesn't return the new release object; fetch it for status/revision.
			getAction := action.NewGet(actionCfg)
			rel, _ := getAction.Run(releaseName)
			status := "unknown"
			newRev := toRev
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
			fmt.Fprintf(cmd.OutOrStdout(), "Release %s %s (revision %d)\n", releaseName, status, newRev)
			writeReportTable(errOut, reportLine{
				Kind:      "revert",
				Result:    "success",
				Release:   releaseName,
				Namespace: resolvedNamespace,
				Chart:     chart,
				Version:   versionStr,
				Revision:  newRev,
				ElapsedMS: time.Since(startedAt).Milliseconds(),
			})
			return nil
		},
	}

	cmd.Flags().StringVar(&releaseName, "release", "", "Helm release name")
	cmd.Flags().IntVar(&revision, "revision", 0, "Target revision to rollback to (default: last known-good)")
	cmd.Flags().BoolVar(&wait, "wait", true, "Wait for resources to become ready")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "How long to wait for the rollback")
	cmd.Flags().BoolVar(&yes, "yes", false, "Auto-approve confirmation prompts")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (equivalent to --log-level=debug)")
	_ = cmd.MarkFlagRequired("release")
	cmd.Flags().StringVarP(&namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	decorateCommandHelp(cmd, "Deploy Operations")
	return cmd
}

func selectRevertTarget(revisions []*release.Release, requested int) (fromRev int, toRev int, err error) {
	var current int
	for _, rel := range revisions {
		if rel == nil || rel.Info == nil {
			continue
		}
		if rel.Info.Status == release.StatusDeployed && rel.Version > current {
			current = rel.Version
		}
	}
	if current == 0 {
		// Fall back to max revision if Helm doesn't mark any as deployed.
		for _, rel := range revisions {
			if rel != nil && rel.Version > current {
				current = rel.Version
			}
		}
	}
	if current == 0 {
		return 0, 0, fmt.Errorf("no release revisions found")
	}
	if requested > 0 {
		if requested >= current {
			return current, requested, fmt.Errorf("target revision %d must be < current revision %d", requested, current)
		}
		return current, requested, nil
	}
	candidate := 0
	for _, rel := range revisions {
		if rel == nil || rel.Info == nil {
			continue
		}
		if rel.Version >= current {
			continue
		}
		switch rel.Info.Status {
		case release.StatusSuperseded, release.StatusDeployed:
			if rel.Version > candidate {
				candidate = rel.Version
			}
		}
	}
	if candidate == 0 {
		return current, 0, fmt.Errorf("no previous successful revision found for %s", revisions[0].Name)
	}
	return current, candidate, nil
}
