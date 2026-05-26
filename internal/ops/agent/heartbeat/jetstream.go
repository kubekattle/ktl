package heartbeat

import (
	"context"
	"errors"
	"fmt"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

const (
	IngestResultKind = "AgentRegistryCompactResult"
)

type IngestOptions struct {
	NATS        NATSConfig
	Store       RegistryStore
	Tenant      string
	Stream      string
	Durable     string
	Batch       int
	MaxMessages int
	Wait        time.Duration
	StaleAfter  time.Duration
}

type IngestResult struct {
	APIVersion   string `json:"apiVersion"`
	Kind         string `json:"kind"`
	Tenant       string `json:"tenant"`
	Stream       string `json:"stream"`
	Consumer     string `json:"consumer"`
	Processed    int    `json:"processed"`
	Stored       int    `json:"stored"`
	LastSequence uint64 `json:"lastSequence,omitempty"`
	Status       string `json:"status"`
	StartedAt    string `json:"startedAt"`
	FinishedAt   string `json:"finishedAt"`
}

func IngestJetStream(ctx context.Context, opts IngestOptions) (IngestResult, error) {
	if opts.Store == nil {
		return IngestResult{}, fmt.Errorf("registry store is required")
	}
	batch := opts.Batch
	if batch <= 0 {
		batch = 64
	}
	wait := opts.Wait
	if wait <= 0 {
		wait = time.Second
	}
	tenant := NormalizeTenant(opts.Tenant)
	stream := AgentEventStreamName(firstNonEmptyHeartbeat(opts.Stream, opts.NATS.Stream))
	durable := NormalizeSubjectToken(firstNonEmptyHeartbeat(opts.Durable, DefaultRegistryDurable), DefaultRegistryDurable)
	startedAt := time.Now().UTC()
	result := IngestResult{
		APIVersion: SnapshotAPIVersion,
		Kind:       IngestResultKind,
		Tenant:     tenant,
		Stream:     stream,
		Consumer:   durable,
		Status:     "succeeded",
		StartedAt:  startedAt.Format(time.RFC3339Nano),
	}
	conn, timeout, err := connect(ctx, opts.NATS, "torque-agent-registry-ingestor")
	if err != nil {
		return resultWithError(result), err
	}
	defer conn.Close()
	js, err := conn.JetStream(natsgo.MaxWait(timeout))
	if err != nil {
		return resultWithError(result), err
	}
	if err := ensureAgentEventStream(ctx, js, stream, opts.NATS.StreamMaxAge); err != nil {
		return resultWithError(result), err
	}
	sub, err := js.PullSubscribe(
		SubjectWildcard(tenant),
		durable,
		natsgo.BindStream(stream),
		natsgo.DeliverAll(),
		natsgo.AckExplicit(),
		natsgo.ManualAck(),
		natsgo.PullMaxWaiting(128),
	)
	if err != nil {
		return resultWithError(result), err
	}
	for opts.MaxMessages <= 0 || result.Processed < opts.MaxMessages {
		if err := ctx.Err(); err != nil {
			return resultWithError(result), err
		}
		remaining := batch
		if opts.MaxMessages > 0 && result.Processed+remaining > opts.MaxMessages {
			remaining = opts.MaxMessages - result.Processed
		}
		if remaining <= 0 {
			break
		}
		msgs, err := sub.Fetch(remaining, natsgo.MaxWait(wait))
		if err != nil {
			if errors.Is(err, natsgo.ErrTimeout) {
				break
			}
			return resultWithError(result), err
		}
		if len(msgs) == 0 {
			break
		}
		for _, msg := range msgs {
			result.Processed++
			heartbeat, err := Parse(msg.Data)
			if err != nil {
				_ = msg.Nak()
				return resultWithError(result), err
			}
			meta, _ := msg.Metadata()
			offset := StreamOffset{
				Stream:     stream,
				Consumer:   durable,
				Subject:    msg.Subject,
				ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
			}
			if meta != nil {
				offset.Sequence = meta.Sequence.Stream
				offset.NumDelivered = meta.NumDelivered
				offset.NumPending = meta.NumPending
				offset.Stream = firstNonEmptyHeartbeat(meta.Stream, offset.Stream)
				offset.Consumer = firstNonEmptyHeartbeat(meta.Consumer, offset.Consumer)
				result.LastSequence = meta.Sequence.Stream
			}
			record, err := NewCompactRecord(heartbeat, offset, time.Now(), opts.StaleAfter)
			if err != nil {
				_ = msg.Nak()
				return resultWithError(result), err
			}
			if err := opts.Store.Put(ctx, record); err != nil {
				_ = msg.Nak()
				return resultWithError(result), err
			}
			if err := msg.Ack(natsgo.Context(ctx)); err != nil {
				return resultWithError(result), err
			}
			result.Stored++
		}
	}
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result, nil
}

func resultWithError(result IngestResult) IngestResult {
	result.Status = "failed"
	result.FinishedAt = time.Now().UTC().Format(time.RFC3339Nano)
	return result
}

func firstNonEmptyHeartbeat(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
