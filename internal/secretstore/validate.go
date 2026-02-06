package secretstore

import (
	"context"
	"fmt"
	"sort"
	"strings"
)

// ValidationIssue captures a single secret reference validation issue.
type ValidationIssue struct {
	Reference   string
	Provider    string
	Path        string
	Message     string
	Suggestions []string
}

// ValidationError is returned when secret references fail validation.
type ValidationError struct {
	Issues []ValidationIssue
}

func (e *ValidationError) Error() string {
	if e == nil || len(e.Issues) == 0 {
		return "secret references failed validation"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "secret references failed validation (%d issue(s)):\n", len(e.Issues))
	for _, issue := range e.Issues {
		ref := strings.TrimSpace(issue.Reference)
		if ref == "" {
			ref = "secret://"
		}
		fmt.Fprintf(&b, "- %s: %s\n", ref, strings.TrimSpace(issue.Message))
		for _, hint := range issue.Suggestions {
			hint = strings.TrimSpace(hint)
			if hint == "" {
				continue
			}
			fmt.Fprintf(&b, "  hint: %s\n", hint)
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// ValidationOptions configure secret reference validation.
type ValidationOptions struct {
	MaxIssues      int
	MaxSuggestions int
}

func (o ValidationOptions) withDefaults() ValidationOptions {
	if o.MaxIssues <= 0 {
		o.MaxIssues = 12
	}
	if o.MaxSuggestions <= 0 {
		o.MaxSuggestions = 6
	}
	return o
}

// ValidateRefs validates secret references in a values tree and returns a ValidationError on failure.
func ValidateRefs(ctx context.Context, resolver *Resolver, values interface{}, opts ValidationOptions) error {
	if resolver == nil {
		return nil
	}
	opts = opts.withDefaults()
	refs := uniqueRefStrings(FindRefs(values))
	if len(refs) == 0 {
		return nil
	}

	defaultProvider := resolver.DefaultProvider()
	providerNames := resolver.ProviderNames()
	providerSet := map[string]struct{}{}
	for _, name := range providerNames {
		providerSet[name] = struct{}{}
	}

	var issues []ValidationIssue
	for _, raw := range refs {
		ref, ok, err := ParseRef(raw, defaultProvider)
		if !ok {
			continue
		}
		if err != nil {
			issue := ValidationIssue{
				Reference: raw,
				Message:   err.Error(),
			}
			issue.Suggestions = append(issue.Suggestions, providerHints(defaultProvider, providerNames)...)
			issues = append(issues, issue)
			if len(issues) >= opts.MaxIssues {
				break
			}
			continue
		}
		if _, ok := providerSet[ref.Provider]; !ok {
			issue := ValidationIssue{
				Reference: raw,
				Provider:  ref.Provider,
				Path:      ref.Path,
				Message:   fmt.Sprintf("secret provider %q is not configured", ref.Provider),
			}
			issue.Suggestions = append(issue.Suggestions, providerHints(defaultProvider, providerNames)...)
			issues = append(issues, issue)
			if len(issues) >= opts.MaxIssues {
				break
			}
			continue
		}
		provider, _ := resolver.Provider(ref.Provider)
		if issue := validateRefWithProvider(ctx, provider, ref, opts); issue != nil {
			issues = append(issues, *issue)
			if len(issues) >= opts.MaxIssues {
				break
			}
		}
	}
	if len(issues) == 0 {
		return nil
	}
	return &ValidationError{Issues: issues}
}

func providerHints(defaultProvider string, providers []string) []string {
	var hints []string
	if len(providers) > 0 {
		hints = append(hints, fmt.Sprintf("configured providers: %s", strings.Join(providers, ", ")))
	}
	if strings.TrimSpace(defaultProvider) == "" {
		hints = append(hints, "set secrets.defaultProvider in .ktl.yaml or pass --secret-provider")
	}
	return hints
}

func validateRefWithProvider(ctx context.Context, provider Provider, ref Ref, opts ValidationOptions) *ValidationIssue {
	switch typed := provider.(type) {
	case *fileProvider:
		return validateFileRef(typed, ref, opts)
	case *vaultProvider:
		return validateVaultRef(ctx, typed, ref, opts)
	default:
		if lister, ok := provider.(Lister); ok {
			return validateWithLister(ctx, lister, ref, opts)
		}
	}
	return nil
}

func validateFileRef(p *fileProvider, ref Ref, opts ValidationOptions) *ValidationIssue {
	path, key := splitRefPath(ref.Path)
	if path == "" {
		return &ValidationIssue{
			Reference: ref.Reference(),
			Provider:  ref.Provider,
			Path:      ref.Path,
			Message:   "secret path is required",
			Suggestions: []string{
				fmt.Sprintf("check file provider path: %s", p.path),
			},
		}
	}
	parts := strings.Split(strings.TrimPrefix(path, "/"), "/")
	current := interface{}(p.data)
	for i, part := range parts {
		if part == "" {
			continue
		}
		isLast := i == len(parts)-1
		switch typed := current.(type) {
		case map[string]interface{}:
			val, ok := typed[part]
			if !ok {
				return fileMissingIssue(ref, part, p.path, sortedKeys(typed), opts)
			}
			if isLast {
				return validateFileLeaf(ref, val, key, p.path, opts)
			}
			current = val
		case map[interface{}]interface{}:
			val, ok := typed[part]
			if !ok {
				return fileMissingIssue(ref, part, p.path, sortedKeysFromInterfaceMap(typed), opts)
			}
			if isLast {
				return validateFileLeaf(ref, val, key, p.path, opts)
			}
			current = val
		default:
			return &ValidationIssue{
				Reference: ref.Reference(),
				Provider:  ref.Provider,
				Path:      ref.Path,
				Message:   fmt.Sprintf("secret path %q does not resolve to a map in %s", ref.Path, p.path),
				Suggestions: limitSuggestions([]string{
					fmt.Sprintf("check file provider path: %s", p.path),
				}, opts.MaxSuggestions),
			}
		}
	}
	return nil
}

func validateFileLeaf(ref Ref, val interface{}, key string, filePath string, opts ValidationOptions) *ValidationIssue {
	if key != "" {
		switch typed := val.(type) {
		case map[string]interface{}:
			if _, ok := typed[key]; !ok {
				return fileKeyMissingIssue(ref, key, filePath, sortedKeys(typed), opts)
			}
			return nil
		case map[interface{}]interface{}:
			if _, ok := typed[key]; !ok {
				return fileKeyMissingIssue(ref, key, filePath, sortedKeysFromInterfaceMap(typed), opts)
			}
			return nil
		default:
			return &ValidationIssue{
				Reference: ref.Reference(),
				Provider:  ref.Provider,
				Path:      ref.Path,
				Message:   fmt.Sprintf("secret path %q does not resolve to a map in %s", ref.Path, filePath),
				Suggestions: limitSuggestions([]string{
					fmt.Sprintf("check file provider path: %s", filePath),
					"file provider does not support #key; use /key in the path instead",
				}, opts.MaxSuggestions),
			}
		}
	}
	switch val.(type) {
	case string, []byte:
		return nil
	case map[string]interface{}:
		return &ValidationIssue{
			Reference: ref.Reference(),
			Provider:  ref.Provider,
			Path:      ref.Path,
			Message:   fmt.Sprintf("secret path %q resolves to a map in %s (expected a string)", ref.Path, filePath),
			Suggestions: limitSuggestions([]string{
				"use /<key> to select a value",
			}, opts.MaxSuggestions),
		}
	case map[interface{}]interface{}:
		return &ValidationIssue{
			Reference: ref.Reference(),
			Provider:  ref.Provider,
			Path:      ref.Path,
			Message:   fmt.Sprintf("secret path %q resolves to a map in %s (expected a string)", ref.Path, filePath),
			Suggestions: limitSuggestions([]string{
				"use /<key> to select a value",
			}, opts.MaxSuggestions),
		}
	default:
		return &ValidationIssue{
			Reference: ref.Reference(),
			Provider:  ref.Provider,
			Path:      ref.Path,
			Message:   fmt.Sprintf("secret path %q resolves to a non-string value in %s", ref.Path, filePath),
			Suggestions: limitSuggestions([]string{
				fmt.Sprintf("check file provider path: %s", filePath),
			}, opts.MaxSuggestions),
		}
	}
}

func fileMissingIssue(ref Ref, part string, filePath string, keys []string, opts ValidationOptions) *ValidationIssue {
	hints := []string{fmt.Sprintf("check file provider path: %s", filePath)}
	if len(keys) > 0 {
		hints = append(hints, fmt.Sprintf("available keys: %s", strings.Join(keys, ", ")))
	}
	hints = limitSuggestions(hints, opts.MaxSuggestions)
	return &ValidationIssue{
		Reference:   ref.Reference(),
		Provider:    ref.Provider,
		Path:        ref.Path,
		Message:     fmt.Sprintf("secret path %q not found in %s (missing %q)", ref.Path, filePath, part),
		Suggestions: hints,
	}
}

func fileKeyMissingIssue(ref Ref, key string, filePath string, keys []string, opts ValidationOptions) *ValidationIssue {
	hints := []string{fmt.Sprintf("check file provider path: %s", filePath)}
	if len(keys) > 0 {
		hints = append(hints, fmt.Sprintf("available keys: %s", strings.Join(keys, ", ")))
	}
	hints = limitSuggestions(hints, opts.MaxSuggestions)
	return &ValidationIssue{
		Reference:   ref.Reference(),
		Provider:    ref.Provider,
		Path:        ref.Path,
		Message:     fmt.Sprintf("secret key %q not found in %s", key, filePath),
		Suggestions: hints,
	}
}

func validateVaultRef(ctx context.Context, p *vaultProvider, ref Ref, opts ValidationOptions) *ValidationIssue {
	path, key := splitVaultPath(ref.Path)
	if path == "" {
		return &ValidationIssue{
			Reference: ref.Reference(),
			Provider:  ref.Provider,
			Path:      ref.Path,
			Message:   "vault secret path is required",
		}
	}
	data, err := p.read(ctx, path)
	if err != nil {
		hints := []string{}
		if parent, _ := parentPath(path); parent != "" {
			hints = append(hints, fmt.Sprintf("list vault path: ktl secrets list --secret-provider %s --path %s", ref.Provider, parent))
		} else {
			hints = append(hints, fmt.Sprintf("list vault path: ktl secrets list --secret-provider %s --path /", ref.Provider))
		}
		return &ValidationIssue{
			Reference:   ref.Reference(),
			Provider:    ref.Provider,
			Path:        ref.Path,
			Message:     err.Error(),
			Suggestions: limitSuggestions(hints, opts.MaxSuggestions),
		}
	}
	if key == "" {
		if isAmbiguousVaultValue(data) {
			keys := sortedMapKeys(data)
			hints := []string{"specify a key with #<key> in the secret reference"}
			if len(keys) > 0 {
				hints = append(hints, fmt.Sprintf("available keys: %s", strings.Join(keys, ", ")))
			}
			return &ValidationIssue{
				Reference:   ref.Reference(),
				Provider:    ref.Provider,
				Path:        ref.Path,
				Message:     "vault secret value is ambiguous (multiple keys present)",
				Suggestions: limitSuggestions(hints, opts.MaxSuggestions),
			}
		}
		return nil
	}
	if _, ok := data[key]; !ok {
		keys := sortedMapKeys(data)
		hints := []string{}
		if len(keys) > 0 {
			hints = append(hints, fmt.Sprintf("available keys: %s", strings.Join(keys, ", ")))
		}
		return &ValidationIssue{
			Reference:   ref.Reference(),
			Provider:    ref.Provider,
			Path:        ref.Path,
			Message:     fmt.Sprintf("vault secret key %q not found", key),
			Suggestions: limitSuggestions(hints, opts.MaxSuggestions),
		}
	}
	return nil
}

func validateWithLister(ctx context.Context, lister Lister, ref Ref, opts ValidationOptions) *ValidationIssue {
	path := strings.Trim(ref.Path, "/")
	if path == "" {
		return nil
	}
	parent, leaf := parentPath(path)
	keys, err := lister.List(ctx, parent)
	if err != nil || len(keys) == 0 {
		return nil
	}
	for _, key := range keys {
		if key == leaf {
			return nil
		}
	}
	hints := []string{fmt.Sprintf("available keys: %s", strings.Join(keys, ", "))}
	return &ValidationIssue{
		Reference:   ref.Reference(),
		Provider:    ref.Provider,
		Path:        ref.Path,
		Message:     fmt.Sprintf("secret path %q not found", ref.Path),
		Suggestions: limitSuggestions(hints, opts.MaxSuggestions),
	}
}

func splitRefPath(path string) (string, string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return "", ""
	}
	parts := strings.SplitN(path, "#", 2)
	base := strings.Trim(strings.TrimSpace(parts[0]), "/")
	key := ""
	if len(parts) > 1 {
		key = strings.TrimSpace(parts[1])
	}
	return base, key
}

func sortedKeysFromInterfaceMap(data map[interface{}]interface{}) []string {
	if len(data) == 0 {
		return nil
	}
	keys := make([]string, 0, len(data))
	for key := range data {
		if s, ok := key.(string); ok {
			keys = append(keys, s)
		}
	}
	sort.Strings(keys)
	return keys
}

func sortedMapKeys(data map[string]interface{}) []string {
	return sortedKeys(data)
}

func parentPath(path string) (string, string) {
	path = strings.Trim(strings.TrimSpace(path), "/")
	if path == "" {
		return "", ""
	}
	idx := strings.LastIndex(path, "/")
	if idx == -1 {
		return "", path
	}
	return path[:idx], path[idx+1:]
}

func limitSuggestions(items []string, max int) []string {
	if max <= 0 || len(items) <= max {
		return items
	}
	return items[:max]
}

func isAmbiguousVaultValue(data map[string]interface{}) bool {
	if len(data) <= 1 {
		return false
	}
	if _, ok := data["value"]; ok {
		return false
	}
	return true
}

func uniqueRefStrings(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		if _, ok := seen[item]; ok {
			continue
		}
		seen[item] = struct{}{}
		out = append(out, item)
	}
	return out
}
