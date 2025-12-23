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
	}
}
