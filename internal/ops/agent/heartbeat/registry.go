package heartbeat

import (
	"sort"
	"time"
)

const (
	SnapshotAPIVersion = "torque.dev/agent-registry/v1"
	SnapshotKind       = "AgentStatusSnapshot"
)

type Registry struct {
	heartbeats map[string]Heartbeat
}

type SnapshotRequest struct {
	Tenant     string
	Selector   map[string]string
	Now        time.Time
	StaleAfter time.Duration
}

type Snapshot struct {
	APIVersion  string        `json:"apiVersion"`
	Kind        string        `json:"kind"`
	Tenant      string        `json:"tenant"`
	GeneratedAt string        `json:"generatedAt"`
	StaleAfter  string        `json:"staleAfter"`
	Summary     Summary       `json:"summary"`
	Agents      []AgentStatus `json:"agents"`
}

type Summary struct {
	Total    int `json:"total"`
	Ready    int `json:"ready"`
	Stale    int `json:"stale"`
	Degraded int `json:"degraded"`
	Draining int `json:"draining"`
	Offline  int `json:"offline"`
}

type AgentStatus struct {
	AgentID        string            `json:"agentId"`
	Tenant         string            `json:"tenant"`
	TargetID       string            `json:"targetId,omitempty"`
	Hostname       string            `json:"hostname,omitempty"`
	Version        string            `json:"version,omitempty"`
	State          string            `json:"state"`
	Health         string            `json:"health"`
	LastSeen       string            `json:"lastSeen"`
	Age            string            `json:"age"`
	Labels         map[string]string `json:"labels,omitempty"`
	Capabilities   []string          `json:"capabilities,omitempty"`
	Slots          Slots             `json:"slots,omitempty"`
	Offsets        Offsets           `json:"offsets,omitempty"`
	Resources      Resources         `json:"resources,omitempty"`
	EvidenceOffset *StreamOffset     `json:"evidenceOffset,omitempty"`
}

func NewRegistry() *Registry {
	return &Registry{heartbeats: map[string]Heartbeat{}}
}

func (r *Registry) Apply(heartbeat Heartbeat) error {
	if err := heartbeat.Validate(); err != nil {
		return err
	}
	if r.heartbeats == nil {
		r.heartbeats = map[string]Heartbeat{}
	}
	existing, ok := r.heartbeats[heartbeat.AgentID]
	if ok {
		existingObserved, err := existing.ObservedTime()
		if err == nil {
			nextObserved, nextErr := heartbeat.ObservedTime()
			if nextErr == nil && nextObserved.Before(existingObserved) {
				return nil
			}
		}
	}
	r.heartbeats[heartbeat.AgentID] = heartbeat
	return nil
}

func (r *Registry) Snapshot(req SnapshotRequest) Snapshot {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	staleAfter := req.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	tenant := NormalizeTenant(req.Tenant)
	agents := make([]AgentStatus, 0, len(r.heartbeats))
	for _, heartbeat := range r.heartbeats {
		if NormalizeTenant(heartbeat.Tenant) != tenant {
			continue
		}
		if !matchesSelector(heartbeat.Labels, req.Selector) {
			continue
		}
		status := agentStatus(heartbeat, now.UTC(), staleAfter)
		agents = append(agents, status)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].AgentID < agents[j].AgentID
	})
	return snapshotFromAgents(tenant, now.UTC(), staleAfter, agents)
}

func agentStatus(heartbeat Heartbeat, now time.Time, staleAfter time.Duration) AgentStatus {
	observedAt, err := heartbeat.ObservedTime()
	if err != nil {
		observedAt = time.Time{}
	}
	age := now.Sub(observedAt)
	if age < 0 {
		age = 0
	}
	health := heartbeat.State
	if age > staleAfter {
		health = "stale"
	} else if health == StateReady {
		health = "ready"
	}
	return AgentStatus{
		AgentID:      heartbeat.AgentID,
		Tenant:       NormalizeTenant(heartbeat.Tenant),
		TargetID:     heartbeat.TargetID,
		Hostname:     heartbeat.Hostname,
		Version:      heartbeat.Version,
		State:        heartbeat.State,
		Health:       health,
		LastSeen:     observedAt.UTC().Format(time.RFC3339Nano),
		Age:          formatAge(age),
		Labels:       cleanLabels(heartbeat.Labels),
		Capabilities: cleanList(heartbeat.Capabilities),
		Slots:        heartbeat.Slots,
		Offsets:      cleanOffsets(heartbeat.Offsets),
		Resources:    heartbeat.Resources,
	}
}

func matchesSelector(labels map[string]string, selector map[string]string) bool {
	if len(selector) == 0 {
		return true
	}
	for key, value := range selector {
		if labels[key] != value {
			return false
		}
	}
	return true
}

func formatAge(age time.Duration) string {
	if age < 0 {
		age = 0
	}
	if age < time.Second {
		return "0s"
	}
	return age.Truncate(time.Second).String()
}

func snapshotFromAgents(tenant string, now time.Time, staleAfter time.Duration, agents []AgentStatus) Snapshot {
	summary := Summary{Total: len(agents)}
	for _, agent := range agents {
		switch agent.Health {
		case "ready":
			summary.Ready++
		case "stale":
			summary.Stale++
		case StateDegraded:
			summary.Degraded++
		case StateDraining:
			summary.Draining++
		case StateOffline:
			summary.Offline++
		}
	}
	return Snapshot{
		APIVersion:  SnapshotAPIVersion,
		Kind:        SnapshotKind,
		Tenant:      NormalizeTenant(tenant),
		GeneratedAt: now.UTC().Format(time.RFC3339Nano),
		StaleAfter:  staleAfter.String(),
		Summary:     summary,
		Agents:      agents,
	}
}
