package deploy

import (
	"errors"
	"strings"
	"testing"
)

func TestWrapUpgradeOnlyNoDeployedReleaseErrAddsGuidance(t *testing.T) {
	baseErr := errors.New("\"roedk\" has no deployed releases")
	err := wrapUpgradeOnlyNoDeployedReleaseErr("roedk", "roedk", baseErr)
	if err == nil {
		t.Fatalf("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "release \"roedk\" is not deployed in namespace \"roedk\"") {
		t.Fatalf("expected release/namespace guidance, got: %s", msg)
	}
	if !strings.Contains(msg, "omit --upgrade") {
		t.Fatalf("expected --upgrade hint, got: %s", msg)
	}
	if !strings.Contains(msg, "ktl list --namespace roedk") {
		t.Fatalf("expected list hint, got: %s", msg)
	}
}
