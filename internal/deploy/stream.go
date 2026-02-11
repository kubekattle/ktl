// File: internal/deploy/stream.go
// Brief: Internal deploy package implementation for 'stream'.

// Package deploy provides deploy helpers.

package deploy

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/tailer"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
)

var statusTitleCaser = cases.Title(language.Und, cases.NoLower)

// StreamEventKind enumerates the payload types used by the deploy webcast.
type StreamEventKind string

const (
	StreamEventPhase     StreamEventKind = "phase"
	StreamEventLog       StreamEventKind = "event"
	StreamEventResources StreamEventKind = "resources"
	StreamEventDiff      StreamEventKind = "diff"
	StreamEventSummary   StreamEventKind = "summary"
	StreamEventHealth    StreamEventKind = "health"
)

// StreamObserver consumes deploy webcast events.
type StreamObserver interface {
	HandleDeployEvent(StreamEvent)
}

// StreamEvent is the envelope shared over the deploy webcast WebSocket.
type StreamEvent struct {
	Kind      StreamEventKind  `json:"kind"`
	Timestamp string           `json:"ts"`
	Phase     *PhasePayload    `json:"phase,omitempty"`
	Log       *LogPayload      `json:"log,omitempty"`
	Resources []ResourceStatus `json:"resources,omitempty"`
	Diff      *DiffPayload     `json:"diff,omitempty"`
	Summary   *SummaryPayload  `json:"summary,omitempty"`
	Health    *HealthSnapshot  `json:"health,omitempty"`
}

// HistoryBreadcrumb captures a prior Helm revision so UIs can show run breadcrumbs.
type HistoryBreadcrumb struct {
	Revision    int    `json:"revision"`
	Status      string `json:"status"`
	Chart       string `json:"chart,omitempty"`
	Version     string `json:"version,omitempty"`
	AppVersion  string `json:"appVersion,omitempty"`
	Description string `json:"description,omitempty"`
	DeployedAt  string `json:"deployedAt,omitempty"`
}

// PhasePayload captures timeline updates for Helm phases.
type PhasePayload struct {
	Name        string `json:"name"`
	State       string `json:"state"`
	Status      string `json:"status"`
	Message     string `json:"message,omitempty"`
	StartedAt   string `json:"startedAt,omitempty"`
	CompletedAt string `json:"completedAt,omitempty"`
	startTime   time.Time
	endTime     time.Time
}

// LogPayload reflects an emitted log/event message.
type LogPayload struct {
	Level     string `json:"level"`
	Message   string `json:"message"`
	Source    string `json:"source,omitempty"`
	Namespace string `json:"namespace,omitempty"`
	Pod       string `json:"pod,omitempty"`
	Container string `json:"container,omitempty"`
}

// DiffPayload describes a rendered diff, if requested.
type DiffPayload struct {
	Text    string `json:"text"`
	HasDiff bool   `json:"hasDiff"`
}

// SummaryPayload surfaces the final release outcome.
type SummaryPayload struct {
	Release        string              `json:"release"`
	Namespace      string              `json:"namespace"`
	Status         string              `json:"status"`
	Chart          string              `json:"chart,omitempty"`
	Version        string              `json:"version,omitempty"`
	Action         string              `json:"action,omitempty"`
	Notes          string              `json:"notes,omitempty"`
	Error          string              `json:"error,omitempty"`
	Duration       string              `json:"duration,omitempty"`
	PhaseDurations map[string]string   `json:"phaseDurations,omitempty"`
	History        []HistoryBreadcrumb `json:"history,omitempty"`
	LastSuccessful *HistoryBreadcrumb  `json:"lastSuccessful,omitempty"`
	Secrets        []SecretRef         `json:"secrets,omitempty"`
}

// HealthSnapshot aggregates readiness stats for the release.
type HealthSnapshot struct {
	Ready       int    `json:"ready"`
	Progressing int    `json:"progressing"`
	Failed      int    `json:"failed"`
	Pending     int    `json:"pending"`
	Total       int    `json:"total"`
	LastUpdated string `json:"lastUpdated"`
}

var defaultDeployPhases = []string{PhaseRender, PhaseDiff, PhaseUpgrade, PhaseInstall, PhaseWait, PhasePostHooks}

// StreamBroadcaster fan-outs deploy telemetry to zero or more observers.
type StreamBroadcaster struct {
	mu               sync.Mutex
	release          string
	namespace        string
	chart            string
	observers        []StreamObserver
	phases           map[string]*PhasePayload
	start            time.Time
	resourceLogState map[string]string
}

// NewStreamBroadcaster constructs a deploy stream broadcaster for the given release.
func NewStreamBroadcaster(release, namespace, chart string) *StreamBroadcaster {
	b := &StreamBroadcaster{
		release:          strings.TrimSpace(release),
		namespace:        strings.TrimSpace(namespace),
		chart:            strings.TrimSpace(chart),
		phases:           make(map[string]*PhasePayload),
		resourceLogState: make(map[string]string),
		start:            time.Now(),
	}
	for _, name := range defaultDeployPhases {
		lower := strings.TrimSpace(name)
		if lower == "" {
			continue
		}
		b.phases[lower] = &PhasePayload{Name: lower, State: "pending", Status: "pending"}
	}
	return b
}

