// File: internal/agent/mirror_service.go
// Brief: Internal agent package implementation for 'mirror service'.

// Package agent provides agent helpers.
package agent

import (
	"context"
	"errors"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

var errReplayStop = errors.New("stop replay")

// MirrorServer implements the gRPC mirror bus.
//
// The server can optionally persist frames via a MirrorStore so sessions are replayable across restarts.
type MirrorServer struct {
	apiv1.UnimplementedMirrorServiceServer
	mu       sync.RWMutex
	sessions map[string]*mirrorSession
	buffer   int
	store    MirrorStore
	now      func() time.Time
}

type MirrorServerOption func(*MirrorServer)

func WithMirrorBuffer(n int) MirrorServerOption {
	return func(s *MirrorServer) {
		if s == nil {
			return
		}
		if n < 0 {
			n = 0
		}
		s.buffer = n
	}
}

func WithMirrorStore(store MirrorStore) MirrorServerOption {
	return func(s *MirrorServer) {
		if s == nil {
			return
		}
		s.store = store
	}
}

// NewMirrorServer constructs a mirror bus server.
func NewMirrorServer(opts ...MirrorServerOption) *MirrorServer {
	s := &MirrorServer{
		sessions: make(map[string]*mirrorSession),
		buffer:   1024,
		now:      func() time.Time { return time.Now().UTC() },
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

func (s *MirrorServer) Close() error {
	if s == nil || s.store == nil {
		return nil
	}
	return s.store.Close()
}

// Publish accepts frames from producers.
func (s *MirrorServer) Publish(stream apiv1.MirrorService_PublishServer) error {
	if s == nil {
		return status.Error(codes.Unavailable, "mirror server unavailable")
	}
	ctx := stream.Context()
	for {
		frame, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		seq, msg, err := s.ingestFrame(ctx, frame)
		if err != nil {
			return err
		}
		if seq == 0 {
			continue
		}
		ack := &apiv1.MirrorAck{SessionId: strings.TrimSpace(frame.GetSessionId()), Message: msg, Sequence: seq}
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
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	ctx := stream.Context()
	session := s.getOrCreateSession(ctx, sessionID)
	subscriber := session.subscribe()
	defer session.unsubscribe(subscriber)

	fromSeq := req.GetFromSequence()
	if fromSeq == 0 {
		fromSeq = 1
	}

	// Bound the replay snapshot to avoid replay/live duplicates.
	snapshotSeq := session.currentSeq()

	lastSentSeq := uint64(0)
	if req.GetReplay() {
		if s.store != nil {
			_, err := s.store.Replay(ctx, sessionID, fromSeq, func(frame *apiv1.MirrorFrame) error {
				if frame == nil {
					return nil
				}
				seq := frame.GetSequence()
				if seq == 0 {
					return nil
				}
				if seq > snapshotSeq {
					return errReplayStop
				}
				if err := stream.Send(frame); err != nil {
					return err
				}
				lastSentSeq = seq
				return nil
			})
			if err != nil && !errors.Is(err, errReplayStop) {
				return err
			}
		} else {
			last, err := session.replay(fromSeq, snapshotSeq, func(frame *apiv1.MirrorFrame) error {
				return stream.Send(frame)
			})
			if err != nil {
				return err
			}
			lastSentSeq = last
		}
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case frame, ok := <-subscriber.ch:
			if !ok {
				return nil
			}
			seq := frame.GetSequence()
			if seq < fromSeq {
				continue
			}
			if lastSentSeq > 0 && seq <= lastSentSeq {
				continue
			}
			if err := stream.Send(frame); err != nil {
				return err
			}
		}
	}
}

func (s *MirrorServer) ListSessions(ctx context.Context, req *apiv1.MirrorListSessionsRequest) (*apiv1.MirrorListSessionsResponse, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "mirror server unavailable")
	}
	limit := 200
	if req != nil && req.GetLimit() > 0 {
		limit = int(req.GetLimit())
	}
	if limit > 1000 {
		limit = 1000
	}

	filterMeta := MirrorSessionMeta{}
	filterTags := map[string]string(nil)
	filterState := MirrorSessionStateUnspecified
	sinceLastSeen := int64(0)
	untilLastSeen := int64(0)
	if req != nil {
		filterMeta = fromProtoMeta(req.GetMeta())
		filterTags = req.GetTags()
		filterState = MirrorSessionState(req.GetState())
		sinceLastSeen = req.GetSinceLastSeenUnixNano()
		untilLastSeen = req.GetUntilLastSeenUnixNano()
	}
	hasFilters := !isEmptyMeta(filterMeta) ||
		len(filterTags) > 0 ||
		filterState != MirrorSessionStateUnspecified ||
		sinceLastSeen > 0 ||
		untilLastSeen > 0
	storeLimit := limit
	if hasFilters {
		storeLimit = 1000
	}

	merged := map[string]MirrorSession{}
	if s.store != nil {
		rows, err := s.store.ListSessions(ctx, storeLimit)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "list sessions: %v", err)
		}
		for _, row := range rows {
			merged[row.SessionID] = row
		}
	}
	for _, row := range s.listInMemorySessions() {
		if existing, ok := merged[row.SessionID]; ok {
			merged[row.SessionID] = mergeSession(existing, row)
		} else {
			merged[row.SessionID] = row
		}
	}

	out := make([]MirrorSession, 0, len(merged))
	for _, row := range merged {
		if !matchesSession(row, filterMeta, filterTags, filterState, sinceLastSeen, untilLastSeen) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].LastSeenUnixNano > out[j].LastSeenUnixNano
	})
	if len(out) > limit {
		out = out[:limit]
	}
	resp := &apiv1.MirrorListSessionsResponse{Sessions: make([]*apiv1.MirrorSession, 0, len(out))}
	for _, row := range out {
		resp.Sessions = append(resp.Sessions, toProtoSession(row))
	}
	return resp, nil
}

