package stack

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"strings"
)

func PrintRunAuditHTML(w io.Writer, a *RunAudit) error {
	if a == nil {
		return fmt.Errorf("audit is nil")
	}
	raw, err := json.MarshalIndent(a, "", "  ")
	if err != nil {
		return err
	}
	prettyPlan := ""
	if a.Plan != nil {
		if b, err := json.MarshalIndent(a.Plan, "", "  "); err == nil {
			prettyPlan = string(b)
		}
	}
	eventsText := ""
	if len(a.Events) > 0 {
		var sb strings.Builder
		enc := json.NewEncoder(&sb)
		enc.SetIndent("", "  ")
		_ = enc.Encode(a.Events)
		eventsText = sb.String()
	}

	data := struct {
		Title      string
		AuditJSON  string
		PlanJSON   string
		EventsJSON string
	}{
		Title:      fmt.Sprintf("ktl stack audit %s", a.RunID),
		AuditJSON:  string(raw),
		PlanJSON:   prettyPlan,
		EventsJSON: eventsText,
	}

	const tpl = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>{{ .Title }}</title>
  <style>
    :root { color-scheme: light; }
    body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 24px; color: #0f172a; background: #f8fafc; }
    .panel { background: rgba(255,255,255,0.95); border: 1px solid rgba(15,23,42,0.12); border-radius: 16px; padding: 16px; margin-bottom: 16px; box-shadow: 0 18px 40px rgba(15,23,42,0.08); }
    h1 { font-size: 20px; margin: 0 0 8px; }
    h2 { font-size: 14px; margin: 0 0 8px; text-transform: uppercase; letter-spacing: .14em; color: rgba(15,23,42,0.65); }
    .kv { display: grid; grid-template-columns: 160px 1fr; gap: 6px 12px; font-size: 13px; }
    .k { color: rgba(15,23,42,0.65); }
    code, pre { font-family: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", monospace; }
    pre { white-space: pre-wrap; word-break: break-word; background: rgba(15,23,42,0.04); border: 1px solid rgba(15,23,42,0.08); border-radius: 12px; padding: 12px; font-size: 12px; margin: 0; }
  </style>
</head>
<body>
  <div class="panel">
    <h1>{{ .Title }}</h1>
  </div>

  <div class="panel">
    <h2>Audit</h2>
    <pre>{{ .AuditJSON }}</pre>
  </div>

  {{ if .PlanJSON }}
  <div class="panel">
    <h2>Plan</h2>
    <pre>{{ .PlanJSON }}</pre>
  </div>
  {{ end }}

  {{ if .EventsJSON }}
  <div class="panel">
    <h2>Timeline</h2>
    <pre>{{ .EventsJSON }}</pre>
  </div>
  {{ end }}
</body>
</html>`

	t, err := template.New("audit").Parse(tpl)
	if err != nil {
		return err
	}
	return t.Execute(w, data)
}
