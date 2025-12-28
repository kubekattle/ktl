package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

type stackEffectiveConfig struct {
	RootDir  string
	Profile  string
	Output   string
	Clusters []string

	Selector         stack.Selector
	InferDeps        bool
	InferConfigRefs  bool
	ResolvedStackCLI stack.StackCLIResolved
}

func compileInferSelect(cmd *cobra.Command, common stackCommandCommon) (*stack.Universe, *stack.Plan, stackEffectiveConfig, error) {
	effective, err := resolveStackEffectiveConfig(cmd, common)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}

	u, err := stack.Discover(effective.RootDir)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}

	if strings.TrimSpace(effective.Profile) == "" {
		effective.Profile = strings.TrimSpace(u.DefaultProfile)
	}

	stackCLI, err := stack.ResolveStackCLIConfig(u, effective.Profile)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}
	effective.ResolvedStackCLI = stackCLI

	p, err := stack.Compile(u, stack.CompileOptions{Profile: effective.Profile})
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}

	inferDeps, inferConfigRefs, err := resolveInferDefaults(cmd, common, stackCLI)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}
	effective.InferDeps = inferDeps
	effective.InferConfigRefs = inferConfigRefs

	if inferDeps {
		kubeconfigPath := derefString(common.kubeconfig)
		kubeCtx := derefString(common.kubeContext)
		if err := stack.InferDependencies(cmd.Context(), p, kubeconfigPath, kubeCtx, stack.InferDepsOptions{IncludeConfigRefs: inferConfigRefs}); err != nil {
			return nil, nil, stackEffectiveConfig{}, err
		}
		if err := stack.RecomputeExecutionGroups(p); err != nil {
			return nil, nil, stackEffectiveConfig{}, err
		}
	}

	clusters, selector, err := resolveSelectorDefaults(cmd, common, stackCLI)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}
	effective.Clusters = clusters
	effective.Selector = selector

	selected, err := stack.Select(u, p, clusters, selector)
	if err != nil {
		return nil, nil, stackEffectiveConfig{}, err
	}
	effective.Output = resolveOutputDefault(cmd, common, stackCLI)
	return u, selected, effective, nil
}

func resolveStackEffectiveConfig(cmd *cobra.Command, common stackCommandCommon) (stackEffectiveConfig, error) {
	root := strings.TrimSpace(*common.rootDir)
	if cmd != nil && !cmd.Flags().Changed("root") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_ROOT")); v != "" {
			root = v
		}
	}
	if root == "" {
		root = "."
	}

	profile := strings.TrimSpace(*common.profile)
	if cmd != nil && !cmd.Flags().Changed("profile") {
		if v := strings.TrimSpace(os.Getenv("KTL_STACK_PROFILE")); v != "" {
			profile = v
		}
	}

	return stackEffectiveConfig{RootDir: root, Profile: profile}, nil
}

func resolveInferDefaults(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) (inferDeps bool, inferConfigRefs bool, err error) {
	inferDeps = cfg.InferDeps
	inferConfigRefs = cfg.InferConfigRefs

	if cmd.Flags().Changed("infer-deps") {
		inferDeps = common.inferDeps != nil && *common.inferDeps
	} else if v, ok, err := envBool("KTL_STACK_INFER_DEPS"); err != nil {
		return false, false, err
	} else if ok {
		inferDeps = v
	}

	if cmd.Flags().Changed("infer-config-refs") {
		inferConfigRefs = common.inferConfigRefs != nil && *common.inferConfigRefs
	} else if v, ok, err := envBool("KTL_STACK_INFER_CONFIG_REFS"); err != nil {
		return false, false, err
	} else if ok {
		inferConfigRefs = v
	}

	return inferDeps, inferConfigRefs, nil
}

