package stack

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

type hookedExecutor struct {
	base   NodeExecutor
	run    *runState
	opts   RunOptions
	out    io.Writer
	errOut io.Writer
}

func (h *hookedExecutor) RunNode(ctx context.Context, node *runNode, command string) error {
	if node == nil {
		return fmt.Errorf("node is nil")
	}
	if h.base == nil {
		return fmt.Errorf("executor is nil")
	}

	pre, post := hooksForNode(node, command)
	if err := runHookList(ctx, hookRunContext{
		run:     h.run,
		opts:    h.opts,
		node:    node,
		errOut:  h.errOut,
		phase:   "pre-" + command,
		status:  "success",
		baseDir: node.Dir,
	}, pre); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}

	if err := h.base.RunNode(ctx, node, command); err != nil {
		return err
	}

	if err := runHookList(ctx, hookRunContext{
		run:     h.run,
		opts:    h.opts,
		node:    node,
		errOut:  h.errOut,
		phase:   "post-" + command,
		status:  "success",
		baseDir: node.Dir,
	}, post); err != nil {
		return wrapNodeErr(node.ResolvedRelease, err)
	}
	return nil
}

func hooksForNode(node *runNode, command string) (pre []HookSpec, post []HookSpec) {
	if node == nil {
		return nil, nil
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "apply":
		return node.Hooks.PreApply, node.Hooks.PostApply
	case "delete":
		return node.Hooks.PreDelete, node.Hooks.PostDelete
	default:
		return nil, nil
	}
}

type hookRunContext struct {
	run     *runState
	opts    RunOptions
	node    *runNode
	errOut  io.Writer
	phase   string // e.g. pre-apply, post-delete
	status  string // success|failure (for "when" evaluation)
	baseDir string
}

func runHookList(ctx context.Context, hc hookRunContext, hooks []HookSpec) error {
	if len(hooks) == 0 {
		return nil
	}
	for i := range hooks {
		hook := hooks[i]
		if !shouldRunHook(hook, hc.status, hc.phase) {
			if hc.run != nil {
				nodeID := ""
				attempt := 0
				if hc.node != nil {
					nodeID = hc.node.ID
					attempt = hc.node.Attempt
				}
				name := strings.TrimSpace(hook.Name)
				if name == "" {
					name = strings.TrimSpace(hook.Type)
					if name == "" {
						name = "hook"
					}
				}

				effectiveWhen := strings.ToLower(strings.TrimSpace(hook.When))
				if effectiveWhen == "" {
					effectiveWhen = "success"
					if strings.HasPrefix(strings.ToLower(strings.TrimSpace(hc.phase)), "pre-") {
						effectiveWhen = "always"
					}
				}
				desc := fmt.Sprintf("%s %s skipped", strings.TrimSpace(hc.phase), name)
				hc.run.AppendEvent(nodeID, HookSkipped, attempt, desc, map[string]any{
					"hook":    strings.TrimSpace(name),
					"phase":   strings.TrimSpace(hc.phase),
					"when":    effectiveWhen,
					"runOnce": hook.RunOnce,
					"type":    strings.TrimSpace(hook.Type),
					"summary": hookCommandSummary(hook),
					"reason":  fmt.Sprintf("status=%s", strings.ToLower(strings.TrimSpace(hc.status))),
				}, nil)
			}
			continue
		}
		if err := runOneHook(ctx, hc, hook); err != nil {
			return err
		}
	}
	return nil
}

func shouldRunHook(h HookSpec, status string, phase string) bool {
	when := strings.ToLower(strings.TrimSpace(h.When))
	if when == "" {
		when = "success"
		if strings.HasPrefix(strings.ToLower(phase), "pre-") {
			when = "always"
		}
	}
	switch when {
	case "always":
		return true
	case "success":
		return strings.ToLower(status) == "success"
	case "failure":
		return strings.ToLower(status) == "failure"
	default:
		return false
	}
}

