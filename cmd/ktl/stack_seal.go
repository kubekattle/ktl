// File: cmd/ktl/stack_seal.go
// Brief: `ktl stack seal` command wiring.

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/kubekattle/ktl/internal/stack"
	"github.com/kubekattle/ktl/internal/version"
	"github.com/spf13/cobra"
)

type stackSealAttestation struct {
	APIVersion string `json:"apiVersion"`
	CreatedAt  string `json:"createdAt"`

	PlanHash string `json:"planHash"`

	PlanFile       string `json:"planFile"`
	InputsBundle   string `json:"inputsBundle,omitempty"`
	InputsBundleSH string `json:"inputsBundleDigest,omitempty"`

	StackGitCommit string `json:"stackGitCommit,omitempty"`
	StackGitDirty  bool   `json:"stackGitDirty,omitempty"`

	KtlVersion   string `json:"ktlVersion,omitempty"`
	KtlGitCommit string `json:"ktlGitCommit,omitempty"`
}

func newStackSealCommand(rootDir, profile *string, clusters *[]string, inferDeps *bool, inferConfigRefs *bool, tags *[]string, fromPaths *[]string, releases *[]string, gitRange *string, gitIncludeDeps *bool, gitIncludeDependents *bool, includeDeps *bool, includeDependents *bool, allowMissingDeps *bool) *cobra.Command {
	var outDir string
	var command string
	var concurrency int
	var failMode string
	var includeBundle bool
	var planFilename string
	var bundleFilename string
	var attestationFilename string
	cmd := &cobra.Command{
		Use:   "seal",
		Short: "Write a sealed, reproducible stack plan + optional inputs bundle for CI",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			command = strings.ToLower(strings.TrimSpace(command))
			if command == "" {
				command = "apply"
			}
			if command != "apply" && command != "delete" {
				return fmt.Errorf("invalid --command %q (expected apply|delete)", command)
			}

			u, err := stack.Discover(*rootDir)
			if err != nil {
				return err
			}
			p, err := stack.Compile(u, stack.CompileOptions{Profile: *profile})
			if err != nil {
				return err
			}
			if inferDeps != nil && *inferDeps {
				kubeconfigPath, _ := cmd.Flags().GetString("kubeconfig")
				kubeCtx, _ := cmd.Flags().GetString("context")
				if err := stack.InferDependencies(cmd.Context(), p, kubeconfigPath, kubeCtx, stack.InferDepsOptions{IncludeConfigRefs: inferConfigRefs != nil && *inferConfigRefs}); err != nil {
					return err
				}
				if err := stack.RecomputeExecutionGroups(p); err != nil {
					return err
				}
			}
			p, err = stack.Select(u, p, splitCSV(*clusters), stack.Selector{
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

			// Create a run ID so artifacts are self-contained.
			runID := time.Now().UTC().Format("2006-01-02T15-04-05.000000000Z")
			now := time.Now().UTC()
			out := strings.TrimSpace(outDir)
			if out == "" {
				out = filepath.Join(*rootDir, ".ktl", "stack", "sealed", runID)
			}
			if err := os.MkdirAll(out, 0o755); err != nil {
				return err
			}
			if strings.TrimSpace(planFilename) == "" {
				planFilename = "plan.json"
			}
			if strings.TrimSpace(bundleFilename) == "" {
				bundleFilename = "inputs.tar.gz"
			}
			if strings.TrimSpace(attestationFilename) == "" {
				attestationFilename = "attestation.json"
			}

			for _, n := range p.Nodes {
				hash, input, err := stack.ComputeEffectiveInputHash(p.StackRoot, n, true)
				if err != nil {
					return err
				}
				n.EffectiveInputHash = hash
				n.EffectiveInput = input
			}

			runner := p.Runner
			if concurrency > 0 {
				runner.Concurrency = concurrency
			}
			if err := stack.ValidateRunnerResolved(runner); err != nil {
				return err
			}

			gid, err := stack.GitIdentityForRoot(p.StackRoot)
			if err != nil {
				return err
			}

			if strings.TrimSpace(failMode) == "" {
				failMode = "fail-fast"
			}

			rp := &stack.RunPlan{
				APIVersion:  "ktl.dev/stack-run/v1",
				RunID:       runID,
				StackRoot:   p.StackRoot,
				StackName:   p.StackName,
				Command:     command,
				Profile:     p.Profile,
				Concurrency: runner.Concurrency,
				FailMode:    failMode,
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
				Nodes:  p.Nodes,
				Runner: runner,

				StackGitCommit: gid.Commit,
				StackGitDirty:  gid.Dirty,
				KtlVersion:     version.Version,
				KtlGitCommit:   version.GitCommit,
			}
			planHash, err := stack.ComputeRunPlanHash(rp)
			if err != nil {
				return err
			}
			rp.PlanHash = planHash

			planPath := filepath.Join(out, planFilename)
			if err := writeJSONFile(planPath, rp); err != nil {
				return err
			}

			var bundlePath string
			var bundleDigest string
			if includeBundle {
				bundlePath = filepath.Join(out, bundleFilename)
				manifest, digest, err := stack.WriteInputBundle(cmd.Context(), bundlePath, planHash, p.Nodes)
				if err != nil {
					return err
				}
				bundleDigest = digest
				_ = writeJSONFile(filepath.Join(out, "inputs.manifest.json"), manifest)
			}

			att := stackSealAttestation{
				APIVersion: "ktl.dev/stack-seal-attestation/v1",
				CreatedAt:  now.Format(time.RFC3339Nano),
				PlanHash:   planHash,
				PlanFile:   planFilename,

				InputsBundle:   bundleFilename,
				InputsBundleSH: bundleDigest,

				StackGitCommit: gid.Commit,
				StackGitDirty:  gid.Dirty,
				KtlVersion:     version.Version,
				KtlGitCommit:   version.GitCommit,
			}
			if !includeBundle {
				att.InputsBundle = ""
				att.InputsBundleSH = ""
			}
			if err := writeJSONFile(filepath.Join(out, attestationFilename), att); err != nil {
				return err
			}

			fmt.Fprintf(cmd.ErrOrStderr(), "ktl stack seal: wrote %s (planHash=%s)\n", out, planHash)
			return nil
		},
	}
	cmd.Flags().StringVar(&outDir, "out", "", "Output directory (defaults to --root/.ktl/stack/sealed/<runId>)")
	cmd.Flags().StringVar(&command, "command", "apply", "Command the plan is intended for: apply|delete")
	cmd.Flags().IntVar(&concurrency, "concurrency", 1, "Recommended concurrency for this sealed plan")
	cmd.Flags().StringVar(&failMode, "fail-mode", "fail-fast", "Recommended failure mode: fail-fast|continue")
	cmd.Flags().BoolVar(&includeBundle, "bundle", true, "Write an inputs bundle (chart + values files) alongside plan.json")
	cmd.Flags().StringVar(&planFilename, "plan-file", "plan.json", "Plan filename written inside --out")
	cmd.Flags().StringVar(&bundleFilename, "bundle-file", "inputs.tar.gz", "Bundle filename written inside --out")
	cmd.Flags().StringVar(&attestationFilename, "attestation-file", "attestation.json", "Attestation filename written inside --out")
	return cmd
}

func writeJSONFile(path string, v any) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Sync(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, path)
}