func (s *MirrorServer) GetSession(ctx context.Context, req *apiv1.MirrorGetSessionRequest) (*apiv1.MirrorSession, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	found := false
	var row MirrorSession
	if s.store != nil {
		stored, ok, err := s.store.GetSession(ctx, sessionID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "get session: %v", err)
		}
		if ok {
			row = stored
			found = true
		}
	}
	if sess, ok := s.getSession(sessionID); ok && sess != nil {
		mem := sess.snapshot(sessionID)
		if found {
			row = mergeSession(row, mem)
		} else {
			row = mem
			found = true
		}
	}
	if !found {
		return nil, status.Error(codes.NotFound, "session not found")
	}
	return toProtoSession(row), nil
}

func (s *MirrorServer) SetSessionMeta(ctx context.Context, req *apiv1.MirrorSetSessionMetaRequest) (*apiv1.MirrorSession, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	meta := fromProtoMeta(req.GetMeta())
	row := s.UpsertSessionMeta(ctx, sessionID, meta, req.GetTags())
	return toProtoSession(row), nil
}

func (s *MirrorServer) SetSessionStatus(ctx context.Context, req *apiv1.MirrorSetSessionStatusRequest) (*apiv1.MirrorSession, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}
	if req.GetStatus() == nil {
		return nil, status.Error(codes.InvalidArgument, "status is required")
	}
	st := fromProtoStatus(req.GetStatus())
	row := s.UpsertSessionStatus(ctx, sessionID, st)
	return toProtoSession(row), nil
}

