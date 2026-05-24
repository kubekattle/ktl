package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/ingresslabs/torque/internal/kube"
	hostfacts "github.com/ingresslabs/torque/internal/ops/facts/host"
	k8sfacts "github.com/ingresslabs/torque/internal/ops/facts/k8s"
	"github.com/ingresslabs/torque/internal/ops/inventory"
	"github.com/ingresslabs/torque/internal/ops/targetgraph"
	localtransport "github.com/ingresslabs/torque/internal/ops/transport/local"
	sshtransport "github.com/ingresslabs/torque/internal/ops/transport/ssh"
	"github.com/spf13/cobra"
	"k8s.io/client-go/kubernetes"
)

type opsKubeFactClient struct {
	Clientset kubernetes.Interface
	Namespace string
}

var newOpsKubeFactClient = func(ctx context.Context, kubeconfigPath, kubeContext string) (opsKubeFactClient, error) {
	client, err := kube.New(ctx, kubeconfigPath, kubeContext)
	if err != nil {
		return opsKubeFactClient{}, err
	}
	return opsKubeFactClient{Clientset: client.Clientset, Namespace: client.Namespace}, nil
}

func newOpsFactsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "facts",
		Short: "Collect and compare ops target facts",
	}
	cmd.AddCommand(newOpsFactsCollectCommand(), newOpsFactsDiffCommand())
	decorateCommandHelp(cmd, "Facts Flags")
	return cmd
}

func newOpsFactsCollectCommand() *cobra.Command {
	var targetGraphPath string
	var selectorValues []string
	var groups []string
	var limit int
	var cacheDir string
	var cacheOnly bool
	var timeout time.Duration
	var ttlOverride time.Duration
	var outputPath string
	var outDir string
	var namespaces []string
	var allNamespaces bool
	var eventLimit int
	var workloadSamples int
	format := "table"

	cmd := &cobra.Command{
		Use:   "collect",
		Short: "Collect host and Kubernetes facts for selected ops targets",
		Example: `  torque ops facts collect --targets ./targetgraph.yaml --selector role=web
  torque ops facts collect --targets ./targetgraph.yaml --group web --cache-dir ./.torque/ops/facts --format json
  torque ops facts collect --targets ./targetgraph.yaml --selector role=web --out-dir ./ops-facts-evidence
  torque ops facts collect --targets ./targetgraph.yaml --selector kind=k8s --namespace prod --format json
  torque ops facts collect --targets ./targetgraph.yaml --cache-dir ./.torque/ops/facts --cache-only`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops facts collect does not accept positional arguments")
			}
			if strings.TrimSpace(targetGraphPath) == "" {
				return fmt.Errorf("--targets is required")
			}
			selector, err := inventory.ParseSelector(selectorValues)
			if err != nil {
				return err
			}
			graph, err := targetgraph.LoadFile(targetGraphPath)
			if err != nil {
				return err
			}
			result, err := collectOpsFacts(cmd, graph, opsFactsCollectOptions{
				Selector:        selector,
				Groups:          groups,
				Limit:           limit,
				CacheDir:        cacheDir,
				CacheOnly:       cacheOnly,
				Timeout:         timeout,
				TTLOverride:     ttlOverride,
				Namespaces:      namespaces,
				AllNamespaces:   allNamespaces,
				EventLimit:      eventLimit,
				WorkloadSamples: workloadSamples,
			})
			if err != nil {
				return err
			}
			if strings.TrimSpace(outDir) != "" {
				result.EvidenceDir = strings.TrimSpace(outDir)
				if _, err := writeOpsFactsEvidenceBundle(outDir, result); err != nil {
					return err
				}
			}
			if strings.TrimSpace(outputPath) != "" {
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
					return err
				}
				if err := os.WriteFile(outputPath, append(raw, '\n'), 0o644); err != nil {
					return err
				}
				return opsFactsCollectStatusError(result)
			}
			switch format {
			case "json":
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				if _, err := fmt.Fprintln(cmd.OutOrStdout(), string(raw)); err != nil {
					return err
				}
				return opsFactsCollectStatusError(result)
			case "table", "":
				if err := renderOpsFactsCollectTable(cmd.OutOrStdout(), result); err != nil {
					return err
				}
				return opsFactsCollectStatusError(result)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&targetGraphPath, "targets", "", "TargetGraph YAML file")
	cmd.Flags().StringArrayVar(&selectorValues, "selector", nil, "Target label selector as key=value (repeatable)")
	cmd.Flags().StringArrayVar(&groups, "group", nil, "Target group to include (repeatable)")
	cmd.Flags().IntVar(&limit, "limit", 0, "Maximum selected targets to collect")
	cmd.Flags().StringVar(&cacheDir, "cache-dir", "", "Directory for host fact cache entries")
	cmd.Flags().BoolVar(&cacheOnly, "cache-only", false, "Read cached facts and freshness decisions without refreshing")
	cmd.Flags().DurationVar(&timeout, "timeout", 30*time.Second, "Timeout per transport command")
	cmd.Flags().DurationVar(&ttlOverride, "ttl", 0, "Override target fact TTL")
	cmd.Flags().StringArrayVarP(&namespaces, "namespace", "n", nil, "Kubernetes namespace to collect (repeatable; defaults to active context)")
	cmd.Flags().BoolVar(&allNamespaces, "all-namespaces", false, "Collect Kubernetes namespace-scoped facts from all namespaces")
	cmd.Flags().IntVar(&eventLimit, "event-limit", 25, "Maximum Kubernetes events to include per snapshot")
	cmd.Flags().IntVar(&workloadSamples, "workload-samples", 25, "Maximum Kubernetes workload sample rows per snapshot")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	cmd.Flags().StringVar(&outputPath, "output", "", "Write JSON output to a file instead of stdout")
	cmd.Flags().StringVar(&outDir, "out-dir", "", "Write a redacted facts evidence bundle to a directory")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Facts Collect Flags")
	return cmd
}

