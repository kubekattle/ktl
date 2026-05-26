package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"time"

	natstransport "github.com/ingresslabs/torque/internal/ops/transport/nats"
	natsgo "github.com/nats-io/nats.go"
)

type NATSConfig struct {
	Server  string
	Creds   string
	NKey    string
	Timeout time.Duration
	Name    string
}

type Publisher struct {
	conn    *natsgo.Conn
	timeout time.Duration
}

type CollectOptions struct {
	NATS       NATSConfig
	Tenant     string
	Selector   map[string]string
	StaleAfter time.Duration
	Listen     time.Duration
}

func NewPublisher(ctx context.Context, config NATSConfig) (*Publisher, error) {
	conn, timeout, err := connect(ctx, config, "torque-agent-heartbeat")
	if err != nil {
		return nil, err
	}
	return &Publisher{conn: conn, timeout: timeout}, nil
}

func (p *Publisher) Publish(ctx context.Context, heartbeat Heartbeat, shardCount int) (string, error) {
	if p == nil || p.conn == nil {
		return "", fmt.Errorf("heartbeat publisher is not connected")
	}
	if err := heartbeat.Validate(); err != nil {
		return "", err
	}
	select {
	case <-ctx.Done():
		return "", ctx.Err()
	default:
	}
	subject := Subject(heartbeat.Tenant, shardCount, heartbeat.AgentID)
	raw, err := heartbeat.MarshalJSONBytes()
	if err != nil {
		return "", err
	}
	if err := p.conn.Publish(subject, raw); err != nil {
		return "", err
	}
	if err := p.conn.FlushTimeout(p.timeout); err != nil {
		return "", err
	}
	return subject, nil
}

func (p *Publisher) Close() {
	if p != nil && p.conn != nil {
		p.conn.Close()
	}
}

func Collect(ctx context.Context, opts CollectOptions) (Snapshot, error) {
	listen := opts.Listen
	if listen <= 0 {
		listen = 2 * time.Second
	}
	conn, timeout, err := connect(ctx, opts.NATS, "torque-agent-status")
	if err != nil {
		return Snapshot{}, err
	}
	defer conn.Close()

	registry := NewRegistry()
	sub, err := conn.SubscribeSync(SubjectWildcard(opts.Tenant))
	if err != nil {
		return Snapshot{}, err
	}
	defer func() { _ = sub.Unsubscribe() }()
	if err := conn.FlushTimeout(timeout); err != nil {
		return Snapshot{}, err
	}

	deadline := time.Now().Add(listen)
	for {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			break
		}
		wait := 100 * time.Millisecond
		if remaining < wait {
			wait = remaining
		}
		msg, err := sub.NextMsg(wait)
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				continue
			}
			return Snapshot{}, err
		}
		heartbeat, err := Parse(msg.Data)
		if err != nil {
			return Snapshot{}, err
		}
		if err := registry.Apply(heartbeat); err != nil {
			return Snapshot{}, err
		}
	}
	return registry.Snapshot(SnapshotRequest{
		Tenant:     opts.Tenant,
		Selector:   opts.Selector,
		Now:        time.Now(),
		StaleAfter: opts.StaleAfter,
	}), nil
}

func connect(ctx context.Context, config NATSConfig, defaultName string) (*natsgo.Conn, time.Duration, error) {
	if err := ctx.Err(); err != nil {
		return nil, 0, err
	}
	timeout := config.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	name := config.Name
	if name == "" {
		name = defaultName
	}
	opts, err := natstransport.ConnectOptions(natstransport.DialConfig{
		Server:  config.Server,
		Creds:   config.Creds,
		NKey:    config.NKey,
		Timeout: timeout,
		Name:    name,
	})
	if err != nil {
		return nil, 0, err
	}
	conn, err := natsgo.Connect(natstransport.ServerOrDefault(config.Server), opts...)
	if err != nil {
		return nil, 0, err
	}
	return conn, timeout, nil
}
