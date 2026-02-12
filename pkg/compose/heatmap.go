package compose

import (
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/moby/buildkit/client"

	"github.com/kubekattle/ktl/pkg/buildkit"
)

// HeatmapListener receives per-service summaries during compose builds.
type HeatmapListener interface {
	HandleServiceHeatmap(ServiceHeatmapSummary)
}

// ServiceHeatmapSummary captures cache behavior and hotspots for a single compose service build.
type ServiceHeatmapSummary struct {
	Service        string           `json:"service"`
	Status         string           `json:"status"`
	DurationMillis int64            `json:"durationMs"`
	CacheHits      int              `json:"cacheHits"`
	CacheMisses    int              `json:"cacheMisses"`
	StepsTotal     int              `json:"stepsTotal"`
	StepsCached    int              `json:"stepsCached"`
	StepsExecuted  int              `json:"stepsExecuted"`
	Hotspots       []HeatmapHotspot `json:"hotspots,omitempty"`
	FailedStep     string           `json:"failedStep,omitempty"`
	FailureMessage string           `json:"failureMessage,omitempty"`
}

// HeatmapHotspot describes a slowest layer/vertex during a build.
type HeatmapHotspot struct {
	Name        string `json:"name"`
	DurationMS  int64  `json:"durationMs"`
	Cached      bool   `json:"cached"`
	Description string `json:"description,omitempty"`
}

const (
	heatmapStatusPass = "pass"
	heatmapStatusWarn = "warn"
	heatmapStatusFail = "fail"

	hotspotWarnThreshold = 5 * time.Second
	maxHotspots          = 3
)

type serviceHeatmapCollector struct {
	service string

	mu           sync.Mutex
	vertices     map[string]*heatmapVertex
	cacheHits    int
	cacheMisses  int
	firstStarted time.Time
	lastFinished time.Time
}

type heatmapVertex struct {
	name      string
	cached    bool
	started   time.Time
	completed time.Time
	duration  time.Duration
	errorMsg  string
}

func newServiceHeatmapCollector(service string) *serviceHeatmapCollector {
	return &serviceHeatmapCollector{
		service:  service,
		vertices: make(map[string]*heatmapVertex),
	}
}

// HandleStatus satisfies buildkit.ProgressObserver.
func (c *serviceHeatmapCollector) HandleStatus(status *client.SolveStatus) {
	if c == nil || status == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, vertex := range status.Vertexes {
		c.ingestVertex(vertex)
	}
}

func (c *serviceHeatmapCollector) ingestVertex(vertex *client.Vertex) {
	if vertex == nil {
		return
	}
	key := strings.TrimSpace(vertex.Digest.String())
	if key == "" {
		key = vertex.Name
	}
	if key == "" {
		return
	}
	v := c.vertex(key)
	if name := strings.TrimSpace(vertex.Name); name != "" {
		v.name = name
	}
	if vertex.Cached {
		v.cached = true
	}
	if vertex.Started != nil {
		started := vertex.Started.UTC()
		v.started = started
		if c.firstStarted.IsZero() || started.Before(c.firstStarted) {
			c.firstStarted = started
		}
	}
	if vertex.Completed != nil {
		completed := vertex.Completed.UTC()
		v.completed = completed
		if !v.started.IsZero() && !completed.IsZero() {
			if completed.After(v.started) {
				v.duration = completed.Sub(v.started)
			} else {
				v.duration = 0
			}
		}
		if c.lastFinished.IsZero() || completed.After(c.lastFinished) {
			c.lastFinished = completed
		}
	}
	if vertex.Error != "" {
		v.errorMsg = vertex.Error
	}
}

func (c *serviceHeatmapCollector) vertex(key string) *heatmapVertex {
	if existing, ok := c.vertices[key]; ok {
		return existing
	}
	v := &heatmapVertex{}
	c.vertices[key] = v
	return v
}

// HandleDiagnostic satisfies buildkit.BuildDiagnosticObserver.
func (c *serviceHeatmapCollector) HandleDiagnostic(diag buildkit.BuildDiagnostic) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	switch diag.Type {
	case buildkit.DiagnosticCacheHit:
		c.cacheHits++
	case buildkit.DiagnosticCacheMiss:
		c.cacheMisses++
	}
}

func (c *serviceHeatmapCollector) finalize(err error) ServiceHeatmapSummary {
	if c == nil {
		return ServiceHeatmapSummary{}
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	summary := ServiceHeatmapSummary{
		Service:     c.service,
		CacheHits:   c.cacheHits,
		CacheMisses: c.cacheMisses,
	}

	if !c.firstStarted.IsZero() && !c.lastFinished.IsZero() && c.lastFinished.After(c.firstStarted) {
		summary.DurationMillis = c.lastFinished.Sub(c.firstStarted).Milliseconds()
	}

	hotspots := make([]HeatmapHotspot, 0)
	for _, v := range c.vertices {
		summary.StepsTotal++
		if v.cached {
			summary.StepsCached++
		} else {
			summary.StepsExecuted++
		}
		if v.errorMsg != "" && summary.FailedStep == "" {
			summary.FailedStep = v.name
			summary.FailureMessage = v.errorMsg
		}
		if v.duration > 0 {
			hotspots = append(hotspots, HeatmapHotspot{
				Name:       fallbackVertexName(v.name, c.service),
				DurationMS: v.duration.Milliseconds(),
				Cached:     v.cached,
			})
		}
	}

	sort.SliceStable(hotspots, func(i, j int) bool {
		return hotspots[i].DurationMS > hotspots[j].DurationMS
	})
	if len(hotspots) > maxHotspots {
		hotspots = hotspots[:maxHotspots]
	}
	summary.Hotspots = hotspots

	summary.Status = heatmapStatusPass
	if err != nil || summary.FailedStep != "" {
		summary.Status = heatmapStatusFail
	} else if hasHotspotWarning(hotspots) || summary.CacheMisses > summary.CacheHits {
		summary.Status = heatmapStatusWarn
	}

	return summary
}

func fallbackVertexName(name, service string) string {
	if strings.TrimSpace(name) != "" {
		return name
	}
	if strings.TrimSpace(service) != "" {
		return service
	}
	return "layer"
}

func hasHotspotWarning(hotspots []HeatmapHotspot) bool {
	if len(hotspots) == 0 {
		return false
	}
	return time.Duration(hotspots[0].DurationMS)*time.Millisecond >= hotspotWarnThreshold
}

func (c *serviceHeatmapCollector) snapshotSummary(err error) ServiceHeatmapSummary {
	return c.finalize(err)
}