func newOpsFactsDiffCommand() *cobra.Command {
	var fromPath string
	var toPath string
	format := "table"

	cmd := &cobra.Command{
		Use:   "diff",
		Short: "Compare two fact snapshots or collections",
		Example: `  torque ops facts diff --from ./facts-before.json --to ./facts-after.json
  torque ops facts diff --from ./facts-before.json --to ./facts-after.json --format json`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) != 0 {
				return fmt.Errorf("torque ops facts diff does not accept positional arguments")
			}
			if strings.TrimSpace(fromPath) == "" || strings.TrimSpace(toPath) == "" {
				return fmt.Errorf("--from and --to are required")
			}
			from, err := loadOpsFactSnapshots(fromPath)
			if err != nil {
				return err
			}
			to, err := loadOpsFactSnapshots(toPath)
			if err != nil {
				return err
			}
			result := diffOpsFactSnapshots(fromPath, toPath, from, to)
			switch format {
			case "json":
				raw, err := json.MarshalIndent(result, "", "  ")
				if err != nil {
					return err
				}
				_, err = fmt.Fprintln(cmd.OutOrStdout(), string(raw))
				return err
			case "table", "":
				return renderOpsFactsDiffTable(cmd.OutOrStdout(), result)
			default:
				return fmt.Errorf("--format must be table or json")
			}
		},
	}
	cmd.Flags().StringVar(&fromPath, "from", "", "Earlier fact snapshot, cache entry, resolve result, or collection JSON")
	cmd.Flags().StringVar(&toPath, "to", "", "Later fact snapshot, cache entry, resolve result, or collection JSON")
	cmd.Flags().StringVar(&format, "format", format, "Output format: table or json")
	_ = cmd.RegisterFlagCompletionFunc("format", cobra.FixedCompletions([]string{"table", "json"}, cobra.ShellCompDirectiveNoFileComp))
	decorateCommandHelp(cmd, "Facts Diff Flags")
	return cmd
}

const (
	opsFactsAPIVersion     = "torque.dev/ops/facts/v1alpha1"
	opsFactsCollectionKind = "FactCollection"
	opsFactsDiffKind       = "FactDiff"
)

type opsFactsCollectOptions struct {
	Selector        map[string]string
	Groups          []string
	Limit           int
	CacheDir        string
	CacheOnly       bool
	Timeout         time.Duration
	TTLOverride     time.Duration
	Namespaces      []string
	AllNamespaces   bool
	EventLimit      int
	WorkloadSamples int
}

type opsFactsCollectResult struct {
	APIVersion  string                      `json:"apiVersion"`
	Kind        string                      `json:"kind"`
	GraphName   string                      `json:"graphName"`
	Selection   targetgraph.SelectionResult `json:"selection"`
	CacheDir    string                      `json:"cacheDir,omitempty"`
	EvidenceDir string                      `json:"evidenceDir,omitempty"`
	Results     []opsFactsTargetResult      `json:"results"`
	Summary     opsFactsCollectSummary      `json:"summary"`
}

type opsFactsCollectSummary struct {
	Selected  int `json:"selected"`
	Collected int `json:"collected"`
	Cached    int `json:"cached"`
	Blocked   int `json:"blocked"`
	Failed    int `json:"failed"`
	Skipped   int `json:"skipped"`
}

type opsFactsTargetResult struct {
	TargetID      string                       `json:"targetId"`
	TargetType    string                       `json:"targetType"`
	TransportKind string                       `json:"transportKind,omitempty"`
	Status        string                       `json:"status"`
	Source        string                       `json:"source,omitempty"`
	CachePath     string                       `json:"cachePath,omitempty"`
	Decision      *hostfacts.FreshnessDecision `json:"decision,omitempty"`
	Snapshot      *hostfacts.Snapshot          `json:"snapshot,omitempty"`
	K8sSnapshot   *k8sfacts.Snapshot           `json:"k8sSnapshot,omitempty"`
	Error         string                       `json:"error,omitempty"`
}

type opsFactsDiffResult struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	From       string              `json:"from"`
	To         string              `json:"to"`
	Changes    []opsFactsDiffEntry `json:"changes"`
	Summary    opsFactsDiffSummary `json:"summary"`
}

type opsFactsDiffEntry struct {
	TargetID      string   `json:"targetId"`
	SnapshotKind  string   `json:"snapshotKind,omitempty"`
	Status        string   `json:"status"`
	FromDigest    string   `json:"fromDigest,omitempty"`
	ToDigest      string   `json:"toDigest,omitempty"`
	ChangedFields []string `json:"changedFields,omitempty"`
}

type opsFactsDiffSummary struct {
	Targets   int `json:"targets"`
	Unchanged int `json:"unchanged"`
	Changed   int `json:"changed"`
	Added     int `json:"added"`
	Removed   int `json:"removed"`
}

type opsFactsEvidenceManifest struct {
	APIVersion  string                   `json:"apiVersion"`
	Kind        string                   `json:"kind"`
	GeneratedAt string                   `json:"generatedAt"`
	GraphName   string                   `json:"graphName"`
	Files       []opsFactsEvidenceFile   `json:"files"`
	Summary     opsFactsCollectSummary   `json:"summary"`
	Redaction   opsFactsRedactionSummary `json:"redaction"`
}

type opsFactsEvidenceFile struct {
	Path   string `json:"path"`
	Kind   string `json:"kind"`
	Digest string `json:"digest"`
	Bytes  int    `json:"bytes"`
}

type opsFactsEvidenceSelection struct {
	APIVersion string                      `json:"apiVersion"`
	Kind       string                      `json:"kind"`
	GraphName  string                      `json:"graphName"`
	Selection  targetgraph.SelectionResult `json:"selection"`
	Summary    opsFactsCollectSummary      `json:"summary"`
}