func runOneHook(ctx context.Context, hc hookRunContext, hook HookSpec) error {
	nodeID := ""
	attempt := 0
	if hc.node != nil {
		nodeID = hc.node.ID
		attempt = hc.node.Attempt
	}

	name := strings.TrimSpace(hook.Name)
	if name == "" {
		name = strings.TrimSpace(hook.Type)
		if name == "" {
			name = "hook"
		}
	}
	desc := fmt.Sprintf("%s %s", strings.TrimSpace(hc.phase), name)

	effectiveWhen := strings.ToLower(strings.TrimSpace(hook.When))
	if effectiveWhen == "" {
		effectiveWhen = "success"
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(hc.phase)), "pre-") {
			effectiveWhen = "always"
		}
	}
	summary := hookCommandSummary(hook)
	hookType := strings.TrimSpace(hook.Type)

	if hc.run != nil {
		hc.run.AppendEvent(nodeID, HookStarted, attempt, desc, map[string]any{
			"hook":    strings.TrimSpace(name),
			"phase":   strings.TrimSpace(hc.phase),
			"when":    effectiveWhen,
			"runOnce": hook.RunOnce,
			"type":    hookType,
			"summary": summary,
		}, nil)
	}

	maxAttempts := 1
	if hook.Retry != nil {
		maxAttempts = *hook.Retry
	}
	if maxAttempts < 1 {
		maxAttempts = 1
	}

	var lastErr error
	for try := 1; try <= maxAttempts; try++ {
		timeout := 5 * time.Minute
		if hook.Timeout != nil {
			timeout = *hook.Timeout
		}
		tryCtx := ctx
		cancel := func() {}
		if timeout > 0 {
			tryCtx, cancel = context.WithTimeout(ctx, timeout)
		}

		lastErr = runOneHookAttempt(tryCtx, hc, hook, desc)
		cancel()
		if lastErr == nil {
			if hc.run != nil {
				hc.run.AppendEvent(nodeID, HookSucceeded, attempt, desc, map[string]any{
					"hook":    strings.TrimSpace(name),
					"phase":   strings.TrimSpace(hc.phase),
					"when":    effectiveWhen,
					"runOnce": hook.RunOnce,
					"type":    hookType,
					"summary": summary,
				}, nil)
			}
			return nil
		}

		if try < maxAttempts {
			backoff := time.Duration(try) * 500 * time.Millisecond
			if hc.run != nil {
				hc.run.EmitEphemeralEvent(nodeID, NodeLog, attempt, fmt.Sprintf("hook %s failed (attempt %d/%d): %v (retrying in %s)", desc, try, maxAttempts, lastErr, backoff), map[string]any{"hook": strings.TrimSpace(name), "attempt": try, "maxAttempts": maxAttempts, "backoff": backoff.String()})
			}
			select {
			case <-ctx.Done():
				break
			case <-time.After(backoff):
			}
		}
	}

	if hc.run != nil {
		hc.run.AppendEvent(nodeID, HookFailed, attempt, fmt.Sprintf("%s: %v", desc, lastErr), map[string]any{
			"hook":    strings.TrimSpace(name),
			"phase":   strings.TrimSpace(hc.phase),
			"when":    effectiveWhen,
			"runOnce": hook.RunOnce,
			"type":    hookType,
			"summary": summary,
		}, &RunError{Class: "HOOK_FAILED", Message: lastErr.Error(), Digest: computeRunErrorDigest("HOOK_FAILED", lastErr.Error())})
	}
	return lastErr
}

func hookCommandSummary(h HookSpec) string {
	switch strings.ToLower(strings.TrimSpace(h.Type)) {
	case "script":
		if h.Script == nil || len(h.Script.Command) == 0 {
			return ""
		}
		// Avoid extremely long lines; keep the first few args.
		cmd := h.Script.Command
		if len(cmd) > 8 {
			cmd = append(append([]string(nil), cmd[:8]...), "…")
		}
		return strings.Join(cmd, " ")
	case "kubectl":
		if h.Kubectl == nil || len(h.Kubectl.Args) == 0 {
			return ""
		}
		args := h.Kubectl.Args
		if len(args) > 10 {
			args = append(append([]string(nil), args[:10]...), "…")
		}
		return "kubectl " + strings.Join(args, " ")
	case "http":
		if h.HTTP == nil {
			return ""
		}
		method := strings.TrimSpace(h.HTTP.Method)
		if method == "" {
			if strings.TrimSpace(h.HTTP.Body) != "" {
				method = http.MethodPost
			} else {
				method = http.MethodGet
			}
		}
		url := strings.TrimSpace(h.HTTP.URL)
		if url == "" {
			return ""
		}
		return method + " " + url
	default:
		return ""
	}
}

