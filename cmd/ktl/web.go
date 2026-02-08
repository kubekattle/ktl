package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

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
	mux.HandleFunc("/api/replay", handleReplay)

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

func handleReplay(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if r.Method != "POST" {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var reqID struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&reqID); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	// Find request
	requestLogMu.Lock()
	var targetLog *HTTPRequestLog
	for _, l := range requestLog {
		if l.ID == reqID.ID {
			targetLog = &l
			break
		}
	}
	requestLogMu.Unlock() // Unlock early

	if targetLog == nil {
		http.Error(w, "Request not found", http.StatusNotFound)
		return
	}

	// Replay logic
	// We need to re-issue the request.
	// targetLog.URL is the full URL?
	// In tunnel.go logRequest: URL: req.URL.String() (e.g. /foo?bar=1)
	// Tunnel: req.Host (e.g. localhost:8080)
	// We construct full URL: http://Tunnel + URL
	
	fullURL := fmt.Sprintf("http://%s%s", targetLog.Tunnel, targetLog.URL)
	
	// Create Request
	// TODO: Store body/headers in HTTPRequestLog to allow full replay?
	// For MVP, we just replay GET/POST to same URL with empty body if we didn't save it.
	// Let's assume GET for now or empty body.
	
	newReq, err := http.NewRequest(targetLog.Method, fullURL, nil)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	
	// Set headers? We didn't save them.
	// Add a marker header
	newReq.Header.Set("X-KTL-Replay", "true")

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(newReq)
	if err != nil {
		http.Error(w, fmt.Sprintf("Replay failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer resp.Body.Close()

	json.NewEncoder(w).Encode(map[string]interface{}{
		"status": "replayed",
		"code":   resp.StatusCode,
	})
}
