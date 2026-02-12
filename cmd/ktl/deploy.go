// File: cmd/ktl/deploy.go
// Brief: Shared Helm plan/apply/delete CLI implementation.

// deploy.go contains the shared implementation for Helm operations used by `ktl apply plan`, `ktl apply`, and `ktl delete`.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/kubekattle/ktl/internal/capture"
	"github.com/kubekattle/ktl/internal/caststream"
	"github.com/kubekattle/ktl/internal/castutil"
	"github.com/kubekattle/ktl/internal/config"
	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/kube"
	"github.com/kubekattle/ktl/internal/secretstore"
	"github.com/kubekattle/ktl/internal/tailer"
	"github.com/kubekattle/ktl/internal/telemetry"
	"github.com/kubekattle/ktl/internal/ui"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"go.uber.org/zap/zapcore"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	helmkube "helm.sh/helm/v3/pkg/kube"
	"helm.sh/helm/v3/pkg/release"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const historyBreadcrumbLimit = 6

func newDeployApplyCommand(namespace *string, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string, helpSection string) *cobra.Command {
	ownNamespaceFlag := false
	if namespace == nil {
		namespace = new(string)
		ownNamespaceFlag = true
	}
	var chart string
	var releaseName string
	var version string
	var valuesFiles []string
	var setValues []string
	var setStringValues []string
	var setFileValues []string
	var secretProvider string
	var secretConfig string
	wait := true
	atomic := true
	upgrade := false
	var createNamespace bool
	var dryRun bool
	var watchDuration time.Duration
	var uiAddr string
	var wsListenAddr string
	var verbose bool
	var autoApprove bool
	var nonInteractive bool
	var planServer bool
	var capturePath string
	var captureTags []string
	var driftGuard bool
	var driftGuardMode string
	var requireVerified string
	timeout := 5 * time.Minute

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Render and apply a Helm chart using upgrade --install",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateVerboseLogLevel(cmd, verbose, logLevel); err != nil {
				return err
			}
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				if watchDuration > 0 {
					return fmt.Errorf("--watch is not supported with --remote-agent")
				}
				if strings.TrimSpace(uiAddr) != "" || strings.TrimSpace(wsListenAddr) != "" {
					return fmt.Errorf("--ui/--ws-listen are not supported with --remote-agent")
				}
				if strings.TrimSpace(requireVerified) != "" {
					return fmt.Errorf("--require-verified is not supported with --remote-agent")
				}
				if strings.TrimSpace(secretProvider) != "" || strings.TrimSpace(secretConfig) != "" {
					return fmt.Errorf("--secret-provider/--secret-config are not supported with --remote-agent")
				}
			}
			if watchDuration > 0 && dryRun {
				return fmt.Errorf("--watch cannot be combined with --dry-run")
			}
			if err := validateNonInteractive(cmd, nonInteractive, autoApprove); err != nil {
				return fmt.Errorf("%w (or use --dry-run)", err)
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
			var report reportLine
			var reportReady bool
			var (
				historyBreadcrumbs []deploy.HistoryBreadcrumb
				lastSuccessful     *deploy.HistoryBreadcrumb
				actionHeadline     string
				console            *ui.DeployConsole
			)
			ctx := cmd.Context()
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				return runRemoteDeployApply(cmd, remoteDeployApplyArgs{
					Chart:           chart,
					Release:         releaseName,
					Namespace:       namespace,
					Version:         version,
					ValuesFiles:     valuesFiles,
					SetValues:       setValues,
					SetStringValues: setStringValues,
					SetFileValues:   setFileValues,
					Timeout:         timeout,
					Wait:            wait,
					Atomic:          atomic,
					UpgradeOnly:     upgrade,
					CreateNamespace: createNamespace,
					DryRun:          dryRun,
					Diff:            false,
					KubeConfig:      kubeconfig,
					KubeContext:     kubeContext,
					RemoteAddr:      strings.TrimSpace(*remoteAgent),
				})
			}
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}

			resolvedNamespace := ""
			if namespace != nil {
				resolvedNamespace = *namespace
			}
			if resolvedNamespace == "" {
				resolvedNamespace = kubeClient.Namespace
			}

			settings := cli.New()
			if kubeconfig != nil && *kubeconfig != "" {
				settings.KubeConfig = *kubeconfig
			}
			if kubeContext != nil && *kubeContext != "" {
				settings.KubeContext = *kubeContext
			}
			if resolvedNamespace != "" {
				settings.SetNamespace(resolvedNamespace)
			}
			attachKubeTelemetry(settings, kubeClient)
			helmDebug := shouldLogAtLevel(currentLogLevel, zapcore.DebugLevel)
			settings.Debug = helmDebug

			dec, err := approvalMode(cmd, autoApprove, nonInteractive)
			if err != nil {
				return err
			}

			exists, err := namespaceExists(ctx, kubeClient.Clientset, resolvedNamespace)
			if err != nil {
				return err
			}
			if !exists && !createNamespace {
				return fmt.Errorf("namespace %s does not exist (rerun with --create-namespace to create it)", resolvedNamespace)
			}

			if createNamespace {
				if err := ensureNamespace(ctx, kubeClient.Clientset, resolvedNamespace); err != nil {
					return err
				}
			}

			actionCfg := new(action.Configuration)
			logFunc := func(format string, v ...interface{}) {
				if !helmDebug {
					return
				}
				fmt.Fprintf(errOut, "[helm] "+format+"\n", v...)
			}
			if err := actionCfg.Init(settings.RESTClientGetter(), resolvedNamespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
				return fmt.Errorf("init helm action config: %w", err)
			}

			var secretRefs []deploy.SecretRef
			secretResolver, secretAuditSink, err := buildDeploySecretResolver(ctx, deploySecretConfig{
				Chart:      chart,
				ConfigPath: secretConfig,
				Provider:   secretProvider,
				Mode:       secretstore.ResolveModeValue,
				ErrOut:     errOut,
			})
			if err != nil {
				return err
			}
			auditSink := func(report secretstore.AuditReport) {
				secretRefs = secretRefsFromAudit(report)
				if secretAuditSink != nil {
					secretAuditSink(report)
				}
			}
			secretOptions := &deploy.SecretOptions{Resolver: secretResolver, AuditSink: auditSink}

			if driftGuard {
				driftOpts := deploy.InstallOptions{
					Chart:           chart,
					Version:         version,
					ReleaseName:     releaseName,
					Namespace:       resolvedNamespace,
					ValuesFiles:     valuesFiles,
					SetValues:       setValues,
					SetStringValues: setStringValues,
					SetFileValues:   setFileValues,
					Secrets:         secretOptions,
					Timeout:         timeout,
					Wait:            false,
					Atomic:          false,
					CreateNamespace: createNamespace,
					DryRun:          true,
					Diff:            false,
					UpgradeOnly:     upgrade,
				}
				if err := deploy.RunDriftCheck(ctx, actionCfg, settings, kubeClient, driftGuardMode, releaseName, driftOpts); err != nil {
					return err
				}
			}

			// Terraform-like safety rail: show a concise plan summary and ask for confirmation
			// before making any cluster changes (unless --auto-approve or in dry-run mode).
			if !dryRun && !autoApprove {
				preview, previewErr := deploy.GeneratePlanPreview(ctx, actionCfg, settings, kubeClient, deploy.InstallOptions{
					Chart:           chart,
					Version:         version,
					ReleaseName:     releaseName,
					Namespace:       resolvedNamespace,
					ValuesFiles:     valuesFiles,
					SetValues:       setValues,
					SetStringValues: setStringValues,
					SetFileValues:   setFileValues,
					Secrets:         secretOptions,
					Timeout:         timeout,
					Wait:            false,
					Atomic:          false,
					CreateNamespace: createNamespace,
					DryRun:          true,
					Diff:            true,
					UpgradeOnly:     upgrade,
				}, planServer)
				if previewErr != nil {
					return previewErr
				}
				if preview != nil && preview.PlanSummary != nil {
					fmt.Fprintf(errOut, "Plan: %d to add, %d to change, %d to replace, %d to destroy.\n", preview.PlanSummary.Add, preview.PlanSummary.Change, preview.PlanSummary.Replace, preview.PlanSummary.Destroy)
					if preview.PlanSummary.Hooks.Add > 0 || preview.PlanSummary.Hooks.Change > 0 || preview.PlanSummary.Hooks.Replace > 0 || preview.PlanSummary.Hooks.Destroy > 0 {
						fmt.Fprintf(errOut, "Hooks: %d to add, %d to change, %d to replace, %d to destroy.\n", preview.PlanSummary.Hooks.Add, preview.PlanSummary.Hooks.Change, preview.PlanSummary.Hooks.Replace, preview.PlanSummary.Hooks.Destroy)
					}
					if preview.PlanSummarizeError != "" && shouldLogAtLevel(currentLogLevel, zapcore.WarnLevel) {
						fmt.Fprintf(errOut, "Warning: unable to fully summarize plan: %s\n", preview.PlanSummarizeError)
					}
					limit := 12
					if len(preview.PlanSummary.Changes) > 0 {
						if len(preview.PlanSummary.Changes) < limit {
							limit = len(preview.PlanSummary.Changes)
						}
						for _, ch := range preview.PlanSummary.Changes[:limit] {
							prefix := "~"
							switch ch.Action {
							case deploy.PlanAdd:
								prefix = "+"
							case deploy.PlanDestroy:
								prefix = "-"
							case deploy.PlanUpdate:
								prefix = "~"
							case deploy.PlanReplace:
								prefix = "±"
							}
							nsLabel := ch.Namespace
							if nsLabel == "" {
								nsLabel = "-"
							}
							fmt.Fprintf(errOut, "  %s %s/%s (ns: %s)\n", prefix, ch.Kind, ch.Name, nsLabel)
						}
						if len(preview.PlanSummary.Changes) > limit {
							fmt.Fprintf(errOut, "  (and %d more)\n", len(preview.PlanSummary.Changes)-limit)
						}
					}
					if len(preview.PlanSummary.Hooks.Changes) > 0 {
						fmt.Fprintln(errOut, "Hook changes:")
						limitHooks := 8
						if len(preview.PlanSummary.Hooks.Changes) < limitHooks {
							limitHooks = len(preview.PlanSummary.Hooks.Changes)
						}
						for _, ch := range preview.PlanSummary.Hooks.Changes[:limitHooks] {
							prefix := "~"
							switch ch.Action {
							case deploy.PlanAdd:
								prefix = "+"
							case deploy.PlanDestroy:
								prefix = "-"
							case deploy.PlanReplace:
								prefix = "±"
							}
							nsLabel := ch.Namespace
							if nsLabel == "" {
								nsLabel = "-"
							}
							hookLabel := strings.TrimSpace(ch.Hook)
							if hookLabel == "" {
								hookLabel = "hook"
							}
							fmt.Fprintf(errOut, "  %s %s/%s (ns: %s, %s)\n", prefix, ch.Kind, ch.Name, nsLabel, hookLabel)
						}
						if len(preview.PlanSummary.Hooks.Changes) > limitHooks {
							fmt.Fprintf(errOut, "  (and %d more)\n", len(preview.PlanSummary.Hooks.Changes)-limitHooks)
						}
					}
				}
				if err := confirmAction(cmd.Context(), cmd.InOrStdin(), errOut, dec, "Do you want to perform these actions? Only 'yes' will be accepted:", confirmModeYes, ""); err != nil {
					return err
				}
			}

			stream := deploy.NewStreamBroadcaster(releaseName, resolvedNamespace, chart)
			var captureRecorder *capture.Recorder
			if path := strings.TrimSpace(capturePath); path != "" {
				path, err = capture.ResolvePath(cmd.CommandPath(), path, time.Now())
				if err != nil {
					return err
				}
				host, _ := os.Hostname()
				tagMap, err := parseCaptureTags(captureTags)
				if err != nil {
					return err
				}
				rec, err := capture.Open(path, capture.SessionMeta{
					Command:   cmd.CommandPath(),
					Args:      append([]string(nil), os.Args[1:]...),
					StartedAt: time.Now().UTC(),
					Host:      host,
					Tags:      tagMap,
					Entities: capture.Entities{
						KubeContext:  derefString(kubeContext),
						Namespace:    resolvedNamespace,
						Release:      releaseName,
						Chart:        chart,
						BuildContext: "",
					},
				})
				if err != nil {
					return err
				}
				captureRecorder = rec
				stream.AddObserver(rec)
				fmt.Fprintf(errOut, "Capturing apply session to %s (session %s)\n", path, rec.SessionID())
			}
			timerObserver := newPhaseTimerObserver()
			var deployedRelease *release.Release
			defer func() {
				if captureRecorder != nil {
					_ = captureRecorder.Close()
				}
				summary := deploy.SummaryPayload{
					Release:   releaseName,
					Namespace: resolvedNamespace,
				}
				if runErr != nil {
					summary.Status = "failed"
					summary.Error = runErr.Error()
				}
				if deployedRelease != nil {
					if deployedRelease.Info != nil {
						summary.Status = deployedRelease.Info.Status.String()
						summary.Notes = deployedRelease.Info.Notes
					}
					if deployedRelease.Chart != nil && deployedRelease.Chart.Metadata != nil {
						summary.Chart = deployedRelease.Chart.Metadata.Name
						summary.Version = deployedRelease.Chart.Metadata.Version
					}
				}
				summary.Secrets = cloneSecretRefs(secretRefs)
				historyCopy := deploy.CloneBreadcrumbs(historyBreadcrumbs)
				lastSuccessCopy := deploy.CloneBreadcrumbPointer(lastSuccessful)
				if deployedRelease != nil {
					if crumb, ok := deploy.BreadcrumbFromRelease(deployedRelease); ok {
						historyCopy = deploy.PrependBreadcrumb(historyCopy, crumb, historyBreadcrumbLimit)
						if deploy.IsSuccessfulStatus(summary.Status) {
							c := crumb
							lastSuccessCopy = &c
						}
						actionHeadline = deploy.DescribeDeployAction(deploy.ActionDescriptor{
							Release:   releaseName,
							Chart:     crumb.Chart,
							Version:   crumb.Version,
							Namespace: resolvedNamespace,
							DryRun:    dryRun,
							Diff:      false,
						})
					}
				}
				if actionHeadline == "" {
					actionHeadline = deploy.DescribeDeployAction(deploy.ActionDescriptor{
						Release:   releaseName,
						Chart:     summary.Chart,
						Version:   summary.Version,
						Namespace: resolvedNamespace,
						DryRun:    dryRun,
						Diff:      false,
					})
				}
				summary.Action = actionHeadline
				summary.History = historyCopy
				summary.LastSuccessful = lastSuccessCopy
				summary.PhaseDurations = formatPhaseDurations(timerObserver.snapshot())
				if stream != nil {
					stream.EmitSummary(summary)
				}
			}()

			var historyErr error
			historyBreadcrumbs, lastSuccessful, historyErr = deploy.ReleaseHistoryBreadcrumbs(actionCfg, releaseName, historyBreadcrumbLimit)
			if historyErr != nil && shouldLogAtLevel(currentLogLevel, zapcore.WarnLevel) {
				fmt.Fprintf(errOut, "Warning: unable to load release history for %s: %v\n", releaseName, historyErr)
			}
			actionHeadline = deploy.DescribeDeployAction(deploy.ActionDescriptor{
				Release:   releaseName,
				Chart:     chart,
				Version:   version,
				Namespace: resolvedNamespace,
				DryRun:    dryRun,
				Diff:      false,
			})

			trackerManifest, err := renderManifestForTracking(ctx, settings, resolvedNamespace, chart, version, releaseName, valuesFiles, setValues, setStringValues, setFileValues, secretOptions)
			if err != nil && shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) {
				fmt.Fprintf(errOut, "Warning: failed to pre-render manifest for deploy tracker: %v\n", err)
			}
			if strings.TrimSpace(requireVerified) != "" && strings.TrimSpace(trackerManifest) != "" {
				if verr := enforceVerifiedDigest(requireVerified, trackerManifest, releaseName, resolvedNamespace); verr != nil {
					return verr
				}
			}
			if captureRecorder != nil && strings.TrimSpace(trackerManifest) != "" {
				_ = captureRecorder.RecordArtifact(ctx, "rendered_manifest", trackerManifest)
			}
			if captureRecorder != nil {
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.chart", strings.TrimSpace(chart))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.version", strings.TrimSpace(version))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.release", strings.TrimSpace(releaseName))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.namespace", strings.TrimSpace(resolvedNamespace))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.set_values_json", captureJSON(setValues))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.set_string_values_json", captureJSON(setStringValues))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.set_file_values_json", captureJSON(setFileValues))
				_ = captureRecorder.RecordArtifact(ctx, "apply.inputs.values_files_json", captureJSON(deploy.HashFiles(valuesFiles)))
			}

			if stream != nil && (strings.TrimSpace(uiAddr) != "" || strings.TrimSpace(wsListenAddr) != "") {
				logger, logErr := buildLogger(currentLogLevel)
				if logErr != nil {
					return logErr
				}
				viewerLabel := ""
				if addr := strings.TrimSpace(uiAddr); addr != "" {
					uiServer := caststream.New(addr, caststream.ModeWeb, viewerLabel, logger.WithName("deploy-ui"), caststream.WithDeployUI())
					stream.AddObserver(uiServer)
					uiLabel := fmt.Sprintf("%s UI", cmd.CommandPath())
					if err := castutil.StartCastServer(ctx, uiServer, uiLabel, logger.WithName("ui"), errOut); err != nil {
						return err
					}
					fmt.Fprintf(errOut, "Serving %s on %s\n", uiLabel, addr)
				}
				if addr := strings.TrimSpace(wsListenAddr); addr != "" {
					wsServer := caststream.New(addr, caststream.ModeWS, viewerLabel, logger.WithName("deploy-ws"), caststream.WithDeployUI())
					stream.AddObserver(wsServer)
					wsLabel := fmt.Sprintf("%s websocket stream", cmd.CommandPath())
					if err := castutil.StartCastServer(ctx, wsServer, wsLabel, logger.WithName("ws"), errOut); err != nil {
						return err
					}
					fmt.Fprintf(errOut, "Serving %s on %s\n", wsLabel, addr)
				}
			}
			if stream != nil {
				initialSummary := deploy.SummaryPayload{
					Release:   releaseName,
					Namespace: resolvedNamespace,
					Status:    "pending",
				}
				if chart != "" {
					initialSummary.Chart = chart
				}
				if version != "" {
					initialSummary.Version = version
				}
				if actionHeadline != "" {
					initialSummary.Action = actionHeadline
				}
				initialSummary.History = deploy.CloneBreadcrumbs(historyBreadcrumbs)
				initialSummary.LastSuccessful = deploy.CloneBreadcrumbPointer(lastSuccessful)
				initialSummary.Secrets = cloneSecretRefs(secretRefs)
				stream.EmitSummary(initialSummary)
			}

			if console == nil && shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) && isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				meta := ui.DeployMetadata{
					Release:         releaseName,
					Namespace:       resolvedNamespace,
					Chart:           chart,
					ChartVersion:    version,
					ValuesFiles:     append([]string(nil), valuesFiles...),
					SetValues:       append([]string(nil), setValues...),
					SetStringValues: append([]string(nil), setStringValues...),
				}
				console = ui.NewDeployConsole(errOut, meta, ui.DeployConsoleOptions{
					Enabled: true,
					Width:   width,
				})
			}

			var (
				stopSpinner func(success bool)
				cancelTrack context.CancelFunc
				stopLogFeed context.CancelFunc
			)
			defer func() {
				if stopLogFeed != nil {
					stopLogFeed()
				}
			}()
			var statusUpdaters []deploy.StatusUpdateFunc
			updateConsoleMetadata := func() {}
			if console != nil {
				updateConsoleMetadata = func() {
					console.UpdateMetadata(ui.DeployMetadata{
						Release:         releaseName,
						Namespace:       resolvedNamespace,
						Chart:           chart,
						ChartVersion:    version,
						ValuesFiles:     append([]string(nil), valuesFiles...),
						SetValues:       append([]string(nil), setValues...),
						SetStringValues: append([]string(nil), setStringValues...),
					})
				}
				updateConsoleMetadata()
				statusUpdaters = append(statusUpdaters, console.UpdateResources)
			} else if shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) {
				stopSpinner = ui.StartSpinner(errOut, fmt.Sprintf("Applying release %s", releaseName))
			} else if shouldLogAtLevel(currentLogLevel, zapcore.WarnLevel) {
				fmt.Fprintf(errOut, "Applying release %s\n", releaseName)
			}
			// When rendering a plan (dry-run), don't start Kubernetes watchers or resource tracking:
			// - resources aren't created, so "Pending/Unknown" tracking is misleading
			// - status tracking requires live API discovery calls that can fail on minimal RBAC
			if !dryRun {
				if stream != nil && stream.HasObservers() {
					statusUpdaters = append(statusUpdaters, stream.UpdateResources)
					cancelFeed, err := streamReleaseFeed(ctx, kubeClient, releaseName, resolvedNamespace, currentLogLevel, stream)
					if err != nil {
						return err
					}
					stopLogFeed = cancelFeed
					stream.EmitEvent("info", fmt.Sprintf("Watching Kubernetes events for release %s in namespace %s", releaseName, resolvedNamespace))
				}

				if len(statusUpdaters) > 0 {
					trackerCtx, cancel := context.WithCancel(ctx)
					multiUpdate := func(rows []deploy.ResourceStatus) {
						for _, fn := range statusUpdaters {
							if fn != nil {
								fn(rows)
							}
						}
					}
					tracker := deploy.NewResourceTracker(kubeClient, resolvedNamespace, releaseName, trackerManifest, multiUpdate)
					go tracker.Run(trackerCtx)
					cancelTrack = cancel
				}
			}
			defer func() {
				if cancelTrack != nil {
					cancelTrack()
				}
				if console != nil {
					console.Done()
				}
				if stopSpinner != nil {
					stopSpinner(false)
				}
				// Emit the CI-friendly report line after the terminal UI is torn down so it
				// doesn't get overwritten by final console repaints.
				if reportReady {
					report.Result = "success"
					if runErr != nil {
						report.Result = "fail"
					}
					writeReportTable(errOut, report)
				}
			}()

			var progressObservers []deploy.ProgressObserver
			progressObservers = append(progressObservers, timerObserver)
			if console != nil {
				progressObservers = append(progressObservers, console)
			}
			if stream != nil {
				progressObservers = append(progressObservers, stream)
			}

			result, err := deploy.InstallOrUpgrade(ctx, actionCfg, settings, deploy.InstallOptions{
				Chart:             chart,
				Version:           version,
				ReleaseName:       releaseName,
				Namespace:         resolvedNamespace,
				ValuesFiles:       valuesFiles,
				SetValues:         setValues,
				SetStringValues:   setStringValues,
				SetFileValues:     setFileValues,
				Secrets:           secretOptions,
				Timeout:           timeout,
				Wait:              wait,
				Atomic:            atomic,
				CreateNamespace:   createNamespace,
				DryRun:            dryRun,
				Diff:              false,
				UpgradeOnly:       upgrade,
				ProgressObservers: progressObservers,
			})
			if err != nil {
				return err
			}

			deployedRelease = result.Release
			if deployedRelease != nil && deployedRelease.Chart != nil && deployedRelease.Chart.Metadata != nil {
				if deployedRelease.Chart.Metadata.Name != "" {
					chart = deployedRelease.Chart.Metadata.Name
				}
				if deployedRelease.Chart.Metadata.Version != "" {
					version = deployedRelease.Chart.Metadata.Version
				}
				updateConsoleMetadata()
			}
			if stopSpinner != nil {
				stopSpinner(true)
				stopSpinner = nil
			}

			rel := result.Release
			status := "unknown"
			if rel.Info != nil {
				status = rel.Info.Status.String()
			}
			if captureRecorder != nil {
				captureHelmRelease(ctx, captureRecorder, rel)
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Release %s %s\n", rel.Name, status)
			if rel.Info != nil && rel.Info.Notes != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Notes:\n%s\n", rel.Info.Notes)
				if stream != nil {
					stream.EmitEvent("info", fmt.Sprintf("Notes:\n%s", rel.Info.Notes))
				}
				if captureRecorder != nil {
					_ = captureRecorder.RecordArtifact(ctx, "apply.notes", rel.Info.Notes)
					_ = captureRecorder.RecordArtifact(ctx, "apply.status", status)
				}
			}
			if watchDuration > 0 && !dryRun {
				fmt.Fprintf(errOut, "Watching release %s for %s...\n", rel.Name, watchDuration)
				var watchObserver tailer.LogObserver
				if stream != nil && stream.HasObservers() {
					watchObserver = stream
				}
				if err := watchRelease(ctx, cmd, kubeClient, rel.Name, resolvedNamespace, watchDuration, currentLogLevel, watchObserver); err != nil {
					return err
				}
			}
			if line := renderPhaseDurationsLine(formatPhaseDurations(timerObserver.snapshot())); line != "" {
				fmt.Fprintf(errOut, "Phase durations: %s\n", line)
			}
			telemetrySummary := telemetry.Summary{
				Total:  time.Since(startedAt),
				Phases: timerObserver.snapshot(),
			}
			if kubeClient != nil && kubeClient.APIStats != nil {
				metrics := kubeClient.APIStats.Snapshot()
				telemetrySummary.KubeRequests = metrics.Count
				telemetrySummary.KubeAvg = metrics.Avg()
				telemetrySummary.KubeMax = metrics.Max
			}
			if line := telemetrySummary.Line(); line != "" {
				fmt.Fprintln(errOut, line)
			}
			report = reportLine{
				Kind:      "apply",
				Release:   rel.Name,
				Namespace: resolvedNamespace,
				Chart:     chart,
				Version:   version,
				Revision:  rel.Version,
				DryRun:    dryRun,
				ElapsedMS: time.Since(startedAt).Milliseconds(),
			}
			reportReady = true
			return nil
		},
	}

	cmd.Flags().StringVar(&chart, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&releaseName, "release", "", "Helm release name")
	cmd.Flags().StringVar(&version, "version", "", "Chart version (default: latest)")
	cmd.Flags().StringSliceVarP(&valuesFiles, "values", "f", nil, "Values files to apply (can be repeated)")
	cmd.Flags().StringArrayVar(&setValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringArrayVar(&setStringValues, "set-string", nil, "Set STRING values on the command line")
	cmd.Flags().StringArrayVar(&setFileValues, "set-file", nil, "Set values from files (key=path)")
	cmd.Flags().StringVar(&secretProvider, "secret-provider", "", "Secret provider name for secret:// references")
	cmd.Flags().StringVar(&secretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.ktl/config.yaml and repo .ktl.yaml)")
	cmd.Flags().BoolVar(&wait, "wait", wait, "Wait for resources to be ready")
	cmd.Flags().BoolVar(&atomic, "atomic", atomic, "Rollback changes if the upgrade fails")
	cmd.Flags().BoolVar(&upgrade, "upgrade", upgrade, "Only perform the upgrade path (skip install fallback)")
	cmd.Flags().BoolVar(&createNamespace, "create-namespace", false, "Create the release namespace if it does not exist")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Render the chart without applying it")
	cmd.Flags().StringVar(&requireVerified, "require-verified", "", "Require a matching verify report (JSON) for this exact render before applying")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip interactive confirmation prompts")
	_ = cmd.Flags().MarkHidden("auto-approve")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "Alias for --auto-approve")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	cmd.Flags().BoolVar(&planServer, "plan-server", false, "Use server-side dry-run to classify replacements (slower; requires RBAC)")
	cmd.Flags().DurationVar(&watchDuration, "watch", 0, "After a successful deploy, stream logs/events for this long (e.g. 2m)")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "Time to wait for any Kubernetes operation")
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve the live deploy viewer at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	cmd.Flags().StringVar(&wsListenAddr, "ws-listen", "", "Serve the raw deploy event stream over WebSocket at this address (e.g. :9086)")
	cmd.Flags().BoolVar(&driftGuard, "drift-guard", false, "Fail if live cluster resources drift from the last applied Helm release state")
	cmd.Flags().StringVar(&driftGuardMode, "drift-guard-mode", "last-applied", "Drift guard mode: last-applied (compare to current Helm release) or desired (compare to newly rendered manifest)")
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (equivalent to --log-level=debug)")
	cmd.Flags().StringVar(&capturePath, "capture", "", "Capture deploy events/logs/manifests to a SQLite database at this path")
	if flag := cmd.Flags().Lookup("capture"); flag != nil {
		flag.NoOptDefVal = "__auto__"
	}
	cmd.Flags().StringArrayVar(&captureTags, "capture-tag", nil, "Tag the capture session (KEY=VALUE). Repeatable.")

	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")

	if ownNamespaceFlag {
		cmd.Flags().StringVarP(namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	}
	section := strings.TrimSpace(helpSection)
	if section == "" {
		section = "Apply Flags"
	}
	decorateCommandHelp(cmd, section)
	return cmd
}

