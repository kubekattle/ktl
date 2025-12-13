package main

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

func TestNamespaceHelpRequestedDetectsHelpToken(t *testing.T) {
	set := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var ns []string
	set.StringSliceVarP(&ns, "namespace", "n", nil, "")
	if err := set.Parse([]string{"-n", "-h"}); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if !namespaceHelpRequested(set) {
		t.Fatalf("expected help detection when namespace equals -h")
	}
}

func TestNamespaceHelpRequestedIgnoresRegularValues(t *testing.T) {
	set := pflag.NewFlagSet("test", pflag.ContinueOnError)
	var ns []string
	set.StringSliceVarP(&ns, "namespace", "n", nil, "")
	if err := set.Parse([]string{"-n", "ktl-logger"}); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	if namespaceHelpRequested(set) {
		t.Fatalf("did not expect help detection for real namespace")
	}
}

func TestCommandNamespaceHelpRequestedSeesInheritedFlags(t *testing.T) {
	child := &cobra.Command{Use: "child"}
	set := pflag.NewFlagSet("inherited", pflag.ContinueOnError)
	var ns []string
	set.StringSliceVarP(&ns, "namespace", "n", nil, "")
	if err := set.Parse([]string{"-n", "-h"}); err != nil {
		t.Fatalf("parse failed: %v", err)
	}
	child.InheritedFlags().AddFlagSet(set)
	if !commandNamespaceHelpRequested(child) {
		t.Fatalf("expected help detection via inherited namespace flag")
	}
}
