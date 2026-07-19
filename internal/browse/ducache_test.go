package browse

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// freezeTime points timeNow at *at so a test can advance the clock by mutating
// at, and restores the real clock when the test ends.
func freezeTime(t *testing.T, at *time.Time) {
	t.Helper()
	prev := timeNow
	timeNow = func() time.Time { return *at }
	t.Cleanup(func() { timeNow = prev })
}

func total(l DuListing) int64 {
	var s int64
	for _, e := range l.Entries {
		s += e.Size
	}
	return s
}

func TestDuCacheServesCachedResult(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "12345") // 5 bytes
	c := NewDuCache(time.Hour, 0)

	first, err := c.Get(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if first.Cached {
		t.Error("first Get reported Cached=true")
	}
	if first.ComputedAt == "" {
		t.Error("ComputedAt not stamped on a fresh walk")
	}
	if total(first) != 5 {
		t.Fatalf("total = %d, want 5", total(first))
	}

	// Grow the tree; a cache hit must ignore the change and report the old size.
	mustWrite(t, filepath.Join(dir, "b.txt"), "1234567890") // +10 bytes
	second, err := c.Get(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !second.Cached {
		t.Error("second Get reported Cached=false — expected a cache hit")
	}
	if total(second) != 5 {
		t.Errorf("cached total = %d, want 5 (the new file must not show)", total(second))
	}
	if second.ComputedAt != first.ComputedAt {
		t.Errorf("cached ComputedAt = %q, want the original walk's %q", second.ComputedAt, first.ComputedAt)
	}
}

func TestDuCacheForceRefreshBypasses(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "12345")
	c := NewDuCache(time.Hour, 0)
	if _, err := c.Get(context.Background(), dir, false); err != nil {
		t.Fatalf("seed Get: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "b.txt"), "1234567890")
	refreshed, err := c.Get(context.Background(), dir, true)
	if err != nil {
		t.Fatalf("Get(refresh): %v", err)
	}
	if refreshed.Cached {
		t.Error("forced refresh reported Cached=true")
	}
	if total(refreshed) != 15 {
		t.Errorf("refreshed total = %d, want 15", total(refreshed))
	}
}

func TestDuCacheTTLExpiryRecomputes(t *testing.T) {
	now := time.Unix(1000, 0)
	freezeTime(t, &now)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "12345")
	c := NewDuCache(time.Minute, 0)
	if _, err := c.Get(context.Background(), dir, false); err != nil {
		t.Fatalf("seed Get: %v", err)
	}

	mustWrite(t, filepath.Join(dir, "b.txt"), "1234567890")
	now = now.Add(2 * time.Minute) // past the 1-minute TTL

	l, err := c.Get(context.Background(), dir, false)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if l.Cached {
		t.Error("entry past its TTL should have been recomputed, not served from cache")
	}
	if total(l) != 15 {
		t.Errorf("total = %d, want 15 after TTL expiry", total(l))
	}
}

func TestDuCacheDoesNotCacheErrors(t *testing.T) {
	c := NewDuCache(time.Hour, 0)
	if _, err := c.Get(context.Background(), "some/relative/path", false); err == nil {
		t.Fatal("expected an error for a relative path")
	}
	if len(c.items) != 0 {
		t.Errorf("error path left %d entries cached, want 0", len(c.items))
	}
}

func TestDuCacheEvictsLeastRecentlyUsed(t *testing.T) {
	now := time.Unix(1000, 0)
	freezeTime(t, &now)

	base := t.TempDir()
	c := NewDuCache(time.Hour, 2)
	dirs := make([]string, 3)
	for i := range dirs {
		d := filepath.Join(base, "d"+pad(i))
		mustMkdir(t, d)
		mustWrite(t, filepath.Join(d, "f"), "x")
		dirs[i] = d
		now = now.Add(time.Minute) // each Get is more recent than the last
		if _, err := c.Get(context.Background(), d, false); err != nil {
			t.Fatalf("Get %s: %v", d, err)
		}
	}

	if len(c.items) != 2 {
		t.Errorf("cache holds %d entries, want the 2-key cap", len(c.items))
	}
	if _, ok := c.items[cacheKey(dirs[0])]; ok {
		t.Error("least-recently-used entry was not evicted")
	}
	if _, ok := c.items[cacheKey(dirs[2])]; !ok {
		t.Error("most-recent entry is missing")
	}
}

func TestDuCacheRefreshWarmsThenRetentionEvicts(t *testing.T) {
	now := time.Unix(1000, 0)
	freezeTime(t, &now)

	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "a.txt"), "12345")
	c := NewDuCache(time.Hour, 0)
	if _, err := c.Get(context.Background(), dir, false); err != nil {
		t.Fatalf("seed Get: %v", err)
	}
	key := cacheKey(dir)
	accessedAt := c.items[key].lastAccess

	// Grow the tree, advance within the retain window, and run one sweep. The
	// entry should be re-walked to the new size, but its lastAccess must stay
	// put — a background refresh doesn't count as someone visiting the path.
	mustWrite(t, filepath.Join(dir, "b.txt"), "1234567890")
	now = now.Add(30 * time.Minute)
	c.refreshOnce(context.Background(), time.Second, time.Hour)

	e := c.items[key]
	if e == nil {
		t.Fatal("entry vanished after a within-retain refresh")
	}
	if total(e.listing) != 15 {
		t.Errorf("refreshed size = %d, want 15", total(e.listing))
	}
	if !e.lastAccess.Equal(accessedAt) {
		t.Errorf("lastAccess = %v, want it preserved at %v across a background refresh", e.lastAccess, accessedAt)
	}

	// Now jump past the retain window; the next sweep should drop it instead of
	// re-walking a path no one has looked at in hours.
	now = now.Add(2 * time.Hour)
	if keys := c.refreshable(time.Hour); len(keys) != 0 {
		t.Errorf("refreshable returned %v, want nothing (entry past retain)", keys)
	}
	if _, ok := c.items[key]; ok {
		t.Error("stale entry past retain was not evicted")
	}
}
