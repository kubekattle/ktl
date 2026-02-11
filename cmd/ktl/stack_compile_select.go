package main

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func compileInferSelect(cmd *cobra.Command, common stackCommandCommon) (*stack.Universe, *stack.Plan, stackCommandConfig, error) {
	cfg, err := resolveStackCommandConfig(cmd, common)
	if err != nil {
		return nil, nil, stackCommandConfig{}, err
	}
	printStackConfigWarnings(cmd, cfg.Warnings)
	return compileInferSelectWithConfig(cmd, common, cfg)
}

func compileInferSelectWithConfig(cmd *cobra.Command, common stackCommandCommon, cfg stackCommandConfig) (*stack.Universe, *stack.Plan, stackCommandConfig, error) {
	u := cfg.Universe

	p, err := stack.Compile(u, stack.CompileOptions{Profile: cfg.Profile})
	if err != nil {
		return nil, nil, stackCommandConfig{}, err
	}

	if cfg.InferDeps {
		kubeconfigPath := derefString(common.kubeconfig)
		kubeCtx := derefString(common.kubeContext)
		secretOptions, err := buildStackSecretOptions(cmd.Context(), p.StackRoot, derefString(common.secretProvider), derefString(common.secretConfig), nil)
		if err != nil {
			return nil, nil, stackCommandConfig{}, err
		}
		if secretOptions == nil {
			secretOptions = &deploy.SecretOptions{}
		}
		if err := stack.InferDependencies(cmd.Context(), p, kubeconfigPath, kubeCtx, stack.InferDepsOptions{
			IncludeConfigRefs: cfg.InferConfigRefs,
			Secrets:           secretOptions,
		}); err != nil {
			return nil, nil, stackCommandConfig{}, err
		}
		if err := stack.RecomputeExecutionGroups(p); err != nil {
			return nil, nil, stackCommandConfig{}, err
		}
	}

	selected, err := stack.Select(u, p, cfg.Clusters, cfg.Selector)
	if err != nil {
		return nil, nil, stackCommandConfig{}, withSelectionHint(err)
	}
	if selected != nil && len(selected.Nodes) == 0 {
		return nil, nil, stackCommandConfig{}, fmt.Errorf("selection matched 0 releases\nhint: set stack.yaml cli.selector.* defaults or use KTL_STACK_TAG / KTL_STACK_RELEASE (run `ktl env --match stack`)")
	}

	return u, selected, cfg, nil
}

func withSelectionHint(err error) error {
	if err == nil {
		return nil
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "unknown release"):
		return fmt.Errorf("%w\nhint: configure stack.yaml cli.selector.releases or set KTL_STACK_RELEASE", err)
	case strings.Contains(msg, "ambiguous release name"):
		return fmt.Errorf("%w\nhint: disambiguate with --cluster (or set KTL_STACK_CLUSTER)", err)
	default:
		return err
	}
}
