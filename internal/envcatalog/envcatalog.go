package envcatalog

type VarInfo struct {
	Category    string
	Name        string
	Description string
	Dynamic     bool
	Internal    bool
}

func Catalog() []VarInfo {
	return []VarInfo{
		{
			Category:    "Config",
			Name:        "KTL_CONFIG",
			Description: "Path to the ktl config file.",
		},
		{
			Category:    "Config",
			Name:        "KTL_<FLAG>",
			Dynamic:     true,
			Description: "Set any ktl CLI flag via environment (hyphens become underscores). Example: KTL_NAMESPACE=default.",
		},
		{
			Category:    "Output",
			Name:        "NO_COLOR",
			Description: "Disable ANSI color output (any non-empty value).",
		},
		{
			Category:    "CLI",
			Name:        "KTL_YES",
			Description: "Auto-approve confirmations (equivalent to passing --yes).",
		},
		{
			Category:    "Logging",
			Name:        "KTL_KUBE_LOG_LEVEL",
			Description: "Kubernetes client-go verbosity (klog -v). At >=6 enables HTTP request/response tracing.",
		},
		{
			Category:    "Profiling",
			Name:        "KTL_PROFILE",
			Description: "Enable profiling modes for ktl itself (e.g. startup writes CPU/heap profiles to the working directory).",
		},
		{
			Category:    "Features",
			Name:        "KTL_FEATURE_<FLAG>",
			Dynamic:     true,
			Description: "Enable an experimental feature flag (repeatable via env). Example: KTL_FEATURE_DEPLOY_PLAN_HTML_V3=1.",
		},
		{
			Category:    "Build",
			Name:        "KTL_BUILDKIT_HOST",
			Description: "Override the BuildKit address used by `ktl build`.",
		},
		{
			Category:    "Build",
			Name:        "KTL_BUILDKIT_CACHE",
			Description: "Configure BuildKit cache import/export for `ktl build`.",
		},
		{
			Category:    "Build",
			Name:        "KTL_DOCKER_CONTEXT",
			Description: "Docker context to use for Buildx fallback (when provisioning a Docker-backed BuildKit builder).",
		},
		{
			Category:    "Build",
			Name:        "KTL_DOCKER_CONFIG",
			Description: "Override Docker config directory for Buildx fallback (equivalent to DOCKER_CONFIG).",
		},
		{
			Category:    "Registry",
			Name:        "KTL_AUTHFILE",
			Description: "Path to a container registry auth file for `ktl build` (containers-auth.json).",
		},
		{
			Category:    "Registry",
			Name:        "KTL_REGISTRY_AUTH_FILE",
			Description: "Alternate registry auth file path for `ktl build`.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_DISABLE",
			Description: "Disable sandbox execution where supported (set to 1).",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CONFIG",
			Description: "Path to the sandbox policy configuration file.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_ACTIVE",
			Internal:    true,
			Description: "Internal marker set inside the sandbox runtime.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_LOG_PATH",
			Internal:    true,
			Description: "Internal path used by the sandbox to mirror diagnostics/logs.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CONTEXT",
			Internal:    true,
			Description: "Internal sandbox context marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_CACHE",
			Internal:    true,
			Description: "Internal sandbox cache marker.",
		},
		{
			Category:    "Sandbox",
			Name:        "KTL_SANDBOX_BUILDER",
			Internal:    true,
			Description: "Internal sandbox builder marker.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_DISABLE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_DISABLE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_ACTIVE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_ACTIVE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_LOG_PATH",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_LOG_PATH.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_CONTEXT",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_CONTEXT.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_CACHE",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_CACHE.",
		},
		{
			Category:    "Sandbox (Legacy)",
			Name:        "KTL_NSJAIL_BUILDER",
			Internal:    true,
			Description: "Legacy alias for KTL_SANDBOX_BUILDER.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_QUEUE_SIZE",
			Description: "Capture recorder in-memory queue size.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_BATCH_SIZE",
			Description: "Capture recorder flush batch size.",
		},
		{
			Category:    "Capture",
			Name:        "KTL_CAPTURE_FLUSH_MS",
			Description: "Capture recorder flush interval in milliseconds.",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_ROOT",
			Description: "Default stack root for `ktl stack ...` when --root is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_PROFILE",
			Description: "Default stack profile overlay for `ktl stack ...` when --profile is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_OUTPUT",
			Description: "Default output format for `ktl stack` commands when --output is not provided (table|json).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_CLUSTER",
			Description: "Default cluster filter for `ktl stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_TAG",
			Description: "Default tag selector for `ktl stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_FROM_PATH",
			Description: "Default from-path selector for `ktl stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_RELEASE",
			Description: "Default release selector for `ktl stack` selection (comma-separated).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_GIT_RANGE",
			Description: "Default git diff range selector for `ktl stack` selection (example: origin/main...HEAD).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_GIT_INCLUDE_DEPS",
			Description: "When using KTL_STACK_GIT_RANGE, include dependencies (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_GIT_INCLUDE_DEPENDENTS",
			Description: "When using KTL_STACK_GIT_RANGE, include dependents (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_INCLUDE_DEPS",
			Description: "Include dependencies in selection expansion (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_INCLUDE_DEPENDENTS",
			Description: "Include dependents in selection expansion (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_ALLOW_MISSING_DEPS",
			Description: "Allow missing dependencies when selecting a subset (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_INFER_DEPS",
			Description: "Enable inferred dependencies when not explicitly set via flags (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_INFER_CONFIG_REFS",
			Description: "Enable inferred ConfigMap/Secret reference edges when inferring deps (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_APPLY_DRY_RUN",
			Description: "Default `ktl stack apply --dry-run` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_APPLY_DIFF",
			Description: "Default `ktl stack apply --diff` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_DELETE_CONFIRM_THRESHOLD",
			Description: "Default delete confirmation threshold for `ktl stack delete` when the flag is not provided.",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_RESUME_ALLOW_DRIFT",
			Description: "Default `ktl stack --allow-drift` value when the flag is not provided (set to 1/true).",
		},
		{
			Category:    "Stack",
			Name:        "KTL_STACK_RESUME_RERUN_FAILED",
			Description: "Default `ktl stack --rerun-failed` value when the flag is not provided (set to 1/true).",
		},
	}
}
