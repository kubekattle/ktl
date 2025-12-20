package compose

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/containerd/console"
	"github.com/docker/cli/cli/config"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/example/ktl/internal/csvutil"
	"github.com/example/ktl/pkg/buildkit"
	"github.com/example/ktl/pkg/registry"
)

type (
	CacheSpec              = buildkit.CacheSpec
	OutputSpec             = buildkit.OutputSpec
	BuildResult            = buildkit.BuildResult
	DockerfileBuildOptions = buildkit.DockerfileBuildOptions
)

// ComposeBuildOptions configure a compose-driven build.
type ComposeBuildOptions struct {
	Files                []string
	ProjectName          string
	Services             []string
	Profiles             []string
	BuilderAddr          string
	AllowBuilderFallback bool
	CacheDir             string
	Push                 bool
	Load                 bool
	NoCache              bool
	Pull                 bool
	AttestProvenance     bool
	AttestSBOM           bool
	Platforms            []string
	BuildArgs            map[string]string
	CacheExports         []CacheSpec
	CacheImports         []CacheSpec
	ExtraOutputs         []OutputSpec
	ProgressMode         string
	ProgressOutput       console.File
	DockerConfig         *configfile.ConfigFile
	Parallelism          int
	ProgressObservers    []buildkit.ProgressObserver
	DiagnosticObservers  []buildkit.BuildDiagnosticObserver
	HeatmapListener      HeatmapListener
}

// ServiceBuildResult captures the output of a compose service build.
type ServiceBuildResult struct {
	Service string
	Image   string
	Tags    []string
	Result  *BuildResult
}

// Runner exposes compose build orchestration for reuse.
type Runner interface {
	BuildCompose(ctx context.Context, opts ComposeBuildOptions) ([]ServiceBuildResult, error)
}

// NewRunner returns a compose runner using the provided dependencies (defaults when nil).
func NewRunner(buildRunner buildkit.Runner, reg registry.Client) Runner {
	if buildRunner == nil {
		buildRunner = buildkit.NewRunner()
	}
	if reg == nil {
		reg = registry.NewClient()
	}
	return &composeRunner{
		buildRunner: buildRunner,
		registry:    reg,
	}
}

var defaultRunner Runner = NewRunner(nil, nil)

// BuildCompose loads a compose project and builds every service with a build specification.
func BuildCompose(ctx context.Context, opts ComposeBuildOptions) ([]ServiceBuildResult, error) {
	return defaultRunner.BuildCompose(ctx, opts)
}

type composeRunner struct {
	buildRunner buildkit.Runner
	registry    registry.Client
}

