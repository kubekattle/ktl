// build.go exposes the 'ktl build' pipeline, delegating to BuildKit and registry helpers to assemble and ship OCI artifacts from Compose/Build files.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/containerd/console"
	"github.com/example/ktl/pkg/buildkit"
	appcompose "github.com/example/ktl/pkg/compose"
	"github.com/example/ktl/pkg/registry"
	"github.com/mattn/go-shellwords"
	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
	"github.com/spf13/cobra"
	csvvalue "github.com/tonistiigi/go-csvvalue"

	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/tailer"
	"golang.org/x/term"
)

type buildCLIOptions struct {
	contextDir       string
	dockerfile       string
	tags             []string
	platforms        []string
	buildArgs        []string
	secrets          []string
	cacheFrom        []string
	cacheTo          []string
	push             bool
	load             bool
	noCache          bool
	builder          string
	cacheDir         string
	interactive      bool
	interactiveShell string
	buildMode        string
	composeFiles     []string
	composeProfiles  []string
	composeServices  []string
	composeProject   string
	authFile         string
	sandboxConfig    string
	sandboxBin       string
	sandboxBinds     []string
	sandboxWorkdir   string
	sandboxLogs      bool
	logFile          string
	rm               bool
	quiet            bool
	uiAddr           string
	wsListenAddr     string
}

var (
	buildRunner       = buildkit.NewRunner()
	registryClient    = registry.NewClient()
	composeRunner     = appcompose.NewRunner(buildRunner, registryClient)
	buildDockerfileFn = buildRunner.BuildDockerfile
	recordBuildFn     = registryClient.RecordBuild
)

type buildMode string

const (
	modeAuto       buildMode = "auto"
	modeDockerfile buildMode = "dockerfile"
	modeCompose    buildMode = "compose"
)

