package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"time"

	"github.com/example/ktl/internal/appconfig"
	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/verify"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
)

type verifyService struct {
	apiv1.UnimplementedVerifyServiceServer
	cfg Config
}

func newVerifyService(cfg Config) *verifyService {
	return &verifyService{cfg: cfg}
}

func (s *verifyService) Verify(req *apiv1.VerifyRequest, stream apiv1.VerifyService_VerifyServer) error {
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}
	ctx := stream.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	opts := req.GetOptions()
	mode := strings.ToLower(strings.TrimSpace(opts.GetMode()))
	if mode == "" {
		mode = "warn"
	}
	failOn := strings.ToLower(strings.TrimSpace(opts.GetFailOn()))
	if failOn == "" {
		failOn = "high"
	}
	format := strings.ToLower(strings.TrimSpace(opts.GetFormat()))
	if format == "" {
		format = "json"
	}
	rulesDir := strings.TrimSpace(opts.GetRulesDir())
	if rulesDir == "" {
		rulesDir = filepath.Join(appconfig.FindRepoRoot("."), "internal", "verify", "rules", "builtin")
	}

	var objects []map[string]any
	target := ""
	switch t := req.GetTarget().(type) {
	case *apiv1.VerifyRequest_Chart:
		target = "chart"
		chart := strings.TrimSpace(t.Chart.GetChart())
		release := strings.TrimSpace(t.Chart.GetRelease())
		namespace := strings.TrimSpace(t.Chart.GetNamespace())
		if chart == "" || release == "" {
			return status.Error(codes.InvalidArgument, "chart and release are required")
		}
		settings := cli.New()
		settings.KubeConfig = strings.TrimSpace(t.Chart.GetKubeconfigPath())
		settings.KubeContext = strings.TrimSpace(t.Chart.GetKubeContext())
		templateCfg := new(action.Configuration)
		if err := templateCfg.Init(settings.RESTClientGetter(), namespace, "", func(string, ...interface{}) {}); err != nil {
			return status.Errorf(codes.Internal, "init helm: %v", err)
		}
		res, err := deploy.RenderTemplate(ctx, templateCfg, settings, deploy.TemplateOptions{
			Chart:           chart,
			Version:         strings.TrimSpace(t.Chart.GetVersion()),
			ReleaseName:     release,
			Namespace:       namespace,
			ValuesFiles:     t.Chart.GetValuesFiles(),
			SetValues:       t.Chart.GetSetValues(),
			SetStringValues: t.Chart.GetSetStringValues(),
			SetFileValues:   t.Chart.GetSetFileValues(),
			IncludeCRDs:     true,
			UseCluster:      true,
		})
		if err != nil {
			return status.Errorf(codes.Internal, "render chart: %v", err)
		}
		objs, err := verify.DecodeK8SYAML(res.Manifest)
		if err != nil {
			return status.Errorf(codes.Internal, "decode manifest: %v", err)
		}
		objects = objs
	case *apiv1.VerifyRequest_Namespace:
		target = "namespace"
		ns := strings.TrimSpace(t.Namespace.GetNamespace())
		if ns == "" {
			return status.Error(codes.InvalidArgument, "namespace is required")
		}
		client, err := kube.New(ctx, strings.TrimSpace(t.Namespace.GetKubeconfigPath()), strings.TrimSpace(t.Namespace.GetKubeContext()))
		if err != nil {
			return status.Errorf(codes.Internal, "kube client: %v", err)
		}
		objs, err := collectNamespacedObjectsForAgent(ctx, client, ns)
		if err != nil {
			return status.Errorf(codes.Internal, "collect objects: %v", err)
		}
		objects = objs
	default:
		return status.Error(codes.InvalidArgument, "target is required")
	}

	policyRef := strings.TrimSpace(opts.GetPolicy())
	policyMode := strings.TrimSpace(opts.GetPolicyMode())
	if policyMode == "" {
		policyMode = "warn"
	}

	emit := func(ev verify.Event) error {
		now := time.Now().UTC()
		msg := &apiv1.VerifyEvent{TimestampUnixNano: now.UnixNano()}
		switch ev.Type {
		case verify.EventStarted:
			msg.Body = &apiv1.VerifyEvent_Started{Started: &apiv1.VerifyStarted{
				Target:     target,
				Ruleset:    ev.Ruleset,
				PolicyRef:  policyRef,
				PolicyMode: policyMode,
			}}
		case verify.EventProgress:
			counts := map[string]int32{}
			for k, v := range ev.Counts {
				counts[k] = int32(v)
			}
			msg.Body = &apiv1.VerifyEvent_Progress{Progress: &apiv1.VerifyProgress{
				Phase:        ev.Phase,
				CountsByKind: counts,
			}}
		case verify.EventFinding:
			if ev.Finding == nil {
				return nil
			}
			f := ev.Finding
			msg.Body = &apiv1.VerifyEvent_Finding{Finding: &apiv1.VerifyFinding{
				RuleId:   f.RuleID,
				Severity: string(f.Severity),
				Category: f.Category,
				Message:  f.Message,
				Location: f.Location,
				HelpUrl:  f.HelpURL,
				Subject: &apiv1.VerifySubject{
					Kind:      f.Subject.Kind,
					Namespace: f.Subject.Namespace,
					Name:      f.Subject.Name,
				},
			}}
		case verify.EventSummary:
			bySev := map[string]int32{}
			if ev.Summary != nil {
				for k, v := range ev.Summary.BySev {
					bySev[string(k)] = int32(v)
				}
			}
			total := int32(0)
			blocked := ev.Blocked
			if ev.Summary != nil {
				total = int32(ev.Summary.Total)
				blocked = ev.Summary.Blocked
			}
			msg.Body = &apiv1.VerifyEvent_Summary{Summary: &apiv1.VerifySummary{
				Total:      total,
				BySeverity: bySev,
				Blocked:    blocked,
			}}
		case verify.EventDone:
			msg.Body = &apiv1.VerifyEvent_Done{Done: &apiv1.VerifyDone{Passed: ev.Passed, Blocked: ev.Blocked}}
		default:
			return nil
		}
		return stream.Send(msg)
	}

	// Emit collect counts (cheap and useful).
	counts := map[string]int{}
	for _, obj := range objects {
		if kind, ok := obj["kind"].(string); ok && strings.TrimSpace(kind) != "" {
			counts[strings.TrimSpace(kind)]++
		}
	}
	_ = emit(verify.Event{Type: verify.EventProgress, When: time.Now().UTC(), Phase: "collect", Counts: counts})

	runner := verify.Runner{RulesDir: rulesDir}
	rep, err := runner.Verify(ctx, target, objects, verify.Options{
		Mode:     verify.Mode(mode),
		FailOn:   verify.Severity(failOn),
		Format:   verify.OutputFormat(format),
		RulesDir: rulesDir,
	}, emit)
	if err != nil {
		return status.Errorf(codes.Internal, "verify: %v", err)
	}

	if policyRef != "" {
		pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: policyRef, Mode: policyMode}, objects)
		if err != nil {
			return status.Errorf(codes.Internal, "policy: %v", err)
		}
		pfindings := verify.PolicyReportToFindings(pol)
		for i := range pfindings {
			f := pfindings[i]
			_ = emit(verify.Event{Type: verify.EventFinding, When: time.Now().UTC(), Finding: &f})
			rep.Findings = append(rep.Findings, f)
		}
		if strings.EqualFold(strings.TrimSpace(policyMode), "enforce") && pol != nil && pol.DenyCount > 0 {
			rep.Blocked = true
			rep.Passed = false
		}
	}

	// Optional full report payload (for clients that want one blob).
	raw, _ := json.Marshal(rep)
	_ = stream.Send(&apiv1.VerifyEvent{
		TimestampUnixNano: time.Now().UTC().UnixNano(),
		Body:              &apiv1.VerifyEvent_Json{Json: string(raw)},
	})
	return nil
}

func collectNamespacedObjectsForAgent(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	return verify.CollectNamespacedObjects(ctx, client, namespace)
}
