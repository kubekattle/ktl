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
