// File: internal/workflows/buildsvc/run.go
// Brief: Internal buildsvc package implementation for 'run'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/caststream"
	"github.com/example/ktl/internal/castutil"
	"github.com/example/ktl/internal/dockerconfig"
	"github.com/example/ktl/internal/logging"
	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/pkg/buildkit"
	appcompose "github.com/example/ktl/pkg/compose"
	"github.com/example/ktl/pkg/registry"
)

// Dependencies configures a build Service.
type Dependencies struct {
	BuildRunner   buildkit.Runner
	Registry      registry.Client
	ComposeRunner appcompose.Runner
}

// Result summarizes the outcome of a build.
type Result struct {
	Tags         []string
	Digest       string
	OCIOutputDir string
}

func parseOptionalBool(v string) (*bool, error) {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil, nil
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return nil, fmt.Errorf("parse bool %q: %w", v, err)
	}
	return &parsed, nil
}

func parseCaptureTags(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid capture tag %q (want KEY=VALUE)", v)
		}
		k := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if k == "" || val == "" {
			return nil, fmt.Errorf("invalid capture tag %q (empty key/value)", v)
		}
		out[k] = val
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func mustMarshalJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}

type service struct {
	buildRunner   buildkit.Runner
	registry      registry.Client
	composeRunner appcompose.Runner
}

// New returns a default build Service.
func New(deps Dependencies) Service {
	br := deps.BuildRunner
	if br == nil {
		br = buildkit.NewRunner()
	}
	reg := deps.Registry
	if reg == nil {
		reg = registry.NewClient()
	}
	cr := deps.ComposeRunner
	if cr == nil {
		cr = appcompose.NewRunner(br, reg)
	}
	return &service{
		buildRunner:   br,
		registry:      reg,
		composeRunner: cr,
	}
}