func runOneHookAttempt(ctx context.Context, hc hookRunContext, hook HookSpec, desc string) error {
	switch strings.ToLower(strings.TrimSpace(hook.Type)) {
	case "kubectl":
		return runKubectlHook(ctx, hc, hook, desc)
	case "script":
		return runScriptHook(ctx, hc, hook, desc)
	case "http":
		return runHTTPHook(ctx, hc, hook, desc)
	default:
		return fmt.Errorf("unknown hook type %q", hook.Type)
	}
}

func runKubectlHook(ctx context.Context, hc hookRunContext, hook HookSpec, desc string) error {
	if hook.Kubectl == nil || len(hook.Kubectl.Args) == 0 {
		return fmt.Errorf("kubectl hook missing kubectl.args")
	}

	kubeconfigPath, kubeCtx := effectiveKubeContext(hc, hook)
	namespace := effectiveNamespace(hc, hook)

	args := append([]string(nil), hook.Kubectl.Args...)
	if kubeconfigPath != "" {
		args = append([]string{"--kubeconfig", kubeconfigPath}, args...)
	}
	if kubeCtx != "" {
		args = append([]string{"--context", kubeCtx}, args...)
	}
	if namespace != "" && !argsContainNamespace(args) {
		args = append([]string{"-n", namespace}, args...)
	}

	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Dir = chooseWorkDir(hc, hook)
	out, err := cmd.CombinedOutput()
	emitHookOutput(hc, desc, out)
	if err != nil {
		return fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runScriptHook(ctx context.Context, hc hookRunContext, hook HookSpec, desc string) error {
	if hook.Script == nil || len(hook.Script.Command) == 0 {
		return fmt.Errorf("script hook missing script.command")
	}
	cmd := exec.CommandContext(ctx, hook.Script.Command[0], hook.Script.Command[1:]...)
	cmd.Dir = chooseWorkDir(hc, hook)
	cmd.Env = buildHookEnv(hc, hook)
	out, err := cmd.CombinedOutput()
	emitHookOutput(hc, desc, out)
	if err != nil {
		return fmt.Errorf("script %s: %w", strings.Join(hook.Script.Command, " "), err)
	}
	return nil
}

func runHTTPHook(ctx context.Context, hc hookRunContext, hook HookSpec, desc string) error {
	if hook.HTTP == nil {
		return fmt.Errorf("http hook missing http config")
	}
	url := strings.TrimSpace(hook.HTTP.URL)
	if url == "" {
		return fmt.Errorf("http hook missing http.url")
	}
	method := strings.TrimSpace(hook.HTTP.Method)
	if method == "" {
		if strings.TrimSpace(hook.HTTP.Body) != "" {
			method = http.MethodPost
		} else {
			method = http.MethodGet
		}
	}

	req, err := http.NewRequestWithContext(ctx, method, url, strings.NewReader(hook.HTTP.Body))
	if err != nil {
		return err
	}
	for k, v := range hook.HTTP.Headers {
		req.Header.Set(k, v)
	}

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 64*1024))
	if resp.StatusCode >= 400 {
		msg := strings.TrimSpace(string(body))
		if msg == "" {
			msg = resp.Status
		}
		return fmt.Errorf("http %s %s failed: %s", method, url, msg)
	}
	emitHookOutput(hc, desc, body)
	return nil
}

func emitHookOutput(hc hookRunContext, desc string, output []byte) {
	if len(output) == 0 || hc.run == nil {
		return
	}
	nodeID := ""
	attempt := 0
	if hc.node != nil {
		nodeID = hc.node.ID
		attempt = hc.node.Attempt
	}
	text := string(output)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	lines := strings.Split(text, "\n")
	for _, line := range lines {
		line = strings.TrimRight(line, " \t")
		if strings.TrimSpace(line) == "" {
			continue
		}
		hc.run.EmitEphemeralEvent(nodeID, NodeLog, attempt, fmt.Sprintf("hook-output %s: %s", strings.TrimSpace(desc), line), map[string]any{"kind": "hook-output"})
	}
}

func argsContainNamespace(args []string) bool {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-n", "--namespace":
			return true
		}
	}
	return false
}

