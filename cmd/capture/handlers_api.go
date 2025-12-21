package main

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *server) handleSessions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sessions, err := s.store.ListSessions(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"sessions": sessions})
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	// /api/session/{id}/...
	path := strings.TrimPrefix(r.URL.Path, "/api/session/")
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) < 1 || parts[0] == "" {
		http.NotFound(w, r)
		return
	}
	id := parts[0]
	rest := ""
	if len(parts) > 1 {
		rest = parts[1]
	}

	switch rest {
	case "meta":
		meta, err := s.store.GetSessionMeta(r.Context(), id)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, meta)
		return
	case "timeline":
		q := r.URL.Query()
		bucket := parseDuration(q.Get("bucket"), 1*time.Second)
		if bucket < 100*time.Millisecond {
			bucket = 100 * time.Millisecond
		}
		if bucket > 5*time.Minute {
			bucket = 5 * time.Minute
		}
		startNS := parseInt64(q.Get("start_ns"), 0)
		endNS := parseInt64(q.Get("end_ns"), 0)
		search := strings.TrimSpace(q.Get("q"))
		rows, err := s.store.Timeline(r.Context(), id, bucket, startNS, endNS, search)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"bucket_ms": int64(bucket / time.Millisecond),
			"start_ns":  startNS,
			"end_ns":    endNS,
			"rows":      rows,
		})
		return
	case "logs":
		q := r.URL.Query()
		limit := int(parseInt64(q.Get("limit"), 300))
		if limit < 50 {
			limit = 50
		}
		if limit > 2000 {
			limit = 2000
		}
		cursor := parseInt64(q.Get("cursor"), 0)
		search := strings.TrimSpace(q.Get("q"))
		out, err := s.store.Logs(r.Context(), id, cursor, limit, search)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, out)
		return
	default:
		http.NotFound(w, r)
		return
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func parseInt64(v string, def int64) int64 {
	v = strings.TrimSpace(v)
	if v == "" {
		return def
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return def
	}
	return n
}

func parseDuration(raw string, def time.Duration) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err == nil {
		return d
	}
	if ms, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return time.Duration(ms) * time.Millisecond
	}
	return def
}

