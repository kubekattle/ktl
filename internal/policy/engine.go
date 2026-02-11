package policy

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/open-policy-agent/opa/rego"
)

type Mode string

const (
	ModeEnforce Mode = "enforce"
	ModeWarn    Mode = "warn"
)

type BuildInput struct {
	WhenUTC  time.Time         `json:"whenUtc"`
	Context  string            `json:"context,omitempty"`
	Digest   string            `json:"digest,omitempty"`
	Tags     []string          `json:"tags,omitempty"`
	Bases    []string          `json:"bases,omitempty"`
	Labels   map[string]string `json:"labels,omitempty"`
	Files    []string          `json:"files,omitempty"`
	External json.RawMessage   `json:"external,omitempty"`
	Data     map[string]any    `json:"data,omitempty"`
}

type Violation struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Subject string `json:"subject,omitempty"`
}

type Report struct {
	PolicyRef   string      `json:"policyRef,omitempty"`
	Mode        Mode        `json:"mode"`
	Passed      bool        `json:"passed"`
	DenyCount   int         `json:"denyCount"`
	WarnCount   int         `json:"warnCount"`
	Deny        []Violation `json:"deny,omitempty"`
	Warn        []Violation `json:"warn,omitempty"`
	EvaluatedAt time.Time   `json:"evaluatedAt"`
}

func Evaluate(ctx context.Context, bundle *Bundle, input BuildInput) (*Report, error) {
	return EvaluateWithQuery(ctx, bundle, input, "data.ktl.build")
}

func EvaluateWithQuery(ctx context.Context, bundle *Bundle, input BuildInput, query string) (*Report, error) {
	if bundle == nil {
		return nil, errors.New("policy bundle is required")
	}
	input.Data = bundle.Data
	modules, err := loadRegoModules(bundle.Dir)
	if err != nil {
		return nil, err
	}
	query = strings.TrimSpace(query)
	if query == "" {
		query = "data.ktl.build"
	}
	opts := []func(*rego.Rego){
		rego.Query(query),
		rego.Input(input),
	}
	for name, src := range modules {
		opts = append(opts, rego.Module(name, src))
	}
	r := rego.New(opts...)
	rs, err := r.Eval(ctx)
	if err != nil {
		return nil, err
	}
	out := &Report{
		Mode:        ModeEnforce,
		EvaluatedAt: time.Now().UTC(),
	}
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return out, nil
	}
	obj, ok := rs[0].Expressions[0].Value.(map[string]any)
	if !ok {
		return out, nil
	}
	parseViolations := func(v any) []Violation {
		list, ok := v.([]any)
		if !ok {
			return nil
		}
		out := make([]Violation, 0, len(list))
		for _, entry := range list {
			switch t := entry.(type) {
			case string:
				out = append(out, Violation{Message: t})
			case map[string]any:
				viol := Violation{}
				if s, ok := t["message"].(string); ok {
					viol.Message = s
				}
				if s, ok := t["code"].(string); ok {
					viol.Code = s
				}
				if s, ok := t["path"].(string); ok {
					viol.Path = s
				}
				if s, ok := t["subject"].(string); ok {
					viol.Subject = s
				}
				if viol.Message == "" {
					viol.Message = fmt.Sprintf("%v", t)
				}
				out = append(out, viol)
			default:
				out = append(out, Violation{Message: fmt.Sprintf("%v", t)})
			}
		}
		return out
	}
	if deny, ok := obj["deny"]; ok {
		out.Deny = parseViolations(deny)
	}
	if warn, ok := obj["warn"]; ok {
		out.Warn = parseViolations(warn)
	}
	out.DenyCount = len(out.Deny)
	out.WarnCount = len(out.Warn)
	out.Passed = out.DenyCount == 0
	return out, nil
}

func loadRegoModules(dir string) (map[string]string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return nil, errors.New("policy dir is required")
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