func (r *composeRunner) BuildCompose(ctx context.Context, opts ComposeBuildOptions) ([]ServiceBuildResult, error) {
	if len(opts.Files) == 0 {
		return nil, errors.New("no compose files specified")
	}
	builderProvided := opts.BuilderAddr != ""
	if opts.BuilderAddr == "" {
		opts.BuilderAddr = buildkit.DefaultBuilderAddress()
	}
	if !builderProvided && !opts.AllowBuilderFallback {
		opts.AllowBuilderFallback = true
	}
	if opts.CacheDir == "" {
		opts.CacheDir = buildkit.DefaultCacheDir()
	}
	if err := os.MkdirAll(opts.CacheDir, 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	if opts.ProgressMode == "" {
		opts.ProgressMode = "auto"
	}
	if opts.ProgressOutput == nil {
		opts.ProgressOutput = os.Stderr
	}
	if opts.DockerConfig == nil {
		opts.DockerConfig = config.LoadDefaultConfigFile(os.Stderr)
	}
	if opts.BuildArgs == nil {
		opts.BuildArgs = map[string]string{}
	}

	normalizedFiles, err := absolutePaths(opts.Files)
	if err != nil {
		return nil, err
	}
	opts.Files = normalizedFiles
	opts.Platforms = buildkit.NormalizePlatforms(opts.Platforms)

	project, err := loadComposeProject(opts)
	if err != nil {
		return nil, err
	}

	tasks, skipped, err := collectBuildableServices(project, opts.Services)
	if err != nil {
		return nil, err
	}
	if len(tasks) == 0 {
		if len(skipped) > 0 {
			return nil, fmt.Errorf("no buildable services found; skipped: %s", strings.Join(skipped, ", "))
		}
		return nil, errors.New("no buildable services found")
	}

	dependents, pending := composeDependencyGraph(tasks)
	results, err := r.runComposeBuilds(ctx, project, tasks, dependents, pending, opts)
	if err != nil {
		return nil, err
	}
	return results, nil
}

// LoadComposeProject exposes compose loading so callers can inspect project metadata.
func LoadComposeProject(files []string, projectName string, profiles []string) (*composetypes.Project, error) {
	return loadComposeProject(ComposeBuildOptions{Files: files, ProjectName: projectName, Profiles: profiles})
}

// CollectBuildableServices returns all build-capable services referenced by the requested set.
func CollectBuildableServices(project *composetypes.Project, requested []string) (map[string]composetypes.ServiceConfig, []string, error) {
	return collectBuildableServices(project, requested)
}

func collectBuildableServices(project *composetypes.Project, requested []string) (map[string]composetypes.ServiceConfig, []string, error) {
	visited := make(map[string]bool)
	selection := make([]string, 0)
	var visit func(string) error
	visit = func(name string) error {
		if visited[name] {
			return nil
		}
		svc, err := project.GetService(name)
		if err != nil {
			return err
		}
		visited[name] = true
		for dep := range svc.DependsOn {
			if err := visit(dep); err != nil {
				return err
			}
		}
		selection = append(selection, name)
		return nil
	}

	names := requested
	if len(names) == 0 {
		names = project.ServiceNames()
	}
	for _, name := range names {
		if err := visit(name); err != nil {
			return nil, nil, err
		}
	}

	services := make(map[string]composetypes.ServiceConfig)
	skipped := make([]string, 0)
	seen := make(map[string]bool)
	for i := len(selection) - 1; i >= 0; i-- {
		name := selection[i]
		if seen[name] {
			continue
		}
		seen[name] = true
		svc, _ := project.GetService(name)
		if svc.Build == nil || svc.Build.Context == "" {
			skipped = append(skipped, name)
			continue
		}
		services[name] = svc
	}
	sort.Strings(skipped)
	return services, skipped, nil
}

// ComposeDependencyGraph computes the dependency graph for the provided build tasks.
func ComposeDependencyGraph(tasks map[string]composetypes.ServiceConfig) (map[string][]string, map[string]int) {
	return composeDependencyGraph(tasks)
}

// ServiceTags returns the tags that will be applied to a composed service build.
func ServiceTags(project *composetypes.Project, name string, cfg composetypes.ServiceConfig) []string {
	return composeTags(project, name, cfg)
}

func composeDependencyGraph(tasks map[string]composetypes.ServiceConfig) (map[string][]string, map[string]int) {
	dependents := make(map[string][]string, len(tasks))
	pending := make(map[string]int, len(tasks))
	for name, svc := range tasks {
		count := 0
		for dep := range svc.DependsOn {
			if _, ok := tasks[dep]; !ok {
				continue
			}
			count++
			dependents[dep] = append(dependents[dep], name)
		}
		pending[name] = count
	}
	return dependents, pending
}

func (r *composeRunner) runComposeBuilds(ctx context.Context, project *composetypes.Project, tasks map[string]composetypes.ServiceConfig, dependents map[string][]string, pending map[string]int, opts ComposeBuildOptions) ([]ServiceBuildResult, error) {
	parallelism := opts.Parallelism
	if parallelism <= 0 {
		parallelism = runtime.NumCPU()
		if parallelism < 1 {
			parallelism = 1
		}
	}

	ready := make([]string, 0)
	for name, count := range pending {
		if count == 0 {
			ready = append(ready, name)
		}
	}
	sort.Strings(ready)
	if len(ready) == 0 {
		return nil, errors.New("compose graph contains cycles or no independent services")
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	var (
		mu        sync.Mutex
		cond      = sync.NewCond(&mu)
		results   []ServiceBuildResult
		firstErr  error
		active    int
		remaining = len(tasks)
	)

	start := func(name string) {
		active++
		go func(service string) {
			res, err := r.buildComposeService(ctx, project, service, tasks[service], opts)
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("build service %s: %w", service, err)
					cancel()
				}
			} else {
				results = append(results, res)
				for _, dep := range dependents[service] {
					pending[dep]--
					if pending[dep] == 0 {
						ready = append(ready, dep)
					}
				}
			}
			active--
			remaining--
			cond.Broadcast()
		}(name)
	}

	mu.Lock()
	for len(ready) > 0 && active < parallelism {
		name := ready[0]
		ready = ready[1:]
		start(name)
	}
	for firstErr == nil && remaining > 0 {
		if len(ready) == 0 && active == 0 {
			firstErr = errors.New("compose graph deadlock detected")
			break
		}
		cond.Wait()
		for len(ready) > 0 && active < parallelism && firstErr == nil {
			name := ready[0]
			ready = ready[1:]
			start(name)
		}
	}
	for active > 0 {
		cond.Wait()
	}
	mu.Unlock()

	if firstErr != nil {
		return nil, firstErr
	}
	sort.Slice(results, func(i, j int) bool { return results[i].Service < results[j].Service })
	return results, nil
}

