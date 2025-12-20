// File: internal/tailer/tailer_test.go
// Brief: Internal tailer package implementation for 'tailer'.

// tailer_test.go covers palette/color helpers and other tailer utilities.
package tailer

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/example/ktl/internal/config"
	"github.com/fatih/color"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
)

func TestBuildCustomPaletteSupportsMultiAttribute(t *testing.T) {
	t.Setenv("NO_COLOR", "")
	prev := color.NoColor
	color.NoColor = false
	t.Cleanup(func() {
		color.NoColor = prev
	})
	palette, err := buildCustomPalette([]string{"38;2;255;97;136"}, "--pod-colors")
	if err != nil {
		t.Fatalf("buildCustomPalette returned error: %v", err)
	}
	if len(palette) != 1 {
		t.Fatalf("expected single palette entry, got %d", len(palette))
	}
	got := palette[0].Sprint("demo")
	if !strings.Contains(got, "\x1b[38;2;255;97;136m") {
		t.Fatalf("missing expected SGR prefix in %q", got)
	}
	if !strings.Contains(got, "demo") {
		t.Fatalf("expected colored string to contain payload, got %q", got)
	}
}

func TestNormalizeColorList(t *testing.T) {
	input := []string{"", "   ", "32", " 95 "}
	got := normalizeColorList(input)
	want := []string{"32", "95"}
	if len(got) != len(want) {
		t.Fatalf("expected %d entries, got %d (%v)", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unexpected value at position %d: want %q got %q", i, want[i], got[i])
		}
	}
}

func TestFormatContainerTagWrapsName(t *testing.T) {
	t.Run("populated name", func(t *testing.T) {
		got := formatContainerTag("  coredns ")
		if got != "[coredns]" {
			t.Fatalf("expected bracketed container tag, got %q", got)
		}
	})
	t.Run("empty name", func(t *testing.T) {
		got := formatContainerTag("   ")
		if got != "" {
			t.Fatalf("expected empty tag, got %q", got)
		}
	})
}

func TestApplyColorsHandlesOverlappingNames(t *testing.T) {
	prev := color.NoColor
	color.NoColor = false
	t.Cleanup(func() {
		color.NoColor = prev
	})

	podColor := color.New(color.FgRed)
	containerColor := color.New(color.FgBlue)
	opts := &config.Options{ColorMode: "always"}
	tailer := &Tailer{
		opts:            opts,
		podColors:       []*color.Color{podColor},
		containerColors: []*color.Color{containerColor},
	}

	timestamp := "[15:14:29]"
	pod := "coredns-6d668d687-9zcgh"
	containerTag := formatContainerTag("coredns")
	payload := fmt.Sprintf("%s %s %s [WARNING] path /etc/%s/custom", timestamp, pod, containerTag, "coredns")

	colored := tailer.applyColors(timestamp, pod, containerTag, payload)
	if !strings.Contains(colored, podColor.Sprint(pod)) {
		t.Fatalf("pod name was not colored as expected: %q", colored)
	}
	if !strings.Contains(colored, containerColor.Sprint(containerTag)) {
		t.Fatalf("container tag was not colored as expected: %q", colored)
	}
}

func TestLogSourceGlyphs(t *testing.T) {
	if sourcePod.glyph() == "" || sourcePod.label() != "pod" {
		t.Fatalf("pod glyph/label not set")
	}
	if sourceNode.glyph() == "" || sourceNode.label() != "node" {
		t.Fatalf("node glyph/label not set")
	}
	if sourceEvent.glyph() == "" || sourceEvent.label() != "event" {
		t.Fatalf("event glyph/label not set")
	}
}

func TestIsRetryableLogStreamErr(t *testing.T) {
	msg := `container "grafana" in pod "grafana-6fc5cbc64b-t5n2j" is waiting to start: ContainerCreating`

	t.Run("plain error string", func(t *testing.T) {
		if !isRetryableLogStreamErr(errors.New(msg)) {
			t.Fatalf("expected retryable for %q", msg)
		}
	})

	t.Run("k8s StatusError", func(t *testing.T) {
		err := apierrors.NewBadRequest(msg)
		if !isRetryableLogStreamErr(err) {
			t.Fatalf("expected retryable for StatusError: %v", err)
		}
	})

	t.Run("wrapped StatusError", func(t *testing.T) {
		err := fmt.Errorf("wrapped: %w", apierrors.NewBadRequest(msg))
		if !isRetryableLogStreamErr(err) {
			t.Fatalf("expected retryable for wrapped StatusError: %v", err)
		}
	})

	t.Run("url.Error wrapper", func(t *testing.T) {
		err := &url.Error{Op: "Get", URL: "https://example.invalid", Err: apierrors.NewBadRequest(msg)}
		if !isRetryableLogStreamErr(err) {
			t.Fatalf("expected retryable for url.Error wrapper: %v", err)
		}
	})

	t.Run("non-retryable", func(t *testing.T) {
		if isRetryableLogStreamErr(errors.New("forbidden")) {
			t.Fatalf("expected non-retryable")
		}
	})
}