func newBuildCommand() *cobra.Command {
	opts := buildCLIOptions{
		contextDir: ".",
		dockerfile: "Dockerfile",
		builder:    buildkit.DefaultBuilderAddress(),
		cacheDir:   buildkit.DefaultCacheDir(),
		rm:         true,
	}

	cmd := &cobra.Command{
		Use:   "build CONTEXT",
		Short: "Build container images with BuildKit",
		Args:  cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireBuildContextArg(cmd, args); err != nil {
				if errors.Is(err, errMissingBuildContext) {
					_ = cmd.Help()
					return nil
				}
				return err
			}
			runOpts := opts
			if len(args) > 0 {
				runOpts.contextDir = args[0]
			}
			if !cmd.Flags().Changed("builder") {
				runOpts.builder = ""
			}
			return runBuildCommand(cmd, runOpts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	cmd.Flags().StringVarP(&opts.dockerfile, "file", "f", opts.dockerfile, "Path to the Dockerfile (default: Dockerfile)")
	cmd.Flags().StringSliceVarP(&opts.tags, "tag", "t", nil, "One or more image tags to apply to the result")
	cmd.Flags().StringSliceVar(&opts.platforms, "platform", nil, "Target platforms (comma-separated values like linux/amd64)")
	cmd.Flags().StringArrayVar(&opts.buildArgs, "build-arg", nil, "Add a build-time variable (KEY=VALUE)")
	cmd.Flags().StringArrayVar(&opts.secrets, "secret", nil, "Expose an environment variable as a BuildKit secret (NAME)")
	cmd.Flags().StringArrayVar(&opts.cacheFrom, "cache-from", nil, "Cache import sources (comma-separated key=value pairs)")
	cmd.Flags().StringArrayVar(&opts.cacheTo, "cache-to", nil, "Cache export destinations (comma-separated key=value pairs)")
	cmd.Flags().BoolVar(&opts.push, "push", false, "Push all tags to their registries after a successful build")
	cmd.Flags().BoolVar(&opts.load, "load", false, "Load the resulting image into the local container runtime (docker build --load)")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable BuildKit cache usage")
	cmd.Flags().StringVar(&opts.builder, "builder", opts.builder, "BuildKit address (override with KTL_BUILDKIT_HOST)")
	cmd.Flags().StringVar(&opts.cacheDir, "cache-dir", opts.cacheDir, "Local cache directory for BuildKit metadata")
	cmd.Flags().BoolVarP(&opts.interactive, "interactive", "i", false, "Drop into an interactive shell when a RUN step fails")
	cmd.Flags().StringVar(&opts.interactiveShell, "interactive-shell", "/bin/sh", "Shell command to start when --interactive attaches")
	cmd.Flags().StringVar(&opts.buildMode, "mode", string(modeAuto), "Build mode: auto, dockerfile, or compose")
	cmd.Flags().StringArrayVar(&opts.composeFiles, "compose-file", nil, "Compose file(s) to use when building compose projects")
	cmd.Flags().StringArrayVar(&opts.composeProfiles, "compose-profile", nil, "Compose profile(s) to enable")
	cmd.Flags().StringArrayVar(&opts.composeServices, "compose-service", nil, "Compose service(s) to build (default: all buildable services)")
	cmd.Flags().StringVar(&opts.composeProject, "compose-project", "", "Override the compose project name")
	cmd.Flags().StringVar(&opts.logFile, "logfile", "", "Log to file instead of stdout/stderr")
	cmd.Flags().BoolVar(&opts.rm, "rm", true, "Remove intermediate containers after a successful build")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Refrain from announcing build instructions and progress")
	cmd.Flags().BoolVar(&opts.sandboxLogs, "sandbox-logs", false, "Stream sandbox runtime logs to stderr and the build viewer")
	cmd.Flags().BoolVar(&opts.sandboxLogs, "nsjail-logs", false, "DEPRECATED: use --sandbox-logs")
	_ = cmd.Flags().MarkHidden("nsjail-logs")
	if legacy := cmd.Flags().Lookup("nsjail-logs"); legacy != nil {
		legacy.Deprecated = "use --sandbox-logs"
	}
	cmd.Flags().StringVar(&opts.uiAddr, "ui", "", "Serve the live BuildKit viewer at this address (e.g. :8085)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8085"
	}
	cmd.Flags().StringVar(&opts.wsListenAddr, "ws-listen", "", "Serve the raw BuildKit event stream over WebSocket at this address (e.g. :9085)")
	cmd.PersistentFlags().StringVar(&opts.authFile, "authfile", "", "Path to the authentication file (Docker config.json)")
	cmd.PersistentFlags().StringVar(&opts.sandboxConfig, "sandbox-config", "", "Path to a sandbox runtime config file")
	cmd.PersistentFlags().StringVar(&opts.sandboxConfig, "nsjail-config", "", "DEPRECATED: use --sandbox-config")
	_ = cmd.PersistentFlags().MarkHidden("nsjail-config")
	if legacy := cmd.PersistentFlags().Lookup("nsjail-config"); legacy != nil {
		legacy.Deprecated = "use --sandbox-config"
	}
	cmd.PersistentFlags().StringVar(&opts.sandboxBin, "sandbox-bin", "", "Path to the sandbox runtime binary")
	cmd.PersistentFlags().StringVar(&opts.sandboxBin, "nsjail-bin", "", "DEPRECATED: use --sandbox-bin")
	_ = cmd.PersistentFlags().MarkHidden("nsjail-bin")
	if legacy := cmd.PersistentFlags().Lookup("nsjail-bin"); legacy != nil {
		legacy.Deprecated = "use --sandbox-bin"
	}
	cmd.PersistentFlags().StringArrayVar(&opts.sandboxBinds, "sandbox-bind", nil, "Additional sandbox bind mounts (host:guest)")
	cmd.PersistentFlags().StringArrayVar(&opts.sandboxBinds, "nsjail-bind", nil, "DEPRECATED: use --sandbox-bind")
	_ = cmd.PersistentFlags().MarkHidden("nsjail-bind")
	if legacy := cmd.PersistentFlags().Lookup("nsjail-bind"); legacy != nil {
		legacy.Deprecated = "use --sandbox-bind"
	}
	cmd.PersistentFlags().StringVar(&opts.sandboxWorkdir, "sandbox-workdir", "", "Working directory inside the sandbox (default: build context)")
	cmd.PersistentFlags().StringVar(&opts.sandboxWorkdir, "nsjail-workdir", "", "DEPRECATED: use --sandbox-workdir")
	_ = cmd.PersistentFlags().MarkHidden("nsjail-workdir")
	if legacy := cmd.PersistentFlags().Lookup("nsjail-workdir"); legacy != nil {
		legacy.Deprecated = "use --sandbox-workdir"
	}

	cmd.AddCommand(newBuildLoginCommand(&opts), newBuildLogoutCommand(&opts))

	decorateCommandHelp(cmd, "Build Flags")
	return cmd
}

var errMissingBuildContext = errors.New("'ktl build' requires 1 argument (CONTEXT). Try '.' for the current directory")

func requireBuildContextArg(_ *cobra.Command, args []string) error {
	switch {
	case len(args) == 0:
		return errMissingBuildContext
	case len(args) > 1:
		return fmt.Errorf("'ktl build' accepts exactly one context argument, received %d", len(args))
	default:
		return nil
	}
}

func validateBuildMirrorFlags(opts buildCLIOptions) error {
	hasMirror := strings.TrimSpace(opts.uiAddr) != "" || strings.TrimSpace(opts.wsListenAddr) != ""
	if !hasMirror {
		return nil
	}
	if opts.quiet {
		return fmt.Errorf("--ui/--ws-listen cannot be combined with --quiet")
	}
	if strings.TrimSpace(opts.logFile) != "" {
		return fmt.Errorf("--ui/--ws-listen cannot be combined with --logfile; mirrors already capture the live stream")
	}
	return nil
}

func runBuildCommand(cmd *cobra.Command, opts buildCLIOptions) (err error) {
	if err := validateBuildMirrorFlags(opts); err != nil {
		return err
	}
	if requestedHelp(opts.uiAddr) || requestedHelp(opts.wsListenAddr) {
		return cmd.Help()
	}

	start := time.Now()
	ctx := cmd.Context()

	var logCloser io.Closer
	if logPath := strings.TrimSpace(opts.logFile); logPath != "" {
		dir := filepath.Dir(logPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return fmt.Errorf("create logfile directory: %w", err)
			}
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return fmt.Errorf("open logfile: %w", err)
		}
		logCloser = f
		cmd.SetOut(f)
		cmd.SetErr(f)
	}
	defer func() {
		if logCloser != nil {
			_ = logCloser.Close()
		}
	}()

	errOut := cmd.ErrOrStderr()

	if sandboxActive() {
		policy := strings.TrimSpace(opts.sandboxConfig)
		if policy == "" {
			policy = "embedded default"
		}
		runtime := strings.TrimSpace(opts.sandboxBin)
		if runtime == "" {
			runtime = "system default"
		}
		fmt.Fprintf(errOut, "Running ktl build inside the sandbox (policy: %s, binary: %s). Set KTL_SANDBOX_DISABLE=1 to opt out.\n", policy, runtime)
	}

	if opts.contextDir == "" {
		opts.contextDir = "."
	}
	if opts.cacheDir == "" {
		opts.cacheDir = buildkit.DefaultCacheDir()
	}

	if err := applyAuthfileEnv(opts.authFile); err != nil {
		return err
	}

	contextAbs, err := filepath.Abs(opts.contextDir)
	if err != nil {
		return err
	}

	var (
		progressObservers   []buildkit.ProgressObserver
		diagnosticObservers []buildkit.BuildDiagnosticObserver
		buildStream         *buildProgressBroadcaster
	)

	buildStream = newBuildProgressBroadcaster(filepath.Base(contextAbs))
	progressObservers = append(progressObservers, buildStream)
	if buildStream != nil {
		diagnosticObservers = append(diagnosticObservers, &buildDiagnosticObserver{
			stream: buildStream,
			writer: errOut,
		})
	}

	var consoleObserver tailer.LogObserver
	if !opts.quiet && isTerminalWriter(errOut) {
		consoleObserver = newBuildConsoleObserver(errOut)
		if consoleObserver != nil {
			buildStream.addObserver(consoleObserver)
		}
	}
	quietProgress := opts.quiet || (consoleObserver != nil && os.Getenv("BUILDKIT_PROGRESS") == "")

	var stopSandboxLogs func()
	if opts.sandboxLogs && sandboxActive() {
		logPath := sandboxLogPathFromEnv()
		if logPath == "" {
			fmt.Fprintln(errOut, "sandbox logs requested but log path unavailable")
		} else {
			var observer func(string)
			if buildStream != nil {
				observer = buildStream.emitSandboxLog
			}
			stop, streamErr := startSandboxLogStreamer(ctx, logPath, errOut, observer)
			if streamErr != nil {
				fmt.Fprintf(errOut, "sandbox logs unavailable: %v\n", streamErr)
			} else {
				stopSandboxLogs = stop
			}
		}
	}
	defer func() {
		if stopSandboxLogs != nil {
			stopSandboxLogs()
		}
	}()

	if opts.sandboxWorkdir == "" {
		opts.sandboxWorkdir = contextAbs
	}

	if injector := getSandboxInjector(); injector != nil {
		if handled, err := injector(cmd, &opts, contextAbs); err != nil {
			return err
		} else if handled {
			return nil
		}
	} else if opts.sandboxLogs {
		return fmt.Errorf("--sandbox-logs is only supported on Linux hosts with a sandbox runtime installed")
	}

	mirrorLabel := fmt.Sprintf("Context: %s", contextAbs)
	if strings.TrimSpace(opts.uiAddr) != "" || strings.TrimSpace(opts.wsListenAddr) != "" {
		logger, logErr := buildLogger("info")
		if logErr != nil {
			return logErr
		}
		if addr := strings.TrimSpace(opts.uiAddr); addr != "" {
			uiServer := caststream.New(addr, caststream.ModeWeb, mirrorLabel, logger.WithName("build-ui"), caststream.WithoutFilters(), caststream.WithoutLogTitle())
			buildStream.addObserver(uiServer)
			if err := startCastServer(ctx, uiServer, "ktl build UI", logger.WithName("build-ui"), errOut); err != nil {
				return err
			}
			fmt.Fprintf(errOut, "Serving ktl build UI on %s\n", addr)
		}
		if addr := strings.TrimSpace(opts.wsListenAddr); addr != "" {
			wsServer := caststream.New(addr, caststream.ModeWS, mirrorLabel, logger.WithName("build-ws"))
			buildStream.addObserver(wsServer)
			if err := startCastServer(ctx, wsServer, "ktl build websocket stream", logger.WithName("build-ws"), errOut); err != nil {
				return err
			}
			fmt.Fprintf(errOut, "Serving ktl websocket build stream on %s\n", addr)
		}
	}
	buildStream.emitInfo(fmt.Sprintf("Streaming ktl build from %s", opts.contextDir))
	defer func() {
		if buildStream != nil {
			buildStream.emitSummary(buildSummary{
				Tags:      append([]string(nil), opts.tags...),
				Platforms: append([]string(nil), opts.platforms...),
				Mode:      opts.buildMode,
				Push:      opts.push,
				Load:      opts.load,
			})
			buildStream.emitResult(err, time.Since(start))
		}
	}()

	mode, composeFiles, err := selectBuildMode(contextAbs, opts)
	if err != nil {
		return err
	}

	if mode == modeCompose {
		return runComposeBuildFromBuild(cmd, composeFiles, opts, progressObservers, diagnosticObservers, quietProgress, buildStream)
	}

	buildArgs, err := parseKeyValueArgs(opts.buildArgs)
	if err != nil {
		return err
	}

	cacheFrom, err := parseCacheSpecs(opts.cacheFrom)
	if err != nil {
		return err
	}
	cacheTo, err := parseCacheSpecs(opts.cacheTo)
	if err != nil {
		return err
	}
	if opts.noCache && len(cacheFrom) > 0 {
		fmt.Fprintln(errOut, "warning: ignoring --cache-from entries because --no-cache is set")
		cacheFrom = nil
	}

	platforms := buildkit.NormalizePlatforms(expandPlatforms(opts.platforms))

	tags := opts.tags
	if opts.push && len(tags) == 0 {
		return errors.New("--tag must be provided when using --push")
	}
	if len(tags) == 0 {
		tags = []string{buildkit.DefaultLocalTag(opts.contextDir)}
		fmt.Fprintf(errOut, "Defaulting to local tag %s\n", tags[0])
	}
	if buildStream != nil {
		buildStream.emitInfo(fmt.Sprintf("Target tags: %s", strings.Join(tags, ", ")))
	}

	secrets := make([]buildkit.Secret, 0, len(opts.secrets))
	for _, id := range opts.secrets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := os.LookupEnv(id); !ok {
			return fmt.Errorf("secret %s is not present in the environment", id)
		}
		secrets = append(secrets, buildkit.Secret{ID: id, Env: id})
	}

	dockerCfg, err := loadDockerConfigFile(opts.authFile, errOut)
	if err != nil {
		return err
	}
	progressOut := resolveConsoleFile(errOut)

	buildOpts := buildkit.DockerfileBuildOptions{
		BuilderAddr:          opts.builder,
		AllowBuilderFallback: opts.builder == "",
		ContextDir:           opts.contextDir,
		DockerfilePath:       opts.dockerfile,
		Platforms:            platforms,
		BuildArgs:            buildArgs,
		Secrets:              secrets,
		Tags:                 tags,
		Push:                 opts.push,
		LoadToContainerd:     opts.load,
		CacheDir:             opts.cacheDir,
		CacheExports:         cacheTo,
		CacheImports:         cacheFrom,
		NoCache:              opts.noCache,
		ProgressOutput:       progressOut,
		DockerConfig:         dockerCfg,
		ProgressObservers:    progressObservers,
		DiagnosticObservers:  diagnosticObservers,
	}
	if quietProgress {
		buildOpts.ProgressMode = "quiet"
	}

	if opts.interactive {
		tty := detectTTY(cmd)
		if tty == nil {
			return errors.New("--interactive requires a TTY. Run ktl build from an interactive terminal or omit --interactive")
		}
		shellArgs, err := parseInteractiveShell(opts.interactiveShell)
		if err != nil {
			return err
		}
		buildOpts.Interactive = &buildkit.InteractiveShellConfig{
			Shell:  shellArgs,
			Stdin:  cmd.InOrStdin(),
			Stdout: cmd.OutOrStdout(),
			Stderr: errOut,
			TTY:    tty,
		}
	}

	result, err := buildDockerfileFn(ctx, buildOpts)
	if err != nil {
		return err
	}

	if result.OCIOutputPath != "" {
		if recErr := recordBuildFn(tags, result.OCIOutputPath); recErr != nil {
			fmt.Fprintf(errOut, "warning: unable to record build metadata: %v\n", recErr)
		}
	}

	fmt.Fprintf(cmd.OutOrStdout(), "Built %s", strings.Join(tags, ", "))
	if result.Digest != "" {
		fmt.Fprintf(cmd.OutOrStdout(), " (digest %s)", result.Digest)
	}
	fmt.Fprintln(cmd.OutOrStdout())
	if result.OCIOutputPath != "" {
		rel, relErr := filepath.Rel(opts.contextDir, result.OCIOutputPath)
		if relErr != nil {
			rel = result.OCIOutputPath
		}
		fmt.Fprintf(cmd.OutOrStdout(), "OCI layout saved at %s\n", rel)
		if buildStream != nil {
			buildStream.emitInfo(fmt.Sprintf("OCI layout saved at %s", rel))
		}
	}
	if buildStream != nil && result != nil && result.Digest != "" {
		buildStream.emitInfo(fmt.Sprintf("Image digest: %s", result.Digest))
	}

	return nil
}

