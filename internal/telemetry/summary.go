package telemetry

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"
)

type Summary struct {
	Total        time.Duration
	Phases       map[string]time.Duration
	KubeRequests int
	KubeAvg      time.Duration
	KubeMax      time.Duration
	CacheHits    int
	CacheMisses  int
}

func (s Summary) Line() string {
	var parts []string
	if s.Total > 0 {
		parts = append(parts, fmt.Sprintf("total=%s", formatDuration(s.Total)))
	}
	if len(s.Phases) > 0 {
		parts = append(parts, fmt.Sprintf("phases %s", formatPhases(s.Phases)))
	}
	if s.KubeRequests > 0 {
		parts = append(parts, fmt.Sprintf("api %d req avg=%s max=%s", s.KubeRequests, formatDuration(s.KubeAvg), formatDuration(s.KubeMax)))
	}
	if s.CacheHits+s.CacheMisses > 0 {
		parts = append(parts, fmt.Sprintf("cache %d hit / %d miss", s.CacheHits, s.CacheMisses))
	}
	if len(parts) == 0 {
		return ""
	}
	return "Telemetry: " + strings.Join(parts, " · ")
}

func formatPhases(phases map[string]time.Duration) string {
	if len(phases) == 0 {
		return ""
	}
	keys := make([]string, 0, len(phases))
	for k := range phases {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		parts = append(parts, fmt.Sprintf("%s=%s", key, formatDuration(phases[key])))
	}
	return strings.Join(parts, ", ")
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "0s"
	}
	rounded := d.Round(10 * time.Millisecond)
	if rounded <= 0 {
		rounded = d
	}
	return rounded.String()
}

type PhaseTimer struct {
	mu      sync.Mutex
	started time.Time
	phases  map[string]time.Duration
}

func NewPhaseTimer() *PhaseTimer {
	return &PhaseTimer{
		started: time.Now(),
		phases:  map[string]time.Duration{},
	}
}

func (t *PhaseTimer) Start() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.started = time.Now()
	t.mu.Unlock()
}

func (t *PhaseTimer) Track(name string, fn func() error) error {
	if t == nil {
		return fn()
	}
	start := time.Now()
	err := fn()
	t.Add(name, time.Since(start))
	return err
}

func (t *PhaseTimer) TrackFunc(name string, fn func()) {
	if t == nil {
		fn()
		return
	}
	start := time.Now()
	fn()
	t.Add(name, time.Since(start))
}

func (t *PhaseTimer) Add(name string, d time.Duration) {
	if t == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return
	}
	t.mu.Lock()
	if t.phases == nil {
		t.phases = map[string]time.Duration{}
	}
	t.phases[name] += d
	t.mu.Unlock()
}

func (t *PhaseTimer) Snapshot() map[string]time.Duration {
	if t == nil {
		return nil
	}
	t.mu.Lock()
	out := make(map[string]time.Duration, len(t.phases))
	for k, v := range t.phases {
		out[k] = v
	}
	t.mu.Unlock()
	return out
}

func (t *PhaseTimer) Total() time.Duration {
	if t == nil || t.started.IsZero() {
		return 0
	}
	return time.Since(t.started)
}
