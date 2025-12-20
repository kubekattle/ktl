// Package caststream hosts the lightweight remote log “casting” servers used by
// `ktl logs --ui/--ws-listen`. It mirrors Tailer output to HTML or raw
// WebSocket clients so responders can follow the same stream without rerunning
// ktl locally.
package caststream

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"

	"github.com/example/ktl/internal/deploy"
	"github.com/example/ktl/internal/tailer"
)

type Mode int

const (
	ModeWeb Mode = iota
	ModeWS
)

// Option configures the caststream server.
type Option func(*Server)

// WithDeployUI switches the server into deploy webcast mode.
func WithDeployUI() Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.renderIndex = s.renderDeployIndex
		s.acceptLogs = false
		s.acceptDeploy = true
		if s.deployState == nil {
			s.deployState = newDeployState()
		}
	}
}

// WithoutFilters hides the filter/search chrome when rendering the log mirror (used by ktl build UI).
func WithoutFilters() Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.filtersEnabled = false
	}
}

// WithoutClusterInfo hides the contextual subtitle so the log mirror can focus on log content.
func WithoutClusterInfo() Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.clusterInfo = ""
	}
}

// WithoutLogTitle hides the H1 header so embeds can reclaim vertical space.
func WithoutLogTitle() Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.logTitle = ""
	}
}

// WithLogTitle overrides the default H1 header in log mirror mode.
func WithLogTitle(title string) Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.logTitle = title
	}
}

// WithLogReplay configures a per-connection log replay source.
//
// When set, each websocket client will receive the output of replay on connect.
// This is used for offline viewers (e.g., capture replay) where the full log
// history should be delivered even if the client connects after the command
// starts.
func WithLogReplay(replay func(context.Context, func(tailer.LogRecord) error) error) Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.logReplay = replay
	}
}

// Server exposes a lightweight HTML + WebSocket view of ktl log streams.
type Server struct {
	addr           string
	mode           Mode
	logger         logr.Logger
	hub            *hub
	upgrader       websocket.Upgrader
	logTitle       string
	clusterInfo    string
	renderIndex    func(string) string
	filtersEnabled bool
	acceptLogs     bool
	acceptDeploy   bool
	logReplay      func(context.Context, func(tailer.LogRecord) error) error
	deployState    *deployState
	logTemplate    *template.Template
	deployTemplate *template.Template

	captureController CaptureController
	captureRoot       string
	captureStoreMu    sync.RWMutex
	captureStore      map[string]*storedCapture // id -> capture
}

func New(addr string, mode Mode, clusterInfo string, logger logr.Logger, opts ...Option) *Server {
	server := &Server{
		addr:           addr,
		mode:           mode,
		logger:         logger,
		hub:            newHub(logger),
		logTitle:       "ktl Log Mirror",
		clusterInfo:    clusterInfo,
		filtersEnabled: true,
		acceptLogs:     true,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		captureStore: make(map[string]*storedCapture),
	}
	server.renderIndex = server.renderLogIndex
	server.logTemplate = template.Must(template.New("log_mirror").Parse(logMirrorHTML))
	server.deployTemplate = template.Must(template.New("deploy_viewer").Parse(deployViewerHTML))
	for _, opt := range opts {
		if opt != nil {
			opt(server)
		}
	}
	return server
}

