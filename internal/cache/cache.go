// Package cache provides a small in-process TTL cache that coalesces concurrent loads.
package cache

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

// DefaultLoadTimeout bounds a load when Options.LoadTimeout is not set.
const DefaultLoadTimeout = time.Minute

const (
	// DefaultMaxConcurrentLoads bounds simultaneous loads when Options.MaxConcurrentLoads is not set.
	DefaultMaxConcurrentLoads = 16
	// DefaultMaxQueuedLoads bounds loads waiting for a slot when Options.MaxQueuedLoads is not set.
	DefaultMaxQueuedLoads = 64
)

// ErrOverloaded is returned when too many distinct keys are loading and the wait queue is full,
// or a load waited LoadTimeout for a slot. It is never cached.
var ErrOverloaded = errors.New("cache: too many concurrent loads")

type Options[V any] struct {
	// TTL is how long a successfully loaded value is served. Must be positive.
	TTL time.Duration
	// NegativeTTL is how long a "known absent" result is served. Zero disables negative caching.
	NegativeTTL time.Duration
	// MaxEntries bounds memory use. Must be positive.
	MaxEntries int
	// LoadTimeout is the deadline of a single load, which runs detached from its callers. Defaults to
	// DefaultLoadTimeout. It must cover every upstream request a loader makes, such as pagination.
	LoadTimeout time.Duration
	// MaxConcurrentLoads bounds loads running at once across distinct keys, since they keep running
	// after their callers disconnect. Coalesced callers of one key share a slot. Defaults to
	// DefaultMaxConcurrentLoads.
	MaxConcurrentLoads int
	// MaxQueuedLoads bounds loads waiting for a slot. Beyond it, new keys fail fast with
	// ErrOverloaded. A queued load also gives up after LoadTimeout. Defaults to DefaultMaxQueuedLoads.
	MaxQueuedLoads int
	// Clone is applied to every value handed out, so callers can mutate results without
	// corrupting the cache. Required when V is a slice, map or pointer.
	Clone func(V) V
	// IsNegativeError marks loader errors (e.g. upstream 404) that are cached for NegativeTTL.
	// All other errors are never cached.
	IsNegativeError func(error) bool
	// IsNegativeValue marks successfully loaded values (e.g. an empty list) that are cached
	// for NegativeTTL instead of TTL.
	IsNegativeValue func(V) bool
	// Now is injectable for tests. Defaults to time.Now.
	Now func() time.Time
}

type Stats struct {
	Hits         int64
	NegativeHits int64
	Misses       int64
	// Loads counts loader invocations. Misses minus Loads is the number of coalesced or late-served callers.
	Loads     int64
	Evictions int64
	// Rejected counts loads refused with ErrOverloaded.
	Rejected int64
	Size     int
}

type entry[V any] struct {
	value    V
	err      error
	negative bool
	expires  time.Time
}

type Cache[V any] struct {
	opts    Options[V]
	mu      sync.Mutex
	entries map[string]entry[V]
	group   singleflight.Group
	slots   chan struct{}
	queued  atomic.Int64

	hits, negativeHits, misses, loads, evictions, rejected atomic.Int64
}

