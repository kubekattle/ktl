// File: internal/caststream/capture_test.go
// Brief: Internal caststream package implementation for 'capture'.

// Package caststream provides caststream helpers.

package caststream

import (
	"context"
	"github.com/go-logr/logr"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/example/ktl/internal/sqlitewriter"
)

type fakeCaptureController struct {
	running  bool
	id       string
	artifact string
}

func (f *fakeCaptureController) Start(ctx context.Context) (CaptureStatus, error) {
	f.running = true
	f.id = "abc"
	return CaptureStatus{Running: true, ID: f.id}, nil
}

func (f *fakeCaptureController) Stop(ctx context.Context) (CaptureStatus, CaptureView, error) {
	f.running = false
	return CaptureStatus{Running: false, ID: f.id, Artifact: f.artifact}, CaptureView{ID: f.id}, nil
}

func (f *fakeCaptureController) Status(ctx context.Context) (CaptureStatus, error) {
	return CaptureStatus{Running: f.running, ID: f.id}, nil
}

func TestCaptureEndpoints(t *testing.T) {
	tmp, err := os.MkdirTemp("", "ktl-capture-test-*")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(tmp) })

	artifactDir := filepath.Join(tmp, "artifact")
	if err := os.MkdirAll(artifactDir, 0o755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	w, err := sqlitewriter.New(filepath.Join(artifactDir, "logs.sqlite"))
	if err != nil {
		t.Fatalf("open sqlite writer: %v", err)
	}
	_ = w.Write(context.Background(), sqlitewriter.Entry{
		Namespace: "default",
		Pod:       "p",
		Container: "c",
		Raw:       "hello",
		Rendered:  "hello",
	})
	_ = w.Close()
	_ = os.WriteFile(filepath.Join(artifactDir, "metadata.json"), []byte(`{"sessionName":"t","startedAt":"2025-01-01T00:00:00Z","endedAt":"2025-01-01T00:00:01Z","durationSeconds":1,"namespaces":["default"],"allNamespaces":false,"podQuery":"","tailLines":0,"since":"","context":"","kubeconfig":"","observedPodCount":1,"eventsEnabled":false,"follow":false,"sqlitePath":"logs.sqlite","manifestsEnabled":false}`), 0o644)

	ctrl := &fakeCaptureController{artifact: artifactDir}
	s := New(":0", ModeWeb, "c", logr.Discard(), WithCaptureController(ctrl))
	s.captureRoot = filepath.Join(tmp, "store")
	if err := os.MkdirAll(s.captureRoot, 0o755); err != nil {
		t.Fatalf("mkdir store: %v", err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/api/capture/status", s.handleCaptureStatus)
	mux.HandleFunc("/api/capture/start", s.handleCaptureStart)
	mux.HandleFunc("/api/capture/stop", s.handleCaptureStop)
	mux.HandleFunc("/capture/view/", s.handleCaptureView)

	req := httptest.NewRequest(http.MethodGet, "/api/capture/status", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/capture/start", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("start code=%d body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/api/capture/stop", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("stop code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "viewerURL") {
		t.Fatalf("expected viewerURL in stop response, got %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/capture/view/abc", nil)
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("view code=%d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "data-capture-id=\"abc\"") {
		t.Fatalf("expected capture id attribute, got %s", rec.Body.String())
	}
}

func TestCaptureStatusUnavailableReturnsNull(t *testing.T) {
	s := New(":0", ModeWeb, "c", logr.Discard())
	req := httptest.NewRequest(http.MethodGet, "/api/capture/status", nil)
	rec := httptest.NewRecorder()
	s.handleCaptureStatus(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if strings.TrimSpace(rec.Body.String()) != "null" {
		t.Fatalf("expected null JSON body, got %q", rec.Body.String())
	}
}
