package buildkit

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/containerd/console"
	"github.com/docker/cli/cli/config/configfile"
	"github.com/moby/buildkit/client"
	"github.com/opencontainers/go-digest"
)

// CacheSpec represents a cache import or export entry in a user-friendly form.
type CacheSpec struct {
	Type  string
	Attrs map[string]string
}

// OutputSpec represents a BuildKit exporter configuration.
type OutputSpec struct {
	Type  string
	Attrs map[string]string
}

// Secret configures a BuildKit secret exposed during the build.
type Secret struct {
	ID   string
	Env  string
	File string
}

// DockerfileBuildOptions configures a Dockerfile-based build invocation.
type DockerfileBuildOptions struct {
	BuilderAddr          string
	AllowBuilderFallback bool
	DockerContext        string
	ContextDir           string
	DockerfilePath       string
	Platforms            []string
	BuildArgs            map[string]string
	Secrets              []Secret
	Target               string
	Tags                 []string
	Push                 bool
	LoadToContainerd     bool
	CacheDir             string
	CacheExports         []CacheSpec
	CacheImports         []CacheSpec
	ExtraOutputs         []OutputSpec
	NoCache              bool
	Pull                 bool
	ProgressMode         string
	ProgressOutput       console.File
	DockerConfig         *configfile.ConfigFile
	OCIOutputPath        string
	SkipDefaultOCILayout bool
	AttestProvenance     bool
	AttestSBOM           bool
	Interactive          *InteractiveShellConfig
	ProgressObservers    []ProgressObserver
	DiagnosticObservers  []BuildDiagnosticObserver
	PhaseEmitter         PhaseEmitter
}

// BuildResult describes the result of a Dockerfile build.
type BuildResult struct {
	Digest           string
	ExporterResponse map[string]string
	OCIOutputPath    string
}

// InteractiveShellConfig configures the optional debugging shell started after a failed RUN.
type InteractiveShellConfig struct {
	Shell   []string
	Stdin   io.Reader
	Stdout  io.Writer
	Stderr  io.Writer
	TTY     console.File
	Console console.Console
}

// ProgressObserver consumes raw BuildKit solve status updates (vertex statuses, logs, etc.).
type ProgressObserver interface {
	HandleStatus(*client.SolveStatus)
}

// BuildDiagnosticType enumerates diagnostic event categories.
type BuildDiagnosticType string

const (
	// DiagnosticCacheHit indicates BuildKit reported a cached vertex.
	DiagnosticCacheHit BuildDiagnosticType = "cache_hit"
	// DiagnosticCacheMiss indicates BuildKit executed a vertex because cache wasn't reusable.
	DiagnosticCacheMiss BuildDiagnosticType = "cache_miss"
)

// BuildDiagnostic captures metadata about cache behavior or other heuristics during a solve.
type BuildDiagnostic struct {
	Vertex digest.Digest
	Name   string
	Type   BuildDiagnosticType
	Reason string
}

// BuildDiagnosticObserver consumes structured diagnostics emitted during a solve.
type BuildDiagnosticObserver interface {
	HandleDiagnostic(BuildDiagnostic)
}

// PhaseEmitter records high-level phase transitions for a build.
// Implementations should be fast and non-blocking.
type PhaseEmitter interface {
	EmitPhase(name, state, message string)
}

type PhaseEmitterFunc func(name, state, message string)

func (f PhaseEmitterFunc) EmitPhase(name, state, message string) {
	if f == nil {
		return
	}
	f(name, state, message)
}

// Runner defines the programmable contract for invoking BuildKit solves.
type Runner interface {
	BuildDockerfile(ctx context.Context, opts DockerfileBuildOptions) (*BuildResult, error)
}

type defaultRunner struct{}

// NewRunner returns the default BuildKit runner implementation used by the CLI.
func NewRunner() Runner {
	return defaultRunner{}
}

func (defaultRunner) BuildDockerfile(ctx context.Context, opts DockerfileBuildOptions) (*BuildResult, error) {
	return BuildDockerfile(ctx, opts)
}

// DefaultBuilderAddress returns the best-effort rootless BuildKit socket.
func DefaultBuilderAddress() string {
	if v := os.Getenv("KTL_BUILDKIT_HOST"); v != "" {
		return v
	}
	if v := os.Getenv("BUILDKIT_HOST"); v != "" {
		return v
	}
	if runtime.GOOS == "windows" {
		return "npipe:////./pipe/buildkitd"
	}
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return "unix://" + filepath.Join(dir, "buildkit", "buildkitd.sock")
	}
	if uid := os.Getenv("UID"); uid != "" {
		return fmt.Sprintf("unix:///run/user/%s/buildkit/buildkitd.sock", uid)
	}
	if u, err := user.Current(); err == nil && u.Uid != "" {
		return fmt.Sprintf("unix:///run/user/%s/buildkit/buildkitd.sock", u.Uid)
	}
	return "unix:///run/user/1000/buildkit/buildkitd.sock"
}

// DefaultCacheDir returns a user cache folder for BuildKit metadata.
func DefaultCacheDir() string {
	if v := os.Getenv("KTL_BUILDKIT_CACHE"); v != "" {
		return v
	}
	if cacheDir, err := os.UserCacheDir(); err == nil {
		return filepath.Join(cacheDir, "ktl", "buildkit-cache")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		return filepath.Join(home, ".cache", "ktl", "buildkit-cache")
	}
	return filepath.Join(os.TempDir(), "ktl-buildkit-cache")
}

// DefaultOCIOutputDir returns the dist/ path for OCI exports relative to the context.
func DefaultOCIOutputDir(contextDir string) string {
	abs := contextDir
	if !filepath.IsAbs(abs) {
		abs, _ = filepath.Abs(abs)
	}
	return filepath.Join(abs, "dist", "oci")
}

// DefaultLocalTag derives a reproducible local tag from the context path.
func DefaultLocalTag(contextDir string) string {
	base := filepath.Base(contextDir)
	if base == "." || base == string(filepath.Separator) {
		base = "workspace"
	}
	sanitized := strings.ToLower(base)
	sanitized = strings.ReplaceAll(sanitized, " ", "-")
	sanitized = strings.ReplaceAll(sanitized, "_", "-")
	sanitized = strings.Trim(sanitized, "-.")
	if sanitized == "" {
		sanitized = "workspace"
	}
	return fmt.Sprintf("ktl.local/%s:dev", sanitized)
}
