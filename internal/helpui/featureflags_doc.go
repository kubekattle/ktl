package helpui

import (
	"fmt"
	"strings"

	"github.com/example/ktl/internal/featureflags"
)

func featureFlagsDoc() string {
	defs := featureflags.Definitions()
	if len(defs) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Registered feature flags\n\n")
	b.WriteString("Enable per invocation with `ktl --feature <name>` (repeatable/comma-separated), via config (`feature: [\"<name>\"]`), or env vars `KTL_FEATURE_<FLAG>`.\n\n")
	for _, def := range defs {
		defaultState := "off"
		if def.Default {
			defaultState = "on"
		}
		fmt.Fprintf(&b, "- %s (%s, default %s)\n", def.Name, def.Stage, defaultState)
		fmt.Fprintf(&b, "  Env: %s\n", def.EnvVar())
		if desc := strings.TrimSpace(def.Description); desc != "" {
			fmt.Fprintf(&b, "  %s\n", desc)
		}
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

