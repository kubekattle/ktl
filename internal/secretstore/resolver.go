package secretstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ResolveMode controls how resolved secrets are returned.
type ResolveMode string

const (
	ResolveModeValue ResolveMode = "value"
	ResolveModeMask  ResolveMode = "mask"
)

// Provider resolves secret paths.
type Provider interface {
	Resolve(ctx context.Context, path string) (string, error)
}

// Lister exposes optional listing of secret keys under a path.
type Lister interface {
	List(ctx context.Context, path string) ([]string, error)
}

// ResolverOptions customize resolver behavior.
type ResolverOptions struct {
	DefaultProvider string
	Mode            ResolveMode
	Mask            string
	BaseDir         string
}

// AuditEntry records a secret reference that was resolved.
type AuditEntry struct {
	Provider  string
	Path      string
	Reference string
	Masked    bool
}

// AuditReport is a sorted list of resolved references.
type AuditReport struct {
	Entries []AuditEntry
}

// Empty reports whether the report has any entries.
func (r AuditReport) Empty() bool {
	return len(r.Entries) == 0
}

type Resolver struct {
	providers       map[string]Provider
	defaultProvider string
	mode            ResolveMode
	mask            string
	cache           map[string]string
	seen            map[string]struct{}
	audit           []AuditEntry
}

// NewResolver builds a resolver from config and options.
func NewResolver(cfg Config, opts ResolverOptions) (*Resolver, error) {
	providers := make(map[string]Provider, len(cfg.Providers))
	for name, pcfg := range cfg.Providers {
		providerName := strings.TrimSpace(name)
		if providerName == "" {
			return nil, fmt.Errorf("secret provider name cannot be empty")
		}
		providerType := strings.ToLower(strings.TrimSpace(pcfg.Type))
		switch providerType {
		case "file":
			provider, err := newFileProvider(pcfg.Path, opts.BaseDir)
			if err != nil {
				return nil, fmt.Errorf("provider %q: %w", providerName, err)
			}
			providers[providerName] = provider
		case "vault":
			provider, err := newVaultProvider(pcfg)
			if err != nil {
				return nil, fmt.Errorf("provider %q: %w", providerName, err)
			}
			providers[providerName] = provider
		case "":
			return nil, fmt.Errorf("provider %q missing type", providerName)
		default:
			return nil, fmt.Errorf("provider %q has unsupported type %q", providerName, providerType)
		}
	}
	mode := opts.Mode
	if mode == "" {
		mode = ResolveModeValue
	}
	defaultProvider := strings.TrimSpace(opts.DefaultProvider)
	if defaultProvider == "" {
		defaultProvider = strings.TrimSpace(cfg.DefaultProvider)
	}
	return &Resolver{
		providers:       providers,
		defaultProvider: defaultProvider,
		mode:            mode,
		mask:            strings.TrimSpace(opts.Mask),
		cache:           map[string]string{},
		seen:            map[string]struct{}{},
	}, nil
}

// ResolveValues walks arbitrary values and replaces secret references in place.
func (r *Resolver) ResolveValues(ctx context.Context, values interface{}) error {
	if r == nil {
		return nil
	}
	r.audit = nil
	r.seen = map[string]struct{}{}
	_, err := r.resolveValue(ctx, values)
	return err
}

// Audit returns a sorted copy of the audit report.
func (r *Resolver) Audit() AuditReport {
	if r == nil {
		return AuditReport{}
	}
	entries := append([]AuditEntry(nil), r.audit...)
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Provider != entries[j].Provider {
			return entries[i].Provider < entries[j].Provider
		}
		return entries[i].Path < entries[j].Path
	})
	return AuditReport{Entries: entries}
}

// Provider returns a provider by name.
func (r *Resolver) Provider(name string) (Provider, bool) {
	if r == nil {
		return nil, false
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, false
	}
	provider, ok := r.providers[name]
	return provider, ok
}

// ProviderNames lists configured provider names.
func (r *Resolver) ProviderNames() []string {
	if r == nil {
		return nil
	}
	names := make([]string, 0, len(r.providers))
	for name := range r.providers {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// DefaultProvider returns the resolver's default provider name.
func (r *Resolver) DefaultProvider() string {
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.defaultProvider)
}

func (r *Resolver) resolveValue(ctx context.Context, value interface{}) (interface{}, error) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for k, v := range typed {
			resolved, err := r.resolveValue(ctx, v)
			if err != nil {
				return nil, err
			}
			typed[k] = resolved
		}
		return typed, nil
	case map[interface{}]interface{}:
		for k, v := range typed {
			resolved, err := r.resolveValue(ctx, v)
			if err != nil {
				return nil, err
			}
			typed[k] = resolved
		}
		return typed, nil
	case []interface{}:
		for i, v := range typed {
			resolved, err := r.resolveValue(ctx, v)
			if err != nil {
				return nil, err
			}
			typed[i] = resolved
		}
		return typed, nil
	case string:
		resolved, replaced, err := r.ResolveString(ctx, typed)
		if err != nil {
			return nil, err
		}
		if replaced {
			return resolved, nil
		}
		return typed, nil
	default:
		return value, nil
	}
}

