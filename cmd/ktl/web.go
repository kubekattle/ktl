package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/fatih/color"
)

//go:embed dashboard.html
var dashboardFS embed.FS

func startWebServer(port int) {
	mux := http.NewServeMux()

	// Serve Static Files
	mux.Handle("/", http.FileServer(http.FS(dashboardFS)))

	// API Endpoints
	mux.HandleFunc("/api/tunnels", handleTunnels)
	mux.HandleFunc("/api/logs", handleLogs)
	mux.HandleFunc("/api/requests", handleRequests)

	addr := fmt.Sprintf(":%d", port)
	color.New(color.FgGreen).Printf("Starting dashboard on %s\n", addr)
	
	if err := http.ListenAndServe(addr, mux); err != nil {
		logEvent("Web server error: %v", err)
	}
}

func handleTunnels(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	tunnelsMu.RLock()
	defer tunnelsMu.RUnlock()
	
	if err := json.NewEncoder(w).Encode(tunnels); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleLogs(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	logMu.Lock()
	defer logMu.Unlock()
	
	if err := json.NewEncoder(w).Encode(logBuffer); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func handleRequests(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	requestLogMu.Lock()
	defer requestLogMu.Unlock()
	
	if err := json.NewEncoder(w).Encode(requestLog); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
