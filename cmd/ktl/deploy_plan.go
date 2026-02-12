// File: cmd/ktl/deploy_plan.go
// Brief: CLI command wiring and implementation for 'deploy plan'.

// deploy_plan.go contains the deploy plan/apply logic (ktl apply plan / ktl apply), rendering manifests, producing HTML diffs, and teeing the plan into files.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	_ "embed"

	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/kube"
	"github.com/kubekattle/ktl/internal/secretstore"
	"github.com/kubekattle/ktl/internal/telemetry"
	"github.com/kubekattle/ktl/internal/ui"
	"github.com/pmezard/go-difflib/difflib"
	"github.com/spf13/cobra"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	"helm.sh/helm/v3/pkg/storage/driver"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/yaml"
)

func newDeployPlanCommand(namespace *string, kubeconfig *string, kubeContext *string, helpSection string) *cobra.Command {
	ownNamespaceFlag := false
	if namespace == nil {
		namespace = new(string)
		ownNamespaceFlag = true
	}
	var chart string
	var release string
	var version string
	var valuesFiles []string
	var setValues []string
	var setStringValues []string
	var setFileValues []string
	var secretProvider string
	var secretConfig string
	var includeCRDs bool
	var format string
	var outputPath string
	var visualize bool
	var visualizeExplain bool
	var compareSource string
	var compareTo string
	var compareExit bool
	var baselinePath string
	resolvedFormat := ""
	resolveFormat := func() string {
		return resolveDeployPlanFormat(format, visualize)
	}

	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Preview Helm release changes without applying them",
		Long:  "Render the chart, diff it against live cluster resources, and summarize the net creates/updates/deletes before running ktl apply.",
		Args:  cobra.NoArgs,
		PreRunE: func(cmd *cobra.Command, args []string) error {
			resolvedFormat = resolveFormat()
			switch resolvedFormat {
			case "text", "json", "yaml", "html", "visualize-html", "visualize-json", "visualize-yaml":
			default:
				return fmt.Errorf("unsupported format %q (expected text, json, yaml, html, or visualize)", resolvedFormat)
			}
			if resolvedFormat == "text" && strings.TrimSpace(outputPath) != "" {
				return fmt.Errorf("--output is only supported with --format=html, --format=json, --format=yaml, or --visualize")
			}
			if visualizeExplain && !visualize {
				return fmt.Errorf("--visualize-explain requires --visualize")
			}
			if strings.TrimSpace(compareSource) != "" && !visualize {
				return fmt.Errorf("--compare is only supported with --visualize")
			}
			if strings.TrimSpace(compareTo) == "" && cmd.Flags().Changed("compare-exit") {
				return fmt.Errorf("--compare-exit requires --compare-to")
			}
			if strings.TrimSpace(baselinePath) == "-" {
				return fmt.Errorf("--baseline must be a file path (\"-\" is not supported)")
			}
			return nil
		},
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx := cmd.Context()
			kubeClient, err := kube.New(ctx, *kubeconfig, *kubeContext)
			if err != nil {
				return err
			}
			selectedFormat := resolvedFormat
			if selectedFormat == "" {
				selectedFormat = resolveFormat()
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

			actionCfg := new(action.Configuration)
			logFunc := func(format string, v ...interface{}) {
				fmt.Fprintf(cmd.ErrOrStderr(), format+"\n", v...)
			}
			if err := actionCfg.Init(settings.RESTClientGetter(), resolvedNamespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
				return fmt.Errorf("init helm action config: %w", err)
			}

			var secretAudit secretstore.AuditReport
			secretResolver, secretAuditSink, err := buildDeploySecretResolver(ctx, deploySecretConfig{
				Chart:      chart,
				ConfigPath: secretConfig,
				Provider:   secretProvider,
				Mode:       secretstore.ResolveModeMask,
				ErrOut:     cmd.ErrOrStderr(),
			})
			if err != nil {
				return err
			}
			auditSink := func(report secretstore.AuditReport) {
				secretAudit = report
				if secretAuditSink != nil {
					secretAuditSink(report)
				}
			}
			secretOptions := &deploy.SecretOptions{Resolver: secretResolver, AuditSink: auditSink, Validate: true}

			stopSpinner := ui.StartSpinner(cmd.ErrOrStderr(), fmt.Sprintf("Planning release %s", release))
			defer func() {
				if stopSpinner != nil {
					stopSpinner(false)
				}
			}()

			timer := telemetry.NewPhaseTimer()
			options := deployPlanOptions{
				Chart:           chart,
				Release:         release,
				Version:         version,
				Namespace:       resolvedNamespace,
				ValuesFiles:     valuesFiles,
				SetValues:       setValues,
				SetStringValues: setStringValues,
				SetFileValues:   setFileValues,
				Secrets:         secretOptions,
				IncludeCRDs:     includeCRDs,
			}
			planResult, err := executeDeployPlan(ctx, actionCfg, settings, kubeClient, options, timer)
			if err != nil {
				return err
			}
			planResult.Secrets = planSecretsFromAudit(secretAudit)
			if timer != nil {
				summary := telemetry.Summary{
					Total:  timer.Total(),
					Phases: timer.Snapshot(),
				}
				if kubeClient != nil && kubeClient.APIStats != nil {
					metrics := kubeClient.APIStats.Snapshot()
					summary.KubeRequests = metrics.Count
					summary.KubeAvg = metrics.Avg()
					summary.KubeMax = metrics.Max
				}
				planResult.Telemetry = buildPlanTelemetry(summary)
				if line := summary.Line(); line != "" {
					fmt.Fprintln(cmd.ErrOrStderr(), line)
				}
			}

			stopSpinner(true)
			stopSpinner = nil

			var compareResult *deployPlanResult
			if strings.TrimSpace(compareTo) != "" {
				var cerr error
				compareResult, cerr = loadPlanResultFromSource(ctx, compareTo)
				if cerr != nil {
					return fmt.Errorf("load baseline plan: %w", cerr)
				}
				compare := comparePlanResults(planResult, compareResult, compareTo)
				planResult.Compare = compare
				if compare != nil {
					if line := renderPlanCompareLine(compare); line != "" {
						fmt.Fprintln(cmd.ErrOrStderr(), line)
					}
					if compareExit && compare.Summary.HasRegressions() {
						return fmt.Errorf("plan regression detected (new=%d changed=%d)", compare.Summary.New, compare.Summary.Changed)
					}
				}
			}
			if strings.TrimSpace(baselinePath) != "" {
				if err := writePlanBaseline(baselinePath, planResult); err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "Baseline written to %s\n", baselinePath)
			}

			switch selectedFormat {
			case "html":
				path := outputPath
				if strings.TrimSpace(path) == "" {
					slug := sanitizeFilename(release)
					if slug == "" {
						slug = "release"
					}
					path = fmt.Sprintf("ktl-deploy-plan-%s-%s.html", slug, planResult.GeneratedAt.Format("20060102-150405"))
				}
				html, err := renderDeployPlanHTML(planResult)
				if err != nil {
					return err
				}
				if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
					return fmt.Errorf("write html: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Plan written to %s\n", path)
				return nil
			case "visualize-html":
				path := strings.TrimSpace(outputPath)
				if path == "" {
					path = defaultDeployVisualizeOutputPath(release, planResult.GeneratedAt)
				}
				var visualizeCompare *deployPlanResult
				if strings.TrimSpace(compareSource) != "" {
					var cerr error
					visualizeCompare, cerr = loadPlanResultFromSource(ctx, compareSource)
					if cerr != nil {
						return fmt.Errorf("load compare artifact: %w", cerr)
					}
				} else {
					visualizeCompare = compareResult
				}
				html, err := renderDeployVisualizeHTML(planResult, visualizeCompare, deployVisualizeFeatures{
					ExplainDiff: visualizeExplain,
				})
				if err != nil {
					return err
				}
				if path == "-" {
					fmt.Fprintln(cmd.OutOrStdout(), html)
					return nil
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
				if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
					return fmt.Errorf("write visualize html: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Visualization written to %s\n", path)
				return nil
			case "visualize-json", "visualize-yaml":
				path := strings.TrimSpace(outputPath)
				if path == "" {
					ext := "json"
					if selectedFormat == "visualize-yaml" {
						ext = "yaml"
					}
					path = defaultDeployVisualizeDataOutputPath(release, planResult.GeneratedAt, ext)
				}
				var visualizeCompare *deployPlanResult
				if strings.TrimSpace(compareSource) != "" {
					var cerr error
					visualizeCompare, cerr = loadPlanResultFromSource(ctx, compareSource)
					if cerr != nil {
						return fmt.Errorf("load compare artifact: %w", cerr)
					}
				} else {
					visualizeCompare = compareResult
				}
				payload, err := buildDeployVisualizePayload(planResult, visualizeCompare)
				if err != nil {
					return err
				}
				var data []byte
				if selectedFormat == "visualize-yaml" {
					data, err = yaml.Marshal(payload)
					if err != nil {
						return fmt.Errorf("marshal viz yaml: %w", err)
					}
				} else {
					data, err = json.MarshalIndent(payload, "", "  ")
					if err != nil {
						return fmt.Errorf("marshal viz json: %w", err)
					}
				}
				if path == "-" {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
					return nil
				}
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					return fmt.Errorf("create output dir: %w", err)
				}
				if err := os.WriteFile(path, data, 0o644); err != nil {
					return fmt.Errorf("write visualize data: %w", err)
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Visualization data written to %s\n", path)
				return nil
			case "json":
				data, err := json.MarshalIndent(planResult, "", "  ")
				if err != nil {
					return fmt.Errorf("marshal plan json: %w", err)
				}
				if strings.TrimSpace(outputPath) != "" {
					if err := os.WriteFile(outputPath, data, 0o644); err != nil {
						return fmt.Errorf("write json: %w", err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Plan written to %s\n", outputPath)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
				}
				return nil
			case "yaml":
				data, err := yaml.Marshal(planResult)
				if err != nil {
					return fmt.Errorf("marshal plan yaml: %w", err)
				}
				if strings.TrimSpace(outputPath) != "" {
					if err := os.WriteFile(outputPath, data, 0o644); err != nil {
						return fmt.Errorf("write yaml: %w", err)
					}
					fmt.Fprintf(cmd.OutOrStdout(), "Plan written to %s\n", outputPath)
				} else {
					fmt.Fprintf(cmd.OutOrStdout(), "%s\n", data)
				}
				return nil
			default:
				renderDeployPlan(cmd.OutOrStdout(), planResult)
				return nil
			}
		},
	}

	cmd.Flags().StringVar(&chart, "chart", "", "Chart reference (path, repo/name, or OCI ref)")
	cmd.Flags().StringVar(&release, "release", "", "Helm release name")
	cmd.Flags().StringVar(&version, "version", "", "Chart version (default: latest)")
	cmd.Flags().StringSliceVarP(&valuesFiles, "values", "f", nil, "Values files to apply (can be repeated)")
	cmd.Flags().StringArrayVar(&setValues, "set", nil, "Set values on the command line (key=val)")
	cmd.Flags().StringArrayVar(&setStringValues, "set-string", nil, "Set STRING values on the command line")
	cmd.Flags().StringArrayVar(&setFileValues, "set-file", nil, "Set values from files (key=path)")
	cmd.Flags().StringVar(&secretProvider, "secret-provider", "", "Secret provider name for secret:// references")
	cmd.Flags().StringVar(&secretConfig, "secret-config", "", "Secrets provider config file (defaults to ~/.ktl/config.yaml and repo .ktl.yaml)")
	cmd.Flags().BoolVar(&includeCRDs, "include-crds", false, "Render CRDs in addition to the main chart objects")
	cmd.Flags().StringVar(&compareSource, "compare", "", "Plan artifact (path or URL) to embed for visualize comparisons")
	cmd.Flags().StringVar(&compareTo, "compare-to", "", "Compare against a previous plan (path or URL) and report regressions")
	cmd.Flags().BoolVar(&compareExit, "compare-exit", true, "Exit non-zero when --compare-to detects regressions")
	cmd.Flags().StringVar(&baselinePath, "baseline", "", "Write plan JSON baseline to this path")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: text, json, yaml, or html")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write the rendered plan to this path (HTML defaults to ./ktl-deploy-plan-<release>-<timestamp>.html)")
	cmd.Flags().BoolVar(&visualize, "visualize", false, "Render the interactive visualization")
	cmd.Flags().BoolVar(&visualizeExplain, "visualize-explain", false, "Add an Explain Diff tab in --visualize output (experimental)")
	_ = cmd.MarkFlagRequired("chart")
	_ = cmd.MarkFlagRequired("release")

	if ownNamespaceFlag {
		cmd.Flags().StringVarP(namespace, "namespace", "n", "", "Namespace for the Helm release (defaults to active context)")
	}
	section := strings.TrimSpace(helpSection)
	if section == "" {
		section = "Plan Flags"
	}
	decorateCommandHelp(cmd, section)
	return cmd
}

