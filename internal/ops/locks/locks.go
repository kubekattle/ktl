package locks

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	APIVersion = "torque.dev/ops/locks/v1alpha1"
	RecordKind = "TargetLock"
	ResultKind = "TargetLockAcquisition"
)

type FileStore struct {
	Dir string
	Now func() time.Time
}

type AcquireRequest struct {
	Scope        string
	TargetID     string
	Holder       string
	Operation    string
	TTL          time.Duration
	Wait         time.Duration
	PollInterval time.Duration
	Metadata     map[string]string
}

type AcquireResult struct {
	APIVersion string  `json:"apiVersion"`
	Kind       string  `json:"kind"`
	Scope      string  `json:"scope"`
	Decision   string  `json:"decision"`
	Reason     string  `json:"reason"`
	Record     *Record `json:"record,omitempty"`
	Existing   *Record `json:"existing,omitempty"`
}

type Record struct {
	APIVersion string            `json:"apiVersion"`
	Kind       string            `json:"kind"`
	Scope      string            `json:"scope"`
	Token      string            `json:"token"`
	TargetID   string            `json:"targetId,omitempty"`
	Holder     string            `json:"holder"`
	Operation  string            `json:"operation,omitempty"`
	AcquiredAt string            `json:"acquiredAt"`
	ExpiresAt  string            `json:"expiresAt"`
	ReleasedAt string            `json:"releasedAt,omitempty"`
	Status     string            `json:"status"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

func (s FileStore) Acquire(ctx context.Context, req AcquireRequest) (AcquireResult, error) {
	if strings.TrimSpace(s.Dir) == "" {
		return AcquireResult{}, fmt.Errorf("lock dir is required")
	}
	scope := strings.TrimSpace(req.Scope)
	if scope == "" {
		return AcquireResult{}, fmt.Errorf("lock scope is required")
	}
	holder := strings.TrimSpace(req.Holder)
	if holder == "" {
		return AcquireResult{}, fmt.Errorf("lock holder is required")
	}
	ttl := req.TTL
	if ttl <= 0 {
		ttl = 15 * time.Minute
	}
	poll := req.PollInterval
	if poll <= 0 {
		poll = 100 * time.Millisecond
	}
	deadline := s.now().Add(req.Wait)
	for {
		result, retry, err := s.tryAcquire(scope, holder, ttl, req)
		if err != nil || !retry {
			return result, err
		}
		if result.Decision == "retry" {
			continue
		}
		if req.Wait <= 0 || !s.now().Before(deadline) {
			return result, nil
		}
		select {
		case <-ctx.Done():
			return AcquireResult{}, ctx.Err()
		case <-time.After(poll):
		}
	}
}

func (s FileStore) Inspect(scope string) (*Record, bool, error) {
	record, found, err := s.load(scope)
	if err != nil || !found {
		return record, found, err
	}
	if recordExpired(*record, s.now()) {
		record.Status = "stale"
	}
	return record, true, nil
}

func (s FileStore) Release(scope, token string) (*Record, error) {
	scope = strings.TrimSpace(scope)
	token = strings.TrimSpace(token)
	if scope == "" || token == "" {
		return nil, fmt.Errorf("lock scope and token are required")
	}
	record, found, err := s.load(scope)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("lock %q was not found", scope)
	}
	if record.Token != token {
		return nil, fmt.Errorf("lock %q token mismatch", scope)
	}
	record.Status = "released"
	record.ReleasedAt = s.now().UTC().Format(time.RFC3339)
	if err := os.Remove(s.path(scope)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove lock: %w", err)
	}
	return record, nil
}

func (s FileStore) tryAcquire(scope, holder string, ttl time.Duration, req AcquireRequest) (AcquireResult, bool, error) {
	now := s.now().UTC()
	record := Record{
		APIVersion: APIVersion,
		Kind:       RecordKind,
		Scope:      scope,
		Token:      newToken(),
		TargetID:   strings.TrimSpace(req.TargetID),
		Holder:     holder,
		Operation:  strings.TrimSpace(req.Operation),
		AcquiredAt: now.Format(time.RFC3339),
		ExpiresAt:  now.Add(ttl).Format(time.RFC3339),
		Status:     "held",
		Metadata:   copyMap(req.Metadata),
	}
	raw, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return AcquireResult{}, false, err
	}
	if err := os.MkdirAll(s.Dir, 0o755); err != nil {
		return AcquireResult{}, false, fmt.Errorf("create lock dir: %w", err)
	}
	path := s.path(scope)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err == nil {
		defer file.Close()
		if _, err := file.Write(append(raw, '\n')); err != nil {
			return AcquireResult{}, false, fmt.Errorf("write lock: %w", err)
		}
		return AcquireResult{APIVersion: APIVersion, Kind: ResultKind, Scope: scope, Decision: "acquired", Reason: "lock acquired", Record: &record}, false, nil
	}
	if !errors.Is(err, os.ErrExist) {
		return AcquireResult{}, false, fmt.Errorf("create lock: %w", err)
	}
	existing, found, loadErr := s.load(scope)
	if loadErr != nil {
		return AcquireResult{}, false, loadErr
	}
	if !found {
		return AcquireResult{}, true, nil
	}
	if recordExpired(*existing, now) {
		_ = os.Remove(path)
		return AcquireResult{APIVersion: APIVersion, Kind: ResultKind, Scope: scope, Decision: "retry", Reason: "stale lock removed", Existing: existing}, true, nil
	}
	return AcquireResult{APIVersion: APIVersion, Kind: ResultKind, Scope: scope, Decision: "blocked", Reason: "lock is already held", Existing: existing}, false, nil
}

func (s FileStore) load(scope string) (*Record, bool, error) {
	raw, err := os.ReadFile(s.path(scope))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read lock: %w", err)
	}
	var record Record
	if err := json.Unmarshal(raw, &record); err != nil {
		return nil, false, fmt.Errorf("decode lock: %w", err)
	}
	return &record, true, nil
}

func (s FileStore) path(scope string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(scope)))
	return filepath.Join(s.Dir, hex.EncodeToString(sum[:])+".json")
}

func (s FileStore) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func recordExpired(record Record, now time.Time) bool {
	expiresAt, err := time.Parse(time.RFC3339, record.ExpiresAt)
	if err != nil {
		return true
	}
	return !now.UTC().Before(expiresAt.UTC())
}

func newToken() string {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		sum := sha256.Sum256([]byte(time.Now().UTC().Format(time.RFC3339Nano)))
		return hex.EncodeToString(sum[:16])
	}
	return hex.EncodeToString(raw[:])
}

func copyMap(values map[string]string) map[string]string {
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
