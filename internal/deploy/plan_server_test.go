// File: internal/deploy/plan_server_test.go
// Brief: Tests for server-side plan helpers.

package deploy

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestParseStatusFromErrorString(t *testing.T) {
	statusJSON := `{"kind":"Status","apiVersion":"v1","status":"Failure","message":"invalid","reason":"Invalid","details":{"causes":[{"field":"spec.clusterIP","message":"field is immutable"}]}}`
	status, ok := parseStatusFromErrorString(statusJSON)
	if !ok {
		t.Fatalf("expected ok")
	}
	if status.Kind != "Status" {
		t.Fatalf("unexpected kind: %q", status.Kind)
	}
	if !looksLikeImmutableStatus("Service", status) {
		t.Fatalf("expected immutable status to be detected")
	}
	if looksLikeImmutableStatus("Deployment", status) {
		t.Fatalf("did not expect immutable status for Deployment")
	}
}

func TestLooksLikeImmutableStatusRequiresCauses(t *testing.T) {
	if looksLikeImmutableStatus("Service", metav1.Status{}) {
		t.Fatalf("expected false")
	}
}
