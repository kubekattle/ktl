// File: internal/agent/mirror_service.go
// Brief: Internal agent package implementation for 'mirror service'.

// Package agent provides agent helpers.

package agent

import (
	"sync"

	apiv1 "github.com/example/ktl/pkg/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// MirrorServer implements the gRPC mirror bus.
type MirrorServer struct {
	apiv1.UnimplementedMirrorServiceServer
	mu       sync.RWMutex
	sessions map[string]*mirrorSession
	buffer   int
}

// NewMirrorServer constructs a mirror bus server.
func NewMirrorServer() *MirrorServer {
	return &MirrorServer{
		sessions: make(map[string]*mirrorSession),
		buffer:   1024,
	}
}

// Publish accepts frames from producers.
func (s *MirrorServer) Publish(stream apiv1.MirrorService_PublishServer) error {
	if s == nil {
		return status.Error(codes.Unavailable, "mirror server unavailable")
	}
	for {
		frame, err := stream.Recv()
		if err != nil {
			return err
		}
		sessionID := frame.GetSessionId()
		if sessionID == "" {
			continue
		}
		session := s.getOrCreateSession(sessionID)
		seq := session.append(frame)
		ack := &apiv1.MirrorAck{SessionId: sessionID, Sequence: seq}
		if err := stream.Send(ack); err != nil {
			return err
		}
	}
}

// Subscribe streams frames for a session to the caller.
func (s *MirrorServer) Subscribe(req *apiv1.MirrorSubscribeRequest, stream apiv1.MirrorService_SubscribeServer) error {
	if s == nil {
		return status.Error(codes.Unavailable, "mirror server unavailable")
	}
	sessionID := req.GetSessionId()
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	session := s.getOrCreateSession(sessionID)
	subscriber := session.subscribe()
	defer session.unsubscribe(subscriber)
	if req.GetReplay() {
		session.replay(func(frame *apiv1.MirrorFrame) {
			_ = stream.Send(frame)
		})
	}
	ctx := stream.Context()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-subscriber.ch:
			if !ok {
				return nil
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		}
	}
}

func (s *MirrorServer) getOrCreateSession(id string) *mirrorSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	if sess, ok := s.sessions[id]; ok {
		return sess
	}
	sess := &mirrorSession{
		limit:  s.buffer,
		buffer: make([]*apiv1.MirrorFrame, 0, s.buffer),
		subs:   make(map[uint64]*mirrorSubscriber),
	}
	s.sessions[id] = sess
	return sess
}

type mirrorSession struct {
	mu     sync.RWMutex
	seq    uint64
	limit  int
	buffer []*apiv1.MirrorFrame
	subs   map[uint64]*mirrorSubscriber
	nextID uint64
}

func (m *mirrorSession) append(frame *apiv1.MirrorFrame) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if len(m.buffer) >= m.limit && m.limit > 0 {
		m.buffer = m.buffer[1:]
	}
	var cloned *apiv1.MirrorFrame
	if frame != nil {
		cloned = proto.Clone(frame).(*apiv1.MirrorFrame)
	}
	m.buffer = append(m.buffer, cloned)
	for _, sub := range m.subs {
		select {
		case sub.ch <- cloned:
		default:
		}
	}
	return m.seq
}

func (m *mirrorSession) subscribe() *mirrorSubscriber {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.nextID++
	sub := &mirrorSubscriber{
		id: m.nextID,
		ch: make(chan *apiv1.MirrorFrame, 256),
	}
	m.subs[sub.id] = sub
	return sub
}

func (m *mirrorSession) unsubscribe(sub *mirrorSubscriber) {
	if sub == nil {
		return
	}
	m.mu.Lock()
	if _, ok := m.subs[sub.id]; ok {
		delete(m.subs, sub.id)
		close(sub.ch)
	}
	m.mu.Unlock()
}

func (m *mirrorSession) replay(send func(*apiv1.MirrorFrame)) {
	if send == nil {
		return
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, frame := range m.buffer {
		if frame == nil {
			continue
		}
		send(proto.Clone(frame).(*apiv1.MirrorFrame))
	}
}

type mirrorSubscriber struct {
	id uint64
	ch chan *apiv1.MirrorFrame
}
