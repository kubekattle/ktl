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
	"net/http"
	"regexp"
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
	deployState    *deployState
	logTemplate    *template.Template
	deployTemplate *template.Template
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
	mux := http.NewServeMux()
	if s.mode == ModeWeb {
		mux.HandleFunc("/", s.handleIndex)
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
	}()
	s.logger.V(1).Info("cast listener ready", "addr", s.addr, "mode", s.mode.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
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

type logTemplateData struct {
	Title          string
	ClusterInfo    string
	FiltersEnabled bool
}

type deployTemplateData struct {
	ClusterInfo string
}

func (s *Server) renderLogIndex(info string) string {
	title := ""
	if s != nil {
		title = s.logTitle
	}
	return s.renderTemplate(s.logTemplate, logTemplateData{
		Title:          title,
		ClusterInfo:    info,
		FiltersEnabled: s != nil && s.filtersEnabled,
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
	if _, ok := h.clients[c]; ok {
		delete(h.clients, c)
	}
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
