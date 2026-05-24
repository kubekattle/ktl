package hostfacts

import (
	"context"
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
	CacheEntryAPIVersion = "torque.dev/ops/facts/v1alpha1"
	CacheEntryKind       = "HostFactCacheEntry"
	CacheDecisionKind    = "HostFactFreshnessDecision"
	CacheResolutionKind  = "HostFactCacheResolution"
)

type FileCache struct {
	Dir string
}

type CacheEntry struct {
	APIVersion     string   `json:"apiVersion"`
	Kind           string   `json:"kind"`
	TargetID       string   `json:"targetId"`
	TargetDigest   string   `json:"targetDigest"`
	SnapshotDigest string   `json:"snapshotDigest"`
	StoredAt       string   `json:"storedAt"`
	Snapshot       Snapshot `json:"snapshot"`
}

type FreshnessDecision struct {
	APIVersion     string `json:"apiVersion"`
	Kind           string `json:"kind"`
	TargetID       string `json:"targetId"`
	TargetDigest   string `json:"targetDigest"`
	SnapshotDigest string `json:"snapshotDigest,omitempty"`
	ObservedAt     string `json:"observedAt,omitempty"`
	ExpiresAt      string `json:"expiresAt,omitempty"`
	CheckedAt      string `json:"checkedAt"`
	Status         string `json:"status"`
	Decision       string `json:"decision"`
	Reason         string `json:"reason"`
	Fresh          bool   `json:"fresh"`
	Blocked        bool   `json:"blocked"`
	AgeMillis      int64  `json:"ageMillis,omitempty"`
	StaleByMillis  int64  `json:"staleByMillis,omitempty"`
}

type ResolveOptions struct {
	CacheDir string
	Refresh  bool
	Now      time.Time
}

type ResolveResult struct {
	APIVersion       string             `json:"apiVersion"`
	Kind             string             `json:"kind"`
	TargetID         string             `json:"targetId"`
	Source           string             `json:"source"`
	Refreshed        bool               `json:"refreshed"`
	CachePath        string             `json:"cachePath"`
	Decision         FreshnessDecision  `json:"decision"`
	PreviousDecision *FreshnessDecision `json:"previousDecision,omitempty"`
	Snapshot         *Snapshot          `json:"snapshot,omitempty"`
}

func (c FileCache) Store(snapshot *Snapshot, storedAt time.Time) (*CacheEntry, error) {
	if snapshot == nil {
		return nil, fmt.Errorf("snapshot is required")
	}
	if strings.TrimSpace(c.Dir) == "" {
		return nil, fmt.Errorf("cache dir is required")
	}
	storedAt = storedAt.UTC()
	if storedAt.IsZero() {
		storedAt = time.Now().UTC()
	}
	entry := &CacheEntry{
		APIVersion:     CacheEntryAPIVersion,
		Kind:           CacheEntryKind,
		TargetID:       snapshot.TargetID,
		TargetDigest:   snapshot.TargetDigest,
		SnapshotDigest: snapshot.Digest,
		StoredAt:       storedAt.Format(time.RFC3339),
		Snapshot:       *snapshot,
	}
	raw, err := json.MarshalIndent(entry, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal cache entry: %w", err)
	}
	path := c.Path(snapshot.TargetID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create cache dir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".fact-cache-*.tmp")
	if err != nil {
		return nil, fmt.Errorf("create cache temp file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(append(raw, '\n')); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("write cache temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close cache temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return nil, fmt.Errorf("replace cache entry: %w", err)
	}
	return entry, nil
}

func (c FileCache) Load(targetID string) (*CacheEntry, bool, error) {
	if strings.TrimSpace(c.Dir) == "" {
		return nil, false, fmt.Errorf("cache dir is required")
	}
	path := c.Path(targetID)
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read cache entry: %w", err)
	}
	var entry CacheEntry
	if err := json.Unmarshal(raw, &entry); err != nil {
		return nil, false, fmt.Errorf("decode cache entry: %w", err)
	}
	if entry.APIVersion != CacheEntryAPIVersion || entry.Kind != CacheEntryKind {
		return nil, false, fmt.Errorf("invalid cache entry type: %s/%s", entry.APIVersion, entry.Kind)
	}
	if entry.TargetID != strings.TrimSpace(targetID) {
		return nil, false, fmt.Errorf("cache entry target mismatch: got %q, want %q", entry.TargetID, strings.TrimSpace(targetID))
	}
	if entry.Snapshot.Digest == "" {
		entry.Snapshot.Digest = entry.Snapshot.StableDigest()
	}
	return &entry, true, nil
}

