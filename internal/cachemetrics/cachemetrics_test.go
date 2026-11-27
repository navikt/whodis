package cachemetrics

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/go-chi/metrics"
	"github.com/navikt/whodis/internal/cache"
)

var nameSequence atomic.Int32

// uniqueName keeps tests independent: the metrics live in a process-wide registry.
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, nameSequence.Add(1))
}

func scrape(t *testing.T, reporter *Reporter) string {
	t.Helper()
	w := httptest.NewRecorder()
	reporter.Wrap(metrics.Handler()).ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/internal/metrics", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	return w.Body.String()
}

func expectLine(t *testing.T, body, line string) {
	t.Helper()
	if !strings.Contains(body, line+"\n") {
		t.Errorf("missing metric line %q in:\n%s", line, grepLines(body, "whodis_cache_"))
	}
}

func grepLines(body, prefix string) string {
	var lines []string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n")
}

func TestScrapeReportsCacheStatsWithoutDoubleCounting(t *testing.T) {
	name := uniqueName("totals")
	current := cache.Stats{Hits: 3, NegativeHits: 1, Misses: 2, Loads: 2, Evictions: 1, Size: 5}
	reporter := NewReporter(func() map[string]cache.Stats {
		return map[string]cache.Stats{name: current}
	})

	body := scrape(t, reporter)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="hit"} 3`)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="negative_hit"} 1`)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="miss"} 2`)
	expectLine(t, body, `whodis_cache_loads_total{cache="`+name+`"} 2`)
	expectLine(t, body, `whodis_cache_evictions_total{cache="`+name+`"} 1`)
	expectLine(t, body, `whodis_cache_entries{cache="`+name+`"} 5`)

	// A repeated scrape with unchanged stats must not add anything.
	body = scrape(t, reporter)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="hit"} 3`)

	current = cache.Stats{Hits: 7, NegativeHits: 1, Misses: 3, Loads: 3, Evictions: 1, Size: 4}
	body = scrape(t, reporter)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="hit"} 7`)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+name+`",result="miss"} 3`)
	expectLine(t, body, `whodis_cache_loads_total{cache="`+name+`"} 3`)
	expectLine(t, body, `whodis_cache_entries{cache="`+name+`"} 4`)
}

func TestCachesAreReportedSeparately(t *testing.T) {
	first, second := uniqueName("first"), uniqueName("second")
	reporter := NewReporter(func() map[string]cache.Stats {
		return map[string]cache.Stats{
			first:  {Hits: 1, Size: 1},
			second: {Hits: 2, Size: 2},
		}
	})

	body := scrape(t, reporter)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+first+`",result="hit"} 1`)
	expectLine(t, body, `whodis_cache_requests_total{cache="`+second+`",result="hit"} 2`)
}

func TestCreatingSeveralReportersDoesNotPanic(t *testing.T) {
	for range 3 {
		scrape(t, NewReporter(func() map[string]cache.Stats { return nil }))
	}
}