type opsFactsEvidenceTargets struct {
	APIVersion string                   `json:"apiVersion"`
	Kind       string                   `json:"kind"`
	Targets    []opsFactsEvidenceTarget `json:"targets"`
}

type opsFactsEvidenceTarget struct {
	TargetID      string `json:"targetId"`
	TargetType    string `json:"targetType"`
	TransportKind string `json:"transportKind,omitempty"`
	Status        string `json:"status"`
	Source        string `json:"source,omitempty"`
	SnapshotKind  string `json:"snapshotKind,omitempty"`
	Digest        string `json:"digest,omitempty"`
	CachePath     string `json:"cachePath,omitempty"`
	Error         string `json:"error,omitempty"`
}

type opsFactsEvidenceFreshness struct {
	APIVersion      string                           `json:"apiVersion"`
	Kind            string                           `json:"kind"`
	GeneratedAt     string                           `json:"generatedAt"`
	HostDecisions   []hostfacts.FreshnessDecision    `json:"hostDecisions,omitempty"`
	K8sObservations []opsFactsK8sObservation         `json:"k8sObservations,omitempty"`
	Summary         opsFactsEvidenceFreshnessSummary `json:"summary"`
}

type opsFactsK8sObservation struct {
	TargetID       string   `json:"targetId"`
	SnapshotDigest string   `json:"snapshotDigest"`
	ObservedAt     string   `json:"observedAt"`
	Namespaces     []string `json:"namespaces,omitempty"`
	Status         string   `json:"status"`
}

type opsFactsEvidenceFreshnessSummary struct {
	HostDecisions   int `json:"hostDecisions"`
	K8sObservations int `json:"k8sObservations"`
	Blocked         int `json:"blocked"`
	Failed          int `json:"failed"`
}

type opsFactsRedactionProof struct {
	APIVersion  string                   `json:"apiVersion"`
	Kind        string                   `json:"kind"`
	GeneratedAt string                   `json:"generatedAt"`
	Status      string                   `json:"status"`
	Files       []opsFactsRedactionFile  `json:"files"`
	Summary     opsFactsRedactionSummary `json:"summary"`
}

type opsFactsRedactionFile struct {
	Path              string `json:"path"`
	Bytes             int    `json:"bytes"`
	SecretRefFindings int    `json:"secretRefFindings"`
}

type opsFactsRedactionSummary struct {
	Files             int `json:"files"`
	SecretRefFindings int `json:"secretRefFindings"`
}

func collectOpsFacts(cmd *cobra.Command, graph *targetgraph.TargetGraph, opts opsFactsCollectOptions) (opsFactsCollectResult, error) {
	selection, err := graph.ResolveSelection(targetgraph.SelectionRequest{
		Selector: opts.Selector,
		Groups:   opts.Groups,
		Limit:    opts.Limit,
	})
	if err != nil {
		return opsFactsCollectResult{}, err
	}
	result := opsFactsCollectResult{
		APIVersion: opsFactsAPIVersion,
		Kind:       opsFactsCollectionKind,
		GraphName:  graph.Metadata.Name,
		Selection:  selection,
		CacheDir:   strings.TrimSpace(opts.CacheDir),
		Summary: opsFactsCollectSummary{
			Selected: len(selection.MatchedTargetIDs),
		},
	}
	for _, targetID := range selection.MatchedTargetIDs {
		target, ok := opsTargetByID(graph, targetID)
		if !ok {
			result.Results = append(result.Results, opsFactsTargetResult{
				TargetID: targetID,
				Status:   "failed",
				Error:    "selection referenced unknown target",
			})
			result.Summary.Failed++
			continue
		}
		targetResult := collectOpsFactsForTarget(cmd, graph, target, opts)
		result.Results = append(result.Results, targetResult)
		switch targetResult.Status {
		case "collected":
			result.Summary.Collected++
		case "cached":
			result.Summary.Cached++
		case "blocked":
			result.Summary.Blocked++
		case "skipped":
			result.Summary.Skipped++
		default:
			result.Summary.Failed++
		}
	}
	return result, nil
}

func writeOpsFactsEvidenceBundle(outDir string, result opsFactsCollectResult) (opsFactsEvidenceManifest, error) {
	outDir = strings.TrimSpace(outDir)
	if outDir == "" {
		return opsFactsEvidenceManifest{}, fmt.Errorf("facts evidence out dir is required")
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return opsFactsEvidenceManifest{}, fmt.Errorf("create facts evidence dir: %w", err)
	}
	generatedAt := time.Now().UTC().Format(time.RFC3339)
	var files []opsFactsEvidenceFile
	factsFile, err := writeOpsFactsEvidenceJSON(outDir, "facts.json", opsFactsCollectionKind, result)
	if err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	files = append(files, factsFile)
	selectionFile, err := writeOpsFactsEvidenceJSON(outDir, "selection.json", "FactSelection", opsFactsEvidenceSelection{
		APIVersion: opsFactsAPIVersion,
		Kind:       "FactSelection",
		GraphName:  result.GraphName,
		Selection:  result.Selection,
		Summary:    result.Summary,
	})
	if err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	files = append(files, selectionFile)
	targetsFile, err := writeOpsFactsEvidenceJSON(outDir, "targets.json", "FactTargetReceipts", opsFactsEvidenceTargets{
		APIVersion: opsFactsAPIVersion,
		Kind:       "FactTargetReceipts",
		Targets:    opsFactsEvidenceTargetsFromResult(result),
	})
	if err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	files = append(files, targetsFile)
	freshness := opsFactsEvidenceFreshnessFromResult(result, generatedAt)
	freshnessFile, err := writeOpsFactsEvidenceJSON(outDir, "freshness.json", freshness.Kind, freshness)
	if err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	files = append(files, freshnessFile)
	redaction := scanOpsFactsEvidenceRedaction(outDir, files, generatedAt)
	redactionFile, err := writeOpsFactsEvidenceJSON(outDir, "redaction.proof.json", redaction.Kind, redaction)
	if err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	files = append(files, redactionFile)
	manifest := opsFactsEvidenceManifest{
		APIVersion:  opsFactsAPIVersion,
		Kind:        "FactEvidenceManifest",
		GeneratedAt: generatedAt,
		GraphName:   result.GraphName,
		Files:       files,
		Summary:     result.Summary,
		Redaction:   redaction.Summary,
	}
	if _, err := writeOpsFactsEvidenceJSON(outDir, "manifest.json", manifest.Kind, manifest); err != nil {
		return opsFactsEvidenceManifest{}, err
	}
	if redaction.Summary.SecretRefFindings > 0 {
		return manifest, fmt.Errorf("facts evidence redaction failed: %d secret references remained", redaction.Summary.SecretRefFindings)
	}
	return manifest, nil
}

