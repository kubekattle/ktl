package secrets

import (
	"regexp"
	"strings"
)

var (
	suspiciousKeyRE = regexp.MustCompile(`(?i)(token|secret|password|passwd|api[_-]?key|auth|private[_-]?key|github[_-]?token|npm[_-]?token|pypi[_-]?token|aws[_-]?secret|gcp[_-]?key|docker[_-]?password)`)
	jwtRE           = regexp.MustCompile(`\beyJ[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\.[a-zA-Z0-9_-]{10,}\b`)
	privateKeyRE    = regexp.MustCompile(`-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----`)
	ghTokenRE       = regexp.MustCompile(`\b(ghp|gho|ghu|ghs|ghr)_[A-Za-z0-9]{20,}\b`)
	awsAccessKeyRE  = regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)
)

func DetectBuildArgs(buildArgs []string) []Finding {
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
		upper := strings.ToUpper(k)
		if suspiciousKeyRE.MatchString(k) || strings.HasSuffix(upper, "_TOKEN") || strings.HasSuffix(upper, "_SECRET") || strings.HasSuffix(upper, "_PASSWORD") {
			if strings.HasPrefix(v, "$") || strings.Contains(v, "${") {
				findings = append(findings, Finding{
					Severity: SeverityWarn,
					Source:   SourceBuildArg,
					Rule:     "ARG_ENV_SECRET_NAME",
					Message:  "build-arg name looks like a secret and value comes from environment; prefer BuildKit secrets instead of --build-arg",
					Key:      k,
				})
				continue
			}
			if looksSecretLike(v) {
				findings = append(findings, Finding{
					Severity: SeverityBlock,
					Source:   SourceBuildArg,
					Rule:     "ARG_VALUE_LOOKS_SECRET",
					Message:  "build-arg value looks like a secret; use --secret and mount it in BuildKit instead of --build-arg",
					Key:      k,
					Match:    Redact(v),
				})
				continue
			}
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceBuildArg,
				Rule:     "ARG_NAME_SUSPICIOUS",
				Message:  "build-arg name looks like a secret; consider using --secret instead",
				Key:      k,
			})
			continue
		}
		if looksSecretLike(v) {
			findings = append(findings, Finding{
				Severity: SeverityWarn,
				Source:   SourceBuildArg,
				Rule:     "ARG_VALUE_SUSPECT",
				Message:  "build-arg value looks secret-like; consider using --secret instead",
				Key:      k,
				Match:    Redact(v),
			})
		}
	}
	return findings
}

func looksSecretLike(v string) bool {
	v = strings.TrimSpace(v)
	if v == "" {
		return false
	}
	if privateKeyRE.MatchString(v) {
		return true
	}
	if jwtRE.MatchString(v) {
		return true
	}
	if ghTokenRE.MatchString(v) {
		return true
	}
	if awsAccessKeyRE.MatchString(v) {
		return true
	}
	if len(v) >= 32 && strings.Count(v, ".") == 0 && strings.Count(v, "-") < 4 {
		hasLetter := false
		hasDigit := false
		for _, r := range v {
			if r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' {
				hasLetter = true
			} else if r >= '0' && r <= '9' {
				hasDigit = true
			} else if r == '_' || r == '-' || r == '=' || r == '/' || r == '+' {
				continue
			} else {
				return false
			}
		}
		return hasLetter && hasDigit
	}
	return false
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
