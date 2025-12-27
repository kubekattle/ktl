package stack

import "testing"

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