func parseKeyValueArgs(values []string) (map[string]string, error) {
	if len(values) == 0 {
		return map[string]string{}, nil
	}
	args := make(map[string]string, len(values))
	for _, raw := range values {
		key, val, ok := strings.Cut(raw, "=")
		if !ok {
			return nil, fmt.Errorf("invalid build argument %q (expected KEY=VALUE)", raw)
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, fmt.Errorf("invalid build argument %q (empty key)", raw)
		}
		args[key] = val
	}
	return args, nil
}

func parseCacheSpecs(values []string) ([]buildkit.CacheSpec, error) {
	specs := make([]buildkit.CacheSpec, 0, len(values))
	for _, raw := range values {
		attrs, err := parseKeyValueCSV(raw)
		if err != nil {
			return nil, err
		}
		typ := attrs["type"]
		if typ == "" {
			return nil, fmt.Errorf("cache spec %q missing type=<type>", raw)
		}
		delete(attrs, "type")
		specs = append(specs, buildkit.CacheSpec{Type: typ, Attrs: attrs})
	}
	return specs, nil
}

func parseKeyValueCSV(raw string) (map[string]string, error) {
	fields, err := csvvalue.Fields(raw, nil)
	if err != nil {
		return nil, err
	}
	attrs := make(map[string]string, len(fields))
	for _, field := range fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			attrs[strings.ToLower(strings.TrimSpace(field))] = ""
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		attrs[key] = strings.TrimSpace(value)
	}
	return attrs, nil
}