func resolveOutputDefault(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) string {
	if cmd.Flags().Changed("output") {
		return strings.ToLower(strings.TrimSpace(*common.output))
	}
	if v := strings.TrimSpace(os.Getenv("KTL_STACK_OUTPUT")); v != "" {
		return strings.ToLower(v)
	}
	if strings.TrimSpace(cfg.Output) != "" {
		return strings.ToLower(strings.TrimSpace(cfg.Output))
	}
	return strings.ToLower(strings.TrimSpace(*common.output))
}

func resolveSelectorDefaults(cmd *cobra.Command, common stackCommandCommon, cfg stack.StackCLIResolved) ([]string, stack.Selector, error) {
	clusters := resolveStringSliceDefault(cmd, "cluster", splitCSV(*common.clusters), "KTL_STACK_CLUSTER", cfg.Clusters)
	selector := cfg.Selector

	selector.Tags = resolveStringSliceDefault(cmd, "tag", splitCSV(*common.tags), "KTL_STACK_TAG", cfg.Selector.Tags)
	selector.FromPaths = resolveStringSliceDefault(cmd, "from-path", splitCSV(*common.fromPaths), "KTL_STACK_FROM_PATH", cfg.Selector.FromPaths)
	selector.Releases = resolveStringSliceDefault(cmd, "release", splitCSV(*common.releases), "KTL_STACK_RELEASE", cfg.Selector.Releases)

	if cmd.Flags().Changed("git-range") {
		selector.GitRange = strings.TrimSpace(*common.gitRange)
	} else if v := strings.TrimSpace(os.Getenv("KTL_STACK_GIT_RANGE")); v != "" {
		selector.GitRange = v
	} else {
		selector.GitRange = strings.TrimSpace(cfg.Selector.GitRange)
	}

	selector.GitIncludeDeps = resolveBoolDefault(cmd, "git-include-deps", common.gitIncludeDeps, "KTL_STACK_GIT_INCLUDE_DEPS", cfg.Selector.GitIncludeDeps)
	selector.GitIncludeDependents = resolveBoolDefault(cmd, "git-include-dependents", common.gitIncludeDependents, "KTL_STACK_GIT_INCLUDE_DEPENDENTS", cfg.Selector.GitIncludeDependents)
	selector.IncludeDeps = resolveBoolDefault(cmd, "include-deps", common.includeDeps, "KTL_STACK_INCLUDE_DEPS", cfg.Selector.IncludeDeps)
	selector.IncludeDependents = resolveBoolDefault(cmd, "include-dependents", common.includeDependents, "KTL_STACK_INCLUDE_DEPENDENTS", cfg.Selector.IncludeDependents)
	selector.AllowMissingDeps = resolveBoolDefault(cmd, "allow-missing-deps", common.allowMissingDeps, "KTL_STACK_ALLOW_MISSING_DEPS", cfg.Selector.AllowMissingDeps)

	return clusters, selector, nil
}

func resolveStringSliceDefault(cmd *cobra.Command, flagName string, flagValue []string, envName string, yamlValue []string) []string {
	if cmd.Flags().Changed(flagName) {
		return flagValue
	}
	if v := strings.TrimSpace(os.Getenv(envName)); v != "" {
		return splitCSV([]string{v})
	}
	if len(yamlValue) > 0 {
		return splitCSV(yamlValue)
	}
	return flagValue
}

func resolveBoolDefault(cmd *cobra.Command, flagName string, flagPtr *bool, envName string, yamlValue bool) bool {
	if cmd.Flags().Changed(flagName) && flagPtr != nil {
		return *flagPtr
	}
	if v, ok, err := envBool(envName); err == nil && ok {
		return v
	}
	return yamlValue
}

func envBool(name string) (bool, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return false, false, nil
	}
	switch strings.ToLower(raw) {
	case "1", "true", "t", "yes", "y", "on":
		return true, true, nil
	case "0", "false", "f", "no", "n", "off":
		return false, true, nil
	}
	if n, err := strconv.Atoi(raw); err == nil {
		return n != 0, true, nil
	}
	return false, true, fmt.Errorf("invalid %s %q (expected boolean)", name, raw)
}
