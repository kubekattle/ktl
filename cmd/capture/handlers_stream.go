package main

import (
	"fmt"
	"net/http"
	"time"
)

func (s *server) handleStream(w http.ResponseWriter, r *http.Request, sessionID string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "stream unsupported", http.StatusInternalServerError)
		return
	}
	cursor := parseInt64(r.URL.Query().Get("cursor"), 0)
	search := r.URL.Query().Get("q")
	startNS := parseInt64(r.URL.Query().Get("start_ns"), 0)
	endNS := parseInt64(r.URL.Query().Get("end_ns"), 0)

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ticker := time.NewTicker(600 * time.Millisecond)
	defer ticker.Stop()

	send := func(event string, data any) error {
		payload, err := jsonMarshal(data)
		if err != nil {
			return err
		}
		_, err = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, payload)
		if err != nil {
			return err
		}
		flusher.Flush()
		return nil
	}

	_ = send("hello", map[string]any{"cursor": cursor})
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			page, err := s.store.Logs(r.Context(), sessionID, cursor, 400, search, startNS, endNS)
			if err != nil {
				_ = send("error", map[string]any{"error": err.Error()})
				return
			}
			if len(page.Lines) == 0 {
				continue
			}
			cursor = page.Cursor
			_ = send("logs", page)
		}
	}
}

func jsonMarshal(v any) ([]byte, error) {
	return marshalJSON(v)
}
