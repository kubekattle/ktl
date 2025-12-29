package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVerifyYAML_ShowcaseManifest_CRITICALRule(t *testing.T) {
	kubeconfig := ""
	kubeContext := ""
	logLevel := "info"
	cmd := newVerifyCommand(&kubeconfig, &kubeContext, &logLevel)
	stdout := &bytes.Buffer{}
	stderr := &bytes.Buffer{}
	cmd.SetOut(stdout)
	cmd.SetErr(stderr)
	cmd.SetArgs([]string{"../../testdata/verify/showcase/verify.yaml"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("expected success, got %v\nstderr:\n%s", err, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "[CRITICAL] k8s/host_namespace_isolated") {
		t.Fatalf("expected CRITICAL finding, got:\n%s", out)
	}
}
