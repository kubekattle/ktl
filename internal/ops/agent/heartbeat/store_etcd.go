package heartbeat

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
	endpoints := cleanList(config.Endpoints)
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
	return &EtcdStore{
		client: client,
		prefix: normalizeRegistryPrefix(config.Prefix),
	}, nil
}

func (s *EtcdStore) Put(ctx context.Context, record CompactRecord) error {
	if s == nil || s.client == nil {
		return fmt.Errorf("etcd store is not connected")
	}
	if err := record.Validate(); err != nil {
		return err
	}
	raw, err := json.Marshal(record)
	if err != nil {
		return err
	}
	_, err = s.client.Put(ctx, RegistryStoreKey(s.prefix, record.Tenant, record.AgentID), string(raw))
	return err
}

func (s *EtcdStore) List(ctx context.Context, tenant string) ([]CompactRecord, error) {
	if s == nil || s.client == nil {
		return nil, fmt.Errorf("etcd store is not connected")
	}
	resp, err := s.client.Get(ctx, RegistryStorePrefix(s.prefix, tenant), clientv3.WithPrefix())
	if err != nil {
		return nil, err
	}
	records := make([]CompactRecord, 0, len(resp.Kvs))
	for _, kv := range resp.Kvs {
		var record CompactRecord
		if err := json.Unmarshal(kv.Value, &record); err != nil {
			return nil, fmt.Errorf("parse compact status %s: %w", string(kv.Key), err)
		}
		if NormalizeTenant(record.Tenant) == NormalizeTenant(tenant) {
			records = append(records, record)
		}
	}
	sortCompactRecords(records)
	return records, nil
}

func (s *EtcdStore) Close() error {
	if s == nil || s.client == nil {
		return nil
	}
	return s.client.Close()
}

func ParseEtcdEndpoints(raw string) []string {
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}
