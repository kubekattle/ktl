package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

func (s *server) handleExportNDJSON(w http.ResponseWriter, r *http.Request, sessionID string, search string, startNS, endNS int64) error {
	ctx := r.Context()
	w.Header().Set("Content-Type", "application/x-ndjson; charset=utf-8")
	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=\"ktl-capture-%s.ndjson\"", sessionID))

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)

	cursor := int64(0)
	for {
		page, err := s.store.Feed(ctx, sessionID, cursor, 2000, search, startNS, endNS, "next")
		if err != nil {
			return err
		}
		for _, line := range page.Lines {
			if err := enc.Encode(line); err != nil {
				return err
			}
		}
		if !page.HasMore || len(page.Lines) == 0 {
			return nil
		}
		cursor = page.Cursor
		select {
		case <-ctx.Done():
			return context.Canceled
		default:
		}
	}
}
