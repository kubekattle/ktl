package secrets

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

type CompiledRule struct {
	Rule
	re *regexp.Regexp
}

type CompiledRules struct {
	Rules []CompiledRule
}

func DefaultConfig() Config {
	enabled := true
	return Config{
		Version: "v1",
		Rules: []Rule{
			{
				ID:        "arg_name_suspicious",
				Enabled:   &enabled,
				Severity:  SeverityWarn,
				AppliesTo: []ApplyTo{ApplyBuildArgName},
				Regex:     `(?i)(token|secret|password|passwd|api[_-]?key|auth|private[_-]?key|github[_-]?token|npm[_-]?token|pypi[_-]?token|aws[_-]?secret|gcp[_-]?key|docker[_-]?password)|(_TOKEN|_SECRET|_PASSWORD)$`,
				Message:   "build-arg name looks like a secret; prefer BuildKit secrets instead of --build-arg",
				Suggest:   "Use `--secret <NAME>` and mount it via BuildKit instead of passing it as a build arg.",
			},
			{
				ID:        "arg_value_private_key",
				Enabled:   &enabled,
				Severity:  SeverityBlock,
				AppliesTo: []ApplyTo{ApplyBuildArgValue, ApplyOCIContent, ApplyLogLine},
				Regex:     `-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----`,
				Message:   "private key material detected",
			},
			{
				ID:        "arg_value_jwt",
				Enabled:   &enabled,
				Severity:  SeverityWarn,
				AppliesTo: []ApplyTo{ApplyBuildArgValue, ApplyOCIContent, ApplyLogLine},
				Regex:     `\\beyJ[a-zA-Z0-9_-]{10,}\\.[a-zA-Z0-9_-]{10,}\\.[a-zA-Z0-9_-]{10,}\\b`,
				Message:   "JWT-like token detected",
			},
			{
				ID:        "arg_value_github_token",
				Enabled:   &enabled,
				Severity:  SeverityWarn,
				AppliesTo: []ApplyTo{ApplyBuildArgValue, ApplyOCIContent, ApplyLogLine},
				Regex:     `\\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\\b`,
				Message:   "GitHub token-like string detected",
			},
			{
				ID:        "oci_suspect_cred_file",
				Enabled:   &enabled,
				Severity:  SeverityWarn,
				AppliesTo: []ApplyTo{ApplyOCIPath},
				Regex:     `(?i)(^|/)(\\.npmrc|\\.pypirc|\\.netrc|pip\\.conf)$|(?i)(^|/)config\\.json$`,
				Message:   "possible credential/config file in image layer",
			},
		},
	}
}

func MergeConfig(base Config, override Config) Config {
	if override.Version != "" {
		base.Version = override.Version
	}
	byID := map[string]Rule{}
	for _, r := range base.Rules {
		if strings.TrimSpace(r.ID) == "" {
			continue
		}
		byID[r.ID] = r
	}
	for _, r := range override.Rules {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		byID[id] = r
	}
	ids := make([]string, 0, len(byID))
	for id := range byID {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := Config{Version: base.Version, Rules: make([]Rule, 0, len(ids))}
	for _, id := range ids {
		out.Rules = append(out.Rules, byID[id])
	}
	return out
}

func CompileConfig(cfg Config) (CompiledRules, error) {
	out := CompiledRules{}
	for _, r := range cfg.Rules {
		id := strings.TrimSpace(r.ID)
		if id == "" {
			continue
		}
		enabled := true
		if r.Enabled != nil {
			enabled = *r.Enabled
		}
		if !enabled {
			continue
		}
		r.ID = id
		pat := strings.TrimSpace(r.Regex)
		var re *regexp.Regexp
		var err error
		if pat != "" {
			re, err = regexp.Compile(pat)
			if err != nil {
				return CompiledRules{}, fmt.Errorf("rule %s: invalid regex: %w", r.ID, err)
			}
		}
		out.Rules = append(out.Rules, CompiledRule{Rule: r, re: re})
	}
	sort.Slice(out.Rules, func(i, j int) bool { return out.Rules[i].ID < out.Rules[j].ID })
	return out, nil
}

func (cr CompiledRule) Applies(target ApplyTo) bool {
	if len(cr.AppliesTo) == 0 {
		return false
	}
	for _, t := range cr.AppliesTo {
		if t == target {
			return true
		}
	}
	return false
}
