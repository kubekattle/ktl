// File: cmd/ktl/build.go
// Brief: CLI command wiring and implementation for 'build'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/grpcutil"
	"github.com/example/ktl/internal/logging"
	"github.com/example/ktl/internal/mirrorbus"
	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/example/ktl/pkg/api/v1"
	"github.com/example/ktl/pkg/buildkit"
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
	sbom             bool
	provenance       bool
	hermetic         bool
	allowNetwork     bool
	allowUnpinned    bool
	policyRef        string
	policyMode       string
	policyReportPath string
	secretsMode      string
	secretsReport    string
	secretsConfig    string
	profile          string
	intentSecure     bool
	intentPublish    bool
	intentOCI        bool
	wizard           bool
	attestDir        string
	capturePath      string
	captureTags      []string
	push             bool
	load             bool
	noCache          bool
	builder          string
	cacheDir         string
	sign             bool
	signKey          string
	rekorURL         string
	tlogUpload       string
	interactive      bool
	interactiveShell string
	buildMode        string
	composeFiles     []string
	composeProfiles  []string
	composeServices  []string
	composeProject   string
	composeParallel  int
	authFile         string
	sandboxConfig    string
	sandboxBin       string
	sandboxBinds     []string
	sandboxBindHome  bool
	sandboxWorkdir   string
	sandboxLogs      bool
	sandboxProbePath string
	sandboxRequired  bool
	logFile          string
	rm               bool
	quiet            bool
	wsListenAddr     string
	remoteAddr       string
}

var defaultBuildService buildsvc.Service = buildsvc.New(buildsvc.Dependencies{})

func newBuildCommand() *cobra.Command {
	return newBuildCommandWithService(defaultBuildService)
}

