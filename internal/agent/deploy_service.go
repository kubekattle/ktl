// File: internal/agent/deploy_service.go
// Brief: Internal agent package implementation for 'deploy service'.

// Package agent provides agent helpers.

package agent

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/go-logr/logr"
	"helm.sh/helm/v3/pkg/action"
	"helm.sh/helm/v3/pkg/cli"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	"github.com/kubekattle/ktl/internal/api/convert"
	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/kube"
	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
)

// DeployServer exposes deploy apply/destroy workflows over gRPC.
type DeployServer struct {
	apiv1.UnimplementedDeployServiceServer
	Logger logr.Logger
	Mirror *MirrorServer
}

// Apply runs the Helm upgrade/install workflow remotely.
func (s *DeployServer) Apply(req *apiv1.DeployApplyRequest, stream apiv1.DeployService_ApplyServer) (retErr error) {
	if req == nil || req.GetOptions() == nil {
		return fmt.Errorf("deploy options are required")
	}
	cfg := convert.DeployApplyFromProto(req.GetOptions())
	ctx := stream.Context()
	sessionID := strings.TrimSpace(req.GetSessionId())
	producer := "deploy"
	if strings.TrimSpace(req.GetRequester()) != "" {
		producer = "deploy:" + strings.TrimSpace(req.GetRequester())
	}

	namespace := cfg.Namespace
	settings := helmSettings(cfg.KubeConfigPath, cfg.KubeContext, namespace)

	actionCfg := new(action.Configuration)
	logFunc := func(format string, v ...interface{}) {
		if s.Logger.GetSink() != nil {
			s.Logger.Info(fmt.Sprintf(format, v...))
		}
	}
	if err := actionCfg.Init(settings.RESTClientGetter(), namespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
		return fmt.Errorf("init helm action config: %w", err)
	}

	kubeClient, err := kube.New(ctx, cfg.KubeConfigPath, cfg.KubeContext)
	if err != nil {
		return err
	}
	if namespace == "" {
		namespace = kubeClient.Namespace
		settings.SetNamespace(namespace)
	}
	if cfg.CreateNamespace {
		if err := ensureNamespace(ctx, kubeClient.Clientset, namespace); err != nil {
			return err
		}
	}

	if s.Mirror != nil && sessionID != "" {
		_ = s.Mirror.UpsertSessionMeta(ctx, sessionID, MirrorSessionMeta{
			Requester:   strings.TrimSpace(req.GetRequester()),
			KubeContext: strings.TrimSpace(cfg.KubeContext),
			Namespace:   strings.TrimSpace(namespace),
			Release:     strings.TrimSpace(cfg.ReleaseName),
			Chart:       strings.TrimSpace(cfg.Chart),
		}, nil)
		_ = s.Mirror.UpsertSessionStatus(ctx, sessionID, MirrorSessionStatus{State: MirrorSessionStateRunning})
		defer func() {
			st := MirrorSessionStatus{
				State:             MirrorSessionStateDone,
				ExitCode:          0,
				CompletedUnixNano: time.Now().UTC().UnixNano(),
			}
			if retErr != nil {
				if errors.Is(retErr, context.Canceled) {
					st.State = MirrorSessionStateDone
					st.ExitCode = 130
					st.ErrorMessage = "canceled"
				} else {
					st.State = MirrorSessionStateError
					st.ExitCode = 1
					st.ErrorMessage = retErr.Error()
				}
			}
			_ = s.Mirror.UpsertSessionStatus(context.Background(), sessionID, st)
		}()
	}

	streamBroadcaster := deploy.NewStreamBroadcaster(cfg.ReleaseName, namespace, cfg.Chart)
	forwarder := &deployStreamForwarder{stream: stream, mirror: s.Mirror, sessionID: sessionID, producer: producer}
	streamBroadcaster.AddObserver(forwarder)

	result, err := deploy.InstallOrUpgrade(ctx, actionCfg, settings, deploy.InstallOptions{
		Chart:             cfg.Chart,
		Version:           cfg.Version,
		ReleaseName:       cfg.ReleaseName,
		Namespace:         namespace,
		ValuesFiles:       cfg.ValuesFiles,
		SetValues:         cfg.SetValues,
		SetStringValues:   cfg.SetStringValues,
		SetFileValues:     cfg.SetFileValues,
		Timeout:           cfg.Timeout,
		Wait:              cfg.Wait,
		Atomic:            cfg.Atomic,
		CreateNamespace:   cfg.CreateNamespace,
		DryRun:            cfg.DryRun,
		Diff:              cfg.Diff,
		UpgradeOnly:       cfg.UpgradeOnly,
		ProgressObservers: []deploy.ProgressObserver{streamBroadcaster},
	})
	if err != nil {
		return err
	}

	summary := deploy.SummaryPayload{
		Release:   cfg.ReleaseName,
		Namespace: namespace,
		Status:    "succeeded",
	}
	if result.Release != nil {
		if result.Release.Chart != nil && result.Release.Chart.Metadata != nil {
			summary.Chart = result.Release.Chart.Metadata.Name
			summary.Version = result.Release.Chart.Metadata.Version
		}
		if result.Release.Info != nil {
			summary.Status = result.Release.Info.Status.String()
			summary.Notes = result.Release.Info.Notes
		}
	}
	streamBroadcaster.EmitSummary(summary)
	return nil
}

