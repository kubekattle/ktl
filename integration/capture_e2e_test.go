package integration_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/example/ktl/internal/capture"
)

func TestKtlCaptureProducesMetadataAndSQLite(t *testing.T) {
	artifact := createCaptureArtifact(t, captureRequest{
		duration:    30 * time.Second,
		sessionName: "integration-suite",
		sqlite:      true,
	})
	meta, err := capture.LoadMetadata(artifact)
	if err != nil {
		t.Fatalf("load capture metadata: %v", err)
	}
	if meta.SessionName != "integration-suite" {
		t.Fatalf("expected session name %q, got %q", "integration-suite", meta.SessionName)
	}
	if len(meta.Namespaces) == 0 || meta.Namespaces[0] != testNamespace {
		t.Fatalf("expected namespace list to include %q: %+v", testNamespace, meta.Namespaces)
	}
	if meta.PodCount == 0 {
		t.Fatalf("expected pod count to be recorded, metadata: %+v", meta)
	}
	if meta.DurationSeconds < 5 {
		t.Fatalf("expected capture duration >=5s, got %.2f", meta.DurationSeconds)
	}
	if meta.SQLitePath != "logs.sqlite" {
		t.Fatalf("expected sqlite archive path recorded, got %q", meta.SQLitePath)
	}
	dir, cleanup, err := capture.PrepareArtifact(artifact)
	if err != nil {
		t.Fatalf("prepare capture artifact: %v", err)
	}
	if cleanup != nil {
		defer cleanup()
	}
	if _, err := os.Stat(filepath.Join(dir, "logs.sqlite")); err != nil {
		t.Fatalf("expected logs.sqlite inside capture: %v", err)
	}
}

func TestKtlCaptureReplayFilters(t *testing.T) {
	artifact := createCaptureArtifact(t, captureRequest{duration: 12 * time.Second})
	args := []string{
		"logs", "capture", "replay", artifact,
		"--namespace", testNamespace,
		"--pod", testPodName,
		"--container", "alpha",
		"--limit", "3",
		"--json",
	}
	out := runKtl(t, 45*time.Second, args...)
	lines := toLines(out)
	if len(lines) != 3 {
		t.Fatalf("expected exactly 3 replay lines, got %d (%v)", len(lines), lines)
	}
	for _, line := range lines {
		var entry capture.Entry
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("parse replay json %q: %v", line, err)
		}
		if entry.Namespace != testNamespace {
			t.Fatalf("expected namespace %q, got %q", testNamespace, entry.Namespace)
		}
		if entry.Pod != testPodName {
			t.Fatalf("expected pod %q, got %q", testPodName, entry.Pod)
		}
		if entry.Container != "alpha" {
			t.Fatalf("expected container alpha, got %q", entry.Container)
		}
		if !strings.Contains(entry.Raw, "alpha") {
			t.Fatalf("expected raw log to contain alpha marker, got %q", entry.Raw)
		}
	}
}

func TestKtlCaptureDiffLive(t *testing.T) {
	artifact := createCaptureArtifact(t, captureRequest{duration: 12 * time.Second})
	args := []string{
		"logs", "capture", "diff", artifact,
		"--live",
		"--namespace", testNamespace,
		"--pod-query", testPodName,
	}
	stdout, stderr := runKtlWithStreams(t, 60*time.Second, args...)
	combined := stdout + stderr
	if !strings.Contains(combined, "Capture (") {
		t.Fatalf("expected capture summary in diff output:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(combined, "Live (current cluster)") {
		t.Fatalf("expected live summary in diff output:\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
}

type captureRequest struct {
	duration    time.Duration
	sessionName string
	sqlite      bool
	extraArgs   []string
}

func createCaptureArtifact(t *testing.T, req captureRequest) string {
	t.Helper()
	duration := req.duration
	if duration <= 0 {
		duration = 5 * time.Second
	}
	if duration < 12*time.Second {
		duration = 12 * time.Second
	}
	sessionName := req.sessionName
	if strings.TrimSpace(sessionName) == "" {
		sessionName = fmt.Sprintf("integration-%d", time.Now().UnixNano())
	}
	outDir := t.TempDir()
	artifact := filepath.Join(outDir, fmt.Sprintf("capture-%d.tar.gz", time.Now().UnixNano()))
	args := []string{
		"logs", "capture", testPodName,
		"--namespace", testNamespace,
		fmt.Sprintf("--duration=%s", duration),
		fmt.Sprintf("--capture-output=%s", artifact),
		"--tail=30",
		"--session-name", sessionName,
		"--color=never",
	}
	if req.sqlite {
		args = append(args, "--capture-sqlite")
	}
	if len(req.extraArgs) > 0 {
		args = append(args, req.extraArgs...)
	}
	timeout := duration + 60*time.Second
	if _, stderr, err := execKtl(timeout, args...); err != nil {
		if strings.Contains(stderr, "capture graph informers failed to sync") {
			t.Skipf("capture graph informers failed to sync within %s; stderr:\n%s", duration, stderr)
		}
		t.Fatalf("ktl %v failed: %v\nstderr:\n%s", args, err, stderr)
	}
	info, err := os.Stat(artifact)
	if err != nil {
		t.Fatalf("stat capture artifact: %v", err)
	}
	if info.Size() == 0 {
		t.Fatalf("capture artifact %s is empty", artifact)
	}
	return artifact
}
