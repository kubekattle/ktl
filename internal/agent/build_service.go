// File: internal/agent/build_service.go
// Brief: Internal agent package implementation for 'build service'.

// Package agent provides agent helpers.

package agent

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// BuildServer exposes buildsvc over gRPC.
type BuildServer struct {
	apiv1.UnimplementedBuildServiceServer
	Service buildsvc.Service
	Mirror  *MirrorServer
	Logger  logr.Logger
}

// RunBuild executes a remote build and streams progress.
func (s *BuildServer) RunBuild(req *apiv1.RunBuildRequest, stream apiv1.BuildService_RunBuildServer) error {
	if s == nil || s.Service == nil {
		return status.Error(codes.Unavailable, "build service not configured")
	}
	ctx := stream.Context()
	opts := convert.BuildOptionsFromProto(req.GetOptions())
	sessionID := strings.TrimSpace(req.GetSessionId())
	producer := "build"
	if strings.TrimSpace(req.GetRequester()) != "" {
		producer = "build:" + strings.TrimSpace(req.GetRequester())
	}
	if s.Mirror != nil && sessionID != "" {
		tags := map[string]string{
			"build.context_dir": strings.TrimSpace(opts.ContextDir),
			"build.dockerfile":  strings.TrimSpace(opts.Dockerfile),
		}
		_ = s.Mirror.UpsertSessionMeta(ctx, sessionID, MirrorSessionMeta{
			Requester: strings.TrimSpace(req.GetRequester()),
		}, tags)
		_ = s.Mirror.UpsertSessionStatus(ctx, sessionID, MirrorSessionStatus{State: MirrorSessionStateRunning})
	}
	var runErr error
	defer func() {
		if s.Mirror == nil || sessionID == "" {
			return
		}
		st := MirrorSessionStatus{
			State:             MirrorSessionStateDone,
			ExitCode:          0,
			CompletedUnixNano: time.Now().UTC().UnixNano(),
		}
		if runErr != nil {
			if errors.Is(runErr, context.Canceled) {
				st.State = MirrorSessionStateDone
				st.ExitCode = 130
				st.ErrorMessage = "canceled"
			} else {
				st.State = MirrorSessionStateError
				st.ExitCode = 1
				st.ErrorMessage = runErr.Error()
			}
		}
		_ = s.Mirror.UpsertSessionStatus(context.Background(), sessionID, st)
	}()
	observer := &buildStreamObserver{stream: stream, mirror: s.Mirror, sessionID: sessionID, producer: producer}
	opts.Observers = append(opts.Observers, observer)
	opts.Streams.Err = io.Discard
	opts.Streams.Out = io.Discard
	result, err := s.Service.Run(ctx, opts)
	runErr = err
	res := convert.BuildResultToProto(result, err)
	if res != nil {
		ev := &apiv1.BuildEvent{
			TimestampUnixNano: time.Now().UnixNano(),
			Body:              &apiv1.BuildEvent_Result{Result: res},
		}
		_ = stream.Send(ev)
		if s.Mirror != nil && sessionID != "" {
			_, _, _ = s.Mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
				SessionId: sessionID,
				Producer:  producer,
				Payload:   &apiv1.MirrorFrame_Build{Build: ev},
			})
		}
	}
	return err
}

type buildStreamObserver struct {
	stream    apiv1.BuildService_RunBuildServer
	mirror    *MirrorServer
	sessionID string
	producer  string
	mu        sync.Mutex
}

func (b *buildStreamObserver) ObserveLog(rec tailer.LogRecord) {
	if b == nil || b.stream == nil {
		return
	}
	line := convert.ToProtoLogRecord(rec)
	event := &apiv1.BuildEvent{
		TimestampUnixNano: rec.Timestamp.UnixNano(),
		Body:              &apiv1.BuildEvent_Log{Log: line},
	}
	b.mu.Lock()
	_ = b.stream.Send(event)
	b.mu.Unlock()
	if b.mirror != nil && b.sessionID != "" {
		_, _, _ = b.mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
			SessionId: b.sessionID,
			Producer:  b.producer,
			Payload:   &apiv1.MirrorFrame_Build{Build: event},
		})
	}
}
