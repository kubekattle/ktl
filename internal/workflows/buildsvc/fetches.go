package buildsvc

import (
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"
)

type externalFetch struct {
	When    time.Time `json:"when"`
	Kind    string    `json:"kind"`
	Details string    `json:"details"`
}

// externalFetchCollector best-effort extracts "external fetch" hints from BuildKit solve status.
// It is intentionally heuristic: BuildKit doesn't provide a canonical "network fetch" event.
type externalFetchCollector struct {
	mu      sync.Mutex
	seen    map[string]externalFetch
	started time.Time
}

var (
	urlRE      = regexp.MustCompile(`https?://[^\s'"\\]+`)
	imageLike  = regexp.MustCompile(`(?i)\b(?:docker\.io|ghcr\.io|gcr\.io|quay\.io|registry\.)[^\s]+`)
	loadMetaRE = regexp.MustCompile(`(?i)\bload metadata for\s+(.+)$`)
)

func newExternalFetchCollector(started time.Time) *externalFetchCollector {
	if started.IsZero() {
		started = time.Now()
	}
	return &externalFetchCollector{
		seen:    map[string]externalFetch{},
		started: started,
	}
}

func (c *externalFetchCollector) HandleStatus(status *client.SolveStatus) {
	if c == nil || status == nil {
		return
	}
	now := time.Now().UTC()
	for _, v := range status.Vertexes {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}
		if m := loadMetaRE.FindStringSubmatch(name); len(m) == 2 {
			c.add(now, "image_metadata", strings.TrimSpace(m[1]))
			continue
		}
		if m := urlRE.FindStringSubmatch(name); len(m) > 0 {
			c.add(now, "url", m[0])
			continue
		}
		if m := imageLike.FindStringSubmatch(name); len(m) > 0 {
			c.add(now, "image_ref", m[0])
			continue
		}
	}
	for _, l := range status.Logs {
		line := strings.TrimSpace(string(l.Data))
		if line == "" {
			continue
		}
		if m := urlRE.FindStringSubmatch(line); len(m) > 0 {
			c.add(now, "url", m[0])
		}
	}
}

func (c *externalFetchCollector) add(when time.Time, kind, details string) {
	kind = strings.TrimSpace(kind)
	details = strings.TrimSpace(details)
	if kind == "" || details == "" {
		return
	}
	key := kind + ":" + details
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.seen[key]; ok {
		return
	}
	c.seen[key] = externalFetch{When: when, Kind: kind, Details: details}
}

func (c *externalFetchCollector) snapshot() []externalFetch {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]externalFetch, 0, len(c.seen))
	for _, v := range c.seen {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Kind != out[j].Kind {
			return out[i].Kind < out[j].Kind
		}
		return out[i].Details < out[j].Details
	})
	return out
}

func (c *externalFetchCollector) snapshotJSON() string {
	events := c.snapshot()
	if len(events) == 0 {
		return "[]"
	}
	b, err := json.Marshal(events)
	if err != nil {
		return "[]"
	}
	return string(b)
}
