package helpui

import (
	"testing"

	"github.com/spf13/cobra"
)

func TestBuildIndex_IncludesCommandsFlagsAndEnv(t *testing.T) {
	root := &cobra.Command{Use: "ktl"}
	root.PersistentFlags().String("log-level", "info", "Log level")
	apply := &cobra.Command{Use: "apply", Short: "Apply chart", Example: "ktl apply --chart ./chart --release foo"}
	apply.Flags().StringP("namespace", "n", "", "Namespace to deploy into")
	root.AddCommand(apply)

	index := BuildIndex(root, false)
	if len(index.Entries) == 0 {
		t.Fatalf("expected entries, got none")
	}
	assertHas := func(kind string, contains string) {
		t.Helper()
		for _, e := range index.Entries {
			if e.Kind != kind {
				continue
			}
			if e.Title == contains {
				return
			}
		}
		t.Fatalf("expected %s entry with title %q", kind, contains)
	}
	assertHas("command", "ktl")
	assertHas("command", "ktl apply")
	assertHas("env", "KTL_CONFIG")

	foundFlag := false
	for _, e := range index.Entries {
		if e.Kind == "flag" && e.Title == "-n, --namespace" {
			foundFlag = true
			break
		}
	}
	if !foundFlag {
		t.Fatalf("expected flag entry for -n/--namespace")
	}
}
