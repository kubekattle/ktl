package policy

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "policy.rego"), []byte(`
package ktl.build

deny[msg] {
  base := input.bases[_]
  contains(base, "docker.io/")
  msg := {"code":"BLOCKED","message":"blocked registry","subject": base}
}
`), 0o644); err != nil {
		t.Fatalf("write rego: %v", err)
	}
	b := &Bundle{Dir: dir, Data: map[string]any{"hello": "world"}}

	rep, err := Evaluate(context.Background(), b, BuildInput{
		WhenUTC:  time.Now().UTC(),
		Bases:    []string{"docker.io/library/alpine:3.20"},
		Labels:   map[string]string{},
		External: nil,
	})
	if err != nil {
		t.Fatalf("Evaluate: %v", err)
	}
	if rep.DenyCount != 1 {
		t.Fatalf("expected 1 deny, got %d", rep.DenyCount)
	}
	if rep.Deny[0].Code != "BLOCKED" {
		t.Fatalf("unexpected code %q", rep.Deny[0].Code)
	}
}
