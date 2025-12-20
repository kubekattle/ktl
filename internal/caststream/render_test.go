package caststream

import (
	"strings"
	"testing"
)

func TestRenderLogMirrorHTMLEmbedsStaticLogs(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
		StaticLogsB64:  "YWJj",
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `id="ktlStaticLogsB64"`) {
		t.Fatalf("expected static payload element in html")
	}
	if !strings.Contains(html, "statusChip.textContent = 'Static'") {
		t.Fatalf("expected static mode wiring in html")
	}
}

func TestRenderLogMirrorHTMLSkipsStaticLogsWhenEmpty(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if strings.Contains(html, `id="ktlStaticLogsB64"`) {
		t.Fatalf("did not expect static payload element in html")
	}
}

func TestRenderLogMirrorHTMLCanForceStaticModeWhenEmpty(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `id="ktlStaticLogsB64"`) {
		t.Fatalf("expected static payload element in html when forcing static mode")
	}
	if !strings.Contains(html, `data-ktl-static="true"`) {
		t.Fatalf("expected forced static mode marker in html")
	}
}

func TestRenderLogMirrorHTMLDoesNotEscapeBase64Payload(t *testing.T) {
	payload := "a+b/c=="
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
		StaticLogsB64:  payload,
		SessionMetaB64: payload,
		EventsB64:      payload,
		ManifestsB64:   payload,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `<textarea id="ktlStaticLogsB64"`) {
		t.Fatalf("expected static payload to use a textarea element")
	}
	if strings.Contains(html, `<script id="ktlStaticLogsB64"`) {
		t.Fatalf("did not expect static payload to be embedded in a script element")
	}
	if !strings.Contains(html, `<textarea id="ktlSessionMetaB64"`) {
		t.Fatalf("expected session payload to use a textarea element")
	}
	if !strings.Contains(html, `<textarea id="ktlK8sEventsB64"`) {
		t.Fatalf("expected events payload to use a textarea element")
	}
	if !strings.Contains(html, `<textarea id="ktlK8sManifestsB64"`) {
		t.Fatalf("expected manifests payload to use a textarea element")
	}
}

func TestRenderLogMirrorHTMLContainsLaneChips(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `aria-label="Lane filters"`) {
		t.Fatalf("expected lane filters block in html")
	}
	if !strings.Contains(html, `data-lane="pod"`) || !strings.Contains(html, `data-lane="event"`) {
		t.Fatalf("expected lane chips in html")
	}
}

func TestRenderLogMirrorHTMLContainsHealthPanel(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `id="healthPanel"`) {
		t.Fatalf("expected health panel in html")
	}
}

func TestRenderLogMirrorHTMLContainsCaptureButton(t *testing.T) {
	html, err := RenderLogMirrorHTML(LogMirrorHTMLData{
		Title:          "t",
		ClusterInfo:    "c",
		FiltersEnabled: true,
		ForceStatic:    true,
	})
	if err != nil {
		t.Fatalf("RenderLogMirrorHTML returned error: %v", err)
	}
	if !strings.Contains(html, `id="captureToggle"`) {
		t.Fatalf("expected capture toggle button in html")
	}
	if !strings.Contains(html, `/api/capture/start`) || !strings.Contains(html, `/api/capture/stop`) {
		t.Fatalf("expected capture api wiring in html")
	}
}
