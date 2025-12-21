package secrets

import (
	"regexp"
	"strings"
)

var (
	privateKeyBlockRE = regexp.MustCompile(`-----BEGIN ([A-Z ]+ )?PRIVATE KEY-----[\s\S]+?-----END ([A-Z ]+ )?PRIVATE KEY-----`)
	jwtTokenRE        = jwtRE
	ghTokenAnyRE      = ghTokenRE
)

// RedactText removes obvious secret-like substrings from a line of text to reduce accidental leakage in logs.
func RedactText(line string) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	out := privateKeyBlockRE.ReplaceAllString(line, "-----BEGIN PRIVATE KEY-----…REDACTED…-----END PRIVATE KEY-----")
	out = jwtTokenRE.ReplaceAllStringFunc(out, func(m string) string { return Redact(m) })
	out = ghTokenAnyRE.ReplaceAllStringFunc(out, func(m string) string { return Redact(m) })
	return out
}
