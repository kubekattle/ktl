package verify

import (
	"fmt"
	"regexp"
	"strings"
)

type Selector struct {
	Kinds      []string          `yaml:"kinds,omitempty" json:"kinds,omitempty"`
	Namespaces []string          `yaml:"namespaces,omitempty" json:"namespaces,omitempty"`
	Labels     map[string]string `yaml:"labels,omitempty" json:"labels,omitempty"`
	Regex      []string          `yaml:"regex,omitempty" json:"regex,omitempty"`
}

type SelectorSet struct {
	Include Selector `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude Selector `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

type RuleSelector struct {
	Rule    string   `yaml:"rule,omitempty" json:"rule,omitempty"`
	Include Selector `yaml:"include,omitempty" json:"include,omitempty"`
	Exclude Selector `yaml:"exclude,omitempty" json:"exclude,omitempty"`
}

type compiledSelector struct {
	kinds      map[string]struct{}
	namespaces map[string]struct{}
	labels     map[string]string
	regex      []*regexp.Regexp
	empty      bool
}

type compiledRuleSelector struct {
	pattern *regexp.Regexp
	include compiledSelector
	exclude compiledSelector
}

func compileSelector(sel Selector) (compiledSelector, error) {
	out := compiledSelector{
		kinds:      map[string]struct{}{},
		namespaces: map[string]struct{}{},
		labels:     map[string]string{},
	}
	for _, kind := range sel.Kinds {
		if k := strings.TrimSpace(kind); k != "" {
			out.kinds[strings.ToLower(k)] = struct{}{}
		}
	}
	for _, ns := range sel.Namespaces {
		if n := strings.TrimSpace(ns); n != "" {
			out.namespaces[strings.ToLower(n)] = struct{}{}
		}
	}
	for k, v := range sel.Labels {
		key := strings.TrimSpace(k)
		if key == "" {
			continue
		}
		out.labels[key] = strings.TrimSpace(v)
	}
	for _, raw := range sel.Regex {
		pattern := strings.TrimSpace(raw)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return compiledSelector{}, fmt.Errorf("invalid selector regex %q: %w", pattern, err)
		}
		out.regex = append(out.regex, re)
	}
	out.empty = len(out.kinds) == 0 && len(out.namespaces) == 0 && len(out.labels) == 0 && len(out.regex) == 0
	return out, nil
}

func compileRuleSelectors(raw []RuleSelector) ([]compiledRuleSelector, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	out := make([]compiledRuleSelector, 0, len(raw))
	for _, sel := range raw {
		pattern := strings.TrimSpace(sel.Rule)
		if pattern == "" {
			continue
		}
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf("invalid rule selector pattern %q: %w", pattern, err)
		}
		includeSel, err := compileSelector(sel.Include)
		if err != nil {
			return nil, err
		}
		excludeSel, err := compileSelector(sel.Exclude)
		if err != nil {
			return nil, err
		}
		out = append(out, compiledRuleSelector{
			pattern: re,
			include: includeSel,
			exclude: excludeSel,
		})
	}
	return out, nil
}

func selectorMatches(sel compiledSelector, info objectInfo) bool {
	if sel.empty {
		return true
	}
	subject := info.subject
	if len(sel.kinds) > 0 {
		if _, ok := sel.kinds[strings.ToLower(strings.TrimSpace(subject.Kind))]; !ok {
			return false
		}
	}
	if len(sel.namespaces) > 0 {
		ns := strings.TrimSpace(subject.Namespace)
		if ns == "" {
			ns = "cluster"
		}
		if _, ok := sel.namespaces[strings.ToLower(ns)]; !ok {
			return false
		}
	}
	if len(sel.labels) > 0 {
		for key, want := range sel.labels {
			got, ok := info.labels[key]
			if !ok {
				return false
			}
			if want != "" && got != want {
				return false
			}
		}
	}
	if len(sel.regex) > 0 {
		target := resourceKey(subject)
		matched := false
		for _, re := range sel.regex {
			if re.MatchString(target) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return true
}

func resourceKey(subject Subject) string {
	kind := strings.TrimSpace(subject.Kind)
	name := strings.TrimSpace(subject.Name)
	ns := strings.TrimSpace(subject.Namespace)
	if kind == "" && name == "" && ns == "" {
		return ""
	}
	if ns == "" {
		ns = "cluster"
	}
	if kind == "" {
		return ns + "/" + name
	}
	if name == "" {
		return ns + "/" + kind
	}
	return ns + "/" + kind + "/" + name
}
