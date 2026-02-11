// File: internal/stack/helm_exec.go
// Brief: Adapter to reuse internal/deploy for stack execution.

package stack

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/rest"
)

type helmExecutor struct {
	kubeconfig  *string
	kubeContext *string
	run         *runState

	out    io.Writer
	errOut io.Writer

	dryRun     bool
	diff       bool
	cacheApply bool

	helmLogs bool

	kubeQPS   float32
	kubeBurst int

	clients clientCache

	secrets *deploy.SecretOptions
}

type NodeExecutor interface {
	RunNode(ctx context.Context, node *runNode, command string) error
}

type clientCache struct {
	mu sync.Mutex
	m  map[string]*kube.Client
}

func (c *clientCache) get(ctx context.Context, kubeconfigPath, kubeContext string) (*kube.Client, error) {
	key := kubeconfigPath + "\n" + kubeContext
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		c.m = map[string]*kube.Client{}
	}
	if v, ok := c.m[key]; ok {
		return v, nil
	}
	cli, err := kube.New(ctx, kubeconfigPath, kubeContext)
	if err != nil {
		return nil, err
	}
	c.m[key] = cli
	return cli, nil
}

func (e *helmExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	kubeconfigPath := ""
	if node.Cluster.Kubeconfig != "" {
		kubeconfigPath = expandTilde(node.Cluster.Kubeconfig)
	} else if e.kubeconfig != nil {
		kubeconfigPath = strings.TrimSpace(*e.kubeconfig)
	}
	kubeCtx := ""
	if node.Cluster.Context != "" {
		kubeCtx = node.Cluster.Context
	} else if e.kubeContext != nil {
		kubeCtx = strings.TrimSpace(*e.kubeContext)
	}

	kubeClient, err := e.clients.get(ctx, kubeconfigPath, kubeCtx)
	if err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	settings := cli.New()
	if kubeconfigPath != "" {
		settings.KubeConfig = kubeconfigPath
	}
	if kubeCtx != "" {
		settings.KubeContext = kubeCtx
	}
	if node.Namespace != "" {
		settings.SetNamespace(node.Namespace)
	}

	helmDebug := strings.TrimSpace(os.Getenv("KTL_STACK_HELM_DEBUG")) == "1"
	helmLogEnabled := e.helmLogs || helmDebug
	settings.Debug = helmLogEnabled

	actionCfg := new(action.Configuration)
	getter := settings.RESTClientGetter()
	if cfgFlags, ok := getter.(*genericclioptions.ConfigFlags); ok && cfgFlags != nil {
		prev := cfgFlags.WrapConfigFn
		cfgFlags.WrapConfigFn = func(cfg *rest.Config) *rest.Config {
			if prev != nil {
				cfg = prev(cfg)
			}
			if cfg == nil {
				return cfg
			}
			if e.kubeQPS > 0 {
				cfg.QPS = e.kubeQPS
			}
			if e.kubeBurst > 0 {
				cfg.Burst = e.kubeBurst
			}
			return cfg
		}
	}
	logFunc := func(format string, v ...interface{}) {
		if !helmLogEnabled {
			return
		}
		msg := strings.TrimSpace(fmt.Sprintf(format, v...))
		if msg == "" {
			return
		}
		if e.helmLogs && e.run != nil {
			e.run.AppendEvent(node.ID, HelmLog, node.Attempt, msg, map[string]any{"source": "helm"}, nil)
			return
		}
		fmt.Fprintf(e.errOut, "[helm] %s\n", msg)
	}
	if err := actionCfg.Init(getter, node.Namespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("init helm action config: %w", err))
	}

	obs := &stackEventObserver{run: e.run, node: node}
	switch command {
	case "apply":
		timeout := 5 * time.Minute
		if node.Apply.Timeout != nil {
			timeout = *node.Apply.Timeout
		}
		wait := true
		if node.Apply.Wait != nil {
			wait = *node.Apply.Wait
		}
		atomic := true
		if node.Apply.Atomic != nil {
			atomic = *node.Apply.Atomic
		}
		createNamespace := false
		if node.Apply.CreateNamespace != nil {
			createNamespace = *node.Apply.CreateNamespace
		}

		if node.resume != nil && node.resume.VerifyOnly {
			obs.PhaseCompleted(deploy.PhaseRender, "skipped", "Resume verify-only skipped")
			obs.PhaseCompleted(deploy.PhaseDiff, "skipped", "Resume verify-only skipped")
			obs.PhaseCompleted(deploy.PhaseUpgrade, "skipped", "Resume verify-only skipped")
			obs.PhaseCompleted(deploy.PhaseInstall, "skipped", "Resume verify-only skipped")
			obs.PhaseCompleted(deploy.PhaseWait, "skipped", "Resume verify-only skipped")
			obs.PhaseCompleted(deploy.PhasePostHooks, "skipped", "Resume verify-only skipped")
			manifest := ""
			getAction := action.NewGet(actionCfg)
			if rel, err := getAction.Run(node.Name); err == nil && rel != nil {
				manifest = rel.Manifest
			}
			clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx)
			if err := maybeVerify(ctx, e.run, clusterKey, kubeClient, obs, node, manifest, node.Verify, e.dryRun); err != nil {
				return wrapNodeErr(node.ResolvedRelease, err)
			}
			return nil
		}

		if e.cacheApply && !e.dryRun {
			clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx)
			key, keyErr := applyCacheKeyForNode(clusterKey, node, command)
			if keyErr == nil && e.run != nil && e.run.store != nil {
				dec, decErr := CheckApplyCache(ctx, e.run.store, key, e.run.RunID,
					func(ctx context.Context) (string, bool, error) {
						res, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
							Chart:       node.Chart,
							Version:     node.ChartVersion,
							ReleaseName: node.Name,
							Namespace:   node.Namespace,
							ValuesFiles: node.Values,
							SetValues:   flattenSet(node.Set),
							UseCluster:  true,
							Secrets:     e.secrets,
						})
						if err != nil {
							return "", false, err
						}
						d, hasHooks, err := deploy.DigestNormalizedManifest(res.Manifest)
						return d, hasHooks, err
					},
					func(ctx context.Context) (string, bool, error) {
						getAction := action.NewGet(actionCfg)
						rel, err := getAction.Run(node.Name)
						if err != nil || rel == nil {
							return "", false, nil
						}
						d, _, err := deploy.DigestNormalizedManifest(rel.Manifest)
						return d, true, err
					},
				)
				if decErr != nil {
					return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("apply cache check: %w", decErr))
				}
				if dec.Skip {
					obs.PhaseCompleted(deploy.PhaseRender, "skipped", "Apply cache: manifest digest match")
					obs.PhaseCompleted(deploy.PhaseDiff, "skipped", "Apply cache: manifest digest match")
					obs.PhaseCompleted(deploy.PhaseUpgrade, "skipped", "Apply cache: manifest digest match")
					obs.PhaseCompleted(deploy.PhaseInstall, "skipped", "Apply cache: manifest digest match")
					if wait {
						obs.PhaseStarted(deploy.PhaseWait)
						if node.resume != nil && node.resume.WaitOnly {
							obs.PhaseCompleted(deploy.PhaseWait, "skipped", "Resume wait-only skipped")
						} else {
							if err := waitForReleaseReady(ctx, e.run, obs, kubeClient, actionCfg, node, timeout); err != nil {
								return wrapNodeErr(node.ResolvedRelease, err)
							}
						}
					} else {
						obs.PhaseCompleted(deploy.PhaseWait, "skipped", "Helm --wait disabled")
					}
					getAction := action.NewGet(actionCfg)
					manifest := ""
					if rel, err := getAction.Run(node.Name); err == nil && rel != nil {
						manifest = rel.Manifest
					}
					clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx)
					if err := maybeVerify(ctx, e.run, clusterKey, kubeClient, obs, node, manifest, node.Verify, e.dryRun); err != nil {
						return wrapNodeErr(node.ResolvedRelease, err)
					}
					obs.PhaseCompleted(deploy.PhasePostHooks, "skipped", "Apply cache: no-op")
					return nil
				}
			}
		}

		if node.resume != nil && node.resume.WaitOnly {
			obs.PhaseStarted(deploy.PhaseWait)
			if e.dryRun || !wait {
				obs.PhaseCompleted(deploy.PhaseWait, "skipped", "Resume wait-only skipped")
				obs.PhaseStarted(deploy.PhasePostHooks)
				obs.PhaseCompleted(deploy.PhasePostHooks, "succeeded", "Helm post-upgrade hooks completed")
				return nil
			}
			manifest := ""
			getAction := action.NewGet(actionCfg)
			if rel, err := getAction.Run(node.Name); err == nil && rel != nil {
				manifest = rel.Manifest
			}

			tracker := deploy.NewResourceTracker(kubeClient, node.Namespace, node.Name, manifest, nil)
			deadline := time.Now().Add(timeout)
			var lastRows []deploy.ResourceStatus
			for {
				if ctx.Err() != nil {
					return wrapNodeErr(node.ResolvedRelease, ctx.Err())
				}
				rows := tracker.Snapshot(ctx)
				lastRows = rows
				if allReleaseResourcesReady(rows) {
					obs.PhaseCompleted(deploy.PhaseWait, "succeeded", "Release resources ready")
					clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx)
					if err := maybeVerify(ctx, e.run, clusterKey, kubeClient, obs, node, manifest, node.Verify, e.dryRun); err != nil {
						return wrapNodeErr(node.ResolvedRelease, err)
					}
					obs.PhaseStarted(deploy.PhasePostHooks)
					obs.PhaseCompleted(deploy.PhasePostHooks, "succeeded", "Helm post-upgrade hooks completed")
					return nil
				}
				if time.Now().After(deadline) {
					blockers := deploy.TopBlockers(lastRows, 6)
					if len(blockers) > 0 && e.run != nil {
						e.run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, "TOP BLOCKERS", map[string]any{"kind": "top-blockers"})
						for _, b := range blockers {
							reason := strings.TrimSpace(b.Reason)
							if reason == "" {
								reason = "-"
							}
							msg := strings.TrimSpace(b.Message)
							if msg == "" {
								msg = "-"
							}
							e.run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, fmt.Sprintf("%s/%s\t%s\t%s\t%s", b.Kind, b.Name, b.Status, reason, msg), map[string]any{
								"kind":      "top-blocker",
								"resource":  fmt.Sprintf("%s/%s", b.Kind, b.Name),
								"status":    b.Status,
								"reason":    reason,
								"message":   msg,
								"namespace": b.Namespace,
							})
						}
					}
					err := fmt.Errorf("resume wait: timeout after %s", timeout.String())
					obs.PhaseCompleted(deploy.PhaseWait, "failed", err.Error())
					return wrapNodeErr(node.ResolvedRelease, err)
				}
				select {
				case <-ctx.Done():
					return wrapNodeErr(node.ResolvedRelease, ctx.Err())
				case <-time.After(2 * time.Second):
				}
			}
		}

		var (
			trackCtx    context.Context
			cancelTrack context.CancelFunc
			lastRowsMu  sync.Mutex
			lastRows    []deploy.ResourceStatus
		)
		if wait && !e.dryRun && kubeClient != nil {
			trackCtx, cancelTrack = context.WithCancel(ctx)
			tracker := deploy.NewResourceTracker(kubeClient, node.Namespace, node.Name, "", func(rows []deploy.ResourceStatus) {
				lastRowsMu.Lock()
				lastRows = append(lastRows[:0], rows...)
				lastRowsMu.Unlock()
			})
			go tracker.Run(trackCtx)
			defer cancelTrack()
		}

		setPairs := flattenSet(node.Set)
		diffEnabled := e.diff
		if node.resume != nil && node.resume.SkipDiff {
			diffEnabled = false
		}
		res, err := deploy.InstallOrUpgrade(ctx, actionCfg, settings, deploy.InstallOptions{
			Chart:             node.Chart,
			Version:           node.ChartVersion,
			ReleaseName:       node.Name,
			Namespace:         node.Namespace,
			ValuesFiles:       node.Values,
			SetValues:         setPairs,
			Secrets:           e.secrets,
			Timeout:           timeout,
			Wait:              wait,
			Atomic:            atomic,
			CreateNamespace:   createNamespace,
			DryRun:            e.dryRun,
			Diff:              diffEnabled,
			UpgradeOnly:       false,
			ProgressObservers: []deploy.ProgressObserver{obs},
		})
		if err != nil {
			if wait && !e.dryRun {
				lastRowsMu.Lock()
				snap := append([]deploy.ResourceStatus(nil), lastRows...)
				lastRowsMu.Unlock()
				blockers := deploy.TopBlockers(snap, 6)
				if len(blockers) > 0 {
					if e.run != nil {
						e.run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, "TOP BLOCKERS", map[string]any{"kind": "top-blockers"})
					}
					for _, b := range blockers {
						reason := strings.TrimSpace(b.Reason)
						if reason == "" {
							reason = "-"
						}
						msg := strings.TrimSpace(b.Message)
						if msg == "" {
							msg = "-"
						}
						if e.run != nil {
							e.run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, fmt.Sprintf("%s/%s\t%s\t%s\t%s", b.Kind, b.Name, b.Status, reason, msg), map[string]any{
								"kind":      "top-blocker",
								"resource":  fmt.Sprintf("%s/%s", b.Kind, b.Name),
								"status":    b.Status,
								"reason":    reason,
								"message":   msg,
								"namespace": b.Namespace,
							})
						}
					}
				}
			}
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		manifest := ""
		if res != nil && res.Release != nil {
			manifest = res.Release.Manifest
		}
		if strings.TrimSpace(manifest) == "" {
			getAction := action.NewGet(actionCfg)
			if rel, err := getAction.Run(node.Name); err == nil && rel != nil {
				manifest = rel.Manifest
			}
		}
		clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx)
		if err := maybeVerify(ctx, e.run, clusterKey, kubeClient, obs, node, manifest, node.Verify, e.dryRun); err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		if e.cacheApply && e.run != nil && e.run.store != nil && !e.dryRun {
			if clusterKey := stackClusterCacheKey(node.Cluster.Name, kubeconfigPath, kubeCtx); clusterKey != "" {
				key, _ := applyCacheKeyForNode(clusterKey, node, command)
				getAction := action.NewGet(actionCfg)
				rel, err := getAction.Run(node.Name)
				if err == nil && rel != nil {
					d, hasHooksInManifest, dErr := deploy.DigestNormalizedManifest(rel.Manifest)
					if dErr == nil {
						hasHooks := hasHooksInManifest || len(rel.Hooks) > 0
						_ = e.run.store.UpsertApplyCache(ctx, key, d, hasHooks, e.run.RunID, time.Now().UTC().UnixNano())
					}
				}
			}
		}
		return nil
	case "delete":
		if e.run != nil {
			e.run.AppendEvent(node.ID, PhaseStarted, node.Attempt, "destroy", map[string]any{"phase": "destroy"}, nil)
		}
		timeout := 5 * time.Minute
		if node.Delete.Timeout != nil {
			timeout = *node.Delete.Timeout
		}
		uninstall := action.NewUninstall(actionCfg)
		uninstall.Timeout = timeout
		_, err := uninstall.Run(node.Name)
		if err != nil {
			if e.run != nil {
				e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "destroy failure", map[string]any{"phase": "destroy", "status": "failure"}, nil)
			}
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		if e.run != nil {
			e.run.AppendEvent(node.ID, PhaseCompleted, node.Attempt, "destroy success", map[string]any{"phase": "destroy", "status": "success"}, nil)
		}
		return nil
	default:
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("unknown command %q", command))
	}
}