type deployRemovalConfig struct {
	Use        string
	Short      string
	HelpLabel  string
	Hidden     bool
	WarningMsg string
}

func newDeployRemovalCommand(cfg deployRemovalConfig, namespace *string, kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	ownNamespaceFlag := false
	if namespace == nil {
		namespace = new(string)
		ownNamespaceFlag = true
	}
	var release string
	var wait bool
	var keepHistory bool
	var dryRun bool
	var autoApprove bool
	var nonInteractive bool
	var uiAddr string
	var wsListenAddr string
	var force bool
	var disableHooks bool
	var verbose bool
	var capturePath string
	var captureTags []string
	timeout := 5 * time.Minute

	cmd := &cobra.Command{
		Use:    cfg.Use,
		Short:  cfg.Short,
		Hidden: cfg.Hidden,
		Args:   cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			if err := validateVerboseLogLevel(cmd, verbose, logLevel); err != nil {
				return err
			}
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				if strings.TrimSpace(uiAddr) != "" || strings.TrimSpace(wsListenAddr) != "" {
					return fmt.Errorf("--ui/--ws-listen are not supported with --remote-agent")
				}
			}
			if err := validateNonInteractive(cmd, nonInteractive, autoApprove); err != nil {
				return fmt.Errorf("%w (or use --dry-run)", err)
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
			out := cmd.OutOrStdout()
			startedAt := time.Now()
			ctx := cmd.Context()
			report := reportLine{
				Kind:        "delete",
				Release:     release,
				DryRun:      dryRun,
				KeepHistory: keepHistory,
				Wait:        wait,
			}
			defer func() {
				report.Result = "success"
				if runErr != nil {
					report.Result = "fail"
				}
				report.ElapsedMS = time.Since(startedAt).Milliseconds()
				writeReportTable(errOut, report)
			}()
			if remoteAgent != nil && strings.TrimSpace(*remoteAgent) != "" {
				return runRemoteDeployDestroy(cmd, remoteDeployDestroyArgs{
					Release:      release,
					Namespace:    namespace,
					Timeout:      timeout,
					Wait:         wait,
					KeepHistory:  keepHistory,
					DryRun:       dryRun,
					Force:        force,
					DisableHooks: disableHooks,
					KubeConfig:   kubeconfig,
					KubeContext:  kubeContext,
					RemoteAddr:   strings.TrimSpace(*remoteAgent),
				})
			}
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			var (
				historyBreadcrumbs []deploy.HistoryBreadcrumb
				lastSuccessful     *deploy.HistoryBreadcrumb
				actionHeadline     string
			)

			resolvedNamespace := ""
			if namespace != nil {
				resolvedNamespace = *namespace
			}
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
			if resolvedNamespace != "" {
				settings.SetNamespace(resolvedNamespace)
			}
			attachKubeTelemetry(settings, kubeClient)
			helmDebug := shouldLogAtLevel(currentLogLevel, zapcore.DebugLevel)
			settings.Debug = helmDebug

			exists, err := namespaceExists(ctx, kubeClient.Clientset, resolvedNamespace)
			if err != nil {
				return err
			}
			if !exists {
				return fmt.Errorf("namespace %s does not exist (set --namespace or switch contexts)", resolvedNamespace)
			}

			if cfg.WarningMsg != "" {
				fmt.Fprintln(errOut, cfg.WarningMsg)
				fmt.Fprintf(errOut, "Equivalent command: %s\n", buildDestroySuggestion(release, resolvedNamespace, cmd.Flags()))
			}

			stream := deploy.NewStreamBroadcaster(release, resolvedNamespace, "")
			var captureRecorder *capture.Recorder
			if path := strings.TrimSpace(capturePath); path != "" {
				path, err = capture.ResolvePath(cmd.CommandPath(), path, time.Now())
				if err != nil {
					return err
				}
				host, _ := os.Hostname()
				tagMap, err := parseCaptureTags(captureTags)
				if err != nil {
					return err
				}
				rec, err := capture.Open(path, capture.SessionMeta{
					Command:   cmd.CommandPath(),
					Args:      append([]string(nil), os.Args[1:]...),
					StartedAt: time.Now().UTC(),
					Host:      host,
					Tags:      tagMap,
					Entities: capture.Entities{
						KubeContext: derefString(kubeContext),
						Namespace:   resolvedNamespace,
						Release:     release,
					},
				})
				if err != nil {
					return err
				}
				captureRecorder = rec
				stream.AddObserver(rec)
				fmt.Fprintf(errOut, "Capturing delete session to %s (session %s)\n", path, rec.SessionID())
			}
			timerObserver := newPhaseTimerObserver()
			meta := ui.DeployMetadata{Release: release, Namespace: resolvedNamespace}
			var (
				console     *ui.DeployConsole
				stopSpinner func(bool)
				cancelTrack context.CancelFunc
				stopLogFeed context.CancelFunc
			)
			defer func() {
				if captureRecorder != nil {
					_ = captureRecorder.Close()
				}
				if stopLogFeed != nil {
					stopLogFeed()
				}
				if cancelTrack != nil {
					cancelTrack()
				}
				if console != nil {
					console.Done()
				}
				// Prevent late tracker/feed updates from repainting after teardown/report output.
				console = nil
				stream = nil
				if stopSpinner != nil {
					stopSpinner(false)
				}
			}()

			if shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) && isTerminalWriter(errOut) {
				width, _ := ui.TerminalWidth(errOut)
				console = ui.NewDeployConsole(errOut, meta, ui.DeployConsoleOptions{
					Enabled: true,
					Width:   width,
				})
				console.UpdateMetadata(meta)
			} else if shouldLogAtLevel(currentLogLevel, zapcore.InfoLevel) {
				stopSpinner = ui.StartSpinner(errOut, fmt.Sprintf("Destroying release %s (namespace: %s)", release, resolvedNamespace))
			} else if shouldLogAtLevel(currentLogLevel, zapcore.WarnLevel) {
				fmt.Fprintf(errOut, "Destroying release %s in namespace %s\n", release, resolvedNamespace)
			}
			applyConsoleMeta := func() {}
			if console != nil {
				applyConsoleMeta = func() { console.UpdateMetadata(meta) }
				applyConsoleMeta()
			}

			var logger logr.Logger
			if strings.TrimSpace(uiAddr) != "" || strings.TrimSpace(wsListenAddr) != "" {
				logger, err = buildLogger(currentLogLevel)
				if err != nil {
					return err
				}
			}
			if stream != nil && (strings.TrimSpace(uiAddr) != "" || strings.TrimSpace(wsListenAddr) != "") {
				viewerLabel := ""
				if addr := strings.TrimSpace(uiAddr); addr != "" {
					uiServer := caststream.New(addr, caststream.ModeWeb, viewerLabel, logger.WithName("destroy-ui"), caststream.WithDeployUI())
					stream.AddObserver(uiServer)
					uiLabel := fmt.Sprintf("%s UI", cmd.CommandPath())
					if err := castutil.StartCastServer(ctx, uiServer, uiLabel, logger.WithName("ui"), errOut); err != nil {
						return err
					}
					fmt.Fprintf(errOut, "Serving %s on %s\n", uiLabel, addr)
				}
				if addr := strings.TrimSpace(wsListenAddr); addr != "" {
					wsServer := caststream.New(addr, caststream.ModeWS, viewerLabel, logger.WithName("destroy-ws"), caststream.WithDeployUI())
					stream.AddObserver(wsServer)
					wsLabel := fmt.Sprintf("%s websocket stream", cmd.CommandPath())
					if err := castutil.StartCastServer(ctx, wsServer, wsLabel, logger.WithName("ws"), errOut); err != nil {
						return err
					}
					fmt.Fprintf(errOut, "Serving %s on %s\n", wsLabel, addr)
				}
			}

			actionCfg := new(action.Configuration)
			logFunc := func(format string, v ...interface{}) {
				if !helmDebug {
					return
				}
				fmt.Fprintf(errOut, "[helm] "+format+"\n", v...)
			}
			if err := actionCfg.Init(settings.RESTClientGetter(), resolvedNamespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
				return fmt.Errorf("init helm action config: %w", err)
			}

			shouldPreview := dryRun || (!autoApprove && !keepHistory)
			if dryRun || !autoApprove {
				manifest, reason := deploy.FetchLatestReleaseManifest(actionCfg, release)
				if strings.TrimSpace(manifest) == "" {
					fmt.Fprintf(errOut, "Plan: 0 to add, 0 to change, 0 to replace, 0 to destroy.\n")
					fmt.Fprintf(errOut, "Destroy preview unavailable: %s\n", reason)
				} else if shouldPreview {
					var resources []deploy.PlanChange
					var listErr error
					if kc, ok := actionCfg.KubeClient.(*helmkube.Client); ok && kc != nil {
						resources, listErr = deploy.ListManifestResourcesWithHelmKube(kc, manifest)
					} else {
						resources, listErr = deploy.ListManifestResources(manifest)
					}
					if listErr == nil {
						fmt.Fprintf(errOut, "Plan: 0 to add, 0 to change, 0 to replace, %d to destroy.\n", len(resources))
						limit := 12
						if len(resources) < limit {
							limit = len(resources)
						}
						for _, r := range resources[:limit] {
							nsLabel := r.Namespace
							if nsLabel == "" {
								nsLabel = "-"
							}
							if r.IsHook {
								hookLabel := strings.TrimSpace(r.Hook)
								if hookLabel == "" {
									hookLabel = "hook"
								}
								fmt.Fprintf(errOut, "  - %s/%s (ns: %s, %s)\n", r.Kind, r.Name, nsLabel, hookLabel)
							} else {
								fmt.Fprintf(errOut, "  - %s/%s (ns: %s)\n", r.Kind, r.Name, nsLabel)
							}
						}
						if len(resources) > limit {
							fmt.Fprintf(errOut, "  (and %d more)\n", len(resources)-limit)
						}
					}
				}
			}
			dec, err := approvalMode(cmd, autoApprove, nonInteractive)
			if err != nil {
				return err
			}
			if !dryRun {
				if err := confirmAction(cmd.Context(), cmd.InOrStdin(), errOut, dec, fmt.Sprintf("Type %q to confirm destroy:", release), confirmModeExact, release); err != nil {
					return err
				}
			}

			historyBreadcrumbs, lastSuccessful, err = deploy.ReleaseHistoryBreadcrumbs(actionCfg, release, historyBreadcrumbLimit)
			if err != nil {
				fmt.Fprintf(errOut, "Warning: unable to load release history for %s: %v\n", release, err)
			}
			historyChart := ""
			historyVersion := ""
			if len(historyBreadcrumbs) > 0 {
				historyChart = historyBreadcrumbs[0].Chart
				historyVersion = historyBreadcrumbs[0].Version
			}
			actionHeadline = deploy.DescribeDeployAction(deploy.ActionDescriptor{
				Release:   release,
				Chart:     historyChart,
				Version:   historyVersion,
				Namespace: resolvedNamespace,
				Destroy:   true,
			})
			meta.Chart = historyChart
			meta.ChartVersion = historyVersion
			applyConsoleMeta()

			defer func() {
				summary := deploy.SummaryPayload{
					Release:   release,
					Namespace: resolvedNamespace,
					Status:    "destroyed",
				}
				if runErr != nil {
					summary.Status = "failed"
					summary.Error = runErr.Error()
				}
				if keepHistory {
					summary.Notes = "Release history retained"
				}
				historyCopy := deploy.CloneBreadcrumbs(historyBreadcrumbs)
				lastSuccessCopy := deploy.CloneBreadcrumbPointer(lastSuccessful)
				if len(historyCopy) > 0 {
					if summary.Chart == "" {
						summary.Chart = historyCopy[0].Chart
					}
					if summary.Version == "" {
						summary.Version = historyCopy[0].Version
					}
				}
				if actionHeadline == "" {
					actionHeadline = deploy.DescribeDeployAction(deploy.ActionDescriptor{
						Release:   release,
						Chart:     summary.Chart,
						Version:   summary.Version,
						Namespace: resolvedNamespace,
						Destroy:   true,
					})
				}
				summary.Action = actionHeadline
				summary.History = historyCopy
				summary.LastSuccessful = lastSuccessCopy
				summary.PhaseDurations = formatPhaseDurations(timerObserver.snapshot())
				if stream != nil {
					stream.EmitSummary(summary)
				}
			}()

			var statusUpdaters []deploy.StatusUpdateFunc
			if console != nil {
				statusUpdaters = append(statusUpdaters, func(rows []deploy.ResourceStatus) {
					if console != nil {
						console.UpdateResources(rows)
					}
				})
			}
			if stream != nil && stream.HasObservers() {
				statusUpdaters = append(statusUpdaters, func(rows []deploy.ResourceStatus) {
					if stream != nil {
						stream.UpdateResources(rows)
					}
				})
				cancelFeed, feedErr := streamReleaseFeed(ctx, kubeClient, release, resolvedNamespace, currentLogLevel, stream)
				if feedErr != nil {
					return feedErr
				}
				stopLogFeed = cancelFeed
				stream.EmitEvent("info", fmt.Sprintf("Watching Kubernetes events for release %s in namespace %s", release, resolvedNamespace))
			}
			if len(statusUpdaters) > 0 {
				trackerCtx, cancel := context.WithCancel(ctx)
				multiUpdate := func(rows []deploy.ResourceStatus) {
					for _, fn := range statusUpdaters {
						if fn != nil {
							fn(rows)
						}
					}
				}
				tracker := deploy.NewResourceTracker(kubeClient, resolvedNamespace, release, "", multiUpdate)
				go tracker.Run(trackerCtx)
				cancelTrack = cancel
			}

			progressObservers := []deploy.ProgressObserver{timerObserver}
			if console != nil {
				progressObservers = append(progressObservers, console)
			}
			if stream != nil {
				progressObservers = append(progressObservers, stream)
			}
			phaseStarted := func(name string) {
				for _, obs := range progressObservers {
					if obs != nil {
						obs.PhaseStarted(name)
					}
				}
			}
			phaseCompleted := func(name, status, message string) {
				for _, obs := range progressObservers {
					if obs != nil {
						obs.PhaseCompleted(name, status, message)
					}
				}
			}
			emitEvent := func(level, message string) {
				msg := strings.TrimSpace(message)
				if msg == "" {
					return
				}
				for _, obs := range progressObservers {
					if obs != nil {
						obs.EmitEvent(level, msg)
					}
				}
			}

			uninstall := action.NewUninstall(actionCfg)
			uninstall.Timeout = timeout
			uninstall.Wait = wait
			uninstall.KeepHistory = keepHistory
			uninstall.DryRun = dryRun
			uninstall.DisableHooks = disableHooks
			if force {
				uninstall.IgnoreNotFound = true
				uninstall.DeletionPropagation = string(metav1.DeletePropagationForeground)
			}

			phaseStarted("destroy")
			emitEvent("info", fmt.Sprintf("Destroying release %s in namespace %s", release, resolvedNamespace))
			resp, err := uninstall.Run(release)
			if err != nil {
				phaseCompleted("destroy", "failed", err.Error())
				runErr = fmt.Errorf("helm uninstall: %w", err)
				return runErr
			}

			if stopSpinner != nil {
				stopSpinner(true)
				stopSpinner = nil
			}
			fmt.Fprintf(out, "Release %s destroyed (resources removed)\n", release)
			if resp != nil && resp.Info != "" {
				fmt.Fprintf(out, "%s\n", resp.Info)
			}
			if keepHistory {
				fmt.Fprintln(out, "History retained (resources removed)")
			}
			phaseCompleted("destroy", "succeeded", "Release destroyed")
			if line := renderPhaseDurationsLine(formatPhaseDurations(timerObserver.snapshot())); line != "" {
				fmt.Fprintf(errOut, "Destroy duration: %s\n", line)
			}
			telemetrySummary := telemetry.Summary{
				Total:  time.Since(startedAt),
				Phases: timerObserver.snapshot(),
			}
			if kubeClient != nil && kubeClient.APIStats != nil {
				metrics := kubeClient.APIStats.Snapshot()
				telemetrySummary.KubeRequests = metrics.Count
				telemetrySummary.KubeAvg = metrics.Avg()
				telemetrySummary.KubeMax = metrics.Max
			}
			if line := telemetrySummary.Line(); line != "" {
				fmt.Fprintln(errOut, line)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&release, "release", "", "Helm release name")
	cmd.Flags().BoolVar(&wait, "wait", false, "Wait for resources to be deleted")
	cmd.Flags().BoolVar(&keepHistory, "keep-history", false, "Retain release history (equivalent to helm uninstall --keep-history)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "Simulate destroy without removing resources")
	cmd.Flags().BoolVar(&autoApprove, "auto-approve", false, "Skip interactive confirmation prompts")
	_ = cmd.Flags().MarkHidden("auto-approve")
	cmd.Flags().BoolVar(&autoApprove, "yes", false, "Alias for --auto-approve")
	cmd.Flags().BoolVar(&nonInteractive, "non-interactive", false, "Fail instead of prompting (requires --yes)")
	cmd.Flags().DurationVar(&timeout, "timeout", timeout, "How long to wait for resource deletions")
	cmd.Flags().StringVar(&uiAddr, "ui", "", "Serve the destroy viewer at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	cmd.Flags().StringVar(&wsListenAddr, "ws-listen", "", "Serve the destroy event stream over WebSocket (e.g. :9087)")
	cmd.Flags().BoolVar(&force, "force", false, "Force uninstall even if Kubernetes resources are in a bad state")
	cmd.Flags().BoolVar(&disableHooks, "disable-hooks", false, "Disable Helm hooks while destroying the release")
	// --console-wide/--console-details removed.
	cmd.Flags().BoolVarP(&verbose, "verbose", "v", false, "Enable verbose logging (equivalent to --log-level=debug)")
	cmd.Flags().StringVar(&capturePath, "capture", "", "Capture destroy events/logs to a SQLite database at this path")
	if flag := cmd.Flags().Lookup("capture"); flag != nil {
		flag.NoOptDefVal = "__auto__"
	}
	cmd.Flags().StringArrayVar(&captureTags, "capture-tag", nil, "Tag the capture session (KEY=VALUE). Repeatable.")
	_ = cmd.MarkFlagRequired("release")

	if ownNamespaceFlag {
		cmd.Flags().StringVarP(namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	}
	label := strings.TrimSpace(cfg.HelpLabel)
	if label == "" {
		label = fmt.Sprintf("%s Flags", strings.TrimSpace(cfg.Use))
	}
	decorateCommandHelp(cmd, label)
	return cmd
}

func renderManifestForTracking(ctx context.Context, settings *cli.EnvSettings, namespace, chart, version, release string, valuesFiles, setValues, setStringValues, setFileValues []string, secrets *deploy.SecretOptions) (string, error) {
	if chart == "" || release == "" {
		return "", fmt.Errorf("chart and release are required")
	}
	templateCfg := new(action.Configuration)
	if err := templateCfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
		return "", fmt.Errorf("init template config: %w", err)
	}
	result, err := deploy.RenderTemplate(ctx, templateCfg, settings, deploy.TemplateOptions{
		Chart:           chart,
		Version:         version,
		ReleaseName:     release,
		Namespace:       namespace,
		ValuesFiles:     valuesFiles,
		SetValues:       setValues,
		SetStringValues: setStringValues,
		SetFileValues:   setFileValues,
		Secrets:         secrets,
		IncludeCRDs:     true,
		UseCluster:      true,
	})
	if err != nil {
		return "", err
	}
	return result.Manifest, nil
}

func ensureNamespace(ctx context.Context, client kubernetes.Interface, namespace string) error {
	if namespace == "" || client == nil {
		return nil
	}
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return fmt.Errorf("get namespace %s: %w", namespace, err)
	}
	_, err = client.CoreV1().Namespaces().Create(ctx, &corev1.Namespace{ObjectMeta: metav1.ObjectMeta{Name: namespace}}, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create namespace %s: %w", namespace, err)
	}
	return nil
}

func namespaceExists(ctx context.Context, client kubernetes.Interface, namespace string) (bool, error) {
	if namespace == "" || client == nil {
		return true, nil
	}
	_, err := client.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err == nil {
		return true, nil
	}
	if apierrors.IsNotFound(err) {
		return false, nil
	}
	return false, fmt.Errorf("get namespace %s: %w", namespace, err)
}

func watchRelease(parentCtx context.Context, cmd *cobra.Command, kubeClient *kube.Client, releaseName, namespace string, duration time.Duration, logLevel string, observer tailer.LogObserver) error {
	if kubeClient == nil || releaseName == "" {
		return nil
	}
	if namespace == "" {
		namespace = kubeClient.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}
	watchCtx, cancel := context.WithTimeout(parentCtx, duration)
	defer cancel()

	opts := config.NewOptions()
	opts.PodQuery = ".*"
	opts.Namespaces = []string{namespace}
	opts.LabelSelector = fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName)
	opts.Follow = true
	opts.TailLines = 20
	opts.Events = true
	opts.EventsOnly = false
	if err := opts.Validate(); err != nil {
		return fmt.Errorf("validate watch options: %w", err)
	}
	logger, err := buildLogger(logLevel)
	if err != nil {
		return fmt.Errorf("build logger: %w", err)
	}
	tailerOpts := []tailer.Option{tailer.WithOutput(cmd.OutOrStdout())}
	if observer != nil {
		tailerOpts = append(tailerOpts, tailer.WithLogObserver(observer))
	}
	t, err := tailer.New(kubeClient.Clientset, opts, logger, tailerOpts...)
	if err != nil {
		return fmt.Errorf("init tailer: %w", err)
	}
	if err := t.Run(watchCtx); err != nil && !errors.Is(err, context.Canceled) {
		return fmt.Errorf("watch release logs: %w", err)
	}
	return nil
}

