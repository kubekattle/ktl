package policy

import "strings"

const (
	APIVersion   = "torque.dev/ops/policy/v1alpha1"
	DecisionKind = "MutationPolicyDecision"
)

type Mode string

const (
	ModeAutomatic   Mode = "automatic"
	ModeGuarded     Mode = "guarded"
	ModeManual      Mode = "manual"
	ModeObserveOnly Mode = "observe-only"
	ModeUnsafe      Mode = "unsafe"
)

type Request struct {
	Mode            Mode   `json:"mode"`
	Operation       string `json:"operation,omitempty"`
	Adapter         string `json:"adapter,omitempty"`
	TargetID        string `json:"targetId,omitempty"`
	Mutating        bool   `json:"mutating"`
	Unsafe          bool   `json:"unsafe"`
	Approved        bool   `json:"approved"`
	ProofGatePassed bool   `json:"proofGatePassed"`
	AllowUnsafe     bool   `json:"allowUnsafe"`
	LocalExperiment bool   `json:"localExperiment"`
}

type Decision struct {
	APIVersion string   `json:"apiVersion"`
	Kind       string   `json:"kind"`
	Mode       Mode     `json:"mode"`
	Decision   string   `json:"decision"`
	Reason     string   `json:"reason"`
	Operation  string   `json:"operation,omitempty"`
	Adapter    string   `json:"adapter,omitempty"`
	TargetID   string   `json:"targetId,omitempty"`
	Mutating   bool     `json:"mutating"`
	Unsafe     bool     `json:"unsafe"`
	Required   []string `json:"required,omitempty"`
}

func Evaluate(req Request) Decision {
	mode := normalizeMode(req.Mode)
	decision := Decision{
		APIVersion: APIVersion,
		Kind:       DecisionKind,
		Mode:       mode,
		Operation:  strings.TrimSpace(req.Operation),
		Adapter:    strings.TrimSpace(req.Adapter),
		TargetID:   strings.TrimSpace(req.TargetID),
		Mutating:   req.Mutating,
		Unsafe:     req.Unsafe,
	}
	if req.Unsafe {
		if mode == ModeUnsafe && req.AllowUnsafe && req.LocalExperiment {
			decision.Decision = "allow"
			decision.Reason = "unsafe operation explicitly allowed for a local experiment"
			return decision
		}
		decision.Decision = "block"
		decision.Reason = "unsafe operation is blocked by policy"
		decision.Required = []string{"allow-unsafe", "local-experiment"}
		return decision
	}
	if !req.Mutating {
		decision.Decision = "allow"
		decision.Reason = "observe-only operation"
		return decision
	}
	switch mode {
	case ModeObserveOnly:
		decision.Decision = "block"
		decision.Reason = "observe-only policy forbids mutation"
	case ModeManual:
		decision.Decision = "manual"
		decision.Reason = "manual policy requires an operator-run foreground action"
		decision.Required = []string{"operator"}
	case ModeGuarded:
		if req.Approved || req.ProofGatePassed {
			decision.Decision = "allow"
			decision.Reason = "guarded policy satisfied"
		} else {
			decision.Decision = "approval-required"
			decision.Reason = "guarded policy requires approval or a passed proof gate"
			decision.Required = []string{"approval", "proof-gate"}
		}
	case ModeAutomatic:
		decision.Decision = "allow"
		decision.Reason = "automatic policy allows non-unsafe mutation"
	case ModeUnsafe:
		decision.Decision = "allow"
		decision.Reason = "unsafe mode allows non-unsafe mutation"
	default:
		decision.Decision = "block"
		decision.Reason = "unknown mutation policy mode"
	}
	return decision
}

func normalizeMode(mode Mode) Mode {
	switch Mode(strings.ToLower(strings.TrimSpace(string(mode)))) {
	case ModeAutomatic:
		return ModeAutomatic
	case ModeGuarded:
		return ModeGuarded
	case ModeManual:
		return ModeManual
	case ModeObserveOnly:
		return ModeObserveOnly
	case ModeUnsafe:
		return ModeUnsafe
	default:
		if strings.TrimSpace(string(mode)) == "" {
			return ModeGuarded
		}
		return Mode(strings.ToLower(strings.TrimSpace(string(mode))))
	}
}
