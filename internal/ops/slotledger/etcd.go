package slotledger

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	clientv3 "go.etcd.io/etcd/client/v3"
)

type EtcdConfig struct {
	Endpoints   []string
	Prefix      string
	DialTimeout time.Duration
}

type EtcdStore struct {
	client *clientv3.Client
	prefix string
}

func NewEtcdStore(ctx context.Context, config EtcdConfig) (*EtcdStore, error) {
	endpoints := cleanEndpointList(config.Endpoints)
	if len(endpoints) == 0 {
		return nil, fmt.Errorf("etcd endpoints are required")
	}
	timeout := config.DialTimeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	client, err := clientv3.New(clientv3.Config{
		Endpoints:   endpoints,
		DialTimeout: timeout,
	})
	if err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		_ = client.Close()
		return nil, ctx.Err()
	default:
	}
	return &EtcdStore{client: client, prefix: NormalizePrefix(config.Prefix)}, nil
}

func (s *EtcdStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func (s *EtcdStore) Reserve(ctx context.Context, req ReserveRequest) (ReserveResult, error) {
	if s == nil || s.client == nil {
		return ReserveResult{}, fmt.Errorf("etcd slot ledger is not connected")
	}
	req.Store = firstNonEmpty(req.Store, StoreEtcd)
	req.StoreScope = firstNonEmpty(req.StoreScope, s.prefix)
	normalized, err := req.normalized(time.Now())
	if err != nil {
		return ReserveResult{}, err
	}
	for attempt := 0; attempt < normalized.MaxSlots+2; attempt++ {
		reclaimed, err := s.expireHeld(ctx, normalized)
		if err != nil {
			return ReserveResult{}, err
		}
		existing, err := s.listActive(ctx, normalized.Tenant, normalized.TargetID)
		if err != nil {
			return ReserveResult{}, err
		}
		heldSlots := map[int]struct{}{}
		for _, record := range existing {
			heldSlots[record.SlotIndex] = struct{}{}
		}
		capacity := normalized.MaxSlots
		available := capacity - len(existing)
		if available < 0 {
			available = 0
		}
		result := ReserveResult{
			APIVersion: APIVersion,
			Kind:       ReserveResultKind,
			Capacity:   capacity,
			Held:       len(existing),
			Available:  available,
			Existing:   existing,
			Reclaimed:  reclaimed,
		}
		if len(existing) >= capacity {
			result.Decision = "blocked"
			result.Reason = fmt.Sprintf("target %s has no available ledger slots (held=%d capacity=%d)", normalized.TargetID, len(existing), capacity)
			return result, nil
		}
		slotIndex := normalized.SlotIndex
		if slotIndex < 1 {
			for idx := 1; idx <= capacity; idx++ {
				if _, ok := heldSlots[idx]; !ok {
					slotIndex = idx
					break
				}
			}
		}
		if slotIndex < 1 || slotIndex > capacity {
			return ReserveResult{}, fmt.Errorf("slot index %d is outside capacity %d", slotIndex, capacity)
		}
		activeKey := s.activeKey(normalized.Tenant, normalized.TargetID, slotIndex)
		lease := NewLeaseRecord(normalized, slotIndex, activeKey)
		raw, err := json.Marshal(lease)
		if err != nil {
			return ReserveResult{}, err
		}
		txnResp, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.Version(activeKey), "=", 0)).
			Then(
				clientv3.OpPut(activeKey, string(raw)),
				clientv3.OpPut(s.historyKey(normalized.Tenant, normalized.TargetID, normalized.LeaseID), string(raw)),
			).
			Commit()
		if err != nil {
			return ReserveResult{}, err
		}
		if !txnResp.Succeeded {
			continue
		}
		result.Decision = "acquired"
		result.Reason = "slot lease acquired"
		result.Lease = &lease
		result.Held++
		result.Available = capacity - result.Held
		if result.Available < 0 {
			result.Available = 0
		}
		return result, nil
	}
	return ReserveResult{}, fmt.Errorf("slot ledger reservation conflicted too many times")
}

func (s *EtcdStore) Release(ctx context.Context, req ReleaseRequest) (LeaseRecord, error) {
	if s == nil || s.client == nil {
		return LeaseRecord{}, fmt.Errorf("etcd slot ledger is not connected")
	}
	normalized, err := req.normalized(time.Now())
	if err != nil {
		return LeaseRecord{}, err
	}
	historyKey := s.historyKey(normalized.Tenant, normalized.TargetID, normalized.LeaseID)
	resp, err := s.client.Get(ctx, historyKey)
	if err != nil {
		return LeaseRecord{}, err
	}
	if len(resp.Kvs) == 0 {
		return LeaseRecord{}, fmt.Errorf("slot lease %q was not found", normalized.LeaseID)
	}
	var record LeaseRecord
	if err := json.Unmarshal(resp.Kvs[0].Value, &record); err != nil {
		return LeaseRecord{}, fmt.Errorf("parse slot lease %s: %w", historyKey, err)
	}
	if record.TokenDigest != TokenDigest(normalized.Token) {
		return LeaseRecord{}, fmt.Errorf("slot lease %q token mismatch", normalized.LeaseID)
	}
	if record.Status == StatusReleased {
		return record, nil
	}
	heldValue := string(resp.Kvs[0].Value)
	now := normalized.Now.UTC().Format(time.RFC3339Nano)
	record.Status = StatusReleased
	record.ReleasedAt = now
	record.UpdatedAt = now
	raw, err := json.Marshal(record)
	if err != nil {
		return LeaseRecord{}, err
	}
	activeKey := s.activeKey(record.Tenant, record.TargetID, record.SlotIndex)
	txnResp, err := s.client.Txn(ctx).
		If(
			clientv3.Compare(clientv3.ModRevision(historyKey), "=", resp.Kvs[0].ModRevision),
			clientv3.Compare(clientv3.Value(activeKey), "=", heldValue),
		).
		Then(
			clientv3.OpPut(historyKey, string(raw)),
			clientv3.OpDelete(activeKey),
		).
		Commit()
	if err != nil {
		return LeaseRecord{}, err
	}
	if !txnResp.Succeeded {
		return LeaseRecord{}, fmt.Errorf("slot lease %q changed during release", normalized.LeaseID)
	}
	return record, nil
}

