// File: internal/agent/log_service.go
// Brief: Internal agent package implementation for 'log service'.

// Package agent provides agent helpers.

package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

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
func (s *LogServer) StreamLogs(req *apiv1.LogRequest, stream apiv1.LogService_StreamLogsServer) (retErr error) {
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

	sessionID := strings.TrimSpace(req.GetSessionId())
	if s.Mirror != nil && sessionID != "" {
		meta := MirrorSessionMeta{
			Requester:   strings.TrimSpace(req.GetRequester()),
			KubeContext: strings.TrimSpace(kubeContext),
		}
		if !req.GetAllNamespaces() && len(req.GetNamespaces()) == 1 {
			meta.Namespace = strings.TrimSpace(req.GetNamespaces()[0])
		}
		tags := map[string]string{
			"logs.pod_query": strings.TrimSpace(opts.PodQuery),
		}
		_ = s.Mirror.UpsertSessionMeta(ctx, sessionID, meta, tags)
		_ = s.Mirror.UpsertSessionStatus(ctx, sessionID, MirrorSessionStatus{State: MirrorSessionStateRunning})
		defer func() {
			st := MirrorSessionStatus{
				State:             MirrorSessionStateDone,
				ExitCode:          0,
				CompletedUnixNano: time.Now().UTC().UnixNano(),
			}
			if retErr != nil {
				if errors.Is(retErr, context.Canceled) {
					// Client disconnected or canceled; treat as a normal shutdown for log streams.
					st.State = MirrorSessionStateDone
					st.ExitCode = 0
				} else {
					st.State = MirrorSessionStateError
					st.ExitCode = 1
					st.ErrorMessage = retErr.Error()
				}
			}
			_ = s.Mirror.UpsertSessionStatus(context.Background(), sessionID, st)
		}()
	}

	client, err := kube.New(ctx, kubeconfig, kubeContext)
	if err != nil {
		return err
	}
	logger := s.Logger
	if logger.GetSink() == nil {
		logger = logr.Logger{}
	}
	producer := "logs"
	if strings.TrimSpace(req.GetRequester()) != "" {
		producer = "logs:" + strings.TrimSpace(req.GetRequester())
	}
	observer := &logStreamObserver{stream: stream, mirror: s.Mirror, sessionID: sessionID, producer: producer}
	tailerOpts := []tailer.Option{tailer.WithLogObserver(observer), tailer.WithOutput(io.Discard)}
	t, err := tailer.New(client.Clientset, opts, logger, tailerOpts...)
	if err != nil {
		return err
	}
	return t.Run(ctx)
}

type logStreamObserver struct {
	stream    apiv1.LogService_StreamLogsServer
	mirror    *MirrorServer
	sessionID string
	producer  string
}

func (l *logStreamObserver) ObserveLog(rec tailer.LogRecord) {
	if l == nil || l.stream == nil {
		return
	}
	line := convert.ToProtoLogRecord(rec)
	_ = l.stream.Send(line)
	if l.mirror != nil && l.sessionID != "" {
		_, _, _ = l.mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
			SessionId: l.sessionID,
			Producer:  l.producer,
			Payload:   &apiv1.MirrorFrame_Log{Log: line},
		})
	}
}
