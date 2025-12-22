package main

import (
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// ReportLine is a single-line, CI-friendly, stable-key summary.
//
// Example:
//
//	ktl-report kind=apply result=success release=monitoring namespace=default chart=tempo version=1.24.1 revision=9 elapsed_ms=4958
type reportLine struct {
	Kind      string
	Result    string // "success" or "fail"
	Release   string
	Namespace string
	Chart     string
	Version   string
	Revision  int
	ElapsedMS int64

	DryRun      bool
	Diff        bool
	KeepHistory bool
	Wait        bool
}

func reportFields(r reportLine) map[string]string {
	kind := strings.TrimSpace(r.Kind)
	if kind == "" {
		kind = "unknown"
	}
	result := strings.TrimSpace(r.Result)
	if result != "success" && result != "fail" {
		result = "unknown"
	}

	fields := map[string]string{
		"kind":   kind,
		"result": result,
	}
	if v := strings.TrimSpace(r.Release); v != "" {
		fields["release"] = v
	}
	if v := strings.TrimSpace(r.Namespace); v != "" {
		fields["namespace"] = v
	}
	if v := strings.TrimSpace(r.Chart); v != "" {
		fields["chart"] = v
	}
	if v := strings.TrimSpace(r.Version); v != "" {
		fields["version"] = v
	}
	if r.Revision != 0 {
		fields["revision"] = strconv.Itoa(r.Revision)
	}
	if r.ElapsedMS > 0 {
		fields["elapsed_ms"] = strconv.FormatInt(r.ElapsedMS, 10)
	}
	if r.DryRun {
		fields["dry_run"] = "true"
	}
	if r.Diff {
		fields["diff"] = "true"
	}
	if r.KeepHistory {
		fields["keep_history"] = "true"
	}
	if r.Wait {
		fields["wait"] = "true"
	}
	return fields
}

func writeReportTable(w io.Writer, r reportLine) {
	if w == nil {
		return
	}
	fields := reportFields(r)
	if len(fields) == 0 {
		return
	}

	// Render a single-row "resource-style" summary that matches ktl's live tables.
	// Keep it ASCII-only and stable so CI can parse it reliably.
	resource := "Release"
	if v := fields["release"]; v != "" {
		ns := fields["namespace"]
		if ns == "" {
			ns = "default"
		}
		resource = fmt.Sprintf("Release %s/%s", ns, v)
	}
	action := fields["kind"]
	if action == "" {
		action = "unknown"
	}
	status := fields["result"]
	if status == "" {
		status = "unknown"
	}

	// Message is a compact, stable, key=value set (sorted).
	messageKeys := make([]string, 0, len(fields))
	for k := range fields {
		switch k {
		case "kind", "result", "release", "namespace":
			continue
		default:
			messageKeys = append(messageKeys, k)
		}
	}
	sort.Strings(messageKeys)
	var msg strings.Builder
	for _, k := range messageKeys {
		v := fields[k]
		if strings.TrimSpace(v) == "" {
			continue
		}
		if msg.Len() > 0 {
			msg.WriteByte(' ')
		}
		fmt.Fprintf(&msg, "%s=%s", k, v)
	}
	message := msg.String()
	if message == "" {
		message = "-"
	}

	// Column widths chosen to match the existing live table feel.
	const resourceW = 40
	const actionW = 7
	const statusW = 10
	fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", resourceW, "Resource", actionW, "Action", statusW, "Status", "Message")
	fmt.Fprintln(w, strings.Repeat("-", resourceW+2+actionW+2+statusW+2+len("Message")+2))
	fmt.Fprintf(w, "%-*s  %-*s  %-*s  %s\n", resourceW, resource, actionW, action, statusW, status, message)
}