func resolveDeployPlanFormat(format string, visualize bool) string {
	selected := strings.ToLower(strings.TrimSpace(format))
	if visualize {
		switch selected {
		case "", "text":
			// Preserve the original behavior: if the user asked for text output,
			// --visualize should not force HTML.
			selected = "text"
		case "visualize", "html":
			selected = "visualize-html"
		case "json":
			selected = "visualize-json"
		case "yaml", "yml":
			selected = "visualize-yaml"
		}
	}
	if selected == "" {
		selected = "text"
	}
	return selected
}

type deployPlanOptions struct {
	Chart           string
	Release         string
	Version         string
	Namespace       string
	ValuesFiles     []string
	SetValues       []string
	SetStringValues []string
	SetFileValues   []string
	Secrets         *deploy.SecretOptions
	IncludeCRDs     bool
}

type deployPlanResult struct {
	ReleaseName       string                  `json:"release"`
	Namespace         string                  `json:"namespace"`
	ChartVersion      string                  `json:"chartVersion,omitempty"`
	ChartRef          string                  `json:"chartReference,omitempty"`
	RequestedChart    string                  `json:"requestedChart,omitempty"`
	RequestedVersion  string                  `json:"requestedVersion,omitempty"`
	ValuesFiles       []string                `json:"valuesFiles,omitempty"`
	SetValues         []string                `json:"setValues,omitempty"`
	SetStringValues   []string                `json:"setStringValues,omitempty"`
	SetFileValues     []string                `json:"setFileValues,omitempty"`
	Secrets           []planSecretRef         `json:"secrets,omitempty"`
	GraphNodes        []deployGraphNode       `json:"graphNodes,omitempty"`
	GraphEdges        []deployGraphEdge       `json:"graphEdges,omitempty"`
	ManifestBlobs     map[string]string       `json:"manifestBlobs,omitempty"`
	LiveManifests     map[string]string       `json:"liveManifestBlobs,omitempty"`
	ManifestDiffs     map[string]string       `json:"manifestDiffs,omitempty"`
	ManifestTemplates map[string]string       `json:"manifestTemplates,omitempty"`
	TemplateSources   map[string]string       `json:"templateSources,omitempty"`
	Changes           []planResourceChange    `json:"changes"`
	Summary           planSummary             `json:"summary"`
	Warnings          []string                `json:"warnings,omitempty"`
	DesiredQuota      *quotaReport            `json:"desiredQuota,omitempty"`
	DesiredQuotaByNS  map[string]*quotaReport `json:"desiredQuotaByNamespace,omitempty"`
	ClusterHost       string                  `json:"clusterHost,omitempty"`
	InstallCmd        string                  `json:"installCommand,omitempty"`
	GeneratedAt       time.Time               `json:"generatedAt"`
	OfflineFallback   bool                    `json:"offlineFallback"`
	Compare           *planCompare            `json:"compare,omitempty"`
	Telemetry         *planTelemetry          `json:"telemetry,omitempty"`
}

type planChangeKind string

const (
	changeCreate planChangeKind = "create"
	changeUpdate planChangeKind = "update"
	changeDelete planChangeKind = "delete"
)

type planResourceChange struct {
	Key  resourceKey    `json:"resource"`
	Kind planChangeKind `json:"change"`
	Diff string         `json:"diff,omitempty"`
}

type deployGraphNode struct {
	ID        string            `json:"id"`
	Kind      string            `json:"kind"`
	Name      string            `json:"name"`
	Namespace string            `json:"namespace,omitempty"`
	Source    string            `json:"source"`
	Live      bool              `json:"live"`
	Meta      map[string]string `json:"meta,omitempty"`
}

type deployGraphEdge struct {
	From   string `json:"from"`
	To     string `json:"to"`
	Reason string `json:"reason,omitempty"`
}

type planSummary struct {
	Creates   int `json:"creates"`
	Updates   int `json:"updates"`
	Deletes   int `json:"deletes"`
	Unchanged int `json:"unchanged"`
}

type planSecretRef struct {
	Provider  string `json:"provider"`
	Path      string `json:"path,omitempty"`
	Reference string `json:"reference,omitempty"`
	Masked    bool   `json:"masked,omitempty"`
}

func planSecretsFromAudit(report secretstore.AuditReport) []planSecretRef {
	if report.Empty() {
		return nil
	}
	out := make([]planSecretRef, 0, len(report.Entries))
	for _, entry := range report.Entries {
		if entry.Provider == "" && entry.Path == "" && entry.Reference == "" {
			continue
		}
		out = append(out, planSecretRef{
			Provider:  entry.Provider,
			Path:      entry.Path,
			Reference: entry.Reference,
			Masked:    entry.Masked,
		})
	}
	return out
}

type planCompareSummary struct {
	New       int `json:"new"`
	Changed   int `json:"changed"`
	Resolved  int `json:"resolved"`
	Unchanged int `json:"unchanged"`
}

func (s planCompareSummary) HasRegressions() bool {
	return s.New > 0 || s.Changed > 0
}

type planCompare struct {
	Source   string               `json:"source,omitempty"`
	Summary  planCompareSummary   `json:"summary"`
	New      []planResourceChange `json:"new,omitempty"`
	Changed  []planChangeDelta    `json:"changed,omitempty"`
	Resolved []planResourceChange `json:"resolved,omitempty"`
}

type planChangeDelta struct {
	Resource     planResourceChange `json:"resource"`
	PreviousKind planChangeKind     `json:"previousKind"`
}

type planTelemetry struct {
	TotalMs      int64            `json:"totalMs,omitempty"`
	PhasesMs     map[string]int64 `json:"phasesMs,omitempty"`
	KubeRequests int              `json:"kubeRequests,omitempty"`
	KubeAvgMs    int64            `json:"kubeAvgMs,omitempty"`
	KubeMaxMs    int64            `json:"kubeMaxMs,omitempty"`
}

func comparePlanResults(current *deployPlanResult, baseline *deployPlanResult, source string) *planCompare {
	if current == nil || baseline == nil {
		return nil
	}
	baseMap := map[resourceKey]planResourceChange{}
	for _, ch := range baseline.Changes {
		baseMap[ch.Key] = ch
	}
	curMap := map[resourceKey]planResourceChange{}
	for _, ch := range current.Changes {
		curMap[ch.Key] = ch
	}

	var newChanges []planResourceChange
	var changed []planChangeDelta
	unchanged := 0
	for _, ch := range current.Changes {
		prev, ok := baseMap[ch.Key]
		if !ok {
			newChanges = append(newChanges, planChangeWithoutDiff(ch))
			continue
		}
		if prev.Kind != ch.Kind {
			changed = append(changed, planChangeDelta{
				Resource:     planChangeWithoutDiff(ch),
				PreviousKind: prev.Kind,
			})
			continue
		}
		unchanged++
	}

	var resolved []planResourceChange
	for _, ch := range baseline.Changes {
		if _, ok := curMap[ch.Key]; !ok {
			resolved = append(resolved, planChangeWithoutDiff(ch))
		}
	}

	sortPlanResourceChanges(newChanges)
	sortPlanResourceChanges(resolved)
	sortPlanChangeDeltas(changed)

	summary := planCompareSummary{
		New:       len(newChanges),
		Changed:   len(changed),
		Resolved:  len(resolved),
		Unchanged: unchanged,
	}
	return &planCompare{
		Source:   strings.TrimSpace(source),
		Summary:  summary,
		New:      newChanges,
		Changed:  changed,
		Resolved: resolved,
	}
}

