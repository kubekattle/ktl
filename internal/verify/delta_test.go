package verify

import "testing"

func TestComputeDelta_Details(t *testing.T) {
	base := &Report{
		Findings: []Finding{
			{RuleID: "r1", Severity: SeverityHigh, Message: "m1", Observed: "a", Expected: "x", Subject: Subject{Kind: "Deployment", Namespace: "ns", Name: "app"}, FieldPath: "spec.foo", Fingerprint: "fp1"},
			{RuleID: "r2", Severity: SeverityMedium, Message: "m2", Subject: Subject{Kind: "Service", Namespace: "ns", Name: "svc"}, Location: "spec.type", Fingerprint: "fp2"},
		},
		Summary: Summary{Total: 2},
	}
	cur := &Report{
		Findings: []Finding{
			// unchanged (same fp)
			{RuleID: "r1", Severity: SeverityHigh, Message: "m1", Observed: "a", Expected: "x", Subject: Subject{Kind: "Deployment", Namespace: "ns", Name: "app"}, FieldPath: "spec.foo", Fingerprint: "fp1"},
			// changed (same identity, different observed, different fp)
			{RuleID: "r2", Severity: SeverityMedium, Message: "m2", Observed: "new", Subject: Subject{Kind: "Service", Namespace: "ns", Name: "svc"}, Location: "spec.type", Fingerprint: "fp2-new"},
			// new
			{RuleID: "r3", Severity: SeverityLow, Message: "m3", Subject: Subject{Kind: "Pod", Namespace: "ns", Name: "p"}, Location: "spec.hostNetwork", Fingerprint: "fp3"},
		},
		Summary: Summary{Total: 3},
	}

	d := ComputeDelta(cur, base)
	if d.Unchanged != 1 {
		t.Fatalf("unchanged=%d, want 1", d.Unchanged)
	}
	if len(d.NewOrChanged) != 2 {
		t.Fatalf("newOrChanged=%d, want 2", len(d.NewOrChanged))
	}
	if len(d.NewOrChangedDetails) != 2 {
		t.Fatalf("newOrChangedDetails=%d, want 2", len(d.NewOrChangedDetails))
	}

	// Find r2 detail.
	var r2 *DeltaDetail
	for i := range d.NewOrChangedDetails {
		x := d.NewOrChangedDetails[i]
		if x.Current != nil && x.Current.RuleID == "r2" {
			r2 = &x
			break
		}
	}
	if r2 == nil {
		t.Fatalf("missing r2 detail")
	}
	if r2.Kind != "changed" {
		t.Fatalf("r2 kind=%q, want %q", r2.Kind, "changed")
	}
	foundObserved := false
	for _, c := range r2.Changes {
		if c == "observed" {
			foundObserved = true
		}
	}
	if !foundObserved {
		t.Fatalf("r2 changes=%v, want includes observed", r2.Changes)
	}

	// Fixed: baseline had r2 but current also has r2 identity, so fixed should be empty.
	// baseline had no extra fp missing other than r2 fp2, but identity matches, so fixed slice may
	// still include fp2 depending on fingerprinting. Ensure fixedDetails is empty because identity exists.
	if len(d.FixedDetails) != 0 {
		t.Fatalf("fixedDetails=%d, want 0", len(d.FixedDetails))
	}
}
