package helpui

import (
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

type Model struct {
	Commands []Command `json:"commands"`
}

type Command struct {
	Path         string `json:"path"`
	Use          string `json:"use"`
	Short        string `json:"short"`
	Long         string `json:"long,omitempty"`
	Example      string `json:"example,omitempty"`
	FlagsSummary string `json:"flagsSummary,omitempty"`
	FlagsText    string `json:"flagsText,omitempty"`
}

func BuildModel(root *cobra.Command) Model {
	if root == nil {
		return Model{}
	}
	var cmds []Command
	visitCommands(root, func(cmd *cobra.Command) {
		cmds = append(cmds, Command{
			Path:         cmd.CommandPath(),
			Use:          cmd.UseLine(),
			Short:        strings.TrimSpace(cmd.Short),
			Long:         strings.TrimSpace(cmd.Long),
			Example:      strings.TrimSpace(cmd.Example),
			FlagsSummary: summarizeFlags(cmd),
			FlagsText:    flattenFlags(cmd),
		})
	})
	sort.Slice(cmds, func(i, j int) bool {
		return cmds[i].Path < cmds[j].Path
	})
	return Model{Commands: cmds}
}

func visitCommands(root *cobra.Command, fn func(*cobra.Command)) {
	for _, cmd := range root.Commands() {
		if cmd == nil {
			continue
		}
		if cmd.Name() == "help" {
			continue
		}
		if !cmd.IsAvailableCommand() {
			continue
		}
		fn(cmd)
		visitCommands(cmd, fn)
	}
}

func summarizeFlags(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	var names []string
	collect := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(f *pflag.Flag) {
			if f == nil || f.Hidden {
				return
			}
			name := "--" + f.Name
			if f.Shorthand != "" {
				name = "-" + f.Shorthand + ", " + name
			}
			names = append(names, name)
		})
	}
	collect(cmd.LocalFlags())
	if cmd.Parent() == nil {
		collect(cmd.InheritedFlags())
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	if len(names) > 4 {
		return strings.Join(names[:4], " · ") + " · …"
	}
	return strings.Join(names, " · ")
}

func flattenFlags(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}
	var b strings.Builder
	appendSet := func(fs *pflag.FlagSet) {
		if fs == nil {
			return
		}
		fs.VisitAll(func(f *pflag.Flag) {
			if f == nil || f.Hidden {
				return
			}
			b.WriteString("--")
			b.WriteString(f.Name)
			if f.Shorthand != "" {
				b.WriteString(" -")
				b.WriteString(f.Shorthand)
			}
			if u := strings.TrimSpace(f.Usage); u != "" {
				b.WriteString(" ")
				b.WriteString(u)
			}
			b.WriteString("\n")
		})
	}
	appendSet(cmd.LocalFlags())
	if cmd.Parent() == nil {
		appendSet(cmd.InheritedFlags())
	}
	return strings.TrimSpace(b.String())
}