func planChangeWithoutDiff(ch planResourceChange) planResourceChange {
	ch.Diff = ""
	return ch
}

func sortPlanResourceChanges(changes []planResourceChange) {
	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind != changes[j].Kind {
			return changes[i].Kind < changes[j].Kind
		}
		return changes[i].Key.String() < changes[j].Key.String()
	})
}

func sortPlanChangeDeltas(deltas []planChangeDelta) {
	sort.Slice(deltas, func(i, j int) bool {
		if deltas[i].Resource.Kind != deltas[j].Resource.Kind {
			return deltas[i].Resource.Kind < deltas[j].Resource.Kind
		}
		return deltas[i].Resource.Key.String() < deltas[j].Resource.Key.String()
	})
}

func renderPlanCompareLine(compare *planCompare) string {
	if compare == nil {
		return ""
	}
	s := compare.Summary
	if s.New == 0 && s.Changed == 0 && s.Resolved == 0 && s.Unchanged == 0 {
		return ""
	}
	label := "Plan compare"
	if compare.Source != "" {
		label = fmt.Sprintf("Plan compare (%s)", compare.Source)
	}
	return fmt.Sprintf("%s: +%d new, ~%d changed, -%d resolved, %d unchanged", label, s.New, s.Changed, s.Resolved, s.Unchanged)
}

func buildPlanTelemetry(summary telemetry.Summary) *planTelemetry {
	if summary.Total == 0 && len(summary.Phases) == 0 && summary.KubeRequests == 0 {
		return nil
	}
	tele := &planTelemetry{
		TotalMs:      summary.Total.Milliseconds(),
		PhasesMs:     durationMapToMs(summary.Phases),
		KubeRequests: summary.KubeRequests,
		KubeAvgMs:    summary.KubeAvg.Milliseconds(),
		KubeMaxMs:    summary.KubeMax.Milliseconds(),
	}
	if tele.PhasesMs != nil && len(tele.PhasesMs) == 0 {
		tele.PhasesMs = nil
	}
	return tele
}

func durationMapToMs(phases map[string]time.Duration) map[string]int64 {
	if len(phases) == 0 {
		return nil
	}
	out := make(map[string]int64, len(phases))
	for key, value := range phases {
		out[key] = value.Milliseconds()
	}
	return out
}

type graphRef struct {
	Kind      string
	Name      string
	Namespace string
	Reason    string
}

func buildDependencyGraph(desired map[resourceKey]manifestDoc, live map[resourceKey]*unstructured.Unstructured) ([]deployGraphNode, []deployGraphEdge) {
	if len(desired) == 0 {
		return nil, nil
	}
	nodes := make(map[string]*deployGraphNode)
	edges := make([]deployGraphEdge, 0)
	edgeSet := make(map[string]struct{})

	addNode := func(key resourceKey, source string) *deployGraphNode {
		if key.Namespace == "" && strings.EqualFold(key.Kind, "Namespace") {
			key.Namespace = "cluster"
		}
		id := graphNodeID(key)
		if existing, ok := nodes[id]; ok {
			if source == "rendered" {
				existing.Source = source
			}
			if !existing.Live && findLiveObject(key, live) != nil {
				existing.Live = true
			}
			return existing
		}
		node := &deployGraphNode{
			ID:        id,
			Kind:      key.Kind,
			Name:      key.Name,
			Namespace: key.Namespace,
			Source:    source,
			Live:      findLiveObject(key, live) != nil,
		}
		if doc, ok := desired[key]; ok {
			node.Meta = extractNodeMeta(doc)
		}
		nodes[id] = node
		return node
	}

	for key, doc := range desired {
		doc := doc
		added := addNode(key, "rendered")
		if added != nil && added.Meta == nil {
			added.Meta = extractNodeMeta(doc)
		}
		refs := extractWorkloadRefs(doc.Obj)
		if len(refs) == 0 {
			continue
		}
		fromID := graphNodeID(key)
		for _, ref := range refs {
			refKey := referenceToResourceKey(ref, key.Namespace)
			source := "rendered"
			if actualKey, ok := findRenderedResource(refKey, desired); ok {
				refKey = actualKey
			} else {
				source = "external"
			}
			addNode(refKey, source)
			toID := graphNodeID(refKey)
			edgeKey := fromID + "|" + toID + "|" + ref.Reason
			if _, exists := edgeSet[edgeKey]; exists {
				continue
			}
			edgeSet[edgeKey] = struct{}{}
			edges = append(edges, deployGraphEdge{
				From:   fromID,
				To:     toID,
				Reason: ref.Reason,
			})
		}
	}

	if len(nodes) == 0 {
		return nil, nil
	}

	nodeList := make([]deployGraphNode, 0, len(nodes))
	for _, node := range nodes {
		nodeList = append(nodeList, *node)
	}
	sort.Slice(nodeList, func(i, j int) bool {
		if nodeList[i].Namespace != nodeList[j].Namespace {
			return nodeList[i].Namespace < nodeList[j].Namespace
		}
		if nodeList[i].Kind != nodeList[j].Kind {
			return nodeList[i].Kind < nodeList[j].Kind
		}
		return nodeList[i].Name < nodeList[j].Name
	})

	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From != edges[j].From {
			return edges[i].From < edges[j].From
		}
		if edges[i].To != edges[j].To {
			return edges[i].To < edges[j].To
		}
		return edges[i].Reason < edges[j].Reason
	})

	return nodeList, edges
}

func trackPlanPhase(timer *telemetry.PhaseTimer, name string, fn func() error) error {
	if timer == nil {
		return fn()
	}
	return timer.Track(name, fn)
}

func trackPlanPhaseFunc(timer *telemetry.PhaseTimer, name string, fn func()) {
	if timer == nil {
		fn()
		return
	}
	timer.TrackFunc(name, fn)
}

func executeDeployPlan(ctx context.Context, actionCfg *action.Configuration, settings *cli.EnvSettings, kubeClient *kube.Client, opts deployPlanOptions, timer *telemetry.PhaseTimer) (*deployPlanResult, error) {
	if opts.Chart == "" {
		return nil, fmt.Errorf("chart reference is required")
	}
	if opts.Release == "" {
		return nil, fmt.Errorf("release name is required")
	}

	var templateResult *deploy.TemplateResult
	if err := trackPlanPhase(timer, "render", func() error {
		var err error
		templateResult, err = deploy.RenderTemplate(ctx, actionCfg, settings, deploy.TemplateOptions{
			Chart:           opts.Chart,
			Version:         opts.Version,
			ReleaseName:     opts.Release,
			Namespace:       opts.Namespace,
			ValuesFiles:     opts.ValuesFiles,
			SetValues:       opts.SetValues,
			SetStringValues: opts.SetStringValues,
			SetFileValues:   opts.SetFileValues,
			Secrets:         opts.Secrets,
			IncludeCRDs:     opts.IncludeCRDs,
		})
		return err
	}); err != nil {
		return nil, err
	}

	desiredDocs := docsToMap(parseManifestDocs(templateResult.Manifest))
	manifestTemplates := buildManifestTemplateIndex(desiredDocs)

	var previousDocs map[resourceKey]manifestDoc
	if actionCfg != nil {
		if err := trackPlanPhase(timer, "release", func() error {
			getAction := action.NewGet(actionCfg)
			if rel, err := getAction.Run(opts.Release); err == nil && rel != nil {
				previousDocs = docsToMap(parseManifestDocs(rel.Manifest))
				return nil
			} else if err != nil && !errors.Is(err, driver.ErrReleaseNotFound) {
				return fmt.Errorf("get release %s: %w", opts.Release, err)
			}
			return nil
		}); err != nil {
			return nil, err
		}
	}
	if previousDocs == nil {
		previousDocs = map[resourceKey]manifestDoc{}
	}

	var liveState map[resourceKey]*unstructured.Unstructured
	var lookupWarnings []string
	err := trackPlanPhase(timer, "live", func() error {
		var err error
		liveState, lookupWarnings, err = collectLiveResources(ctx, kubeClient, desiredDocs, opts.Namespace)
		return err
	})
	offlineFallback := false
	if err != nil {
		offlineFallback = true
		lookupWarnings = append(lookupWarnings, fmt.Sprintf("Live lookup failed (%v); falling back to previous release manifest.", err))
		liveState = nil
	}

	var (
		changes           []planResourceChange
		summary           planSummary
		graphNodes        []deployGraphNode
		graphEdges        []deployGraphEdge
		manifestBlobs     map[string]string
		liveManifestBlobs map[string]string
		manifestDiffs     map[string]string
		warnings          []string
	)
	trackPlanPhaseFunc(timer, "diff", func() {
		changes, summary = buildPlanChanges(desiredDocs, previousDocs, liveState)
		graphNodes, graphEdges = buildDependencyGraph(desiredDocs, liveState)
		manifestBlobs = buildManifestBlobs(desiredDocs)
		liveManifestBlobs = buildLiveManifestBlobs(liveState)
		manifestDiffs = buildManifestDiffs(liveManifestBlobs, manifestBlobs)
		warnings = append([]string{}, lookupWarnings...)
		warnings = append(warnings, planWarnings(changes)...)
	})
	quotaNamespaces := map[string]struct{}{}
	if strings.TrimSpace(opts.Namespace) != "" {
		quotaNamespaces[opts.Namespace] = struct{}{}
	}
	for key, doc := range desiredDocs {
		if key.Kind == "Namespace" {
			continue
		}
		ns := strings.TrimSpace(key.Namespace)
		if ns == "" && doc.Obj != nil {
			ns = strings.TrimSpace(doc.Obj.GetNamespace())
		}
		if ns == "" {
			continue
		}
		quotaNamespaces[ns] = struct{}{}
	}
	desiredQuotaByNS := map[string]*quotaReport{}
	trackPlanPhaseFunc(timer, "quota", func() {
		if len(quotaNamespaces) == 0 {
			return
		}
		var nsList []string
		for ns := range quotaNamespaces {
			nsList = append(nsList, ns)
		}
		sort.Strings(nsList)
		for _, ns := range nsList {
			report := buildDesiredQuotaReport(desiredDocs, ns)
			if report == nil {
				continue
			}
			if kubeClient != nil && kubeClient.Clientset != nil {
				if err := populateQuotaLive(ctx, kubeClient.Clientset, report); err != nil {
					report.Warnings = append(report.Warnings, fmt.Sprintf("Failed to load namespace quotas: %v", err))
				}
			}
			desiredQuotaByNS[ns] = report
		}
	})
	desiredQuota := desiredQuotaByNS[opts.Namespace]

	var cluster string
	if kubeClient != nil && kubeClient.RESTConfig != nil {
		cluster = kubeClient.RESTConfig.Host
	}
	return &deployPlanResult{
		ReleaseName:       opts.Release,
		Namespace:         opts.Namespace,
		ChartVersion:      templateResult.ChartVersion,
		ChartRef:          opts.Chart,
		RequestedChart:    opts.Chart,
		RequestedVersion:  opts.Version,
		ValuesFiles:       append([]string(nil), opts.ValuesFiles...),
		SetValues:         append([]string(nil), opts.SetValues...),
		SetStringValues:   append([]string(nil), opts.SetStringValues...),
		SetFileValues:     append([]string(nil), opts.SetFileValues...),
		GraphNodes:        graphNodes,
		GraphEdges:        graphEdges,
		ManifestBlobs:     manifestBlobs,
		LiveManifests:     liveManifestBlobs,
		ManifestDiffs:     manifestDiffs,
		ManifestTemplates: manifestTemplates,
		TemplateSources:   templateResult.Templates,
		Changes:           changes,
		Summary:           summary,
		Warnings:          warnings,
		DesiredQuota:      desiredQuota,
		DesiredQuotaByNS:  desiredQuotaByNS,
		ClusterHost:       cluster,
		InstallCmd:        buildInstallCommand(opts),
		GeneratedAt:       time.Now().UTC(),
		OfflineFallback:   offlineFallback,
	}, nil
}