func streamReleaseFeed(parentCtx context.Context, kubeClient *kube.Client, releaseName, namespace, logLevel string, observer tailer.LogObserver) (context.CancelFunc, error) {
	if kubeClient == nil || observer == nil || strings.TrimSpace(releaseName) == "" {
		return nil, nil
	}
	if namespace == "" {
		namespace = kubeClient.Namespace
	}
	if namespace == "" {
		namespace = "default"
	}
	ctx, cancel := context.WithCancel(parentCtx)
	opts := config.NewOptions()
	opts.PodQuery = ".*"
	opts.Namespaces = []string{namespace}
	opts.LabelSelector = fmt.Sprintf("app.kubernetes.io/instance=%s", releaseName)
	opts.Follow = true
	opts.TailLines = 0
	opts.Events = true
	opts.EventsOnly = true
	if err := opts.Validate(); err != nil {
		cancel()
		return nil, fmt.Errorf("validate deploy event stream options: %w", err)
	}
	logger, err := buildLogger(logLevel)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("build logger: %w", err)
	}
	t, err := tailer.New(kubeClient.Clientset, opts, logger, tailer.WithOutput(io.Discard), tailer.WithLogObserver(observer))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("init deploy event stream: %w", err)
	}
	go func() {
		defer cancel()
		if err := t.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			logger.Error(err, "deploy event stream finished")
		}
	}()
	return cancel, nil
}

