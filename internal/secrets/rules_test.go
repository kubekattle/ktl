package secrets

import "testing"

func TestCompileConfigRejectsBadRegex(t *testing.T) {
	t.Parallel()

	_, err := CompileConfig(Config{
		Version: "v1",
		Rules: []Rule{
			{ID: "bad", Severity: SeverityWarn, AppliesTo: []ApplyTo{ApplyLogLine}, Regex: "("},
		},
	})
	if err == nil {
		t.Fatalf("expected error")
	}
}

func TestMergeConfigOverridesByID(t *testing.T) {
	t.Parallel()

	base := DefaultConfig()
	override := Config{
		Version: "v1",
		Rules: []Rule{
			{ID: "arg_value_github_token", Severity: SeverityBlock, AppliesTo: []ApplyTo{ApplyLogLine}, Regex: `ghp_[A-Za-z0-9]{20,}`},
		},
	}
	merged := MergeConfig(base, override)
	compiled, err := CompileConfig(merged)
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	found := false
	for _, r := range compiled.Rules {
		if r.ID == "arg_value_github_token" {
			found = true
			if r.Severity != SeverityBlock {
				t.Fatalf("expected override severity block, got %s", r.Severity)
			}
		}
	}
	if !found {
		t.Fatalf("expected merged rule")
	}
}
