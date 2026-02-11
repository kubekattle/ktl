package secrets

import "strings"

func MatchKeyValueWithRules(key, value string, rules CompiledRules, source Source, location string) []Finding {
	key = strings.TrimSpace(key)
	value = strings.TrimSpace(value)
	location = strings.TrimSpace(location)
	if key == "" && value == "" {
		return nil
	}
	var findings []Finding
	for _, r := range rules.Rules {
		switch {
		case key != "" && r.Applies(ApplyBuildArgName) && r.re != nil && r.re.MatchString(key):
			findings = append(findings, Finding{
				Severity: r.Severity,
				Source:   source,
				Rule:     r.ID,
				Message:  firstNonEmpty(r.Message, "key matched secrets rule"),
				Key:      key,
				Location: location,
			})
		case value != "" && r.Applies(ApplyBuildArgValue) && r.re != nil && r.re.MatchString(value):
			match := r.re.FindString(value)
			findings = append(findings, Finding{
				Severity: r.Severity,
				Source:   source,
				Rule:     r.ID,
				Message:  firstNonEmpty(r.Message, "value matched secrets rule"),
				Key:      key,
				Location: location,
				Match:    Redact(match),
			})
		}
	}
	return findings
}

func MatchTextWithRules(text string, rules CompiledRules, source Source, location string) []Finding {
	text = strings.TrimSpace(text)
	location = strings.TrimSpace(location)
	if text == "" {
		return nil
	}
	var findings []Finding
	for _, r := range rules.Rules {
		if r.re == nil {
			continue
		}
		if !(r.Applies(ApplyLogLine) || r.Applies(ApplyOCIContent)) {
			continue
		}
		if !r.re.MatchString(text) {
			continue
		}
		match := r.re.FindString(text)
		findings = append(findings, Finding{
			Severity: r.Severity,
			Source:   source,
			Rule:     r.ID,
			Message:  firstNonEmpty(r.Message, "text matched secrets rule"),
			Location: location,
			Match:    Redact(match),
		})
	}
	return findings
}