func (c FileCache) Path(targetID string) string {
	key := cacheKey(targetID)
	return filepath.Join(c.Dir, key+".json")
}

func EvaluateFreshness(snapshot *Snapshot, targetDigest string, checkedAt time.Time) FreshnessDecision {
	checkedAt = checkedAt.UTC()
	if checkedAt.IsZero() {
		checkedAt = time.Now().UTC()
	}
	decision := FreshnessDecision{
		APIVersion:   CacheEntryAPIVersion,
		Kind:         CacheDecisionKind,
		TargetDigest: targetDigest,
		CheckedAt:    checkedAt.Format(time.RFC3339),
		Status:       "missing",
		Decision:     "block",
		Reason:       "fact snapshot is missing",
		Blocked:      true,
	}
	if snapshot == nil {
		return decision
	}
	decision.TargetID = snapshot.TargetID
	decision.SnapshotDigest = snapshot.Digest
	decision.ObservedAt = snapshot.ObservedAt
	decision.ExpiresAt = snapshot.ExpiresAt
	if decision.SnapshotDigest == "" {
		decision.SnapshotDigest = snapshot.StableDigest()
	}
	if snapshot.TargetDigest != "" {
		decision.TargetDigest = snapshot.TargetDigest
	}
	if targetDigest != "" && snapshot.TargetDigest != "" && snapshot.TargetDigest != targetDigest {
		decision.Status = "target-digest-mismatch"
		decision.Reason = "cached facts were collected for a different target digest"
		return decision
	}
	observedAt, observedErr := time.Parse(time.RFC3339, snapshot.ObservedAt)
	expiresAt, expiresErr := time.Parse(time.RFC3339, snapshot.ExpiresAt)
	if observedErr != nil || expiresErr != nil {
		decision.Status = "invalid"
		decision.Reason = "cached facts have invalid observation timestamps"
		return decision
	}
	if checkedAt.After(observedAt) {
		decision.AgeMillis = checkedAt.Sub(observedAt).Milliseconds()
	}
	if !checkedAt.Before(expiresAt) {
		decision.Status = "stale"
		decision.Reason = "cached facts expired"
		decision.StaleByMillis = checkedAt.Sub(expiresAt).Milliseconds()
		return decision
	}
	decision.Status = "fresh"
	decision.Decision = "allow"
	decision.Reason = "cached facts are within TTL"
	decision.Fresh = true
	decision.Blocked = false
	return decision
}

func Resolve(ctx context.Context, target Transport, request CollectRequest, options ResolveOptions) (*ResolveResult, error) {
	if target == nil {
		return nil, fmt.Errorf("transport is required")
	}
	targetID := strings.TrimSpace(request.TargetID)
	if targetID == "" {
		return nil, fmt.Errorf("target id is required")
	}
	now := options.Now.UTC()
	if now.IsZero() {
		now = time.Now().UTC()
	}
	cache := FileCache{Dir: options.CacheDir}
	targetDigest := target.TargetDigest()
	entry, found, err := cache.Load(targetID)
	if err != nil {
		return nil, err
	}
	result := &ResolveResult{
		APIVersion: CacheEntryAPIVersion,
		Kind:       CacheResolutionKind,
		TargetID:   targetID,
		Source:     "cache",
		CachePath:  cache.Path(targetID),
	}
	if found {
		decision := EvaluateFreshness(&entry.Snapshot, targetDigest, now)
		result.Decision = decision
		result.Snapshot = &entry.Snapshot
		if decision.Fresh || !options.Refresh {
			return result, nil
		}
		result.PreviousDecision = &decision
	} else {
		decision := EvaluateFreshness(nil, targetDigest, now)
		decision.TargetID = targetID
		result.Decision = decision
		result.Source = "none"
		if !options.Refresh {
			return result, nil
		}
		result.PreviousDecision = &decision
	}

	collectRequest := request
	if collectRequest.ObservedAt.IsZero() {
		collectRequest.ObservedAt = now
	}
	snapshot, err := Collect(ctx, target, collectRequest)
	if err != nil {
		return nil, err
	}
	if _, err := cache.Store(snapshot, now); err != nil {
		return nil, err
	}
	decision := EvaluateFreshness(snapshot, targetDigest, now)
	result.Source = "refresh"
	result.Refreshed = true
	result.Decision = decision
	result.Snapshot = snapshot
	return result, nil
}

func cacheKey(targetID string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(targetID)))
	return hex.EncodeToString(sum[:])
}