func expandPlatforms(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	set := map[string]struct{}{}
	out := make([]string, 0)
	for _, chunk := range values {
		for _, part := range strings.Split(chunk, ",") {
			trimmed := strings.TrimSpace(part)
			if trimmed == "" {
				continue
			}
			if _, ok := set[trimmed]; ok {
				continue
			}
			set[trimmed] = struct{}{}
			out = append(out, trimmed)
		}
	}
	return out
}

func runComposeBuildFromBuild(cmd *cobra.Command, composeFiles []string, opts buildCLIOptions, observers []buildkit.ProgressObserver, diagnosticObservers []buildkit.BuildDiagnosticObserver, quietProgress bool, stream *buildProgressBroadcaster) error {
	argMap, err := parseKeyValueArgs(opts.buildArgs)
	if err != nil {
		return err
	}

	dockerCfg, err := loadDockerConfigFile(opts.authFile, cmd.ErrOrStderr())
	if err != nil {
		return err
	}
	progressOut := resolveConsoleFile(cmd.ErrOrStderr())

	composeOpts := appcompose.ComposeBuildOptions{
		Files:                composeFiles,
		ProjectName:          opts.composeProject,
		Services:             opts.composeServices,
		Profiles:             opts.composeProfiles,
		BuilderAddr:          opts.builder,
		AllowBuilderFallback: opts.builder == "",
		CacheDir:             opts.cacheDir,
		Push:                 opts.push,
		Load:                 opts.load,
		NoCache:              opts.noCache,
		Platforms:            buildkit.NormalizePlatforms(expandPlatforms(opts.platforms)),
		BuildArgs:            argMap,
		ProgressOutput:       progressOut,
		DockerConfig:         dockerCfg,
		ProgressObservers:    observers,
		DiagnosticObservers:  diagnosticObservers,
	}
	if quietProgress {
		composeOpts.ProgressMode = "quiet"
	}
	if stream != nil {
		composeOpts.HeatmapListener = &heatmapStreamBridge{stream: stream}
	}

	results, err := composeRunner.BuildCompose(cmd.Context(), composeOpts)
	if err != nil {
		return err
	}

	for _, svc := range results {
		fmt.Fprintf(cmd.OutOrStdout(), "%s: %s\n", svc.Service, strings.Join(svc.Tags, ", "))
	}
	return nil
}