// ResolveString resolves a single value if it is a secret reference.
func (r *Resolver) ResolveString(ctx context.Context, value string) (string, bool, error) {
	defaultProvider := ""
	if r != nil {
		defaultProvider = r.defaultProvider
	}
	ref, ok, err := ParseRef(value, defaultProvider)
	if !ok {
		return value, false, err
	}
	if err != nil {
		return "", false, err
	}
	if r == nil {
		return "", false, fmt.Errorf("secret resolver is not configured")
	}
	val, err := r.resolveRef(ctx, ref)
	if err != nil {
		return "", false, err
	}
	if r.mode == ResolveModeMask {
		return r.maskFor(ref), true, nil
	}
	return val, true, nil
}

func (r *Resolver) resolveRef(ctx context.Context, ref Ref) (string, error) {
	key := ref.Provider + "|" + ref.Path
	if cached, ok := r.cache[key]; ok {
		r.record(ref)
		return cached, nil
	}
	provider := r.providers[ref.Provider]
	if provider == nil {
		return "", fmt.Errorf("secret provider %q is not configured", ref.Provider)
	}
	val, err := provider.Resolve(ctx, ref.Path)
	if err != nil {
		return "", err
	}
	r.cache[key] = val
	r.record(ref)
	return val, nil
}

func (r *Resolver) record(ref Ref) {
	key := ref.Provider + "|" + ref.Path
	if _, ok := r.seen[key]; ok {
		return
	}
	r.seen[key] = struct{}{}
	r.audit = append(r.audit, AuditEntry{
		Provider:  ref.Provider,
		Path:      ref.Path,
		Reference: ref.Reference(),
		Masked:    r.mode == ResolveModeMask,
	})
}

func (r *Resolver) maskFor(ref Ref) string {
	if r.mask != "" {
		return r.mask
	}
	return "[secret:" + ref.Provider + "/" + ref.Path + "]"
}

// Ref captures a parsed secret reference.
type Ref struct {
	Provider string
	Path     string
	Raw      string
}

// Reference returns the canonical secret reference string.
func (r Ref) Reference() string {
	if r.Provider == "" {
		return "secret:///" + r.Path
	}
	return "secret://" + r.Provider + "/" + r.Path
}

// ParseRef detects and parses secret:// references. Returns ok=false when value is not a reference.
func ParseRef(value string, defaultProvider string) (Ref, bool, error) {
	const prefix = "secret://"
	if !strings.HasPrefix(value, prefix) {
		return Ref{}, false, nil
	}
	rest := strings.TrimSpace(strings.TrimPrefix(value, prefix))
	if rest == "" {
		return Ref{}, true, fmt.Errorf("secret reference is missing provider/path")
	}
	if strings.HasPrefix(rest, "/") {
		rest = strings.TrimPrefix(rest, "/")
		if rest == "" {
			return Ref{}, true, fmt.Errorf("secret reference is missing path")
		}
		if strings.TrimSpace(defaultProvider) == "" {
			return Ref{}, true, fmt.Errorf("secret reference %q requires a default provider", value)
		}
		return Ref{Provider: strings.TrimSpace(defaultProvider), Path: rest, Raw: value}, true, nil
	}
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) == 1 {
		if strings.TrimSpace(defaultProvider) == "" {
			return Ref{}, true, fmt.Errorf("secret reference %q is missing provider", value)
		}
		path := strings.TrimSpace(parts[0])
		if path == "" {
			return Ref{}, true, fmt.Errorf("secret reference %q is missing path", value)
		}
		return Ref{Provider: strings.TrimSpace(defaultProvider), Path: path, Raw: value}, true, nil
	}
	provider := strings.TrimSpace(parts[0])
	path := strings.TrimSpace(parts[1])
	if provider == "" {
		if strings.TrimSpace(defaultProvider) == "" {
			return Ref{}, true, fmt.Errorf("secret reference %q is missing provider", value)
		}
		provider = strings.TrimSpace(defaultProvider)
	}
	if path == "" {
		return Ref{}, true, fmt.Errorf("secret reference %q is missing path", value)
	}
	return Ref{Provider: provider, Path: path, Raw: value}, true, nil
}

// FindRefs returns the raw secret references found in a values tree.
func FindRefs(values interface{}) []string {
	var refs []string
	scanRefs(values, &refs)
	return refs
}

func scanRefs(value interface{}, out *[]string) {
	switch typed := value.(type) {
	case map[string]interface{}:
		for _, v := range typed {
			scanRefs(v, out)
		}
	case map[interface{}]interface{}:
		for _, v := range typed {
			scanRefs(v, out)
		}
	case []interface{}:
		for _, v := range typed {
			scanRefs(v, out)
		}
	case string:
		if strings.HasPrefix(typed, "secret://") {
			*out = append(*out, typed)
		}
	}
}
