package stack

// RunEventType enumerates structured stack run events.
//
// These values are persisted in the sqlite state store and are consumed by
// `ktl stack status --follow` and the stack run renderers.
type RunEventType string

const (
	RunStarted     RunEventType = "RUN_STARTED"
	RunCompleted   RunEventType = "RUN_COMPLETED"
	RunConcurrency RunEventType = "RUN_CONCURRENCY"

	NodeMeta RunEventType = "NODE_META"

	NodeQueued    RunEventType = "NODE_QUEUED"
	NodeRunning   RunEventType = "NODE_RUNNING"
	NodeSucceeded RunEventType = "NODE_SUCCEEDED"
	NodeFailed    RunEventType = "NODE_FAILED"
	NodeBlocked   RunEventType = "NODE_BLOCKED"

	PhaseStarted   RunEventType = "PHASE_STARTED"
	PhaseCompleted RunEventType = "PHASE_COMPLETED"

	HookStarted   RunEventType = "HOOK_STARTED"
	HookSucceeded RunEventType = "HOOK_SUCCEEDED"
	HookFailed    RunEventType = "HOOK_FAILED"
	HookSkipped   RunEventType = "HOOK_SKIPPED"

	BudgetWait     RunEventType = "BUDGET_WAIT"
	RetryScheduled RunEventType = "RETRY_SCHEDULED"

	// NodeLog is an ephemeral, non-durable event used for verbose rendering.
	// It is not expected to be stored in sqlite.
	NodeLog RunEventType = "NODE_LOG"

	// HelmLog is an optional, durable log stream captured from Helm operations.
	// It is intended to be stored in sqlite when enabled by the caller.
	HelmLog RunEventType = "HELM_LOG"
)

type RunEventObserver interface {
	ObserveRunEvent(RunEvent)
}

type RunEventObserverFunc func(RunEvent)

func (f RunEventObserverFunc) ObserveRunEvent(ev RunEvent) {
	if f == nil {
		return
	}
	f(ev)
}