func newBuildCommandWithService(service buildsvc.Service) *cobra.Command {
	if service == nil {
		service = defaultBuildService
	}
	opts := buildCLIOptions{
		contextDir:  ".",
		dockerfile:  "Dockerfile",
		builder:     buildkit.DefaultBuilderAddress(),
		cacheDir:    buildkit.DefaultCacheDir(),
		rm:          true,
		policyMode:  "enforce",
		secretsMode: "warn",
		profile:     "dev",
	}

	cmd := &cobra.Command{
		Use:   "build CONTEXT",
		Short: "Build container images with BuildKit",
		Example: `  # Build the current directory
  ktl build .

  # Enforce an OPA/Rego policy bundle (writes JSON evidence to dist/attest)
  ktl build . --attest-dir dist/attest --policy ./examples/policy/demo --policy-mode enforce

  # Auto-detect a compose project and build all services
  ktl build ./testdata/build/compose

  # Build from a compose file directly and limit parallelism
  ktl build ./testdata/build/compose/docker-compose.yml --compose-parallelism 2

  # Build with tags and push
  ktl build . -f Dockerfile -t ghcr.io/acme/app:latest --push`,
		Args: func(cmd *cobra.Command, args []string) error {
			if err := requireBuildContextArg(cmd, args); err != nil {
				if errors.Is(err, errMissingBuildContext) {
					_ = cmd.Help()
				}
				return err
			}
			return nil
		},
		PreRunE: func(cmd *cobra.Command, args []string) error {
			return validateBuildMirrorFlags(opts)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			runOpts := opts
			if len(args) > 0 {
				runOpts.contextDir = args[0]
			}
			if !cmd.Flags().Changed("builder") {
				runOpts.builder = ""
			}
			return runBuildCommand(cmd, service, runOpts)
		},
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	attachValidatedStringArray(cmd.Flags(), "build-arg", &opts.buildArgs, "Add a build-time variable (KEY=VALUE)", func(raw string) error {
		_, err := buildsvc.ParseKeyValueArgs([]string{raw})
		return err
	})

	attachValidatedStringArray(cmd.Flags(), "secret", &opts.secrets, "Expose an environment variable as a BuildKit secret (NAME)", validateEnvVarName)

	attachValidatedStringArray(cmd.Flags(), "cache-from", &opts.cacheFrom, "Cache import sources (comma-separated key=value pairs)", func(raw string) error {
		_, err := buildsvc.ParseCacheSpecs([]string{raw})
		return err
	})
	attachValidatedStringArray(cmd.Flags(), "cache-to", &opts.cacheTo, "Cache export destinations (comma-separated key=value pairs)", func(raw string) error {
		_, err := buildsvc.ParseCacheSpecs([]string{raw})
		return err
	})

	cmd.Flags().VarP(&validatedStringValue{dest: &opts.dockerfile, name: "--file", validator: func(raw string) error {
		if strings.TrimSpace(raw) == "" {
			return fmt.Errorf("dockerfile path cannot be empty")
		}
		return nil
	}}, "file", "f", "Path to the Dockerfile (default: Dockerfile)")
	cmd.Flags().VarP(&validatedCSVListValue{dest: &opts.tags, validator: validateTag, name: "--tag"}, "tag", "t", "One or more image tags to apply to the result")
	cmd.Flags().Var(&validatedCSVListValue{dest: &opts.platforms, validator: validatePlatform, name: "--platform"}, "platform", "Target platforms (comma-separated values like linux/amd64)")
	cmd.Flags().BoolVar(&opts.sbom, "sbom", false, "Generate an SBOM attestation (in-toto) during the build")
	cmd.Flags().BoolVar(&opts.provenance, "provenance", false, "Generate a SLSA provenance attestation (in-toto) during the build")
	cmd.Flags().BoolVar(&opts.hermetic, "hermetic", false, "Enable hermetic/locked build mode (no network egress unless explicitly allowed; requires pinned base-image digests; records external fetches)")
	cmd.Flags().BoolVar(&opts.hermetic, "locked", false, "Alias for --hermetic")
	cmd.Flags().BoolVar(&opts.allowNetwork, "allow-network", false, "Allow network egress when --hermetic is enabled")
	cmd.Flags().BoolVar(&opts.allowUnpinned, "allow-unpinned-bases", false, "Allow unpinned base images (FROM without @sha256 digest) when --hermetic is enabled")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.policyRef, name: "--policy", allowEmpty: true, validator: nil}, "policy", "Policy bundle path or https URL to evaluate before/after build (OPA/Rego).")
	cmd.Flags().Var(newEnumStringValue(&opts.policyMode, "enforce", "enforce", "warn"), "policy-mode", "Policy enforcement mode: enforce or warn")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.policyReportPath, name: "--policy-report", allowEmpty: true, validator: nil}, "policy-report", "Write a machine-readable policy report JSON to this path (defaults to --attest-dir/ktl-policy-report.json when --attest-dir is set)")
	cmd.Flags().Var(newEnumStringValue(&opts.secretsMode, "warn", "warn", "block", "off"), "secrets", "Secret-leak guardrails: warn (default), block, or off")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.secretsReport, name: "--secrets-report", allowEmpty: true, validator: nil}, "secrets-report", "Write a machine-readable secrets report JSON to this path (defaults to --attest-dir/ktl-secrets-report.json when --attest-dir is set)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.secretsConfig, name: "--secrets-config", allowEmpty: true, validator: nil}, "secrets-config", "Secrets rule config file or https URL (YAML/JSON). When unset, built-in defaults apply.")
	cmd.Flags().Var(newEnumStringValue(&opts.profile, "dev", "dev", "ci", "secure", "remote"), "profile", "Build profile: dev, ci, secure, or remote (expands to sensible defaults)")
	cmd.Flags().BoolVar(&opts.intentSecure, "secure", false, "Intent: secure build (implies hermetic+sandbox+attest+policy+secrets scan)")
	cmd.Flags().BoolVar(&opts.intentPublish, "publish", false, "Intent: publish build (implies --push, and enables signing when combined with --sign)")
	cmd.Flags().BoolVar(&opts.intentOCI, "oci", false, "Intent: export OCI layout and write attestations (implies --attest-dir when unset)")
	cmd.Flags().BoolVar(&opts.wizard, "wizard", false, "Guided mode: prompt for a profile/config and print the generated command")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.attestDir, name: "--attest-dir", allowEmpty: true, validator: nil}, "attest-dir", "Write generated attestations (SBOM/provenance) to this directory as JSON files (implies --sbom and --provenance; requires OCI layout export)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.capturePath, name: "--capture", allowEmpty: true, validator: nil}, "capture", "Capture build logs/events to a SQLite database at this path")
	if flag := cmd.Flags().Lookup("capture"); flag != nil {
		flag.NoOptDefVal = "__auto__"
	}
	attachValidatedStringArray(cmd.Flags(), "capture-tag", &opts.captureTags, "Tag the capture session (KEY=VALUE). Repeatable.", func(raw string) error {
		_, err := buildsvc.ParseKeyValueArgs([]string{raw})
		return err
	})
	cmd.Flags().BoolVar(&opts.push, "push", false, "Push all tags to their registries after a successful build")
	cmd.Flags().BoolVar(&opts.load, "load", false, "Load the resulting image into the local container runtime (docker build --load)")
	cmd.Flags().BoolVar(&opts.noCache, "no-cache", false, "Disable BuildKit cache usage")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.builder, name: "--builder", validator: validateBuildkitAddr}, "builder", "BuildKit address (override with KTL_BUILDKIT_HOST)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.cacheDir, name: "--cache-dir", allowEmpty: false, validator: nil}, "cache-dir", "Local cache directory for BuildKit metadata")
	cmd.Flags().BoolVar(&opts.sign, "sign", false, "Sign pushed image tags with cosign after a successful build (requires --push)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.signKey, name: "--sign-key", allowEmpty: true, validator: nil}, "sign-key", "cosign key reference for signing (e.g. awskms://..., gcpkms://..., azurekms://...)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.rekorURL, name: "--rekor-url", allowEmpty: true, validator: nil}, "rekor-url", "Override the Rekor transparency log URL used by cosign")
	cmd.Flags().Var(&optionalBoolStringValue{dest: &opts.tlogUpload}, "tlog-upload", "Override cosign transparency log upload (true/false); default uses cosign's behavior")
	cmd.Flags().BoolVarP(&opts.interactive, "interactive", "i", false, "Drop into an interactive shell when a RUN step fails")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.interactiveShell, name: "--interactive-shell", allowEmpty: false, validator: nil}, "interactive-shell", "Shell command to start when --interactive attaches")
	_ = cmd.Flags().Set("interactive-shell", "/bin/sh")
	cmd.Flags().Var(newEnumStringValue(&opts.buildMode, string(buildsvc.ModeAuto), string(buildsvc.ModeDockerfile), string(buildsvc.ModeCompose)), "mode", "Build mode: auto, dockerfile, or compose")
	attachValidatedStringArray(cmd.Flags(), "compose-file", &opts.composeFiles, "Compose file(s) to use when building compose projects", nil)
	attachValidatedStringArray(cmd.Flags(), "compose-profile", &opts.composeProfiles, "Compose profile(s) to enable", nil)
	attachValidatedStringArray(cmd.Flags(), "compose-service", &opts.composeServices, "Compose service(s) to build (default: all buildable services)", nil)
	cmd.Flags().Var(&validatedStringValue{dest: &opts.composeProject, name: "--compose-project", allowEmpty: true, validator: nil}, "compose-project", "Override the compose project name")
	cmd.Flags().Var(&nonNegativeIntValue{dest: &opts.composeParallel}, "compose-parallelism", "Max parallel compose builds (default: NumCPU)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.logFile, name: "--logfile", allowEmpty: true, validator: nil}, "logfile", "Log to file instead of stdout/stderr")
	cmd.Flags().BoolVar(&opts.rm, "rm", true, "Remove intermediate containers after a successful build")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Refrain from announcing build instructions and progress")
	cmd.Flags().BoolVar(&opts.sandboxLogs, "sandbox-logs", false, "Stream sandbox runtime logs to stderr and the websocket mirror (when --ws-listen is set)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.sandboxProbePath, name: "--sandbox-probe-path", allowEmpty: true, validator: nil}, "sandbox-probe-path", "Probe filesystem visibility before building by attempting to stat this host path")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.wsListenAddr, name: "--ws-listen", allowEmpty: true, validator: validateWSListenAddr}, "ws-listen", "Serve the raw BuildKit event stream over WebSocket at this address (e.g. :9085)")
	cmd.Flags().Var(&validatedStringValue{dest: &opts.remoteAddr, name: "--remote-build", allowEmpty: true, validator: validateRemoteAddr}, "remote-build", "Execute this build via a remote ktl-agent gRPC endpoint (host:port)")
	cmd.PersistentFlags().Var(&validatedStringValue{dest: &opts.authFile, name: "--authfile", allowEmpty: true, validator: nil}, "authfile", "Path to the authentication file (Docker config.json)")
	cmd.PersistentFlags().Var(&validatedStringValue{dest: &opts.sandboxConfig, name: "--sandbox-config", allowEmpty: true, validator: nil}, "sandbox-config", "Path to a sandbox runtime config file")
	cmd.PersistentFlags().Var(&validatedStringValue{dest: &opts.sandboxBin, name: "--sandbox-bin", allowEmpty: true, validator: nil}, "sandbox-bin", "Path to the sandbox runtime binary")
	attachValidatedStringArray(cmd.PersistentFlags(), "sandbox-bind", &opts.sandboxBinds, "Additional sandbox bind mounts (host:guest)", validateSandboxBind)
	cmd.PersistentFlags().BoolVar(&opts.sandboxBindHome, "sandbox-bind-home", false, "Bind-mount the current user's home directory into the sandbox (use with caution; prefer --authfile/--secret/--sandbox-bind)")
	cmd.PersistentFlags().Var(&validatedStringValue{dest: &opts.sandboxWorkdir, name: "--sandbox-workdir", allowEmpty: true, validator: nil}, "sandbox-workdir", "Working directory inside the sandbox (default: build context)")
	cmd.PersistentFlags().BoolVar(&opts.sandboxRequired, "sandbox", false, "Require executing the build inside the sandbox (fail if unavailable)")

	cmd.AddCommand(newBuildLoginCommand(&opts), newBuildLogoutCommand(&opts))

	decorateCommandHelp(cmd, "Build Flags")
	return cmd
}

type validatedStringValue struct {
	dest       *string
	name       string
	allowEmpty bool
	validator  func(string) error
}

func (v *validatedStringValue) String() string {
	if v == nil || v.dest == nil {
		return ""
	}
	return *v.dest
}

func (v *validatedStringValue) Set(s string) error {
	raw := strings.TrimSpace(s)
	if raw == "" {
		if v.allowEmpty {
			*v.dest = ""
			return nil
		}
		return fmt.Errorf("%s cannot be empty", v.name)
	}
	if v.validator != nil {
		if err := v.validator(raw); err != nil {
			return err
		}
	}
	*v.dest = raw
	return nil
}

func (v *validatedStringValue) Type() string { return "string" }

func attachValidatedStringArray(flags *pflag.FlagSet, name string, dest *[]string, usage string, validator func(string) error) {
	flags.Var(&validatedStringArrayValue{dest: dest, validator: validator, name: "--" + name}, name, usage)
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
	hasMirror := strings.TrimSpace(opts.wsListenAddr) != ""
	if !hasMirror {
		return nil
	}
	if opts.quiet {
		return fmt.Errorf("--ws-listen cannot be combined with --quiet")
	}
	if strings.TrimSpace(opts.logFile) != "" {
		return fmt.Errorf("--ws-listen cannot be combined with --logfile; mirrors already capture the live stream")
	}
	return nil
}

func runBuildCommand(cmd *cobra.Command, service buildsvc.Service, opts buildCLIOptions) error {
	if requestedHelp(opts.wsListenAddr) {
		return cmd.Help()
	}
	if addr := strings.TrimSpace(opts.remoteAddr); addr != "" {
		return runRemoteBuild(cmd, opts, addr)
	}

	streams := buildsvc.Streams{
		In:        cmd.InOrStdin(),
		Out:       cmd.OutOrStdout(),
		Err:       cmd.ErrOrStderr(),
		Terminals: []any{cmd.InOrStdin(), cmd.OutOrStdout(), cmd.ErrOrStderr()},
	}

	svcOpts := cliOptionsToServiceOptions(opts)
	svcOpts.Streams = streams
	var observerClosers []io.Closer
	if flag := cmd.Flags().Lookup("mirror-bus"); flag != nil {
		if addr := strings.TrimSpace(flag.Value.String()); addr != "" {
			sessionID := fmt.Sprintf("build-%d", time.Now().UnixNano())
			pub, err := mirrorbus.NewPublisher(cmd.Context(), addr, sessionID, "build")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "Mirror bus unavailable: %v\n", err)
			} else {
				svcOpts.Observers = append(svcOpts.Observers, pub)
				observerClosers = append(observerClosers, pub)
				fmt.Fprintf(cmd.ErrOrStderr(), "Publishing build mirror session %s via %s\n", sessionID, addr)
			}
		}
	}
	defer func() {
		for _, closer := range observerClosers {
			_ = closer.Close()
		}
	}()

	_, err := service.Run(cmd.Context(), svcOpts)
	return err
}

