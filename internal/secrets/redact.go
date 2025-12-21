package secrets

import "strings"

// RedactText removes secret-like substrings from a line of text using the default rules.
func RedactText(line string) string {
	rules, _ := CompileConfig(DefaultConfig())
	return RedactTextWithRules(line, rules)
}

// RedactTextWithRules removes secret-like substrings from a line of text using configured rules.
func RedactTextWithRules(line string, rules CompiledRules) string {
	line = strings.TrimSpace(line)
	if line == "" {
		return ""
	}
	out := line
	for _, r := range rules.Rules {
		if r.re == nil || !r.Applies(ApplyLogLine) {
			continue
		}
		out = r.re.ReplaceAllStringFunc(out, func(m string) string { return Redact(m) })
	}
	return out
}
