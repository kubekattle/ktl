package caststream

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"

	"github.com/example/ktl/internal/deploy"
)

// deployState caches the most recent deploy webcast events so late-joining
// clients can hydrate their UI immediately instead of waiting for future
// updates.
type deployState struct {
	mu        sync.RWMutex
	summary   *deploy.StreamEvent
	diff      *deploy.StreamEvent
	resources *deploy.StreamEvent
	health    *deploy.StreamEvent
	phases    map[string]*deploy.StreamEvent
	logs      []*deploy.StreamEvent
}

const (
	maxCachedLogs = 300
)

var phaseReplayOrder = []string{
	deploy.PhaseRender,
	deploy.PhaseDiff,
	deploy.PhaseUpgrade,
	deploy.PhaseInstall,
	deploy.PhaseWait,
	deploy.PhasePostHooks,
}

func newDeployState() *deployState {
	return &deployState{
		phases: make(map[string]*deploy.StreamEvent),
	}
}

func (s *deployState) Record(event deploy.StreamEvent) {
	if s == nil {
		return
	}
	cp := cloneStreamEvent(event)
	nameKey := ""
	if cp.Phase != nil {
		nameKey = strings.TrimSpace(strings.ToLower(cp.Phase.Name))
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	switch cp.Kind {
	case deploy.StreamEventSummary:
		s.summary = &cp
	case deploy.StreamEventDiff:
		s.diff = &cp
	case deploy.StreamEventResources:
		s.resources = &cp
	case deploy.StreamEventHealth:
		s.health = &cp
	case deploy.StreamEventPhase:
		if nameKey == "" {
			return
		}
		s.phases[nameKey] = &cp
	case deploy.StreamEventLog:
		if cp.Log == nil || cp.Log.Message == "" {
			return
		}
		s.logs = append(s.logs, &cp)
		if overflow := len(s.logs) - maxCachedLogs; overflow > 0 {
			s.logs = s.logs[overflow:]
		}
	}
}

func (s *deployState) Replay(out chan<- []byte) {
	if s == nil || out == nil {
		return
	}
	for _, evt := range s.snapshot() {
		payload, err := json.Marshal(evt)
		if err != nil {
			continue
		}
		if !safeEnqueue(out, payload) {
			return
		}
	}
}

func (s *deployState) snapshot() []deploy.StreamEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var snapshot []deploy.StreamEvent
	if s.summary != nil {
		snapshot = append(snapshot, cloneStreamEvent(*s.summary))
	}
	if len(s.phases) > 0 {
		appendPhase := func(key string) {
			if evt, ok := s.phases[strings.TrimSpace(strings.ToLower(key))]; ok && evt != nil {
				snapshot = append(snapshot, cloneStreamEvent(*evt))
			}
		}
		for _, name := range phaseReplayOrder {
			appendPhase(name)
		}
		if len(s.phases) > len(phaseReplayOrder) {
			var extras []string
			for key := range s.phases {
				if !containsPhase(key) {
					extras = append(extras, key)
				}
			}
			sort.Strings(extras)
			for _, key := range extras {
				appendPhase(key)
			}
		}
	}
	if s.resources != nil {
		snapshot = append(snapshot, cloneStreamEvent(*s.resources))
	}
	if s.health != nil {
		snapshot = append(snapshot, cloneStreamEvent(*s.health))
	}
	if s.diff != nil {
		snapshot = append(snapshot, cloneStreamEvent(*s.diff))
	}
	for _, log := range s.logs {
		if log == nil {
			continue
		}
		snapshot = append(snapshot, cloneStreamEvent(*log))
	}
	return snapshot
}

func safeEnqueue(out chan<- []byte, payload []byte) (ok bool) {
	defer func() {
		if r := recover(); r != nil {
			ok = false
		}
	}()
	ok = true
	out <- payload
	return
}

func containsPhase(name string) bool {
	name = strings.TrimSpace(strings.ToLower(name))
	for _, candidate := range phaseReplayOrder {
		if strings.TrimSpace(strings.ToLower(candidate)) == name {
			return true
		}
	}
	return false
}

func cloneStreamEvent(event deploy.StreamEvent) deploy.StreamEvent {
	cloned := event
	if event.Phase != nil {
		cp := *event.Phase
		cloned.Phase = &cp
	}
	if event.Log != nil {
		cp := *event.Log
		cloned.Log = &cp
	}
	if event.Diff != nil {
		cp := *event.Diff
		cloned.Diff = &cp
	}
	if event.Summary != nil {
		cp := *event.Summary
		if len(event.Summary.History) > 0 {
			cp.History = append([]deploy.HistoryBreadcrumb(nil), event.Summary.History...)
		}
		if event.Summary.LastSuccessful != nil {
			last := *event.Summary.LastSuccessful
			cp.LastSuccessful = &last
		}
		cloned.Summary = &cp
	}
	if event.Health != nil {
		cp := *event.Health
		cloned.Health = &cp
	}
	if len(event.Resources) > 0 {
		cloned.Resources = append([]deploy.ResourceStatus(nil), event.Resources...)
	}
	return cloned
}