func selectBuildMode(contextAbs string, opts buildCLIOptions) (buildMode, []string, error) {
	mode, err := normalizeBuildMode(opts.buildMode)
	if err != nil {
		return modeDockerfile, nil, err
	}

	if mode == modeDockerfile {
		return modeDockerfile, nil, nil
	}

	explicitFiles := len(opts.composeFiles) > 0
	if mode == modeCompose {
		files := opts.composeFiles
		if !explicitFiles {
			detected, err := findComposeFiles(contextAbs)
			if err != nil {
				return modeCompose, nil, err
			}
			if len(detected) == 0 {
				return modeCompose, nil, fmt.Errorf("compose mode selected but no compose files found in %s (looked for %s)", contextAbs, strings.Join(composeDefaultFilenames, ", "))
			}
			files = detected
		}
		absFiles, err := absolutePaths(files)
		if err != nil {
			return modeCompose, nil, err
		}
		return modeCompose, absFiles, nil
	}

	if explicitFiles {
		absFiles, err := absolutePaths(opts.composeFiles)
		if err != nil {
			return modeCompose, nil, err
		}
		return modeCompose, absFiles, nil
	}

	detected, err := findComposeFiles(contextAbs)
	if err != nil {
		return modeDockerfile, nil, err
	}
	if len(detected) == 0 {
		return modeDockerfile, nil, nil
	}

	dockerfilePath := opts.dockerfile
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(contextAbs, dockerfilePath)
	}
	if fileExists(dockerfilePath) {
		return modeDockerfile, nil, nil
	}

	return modeCompose, detected, nil
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return !info.IsDir()
}

func pathExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func normalizeBuildMode(value string) (buildMode, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "", "auto":
		return modeAuto, nil
	case "docker", "dockerfile":
		return modeDockerfile, nil
	case "compose":
		return modeCompose, nil
	default:
		return modeDockerfile, fmt.Errorf("invalid build mode %q (expected auto, dockerfile, or compose)", value)
	}
}

