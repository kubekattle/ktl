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
			return runBuildCommand(cmd, service, runOpts)
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
	cmd.Flags().StringVar(&opts.buildMode, "mode", string(buildsvc.ModeAuto), "Build mode: auto, dockerfile, or compose")
	cmd.Flags().StringArrayVar(&opts.composeFiles, "compose-file", nil, "Compose file(s) to use when building compose projects")
	cmd.Flags().StringArrayVar(&opts.composeProfiles, "compose-profile", nil, "Compose profile(s) to enable")
	cmd.Flags().StringArrayVar(&opts.composeServices, "compose-service", nil, "Compose service(s) to build (default: all buildable services)")
	cmd.Flags().StringVar(&opts.composeProject, "compose-project", "", "Override the compose project name")
	cmd.Flags().StringVar(&opts.logFile, "logfile", "", "Log to file instead of stdout/stderr")
	cmd.Flags().BoolVar(&opts.rm, "rm", true, "Remove intermediate containers after a successful build")
	cmd.Flags().BoolVarP(&opts.quiet, "quiet", "q", false, "Refrain from announcing build instructions and progress")
	cmd.Flags().BoolVar(&opts.sandboxLogs, "sandbox-logs", false, "Stream sandbox runtime logs to stderr and the build viewer")
	cmd.Flags().StringVar(&opts.uiAddr, "ui", "", "Serve the live BuildKit viewer at this address (e.g. :8080)")
	if flag := cmd.Flags().Lookup("ui"); flag != nil {
		flag.NoOptDefVal = ":8080"
	}
	cmd.Flags().StringVar(&opts.wsListenAddr, "ws-listen", "", "Serve the raw BuildKit event stream over WebSocket at this address (e.g. :9085)")
	cmd.Flags().StringVar(&opts.remoteAddr, "remote-build", "", "Execute this build via a remote ktl-agent gRPC endpoint (host:port)")
	cmd.PersistentFlags().StringVar(&opts.authFile, "authfile", "", "Path to the authentication file (Docker config.json)")
	cmd.PersistentFlags().StringVar(&opts.sandboxConfig, "sandbox-config", "", "Path to a sandbox runtime config file")
	cmd.PersistentFlags().StringVar(&opts.sandboxBin, "sandbox-bin", "", "Path to the sandbox runtime binary")
	cmd.PersistentFlags().StringArrayVar(&opts.sandboxBinds, "sandbox-bind", nil, "Additional sandbox bind mounts (host:guest)")
	cmd.PersistentFlags().StringVar(&opts.sandboxWorkdir, "sandbox-workdir", "", "Working directory inside the sandbox (default: build context)")

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

func runBuildCommand(cmd *cobra.Command, service buildsvc.Service, opts buildCLIOptions) error {
	if err := validateBuildMirrorFlags(opts); err != nil {
		return err
	}
	if requestedHelp(opts.uiAddr) || requestedHelp(opts.wsListenAddr) {
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
				fmt.Fprintf(cmd.ErrOrStderr(), "Share via: ktl mirror proxy --bus %s --session %s --mode logs\n", addr, sessionID)
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
		Push:               opts.push,
		Load:               opts.load,
		NoCache:            opts.noCache,
		Builder:            opts.builder,
		CacheDir:           opts.cacheDir,
		Interactive:        opts.interactive,
		InteractiveShell:   opts.interactiveShell,
		BuildMode:          opts.buildMode,
		ComposeFiles:       append([]string(nil), opts.composeFiles...),
		ComposeProfiles:    append([]string(nil), opts.composeProfiles...),
		ComposeServices:    append([]string(nil), opts.composeServices...),
		ComposeProject:     opts.composeProject,
		AuthFile:           opts.authFile,
		SandboxConfig:      opts.sandboxConfig,
		SandboxBin:         opts.sandboxBin,
		SandboxBinds:       append([]string(nil), opts.sandboxBinds...),
		SandboxWorkdir:     opts.sandboxWorkdir,
		SandboxLogs:        opts.sandboxLogs,
		LogFile:            opts.logFile,
		RemoveIntermediate: opts.rm,
		Quiet:              opts.quiet,
		UIAddr:             opts.uiAddr,
		WSListenAddr:       opts.wsListenAddr,
	}
}

func startBuildMirrors(ctx context.Context, opts buildCLIOptions, mirrorAddr, label string, logger logr.Logger, errOut io.Writer) ([]tailer.LogObserver, func(), error) {
	var observers []tailer.LogObserver
	var closers []func()
	if addr := strings.TrimSpace(opts.uiAddr); addr != "" {
		uiServer := caststream.New(addr, caststream.ModeWeb, label, logger.WithName("build-ui"), caststream.WithoutFilters(), caststream.WithoutLogTitle())
		if err := castutil.StartCastServer(ctx, uiServer, "ktl build UI", logger.WithName("build-ui"), errOut); err != nil {
			return nil, func() {}, err
		}
		observers = append(observers, uiServer)
		fmt.Fprintf(errOut, "Serving ktl build UI on %s\n", addr)
	}
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
			fmt.Fprintf(errOut, "Share via: ktl mirror proxy --bus %s --session %s --mode logs\n", mirrorAddr, sessionID)
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
