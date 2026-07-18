package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// TestAPIFleetColdCacheFallsBackToLiveScan mirrors TestAPIDiscoverEndpoint: the
// very first request, before the background loop has ever run, still gets a
// live answer rather than an empty cache.
func TestAPIFleetColdCacheFallsBackToLiveScan(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(vitals.Snapshot{Host: "beacon", OS: "Debian 12", Status: "good", CPU: 7})
	}))
	defer agent.Close()

	store := newTestStore(t, []Agent{{Name: "beacon", Addr: agent.URL}})
	mux, _ := buildMux(store, muxDiscoverer(store), "")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got []Machine
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].ID != "beacon" || got[0].CPU != 7 {
		t.Fatalf("fleet = %+v, want a single reachable beacon", got)
	}
}

// TestAPIFleetServesCacheWithoutReprobing proves that once the cache is warm,
// /api/fleet serves the cached snapshot directly rather than re-fetching every
// agent's /vitals on each request — the fix for a slow or offline machine
// otherwise stalling every poll (and every reachable machine's data along
// with it) behind its own timeout.
func TestAPIFleetServesCacheWithoutReprobing(t *testing.T) {
	var hits int32
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		json.NewEncoder(w).Encode(vitals.Snapshot{Host: "beacon", OS: "Debian 12", Status: "good"})
	}))
	defer agent.Close()

	store := newTestStore(t, []Agent{{Name: "beacon", Addr: agent.URL}})
	mux, fc := buildMux(store, muxDiscoverer(store), "")

	// Warm the cache with one scan (what run() does at startup).
	if got := fc.scan(context.Background()); len(got) != 1 {
		t.Fatalf("first scan: %d machines, want 1", len(got))
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("agent hit %d times after warming scan, want 1", got)
	}

	// Several plain GETs should all be served from the cache: the agent must
	// not see any additional requests.
	for i := 0; i < 3; i++ {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET /api/fleet: status = %d", rr.Code)
		}
		var got []Machine
		if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
			t.Fatalf("decode response: %v", err)
		}
		if len(got) != 1 || got[0].ID != "beacon" || !got[0].Online {
			t.Fatalf("fleet = %+v, want the cached beacon entry", got)
		}
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("agent hit %d times after 3 cached GETs, want 1 (served from cache)", got)
	}
}