func writeOpsFactsEvidenceJSON(outDir, name, kind string, value any) (opsFactsEvidenceFile, error) {
	raw, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return opsFactsEvidenceFile{}, fmt.Errorf("marshal %s: %w", name, err)
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(outDir, name), raw, 0o644); err != nil {
		return opsFactsEvidenceFile{}, fmt.Errorf("write %s: %w", name, err)
	}
	return opsFactsEvidenceFile{
		Path:   name,
		Kind:   kind,
		Digest: opsFactsDigest(raw),
		Bytes:  len(raw),
	}, nil
}

func opsFactsEvidenceTargetsFromResult(result opsFactsCollectResult) []opsFactsEvidenceTarget {
	targets := make([]opsFactsEvidenceTarget, 0, len(result.Results))
	for _, item := range result.Results {
		targets = append(targets, opsFactsEvidenceTarget{
			TargetID:      item.TargetID,
			TargetType:    item.TargetType,
			TransportKind: item.TransportKind,
			Status:        item.Status,
			Source:        item.Source,
			SnapshotKind:  opsFactsSnapshotKind(item),
			Digest:        opsFactsSnapshotDigest(item),
			CachePath:     item.CachePath,
			Error:         inventory.RedactSecretRefs(item.Error),
		})
	}
	return targets
}

func opsFactsEvidenceFreshnessFromResult(result opsFactsCollectResult, generatedAt string) opsFactsEvidenceFreshness {
	out := opsFactsEvidenceFreshness{
		APIVersion:  opsFactsAPIVersion,
		Kind:        "FactFreshnessEvidence",
		GeneratedAt: generatedAt,
	}
	for _, item := range result.Results {
		if item.Decision != nil {
			out.HostDecisions = append(out.HostDecisions, *item.Decision)
			if item.Decision.Blocked {
				out.Summary.Blocked++
			}
		}
		if item.K8sSnapshot != nil {
			status := "observed"
			if item.Status != "collected" {
				status = item.Status
			}
			out.K8sObservations = append(out.K8sObservations, opsFactsK8sObservation{
				TargetID:       item.TargetID,
				SnapshotDigest: firstNonEmpty(item.K8sSnapshot.Digest, item.K8sSnapshot.StableDigest()),
				ObservedAt:     item.K8sSnapshot.ObservedAt,
				Namespaces:     append([]string(nil), item.K8sSnapshot.Workloads.Namespaces...),
				Status:         status,
			})
		}
		if item.Status == "failed" {
			out.Summary.Failed++
		}
	}
	out.Summary.HostDecisions = len(out.HostDecisions)
	out.Summary.K8sObservations = len(out.K8sObservations)
	return out
}

func scanOpsFactsEvidenceRedaction(outDir string, files []opsFactsEvidenceFile, generatedAt string) opsFactsRedactionProof {
	proof := opsFactsRedactionProof{
		APIVersion:  opsFactsAPIVersion,
		Kind:        "FactRedactionProof",
		GeneratedAt: generatedAt,
		Status:      "passed",
	}
	for _, file := range files {
		raw, err := os.ReadFile(filepath.Join(outDir, file.Path))
		findingCount := 0
		if err == nil {
			findingCount = bytes.Count(raw, []byte("secret://"))
		} else {
			findingCount = 1
		}
		proof.Files = append(proof.Files, opsFactsRedactionFile{
			Path:              file.Path,
			Bytes:             len(raw),
			SecretRefFindings: findingCount,
		})
		proof.Summary.SecretRefFindings += findingCount
	}
	proof.Summary.Files = len(proof.Files)
	if proof.Summary.SecretRefFindings > 0 {
		proof.Status = "failed"
	}
	return proof
}

func opsFactsSnapshotKind(item opsFactsTargetResult) string {
	if item.Snapshot != nil {
		return firstNonEmpty(item.Snapshot.Kind, hostfacts.Kind)
	}
	if item.K8sSnapshot != nil {
		return firstNonEmpty(item.K8sSnapshot.Kind, k8sfacts.Kind)
	}
	return ""
}

func opsFactsSnapshotDigest(item opsFactsTargetResult) string {
	if item.Snapshot != nil {
		return firstNonEmpty(item.Snapshot.Digest, item.Snapshot.StableDigest())
	}
	if item.K8sSnapshot != nil {
		return firstNonEmpty(item.K8sSnapshot.Digest, item.K8sSnapshot.StableDigest())
	}
	return ""
}