// Run executes the build workflow with the provided options.
func (s *service) Run(ctx context.Context, opts Options) (*Result, error) {
	if err := opts.Streams.validate(); err != nil {
		return nil, err
	}
	start := time.Now()
	streams := opts.Streams
	errOut := streams.ErrWriter()

	if opts.Hermetic {
		if !opts.AllowNetwork {
			opts.RequireSandbox = true
		}
		if strings.TrimSpace(opts.AttestationDir) == "" {
			return nil, errors.New("--hermetic requires --attest-dir so ktl can persist provenance (including external fetches)")
		}
		opts.AttestProvenance = true
		opts.AttestSBOM = true
	}

	var logCloser io.Closer
	if logPath := strings.TrimSpace(opts.LogFile); logPath != "" {
		dir := filepath.Dir(logPath)
		if dir != "" && dir != "." {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return nil, fmt.Errorf("create logfile directory: %w", err)
			}
		}
		f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, fmt.Errorf("open logfile: %w", err)
		}
		logCloser = f
		streams.SetOutErr(f, f)
	}
	defer func() {
		if logCloser != nil {
			_ = logCloser.Close()
		}
	}()

	if sandboxActive() {
		policy := strings.TrimSpace(opts.SandboxConfig)
		if policy == "" {
			policy = "embedded default"
		}
		runtime := strings.TrimSpace(opts.SandboxBin)
		if runtime == "" {
			runtime = "system default"
		}
		fmt.Fprintf(errOut, "Running ktl build inside the sandbox (policy: %s, binary: %s). Set KTL_SANDBOX_DISABLE=1 to opt out.\n", policy, runtime)
	}

	contextDir := opts.ContextDir
	if contextDir == "" {
		contextDir = "."
	}
	if sandboxActive() {
		if envContext := strings.TrimSpace(sandboxContextFromEnv()); envContext != "" {
			contextDir = envContext
			opts.ContextDir = envContext
		}
		if envCache := strings.TrimSpace(sandboxCacheFromEnv()); envCache != "" {
			opts.CacheDir = envCache
		}
	}
	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = buildkit.DefaultCacheDir()
	}

	if err := dockerconfig.ApplyAuthfileEnv(opts.AuthFile); err != nil {
		return nil, err
	}

	contextAbs, err := filepath.Abs(contextDir)
	if err != nil {
		return nil, err
	}

	secretGuard := newSecretsGuard(opts.SecretsMode, opts.SecretsReportPath, opts.AttestationDir)
	if _, err := secretGuard.preflightBuildArgs(errOut, opts.BuildArgs); err != nil {
		return nil, err
	}

	gate, err := newPolicyGate(ctx, opts.PolicyRef, opts.PolicyMode, opts.PolicyReportPath, opts.AttestationDir)
	if err != nil {
		return nil, err
	}

	if strings.TrimSpace(opts.AttestationDir) != "" && !opts.AttestProvenance && !opts.AttestSBOM {
		opts.AttestProvenance = true
		opts.AttestSBOM = true
	}

	if injector := getSandboxInjector(); injector != nil {
		if handled, err := injector(ctx, &opts, streams, contextAbs); err != nil {
			return nil, err
		} else if handled {
			return &Result{}, nil
		}
	} else if opts.RequireSandbox && !sandboxActive() {
		return nil, fmt.Errorf("sandbox is required but unavailable on this host (set KTL_SANDBOX_DISABLE=1 to opt out, or omit --hermetic/--sandbox)")
	} else if opts.SandboxLogs {
		return nil, fmt.Errorf("--sandbox-logs is only supported on Linux hosts with a sandbox runtime installed")
	}

	if probe := strings.TrimSpace(opts.SandboxProbePath); probe != "" {
		if _, err := os.Stat(probe); err == nil {
			fmt.Fprintf(errOut, "[probe] stat %q: OK\n", probe)
		} else {
			fmt.Fprintf(errOut, "[probe] stat %q: %v\n", probe, err)
		}
	}

	stream := newBuildProgressBroadcaster(filepath.Base(contextAbs))
	progressObservers := []buildkit.ProgressObserver{stream}
	diagnosticObservers := []buildkit.BuildDiagnosticObserver{&buildDiagnosticObserver{
		stream: stream,
		writer: errOut,
	}}

	var fetches *externalFetchCollector
	if opts.Hermetic {
		fetches = newExternalFetchCollector(start)
		progressObservers = append(progressObservers, fetches)
	}

	var captureRecorder *capture.Recorder
	if path := strings.TrimSpace(opts.CapturePath); path != "" {
		resolved, err := capture.ResolvePath("ktl build", path, time.Now())
		if err != nil {
			return nil, err
		}
		host, _ := os.Hostname()
		tagMap, err := parseCaptureTags(opts.CaptureTags)
		if err != nil {
			return nil, err
		}
		rec, err := capture.Open(resolved, capture.SessionMeta{
			Command:   "ktl build",
			Args:      append([]string(nil), os.Args[1:]...),
			StartedAt: time.Now().UTC(),
			Host:      host,
			Tags:      tagMap,
			Entities: capture.Entities{
				BuildContext: contextDir,
			},
		})
		if err != nil {
			return nil, err
		}
		captureRecorder = rec
		opts.Observers = append(opts.Observers, rec)
		fmt.Fprintf(errOut, "Capturing build session to %s (session %s)\n", resolved, rec.SessionID())
	}
	defer func() {
		if captureRecorder != nil {
			_ = captureRecorder.Close()
		}
	}()

	var consoleObserver tailer.LogObserver
	if !opts.Quiet && streams.IsTerminal(errOut) {
		consoleObserver = NewConsoleObserver(errOut)
		if consoleObserver != nil {
			stream.addObserver(consoleObserver)
		}
	}
	for _, obs := range opts.Observers {
		stream.addObserver(obs)
	}
	quietProgress := opts.Quiet || (consoleObserver != nil && os.Getenv("BUILDKIT_PROGRESS") == "")

	var stopSandboxLogs func()
	if opts.SandboxLogs && sandboxActive() {
		logPath := sandboxLogPathFromEnv()
		if logPath == "" {
			fmt.Fprintln(errOut, "sandbox logs requested but log path unavailable")
		} else {
			var observer func(string)
			if stream != nil {
				observer = stream.emitSandboxLog
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

	mirrorLabel := fmt.Sprintf("Context: %s", contextAbs)
	if addr := strings.TrimSpace(opts.WSListenAddr); addr != "" {
		logger, logErr := logging.New("info")
		if logErr != nil {
			return nil, logErr
		}
		wsServer := caststream.New(addr, caststream.ModeWS, mirrorLabel, logger.WithName("build-ws"))
		stream.addObserver(wsServer)
		if err := castutil.StartCastServer(ctx, wsServer, "ktl build websocket stream", logger.WithName("build-ws"), errOut); err != nil {
			return nil, err
		}
		fmt.Fprintf(errOut, "Serving ktl websocket build stream on %s\n", addr)
	}
	stream.emitInfo(fmt.Sprintf("Streaming ktl build from %s", contextDir))
	defer func() {
		if stream != nil {
			stream.emitSummary(buildSummary{
				Tags:      append([]string(nil), opts.Tags...),
				Platforms: append([]string(nil), opts.Platforms...),
				Mode:      opts.BuildMode,
				Push:      opts.Push,
				Load:      opts.Load,
			})
			stream.emitResult(nil, time.Since(start))
		}
		if captureRecorder != nil {
			if opts.ContextDir != "" {
				_ = captureRecorder.RecordArtifact(context.Background(), "build_context", opts.ContextDir)
			}
			if fetches != nil {
				_ = captureRecorder.RecordArtifact(context.Background(), "build.external_fetches_json", fetches.snapshotJSON())
			}
		}
	}()

	mode, composeFiles, err := selectBuildMode(contextAbs, opts)
	if err != nil {
		return nil, err
	}

	if mode == modeCompose {
		if gate != nil {
			pre := buildPolicyInput(time.Now(), contextDir, "", append([]string(nil), opts.Tags...), dockerfileMeta{}, opts.AttestationDir)
			if rep, err := gate.eval(ctx, pre); err != nil {
				return nil, err
			} else if err := gate.enforceOrWarn(errOut, rep, "pre", 10); err != nil {
				return nil, err
			}
		}
		if err := s.runComposeBuild(ctx, composeFiles, opts, progressObservers, diagnosticObservers, quietProgress, stream, streams); err != nil {
			return nil, err
		}
		if fetches != nil {
			if dir := strings.TrimSpace(opts.AttestationDir); dir != "" {
				if err := os.MkdirAll(dir, 0o755); err == nil {
					_ = os.WriteFile(filepath.Join(dir, "ktl-external-fetches.json"), []byte(fetches.snapshotJSON()), 0o644)
				}
			}
		}
		if gate != nil {
			post := buildPolicyInput(time.Now(), contextDir, "", append([]string(nil), opts.Tags...), dockerfileMeta{}, opts.AttestationDir)
			if rep, err := gate.eval(ctx, post); err != nil {
				return nil, err
			} else if err := gate.enforceOrWarn(errOut, rep, "post", 10); err != nil {
				return nil, err
			}
		}
		return &Result{}, nil
	}

	if opts.Hermetic {
		dockerfilePath := opts.Dockerfile
		if dockerfilePath == "" {
			dockerfilePath = "Dockerfile"
		}
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(contextAbs, dockerfilePath)
		}
		if err := validatePinnedBaseImagesWithOptions(dockerfilePath, opts.AllowUnpinnedBases); err != nil {
			return nil, err
		}
	}

	var dfMeta dockerfileMeta
	if gate != nil {
		dockerfilePath := opts.Dockerfile
		if dockerfilePath == "" {
			dockerfilePath = "Dockerfile"
		}
		if !filepath.IsAbs(dockerfilePath) {
			dockerfilePath = filepath.Join(contextAbs, dockerfilePath)
		}
		meta, derr := readDockerfileMeta(dockerfilePath)
		if derr != nil {
			return nil, fmt.Errorf("policy gate: read dockerfile metadata: %w", derr)
		}
		dfMeta = meta
		pre := buildPolicyInput(time.Now(), contextDir, "", append([]string(nil), opts.Tags...), dfMeta, opts.AttestationDir)
		if rep, err := gate.eval(ctx, pre); err != nil {
			return nil, err
		} else if err := gate.enforceOrWarn(errOut, rep, "pre", 10); err != nil {
			return nil, err
		}
	}

	buildArgs, err := parseKeyValueArgs(opts.BuildArgs)
	if err != nil {
		return nil, err
	}

	cacheFrom, err := parseCacheSpecs(opts.CacheFrom)
	if err != nil {
		return nil, err
	}
	cacheTo, err := parseCacheSpecs(opts.CacheTo)
	if err != nil {
		return nil, err
	}
	if opts.NoCache && len(cacheFrom) > 0 {
		fmt.Fprintln(errOut, "warning: ignoring --cache-from entries because --no-cache is set")
		cacheFrom = nil
	}

	platforms := buildkit.NormalizePlatforms(expandPlatforms(opts.Platforms))

	tags := opts.Tags
	if opts.Push && len(tags) == 0 {
		return nil, errors.New("--tag must be provided when using --push")
	}
	if len(tags) == 0 {
		tags = []string{buildkit.DefaultLocalTag(contextDir)}
		fmt.Fprintf(errOut, "Defaulting to local tag %s\n", tags[0])
	}
	if stream != nil {
		stream.emitInfo(fmt.Sprintf("Target tags: %s", strings.Join(tags, ", ")))
	}

	secrets := make([]buildkit.Secret, 0, len(opts.Secrets))
	for _, id := range opts.Secrets {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := os.LookupEnv(id); !ok {
			return nil, fmt.Errorf("secret %s is not present in the environment", id)
		}
		secrets = append(secrets, buildkit.Secret{ID: id, Env: id})
	}

	dockerCfg, err := dockerconfig.LoadConfigFile(opts.AuthFile, errOut)
	if err != nil {
		return nil, err
	}
	progressOut := resolveConsoleFile(errOut)

	buildOpts := buildkit.DockerfileBuildOptions{
		BuilderAddr:          opts.Builder,
		AllowBuilderFallback: opts.Builder == "",
		ContextDir:           contextDir,
		DockerfilePath:       opts.Dockerfile,
		Platforms:            platforms,
		BuildArgs:            buildArgs,
		Secrets:              secrets,
		Tags:                 tags,
		Push:                 opts.Push,
		LoadToContainerd:     opts.Load,
		CacheDir:             cacheDir,
		CacheExports:         cacheTo,
		CacheImports:         cacheFrom,
		NoCache:              opts.NoCache,
		AttestProvenance:     opts.AttestProvenance,
		AttestSBOM:           opts.AttestSBOM,
		ProgressOutput:       progressOut,
		DockerConfig:         dockerCfg,
		ProgressObservers:    progressObservers,
		DiagnosticObservers:  diagnosticObservers,
	}
	if quietProgress {
		buildOpts.ProgressMode = "quiet"
	}

	if opts.Interactive {
		tty := detectTTY(streams)
		if tty == nil {
			return nil, errors.New("--interactive requires a TTY. Run ktl build from an interactive terminal or omit --interactive")
		}
		shellArgs, err := parseInteractiveShell(opts.InteractiveShell)
		if err != nil {
			return nil, err
		}
		buildOpts.Interactive = &buildkit.InteractiveShellConfig{
			Shell:  shellArgs,
			Stdin:  streams.InReader(),
			Stdout: streams.OutWriter(),
			Stderr: errOut,
			TTY:    tty,
		}
	}

	result, err := s.buildRunner.BuildDockerfile(ctx, buildOpts)
	if err != nil {
		return nil, err
	}
	if captureRecorder != nil && result != nil {
		_ = captureRecorder.RecordArtifact(ctx, "build.digest", strings.TrimSpace(result.Digest))
		_ = captureRecorder.RecordArtifact(ctx, "build.oci_output_dir", strings.TrimSpace(result.OCIOutputPath))
		_ = captureRecorder.RecordArtifact(ctx, "build.tags_json", mustMarshalJSON(tags))
		_ = captureRecorder.RecordArtifact(ctx, "build.platforms_json", mustMarshalJSON(platforms))
		_ = captureRecorder.RecordArtifact(ctx, "build.exporter_response_json", mustMarshalJSON(result.ExporterResponse))
	}

	if result.OCIOutputPath != "" {
		if recErr := s.registry.RecordBuild(tags, result.OCIOutputPath); recErr != nil {
			fmt.Fprintf(errOut, "warning: unable to record build metadata: %v\n", recErr)
		}
	}

	if strings.TrimSpace(opts.AttestationDir) != "" {
		if result.OCIOutputPath == "" {
			return nil, errors.New("--attest-dir requires an OCI layout export but no OCI output path is available")
		}
		wrote, attErr := buildkit.WriteAttestationsFromOCI(result.OCIOutputPath, opts.AttestationDir)
		if attErr != nil {
			return nil, attErr
		}
		if captureRecorder != nil {
			_ = captureRecorder.RecordArtifact(ctx, "build.attestations_json", mustMarshalJSON(wrote))
		}
		if len(wrote) > 0 {
			fmt.Fprintf(streams.OutWriter(), "Wrote %d attestation file(s) to %s\n", len(wrote), opts.AttestationDir)
			if stream != nil {
				stream.emitInfo(fmt.Sprintf("Wrote %d attestation file(s) to %s", len(wrote), opts.AttestationDir))
			}
		} else if stream != nil {
			stream.emitInfo("No attestations found in OCI layout")
		}
		if fetches != nil {
			if err := os.MkdirAll(opts.AttestationDir, 0o755); err == nil {
				_ = os.WriteFile(filepath.Join(opts.AttestationDir, "ktl-external-fetches.json"), []byte(fetches.snapshotJSON()), 0o644)
			}
		}
	}

	if gate != nil && result != nil {
		post := buildPolicyInput(time.Now(), contextDir, strings.TrimSpace(result.Digest), append([]string(nil), tags...), dfMeta, opts.AttestationDir)
		if rep, err := gate.eval(ctx, post); err != nil {
			return nil, err
		} else if err := gate.enforceOrWarn(errOut, rep, "post", 10); err != nil {
			return nil, err
		}
	}

	if opts.Sign {
		if !opts.Push {
			return nil, errors.New("--sign requires --push")
		}
		tlogUpload, err := parseOptionalBool(opts.TLogUpload)
		if err != nil {
			return nil, fmt.Errorf("--tlog-upload: %w", err)
		}
		signOpts := registry.CosignSignOptions{
			KeyRef:      strings.TrimSpace(opts.SignKey),
			RekorURL:    strings.TrimSpace(opts.RekorURL),
			TLogUpload:  tlogUpload,
			Output:      streams.OutWriter(),
			ErrorOutput: errOut,
		}
		for _, tag := range tags {
			if tag == "" {
				continue
			}
			ref := tag
			if result != nil && strings.TrimSpace(result.Digest) != "" && !strings.Contains(tag, "@") {
				ref = fmt.Sprintf("%s@%s", tag, strings.TrimSpace(result.Digest))
			}
			if stream != nil {
				stream.emitInfo(fmt.Sprintf("Signing %s", ref))
			}
			if err := registry.CosignSign(ctx, ref, signOpts); err != nil {
				return nil, err
			}
		}
	}

	fmt.Fprintf(streams.OutWriter(), "Built %s", strings.Join(tags, ", "))
	if result.Digest != "" {
		fmt.Fprintf(streams.OutWriter(), " (digest %s)", result.Digest)
	}
	fmt.Fprintln(streams.OutWriter())
	if result.OCIOutputPath != "" {
		rel, relErr := filepath.Rel(contextDir, result.OCIOutputPath)
		if relErr != nil {
			rel = result.OCIOutputPath
		}
		fmt.Fprintf(streams.OutWriter(), "OCI layout saved at %s\n", rel)
		if stream != nil {
			stream.emitInfo(fmt.Sprintf("OCI layout saved at %s", rel))
		}
	}
	if stream != nil && result != nil && result.Digest != "" {
		stream.emitInfo(fmt.Sprintf("Image digest: %s", result.Digest))
	}

	return &Result{
		Tags:         append([]string(nil), tags...),
		Digest:       result.Digest,
		OCIOutputDir: result.OCIOutputPath,
	}, nil
}
