package agent

import (
	"context"

	apiv1 "github.com/kubekattle/ktl/pkg/api/ktl/api/v1"
)

// MirrorSessionMeta holds optional fields that make sessions searchable and
// self-describing for UIs and AI agents.
type MirrorSessionMeta struct {
	Command     string
	Args        []string
	Requester   string
	Cluster     string
	KubeContext string
	Namespace   string
	Release     string
	Chart       string
}

// MirrorSession holds lightweight metadata about a mirror session stored by a flight recorder.
type MirrorSession struct {
	SessionID        string
	CreatedUnixNano  int64
	LastSeenUnixNano int64
	LastSequence     uint64
	Meta             MirrorSessionMeta
	Tags             map[string]string
	Status           MirrorSessionStatus
}

type MirrorSessionState int32

const (
	MirrorSessionStateUnspecified MirrorSessionState = 0
	MirrorSessionStateRunning     MirrorSessionState = 1
	MirrorSessionStateDone        MirrorSessionState = 2
	MirrorSessionStateError       MirrorSessionState = 3
)

// MirrorSessionStatus is a lightweight lifecycle marker for UIs/agents.
type MirrorSessionStatus struct {
	State             MirrorSessionState
	ExitCode          int32
	ErrorMessage      string
	CompletedUnixNano int64
}

// MirrorStore is an optional durable backend for MirrorService.
//
// Append must be fast; implementations should batch/flush asynchronously.
type MirrorStore interface {
	Append(frame *apiv1.MirrorFrame) error
	UpsertSessionMeta(ctx context.Context, sessionID string, meta MirrorSessionMeta, tags map[string]string) error
	UpsertSessionStatus(ctx context.Context, sessionID string, st MirrorSessionStatus) error
	DeleteSession(ctx context.Context, sessionID string) (bool, error)
	GetSession(ctx context.Context, sessionID string) (MirrorSession, bool, error)
	ListSessions(ctx context.Context, limit int) ([]MirrorSession, error)
	Replay(ctx context.Context, sessionID string, fromSequence uint64, send func(*apiv1.MirrorFrame) error) (lastSequence uint64, _ error)
	Close() error
}
