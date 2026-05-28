package postgres

import (
	"strings"
	"testing"
)

func TestDefaultLockPolicyScopesMutableResources(t *testing.T) {
	policy := DefaultLockPolicy("lab", "postgres.role.ensure", Spec{
		Database: "keycloak",
		Role:     RoleSpec{Name: "torque_auditor"},
	})
	if policy == nil || !policy.Enabled {
		t.Fatalf("expected enabled lock policy, got %#v", policy)
	}
	if policy.TimeoutMillis != defaultLockTimeoutMillis || policy.OnFailure != "block" {
		t.Fatalf("lock defaults = %#v", policy)
	}
	for _, want := range []string{
		"tenant=lab",
		"database=postgres",
		"kind=postgres.role.ensure",
		"identity=torque_auditor",
	} {
		if !strings.Contains(policy.Key, want) {
			t.Fatalf("lock key %q missing %q", policy.Key, want)
		}
	}
	if got := digestValue(policy.Key); !strings.HasPrefix(got, "sha256:") || len(got) != len("sha256:")+64 {
		t.Fatalf("lock digest = %q", got)
	}
	if advisoryLockID(policy.Key) == 0 {
		t.Fatalf("advisory lock id should be stable and non-zero")
	}
}

func TestDefaultLockPolicySkipsReadOnlyVerifyResources(t *testing.T) {
	for _, kind := range []string{"postgres.replication.verify", "postgres.backup.verify"} {
		if policy := DefaultLockPolicy("lab", kind, Spec{}); policy != nil {
			t.Fatalf("%s lock policy = %#v, want nil", kind, policy)
		}
	}
}

func TestTransactionUnsupportedReceiptExplainsCrossTransactionResources(t *testing.T) {
	cases := map[string]string{
		"postgres.database.ensure": "CREATE DATABASE",
		"postgres.config.ensure":   "ALTER SYSTEM",
		"postgres.backup.run":      "pg_dump",
		"postgres.restore.drill":   "pg_restore",
	}
	for kind, want := range cases {
		receipt := transactionUnsupportedReceipt(kind)
		if receipt == nil || receipt.Supported {
			t.Fatalf("%s receipt = %#v, want unsupported transaction", kind, receipt)
		}
		if !strings.Contains(receipt.Reason, want) {
			t.Fatalf("%s reason = %q, want %q", kind, receipt.Reason, want)
		}
	}
}
