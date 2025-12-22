package main

import "testing"

func TestApplyHasDriftGuardFlagDisabledByDefault(t *testing.T) {
	var ns string
	var kubeconfig string
	var kubeContext string
	logLevel := "info"
	var remoteAgent string

	cmd := newDeployApplyCommand(&ns, &kubeconfig, &kubeContext, &logLevel, &remoteAgent, "")
	f := cmd.Flags().Lookup("drift-guard")
	if f == nil {
		t.Fatalf("expected --drift-guard flag to exist")
	}
	if f.DefValue != "false" {
		t.Fatalf("expected --drift-guard default to be false, got %q", f.DefValue)
	}
}
