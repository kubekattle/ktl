package featureflags

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// Stage indicates the lifecycle of a feature flag.
type Stage string

const (
	StageExperimental Stage = "experimental"
	StageBeta         Stage = "beta"
	StageGA           Stage = "ga"
)

// Name is the canonical identifier for a feature flag (kebab-case).
type Name string

const (
	// FeatureDeployPlanHTMLV3 guards the in-progress plan visualization refresh.
	FeatureDeployPlanHTMLV3 Name = "deploy-plan-html-v3"
)

// Definition tracks the metadata for a feature flag.
type Definition struct {
	Name        Name
	Description string
	Stage       Stage
	Default     bool
}

var registry = map[Name]Definition{
	FeatureDeployPlanHTMLV3: {
		Name:        FeatureDeployPlanHTMLV3,
		Description: "Switch ktl plan visualize output to the v3 UI components.",
		Stage:       StageExperimental,
		Default:     false,
	},
}

// ErrUnknownFeature is returned when a caller references a flag that has not been registered.
var ErrUnknownFeature = errors.New("unknown feature flag")

// DefinitionByName returns the definition for the provided feature.
func DefinitionByName(name Name) (Definition, bool) {
	def, ok := registry[name]
	return def, ok
}

// Definitions returns the full set of registered flags in alphabetical order.
func Definitions() []Definition {
	defs := make([]Definition, 0, len(registry))
	for _, def := range registry {
		defs = append(defs, def)
	}
	sort.Slice(defs, func(i, j int) bool {
		return defs[i].Name < defs[j].Name
	})
	return defs
}

// Flags captures the resolved state of all feature flags for a ktl invocation.
type Flags struct {
	values map[Name]bool
}

// Enabled reports whether the provided feature is on for the current process.
func (f Flags) Enabled(name Name) bool {
	if f.values == nil {
		return false
	}
	return f.values[name]
}

// EnabledNames returns the currently enabled flag names in alphabetical order.
func (f Flags) EnabledNames() []Name {
	if len(f.values) == 0 {
		return nil
	}
	names := make([]Name, 0, len(f.values))
	for name, on := range f.values {
		if on {
			names = append(names, name)
		}
	}
	sort.Slice(names, func(i, j int) bool { return names[i] < names[j] })
	return names
}

// EnvVar returns the environment variable that toggles the flag (e.g. KTL_FEATURE_DEPLOY_PLAN_HTML_V3).
func (d Definition) EnvVar() string {
	return envVarForName(d.Name)
}

// Resolve combines defaults plus explicit sources (CLI/config/env) into a Flags set.
func Resolve(sources ...[]string) (Flags, error) {
	values := make(map[Name]bool, len(registry))
	for _, def := range registry {
		if def.Default {
			values[def.Name] = true
		}
	}
	for _, source := range sources {
		for _, token := range splitTokens(source) {
			if token == "" {
				continue
			}
			name := normalizeName(token)
			if _, ok := registry[name]; !ok {
				return Flags{}, fmt.Errorf("%w: %s", ErrUnknownFeature, token)
			}
			values[name] = true
		}
	}
	return Flags{values: values}, nil
}

// EnabledFromEnv scans the current environment (or the provided list) for KTL_FEATURE_* values.
func EnabledFromEnv(environ []string) []string {
	if environ == nil {
		environ = os.Environ()
	}
	var enabled []string
	for _, entry := range environ {
		if !strings.HasPrefix(entry, "KTL_FEATURE_") {
			continue
		}
		parts := strings.SplitN(entry, "=", 2)
		if len(parts) != 2 {
			continue
		}
		if !isTruthy(parts[1]) {
			continue
		}
		name := strings.TrimPrefix(parts[0], "KTL_FEATURE_")
		name = strings.ReplaceAll(name, "_", "-")
		name = strings.ToLower(name)
		enabled = append(enabled, name)
	}
	return enabled
}

// ContextWithFlags stores the resolved flags on the provided context.
func ContextWithFlags(ctx context.Context, flags Flags) context.Context {
	return context.WithValue(ctx, ctxKey{}, flags)
}

// FromContext extracts the feature flag set from a context. The return value is safe to use even when no flags were stored.
func FromContext(ctx context.Context) Flags {
	if ctx == nil {
		return Flags{}
	}
	flags, ok := ctx.Value(ctxKey{}).(Flags)
	if !ok {
		return Flags{}
	}
	return flags
}

type ctxKey struct{}

func splitTokens(values []string) []string {
	var tokens []string
	for _, value := range values {
		if value == "" {
			continue
		}
		for _, part := range strings.Split(value, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				tokens = append(tokens, part)
			}
		}
	}
	return tokens
}

func normalizeName(raw string) Name {
	raw = strings.TrimSpace(raw)
	raw = strings.ToLower(raw)
	raw = strings.ReplaceAll(raw, "_", "-")
	return Name(raw)
}

func envVarForName(name Name) string {
	upper := strings.ToUpper(string(name))
	upper = strings.ReplaceAll(upper, "-", "_")
	return "KTL_FEATURE_" + upper
}

func isTruthy(val string) bool {
	val = strings.TrimSpace(strings.ToLower(val))
	switch val {
	case "1", "t", "true", "y", "yes", "on":
		return true
	default:
		return false
	}
}
