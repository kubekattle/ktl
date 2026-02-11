package helpui

import "strings"

var commandOwners = map[string][]string{
	"ktl apply":      {"internal/deploy", "internal/ui"},
	"ktl apply plan": {"internal/deploy", "internal/ui"},
	"ktl build":      {"internal/workflows/buildsvc"},
	"ktl delete":     {"internal/deploy", "internal/ui"},
	"ktl init":       {"internal/appconfig"},
	"ktl help":       {"internal/helpui"},
	"ktl logs":       {"internal/tailer"},
	"ktl secrets":    {"internal/secretstore"},
	"ktl stack":      {"internal/stack"},
}

func ownersForCommand(path string) []string {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}
	// Allow subcommands to inherit ownership from parent commands.
	for candidate := path; candidate != ""; {
		if owners, ok := commandOwners[candidate]; ok && len(owners) > 0 {
			out := make([]string, 0, len(owners))
			out = append(out, owners...)
			return out
		}
		idx := strings.LastIndex(candidate, " ")
		if idx < 0 {
			break
		}
		candidate = strings.TrimSpace(candidate[:idx])
	}
	return nil
}
