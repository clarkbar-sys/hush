// /api/fleet is polled by the console every 2.5s (see web/index.html's
// poll()). collectFleet already fans out to every agent's /vitals
// concurrently, but the handler used to call it inline and wait for every
// single agent to answer (or hit the 2s per-agent client timeout) before
// writing the response. One powered-off machine therefore held up the whole
// poll — including the data for every reachable machine — by up to 2s, on
// every single cycle, because the console only renders once the full JSON
// array arrives.
//
// fleetCache decouples the two, the same way discoverer already does for
// tailnet scans: a background loop probes the fleet on its own timer and
// caches the result, so /api/fleet always serves the last known snapshot
// immediately. A machine that's down still shows up right away, marked
// unreachable, instead of stalling every other machine's data behind it.
package main

import (
	"context"
	"net/http"
	"sync"
	"time"
)

// fleetPollInterval is how often the background loop re-probes every agent.
// Matched to the console's own poll cadence so the cache is never more than
// one cycle stale.
const fleetPollInterval = 2500 * time.Millisecond

// fleetCache runs collectFleet on a timer and caches the latest result.
type fleetCache struct {
	client *http.Client
	agents func() []Agent
	latest func(ctx context.Context) string

	mu       sync.RWMutex
	cached   []Machine
	hasCache bool
}

// newFleetCache builds a fleetCache over the shared fleet-poll client, the
// current agent list (store.Snapshot), and the latest known release tag
// (versionChecker.status, already cached on its own hour-long TTL).
func newFleetCache(client *http.Client, agents func() []Agent, latest func(ctx context.Context) string) *fleetCache {
	return &fleetCache{client: client, agents: agents, latest: latest}
}

// scan runs one collection pass and refreshes the cache, returning the result.
// It backs both the background timer and an on-demand scan on a cold cache.
func (f *fleetCache) scan(ctx context.Context) []Machine {
	res := collectFleet(f.client, f.agents(), f.latest(ctx))
	f.mu.Lock()
	f.cached, f.hasCache = res, true
	f.mu.Unlock()
	return res
}

// snapshot returns the last cached result. ok is false until the first scan
// completes, letting the handler fall back to a live scan on a cold cache.
func (f *fleetCache) snapshot() (result []Machine, ok bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	return f.cached, f.hasCache
}

// run scans immediately (to warm the cache at startup) and then on every tick
// until ctx is cancelled.
func (f *fleetCache) run(ctx context.Context) {
	f.scan(ctx)
	t := time.NewTicker(fleetPollInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			f.scan(ctx)
		}
	}
}