func maybeVerify(ctx context.Context, run *runState, clusterKey string, kubeClient *kube.Client, obs *stackEventObserver, node *runNode, manifest string, v VerifyOptions, dryRun bool) error {
	if dryRun || !verifyEnabled(v) {
		return nil
	}
	timeout := verifyTimeout(v)
	tryCtx := ctx
	cancel := func() {}
	if timeout > 0 {
		tryCtx, cancel = context.WithTimeout(ctx, timeout)
	}
	defer cancel()

	if obs != nil {
		obs.PhaseStarted("verify")
	}
	sinceNS := int64(0)
	lastEventRVJSON := ""
	if run != nil && run.store != nil && strings.TrimSpace(clusterKey) != "" && node != nil {
		if entry, ok, _ := run.store.GetVerifyCache(ctx, VerifyCacheKey{ClusterKey: clusterKey, Namespace: node.Namespace, ReleaseName: node.Name}); ok {
			sinceNS = entry.LastOKAtNS
			lastEventRVJSON = entry.LastEventRVJSON
		}
	}
	res, err := verifyKubeRelease(tryCtx, kubeClient, node.Namespace, node.Name, manifest, v, sinceNS, lastEventRVJSON)
	if err != nil {
		if obs != nil {
			obs.PhaseCompleted("verify", "failed", err.Error())
		}
		if run != nil && run.store != nil && strings.TrimSpace(clusterKey) != "" && node != nil {
			_ = run.store.UpsertVerifyCache(ctx, VerifyCacheKey{ClusterKey: clusterKey, Namespace: node.Namespace, ReleaseName: node.Name}, time.Now().UTC().UnixNano(), 0, "failed", err.Error(), res.EventRVJSON, res.EvidenceJSON)
		}
		if verifyWarnOnly(v) {
			return nil
		}
		return err
	}
	if obs != nil {
		obs.PhaseCompleted("verify", "succeeded", strings.TrimSpace(res.Message))
	}
	if run != nil && run.store != nil && strings.TrimSpace(clusterKey) != "" && node != nil {
		now := time.Now().UTC().UnixNano()
		_ = run.store.UpsertVerifyCache(ctx, VerifyCacheKey{ClusterKey: clusterKey, Namespace: node.Namespace, ReleaseName: node.Name}, now, now, "succeeded", strings.TrimSpace(res.Message), res.EventRVJSON, res.EvidenceJSON)
	}
	return nil
}

