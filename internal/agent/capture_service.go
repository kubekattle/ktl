package agent

import (
	"fmt"
	"io"
	"os"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/capture"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/logging"
	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// CaptureServer exposes capture workflows over gRPC.
type CaptureServer struct {
	apiv1.UnimplementedCaptureServiceServer
}

// RunCapture executes a capture session and streams the resulting artifact.
func (s *CaptureServer) RunCapture(req *apiv1.CaptureRequest, stream apiv1.CaptureService_RunCaptureServer) error {
	if req == nil {
		return fmt.Errorf("capture request is required")
	}
	cfg := convert.DefaultConfigFromProto(req.GetLog())
	capCfg := convert.CaptureFromProto(req.GetCapture())
	ctx := stream.Context()

	if err := cfg.Validate(); err != nil {
		return err
	}
	captureOpts := capture.NewOptions()
	captureOpts.Duration = capCfg.Duration
	captureOpts.OutputPath = capCfg.OutputName
	captureOpts.SQLite = capCfg.SQLite
	captureOpts.AttachDescribe = capCfg.AttachDescribe
	captureOpts.SessionName = capCfg.SessionName
	if err := captureOpts.Validate(); err != nil {
		return err
	}

	kubeClient, err := kube.New(ctx, cfg.KubeConfigPath, cfg.Context)
	if err != nil {
		return err
	}
	if !cfg.AllNamespaces && len(cfg.Namespaces) == 0 && kubeClient.Namespace != "" {
		cfg.Namespaces = []string{kubeClient.Namespace}
	}
	logger, err := logging.New("info")
	if err != nil {
		return err
	}
	session, err := capture.NewSession(kubeClient, cfg, captureOpts, logger)
	if err != nil {
		return err
	}
	artifact, err := session.Run(ctx)
	if err != nil {
		return err
	}
	file, err := os.Open(artifact)
	if err != nil {
		return err
	}
	defer file.Close()
	defer os.Remove(artifact)
	buf := make([]byte, 64*1024)
	for {
		n, readErr := file.Read(buf)
		if n > 0 {
			chunk := &apiv1.CaptureChunk{Data: append([]byte(nil), buf[:n]...), Filename: artifact}
			if readErr == io.EOF {
				chunk.Last = true
			}
			if err := stream.Send(chunk); err != nil {
				return err
			}
		}
		if readErr != nil {
			if readErr == io.EOF {
				break
			}
			return readErr
		}
	}
	return nil
}
