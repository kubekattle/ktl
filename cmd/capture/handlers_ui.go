package main

import (
	"embed"
	"net/http"
	"strings"
)

//go:embed ui/*
var uiFS embed.FS

func (s *server) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	data, err := uiFS.ReadFile("ui/index.html")
	if err != nil {
		http.Error(w, "ui missing", http.StatusInternalServerError)
		return
	}
	payload := string(data)
	if s != nil && strings.TrimSpace(s.cfg.SessionID) != "" {
		payload = strings.ReplaceAll(payload, "__INITIAL_SESSION__", s.cfg.SessionID)
	} else {
		payload = strings.ReplaceAll(payload, "__INITIAL_SESSION__", "")
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(payload))
}
