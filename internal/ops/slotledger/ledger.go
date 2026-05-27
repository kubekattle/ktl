package slotledger

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

const (
	APIVersion         = "torque.dev/ops/target-slot-ledger/v1alpha1"
	LeaseKind          = "TargetSlotLease"
	ReserveResultKind  = "TargetSlotReservation"
	DefaultStorePrefix = "/torque"

	StatusHeld     = "held"
	StatusReleased = "released"
	StatusExpired  = "expired"

	StoreFile = "file"
	StoreEtcd = "etcd"
)

type Store interface {
	Reserve(ctx context.Context, req ReserveRequest) (ReserveResult, error)
	Renew(ctx context.Context, req RenewRequest) (LeaseRecord, error)
	Release(ctx context.Context, req ReleaseRequest) (LeaseRecord, error)
	List(ctx context.Context, req ListRequest) ([]LeaseRecord, error)
	Close() error
}

type ReserveRequest struct {
	Tenant     string
	TargetID   string
	Holder     string
	RunID      string
	NodeID     string
	LeaseID    string
	MaxSlots   int
	Slots      int
	TTL        time.Duration
	Now        time.Time
	Metadata   map[string]string
	Token      string
	SlotIndex  int
	Store      string
	StoreKey   string
	StoreScope string
}

type ReleaseRequest struct {
	Tenant   string
	TargetID string
	LeaseID  string
	Token    string
	Now      time.Time
}

type RenewRequest struct {
	Tenant   string
	TargetID string
	LeaseID  string
	Token    string
	TTL      time.Duration
	Now      time.Time
}

type ListRequest struct {
	Tenant   string
	TargetID string
	Include  string
}

type ReserveResult struct {
	APIVersion string        `json:"apiVersion"`
	Kind       string        `json:"kind"`
	Decision   string        `json:"decision"`
	Reason     string        `json:"reason,omitempty"`
	Capacity   int           `json:"capacity"`
	Held       int           `json:"held"`
	Available  int           `json:"available"`
	Lease      *LeaseRecord  `json:"lease,omitempty"`
	Existing   []LeaseRecord `json:"existing,omitempty"`
	Reclaimed  []LeaseRecord `json:"reclaimed,omitempty"`
}

