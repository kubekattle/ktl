package verify

import "testing"

func TestPatchSuggestion_PodUsesSpecNotTemplate(t *testing.T) {
	sub := Subject{Kind: "Pod", Namespace: "default", Name: "demo"}
	patch := patchSuggestion(sub, "k8s/service_account_token_automount_not_disabled")
	if patch == "" {
		t.Fatalf("expected patch")
	}
	if contains(patch, "template:") {
		t.Fatalf("expected Pod patch to not use spec.template, got:\n%s", patch)
	}
	if !contains(patch, "automountServiceAccountToken: false") {
		t.Fatalf("expected automountServiceAccountToken, got:\n%s", patch)
	}
}

func TestEvaluateRules_UsesDocumentMetadataForSubject(t *testing.T) {
	// Use a rule that fires based on missing securityContext; inject namespace and ensure we propagate it.
	rules, err := LoadRuleset(verifyTestdata("internal", "verify", "rules", "builtin"))
	if err != nil {
		t.Fatalf("LoadRuleset: %v", err)
	}
	obj := map[string]any{
		"apiVersion": "apps/v1",
		"kind":       "Deployment",
		"metadata": map[string]any{
			"name":      "demo",
			"namespace": "default",
		},
		"spec": map[string]any{
			"template": map[string]any{
				"spec": map[string]any{
					"containers": []any{
						map[string]any{"name": "c", "image": "busybox"},
					},
				},
			},
		},
	}
	findings, err := EvaluateRules(t.Context(), rules, []map[string]any{obj}, verifyTestdata("internal", "verify", "rules", "builtin", "lib"))
	if err != nil {
		t.Fatalf("EvaluateRules: %v", err)
	}
	var hit *Finding
	for i := range findings {
		if findings[i].RuleID == "k8s/pod_or_container_without_security_context" {
			hit = &findings[i]
			break
		}
	}
	if hit == nil {
		t.Fatalf("expected rule to fire")
	}
	if hit.Subject.Namespace != "default" {
		t.Fatalf("expected namespace to propagate from document metadata, got %q", hit.Subject.Namespace)
	}
	if hit.Subject.Name != "demo" {
		t.Fatalf("expected name to propagate from document metadata, got %q", hit.Subject.Name)
	}
}

func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && (func() bool { return (stringIndex(s, sub) >= 0) })())
}

func stringIndex(s, sub string) int {
	// avoid importing strings in this tiny test file to keep dependencies minimal
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
