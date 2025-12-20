package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

func parseCaptureTags(values []string) (map[string]string, error) {
	out := map[string]string{}
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v == "" {
			continue
		}
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid --capture-tag %q (want KEY=VALUE)", v)
		}
		k := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if k == "" || val == "" {
			return nil, fmt.Errorf("invalid --capture-tag %q (empty key/value)", v)
		}
		out[k] = val
	}
	if len(out) == 0 {
		return nil, nil
	}
	return out, nil
}

func captureJSON(v any) string {
	raw, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	return string(raw)
}
