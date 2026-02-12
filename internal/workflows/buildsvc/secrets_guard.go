package buildsvc

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/compose-spec/compose-go/v2/loader"
	composetypes "github.com/compose-spec/compose-go/v2/types"
	"github.com/kubekattle/ktl/internal/secrets"
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

func (g *secretsGuard) preflight(errOut io.Writer, opts Options) (*secrets.Report, error) {
	if g == nil || g.mode == secrets.ModeOff {
		return nil, nil
	}
	findings := secrets.DetectBuildArgsWithRules(opts.BuildArgs, g.rules)

	switch strings.ToLower(strings.TrimSpace(opts.BuildMode)) {
	case "", string(ModeAuto), string(ModeDockerfile):
		dfFindings, _ := g.scanDockerfile(opts)
		findings = append(findings, dfFindings...)
	case string(ModeCompose):
		composeFindings, _ := g.scanCompose(opts)
		findings = append(findings, composeFindings...)
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

func (g *secretsGuard) scanDockerfile(opts Options) ([]secrets.Finding, error) {
	contextDir := strings.TrimSpace(opts.ContextDir)
	if contextDir == "" {
		contextDir = "."
	}
	dockerfile := strings.TrimSpace(opts.Dockerfile)
	if dockerfile == "" {
		dockerfile = filepath.Join(contextDir, "Dockerfile")
	}
	if _, err := os.Stat(dockerfile); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	return secrets.ScanDockerfileForSecretsWithRules(dockerfile, g.rules)
}

func (g *secretsGuard) scanCompose(opts Options) ([]secrets.Finding, error) {
	if len(opts.ComposeFiles) == 0 {
		return nil, nil
	}
	project, err := loadComposeProjectForSecrets(opts)
	if err != nil {
		return nil, err
	}
	allowedServices := map[string]bool{}
	if len(opts.ComposeServices) > 0 {
		for _, name := range opts.ComposeServices {
			name = strings.TrimSpace(name)
			if name != "" {
				allowedServices[name] = true
			}
		}
	}

	var findings []secrets.Finding
	for _, svc := range project.Services {
		if len(allowedServices) > 0 && !allowedServices[svc.Name] {
			continue
		}
		if svc.Build != nil {
			for key, value := range svc.Build.Args {
				loc := fmt.Sprintf("%s:service/%s:build.args", strings.Join(opts.ComposeFiles, ","), svc.Name)
				v := ""
				if value != nil {
					v = *value
				}
				if strings.TrimSpace(v) == "" {
					continue
				}
				findings = append(findings, secrets.MatchKeyValueWithRules(key, v, g.rules, secrets.SourceCompose, loc)...)
			}
		}
		for key, value := range svc.Environment {
			v := ""
			if value != nil {
				v = *value
			}
			if strings.TrimSpace(v) == "" {
				continue
			}
			loc := fmt.Sprintf("%s:service/%s:environment", strings.Join(opts.ComposeFiles, ","), svc.Name)
			findings = append(findings, secrets.MatchKeyValueWithRules(key, v, g.rules, secrets.SourceCompose, loc)...)
		}
	}
	return findings, nil
}

func loadComposeProjectForSecrets(opts Options) (*composetypes.Project, error) {
	env := make(composetypes.Mapping)
	for _, kv := range os.Environ() {
		key, value, ok := strings.Cut(kv, "=")
		if !ok {
			continue
		}
		env[key] = value
	}

	configFiles := make([]composetypes.ConfigFile, 0, len(opts.ComposeFiles))
	for _, path := range opts.ComposeFiles {
		path = strings.TrimSpace(path)
		if path == "" {
			return nil, errors.New("compose file path cannot be empty")
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read compose file %s: %w", path, err)
		}
		configFiles = append(configFiles, composetypes.ConfigFile{Filename: path, Content: data})
	}

	workingDir := filepath.Dir(opts.ComposeFiles[0])
	projectName := strings.TrimSpace(opts.ComposeProject)
	if projectName == "" {
		projectName = "ktl"
	}
	details := composetypes.ConfigDetails{
		WorkingDir:  workingDir,
		ConfigFiles: configFiles,
		Environment: env,
	}
	project, err := loader.Load(details, func(o *loader.Options) {
		o.SetProjectName(projectName, true)
		if len(opts.ComposeProfiles) > 0 {
			o.Profiles = append(o.Profiles, opts.ComposeProfiles...)
		}
	})
	if err != nil {
		return nil, err
	}
	return project, nil
}
