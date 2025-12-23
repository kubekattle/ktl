package helpui

import (
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/envcatalog"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Index struct {
	GeneratedAt string  `json:"generatedAt"`
	Entries     []Entry `json:"entries"`
}

type Entry struct {
	ID       string   `json:"id"`
	Kind     string   `json:"kind"` // command|flag|env
	Title    string   `json:"title"`
	Subtitle string   `json:"subtitle,omitempty"`
	Content  string   `json:"content,omitempty"`
	Examples []string `json:"examples,omitempty"`
	Tags     []string `json:"tags,omitempty"`
}

func BuildIndex(root *cobra.Command, includeHidden bool) Index {
	now := time.Now().UTC().Format(time.RFC3339)
	entries := make([]Entry, 0, 256)
	seenFlags := make(map[string]struct{})

	visitCommands(root, includeHidden, func(cmd *cobra.Command) {
		if cmd == nil {
			return
		}
		path := strings.TrimSpace(cmd.CommandPath())
		if path == "" {
			path = cmd.Name()
		}
		desc := firstNonEmpty(strings.TrimSpace(cmd.Long), strings.TrimSpace(cmd.Short))
		var contentParts []string
		if desc != "" {
			contentParts = append(contentParts, desc)
		}
		if useLine := strings.TrimSpace(cmd.UseLine()); useLine != "" {
			contentParts = append(contentParts, "Usage:\n  "+useLine)
		}
		if flags := flagUsages(cmd.LocalFlags()); flags != "" {
			contentParts = append(contentParts, "Flags:\n"+flags)
		}
		entries = append(entries, Entry{
			ID:       "cmd:" + path,
			Kind:     "command",
			Title:    path,
			Subtitle: strings.TrimSpace(cmd.Short),
			Content:  strings.Join(contentParts, "\n\n"),
			Examples: splitExamples(cmd.Example),
			Tags:     []string{"command"},
		})

		addFlagEntries := func(fs *pflag.FlagSet, scope string) {
			if fs == nil {
				return
			}
			fs.VisitAll(func(f *pflag.Flag) {
				if f == nil {
					return
				}
				if f.Hidden && !includeHidden {
					return
				}
				key := path + "\x00" + f.Name + "\x00" + scope
				if _, ok := seenFlags[key]; ok {
					return
				}
				seenFlags[key] = struct{}{}
				title := "--" + f.Name
				if f.Shorthand != "" {
					title = "-" + f.Shorthand + ", " + title
				}
				content := strings.TrimSpace(f.Usage)
				if def := strings.TrimSpace(f.DefValue); def != "" && def != "false" && def != "0" {
					content = strings.TrimSpace(content + "\n\nDefault: " + def)
				}
				entries = append(entries, Entry{
					ID:       "flag:" + path + ":" + scope + ":" + f.Name,
					Kind:     "flag",
					Title:    title,
					Subtitle: path + " (" + scope + ")",
					Content:  content,
					Tags:     []string{"flag", path},
				})
			})
		}
		addFlagEntries(cmd.LocalFlags(), "command")
		addFlagEntries(cmd.InheritedFlags(), "global")
	})

	for _, env := range envcatalog.Catalog() {
		if env.Internal && !includeHidden {
			continue
		}
		entries = append(entries, Entry{
			ID:       "env:" + env.Name,
			Kind:     "env",
			Title:    env.Name,
			Subtitle: env.Category,
			Content:  env.Description,
			Tags:     []string{"env", env.Category},
		})
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return entries[i].Kind < entries[j].Kind
		}
		return entries[i].Title < entries[j].Title
	})

	return Index{
		GeneratedAt: now,
		Entries:     entries,
	}
}

func visitCommands(root *cobra.Command, includeHidden bool, fn func(*cobra.Command)) {
	if root == nil {
		return
	}
	queue := []*cobra.Command{root}
	for len(queue) > 0 {
		cmd := queue[0]
		queue = queue[1:]
		if cmd == nil {
			continue
		}
		if cmd.Hidden && !includeHidden {
			continue
		}
		fn(cmd)
		for _, child := range cmd.Commands() {
			if child == nil {
				continue
			}
			if child.Name() == "help" {
				continue
			}
			queue = append(queue, child)
		}
	}
}

func splitExamples(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	lines := strings.Split(raw, "\n")
	var blocks []string
	var buf []string
	flush := func() {
		if len(buf) == 0 {
			return
		}
		block := strings.TrimSpace(strings.Join(buf, "\n"))
		if block != "" {
			blocks = append(blocks, block)
		}
		buf = buf[:0]
	}
	for _, line := range lines {
		if strings.TrimSpace(line) == "" {
			flush()
			continue
		}
		buf = append(buf, strings.TrimRight(line, " \t"))
	}
	flush()
	return blocks
}

func firstNonEmpty(values ...string) string {
	for _, val := range values {
		val = strings.TrimSpace(val)
		if val != "" {
			return val
		}
	}
	return ""
}

func flagUsages(fs *pflag.FlagSet) string {
	if fs == nil || !fs.HasAvailableFlags() {
		return ""
	}
	out := fs.FlagUsagesWrapped(92)
	out = strings.ReplaceAll(out, "\t", "  ")
	out = strings.TrimRight(out, "\n")
	return out
}
