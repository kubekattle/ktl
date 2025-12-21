package secrets

import (
	"regexp"
	"strings"
)

var (
	jwtRE     = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\b`)
	ghTokenRE = regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`)
)

func DetectBuildArgs(buildArgs []string) []Finding {
	rules, _ := CompileConfig(DefaultConfig())
	return DetectBuildArgsWithRules(buildArgs, rules)
}

func DetectBuildArgsWithRules(buildArgs []string, rules CompiledRules) []Finding {
	var findings []Finding
	for _, raw := range buildArgs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		k, v, ok := strings.Cut(raw, "=")
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if !ok || k == "" {
			continue
		}
		for _, r := range rules.Rules {
			switch {
			case r.Applies(ApplyBuildArgName) && r.re != nil && r.re.MatchString(k):
				findings = append(findings, Finding{
					Severity: r.Severity,
					Source:   SourceBuildArg,
					Rule:     r.ID,
					Message:  firstNonEmpty(r.Message, "build-arg name matched secrets rule"),
					Key:      k,
				})
			case r.Applies(ApplyBuildArgValue) && r.re != nil && r.re.MatchString(v):
				match := r.re.FindString(v)
				findings = append(findings, Finding{
					Severity: r.Severity,
					Source:   SourceBuildArg,
					Rule:     r.ID,
					Message:  firstNonEmpty(r.Message, "build-arg value matched secrets rule"),
					Key:      k,
					Match:    Redact(match),
				})
			}
		}
	}
	return findings
}

func Redact(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if len(v) <= 8 {
		return "REDACTED"
	}
	return v[:3] + "…" + v[len(v)-3:]
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return ""
}
