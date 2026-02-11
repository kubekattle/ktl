package agent

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/example/ktl/pkg/api/ktl/api/v1"
)

func TestHTTPGateway_ListSessions_Auth(t *testing.T) {
	mirror := NewMirrorServer()
	_, _, _ = mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
		SessionId: "s1",
		Producer:  "p",
		Payload:   &apiv1.MirrorFrame_Raw{Raw: []byte("x")},
	})
	_ = mirror.UpsertSessionMeta(context.Background(), "s1", MirrorSessionMeta{Command: "ktl logs"}, map[string]string{"team": "infra"})

	h := newHTTPGateway("secret", mirror)

	// Missing token.
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/mirror/sessions", nil)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusUnauthorized {
			t.Fatalf("missing token: got status %d want %d", rr.Code, http.StatusUnauthorized)
		}
	}

	// Correct token.
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/mirror/sessions", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("with token: got status %d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
		body := rr.Body.String()
		if !strings.Contains(body, "\"session_id\":\"s1\"") {
			t.Fatalf("expected session id in response, got %s", body)
		}
		if !strings.Contains(body, "\"command\":\"ktl logs\"") {
			t.Fatalf("expected command in response, got %s", body)
		}
		if !strings.Contains(body, "\"team\":\"infra\"") {
			t.Fatalf("expected tags in response, got %s", body)
		}
	}
}

func TestHTTPGateway_CookieAuth(t *testing.T) {
	mirror := NewMirrorServer()
	_, _, _ = mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
		SessionId: "s1",
		Producer:  "p",
		Payload:   &apiv1.MirrorFrame_Raw{Raw: []byte("x")},
	})

	h := newHTTPGateway("secret", mirror)

	// Login sets cookie.
	var cookie string
	{
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/cookie", nil)
		req.Header.Set("Authorization", "Bearer secret")
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusNoContent {
			t.Fatalf("login: got status %d want %d body=%s", rr.Code, http.StatusNoContent, rr.Body.String())
		}
		raw := rr.Header().Get("Set-Cookie")
		if !strings.Contains(raw, "ktl_token=") {
			t.Fatalf("expected Set-Cookie to include ktl_token, got %q", raw)
		}
		cookie = strings.TrimSpace(strings.Split(raw, ";")[0])
	}

	// Cookie authenticates requests (no header token).
	{
		req := httptest.NewRequest(http.MethodGet, "/api/v1/mirror/sessions", nil)
		req.Header.Set("Cookie", cookie)
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, req)
		if rr.Code != http.StatusOK {
			t.Fatalf("with cookie: got status %d want %d body=%s", rr.Code, http.StatusOK, rr.Body.String())
		}
	}
}

func TestHTTPGateway_TailSSE(t *testing.T) {
	mirror := NewMirrorServer()
	_, _, _ = mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
		SessionId: "s1",
		Producer:  "p",
		Payload:   &apiv1.MirrorFrame_Raw{Raw: []byte("x")},
	})

	h := newHTTPGateway("secret", mirror)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mirror/sessions/s1/tail?from_sequence=1&replay=1", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("tail handler did not stop in time")
	}

	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content-type: got %q want text/event-stream", ct)
	}
	body := rr.Body.String()
	if !strings.Contains(body, "event: frame") {
		t.Fatalf("expected SSE frame event, got %q", body)
	}
	if !strings.Contains(body, "\"session_id\":\"s1\"") {
		t.Fatalf("expected session id in SSE payload, got %q", body)
	}
}

func TestHTTPGateway_TailSSE_LastEventID(t *testing.T) {
	mirror := NewMirrorServer()
	for i := 0; i < 3; i++ {
		_, _, _ = mirror.ingestFrame(context.Background(), &apiv1.MirrorFrame{
			SessionId: "s1",
			Producer:  "p",
			Payload:   &apiv1.MirrorFrame_Raw{Raw: []byte("x")},
		})
	}

	h := newHTTPGateway("secret", mirror)

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/mirror/sessions/s1/tail?replay=1", nil).WithContext(ctx)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Last-Event-ID", "1")
	rr := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		h.ServeHTTP(rr, req)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("tail handler did not stop in time")
	}

	body := rr.Body.String()
	if strings.Contains(body, "id: 1\n") {
		t.Fatalf("expected stream to resume after id=1, got %q", body)
	}
	if !strings.Contains(body, "id: 2\n") || !strings.Contains(body, "id: 3\n") {
		t.Fatalf("expected ids 2 and 3 in response, got %q", body)
	}
}
