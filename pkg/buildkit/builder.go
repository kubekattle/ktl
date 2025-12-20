package buildkit

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/moby/buildkit/client"
	"github.com/moby/buildkit/exporter/containerimage/exptypes"
	"github.com/moby/buildkit/session"
	"github.com/moby/buildkit/session/auth/authprovider"
	"github.com/moby/buildkit/session/filesync"
	"github.com/moby/buildkit/session/secrets/secretsprovider"
	"github.com/moby/buildkit/util/progress/progresswriter"
	"github.com/opencontainers/go-digest"
)

// BuildDockerfile executes a BuildKit solve using the dockerfile frontend.
func BuildDockerfile(ctx context.Context, opts DockerfileBuildOptions) (*BuildResult, error) {
	if opts.ContextDir == "" {
		opts.ContextDir = "."
	}

	absContext, err := filepath.Abs(opts.ContextDir)
	if err != nil {
		return nil, fmt.Errorf("resolve context: %w", err)
	}
	if err := ensureDirExists(absContext); err != nil {
		return nil, fmt.Errorf("context %s: %w", absContext, err)
	}

	dockerfilePath := opts.DockerfilePath
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(absContext, "Dockerfile")
	}
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(absContext, dockerfilePath)
	}
	dockerfileDir, dockerfileName, err := splitDockerfile(dockerfilePath)
	if err != nil {
		return nil, err
	}

	if len(opts.Platforms) > 0 {
		opts.Platforms = NormalizePlatforms(opts.Platforms)
	}

	if opts.BuildArgs == nil {
		opts.BuildArgs = map[string]string{}
	}

	if opts.ProgressOutput == nil {
		opts.ProgressOutput = os.Stderr
	}
	if opts.ProgressMode == "" {
		opts.ProgressMode = "auto"
	}

	dockerCfg := opts.DockerConfig
	if dockerCfg == nil {
		dockerCfg = config.LoadDefaultConfigFile(os.Stderr)
	}

	cacheDir := opts.CacheDir
	if cacheDir == "" {
		cacheDir = DefaultCacheDir()
	}
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}

	clientCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	builderAddr := opts.BuilderAddr
	if builderAddr == "" {
		builderAddr = DefaultBuilderAddress()
	}
	cf := buildkitClientFactory{
		allowFallback: opts.AllowBuilderFallback,
		logWriter:     opts.ProgressOutput,
	}
	c, _, err := cf.new(clientCtx, builderAddr)
	if err != nil {
		return nil, err
	}
	defer c.Close()

	if len(opts.Platforms) == 0 {
		if platforms, derr := detectBuilderPlatforms(clientCtx, c); derr == nil && len(platforms) > 0 {
			if selected := selectDefaultBuilderPlatform(platforms, runtime.GOOS, runtime.GOARCH); selected != "" {
				opts.Platforms = []string{selected}
			}
		}
		if len(opts.Platforms) == 0 {
			opts.Platforms = []string{defaultPlatform(runtime.GOOS, runtime.GOARCH)}
		}
	}

	frontendAttrs := map[string]string{
		"filename": dockerfileName,
	}
	if opts.AttestProvenance {
		frontendAttrs["attest:provenance"] = ""
	}
	if opts.AttestSBOM {
		frontendAttrs["attest:sbom"] = ""
	}
	if len(opts.Platforms) > 0 {
		frontendAttrs["platform"] = strings.Join(opts.Platforms, ",")
	}
	if opts.Target != "" {
		frontendAttrs["target"] = opts.Target
	}
	if opts.Pull {
		frontendAttrs["pull"] = "true"
	}
	if opts.NoCache {
		frontendAttrs["no-cache"] = ""
	}
	keys := make([]string, 0, len(opts.BuildArgs))
	for k := range opts.BuildArgs {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		frontendAttrs["build-arg:"+k] = opts.BuildArgs[k]
	}

	localDirs := map[string]string{
		"context":    absContext,
		"dockerfile": dockerfileDir,
	}

	attachable, err := buildSessionAttachables(dockerCfg, opts)
	if err != nil {
		return nil, err
	}

	solveOpt := client.SolveOpt{
		Frontend:      "dockerfile.v0",
		FrontendAttrs: frontendAttrs,
		LocalDirs:     localDirs,
		Session:       attachable,
	}

	cacheExports := convertCacheSpecs(opts.CacheExports)
	cacheImports := convertCacheSpecs(opts.CacheImports)
	solveOpt.CacheExports = append(solveOpt.CacheExports, cacheExports...)
	if opts.NoCache {
		if len(cacheImports) > 0 && opts.ProgressOutput != nil {
			fmt.Fprintln(opts.ProgressOutput, "warning: ignoring cache imports because --no-cache is set")
		}
	} else {
		solveOpt.CacheImports = append(solveOpt.CacheImports, cacheImports...)
	}
	if !opts.NoCache {
		solveOpt.CacheExports = append(solveOpt.CacheExports, client.CacheOptionsEntry{
			Type: "local",
			Attrs: map[string]string{
				"dest": cacheDir,
				"mode": "max",
			},
		})
	}
	if !opts.NoCache {
		solveOpt.CacheImports = append([]client.CacheOptionsEntry{{
			Type: "local",
			Attrs: map[string]string{
				"src": cacheDir,
			},
		}}, solveOpt.CacheImports...)
	}

	exports, ociPath, err := buildExportEntries(opts, absContext)
	if err != nil {
		return nil, err
	}
	solveOpt.Exports = exports

	pw, err := progresswriter.NewPrinter(context.TODO(), opts.ProgressOutput, opts.ProgressMode)
	if err != nil {
		return nil, fmt.Errorf("create progress UI: %w", err)
	}

	if len(opts.ProgressObservers) > 0 {
		ch := make(chan *client.SolveStatus)
		pw = progresswriter.Tee(pw, ch)
		go fanOutSolveStatus(ch, opts.ProgressObservers)
	}
	if len(opts.DiagnosticObservers) > 0 {
		ch := make(chan *client.SolveStatus)
		pw = progresswriter.Tee(pw, ch)
		go emitDiagnostics(ch, opts.DiagnosticObservers)
	}

	statusCh := pw.Status()
	var resp *client.SolveResponse
	if opts.Interactive != nil {
		resp, err = runInteractiveDockerfile(clientCtx, c, solveOpt, opts, statusCh)
	} else {
		resp, err = c.Solve(clientCtx, nil, solveOpt, statusCh)
	}
	<-pw.Done()
	if perr := pw.Err(); perr != nil {
		err = errors.Join(err, perr)
	}
	if err != nil {
		return nil, err
	}

	digest := resp.ExporterResponse["containerimage.digest"]
	if digest == "" {
		digest = resp.ExporterResponse["oci.digest"]
	}

	return &BuildResult{
		Digest:           digest,
		ExporterResponse: resp.ExporterResponse,
		OCIOutputPath:    ociPath,
	}, nil
}

