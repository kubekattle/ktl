package verify

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestBuiltinRules_GovernanceAndSnapshots(t *testing.T) {
	ctx := context.Background()
	rulesDir := verifyTestdata("internal", "verify", "rules", "builtin")
	rs, err := LoadRuleset(rulesDir)
	if err != nil {
		t.Fatalf("LoadRuleset: %v", err)
	}
	if len(rs.Rules) == 0 {
		t.Fatalf("expected builtin rules, got none")
	}

	update := strings.TrimSpace(os.Getenv("KTL_UPDATE_RULE_GOLDENS")) != ""
	commonDirs := []string{verifyTestdata("internal", "verify", "rules", "builtin", "lib")}

	for _, rule := range rs.Rules {
		rule := rule
		t.Run(rule.ID, func(t *testing.T) {
			validateRuleMetadata(t, rule.Dir)
			testDir := filepath.Join(rule.Dir, "test")
			passY := filepath.Join(testDir, "pass.yaml")
			failY := filepath.Join(testDir, "fail.yaml")
			edgeY := filepath.Join(testDir, "edge.yaml")

			mustExist(t, passY)
			mustExist(t, failY)
			mustExist(t, edgeY)

			// Pass fixture: 0 findings for this rule.
			passObjs := decodeFixture(t, passY)
			passFindings := evalRuleOnly(t, ctx, rulesDir, commonDirs, rule.ID, passObjs)
			if len(passFindings) != 0 {
				t.Fatalf("pass fixture produced %d findings (want 0): %#v", len(passFindings), passFindings)
			}

			// Fail fixture: >=1 finding, snapshot.
			failObjs := decodeFixture(t, failY)
			failFindings := evalRuleOnly(t, ctx, rulesDir, commonDirs, rule.ID, failObjs)
			if len(failFindings) == 0 {
				t.Fatalf("fail fixture produced 0 findings (want >=1)")
			}
			writeOrCompareSnapshot(t, update, filepath.Join(testDir, "fail.findings.json"), failFindings)

			// Edge fixture: >=1 finding, snapshot.
			edgeObjs := decodeFixture(t, edgeY)
			edgeFindings := evalRuleOnly(t, ctx, rulesDir, commonDirs, rule.ID, edgeObjs)
			if len(edgeFindings) == 0 {
				t.Fatalf("edge fixture produced 0 findings (want >=1)")
			}
			writeOrCompareSnapshot(t, update, filepath.Join(testDir, "edge.findings.json"), edgeFindings)
		})
	}
}

func validateRuleMetadata(t *testing.T, ruleDir string) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(ruleDir, "metadata.json"))
	if err != nil {
		t.Fatalf("read metadata.json: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("decode metadata.json: %v", err)
	}
	req := []string{"id", "queryName", "severity", "category", "descriptionText", "descriptionUrl"}
	for _, k := range req {
		v, _ := m[k]
		s, _ := v.(string)
		if strings.TrimSpace(s) == "" {
			t.Fatalf("metadata.json missing/empty %q", k)
		}
	}
}

func mustExist(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing required fixture %s: %v", path, err)
	}
}

func decodeFixture(t *testing.T, path string) []map[string]any {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	objs, err := DecodeK8SYAML(string(raw))
	if err != nil {
		t.Fatalf("decode fixture yaml: %v", err)
	}
	return objs
}

func evalRuleOnly(t *testing.T, ctx context.Context, rulesDir string, commonDirs []string, ruleID string, objs []map[string]any) []Finding {
	t.Helper()
	rs, err := LoadRuleset(rulesDir)
	if err != nil {
		t.Fatalf("LoadRuleset: %v", err)
	}

	pat := "^" + regexp.QuoteMeta(strings.TrimSpace(ruleID)) + "$"
	findings, err := EvaluateRulesWithSelectors(ctx, rs, objs, commonDirs, SelectorSet{}, []RuleSelector{{Rule: pat}})
	if err != nil {
		t.Fatalf("EvaluateRulesWithSelectors: %v", err)
	}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if strings.TrimSpace(f.RuleID) != strings.TrimSpace(ruleID) {
			continue
		}
		out = append(out, f)
	}
	sortFindingsForSnapshot(out)
	return out
}

func sortFindingsForSnapshot(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		fi, fj := fs[i], fs[j]
		if fi.Severity != fj.Severity {
			return severityRank(fi.Severity) < severityRank(fj.Severity)
		}
		if fi.ResourceKey != fj.ResourceKey {
			return fi.ResourceKey < fj.ResourceKey
		}
		if fi.FieldPath != fj.FieldPath {
			return fi.FieldPath < fj.FieldPath
		}
		if fi.Location != fj.Location {
			return fi.Location < fj.Location
		}
		return fi.Fingerprint < fj.Fingerprint
	})
}

func writeOrCompareSnapshot(t *testing.T, update bool, path string, fs []Finding) {
	t.Helper()
	raw, err := json.MarshalIndent(fs, "", "  ")
	if err != nil {
		t.Fatalf("marshal snapshot: %v", err)
	}
	raw = append(raw, '\n')
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatalf("mkdir snapshot dir: %v", err)
		}
		if err := os.WriteFile(path, raw, 0o644); err != nil {
			t.Fatalf("write snapshot: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read snapshot %s (set KTL_UPDATE_RULE_GOLDENS=1 to generate): %v", path, err)
	}
	if string(want) != string(raw) {
		t.Fatalf("snapshot mismatch: %s (set KTL_UPDATE_RULE_GOLDENS=1 to update)", path)
	}
}