func runRemoteBuild(cmd *cobra.Command, opts buildCLIOptions, remoteAddr string) error {
	ctx := cmd.Context()
	conn, err := grpcutil.Dial(ctx, remoteAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return err
	}
	defer conn.Close()

	logger, err := logging.New("info")
	if err != nil {
		return err
	}
	errOut := cmd.ErrOrStderr()
	var observers []tailer.LogObserver
	if !opts.quiet {
		if obs := buildsvc.NewConsoleObserver(errOut); obs != nil {
			observers = append(observers, obs)
		}
	}
	mirrorAddr := ""
	if flag := cmd.Flags().Lookup("mirror-bus"); flag != nil {
		mirrorAddr = strings.TrimSpace(flag.Value.String())
	}
	mirrorLabel := fmt.Sprintf("Context: %s", opts.contextDir)
	extraObservers, cleanup, err := startBuildMirrors(ctx, opts, mirrorAddr, mirrorLabel, logger.WithName("remote-build"), errOut)
	if err != nil {
		return err
	}
	defer cleanup()
	observers = append(observers, extraObservers...)

	client := apiv1.NewBuildServiceClient(conn)
	buildOpts := cliOptionsToServiceOptions(opts)
	req := &apiv1.RunBuildRequest{
		SessionId: fmt.Sprintf("remote-%d", time.Now().UnixNano()),
		Options:   convert.BuildOptionsToProto(buildOpts),
	}
	stream, err := client.RunBuild(ctx, req)
	if err != nil {
		return err
	}
	for {
		event, err := stream.Recv()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		if log := event.GetLog(); log != nil {
			rec := convert.FromProtoLogLine(log)
			notifyBuildObservers(observers, rec)
		}
		if res := event.GetResult(); res != nil {
			if res.GetError() != "" {
				return fmt.Errorf("remote build failed: %s", res.GetError())
			}
			fmt.Fprintf(errOut, "Remote build complete. Digest: %s\n", res.GetDigest())
			if len(res.GetTags()) > 0 {
				fmt.Fprintf(errOut, "Tags: %s\n", strings.Join(res.GetTags(), ", "))
			}
			return nil
		}
	}
}

