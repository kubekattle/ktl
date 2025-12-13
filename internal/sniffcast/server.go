package sniffcast

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

	"github.com/example/ktl/internal/sniff"
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

func (s *Server) ObserveTraffic(record sniff.Record) {
	if s == nil {
		return
	}
	payload, err := encodeRecord(record)
	if err != nil {
		s.logger.Error(err, "encode traffic payload")
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
	s.logger.V(1).Info("traffic cast ready", "addr", s.addr, "mode", s.mode.String())
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
		s.logger.Error(err, "upgrade traffic websocket")
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
	Timestamp string `json:"ts"`
	DisplayTS string `json:"displayTs"`
	Label     string `json:"label"`
	Namespace string `json:"namespace"`
	Pod       string `json:"pod"`
	Container string `json:"container"`
	Stream    string `json:"stream"`
	Line      string `json:"line"`
}

func encodeRecord(record sniff.Record) ([]byte, error) {
	ts := record.Timestamp
	if ts.IsZero() {
		ts = time.Now()
	}
	label := record.Target.Label
	if strings.TrimSpace(label) == "" {
		label = fmt.Sprintf("%s/%s:%s", record.Target.Namespace, record.Target.Pod, record.Target.Container)
	}
	entry := payload{
		Timestamp: ts.UTC().Format(time.RFC3339Nano),
		DisplayTS: ts.Format("15:04:05.000"),
		Label:     label,
		Namespace: record.Target.Namespace,
		Pod:       record.Target.Pod,
		Container: record.Target.Container,
		Stream:    record.Stream,
		Line:      stripANSI(record.Line),
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
			h.logger.Info("dropping traffic client for slow reader")
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
				c.logger.V(1).Info("traffic client read error", "err", err)
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
				c.logger.V(1).Info("traffic client write error", "err", err)
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
  <title>ktl Traffic Mirror</title>
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
    .log-panel { padding:0; overflow:hidden; }
    .log-toolbar { display:flex; justify-content:space-between; align-items:center; padding:24px 32px 12px; }
    .log-toolbar span { font-size:0.85rem; letter-spacing:0.18em; text-transform:uppercase; color:var(--muted); }
    .log-feed {
      font-family: SFMono-Regular,Menlo,Monaco,Consolas,'Liberation Mono',monospace;
      padding:24px 32px 32px;
      max-height:70vh;
      overflow:auto;
      display:flex;
      flex-direction:column;
      gap:18px;
      background:linear-gradient(180deg, rgba(255,255,255,0.95), rgba(255,255,255,0.75));
    }
    .log-entry { border-left:4px solid rgba(37,99,235,0.35); padding-left:1rem; }
    .log-entry .meta { font-size:0.9rem; color:var(--muted); margin-bottom:0.35rem; display:flex; flex-wrap:wrap; gap:0.5rem; }
    .chip-inline {
      border-radius:999px;
      padding:0.15rem 0.75rem;
      background:var(--chip-bg);
      color:var(--chip-text);
      font-size:0.8rem;
      letter-spacing:0.08em;
      text-transform:uppercase;
    }
    .log-line {
      font-size:0.95rem;
      line-height:1.45;
      color:var(--text);
      white-space:pre-wrap;
      word-break:break-word;
    }
    .stat-panel h2, .helper-panel h2 { margin:0 0 1rem; font-size:1.2rem; letter-spacing:0.02em; }
    .stat-panel p { margin:0; color:var(--muted); font-size:0.95rem; }
  </style>
</head>
<body>
  <div class="chrome">
    <header>
      <div>
        <h1>ktl traffic</h1>
        <p class="subtitle">{{CLUSTER}}</p>
      </div>
      <div class="status-chip" id="statusChip">Connecting…</div>
    </header>
    <div class="layout">
      <div class="main-column">
        <section class="panel log-panel">
          <div class="log-toolbar">
            <span>Live capture</span>
            <span id="entryCount">0 packets</span>
          </div>
          <div class="log-feed" id="logFeed"></div>
        </section>
      </div>
      <aside class="insight-stack">
        <section class="panel stat-panel">
          <h2>Targets</h2>
          <p id="targetList">—</p>
        </section>
        <section class="panel helper-panel">
          <h2>Share</h2>
          <p>Point teammates to this URL to mirror the same tcpdump stream without rerunning ktl locally.</p>
        </section>
      </aside>
    </div>
  </div>
<script>
(function(){
  const feed = document.getElementById('logFeed');
  const statusChip = document.getElementById('statusChip');
  const entryCount = document.getElementById('entryCount');
  const targetList = document.getElementById('targetList');
  const maxEntries = 400;
  const targets = new Set();
  let total = 0;

  function setStatus(text, color){
    statusChip.textContent = text;
    statusChip.style.borderColor = color;
    statusChip.style.color = color;
  }

  function renderTargets(){
    if (targets.size === 0) {
      targetList.textContent = '—';
      return;
    }
    targetList.textContent = Array.from(targets).slice(0,6).join(', ') + (targets.size > 6 ? '…' : '');
  }

  function appendEntry(entry){
    total += 1;
    entryCount.textContent = total + (total === 1 ? ' packet' : ' packets');
    const wrapper = document.createElement('div');
    wrapper.className = 'log-entry';

    const meta = document.createElement('div');
    meta.className = 'meta';
    const ts = document.createElement('span');
    ts.textContent = entry.displayTs || new Date(entry.ts).toLocaleTimeString();
    const chip = document.createElement('span');
    chip.className = 'chip-inline';
    chip.textContent = entry.stream;
    const label = document.createElement('span');
    const fallback = entry.namespace + '/' + entry.pod + ':' + entry.container;
    const labelText = entry.label && entry.label.trim() !== '' ? entry.label : fallback;
    label.textContent = labelText;
    targets.add(labelText);
    renderTargets();
    meta.appendChild(ts);
    meta.appendChild(chip);
    meta.appendChild(label);

    const line = document.createElement('div');
    line.className = 'log-line';
    line.textContent = entry.line;

    wrapper.appendChild(meta);
    wrapper.appendChild(line);
    feed.insertBefore(wrapper, feed.firstChild);
    while (feed.children.length > maxEntries) {
      feed.removeChild(feed.lastChild);
    }
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
        appendEntry(data);
      } catch(err) {
        console.error('traffic parse error', err);
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