func buildSessionAttachables(cfg *configfile.ConfigFile, opts DockerfileBuildOptions) ([]session.Attachable, error) {
	attachable := []session.Attachable{}
	attachable = append(attachable, authprovider.NewDockerAuthProvider(authprovider.DockerAuthProviderConfig{
		ConfigFile: cfg,
	}))
	if len(opts.Secrets) > 0 {
		sources := make([]secretsprovider.Source, 0, len(opts.Secrets))
		for _, sec := range opts.Secrets {
			if sec.ID == "" {
				continue
			}
			source := secretsprovider.Source{ID: sec.ID}
			if sec.File != "" {
				source.FilePath = sec.File
			} else {
				source.Env = sec.Env
				if source.Env == "" {
					source.Env = sec.ID
				}
			}
			sources = append(sources, source)
		}
		if len(sources) > 0 {
			store, err := secretsprovider.NewStore(sources)
			if err != nil {
				return nil, err
			}
			attachable = append(attachable, secretsprovider.NewSecretProvider(store))
		}
	}
	return attachable, nil
}

func ensureDirExists(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", path)
	}
	return nil
}

func splitDockerfile(path string) (string, string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", "", fmt.Errorf("stat dockerfile: %w", err)
	}
	if info.IsDir() {
		return "", "", fmt.Errorf("dockerfile path %s is a directory", path)
	}
	return filepath.Dir(path), filepath.Base(path), nil
}

// NormalizePlatforms trims whitespace and removes duplicates while preserving order.
func NormalizePlatforms(platforms []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(platforms))
	for _, p := range platforms {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if _, ok := seen[p]; ok {
			continue
		}
		seen[p] = struct{}{}
		out = append(out, p)
	}
	return out
}

func defaultPlatform(goos, goarch string) string {
	osPart := strings.TrimSpace(strings.ToLower(goos))
	if osPart == "" {
		osPart = strings.TrimSpace(strings.ToLower(runtime.GOOS))
	}
	archPart := strings.TrimSpace(strings.ToLower(goarch))
	if archPart == "" {
		archPart = runtime.GOARCH
	}
	return fmt.Sprintf("%s/%s", osPart, archPart)
}

