package main

import (
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// namespaceHelpRequested reports whether the namespace flag captured a help token
// (for example, when a user runs `-n -h`). This allows us to forward the request
// to Cobra's help plumbing instead of treating "-h" as a namespace value.
func namespaceHelpRequested(fs *pflag.FlagSet) bool {
	if fs == nil {
		return false
	}
	flag := fs.Lookup("namespace")
	if flag == nil || !flag.Changed {
		return false
	}
	values := extractNamespaceValues(flag.Value.String())
	for _, val := range values {
		if requestedHelp(val) {
			return true
		}
	}
	return false
}

func extractNamespaceValues(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	if strings.HasPrefix(raw, "[") && strings.HasSuffix(raw, "]") {
		trimmed := strings.Trim(raw, "[]")
		if trimmed == "" {
			return nil
		}
		pieces := strings.Split(trimmed, ",")
		vals := make([]string, 0, len(pieces))
		for _, piece := range pieces {
			vals = append(vals, strings.TrimSpace(piece))
		}
		return vals
	}
	return []string{raw}
}

func commandNamespaceHelpRequested(cmd *cobra.Command) bool {
	if cmd == nil {
		return false
	}
	if namespaceHelpRequested(cmd.Flags()) {
		return true
	}
	return namespaceHelpRequested(cmd.InheritedFlags())
}