// Destroy removes a Helm release remotely.
func (s *DeployServer) Destroy(req *apiv1.DeployDestroyRequest, stream apiv1.DeployService_DestroyServer) (retErr error) {
	if req == nil || req.GetOptions() == nil {
		return fmt.Errorf("destroy options are required")
	}
	cfg := convert.DeployDestroyFromProto(req.GetOptions())
	sessionID := strings.TrimSpace(req.GetSessionId())
	producer := "deploy"
	if strings.TrimSpace(req.GetRequester()) != "" {
		producer = "deploy:" + strings.TrimSpace(req.GetRequester())
	}
	if s.Mirror != nil && sessionID != "" {
		_ = s.Mirror.UpsertSessionMeta(stream.Context(), sessionID, MirrorSessionMeta{
			Requester:   strings.TrimSpace(req.GetRequester()),
			KubeContext: strings.TrimSpace(cfg.KubeContext),
			Namespace:   strings.TrimSpace(cfg.Namespace),
			Release:     strings.TrimSpace(cfg.Release),
		}, nil)
		_ = s.Mirror.UpsertSessionStatus(stream.Context(), sessionID, MirrorSessionStatus{State: MirrorSessionStateRunning})
		defer func() {
			st := MirrorSessionStatus{
				State:             MirrorSessionStateDone,
				ExitCode:          0,
				CompletedUnixNano: time.Now().UTC().UnixNano(),
			}
			if retErr != nil {
				if errors.Is(retErr, context.Canceled) {
					st.State = MirrorSessionStateDone
					st.ExitCode = 130
					st.ErrorMessage = "canceled"
				} else {
					st.State = MirrorSessionStateError
					st.ExitCode = 1
					st.ErrorMessage = retErr.Error()
				}
			}
			_ = s.Mirror.UpsertSessionStatus(context.Background(), sessionID, st)
		}()
	}

	settings := helmSettings(cfg.KubeConfigPath, cfg.KubeContext, cfg.Namespace)
	actionCfg := new(action.Configuration)
	logFunc := func(format string, v ...interface{}) {
		if s.Logger.GetSink() != nil {
			s.Logger.Info(fmt.Sprintf(format, v...))
		}
	}
	if err := actionCfg.Init(settings.RESTClientGetter(), cfg.Namespace, os.Getenv("HELM_DRIVER"), logFunc); err != nil {
		return fmt.Errorf("init helm action config: %w", err)
	}

	streamBroadcaster := deploy.NewStreamBroadcaster(cfg.Release, cfg.Namespace, "")
	streamBroadcaster.AddObserver(&deployStreamForwarder{stream: stream, mirror: s.Mirror, sessionID: sessionID, producer: producer})

	uninstall := action.NewUninstall(actionCfg)
	uninstall.Wait = cfg.Wait
	uninstall.Timeout = cfg.Timeout
	uninstall.DryRun = cfg.DryRun
	uninstall.KeepHistory = cfg.KeepHistory
	uninstall.DisableHooks = cfg.DisableHooks

	res, err := uninstall.Run(cfg.Release)
	if err != nil {
		return err
	}

	summary := deploy.SummaryPayload{
		Release:   cfg.Release,
		Namespace: cfg.Namespace,
		Status:    "destroyed",
	}
	if res != nil && res.Info != "" {
		summary.Notes = res.Info
	}
	streamBroadcaster.EmitSummary(summary)
	return nil
}

type deployStreamForwarder struct {
	stream interface {
		Send(*apiv1.DeployEvent) error
		Context() context.Context
	}
	mirror    *MirrorServer
	sessionID string
	producer  string
}

func (d *deployStreamForwarder) HandleDeployEvent(evt deploy.StreamEvent) {
	if d == nil || d.stream == nil {
		return
	}
	msg, err := convert.DeployEventToProto(evt)
	if err != nil {
		return
	}
	_ = d.stream.Send(msg)
	if d.mirror != nil && d.sessionID != "" {
		_, _, _ = d.mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
			SessionId: d.sessionID,
			Producer:  d.producer,
			Payload:   &apiv1.MirrorFrame_Deploy{Deploy: msg},
		})
	}
}

func helmSettings(kubeconfig, kubeContext, namespace string) *cli.EnvSettings {
	settings := cli.New()
	if kubeconfig != "" {
		settings.KubeConfig = kubeconfig
	}
	if kubeContext != "" {
		settings.KubeContext = kubeContext
	}
	if namespace != "" {
		settings.SetNamespace(namespace)
	}
	return settings
}

func ensureNamespace(ctx context.Context, clientset kubernetes.Interface, name string) error {
	if name == "" {
		return fmt.Errorf("namespace is required")
	}
	_, err := clientset.CoreV1().Namespaces().Get(ctx, name, metav1.GetOptions{})
	if err == nil {
		return nil
	}
	if !apierrors.IsNotFound(err) {
		return err
	}
	ns := &corev1.Namespace{}
	ns.Name = name
	_, err = clientset.CoreV1().Namespaces().Create(ctx, ns, metav1.CreateOptions{})
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return err
	}
	return nil
}
