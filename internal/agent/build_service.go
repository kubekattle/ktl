// File: internal/agent/build_service.go
// Brief: Internal agent package implementation for 'build service'.

// Package agent provides agent helpers.

package agent

import (
	"io"
	"sync"
	"time"

	"github.com/go-logr/logr"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/internal/workflows/buildsvc"
	apiv1 "github.com/example/ktl/pkg/api/v1"
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
	observer := &buildStreamObserver{stream: stream}
	opts.Observers = append(opts.Observers, observer)
	opts.Streams.Err = io.Discard
	opts.Streams.Out = io.Discard
	result, err := s.Service.Run(ctx, opts)
	res := convert.BuildResultToProto(result, err)
	if res != nil {
		_ = stream.Send(&apiv1.BuildEvent{
			TimestampUnixNano: time.Now().UnixNano(),
			Body:              &apiv1.BuildEvent_Result{Result: res},
		})
	}
	return err
}

type buildStreamObserver struct {
	stream apiv1.BuildService_RunBuildServer
	mu     sync.Mutex
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
}
