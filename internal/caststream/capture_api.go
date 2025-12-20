// File: internal/caststream/capture_api.go
// Brief: Internal caststream package implementation for 'capture api'.

// Package caststream provides caststream helpers.

package caststream

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type logCursor struct {
	TS string `json:"ts"`
	ID int64  `json:"id"`
}

func encodeCursor(c logCursor) string {
	raw, _ := json.Marshal(c)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(v string) (logCursor, bool) {
	text := strings.TrimSpace(v)
	if text == "" {
		return logCursor{}, false
	}
	decoded, err := base64.RawURLEncoding.DecodeString(text)
	if err != nil {
		return logCursor{}, false
	}
	var c logCursor
	if err := json.Unmarshal(decoded, &c); err != nil {
		return logCursor{}, false
	}
	if c.TS == "" || c.ID <= 0 {
		return logCursor{}, false
	}
	return c, true
}

func (s *Server) handleCaptureAPI(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.NotFound(w, r)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/capture/")
	path = strings.TrimPrefix(path, "/")
	parts := strings.SplitN(path, "/", 3)
	if len(parts) < 2 {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimSpace(parts[0])
	resource := strings.TrimSpace(parts[1])
	if id == "" || resource == "" {
		http.NotFound(w, r)
		return
	}

	s.captureStoreMu.RLock()
	cap := s.captureStore[id]
	s.captureStoreMu.RUnlock()
	if cap == nil || cap.db == nil {
		http.NotFound(w, r)
		return
	}

	switch resource {
	case "meta":
		s.handleCaptureMeta(w, r, cap)
	case "timeline":
		s.handleCaptureTimeline(w, r, cap)
	case "logs":
		s.handleCaptureLogs(w, r, cap)
	case "events":
		s.handleCaptureEvents(w, r, cap)
	case "facets":
		s.handleCaptureFacets(w, r, cap)
	case "manifests":
		s.handleCaptureManifests(w, r, cap)
	case "manifest":
		if len(parts) < 3 {
			http.NotFound(w, r)
			return
		}
		s.handleCaptureManifest(w, r, cap, parts[2])
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleCaptureFacets(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	q := r.URL.Query()
	start, _ := parseTimeRFC3339(q.Get("start"))
	end, _ := parseTimeRFC3339(q.Get("end"))
	tsExpr := `COALESCE(log_timestamp, collected_at)`

	where := []string{"1=1"}
	var args []any
	if !start.IsZero() {
		where = append(where, tsExpr+` >= ?`)
		args = append(args, start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		where = append(where, tsExpr+` <= ?`)
		args = append(args, end.UTC().Format(time.RFC3339Nano))
	}
	cond := strings.Join(where, " AND ")

	type facetRow struct {
		Value string `json:"value"`
		Count int64  `json:"count"`
	}
	type podRow struct {
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Count     int64  `json:"count"`
	}

	ctx := r.Context()
	nsRows, _ := cap.db.QueryContext(ctx, `SELECT namespace, COUNT(1) AS n FROM logs WHERE `+cond+` AND namespace IS NOT NULL AND TRIM(namespace) != '' GROUP BY namespace ORDER BY n DESC LIMIT 5000`, args...)
	var namespaces []facetRow
	if nsRows != nil {
		for nsRows.Next() {
			var v string
			var n int64
			if err := nsRows.Scan(&v, &n); err != nil {
				break
			}
			namespaces = append(namespaces, facetRow{Value: v, Count: n})
		}
		_ = nsRows.Close()
	}

	pRows, _ := cap.db.QueryContext(ctx, `SELECT namespace, pod, COUNT(1) AS n FROM logs WHERE `+cond+` AND pod IS NOT NULL AND TRIM(pod) != '' GROUP BY namespace, pod ORDER BY n DESC LIMIT 10000`, args...)
	var pods []podRow
	if pRows != nil {
		for pRows.Next() {
			var ns, pod string
			var n int64
			if err := pRows.Scan(&ns, &pod, &n); err != nil {
				break
			}
			pods = append(pods, podRow{Namespace: ns, Pod: pod, Count: n})
		}
		_ = pRows.Close()
	}

	cRows, _ := cap.db.QueryContext(ctx, `SELECT container, COUNT(1) AS n FROM logs WHERE `+cond+` AND container IS NOT NULL AND TRIM(container) != '' GROUP BY container ORDER BY n DESC LIMIT 1000`, args...)
	var containers []facetRow
	if cRows != nil {
		for cRows.Next() {
			var v string
			var n int64
			if err := cRows.Scan(&v, &n); err != nil {
				break
			}
			containers = append(containers, facetRow{Value: v, Count: n})
		}
		_ = cRows.Close()
	}

	writeJSON(w, map[string]any{
		"namespaces": namespaces,
		"pods":       pods,
		"containers": containers,
	})
}

func (s *Server) handleCaptureMeta(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	type resp struct {
		ID        string       `json:"id"`
		Meta      captureMeta  `json:"meta"`
		Stats     captureStats `json:"stats"`
		HasFTS    bool         `json:"hasFts"`
		HasEvents bool         `json:"hasEvents"`
		HasManif  bool         `json:"hasManifests"`
	}
	meta := cap.meta
	title := strings.TrimSpace(meta.SessionName)
	if title == "" {
		title = "Capture"
	}
	out := resp{
		ID: idOrEmpty(cap),
		Meta: captureMeta{
			SessionName: title,
			Context:     strings.TrimSpace(meta.Context),
			StartedAt:   fmtRFC3339(meta.StartedAt),
			EndedAt:     fmtRFC3339(meta.EndedAt),
			Duration:    time.Duration(meta.DurationSeconds * float64(time.Second)).String(),
			Namespaces:  meta.Namespaces,
			PodQuery:    meta.PodQuery,
			PodCount:    meta.PodCount,
		},
		HasFTS:    cap.hasFTS,
		HasEvents: cap.hasEvents,
		HasManif:  cap.hasManif,
	}

	stats, _ := queryCaptureStats(r.Context(), cap.db)
	out.Stats = stats

	writeJSON(w, out)
}

type captureMeta struct {
	SessionName string   `json:"sessionName,omitempty"`
	Context     string   `json:"context,omitempty"`
	StartedAt   string   `json:"startedAt,omitempty"`
	EndedAt     string   `json:"endedAt,omitempty"`
	Duration    string   `json:"duration,omitempty"`
	Namespaces  []string `json:"namespaces,omitempty"`
	PodQuery    string   `json:"podQuery,omitempty"`
	PodCount    int      `json:"podCount,omitempty"`
}

type captureStats struct {
	Lines      int64  `json:"lines"`
	FirstTS    string `json:"firstTs,omitempty"`
	LastTS     string `json:"lastTs,omitempty"`
	Pods       int64  `json:"pods"`
	Namespaces int64  `json:"namespaces"`
}

func queryCaptureStats(ctx context.Context, db *sql.DB) (captureStats, error) {
	if db == nil {
		return captureStats{}, errors.New("db is required")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	var st captureStats
	err := db.QueryRowContext(ctx, `
SELECT
  COUNT(1) AS lines,
  MIN(COALESCE(log_timestamp, collected_at)) AS first_ts,
  MAX(COALESCE(log_timestamp, collected_at)) AS last_ts,
  COUNT(DISTINCT namespace || ':' || pod) AS pods,
  COUNT(DISTINCT namespace) AS namespaces
FROM logs`).Scan(&st.Lines, &st.FirstTS, &st.LastTS, &st.Pods, &st.Namespaces)
	return st, err
}

func (s *Server) handleCaptureTimeline(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	q := r.URL.Query()
	start, _ := parseTimeRFC3339(q.Get("start"))
	end, _ := parseTimeRFC3339(q.Get("end"))
	bucketMs := parseInt(q.Get("bucketMs"), 60_000)
	if bucketMs < 500 {
		bucketMs = 500
	}
	if bucketMs > 60*60*1000 {
		bucketMs = 60 * 60 * 1000
	}

	stats, _ := queryCaptureStats(r.Context(), cap.db)
	if start.IsZero() && stats.FirstTS != "" {
		start, _ = parseTimeRFC3339(stats.FirstTS)
	}
	if end.IsZero() && stats.LastTS != "" {
		end, _ = parseTimeRFC3339(stats.LastTS)
	}
	if start.IsZero() || end.IsZero() || !end.After(start) {
		writeJSON(w, map[string]any{"buckets": []any{}})
		return
	}

	q2 := url.Values{}
	for k, vals := range q {
		for _, v := range vals {
			q2.Add(k, v)
		}
	}
	q2.Set("start", start.UTC().Format(time.RFC3339Nano))
	q2.Set("end", end.UTC().Format(time.RFC3339Nano))

	where, args := buildLogsWhere(q2, true, cap.hasFTS)
	tsExpr := `COALESCE(log_timestamp, collected_at)`

	baseQuery := `SELECT COALESCE(log_timestamp, collected_at) AS ts, raw, rendered FROM logs`
	if cap.hasFTS && strings.TrimSpace(q.Get("q")) != "" {
		baseQuery = `SELECT ` + tsExpr + ` AS ts, raw, rendered FROM logs JOIN logs_fts ON logs_fts.rowid = logs.id`
	}
	stmt := baseQuery + ` WHERE ` + strings.Join(where, " AND ") + `
ORDER BY ts ASC`

	rows, err := cap.db.QueryContext(r.Context(), `
WITH filtered AS (`+stmt+`)
SELECT
  CAST(((julianday(ts) - julianday(?)) * 86400000.0) / ? AS INTEGER) AS bucket_idx,
  COUNT(1) AS count,
  SUM(CASE WHEN lower(COALESCE(rendered, raw, '')) LIKE '%error%' OR lower(COALESCE(rendered, raw, '')) LIKE '%fatal%' OR lower(COALESCE(rendered, raw, '')) LIKE '%panic%' OR lower(COALESCE(rendered, raw, '')) LIKE '%oomkilled%' THEN 1 ELSE 0 END) AS errors,
  SUM(CASE WHEN lower(COALESCE(rendered, raw, '')) LIKE '%warn%' THEN 1 ELSE 0 END) AS warns
FROM filtered
GROUP BY bucket_idx
ORDER BY bucket_idx ASC`,
		append(args, start.UTC().Format(time.RFC3339Nano), bucketMs)...,
	)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type bucket struct {
		StartMs int64 `json:"startMs"`
		EndMs   int64 `json:"endMs"`
		Count   int64 `json:"count"`
		Errors  int64 `json:"errors"`
		Warns   int64 `json:"warns"`
	}
	var buckets []bucket
	for rows.Next() {
		var idx int64
		var count, errs, warns int64
		if err := rows.Scan(&idx, &count, &errs, &warns); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		bStart := start.Add(time.Duration(idx*int64(bucketMs)) * time.Millisecond)
		bEnd := bStart.Add(time.Duration(bucketMs) * time.Millisecond)
		buckets = append(buckets, bucket{
			StartMs: bStart.UnixMilli(),
			EndMs:   bEnd.UnixMilli(),
			Count:   count,
			Errors:  errs,
			Warns:   warns,
		})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{
		"startMs":  start.UnixMilli(),
		"endMs":    end.UnixMilli(),
		"bucketMs": bucketMs,
		"buckets":  buckets,
	})
}

func (s *Server) handleCaptureLogs(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	q := r.URL.Query()
	limit := parseInt(q.Get("limit"), 500)
	if limit <= 0 {
		limit = 500
	}
	if limit > 5000 {
		limit = 5000
	}

	direction := strings.ToLower(strings.TrimSpace(q.Get("direction")))
	if direction == "" {
		direction = "forward"
	}

	where, args := buildLogsWhere(q, true, cap.hasFTS)
	tsExpr := `COALESCE(log_timestamp, collected_at)`

	if cur, ok := decodeCursor(q.Get("cursor")); ok {
		if direction == "backward" {
			where = append(where, `(`+tsExpr+` < ? OR (`+tsExpr+` = ? AND id < ?))`)
			args = append(args, cur.TS, cur.TS, cur.ID)
		} else {
			where = append(where, `(`+tsExpr+` > ? OR (`+tsExpr+` = ? AND id > ?))`)
			args = append(args, cur.TS, cur.TS, cur.ID)
		}
	}

	baseQuery := `SELECT id, ` + tsExpr + ` AS ts, namespace, pod, container, raw, rendered FROM logs`
	if cap.hasFTS && strings.TrimSpace(q.Get("q")) != "" {
		baseQuery = `SELECT logs.id, ` + tsExpr + ` AS ts, namespace, pod, container, raw, rendered FROM logs JOIN logs_fts ON logs_fts.rowid = logs.id`
	}
	order := ` ORDER BY ts ASC, id ASC`
	if direction == "backward" {
		order = ` ORDER BY ts DESC, id DESC`
	}
	queryText := baseQuery + ` WHERE ` + strings.Join(where, " AND ") + order + ` LIMIT ?`
	args = append(args, limit+1)

	rows, err := cap.db.QueryContext(r.Context(), queryText, args...)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	defer rows.Close()

	type payload struct {
		Timestamp string `json:"ts"`
		DisplayTS string `json:"displayTs,omitempty"`
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Container string `json:"container"`
		Source    string `json:"source"`
		Glyph     string `json:"glyph"`
		Line      string `json:"line"`
		LineANSI  string `json:"lineAnsi,omitempty"`
		Raw       string `json:"raw"`
	}
	items := make([]payload, 0, limit+1)
	cursors := make([]logCursor, 0, limit+1)

	for rows.Next() {
		var id int64
		var ts, ns, pod, c, raw, rendered string
		if err := rows.Scan(&id, &ts, &ns, &pod, &c, &raw, &rendered); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		lineWithANSI := strings.TrimSpace(rendered)
		if lineWithANSI == "" {
			lineWithANSI = raw
		}
		items = append(items, payload{
			Timestamp: ts,
			DisplayTS: ts,
			Namespace: ns,
			Pod:       pod,
			Container: c,
			Source:    "pod",
			Glyph:     "▌",
			Line:      stripANSI(lineWithANSI),
			LineANSI:  lineWithANSI,
			Raw:       stripANSI(raw),
		})
		cursors = append(cursors, logCursor{TS: ts, ID: id})
	}
	if err := rows.Err(); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	hasMore := false
	var nextCursor string
	if len(items) > limit {
		hasMore = true
		items = items[:limit]
		cursors = cursors[:limit]
	}
	if len(cursors) > 0 {
		nextCursor = encodeCursor(cursors[len(cursors)-1])
	}

	if direction == "backward" {
		for i, j := 0, len(items)-1; i < j; i, j = i+1, j-1 {
			items[i], items[j] = items[j], items[i]
		}
	}

	resp := map[string]any{
		"items":     items,
		"hasMore":   hasMore,
		"direction": direction,
	}
	if hasMore && nextCursor != "" {
		resp["nextCursor"] = nextCursor
	}
	writeJSON(w, resp)
}

func (s *Server) handleCaptureEvents(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	if !cap.hasEvents {
		writeJSON(w, []any{})
		return
	}
	q := r.URL.Query()
	start, _ := parseTimeRFC3339(q.Get("start"))
	end, _ := parseTimeRFC3339(q.Get("end"))

	where := []string{"1=1"}
	var args []any
	if !start.IsZero() {
		where = append(where, `last_timestamp >= ?`)
		args = append(args, start.UTC().Format(time.RFC3339Nano))
	}
	if !end.IsZero() {
		where = append(where, `last_timestamp <= ?`)
		args = append(args, end.UTC().Format(time.RFC3339Nano))
	}
	rows, err := cap.db.QueryContext(r.Context(), `SELECT namespace, type, reason, message, involved_kind, involved_name, involved_namespace, count, first_timestamp, last_timestamp FROM events WHERE `+strings.Join(where, " AND ")+` ORDER BY last_timestamp ASC`, args...)
	if err != nil {
		writeJSON(w, []any{})
		return
	}
	defer rows.Close()

	type event struct {
		Namespace         string `json:"namespace,omitempty"`
		Type              string `json:"type,omitempty"`
		Reason            string `json:"reason,omitempty"`
		Message           string `json:"message,omitempty"`
		InvolvedKind      string `json:"involvedKind,omitempty"`
		InvolvedName      string `json:"involvedName,omitempty"`
		InvolvedNamespace string `json:"involvedNamespace,omitempty"`
		Count             int    `json:"count,omitempty"`
		FirstTimestamp    string `json:"firstTimestamp,omitempty"`
		LastTimestamp     string `json:"lastTimestamp,omitempty"`
		TS                string `json:"ts,omitempty"`
		Dataset           string `json:"dataset,omitempty"`
	}
	var out []event
	for rows.Next() {
		var ns, typ, reason, msg, kind, name, invNS, firstTS, lastTS string
		var count int
		if err := rows.Scan(&ns, &typ, &reason, &msg, &kind, &name, &invNS, &count, &firstTS, &lastTS); err != nil {
			break
		}
		out = append(out, event{
			Namespace:         ns,
			Type:              typ,
			Reason:            reason,
			Message:           msg,
			InvolvedKind:      kind,
			InvolvedName:      name,
			InvolvedNamespace: invNS,
			Count:             count,
			FirstTimestamp:    firstTS,
			LastTimestamp:     lastTS,
			TS:                lastTS,
			Dataset:           "",
		})
	}
	writeJSON(w, out)
}

func (s *Server) handleCaptureManifests(w http.ResponseWriter, r *http.Request, cap *storedCapture) {
	if !cap.hasManif {
		writeJSON(w, map[string]any{"resources": []any{}})
		return
	}
	ctx := r.Context()
	type manifestKey struct {
		Group     string `json:"group,omitempty"`
		Kind      string `json:"kind,omitempty"`
		Namespace string `json:"namespace,omitempty"`
		Name      string `json:"name,omitempty"`
	}
	type manifestRow struct {
		Key     manifestKey   `json:"key"`
		Dataset string        `json:"dataset,omitempty"`
		YAML    string        `json:"yaml"`
		Owners  []manifestKey `json:"owners,omitempty"`
	}

	owners := make(map[string][]manifestKey)
	edgeRows, err := cap.db.QueryContext(ctx, `
SELECT
  child.kind, COALESCE(child.namespace,''), child.name,
  parent.kind, COALESCE(parent.namespace,''), parent.name,
  COALESCE(parent.api_version,'')
FROM manifest_edges
JOIN manifest_resources child ON child.id = manifest_edges.child_id
JOIN manifest_resources parent ON parent.id = manifest_edges.parent_id`)
	if err == nil {
		for edgeRows.Next() {
			var ck, cns, cn, pk, pns, pn, pAPIVersion string
			if err := edgeRows.Scan(&ck, &cns, &cn, &pk, &pns, &pn, &pAPIVersion); err != nil {
				break
			}
			childKey := fmt.Sprintf("%s|%s|%s", ck, cns, cn)
			owners[childKey] = append(owners[childKey], manifestKey{
				Group:     apiGroupFromVersion(pAPIVersion),
				Kind:      pk,
				Namespace: pns,
				Name:      pn,
			})
		}
		_ = edgeRows.Close()
	}

	rows, err := cap.db.QueryContext(ctx, `SELECT api_version, kind, COALESCE(namespace,''), name, yaml FROM manifest_resources ORDER BY kind, namespace, name`)
	if err != nil {
		writeJSON(w, map[string]any{"resources": []any{}})
		return
	}
	defer rows.Close()

	var res []manifestRow
	for rows.Next() {
		var apiVersion, kind, ns, name, yml string
		if err := rows.Scan(&apiVersion, &kind, &ns, &name, &yml); err != nil {
			break
		}
		childKey := fmt.Sprintf("%s|%s|%s", kind, ns, name)
		res = append(res, manifestRow{
			Key: manifestKey{
				Group:     apiGroupFromVersion(apiVersion),
				Kind:      kind,
				Namespace: ns,
				Name:      name,
			},
			Dataset: "",
			YAML:    yml,
			Owners:  owners[childKey],
		})
	}
	writeJSON(w, map[string]any{"resources": res})
}

func (s *Server) handleCaptureManifest(w http.ResponseWriter, r *http.Request, cap *storedCapture, key string) {
	decoded, err := url.PathUnescape(key)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	parts := strings.Split(decoded, "|")
	if len(parts) != 3 {
		http.NotFound(w, r)
		return
	}
	kind := strings.TrimSpace(parts[0])
	ns := strings.TrimSpace(parts[1])
	name := strings.TrimSpace(parts[2])
	if kind == "" || name == "" {
		http.NotFound(w, r)
		return
	}
	var yml string
	err = cap.db.QueryRowContext(r.Context(), `SELECT yaml FROM manifest_resources WHERE kind = ? AND COALESCE(namespace,'') = ? AND name = ? LIMIT 1`, kind, ns, name).Scan(&yml)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	writeJSON(w, map[string]any{"yaml": yml})
}

func buildLogsWhere(q url.Values, requireTime bool, useFTS bool) ([]string, []any) {
	tsExpr := `COALESCE(log_timestamp, collected_at)`
	where := []string{"1=1"}
	var args []any

	start, _ := parseTimeRFC3339(q.Get("start"))
	end, _ := parseTimeRFC3339(q.Get("end"))
	if requireTime || !start.IsZero() {
		if !start.IsZero() {
			where = append(where, tsExpr+` >= ?`)
			args = append(args, start.UTC().Format(time.RFC3339Nano))
		}
	}
	if requireTime || !end.IsZero() {
		if !end.IsZero() {
			where = append(where, tsExpr+` <= ?`)
			args = append(args, end.UTC().Format(time.RFC3339Nano))
		}
	}

	namespaces := splitMulti(q, "ns", "namespace")
	if len(namespaces) > 0 {
		where = append(where, inClause("namespace", len(namespaces)))
		for _, v := range namespaces {
			args = append(args, v)
		}
	}

	containers := splitMulti(q, "container", "c")
	if len(containers) > 0 {
		where = append(where, inClause("container", len(containers)))
		for _, v := range containers {
			args = append(args, v)
		}
	}

	// podValue values are encoded as "namespace::pod" so we can do an OR over pairs.
	podValues := splitMulti(q, "podValue")
	if len(podValues) > 0 {
		var ors []string
		for _, pv := range podValues {
			ns, pod := splitPodValue(pv)
			if pod == "" {
				continue
			}
			ors = append(ors, `(namespace = ? AND pod = ?)`)
			args = append(args, ns, pod)
		}
		if len(ors) > 0 {
			where = append(where, "("+strings.Join(ors, " OR ")+")")
		}
	}

	rawQ := strings.TrimSpace(q.Get("q"))
	if rawQ != "" {
		if useFTS {
			where = append(where, `logs_fts MATCH ?`)
			args = append(args, rawQ)
		} else {
			where = append(where, `(raw LIKE ? OR rendered LIKE ?)`)
			like := "%" + rawQ + "%"
			args = append(args, like, like)
		}
	}

	return where, args
}

func splitPodValue(v string) (string, string) {
	parts := strings.SplitN(v, "::", 2)
	if len(parts) == 1 {
		return "", strings.TrimSpace(parts[0])
	}
	return strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
}

func splitMulti(q url.Values, keys ...string) []string {
	var out []string
	for _, key := range keys {
		for _, raw := range q[key] {
			for _, part := range strings.Split(raw, ",") {
				val := strings.TrimSpace(part)
				if val == "" {
					continue
				}
				out = append(out, val)
			}
		}
	}
	return out
}

func inClause(col string, n int) string {
	if n <= 0 {
		return "1=1"
	}
	parts := make([]string, 0, n)
	for i := 0; i < n; i++ {
		parts = append(parts, "?")
	}
	return fmt.Sprintf("%s IN (%s)", col, strings.Join(parts, ","))
}

func parseTimeRFC3339(v string) (time.Time, bool) {
	text := strings.TrimSpace(v)
	if text == "" {
		return time.Time{}, false
	}
	// Try nano first for capture format.
	if t, err := time.Parse(time.RFC3339Nano, text); err == nil {
		return t, true
	}
	if t, err := time.Parse(time.RFC3339, text); err == nil {
		return t, true
	}
	return time.Time{}, false
}

func fmtRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseInt(v string, def int) int {
	text := strings.TrimSpace(v)
	if text == "" {
		return def
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		return def
	}
	return n
}

func writeJSON(w http.ResponseWriter, payload any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(payload)
}

func apiGroupFromVersion(apiVersion string) string {
	trim := strings.TrimSpace(apiVersion)
	if trim == "" {
		return ""
	}
	parts := strings.SplitN(trim, "/", 2)
	if len(parts) == 2 {
		return parts[0]
	}
	return "core"
}

func idOrEmpty(cap *storedCapture) string {
	if cap == nil {
		return ""
	}
	return cap.id
}
