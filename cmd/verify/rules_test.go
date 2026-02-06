package main

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ktl/internal/verify"
)

func TestVerifyRules_ListAndShow(t *testing.T) {
	chdirRepoRoot(t)
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	noColor := false
	rulesPath := ""

	cmd := newVerifyCommand(&kubeconfig, &kubeContext, &logLevel, &noColor, &rulesPath)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)

	cmd.SetArgs([]string{"rules", "list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rules list: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "k8s/container_is_privileged") {
		t.Fatalf("expected builtin rule id in output, got:\n%s", stdout.String())
	}

	stdout.Reset()
	stderr.Reset()
	cmd = newVerifyCommand(&kubeconfig, &kubeContext, &logLevel, &noColor, &rulesPath)
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"rules", "show", "k8s/container_is_privileged"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rules show: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "ID: k8s/container_is_privileged") {
		t.Fatalf("expected rule header, got:\n%s", stdout.String())
	}
}

func TestVerifyRules_Explain(t *testing.T) {
	chdirRepoRoot(t)
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	noColor := false
	rulesPath := ""

	cmd := newVerifyCommand(&kubeconfig, &kubeContext, &logLevel, &noColor, &rulesPath)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"rules", "explain", "k8s/container_is_privileged"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rules explain: %v\nstderr:\n%s", err, stderr.String())
	}
	if !strings.Contains(stdout.String(), "Fixtures:") {
		t.Fatalf("expected fixtures section, got:\n%s", stdout.String())
	}
}

func TestVerifyRules_Explain_AllBuiltinRules(t *testing.T) {
	chdirRepoRoot(t)

	rs, err := verify.LoadRuleset(filepath.Join(".", "internal", "verify", "rules", "builtin"))
	if err != nil {
		t.Fatalf("LoadRuleset: %v", err)
	}
	if len(rs.Rules) == 0 {
		t.Fatalf("expected builtin rules, got none")
	}

	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	noColor := false
	rulesPath := ""

	for _, r := range rs.Rules {
		r := r
		t.Run(r.ID, func(t *testing.T) {
			cmd := newVerifyCommand(&kubeconfig, &kubeContext, &logLevel, &noColor, &rulesPath)
			stdout := &bytes.Buffer{}
			stderr := &bytes.Buffer{}
			cmd.SetOut(stdout)
			cmd.SetErr(stderr)
			cmd.SetArgs([]string{"rules", "explain", r.ID})
			if err := cmd.Execute(); err != nil {
				t.Fatalf("rules explain: %v\nstderr:\n%s", err, stderr.String())
			}
			if !strings.Contains(stdout.String(), "ID: "+r.ID) {
				t.Fatalf("expected rule header, got:\n%s", stdout.String())
			}
			if !strings.Contains(stdout.String(), "Fixtures:") {
				t.Fatalf("expected fixtures section, got:\n%s", stdout.String())
			}
			if !strings.Contains(stdout.String(), "pass.yaml") {
				t.Fatalf("expected pass.yaml fixture, got:\n%s", stdout.String())
			}
		})
	}
}