func opsFactsDigest(raw []byte) string {
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func collectOpsFactsForTarget(cmd *cobra.Command, graph *targetgraph.TargetGraph, target targetgraph.Target, opts opsFactsCollectOptions) opsFactsTargetResult {
	if opsTargetIsKubernetes(target) {
		return collectOpsK8sFactsForTarget(cmd, graph, target, opts)
	}
	transportClient, transportKind, err := opsFactTransport(graph, target, opts.Timeout)
	if err != nil {
		return opsFactsTargetResult{
			TargetID:   target.ID,
			TargetType: target.Type,
			Status:     "skipped",
			Error:      opsFactsError(err),
		}
	}
	ttl, err := opsFactTTL(target, opts.TTLOverride)
	if err != nil {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: transportKind,
			Status:        "failed",
			Error:         opsFactsError(err),
		}
	}
	request := hostfacts.CollectRequest{
		TargetID: target.ID,
		TTL:      ttl,
	}
	if strings.TrimSpace(opts.CacheDir) != "" {
		resolved, err := hostfacts.Resolve(cmd.Context(), transportClient, request, hostfacts.ResolveOptions{
			CacheDir: opts.CacheDir,
			Refresh:  !opts.CacheOnly,
		})
		if err != nil {
			return opsFactsTargetResult{
				TargetID:      target.ID,
				TargetType:    target.Type,
				TransportKind: transportKind,
				Status:        "failed",
				Error:         opsFactsError(err),
			}
		}
		status := "blocked"
		if resolved.Snapshot != nil {
			if resolved.Refreshed {
				status = "collected"
			} else if resolved.Decision.Fresh {
				status = "cached"
			}
		}
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: transportKind,
			Status:        status,
			Source:        resolved.Source,
			CachePath:     resolved.CachePath,
			Decision:      &resolved.Decision,
			Snapshot:      resolved.Snapshot,
		}
	}
	snapshot, err := hostfacts.Collect(cmd.Context(), transportClient, request)
	if err != nil {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: transportKind,
			Status:        "failed",
			Source:        "live",
			Error:         opsFactsError(err),
		}
	}
	return opsFactsTargetResult{
		TargetID:      target.ID,
		TargetType:    target.Type,
		TransportKind: transportKind,
		Status:        "collected",
		Source:        "live",
		Snapshot:      snapshot,
	}
}

func collectOpsK8sFactsForTarget(cmd *cobra.Command, graph *targetgraph.TargetGraph, target targetgraph.Target, opts opsFactsCollectOptions) opsFactsTargetResult {
	if opts.CacheOnly {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: "kubernetes",
			Status:        "blocked",
			Error:         "kubernetes fact collection requires a live API read; cache-only is only available for host facts",
		}
	}
	kubeconfigPath, kubeContext, transportKind, err := opsK8sFactConnection(graph, target, cmd)
	if err != nil {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: firstNonEmpty(transportKind, "kubernetes"),
			Status:        "skipped",
			Error:         opsFactsError(err),
		}
	}
	client, err := newOpsKubeFactClient(cmd.Context(), kubeconfigPath, kubeContext)
	if err != nil {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: firstNonEmpty(transportKind, "kubernetes"),
			Status:        "failed",
			Error:         opsFactsError(err),
		}
	}
	namespaces := opsK8sFactNamespaces(target, opts.Namespaces, client.Namespace)
	namespaceScope := strings.Join(namespaces, ",")
	if opts.AllNamespaces {
		namespaceScope = "*"
	}
	snapshot, err := k8sfacts.Collect(cmd.Context(), client.Clientset, k8sfacts.CollectRequest{
		TargetID:        target.ID,
		TargetDigest:    k8sfacts.TargetDigest(strings.Join([]string{target.ID, target.TransportRef, kubeContext, namespaceScope}, "\x00")),
		Namespaces:      namespaces,
		AllNamespaces:   opts.AllNamespaces,
		EventLimit:      opts.EventLimit,
		WorkloadSamples: opts.WorkloadSamples,
	})
	if err != nil {
		return opsFactsTargetResult{
			TargetID:      target.ID,
			TargetType:    target.Type,
			TransportKind: firstNonEmpty(transportKind, "kubernetes"),
			Status:        "failed",
			Source:        "kubernetes-api",
			Error:         opsFactsError(err),
		}
	}
	return opsFactsTargetResult{
		TargetID:      target.ID,
		TargetType:    target.Type,
		TransportKind: firstNonEmpty(transportKind, "kubernetes"),
		Status:        "collected",
		Source:        "kubernetes-api",
		K8sSnapshot:   snapshot,
	}
}

func opsFactTransport(graph *targetgraph.TargetGraph, target targetgraph.Target, timeout time.Duration) (hostfacts.Transport, string, error) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	redactValues := opsTargetRedactValues(graph, target)
	if target.Type == "local" {
		client, err := localtransport.New(localtransport.Config{
			Target:       firstNonEmpty(target.TransportRef, target.ID),
			Timeout:      timeout,
			RedactValues: redactValues,
		})
		return client, "local", err
	}
	if target.Type != "host" {
		return nil, "", fmt.Errorf("target type %q is not supported by host fact collection", target.Type)
	}
	ref := strings.TrimSpace(target.TransportRef)
	if strings.HasPrefix(ref, "local://") {
		client, err := localtransport.New(localtransport.Config{
			Target:       ref,
			Timeout:      timeout,
			RedactValues: redactValues,
		})
		return client, "local", err
	}
	if strings.HasPrefix(ref, "ssh://") {
		client, err := sshtransport.New(sshtransport.Config{
			Target:       ref,
			Timeout:      timeout,
			RedactValues: redactValues,
		})
		return client, "ssh", err
	}
	transportRef, ok := opsTransportByID(graph, ref)
	if !ok {
		return nil, "", fmt.Errorf("target %q transportRef %q was not found", target.ID, ref)
	}
	switch transportRef.Kind {
	case "local":
		client, err := localtransport.New(localtransport.Config{
			Target:       firstNonEmpty(transportRef.URL, transportRef.Host, target.ID),
			Timeout:      timeout,
			RedactValues: redactValues,
		})
		return client, "local", err
	case "ssh":
		sshTarget := strings.TrimSpace(transportRef.Host)
		if sshTarget == "" {
			return nil, "ssh", fmt.Errorf("ssh transport %q requires host", transportRef.ID)
		}
		if transportRef.User != "" && !strings.Contains(sshTarget, "@") {
			sshTarget = transportRef.User + "@" + sshTarget
		}
		client, err := sshtransport.New(sshtransport.Config{
			Target:       sshTarget,
			IdentityFile: opsConfigString(transportRef.Config, "identityFile", "identity_file"),
			Timeout:      timeout,
			RedactValues: redactValues,
		})
		return client, "ssh", err
	default:
		return nil, transportRef.Kind, fmt.Errorf("transport kind %q is not supported by host fact collection", transportRef.Kind)
	}
}

