package caststream

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-logr/logr"

	"github.com/example/ktl/internal/tailer"
)

func TestHubBroadcastDeliversMessages(t *testing.T) {
	h := newHub(logr.Discard())
	c := &client{send: make(chan []byte, 1), logger: logr.Discard()}
	h.Register(c)

	msg := []byte("hello")
	h.Broadcast(msg)

	select {
	case got := <-c.send:
		if string(got) != string(msg) {
			t.Fatalf("unexpected payload: %q", got)
		}
	case <-time.After(time.Second):
		t.Fatalf("timed out waiting for broadcast")
	}
}

func TestHubBroadcastDropsSlowClients(t *testing.T) {
	h := newHub(logr.Discard())
	c := &client{send: make(chan []byte, 1), logger: logr.Discard()}
	h.Register(c)
	c.send <- []byte("backlog")

	h.Broadcast([]byte("next"))

	waitForCondition(t, func() bool {
		h.mu.RLock()
		defer h.mu.RUnlock()
		_, ok := h.clients[c]
		return !ok
	})
}

func TestEncodePayloadDefaults(t *testing.T) {
	record := tailer.LogRecord{
		Namespace:   "ns",
		Pod:         "pod",
		Container:   "ctr",
		Raw:         "raw-line",
		Source:      "stdout",
		SourceGlyph: "•",
	}
	payload, err := encodePayload(record)
	if err != nil {
		t.Fatalf("encodePayload returned error: %v", err)
	}
	var decoded map[string]string
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("unable to unmarshal payload: %v", err)
	}
	if decoded["line"] != "raw-line" {
		t.Fatalf("expected raw line fallback, got %v", decoded["line"])
	}
	if decoded["ts"] == "" {
		t.Fatalf("expected timestamp to be populated")
	}
}

func waitForCondition(t *testing.T, ok func() bool) {
	t.Helper()
	deadline := time.After(500 * time.Millisecond)
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline:
			t.Fatalf("condition not met before timeout")
		case <-ticker.C:
			if ok() {
				return
			}
		}
	}
}
