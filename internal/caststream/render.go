package caststream

import (
	"bytes"
	"fmt"
	"html/template"
)

// LogMirrorHTMLData configures the HTML shell used by the log mirror UI.
type LogMirrorHTMLData struct {
	Title          string
	ClusterInfo    string
	FiltersEnabled bool
	ForceStatic    bool
	StaticLogsB64  string
	SessionMetaB64 string
	EventsB64      string
	ManifestsB64   string
	CaptureID      string
}

// RenderLogMirrorHTML renders the log mirror HTML template with the provided data.
func RenderLogMirrorHTML(data LogMirrorHTMLData) (string, error) {
	tmpl, err := template.New("log_mirror").Parse(logMirrorHTML)
	if err != nil {
		return "", fmt.Errorf("parse log mirror template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render log mirror template: %w", err)
	}
	return buf.String(), nil
}
