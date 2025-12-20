package caststream

import "context"

// CaptureController enables the log UI to start/stop captures from the browser.
// Implementations live in cmd/ktl (so they can reuse kubeconfig + filters).
type CaptureController interface {
	Start(ctx context.Context) (CaptureStatus, error)
	Stop(ctx context.Context) (CaptureStatus, CaptureView, error)
	Status(ctx context.Context) (CaptureStatus, error)
}

type CaptureStatus struct {
	Running     bool   `json:"running"`
	ID          string `json:"id,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	Artifact    string `json:"artifact,omitempty"`
	LastError   string `json:"lastError,omitempty"`
	ViewerReady bool   `json:"viewerReady,omitempty"`
}

type CaptureView struct {
	ID   string `json:"id"`
	HTML string `json:"-"`
}
