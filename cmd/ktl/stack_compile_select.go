package main

import (
	"github.com/example/ktl/internal/stack"
	"github.com/spf13/cobra"
)

func compileInferSelect(cmd *cobra.Command, common stackCommandCommon) (*stack.Universe, *stack.Plan, error) {
	u, err := stack.Discover(*common.rootDir)
	if err != nil {
		return nil, nil, err
	}
	p, err := stack.Compile(u, stack.CompileOptions{Profile: *common.profile})
	if err != nil {
		return nil, nil, err
	}
	if common.inferDeps != nil && *common.inferDeps {
		kubeconfigPath := derefString(common.kubeconfig)
		kubeCtx := derefString(common.kubeContext)
		if err := stack.InferDependencies(cmd.Context(), p, kubeconfigPath, kubeCtx, stack.InferDepsOptions{IncludeConfigRefs: common.inferConfigRefs != nil && *common.inferConfigRefs}); err != nil {
			return nil, nil, err
		}
		if err := stack.RecomputeExecutionGroups(p); err != nil {
			return nil, nil, err
		}
	}
	selected, err := stack.Select(u, p, splitCSV(*common.clusters), stack.Selector{
		Tags:                 *common.tags,
		FromPaths:            *common.fromPaths,
		Releases:             *common.releases,
		GitRange:             *common.gitRange,
		GitIncludeDeps:       *common.gitIncludeDeps,
		GitIncludeDependents: *common.gitIncludeDependents,
		IncludeDeps:          *common.includeDeps,
		IncludeDependents:    *common.includeDependents,
		AllowMissingDeps:     *common.allowMissingDeps,
	})
	if err != nil {
		return nil, nil, err
	}
	return u, selected, nil
}
