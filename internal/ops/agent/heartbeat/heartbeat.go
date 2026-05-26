package heartbeat

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"regexp"
	"sort"
	"strings"
	"time"
)

const (
	APIVersion = "torque.dev/agent-heartbeat/v1"
	Kind       = "AgentHeartbeat"

	DefaultTenant          = "default"
	DefaultShardCount      = 256
	DefaultEventStream     = "TORQUE_AGENT_EVENTS"
	DefaultRegistryDurable = "torque-agent-registry"

	StateReady    = "ready"
	StateDegraded = "degraded"
	StateDraining = "draining"
	StateOffline  = "offline"
)

var subjectTokenPattern = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type Slots struct {
	Total int `json:"total,omitempty"`
	InUse int `json:"inUse,omitempty"`
}

type Offsets struct {
	Assignment string `json:"assignment,omitempty"`
	Receipt    string `json:"receipt,omitempty"`
}

type Resources struct {
	Load1             float64 `json:"load1,omitempty"`
	MemoryFreeBytes   int64   `json:"memoryFreeBytes,omitempty"`
	DiskFreeBytes     int64   `json:"diskFreeBytes,omitempty"`
	ConnectedNATSName string  `json:"connectedNatsName,omitempty"`
}

type Heartbeat struct {
	APIVersion       string            `json:"apiVersion"`
	Kind             string            `json:"kind"`
	AgentID          string            `json:"agentId"`
	Tenant           string            `json:"tenant"`
	TargetID         string            `json:"targetId,omitempty"`
	Hostname         string            `json:"hostname,omitempty"`
	Version          string            `json:"version,omitempty"`
	Labels           map[string]string `json:"labels,omitempty"`
	Capabilities     []string          `json:"capabilities,omitempty"`
	CapabilityDigest string            `json:"capabilityDigest,omitempty"`
	Slots            Slots             `json:"slots,omitempty"`
	Offsets          Offsets           `json:"offsets,omitempty"`
	Resources        Resources         `json:"resources,omitempty"`
	State            string            `json:"state"`
	ObservedAt       string            `json:"observedAt"`
}

type Options struct {
	AgentID          string
	Tenant           string
	TargetID         string
	Hostname         string
	Version          string
	Labels           map[string]string
	Capabilities     []string
	CapabilityDigest string
	Slots            Slots
	Offsets          Offsets
	Resources        Resources
	State            string
	ObservedAt       time.Time
}

func New(opts Options) Heartbeat {
	observedAt := opts.ObservedAt
	if observedAt.IsZero() {
		observedAt = time.Now()
	}
	state := strings.ToLower(strings.TrimSpace(opts.State))
	if state == "" {
		state = StateReady
	}
	agentID := strings.TrimSpace(opts.AgentID)
	targetID := strings.TrimSpace(opts.TargetID)
	if targetID == "" {
		targetID = agentID
	}
	return Heartbeat{
		APIVersion:       APIVersion,
		Kind:             Kind,
		AgentID:          agentID,
		Tenant:           NormalizeTenant(opts.Tenant),
		TargetID:         targetID,
		Hostname:         strings.TrimSpace(opts.Hostname),
		Version:          strings.TrimSpace(opts.Version),
		Labels:           cleanLabels(opts.Labels),
		Capabilities:     cleanList(opts.Capabilities),
		CapabilityDigest: strings.TrimSpace(opts.CapabilityDigest),
		Slots:            opts.Slots,
		Offsets:          cleanOffsets(opts.Offsets),
		Resources:        opts.Resources,
		State:            state,
		ObservedAt:       observedAt.UTC().Format(time.RFC3339Nano),
	}
}

