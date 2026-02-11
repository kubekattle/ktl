package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/open-policy-agent/opa/rego"
)

func EvaluateRules(ctx context.Context, rules Ruleset, objects []map[string]any, commonDirs []string) ([]Finding, error) {
	return EvaluateRulesWithSelectors(ctx, rules, objects, commonDirs, SelectorSet{}, nil)
}

func EvaluateRulesWithSelectors(ctx context.Context, rules Ruleset, objects []map[string]any, commonDirs []string, selectors SelectorSet, ruleSelectors []RuleSelector) ([]Finding, error) {
	if len(rules.Rules) == 0 || len(objects) == 0 {
		return nil, nil
	}
	infos := buildObjectInfos(objects)
	if len(infos) == 0 {
		return nil, nil
	}
	includeSel, err := compileSelector(selectors.Include)
	if err != nil {
		return nil, err
	}
	excludeSel, err := compileSelector(selectors.Exclude)
	if err != nil {
		return nil, err
	}
	eligible := make([]objectInfo, 0, len(infos))
	for _, info := range infos {
		if !selectorMatches(includeSel, info) {
			continue
		}
		if !excludeSel.empty && selectorMatches(excludeSel, info) {
			continue
		}
		eligible = append(eligible, info)
	}
	if len(eligible) == 0 {
		return nil, nil
	}
	ruleFilters, err := compileRuleSelectors(ruleSelectors)
	if err != nil {
		return nil, err
	}

	modules, err := loadRegoModulesDirs(commonDirs)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, rule := range rules.Rules {
		ruleInfos := eligible

		// Resource-level suppression:
		// - ktl.verify/ignore: "*" or comma/space-separated rule ids (e.g. "k8s/pss_restricted_profile")
		// This is checked per rule so users can suppress one noisy rule without disabling verify.
		filteredByIgnore := ruleInfos[:0]
		for _, info := range ruleInfos {
			if shouldIgnoreRule(info, rule.ID) {
				continue
			}
			filteredByIgnore = append(filteredByIgnore, info)
		}
		ruleInfos = filteredByIgnore
		if len(ruleInfos) == 0 {
			continue
		}

		if len(ruleFilters) > 0 {
			var matched []compiledRuleSelector
			for _, sel := range ruleFilters {
				if sel.pattern.MatchString(rule.ID) {
					matched = append(matched, sel)
				}
			}
			if len(matched) > 0 {
				filtered := make([]objectInfo, 0, len(ruleInfos))
				for _, info := range ruleInfos {
					if !selectorMatchesAll(info, matched) {
						continue
					}
					filtered = append(filtered, info)
				}
				ruleInfos = filtered
			}
		}
		if len(ruleInfos) == 0 {
			continue
		}

		inputDocs, docIndex := buildInputDocs(ruleInfos)
		input := map[string]any{"document": inputDocs}

		ruleModules := make(map[string]string, len(modules)+8)
		for k, v := range modules {
			ruleModules[k] = v
		}
		ruleMod, err := os.ReadFile(filepath.Join(rule.Dir, "query.rego"))
		if err != nil {
			return nil, fmt.Errorf("read rule %s: %w", rule.ID, err)
		}
		ruleModules["rule.rego"] = string(ruleMod)

		opts := []func(*rego.Rego){
			rego.Query("data.Cx.CxPolicy"),
			rego.Input(input),
		}
		names := make([]string, 0, len(ruleModules))
		for n := range ruleModules {
			names = append(names, n)
		}
		sort.Strings(names)
		for _, name := range names {
			opts = append(opts, rego.Module(name, ruleModules[name]))
		}
		r := rego.New(opts...)
		rs, err := r.Eval(ctx)
		if err != nil {
			return nil, fmt.Errorf("eval rule %s: %w", rule.ID, err)
		}
		if len(rs) == 0 || len(rs[0].Expressions) == 0 {
			return nil, fmt.Errorf("eval rule %s: missing expressions", rule.ID)
		}
		list, ok := rs[0].Expressions[0].Value.([]any)
		if !ok {
			return nil, fmt.Errorf("eval rule %s: unexpected result type %T", rule.ID, rs[0].Expressions[0].Value)
		}
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			msg := strings.TrimSpace(rule.Description)
			if msg == "" {
				msg = strings.TrimSpace(rule.Title)
			}
			if v := firstString(m, "message", "msg", "description"); v != "" {
				msg = v
			}
			subj := Subject{}
			docID := strings.TrimSpace(firstString(m, "documentId"))
			var obj map[string]any
			if docID != "" {
				if base, ok := docIndex[docID]; ok {
					subj = base.subject
					obj = base.obj
				}
			}
			if v := firstString(m, "resourceType"); v != "" {
				subj.Kind = v
			}
			if v := firstString(m, "resourceName"); v != "" {
				subj.Name = v
			}
			if v := firstString(m, "resourceNamespace"); v != "" {
				subj.Namespace = v
			}

			loc := strings.TrimSpace(firstString(m, "searchKey"))
			fieldPath, _ := parseSearchLine(m["searchLine"])
			expected := strings.TrimSpace(firstString(m, "keyExpectedValue", "expected", "expectedValue"))
			observed := strings.TrimSpace(firstString(m, "keyActualValue", "actual", "actualValue", "observed"))
			key := resourceKey(subj)
			fp := rule.ID + ":" + key
			if loc != "" {
				fp += ":" + loc
			}

			findings = append(findings, Finding{
				RuleID:      rule.ID,
				Severity:    rule.Severity,
				Category:    rule.Category,
				Message:     msg,
				FieldPath:   strings.TrimSpace(fieldPath),
				Path:        "",
				Line:        0,
				Location:    loc,
				ResourceKey: key,
				Expected:    expected,
				Observed:    observed,
				Subject:     subj,
				Fingerprint: fp,
				HelpURL:     rule.HelpURL,
				Evidence:    buildEvidenceWithExtras(obj, fieldPath, m),
			})
		}
	}
	return findings, nil
}

