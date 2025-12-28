package stack

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/deploy"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	helmkube "helm.sh/helm/v3/pkg/kube"
)

type StackPlanBundleManifest struct {
	APIVersion         string `json:"apiVersion"`
	Kind               string `json:"kind"`
	CreatedAt          string `json:"createdAt,omitempty"`
	PlanHash           string `json:"planHash"`
	InputsBundleSha256 string `json:"inputsBundleSha256,omitempty"`
	StackName          string `json:"stackName,omitempty"`
	Profile            string `json:"profile,omitempty"`
}

type StackDiffSummary struct {
	APIVersion string                     `json:"apiVersion"`
	CreatedAt  string                     `json:"createdAt,omitempty"`
	PlanHash   string                     `json:"planHash"`
	Nodes      map[string]NodeDiffSummary `json:"nodes"`
}

type NodeDiffSummary struct {
	Add     int            `json:"add"`
	Change  int            `json:"change"`
	Replace int            `json:"replace"`
	Destroy int            `json:"destroy"`
	Risky   map[string]int `json:"risky,omitempty"`
	Error   string         `json:"error,omitempty"`
}

func BuildStackDiffSummary(ctx context.Context, p *Plan, defaultKubeconfig string, defaultKubeContext string, planHash string) (*StackDiffSummary, error) {
	if p == nil {
		return nil, fmt.Errorf("plan is nil")
	}
	out := &StackDiffSummary{
		APIVersion: "ktl.dev/stack-diff-summary/v1",
		PlanHash:   strings.TrimSpace(planHash),
		Nodes:      map[string]NodeDiffSummary{},
	}
	for _, n := range p.Nodes {
		if n == nil {
			continue
		}
		sum, err := diffSummaryForNode(ctx, n, defaultKubeconfig, defaultKubeContext)
		if err != nil {
			out.Nodes[n.ID] = NodeDiffSummary{Error: err.Error()}
			continue
		}
		out.Nodes[n.ID] = *sum
	}
	return out, nil
}

func diffSummaryForNode(ctx context.Context, node *ResolvedRelease, defaultKubeconfig string, defaultKubeContext string) (*NodeDiffSummary, error) {
	kubeconfigPath := strings.TrimSpace(expandTilde(node.Cluster.Kubeconfig))
	if kubeconfigPath == "" {
		kubeconfigPath = strings.TrimSpace(defaultKubeconfig)
	}
	kubeCtx := strings.TrimSpace(node.Cluster.Context)
	if kubeCtx == "" {
		kubeCtx = strings.TrimSpace(defaultKubeContext)
	}
	if kubeconfigPath == "" {
		return nil, fmt.Errorf("missing kubeconfig for %s", node.ID)
	}

	settings := cli.New()
	settings.KubeConfig = kubeconfigPath
	if kubeCtx != "" {
		settings.KubeContext = kubeCtx
	}
	if node.Namespace != "" {
		settings.SetNamespace(node.Namespace)
	}
	actionCfg := new(action.Configuration)
	if err := actionCfg.Init(settings.RESTClientGetter(), node.Namespace, os.Getenv("HELM_DRIVER"), func(string, ...interface{}) {}); err != nil {
		return nil, fmt.Errorf("init helm: %w", err)
	}

	var helmClient *helmkube.Client
	if kc, ok := actionCfg.KubeClient.(*helmkube.Client); ok && kc != nil {
		helmClient = kc
	}

	get := action.NewGet(actionCfg)
	prevManifest := ""
	if rel, err := get.Run(node.Name); err == nil && rel != nil {
		prevManifest = rel.Manifest
	}

	rendered, err := deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
		Chart:       node.Chart,
		Version:     node.ChartVersion,
		ReleaseName: node.Name,
		Namespace:   node.Namespace,
		ValuesFiles: node.Values,
		SetValues:   flattenSet(node.Set),
		UseCluster:  true,
	})
	if err != nil {
		return nil, err
	}
	nextManifest := ""
	if rendered != nil {
		nextManifest = rendered.Manifest
	}

	summary, err := deploy.SummarizeManifestPlanWithHelmKube(helmClient, prevManifest, nextManifest)
	if err != nil {
		summary, err = deploy.SummarizeManifestPlan(prevManifest, nextManifest)
		if err != nil {
			return nil, err
		}
	}

	out := &NodeDiffSummary{
		Add:     summary.Add,
		Change:  summary.Change,
		Replace: summary.Replace,
		Destroy: summary.Destroy,
	}
	out.Risky = riskyKinds(summary)
	return out, nil
}

func riskyKinds(summary *deploy.PlanSummary) map[string]int {
	if summary == nil {
		return nil
	}
	riskySet := map[string]struct{}{
		"CustomResourceDefinition":       {},
		"ValidatingWebhookConfiguration": {},
		"MutatingWebhookConfiguration":   {},
		"ClusterRole":                    {},
		"ClusterRoleBinding":             {},
		"Role":                           {},
		"RoleBinding":                    {},
		"PodDisruptionBudget":            {},
		"NetworkPolicy":                  {},
	}
	out := map[string]int{}
	add := func(ch deploy.PlanChange) {
		if _, ok := riskySet[ch.Kind]; ok {
			out[ch.Kind]++
		}
	}
	for _, ch := range summary.Changes {
		add(ch)
	}
	for _, ch := range summary.Hooks.Changes {
		add(ch)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func WriteStackPlanBundle(outPath string, planJSON []byte, attestationJSON []byte, inputsPath string, inputsManifestJSON []byte, diffSummaryJSON []byte, manifest StackPlanBundleManifest) (string, error) {
	outPath = strings.TrimSpace(outPath)
	if outPath == "" {
		return "", fmt.Errorf("bundle output path is required")
	}
	tmp, err := os.MkdirTemp("", "ktl-stack-plan-bundle-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmp)

	planFile := filepath.Join(tmp, "plan.json")
	if err := os.WriteFile(planFile, append(planJSON, '\n'), 0o644); err != nil {
		return "", err
	}
	attFile := filepath.Join(tmp, "attestation.json")
	if err := os.WriteFile(attFile, append(attestationJSON, '\n'), 0o644); err != nil {
		return "", err
	}
	manifestFile := filepath.Join(tmp, "manifest.json")
	rawManifest, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(manifestFile, append(rawManifest, '\n'), 0o644); err != nil {
		return "", err
	}

	var files []tarFile
	files = append(files, tarFile{Name: "manifest.json", Path: manifestFile, Mode: 0o644})
	files = append(files, tarFile{Name: "plan.json", Path: planFile, Mode: 0o644})
	files = append(files, tarFile{Name: "attestation.json", Path: attFile, Mode: 0o644})

	if strings.TrimSpace(inputsPath) != "" {
		files = append(files, tarFile{Name: "inputs.tar.gz", Path: inputsPath, Mode: 0o644})
	}
	if len(inputsManifestJSON) > 0 {
		p := filepath.Join(tmp, "inputs.manifest.json")
		if err := os.WriteFile(p, append(inputsManifestJSON, '\n'), 0o644); err != nil {
			return "", err
		}
		files = append(files, tarFile{Name: "inputs.manifest.json", Path: p, Mode: 0o644})
	}
	if len(diffSummaryJSON) > 0 {
		p := filepath.Join(tmp, "diff_summary.json")
		if err := os.WriteFile(p, append(diffSummaryJSON, '\n'), 0o644); err != nil {
			return "", err
		}
		files = append(files, tarFile{Name: "diff_summary.json", Path: p, Mode: 0o644})
	}

	if err := writeDeterministicTarGz(outPath, files); err != nil {
		return "", err
	}
	return outPath, nil
}