func collectLiveResources(ctx context.Context, kubeClient *kube.Client, desired map[resourceKey]manifestDoc, defaultNamespace string) (map[resourceKey]*unstructured.Unstructured, []string, error) {
	if kubeClient == nil || kubeClient.Dynamic == nil || kubeClient.RESTMapper == nil {
		return nil, nil, fmt.Errorf("kubernetes client is not initialized")
	}
	live := make(map[resourceKey]*unstructured.Unstructured, len(desired))
	var warnings []string
	for key, doc := range desired {
		res, warn, err := fetchLiveResource(ctx, kubeClient, doc.Obj, defaultNamespace)
		if err != nil {
			return nil, nil, fmt.Errorf("fetch %s: %w", key.String(), err)
		}
		if warn != "" {
			warnings = append(warnings, warn)
		}
		live[key] = res
	}
	return live, warnings, nil
}

func fetchLiveResource(ctx context.Context, kubeClient *kube.Client, obj *unstructured.Unstructured, defaultNamespace string) (*unstructured.Unstructured, string, error) {
	if obj == nil {
		return nil, "", nil
	}
	gvk := schema.FromAPIVersionAndKind(obj.GetAPIVersion(), obj.GetKind())
	mapping, err := kubeClient.RESTMapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		if meta.IsNoMatchError(err) {
			return nil, fmt.Sprintf("Skipping live lookup for %s: %v", obj.GetName(), err), nil
		}
		return nil, "", err
	}

	resource := kubeClient.Dynamic.Resource(mapping.Resource)
	namespace := obj.GetNamespace()
	if namespace == "" && mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		namespace = defaultNamespace
	}

	var live *unstructured.Unstructured
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		if namespace == "" {
			namespace = "default"
		}
		live, err = resource.Namespace(namespace).Get(ctx, obj.GetName(), metav1.GetOptions{})
	} else {
		live, err = resource.Get(ctx, obj.GetName(), metav1.GetOptions{})
	}
	if apierrors.IsNotFound(err) {
		return nil, "", nil
	}
	if err != nil {
		return nil, "", err
	}
	return live, "", nil
}

func docsToMap(docs []manifestDoc) map[resourceKey]manifestDoc {
	result := make(map[resourceKey]manifestDoc, len(docs))
	for _, doc := range docs {
		if doc.Key.Name == "" || doc.Key.Kind == "" {
			continue
		}
		result[doc.Key] = doc
	}
	return result
}

func buildPlanChanges(desired map[resourceKey]manifestDoc, previous map[resourceKey]manifestDoc, live map[resourceKey]*unstructured.Unstructured) ([]planResourceChange, planSummary) {
	if live == nil {
		live = map[resourceKey]*unstructured.Unstructured{}
	}
	changes := make([]planResourceChange, 0, len(desired))
	summary := planSummary{}

	for key, doc := range desired {
		liveObj := live[key]
		if liveObj == nil {
			if prev, ok := previous[key]; ok && prev.Obj != nil {
				liveObj = prev.Obj
			}
		}
		desiredStr := objectYAML(doc.Obj)
		if liveObj == nil {
			summary.Creates++
			changes = append(changes, planResourceChange{Key: key, Kind: changeCreate, Diff: diffStrings("", desiredStr)})
			continue
		}
		liveStr := objectYAML(liveObj)
		if strings.TrimSpace(liveStr) == strings.TrimSpace(desiredStr) {
			summary.Unchanged++
			continue
		}
		summary.Updates++
		changes = append(changes, planResourceChange{Key: key, Kind: changeUpdate, Diff: diffStrings(liveStr, desiredStr)})
	}

	for key, doc := range previous {
		if _, ok := desired[key]; ok {
			continue
		}
		summary.Deletes++
		changes = append(changes, planResourceChange{Key: key, Kind: changeDelete, Diff: diffStrings(objectYAML(doc.Obj), "")})
	}

	sort.Slice(changes, func(i, j int) bool {
		if changes[i].Kind == changes[j].Kind {
			return changes[i].Key.String() < changes[j].Key.String()
		}
		return changes[i].Kind < changes[j].Kind
	})

	return changes, summary
}

func planWarnings(changes []planResourceChange) []string {
	var warnings []string
	for _, change := range changes {
		switch change.Kind {
		case changeUpdate:
			if isWorkloadKind(change.Key.Kind) {
				warnings = append(warnings, fmt.Sprintf("Updating %s will restart pods; ensure PodDisruptionBudgets allow the rollout.", change.Key.String()))
			}
		case changeDelete:
			if strings.EqualFold(change.Key.Kind, "PodDisruptionBudget") {
				warnings = append(warnings, fmt.Sprintf("Deleting %s removes disruption safeguards; coordinate with SREs before proceeding.", change.Key.String()))
			}
			if isWorkloadKind(change.Key.Kind) {
				warnings = append(warnings, fmt.Sprintf("Deleting %s will evict running pods.", change.Key.String()))
			}
		}
	}
	return warnings
}

func buildManifestBlobs(desired map[resourceKey]manifestDoc) map[string]string {
	if len(desired) == 0 {
		return nil
	}
	out := make(map[string]string, len(desired))
	for key, doc := range desired {
		if doc.Body == "" {
			doc.Body = objectYAML(doc.Obj)
		}
		out[graphNodeID(key)] = doc.Body
	}
	return out
}

