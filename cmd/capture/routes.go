package main

import (
	"net/http"
)

func (s *server) routes(mux *http.ServeMux) {
	mux.HandleFunc("/", s.handleIndex)
	mux.HandleFunc("/favicon.ico", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/api/sessions", s.handleSessions)
	mux.HandleFunc("/api/session/", s.handleSession)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
}
