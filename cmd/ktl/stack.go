// File: cmd/ktl/stack.go
// Brief: CLI wiring for `ktl stack` (multi-release orchestration).

package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func newStackCommand(kubeconfig *string, kubeContext *string, logLevel *string, remoteAgent *string) *cobra.Command {
	var rootDir string
	var profile string
	var clusters []string
	var tags []string
	var fromPaths []string
	var releases []string
	var gitRange string
	var gitIncludeDeps bool
	var gitIncludeDependents bool
	var includeDeps bool
	var includeDependents bool
	var allowMissingDeps bool
	var output string
	var planOnly bool
	var inferDeps bool
	var inferConfigRefs bool

	cmd := &cobra.Command{
		Use:   "stack",
		Short: "Compile and orchestrate many Helm releases as a dependency graph",
		Long:  "ktl stack discovers stack.yaml/release.yaml, compiles them with inheritance into a DAG, and runs ktl apply/delete per release.",
	}
	cmd.PersistentFlags().StringVar(&rootDir, "root", ".", "Stack root directory")
	cmd.PersistentFlags().StringVar(&profile, "profile", "", "Profile overlay name (defaults to stack.yaml.defaultProfile when present)")
	cmd.PersistentFlags().StringSliceVar(&clusters, "cluster", nil, "Filter the universe by cluster name (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&tags, "tag", nil, "Select releases by tag (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&fromPaths, "from-path", nil, "Select releases under a directory subtree (repeatable or comma-separated)")
	cmd.PersistentFlags().StringSliceVar(&releases, "release", nil, "Select releases by name (repeatable or comma-separated)")
	cmd.PersistentFlags().StringVar(&gitRange, "git-range", "", "Select releases affected by a git diff range (example: origin/main...HEAD)")
	cmd.PersistentFlags().BoolVar(&gitIncludeDeps, "git-include-deps", false, "When using --git-range, expand selection to include dependencies")
	cmd.PersistentFlags().BoolVar(&gitIncludeDependents, "git-include-dependents", false, "When using --git-range, expand selection to include dependents")
	cmd.PersistentFlags().BoolVar(&includeDeps, "include-deps", false, "Expand selection to include dependencies")
	cmd.PersistentFlags().BoolVar(&includeDependents, "include-dependents", false, "Expand selection to include dependents")
	cmd.PersistentFlags().BoolVar(&allowMissingDeps, "allow-missing-deps", false, "Allow selected releases to run even if their declared needs are not selected (missing needs are ignored)")
	cmd.PersistentFlags().StringVar(&output, "output", "table", "Output format: table|json")
	cmd.PersistentFlags().BoolVar(&planOnly, "plan-only", false, "Compile and print the plan, but do not execute")
	cmd.PersistentFlags().BoolVar(&inferDeps, "infer-deps", true, "Infer additional dependencies between releases by rendering manifests (client-side)")
	cmd.PersistentFlags().BoolVar(&inferConfigRefs, "infer-config-refs", false, "When inferring deps, also add edges for ConfigMap/Secret references from workloads")

	// Minimal-flag UX: most selection and output controls are intended to live in stack.yaml `cli:`
	// and/or be provided via env vars. Flags remain supported (including `KTL_<FLAG>`), but are hidden.
	_ = cmd.PersistentFlags().MarkHidden("cluster")
	_ = cmd.PersistentFlags().MarkHidden("tag")
	_ = cmd.PersistentFlags().MarkHidden("from-path")
	_ = cmd.PersistentFlags().MarkHidden("release")
	_ = cmd.PersistentFlags().MarkHidden("git-range")
	_ = cmd.PersistentFlags().MarkHidden("git-include-deps")
	_ = cmd.PersistentFlags().MarkHidden("git-include-dependents")
	_ = cmd.PersistentFlags().MarkHidden("include-deps")
	_ = cmd.PersistentFlags().MarkHidden("include-dependents")
	_ = cmd.PersistentFlags().MarkHidden("allow-missing-deps")
	_ = cmd.PersistentFlags().MarkHidden("output")
	_ = cmd.PersistentFlags().MarkHidden("infer-deps")
	_ = cmd.PersistentFlags().MarkHidden("infer-config-refs")

	common := stackCommandCommon{
		rootDir:              &rootDir,
		profile:              &profile,
		clusters:             &clusters,
		output:               &output,
		planOnly:             &planOnly,
		inferDeps:            &inferDeps,
		inferConfigRefs:      &inferConfigRefs,
		tags:                 &tags,
		fromPaths:            &fromPaths,
		releases:             &releases,
		gitRange:             &gitRange,
		gitIncludeDeps:       &gitIncludeDeps,
		gitIncludeDependents: &gitIncludeDependents,
		includeDeps:          &includeDeps,
		includeDependents:    &includeDependents,
		allowMissingDeps:     &allowMissingDeps,
		kubeconfig:           kubeconfig,
		kubeContext:          kubeContext,
		logLevel:             logLevel,
		remoteAgent:          remoteAgent,
	}

	cmd.AddCommand(newStackPlanCommand(common))
	cmd.AddCommand(newStackGraphCommand(common))
	cmd.AddCommand(newStackExplainCommand(common))

	cmd.AddCommand(newStackSealCommand(&rootDir, &profile, &clusters, &inferDeps, &inferConfigRefs, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents, &allowMissingDeps))
	cmd.AddCommand(newStackStatusCommand(&rootDir))
	cmd.AddCommand(newStackRunsCommand(&rootDir, &output))
	cmd.AddCommand(newStackAuditCommand(&rootDir))
	cmd.AddCommand(newStackExportCommand(&rootDir))
	cmd.AddCommand(newStackKeygenCommand(&rootDir))
	cmd.AddCommand(newStackSignCommand(&rootDir))
	cmd.AddCommand(newStackVerifyCommand(&rootDir))
	cmd.AddCommand(newStackApplyCommand(common))
	cmd.AddCommand(newStackDeleteCommand(common))
	cmd.AddCommand(newStackRerunFailedCommand(&rootDir, &profile, &clusters, &inferDeps, &inferConfigRefs, &tags, &fromPaths, &releases, &gitRange, &gitIncludeDeps, &gitIncludeDependents, &includeDeps, &includeDependents, &allowMissingDeps, kubeconfig, kubeContext, logLevel, remoteAgent))
	return cmd
}

func newStackPlanCommand(common stackCommandCommon) *cobra.Command {
	var bundlePath string
	var bundleDiffSummary bool
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Compile stack configs into an execution plan",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			_, selected, effective, err := compileInferSelect(cmd, common)
			if err != nil {
				return err
			}
			if strings.TrimSpace(bundlePath) != "" {
				kubeconfigPath := derefString(common.kubeconfig)
				kubeContext := derefString(common.kubeContext)

				// Compute effective inputs so the plan artifact is self-verifying.
				for _, n := range selected.Nodes {
					hash, input, err := stack.ComputeEffectiveInputHash(selected.StackRoot, n, true)
					if err != nil {
						return err
					}
					n.EffectiveInputHash = hash
					n.EffectiveInput = input
				}

				gid, err := stack.GitIdentityForRoot(selected.StackRoot)
				if err != nil {
					return err
				}

				rp := &stack.RunPlan{
					APIVersion: "ktl.dev/stack-run/v1",
					// Keep RunID empty so planHash is stable for identical inputs.
					RunID:       "",
					StackRoot:   selected.StackRoot,
					StackName:   selected.StackName,
					Command:     "apply",
					Profile:     selected.Profile,
					Concurrency: selected.Runner.Concurrency,
					FailMode:    "fail-fast",
					Selector: stack.RunSelector{
						Clusters:             splitCSV(*common.clusters),
						Tags:                 splitCSV(*common.tags),
						FromPaths:            splitCSV(*common.fromPaths),
						Releases:             splitCSV(*common.releases),
						GitRange:             strings.TrimSpace(*common.gitRange),
						GitIncludeDeps:       *common.gitIncludeDeps,
						GitIncludeDependents: *common.gitIncludeDependents,
						IncludeDeps:          *common.includeDeps,
						IncludeDependents:    *common.includeDependents,
						AllowMissingDeps:     *common.allowMissingDeps,
					},
					Nodes:  selected.Nodes,
					Runner: selected.Runner,

					StackGitCommit: gid.Commit,
					StackGitDirty:  gid.Dirty,
				}
				planHash, err := stack.ComputeRunPlanHash(rp)
				if err != nil {
					return err
				}
				rp.PlanHash = planHash

				planJSON, err := json.MarshalIndent(rp, "", "  ")
				if err != nil {
					return err
				}

				// Write inputs bundle to a temp dir, then pack everything into a deterministic tarball.
				tmpDir, err := os.MkdirTemp("", "ktl-stack-plan-bundle-*")
				if err != nil {
					return err
				}
				defer os.RemoveAll(tmpDir)

				inputsPath := filepath.Join(tmpDir, "inputs.tar.gz")
				inputsManifest, _, err := stack.WriteInputBundle(cmd.Context(), inputsPath, planHash, selected.Nodes)
				if err != nil {
					return err
				}
				inputsManifestJSON, err := json.MarshalIndent(inputsManifest, "", "  ")
				if err != nil {
					return err
				}

				inputsSHA, err := sha256HexFile(inputsPath)
				if err != nil {
					return err
				}

				var diffSummaryJSON []byte
				if bundleDiffSummary {
					ds, err := stack.BuildStackDiffSummary(cmd.Context(), selected, kubeconfigPath, kubeContext, planHash)
					if err != nil {
						return err
					}
					diffSummaryJSON, err = json.MarshalIndent(ds, "", "  ")
					if err != nil {
						return err
					}
				}

				att := map[string]any{
					"apiVersion":         "ktl.dev/stack-seal-attestation/v1",
					"planHash":           planHash,
					"planFile":           "plan.json",
					"inputsBundle":       "inputs.tar.gz",
					"inputsBundleDigest": "sha256:" + inputsSHA,
					"stackGitCommit":     gid.Commit,
					"stackGitDirty":      gid.Dirty,
				}
				attJSON, err := json.MarshalIndent(att, "", "  ")
				if err != nil {
					return err
				}

				manifest := stack.StackPlanBundleManifest{
					APIVersion:         "ktl.dev/stack-bundle/v1",
					Kind:               "StackPlanBundle",
					PlanHash:           planHash,
					InputsBundleSha256: inputsSHA,
					StackName:          selected.StackName,
					Profile:            selected.Profile,
				}

				wrote, err := stack.WriteStackPlanBundle(strings.TrimSpace(bundlePath), planJSON, attJSON, inputsPath, inputsManifestJSON, diffSummaryJSON, manifest)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack plan: wrote bundle %s (planHash=%s)\n", wrote, planHash)
				return nil
			}
			switch strings.ToLower(strings.TrimSpace(effective.Output)) {
			case "json":
				enc := json.NewEncoder(cmd.OutOrStdout())
				enc.SetIndent("", "  ")
				return enc.Encode(selected)
			case "", "table":
				return stack.PrintPlanTable(cmd.OutOrStdout(), selected)
			default:
				return fmt.Errorf("unknown --output %q (expected table|json)", effective.Output)
			}
		},
	}
	cmd.Flags().StringVar(&bundlePath, "bundle", "", "Write a reproducible plan bundle (.tgz) instead of printing the plan")
	cmd.Flags().BoolVar(&bundleDiffSummary, "bundle-diff-summary", false, "Compute and embed a diff summary against the live cluster (requires cluster access)")
	return cmd
}

func splitCSV(vals []string) []string {
	var out []string
	for _, v := range vals {
		for _, part := range strings.Split(v, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}

func sha256HexFile(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}