func opsTargetIsKubernetes(target targetgraph.Target) bool {
	switch strings.ToLower(strings.TrimSpace(target.Type)) {
	case "kubernetes.cluster", "kubernetes.namespace", "k8s.cluster", "k8s.namespace", "kubernetes", "k8s":
		return true
	default:
		return false
	}
}

func opsK8sFactConnection(graph *targetgraph.TargetGraph, target targetgraph.Target, cmd *cobra.Command) (string, string, string, error) {
	kubeconfigPath := opsRootFlag(cmd, "kubeconfig")
	kubeContext := opsRootFlag(cmd, "context")
	transportKind := "kubernetes"
	ref := strings.TrimSpace(target.TransportRef)
	if ref == "" {
		return kubeconfigPath, kubeContext, transportKind, nil
	}
	if strings.HasPrefix(ref, "secret://") && kubeconfigPath == "" {
		return "", "", transportKind, fmt.Errorf("target uses a secret kubeconfig reference; pass --kubeconfig or materialize it outside evidence")
	}
	if strings.HasPrefix(ref, "kubeconfig://") && kubeconfigPath == "" {
		return strings.TrimPrefix(ref, "kubeconfig://"), kubeContext, transportKind, nil
	}
	transportRef, ok := opsTransportByID(graph, ref)
	if !ok {
		if kubeconfigPath != "" {
			return kubeconfigPath, kubeContext, transportKind, nil
		}
		return "", "", transportKind, fmt.Errorf("target %q transportRef %q was not found", target.ID, ref)
	}
	transportKind = firstNonEmpty(transportRef.Kind, transportKind)
	if kubeContext == "" {
		kubeContext = opsConfigString(transportRef.Config, "context", "kubeContext", "kube_context")
	}
	if kubeconfigPath == "" {
		kubeconfigPath = firstNonEmpty(
			opsConfigString(transportRef.Config, "kubeconfig", "kubeconfigPath", "kubeconfig_path"),
			transportRef.KubeconfigRef,
		)
	}
	if strings.HasPrefix(kubeconfigPath, "secret://") {
		return "", "", transportKind, fmt.Errorf("target uses a secret kubeconfig reference; pass --kubeconfig or materialize it outside evidence")
	}
	return kubeconfigPath, kubeContext, transportKind, nil
}

func opsK8sFactNamespaces(target targetgraph.Target, requested []string, activeNamespace string) []string {
	namespaces := append([]string(nil), requested...)
	if strings.Contains(strings.ToLower(target.Type), "namespace") {
		if ns := strings.TrimSpace(target.Labels["namespace"]); ns != "" {
			namespaces = append(namespaces, ns)
		} else if ns := strings.TrimSpace(target.Labels["kubernetesNamespace"]); ns != "" {
			namespaces = append(namespaces, ns)
		} else if ns := namespaceFromTargetID(target.ID); ns != "" {
			namespaces = append(namespaces, ns)
		}
	}
	if len(namespaces) == 0 && strings.TrimSpace(activeNamespace) != "" {
		namespaces = append(namespaces, strings.TrimSpace(activeNamespace))
	}
	return normalizeOpsStrings(namespaces)
}

func namespaceFromTargetID(targetID string) string {
	targetID = strings.Trim(strings.TrimSpace(targetID), "/")
	if targetID == "" {
		return ""
	}
	parts := strings.Split(targetID, "/")
	candidate := strings.TrimSpace(parts[len(parts)-1])
	if candidate == "" || strings.EqualFold(candidate, "namespace") || strings.EqualFold(candidate, "cluster") {
		return ""
	}
	return candidate
}

func normalizeOpsStrings(values []string) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func opsRootFlag(cmd *cobra.Command, name string) string {
	if cmd == nil {
		return ""
	}
	if root := cmd.Root(); root != nil {
		if flag := root.PersistentFlags().Lookup(name); flag != nil {
			return strings.TrimSpace(flag.Value.String())
		}
	}
	if flag := cmd.InheritedFlags().Lookup(name); flag != nil {
		return strings.TrimSpace(flag.Value.String())
	}
	if flag := cmd.Flags().Lookup(name); flag != nil {
		return strings.TrimSpace(flag.Value.String())
	}
	return ""
}

func opsFactTTL(target targetgraph.Target, override time.Duration) (time.Duration, error) {
	if override > 0 {
		return override, nil
	}
	if strings.TrimSpace(target.Facts.TTL) == "" {
		return 15 * time.Minute, nil
	}
	ttl, err := time.ParseDuration(target.Facts.TTL)
	if err != nil {
		return 0, fmt.Errorf("target %q facts.ttl is invalid: %w", target.ID, err)
	}
	return ttl, nil
}

func opsTargetByID(graph *targetgraph.TargetGraph, targetID string) (targetgraph.Target, bool) {
	for _, target := range graph.Targets {
		if target.ID == targetID {
			return target, true
		}
	}
	return targetgraph.Target{}, false
}

func opsTransportByID(graph *targetgraph.TargetGraph, transportID string) (targetgraph.Transport, bool) {
	for _, transportRef := range graph.Transports {
		if transportRef.ID == transportID {
			return transportRef, true
		}
	}
	return targetgraph.Transport{}, false
}

