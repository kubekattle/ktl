package stack

import (
	"fmt"
	"strings"
)

func ResolveStackHooksConfig(u *Universe, profile string) (StackHooksConfig, error) {
	var out StackHooksConfig
	if u == nil {
		return out, nil
	}
	sf, ok := u.Stacks[u.RootDir]
	if !ok {
		return out, nil
	}

	mergeHooksConfig(&out, sf.Hooks)
	if strings.TrimSpace(profile) != "" {
		if sp, ok := sf.Profiles[strings.TrimSpace(profile)]; ok {
			mergeHooksConfig(&out, sp.Hooks)
		}
	}

	// Resolve relative paths in hook definitions using the stack root.
	out.PreApply = resolveHookPaths(u.RootDir, out.PreApply, true)
	out.PostApply = resolveHookPaths(u.RootDir, out.PostApply, true)
	out.PreDelete = resolveHookPaths(u.RootDir, out.PreDelete, true)
	out.PostDelete = resolveHookPaths(u.RootDir, out.PostDelete, true)

	if err := ValidateHooksConfig(out, true, "stack.yaml hooks"); err != nil {
		return StackHooksConfig{}, err
	}
	return out, nil
}

func ValidateHooksConfig(cfg StackHooksConfig, allowRunOnce bool, where string) error {
	for _, h := range cfg.PreApply {
		if err := validateHookSpec(h, allowRunOnce, where+".preApply"); err != nil {
			return err
		}
	}
	for _, h := range cfg.PostApply {
		if err := validateHookSpec(h, allowRunOnce, where+".postApply"); err != nil {
			return err
		}
	}
	for _, h := range cfg.PreDelete {
		if err := validateHookSpec(h, allowRunOnce, where+".preDelete"); err != nil {
			return err
		}
	}
	for _, h := range cfg.PostDelete {
		if err := validateHookSpec(h, allowRunOnce, where+".postDelete"); err != nil {
			return err
		}
	}
	return nil
}

func validateHookSpec(h HookSpec, allowRunOnce bool, where string) error {
	if h.RunOnce && !allowRunOnce {
		return fmt.Errorf("%s: runOnce is not allowed here (only the root stack.yaml hooks can use runOnce)", where)
	}

	t := strings.ToLower(strings.TrimSpace(h.Type))
	switch t {
	case "kubectl":
		if h.Kubectl == nil || len(h.Kubectl.Args) == 0 {
			return fmt.Errorf("%s: kubectl hook requires kubectl.args", where)
		}
	case "script":
		if h.Script == nil || len(h.Script.Command) == 0 {
			return fmt.Errorf("%s: script hook requires script.command", where)
		}
	case "http":
		if h.HTTP == nil || strings.TrimSpace(h.HTTP.URL) == "" {
			return fmt.Errorf("%s: http hook requires http.url", where)
		}
	default:
		if t == "" {
			return fmt.Errorf("%s: hook type is required (kubectl|script|http)", where)
		}
		return fmt.Errorf("%s: unknown hook type %q (expected kubectl|script|http)", where, h.Type)
	}

	if h.Retry != nil && *h.Retry < 1 {
		return fmt.Errorf("%s: retry must be >= 1 (got %d)", where, *h.Retry)
	}
	if h.Timeout != nil && *h.Timeout <= 0 {
		return fmt.Errorf("%s: timeout must be > 0 (got %s)", where, *h.Timeout)
	}

	when := strings.ToLower(strings.TrimSpace(h.When))
	switch when {
	case "", "success", "failure", "always":
	default:
		return fmt.Errorf("%s: when must be success|failure|always (got %q)", where, h.When)
	}

	if strings.ToLower(strings.TrimSpace(h.Type)) != t {
		// no-op: validation normalizes at runtime
	}
	return nil
}

func filterHooksRunOnce(cfg StackHooksConfig, want bool) StackHooksConfig {
	return StackHooksConfig{
		PreApply:   filterHookList(cfg.PreApply, want),
		PostApply:  filterHookList(cfg.PostApply, want),
		PreDelete:  filterHookList(cfg.PreDelete, want),
		PostDelete: filterHookList(cfg.PostDelete, want),
	}
}

func filterHookList(in []HookSpec, wantRunOnce bool) []HookSpec {
	if len(in) == 0 {
		return nil
	}
	out := make([]HookSpec, 0, len(in))
	for _, h := range in {
		if h.RunOnce == wantRunOnce {
			out = append(out, h)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func mergeHooksConfig(dst *StackHooksConfig, src StackHooksConfig) {
	if dst == nil {
		return
	}
	dst.PreApply = append(dst.PreApply, src.PreApply...)
	dst.PostApply = append(dst.PostApply, src.PostApply...)
	dst.PreDelete = append(dst.PreDelete, src.PreDelete...)
	dst.PostDelete = append(dst.PostDelete, src.PostDelete...)
}
