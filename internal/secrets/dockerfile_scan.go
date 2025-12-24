package secrets

import (
	"bufio"
	"errors"
	"os"
	"strings"
)

func ScanDockerfileForSecretsWithRules(path string, rules CompiledRules) ([]Finding, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("dockerfile path is required")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var findings []Finding
	s := bufio.NewScanner(f)
	lineno := 0
	for s.Scan() {
		lineno++
		raw := s.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		loc := path + ":" + itoa(lineno)

		// Always scan the full line for high-signal patterns (private keys, JWTs, tokens).
		findings = append(findings, MatchTextWithRules(line, rules, SourceDockerfile, loc)...)

		kw, rest, ok := strings.Cut(line, " ")
		if !ok {
			continue
		}
		switch strings.ToUpper(strings.TrimSpace(kw)) {
		case "ARG":
			name, value, ok := parseDockerfileKeyValue(strings.TrimSpace(rest))
			if !ok {
				continue
			}
			findings = append(findings, MatchKeyValueWithRules(name, value, rules, SourceDockerfile, loc)...)
		case "ENV":
			kvs := parseDockerfileEnv(strings.TrimSpace(rest))
			for _, kv := range kvs {
				findings = append(findings, MatchKeyValueWithRules(kv.key, kv.value, rules, SourceDockerfile, loc)...)
			}
		default:
			continue
		}
	}
	if err := s.Err(); err != nil {
		return findings, err
	}
	return findings, nil
}

type kv struct {
	key   string
	value string
}

func parseDockerfileKeyValue(rest string) (key, value string, ok bool) {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return "", "", false
	}
	// Ignore multiple ARGs on one line; treat the first token as the declaration.
	token := rest
	if head, _, hasSpace := strings.Cut(rest, " "); hasSpace {
		token = head
	}
	k, v, hasEq := strings.Cut(token, "=")
	k = strings.TrimSpace(k)
	if k == "" {
		return "", "", false
	}
	if !hasEq {
		return k, "", true
	}
	return k, strings.TrimSpace(v), true
}

func parseDockerfileEnv(rest string) []kv {
	rest = strings.TrimSpace(rest)
	if rest == "" {
		return nil
	}
	parts := strings.Fields(rest)
	if len(parts) == 0 {
		return nil
	}

	// ENV key=value key2=value2 ...
	hasEquals := false
	for _, p := range parts {
		if strings.Contains(p, "=") {
			hasEquals = true
			break
		}
	}
	if hasEquals {
		var out []kv
		for _, p := range parts {
			k, v, ok := strings.Cut(p, "=")
			k = strings.TrimSpace(k)
			if !ok || k == "" {
				continue
			}
			out = append(out, kv{key: k, value: strings.TrimSpace(v)})
		}
		return out
	}

	// ENV key value (value may contain spaces).
	if len(parts) >= 2 {
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(strings.Join(parts[1:], " "))
		if key == "" {
			return nil
		}
		return []kv{{key: key, value: value}}
	}
	return nil
}

func itoa(v int) string {
	// tiny, avoids fmt in a hot-ish scan path
	if v == 0 {
		return "0"
	}
	neg := v < 0
	if neg {
		v = -v
	}
	var buf [32]byte
	i := len(buf)
	for v > 0 {
		i--
		buf[i] = byte('0' + (v % 10))
		v /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