func effectiveLogLevel(logLevel *string) string {
	if logLevel == nil {
		return "info"
	}
	level := strings.TrimSpace(*logLevel)
	if level == "" {
		return "info"
	}
	return level
}

func shouldLogAtLevel(level string, threshold zapcore.Level) bool {
	parsed, err := zapcore.ParseLevel(strings.ToLower(strings.TrimSpace(level)))
	if err != nil {
		parsed = zapcore.InfoLevel
	}
	return parsed <= threshold
}

type phaseTimerObserver struct {
	mu        sync.Mutex
	starts    map[string]time.Time
	durations map[string]time.Duration
}

func newPhaseTimerObserver() *phaseTimerObserver {
	return &phaseTimerObserver{
		starts:    make(map[string]time.Time),
		durations: make(map[string]time.Duration),
	}
}

func (o *phaseTimerObserver) PhaseStarted(name string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	o.mu.Lock()
	o.starts[name] = time.Now()
	o.mu.Unlock()
}

func (o *phaseTimerObserver) PhaseCompleted(name, _ string, _ string) {
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	now := time.Now()
	o.mu.Lock()
	start, ok := o.starts[name]
	if ok && !start.IsZero() {
		o.durations[name] = now.Sub(start)
	}
	o.mu.Unlock()
}

