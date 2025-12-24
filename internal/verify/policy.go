package verify

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/example/ktl/internal/policy"
)

type PolicyOptions struct {
	Ref  string
	Mode string // warn|enforce (mapped by caller)
}

func EvaluatePolicy(ctx context.Context, opts PolicyOptions, objects []map[string]any) (*policy.Report, error) {
	ref := strings.TrimSpace(opts.Ref)
	if ref == "" {
		return nil, nil
	}
	bundle, err := policy.LoadBundle(ctx, ref)
	if err != nil {
		return nil, err
	}
	payload := map[string]any{
		"k8s": map[string]any{
			"objects": objects,
		},
	}
	raw, _ := json.Marshal(payload)
	rep, err := policy.EvaluateWithQuery(ctx, bundle, policy.BuildInput{
		WhenUTC:  time.Now().UTC(),
		Context:  "verify",
		External: raw,
	}, "data.ktl.verify")
	if err != nil {
		return nil, err
	}
	rep.PolicyRef = ref
	return rep, nil
}

func PolicyReportToFindings(rep *policy.Report) []Finding {
	if rep == nil {
		return nil
	}
	var out []Finding
	for _, v := range rep.Deny {
		out = append(out, Finding{
			RuleID:      nonEmpty("policy/"+strings.TrimSpace(v.Code), "policy/deny"),
			Severity:    SeverityHigh,
			Category:    "Policy",
			Message:     strings.TrimSpace(v.Message),
			Location:    strings.TrimSpace(v.Path),
			Fingerprint: strings.TrimSpace(v.Subject) + ":" + strings.TrimSpace(v.Path) + ":" + strings.TrimSpace(v.Message),
			Subject:     Subject{Name: strings.TrimSpace(v.Subject)},
		})
	}
	for _, v := range rep.Warn {
		out = append(out, Finding{
			RuleID:      nonEmpty("policy/"+strings.TrimSpace(v.Code), "policy/warn"),
			Severity:    SeverityMedium,
			Category:    "Policy",
			Message:     strings.TrimSpace(v.Message),
			Location:    strings.TrimSpace(v.Path),
			Fingerprint: strings.TrimSpace(v.Subject) + ":" + strings.TrimSpace(v.Path) + ":" + strings.TrimSpace(v.Message),
			Subject:     Subject{Name: strings.TrimSpace(v.Subject)},
		})
	}
	return out
}
