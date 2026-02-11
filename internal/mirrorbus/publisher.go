// File: internal/mirrorbus/publisher.go
// Brief: Internal mirrorbus package implementation for 'publisher'.

// Package mirrorbus provides mirrorbus helpers.

package mirrorbus

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/example/ktl/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/tailer"
	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
)

// Publisher streams log records to the central mirror bus.
type Publisher struct {
	sessionID string
	producer  string
	stream    apiv1.MirrorService_PublishClient
	conn      *grpc.ClientConn
	mu        sync.Mutex
	lastSeq   uint64
}

// NewPublisher dials the mirror service and opens a streaming publisher.
func NewPublisher(ctx context.Context, addr, sessionID, producer string, meta *apiv1.MirrorSessionMeta, tags map[string]string, dialOpts ...grpc.DialOption) (*Publisher, error) {
	opts := append([]grpc.DialOption{grpc.WithTransportCredentials(insecure.NewCredentials())}, dialOpts...)
	conn, err := grpcutil.Dial(ctx, addr, opts...)
	if err != nil {
		return nil, err
	}
	client := apiv1.NewMirrorServiceClient(conn)
	// Best-effort metadata. Older servers might not implement it yet.
	if strings.TrimSpace(sessionID) != "" && (meta != nil || len(tags) > 0) {
		_, err := client.SetSessionMeta(ctx, &apiv1.MirrorSetSessionMetaRequest{
			SessionId: sessionID,
			Meta:      meta,
			Tags:      tags,
		})
		if err != nil {
			st, ok := status.FromError(err)
			if !ok || (st.Code() != codes.Unimplemented && st.Code() != codes.Unavailable) {
				// Ignore to keep publishers resilient; SetSessionMeta is an optional hint.
				_ = strings.TrimSpace(err.Error())
			}
		}
	}
	stream, err := client.Publish(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	p := &Publisher{sessionID: sessionID, producer: producer, stream: stream, conn: conn}
	// MirrorService.Publish is a bidi stream; draining acks prevents server-side flow control
	// from stalling the stream under sustained load.
	go p.drainAcks()
	return p, nil
}

// ObserveLog satisfies tailer.LogObserver.
func (p *Publisher) ObserveLog(rec tailer.LogRecord) {
	if p == nil || p.stream == nil {
		return
	}
	frame := &apiv1.MirrorFrame{
		SessionId: p.sessionID,
		Producer:  p.producer,
		Payload: &apiv1.MirrorFrame_Log{
			Log: convert.ToProtoLogRecord(rec),
		},
	}
	p.mu.Lock()
	_ = p.stream.Send(frame)
	p.mu.Unlock()
}

func (p *Publisher) drainAcks() {
	if p == nil || p.stream == nil {
		return
	}
	for {
		ack, err := p.stream.Recv()
		if err != nil {
			return
		}
		if ack == nil {
			continue
		}
		atomic.StoreUint64(&p.lastSeq, ack.GetSequence())
		_ = strings.TrimSpace(ack.GetMessage()) // reserved for future diagnostics
	}
}

// Close tears down the publisher.
func (p *Publisher) Close() error {
	if p == nil {
		return nil
	}
	if p.stream != nil {
		_ = p.stream.CloseSend()
	}
	if p.conn != nil {
		return p.conn.Close()
	}
	return nil
}