func (s *EtcdStore) List(ctx context.Context, req ListRequest) ([]LeaseRecord, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("etcd slot ledger is not connected")
	}
	req = req.normalized()
	var prefix string
	if req.Include == "" || req.Include == StatusHeld {
		prefix = s.activePrefix(req.Tenant, req.TargetID)
	} else {
		prefix = s.historyPrefix(req.Tenant, req.TargetID)
	}
	resp, err := s.client.Get(ctx, prefix, clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	out := make([]LeaseRecord, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var record LeaseRecord
		if err := json.Unmarshal(kv.Value, &record); err != nil {
			return nil, fmt.Errorf("parse slot lease %s: %w", string(kv.Key), err)
		}
		if req.Include != "" && req.Include != "all" && record.Status != req.Include {
			continue
		}
		out = append(out, record)
	}
	SortLeaseRecords(out)
	return out, nil
}

func (s *EtcdStore) expireHeld(ctx context.Context, req ReserveRequest) ([]LeaseRecord, error) {
	resp, err := s.client.Get(ctx, s.activePrefix(req.Tenant, req.TargetID), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	var reclaimed []LeaseRecord
	for _, kv := range resp.Kvs {
		var record LeaseRecord
		if err := json.Unmarshal(kv.Value, &record); err != nil {
			return nil, fmt.Errorf("parse slot lease %s: %w", string(kv.Key), err)
		}
		if record.Status != StatusHeld || !Expired(record, req.Now) {
			continue
		}
		record.Status = StatusExpired
		record.UpdatedAt = req.Now.UTC().Format(time.RFC3339Nano)
		raw, err := json.Marshal(record)
		if err != nil {
			return nil, err
		}
		historyKey := s.historyKey(record.Tenant, record.TargetID, record.LeaseID)
		txnResp, err := s.client.Txn(ctx).
			If(clientv3.Compare(clientv3.ModRevision(string(kv.Key)), "=", kv.ModRevision)).
			Then(
				clientv3.OpDelete(string(kv.Key)),
				clientv3.OpPut(historyKey, string(raw)),
			).
			Commit()
		if err != nil {
			return nil, err
		}
		if txnResp.Succeeded {
			reclaimed = append(reclaimed, record)
		}
	}
	return reclaimed, nil
}

func (s *EtcdStore) listActive(ctx context.Context, tenant string, targetID string) ([]LeaseRecord, error) {
	resp, err := s.client.Get(ctx, s.activePrefix(tenant, targetID), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	out := make([]LeaseRecord, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var record LeaseRecord
		if err := json.Unmarshal(kv.Value, &record); err != nil {
			return nil, fmt.Errorf("parse active slot lease %s: %w", string(kv.Key), err)
		}
		if record.Status == StatusHeld {
			out = append(out, record)
		}
	}
	SortLeaseRecords(out)
	return out, nil
}

func (s *EtcdStore) activePrefix(tenant string, targetID string) string {
	prefix := s.prefix + "/target-slot-ledger/v1/tenants/" + NormalizeTenant(tenant)
	if strings.TrimSpace(targetID) == "" {
		return prefix + "/targets/"
	}
	return prefix + "/targets/" + EncodeKeyPart(targetID) + "/active/"
}

func (s *EtcdStore) historyPrefix(tenant string, targetID string) string {
	prefix := s.prefix + "/target-slot-ledger/v1/tenants/" + NormalizeTenant(tenant)
	if strings.TrimSpace(targetID) == "" {
		return prefix + "/targets/"
	}
	return prefix + "/targets/" + EncodeKeyPart(targetID) + "/leases/"
}

func (s *EtcdStore) activeKey(tenant string, targetID string, slotIndex int) string {
	return fmt.Sprintf("%s%08d", s.activePrefix(tenant, targetID), slotIndex)
}

func (s *EtcdStore) historyKey(tenant string, targetID string, leaseID string) string {
	return s.historyPrefix(tenant, targetID) + EncodeKeyPart(leaseID)
}

func cleanEndpointList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			out = append(out, value)
		}
	}
	return out
}