func opsConfigString(config map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := config[key]; ok {
			switch typed := value.(type) {
			case string:
				return strings.TrimSpace(typed)
			case fmt.Stringer:
				return strings.TrimSpace(typed.String())
			}
		}
	}
	return ""
}

func opsTargetRedactValues(graph *targetgraph.TargetGraph, target targetgraph.Target) []string {
	values := []string{target.TransportRef}
	if transportRef, ok := opsTransportByID(graph, target.TransportRef); ok {
		values = append(values, transportRef.KeyRef, transportRef.KubeconfigRef, transportRef.DSNRef, transportRef.URL)
	}
	for _, layer := range target.Variables {
		values = appendSecretRefsFromMap(values, layer.Values)
	}
	return values
}

func appendSecretRefsFromMap(values []string, fields map[string]any) []string {
	for _, value := range fields {
		values = appendSecretRefs(values, value)
	}
	return values
}

func appendSecretRefs(values []string, value any) []string {
	switch typed := value.(type) {
	case string:
		if strings.HasPrefix(typed, "secret://") {
			values = append(values, typed)
		}
	case []any:
		for _, item := range typed {
			values = appendSecretRefs(values, item)
		}
	case map[string]any:
		for _, item := range typed {
			values = appendSecretRefs(values, item)
		}
	}
	return values
}

func opsFactsError(err error) string {
	if err == nil {
		return ""
	}
	return inventory.RedactSecretRefs(err.Error())
}

