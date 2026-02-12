// File: internal/caststream/server.go
// Brief: Internal caststream package implementation for 'server'.

// Package caststream hosts lightweight remote streaming servers used by ktl.
// It can expose log streams over WebSocket (e.g. `ktl logs --ws-listen`) and
// render the deploy viewer HTML shell used by `ktl apply --ui` / `ktl delete --ui`.
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

	"github.com/kubekattle/ktl/internal/deploy"
	"github.com/kubekattle/ktl/internal/tailer"
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
		s.acceptLogs = false
		s.acceptDeploy = true
		if s.deployState == nil {
			s.deployState = newDeployState()
		}
	}
}

// Server exposes a lightweight HTML + WebSocket view of ktl streams.
type Server struct {
	addr           string
	mode           Mode
	logger         logr.Logger
	hub            *hub
	upgrader       websocket.Upgrader
	clusterInfo    string
	acceptLogs     bool
	acceptDeploy   bool
	deployState    *deployState
	deployTemplate *template.Template
}

func New(addr string, mode Mode, clusterInfo string, logger logr.Logger, opts ...Option) *Server {
	server := &Server{
		addr:        addr,
		mode:        mode,
		logger:      logger,
		hub:         newHub(logger),
		clusterInfo: clusterInfo,
		acceptLogs:  true,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
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
	formatted := s.renderDeployIndex(template.HTMLEscapeString(s.clusterInfo))
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

type deployTemplateData struct {
	ClusterInfo string
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

// EncodeLogRecord converts a rendered log record into the JSON payload sent over /ws.
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

	//go:embed templates/deploy_viewer.html
	deployViewerHTML string
)

func stripANSI(text string) string {
	if text == "" {
		return text
	}
	return ansiEscape.ReplaceAllString(text, "")
}
