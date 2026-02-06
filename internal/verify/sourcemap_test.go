package verify

import "testing"

func TestAnnotateFindingsWithRenderedSource_SetsLineAndPath(t *testing.T) {
	manifest := "" +
		"apiVersion: v1\n" +
		"kind: Pod\n" +
		"metadata:\n" +
		"  name: p\n" +
		"  namespace: ns\n" +
		"spec:\n" +
		"  hostNetwork: true\n" +
		"---\n" +
		"# Source: chart/templates/svc.yaml\n" +
		"apiVersion: v1\n" +
		"kind: Service\n" +
		"metadata:\n" +
		"  name: s\n" +
		"  namespace: ns\n" +
		"spec:\n" +
		"  type: LoadBalancer\n"

	findings := []Finding{
		{
			RuleID:    "r1",
			Severity:  SeverityHigh,
			Message:   "bad",
			FieldPath: "spec.hostNetwork",
			Subject:   Subject{Kind: "Pod", Namespace: "ns", Name: "p"},
		},
		{
			RuleID:   "r2",
			Severity: SeverityHigh,
			Message:  "bad",
			Location: "spec.type", // policy-style
			Subject:  Subject{Kind: "Service", Namespace: "ns", Name: "s"},
		},
	}

	out := AnnotateFindingsWithRenderedSource("rendered.yaml", manifest, findings)
	if got := out[0].Path; got != "rendered.yaml" {
		t.Fatalf("finding0 path=%q, want rendered.yaml", got)
	}
	if out[0].Line <= 0 {
		t.Fatalf("finding0 line=%d, want >0", out[0].Line)
	}
	if got := out[1].Path; got != "rendered.yaml" {
		t.Fatalf("finding1 path=%q, want rendered.yaml", got)
	}
	if out[1].Line <= 0 {
		t.Fatalf("finding1 line=%d, want >0", out[1].Line)
	}
	// Service doc starts after the separator and comment; ensure it is not the same
	// as the Pod line number.
	if out[1].Line == out[0].Line {
		t.Fatalf("finding1 line=%d equals finding0 line=%d; want distinct", out[1].Line, out[0].Line)
	}
}
