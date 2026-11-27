package cache

import (
	"context"
	"errors"
	"reflect"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

var errNotFound = errors.New("not found")

type fakeClock struct {
	mu  sync.Mutex
	now time.Time
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func stringsCache(clock *fakeClock, mutate func(*Options[[]string])) *Cache[[]string] {
	opts := Options[[]string]{
		TTL:             10 * time.Minute,
		NegativeTTL:     time.Minute,
		MaxEntries:      100,
		Clone:           slices.Clone[[]string],
		IsNegativeError: func(err error) bool { return errors.Is(err, errNotFound) },
		IsNegativeValue: func(v []string) bool { return len(v) == 0 },
		Now:             clock.Now,
	}
	if mutate != nil {
		mutate(&opts)
	}
	return New(opts)
}

func countingLoader(calls *atomic.Int32, v []string, err error) func(context.Context) ([]string, error) {
	return func(context.Context) ([]string, error) {
		calls.Add(1)
		return v, err
	}
}

func TestValueIsServedUntilTTLExpires(t *testing.T) {
	clock := newFakeClock()
	c := stringsCache(clock, nil)
	var calls atomic.Int32
	load := countingLoader(&calls, []string{"a"}, nil)

	for range 3 {
		if v, err := c.GetOrLoad(context.Background(), "k", load); err != nil || !reflect.DeepEqual(v, []string{"a"}) {
			t.Fatalf("unexpected result %v, %v", v, err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 load within TTL, got %d", calls.Load())
	}

	clock.Advance(10 * time.Minute)
	if _, err := c.GetOrLoad(context.Background(), "k", load); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("expected reload after TTL, got %d loads", calls.Load())
	}
	if s := c.Stats(); s.Hits != 2 || s.Misses != 2 || s.Loads != 2 {
		t.Errorf("unexpected stats %+v", s)
	}
}

func TestConcurrentCallersShareOneLoad(t *testing.T) {
	c := stringsCache(newFakeClock(), nil)
	const callers = 20
	var calls atomic.Int32
	release := make(chan struct{})
	load := func(context.Context) ([]string, error) {
		calls.Add(1)
		<-release
		return []string{"a"}, nil
	}

	var wg sync.WaitGroup
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := c.GetOrLoad(context.Background(), "k", load)
			if err != nil || !reflect.DeepEqual(v, []string{"a"}) {
				t.Errorf("unexpected result %v, %v", v, err)
			}
		}()
	}
	// Nothing is cached until release, so every caller must register as a miss.
	deadline := time.Now().Add(5 * time.Second)
	for c.Stats().Misses < callers {
		if time.Now().After(deadline) {
			t.Fatal("callers did not all arrive")
		}
		time.Sleep(time.Millisecond)
	}
	close(release)
	wg.Wait()

	if calls.Load() != 1 {
		t.Errorf("expected 1 load, got %d", calls.Load())
	}
}

func TestUnexpectedErrorsAreNeverCached(t *testing.T) {
	c := stringsCache(newFakeClock(), nil)
	var calls atomic.Int32
	boom := errors.New("upstream 500")

	for range 2 {
		if _, err := c.GetOrLoad(context.Background(), "k", countingLoader(&calls, nil, boom)); !errors.Is(err, boom) {
			t.Fatalf("expected upstream error, got %v", err)
		}
	}
	if calls.Load() != 2 {
		t.Errorf("expected each call to reach the loader, got %d", calls.Load())
	}
	if c.Stats().Size != 0 {
		t.Error("error result must not be stored")
	}
}

func TestNegativeErrorIsCachedForNegativeTTL(t *testing.T) {
	clock := newFakeClock()
	c := stringsCache(clock, nil)
	var calls atomic.Int32
	load := countingLoader(&calls, nil, errNotFound)

	for range 3 {
		if _, err := c.GetOrLoad(context.Background(), "k", load); !errors.Is(err, errNotFound) {
			t.Fatalf("expected not found, got %v", err)
		}
	}
	if calls.Load() != 1 {
		t.Fatalf("expected 1 load, got %d", calls.Load())
	}
	if c.Stats().NegativeHits != 2 {
		t.Errorf("expected 2 negative hits, got %+v", c.Stats())
	}

	clock.Advance(time.Minute)
	_, _ = c.GetOrLoad(context.Background(), "k", load)
	if calls.Load() != 2 {
		t.Errorf("negative entry should expire after NegativeTTL, got %d loads", calls.Load())
	}
}

func TestEmptyValueUsesNegativeTTL(t *testing.T) {
	clock := newFakeClock()
	c := stringsCache(clock, nil)
	var calls atomic.Int32
	load := countingLoader(&calls, []string{}, nil)

	_, _ = c.GetOrLoad(context.Background(), "k", load)
	clock.Advance(30 * time.Second)
	_, _ = c.GetOrLoad(context.Background(), "k", load)
	if calls.Load() != 1 {
		t.Fatalf("empty value should be cached within NegativeTTL, got %d loads", calls.Load())
	}
	clock.Advance(time.Minute)
	_, _ = c.GetOrLoad(context.Background(), "k", load)
	if calls.Load() != 2 {
		t.Errorf("empty value should expire after NegativeTTL, got %d loads", calls.Load())
	}
}

func TestZeroNegativeTTLDisablesNegativeCaching(t *testing.T) {
	c := stringsCache(newFakeClock(), func(o *Options[[]string]) { o.NegativeTTL = 0 })
	var calls atomic.Int32

	for range 2 {
		_, _ = c.GetOrLoad(context.Background(), "k", countingLoader(&calls, nil, errNotFound))
		_, _ = c.GetOrLoad(context.Background(), "empty", countingLoader(&calls, []string{}, nil))
	}
	if calls.Load() != 4 {
		t.Errorf("expected no negative caching, got %d loads", calls.Load())
	}
}

func TestCallersCannotCorruptCachedValue(t *testing.T) {
	c := stringsCache(newFakeClock(), nil)
	source := []string{"a", "b"}
	load := func(context.Context) ([]string, error) { return source, nil }

	first, _ := c.GetOrLoad(context.Background(), "k", load)
	first[0] = "mutated"
	source[1] = "mutated"

	second, _ := c.GetOrLoad(context.Background(), "k", load)
	if !reflect.DeepEqual(second, []string{"a", "b"}) {
		t.Errorf("cached value was corrupted: %v", second)
	}
}

func TestMaxEntriesDropsExpiredBeforeLiveEntries(t *testing.T) {
	clock := newFakeClock()
	c := stringsCache(clock, func(o *Options[[]string]) { o.MaxEntries = 2 })
	var calls atomic.Int32
	load := countingLoader(&calls, []string{"a"}, nil)
	ctx := context.Background()

	_, _ = c.GetOrLoad(ctx, "old", load)
	clock.Advance(9 * time.Minute)
	_, _ = c.GetOrLoad(ctx, "newer", load)
	clock.Advance(2 * time.Minute) // "old" is now expired, "newer" is live
	_, _ = c.GetOrLoad(ctx, "newest", load)

	if s := c.Stats(); s.Size != 2 || s.Evictions != 0 {
		t.Fatalf("expected the expired entry to be dropped without eviction, got %+v", s)
	}
	calls.Store(0)
	_, _ = c.GetOrLoad(ctx, "newer", load)
	_, _ = c.GetOrLoad(ctx, "newest", load)
	if calls.Load() != 0 {
		t.Errorf("live entries should have survived, got %d loads", calls.Load())
	}
}

func TestMaxEntriesEvictsEntryClosestToExpiry(t *testing.T) {
	clock := newFakeClock()
	c := stringsCache(clock, func(o *Options[[]string]) { o.MaxEntries = 2 })
	var calls atomic.Int32
	load := countingLoader(&calls, []string{"a"}, nil)
	ctx := context.Background()

	_, _ = c.GetOrLoad(ctx, "first", load)
	clock.Advance(time.Minute)
	_, _ = c.GetOrLoad(ctx, "second", load)
	_, _ = c.GetOrLoad(ctx, "third", load)

	if s := c.Stats(); s.Size != 2 || s.Evictions != 1 {
		t.Fatalf("expected one eviction at capacity, got %+v", s)
	}
	calls.Store(0)
	_, _ = c.GetOrLoad(ctx, "second", load)
	_, _ = c.GetOrLoad(ctx, "third", load)
	if calls.Load() != 0 {
		t.Errorf("the oldest entry should have been evicted, got %d loads", calls.Load())
	}
}

func TestWaiterStopsWaitingWhenItsContextIsDone(t *testing.T) {
	c := stringsCache(newFakeClock(), nil)
	release := make(chan struct{})
	started := make(chan struct{})
	load := func(context.Context) ([]string, error) {
		close(started)
		<-release
		return []string{"a"}, nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := c.GetOrLoad(ctx, "k", load)
		done <- err
	}()
	<-started
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}

	close(release)
	deadline := time.Now().Add(5 * time.Second)
	for c.Stats().Size == 0 {
		if time.Now().After(deadline) {
			t.Fatal("load should still complete and be cached after the caller gave up")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestLoaderIsNotCancelledWithCallerButKeepsValues(t *testing.T) {
	type ctxKey struct{}
	c := stringsCache(newFakeClock(), nil)
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "span"))
	cancel()

	loaderRan := make(chan struct{})
	_, _ = c.GetOrLoad(ctx, "k", func(loadCtx context.Context) ([]string, error) {
		defer close(loaderRan)
		if loadCtx.Err() != nil {
			t.Error("loader context must not inherit cancellation")
		}
		if loadCtx.Value(ctxKey{}) != "span" {
			t.Error("loader context must keep caller values")
		}
		return []string{"a"}, nil
	})
	<-loaderRan
}

func TestLoaderPanicBecomesError(t *testing.T) {
	c := stringsCache(newFakeClock(), nil)
	_, err := c.GetOrLoad(context.Background(), "k", func(context.Context) ([]string, error) {
		panic("boom")
	})
	if err == nil {
		t.Fatal("expected an error from a panicking loader")
	}
	if c.Stats().Size != 0 {
		t.Error("panic result must not be stored")
	}
}

func TestStalledLoadIsCutOffByLoadTimeoutAndNotCached(t *testing.T) {
	c := stringsCache(newFakeClock(), func(o *Options[[]string]) { o.LoadTimeout = 20 * time.Millisecond })
	stalled := func(ctx context.Context) ([]string, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	_, err := c.GetOrLoad(context.Background(), "k", stalled)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}

	var calls atomic.Int32
	got, err := c.GetOrLoad(context.Background(), "k", countingLoader(&calls, []string{"a"}, nil))
	if err != nil || !reflect.DeepEqual(got, []string{"a"}) || calls.Load() != 1 {
		t.Fatalf("expected a fresh load after the timeout, got %v, %v, %d calls", got, err, calls.Load())
	}
}

func TestDistinctKeysNeverExceedMaxConcurrentLoads(t *testing.T) {
	const limit, keys = 3, 12
	c := stringsCache(newFakeClock(), func(o *Options[[]string]) {
		o.MaxConcurrentLoads = limit
		o.MaxQueuedLoads = keys
	})
	var running, peak atomic.Int32
	load := func(context.Context) ([]string, error) {
		n := running.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		time.Sleep(10 * time.Millisecond)
		running.Add(-1)
		return []string{"a"}, nil
	}

	var wg sync.WaitGroup
	for i := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.GetOrLoad(context.Background(), string(rune('a'+i)), load); err != nil {
				t.Errorf("queued load should succeed, got %v", err)
			}
		}()
	}
	wg.Wait()
	if peak.Load() > limit {
		t.Errorf("expected at most %d concurrent loads, saw %d", limit, peak.Load())
	}
}