func cliOptionsToServiceOptions(opts buildCLIOptions) buildsvc.Options {
	return buildsvc.Options{
		ContextDir:         opts.contextDir,
		Dockerfile:         opts.dockerfile,
		Tags:               append([]string(nil), opts.tags...),
		Platforms:          append([]string(nil), opts.platforms...),
		BuildArgs:          append([]string(nil), opts.buildArgs...),
		Secrets:            append([]string(nil), opts.secrets...),
		CacheFrom:          append([]string(nil), opts.cacheFrom...),
		CacheTo:            append([]string(nil), opts.cacheTo...),
		Hermetic:           opts.hermetic,
		AllowNetwork:       opts.allowNetwork,
		AllowUnpinnedBases: opts.allowUnpinned,
		PolicyRef:          opts.policyRef,
		PolicyMode:         opts.policyMode,
		PolicyReportPath:   opts.policyReportPath,
		SecretsMode:        opts.secretsMode,
		SecretsReportPath:  opts.secretsReport,
		SecretsConfigRef:   opts.secretsConfig,
		AttestSBOM:         opts.sbom,
		AttestProvenance:   opts.provenance,
		AttestationDir:     opts.attestDir,
		CapturePath:        opts.capturePath,
		CaptureTags:        append([]string(nil), opts.captureTags...),
		Push:               opts.push,
		Load:               opts.load,
		NoCache:            opts.noCache,
		Builder:            opts.builder,
		CacheDir:           opts.cacheDir,
		Sign:               opts.sign,
		SignKey:            opts.signKey,
		RekorURL:           opts.rekorURL,
		TLogUpload:         opts.tlogUpload,
		Interactive:        opts.interactive,
		InteractiveShell:   opts.interactiveShell,
		BuildMode:          opts.buildMode,
		ComposeFiles:       append([]string(nil), opts.composeFiles...),
		ComposeProfiles:    append([]string(nil), opts.composeProfiles...),
		ComposeServices:    append([]string(nil), opts.composeServices...),
		ComposeProject:     opts.composeProject,
		ComposeParallelism: opts.composeParallel,
		AuthFile:           opts.authFile,
		SandboxConfig:      opts.sandboxConfig,
		SandboxBin:         opts.sandboxBin,
		SandboxBinds:       append([]string(nil), opts.sandboxBinds...),
		SandboxBindHome:    opts.sandboxBindHome,
		SandboxWorkdir:     opts.sandboxWorkdir,
		SandboxLogs:        opts.sandboxLogs,
		SandboxProbePath:   opts.sandboxProbePath,
		RequireSandbox:     opts.sandboxRequired,
		LogFile:            opts.logFile,
		RemoveIntermediate: opts.rm,
		Quiet:              opts.quiet,
		WSListenAddr:       opts.wsListenAddr,
	}
}