func buildManifestTemplateIndex(desired map[resourceKey]manifestDoc) map[string]string {
	if len(desired) == 0 {
		return nil
	}
	out := make(map[string]string)
	for key, doc := range desired {
		if doc.TemplateSource == "" {
			continue
		}
		out[graphNodeID(key)] = doc.TemplateSource
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildLiveManifestBlobs(live map[resourceKey]*unstructured.Unstructured) map[string]string {
	if len(live) == 0 {
		return nil
	}
	out := make(map[string]string, len(live))
	for key, obj := range live {
		if obj == nil {
			continue
		}
		out[graphNodeID(key)] = objectYAML(obj.DeepCopy())
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func buildManifestDiffs(live, rendered map[string]string) map[string]string {
	if len(live) == 0 || len(rendered) == 0 {
		return nil
	}
	diffs := make(map[string]string)
	for id, desired := range rendered {
		liveBody, ok := live[id]
		if !ok || strings.TrimSpace(liveBody) == "" {
			continue
		}
		if diff := diffStrings(liveBody, desired); strings.TrimSpace(diff) != "" {
			diffs[id] = diff
		}
	}
	if len(diffs) == 0 {
		return nil
	}
	return diffs
}

func graphNodeID(key resourceKey) string {
	ns := key.Namespace
	if ns == "" {
		ns = "cluster"
	}
	return fmt.Sprintf("%s|%s|%s", strings.ToLower(ns), strings.ToLower(key.Kind), strings.ToLower(key.Name))
}

func referenceToResourceKey(ref graphRef, fallbackNamespace string) resourceKey {
	ns := ref.Namespace
	if ns == "" {
		ns = fallbackNamespace
	}
	return resourceKey{
		Kind:      ref.Kind,
		Name:      ref.Name,
		Namespace: ns,
	}
}

func findRenderedResource(ref resourceKey, desired map[resourceKey]manifestDoc) (resourceKey, bool) {
	if _, ok := desired[ref]; ok {
		return ref, true
	}
	for key := range desired {
		if strings.EqualFold(key.Kind, ref.Kind) && key.Name == ref.Name && key.Namespace == ref.Namespace {
			return key, true
		}
	}
	return ref, false
}

func findLiveObject(key resourceKey, live map[resourceKey]*unstructured.Unstructured) *unstructured.Unstructured {
	if live == nil {
		return nil
	}
	if obj := live[key]; obj != nil {
		return obj
	}
	for existingKey, obj := range live {
		if obj == nil {
			continue
		}
		if strings.EqualFold(existingKey.Kind, key.Kind) && existingKey.Name == key.Name && existingKey.Namespace == key.Namespace {
			return obj
		}
	}
	return nil
}

func extractWorkloadRefs(u *unstructured.Unstructured) []graphRef {
	if u == nil {
		return nil
	}
	kind := strings.ToLower(u.GetKind())
	var podSpec map[string]interface{}
	switch kind {
	case "deployment", "statefulset", "daemonset", "replicaset":
		podSpec, _, _ = unstructured.NestedMap(u.Object, "spec", "template", "spec")
	case "job":
		podSpec, _, _ = unstructured.NestedMap(u.Object, "spec", "template", "spec")
	case "cronjob":
		podSpec, _, _ = unstructured.NestedMap(u.Object, "spec", "jobTemplate", "spec", "template", "spec")
	case "pod":
		podSpec, _, _ = unstructured.NestedMap(u.Object, "spec")
	default:
		return nil
	}
	if len(podSpec) == 0 {
		return nil
	}
	refs := collectRefsFromPodSpec(podSpec)
	if len(refs) == 0 {
		return nil
	}
	for i := range refs {
		if refs[i].Namespace == "" {
			refs[i].Namespace = u.GetNamespace()
		}
	}
	return refs
}

func collectRefsFromPodSpec(spec map[string]interface{}) []graphRef {
	var refs []graphRef
	volumes, _, _ := unstructured.NestedSlice(spec, "volumes")
	for _, volRaw := range volumes {
		vol := toMap(volRaw)
		if vol == nil {
			continue
		}
		volName := toString(vol["name"])
		if cm := toMap(vol["configMap"]); cm != nil {
			name := toString(cm["name"])
			if name != "" {
				refs = append(refs, graphRef{Kind: "ConfigMap", Name: name, Reason: fmt.Sprintf("volume:%s", volName)})
			}
		}
		if sec := toMap(vol["secret"]); sec != nil {
			name := toString(sec["secretName"])
			if name == "" {
				name = toString(sec["name"])
			}
			if name != "" {
				refs = append(refs, graphRef{Kind: "Secret", Name: name, Reason: fmt.Sprintf("volume:%s", volName)})
			}
		}
		if pvc := toMap(vol["persistentVolumeClaim"]); pvc != nil {
			name := toString(pvc["claimName"])
			if name != "" {
				refs = append(refs, graphRef{Kind: "PersistentVolumeClaim", Name: name, Reason: fmt.Sprintf("pvc:%s", volName)})
			}
		}
		if projected := toMap(vol["projected"]); projected != nil {
			sources, _ := projected["sources"].([]interface{})
			for _, source := range sources {
				src := toMap(source)
				if cm := toMap(src["configMap"]); cm != nil {
					name := toString(cm["name"])
					if name != "" {
						refs = append(refs, graphRef{Kind: "ConfigMap", Name: name, Reason: fmt.Sprintf("volume:%s", volName)})
					}
				}
				if sec := toMap(src["secret"]); sec != nil {
					name := toString(sec["name"])
					if name != "" {
						refs = append(refs, graphRef{Kind: "Secret", Name: name, Reason: fmt.Sprintf("volume:%s", volName)})
					}
				}
			}
		}
	}

	for _, field := range []string{"containers", "initContainers", "ephemeralContainers"} {
		items, _, _ := unstructured.NestedSlice(spec, field)
		for _, item := range items {
			container := toMap(item)
			if container == nil {
				continue
			}
			cName := toString(container["name"])
			refs = append(refs, collectContainerRefs(container, cName)...)
		}
	}

	if pullSecrets, _, _ := unstructured.NestedSlice(spec, "imagePullSecrets"); len(pullSecrets) > 0 {
		for _, entry := range pullSecrets {
			secret := toMap(entry)
			if secret == nil {
				continue
			}
			name := toString(secret["name"])
			if name != "" {
				refs = append(refs, graphRef{Kind: "Secret", Name: name, Reason: "imagePullSecret"})
			}
		}
	}

	if saName := toString(spec["serviceAccountName"]); saName != "" {
		refs = append(refs, graphRef{Kind: "ServiceAccount", Name: saName, Reason: "serviceAccount"})
	}

	return refs
}

func collectContainerRefs(container map[string]interface{}, containerName string) []graphRef {
	var refs []graphRef
	if envVars, ok := container["env"].([]interface{}); ok {
		for _, envRaw := range envVars {
			env := toMap(envRaw)
			if env == nil {
				continue
			}
			envName := toString(env["name"])
			if valueFrom := toMap(env["valueFrom"]); valueFrom != nil {
				if cm := toMap(valueFrom["configMapKeyRef"]); cm != nil {
					name := toString(cm["name"])
					if name != "" {
						refs = append(refs, graphRef{Kind: "ConfigMap", Name: name, Reason: fmt.Sprintf("env:%s/%s", containerName, envName)})
					}
				}
				if sec := toMap(valueFrom["secretKeyRef"]); sec != nil {
					name := toString(sec["name"])
					if name != "" {
						refs = append(refs, graphRef{Kind: "Secret", Name: name, Reason: fmt.Sprintf("env:%s/%s", containerName, envName)})
					}
				}
			}
		}
	}
	if envFrom, ok := container["envFrom"].([]interface{}); ok {
		for _, entry := range envFrom {
			item := toMap(entry)
			if item == nil {
				continue
			}
			if cm := toMap(item["configMapRef"]); cm != nil {
				name := toString(cm["name"])
				if name != "" {
					refs = append(refs, graphRef{Kind: "ConfigMap", Name: name, Reason: fmt.Sprintf("envFrom:%s", containerName)})
				}
			}
			if sec := toMap(item["secretRef"]); sec != nil {
				name := toString(sec["name"])
				if name != "" {
					refs = append(refs, graphRef{Kind: "Secret", Name: name, Reason: fmt.Sprintf("envFrom:%s", containerName)})
				}
			}
		}
	}
	return refs
}

func toMap(val interface{}) map[string]interface{} {
	if m, ok := val.(map[string]interface{}); ok {
		return m
	}
	return nil
}

func toString(val interface{}) string {
	if s, ok := val.(string); ok {
		return s
	}
	return ""
}

func extractNodeMeta(doc manifestDoc) map[string]string {
	if doc.Obj == nil {
		return nil
	}
	meta := map[string]string{
		"kind": strings.ToLower(doc.Obj.GetKind()),
	}
	if ns := doc.Obj.GetNamespace(); ns != "" {
		meta["namespace"] = ns
	}
	kind := strings.ToLower(doc.Obj.GetKind())
	switch kind {
	case "deployment", "statefulset", "daemonset":
		if replicas, found, _ := unstructured.NestedInt64(doc.Obj.Object, "spec", "replicas"); found {
			meta["replicas"] = fmt.Sprintf("%d", replicas)
		}
		if containers, found, _ := unstructured.NestedSlice(doc.Obj.Object, "spec", "template", "spec", "containers"); found {
			meta["containers"] = fmt.Sprintf("%d", len(containers))
		}
	case "job", "cronjob":
		if parallelism, found, _ := unstructured.NestedInt64(doc.Obj.Object, "spec", "parallelism"); found {
			meta["parallelism"] = fmt.Sprintf("%d", parallelism)
		}
	case "configmap":
		if data, found, _ := unstructured.NestedMap(doc.Obj.Object, "data"); found {
			meta["keys"] = fmt.Sprintf("%d", len(data))
		}
	case "secret":
		if data, found, _ := unstructured.NestedMap(doc.Obj.Object, "data"); found {
			meta["keys"] = fmt.Sprintf("%d", len(data))
		}
	case "persistentvolumeclaim":
		if size, found, _ := unstructured.NestedString(doc.Obj.Object, "spec", "resources", "requests", "storage"); found && size != "" {
			meta["request"] = size
		}
	}
	return meta
}

func isWorkloadKind(kind string) bool {
	switch strings.ToLower(kind) {
	case "deployment", "statefulset", "daemonset", "job", "cronjob", "replicaset":
		return true
	}
	return false
}

func objectYAML(obj *unstructured.Unstructured) string {
	if obj == nil {
		return ""
	}
	trimmed := trimUnstructured(obj.DeepCopy())
	if trimmed == nil {
		return ""
	}
	data, err := yaml.Marshal(trimmed.Object)
	if err != nil {
		return ""
	}
	return string(data)
}

func trimUnstructured(obj *unstructured.Unstructured) *unstructured.Unstructured {
	if obj == nil {
		return nil
	}
	unstructured.RemoveNestedField(obj.Object, "metadata", "managedFields")
	unstructured.RemoveNestedField(obj.Object, "metadata", "resourceVersion")
	unstructured.RemoveNestedField(obj.Object, "metadata", "uid")
	unstructured.RemoveNestedField(obj.Object, "metadata", "selfLink")
	unstructured.RemoveNestedField(obj.Object, "metadata", "generation")
	unstructured.RemoveNestedField(obj.Object, "metadata", "creationTimestamp")
	unstructured.RemoveNestedField(obj.Object, "metadata", "annotations", "kubectl.kubernetes.io/last-applied-configuration")
	unstructured.RemoveNestedField(obj.Object, "status")
	return obj
}

func diffStrings(current, desired string) string {
	if current == desired {
		return ""
	}
	ud := difflib.UnifiedDiff{
		A:        difflib.SplitLines(current),
		B:        difflib.SplitLines(desired),
		FromFile: "live",
		ToFile:   "desired",
		Context:  3,
	}
	diff, err := difflib.GetUnifiedDiffString(ud)
	if err != nil {
		return fmt.Sprintf("failed to render diff: %v", err)
	}
	return diff
}

func summarizeGraphEdges(nodes []deployGraphNode, edges []deployGraphEdge) []string {
	if len(edges) == 0 {
		return nil
	}
	lookup := make(map[string]deployGraphNode, len(nodes))
	for _, node := range nodes {
		lookup[node.ID] = node
	}
	lines := make([]string, 0, len(edges))
	for _, edge := range edges {
		from := lookup[edge.From]
		to := lookup[edge.To]
		line := fmt.Sprintf("%s -> %s", formatGraphNodeLabel(from), formatGraphNodeLabel(to))
		if edge.Reason != "" {
			line = fmt.Sprintf("%s (%s)", line, edge.Reason)
		}
		lines = append(lines, line)
	}
	return lines
}

func formatGraphNodeLabel(node deployGraphNode) string {
	ns := node.Namespace
	if ns == "" {
		ns = "cluster"
	}
	return fmt.Sprintf("%s/%s (%s)", ns, node.Name, node.Kind)
}

func renderDeployPlan(out io.Writer, result *deployPlanResult) {
	if result == nil {
		return
	}
	namespace := result.Namespace
	if namespace == "" {
		namespace = "(context namespace)"
	}
	fmt.Fprintf(out, "Release %s @ %s\n", result.ReleaseName, namespace)
	if !result.GeneratedAt.IsZero() {
		fmt.Fprintf(out, "Generated at: %s\n", result.GeneratedAt.Format(time.RFC3339))
	}
	if result.ClusterHost != "" {
		fmt.Fprintf(out, "Cluster: %s\n", result.ClusterHost)
	}
	if result.ChartVersion != "" {
		fmt.Fprintf(out, "Chart version: %s\n", result.ChartVersion)
	}
	if result.ChartRef != "" {
		fmt.Fprintf(out, "Chart: %s\n", result.ChartRef)
	}
	if result.RequestedChart != "" && result.RequestedChart != result.ChartRef {
		fmt.Fprintf(out, "Requested chart: %s\n", result.RequestedChart)
	}
	if result.RequestedVersion != "" {
		fmt.Fprintf(out, "Requested version: %s\n", result.RequestedVersion)
	}
	if len(result.ValuesFiles) > 0 {
		fmt.Fprintf(out, "Values files:\n%s\n", indent(strings.Join(result.ValuesFiles, "\n"), "  - "))
	}
	if len(result.SetValues) > 0 {
		fmt.Fprintf(out, "Set values:\n%s\n", indent(strings.Join(result.SetValues, "\n"), "  - "))
	}
	if len(result.SetStringValues) > 0 {
		fmt.Fprintf(out, "Set-string values:\n%s\n", indent(strings.Join(result.SetStringValues, "\n"), "  - "))
	}
	if len(result.SetFileValues) > 0 {
		fmt.Fprintf(out, "Set-file values:\n%s\n", indent(strings.Join(result.SetFileValues, "\n"), "  - "))
	}
	if result.InstallCmd != "" {
		fmt.Fprintf(out, "Install command: %s\n", result.InstallCmd)
	}
	fmt.Fprintf(out, "Creates: %d, Updates: %d, Deletes: %d, Unchanged: %d\n\n", result.Summary.Creates, result.Summary.Updates, result.Summary.Deletes, result.Summary.Unchanged)

	if len(result.Changes) == 0 {
		fmt.Fprintln(out, "No changes detected.")
	} else {
		fmt.Fprintln(out, "Planned changes:")
		for _, change := range result.Changes {
			fmt.Fprintf(out, "- %s %s\n", planChangeLabel(change.Kind), change.Key.String())
			if change.Diff != "" {
				fmt.Fprintf(out, "%s\n", indent(change.Diff, "    "))
			}
		}
	}

	if len(result.Warnings) > 0 {
		fmt.Fprintln(out, "\nWarnings:")
		for _, warn := range result.Warnings {
			fmt.Fprintf(out, "- %s\n", warn)
		}
	}
	if result.DesiredQuota != nil {
		fmt.Fprintln(out, "\nDesired quota:")
		data, err := yaml.Marshal(result.DesiredQuota)
		if err == nil && len(data) > 0 {
			fmt.Fprintf(out, "%s\n", indent(strings.TrimRight(string(data), "\n"), "  "))
		}
	}
	if len(result.GraphEdges) > 0 {
		fmt.Fprintln(out, "\nResource dependencies:")
		for _, line := range summarizeGraphEdges(result.GraphNodes, result.GraphEdges) {
			fmt.Fprintf(out, "- %s\n", line)
		}
	}
	if len(result.GraphNodes) > 0 {
		fmt.Fprintln(out, "\nResources:")
		for _, node := range result.GraphNodes {
			ns := node.Namespace
			if ns == "" {
				ns = namespace
			}
			fmt.Fprintf(out, "- %s %s/%s (%s)\n", node.Kind, ns, node.Name, node.Source)
		}
	}
	writeStringMapSection(out, "\nRendered manifests:", result.ManifestBlobs)
	writeStringMapSection(out, "\nLive manifests:", result.LiveManifests)
	writeStringMapSection(out, "\nManifest diffs:", result.ManifestDiffs)
	writeStringMapSection(out, "\nTemplate sources:", result.TemplateSources)
	writeStringMapSection(out, "\nManifest templates:", result.ManifestTemplates)
	if result.OfflineFallback {
		fmt.Fprintln(out, "\nOffline fallback: true")
	}
}

func writeStringMapSection(out io.Writer, title string, m map[string]string) {
	if len(m) == 0 {
		return
	}
	fmt.Fprintln(out, title)
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := strings.TrimRight(m[k], "\n")
		if v == "" {
			continue
		}
		fmt.Fprintf(out, "- %s\n", k)
		fmt.Fprintf(out, "%s\n", indent(v, "    "))
	}
}

func planChangeLabel(kind planChangeKind) string {
	switch kind {
	case changeCreate:
		return "Create"
	case changeUpdate:
		return "Update"
	case changeDelete:
		return "Delete"
	default:
		return string(kind)
	}
}

func planChangeClass(kind planChangeKind) string {
	switch kind {
	case changeCreate:
		return "added"
	case changeUpdate:
		return "changed"
	case changeDelete:
		return "removed"
	default:
		return ""
	}
}

func indent(text, prefix string) string {
	if text == "" {
		return ""
	}
	lines := strings.Split(strings.TrimRight(text, "\n"), "\n")
	for i, line := range lines {
		lines[i] = prefix + line
	}
	return strings.Join(lines, "\n")
}

func renderDeployPlanHTML(result *deployPlanResult) (string, error) {
	if result == nil {
		return "", fmt.Errorf("plan result is empty")
	}
	namespace := result.Namespace
	if strings.TrimSpace(namespace) == "" {
		namespace = "(context namespace)"
	}
	planJSON, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("encode plan json: %w", err)
	}
	ctx := struct {
		*deployPlanResult
		NamespaceDisplay string
		HasChanges       bool
		PlanJSON         template.JS
		GraphSummaries   []string
	}{
		deployPlanResult: result,
		NamespaceDisplay: namespace,
		HasChanges:       len(result.Changes) > 0,
		PlanJSON:         template.JS(planJSON),
		GraphSummaries:   summarizeGraphEdges(result.GraphNodes, result.GraphEdges),
	}
	tmpl, err := template.New("deployPlanHTML").Funcs(template.FuncMap{
		"changeClass": planChangeClass,
		"changeLabel": planChangeLabel,
		"diffHTML":    diffStringToHTML,
	}).Parse(deployPlanHTMLTemplate)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, ctx); err != nil {
		return "", fmt.Errorf("render template: %w", err)
	}
	return buf.String(), nil
}

var planDataScriptRegex = regexp.MustCompile(`(?s)<script[^>]+id=["']ktlPlanData["'][^>]*>(.*?)</script>`)

// valuesDiffSummary reserves wiring for the upcoming values compare UI.
// The structure stays intentionally minimal so JSON payloads remain stable
// even before the diff data is populated.
type valuesDiffSummary struct {
	Added   []string `json:"added,omitempty"`
	Removed []string `json:"removed,omitempty"`
	Changed []string `json:"changed,omitempty"`
}

type deployVisualizePayload struct {
	Release           string                  `json:"release"`
	Namespace         string                  `json:"namespace"`
	Chart             string                  `json:"chart"`
	ClusterHost       string                  `json:"clusterHost,omitempty"`
	InstallCommand    string                  `json:"installCommand,omitempty"`
	ValuesFiles       []string                `json:"valuesFiles,omitempty"`
	SetValues         []string                `json:"setValues,omitempty"`
	SetStringValues   []string                `json:"setStringValues,omitempty"`
	SetFileValues     []string                `json:"setFileValues,omitempty"`
	Secrets           []planSecretRef         `json:"secrets,omitempty"`
	Nodes             []deployGraphNode       `json:"nodes"`
	Edges             []deployGraphEdge       `json:"edges"`
	Manifests         map[string]string       `json:"manifests"`
	LiveManifests     map[string]string       `json:"liveManifests,omitempty"`
	ManifestDiffs     map[string]string       `json:"manifestDiffs,omitempty"`
	ManifestTemplates map[string]string       `json:"manifestTemplates,omitempty"`
	TemplateSources   map[string]string       `json:"templateSources,omitempty"`
	ChangeKinds       map[string]string       `json:"changeKinds,omitempty"`
	CompareManifests  map[string]string       `json:"compareManifests,omitempty"`
	CompareSummary    string                  `json:"compareSummary,omitempty"`
	Summary           planSummary             `json:"summary,omitempty"`
	Warnings          []string                `json:"warnings,omitempty"`
	ValuesDiff        valuesDiffSummary       `json:"valuesDiff"`
	DesiredQuota      *quotaReport            `json:"desiredQuota,omitempty"`
	DesiredQuotaByNS  map[string]*quotaReport `json:"desiredQuotaByNamespace,omitempty"`
	GeneratedAt       time.Time               `json:"generatedAt,omitempty"`
	OfflineFallback   bool                    `json:"offlineFallback"`
}

type deployVisualizeFeatures struct {
	ExplainDiff bool `json:"explainDiff"`
}

func buildDeployVisualizePayload(result *deployPlanResult, compare *deployPlanResult) (deployVisualizePayload, error) {
	if result == nil {
		return deployVisualizePayload{}, fmt.Errorf("plan result is empty")
	}
	if len(result.GraphNodes) == 0 {
		return deployVisualizePayload{}, fmt.Errorf("no resources available to visualize (chart rendered zero objects)")
	}
	changeKinds := buildChangeKindIndex(result.Changes)
	payload := deployVisualizePayload{
		Release:          result.ReleaseName,
		Namespace:        result.Namespace,
		Chart:            result.ChartRef,
		ClusterHost:      result.ClusterHost,
		InstallCommand:   result.InstallCmd,
		ValuesFiles:      append([]string(nil), result.ValuesFiles...),
		SetValues:        append([]string(nil), result.SetValues...),
		SetStringValues:  append([]string(nil), result.SetStringValues...),
		SetFileValues:    append([]string(nil), result.SetFileValues...),
		Secrets:          append([]planSecretRef(nil), result.Secrets...),
		Nodes:            result.GraphNodes,
		Edges:            result.GraphEdges,
		Manifests:        result.ManifestBlobs,
		LiveManifests:    result.LiveManifests,
		ManifestDiffs:    result.ManifestDiffs,
		ChangeKinds:      changeKinds,
		Warnings:         append([]string(nil), result.Warnings...),
		Summary:          result.Summary,
		DesiredQuota:     result.DesiredQuota,
		DesiredQuotaByNS: result.DesiredQuotaByNS,
		GeneratedAt:      result.GeneratedAt,
		OfflineFallback:  result.OfflineFallback,
	}
	if compare != nil {
		payload.CompareManifests = compare.ManifestBlobs
		payload.CompareSummary = describePlanSummary(compare)
	}
	return payload, nil
}

func renderDeployVisualizeHTML(result *deployPlanResult, compare *deployPlanResult, features deployVisualizeFeatures) (string, error) {
	payload, err := buildDeployVisualizePayload(result, compare)
	if err != nil {
		return "", err
	}
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("encode viz payload: %w", err)
	}
	escaped := escapeJSONForScript(jsonData)
	featuresJSON, err := json.Marshal(features)
	if err != nil {
		return "", fmt.Errorf("encode viz features: %w", err)
	}
	html := strings.Replace(deployVisualizeHTMLTemplate, "__DATA__", escaped, 1)
	html = strings.Replace(html, "__FEATURES__", escapeJSONForScript(featuresJSON), 1)
	return html, nil
}

