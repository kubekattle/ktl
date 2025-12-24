package verify

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/open-policy-agent/opa/rego"
)

type regoResult struct {
	DocumentID       string `json:"documentId"`
	ResourceType     string `json:"resourceType"`
	ResourceName     string `json:"resourceName"`
	SearchKey        string `json:"searchKey"`
	IssueType        string `json:"issueType"`
	KeyExpectedValue string `json:"keyExpectedValue"`
	KeyActualValue   string `json:"keyActualValue"`
	SearchLine       string `json:"searchLine"`
	SearchValue      string `json:"searchValue"`
	Description      string `json:"description"`
	Platform         string `json:"platform"`
	CloudProvider    string `json:"cloudProvider"`
	Framework        string `json:"framework"`
	FrameworkVersion string `json:"frameworkVersion"`
}

func EvaluateRules(ctx context.Context, rules Ruleset, objects []map[string]any, commonDir string) ([]Finding, error) {
	if len(rules.Rules) == 0 || len(objects) == 0 {
		return nil, nil
	}
	inputDocs := make([]map[string]any, 0, len(objects))
	for i, obj := range objects {
		doc := map[string]any{
			"id":       fmt.Sprintf("doc-%d", i+1),
			"kind":     obj["kind"],
			"metadata": obj["metadata"],
			"spec":     obj["spec"],
		}
		// Some queries expect arbitrary keys under the document; keep the full object too.
		for k, v := range obj {
			if _, ok := doc[k]; ok {
				continue
			}
			doc[k] = v
		}
		inputDocs = append(inputDocs, doc)
	}
	input := map[string]any{"document": inputDocs}

	modules, err := loadRegoModules(commonDir)
	if err != nil {
		return nil, err
	}

	var findings []Finding
	for _, rule := range rules.Rules {
		ruleModules := make(map[string]string, len(modules)+8)
		for k, v := range modules {
			ruleModules[k] = v
		}
		ruleMod, err := os.ReadFile(filepath.Join(rule.Dir, "query.rego"))
		if err != nil {
			continue
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
			continue
		}
		if len(rs) == 0 || len(rs[0].Expressions) == 0 {
			continue
		}
		list, ok := rs[0].Expressions[0].Value.([]any)
		if !ok {
			continue
		}
		for _, entry := range list {
			m, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			msg := rule.Title
			if rule.Description != "" {
				msg = rule.Description
			}
			subj := Subject{}
			if v, ok := m["resourceType"]; ok && v != nil {
				subj.Kind = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
			if v, ok := m["resourceName"]; ok && v != nil {
				subj.Name = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
			loc := ""
			if v, ok := m["searchKey"]; ok && v != nil {
				loc = strings.TrimSpace(fmt.Sprintf("%v", v))
			}
			fp := rule.ID + ":" + subj.Kind + ":" + subj.Name + ":" + loc
			findings = append(findings, Finding{
				RuleID:      rule.ID,
				Severity:    rule.Severity,
				Category:    rule.Category,
				Message:     msg,
				Location:    loc,
				Subject:     subj,
				Fingerprint: fp,
				HelpURL:     rule.HelpURL,
			})
		}
	}
	return findings, nil
}

func loadRegoModules(dir string) (map[string]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("rego module dir is required")
	}
	var modules []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
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
	if err != nil {
		return nil, err
	}
	sort.Strings(modules)
	out := map[string]string{}
	for _, path := range modules {
		raw, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		name := filepath.ToSlash(strings.TrimPrefix(path, dir))
		name = strings.TrimPrefix(name, "/")
		out[name] = string(raw)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no .rego modules found under %s", dir)
	}
	return out, nil
}
