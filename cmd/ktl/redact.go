// File: cmd/ktl/redact.go
// Brief: CLI command wiring and implementation for 'redact'.

// Package main provides the ktl CLI entrypoints.

package main

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type redactRule struct {
	Name    string
	Regex   *regexp.Regexp
	Replace string
}

type RedactionReport struct {
	Preset string          `json:"preset,omitempty"`
	Rules  []RuleRedaction `json:"rules,omitempty"`
}

type RuleRedaction struct {
	Name    string `json:"name"`
	Matches int    `json:"matches"`
}

type redactor struct {
	preset string
	rules  []redactRule
	counts map[string]int
}

func newRedactor(preset string, extraPatterns []string) (*redactor, error) {
	preset = strings.TrimSpace(strings.ToLower(preset))
	rules := []redactRule{}

	switch preset {
	case "":
		// no preset
	case "incident":
		// Conservative: redact common secret-ish tokens but avoid nuking arbitrary identifiers.
		rules = append(rules,
			mustRule("JWT", `\beyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\b`, "<redacted:jwt>"),
			mustRule("Bearer", `(?i)\bBearer\s+[A-Za-z0-9._-]{12,}\b`, "Bearer <redacted:token>"),
			mustRule("Basic", `(?i)\bBasic\s+[A-Za-z0-9+/=]{16,}\b`, "Basic <redacted:basic>"),
			mustRule("AWSAccessKey", `\bAKIA[0-9A-Z]{16}\b`, "<redacted:aws_access_key>"),
			mustRule("AWSSecret", `(?i)\baws_secret_access_key\s*[:=]\s*['\"]?[A-Za-z0-9/+=]{20,}['\"]?`, "aws_secret_access_key=<redacted:aws_secret>"),
			mustRule("PasswordKV", `(?i)\b(pass(word)?|pwd|secret|token|api[_-]?key)\s*[:=]\s*['\"]?[^\\s'\";]{6,}['\"]?`, "$1=<redacted>"),
			mustRule("PrivateKey", `(?s)-----BEGIN [A-Z ]*PRIVATE KEY-----.*?-----END [A-Z ]*PRIVATE KEY-----`, "<redacted:private_key>"),
		)
	default:
		return nil, fmt.Errorf("unknown --redact-preset %q (supported: incident)", preset)
	}

	for _, raw := range extraPatterns {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		re, err := regexp.Compile(raw)
		if err != nil {
			return nil, fmt.Errorf("compile --redact %q: %w", raw, err)
		}
		rules = append(rules, redactRule{Name: "custom:" + raw, Regex: re, Replace: "<redacted>"})
	}

	return &redactor{
		preset: preset,
		rules:  rules,
		counts: make(map[string]int, len(rules)),
	}, nil
}

func mustRule(name, pattern, replace string) redactRule {
	return redactRule{Name: name, Regex: regexp.MustCompile(pattern), Replace: replace}
}

func (r *redactor) enabled() bool {
	return r != nil && len(r.rules) > 0
}

func (r *redactor) apply(input string) string {
	if !r.enabled() || input == "" {
		return input
	}
	out := input
	for _, rule := range r.rules {
		locs := rule.Regex.FindAllStringIndex(out, -1)
		if len(locs) > 0 {
			r.counts[rule.Name] += len(locs)
			out = rule.Regex.ReplaceAllString(out, rule.Replace)
		}
	}
	return out
}

func (r *redactor) report() RedactionReport {
	if r == nil || len(r.counts) == 0 {
		return RedactionReport{}
	}
	names := make([]string, 0, len(r.counts))
	for name := range r.counts {
		names = append(names, name)
	}
	sort.Strings(names)
	out := RedactionReport{Preset: r.preset}
	for _, name := range names {
		out.Rules = append(out.Rules, RuleRedaction{Name: name, Matches: r.counts[name]})
	}
	return out
}