func chooseWorkDir(hc hookRunContext, hook HookSpec) string {
	if hook.Script != nil && strings.TrimSpace(hook.Script.WorkDir) != "" {
		return strings.TrimSpace(hook.Script.WorkDir)
	}
	if strings.TrimSpace(hc.baseDir) != "" {
		return strings.TrimSpace(hc.baseDir)
	}
	return ""
}

func effectiveKubeContext(hc hookRunContext, hook HookSpec) (kubeconfigPath string, kubeCtx string) {
	if strings.TrimSpace(hook.Kubeconfig) != "" {
		kubeconfigPath = expandTilde(hook.Kubeconfig)
	} else if hc.node != nil && strings.TrimSpace(hc.node.Cluster.Kubeconfig) != "" {
		kubeconfigPath = expandTilde(hc.node.Cluster.Kubeconfig)
	} else if hc.opts.Kubeconfig != nil {
		kubeconfigPath = strings.TrimSpace(*hc.opts.Kubeconfig)
	}
	if strings.TrimSpace(hook.Context) != "" {
		kubeCtx = strings.TrimSpace(hook.Context)
	} else if hc.node != nil && strings.TrimSpace(hc.node.Cluster.Context) != "" {
		kubeCtx = strings.TrimSpace(hc.node.Cluster.Context)
	} else if hc.opts.KubeContext != nil {
		kubeCtx = strings.TrimSpace(*hc.opts.KubeContext)
	}
	return kubeconfigPath, kubeCtx
}

func effectiveNamespace(hc hookRunContext, hook HookSpec) string {
	if strings.TrimSpace(hook.Namespace) != "" {
		return strings.TrimSpace(hook.Namespace)
	}
	if hc.node != nil {
		return strings.TrimSpace(hc.node.Namespace)
	}
	// Stack-level hook: infer a single namespace if possible.
	if hc.opts.Plan != nil {
		set := map[string]struct{}{}
		for _, n := range hc.opts.Plan.Nodes {
			ns := strings.TrimSpace(n.Namespace)
			if ns == "" {
				ns = "default"
			}
			set[ns] = struct{}{}
		}
		if len(set) == 1 {
			for ns := range set {
				return ns
			}
		}
	}
	return ""
}

func buildHookEnv(hc hookRunContext, hook HookSpec) []string {
	env := append([]string(nil), os.Environ()...)

	stackRoot := ""
	stackProfile := ""
	runID := ""
	stackCommand := ""
	if hc.opts.Plan != nil {
		stackRoot = strings.TrimSpace(hc.opts.Plan.StackRoot)
		stackProfile = strings.TrimSpace(hc.opts.Plan.Profile)
	}
	if hc.run != nil {
		runID = strings.TrimSpace(hc.run.RunID)
	}
	stackCommand = strings.TrimSpace(hc.opts.Command)

	env = append(env,
		"KTL_STACK_ROOT="+stackRoot,
		"KTL_STACK_PROFILE="+stackProfile,
		"KTL_STACK_RUN_ID="+runID,
		"KTL_STACK_COMMAND="+stackCommand,
	)
	kc, kctx := effectiveKubeContext(hc, hook)
	if kc != "" {
		env = append(env, "KUBECONFIG="+kc)
	}
	if kctx != "" {
		env = append(env, "KUBE_CONTEXT="+kctx)
	}

	if hc.node != nil {
		env = append(env,
			"KTL_RELEASE_ID="+hc.node.ID,
			"KTL_RELEASE_NAME="+hc.node.Name,
			"KTL_RELEASE_DIR="+hc.node.Dir,
			"KTL_RELEASE_NAMESPACE="+hc.node.Namespace,
			"KTL_CLUSTER_NAME="+hc.node.Cluster.Name,
		)
	}

	if hook.Script != nil && hook.Script.Env != nil {
		keys := make([]string, 0, len(hook.Script.Env))
		for k := range hook.Script.Env {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			env = append(env, k+"="+hook.Script.Env[k])
		}
	}
	return env
}

func hooksForRunOnce(p *Plan, command string, pre bool) []HookSpec {
	if p == nil {
		return nil
	}
	switch strings.ToLower(strings.TrimSpace(command)) {
	case "apply":
		if pre {
			return p.Hooks.PreApply
		}
		return p.Hooks.PostApply
	case "delete":
		if pre {
			return p.Hooks.PreDelete
		}
		return p.Hooks.PostDelete
	default:
		return nil
	}
}
