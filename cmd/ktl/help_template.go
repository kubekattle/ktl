// File: cmd/ktl/help_template.go
// Brief: CLI command wiring and implementation for 'help template'.

// help_template.go customizes Cobra's help/usage templates so ktl commands share concise, branded flag sections.
package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const (
	localFlagsHeadingKey = "localFlagsHeading"
	localUsageKey        = "localFlagUsages"
	inheritedUsageKey    = "inheritedFlagUsages"
	showInheritedKey     = "showInheritedFlags"
)

const commandHelpTemplate = `{{with or .Long .Short}}{{. | trimTrailingWhitespaces}}{{end}}

Usage:
  {{.UseLine}}

{{if .HasAvailableSubCommands}}Subcommands:
{{range .Commands}}{{if (and .IsAvailableCommand (ne .Name "help"))}}  {{rpad .Name .NamePadding}} {{.Short}}
{{end}}{{end}}

{{end}}

{{- $local := "Command Flags" -}}
{{- if .Annotations -}}
  {{- with index .Annotations "localFlagsHeading" -}}
    {{- $local = . -}}
  {{- end -}}
{{- end -}}
{{$local}}:
{{if .HasAvailableFlags}}{{with index .Annotations "localFlagUsages"}}{{.}}{{else}}{{.LocalFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}{{else}}  (none){{end}}

{{ $hideInherited := and .Annotations (eq (index .Annotations "showInheritedFlags") "false") }}
{{if and .HasAvailableInheritedFlags (not $hideInherited)}}
Global Flags:
{{with index .Annotations "inheritedFlagUsages"}}{{.}}{{else}}{{.InheritedFlags.FlagUsages | trimTrailingWhitespaces}}{{end}}
{{end}}
`

func decorateCommandHelp(cmd *cobra.Command, heading string) {
	if strings.TrimSpace(heading) == "" {
		heading = fmt.Sprintf("%s Flags", titleCase(cmd.Name()))
	}
	cmd.SetHelpTemplate(commandHelpTemplate)
	defaultHelp := cmd.HelpFunc()
	cmd.SetHelpFunc(func(cmd *cobra.Command, args []string) {
		if cmd.Annotations == nil {
			cmd.Annotations = make(map[string]string)
		}
		cmd.Annotations[localFlagsHeadingKey] = heading
		if usages := formatFlagUsages(cmd.LocalFlags()); usages != "" {
			cmd.Annotations[localUsageKey] = usages
		} else {
			delete(cmd.Annotations, localUsageKey)
		}
		showInherited := shouldShowInheritedFlags(cmd)
		cmd.Annotations[showInheritedKey] = fmt.Sprintf("%t", showInherited)
		if showInherited {
			if inherited := formatFlagUsages(cmd.InheritedFlags()); inherited != "" {
				cmd.Annotations[inheritedUsageKey] = inherited
			} else {
				delete(cmd.Annotations, inheritedUsageKey)
			}
		} else {
			delete(cmd.Annotations, inheritedUsageKey)
		}
		defaultHelp(cmd, args)
	})
}

func titleCase(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if len(s) == 1 {
		return strings.ToUpper(s)
	}
	return strings.ToUpper(string(s[0])) + s[1:]
}

func formatFlagUsages(fs *pflag.FlagSet) string {
	if fs == nil {
		return ""
	}
	type mutation struct {
		flag *pflag.Flag
		prev string
	}
	var mutated []mutation
	fs.VisitAll(func(f *pflag.Flag) {
		if f.Shorthand == "" || f.NoOptDefVal != "" {
			return
		}
		if strings.ToLower(f.Value.Type()) != "string" {
			return
		}
		mutated = append(mutated, mutation{flag: f, prev: f.NoOptDefVal})
		f.NoOptDefVal = f.DefValue
	})
	usages := fs.FlagUsagesWrapped(100)
	for _, m := range mutated {
		m.flag.NoOptDefVal = m.prev
	}
	usages = strings.ReplaceAll(usages, "\t", "  ")
	return strings.TrimRight(usages, "\n")
}

func shouldShowInheritedFlags(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if cmd.Annotations != nil {
		if strings.EqualFold(cmd.Annotations[showInheritedKey], "false") {
			return false
		}
	}
	return cmd.HasAvailableInheritedFlags()
}