func startBuildMirrors(ctx context.Context, opts buildCLIOptions, mirrorAddr, label string, logger logr.Logger, errOut io.Writer) ([]tailer.LogObserver, func(), error) {
	var observers []tailer.LogObserver
	var closers []func()
	if addr := strings.TrimSpace(opts.wsListenAddr); addr != "" {
		wsServer := caststream.New(addr, caststream.ModeWS, label, logger.WithName("build-ws"))
		if err := castutil.StartCastServer(ctx, wsServer, "ktl build websocket stream", logger.WithName("build-ws"), errOut); err != nil {
			return nil, func() {}, err
		}
		observers = append(observers, wsServer)
		fmt.Fprintf(errOut, "Serving ktl websocket build stream on %s\n", addr)
	}
	if strings.TrimSpace(mirrorAddr) != "" {
		sessionID := fmt.Sprintf("build-%d", time.Now().UnixNano())
		pub, err := mirrorbus.NewPublisher(ctx, mirrorAddr, sessionID, "build")
		if err != nil {
			fmt.Fprintf(errOut, "Mirror bus unavailable: %v\n", err)
		} else {
			observers = append(observers, pub)
			closers = append(closers, func() { _ = pub.Close() })
			fmt.Fprintf(errOut, "Publishing build mirror session %s via %s\n", sessionID, mirrorAddr)
		}
	}
	cleanup := func() {
		for _, closer := range closers {
			closer()
		}
	}
	return observers, cleanup, nil
}

func notifyBuildObservers(observers []tailer.LogObserver, rec tailer.LogRecord) {
	for _, observer := range observers {
		if observer == nil {
			continue
		}
		observer.ObserveLog(rec)
	}
}
