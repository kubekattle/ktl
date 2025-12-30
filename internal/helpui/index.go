package helpui

import (
	"fmt"
	"sort"
	"strings"
	"time"

	ktldocs "github.com/example/ktl/docs"
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
	globalFlagNames := collectFlagNames(rootPersistentFlags(root))
	flagAggs := make(map[string]*flagAgg, 256)

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
		examples := splitExamples(cmd.Example)
		if curated, ok := curatedExamples[path]; ok {
			examples = append(examples, curated...)
		}
		entries = append(entries, Entry{
			ID:       "cmd:" + path,
			Kind:     "command",
			Title:    path,
			Subtitle: strings.TrimSpace(cmd.Short),
			Content:  strings.Join(contentParts, "\n\n"),
			Examples: examples,
			Tags:     []string{"command"},
		})

		addLocalFlags := func(fs *pflag.FlagSet) {
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
				// Global flags should be indexed once (at the root), not repeated per subcommand.
				if _, ok := globalFlagNames[f.Name]; ok {
					return
				}
				agg := flagAggs[f.Name]
				if agg == nil {
					agg = &flagAgg{name: f.Name, shorthand: f.Shorthand, usage: strings.TrimSpace(f.Usage), defValue: strings.TrimSpace(f.DefValue)}
					flagAggs[f.Name] = agg
				}
				agg.addCommand(path)
			})
		}
		addLocalFlags(cmd.LocalFlags())
	})

	// Index global flags exactly once.
	rootFlags := rootPersistentFlags(root)
	if rootFlags != nil {
		rootFlags.VisitAll(func(f *pflag.Flag) {
			if f == nil {
				return
			}
			if f.Hidden && !includeHidden {
				return
			}
			agg := flagAggs[f.Name]
			if agg == nil {
				agg = &flagAgg{name: f.Name, shorthand: f.Shorthand, usage: strings.TrimSpace(f.Usage), defValue: strings.TrimSpace(f.DefValue)}
				flagAggs[f.Name] = agg
			}
			agg.global = true
		})
	}

	for _, agg := range flagAggs {
		if agg == nil {
			continue
		}
		title := "--" + agg.name
		if agg.shorthand != "" {
			title = "-" + agg.shorthand + ", " + title
		}
		content := strings.TrimSpace(agg.usage)
		if def := strings.TrimSpace(agg.defValue); def != "" && def != "false" && def != "0" {
			content = strings.TrimSpace(content + "\n\nDefault: " + def)
		}
		// Keep flag cards minimal: avoid long "available on" lists that duplicate command names in the UI.
		// When a flag is truly global (persistent), listing every command is redundant noise.
		if !agg.global {
			if n := len(agg.commands); n > 1 {
				content = strings.TrimSpace(content + "\n\nAvailable on: " + fmt.Sprintf("%d commands", n))
			}
		}
		entries = append(entries, Entry{
			ID:      "flag:" + agg.name,
			Kind:    "flag",
			Title:   title,
			Content: content,
			Tags:    []string{"flag"},
		})
	}

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

	if md := strings.TrimSpace(ktldocs.ArchitectureMD); md != "" {
		entries = append(entries, Entry{
			ID:       "doc:architecture",
			Kind:     "doc",
			Title:    "Architecture",
			Subtitle: "Repo layout and core packages",
			Content:  md,
			Tags:     []string{"doc", "internals", "architecture"},
		})
	}

	if md := strings.TrimSpace(ktldocs.DepsMD); md != "" {
		entries = append(entries, Entry{
			ID:       "doc:deps",
			Kind:     "doc",
			Title:    "Deps",
			Subtitle: "Package dependency map (generated)",
			Content:  md,
			Tags:     []string{"doc", "internals", "deps", "dependency"},
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

type flagAgg struct {
	name      string
	shorthand string
	usage     string
	defValue  string
	global    bool
	commands  map[string]struct{}
}

func (f *flagAgg) addCommand(path string) {
	if f == nil || strings.TrimSpace(path) == "" {
		return
	}
	if f.commands == nil {
		f.commands = make(map[string]struct{}, 8)
	}
	f.commands[path] = struct{}{}
}

func (f *flagAgg) commandList() []string {
	if f == nil || len(f.commands) == 0 {
		return nil
	}
	out := make([]string, 0, len(f.commands))
	for path := range f.commands {
		out = append(out, path)
	}
	sort.Strings(out)
	return out
}

func rootPersistentFlags(root *cobra.Command) *pflag.FlagSet {
	if root == nil {
		return nil
	}
	// Cobra treats PersistentFlags as inheritable "global" flags.
	return root.PersistentFlags()
}

func collectFlagNames(fs *pflag.FlagSet) map[string]struct{} {
	out := make(map[string]struct{})
	if fs == nil {
		return out
	}
	fs.VisitAll(func(f *pflag.Flag) {
		if f == nil {
			return
		}
		out[f.Name] = struct{}{}
	})
	return out
}