func (s *MirrorServer) DeleteSession(ctx context.Context, req *apiv1.MirrorDeleteSessionRequest) (*apiv1.MirrorDeleteSessionResponse, error) {
	if s == nil {
		return nil, status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if req == nil {
		return nil, status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return nil, status.Error(codes.InvalidArgument, "session_id is required")
	}

	// Remove from in-memory bus first.
	s.mu.Lock()
	_, existed := s.sessions[sessionID]
	delete(s.sessions, sessionID)
	s.mu.Unlock()

	deleted := existed
	if s.store != nil {
		ok, err := s.store.DeleteSession(ctx, sessionID)
		if err != nil {
			return nil, status.Errorf(codes.Internal, "delete session: %v", err)
		}
		deleted = ok || deleted
	}

	return &apiv1.MirrorDeleteSessionResponse{Deleted: deleted}, nil
}

// UpsertSessionMeta updates in-memory and durable metadata for a session.
// It's safe to call this even if the session has no frames yet.
func (s *MirrorServer) UpsertSessionMeta(ctx context.Context, sessionID string, meta MirrorSessionMeta, tags map[string]string) MirrorSession {
	if s == nil {
		return MirrorSession{}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return MirrorSession{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sess := s.getOrCreateSession(ctx, sessionID)
	row := sess.updateMeta(meta, tags)
	row.SessionID = sessionID
	if s.store != nil {
		_ = s.store.UpsertSessionMeta(ctx, sessionID, meta, tags)
	}
	return row
}

// UpsertSessionStatus updates in-memory and durable lifecycle state for a session.
// It's safe to call this even if the session has no frames yet.
func (s *MirrorServer) UpsertSessionStatus(ctx context.Context, sessionID string, st MirrorSessionStatus) MirrorSession {
	if s == nil {
		return MirrorSession{}
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return MirrorSession{}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sess := s.getOrCreateSession(ctx, sessionID)
	row := sess.updateStatus(st)
	row.SessionID = sessionID
	if s.store != nil {
		_ = s.store.UpsertSessionStatus(ctx, sessionID, st)
	}
	return row
}

func (s *MirrorServer) Export(req *apiv1.MirrorExportRequest, stream apiv1.MirrorService_ExportServer) error {
	if s == nil {
		return status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if req == nil {
		return status.Error(codes.InvalidArgument, "request is required")
	}
	sessionID := strings.TrimSpace(req.GetSessionId())
	if sessionID == "" {
		return status.Error(codes.InvalidArgument, "session_id is required")
	}
	format := strings.ToLower(strings.TrimSpace(req.GetFormat()))
	if format == "" {
		format = "jsonl"
	}
	if format != "jsonl" {
		return status.Errorf(codes.InvalidArgument, "unsupported export format %q", format)
	}
	fromSeq := req.GetFromSequence()
	if fromSeq == 0 {
		fromSeq = 1
	}

	mo := protojson.MarshalOptions{UseProtoNames: true}
	const maxChunk = 64 * 1024
	buf := make([]byte, 0, maxChunk)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		chunk := make([]byte, len(buf))
		copy(chunk, buf)
		buf = buf[:0]
		return stream.Send(&apiv1.MirrorExportChunk{Data: chunk})
	}
	appendLine := func(frame *apiv1.MirrorFrame) error {
		if frame == nil {
			return nil
		}
		raw, err := mo.Marshal(frame)
		if err != nil {
			return err
		}
		if len(raw)+1 > maxChunk {
			// Oversized frame; flush what we have and send it as a dedicated chunk.
			if err := flush(); err != nil {
				return err
			}
			return stream.Send(&apiv1.MirrorExportChunk{Data: append(raw, '\n')})
		}
		if len(buf)+len(raw)+1 > maxChunk {
			if err := flush(); err != nil {
				return err
			}
		}
		buf = append(buf, raw...)
		buf = append(buf, '\n')
		return nil
	}

	ctx := stream.Context()
	if s.store != nil {
		if _, err := s.store.Replay(ctx, sessionID, fromSeq, appendLine); err != nil {
			return err
		}
	} else {
		session := s.getOrCreateSession(ctx, sessionID)
		_, err := session.replay(fromSeq, 0, appendLine)
		if err != nil {
			return err
		}
	}
	return flush()
}

func (s *MirrorServer) listInMemorySessions() []MirrorSession {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]MirrorSession, 0, len(s.sessions))
	for id, sess := range s.sessions {
		if sess == nil {
			continue
		}
		out = append(out, sess.snapshot(id))
	}
	return out
}

func mergeSession(a, b MirrorSession) MirrorSession {
	if strings.TrimSpace(a.SessionID) == "" {
		return b
	}
	if strings.TrimSpace(b.SessionID) == "" {
		return a
	}
	out := a
	if out.CreatedUnixNano == 0 || (b.CreatedUnixNano > 0 && b.CreatedUnixNano < out.CreatedUnixNano) {
		out.CreatedUnixNano = b.CreatedUnixNano
	}
	if b.LastSeenUnixNano > out.LastSeenUnixNano {
		out.LastSeenUnixNano = b.LastSeenUnixNano
	}
	if b.LastSequence > out.LastSequence {
		out.LastSequence = b.LastSequence
	}
	out.Meta = mergeMeta(out.Meta, b.Meta)
	out.Tags = mergeTags(out.Tags, b.Tags)
	out.Status = mergeStatus(out.Status, b.Status)
	return out
}

func isEmptyMeta(m MirrorSessionMeta) bool {
	return strings.TrimSpace(m.Command) == "" &&
		len(m.Args) == 0 &&
		strings.TrimSpace(m.Requester) == "" &&
		strings.TrimSpace(m.Cluster) == "" &&
		strings.TrimSpace(m.KubeContext) == "" &&
		strings.TrimSpace(m.Namespace) == "" &&
		strings.TrimSpace(m.Release) == "" &&
		strings.TrimSpace(m.Chart) == ""
}

func matchesSession(row MirrorSession, meta MirrorSessionMeta, tags map[string]string, state MirrorSessionState, sinceLastSeen, untilLastSeen int64) bool {
	if sinceLastSeen > 0 && row.LastSeenUnixNano < sinceLastSeen {
		return false
	}
	if untilLastSeen > 0 && row.LastSeenUnixNano > untilLastSeen {
		return false
	}
	if state != MirrorSessionStateUnspecified && row.Status.State != state {
		return false
	}

	if v := strings.TrimSpace(meta.Command); v != "" {
		if !strings.Contains(strings.ToLower(strings.TrimSpace(row.Meta.Command)), strings.ToLower(v)) {
			return false
		}
	}
	if len(meta.Args) > 0 {
		if len(row.Meta.Args) < len(meta.Args) {
			return false
		}
		for i := range meta.Args {
			if row.Meta.Args[i] != meta.Args[i] {
				return false
			}
		}
	}
	if v := strings.TrimSpace(meta.Requester); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.Requester), v) {
		return false
	}
	if v := strings.TrimSpace(meta.Cluster); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.Cluster), v) {
		return false
	}
	if v := strings.TrimSpace(meta.KubeContext); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.KubeContext), v) {
		return false
	}
	if v := strings.TrimSpace(meta.Namespace); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.Namespace), v) {
		return false
	}
	if v := strings.TrimSpace(meta.Release); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.Release), v) {
		return false
	}
	if v := strings.TrimSpace(meta.Chart); v != "" && !strings.EqualFold(strings.TrimSpace(row.Meta.Chart), v) {
		return false
	}

	if len(tags) > 0 {
		for k, want := range tags {
			k = strings.TrimSpace(k)
			want = strings.TrimSpace(want)
			if k == "" {
				continue
			}
			got, ok := row.Tags[k]
			if !ok {
				return false
			}
			if want == "" {
				continue
			}
			if strings.TrimSpace(got) != want {
				return false
			}
		}
	}
	return true
}

