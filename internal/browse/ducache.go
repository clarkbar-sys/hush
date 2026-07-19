package browse

import (
	"context"
	"path/filepath"
	"sync"
	"time"
)

// timeNow is the clock DuCache reads. It's a package var so tests can freeze it
// and exercise TTL/retention without sleeping; production leaves it as
// time.Now.
var timeNow = time.Now

// defaultDuCacheKeys bounds how many directories a cache holds when the caller
// doesn't specify. A browsing session touches a few dozen paths at most, so
// this is generous headroom, not a limit anyone should hit in practice.
const defaultDuCacheKeys = 512

// DuCache memoizes Du results per directory so reopening the treemap — or
// drilling back to a directory already sized this session — is instant instead
// of re-walking the tree. Each entry remembers when its walk ran; an entry
// older than ttl is recomputed on the next Get, and a forced refresh recomputes
// regardless of age. The cache holds at most maxKeys directories, evicting the
// least-recently-accessed when full, so a long session can't grow it without
// bound.
//
// A DuCache is safe for concurrent use. The zero value is not usable — call
// NewDuCache.
type DuCache struct {
	ttl     time.Duration
	maxKeys int

	mu    sync.Mutex
	items map[string]*duEntry
}

// duEntry is one cached directory sizing. computedAt is when the walk that
// produced listing ran (what freshness the UI shows); lastAccess is the last
// time a user Get looked it up, which drives both LRU eviction and which paths
// the background refresher keeps warm. A background refresh updates computedAt
// but deliberately leaves lastAccess untouched, so a path visited once ages out
// instead of being re-walked forever.
type duEntry struct {
	listing    DuListing
	computedAt time.Time
	lastAccess time.Time
}

// NewDuCache returns a cache that treats results younger than ttl as fresh and
// holds at most maxKeys directories. A ttl <= 0 disables reuse (every Get
// recomputes); a maxKeys <= 0 falls back to a sane default.
func NewDuCache(ttl time.Duration, maxKeys int) *DuCache {
	if maxKeys <= 0 {
		maxKeys = defaultDuCacheKeys
	}
	return &DuCache{ttl: ttl, maxKeys: maxKeys, items: map[string]*duEntry{}}
}

// Get returns a sized listing for path. It reuses a cached result when one is
// younger than the cache's ttl and refresh is false; otherwise it walks the
// tree via Du and stores the result. The returned listing carries ComputedAt
// (when the walk that produced it actually ran) and Cached (whether this call
// reused an earlier walk rather than walking now), so callers can show how
// stale the number is. Errors from Du are returned unwrapped and never cached.
func (c *DuCache) Get(ctx context.Context, path string, refresh bool) (DuListing, error) {
	key := cacheKey(path)
	if !refresh {
		if l, ok := c.lookup(key); ok {
			return l, nil
		}
	}
	l, err := Du(ctx, path)
	if err != nil {
		return DuListing{}, err
	}
	l = c.insert(key, l)
	l.Cached = false
	return l, nil
}

// lookup returns a fresh cached listing for key, bumping its access time, or
// reports a miss when the key is absent or its entry has aged past ttl.
func (c *DuCache) lookup(key string) (DuListing, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.items[key]
	if !ok {
		return DuListing{}, false
	}
	now := timeNow()
	if c.ttl <= 0 || now.Sub(e.computedAt) > c.ttl {
		return DuListing{}, false
	}
	e.lastAccess = now
	l := e.listing
	l.Cached = true
	return l, true
}

// insert records a freshly walked listing for a user-driven Get: it stamps
// ComputedAt, marks the entry accessed now, evicts if that pushed the cache
// over maxKeys, and returns the stamped listing for the caller to hand back.
func (c *DuCache) insert(key string, l DuListing) DuListing {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := timeNow()
	l.ComputedAt = now.UTC().Format(time.RFC3339)
	c.items[key] = &duEntry{listing: l, computedAt: now, lastAccess: now}
	c.evictLocked()
	return l
}

// restore records a background re-walk: it refreshes the listing and
// computedAt but preserves the entry's existing lastAccess, so keeping a number
// warm doesn't count as someone caring about the path. If the entry was evicted
// between selection and here, it's re-added as a fresh access.
func (c *DuCache) restore(key string, l DuListing) {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := timeNow()
	l.ComputedAt = now.UTC().Format(time.RFC3339)
	if e, ok := c.items[key]; ok {
		e.listing = l
		e.computedAt = now
		return
	}
	c.items[key] = &duEntry{listing: l, computedAt: now, lastAccess: now}
	c.evictLocked()
}

// evictLocked drops least-recently-accessed entries until the cache is within
// maxKeys. The caller must hold c.mu.
func (c *DuCache) evictLocked() {
	for len(c.items) > c.maxKeys {
		var oldestKey string
		var oldest time.Time
		for k, e := range c.items {
			if oldestKey == "" || e.lastAccess.Before(oldest) {
				oldestKey, oldest = k, e.lastAccess
			}
		}
		delete(c.items, oldestKey)
	}
}

// StartRefresher re-sizes recently-viewed cached directories every interval so
// a treemap opened later shows a recent number without paying for a cold walk.
// It only ever touches paths already in the cache — directories someone
// actually browsed to — so an idle agent does no work and no root walk ever
// runs that a user didn't ask for. Paths not accessed within retain are dropped
// rather than refreshed, keeping the working set to what's still of interest.
// Each re-walk is bounded by walkTimeout, and walks run one at a time so the
// refresher never storms the disk. It blocks until ctx is cancelled; run it in
// its own goroutine. An interval <= 0 returns immediately (refreshing off).
func (c *DuCache) StartRefresher(ctx context.Context, interval, walkTimeout, retain time.Duration) {
	if interval <= 0 {
		return
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			c.refreshOnce(ctx, walkTimeout, retain)
		}
	}
}

// refreshOnce re-walks every path worth keeping warm, one at a time, stopping
// early if ctx is cancelled mid-sweep.
func (c *DuCache) refreshOnce(ctx context.Context, walkTimeout, retain time.Duration) {
	for _, key := range c.refreshable(retain) {
		if ctx.Err() != nil {
			return
		}
		wctx, cancel := context.WithTimeout(ctx, walkTimeout)
		l, err := Du(wctx, key)
		cancel()
		if err == nil {
			c.restore(key, l) // a failed walk leaves the last good sizing in place
		}
	}
}

// refreshable returns the keys worth re-warming and evicts the rest: any entry
// not accessed within retain is dropped here instead of refreshed, so one-off
// visits age out rather than being walked on every tick. A retain <= 0 keeps
// every current key.
func (c *DuCache) refreshable(retain time.Duration) []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	cutoff := timeNow().Add(-retain)
	keys := make([]string, 0, len(c.items))
	for k, e := range c.items {
		if retain > 0 && e.lastAccess.Before(cutoff) {
			delete(c.items, k)
			continue
		}
		keys = append(keys, k)
	}
	return keys
}

// cacheKey normalizes a path the way Du does — empty means root, otherwise
// cleaned — so "/srv", "/srv/" and "/srv/." all resolve to one cache entry.
// A non-absolute path yields a key that Du will reject, so it's never stored.
func cacheKey(path string) string {
	if path == "" {
		return "/"
	}
	return filepath.Clean(path)
}
