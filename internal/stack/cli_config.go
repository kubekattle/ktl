package stack

import (
	"fmt"
	"strings"
)

type StackCLIResolved struct {
	Clusters []string
	Selector Selector

	InferDeps       bool
	InferConfigRefs bool
	Output          string

	ApplyDryRun *bool
	ApplyDiff   *bool

	DeleteConfirmThreshold *int

	ResumeAllowDrift  *bool
	ResumeRerunFailed *bool
}

func ResolveStackCLIConfig(u *Universe, profile string) (StackCLIResolved, error) {
	out := StackCLIResolved{
		InferDeps:       true,
		InferConfigRefs: false,
		Output:          "table",
	}
	if u == nil {
		return out, nil
	}
	sf, ok := u.Stacks[u.RootDir]
	if !ok {
		return out, nil
	}

	cfg := StackCLIConfig{}
	mergeStackCLI(&cfg, sf.CLI)
	if strings.TrimSpace(profile) != "" {
		if sp, ok := sf.Profiles[strings.TrimSpace(profile)]; ok {
			mergeStackCLI(&cfg, sp.CLI)
		}
	}

	if cfg.InferDeps != nil {
		out.InferDeps = *cfg.InferDeps
	}
	if cfg.InferConfigRefs != nil {
		out.InferConfigRefs = *cfg.InferConfigRefs
	}
	if strings.TrimSpace(cfg.Output) != "" {
		out.Output = strings.ToLower(strings.TrimSpace(cfg.Output))
		switch out.Output {
		case "table", "json":
		default:
			return StackCLIResolved{}, fmt.Errorf("cli.output must be table|json (got %q)", cfg.Output)
		}
	}

	out.Selector = Selector{
		Tags:      cfg.Selector.Tags,
		FromPaths: cfg.Selector.FromPaths,
		Releases:  cfg.Selector.Releases,
		GitRange:  cfg.Selector.GitRange,
	}
	out.Clusters = append([]string(nil), cfg.Selector.Clusters...)
	if cfg.Selector.GitIncludeDeps != nil {
		out.Selector.GitIncludeDeps = *cfg.Selector.GitIncludeDeps
	}
	if cfg.Selector.GitIncludeDependents != nil {
		out.Selector.GitIncludeDependents = *cfg.Selector.GitIncludeDependents
	}
	if cfg.Selector.IncludeDeps != nil {
		out.Selector.IncludeDeps = *cfg.Selector.IncludeDeps
	}
	if cfg.Selector.IncludeDependents != nil {
		out.Selector.IncludeDependents = *cfg.Selector.IncludeDependents
	}
	if cfg.Selector.AllowMissingDeps != nil {
		out.Selector.AllowMissingDeps = *cfg.Selector.AllowMissingDeps
	}

	out.ApplyDryRun = cfg.Apply.DryRun
	out.ApplyDiff = cfg.Apply.Diff
	out.DeleteConfirmThreshold = cfg.Delete.ConfirmThreshold
	out.ResumeAllowDrift = cfg.Resume.AllowDrift
	out.ResumeRerunFailed = cfg.Resume.RerunFailed

	return out, nil
}

func mergeStackCLI(dst *StackCLIConfig, src StackCLIConfig) {
	if dst == nil {
		return
	}
	if len(src.Selector.Clusters) > 0 {
		dst.Selector.Clusters = append([]string(nil), src.Selector.Clusters...)
	}
	if len(src.Selector.Tags) > 0 {
		dst.Selector.Tags = append([]string(nil), src.Selector.Tags...)
	}
	if len(src.Selector.FromPaths) > 0 {
		dst.Selector.FromPaths = append([]string(nil), src.Selector.FromPaths...)
	}
	if len(src.Selector.Releases) > 0 {
		dst.Selector.Releases = append([]string(nil), src.Selector.Releases...)
	}
	if strings.TrimSpace(src.Selector.GitRange) != "" {
		dst.Selector.GitRange = src.Selector.GitRange
	}

	if src.Selector.GitIncludeDeps != nil {
		dst.Selector.GitIncludeDeps = src.Selector.GitIncludeDeps
	}
	if src.Selector.GitIncludeDependents != nil {
		dst.Selector.GitIncludeDependents = src.Selector.GitIncludeDependents
	}
	if src.Selector.IncludeDeps != nil {
		dst.Selector.IncludeDeps = src.Selector.IncludeDeps
	}
	if src.Selector.IncludeDependents != nil {
		dst.Selector.IncludeDependents = src.Selector.IncludeDependents
	}
	if src.Selector.AllowMissingDeps != nil {
		dst.Selector.AllowMissingDeps = src.Selector.AllowMissingDeps
	}

	if src.InferDeps != nil {
		dst.InferDeps = src.InferDeps
	}
	if src.InferConfigRefs != nil {
		dst.InferConfigRefs = src.InferConfigRefs
	}
	if strings.TrimSpace(src.Output) != "" {
		dst.Output = src.Output
	}

	if src.Apply.DryRun != nil {
		dst.Apply.DryRun = src.Apply.DryRun
	}
	if src.Apply.Diff != nil {
		dst.Apply.Diff = src.Apply.Diff
	}
	if src.Delete.ConfirmThreshold != nil {
		dst.Delete.ConfirmThreshold = src.Delete.ConfirmThreshold
	}
	if src.Resume.AllowDrift != nil {
		dst.Resume.AllowDrift = src.Resume.AllowDrift
	}
	if src.Resume.RerunFailed != nil {
		dst.Resume.RerunFailed = src.Resume.RerunFailed
	}
}