// AddObserver registers a sink for webcast events.
func (b *StreamBroadcaster) AddObserver(obs StreamObserver) {
	if b == nil || obs == nil {
		return
	}
	b.mu.Lock()
	b.observers = append(b.observers, obs)
	phases := make([]*PhasePayload, 0, len(b.phases))
	for _, phase := range b.phases {
		snapshot := *phase
		phases = append(phases, &snapshot)
	}
	b.mu.Unlock()
	for _, phase := range phases {
		b.broadcast(StreamEvent{Kind: StreamEventPhase, Phase: phase})
	}
}

// HasObservers reports whether anything is listening to the stream.
func (b *StreamBroadcaster) HasObservers() bool {
	if b == nil {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.observers) > 0
}

// WantsResourceSnapshots indicates whether the streamer needs live resource status.
func (b *StreamBroadcaster) WantsResourceSnapshots() bool {
	return b.HasObservers()
}

// PhaseStarted marks a phase as running.
func (b *StreamBroadcaster) PhaseStarted(name string) {
	now := time.Now()
	b.updatePhase(name, func(phase *PhasePayload) {
		phase.State = "running"
		phase.Status = "running"
		phase.Message = ""
		phase.startTime = now
		phase.StartedAt = timestamp(now)
		phase.CompletedAt = ""
		phase.endTime = time.Time{}
	})
}

// PhaseCompleted marks a phase as completed with an explicit status (succeeded, failed, skipped).
func (b *StreamBroadcaster) PhaseCompleted(name, status, message string) {
	status = normalizePhaseStatus(status)
	now := time.Now()
	b.updatePhase(name, func(phase *PhasePayload) {
		phase.State = status
		phase.Status = status
		phase.Message = strings.TrimSpace(message)
		if phase.StartedAt == "" {
			phase.startTime = now
			phase.StartedAt = timestamp(now)
		}
		phase.endTime = now
		phase.CompletedAt = timestamp(now)
	})
}

// EmitEvent records an informational/warning/error line.
func (b *StreamBroadcaster) EmitEvent(level, message string) {
	if b == nil {
		return
	}
	message = strings.TrimSpace(message)
	if message == "" {
		return
	}
	level = normalizeLevel(level)
	b.broadcast(StreamEvent{
		Kind: StreamEventLog,
		Log: &LogPayload{
			Level:   level,
			Message: message,
			Source:  "helm",
		},
	})
}

// SetDiff shares the rendered diff (if any).
func (b *StreamBroadcaster) SetDiff(diff string) {
	if b == nil {
		return
	}
	diff = strings.TrimSpace(diff)
	payload := &DiffPayload{Text: diff, HasDiff: diff != ""}
	b.broadcast(StreamEvent{Kind: StreamEventDiff, Diff: payload})
}

// UpdateResources pushes a fresh snapshot of release resource readiness.
func (b *StreamBroadcaster) UpdateResources(rows []ResourceStatus) {
	if b == nil || !b.HasObservers() {
		return
	}
	cp := make([]ResourceStatus, len(rows))
	copy(cp, rows)
	b.broadcast(StreamEvent{Kind: StreamEventResources, Resources: cp})
	b.broadcast(StreamEvent{Kind: StreamEventHealth, Health: summarizeHealth(cp)})
	b.emitResourceLogs(cp)
}

// EmitSummary publishes the final release outcome.
func (b *StreamBroadcaster) EmitSummary(summary SummaryPayload) {
	if b == nil {
		return
	}
	if summary.Release == "" {
		summary.Release = b.release
	}
	if summary.Namespace == "" {
		summary.Namespace = b.namespace
	}
	if summary.Chart == "" {
		summary.Chart = b.chart
	}
	if summary.Duration == "" && !b.start.IsZero() {
		summary.Duration = time.Since(b.start).Truncate(100 * time.Millisecond).String()
	}
	if summary.PhaseDurations == nil {
		summary.PhaseDurations = b.phaseDurations()
	}
	b.broadcast(StreamEvent{Kind: StreamEventSummary, Summary: &summary})
}

