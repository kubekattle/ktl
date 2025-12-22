// File: internal/agent/log_service.go
// Brief: Internal agent package implementation for 'log service'.

// Package agent provides agent helpers.

package agent

import (
	"io"

	"github.com/go-logr/logr"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/kube"
	"github.com/example/ktl/internal/tailer"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// LogServer relays ktl log streams over gRPC.
type LogServer struct {
	apiv1.UnimplementedLogServiceServer
	Config Config
	Logger logr.Logger
	Mirror *MirrorServer
}

// StreamLogs executes a tailer instance and streams log lines to clients.
func (s *LogServer) StreamLogs(req *apiv1.LogRequest, stream apiv1.LogService_StreamLogsServer) error {
	if s == nil {
		return status.Error(codes.Unavailable, "log server unavailable")
	}
	opts := convert.DefaultConfigFromProto(req)
	if opts.PodQuery == "" {
		return status.Error(codes.InvalidArgument, "pod_query is required")
	}
	if err := opts.Validate(); err != nil {
		return err
	}
	ctx := stream.Context()
	kubeconfig := req.GetKubeconfigPath()
	if kubeconfig == "" {
		kubeconfig = s.Config.KubeconfigPath
	}
	kubeContext := req.GetKubeContext()
	if kubeContext == "" {
		kubeContext = s.Config.KubeContext
	}
	opts.KubeConfigPath = kubeconfig
	opts.Context = kubeContext
	client, err := kube.New(ctx, kubeconfig, kubeContext)
	if err != nil {
		return err
	}
	logger := s.Logger
	if logger.GetSink() == nil {
		logger = logr.Logger{}
	}
	observer := &logStreamObserver{stream: stream}
	tailerOpts := []tailer.Option{tailer.WithLogObserver(observer), tailer.WithOutput(io.Discard)}
	t, err := tailer.New(client.Clientset, opts, logger, tailerOpts...)
	if err != nil {
		return err
	}
	return t.Run(ctx)
}

type logStreamObserver struct {
	stream apiv1.LogService_StreamLogsServer
}

func (l *logStreamObserver) ObserveLog(rec tailer.LogRecord) {
	if l == nil || l.stream == nil {
		return
	}
	line := convert.ToProtoLogRecord(rec)
	_ = l.stream.Send(line)
}
