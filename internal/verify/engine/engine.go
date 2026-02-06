package engine

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/telemetry"
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
	RulesPath   []string
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

	timer := telemetry.NewPhaseTimer()

	var emit verify.Emitter
	if console != nil {
		emit = func(ev verify.Event) error {
			console.Observe(ev)
			return nil
		}
	}

	var (
		objs             []map[string]any
		renderedManifest string
		apiStats         *kube.APIRequestStats
	)
	phaseName := cfg.StartPhase()
	if strings.TrimSpace(phaseName) == "" {
		phaseName = "load"
	}
	if err := timer.Track(phaseName, func() error {
		var err error
		objs, renderedManifest, apiStats, err = cfg.LoadObjects(ctx, baseDir, effectiveKubeconfig, effectiveContext, console)
		return err
	}); err != nil {
		finishConsole()
		return err
	}

	rulesDir := strings.TrimSpace(cfg.Verify.RulesDir)
	if rulesDir == "" {
		rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
	}
	extraRules := append([]string{}, cfg.Verify.RulesPath...)
	extraRules = append(extraRules, opts.RulesPath...)
	options := verify.Options{
		Mode:          modeValue,
		FailOn:        failOnValue,
		Format:        cfgpkg.RepModeFormat(cfg.Output.Format),
		RulesDir:      rulesDir,
		ExtraRules:    extraRules,
		Selectors:     cfg.Verify.Selectors,
		RuleSelectors: cfg.Verify.RuleSelectors,
	}
	if console != nil {
		console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "evaluate"})
	}
	var rep *verify.Report
	if err := timer.Track("evaluate", func() error {
		var err error
		rep, err = verify.VerifyObjectsWithEmitter(ctx, targetLabel, objs, options, emit)
		return err
	}); err != nil {
		finishConsole()
		return err
	}
	cfg.AppendInputs(rep, renderedManifest)

	if strings.TrimSpace(cfg.Verify.Policy.Ref) != "" {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "policy", PolicyRef: strings.TrimSpace(cfg.Verify.Policy.Ref), PolicyMode: strings.TrimSpace(cfg.Verify.Policy.Mode)})
		}
		if err := timer.Track("policy", func() error {
			pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: cfg.Verify.Policy.Ref, Mode: cfg.Verify.Policy.Mode}, objs)
			if err != nil {
				return err
			}
			policyFindings := verify.PolicyReportToFindings(pol)
			rep.Findings = append(rep.Findings, policyFindings...)
			rep.Summary = verify.BuildSummary(rep.Findings, rep.Blocked)
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
			rep.Summary = verify.BuildSummary(rep.Findings, rep.Blocked)
			return nil
		}); err != nil {
			finishConsole()
			return err
		}
	}

	if strings.TrimSpace(cfg.Verify.Baseline.Read) != "" {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "baseline"})
		}
		if err := timer.Track("baseline", func() error {
			base, err := verify.LoadReport(cfg.Verify.Baseline.Read)
			if err != nil {
				return err
			}
			delta := verify.ComputeDelta(rep, base)
			rep.Delta = &verify.DeltaReport{
				BaselineTotal: 0,
				Unchanged:     delta.Unchanged,
				NewOrChanged:  append([]verify.Finding(nil), delta.NewOrChanged...),
				Fixed:         append([]verify.Finding(nil), delta.Fixed...),
			}
			if base != nil {
				rep.Delta.BaselineTotal = base.Summary.Total
			}
			if cfg.Verify.Baseline.ExitOnDelta && len(delta.NewOrChanged) > 0 {
				rep.Blocked = true
				rep.Passed = false
			}
			rep.Findings = delta.NewOrChanged
			rep.Summary = verify.BuildSummary(rep.Findings, rep.Blocked)
			if console != nil {
				console.Observe(verify.Event{Type: verify.EventReset, When: time.Now().UTC()})
				for i := range rep.Findings {
					f := rep.Findings[i]
					console.Observe(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
				}
				s := rep.Summary
				console.Observe(verify.Event{Type: verify.EventSummary, When: time.Now().UTC(), Summary: &s})
			}
			return nil
		}); err != nil {
			finishConsole()
			return err
		}
	}

	if cfg.Verify.Exposure.Enabled {
		if console != nil {
			console.Observe(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "exposure"})
		}
		timer.TrackFunc("exposure", func() {
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
		})
	}

	if strings.TrimSpace(cfg.Verify.Baseline.Write) != "" {
		timer.TrackFunc("baseline-write", func() {
			if w, c, err := cfgpkg.OpenOutput(errOut, cfg.Verify.Baseline.Write); err == nil {
				_ = verify.WriteReport(w, rep, verify.OutputJSON)
				if c != nil {
					_ = c.Close()
				}
			}
		})
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
	telemetrySummary := telemetry.Summary{
		Total:  timer.Total(),
		Phases: timer.Snapshot(),
	}
	if apiStats != nil {
		metrics := apiStats.Snapshot()
		telemetrySummary.KubeRequests = metrics.Count
		telemetrySummary.KubeAvg = metrics.Avg()
		telemetrySummary.KubeMax = metrics.Max
	}
	if line := telemetrySummary.Line(); line != "" {
		fmt.Fprintln(errOut, line)
	}
	if rep.Blocked {
		return fmt.Errorf("verify blocked (fail-on=%s)", cfg.Verify.FailOn)
	}
	return nil
}
