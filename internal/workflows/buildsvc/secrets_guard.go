package buildsvc

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/example/ktl/internal/secrets"
)

type secretsGuard struct {
	mode       secrets.Mode
	reportPath string
	rules      secrets.CompiledRules
}

func newSecretsGuard(ctx context.Context, mode, reportPath, attestDir, configRef string) (*secretsGuard, error) {
	m := secrets.ModeWarn
	switch strings.ToLower(strings.TrimSpace(mode)) {
	case string(secrets.ModeOff):
		m = secrets.ModeOff
	case string(secrets.ModeBlock):
		m = secrets.ModeBlock
	case string(secrets.ModeWarn), "":
		m = secrets.ModeWarn
	}
	reportPath = strings.TrimSpace(reportPath)
	if reportPath == "" {
		reportPath = secrets.DefaultReportPath(attestDir)
	}
	base := secrets.DefaultConfig()
	override, err := secrets.LoadConfig(ctx, configRef)
	if err != nil {
		return nil, err
	}
	merged := secrets.MergeConfig(base, override)
	compiled, err := secrets.CompileConfig(merged)
	if err != nil {
		return nil, err
	}
	return &secretsGuard{mode: m, reportPath: reportPath, rules: compiled}, nil
}

func (g *secretsGuard) preflightBuildArgs(errOut io.Writer, buildArgs []string) (*secrets.Report, error) {
	if g == nil || g.mode == secrets.ModeOff {
		return nil, nil
	}
	findings := secrets.DetectBuildArgsWithRules(buildArgs, g.rules)
	rep := &secrets.Report{
		Mode:        g.mode,
		Findings:    findings,
		EvaluatedAt: time.Now().UTC(),
	}
	blocked := false
	for _, f := range findings {
		if f.Severity == secrets.SeverityBlock {
			blocked = true
			break
		}
	}
	rep.Blocked = blocked && g.mode == secrets.ModeBlock
	rep.Passed = !rep.Blocked
	if g.reportPath != "" {
		_ = secrets.WriteReport(g.reportPath, rep)
	}
	if err := g.printSummary(errOut, rep, "pre", 10); err != nil {
		return rep, err
	}
	return rep, nil
}

func (g *secretsGuard) postScanOCI(errOut io.Writer, ociLayoutDir string) (*secrets.Report, error) {
	if g == nil || g.mode == secrets.ModeOff {
		return nil, nil
	}
	findings, err := secrets.ScanOCIForSecretsWithRules(ociLayoutDir, 0, g.rules)
	if err != nil {
		return nil, err
	}
	rep := &secrets.Report{
		Mode:        g.mode,
		Findings:    findings,
		EvaluatedAt: time.Now().UTC(),
	}
	blocked := false
	for _, f := range findings {
		if f.Severity == secrets.SeverityBlock {
			blocked = true
			break
		}
	}
	rep.Blocked = blocked && g.mode == secrets.ModeBlock
	rep.Passed = !rep.Blocked
	if g.reportPath != "" {
		_ = secrets.WriteReport(g.reportPath, rep)
	}
	if err := g.printSummary(errOut, rep, "post", 10); err != nil {
		return rep, err
	}
	return rep, nil
}

func (g *secretsGuard) printSummary(errOut io.Writer, rep *secrets.Report, phase string, max int) error {
	if g == nil || rep == nil || errOut == nil {
		return nil
	}
	if len(rep.Findings) == 0 {
		return nil
	}
	if g.reportPath != "" {
		fmt.Fprintf(errOut, "Secrets report: %s\n", g.reportPath)
	}
	if max <= 0 {
		max = 10
	}
	findings := append([]secrets.Finding(nil), rep.Findings...)
	sort.Slice(findings, func(i, j int) bool { return findings[i].Rule < findings[j].Rule })
	fmt.Fprintf(errOut, "Secrets guardrails (%s): %d finding(s)\n", phase, len(findings))
	for i := 0; i < len(findings) && i < max; i++ {
		f := findings[i]
		line := strings.TrimSpace(f.Message)
		if f.Key != "" {
			line = fmt.Sprintf("%s (key=%s)", line, f.Key)
		}
		if f.Location != "" {
			line = fmt.Sprintf("%s (at=%s)", line, f.Location)
		}
		if f.Match != "" {
			line = fmt.Sprintf("%s (match=%s)", line, f.Match)
		}
		fmt.Fprintf(errOut, "  - [%s] %s\n", strings.ToUpper(string(f.Severity)), line)
	}
	if rep.Blocked {
		return fmt.Errorf("secrets guardrails blocked the build (%s)", phase)
	}
	return nil
}