type LeaseRecord struct {
	APIVersion   string            `json:"apiVersion"`
	Kind         string            `json:"kind"`
	Tenant       string            `json:"tenant"`
	TargetID     string            `json:"targetId"`
	LeaseID      string            `json:"leaseId"`
	SlotIndex    int               `json:"slotIndex"`
	Slots        int               `json:"slots"`
	MaxSlots     int               `json:"maxSlots"`
	Holder       string            `json:"holder"`
	RunID        string            `json:"runId,omitempty"`
	NodeID       string            `json:"nodeId,omitempty"`
	TokenDigest  string            `json:"tokenDigest,omitempty"`
	Status       string            `json:"status"`
	AcquiredAt   string            `json:"acquiredAt"`
	ExpiresAt    string            `json:"expiresAt"`
	ReleasedAt   string            `json:"releasedAt,omitempty"`
	UpdatedAt    string            `json:"updatedAt"`
	Store        string            `json:"store,omitempty"`
	StoreKey     string            `json:"storeKey,omitempty"`
	StoreScope   string            `json:"storeScope,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	ReleaseToken string            `json:"-"`
}

func (r ReserveRequest) normalized(now time.Time) (ReserveRequest, error) {
	r.Tenant = NormalizeTenant(r.Tenant)
	r.TargetID = strings.TrimSpace(r.TargetID)
	r.Holder = strings.TrimSpace(r.Holder)
	r.RunID = strings.TrimSpace(r.RunID)
	r.NodeID = strings.TrimSpace(r.NodeID)
	r.LeaseID = strings.TrimSpace(r.LeaseID)
	r.Token = strings.TrimSpace(r.Token)
	r.Store = strings.ToLower(strings.TrimSpace(r.Store))
	r.StoreKey = strings.TrimSpace(r.StoreKey)
	r.StoreScope = strings.TrimSpace(r.StoreScope)
	r.Metadata = cleanMetadata(r.Metadata)
	if r.TargetID == "" {
		return ReserveRequest{}, fmt.Errorf("targetId is required")
	}
	if r.Holder == "" {
		return ReserveRequest{}, fmt.Errorf("holder is required")
	}
	if r.LeaseID == "" {
		return ReserveRequest{}, fmt.Errorf("leaseId is required")
	}
	if r.MaxSlots < 1 {
		return ReserveRequest{}, fmt.Errorf("maxSlots must be >= 1")
	}
	if r.Slots < 1 {
		r.Slots = 1
	}
	if r.Slots != 1 {
		return ReserveRequest{}, fmt.Errorf("only single-slot reservations are supported in this slice")
	}
	if r.TTL <= 0 {
		return ReserveRequest{}, fmt.Errorf("ttl must be > 0")
	}
	if r.Now.IsZero() {
		r.Now = now.UTC()
	} else {
		r.Now = r.Now.UTC()
	}
	if r.Token == "" {
		token, err := NewToken()
		if err != nil {
			return ReserveRequest{}, err
		}
		r.Token = token
	}
	return r, nil
}

func (r ReleaseRequest) normalized(now time.Time) (ReleaseRequest, error) {
	r.Tenant = NormalizeTenant(r.Tenant)
	r.TargetID = strings.TrimSpace(r.TargetID)
	r.LeaseID = strings.TrimSpace(r.LeaseID)
	r.Token = strings.TrimSpace(r.Token)
	if r.TargetID == "" {
		return ReleaseRequest{}, fmt.Errorf("targetId is required")
	}
	if r.LeaseID == "" {
		return ReleaseRequest{}, fmt.Errorf("leaseId is required")
	}
	if r.Token == "" {
		return ReleaseRequest{}, fmt.Errorf("lease token is required")
	}
	if r.Now.IsZero() {
		r.Now = now.UTC()
	} else {
		r.Now = r.Now.UTC()
	}
	return r, nil
}

func (r RenewRequest) normalized(now time.Time) (RenewRequest, error) {
	r.Tenant = NormalizeTenant(r.Tenant)
	r.TargetID = strings.TrimSpace(r.TargetID)
	r.LeaseID = strings.TrimSpace(r.LeaseID)
	r.Token = strings.TrimSpace(r.Token)
	if r.TargetID == "" {
		return RenewRequest{}, fmt.Errorf("targetId is required")
	}
	if r.LeaseID == "" {
		return RenewRequest{}, fmt.Errorf("leaseId is required")
	}
	if r.Token == "" {
		return RenewRequest{}, fmt.Errorf("lease token is required")
	}
	if r.TTL <= 0 {
		return RenewRequest{}, fmt.Errorf("ttl must be > 0")
	}
	if r.Now.IsZero() {
		r.Now = now.UTC()
	} else {
		r.Now = r.Now.UTC()
	}
	return r, nil
}

func (r ListRequest) normalized() ListRequest {
	r.Tenant = NormalizeTenant(r.Tenant)
	r.TargetID = strings.TrimSpace(r.TargetID)
	r.Include = strings.ToLower(strings.TrimSpace(r.Include))
	return r
}

func NewLeaseRecord(req ReserveRequest, slotIndex int, storeKey string) LeaseRecord {
	now := req.Now.UTC()
	expiresAt := now.Add(req.TTL)
	return LeaseRecord{
		APIVersion:   APIVersion,
		Kind:         LeaseKind,
		Tenant:       NormalizeTenant(req.Tenant),
		TargetID:     strings.TrimSpace(req.TargetID),
		LeaseID:      strings.TrimSpace(req.LeaseID),
		SlotIndex:    slotIndex,
		Slots:        req.Slots,
		MaxSlots:     req.MaxSlots,
		Holder:       strings.TrimSpace(req.Holder),
		RunID:        strings.TrimSpace(req.RunID),
		NodeID:       strings.TrimSpace(req.NodeID),
		TokenDigest:  TokenDigest(req.Token),
		Status:       StatusHeld,
		AcquiredAt:   now.Format(time.RFC3339Nano),
		ExpiresAt:    expiresAt.Format(time.RFC3339Nano),
		UpdatedAt:    now.Format(time.RFC3339Nano),
		Store:        strings.ToLower(strings.TrimSpace(req.Store)),
		StoreKey:     strings.TrimSpace(storeKey),
		StoreScope:   strings.TrimSpace(req.StoreScope),
		Metadata:     cleanMetadata(req.Metadata),
		ReleaseToken: strings.TrimSpace(req.Token),
	}
}

func (r LeaseRecord) Validate() error {
	if r.APIVersion != APIVersion {
		return fmt.Errorf("unsupported slot lease apiVersion %q", r.APIVersion)
	}
	if r.Kind != LeaseKind {
		return fmt.Errorf("unsupported slot lease kind %q", r.Kind)
	}
	if NormalizeTenant(r.Tenant) == "" {
		return fmt.Errorf("slot lease tenant is required")
	}
	if strings.TrimSpace(r.TargetID) == "" {
		return fmt.Errorf("slot lease targetId is required")
	}
	if strings.TrimSpace(r.LeaseID) == "" {
		return fmt.Errorf("slot lease leaseId is required")
	}
	if r.SlotIndex < 1 {
		return fmt.Errorf("slot lease slotIndex must be >= 1")
	}
	if r.Slots < 1 {
		return fmt.Errorf("slot lease slots must be >= 1")
	}
	if r.MaxSlots < 1 {
		return fmt.Errorf("slot lease maxSlots must be >= 1")
	}
	switch strings.ToLower(strings.TrimSpace(r.Status)) {
	case StatusHeld, StatusReleased, StatusExpired:
	default:
		return fmt.Errorf("unsupported slot lease status %q", r.Status)
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.AcquiredAt)); err != nil {
		return fmt.Errorf("parse acquiredAt: %w", err)
	}
	if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.ExpiresAt)); err != nil {
		return fmt.Errorf("parse expiresAt: %w", err)
	}
	if strings.TrimSpace(r.UpdatedAt) != "" {
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.UpdatedAt)); err != nil {
			return fmt.Errorf("parse updatedAt: %w", err)
		}
	}
	if strings.TrimSpace(r.ReleasedAt) != "" {
		if _, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(r.ReleasedAt)); err != nil {
			return fmt.Errorf("parse releasedAt: %w", err)
		}
	}
	return nil
}

func Expired(record LeaseRecord, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(record.ExpiresAt))
	if err != nil {
		return true
	}
	return !now.UTC().Before(expiresAt.UTC())
}

func TokenDigest(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(token))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func NewToken() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate slot lease token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func NormalizeTenant(tenant string) string {
	tenant = strings.TrimSpace(tenant)
	if tenant == "" {
		return "default"
	}
	tenant = strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z':
			return r
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_' || r == '-':
			return r
		default:
			return '_'
		}
	}, tenant)
	tenant = strings.Trim(tenant, "_")
	if tenant == "" {
		return "default"
	}
	return tenant
}

func NormalizePrefix(prefix string) string {
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

func EncodeKeyPart(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		value = "unknown"
	}
	return base64.RawURLEncoding.EncodeToString([]byte(value))
}

func SortLeaseRecords(records []LeaseRecord) {
	sort.Slice(records, func(i, j int) bool {
		if records[i].Tenant != records[j].Tenant {
			return records[i].Tenant < records[j].Tenant
		}
		if records[i].TargetID != records[j].TargetID {
			return records[i].TargetID < records[j].TargetID
		}
		if records[i].SlotIndex != records[j].SlotIndex {
			return records[i].SlotIndex < records[j].SlotIndex
		}
		return records[i].LeaseID < records[j].LeaseID
	})
}

func MarshalMetadata(metadata map[string]string) (string, error) {
	metadata = cleanMetadata(metadata)
	if len(metadata) == 0 {
		return "", nil
	}
	raw, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func UnmarshalMetadata(raw string) map[string]string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	out := map[string]string{}
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil
	}
	return cleanMetadata(out)
}

func cleanMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	out := make(map[string]string, len(metadata))
	for key, value := range metadata {
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