func TestFullQueueRejectsNewKeysWithoutCachingTheFailure(t *testing.T) {
	c := stringsCache(newFakeClock(), func(o *Options[[]string]) {
		o.MaxConcurrentLoads = 1
		o.MaxQueuedLoads = 1
	})
	release := make(chan struct{})
	started := make(chan struct{}, 2)
	blocked := func(context.Context) ([]string, error) {
		started <- struct{}{}
		<-release
		return []string{"a"}, nil
	}
	go c.GetOrLoad(context.Background(), "running", blocked)
	<-started
	go c.GetOrLoad(context.Background(), "queued", blocked)
	deadline := time.Now().Add(5 * time.Second)
	for c.queued.Load() < 1 {
		if time.Now().After(deadline) {
			t.Fatal("second load never queued")
		}
		time.Sleep(time.Millisecond)
	}

	var calls atomic.Int32
	_, err := c.GetOrLoad(context.Background(), "rejected", countingLoader(&calls, []string{"a"}, nil))
	if !errors.Is(err, ErrOverloaded) || calls.Load() != 0 {
		t.Fatalf("expected ErrOverloaded without a load, got %v, %d calls", err, calls.Load())
	}
	if c.Stats().Rejected != 1 {
		t.Errorf("expected 1 rejection, got %+v", c.Stats())
	}

	close(release)
	deadline = time.Now().Add(5 * time.Second)
	for c.Stats().Size < 2 {
		if time.Now().After(deadline) {
			t.Fatal("running loads did not finish")
		}
		time.Sleep(time.Millisecond)
	}
	if _, err := c.GetOrLoad(context.Background(), "rejected", countingLoader(&calls, []string{"a"}, nil)); err != nil || calls.Load() != 1 {
		t.Errorf("rejection must not be cached, got %v, %d calls", err, calls.Load())
	}
}

func TestQueuedLoadGivesUpAfterLoadTimeout(t *testing.T) {
	c := stringsCache(newFakeClock(), func(o *Options[[]string]) {
		o.MaxConcurrentLoads = 1
		o.LoadTimeout = 20 * time.Millisecond
	})
	release := make(chan struct{})
	started := make(chan struct{})
	go c.GetOrLoad(context.Background(), "running", func(context.Context) ([]string, error) {
		close(started)
		<-release
		return []string{"a"}, nil
	})
	<-started
	defer close(release)

	_, err := c.GetOrLoad(context.Background(), "waiting", countingLoader(new(atomic.Int32), []string{"a"}, nil))
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("expected ErrOverloaded after queue timeout, got %v", err)
	}
}
