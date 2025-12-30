package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/verify"
	cfgpkg "github.com/example/ktl/internal/verify/config"
)

type Options struct {
	Kubeconfig  string
	KubeContext string
	LogLevel    *string
	Console     *verify.Console
	ErrOut      io.Writer
	Out         io.Writer
}

func Run(ctx context.Context, cfg *cfgpkg.Config, baseDir string, opts Options) error {
	if cfg == nil {
		return fmt.Errorf("config is required")
	}
	if err := cfg.Validate(baseDir); err != nil {
		return err
	}

	if strings.TrimSpace(cfg.Kube.Kubeconfig) != "" && strings.TrimSpace(opts.Kubeconfig) != "" {
		return fmt.Errorf("kubeconfig is set in both the YAML config and --kubeconfig; pick one")
	}
	if strings.TrimSpace(cfg.Kube.Context) != "" && strings.TrimSpace(opts.KubeContext) != "" {
		return fmt.Errorf("context is set in both the YAML config and --context; pick one")
	}

	effectiveKubeconfig := strings.TrimSpace(cfg.Kube.Kubeconfig)
	if effectiveKubeconfig == "" {
		effectiveKubeconfig = strings.TrimSpace(opts.Kubeconfig)
	}
	effectiveContext := strings.TrimSpace(cfg.Kube.Context)
	if effectiveContext == "" {
		effectiveContext = strings.TrimSpace(opts.KubeContext)
	}

	targetLabel := cfg.TargetLabel()
	modeValue := verify.Mode(strings.ToLower(strings.TrimSpace(cfg.Verify.Mode)))
	failOnValue := verify.Severity(strings.ToLower(strings.TrimSpace(cfg.Verify.FailOn)))

	errOut := opts.ErrOut
	if errOut == nil {
		errOut = io.Discard
	}

	console := opts.Console
	finishConsole := func() {
		if console == nil {
			return
		}
		console.Done()
		console = nil
	}
	if console != nil {
		phase := cfg.StartPhase()
		if phase != "" {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Target: targetLabel, Phase: phase})
		}
	}

	var emit verify.Emitter
	if console != nil {
		emit = func(ev verify.Event) error {
			console.Observe(ev)
			return nil
		}
	}

	objs, renderedManifest, err := cfg.LoadObjects(ctx, baseDir, effectiveKubeconfig, effectiveContext, console)
	if err != nil {
		finishConsole()
		return err
	}

	rulesDir := strings.TrimSpace(cfg.Verify.RulesDir)
	if rulesDir == "" {
		rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
	}
	options := verify.Options{
		Mode:     modeValue,
		FailOn:   failOnValue,
		Format:   verify.OutputFormat(strings.ToLower(strings.TrimSpace(cfg.Output.Format))),
		RulesDir: rulesDir,
	}
	if console != nil {
		console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
	}
	rep, err := verify.VerifyObjectsWithEmitter(ctx, targetLabel, objs, options, emit)
	if err != nil {
		finishConsole()
		return err
	}
	cfg.AppendInputs(rep, renderedManifest)

	if strings.TrimSpace(cfg.Verify.Policy.Ref) != "" {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "policy", PolicyRef: strings.TrimSpace(cfg.Verify.Policy.Ref), PolicyMode: strings.TrimSpace(cfg.Verify.Policy.Mode)})
		}
		pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: cfg.Verify.Policy.Ref, Mode: cfg.Verify.Policy.Mode}, objs)
		if err != nil {
			finishConsole()
			return err
		}
		policyFindings := verify.PolicyReportToFindings(pol)
		rep.Findings = append(rep.Findings, policyFindings...)
		if console != nil {
			for i := range policyFindings {
				f := policyFindings[i]
				console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
			}
		}
		if strings.EqualFold(strings.TrimSpace(cfg.Verify.Policy.Mode), "enforce") && pol != nil && pol.DenyCount > 0 {
			rep.Blocked = true
			rep.Passed = false
		}
	}

	if strings.TrimSpace(cfg.Verify.Baseline.Read) != "" {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "baseline"})
		}
		base, err := verify.LoadReport(cfg.Verify.Baseline.Read)
		if err != nil {
			finishConsole()
			return err
		}
		delta := verify.ComputeDelta(rep, base)
		if cfg.Verify.Baseline.ExitOnDelta && len(delta.NewOrChanged) > 0 {
			rep.Blocked = true
			rep.Passed = false
		}
		rep.Findings = delta.NewOrChanged
		rep.Summary = verify.Summary{Total: len(rep.Findings), BySev: map[verify.Severity]int{}}
		for _, f := range rep.Findings {
			rep.Summary.BySev[f.Severity]++
		}
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventReset, When: time.Now().UTC()})
			for i := range rep.Findings {
				f := rep.Findings[i]
				console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
			}
			s := rep.Summary
			console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
		}
	}

	if cfg.Verify.Exposure.Enabled {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "exposure"})
		}
		ex := verify.AnalyzeExposure(objs)
		rep.Exposure = &ex
		if strings.TrimSpace(cfg.Verify.Exposure.Output) != "" {
			if w, c, err := cfgpkg.OpenOutput(errOut, cfg.Verify.Exposure.Output); err == nil {
				_ = verify.WriteExposureJSON(w, &ex)
				if c != nil {
					_ = c.Close()
				}
			}
		}
	}

	if strings.TrimSpace(cfg.Verify.Baseline.Write) != "" {
		if w, c, err := cfgpkg.OpenOutput(errOut, cfg.Verify.Baseline.Write); err == nil {
			_ = verify.WriteReport(w, rep, verify.OutputJSON)
			if c != nil {
				_ = c.Close()
			}
		}
	}

	if console != nil {
		console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "write"})
		s := rep.Summary
		console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
		console.Observe(verify.Event{Type: verify.EventDone, When: time.Now().UTC(), Passed: rep.Passed, Blocked: rep.Blocked})
	}
	finishConsole()

	out := opts.Out
	if out == nil {
		out = io.Discard
	}
	if err := verify.WriteReport(out, rep, cfgpkg.RepModeFormat(cfg.Output.Format)); err != nil {
		return err
	}
	if cfg.Verify.FixPlan && (strings.TrimSpace(cfg.Output.Report) == "" || strings.TrimSpace(cfg.Output.Report) == "-") && cfgpkg.RepModeFormat(cfg.Output.Format) == verify.OutputTable {
		plan := verify.BuildFixPlan(rep.Findings)
		if text := verify.RenderFixPlanText(plan); text != "" {
			fmt.Fprint(out, text)
		}
	}
	if rep.Blocked {
		return fmt.Errorf("verify blocked (fail-on=%s)", cfg.Verify.FailOn)
	}
	return nil
}
