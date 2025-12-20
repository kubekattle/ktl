// File: internal/deploy/progress.go
// Brief: Internal deploy package implementation for 'progress'.

// Package deploy provides deploy helpers.

package deploy

// Deploy phase identifiers shared by CLI, observers, and the web UI timeline.
const (
	PhaseRender    = "render"
	PhaseDiff      = "diff"
	PhaseUpgrade   = "upgrade"
	PhaseInstall   = "install"
	PhaseWait      = "wait"
	PhasePostHooks = "post-hooks"
)

// ProgressObserver receives instrumentation callbacks during Helm install/upgrade.
type ProgressObserver interface {
	PhaseStarted(name string)
	PhaseCompleted(name, status, message string)
	EmitEvent(level, message string)
	SetDiff(diff string)
}
