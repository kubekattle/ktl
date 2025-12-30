package helpui

import (
	"fmt"
	"sort"
	"strings"
)

type internalModule struct {
	Path        string
	Description string
	CommandIDs  []string
}

func internalsIndexDoc() (string, []Link) {
	modules := []internalModule{
		{
			Path:        "internal/deploy",
			Description: "Helm apply/delete pipeline, phase streaming, rollout readiness tracking.",
			CommandIDs:  []string{"cmd:ktl apply", "cmd:ktl apply plan", "cmd:ktl delete"},
		},
		{
			Path:        "internal/stack",
			Description: "Stack model + selection logic (e.g. ktl stack).",
			CommandIDs:  []string{"cmd:ktl stack"},
		},
		{
			Path:        "internal/workflows/buildsvc",
			Description: "BuildKit-backed build workflows and streaming build service.",
			CommandIDs:  []string{"cmd:ktl build"},
		},
		{
			Path:        "internal/tailer",
			Description: "Fast pod log tailer with filtering.",
			CommandIDs:  []string{"cmd:ktl logs"},
		},
		{
			Path:        "internal/helpui",
			Description: "Searchable help index + web UI (ktl help --ui).",
			CommandIDs:  []string{"cmd:ktl help"},
		},
		{
			Path:        "internal/featureflags",
			Description: "Feature flag registry and resolution helpers.",
			CommandIDs:  []string{"doc:feature-flags"},
		},
		{
			Path:        "docs",
			Description: "Architecture notes and generated dependency map.",
			CommandIDs:  []string{"doc:architecture", "doc:deps"},
		},
	}

	var links []Link
	seen := make(map[string]bool, 32)
	addLink := func(id, title string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		links = append(links, Link{ID: id, Title: title})
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].Path < modules[j].Path })

	var b strings.Builder
	b.WriteString("KTL internals index\n\n")
	b.WriteString("This page maps high-level areas of the codebase to the CLI surfaces they own.\n\n")
	for _, m := range modules {
		if strings.TrimSpace(m.Path) == "" {
			continue
		}
		fmt.Fprintf(&b, "## %s\n", m.Path)
		if desc := strings.TrimSpace(m.Description); desc != "" {
			fmt.Fprintf(&b, "%s\n", desc)
		}
		if len(m.CommandIDs) > 0 {
			b.WriteString("\nRelated:\n")
			for _, id := range m.CommandIDs {
				id = strings.TrimSpace(id)
				if id == "" {
					continue
				}
				title := id
				if strings.HasPrefix(id, "cmd:") {
					title = strings.TrimPrefix(id, "cmd:")
				} else if strings.HasPrefix(id, "doc:") {
					title = strings.TrimPrefix(id, "doc:")
				}
				fmt.Fprintf(&b, "- %s (%s)\n", title, id)
				addLink(id, title)
			}
		}
		b.WriteString("\n")
	}

	sort.Slice(links, func(i, j int) bool { return links[i].Title < links[j].Title })
	return strings.TrimSpace(b.String()), links
}