func resolveConsoleFile(w io.Writer) console.File {
	if cf, ok := w.(console.File); ok {
		return cf
	}
	if f, ok := w.(*os.File); ok {
		return f
	}
	return os.Stderr
}

func parseInteractiveShell(raw string) ([]string, error) {
	args, err := shellwords.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse --interactive-shell: %w", err)
	}
	if len(args) == 0 {
		return nil, errors.New("--interactive-shell must contain at least one argument")
	}
	return args, nil
}

func detectTTY(cmd *cobra.Command) console.File {
	candidates := []any{cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()}
	for _, c := range candidates {
		if cf, ok := c.(console.File); ok {
			return cf
		}
		if f, ok := c.(*os.File); ok {
			return f
		}
	}
	return nil
}

func isTerminalWriter(w io.Writer) bool {
	type fdProvider interface {
		Fd() uintptr
	}
	if v, ok := w.(fdProvider); ok {
		return term.IsTerminal(int(v.Fd()))
	}
	if f, ok := w.(*os.File); ok {
		return term.IsTerminal(int(f.Fd()))
	}
	return false
}

type buildProgressBroadcaster struct {
	mu            sync.Mutex
	label         string
	observers     []tailer.LogObserver
	vertices      map[string]*buildVertexState
	seq           int
	cacheHits     int
	cacheMisses   int
	lastGraphEmit time.Time
}

