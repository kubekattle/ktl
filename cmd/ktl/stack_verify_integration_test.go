//go:build integration

package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestStackVerify_E2E_RealCluster(t *testing.T) {
	// This is an opt-in e2e harness. It requires a real cluster and is intentionally
	// guarded by build tags + env so normal unit test runs stay fast and hermetic.
	if os.Getenv("KTL_STACK_VERIFY_E2E_NAMESPACE") == "" {
		t.Skip("KTL_STACK_VERIFY_E2E_NAMESPACE not set")
	}
	if os.Getenv("KUBECONFIG") == "" {
		t.Skip("KUBECONFIG not set")
	}

	cmd := exec.Command("bash", "./scripts/stack-verify-e2e-real.sh")
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stack verify e2e failed: %v", err)
	}
}