func renderOpsFactsCollectTable(w io.Writer, result opsFactsCollectResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TARGET\tTYPE\tTRANSPORT\tSTATUS\tSOURCE\tDIGEST\tEXPIRES\n"); err != nil {
		return err
	}
	for _, item := range result.Results {
		digest := "-"
		expires := "-"
		if item.Snapshot != nil {
			digest = firstNonEmpty(item.Snapshot.Digest, item.Snapshot.StableDigest())
			expires = firstNonEmpty(item.Snapshot.ExpiresAt, "-")
		} else if item.K8sSnapshot != nil {
			digest = firstNonEmpty(item.K8sSnapshot.Digest, item.K8sSnapshot.StableDigest())
		}
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			item.TargetID,
			item.TargetType,
			firstNonEmpty(item.TransportKind, "-"),
			item.Status,
			firstNonEmpty(item.Source, "-"),
			digest,
			expires,
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func opsFactsCollectStatusError(result opsFactsCollectResult) error {
	if result.Summary.Failed == 0 && result.Summary.Skipped == 0 && result.Summary.Blocked == 0 {
		return nil
	}
	return fmt.Errorf(
		"facts collect incomplete: %d failed, %d skipped, %d blocked",
		result.Summary.Failed,
		result.Summary.Skipped,
		result.Summary.Blocked,
	)
}

type opsFactSnapshotDoc struct {
	TargetID string
	Kind     string
	Digest   string
	Host     *hostfacts.Snapshot
	K8s      *k8sfacts.Snapshot
}

func (s opsFactSnapshotDoc) stableDigest() string {
	if s.Digest != "" {
		return s.Digest
	}
	if s.Host != nil {
		return firstNonEmpty(s.Host.Digest, s.Host.StableDigest())
	}
	if s.K8s != nil {
		return firstNonEmpty(s.K8s.Digest, s.K8s.StableDigest())
	}
	return ""
}

func loadOpsFactSnapshots(path string) (map[string]opsFactSnapshotDoc, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	snapshots, err := decodeOpsFactSnapshots(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	out := make(map[string]opsFactSnapshotDoc, len(snapshots))
	for _, snapshot := range snapshots {
		if strings.TrimSpace(snapshot.TargetID) == "" {
			return nil, fmt.Errorf("%s: snapshot missing targetId", path)
		}
		if snapshot.Digest == "" {
			snapshot.Digest = snapshot.stableDigest()
		}
		out[snapshot.TargetID] = snapshot
	}
	return out, nil
}

func decodeOpsFactSnapshots(raw []byte) ([]opsFactSnapshotDoc, error) {
	var collection opsFactsCollectResult
	if err := json.Unmarshal(raw, &collection); err == nil && collection.Kind == opsFactsCollectionKind {
		var out []opsFactSnapshotDoc
		for _, result := range collection.Results {
			if result.Snapshot != nil {
				out = append(out, opsHostFactDoc(result.Snapshot))
			}
			if result.K8sSnapshot != nil {
				out = append(out, opsK8sFactDoc(result.K8sSnapshot))
			}
		}
		if len(out) == 0 {
			return nil, fmt.Errorf("fact collection contains no snapshots")
		}
		return out, nil
	}
	var resolve hostfacts.ResolveResult
	if err := json.Unmarshal(raw, &resolve); err == nil && resolve.Snapshot != nil && resolve.Snapshot.TargetID != "" {
		return []opsFactSnapshotDoc{opsHostFactDoc(resolve.Snapshot)}, nil
	}
	var cacheEntry hostfacts.CacheEntry
	if err := json.Unmarshal(raw, &cacheEntry); err == nil && cacheEntry.Snapshot.TargetID != "" {
		return []opsFactSnapshotDoc{opsHostFactDoc(&cacheEntry.Snapshot)}, nil
	}
	var k8sSnapshot k8sfacts.Snapshot
	if err := json.Unmarshal(raw, &k8sSnapshot); err == nil && k8sSnapshot.TargetID != "" {
		return []opsFactSnapshotDoc{opsK8sFactDoc(&k8sSnapshot)}, nil
	}
	var snapshot hostfacts.Snapshot
	if err := json.Unmarshal(raw, &snapshot); err == nil && snapshot.TargetID != "" {
		return []opsFactSnapshotDoc{opsHostFactDoc(&snapshot)}, nil
	}
	return nil, fmt.Errorf("unsupported facts document")
}

func opsHostFactDoc(snapshot *hostfacts.Snapshot) opsFactSnapshotDoc {
	if snapshot == nil {
		return opsFactSnapshotDoc{}
	}
	copy := *snapshot
	if copy.Digest == "" {
		copy.Digest = copy.StableDigest()
	}
	return opsFactSnapshotDoc{
		TargetID: copy.TargetID,
		Kind:     firstNonEmpty(copy.Kind, hostfacts.Kind),
		Digest:   copy.Digest,
		Host:     &copy,
	}
}

func opsK8sFactDoc(snapshot *k8sfacts.Snapshot) opsFactSnapshotDoc {
	if snapshot == nil {
		return opsFactSnapshotDoc{}
	}
	copy := *snapshot
	if copy.Digest == "" {
		copy.Digest = copy.StableDigest()
	}
	return opsFactSnapshotDoc{
		TargetID: copy.TargetID,
		Kind:     firstNonEmpty(copy.Kind, k8sfacts.Kind),
		Digest:   copy.Digest,
		K8s:      &copy,
	}
}

func diffOpsFactSnapshots(fromPath, toPath string, from, to map[string]opsFactSnapshotDoc) opsFactsDiffResult {
	ids := map[string]struct{}{}
	for targetID := range from {
		ids[targetID] = struct{}{}
	}
	for targetID := range to {
		ids[targetID] = struct{}{}
	}
	targetIDs := make([]string, 0, len(ids))
	for targetID := range ids {
		targetIDs = append(targetIDs, targetID)
	}
	sort.Strings(targetIDs)

	result := opsFactsDiffResult{
		APIVersion: opsFactsAPIVersion,
		Kind:       opsFactsDiffKind,
		From:       fromPath,
		To:         toPath,
	}
	for _, targetID := range targetIDs {
		before, beforeOK := from[targetID]
		after, afterOK := to[targetID]
		entry := opsFactsDiffEntry{TargetID: targetID}
		switch {
		case !beforeOK:
			entry.Status = "added"
			entry.SnapshotKind = after.Kind
			entry.ToDigest = after.stableDigest()
			result.Summary.Added++
		case !afterOK:
			entry.Status = "removed"
			entry.SnapshotKind = before.Kind
			entry.FromDigest = before.stableDigest()
			result.Summary.Removed++
		default:
			entry.SnapshotKind = firstNonEmpty(after.Kind, before.Kind)
			entry.FromDigest = before.stableDigest()
			entry.ToDigest = after.stableDigest()
			entry.ChangedFields = changedOpsFactFields(before, after)
			if len(entry.ChangedFields) == 0 {
				entry.Status = "unchanged"
				result.Summary.Unchanged++
			} else {
				entry.Status = "changed"
				result.Summary.Changed++
			}
		}
		result.Changes = append(result.Changes, entry)
	}
	result.Summary.Targets = len(result.Changes)
	return result
}

func changedOpsFactFields(before, after opsFactSnapshotDoc) []string {
	if before.Kind != after.Kind {
		return []string{"snapshotKind"}
	}
	if before.Host != nil && after.Host != nil {
		return changedOpsHostFactFields(*before.Host, *after.Host)
	}
	if before.K8s != nil && after.K8s != nil {
		return changedOpsK8sFactFields(*before.K8s, *after.K8s)
	}
	return []string{"snapshotKind"}
}

func changedOpsHostFactFields(before, after hostfacts.Snapshot) []string {
	fields := []struct {
		name string
		a    any
		b    any
	}{
		{name: "targetDigest", a: before.TargetDigest, b: after.TargetDigest},
		{name: "os", a: before.OS, b: after.OS},
		{name: "kernel", a: before.Kernel, b: after.Kernel},
		{name: "packages", a: before.Packages, b: after.Packages},
		{name: "services", a: before.Services, b: after.Services},
		{name: "users", a: before.Users, b: after.Users},
		{name: "disks", a: before.Disks, b: after.Disks},
		{name: "network", a: before.Network, b: after.Network},
	}
	var changed []string
	for _, field := range fields {
		left, _ := json.Marshal(field.a)
		right, _ := json.Marshal(field.b)
		if !bytes.Equal(left, right) {
			changed = append(changed, field.name)
		}
	}
	return changed
}

func changedOpsK8sFactFields(before, after k8sfacts.Snapshot) []string {
	fields := []struct {
		name string
		a    any
		b    any
	}{
		{name: "targetDigest", a: before.TargetDigest, b: after.TargetDigest},
		{name: "cluster", a: before.Cluster, b: after.Cluster},
		{name: "namespaces", a: before.Namespaces, b: after.Namespaces},
		{name: "nodes", a: before.Nodes, b: after.Nodes},
		{name: "workloads", a: before.Workloads, b: after.Workloads},
		{name: "events", a: before.Events, b: after.Events},
	}
	var changed []string
	for _, field := range fields {
		left, _ := json.Marshal(field.a)
		right, _ := json.Marshal(field.b)
		if !bytes.Equal(left, right) {
			changed = append(changed, field.name)
		}
	}
	return changed
}

func renderOpsFactsDiffTable(w io.Writer, result opsFactsDiffResult) error {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	if _, err := fmt.Fprintf(tw, "TARGET\tSTATUS\tFROM\tTO\tFIELDS\n"); err != nil {
		return err
	}
	for _, change := range result.Changes {
		if _, err := fmt.Fprintf(
			tw,
			"%s\t%s\t%s\t%s\t%s\n",
			change.TargetID,
			change.Status,
			firstNonEmpty(change.FromDigest, "-"),
			firstNonEmpty(change.ToDigest, "-"),
			inventory.FormatList(change.ChangedFields),
		); err != nil {
			return err
		}
	}
	return tw.Flush()
}
