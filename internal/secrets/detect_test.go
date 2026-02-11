package secrets

import "testing"

func TestDetectBuildArgs(t *testing.T) {
	t.Parallel()

	findings := DetectBuildArgs([]string{
		"NPM_TOKEN=ghp_0123456789abcdef0123456789abcdef0123",
		"FOO=bar",
		"PASSWORD=$PASSWORD",
	})
	if len(findings) == 0 {
		t.Fatalf("expected findings")
	}
}

func TestRedactText(t *testing.T) {
	t.Parallel()

	in := "failed: token=ghp_0123456789abcdef0123456789abcdef0123"
	out := RedactText(in)
	if out == in {
		t.Fatalf("expected redaction")
	}
}

func TestDetectBuildArgsWithRulesNegative(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Version: "v1",
		Rules: []Rule{
			{
				ID:        "only_npm_token_name",
				Severity:  SeverityWarn,
				AppliesTo: []ApplyTo{ApplyBuildArgName},
				Regex:     `^NPM_TOKEN$`,
				Message:   "match NPM_TOKEN",
			},
		},
	}
	compiled, err := CompileConfig(cfg)
	if err != nil {
		t.Fatalf("CompileConfig: %v", err)
	}
	findings := DetectBuildArgsWithRules([]string{"FOO=bar"}, compiled)
	if len(findings) != 0 {
		t.Fatalf("expected no findings, got %d", len(findings))
	}
}
