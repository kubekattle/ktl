package buildsvc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/policy"
)

type policyGate struct {
	ref        string
	mode       policy.Mode
	reportPath string
	bundle     *policy.Bundle
}

func newPolicyGate(ctx context.Context, ref, mode, reportPath, attestDir string) (*policyGate, error) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil, nil
	}
	bundle, err := policy.LoadBundle(ctx, ref)
	if err != nil {
		return nil, err
	}
	m := policy.ModeEnforce
	if strings.EqualFold(strings.TrimSpace(mode), string(policy.ModeWarn)) {
		m = policy.ModeWarn
	}
	reportPath = strings.TrimSpace(reportPath)
	if reportPath == "" {
		reportPath = policy.DefaultReportPath(attestDir)
	}
	return &policyGate{
		ref:        ref,
		mode:       m,
		reportPath: reportPath,
		bundle:     bundle,
	}, nil
}

func (g *policyGate) eval(ctx context.Context, in policy.BuildInput) (*policy.Report, error) {
	if g == nil {
		return nil, nil
	}
	rep, err := policy.Evaluate(ctx, g.bundle, in)
	if err != nil {
		return nil, err
	}
	rep.PolicyRef = g.ref
	rep.Mode = g.mode
	if g.reportPath != "" {
		_ = policy.WriteReport(g.reportPath, rep)
	}
	return rep, nil
}

func (g *policyGate) enforceOrWarn(errOut ioStringWriter, rep *policy.Report, phase string, max int) error {
	if g == nil || rep == nil {
		return nil
	}
	rep.Passed = rep.DenyCount == 0
	if rep.DenyCount == 0 && rep.WarnCount == 0 {
		return nil
	}
	if max <= 0 {
		max = 10
	}
	format := func(v policy.Violation) string {
		msg := strings.TrimSpace(v.Message)
		if v.Code != "" {
			msg = fmt.Sprintf("%s (%s)", msg, v.Code)
		}
		if v.Subject != "" {
			msg = fmt.Sprintf("%s [subject=%s]", msg, v.Subject)
		}
		if v.Path != "" {
			msg = fmt.Sprintf("%s [path=%s]", msg, v.Path)
		}
		return msg
	}
	deny := append([]policy.Violation(nil), rep.Deny...)
	warn := append([]policy.Violation(nil), rep.Warn...)
	sort.Slice(deny, func(i, j int) bool { return deny[i].Message < deny[j].Message })
	sort.Slice(warn, func(i, j int) bool { return warn[i].Message < warn[j].Message })

	if rep.WarnCount > 0 {
		fmt.Fprintf(errOut, "Policy warnings (%s): %d\n", phase, rep.WarnCount)
		for i := 0; i < len(warn) && i < max; i++ {
			fmt.Fprintf(errOut, "  - %s\n", format(warn[i]))
		}
	}
	if rep.DenyCount == 0 {
		return nil
	}
	fmt.Fprintf(errOut, "Policy violations (%s): %d\n", phase, rep.DenyCount)
	for i := 0; i < len(deny) && i < max; i++ {
		fmt.Fprintf(errOut, "  - %s\n", format(deny[i]))
	}
	if g.mode == policy.ModeWarn {
		return nil
	}
	return fmt.Errorf("policy gate failed (%s)", phase)
}

type ioStringWriter interface {
	Write([]byte) (int, error)
}

func loadAttestDirFiles(attestDir string) ([]string, error) {
	attestDir = strings.TrimSpace(attestDir)
	if attestDir == "" {
		return nil, nil
	}
	var files []string
	err := filepath.WalkDir(attestDir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.HasSuffix(strings.ToLower(d.Name()), ".json") {
			files = append(files, path)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return files, nil
}

func tryReadExternalFetches(attestDir string) json.RawMessage {
	path := filepath.Join(strings.TrimSpace(attestDir), "ktl-external-fetches.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	raw = []byte(strings.TrimSpace(string(raw)))
	if len(raw) == 0 {
		return nil
	}
	return json.RawMessage(raw)
}

func buildPolicyInput(now time.Time, contextDir, digest string, tags []string, df dockerfileMeta, attestDir string) policy.BuildInput {
	files, _ := loadAttestDirFiles(attestDir)
	return policy.BuildInput{
		WhenUTC:  now.UTC(),
		Context:  contextDir,
		Digest:   strings.TrimSpace(digest),
		Tags:     append([]string(nil), tags...),
		Bases:    append([]string(nil), df.Bases...),
		Labels:   df.Labels,
		Files:    files,
		External: tryReadExternalFetches(attestDir),
	}
}