func (s *MirrorServer) getOrCreateSession(ctx context.Context, id string) *mirrorSession {
	s.mu.Lock()
	if sess, ok := s.sessions[id]; ok {
		s.mu.Unlock()
		return sess
	}
	nowNS := s.now().UnixNano()
	meta := MirrorSession{SessionID: id, CreatedUnixNano: nowNS, LastSeenUnixNano: nowNS, LastSequence: 0}
	if s.store != nil {
		if stored, ok, err := s.store.GetSession(ctx, id); err == nil && ok {
			meta = mergeSession(meta, stored)
		}
	}
	sess := &mirrorSession{
		seq:        meta.LastSequence,
		createdNS:  meta.CreatedUnixNano,
		lastSeenNS: meta.LastSeenUnixNano,
		meta:       meta.Meta,
		tags:       mergeTags(nil, meta.Tags),
		status:     meta.Status,
		limit:      s.buffer,
		buffer:     make([]*apiv1.MirrorFrame, 0, max(0, s.buffer)),
		subs:       make(map[uint64]*mirrorSubscriber),
	}
	s.sessions[id] = sess
	s.mu.Unlock()
	return sess
}

func (s *MirrorServer) getSession(id string) (*mirrorSession, bool) {
	if s == nil {
		return nil, false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	sess, ok := s.sessions[id]
	return sess, ok
}

func (s *MirrorServer) ingestFrame(ctx context.Context, frame *apiv1.MirrorFrame) (seq uint64, ackMsg string, err error) {
	if s == nil {
		return 0, "", status.Error(codes.Unavailable, "mirror server unavailable")
	}
	if frame == nil {
		return 0, "", nil
	}
	sessionID := strings.TrimSpace(frame.GetSessionId())
	if sessionID == "" {
		return 0, "", nil
	}
	session := s.getOrCreateSession(ctx, sessionID)
	receivedNS := s.now().UnixNano()
	seq = session.nextSeq(receivedNS)
	cloned := proto.Clone(frame).(*apiv1.MirrorFrame)
	cloned.SessionId = sessionID
	cloned.Sequence = seq
	cloned.ReceivedUnixNano = receivedNS
	session.append(cloned)
	if s.store != nil {
		if err := s.store.Append(cloned); err != nil {
			ackMsg = err.Error()
		}
	}
	return seq, ackMsg, nil
}

type mirrorSession struct {
	mu         sync.RWMutex
	seq        uint64
	createdNS  int64
	lastSeenNS int64
	meta       MirrorSessionMeta
	tags       map[string]string
	status     MirrorSessionStatus
	limit      int
	buffer     []*apiv1.MirrorFrame
	subs       map[uint64]*mirrorSubscriber
	nextID     uint64
}

func (m *mirrorSession) snapshot(id string) MirrorSession {
	if m == nil {
		return MirrorSession{}
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return MirrorSession{
		SessionID:        id,
		CreatedUnixNano:  m.createdNS,
		LastSeenUnixNano: m.lastSeenNS,
		LastSequence:     m.seq,
		Meta:             m.meta,
		Tags:             mergeTags(nil, m.tags),
		Status:           m.status,
	}
}

func (m *mirrorSession) updateMeta(meta MirrorSessionMeta, tags map[string]string) MirrorSession {
	if m == nil {
		return MirrorSession{}
	}
	m.mu.Lock()
	m.meta = mergeMeta(m.meta, meta)
	m.tags = mergeTags(m.tags, tags)
	if m.status.State < MirrorSessionStateRunning {
		m.status.State = MirrorSessionStateRunning
	}
	out := MirrorSession{
		CreatedUnixNano:  m.createdNS,
		LastSeenUnixNano: m.lastSeenNS,
		LastSequence:     m.seq,
		Meta:             m.meta,
		Tags:             mergeTags(nil, m.tags),
		Status:           m.status,
	}
	m.mu.Unlock()
	return out
}

func (m *mirrorSession) updateStatus(st MirrorSessionStatus) MirrorSession {
	if m == nil {
		return MirrorSession{}
	}
	m.mu.Lock()
	m.status = mergeStatus(m.status, st)
	out := MirrorSession{
		CreatedUnixNano:  m.createdNS,
		LastSeenUnixNano: m.lastSeenNS,
		LastSequence:     m.seq,
		Meta:             m.meta,
		Tags:             mergeTags(nil, m.tags),
		Status:           m.status,
	}
	m.mu.Unlock()
	return out
}

func (m *mirrorSession) currentSeq() uint64 {
	if m == nil {
		return 0
	}
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.seq
}

func (m *mirrorSession) nextSeq(receivedNS int64) uint64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	if m.createdNS == 0 {
		m.createdNS = receivedNS
	}
	if receivedNS > m.lastSeenNS {
		m.lastSeenNS = receivedNS
	}
	if m.status.State < MirrorSessionStateRunning {
		m.status.State = MirrorSessionStateRunning
	}
	return m.seq
}

func (m *mirrorSession) append(frame *apiv1.MirrorFrame) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.limit > 0 && len(m.buffer) >= m.limit {
		m.buffer = m.buffer[1:]
	}
	m.buffer = append(m.buffer, frame)
	for _, sub := range m.subs {
		select {
		case sub.ch <- frame:
		default:
		}
	}
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

func (m *mirrorSession) replay(fromSeq, toSeq uint64, send func(*apiv1.MirrorFrame) error) (uint64, error) {
	if send == nil {
		return 0, nil
	}
	if fromSeq == 0 {
		fromSeq = 1
	}
	m.mu.RLock()
	snapshot := append([]*apiv1.MirrorFrame(nil), m.buffer...)
	m.mu.RUnlock()
	var last uint64
	for _, frame := range snapshot {
		if frame == nil {
			continue
		}
		seq := frame.GetSequence()
		if seq == 0 {
			continue
		}
		if seq < fromSeq {
			continue
		}
		if toSeq > 0 && seq > toSeq {
			continue
		}
		if err := send(proto.Clone(frame).(*apiv1.MirrorFrame)); err != nil {
			return last, err
		}
		last = seq
	}
	return last, nil
}

type mirrorSubscriber struct {
	id uint64
	ch chan *apiv1.MirrorFrame
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func toProtoSession(s MirrorSession) *apiv1.MirrorSession {
	if strings.TrimSpace(s.SessionID) == "" {
		return &apiv1.MirrorSession{}
	}
	out := &apiv1.MirrorSession{
		SessionId:        s.SessionID,
		CreatedUnixNano:  s.CreatedUnixNano,
		LastSeenUnixNano: s.LastSeenUnixNano,
		LastSequence:     s.LastSequence,
		Meta:             toProtoMeta(s.Meta),
		Tags:             map[string]string{},
		Status:           toProtoStatus(s.Status),
	}
	for k, v := range s.Tags {
		k = strings.TrimSpace(k)
		v = strings.TrimSpace(v)
		if k == "" || v == "" {
			continue
		}
		out.Tags[k] = v
	}
	if len(out.Tags) == 0 {
		out.Tags = nil
	}
	return out
}

func toProtoStatus(st MirrorSessionStatus) *apiv1.MirrorSessionStatus {
	if st.State == MirrorSessionStateUnspecified &&
		st.ExitCode == 0 &&
		strings.TrimSpace(st.ErrorMessage) == "" &&
		st.CompletedUnixNano == 0 {
		return nil
	}
	return &apiv1.MirrorSessionStatus{
		State:             apiv1.MirrorSessionState(st.State),
		ExitCode:          st.ExitCode,
		ErrorMessage:      strings.TrimSpace(st.ErrorMessage),
		CompletedUnixNano: st.CompletedUnixNano,
	}
}

func fromProtoStatus(st *apiv1.MirrorSessionStatus) MirrorSessionStatus {
	if st == nil {
		return MirrorSessionStatus{}
	}
	return MirrorSessionStatus{
		State:             MirrorSessionState(st.GetState()),
		ExitCode:          st.GetExitCode(),
		ErrorMessage:      strings.TrimSpace(st.GetErrorMessage()),
		CompletedUnixNano: st.GetCompletedUnixNano(),
	}
}

func toProtoMeta(m MirrorSessionMeta) *apiv1.MirrorSessionMeta {
	if strings.TrimSpace(m.Command) == "" &&
		len(m.Args) == 0 &&
		strings.TrimSpace(m.Requester) == "" &&
		strings.TrimSpace(m.Cluster) == "" &&
		strings.TrimSpace(m.KubeContext) == "" &&
		strings.TrimSpace(m.Namespace) == "" &&
		strings.TrimSpace(m.Release) == "" &&
		strings.TrimSpace(m.Chart) == "" {
		return nil
	}
	args := append([]string(nil), m.Args...)
	return &apiv1.MirrorSessionMeta{
		Command:     strings.TrimSpace(m.Command),
		Args:        args,
		Requester:   strings.TrimSpace(m.Requester),
		Cluster:     strings.TrimSpace(m.Cluster),
		KubeContext: strings.TrimSpace(m.KubeContext),
		Namespace:   strings.TrimSpace(m.Namespace),
		Release:     strings.TrimSpace(m.Release),
		Chart:       strings.TrimSpace(m.Chart),
	}
}

func fromProtoMeta(m *apiv1.MirrorSessionMeta) MirrorSessionMeta {
	if m == nil {
		return MirrorSessionMeta{}
	}
	return MirrorSessionMeta{
		Command:     strings.TrimSpace(m.GetCommand()),
		Args:        append([]string(nil), m.GetArgs()...),
		Requester:   strings.TrimSpace(m.GetRequester()),
		Cluster:     strings.TrimSpace(m.GetCluster()),
		KubeContext: strings.TrimSpace(m.GetKubeContext()),
		Namespace:   strings.TrimSpace(m.GetNamespace()),
		Release:     strings.TrimSpace(m.GetRelease()),
		Chart:       strings.TrimSpace(m.GetChart()),
	}
}
