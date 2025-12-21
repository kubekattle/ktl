package buildsvc

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

var (
	labelLineRE = regexp.MustCompile(`(?i)^\s*label\s+(.+)$`)
)

type dockerfileMeta struct {
	Bases  []string
	Labels map[string]string
}

func readDockerfileMeta(path string) (dockerfileMeta, error) {
	out := dockerfileMeta{Labels: map[string]string{}}
	f, err := os.Open(path)
	if err != nil {
		return out, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "#"); idx >= 0 {
			line = strings.TrimSpace(line[:idx])
		}
		if img, ok := parseFromImageRef(line); ok {
			out.Bases = append(out.Bases, img)
			continue
		}
		m := labelLineRE.FindStringSubmatch(line)
		if len(m) != 2 {
			continue
		}
		rest := strings.TrimSpace(m[1])
		if rest == "" {
			continue
		}
		// Minimal parser: handle `LABEL k=v` and `LABEL k="v"`.
		fields := strings.Fields(rest)
		for _, field := range fields {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			k = strings.TrimSpace(k)
			v = strings.Trim(strings.TrimSpace(v), `"'`)
			if k == "" {
				continue
			}
			out.Labels[k] = v
		}
	}
	if err := sc.Err(); err != nil {
		return out, err
	}
	return out, nil
}