func defaultDeployVisualizeDataOutputPath(release string, generatedAt time.Time, ext string) string {
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	stamp := time.Now()
	if !generatedAt.IsZero() {
		stamp = generatedAt
	}
	if strings.TrimSpace(ext) == "" {
		ext = "json"
	}
	return fmt.Sprintf("ktl-deploy-visualize-%s-%s.%s", slug, stamp.Format("20060102-150405"), ext)
}

func escapeJSONForScript(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	var b strings.Builder
	b.Grow(len(data))
	for _, ch := range data {
		switch ch {
		case '<':
			b.WriteString(`\u003c`)
		case '>':
			b.WriteString(`\u003e`)
		case '&':
			b.WriteString(`\u0026`)
		default:
			b.WriteByte(byte(ch))
		}
	}
	return b.String()
}

func loadPlanResultFromFile(path string) (*deployPlanResult, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parsePlanDocument(data)
}

func loadPlanResultFromSource(ctx context.Context, source string) (*deployPlanResult, error) {
	if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, source, nil)
		if err != nil {
			return nil, err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("download %s: %s", source, resp.Status)
		}
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		return parsePlanDocument(data)
	}
	return loadPlanResultFromFile(source)
}

func parsePlanDocument(data []byte) (*deployPlanResult, error) {
	if res, err := parsePlanJSON(data); err == nil {
		return res, nil
	}
	return parsePlanHTML(data)
}

