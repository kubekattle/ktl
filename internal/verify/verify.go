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
	OutputHTML  OutputFormat = "html"
	OutputMD    OutputFormat = "md"
)

type Options struct {
	Mode          Mode
	FailOn        Severity
	Format        OutputFormat
	RulesDir      string
	ExtraRules    []string
	Selectors     SelectorSet
	RuleSelectors []RuleSelector
	AttestDir     string
	ReportPath    string
	Now           func() time.Time
}

func VerifyObjects(ctx context.Context, objects []map[string]any, opts Options) (*Report, error) {
	return VerifyObjectsWithEmitter(ctx, "", objects, opts, nil)
}

func VerifyObjectsWithEmitter(ctx context.Context, target string, objects []map[string]any, opts Options, emit Emitter) (*Report, error) {
	runner := Runner{RulesDir: opts.RulesDir}
	return runner.Verify(ctx, target, objects, opts, emit)
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
		if err := validateSARIF(rep); err != nil {
			return err
		}
		raw, err := ToSARIF(rep)
		if err != nil {
			return err
		}
		_, _ = w.Write(append(raw, '\n'))
		return nil
	case OutputHTML:
		return writeHTML(w, rep)
	case OutputMD:
		return writeMarkdown(w, rep)
	default:
		return fmt.Errorf("unknown format %q", format)
	}
}

func writeTable(w io.Writer, rep *Report) error {
	var b bytes.Buffer
	fmt.Fprintf(&b, "Findings: %d (blocked=%v)\n", rep.Summary.Total, rep.Blocked)
	if strings.TrimSpace(string(rep.Mode)) != "" {
		fmt.Fprintf(&b, "Mode: %s\n", strings.TrimSpace(string(rep.Mode)))
	}
	if strings.TrimSpace(rep.Engine.Ruleset) != "" {
		fmt.Fprintf(&b, "Ruleset: %s\n", strings.TrimSpace(rep.Engine.Ruleset))
	}
	if len(rep.Findings) == 0 {
		_, _ = w.Write(b.Bytes())
		return nil
	}
	sort.Slice(rep.Findings, func(i, j int) bool {
		si, sj := rep.Findings[i].Severity, rep.Findings[j].Severity
		if si != sj {
			return severityRank(si) < severityRank(sj)
		}
		return rep.Findings[i].RuleID < rep.Findings[j].RuleID
	})
	for _, f := range rep.Findings {
		target := f.ResourceKey
		if target == "" {
			target = resourceKey(f.Subject)
		}
		if target == "" {
			target = strings.Trim(strings.TrimSpace(f.Subject.Kind)+" "+strings.TrimSpace(f.Subject.Name), " ")
		}
		if target == "" {
			target = "-"
		}
		msg := strings.TrimSpace(f.Message)
		msg = strings.ReplaceAll(msg, "\n", " ")
		msg = strings.Join(strings.Fields(msg), " ")
		if msg == "" {
			msg = strings.TrimSpace(f.RuleID)
		}
		if len(msg) > 140 {
			msg = msg[:137] + "..."
		}
		if msg != "" {
			fmt.Fprintf(&b, "- [%s] %s: %s (%s)\n", strings.ToUpper(string(f.Severity)), f.RuleID, msg, target)
		} else {
			fmt.Fprintf(&b, "- [%s] %s (%s)\n", strings.ToUpper(string(f.Severity)), f.RuleID, target)
		}
	}
	_, _ = w.Write(b.Bytes())
	return nil
}

func severityRank(sev Severity) int {
	switch sev {
	case SeverityCritical:
		return 0
	case SeverityHigh:
		return 1
	case SeverityMedium:
		return 2
	case SeverityLow:
		return 3
	default:
		return 4
	}
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

func nonEmpty(values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

func splitList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	// Support both comma and colon as list separators.
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ':' })
	var out []string
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func dedupeStrings(in []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(in))
	for _, v := range in {
		v = strings.TrimSpace(v)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}
