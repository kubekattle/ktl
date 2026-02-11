package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPingRegistry_BearerChallengeFlow(t *testing.T) {
	var tokenIssued bool
	var gotBearer bool
	var gotBasic bool

	var baseURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v2/":
			auth := r.Header.Get("Authorization")
			if auth == "" {
				w.Header().Set("WWW-Authenticate", `Bearer realm="`+baseURL+`/token",service="ghcr.io",scope="registry:catalog:*"`)
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			if len(auth) >= 7 && auth[:7] == "Bearer " {
				gotBearer = true
				if auth == "Bearer testtoken" {
					w.WriteHeader(http.StatusOK)
					return
				}
			}
			w.WriteHeader(http.StatusUnauthorized)
		case "/token":
			user, pass, ok := r.BasicAuth()
			gotBasic = ok
			if user == "u" && pass == "p" {
				tokenIssued = true
				w.Header().Set("Content-Type", "application/json")
				_, _ = io.WriteString(w, `{"token":"testtoken"}`)
				return
			}
			w.WriteHeader(http.StatusUnauthorized)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	baseURL = srv.URL

	if err := pingRegistry(srv.URL, "u", "p"); err != nil {
		t.Fatalf("pingRegistry: %v", err)
	}
	if !tokenIssued || !gotBasic || !gotBearer {
		t.Fatalf("expected token flow; tokenIssued=%v gotBasic=%v gotBearer=%v", tokenIssued, gotBasic, gotBearer)
	}
}