func waitForReleaseReady(ctx context.Context, run *runState, obs *stackEventObserver, kubeClient *kube.Client, actionCfg *action.Configuration, node *runNode, timeout time.Duration) error {
	if kubeClient == nil || actionCfg == nil || node == nil {
		return nil
	}
	manifest := ""
	getAction := action.NewGet(actionCfg)
	if rel, err := getAction.Run(node.Name); err == nil && rel != nil {
		manifest = rel.Manifest
	}
	tracker := deploy.NewResourceTracker(kubeClient, node.Namespace, node.Name, manifest, nil)
	deadline := time.Now().Add(timeout)
	var lastRows []deploy.ResourceStatus
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		rows := tracker.Snapshot(ctx)
		lastRows = rows
		if allReleaseResourcesReady(rows) {
			obs.PhaseCompleted(deploy.PhaseWait, "succeeded", "Release resources ready")
			return nil
		}
		if time.Now().After(deadline) {
			blockers := deploy.TopBlockers(lastRows, 6)
			if len(blockers) > 0 && run != nil {
				run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, "TOP BLOCKERS", map[string]any{"kind": "top-blockers"})
				for _, b := range blockers {
					reason := strings.TrimSpace(b.Reason)
					if reason == "" {
						reason = "-"
					}
					msg := strings.TrimSpace(b.Message)
					if msg == "" {
						msg = "-"
					}
					run.EmitEphemeralEvent(node.ID, NodeLog, node.Attempt, fmt.Sprintf("%s/%s\t%s\t%s\t%s", b.Kind, b.Name, b.Status, reason, msg), map[string]any{
						"kind":      "top-blocker",
						"resource":  fmt.Sprintf("%s/%s", b.Kind, b.Name),
						"status":    b.Status,
						"reason":    reason,
						"message":   msg,
						"namespace": b.Namespace,
					})
				}
			}
			err := fmt.Errorf("wait: timeout after %s", timeout.String())
			obs.PhaseCompleted(deploy.PhaseWait, "failed", err.Error())
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func stackClusterCacheKey(clusterName, kubeconfigPath, kubeContext string) string {
	clusterName = strings.TrimSpace(clusterName)
	kubeconfigPath = strings.TrimSpace(kubeconfigPath)
	kubeContext = strings.TrimSpace(kubeContext)
	if clusterName == "" {
		clusterName = "default"
	}
	if kubeconfigPath == "" {
		kubeconfigPath = "default"
	}
	if kubeContext == "" {
		kubeContext = "default"
	}
	return clusterName + "\n" + kubeconfigPath + "\n" + kubeContext
}

type stackEventObserver struct {
	run  *runState
	node *runNode
}

func (o *stackEventObserver) PhaseStarted(name string) {
	if o == nil || o.run == nil || o.node == nil {
		return
	}
	phase := strings.TrimSpace(name)
	o.run.AppendEvent(o.node.ID, PhaseStarted, o.node.Attempt, phase, map[string]any{"phase": phase}, nil)
}

func (o *stackEventObserver) PhaseCompleted(name, status, message string) {
	if o == nil || o.run == nil || o.node == nil {
		return
	}
	desc := strings.TrimSpace(name) + " " + strings.TrimSpace(status)
	if strings.TrimSpace(message) != "" {
		desc += ": " + strings.TrimSpace(message)
	}
	phase := strings.TrimSpace(name)
	st := strings.TrimSpace(status)
	o.run.AppendEvent(o.node.ID, PhaseCompleted, o.node.Attempt, desc, map[string]any{
		"phase":   phase,
		"status":  st,
		"message": strings.TrimSpace(message),
	}, nil)
}

func (o *stackEventObserver) EmitEvent(level, message string) {
	if o == nil || o.run == nil || o.node == nil {
		return
	}
	level = strings.TrimSpace(level)
	message = strings.TrimSpace(message)
	if level == "" {
		level = "info"
	}
	if message == "" {
		return
	}
	o.run.EmitEphemeralEvent(o.node.ID, NodeLog, o.node.Attempt, fmt.Sprintf("%s: %s", level, message), map[string]any{"level": level})
}

func (o *stackEventObserver) SetDiff(diff string) {
	if o == nil || o.run == nil || o.node == nil {
		return
	}
	diff = strings.TrimSpace(diff)
	if diff == "" {
		return
	}
	o.run.EmitEphemeralEvent(o.node.ID, NodeLog, o.node.Attempt, "diff:\n"+diff, map[string]any{"kind": "diff"})
}

func flattenSet(m map[string]string) []string {
	if len(m) == 0 {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, k := range keys {
		out = append(out, fmt.Sprintf("%s=%s", k, m[k]))
	}
	return out
}

func allReleaseResourcesReady(rows []deploy.ResourceStatus) bool {
	if len(rows) == 0 {
		return true
	}
	for _, rs := range rows {
		status := strings.ToLower(strings.TrimSpace(rs.Status))
		switch status {
		case "ready", "succeeded", "suspended":
			continue
		default:
			return false
		}
	}
	return true
}

func expandTilde(path string) string {
	p := strings.TrimSpace(path)
	if p == "" || p[0] != '~' {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	if strings.HasPrefix(p, "~/") {
		return filepath.Join(home, p[2:])
	}
	return p
}
