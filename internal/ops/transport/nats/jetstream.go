package natstransport

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	natsgo "github.com/nats-io/nats.go"
)

const (
	DefaultAssignmentStream = "TORQUE_ASSIGNMENTS"
	DefaultReceiptStream    = "TORQUE_RECEIPTS"

	DefaultAssignmentStreamSubject = "torque.assign.>"
	DefaultReceiptStreamSubject    = "torque.receipt.>"

	DefaultAssignmentConsumer = "torque-agent-assignments"
	DefaultReceiptConsumer    = "torque-stack-receipts"

	DefaultTenant = "default"

	DeliveryRequestReply = "requestReply"
	DeliveryJetStream    = "jetstream"
)

var subjectTokenPattern = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

type StreamOffset struct {
	Stream       string `json:"stream,omitempty"`
	Consumer     string `json:"consumer,omitempty"`
	Subject      string `json:"subject,omitempty"`
	Sequence     uint64 `json:"sequence,omitempty"`
	NumDelivered uint64 `json:"numDelivered,omitempty"`
	NumPending   uint64 `json:"numPending,omitempty"`
	ReceivedAt   string `json:"receivedAt,omitempty"`
	Duplicate    bool   `json:"duplicate,omitempty"`
}

func AssignmentStreamName(name string) string {
	return NormalizeStreamName(name, DefaultAssignmentStream)
}

func ReceiptStreamName(name string) string {
	return NormalizeStreamName(name, DefaultReceiptStream)
}

func NormalizeStreamName(name string, fallback string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		name = fallback
	}
	return NormalizeSubjectToken(name, fallback)
}

func NormalizeTenant(tenant string) string {
	return NormalizeSubjectToken(tenant, DefaultTenant)
}

func NormalizeDelivery(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", "requestreply", "request-reply", "request_reply", "request.reply":
		return DeliveryRequestReply
	case "jetstream", "jet-stream", "jet_stream", "jet.stream":
		return DeliveryJetStream
	default:
		return strings.TrimSpace(raw)
	}
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

func AssignmentSubject(tenant string, targetID string) string {
	return "torque.assign." + NormalizeTenant(tenant) + "." + NormalizeSubjectToken(targetID, "target")
}

func ReceiptSubject(tenant string, runID string, targetID string) string {
	return "torque.receipt." + NormalizeTenant(tenant) + "." + NormalizeSubjectToken(runID, "run") + "." + NormalizeSubjectToken(targetID, "target")
}

func ReceiptSubjectWildcard(tenant string, runID string) string {
	return "torque.receipt." + NormalizeTenant(tenant) + "." + NormalizeSubjectToken(runID, "run") + ".*"
}

func EnsureStream(ctx context.Context, js natsgo.JetStreamContext, name string, subjects []string, maxAge time.Duration) error {
	if js == nil {
		return fmt.Errorf("jetstream context is required")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	name = NormalizeStreamName(name, "TORQUE_STREAM")
	subjects = normalizeSubjects(subjects)
	info, err := js.StreamInfo(name, natsgo.Context(ctx))
	if err == nil && info != nil {
		config := info.Config
		changed := false
		for _, subject := range subjects {
			if !slices.Contains(config.Subjects, subject) {
				config.Subjects = append(config.Subjects, subject)
				changed = true
			}
		}
		if maxAge > 0 && config.MaxAge != maxAge {
			config.MaxAge = maxAge
			changed = true
		}
		if changed {
			_, err = js.UpdateStream(&config, natsgo.Context(ctx))
		}
		return err
	}
	if err != nil && !errors.Is(err, natsgo.ErrStreamNotFound) {
		return err
	}
	config := &natsgo.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Retention: natsgo.LimitsPolicy,
		Storage:   natsgo.FileStorage,
		MaxAge:    maxAge,
	}
	_, err = js.AddStream(config, natsgo.Context(ctx))
	return err
}

func OffsetFromPublish(subject string, ack *natsgo.PubAck) *StreamOffset {
	if ack == nil {
		return nil
	}
	return &StreamOffset{
		Stream:    strings.TrimSpace(ack.Stream),
		Subject:   strings.TrimSpace(subject),
		Sequence:  ack.Sequence,
		Duplicate: ack.Duplicate,
		ReceivedAt: time.Now().
			UTC().
			Format(time.RFC3339Nano),
	}
}

func OffsetFromMessage(msg *natsgo.Msg, fallbackStream string, fallbackConsumer string) *StreamOffset {
	if msg == nil {
		return nil
	}
	offset := &StreamOffset{
		Stream:     strings.TrimSpace(fallbackStream),
		Consumer:   strings.TrimSpace(fallbackConsumer),
		Subject:    strings.TrimSpace(msg.Subject),
		ReceivedAt: time.Now().UTC().Format(time.RFC3339Nano),
	}
	meta, err := msg.Metadata()
	if err != nil || meta == nil {
		return offset
	}
	offset.Sequence = meta.Sequence.Stream
	offset.NumDelivered = meta.NumDelivered
	offset.NumPending = meta.NumPending
	offset.Stream = firstNonEmpty(meta.Stream, offset.Stream)
	offset.Consumer = firstNonEmpty(meta.Consumer, offset.Consumer)
	return offset
}

func normalizeSubjects(subjects []string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, len(subjects))
	for _, subject := range subjects {
		subject = strings.TrimSpace(subject)
		if subject == "" {
			continue
		}
		if _, ok := seen[subject]; ok {
			continue
		}
		seen[subject] = struct{}{}
		out = append(out, subject)
	}
	return out
}
