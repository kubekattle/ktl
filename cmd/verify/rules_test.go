package main

import (
	"bytes"
	"strings"
	"testing"
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
