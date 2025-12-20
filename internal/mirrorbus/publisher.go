package mirrorbus

import (
	"context"
	"sync"

	"github.com/example/ktl/internal/grpcutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/example/ktl/internal/api/convert"
	"github.com/example/ktl/internal/tailer"
	apiv1 "github.com/example/ktl/pkg/api/v1"
)

// Publisher streams log records to the central mirror bus.
type Publisher struct {
	sessionID string
	producer  string
	stream    apiv1.MirrorService_PublishClient
	conn      *grpc.ClientConn
	mu        sync.Mutex
}

// NewPublisher dials the mirror service and opens a streaming publisher.
func NewPublisher(ctx context.Context, addr, sessionID, producer string) (*Publisher, error) {
	conn, err := grpcutil.Dial(ctx, addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, err
	}
	client := apiv1.NewMirrorServiceClient(conn)
	stream, err := client.Publish(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	return &Publisher{sessionID: sessionID, producer: producer, stream: stream, conn: conn}, nil
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