type workerLister interface {
	ListWorkers(ctx context.Context, opts ...client.ListWorkersOption) ([]*client.WorkerInfo, error)
}

func selectDefaultBuilderPlatform(platforms []string, goos, goarch string) string {
	if len(platforms) == 0 {
		return ""
	}
	runtimePlatform := defaultPlatform(goos, goarch)
	for _, p := range platforms {
		if p == runtimePlatform {
			return p
		}
	}
	return platforms[0]
}

func detectBuilderPlatforms(ctx context.Context, l workerLister) ([]string, error) {
	workers, err := l.ListWorkers(ctx)
	if err != nil {
		return nil, err
	}
	return collectWorkerPlatforms(workers), nil
}

func collectWorkerPlatforms(workers []*client.WorkerInfo) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0)
	for _, w := range workers {
		if w == nil {
			continue
		}
		for _, platform := range w.Platforms {
			osPart := strings.TrimSpace(strings.ToLower(platform.OS))
			archPart := strings.TrimSpace(strings.ToLower(platform.Architecture))
			if osPart == "" || archPart == "" {
				continue
			}
			key := fmt.Sprintf("%s/%s", osPart, archPart)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}

func convertCacheSpecs(specs []CacheSpec) []client.CacheOptionsEntry {
	entries := make([]client.CacheOptionsEntry, 0, len(specs))
	for _, spec := range specs {
		if spec.Type == "" {
			continue
		}
		attrs := map[string]string{}
		for k, v := range spec.Attrs {
			attrs[k] = v
		}
		entries = append(entries, client.CacheOptionsEntry{Type: spec.Type, Attrs: attrs})
	}
	return entries
}

func buildExportEntries(opts DockerfileBuildOptions, contextDir string) ([]client.ExportEntry, string, error) {
	exports := make([]client.ExportEntry, 0, len(opts.ExtraOutputs)+2)
	var ociPath string

	if len(opts.ExtraOutputs) == 0 && !opts.SkipDefaultOCILayout {
		defaultOCI := opts.OCIOutputPath
		if defaultOCI == "" {
			sanitized := sanitizePathName(contextDir)
			defaultOCI = filepath.Join(DefaultOCIOutputDir(contextDir), sanitized)
		}
		if err := os.MkdirAll(defaultOCI, 0o755); err != nil {
			return nil, "", fmt.Errorf("create oci output dir: %w", err)
		}
		entry, err := convertOutputSpec(OutputSpec{
			Type: client.ExporterOCI,
			Attrs: map[string]string{
				"dest": defaultOCI,
				"tar":  "false",
			},
		})
		if err != nil {
			return nil, "", err
		}
		exports = append(exports, entry)
		ociPath = defaultOCI
	}

	for _, spec := range opts.ExtraOutputs {
		entry, err := convertOutputSpec(spec)
		if err != nil {
			return nil, "", err
		}
		exports = append(exports, entry)
	}

	if len(opts.Tags) > 0 || opts.Push || opts.LoadToContainerd {
		if len(opts.Tags) == 0 {
			return nil, "", errors.New("at least one --tag is required when pushing or loading into containerd")
		}
		attrs := map[string]string{}
		attrs[string(exptypes.OptKeyName)] = strings.Join(opts.Tags, ",")
		attrs[string(exptypes.OptKeyStore)] = "true"
		if opts.Push {
			attrs[string(exptypes.OptKeyPush)] = "true"
		}
		if opts.LoadToContainerd {
			attrs[string(exptypes.OptKeyUnpack)] = "true"
		}
		exports = append(exports, client.ExportEntry{Type: client.ExporterImage, Attrs: attrs})
	}

	if len(exports) == 0 {
		return nil, "", errors.New("no exporters configured; specify --tag, --push, --output, or provide a Dockerfile export target")
	}

	return exports, ociPath, nil
}

func convertOutputSpec(spec OutputSpec) (client.ExportEntry, error) {
	if spec.Type == "" {
		return client.ExportEntry{}, errors.New("output type is required")
	}
	attrs := map[string]string{}
	for k, v := range spec.Attrs {
		attrs[strings.ToLower(k)] = v
	}
	dest := attrs["dest"]

	entry := client.ExportEntry{Type: spec.Type, Attrs: attrs}
	output, outputDir, err := resolveExporterDest(spec.Type, dest, attrs)
	if err != nil {
		return client.ExportEntry{}, err
	}
	entry.Output = output
	entry.OutputDir = outputDir
	if (output != nil || outputDir != "") && dest != "" {
		delete(entry.Attrs, "dest")
	}
	return entry, nil
}

func resolveExporterDest(exporter, dest string, attrs map[string]string) (filesync.FileOutputFunc, string, error) {
	wrapWriter := func(path string) (filesync.FileOutputFunc, error) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, err
		}
		file, err := os.Create(path)
		if err != nil {
			return nil, err
		}
		return func(map[string]string) (io.WriteCloser, error) {
			return file, nil
		}, nil
	}

	supportFile := false
	supportDir := false
	switch exporter {
	case client.ExporterLocal:
		supportDir = true
	case client.ExporterTar:
		supportFile = true
	case client.ExporterOCI, client.ExporterDocker:
		tarMode := true
		if v, ok := attrs["tar"]; ok {
			if parsed, err := strconv.ParseBool(v); err == nil {
				tarMode = parsed
			}
		}
		supportFile = tarMode
		supportDir = !tarMode
	}

	if supportDir {
		if dest == "" {
			return nil, "", fmt.Errorf("output directory is required for %s exporter", exporter)
		}
		if err := os.MkdirAll(dest, 0o755); err != nil {
			return nil, "", err
		}
		return nil, dest, nil
	}
	if supportFile {
		if dest == "" {
			return nil, "", fmt.Errorf("destination file is required for %s exporter", exporter)
		}
		writer, err := wrapWriter(dest)
		if err != nil {
			return nil, "", err
		}
		return writer, "", nil
	}
	if dest != "" {
		return nil, "", fmt.Errorf("exporter %s does not support dest", exporter)
	}
	return nil, "", nil
}

