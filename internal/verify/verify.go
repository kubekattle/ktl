package verify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/yaml"
)

type OutputFormat string

const (
	OutputTable OutputFormat = "table"
	OutputJSON  OutputFormat = "json"
	OutputSARIF OutputFormat = "sarif"
)

type Options struct {
	Mode       Mode
	FailOn     Severity
	Format     OutputFormat
	RulesDir   string
	AttestDir  string
	ReportPath string
}

func VerifyObjects(ctx context.Context, objects []map[string]any, opts Options) (*Report, error) {
	if opts.Mode == ModeOff {
		return &Report{Tool: "ktl-verify", Engine: EngineMeta{Name: "builtin"}, Mode: opts.Mode, Passed: true, EvaluatedAt: time.Now().UTC()}, nil
	}
	rules, err := LoadRuleset(opts.RulesDir)
	if err != nil {
		return nil, err
	}
	commonDir := strings.TrimSpace(opts.RulesDir)
	commonDir = strings.TrimSuffix(commonDir, "/")
	commonDir = strings.TrimSuffix(commonDir, "\\")
	commonDir = strings.TrimSpace(commonDir)
	commonDir = commonDir + "/common"

	findings, err := EvaluateRules(ctx, rules, objects, commonDir)
	if err != nil {
		return nil, err
	}
	findings = filterNamespacedFindings(findings)
	rep := &Report{
		Tool:        "ktl-verify",
		Engine:      EngineMeta{Name: "builtin", Ruleset: "pinned"},
		Mode:        opts.Mode,
		EvaluatedAt: time.Now().UTC(),
		Findings:    findings,
	}
	rep.Summary.BySev = map[Severity]int{}
	for _, f := range findings {
		rep.Summary.Total++
		rep.Summary.BySev[f.Severity]++
	}
	blocked := opts.Mode == ModeBlock && hasAtLeast(findings, opts.FailOn)
	rep.Blocked = blocked
	rep.Passed = !blocked
	rep.Summary.Blocked = rep.Blocked
	rep.Summary.Passed = rep.Passed
	return rep, nil
}

func DecodeK8SYAML(manifest string) ([]map[string]any, error) {
	dec := yaml.NewYAMLOrJSONDecoder(strings.NewReader(manifest), 4096)
	var out []map[string]any
	for {
		var obj map[string]any
		if err := dec.Decode(&obj); err != nil {
			if err == io.EOF {
				break
			}
			return out, err
		}
		if len(obj) == 0 {
			continue
		}
		out = append(out, obj)
	}
	return out, nil
}

func WriteReport(w io.Writer, rep *Report, format OutputFormat) error {
	if w == nil || rep == nil {
		return nil
	}
	switch format {
	case "", OutputTable:
		return writeTable(w, rep)
	case OutputJSON:
		raw, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			return err
		}
		_, _ = w.Write(append(raw, '\n'))
		return nil
	case OutputSARIF:
		// placeholder: wire SARIF once the finding schema is stable
		return fmt.Errorf("sarif output not implemented yet")
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeTable(w io.Writer, rep *Report) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Findings: %d (blocked=%v)\n", rep.Summary.Total, rep.Blocked)
	if len(rep.Findings) == 0 {
		_, _ = w.Write(b.Bytes())
		return nil
	}
	sort.Slice(rep.Findings, func(i, j int) bool {
		if rep.Findings[i].Severity != rep.Findings[j].Severity {
			return rep.Findings[i].Severity < rep.Findings[j].Severity
		}
		return rep.Findings[i].RuleID < rep.Findings[j].RuleID
	})
	for _, f := range rep.Findings {
		fmt.Fprintf(&b, "- [%s] %s (%s/%s)\n", strings.ToUpper(string(f.Severity)), f.RuleID, f.Subject.Kind, f.Subject.Name)
	}
	_, _ = w.Write(b.Bytes())
	return nil
}

func hasAtLeast(findings []Finding, min Severity) bool {
	order := map[Severity]int{SeverityInfo: 0, SeverityLow: 1, SeverityMedium: 2, SeverityHigh: 3, SeverityCritical: 4}
	minN := order[min]
	for _, f := range findings {
		if order[f.Severity] >= minN {
			return true
		}
	}
	return false
}

func filterNamespacedFindings(findings []Finding) []Finding {
	// KICS-style rules are mostly workload-focused, but enforce the "namespaced-only"
	// scope by dropping findings that clearly reference cluster-scoped kinds.
	clusterKinds := map[string]bool{
		"Namespace":                      true,
		"ClusterRole":                    true,
		"ClusterRoleBinding":             true,
		"CustomResourceDefinition":       true,
		"MutatingWebhookConfiguration":   true,
		"ValidatingWebhookConfiguration": true,
		"StorageClass":                   true,
		"PriorityClass":                  true,
	}
	out := make([]Finding, 0, len(findings))
	for _, f := range findings {
		if clusterKinds[f.Subject.Kind] {
			continue
		}
		out = append(out, f)
	}
	return out
}
