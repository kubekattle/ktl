package verify

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

var _, verifyTestFile, _, _ = runtime.Caller(0)
var verifyRepoRoot = filepath.Clean(filepath.Join(filepath.Dir(verifyTestFile), "..", ".."))

func verifyTestdata(parts ...string) string {
	base := append([]string{verifyRepoRoot}, parts...)
	return filepath.Join(base...)
}

func TestVerifyObjects_K8sRuleFixtures_PositiveAndNegative(t *testing.T) {
	ctx := context.Background()
	rulesDir := verifyTestdata("internal", "verify", "rules", "builtin")

	type tc struct {
		ruleID      string
		positiveYML string
		negativeYML string
	}
	cases := []tc{
		{
			ruleID:      "k8s/container_is_privileged",
			positiveYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "container_is_privileged", "test", "positive1.yaml"),
			negativeYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "container_is_privileged", "test", "negative.yaml"),
		},
		{
			ruleID:      "k8s/service_account_token_automount_not_disabled",
			positiveYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "service_account_token_automount_not_disabled", "test", "positive1.yaml"),
			negativeYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "service_account_token_automount_not_disabled", "test", "negative1.yaml"),
		},
		{
			ruleID:      "k8s/net_raw_capabilities_not_being_dropped",
			positiveYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "net_raw_capabilities_not_being_dropped", "test", "positive1.yaml"),
			negativeYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "net_raw_capabilities_not_being_dropped", "test", "negative.yaml"),
		},
		{
			ruleID:      "k8s/pod_or_container_without_security_context",
			positiveYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "pod_or_container_without_security_context", "test", "positive.yaml"),
			negativeYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "pod_or_container_without_security_context", "test", "negative.yaml"),
		},
		{
			ruleID:      "k8s/memory_limits_not_defined",
			positiveYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "memory_limits_not_defined", "test", "positive1.yaml"),
			negativeYML: verifyTestdata("internal", "verify", "rules", "builtin", "k8s", "memory_limits_not_defined", "test", "negative1.yaml"),
		},
	}

	for _, c := range cases {
		c := c
		t.Run(c.ruleID, func(t *testing.T) {
			rawPos, err := os.ReadFile(c.positiveYML)
			if err != nil {
				t.Fatalf("read positive fixture: %v", err)
			}
			posObjs, err := DecodeK8SYAML(string(rawPos))
			if err != nil {
				t.Fatalf("decode positive yaml: %v", err)
			}
			posRep, err := VerifyObjects(ctx, posObjs, Options{
				Mode:     ModeWarn,
				FailOn:   SeverityHigh,
				Format:   OutputJSON,
				RulesDir: rulesDir,
			})
			if err != nil {
				t.Fatalf("verify positive: %v", err)
			}
			if !hasRule(posRep.Findings, c.ruleID) {
				t.Fatalf("expected positive fixture to trigger %s, got findings: %#v", c.ruleID, posRep.Findings)
			}

			rawNeg, err := os.ReadFile(c.negativeYML)
			if err != nil {
				t.Fatalf("read negative fixture: %v", err)
			}
			negObjs, err := DecodeK8SYAML(string(rawNeg))
			if err != nil {
				t.Fatalf("decode negative yaml: %v", err)
			}
			negRep, err := VerifyObjects(ctx, negObjs, Options{
				Mode:     ModeWarn,
				FailOn:   SeverityHigh,
				Format:   OutputJSON,
				RulesDir: rulesDir,
			})
			if err != nil {
				t.Fatalf("verify negative: %v", err)
			}
			if hasRule(negRep.Findings, c.ruleID) {
				t.Fatalf("expected negative fixture to not trigger %s, got findings: %#v", c.ruleID, negRep.Findings)
			}
		})
	}
}

func hasRule(findings []Finding, ruleID string) bool {
	for _, f := range findings {
		if f.RuleID == ruleID {
			return true
		}
	}
	return false
}
