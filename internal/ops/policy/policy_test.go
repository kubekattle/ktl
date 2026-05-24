package policy

import "testing"

func TestEvaluateMutationModes(t *testing.T) {
	tests := []struct {
		name string
		req  Request
		want string
	}{
		{name: "observe only blocks mutation", req: Request{Mode: ModeObserveOnly, Mutating: true}, want: "block"},
		{name: "observe only allows observe", req: Request{Mode: ModeObserveOnly}, want: "allow"},
		{name: "manual requires operator", req: Request{Mode: ModeManual, Mutating: true}, want: "manual"},
		{name: "guarded needs gate", req: Request{Mode: ModeGuarded, Mutating: true}, want: "approval-required"},
		{name: "guarded allows approval", req: Request{Mode: ModeGuarded, Mutating: true, Approved: true}, want: "allow"},
		{name: "guarded allows proof gate", req: Request{Mode: ModeGuarded, Mutating: true, ProofGatePassed: true}, want: "allow"},
		{name: "automatic allows safe mutation", req: Request{Mode: ModeAutomatic, Mutating: true}, want: "allow"},
		{name: "empty mode defaults guarded", req: Request{Mutating: true}, want: "approval-required"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.req)
			if got.Decision != tt.want {
				t.Fatalf("Evaluate() decision = %q, want %q: %#v", got.Decision, tt.want, got)
			}
		})
	}
}

func TestEvaluateUnsafeRequiresExplicitLocalExperiment(t *testing.T) {
	blocked := Evaluate(Request{Mode: ModeAutomatic, Mutating: true, Unsafe: true})
	if blocked.Decision != "block" {
		t.Fatalf("unsafe automatic decision = %#v", blocked)
	}
	stillBlocked := Evaluate(Request{Mode: ModeUnsafe, Mutating: true, Unsafe: true, AllowUnsafe: true})
	if stillBlocked.Decision != "block" {
		t.Fatalf("unsafe without local experiment decision = %#v", stillBlocked)
	}
	allowed := Evaluate(Request{Mode: ModeUnsafe, Mutating: true, Unsafe: true, AllowUnsafe: true, LocalExperiment: true})
	if allowed.Decision != "allow" {
		t.Fatalf("explicit unsafe local experiment decision = %#v", allowed)
	}
}
