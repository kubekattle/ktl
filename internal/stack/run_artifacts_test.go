package stack

import (
	"encoding/json"
	"testing"
)

func TestComputeRunEventIntegrity_ChainChangesOnMutation(t *testing.T) {
	ev1 := RunEvent{
		Seq:     1,
		TS:      "2025-01-01T00:00:00Z",
		RunID:   "run-1",
		NodeID:  "c1/ns/app",
		Type:    "NODE_RUNNING",
		Attempt: 1,
		Message: "",
	}
	ev1.PrevDigest = ""
	ev1.Digest, ev1.CRC32 = computeRunEventIntegrity(ev1)
	if ev1.Digest == "" || ev1.CRC32 == "" {
		t.Fatalf("missing digest/crc: %+v", ev1)
	}

	ev2 := RunEvent{
		Seq:     2,
		TS:      "2025-01-01T00:00:01Z",
		RunID:   "run-1",
		NodeID:  "c1/ns/app",
		Type:    "NODE_FAILED",
		Attempt: 1,
		Message: "boom",
	}
	ev2.PrevDigest = ev1.Digest
	ev2.Digest, ev2.CRC32 = computeRunEventIntegrity(ev2)

	ev2b := ev2
	ev2b.Message = "boom!"
	ev2b.Digest, ev2b.CRC32 = computeRunEventIntegrity(ev2b)

	if ev2.Digest == ev2b.Digest {
		t.Fatalf("expected digest to change when message changes")
	}
	if ev2.CRC32 == ev2b.CRC32 {
		t.Fatalf("expected crc to change when message changes")
	}
}

func TestComputeRunEventIntegrity_CanonicalizesStructFields(t *testing.T) {
	type receipt struct {
		Operation      string `json:"operation"`
		Status         string `json:"status"`
		TargetDigest   string `json:"targetDigest"`
		DurationMillis int64  `json:"durationMillis"`
		Error          string `json:"error,omitempty"`
	}
	fields := map[string]any{
		"phase":  "host-command",
		"status": "success",
		"receipt": receipt{
			Operation:      "run",
			Status:         "succeeded",
			TargetDigest:   "sha256:abc",
			DurationMillis: 42,
		},
	}
	ev := RunEvent{
		Seq:     1,
		TS:      "2025-01-01T00:00:00Z",
		RunID:   "run-1",
		NodeID:  "host.command.run/a",
		Type:    "PHASE_COMPLETED",
		Attempt: 1,
		Message: "success",
		Fields:  fields,
	}
	digest, crc := computeRunEventIntegrity(ev)

	raw, err := json.Marshal(fields)
	if err != nil {
		t.Fatalf("marshal fields: %v", err)
	}
	var loaded map[string]any
	if err := json.Unmarshal(raw, &loaded); err != nil {
		t.Fatalf("unmarshal fields: %v", err)
	}
	ev.Fields = loaded
	gotDigest, gotCRC := computeRunEventIntegrity(ev)
	if gotDigest != digest {
		t.Fatalf("digest changed after JSON reload: %s != %s", gotDigest, digest)
	}
	if gotCRC != crc {
		t.Fatalf("crc changed after JSON reload: %s != %s", gotCRC, crc)
	}
}