func parsePlanJSON(data []byte) (*deployPlanResult, error) {
	var result deployPlanResult
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, err
	}
	if strings.TrimSpace(result.ReleaseName) == "" {
		return nil, fmt.Errorf("plan is missing release metadata")
	}
	return &result, nil
}

func parsePlanHTML(data []byte) (*deployPlanResult, error) {
	matches := planDataScriptRegex.FindSubmatch(data)
	if len(matches) < 2 {
		return nil, fmt.Errorf("plan HTML does not embed ktlPlanData")
	}
	return parsePlanJSON(matches[1])
}

func buildChangeKindIndex(changes []planResourceChange) map[string]string {
	if len(changes) == 0 {
		return nil
	}
	result := make(map[string]string)
	for _, change := range changes {
		id := graphNodeID(change.Key)
		if id == "" {
			continue
		}
		result[id] = string(change.Kind)
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

func describePlanSummary(res *deployPlanResult) string {
	if res == nil {
		return ""
	}
	var parts []string
	if res.ReleaseName != "" {
		parts = append(parts, fmt.Sprintf("Release %s", res.ReleaseName))
	}
	if res.Namespace != "" {
		parts = append(parts, fmt.Sprintf("ns/%s", res.Namespace))
	}
	if res.ChartRef != "" {
		parts = append(parts, res.ChartRef)
	}
	if !res.GeneratedAt.IsZero() {
		parts = append(parts, res.GeneratedAt.Format("02 Jan 2006 15:04 MST"))
	}
	return strings.Join(parts, " · ")
}

func diffStringToHTML(diff string) template.HTML {
	if strings.TrimSpace(diff) == "" {
		return template.HTML("")
	}
	lines := strings.Split(diff, "\n")
	var b strings.Builder
	for i, line := range lines {
		if i == len(lines)-1 && line == "" {
			continue
		}
		classes := diffLineClasses(line)
		b.WriteString(`<span class="`)
		b.WriteString(strings.Join(classes, " "))
		b.WriteString(`">`)
		b.WriteString(html.EscapeString(line))
		b.WriteString(`</span>`)
		if i < len(lines)-1 {
			b.WriteString("\n")
		}
	}
	return template.HTML(b.String())
}

func diffLineClasses(line string) []string {
	classes := []string{"diff-line"}
	if len(line) > 0 {
		switch line[0] {
		case '+':
			classes = append(classes, "diff-line--added")
		case '-':
			classes = append(classes, "diff-line--removed")
		case '@':
			classes = append(classes, "diff-line--header")
		}
	}
	return classes
}

func sanitizeFilename(s string) string {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return ""
	}
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash && b.Len() > 0 {
			b.WriteRune('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func defaultDeployVisualizeOutputPath(release string, generatedAt time.Time) string {
	slug := sanitizeFilename(release)
	if slug == "" {
		slug = "release"
	}
	return fmt.Sprintf("ktl-deploy-visualize-%s-%s.html", slug, generatedAt.Format("20060102-150405"))
}

func writePlanBaseline(path string, result *deployPlanResult) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if result == nil {
		return fmt.Errorf("plan result is empty")
	}
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal baseline json: %w", err)
	}
	dir := filepath.Dir(path)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create baseline dir: %w", err)
		}
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

func buildInstallCommand(opts deployPlanOptions) string {
	parts := []string{"ktl", "deploy", "apply"}
	if opts.Chart != "" {
		parts = append(parts, "--chart", shellQuote(opts.Chart))
	}
	if opts.Release != "" {
		parts = append(parts, "--release", shellQuote(opts.Release))
	}
	if opts.Namespace != "" {
		parts = append(parts, "--namespace", shellQuote(opts.Namespace))
	}
	if opts.Version != "" {
		parts = append(parts, "--version", shellQuote(opts.Version))
	}
	for _, file := range opts.ValuesFiles {
		parts = append(parts, "--values", shellQuote(file))
	}
	for _, val := range opts.SetValues {
		parts = append(parts, "--set", shellQuote(val))
	}
	for _, val := range opts.SetStringValues {
		parts = append(parts, "--set-string", shellQuote(val))
	}
	for _, val := range opts.SetFileValues {
		parts = append(parts, "--set-file", shellQuote(val))
	}
	return strings.Join(parts, " ")
}

func shellQuote(val string) string {
	if val == "" {
		return "''"
	}
	replaced := strings.ReplaceAll(val, "'", "'\"'\"'")
	return "'" + replaced + "'"
}

const deployPlanHTMLTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>ktl Apply Plan</title>
  <style>
    :root {
      --surface: rgba(255,255,255,0.9);
      --surface-soft: rgba(255,255,255,0.82);
      --border: rgba(15,23,42,0.12);
      --text: #0f172a;
      --muted: rgba(15,23,42,0.65);
      --accent: #2563eb;
      --warn: #fbbf24;
      --fail: #ef4444;
    }
    * { box-sizing: border-box; }
    body {
      font-family: "SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0;
      min-height: 100vh;
      padding: 48px 56px 72px;
      background: radial-gradient(circle at 20% 20%, #ffffff, #e9edf5 45%, #dce3f1);
      color: var(--text);
    }
    .chrome { max-width: 1200px; margin: 0 auto; }
    header { margin-bottom: 32px; }
    .eyebrow {
      text-transform: uppercase;
      letter-spacing: 0.28em;
      font-size: 0.75rem;
      color: var(--muted);
      margin: 0 0 0.4rem;
    }
    h1 { font-size: 2.8rem; font-weight: 600; letter-spacing: -0.04em; margin: 0; }
    .subtitle { font-size: 1rem; color: var(--muted); margin-top: 0.35rem; }
    .layout { display:flex; gap:24px; align-items:flex-start; }
    .main-column { flex:1 1 auto; min-width:0; }
    .insight-stack { width:340px; position:sticky; top:32px; display:flex; flex-direction:column; gap:24px; }
    @media (max-width: 1100px) {
      .layout { flex-direction:column; }
      .insight-stack { width:100%; position:static; }
    }
    .panel {
      border-radius:28px;
      padding:32px;
      background:var(--surface);
      border:1px solid var(--border);
      box-shadow:0 40px 80px rgba(16,23,36,0.12);
      backdrop-filter: blur(18px);
    }
    .grid { display:grid; gap:1rem; grid-template-columns: repeat(auto-fit, minmax(160px,1fr)); }
    .warning-list { margin:0.5rem 0 0; padding-left:1.1rem; color:var(--warn); font-size:0.9rem; }
    .graph-list { list-style:none; margin:0.5rem 0 0; padding:0; font-size:0.9rem; color:var(--muted); }
    .graph-list li { padding:0.35rem 0; border-bottom:1px solid rgba(15,23,42,0.08); }
    .graph-list li:last-child { border-bottom:none; }
    .card { border-radius:24px; background:rgba(255,255,255,0.92); border:1px solid rgba(15,23,42,0.08); padding:1rem 1.2rem; }
    .card span { display:block; text-transform:uppercase; font-size:0.75rem; letter-spacing:0.2em; color:var(--muted); }
    .card strong { display:block; font-size:2rem; margin-top:0.35rem; letter-spacing:-0.04em; }
    .diff-panel { margin-top:32px; }
    .diff-header { display:flex; justify-content:space-between; align-items:flex-start; gap:1rem; flex-wrap:wrap; }
    .summary-meta { font-size:0.9rem; color:var(--muted); margin-top:0.2rem; }
    .diff-list { margin-top:1.5rem; display:flex; flex-direction:column; gap:18px; }
    .diff-item { border:1px solid var(--border); border-radius:24px; padding:1.4rem; background:var(--surface-soft); box-shadow: inset 0 1px 0 rgba(255,255,255,0.4); }
    .diff-item header { display:flex; justify-content:space-between; flex-wrap:wrap; gap:0.5rem; }
    .diff-item.added { border-left:4px solid #22c55e; }
    .diff-item.changed { border-left:4px solid var(--warn); }
    .diff-item.removed { border-left:4px solid var(--fail); }
    .diff-kind { text-transform:uppercase; font-size:0.8rem; letter-spacing:0.18em; color:var(--muted); }
    pre.diff-snippet {
      background:#0f172a;
      color:#e2e8f0;
      padding:1rem;
      border-radius:18px;
      overflow:auto;
      margin-top:1rem;
      font-size:0.85rem;
      line-height:1.4;
      font-family:"SFMono-Regular","JetBrains Mono","Menlo","Source Code Pro",monospace;
    }
    pre.diff-snippet .diff-line {
      display:block;
      white-space:pre;
      margin:0 -1rem;
      padding:0 1rem;
      border-left:4px solid transparent;
    }
    pre.diff-snippet .diff-line--added {
      color:#bbf7d0;
      background:rgba(34,197,94,0.15);
      border-left-color:#22c55e;
    }
    pre.diff-snippet .diff-line--removed {
      color:#fecaca;
      background:rgba(239,68,68,0.18);
      border-left-color:#ef4444;
    }
    pre.diff-snippet .diff-line--header {
      color:#fbbf24;
      font-weight:600;
    }
    .insight-panel {
      border-radius:24px;
      border:1px solid var(--border);
      background:var(--surface);
      padding:24px;
      box-shadow: 0 18px 40px rgba(15,23,42,0.12);
    }
    .graph-pane {
      display:flex;
      flex-direction:column;
      gap:16px;
      min-height:520px;
    }
    .graph-header {
      display:flex;
      justify-content:space-between;
      align-items:flex-start;
      gap:12px;
      flex-wrap:wrap;
    }
    .graph-legend {
      display:flex;
      gap:12px;
      flex-wrap:wrap;
      font-size:0.85rem;
      color:var(--muted);
    }
    .legend-item {
      display:flex;
      align-items:center;
      gap:6px;
    }
    .legend-dot {
      width:10px;
      height:10px;
      border-radius:50%;
      background:rgba(15,23,42,0.35);
      display:inline-flex;
    }
    .legend-dot.change-create { background:#22c55e; }
    .legend-dot.change-update { background:var(--warn); }
    .legend-dot.legend-unchanged { background:rgba(15,23,42,0.3); }
    .graph-canvas {
      position:relative;
      border:1px solid var(--border);
      border-radius:20px;
      background:#fff;
      min-height:520px;
      overflow:auto;
    }
    #graphEdgesLayer {
      position:absolute;
      top:0;
      left:0;
      width:100%;
      height:100%;
      pointer-events:none;
    }
    .graph-nodes {
      position:absolute;
      top:0;
      left:0;
    }
    .graph-node {
      position:absolute;
      transform:translate(-50%,-50%);
      padding:8px 14px;
      border-radius:999px;
      border:1px solid rgba(15,23,42,0.2);
      background:rgba(15,23,42,0.06);
      font-size:0.85rem;
      pointer-events:auto;
      cursor:pointer;
      transition:box-shadow 0.2s ease, transform 0.2s ease;
    }
    .graph-node:hover {
      transform:translate(-50%,-50%) scale(1.02);
      box-shadow:0 8px 18px rgba(15,23,42,0.16);
    }
    .graph-node.change-create {
      border-color:#22c55e;
      background:rgba(34,197,94,0.18);
    }
    .graph-node.change-update {
      border-color:var(--warn);
      background:rgba(251,191,36,0.18);
    }
    .graph-node.selected {
      box-shadow:0 0 0 3px rgba(37,99,235,0.3);
    }
    .graph-node.impact-upstream {
      box-shadow:0 0 0 3px rgba(249,115,22,0.35);
    }
    .graph-node.impact-downstream {
      box-shadow:0 0 0 3px rgba(14,165,233,0.35);
    }
    .graph-edge {
      fill:none;
      stroke:rgba(15,23,42,0.35);
      stroke-width:1.5;
    }
    .graph-edge.impact-upstream {
      stroke:#f97316;
      stroke-width:2;
    }
    .graph-edge.impact-downstream {
      stroke:#0ea5e9;
      stroke-width:2;
    }
    .insight-panel h3 { margin-top:0; margin-bottom:0.5rem; font-size:1.1rem; }
    .timeline { list-style:none; padding:0; margin:0; display:flex; flex-direction:column; gap:16px; }
    .timeline li { position:relative; padding-left:24px; font-size:0.95rem; color:var(--text); }
    .timeline li .dot {
      width:10px; height:10px; border-radius:50%; background:var(--accent);
      position:absolute; left:0; top:0.4rem;
    }
    .timeline li.warn .dot { background: var(--warn); }
    .timeline li.fail .dot { background: var(--fail); }
    .runbook-card pre.snippet {
      background:#0f172a;
      color:#e2e8f0;
      padding:0.8rem;
      border-radius:12px;
      overflow:auto;
      font-size:0.9rem;
    }
    .cta {
      border:none;
      border-radius:999px;
      background:var(--accent);
      color:#fff;
      font-size:0.9rem;
      padding:0.55rem 1.4rem;
      cursor:pointer;
      transition:box-shadow 0.2s ease, transform 0.2s ease;
    }
    .cta:hover { box-shadow:0 12px 24px rgba(37,99,235,0.25); transform:translateY(-1px); }
    .toast {
      position:fixed; bottom:24px; right:24px;
      padding:0.6rem 1.2rem;
      border-radius:12px;
      background:var(--surface);
      border:1px solid var(--border);
      box-shadow:0 12px 30px rgba(0,0,0,0.15);
      opacity:0; transform:translateY(10px);
      transition:opacity 0.2s ease, transform 0.2s ease;
      pointer-events:none;
    }
    .toast.visible { opacity:1; transform:translateY(0); }
    @media print {
      body { background:#fff; padding:24px; }
      .insight-stack { display:none; }
      .panel, .insight-panel { box-shadow:none !important; border-color:#000 !important; }
      .cta, #copyToast { display:none !important; }
    }
  </style>
</head>
<body>
  <div class="chrome">
    <header>
      <p class="eyebrow">ktl apply plan</p>
      <h1>Release {{.ReleaseName}}</h1>
      <div class="subtitle">Namespace <strong>{{.NamespaceDisplay}}</strong>{{if .ChartVersion}} · Chart {{.ChartVersion}}{{end}}{{if .ClusterHost}} · Cluster {{.ClusterHost}}{{end}}</div>
      <div class="subtitle">Generated {{.GeneratedAt.Format "02 Jan 2006 15:04 MST"}}</div>
    </header>
    <div class="layout">
      <div class="main-column">
        <section class="panel">
          <div class="grid">
            <div class="card"><span>Creates</span><strong>{{.Summary.Creates}}</strong></div>
            <div class="card"><span>Updates</span><strong>{{.Summary.Updates}}</strong></div>
            <div class="card"><span>Deletes</span><strong>{{.Summary.Deletes}}</strong></div>
            <div class="card"><span>Unchanged</span><strong>{{.Summary.Unchanged}}</strong></div>
          </div>
        </section>
        <section class="panel diff-panel">
          <div class="diff-header">
            <div>
              <h2>Planned changes</h2>
              <p class="summary-meta">{{len .Changes}} resources evaluated</p>
            </div>
          </div>
          {{if .HasChanges}}
          <div class="diff-list">
            {{range .Changes}}
            <article class="diff-item {{changeClass .Kind}}">
              <header>
                <div>
                  <h3 style="margin:0;">{{.Key.Kind}} · {{.Key.Name}}</h3>
                  <p class="summary-meta">{{.Key.String}}</p>
                </div>
                <span class="diff-kind">{{changeLabel .Kind}}</span>
              </header>
              {{if .Diff}}
              <pre class="diff-snippet">{{diffHTML .Diff}}</pre>
              {{end}}
            </article>
            {{end}}
          </div>
          {{else}}
          <p class="summary-meta diff-empty">No drift detected between the rendered chart and the cluster.</p>
          {{end}}
        </section>
      </div>
    </div>
  </div>
  <div id="copyToast" class="toast">Copied!</div>
  <script>
    (function(){
      const toast = document.getElementById('copyToast');
      function showToast(msg){
        if(!toast) { return; }
        toast.textContent = msg;
        toast.classList.add('visible');
        clearTimeout(showToast._timer);
        showToast._timer = setTimeout(() => toast.classList.remove('visible'), 1400);
      }
      document.querySelectorAll('.cta.copy').forEach(btn => {
        btn.addEventListener('click', async () => {
          const cmd = btn.getAttribute('data-command');
          if(!cmd) { return; }
          try {
            await navigator.clipboard.writeText(cmd);
            showToast('Command copied');
          } catch (err) {
            showToast('Unable to copy');
          }
        });
      });
    })();
  </script>
  <script id="ktlPlanData" type="application/json">{{.PlanJSON}}</script>
</body>
</html>`

//go:embed templates/deploy_visualize.html
var deployVisualizeHTMLTemplate string