func (o *phaseTimerObserver) EmitEvent(string, string) {}

func (o *phaseTimerObserver) SetDiff(string) {}

func (o *phaseTimerObserver) snapshot() map[string]time.Duration {
	o.mu.Lock()
	defer o.mu.Unlock()
	out := make(map[string]time.Duration, len(o.durations))
	for k, v := range o.durations {
		out[k] = v
	}
	return out
}

func formatPhaseDurations(durations map[string]time.Duration) map[string]string {
	if len(durations) == 0 {
		return nil
	}
	out := make(map[string]string, len(durations))
	for k, v := range durations {
		if v < 0 {
			v = 0
		}
		out[k] = v.Truncate(100 * time.Millisecond).String()
	}
	return out
}

func renderPhaseDurationsLine(durations map[string]string) string {
	if len(durations) == 0 {
		return ""
	}
	type kv struct {
		name string
		val  string
	}
	list := make([]kv, 0, len(durations))
	for name, value := range durations {
		list = append(list, kv{name: name, val: value})
	}
	sort.Slice(list, func(i, j int) bool {
		return list[i].name < list[j].name
	})
	parts := make([]string, 0, len(list))
	for _, item := range list {
		parts = append(parts, fmt.Sprintf("%s=%s", item.name, item.val))
	}
	return strings.Join(parts, ", ")
}

func buildDestroySuggestion(release, namespace string, flags *pflag.FlagSet) string {
	parts := []string{"ktl", "deploy", "destroy"}
	if strings.TrimSpace(release) != "" {
		parts = append(parts, "--release", release)
	}
	if strings.TrimSpace(namespace) != "" {
		parts = append(parts, "--namespace", namespace)
	}
	addFlag := func(name string) {
		if flags == nil {
			return
		}
		flag := flags.Lookup(name)
		if flag == nil {
			return
		}
		if strings.EqualFold(flag.Value.String(), "true") {
			parts = append(parts, "--"+name)
		}
	}
	addFlag("wait")
	addFlag("keep-history")
	addFlag("dry-run")
	addFlag("force")
	addFlag("disable-hooks")
	return strings.Join(parts, " ")
}