func (b *StreamBroadcaster) phaseDurations() map[string]string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if len(b.phases) == 0 {
		return nil
	}
	out := make(map[string]string, len(b.phases))
	for name, payload := range b.phases {
		if payload.startTime.IsZero() || payload.endTime.IsZero() {
			continue
		}
		out[name] = payload.endTime.Sub(payload.startTime).Truncate(100 * time.Millisecond).String()
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// ObserveLog satisfies tailer.LogObserver so Kubernetes events/logs can join the webcast feed.
func (b *StreamBroadcaster) ObserveLog(record tailer.LogRecord) {
	if b == nil || !b.HasObservers() {
		return
	}
	message := strings.TrimSpace(record.Rendered)
	if message == "" {
		message = strings.TrimSpace(record.Raw)
	}
	if message == "" {
		return
	}
	ts := record.Timestamp
	b.broadcast(StreamEvent{
		Kind:      StreamEventLog,
		Timestamp: timestamp(ts),
		Log: &LogPayload{
			Level:     "info",
			Message:   message,
			Source:    record.Source,
			Namespace: record.Namespace,
			Pod:       record.Pod,
			Container: record.Container,
		},
	})
}

func (b *StreamBroadcaster) updatePhase(name string, mutate func(*PhasePayload)) {
	if b == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	b.mu.Lock()
	phase, ok := b.phases[name]
	if !ok {
		phase = &PhasePayload{Name: name, State: "pending", Status: "pending"}
		b.phases[name] = phase
	}
	mutate(phase)
	snapshot := *phase
	observers := append([]StreamObserver(nil), b.observers...)
	b.mu.Unlock()
	event := StreamEvent{Kind: StreamEventPhase, Phase: &snapshot}
	for _, obs := range observers {
		obs.HandleDeployEvent(event)
	}
}

func (b *StreamBroadcaster) broadcast(event StreamEvent) {
	if b == nil {
		return
	}
	if event.Timestamp == "" {
		event.Timestamp = timestamp(time.Now())
	}
	b.mu.Lock()
	observers := append([]StreamObserver(nil), b.observers...)
	b.mu.Unlock()
	for _, obs := range observers {
		obs.HandleDeployEvent(event)
	}
}

func timestamp(t time.Time) string {
	if t.IsZero() {
		t = time.Now()
	}
	return t.UTC().Format(time.RFC3339Nano)
}

func normalizeLevel(level string) string {
	level = strings.ToLower(strings.TrimSpace(level))
	switch level {
	case "debug", "info", "warn", "warning", "error":
		if level == "warning" {
			return "warn"
		}
		return level
	default:
		return "info"
	}
}

func normalizePhaseStatus(status string) string {
	status = strings.ToLower(strings.TrimSpace(status))
	switch status {
	case "failed", "succeeded", "skipped", "running":
		return status
	case "success":
		return "succeeded"
	default:
		return "succeeded"
	}
}

func summarizeHealth(rows []ResourceStatus) *HealthSnapshot {
	snap := &HealthSnapshot{LastUpdated: timestamp(time.Now())}
	if len(rows) == 0 {
		return snap
	}
	for _, row := range rows {
		snap.Total++
		switch strings.ToLower(strings.TrimSpace(row.Status)) {
		case "ready":
			snap.Ready++
		case "failed":
			snap.Failed++
		case "progressing":
			snap.Progressing++
		default:
			snap.Pending++
		}
	}
	return snap
}

func (b *StreamBroadcaster) emitResourceLogs(rows []ResourceStatus) {
	if b == nil {
		return
	}
	type pendingLog struct {
		level   string
		message string
	}
	var events []pendingLog
	seen := make(map[string]struct{}, len(rows))

	b.mu.Lock()
	if b.resourceLogState == nil {
		b.resourceLogState = make(map[string]string)
	}
	for _, row := range rows {
		key := resourceStatusKey(row)
		seen[key] = struct{}{}
		state := canonicalResourceState(row.Status)
		message := strings.TrimSpace(row.Message)
		if state == "" {
			state = "unknown"
		}
		signature := state + "|" + message
		if prev, ok := b.resourceLogState[key]; ok && prev == signature {
			continue
		}
		b.resourceLogState[key] = signature
		text := buildResourceLogMessage(row, state, message)
		level := severityForStatus(state)
		events = append(events, pendingLog{level: level, message: text})
	}
	for key := range b.resourceLogState {
		if _, ok := seen[key]; !ok {
			delete(b.resourceLogState, key)
		}
	}
	b.mu.Unlock()

	for _, evt := range events {
		b.EmitEvent(evt.level, evt.message)
	}
}

func resourceStatusKey(row ResourceStatus) string {
	ns := strings.TrimSpace(row.Namespace)
	if ns == "" {
		ns = "-"
	}
	return fmt.Sprintf("%s/%s/%s", strings.ToLower(strings.TrimSpace(row.Kind)), ns, strings.TrimSpace(row.Name))
}

func canonicalResourceState(status string) string {
	return strings.ToLower(strings.TrimSpace(status))
}

func severityForStatus(status string) string {
	switch status {
	case "failed", "error":
		return "error"
	case "progressing", "pending", "waiting", "running":
		return "warn"
	default:
		return "info"
	}
}

func buildResourceLogMessage(row ResourceStatus, status, message string) string {
	ns := strings.TrimSpace(row.Namespace)
	if ns == "" {
		ns = "-"
	}
	name := strings.TrimSpace(row.Name)
	target := fmt.Sprintf("%s %s/%s", strings.TrimSpace(row.Kind), ns, name)
	if message != "" {
		return fmt.Sprintf("%s: %s", target, message)
	}
	if status == "" {
		status = "pending"
	}
	return fmt.Sprintf("%s: %s", target, statusTitleCaser.String(status))
}
