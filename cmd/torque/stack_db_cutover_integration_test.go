//go:build integration

package main

import (
	"os"
	"os/exec"
	"testing"
)

func TestStackDBCutover_E2E_RealCluster(t *testing.T) {
	if os.Getenv("TORQUE_STACK_DB_CUTOVER_E2E_CONFIRM") != "1" {
		t.Skip("TORQUE_STACK_DB_CUTOVER_E2E_CONFIRM not set")
	}
	if os.Getenv("KUBECONFIG") == "" {
		t.Skip("KUBECONFIG not set")
	}

	cmd := exec.Command("bash", repoScript("stack-db-cutover-e2e-real.sh"))
	cmd.Dir = intTestRepoRoot
	cmd.Env = append(os.Environ(), "KUBECONFIG_PATH="+os.Getenv("KUBECONFIG"))
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatalf("stack db cutover e2e failed: %v", err)
	}
}