func (r *composeRunner) buildComposeService(ctx context.Context, project *composetypes.Project, name string, svc composetypes.ServiceConfig, opts ComposeBuildOptions) (ServiceBuildResult, error) {
	buildCtx := svc.Build.Context
	if buildCtx == "" {
		buildCtx = project.WorkingDir
	}
	buildCtx = resolveRelative(project.WorkingDir, buildCtx)

	dockerfilePath := svc.Build.Dockerfile
	var inlinePath string
	if svc.Build.DockerfileInline != "" {
		path, err := writeInlineDockerfile(opts.CacheDir, name, svc.Build.DockerfileInline)
		if err != nil {
			return ServiceBuildResult{}, err
		}
		inlinePath = path
		dockerfilePath = path
	}
	if dockerfilePath == "" {
		dockerfilePath = filepath.Join(buildCtx, "Dockerfile")
	}
	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(buildCtx, dockerfilePath)
	}
	if inlinePath != "" {
		defer os.Remove(inlinePath)
	}

	servicePlatforms := sliceFromStringList(svc.Build.Platforms)
	if len(servicePlatforms) == 0 {
		servicePlatforms = opts.Platforms
	}

	serviceArgs := mergeArgs(opts.BuildArgs, resolveBuildArgs(svc.Build.Args))

	cacheExports := cloneCacheSpecs(opts.CacheExports)
	cacheImports := cloneCacheSpecs(opts.CacheImports)
	if len(svc.Build.CacheTo) > 0 {
		specs, err := parseCacheSpecsFromList(svc.Build.CacheTo)
		if err != nil {
			return ServiceBuildResult{}, fmt.Errorf("service %s cache_to: %w", name, err)
		}
		cacheExports = append(cacheExports, specs...)
	}
	if len(svc.Build.CacheFrom) > 0 {
		specs, err := parseCacheSpecsFromList(svc.Build.CacheFrom)
		if err != nil {
			return ServiceBuildResult{}, fmt.Errorf("service %s cache_from: %w", name, err)
		}
		cacheImports = append(cacheImports, specs...)
	}

	tags := composeTags(project, name, svc)

	ociDir := filepath.Join(opts.CacheDir, "oci", sanitizeName(name))
	var collector *serviceHeatmapCollector
	if opts.HeatmapListener != nil {
		collector = newServiceHeatmapCollector(name)
	}

	progressObservers := append([]buildkit.ProgressObserver(nil), opts.ProgressObservers...)
	if collector != nil {
		progressObservers = append([]buildkit.ProgressObserver{collector}, progressObservers...)
	}

	diagnosticObservers := append([]buildkit.BuildDiagnosticObserver(nil), opts.DiagnosticObservers...)
	if collector != nil {
		diagnosticObservers = append([]buildkit.BuildDiagnosticObserver{collector}, diagnosticObservers...)
	}

	svcOpts := DockerfileBuildOptions{
		BuilderAddr:          opts.BuilderAddr,
		AllowBuilderFallback: opts.AllowBuilderFallback,
		ContextDir:           buildCtx,
		DockerfilePath:       dockerfilePath,
		Platforms:            servicePlatforms,
		BuildArgs:            serviceArgs,
		Target:               svc.Build.Target,
		Tags:                 tags,
		Push:                 opts.Push,
		LoadToContainerd:     opts.Load,
		CacheDir:             opts.CacheDir,
		CacheExports:         cacheExports,
		CacheImports:         cacheImports,
		ExtraOutputs:         opts.ExtraOutputs,
		NoCache:              opts.NoCache || svc.Build.NoCache,
		Pull:                 opts.Pull || svc.Build.Pull,
		ProgressMode:         opts.ProgressMode,
		ProgressOutput:       opts.ProgressOutput,
		DockerConfig:         opts.DockerConfig,
		SkipDefaultOCILayout: false,
		OCIOutputPath:        ociDir,
		AttestProvenance:     opts.AttestProvenance,
		AttestSBOM:           opts.AttestSBOM,
		ProgressObservers:    progressObservers,
		DiagnosticObservers:  diagnosticObservers,
	}

	res, err := r.buildRunner.BuildDockerfile(ctx, svcOpts)
	if collector != nil && opts.HeatmapListener != nil {
		summary := collector.snapshotSummary(err)
		opts.HeatmapListener.HandleServiceHeatmap(summary)
	}
	if err != nil {
		return ServiceBuildResult{}, err
	}
	if res.OCIOutputPath != "" {
		if err := r.registry.RecordBuild(tags, res.OCIOutputPath); err != nil {
			return ServiceBuildResult{}, err
		}
	}

	return ServiceBuildResult{
		Service: name,
		Image:   firstOrEmpty(tags),
		Tags:    tags,
		Result:  res,
	}, nil
}