func New[V any](opts Options[V]) *Cache[V] {
	if opts.TTL <= 0 {
		panic("cache: TTL must be positive")
	}
	if opts.MaxEntries <= 0 {
		panic("cache: MaxEntries must be positive")
	}
	if opts.LoadTimeout <= 0 {
		opts.LoadTimeout = DefaultLoadTimeout
	}
	if opts.MaxConcurrentLoads <= 0 {
		opts.MaxConcurrentLoads = DefaultMaxConcurrentLoads
	}
	if opts.MaxQueuedLoads <= 0 {
		opts.MaxQueuedLoads = DefaultMaxQueuedLoads
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	return &Cache[V]{
		opts:    opts,
		entries: make(map[string]entry[V]),
		slots:   make(chan struct{}, opts.MaxConcurrentLoads),
	}
}

// GetOrLoad returns the cached value for key, or runs load once for all concurrent callers of the same key.
//
// The loader runs detached from the cancellation of the calling context (values such as trace
// spans are kept), so one caller giving up does not fail the others. Each caller still stops
// waiting when its own context is done. Distinct keys load concurrently up to MaxConcurrentLoads. The detached load gets its own LoadTimeout, so an
// unresponsive upstream cannot hold a key's flight open indefinitely.
func (c *Cache[V]) GetOrLoad(ctx context.Context, key string, load func(context.Context) (V, error)) (V, error) {
	if e, ok := c.lookup(key); ok {
		if e.negative {
			c.negativeHits.Add(1)
		} else {
			c.hits.Add(1)
		}
		return c.result(e)
	}
	c.misses.Add(1)

	ch := c.group.DoChan(key, func() (any, error) {
		return c.fill(ctx, key, load), nil
	})
	select {
	case res := <-ch:
		return c.result(res.Val.(entry[V]))
	case <-ctx.Done():
		var zero V
		return zero, ctx.Err()
	}
}

func (c *Cache[V]) Stats() Stats {
	c.mu.Lock()
	size := len(c.entries)
	c.mu.Unlock()
	return Stats{
		Hits:         c.hits.Load(),
		NegativeHits: c.negativeHits.Load(),
		Misses:       c.misses.Load(),
		Loads:        c.loads.Load(),
		Evictions:    c.evictions.Load(),
		Rejected:     c.rejected.Load(),
		Size:         size,
	}
}

func (c *Cache[V]) fill(ctx context.Context, key string, load func(context.Context) (V, error)) entry[V] {
	// Another flight may have populated the key between our miss and this flight starting.
	if e, ok := c.lookup(key); ok {
		return e
	}
	if !c.acquire() {
		c.rejected.Add(1)
		return entry[V]{err: ErrOverloaded}
	}
	defer func() { <-c.slots }()
	c.loads.Add(1)
	loadCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.opts.LoadTimeout)
	defer cancel()
	v, err := safeLoad(loadCtx, load)
	if err == nil && c.opts.Clone != nil {
		// The cache must not share memory with whatever the loader still holds.
		v = c.opts.Clone(v)
	}

	e := entry[V]{value: v, err: err}
	ttl := c.opts.TTL
	switch {
	case err != nil:
		if c.opts.IsNegativeError == nil || !c.opts.IsNegativeError(err) {
			return e
		}
		e.negative, ttl = true, c.opts.NegativeTTL
	case c.opts.IsNegativeValue != nil && c.opts.IsNegativeValue(v):
		e.negative, ttl = true, c.opts.NegativeTTL
	}
	if ttl <= 0 {
		return e
	}
	e.expires = c.opts.Now().Add(ttl)
	c.store(key, e)
	return e
}

// acquire takes a load slot, queueing behind running loads. The queue is bounded in both size and time.
func (c *Cache[V]) acquire() bool {
	select {
	case c.slots <- struct{}{}:
		return true
	default:
	}
	if c.queued.Add(1) > int64(c.opts.MaxQueuedLoads) {
		c.queued.Add(-1)
		return false
	}
	defer c.queued.Add(-1)
	timer := time.NewTimer(c.opts.LoadTimeout)
	defer timer.Stop()
	select {
	case c.slots <- struct{}{}:
		return true
	case <-timer.C:
		return false
	}
}

func (c *Cache[V]) result(e entry[V]) (V, error) {
	if e.err != nil {
		var zero V
		return zero, e.err
	}
	if c.opts.Clone != nil {
		return c.opts.Clone(e.value), nil
	}
	return e.value, nil
}

func (c *Cache[V]) lookup(key string) (entry[V], bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[key]
	if !ok {
		return e, false
	}
	if !c.opts.Now().Before(e.expires) {
		delete(c.entries, key)
		return entry[V]{}, false
	}
	return e, true
}

func (c *Cache[V]) store(key string, e entry[V]) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.opts.MaxEntries {
		c.makeRoom()
	}
	c.entries[key] = e
}

// makeRoom drops expired entries first, then the entry closest to expiry. The scan is O(n)
// but only runs on insert into a full cache, and n is bounded by MaxEntries.
func (c *Cache[V]) makeRoom() {
	now := c.opts.Now()
	for k, e := range c.entries {
		if !now.Before(e.expires) {
			delete(c.entries, k)
		}
	}
	if len(c.entries) < c.opts.MaxEntries {
		return
	}
	var victim string
	var soonest time.Time
	for k, e := range c.entries {
		if soonest.IsZero() || e.expires.Before(soonest) {
			victim, soonest = k, e.expires
		}
	}
	delete(c.entries, victim)
	c.evictions.Add(1)
}

// safeLoad turns loader panics into errors: singleflight re-panics on a goroutine we do not
// control, which would take down the whole process.
func safeLoad[V any](ctx context.Context, load func(context.Context) (V, error)) (v V, err error) {
	defer func() {
		if r := recover(); r != nil {
			var zero V
			v, err = zero, fmt.Errorf("cache loader panicked: %v", r)
		}
	}()
	return load(ctx)
}
