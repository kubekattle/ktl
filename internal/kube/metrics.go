package kube

import (
	"net/http"
	"sync"
	"time"

	"k8s.io/client-go/rest"
)

type APIRequestStats struct {
	mu    sync.Mutex
	count int
	total time.Duration
	max   time.Duration
}

type APIRequestMetrics struct {
	Count int
	Total time.Duration
	Max   time.Duration
}

func NewAPIRequestStats() *APIRequestStats {
	return &APIRequestStats{}
}

func (s *APIRequestStats) observe(d time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.count++
	s.total += d
	if d > s.max {
		s.max = d
	}
	s.mu.Unlock()
}

func (s *APIRequestStats) Snapshot() APIRequestMetrics {
	if s == nil {
		return APIRequestMetrics{}
	}
	s.mu.Lock()
	out := APIRequestMetrics{
		Count: s.count,
		Total: s.total,
		Max:   s.max,
	}
	s.mu.Unlock()
	return out
}

func (m APIRequestMetrics) Avg() time.Duration {
	if m.Count == 0 {
		return 0
	}
	return time.Duration(int64(m.Total) / int64(m.Count))
}

type apiMetricsRoundTripper struct {
	base  http.RoundTripper
	stats *APIRequestStats
}

func (rt *apiMetricsRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := rt.base
	if base == nil {
		base = http.DefaultTransport
	}
	start := time.Now()
	resp, err := base.RoundTrip(req)
	elapsed := time.Since(start)
	if rt.stats != nil {
		rt.stats.observe(elapsed)
	}
	return resp, err
}

// AttachAPITelemetry wraps the REST config transport to capture API latency metrics.
func AttachAPITelemetry(cfg *rest.Config, stats *APIRequestStats) {
	if cfg == nil || stats == nil {
		return
	}
	wrap := cfg.WrapTransport
	cfg.WrapTransport = func(rt http.RoundTripper) http.RoundTripper {
		if wrap != nil {
			rt = wrap(rt)
		}
		return &apiMetricsRoundTripper{base: rt, stats: stats}
	}
}