func sanitizePathName(path string) string {
	base := filepath.Base(path)
	base = strings.ToLower(base)
	base = strings.ReplaceAll(base, " ", "-")
	base = strings.ReplaceAll(base, "_", "-")
	base = strings.Trim(base, "-.")
	if base == "" {
		base = "ktl-build"
	}
	return base
}

func fanOutSolveStatus(ch chan *client.SolveStatus, observers []ProgressObserver) {
	if len(observers) == 0 || ch == nil {
		return
	}
	for status := range ch {
		for _, observer := range observers {
			if observer == nil || status == nil {
				continue
			}
			observer.HandleStatus(status)
		}
	}
}

func emitDiagnostics(ch chan *client.SolveStatus, observers []BuildDiagnosticObserver) {
	if ch == nil || len(observers) == 0 {
		// Drain the channel if it was provided to avoid blocking the writer pipe.
		for range ch {
		}
		return
	}
	emitter := &diagnosticEmitter{
		observers: observers,
		emitted:   make(map[digest.Digest]map[BuildDiagnosticType]struct{}),
	}
	for status := range ch {
		emitter.consume(status)
	}
}

type diagnosticEmitter struct {
	observers []BuildDiagnosticObserver
	emitted   map[digest.Digest]map[BuildDiagnosticType]struct{}
}

func (d *diagnosticEmitter) consume(status *client.SolveStatus) {
	if status == nil {
		return
	}
	for _, vertex := range status.Vertexes {
		if vertex == nil {
			continue
		}
		var dgst digest.Digest
		if vertex.Digest != "" {
			dgst = digest.Digest(vertex.Digest)
		}
		if vertex.Cached {
			d.emit(BuildDiagnostic{
				Vertex: dgst,
				Name:   vertex.Name,
				Type:   DiagnosticCacheHit,
				Reason: "cache hit",
			})
		} else if vertex.Completed != nil {
			d.emit(BuildDiagnostic{
				Vertex: dgst,
				Name:   vertex.Name,
				Type:   DiagnosticCacheMiss,
				Reason: "cache miss (no reusable layer found)",
			})
		}
	}
}

func (d *diagnosticEmitter) emit(diag BuildDiagnostic) {
	if diag.Type == "" {
		return
	}
	key := diag.Vertex
	if _, ok := d.emitted[key]; !ok {
		d.emitted[key] = make(map[BuildDiagnosticType]struct{})
	}
	if _, seen := d.emitted[key][diag.Type]; seen {
		return
	}
	d.emitted[key][diag.Type] = struct{}{}
	for _, observer := range d.observers {
		if observer == nil {
			continue
		}
		observer.HandleDiagnostic(diag)
	}
}