func loadComposeProject(opts ComposeBuildOptions) (*composetypes.Project, error) {
	env := make(composetypes.Mapping)
	for _, kv := range os.Environ() {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[key] = value
	}

	configFiles := make([]composetypes.ConfigFile, 0, len(opts.Files))
	for _, path := range opts.Files {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read compose file %s: %w", path, err)
		}
		configFiles = append(configFiles, composetypes.ConfigFile{Filename: path, Content: data})
	}

	workingDir := filepath.Dir(opts.Files[0])

	details := composetypes.ConfigDetails{
		WorkingDir:  workingDir,
		ConfigFiles: configFiles,
		Environment: env,
	}

	project, err := loader.Load(details, func(o *loader.Options) {
		if opts.ProjectName != "" {
			o.SetProjectName(opts.ProjectName, true)
		}
		if len(opts.Profiles) > 0 {
			o.Profiles = append(o.Profiles, opts.Profiles...)
		}
	})
	if err != nil {
		return nil, err
	}
	return project, nil
}

func absolutePaths(paths []string) ([]string, error) {
	out := make([]string, len(paths))
	for i, p := range paths {
		if p == "" {
			return nil, errors.New("compose file path cannot be empty")
		}
		abs, err := filepath.Abs(p)
		if err != nil {
			return nil, fmt.Errorf("abs %s: %w", p, err)
		}
		out[i] = abs
	}
	return out, nil
}

func resolveRelative(base, target string) string {
	if target == "" {
		return base
	}
	if filepath.IsAbs(target) {
		return target
	}
	return filepath.Join(base, target)
}

func mergeArgs(base map[string]string, overrides map[string]string) map[string]string {
	merged := make(map[string]string, len(base)+len(overrides))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overrides {
		merged[k] = v
	}
	return merged
}

