package helpui

import "strings"

var commandTags = map[string][]string{
	"ktl init": {"onboarding", "setup"},
	"ktl help": {"onboarding"},
}

func tagsForCommand(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	if tags, ok := commandTags[path]; ok && len(tags) > 0 {
		out := make([]string, 0, len(tags))
		out = append(out, tags...)
		return out
	}
	return nil
}
