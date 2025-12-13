package syscallscast

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/syscalls"
	"github.com/go-logr/logr"
	"github.com/gorilla/websocket"
)

type Mode int

const (
	ModeWeb Mode = iota
	ModeWS
)

func (m Mode) String() string {
	switch m {
	case ModeWeb:
		return "web"
	case ModeWS:
		return "ws"
	default:
		return "unknown"
	}
}

type Server struct {
	addr        string
	mode        Mode
	clusterInfo string
	hub         *hub
	upgrader    websocket.Upgrader
	logger      logr.Logger
}

func New(addr string, mode Mode, clusterInfo string, logger logr.Logger) *Server {
	return &Server{
		addr:        addr,
		mode:        mode,
		clusterInfo: clusterInfo,
		hub:         newHub(logger),
		logger:      logger,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

func (s *Server) ObserveProfile(res syscalls.ProfileResult) {
	if s == nil {
		return
	}
	payload, err := encodePayload(res)
	if err != nil {
		s.logger.Error(err, "encode profile payload")
		return
	}
	s.hub.Broadcast(payload)
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
	s.logger.V(1).Info("syscalls cast ready", "addr", s.addr, "mode", s.mode.String())
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func (s *Server) handleIndex(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	rendered := strings.Replace(indexHTML, "{{CLUSTER}}", template.HTMLEscapeString(s.clusterInfo), 1)
	_, _ = w.Write([]byte(rendered))
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	conn, err := s.upgrader.Upgrade(w, r, nil)
	if err != nil {
		s.logger.Error(err, "upgrade syscalls websocket")
		return
	}
	client := newClient(conn, s.logger)
	s.hub.Register(client)
	go client.writeLoop()
	client.readLoop(func() {
		s.hub.Unregister(client)
	})
}

type payload struct {
	Timestamp       string         `json:"ts"`
	Label           string         `json:"label"`
	Namespace       string         `json:"namespace"`
	Pod             string         `json:"pod"`
	TargetContainer string         `json:"targetContainer"`
	Duration        float64        `json:"durationSeconds"`
	TargetPID       int            `json:"targetPid"`
	TraceFilter     string         `json:"traceFilter,omitempty"`
	TotalCalls      int            `json:"totalCalls"`
	TotalErrors     int            `json:"totalErrors"`
	TotalSeconds    float64        `json:"totalSeconds"`
	Rows            []syscalls.Row `json:"rows"`
	Notes           []string       `json:"notes,omitempty"`
}

func encodePayload(res syscalls.ProfileResult) ([]byte, error) {
	ts := time.Now()
	entry := payload{
		Timestamp:       ts.UTC().Format(time.RFC3339Nano),
		Label:           res.Label,
		Namespace:       res.Namespace,
		Pod:             res.Pod,
		TargetContainer: res.TargetContainer,
		Duration:        res.Duration.Seconds(),
		TargetPID:       res.TargetPID,
		TraceFilter:     res.TraceFilter,
		TotalCalls:      res.TotalCalls,
		TotalErrors:     res.TotalErrors,
		TotalSeconds:    res.TotalSeconds,
		Rows:            res.Rows,
		Notes:           res.Notes,
	}
	return json.Marshal(entry)
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
			h.logger.Info("dropping syscalls client for slow reader")
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
}

func newClient(conn *websocket.Conn, logger logr.Logger) *client {
	return &client{conn: conn, send: make(chan []byte, 256), logger: logger}
}

func (c *client) Close() {
	_ = c.conn.Close()
}

func (c *client) readLoop(onClose func()) {
	defer onClose()
	c.conn.SetReadLimit(64 * 1024)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if !websocket.IsCloseError(err, websocket.CloseNormalClosure) {
				c.logger.V(1).Info("syscalls client read error", "err", err)
			}
			break
		}
	}
}

func (c *client) writeLoop() {
	ticker := time.NewTicker((pongWait * 9) / 10)
	defer ticker.Stop()
	for {
		select {
		case msg, ok := <-c.send:
			if !ok {
				c.conn.WriteMessage(websocket.CloseMessage, nil)
				return
			}
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.TextMessage, msg); err != nil {
				c.logger.V(1).Info("syscalls client write error", "err", err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripANSI(text string) string {
	if text == "" {
		return text
	}
	return ansiEscape.ReplaceAllString(text, "")
}

const indexHTML = `<!doctype html>
<html lang="en">
<head>
  <meta charset="utf-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>ktl Syscalls Mirror</title>
  <style>
    :root {
      color-scheme: light;
      --surface: rgba(255,255,255,0.9);
      --surface-soft: rgba(255,255,255,0.82);
      --border: rgba(15,23,42,0.12);
      --text: #0f172a;
      --muted: rgba(15,23,42,0.65);
      --accent: #2563eb;
      --chip-bg: rgba(37,99,235,0.08);
      --chip-text: #1d4ed8;
      --warn: #fbbf24;
      --fail: #ef4444;
    }
    * { box-sizing: border-box; }
    body {
      font-family: "SF Pro Display", "SF Pro Text", -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
      margin: 0;
      min-height: 100vh;
      padding: 48px 56px 72px;
      background: radial-gradient(circle at 20%% 20%%, #ffffff, #e9edf5 45%%, #dce3f1);
      color: var(--text);
    }
    .chrome { max-width: 1600px; margin: 0 auto; }
    header {
      display:flex;
      justify-content:space-between;
      align-items:flex-start;
      gap:1.5rem;
      margin-bottom:32px;
    }
    h1 { font-size:2.4rem; font-weight:600; letter-spacing:-0.04em; margin:0; }
    .subtitle { font-size:1rem; color:var(--muted); margin:0; }
    .status-chip {
      border-radius:999px;
      border:1px solid rgba(37,99,235,0.2);
      padding:0.4rem 1rem;
      font-weight:600;
      color:var(--chip-text);
      background:var(--chip-bg);
    }
    .layout { display:flex; gap:22px; align-items:flex-start; }
    .main-column { flex:1 1 auto; min-width:0; }
    .insight-stack { width:420px; display:flex; flex-direction:column; gap:24px; position:sticky; top:32px; }
    @media (max-width: 1100px) {
      body { padding:32px 24px 48px; }
      .layout { flex-direction:column; }
      .insight-stack { width:100%%; position:static; }
    }
    .panel {
      border-radius:28px;
      padding:32px;
      background:var(--surface);
      border:1px solid var(--border);
      backdrop-filter:blur(18px);
      box-shadow:0 40px 80px rgba(16,23,36,0.12);
    }
    .card-list { display:flex; flex-direction:column; gap:18px; }
    .syscall-card { padding:24px 28px; }
    .card-header { display:flex; justify-content:space-between; align-items:flex-start; gap:1rem; margin-bottom:1rem; }
    .card-header h2 { margin:0; font-size:1.25rem; letter-spacing:-0.01em; }
    .chip-inline {
      border-radius:999px;
      padding:0.15rem 0.75rem;
      background:var(--chip-bg);
      color:var(--chip-text);
      font-size:0.8rem;
      letter-spacing:0.08em;
      text-transform:uppercase;
    }
    table { width:100%%; border-collapse:collapse; font-family:SFMono-Regular,Menlo,Monaco,Consolas,'Liberation Mono',monospace; }
    th, td { padding:6px 4px; border-bottom:1px solid rgba(15,23,42,0.08); font-size:0.85rem; text-align:left; }
    th { text-transform:uppercase; letter-spacing:0.08em; font-size:0.7rem; color:var(--muted); }
    .notes { margin-top:0.75rem; color:var(--muted); font-size:0.9rem; }
  </style>
</head>
<body>
  <div class="chrome">
    <header>
      <div>
        <h1>ktl syscalls</h1>
        <p class="subtitle">{{CLUSTER}}</p>
      </div>
      <div class="status-chip" id="statusChip">Waiting for profiles…</div>
    </header>
    <div class="layout">
      <div class="main-column">
        <div class="card-list" id="cardList"></div>
      </div>
      <aside class="insight-stack">
        <section class="panel">
          <h2>Total Profiles</h2>
          <div id="totalCount" style="font-size:2.6rem;font-weight:600;">0</div>
        </section>
        <section class="panel">
          <h2>Filters</h2>
          <p id="filtersList" style="color:var(--muted); margin:0;">—</p>
        </section>
      </aside>
    </div>
  </div>
<script>
(function(){
  const statusChip = document.getElementById('statusChip');
  const cardList = document.getElementById('cardList');
  const totalCount = document.getElementById('totalCount');
  const filtersList = document.getElementById('filtersList');
  const cards = new Map();
  const filters = new Set();
  let total = 0;

  function setStatus(text, color){
    statusChip.textContent = text;
    statusChip.style.borderColor = color;
    statusChip.style.color = color;
  }

  function updateFilters(filter){
    if (!filter) { return; }
    filters.add(filter);
    filtersList.textContent = Array.from(filters).join(', ');
  }

  function renderRows(rows){
    if (!rows || rows.length === 0) {
      return '<p class="notes">No syscall activity captured.</p>';
    }
    const head = '<tr><th>Syscall</th><th>%</th><th>Seconds</th><th>Calls</th><th>Errors</th></tr>';
    const max = Math.min(rows.length, 10);
    let body = '';
    for (let i = 0; i < max; i++) {
      const row = rows[i];
      body += '<tr><td>' + row.syscall + '</td><td>' + row.percent.toFixed(2) + '</td><td>' + row.seconds.toFixed(6) + '</td><td>' + row.calls + '</td><td>' + row.errors + '</td></tr>';
    }
    return '<table>' + head + body + '</table>';
  }

  function renderNotes(notes){
    if (!notes || notes.length === 0) {
      return '';
    }
    return '<div class="notes">' + notes.map(n => '• ' + n).join('<br/>') + '</div>';
  }

  function upsertCard(entry){
    total += 1;
    totalCount.textContent = total;
    updateFilters(entry.traceFilter);
    const key = entry.label;
    let el = cards.get(key);
    if (!el) {
      el = document.createElement('section');
      el.className = 'panel syscall-card';
      cards.set(key, el);
      cardList.insertBefore(el, cardList.firstChild);
    }
    const duration = entry.durationSeconds.toFixed(1) + 's';
    const header = '<div class="card-header">'
      + '<div>'
      + '<h2>' + entry.label + '</h2>'
      + '<div style="color:var(--muted);font-size:0.9rem;">' + entry.namespace + '/' + entry.pod + ' • container ' + entry.targetContainer + ' • pid ' + entry.targetPid + '</div>'
      + '</div>'
      + '<div class="chip-inline">' + duration + '</div>'
      + '</div>';
    const filtersText = entry.traceFilter ? '<p class="notes">Trace filter: ' + entry.traceFilter + '</p>' : '';
    el.innerHTML = header + renderRows(entry.rows) + filtersText + renderNotes(entry.notes);
  }

  function connect(){
    const proto = location.protocol === 'https:' ? 'wss' : 'ws';
    const ws = new WebSocket(proto + '://' + location.host + '/ws');
    let reconnect = true;
    ws.onopen = function(){ setStatus('Streaming', '#1d4ed8'); };
    ws.onerror = function(){ setStatus('Error', '#ef4444'); };
    ws.onclose = function(){
      if (reconnect) {
        setStatus('Reconnecting…', '#f97316');
        setTimeout(connect, 1200);
      } else {
        setStatus('Closed', '#94a3b8');
      }
    };
    ws.onmessage = function(ev){
      try {
        const data = JSON.parse(ev.data);
        upsertCard(data);
      } catch(err) {
        console.error('syscalls parse error', err);
      }
    };
    window.addEventListener('beforeunload', function(){
      reconnect = false;
      ws.close();
    });
  }

  connect();
})();
</script>
</body>
</html>`
