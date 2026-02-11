package main

import (
	"context"
	"flag"
	"testing"

	"github.com/spf13/cobra"
)

func TestKubeLogLevelFlagSetsKlogVerbosity(t *testing.T) {
	origV := ""
	if f := flag.CommandLine.Lookup("v"); f != nil {
		origV = f.Value.String()
	}
	t.Cleanup(func() {
		if origV != "" {
			_ = flag.CommandLine.Set("v", origV)
		}
	})

	root := newRootCommand()
	root.SetArgs([]string{"noop", "--kube-log-level", "7"})
	root.AddCommand(&cobra.Command{Use: "noop", RunE: func(cmd *cobra.Command, args []string) error { return nil }})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	f := flag.CommandLine.Lookup("v")
	if f == nil {
		t.Fatalf("expected klog flag \"v\" to be registered")
	}
	if got := f.Value.String(); got != "7" {
		t.Fatalf("expected klog -v=7, got %q", got)
	}
}

func TestKubeLogLevelEnvSetsKlogVerbosity(t *testing.T) {
	origV := ""
	if f := flag.CommandLine.Lookup("v"); f != nil {
		origV = f.Value.String()
	}
	t.Cleanup(func() {
		if origV != "" {
			_ = flag.CommandLine.Set("v", origV)
		}
	})

	t.Setenv("KTL_KUBE_LOG_LEVEL", "8")
	root := newRootCommand()
	root.SetArgs([]string{"noop"})
	root.AddCommand(&cobra.Command{Use: "noop", RunE: func(cmd *cobra.Command, args []string) error { return nil }})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	f := flag.CommandLine.Lookup("v")
	if f == nil {
		t.Fatalf("expected klog flag \"v\" to be registered")
	}
	if got := f.Value.String(); got != "8" {
		t.Fatalf("expected klog -v=8, got %q", got)
	}
}

func TestKubeLogLevelDefaultsToTraceInDebugMode(t *testing.T) {
	origV := ""
	if f := flag.CommandLine.Lookup("v"); f != nil {
		origV = f.Value.String()
	}
	t.Cleanup(func() {
		if origV != "" {
			_ = flag.CommandLine.Set("v", origV)
		}
	})

	t.Setenv("KTL_KUBE_LOG_LEVEL", "")
	root := newRootCommand()
	root.SetArgs([]string{"noop", "--log-level", "debug"})
	root.AddCommand(&cobra.Command{Use: "noop", RunE: func(cmd *cobra.Command, args []string) error { return nil }})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("execute: %v", err)
	}
	f := flag.CommandLine.Lookup("v")
	if f == nil {
		t.Fatalf("expected klog flag \"v\" to be registered")
	}
	if got := f.Value.String(); got != "6" {
		t.Fatalf("expected klog -v=6 default in debug mode, got %q", got)
	}
}
