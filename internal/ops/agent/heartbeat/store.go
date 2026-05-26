package heartbeat

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	CompactStatusKind  = "AgentCompactStatus"
	DefaultStorePrefix = "/torque"
)

type StreamOffset struct {
	Stream       string `json:"stream,omitempty"`
	Consumer     string `json:"consumer,omitempty"`
	Subject      string `json:"subject,omitempty"`
	Sequence     uint64 `json:"sequence,omitempty"`
	NumDelivered uint64 `json:"numDelivered,omitempty"`
	NumPending   uint64 `json:"numPending,omitempty"`
	ReceivedAt   string `json:"receivedAt,omitempty"`
}

type CompactRecord struct {
	APIVersion     string       `json:"apiVersion"`
	Kind           string       `json:"kind"`
	Tenant         string       `json:"tenant"`
	AgentID        string       `json:"agentId"`
	Heartbeat      Heartbeat    `json:"heartbeat"`
	Status         AgentStatus  `json:"status"`
	EvidenceOffset StreamOffset `json:"evidenceOffset,omitempty"`
	UpdatedAt      string       `json:"updatedAt"`
}

type RegistryStore interface {
	Put(ctx context.Context, record CompactRecord) error
	List(ctx context.Context, tenant string) ([]CompactRecord, error)
	Close() error
}

type MemoryStore struct {
	mu      sync.Mutex
	records map[string]CompactRecord
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{records: map[string]CompactRecord{}}
}

func (s *MemoryStore) Put(ctx context.Context, record CompactRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.records == nil {
		s.records = map[string]CompactRecord{}
	}
	s.records[RegistryStoreKey("", record.Tenant, record.AgentID)] = record
	return nil
}

func (s *MemoryStore) List(ctx context.Context, tenant string) ([]CompactRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := RegistryStorePrefix("", tenant)
	out := make([]CompactRecord, 0, len(s.records))
	for key, record := range s.records {
		if strings.HasPrefix(key, prefix) {
			out = append(out, record)
		}
	}
	sortCompactRecords(out)
	return out, nil
}

func (s *MemoryStore) Close() error {
	return nil
}

type FileStore struct {
	path string
	mu   sync.Mutex
}

func NewFileStore(path string) (*FileStore, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, fmt.Errorf("file store path is required")
	}
	return &FileStore{path: path}, nil
}

func (s *FileStore) Put(ctx context.Context, record CompactRecord) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := record.Validate(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return err
	}
	records[RegistryStoreKey("", record.Tenant, record.AgentID)] = record
	return s.save(records)
}

func (s *FileStore) List(ctx context.Context, tenant string) ([]CompactRecord, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	records, err := s.load()
	if err != nil {
		return nil, err
	}
	prefix := RegistryStorePrefix("", tenant)
	out := make([]CompactRecord, 0, len(records))
	for key, record := range records {
		if strings.HasPrefix(key, prefix) {
			out = append(out, record)
		}
	}
	sortCompactRecords(out)
	return out, nil
}

func (s *FileStore) Close() error {
	return nil
}

func (s *FileStore) load() (map[string]CompactRecord, error) {
	raw, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]CompactRecord{}, nil
		}
		return nil, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return map[string]CompactRecord{}, nil
	}
	var records map[string]CompactRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		return nil, fmt.Errorf("parse registry file store: %w", err)
	}
	return records, nil
}

func (s *FileStore) save(records map[string]CompactRecord) error {
	raw, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(s.path, raw, 0o600)
}

func NewCompactRecord(heartbeat Heartbeat, offset StreamOffset, now time.Time, staleAfter time.Duration) (CompactRecord, error) {
	if err := heartbeat.Validate(); err != nil {
		return CompactRecord{}, err
	}
	if now.IsZero() {
		now = time.Now()
	}
	status := agentStatus(heartbeat, now.UTC(), staleAfter)
	status.EvidenceOffset = &offset
	return CompactRecord{
		APIVersion:     SnapshotAPIVersion,
		Kind:           CompactStatusKind,
		Tenant:         NormalizeTenant(heartbeat.Tenant),
		AgentID:        heartbeat.AgentID,
		Heartbeat:      heartbeat,
		Status:         status,
		EvidenceOffset: offset,
		UpdatedAt:      now.UTC().Format(time.RFC3339Nano),
	}, nil
}

