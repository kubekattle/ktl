// File: internal/workflows/buildsvc/progress.go
// Brief: Internal buildsvc package implementation for 'progress'.

// Package buildsvc provides buildsvc helpers.

package buildsvc

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/example/ktl/internal/tailer"
	"github.com/example/ktl/pkg/buildkit"
	appcompose "github.com/example/ktl/pkg/compose"
	"github.com/moby/buildkit/client"
	digest "github.com/opencontainers/go-digest"
)

type buildProgressBroadcaster struct {
	mu            sync.Mutex
	label         string
	observers     []tailer.LogObserver
	vertices      map[string]*buildVertexState
	seq           int
	cacheHits     int
	cacheMisses   int
	lastGraphEmit time.Time
}

func newBuildProgressBroadcaster(label string) *buildProgressBroadcaster {
	label = strings.TrimSpace(label)
	if label == "" {
		label = "ktl-build"
	}
	return &buildProgressBroadcaster{
		label:    label,
		vertices: make(map[string]*buildVertexState),
	}
}

func (b *buildProgressBroadcaster) emitHeatmap(summary appcompose.ServiceHeatmapSummary) {
	if b == nil {
		return
	}
	payload, err := json.Marshal(summary)
	if err != nil {
		return
	}
	line := fmt.Sprintf("Heatmap %s", string(payload))
	rec := b.newRecord(time.Now(), "⬢", "heatmap", summary.Service, "heatmap", line)
	rec.Source = "heatmap"
	rec.SourceGlyph = "⬢"
	rec.Raw = string(payload)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

type buildSummary struct {
	Tags        []string `json:"tags,omitempty"`
	Platforms   []string `json:"platforms,omitempty"`
	CacheHits   int      `json:"cacheHits,omitempty"`
	CacheMisses int      `json:"cacheMisses,omitempty"`
	Mode        string   `json:"mode,omitempty"`
	Push        bool     `json:"push,omitempty"`
	Load        bool     `json:"load,omitempty"`
}

type buildVertexState struct {
	id              string
	name            string
	cached          bool
	started         bool
	completed       bool
	errorMsg        string
	current         int64
	total           int64
	inputs          []string
	firstSeen       time.Time
	startedAt       time.Time
	completedAt     time.Time
	announcedStart  bool
	announcedCached bool
	announcedDone   bool
}

func (b *buildProgressBroadcaster) addObserver(observer tailer.LogObserver) {
	if b == nil || observer == nil {
		return
	}
	b.mu.Lock()
	b.observers = append(b.observers, observer)
	b.mu.Unlock()
}

func (b *buildProgressBroadcaster) emitInfo(message string) {
	b.emitCustom(strings.TrimSpace(message), "ℹ")
}

func (b *buildProgressBroadcaster) emitSummary(summary buildSummary) {
	if b == nil {
		return
	}
	b.mu.Lock()
	if summary.CacheHits == 0 {
		summary.CacheHits = b.cacheHits
	}
	if summary.CacheMisses == 0 {
		summary.CacheMisses = b.cacheMisses
	}
	b.mu.Unlock()
	payload, err := json.Marshal(summary)
	if err != nil {
		b.emitCustom("Summary available but failed to encode payload", "ℹ")
		return
	}
	b.emitCustom(fmt.Sprintf("Summary: %s", string(payload)), "ⓘ")
}

func (b *buildProgressBroadcaster) emitResult(resultErr error, duration time.Duration) {
	if b == nil {
		return
	}
	dur := duration.Round(time.Millisecond)
	if dur < 0 {
		dur = 0
	}
	b.emitBuildResult(resultErr, dur)
}

func (b *buildProgressBroadcaster) emitBuildResult(resultErr error, duration time.Duration) {
	type payload struct {
		Success       bool   `json:"success"`
		DurationMS    int64  `json:"durationMs"`
		Error         string `json:"error,omitempty"`
		ErrorKindHint string `json:"errorKindHint,omitempty"`
	}
	p := payload{
		Success:    resultErr == nil,
		DurationMS: duration.Milliseconds(),
	}
	glyph := "✔"
	line := fmt.Sprintf("Build finished in %s", duration)
	if resultErr != nil {
		glyph = "✖"
		p.Error = resultErr.Error()
		line = fmt.Sprintf("Build failed after %s: %v", duration, resultErr)
	}
	raw, _ := json.Marshal(p)
	rec := b.newRecord(time.Now(), glyph, "result", b.label, "result", line)
	rec.Source = "result"
	rec.Raw = string(raw)
	rec.Rendered = line
	rec.RenderedEqualsRaw = false
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) emitCustom(message, glyph string) {
	if b == nil || message == "" {
		return
	}
	rec := b.newRecord(time.Now(), glyph, "build", b.label, "info", message)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) emitPhase(name, state, message string) {
	if b == nil {
		return
	}
	name = strings.ToLower(strings.TrimSpace(name))
	if name == "" {
		return
	}
	state = strings.ToLower(strings.TrimSpace(state))
	if state == "" {
		state = "running"
	}
	msg := strings.TrimSpace(message)
	if msg == "" {
		msg = fmt.Sprintf("%s %s", name, state)
	}
	rec := b.newRecord(time.Now(), "▶", "phase", name, state, msg)
	rec.Source = "phase"
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) emitDiagnostic(diag buildkit.BuildDiagnostic, message string) {
	if b == nil || message == "" {
		return
	}
	glyph := "ℹ"
	b.mu.Lock()
	switch diag.Type {
	case buildkit.DiagnosticCacheHit:
		glyph = "✔"
		b.cacheHits++
	case buildkit.DiagnosticCacheMiss:
		glyph = "⚠"
		b.cacheMisses++
	}
	b.mu.Unlock()
	rec := b.newRecord(time.Now(), glyph, "diagnostic", b.label, "diagnostic", message)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

type buildDiagnosticObserver struct {
	stream *buildProgressBroadcaster
	writer io.Writer
}

func (o *buildDiagnosticObserver) HandleDiagnostic(diag buildkit.BuildDiagnostic) {
	if o == nil {
		return
	}
	message := formatBuildDiagnostic(diag)
	if message == "" {
		return
	}
	if o.stream != nil {
		o.stream.emitDiagnostic(diag, message)
		return
	}
	if o.writer != nil {
		fmt.Fprintln(o.writer, message)
	}
}

func (b *buildProgressBroadcaster) emitSandboxLog(line string) {
	if b == nil {
		return
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
	rec := b.newRecord(time.Now(), "⛓", "sandbox", b.label, "sandbox", line)
	observers := b.snapshotObservers()
	b.dispatch(observers, []tailer.LogRecord{rec})
}

func (b *buildProgressBroadcaster) snapshotObservers() []tailer.LogObserver {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]tailer.LogObserver, len(b.observers))
	copy(out, b.observers)
	return out
}

func (b *buildProgressBroadcaster) HandleStatus(status *client.SolveStatus) {
	if b == nil || status == nil {
		return
	}
	now := time.Now()
	b.mu.Lock()
	records := b.consumeStatusLocked(status, now)
	if graph := b.graphRecordLocked(now); graph != nil {
		records = append(records, *graph)
	}
	observers := append([]tailer.LogObserver(nil), b.observers...)
	b.mu.Unlock()
	b.dispatch(observers, records)
}

func (b *buildProgressBroadcaster) consumeStatusLocked(status *client.SolveStatus, now time.Time) []tailer.LogRecord {
	records := make([]tailer.LogRecord, 0)
	touched := make(map[string]*buildVertexState)
	for _, vertex := range status.Vertexes {
		st := b.vertexFor(vertex.Digest.String())
		if st.firstSeen.IsZero() {
			st.firstSeen = now
		}
		if name := strings.TrimSpace(vertex.Name); name != "" {
			st.name = name
		}
		if vertex.Cached {
			st.cached = true
		}
		if vertex.Started != nil {
			st.started = true
			if st.startedAt.IsZero() {
				st.startedAt = *vertex.Started
			}
		}
		if vertex.Completed != nil {
			st.completed = true
			if st.completedAt.IsZero() {
				st.completedAt = *vertex.Completed
			}
		}
		if vertex.Error != "" {
			st.errorMsg = vertex.Error
			st.completed = true
			if st.completedAt.IsZero() {
				st.completedAt = now
			}
		}
		if len(vertex.Inputs) > 0 {
			st.setInputs(vertex.Inputs)
		}
		touched[st.id] = st
	}
	for _, vs := range status.Statuses {
		st := b.vertexFor(vs.Vertex.String())
		if st.firstSeen.IsZero() {
			st.firstSeen = now
		}
		if name := strings.TrimSpace(vs.Name); name != "" {
			st.name = name
		}
		if vs.Started != nil {
			st.started = true
			if st.startedAt.IsZero() {
				st.startedAt = *vs.Started
			}
		}
		if vs.Completed != nil {
			st.completed = true
			if st.completedAt.IsZero() {
				st.completedAt = *vs.Completed
			}
		}
		if vs.Current > 0 {
			st.current = vs.Current
		}
		if vs.Total > 0 {
			st.total = vs.Total
		}
		touched[st.id] = st
	}
	for _, logEntry := range status.Logs {
		line := strings.TrimRight(string(logEntry.Data), "\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		st := b.vertexFor(logEntry.Vertex.String())
		ts := logEntry.Timestamp
		if ts.IsZero() {
			ts = now
		}
		recordLine := fmt.Sprintf("%s | %s", st.displayName(b.label), line)
		records = append(records, b.newRecord(ts, streamGlyph(logEntry.Stream), "build", st.displayName(b.label), streamLabel(logEntry.Stream), recordLine))
	}
	for _, st := range touched {
		records = append(records, b.transitionRecords(st, now)...)
	}
	return records
}

func (b *buildProgressBroadcaster) vertexFor(key string) *buildVertexState {
	key = strings.TrimSpace(key)
	if key == "" {
		b.seq++
		key = fmt.Sprintf("vertex-%d", b.seq)
	}
	if v, ok := b.vertices[key]; ok {
		return v
	}
	state := &buildVertexState{id: key}
	b.vertices[key] = state
	return state
}

func (b *buildProgressBroadcaster) newRecord(ts time.Time, glyph, namespace, pod, container, line string) tailer.LogRecord {
	if ts.IsZero() {
		ts = time.Now()
	}
	if namespace = strings.TrimSpace(namespace); namespace == "" {
		namespace = "build"
	}
	if pod = strings.TrimSpace(pod); pod == "" {
		pod = b.label
	}
	container = strings.TrimSpace(container)
	display := ts.Local().Format("15:04:05")
	return tailer.LogRecord{
		Timestamp:          ts,
		FormattedTimestamp: display,
		Namespace:          namespace,
		Pod:                pod,
		Container:          container,
		Rendered:           line,
		Raw:                line,
		Source:             "build",
		SourceGlyph:        glyph,
		RenderedEqualsRaw:  true,
	}
}

func (b *buildProgressBroadcaster) transitionRecords(st *buildVertexState, now time.Time) []tailer.LogRecord {
	name := st.displayName(b.label)
	ts := now
	records := make([]tailer.LogRecord, 0, 3)
	if st.started && !st.announcedStart {
		st.announcedStart = true
		records = append(records, b.newRecord(ts, "▶", "build", name, "status", fmt.Sprintf("Started %s", name)))
	}
	if st.cached && !st.announcedCached {
		st.announcedCached = true
		records = append(records, b.newRecord(ts, "⚡", "build", name, "status", fmt.Sprintf("Reused cache for %s", name)))
	}
	if st.completed && !st.announcedDone {
		st.announcedDone = true
		glyph := "✔"
		text := fmt.Sprintf("Completed %s", name)
		if st.errorMsg != "" {
			glyph = "✖"
			text = fmt.Sprintf("Failed %s: %s", name, st.errorMsg)
		}
		if progress := formatProgress(st.current, st.total); progress != "" {
			text = fmt.Sprintf("%s (%s)", text, progress)
		}
		records = append(records, b.newRecord(ts, glyph, "build", name, "status", text))
	}
	return records
}

func (st *buildVertexState) setInputs(inputs []digest.Digest) {
	if st == nil || len(inputs) == 0 {
		return
	}
	existing := make(map[string]struct{}, len(st.inputs))
	for _, in := range st.inputs {
		existing[in] = struct{}{}
	}
	added := false
	for _, input := range inputs {
		key := strings.TrimSpace(input.String())
		if key == "" {
			continue
		}
		if _, ok := existing[key]; ok {
			continue
		}
		st.inputs = append(st.inputs, key)
		existing[key] = struct{}{}
		added = true
	}
	if added {
		sort.Strings(st.inputs)
	}
}

func (st *buildVertexState) status() string {
	if st == nil {
		return "pending"
	}
	switch {
	case st.errorMsg != "":
		return "failed"
	case st.cached:
		return "cached"
	case st.completed:
		return "completed"
	case st.started:
		return "running"
	default:
		return "pending"
	}
}

func (b *buildProgressBroadcaster) graphRecordLocked(now time.Time) *tailer.LogRecord {
	if b == nil || len(b.vertices) == 0 {
		return nil
	}
	if !b.lastGraphEmit.IsZero() && now.Sub(b.lastGraphEmit) < 450*time.Millisecond {
		return nil
	}
	snapshot := b.buildGraphSnapshotLocked()
	if len(snapshot.Nodes) == 0 {
		return nil
	}
	payload, err := json.Marshal(snapshot)
	if err != nil {
		return nil
	}
	b.lastGraphEmit = now
	rec := b.newRecord(now, "◆", "graph", b.label, "graph", "build graph update")
	rec.Source = "graph"
	rec.Raw = string(payload)
	rec.Rendered = rec.Raw
	return &rec
}

func (b *buildProgressBroadcaster) buildGraphSnapshotLocked() buildGraphSnapshot {
	nodes := make([]buildGraphNode, 0, len(b.vertices))
	edges := make([]buildGraphEdge, 0)
	for _, st := range b.vertices {
		firstSeen := st.firstSeen
		startedAt := st.startedAt
		completedAt := st.completedAt
		nodes = append(nodes, buildGraphNode{
			ID:            st.id,
			Label:         st.displayName(b.label),
			Status:        st.status(),
			Cached:        st.cached,
			FirstSeenUnix: unixSeconds(firstSeen),
			StartedUnix:   unixSeconds(startedAt),
			CompletedUnix: unixSeconds(completedAt),
			Current:       st.current,
			Total:         st.total,
			Error:         st.errorMsg,
		})
		for _, input := range st.inputs {
			if input == "" {
				continue
			}
			edges = append(edges, buildGraphEdge{From: input, To: st.id})
		}
	}
	sort.Slice(nodes, func(i, j int) bool {
		if nodes[i].Status == nodes[j].Status {
			return nodes[i].Label < nodes[j].Label
		}
		return nodes[i].Status < nodes[j].Status
	})
	sort.Slice(edges, func(i, j int) bool {
		if edges[i].From == edges[j].From {
			return edges[i].To < edges[j].To
		}
		return edges[i].From < edges[j].From
	})
	return buildGraphSnapshot{
		Nodes: nodes,
		Edges: edges,
	}
}

func unixSeconds(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.Unix()
}

type buildGraphSnapshot struct {
	Nodes []buildGraphNode `json:"nodes"`
	Edges []buildGraphEdge `json:"edges"`
}

type buildGraphNode struct {
	ID            string `json:"id"`
	Label         string `json:"label"`
	Status        string `json:"status"`
	Cached        bool   `json:"cached"`
	FirstSeenUnix int64  `json:"firstSeenUnix,omitempty"`
	StartedUnix   int64  `json:"startedUnix,omitempty"`
	CompletedUnix int64  `json:"completedUnix,omitempty"`
	Current       int64  `json:"current,omitempty"`
	Total         int64  `json:"total,omitempty"`
	Error         string `json:"error,omitempty"`
}

type buildGraphEdge struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (b *buildProgressBroadcaster) dispatch(observers []tailer.LogObserver, records []tailer.LogRecord) {
	if len(observers) == 0 || len(records) == 0 {
		return
	}
	for _, rec := range records {
		for _, obs := range observers {
			obs.ObserveLog(rec)
		}
	}
}

func (st *buildVertexState) displayName(fallback string) string {
	if name := strings.TrimSpace(st.name); name != "" {
		return name
	}
	if st.id != "" {
		return st.id
	}
	return fallback
}

func formatProgress(current, total int64) string {
	if total <= 0 {
		if current <= 0 {
			return ""
		}
		return fmt.Sprintf("%d", current)
	}
	if current < 0 {
		current = 0
	}
	return fmt.Sprintf("%d/%d", current, total)
}

type heatmapStreamBridge struct {
	stream *buildProgressBroadcaster
}

func (h *heatmapStreamBridge) HandleServiceHeatmap(summary appcompose.ServiceHeatmapSummary) {
	if h == nil || h.stream == nil {
		return
	}
	h.stream.emitHeatmap(summary)
}

func streamGlyph(stream int) string {
	switch stream {
	case 2:
		return "!"
	default:
		return "•"
	}
}

func streamLabel(stream int) string {
	switch stream {
	case 2:
		return "stderr"
	default:
		return "stdout"
	}
}

func formatBuildDiagnostic(diag buildkit.BuildDiagnostic) string {
	parts := make([]string, 0, 3)
	reason := strings.TrimSpace(diag.Reason)
	if reason == "" {
		reason = "build diagnostic"
	}
	parts = append(parts, reason)
	if name := strings.TrimSpace(diag.Name); name != "" {
		parts = append(parts, fmt.Sprintf("step: %s", name))
	}
	if v := strings.TrimSpace(string(diag.Vertex)); v != "" {
		parts = append(parts, fmt.Sprintf("vertex: %s", v))
	}
	return strings.Join(parts, " | ")
}