func (s *Server) Run(ctx context.Context) error {
	if s.captureRoot == "" {
		root, err := os.MkdirTemp("", "ktl-capture-store-")
		if err != nil {
			return fmt.Errorf("create capture store dir: %w", err)
		}
		s.captureRoot = root
	}
	mux := http.NewServeMux()
	if s.mode == ModeWeb {
		mux.HandleFunc("/", s.handleIndex)
		mux.HandleFunc("/api/capture/status", s.handleCaptureStatus)
		mux.HandleFunc("/api/capture/start", s.handleCaptureStart)
		mux.HandleFunc("/api/capture/stop", s.handleCaptureStop)
		mux.HandleFunc("/api/capture/upload", s.handleCaptureUpload)
		mux.HandleFunc("/api/capture/", s.handleCaptureAPI)
		mux.HandleFunc("/capture/view/", s.handleCaptureView)
	}
	mux.HandleFunc("/ws", s.handleWS)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = fmt.Fprint(w, "ok")
	})
	srv := &http.Server{Addr: s.addr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
		s.hub.Close()
		if s.captureRoot != "" {
			_ = os.RemoveAll(s.captureRoot)
		}
	}()
	s.logger.V(1).Info("cast listener ready", "addr", s.addr, "mode", s.mode.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func WithCaptureController(controller CaptureController) Option {
	return func(s *Server) {
		if s == nil {
			return
		}
		s.captureController = controller
	}
}

func (s *Server) handleCaptureStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if s == nil || s.captureController == nil {
		// Return a successful JSON response so UIs can probe for capture support
		// without triggering noisy 404s in the browser console.
		_ = json.NewEncoder(w).Encode(nil)
		return
	}
	status, err := s.captureController.Status(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleCaptureStart(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.captureController == nil {
		http.Error(w, "capture not available", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, err := s.captureController.Start(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(status)
}

func (s *Server) handleCaptureStop(w http.ResponseWriter, r *http.Request) {
	if s == nil || s.captureController == nil {
		http.Error(w, "capture not available", http.StatusNotFound)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	status, view, err := s.captureController.Stop(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if view.ID != "" && strings.TrimSpace(status.Artifact) != "" {
		if err := s.importCaptureArtifact(r.Context(), view.ID, status.Artifact); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		status.ViewerReady = true
	}
	resp := map[string]any{
		"status": status,
	}
	if view.ID != "" {
		resp["viewerURL"] = "/capture/view/" + view.ID
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}

func (s *Server) handleCaptureView(w http.ResponseWriter, r *http.Request) {
	if s == nil {
		http.NotFound(w, r)
		return
	}
	id := strings.TrimPrefix(r.URL.Path, "/capture/view/")
	id = strings.TrimSpace(id)
	if id == "" {
		http.NotFound(w, r)
		return
	}
	s.captureStoreMu.RLock()
	cap := s.captureStore[id]
	s.captureStoreMu.RUnlock()
	if cap == nil {
		http.NotFound(w, r)
		return
	}
	title := strings.TrimSpace(cap.meta.SessionName)
	if title == "" {
		title = "Capture"
	}
	clusterInfo := captureClusterInfo(cap.meta, filepath.Base(cap.dir))
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	rendered := s.renderTemplate(s.logTemplate, logTemplateData{
		Title:          title,
		ClusterInfo:    clusterInfo,
		FiltersEnabled: true,
		ForceStatic:    false,
		StaticLogsB64:  "",
		SessionMetaB64: "",
		EventsB64:      "",
		ManifestsB64:   "",
		CaptureID:      id,
	})
	_, _ = w.Write([]byte(rendered))
}

func (s *Server) ObserveLog(record tailer.LogRecord) {
	if s == nil || !s.acceptLogs {
		return
	}
	payload, err := encodePayload(record)
	if err != nil {
		s.logger.Error(err, "encode cast payload")
		return
	}
	s.hub.Broadcast(payload)
}

// HandleDeployEvent satisfies deploy.StreamObserver so deploy applies can mirror structured events.
func (s *Server) HandleDeployEvent(event deploy.StreamEvent) {
	if s == nil || !s.acceptDeploy {
		return
	}
	if s.deployState != nil {
		s.deployState.Record(event)
	}
	payload, err := json.Marshal(event)
	if err != nil {
		s.logger.Error(err, "encode deploy cast payload")
		return
	}
	s.hub.Broadcast(payload)
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	renderer := s.renderIndex
	if renderer == nil {
		renderer = s.renderLogIndex
	}
	formatted := renderer(template.HTMLEscapeString(s.clusterInfo))
	_, _ = w.Write([]byte(formatted))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error(err, "upgrade cast websocket")
		return
	}
	if s.acceptLogs && s.logReplay != nil {
		s.runLogReplayConnection(r.Context(), conn)
		return
	}
	client := newClient(conn, s.logger)
	s.hub.Register(client)
	go client.writeLoop()
	if s.deployState != nil {
		go s.deployState.Replay(client.send)
	}
	client.readLoop(func() {
		s.hub.Unregister(client)
	})
}

func (s *Server) runLogReplayConnection(ctx context.Context, conn *websocket.Conn) {
	if conn == nil {
		return
	}
	defer conn.Close()
	if ctx == nil {
		ctx = context.Background()
	}
	replayCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan struct{})
	go func() {
		defer close(done)
		conn.SetReadLimit(1024)
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		conn.SetPongHandler(func(string) error {
			_ = conn.SetReadDeadline(time.Now().Add(pongWait))
			return nil
		})
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				cancel()
				return
			}
		}
	}()

	send := func(record tailer.LogRecord) error {
		select {
		case <-replayCtx.Done():
			return replayCtx.Err()
		default:
		}
		payload, err := encodePayload(record)
		if err != nil {
			return err
		}
		_ = conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := conn.WriteMessage(websocket.TextMessage, payload); err != nil {
			return err
		}
		return nil
	}

	if err := s.logReplay(replayCtx, send); err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, io.EOF) {
		s.logger.Error(err, "log replay failed")
	}
	<-done
}

type logTemplateData struct {
	Title          string
	ClusterInfo    string
	FiltersEnabled bool
	ForceStatic    bool
	StaticLogsB64  string
	SessionMetaB64 string
	EventsB64      string
	ManifestsB64   string
	CaptureID      string
}

type deployTemplateData struct {
	ClusterInfo string
}

// ImportCapture stores the capture artifact on disk (by id) and enables the query-backed
// capture viewer endpoints for it.
func (s *Server) ImportCapture(ctx context.Context, id string, artifactPath string) error {
	return s.importCaptureArtifact(ctx, id, artifactPath)
}

func (s *Server) renderLogIndex(info string) string {
	if s == nil {
		return ""
	}
	title := s.logTitle
	return s.renderTemplate(s.logTemplate, logTemplateData{
		Title:          title,
		ClusterInfo:    info,
		FiltersEnabled: s.filtersEnabled,
		ForceStatic:    false,
		StaticLogsB64:  "",
		SessionMetaB64: "",
		EventsB64:      "",
		ManifestsB64:   "",
		CaptureID:      "",
	})
}

func (s *Server) renderDeployIndex(info string) string {
	return s.renderTemplate(s.deployTemplate, deployTemplateData{
		ClusterInfo: info,
	})
}

func (s *Server) renderTemplate(t *template.Template, data any) string {
	if t == nil {
		return ""
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		if s != nil {
			s.logger.Error(err, "render cast template")
		}
		return ""
	}
	return buf.String()
}

func encodePayload(record tailer.LogRecord) ([]byte, error) {
	ts := record.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	lineWithANSI := record.Rendered
	if lineWithANSI == "" {
		lineWithANSI = record.Raw
	}
	line := stripANSI(lineWithANSI)
	payload := struct {
		Timestamp string `json:"ts"`
		DisplayTS string `json:"displayTs"`
		Namespace string `json:"namespace"`
		Pod       string `json:"pod"`
		Container string `json:"container"`
		Source    string `json:"source"`
		Glyph     string `json:"glyph"`
		Line      string `json:"line"`
		LineANSI  string `json:"lineAnsi,omitempty"`
		Raw       string `json:"raw"`
	}{
		Timestamp: ts.UTC().Format(time.RFC3339Nano),
		DisplayTS: record.FormattedTimestamp,
		Namespace: record.Namespace,
		Pod:       record.Pod,
		Container: record.Container,
		Source:    record.Source,
		Glyph:     record.SourceGlyph,
		Line:      line,
		LineANSI:  lineWithANSI,
		Raw:       stripANSI(record.Raw),
	}
	return json.Marshal(payload)
}

// EncodeLogRecord converts a rendered log record into the JSON payload expected by
// the log mirror UI (same format as websocket frames from /ws).
func EncodeLogRecord(record tailer.LogRecord) ([]byte, error) {
	return encodePayload(record)
}

type hub struct {
	mu      sync.RWMutex
	clients map[*client]struct{}
	logger  logr.Logger
}

func newHub(logger logr.Logger) *hub {
	return &hub{clients: make(map[*client]struct{}), logger: logger}
}

func (h *hub) Register(c *client) {
	h.mu.Lock()
	h.clients[c] = struct{}{}
	h.mu.Unlock()
}

func (h *hub) Unregister(c *client) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	c.Close()
}

func (h *hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for c := range h.clients {
		select {
		case c.send <- msg:
		default:
			h.logger.Info("dropping cast client for slow reader")
			go h.Unregister(c)
		}
	}
}

func (h *hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for c := range h.clients {
		c.Close()
		delete(h.clients, c)
	}
}

const (
	writeWait = 10 * time.Second
	pongWait  = 60 * time.Second
)

type client struct {
	conn   *websocket.Conn
	send   chan []byte
	logger logr.Logger
	once   sync.Once
}

func newClient(conn *websocket.Conn, logger logr.Logger) *client {
	return &client{
		conn:   conn,
		send:   make(chan []byte, 256),
		logger: logger,
	}
}

func (c *client) writeLoop() {
	for msg := range c.send {
		_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
		if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			c.logger.Error(err, "write cast websocket message")
			return
		}
	}
}

func (c *client) readLoop(onClose func()) {
	defer func() {
		if onClose != nil {
			onClose()
		}
	}()
	c.conn.SetReadLimit(1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			return
		}
	}
}

func (c *client) Close() {
	c.once.Do(func() {
		close(c.send)
		if c.conn != nil {
			_ = c.conn.Close()
		}
	})
}

func (m Mode) String() string {
	switch m {
	case ModeWS:
		return "ws"
	case ModeWeb:
		return "web"
	default:
		return "unknown"
	}
}

var (
	ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

	//go:embed templates/log_mirror.html
	logMirrorHTML string

	//go:embed templates/deploy_viewer.html
	deployViewerHTML string
)

func stripANSI(text string) string {
	if text == "" {
		return text
	}
	return ansiEscape.ReplaceAllString(text, "")
}