func resolveBuildArgs(args composetypes.MappingWithEquals) map[string]string {
	resolved := make(map[string]string, len(args))
	for k, v := range args {
		if v != nil {
			resolved[k] = *v
			continue
		}
		if envVal, ok := os.LookupEnv(k); ok {
			resolved[k] = envVal
		}
	}
	return resolved
}

func sliceFromStringList(list composetypes.StringList) []string {
	if len(list) == 0 {
		return nil
	}
	out := make([]string, len(list))
	copy(out, list)
	return out
}

func parseCacheSpecsFromList(list composetypes.StringList) ([]CacheSpec, error) {
	specs := make([]CacheSpec, 0, len(list))
	for _, raw := range list {
		spec, err := parseCacheSpecString(raw)
		if err != nil {
			return nil, err
		}
		specs = append(specs, spec)
	}
	return specs, nil
}

func parseCacheSpecString(value string) (CacheSpec, error) {
	fields, err := csvutil.SplitFields(value)
	if err != nil {
		return CacheSpec{}, err
	}
	spec := CacheSpec{Attrs: map[string]string{}}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		if field == "" {
			continue
		}
		key, val, ok := strings.Cut(field, "=")
		if !ok {
			if spec.Type == "" {
				spec.Type = field
			} else {
				spec.Attrs[field] = ""
			}
			continue
		}
		key = strings.ToLower(strings.TrimSpace(key))
		val = strings.TrimSpace(val)
		if key == "type" {
			spec.Type = val
			continue
		}
		spec.Attrs[key] = val
	}
	if spec.Type == "" {
		trimmed := strings.TrimSpace(value)
		if trimmed == "" {
			return CacheSpec{}, fmt.Errorf("invalid cache spec %q", value)
		}
		spec.Type = "registry"
		spec.Attrs["ref"] = trimmed
	}
	return spec, nil
}

func cloneCacheSpecs(specs []CacheSpec) []CacheSpec {
	if len(specs) == 0 {
		return nil
	}
	out := make([]CacheSpec, len(specs))
	for i, spec := range specs {
		attrs := make(map[string]string, len(spec.Attrs))
		for k, v := range spec.Attrs {
			attrs[k] = v
		}
		out[i] = CacheSpec{Type: spec.Type, Attrs: attrs}
	}
	return out
}

func composeTags(project *composetypes.Project, service string, cfg composetypes.ServiceConfig) []string {
	var tags []string
	if cfg.Image != "" {
		tags = append(tags, cfg.Image)
	}
	if len(cfg.Build.Tags) > 0 {
		tags = append(tags, sliceFromStringList(cfg.Build.Tags)...)
	}
	if len(tags) == 0 {
		projectName := project.Name
		if projectName == "" {
			projectName = filepath.Base(project.WorkingDir)
		}
		fallback := fmt.Sprintf("ktl.local/%s-%s:dev", sanitizeName(projectName), sanitizeName(service))
		tags = append(tags, fallback)
	}
	return tags
}

func sanitizeName(name string) string {
	cleaned := strings.ToLower(name)
	cleaned = strings.ReplaceAll(cleaned, " ", "-")
	cleaned = strings.ReplaceAll(cleaned, "_", "-")
	cleaned = strings.Trim(cleaned, "-.")
	if cleaned == "" {
		cleaned = "service"
	}
	return cleaned
}

func writeInlineDockerfile(tempDir, service, content string) (string, error) {
	if tempDir == "" {
		tempDir = os.TempDir()
	}
	if err := os.MkdirAll(tempDir, 0o755); err != nil {
		return "", fmt.Errorf("prepare temp dir: %w", err)
	}
	file, err := os.CreateTemp(tempDir, fmt.Sprintf("%s-dockerfile-*.tmp", sanitizeName(service)))
	if err != nil {
		return "", fmt.Errorf("create inline dockerfile: %w", err)
	}
	defer file.Close()
	if _, err := file.WriteString(content); err != nil {
		return "", fmt.Errorf("write inline dockerfile: %w", err)
	}
	return file.Name(), nil
}

func firstOrEmpty(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}
