package stack

import "strings"

func mergeHooks(dst *ResolvedRelease, baseDir string, src StackHooksConfig) {
	if dst == nil {
		return
	}
	dst.Hooks.PreApply = append(dst.Hooks.PreApply, resolveHookPaths(baseDir, src.PreApply, false)...)
	dst.Hooks.PostApply = append(dst.Hooks.PostApply, resolveHookPaths(baseDir, src.PostApply, false)...)
	dst.Hooks.PreDelete = append(dst.Hooks.PreDelete, resolveHookPaths(baseDir, src.PreDelete, false)...)
	dst.Hooks.PostDelete = append(dst.Hooks.PostDelete, resolveHookPaths(baseDir, src.PostDelete, false)...)
}

func resolveHookPaths(baseDir string, hooks []HookSpec, allowRunOnce bool) []HookSpec {
	if len(hooks) == 0 {
		return nil
	}
	out := make([]HookSpec, 0, len(hooks))
	for _, h := range hooks {
		if h.RunOnce && !allowRunOnce {
			// runOnce hooks are handled at the stack level; never attach to nodes.
			continue
		}
		cp := h
		cp.Type = strings.ToLower(strings.TrimSpace(cp.Type))

		if cp.Kubectl != nil && len(cp.Kubectl.Args) > 0 {
			cp.Kubectl = &KubectlHookConfig{Args: resolveKubectlArgs(baseDir, cp.Kubectl.Args)}
		}
		if cp.Script != nil {
			s := *cp.Script
			if strings.TrimSpace(s.WorkDir) != "" {
				s.WorkDir = resolvePath(baseDir, s.WorkDir)
			}
			if len(s.Command) > 0 {
				s.Command = resolveScriptCommand(baseDir, s.Command)
			}
			cp.Script = &s
		}
		out = append(out, cp)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func resolveKubectlArgs(baseDir string, args []string) []string {
	out := append([]string(nil), args...)
	for i := 0; i < len(out)-1; i++ {
		switch out[i] {
		case "-f", "--filename":
			out[i+1] = resolvePath(baseDir, out[i+1])
		}
	}
	return out
}

func resolveScriptCommand(baseDir string, cmd []string) []string {
	if len(cmd) == 0 {
		return nil
	}
	out := append([]string(nil), cmd...)
	first := strings.TrimSpace(out[0])
	if first == "" {
		return out
	}
	if strings.Contains(first, "/") || strings.HasPrefix(first, ".") {
		out[0] = resolvePath(baseDir, first)
	}
	return out
}