func buildEvidenceWithExtras(obj map[string]any, fieldPath string, result map[string]any) map[string]any {
	ev := buildEvidence(obj, fieldPath)
	if ev == nil {
		ev = map[string]any{}
	}
	if result == nil {
		return ev
	}
	// Bundle rules can attach structured metadata to improve UX.
	if v, ok := result["ktlChecksFailed"]; ok && v != nil {
		switch typed := v.(type) {
		case []string:
			ev["checksFailed"] = typed
		case []any:
			var out []string
			for _, it := range typed {
				s := strings.TrimSpace(fmt.Sprintf("%v", it))
				if s != "" {
					out = append(out, s)
				}
			}
			if len(out) > 0 {
				ev["checksFailed"] = out
			}
		default:
			ev["checksFailed"] = fmt.Sprintf("%v", v)
		}
	}
	if v, ok := result["ktlContainerFailures"]; ok && v != nil {
		ev["containerFailures"] = v
	}
	return ev
}

func selectorMatchesAll(info objectInfo, selectors []compiledRuleSelector) bool {
	for _, sel := range selectors {
		if !selectorMatches(sel.include, info) {
			return false
		}
		if !sel.exclude.empty && selectorMatches(sel.exclude, info) {
			return false
		}
	}
	return true
}

func buildInputDocs(infos []objectInfo) ([]map[string]any, map[string]objectInfo) {
	inputDocs := make([]map[string]any, 0, len(infos))
	docIndex := map[string]objectInfo{}
	for i, info := range infos {
		docID := fmt.Sprintf("doc-%d", i+1)
		obj := info.obj
		ktl := map[string]any{}
		if len(info.annotations) > 0 {
			if raw := strings.TrimSpace(info.annotations["ktl.verify/ignore-checks"]); raw != "" {
				ktl["ignoreChecks"] = splitCSV(raw)
			}
		}
		doc := map[string]any{
			"id":       docID,
			"kind":     obj["kind"],
			"metadata": obj["metadata"],
			"spec":     obj["spec"],
			"ktl":      ktl,
		}
		// Some queries expect arbitrary keys under the document; keep the full object too.
		for k, v := range obj {
			if _, ok := doc[k]; ok {
				continue
			}
			doc[k] = v
		}
		docIndex[docID] = info
		inputDocs = append(inputDocs, doc)
	}
	return inputDocs, docIndex
}

func splitCSV(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool { return r == ',' || r == ' ' || r == '\t' || r == '\n' })
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if s := strings.TrimSpace(f); s != "" {
			out = append(out, s)
		}
	}
	return out
}

func shouldIgnoreRule(info objectInfo, ruleID string) bool {
	if len(info.annotations) == 0 {
		return false
	}
	raw := strings.TrimSpace(info.annotations["ktl.verify/ignore"])
	if raw == "" {
		return false
	}
	for _, tok := range splitCSV(raw) {
		if tok == "*" {
			return true
		}
		if strings.EqualFold(strings.TrimSpace(tok), strings.TrimSpace(ruleID)) {
			return true
		}
	}
	return false
}

func firstString(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if v, ok := m[key]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}

func parseSearchLine(raw interface{}) (string, int) {
	switch typed := raw.(type) {
	case int:
		return "", typed
	case int64:
		return "", int(typed)
	case float64:
		return "", int(typed)
	case string:
		return strings.TrimSpace(typed), 0
	case []string:
		return joinPathParts(typed), 0
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			part := strings.TrimSpace(fmt.Sprintf("%v", item))
			if part != "" {
				parts = append(parts, part)
			}
		}
		return joinPathParts(parts), 0
	default:
		return "", 0
	}
}

func joinPathParts(parts []string) string {
	if len(parts) == 0 {
		return ""
	}
	var out []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	if len(out) == 0 {
		return ""
	}
	return strings.Join(out, ".")
}

var regoModuleCache sync.Map // key -> map[string]string

func loadRegoModulesDirs(dirs []string) (map[string]string, error) {
	if len(dirs) == 0 {
		return nil, errors.New("rego module dir is required")
	}
	normalized := dedupeStrings(dirs)
	if len(normalized) == 0 {
		return nil, errors.New("rego module dir is required")
	}
	key := strings.Join(normalized, "|")
	if cached, ok := regoModuleCache.Load(key); ok {
		// modules maps are treated read-only by callers.
		return cached.(map[string]string), nil
	}

	out := map[string]string{}
	for _, dir := range normalized {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		var modules []string
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			if strings.HasSuffix(strings.ToLower(d.Name()), ".rego") {
				modules = append(modules, path)
			}
			return nil
		})
		sort.Strings(modules)
		for _, path := range modules {
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil, err
			}
			name := filepath.ToSlash(strings.TrimPrefix(path, dir))
			name = strings.TrimPrefix(name, "/")
			out[name] = string(raw)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no .rego modules found under %s", strings.Join(normalized, ","))
	}
	regoModuleCache.Store(key, out)
	return out, nil
}