func (b *buildProgressBroadcaster) emitHeatmap(summary appcompose.ServiceHeatmapSummary) {
	if b == nil {
		return
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return
	}
	line := fmt.Sprintf("Heatmap %s", string(payload))
	rec := b.newRecord(time.Now(), "⬢", "heatmap", summary.Service, "heatmap", line)
	rec.Source = "heatmap"
	rec.SourceGlyph = "⬢"
	rec.Raw = string(payload)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

type buildSummary struct {
	Tags        []string `json:"tags,omitempty"`
	Platforms   []string `json:"platforms,omitempty"`
	CacheHits   int      `json:"cacheHits,omitempty"`
	CacheMisses int      `json:"cacheMisses,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Push        bool     `json:"push,omitempty"`
	Load        bool     `json:"load,omitempty"`
}

type buildVertexState struct {
	id              string
	name            string
	cached          bool
	started         bool
	completed       bool
	errorMsg        string
	current         int64
	total           int64
	inputs          []string
	announcedStart  bool
	announcedCached bool
	announcedDone   bool
}

func (st *buildVertexState) setInputs(inputs []digest.Digest) {
	if st == nil || len(inputs) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(st.inputs))
	for _, in := range st.inputs {
		existing[in] = struct{}{}
	}
	added := false
	for _, input := range inputs {
		key := strings.TrimSpace(input.String())
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		st.inputs = append(st.inputs, key)
		existing[key] = struct{}{}
		added = true
	}
	if added {
		sort.Strings(st.inputs)
	}
}

func (st *buildVertexState) status() string {
	if st == nil {
		return "pending"
	}
	switch {
	case st.errorMsg != "":
		return "failed"
	case st.cached:
		return "cached"
	case st.completed:
		return "completed"
	case st.started:
		return "running"
	default:
		return "pending"
	}
}

func newBuildProgressBroadcaster(label string) *buildProgressBroadcaster {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "ktl-build"
	}
	return &buildProgressBroadcaster{
		label:    label,
		vertices: make(map[string]*buildVertexState),
	}
}

func (b *buildProgressBroadcaster) addObserver(observer tailer.LogObserver) {
	if b == nil || observer == nil {
		return
	}
	b.mu.Lock()
	b.observers = append(b.observers, observer)
	b.mu.Unlock()
}

func (b *buildProgressBroadcaster) emitInfo(message string) {
	b.emitCustom(strings.TrimSpace(message), "ℹ")
}

func (b *buildProgressBroadcaster) emitSummary(summary buildSummary) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if summary.CacheHits == 0 {
		summary.CacheHits = b.cacheHits
	}
	if summary.CacheMisses == 0 {
		summary.CacheMisses = b.cacheMisses
	}
	b.mu.Unlock()
	payload, err := json.Marshal(summary)
	if err != nil {
		b.emitCustom("Summary available but failed to encode payload", "ℹ")
		return
	}
	b.emitCustom(fmt.Sprintf("Summary: %s", string(payload)), "ⓘ")
}

func (b *buildProgressBroadcaster) emitResult(resultErr error, duration time.Duration) {
	if b == nil {
		return
	}
	dur := duration.Round(time.Millisecond)
	if dur < 0 {
		dur = 0
	}
	if resultErr != nil {
		b.emitCustom(fmt.Sprintf("Build failed after %s: %v", dur, resultErr), "✖")
		return
	}
	b.emitCustom(fmt.Sprintf("Build finished in %s", dur), "✔")
}

func (b *buildProgressBroadcaster) emitCustom(message, glyph string) {
	if b == nil || message == "" {
		return
	}
	rec := b.newRecord(time.Now(), glyph, "build", b.label, "info", message)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) emitDiagnostic(diag buildkit.BuildDiagnostic, message string) {
	if b == nil || message == "" {
		return
	}
	glyph := "ℹ"
	b.mu.Lock()
	switch diag.Type {
	case buildkit.DiagnosticCacheHit:
		glyph = "✔"
		b.cacheHits++
	case buildkit.DiagnosticCacheMiss:
		glyph = "⚠"
		b.cacheMisses++
	}
	b.mu.Unlock()
	rec := b.newRecord(time.Now(), glyph, "diagnostic", b.label, "diagnostic", message)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

type buildDiagnosticObserver struct {
	stream *buildProgressBroadcaster
	writer io.Writer
}

func (o *buildDiagnosticObserver) HandleDiagnostic(diag buildkit.BuildDiagnostic) {
	if o == nil {
		return
	}
	message := formatBuildDiagnostic(diag)
	if message == "" {
		return
	}
	if o.writer != nil {
		fmt.Fprintln(o.writer, message)
	}
	if o.stream != nil {
		o.stream.emitDiagnostic(diag, message)
	}
}

func (b *buildProgressBroadcaster) emitSandboxLog(line string) {
	if b == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	rec := b.newRecord(time.Now(), "⛓", "sandbox", b.label, "sandbox", line)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) snapshotObservers() []tailer.LogObserver {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]tailer.LogObserver, len(b.observers))
	copy(out, b.observers)
	return out
}

func (b *buildProgressBroadcaster) HandleStatus(status *client.SolveStatus) {
	if b == nil || status == nil {
		return
	}
	now := time.Now()
	b.mu.Lock()
	records := b.consumeStatusLocked(status, now)
	if graph := b.graphRecordLocked(now); graph != nil {
		records = append(records, *graph)
	}
	observers := append([]tailer.LogObserver(nil), b.observers...)
	b.mu.Unlock()
	b.dispatch(observers, records)
}

func (b *buildProgressBroadcaster) consumeStatusLocked(status *client.SolveStatus, now time.Time) []tailer.LogRecord {
	records := make([]tailer.LogRecord, 0)
	touched := make(map[string]*buildVertexState)
	for _, vertex := range status.Vertexes {
		st := b.vertexFor(vertex.Digest.String())
		if name := strings.TrimSpace(vertex.Name); name != "" {
			st.name = name
		}
		if vertex.Cached {
			st.cached = true
		}
		if vertex.Started != nil {
			st.started = true
		}
		if vertex.Completed != nil {
			st.completed = true
		}
		if vertex.Error != "" {
			st.errorMsg = vertex.Error
			st.completed = true
		}
		if len(vertex.Inputs) > 0 {
			st.setInputs(vertex.Inputs)
		}
		touched[st.id] = st
	}
	for _, vs := range status.Statuses {
		st := b.vertexFor(vs.Vertex.String())
		if name := strings.TrimSpace(vs.Name); name != "" {
			st.name = name
		}
		if vs.Started != nil {
			st.started = true
		}
		if vs.Completed != nil {
			st.completed = true
		}
		if vs.Current > 0 {
			st.current = vs.Current
		}
		if vs.Total > 0 {
			st.total = vs.Total
		}
		touched[st.id] = st
	}
	for _, logEntry := range status.Logs {
		line := strings.TrimRight(string(logEntry.Data), "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		st := b.vertexFor(logEntry.Vertex.String())
		ts := logEntry.Timestamp
		if ts.IsZero() {
			ts = now
		}
		recordLine := fmt.Sprintf("%s | %s", st.displayName(b.label), line)
		records = append(records, b.newRecord(ts, streamGlyph(logEntry.Stream), "build", st.displayName(b.label), streamLabel(logEntry.Stream), recordLine))
	}
	for _, st := range touched {
		records = append(records, b.transitionRecords(st, now)...)
	}
	return records
}

func (b *buildProgressBroadcaster) vertexFor(key string) *buildVertexState {
	key = strings.TrimSpace(key)
	if key == "" {
		b.seq++
		key = fmt.Sprintf("vertex-%d", b.seq)
	}
	if v, ok := b.vertices[key]; ok {
		return v
	}
	state := &buildVertexState{id: key}
	b.vertices[key] = state
	return state
}

func (b *buildProgressBroadcaster) newRecord(ts time.Time, glyph, namespace, pod, container, line string) tailer.LogRecord {
	if ts.IsZero() {
		ts = time.Now()
	}
	if namespace = strings.TrimSpace(namespace); namespace == "" {
		namespace = "build"
	}
	if pod = strings.TrimSpace(pod); pod == "" {
		pod = b.label
	}
	container = strings.TrimSpace(container)
	display := ts.Local().Format("15:04:05")
	return tailer.LogRecord{
		Timestamp:          ts,
		FormattedTimestamp: display,
		Namespace:          namespace,
		Pod:                pod,
		Container:          container,
		Rendered:           line,
		Raw:                line,
		Source:             "build",
		SourceGlyph:        glyph,
		RenderedEqualsRaw:  true,
	}
}

func (b *buildProgressBroadcaster) transitionRecords(st *buildVertexState, now time.Time) []tailer.LogRecord {
	name := st.displayName(b.label)
	ts := now
	records := make([]tailer.LogRecord, 0, 3)
	if st.started && !st.announcedStart {
		st.announcedStart = true
		records = append(records, b.newRecord(ts, "▶", "build", name, "status", fmt.Sprintf("Started %s", name)))
	}
	if st.cached && !st.announcedCached {
		st.announcedCached = true
		records = append(records, b.newRecord(ts, "⚡", "build", name, "status", fmt.Sprintf("Reused cache for %s", name)))
	}
	if st.completed && !st.announcedDone {
		st.announcedDone = true
		glyph := "✔"
		text := fmt.Sprintf("Completed %s", name)
		if st.errorMsg != "" {
			glyph = "✖"
			text = fmt.Sprintf("Failed %s: %s", name, st.errorMsg)
		}
		if progress := formatProgress(st.current, st.total); progress != "" {
			text = fmt.Sprintf("%s (%s)", text, progress)
		}
		records = append(records, b.newRecord(ts, glyph, "build", name, "status", text))
	}
	return records
}

type buildGraphSnapshot struct {
	Nodes []buildGraphNode `json:"nodes"`
	Edges []buildGraphEdge `json:"edges"`
}

type buildGraphNode struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Status  string `json:"status"`
	Cached  bool   `json:"cached"`
	Current int64  `json:"current,omitempty"`
	Total   int64  `json:"total,omitempty"`
	Error   string `json:"error,omitempty"`
}

type buildGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (b *buildProgressBroadcaster) graphRecordLocked(now time.Time) *tailer.LogRecord {
	if b == nil || len(b.vertices) == 0 {
		return nil
	}
	if !b.lastGraphEmit.IsZero() && now.Sub(b.lastGraphEmit) < 450*time.Millisecond {
		return nil
	}
	snapshot := b.buildGraphSnapshotLocked()
	if len(snapshot.Nodes) == 0 {
		return nil
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	b.lastGraphEmit = now
	rec := b.newRecord(now, "◆", "graph", b.label, "graph", "build graph update")
	rec.Source = "graph"
	rec.Raw = string(payload)
	rec.Rendered = rec.Raw
	return &rec
}

func (b *buildProgressBroadcaster) buildGraphSnapshotLocked() buildGraphSnapshot {
	nodes := make([]buildGraphNode, 0, len(b.vertices))
	edges := make([]buildGraphEdge, 0)
	for _, st := range b.vertices {
		nodes = append(nodes, buildGraphNode{
			ID:      st.id,
			Label:   st.displayName(b.label),
			Status:  st.status(),
			Cached:  st.cached,
			Current: st.current,
			Total:   st.total,
			Error:   st.errorMsg,
		})
		for _, input := range st.inputs {
			if input == "" {
				continue
			}
			edges = append(edges, buildGraphEdge{From: input, To: st.id})
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Status == nodes[j].Status {
			return nodes[i].Label < nodes[j].Label
		}
		return nodes[i].Status < nodes[j].Status
	})
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	return buildGraphSnapshot{
		Nodes: nodes,
		Edges: edges,
	}
}

func (b *buildProgressBroadcaster) dispatch(observers []tailer.LogObserver, records []tailer.LogRecord) {
	if len(observers) == 0 || len(records) == 0 {
		return
	}
	for _, rec := range records {
		for _, obs := range observers {
			obs.ObserveLog(rec)
		}
	}
}

func (st *buildVertexState) displayName(fallback string) string {
	if name := strings.TrimSpace(st.name); name != "" {
		return name
	}
	if st.id != "" {
		return st.id
	}
	return fallback
}

func formatProgress(current, total int64) string {
	if total <= 0 {
		if current <= 0 {
			return ""
		}
		return fmt.Sprintf("%d", current)
	}
	if current < 0 {
		current = 0
	}
	return fmt.Sprintf("%d/%d", current, total)
}

type heatmapStreamBridge struct {
	stream *buildProgressBroadcaster
}

func (h *heatmapStreamBridge) HandleServiceHeatmap(summary appcompose.ServiceHeatmapSummary) {
	if h == nil || h.stream == nil {
		return
	}
	h.stream.emitHeatmap(summary)
}

func streamGlyph(stream int) string {
	switch stream {
	case 2:
		return "!"
	default:
		return "•"
	}
}

func streamLabel(stream int) string {
	switch stream {
	case 2:
		return "stderr"
	default:
		return "stdout"
	}
}

func formatBuildDiagnostic(diag buildkit.BuildDiagnostic) string {
	parts := make([]string, 0, 3)
	reason := strings.TrimSpace(diag.Reason)
	if reason == "" {
		reason = "build diagnostic"
	}
	parts = append(parts, reason)
	if name := strings.TrimSpace(diag.Name); name != "" {
		parts = append(parts, fmt.Sprintf("step: %s", name))
	}
	if v := strings.TrimSpace(string(diag.Vertex)); v != "" {
		parts = append(parts, fmt.Sprintf("vertex: %s", v))
	}
	return strings.Join(parts, " | ")
}