func Parse(raw []byte) (Heartbeat, error) {
	var heartbeat Heartbeat
	if err := json.Unmarshal(raw, &heartbeat); err != nil {
		return Heartbeat{}, fmt.Errorf("parse agent heartbeat: %w", err)
	}
	heartbeat.APIVersion = strings.TrimSpace(heartbeat.APIVersion)
	heartbeat.Kind = strings.TrimSpace(heartbeat.Kind)
	heartbeat.AgentID = strings.TrimSpace(heartbeat.AgentID)
	heartbeat.Tenant = NormalizeTenant(heartbeat.Tenant)
	heartbeat.TargetID = strings.TrimSpace(heartbeat.TargetID)
	heartbeat.Hostname = strings.TrimSpace(heartbeat.Hostname)
	heartbeat.Version = strings.TrimSpace(heartbeat.Version)
	heartbeat.Labels = cleanLabels(heartbeat.Labels)
	heartbeat.Capabilities = cleanList(heartbeat.Capabilities)
	heartbeat.CapabilityDigest = strings.TrimSpace(heartbeat.CapabilityDigest)
	heartbeat.Offsets = cleanOffsets(heartbeat.Offsets)
	heartbeat.State = strings.ToLower(strings.TrimSpace(heartbeat.State))
	heartbeat.ObservedAt = strings.TrimSpace(heartbeat.ObservedAt)
	if err := heartbeat.Validate(); err != nil {
		return Heartbeat{}, err
	}
	return heartbeat, nil
}

func (h Heartbeat) Validate() error {
	if h.APIVersion != APIVersion {
		return fmt.Errorf("unsupported heartbeat apiVersion %q", h.APIVersion)
	}
	if h.Kind != Kind {
		return fmt.Errorf("unsupported heartbeat kind %q", h.Kind)
	}
	if strings.TrimSpace(h.AgentID) == "" {
		return fmt.Errorf("heartbeat agentId is required")
	}
	if strings.TrimSpace(h.State) == "" {
		return fmt.Errorf("heartbeat state is required")
	}
	switch strings.ToLower(strings.TrimSpace(h.State)) {
	case StateReady, StateDegraded, StateDraining, StateOffline:
	default:
		return fmt.Errorf("unsupported heartbeat state %q", h.State)
	}
	if _, err := h.ObservedTime(); err != nil {
		return err
	}
	return nil
}

func (h Heartbeat) ObservedTime() (time.Time, error) {
	raw := strings.TrimSpace(h.ObservedAt)
	if raw == "" {
		return time.Time{}, fmt.Errorf("heartbeat observedAt is required")
	}
	observedAt, err := time.Parse(time.RFC3339Nano, raw)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse heartbeat observedAt: %w", err)
	}
	return observedAt.UTC(), nil
}

func (h Heartbeat) MarshalJSONBytes() ([]byte, error) {
	return json.Marshal(h)
}

func Subject(tenant string, shardCount int, agentID string) string {
	agentToken := NormalizeSubjectToken(agentID, "agent")
	return fmt.Sprintf("torque.v1.agent.heartbeat.%s.%03d.%s", NormalizeTenant(tenant), ShardForAgent(agentToken, shardCount), agentToken)
}

func SubjectWildcard(tenant string) string {
	return fmt.Sprintf("torque.v1.agent.heartbeat.%s.*.*", NormalizeTenant(tenant))
}

func ShardForAgent(agentID string, shardCount int) int {
	if shardCount <= 0 {
		shardCount = DefaultShardCount
	}
	hash := fnv.New32a()
	_, _ = hash.Write([]byte(NormalizeSubjectToken(agentID, "agent")))
	return int(hash.Sum32() % uint32(shardCount))
}

func NormalizeTenant(tenant string) string {
	return NormalizeSubjectToken(tenant, DefaultTenant)
}

func NormalizeSubjectToken(value string, fallback string) string {
	token := strings.TrimSpace(value)
	if token == "" {
		token = fallback
	}
	token = subjectTokenPattern.ReplaceAllString(token, "_")
	token = strings.Trim(token, "_")
	if token == "" {
		return fallback
	}
	return token
}

func cleanLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}
	out := make(map[string]string, len(labels))
	for key, value := range labels {
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		if key == "" || value == "" {
			continue
		}
		out[key] = value
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func cleanList(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	seen := map[string]struct{}{}
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	sort.Strings(out)
	return out
}

func cleanOffsets(offsets Offsets) Offsets {
	return Offsets{
		Assignment: strings.TrimSpace(offsets.Assignment),
		Receipt:    strings.TrimSpace(offsets.Receipt),
	}
}
