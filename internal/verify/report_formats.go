package verify

import (
	"fmt"
	"html"
	"io"
	"sort"
	"strings"
	"time"
)

func writeMarkdown(w io.Writer, rep *Report) error {
	if w == nil || rep == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("# ktl verify report\n\n")
	b.WriteString(fmt.Sprintf("- Evaluated: %s\n", rep.EvaluatedAt.Format(time.RFC3339)))
	b.WriteString(fmt.Sprintf("- Mode: %s\n", rep.Mode))
	b.WriteString(fmt.Sprintf("- Passed: %t\n", rep.Passed))
	b.WriteString(fmt.Sprintf("- Blocked: %t\n", rep.Blocked))
	b.WriteString(fmt.Sprintf("- Total findings: %d\n", rep.Summary.Total))

	if len(rep.Summary.BySev) > 0 {
		b.WriteString("\n## By severity\n\n")
		writeMarkdownSeverityTable(&b, rep.Summary.BySev)
	}

	if len(rep.Findings) == 0 {
		b.WriteString("\n_No findings._\n")
		_, _ = w.Write([]byte(b.String()))
		return nil
	}

	b.WriteString("\n## Findings\n\n")
	b.WriteString("| Severity | Rule | Resource | Message | Location | Expected | Observed |\n")
	b.WriteString("| --- | --- | --- | --- | --- | --- | --- |\n")
	for _, f := range rep.Findings {
		b.WriteString(fmt.Sprintf("| %s | %s | %s | %s | %s | %s | %s |\n",
			strings.ToUpper(string(f.Severity)),
			mdEscape(f.RuleID),
			mdEscape(findingResource(f)),
			mdEscape(f.Message),
			mdEscape(findingLocation(f)),
			mdEscape(f.Expected),
			mdEscape(f.Observed),
		))
	}
	_, _ = w.Write([]byte(b.String()))
	return nil
}

func writeMarkdownSeverityTable(b *strings.Builder, bySev map[Severity]int) {
	if b == nil {
		return
	}
	sevs := make([]Severity, 0, len(bySev))
	for sev := range bySev {
		sevs = append(sevs, sev)
	}
	sort.Slice(sevs, func(i, j int) bool { return severityRank(sevs[i]) < severityRank(sevs[j]) })
	b.WriteString("| Severity | Count |\n")
	b.WriteString("| --- | --- |\n")
	for _, sev := range sevs {
		b.WriteString(fmt.Sprintf("| %s | %d |\n", strings.ToUpper(string(sev)), bySev[sev]))
	}
}