func (r CompactRecord) Validate() error {
	if r.APIVersion != SnapshotAPIVersion {
		return fmt.Errorf("unsupported compact status apiVersion %q", r.APIVersion)
	}
	if r.Kind != CompactStatusKind {
		return fmt.Errorf("unsupported compact status kind %q", r.Kind)
	}
	if NormalizeTenant(r.Tenant) == "" {
		return fmt.Errorf("compact status tenant is required")
	}
	if strings.TrimSpace(r.AgentID) == "" {
		return fmt.Errorf("compact status agentId is required")
	}
	if err := r.Heartbeat.Validate(); err != nil {
		return err
	}
	if strings.TrimSpace(r.UpdatedAt) == "" {
		return fmt.Errorf("compact status updatedAt is required")
	}
	if _, err := time.Parse(time.RFC3339Nano, r.UpdatedAt); err != nil {
		return fmt.Errorf("parse compact status updatedAt: %w", err)
	}
	return nil
}

func SnapshotFromStore(ctx context.Context, store RegistryStore, req SnapshotRequest) (Snapshot, error) {
	if store == nil {
		return Snapshot{}, fmt.Errorf("registry store is required")
	}
	records, err := store.List(ctx, req.Tenant)
	if err != nil {
		return Snapshot{}, err
	}
	return SnapshotFromRecords(records, req), nil
}

func SnapshotFromRecords(records []CompactRecord, req SnapshotRequest) Snapshot {
	now := req.Now
	if now.IsZero() {
		now = time.Now()
	}
	staleAfter := req.StaleAfter
	if staleAfter <= 0 {
		staleAfter = 45 * time.Second
	}
	tenant := NormalizeTenant(req.Tenant)
	latest := map[string]CompactRecord{}
	for _, record := range records {
		if NormalizeTenant(record.Tenant) != tenant {
			continue
		}
		if !matchesSelector(record.Heartbeat.Labels, req.Selector) {
			continue
		}
		existing, ok := latest[record.AgentID]
		if !ok || newerCompactRecord(record, existing) {
			latest[record.AgentID] = record
		}
	}
	agents := make([]AgentStatus, 0, len(latest))
	for _, record := range latest {
		status := agentStatus(record.Heartbeat, now.UTC(), staleAfter)
		offset := record.EvidenceOffset
		status.EvidenceOffset = &offset
		agents = append(agents, status)
	}
	sort.Slice(agents, func(i, j int) bool {
		return agents[i].AgentID < agents[j].AgentID
	})
	return snapshotFromAgents(tenant, now.UTC(), staleAfter, agents)
}

func RegistryStorePrefix(prefix string, tenant string) string {
	prefix = normalizeRegistryPrefix(prefix)
	return prefix + "/agent-registry/v1/tenants/" + NormalizeTenant(tenant) + "/agents/"
}

func RegistryStoreKey(prefix string, tenant string, agentID string) string {
	return RegistryStorePrefix(prefix, tenant) + registryAgentKey(agentID)
}

func normalizeRegistryPrefix(prefix string) string {
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		prefix = DefaultStorePrefix
	}
	prefix = "/" + strings.Trim(prefix, "/")
	if prefix == "/" {
		return DefaultStorePrefix
	}
	return prefix
}

func registryAgentKey(agentID string) string {
	agentID = strings.TrimSpace(agentID)
	if agentID == "" {
		agentID = "agent"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(agentID))
}

func newerCompactRecord(next CompactRecord, existing CompactRecord) bool {
	if next.EvidenceOffset.Sequence != 0 || existing.EvidenceOffset.Sequence != 0 {
		return next.EvidenceOffset.Sequence > existing.EvidenceOffset.Sequence
	}
	nextAt, nextErr := time.Parse(time.RFC3339Nano, next.UpdatedAt)
	existingAt, existingErr := time.Parse(time.RFC3339Nano, existing.UpdatedAt)
	if nextErr == nil && existingErr == nil {
		return nextAt.After(existingAt)
	}
	return next.UpdatedAt > existing.UpdatedAt
}

func sortCompactRecords(records []CompactRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Tenant != records[j].Tenant {
			return records[i].Tenant < records[j].Tenant
		}
		return records[i].AgentID < records[j].AgentID
	})
}
