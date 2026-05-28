package stack

import (
	"testing"
	"time"

	transport "github.com/ingresslabs/torque/internal/ops/transport/contract"
)

func TestFinalizeFleetNATSFanoutReceiptPreservesBlockedReceipts(t *testing.T) {
	receipt := fleetNATSFanoutReceipt{
		Policy: fleetNATSFanoutPolicy{
			MaxParallel:         1,
			MaxFailed:           0,
			MinSucceededPercent: 100,
			OnPartialFailure:    RunnerFanoutOnBlock,
			Delivery:            "jetstream",
		},
		Summary: fleetNATSFanoutSummary{TargetCount: 1},
		Results: []fleetNATSFanoutResult{{
			Status:  "blocked",
			Receipt: transport.OperationResult{Status: "blocked"},
		}},
	}

	exec := &customNodeExecutor{}
	exec.finalizeFleetNATSFanoutReceipt(&receipt)
	if receipt.Status != "blocked" {
		t.Fatalf("fanout status = %q, want blocked", receipt.Status)
	}
	if receipt.Summary.Blocked != 1 || receipt.Summary.Failed != 0 || receipt.Summary.PolicyViolations == 0 {
		t.Fatalf("fanout summary = %#v, want blocked policy violation without failure", receipt.Summary)
	}
	result := exec.fleetNATSFanoutOperationResult(time.Now(), receipt)
	if result.Status != "blocked" || result.ExitCode == 0 {
		t.Fatalf("operation result = %#v, want blocked non-zero result", result)
	}
	if result.Metadata["blocked"] != "1" || result.Metadata["failed"] != "0" {
		t.Fatalf("operation metadata = %#v, want blocked/failed counts", result.Metadata)
	}
}
