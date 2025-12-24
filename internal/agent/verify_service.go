package agent

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"

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
	switch t := req.GetTarget().(type) {
	case *apiv1.VerifyRequest_Chart:
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

	rep, err := verify.VerifyObjects(ctx, objects, verify.Options{
		Mode:     verify.Mode(mode),
		FailOn:   verify.Severity(failOn),
		Format:   verify.OutputFormat(format),
		RulesDir: rulesDir,
	})
	if err != nil {
		return status.Errorf(codes.Internal, "verify: %v", err)
	}

	if strings.TrimSpace(opts.GetPolicy()) != "" {
		pol, err := verify.EvaluatePolicy(ctx, verify.PolicyOptions{Ref: opts.GetPolicy(), Mode: opts.GetPolicyMode()}, objects)
		if err != nil {
			return status.Errorf(codes.Internal, "policy: %v", err)
		}
		rep.Findings = append(rep.Findings, verify.PolicyReportToFindings(pol)...)
		if strings.EqualFold(strings.TrimSpace(opts.GetPolicyMode()), "enforce") && pol != nil && pol.DenyCount > 0 {
			rep.Blocked = true
			rep.Passed = false
		}
	}

	// Stream a single JSON event for now; CLI/UI can render progressively later.
	raw, _ := json.Marshal(rep)
	return stream.Send(&apiv1.VerifyEvent{Json: string(raw)})
}

func collectNamespacedObjectsForAgent(ctx context.Context, client *kube.Client, namespace string) ([]map[string]any, error) {
	return verify.CollectNamespacedObjects(ctx, client, namespace)
}