func writeHTML(w io.Writer, rep *Report) error {
	if w == nil || rep == nil {
		return nil
	}
	var b strings.Builder
	b.WriteString("<!doctype html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\" />\n")
	b.WriteString("<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\" />\n")
	b.WriteString("<title>ktl verify report</title>\n")
	b.WriteString("<style>\n")
	b.WriteString(":root{color-scheme:light;--surface:rgba(255,255,255,0.9);--border:rgba(15,23,42,0.12);--text:#0f172a;--muted:rgba(15,23,42,0.65);--accent:#2563eb;--warn:#fbbf24;--fail:#ef4444;--success:#16a34a;}\n")
	b.WriteString("body{font-family:\"SF Pro Display\",\"SF Pro Text\",-apple-system,BlinkMacSystemFont,\"Segoe UI\",Roboto,sans-serif;margin:0;padding:40px 48px 64px;background:radial-gradient(circle at 20% 20%,#ffffff,#e9edf5 45%,#dce3f1);color:var(--text);}h1{margin:0 0 0.5rem;font-size:2rem;}h2{margin:1.6rem 0 0.6rem;font-size:1.2rem;}p{margin:0.25rem 0;color:var(--muted);}table{width:100%;border-collapse:collapse;font-size:0.95rem;margin-top:0.8rem;}th,td{padding:0.4rem 0.6rem;border-bottom:1px solid rgba(15,23,42,0.08);text-align:left;vertical-align:top;}th{text-transform:uppercase;letter-spacing:0.12em;font-size:0.72rem;color:var(--muted);}tbody tr:last-child td{border-bottom:none;}code{font-family:\"SFMono-Regular\",\"JetBrains Mono\",\"Menlo\",\"Source Code Pro\",monospace;font-size:0.9em;background:rgba(15,23,42,0.04);padding:0.1rem 0.3rem;border-radius:6px;} .panel{background:var(--surface);border:1px solid var(--border);border-radius:24px;padding:24px 28px;box-shadow:0 18px 40px rgba(15,23,42,0.12);} .badge{border-radius:999px;padding:0.15rem 0.6rem;font-size:0.7rem;text-transform:uppercase;letter-spacing:0.12em;font-weight:600;display:inline-block;} .badge.low{background:rgba(37,99,235,0.12);color:var(--accent);} .badge.medium{background:rgba(251,191,36,0.2);color:var(--warn);} .badge.high,.badge.critical{background:rgba(239,68,68,0.16);color:var(--fail);} .badge.info{background:rgba(15,23,42,0.08);color:var(--muted);} .summary-grid{display:grid;grid-template-columns:repeat(auto-fit,minmax(180px,1fr));gap:0.8rem;margin-top:1rem;}\n")
	b.WriteString("</style>\n</head>\n<body>\n<div class=\"panel\">\n")
	b.WriteString("<h1>ktl verify report</h1>\n")
	b.WriteString(fmt.Sprintf("<p>Evaluated: %s</p>\n", html.EscapeString(rep.EvaluatedAt.Format(time.RFC3339))))
	b.WriteString("<div class=\"summary-grid\">\n")
	b.WriteString(summaryCell("Mode", string(rep.Mode)))
	b.WriteString(summaryCell("Passed", fmt.Sprintf("%t", rep.Passed)))
	b.WriteString(summaryCell("Blocked", fmt.Sprintf("%t", rep.Blocked)))
	b.WriteString(summaryCell("Total findings", fmt.Sprintf("%d", rep.Summary.Total)))
	b.WriteString("</div>\n")
	b.WriteString("</div>\n")

	if len(rep.Summary.BySev) > 0 {
		b.WriteString("<div class=\"panel\" style=\"margin-top:1.5rem;\">\n")
		b.WriteString("<h2>By severity</h2>\n")
		b.WriteString("<table><thead><tr><th>Severity</th><th>Count</th></tr></thead><tbody>\n")
		sevs := make([]Severity, 0, len(rep.Summary.BySev))
		for sev := range rep.Summary.BySev {
			sevs = append(sevs, sev)
		}
		sort.Slice(sevs, func(i, j int) bool { return severityRank(sevs[i]) < severityRank(sevs[j]) })
		for _, sev := range sevs {
			b.WriteString("<tr><td>")
			b.WriteString(severityBadge(sev))
			b.WriteString("</td><td>")
			b.WriteString(html.EscapeString(fmt.Sprintf("%d", rep.Summary.BySev[sev])))
			b.WriteString("</td></tr>\n")
		}
		b.WriteString("</tbody></table></div>\n")
	}

	b.WriteString("<div class=\"panel\" style=\"margin-top:1.5rem;\">\n")
	b.WriteString("<h2>Findings</h2>\n")
	if len(rep.Findings) == 0 {
		b.WriteString("<p>No findings.</p>\n</div>\n</body>\n</html>\n")
		_, _ = io.WriteString(w, b.String())
		return nil
	}
	b.WriteString("<table><thead><tr><th>Severity</th><th>Rule</th><th>Resource</th><th>Message</th><th>Location</th><th>Expected</th><th>Observed</th></tr></thead><tbody>\n")
	for _, f := range rep.Findings {
		b.WriteString("<tr>")
		b.WriteString("<td>" + severityBadge(f.Severity) + "</td>")
		b.WriteString("<td><code>" + html.EscapeString(f.RuleID) + "</code></td>")
		b.WriteString("<td>" + html.EscapeString(findingResource(f)) + "</td>")
		b.WriteString("<td>" + html.EscapeString(f.Message) + "</td>")
		b.WriteString("<td>" + html.EscapeString(findingLocation(f)) + "</td>")
		b.WriteString("<td>" + html.EscapeString(f.Expected) + "</td>")
		b.WriteString("<td>" + html.EscapeString(f.Observed) + "</td>")
		b.WriteString("</tr>\n")
	}
	b.WriteString("</tbody></table></div>\n</body>\n</html>\n")
	_, _ = io.WriteString(w, b.String())
	return nil
}

func summaryCell(label string, value string) string {
	label = html.EscapeString(label)
	value = html.EscapeString(value)
	return fmt.Sprintf("<div><p>%s</p><strong>%s</strong></div>", label, value)
}

func severityBadge(sev Severity) string {
	label := strings.ToLower(strings.TrimSpace(string(sev)))
	if label == "" {
		label = "info"
	}
	return fmt.Sprintf("<span class=\"badge %s\">%s</span>", html.EscapeString(label), html.EscapeString(strings.ToUpper(label)))
}

func findingResource(f Finding) string {
	if f.ResourceKey != "" {
		return f.ResourceKey
	}
	return resourceKey(f.Subject)
}

func findingLocation(f Finding) string {
	if f.Path != "" {
		return f.Path
	}
	return f.Location
}

func mdEscape(value string) string {
	value = strings.ReplaceAll(value, "\n", "<br>")
	value = strings.ReplaceAll(value, "|", "\\|")
	return value
}
