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
)

type helmExecutor struct {
	kubeconfig  *string
	kubeContext *string

	out    io.Writer
	errOut io.Writer

	clients clientCache
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

	_, err := e.clients.get(ctx, kubeconfigPath, kubeCtx)
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

	actionCfg := new(action.Configuration)
	helmDebug := strings.TrimSpace(os.Getenv("KTL_STACK_HELM_DEBUG")) == "1"
	logFunc := func(format string, v ...interface{}) {
		if !helmDebug {
			return
		}
		fmt.Fprintf(e.errOut, "[helm] "+format+"\n", v...)
	}
	if err := actionCfg.Init(settings.RESTClientGetter(), node.Namespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("init helm action config: %w", err))
	}

	obs := &prefixedObserver{prefix: node.ID, out: e.errOut}
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
		setPairs := flattenSet(node.Set)
		_, err := deploy.InstallOrUpgrade(ctx, actionCfg, settings, deploy.InstallOptions{
			Chart:             node.Chart,
			Version:           node.ChartVersion,
			ReleaseName:       node.Name,
			Namespace:         node.Namespace,
			ValuesFiles:       node.Values,
			SetValues:         setPairs,
			Timeout:           timeout,
			Wait:              wait,
			Atomic:            atomic,
			CreateNamespace:   false,
			DryRun:            false,
			Diff:              false,
			UpgradeOnly:       false,
			ProgressObservers: []deploy.ProgressObserver{obs},
		})
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		return nil
	case "delete":
		timeout := 5 * time.Minute
		if node.Delete.Timeout != nil {
			timeout = *node.Delete.Timeout
		}
		uninstall := action.NewUninstall(actionCfg)
		uninstall.Timeout = timeout
		_, err := uninstall.Run(node.Name)
		if err != nil {
			return wrapNodeErr(node.ResolvedRelease, err)
		}
		fmt.Fprintf(e.errOut, "[%s] deleted\n", node.ID)
		return nil
	default:
		return wrapNodeErr(node.ResolvedRelease, fmt.Errorf("unknown command %q", command))
	}
}

type prefixedObserver struct {
	prefix string
	out    io.Writer
}

func (p *prefixedObserver) PhaseStarted(name string) {
	fmt.Fprintf(p.out, "[%s] %s started\n", p.prefix, name)
}

func (p *prefixedObserver) PhaseCompleted(name, status, message string) {
	if strings.TrimSpace(message) == "" {
		fmt.Fprintf(p.out, "[%s] %s %s\n", p.prefix, name, status)
		return
	}
	fmt.Fprintf(p.out, "[%s] %s %s: %s\n", p.prefix, name, status, message)
}

func (p *prefixedObserver) EmitEvent(level, message string) {
	fmt.Fprintf(p.out, "[%s] %s: %s\n", p.prefix, level, message)
}

func (p *prefixedObserver) SetDiff(diff string) {
	if strings.TrimSpace(diff) == "" {
		return
	}
	fmt.Fprintf(p.out, "[%s] diff:\n%s\n", p.prefix, diff)
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
