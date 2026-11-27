// Package cachemetrics publishes in-process cache statistics on the Prometheus endpoint.
package cachemetrics

import (
	"net/http"
	"sync"

	"github.com/go-chi/metrics"
	"github.com/navikt/whodis/internal/cache"
)

type cacheLabels struct {
	Cache string `label:"cache"`
}

type resultLabels struct {
	Cache  string `label:"cache"`
	Result string `label:"result"`
}

var (
	registerOnce sync.Once
	requests     metrics.CounterMetricLabeled[resultLabels]
	loads        metrics.CounterMetricLabeled[cacheLabels]
	evictions    metrics.CounterMetricLabeled[cacheLabels]
	entries      metrics.GaugeMetricLabeled[cacheLabels]
)

// register creates the metrics once per process: go-chi/metrics uses the global registry and
// panics on duplicate registration.
func register() {
	registerOnce.Do(func() {
		requests = metrics.CounterWith[resultLabels]("whodis_cache_requests_total",
			"Cache lookups by result: hit, negative_hit (cached absence) or miss.")
		loads = metrics.CounterWith[cacheLabels]("whodis_cache_loads_total",
			"Upstream loads triggered by cache misses. Misses minus loads were coalesced into another caller's load.")
		evictions = metrics.CounterWith[cacheLabels]("whodis_cache_evictions_total",
			"Live entries evicted because the cache was full.")
		entries = metrics.GaugeWith[cacheLabels]("whodis_cache_entries",
			"Entries currently held, including expired ones that have not been dropped yet.")
	})
}

// Source returns the current statistics of every cache, keyed by cache name.
type Source func() map[string]cache.Stats

type Reporter struct {
	source Source
	mu     sync.Mutex
	last   map[string]cache.Stats
}

func NewReporter(source Source) *Reporter {
	register()
	return &Reporter{source: source, last: make(map[string]cache.Stats)}
}

// Sync adds the change since the previous sync to the Prometheus counters. The caches keep
// plain atomic counters, so this runs at scrape time instead of on every lookup.
func (r *Reporter) Sync() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, now := range r.source() {
		prev := r.last[name]
		add := func(counter *metrics.CounterMetricLabeled[resultLabels], result string, delta int64) {
			if delta > 0 {
				counter.Add(float64(delta), resultLabels{Cache: name, Result: result})
			}
		}
		add(&requests, "hit", now.Hits-prev.Hits)
		add(&requests, "negative_hit", now.NegativeHits-prev.NegativeHits)
		add(&requests, "miss", now.Misses-prev.Misses)
		if d := now.Loads - prev.Loads; d > 0 {
			loads.Add(float64(d), cacheLabels{Cache: name})
		}
		if d := now.Evictions - prev.Evictions; d > 0 {
			evictions.Add(float64(d), cacheLabels{Cache: name})
		}
		entries.Set(float64(now.Size), cacheLabels{Cache: name})
		r.last[name] = now
	}
}

// Wrap refreshes the cache metrics before each request to next, typically the metrics handler.
func (r *Reporter) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		r.Sync()
		next.ServeHTTP(w, req)
	})
}
