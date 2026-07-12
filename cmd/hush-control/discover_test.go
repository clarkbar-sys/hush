package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// fakeLister is a peerLister that returns a fixed set of peers (or an error),
// so discovery can be tested without a real tailnet.
type fakeLister struct {
	peers []discoveredPeer
	err   error
}

func (f fakeLister) Peers(context.Context) ([]discoveredPeer, error) {
	return f.peers, f.err
}

// probeFunc builds an agentProbe from a map of addr -> result, standing in for
// testAgent without any network round-trip. Unknown addresses probe as
// unreachable, mirroring a node that isn't running hush-agent.
func probeFunc(byAddr map[string]testAgentResult) agentProbe {
	return func(addr string) testAgentResult {
		if r, ok := byAddr[addr]; ok {
			return r
		}
		return testAgentResult{Error: "unreachable"}
	}
}

func TestDiscoverUnavailableWithoutLister(t *testing.T) {
	got := discoverCandidates(context.Background(), nil, probeFunc(nil), nil)
	if got.Available {
		t.Fatalf("discover with nil lister: Available = true, want false")
	}
	if len(got.Candidates) != 0 {
		t.Fatalf("discover with nil lister: %d candidates, want 0", len(got.Candidates))
	}
}

func TestDiscoverListerError(t *testing.T) {
	lister := fakeLister{err: errors.New("tailscaled not responding")}
	got := discoverCandidates(context.Background(), lister, probeFunc(nil), nil)
	if !got.Available {
		t.Fatal("discover with a status error: Available = false, want true")
	}
	if got.Error == "" {
		t.Fatal("discover with a status error: Error is empty, want the failure surfaced")
	}
}

func TestDiscoverFiltersPeers(t *testing.T) {
	lister := fakeLister{peers: []discoveredPeer{
		{Host: "beacon", IP: "100.71.6.4", OS: "linux", Online: true},  // running hush-agent, addable
		{Host: "nas", IP: "100.71.4.2", OS: "linux", Online: true},     // already in the fleet
		{Host: "printer", IP: "100.71.9.9", OS: "linux", Online: true}, // online but not a hush-agent
		{Host: "laptop", IP: "100.71.1.1", OS: "macOS", Online: false}, // offline, skipped
	}}
	probe := probeFunc(map[string]testAgentResult{
		"http://100.71.6.4:8765": {OK: true, Host: "beacon", OS: "Debian 12", LatencyMs: 4},
		"http://100.71.4.2:8765": {OK: true, Host: "nas", OS: "Debian 12"},
	})
	fleet := []Agent{{Name: "nas", Addr: "http://100.71.4.2:8765", IP: "100.71.4.2"}}

	got := discoverCandidates(context.Background(), lister, probe, fleet)
	if !got.Available {
		t.Fatal("Available = false, want true")
	}
	if len(got.Candidates) != 1 {
		t.Fatalf("got %d candidates, want 1 (only beacon): %+v", len(got.Candidates), got.Candidates)
	}
	c := got.Candidates[0]
	if c.Name != "beacon" || c.IP != "100.71.6.4" || c.Addr != "http://100.71.6.4:8765" {
		t.Fatalf("candidate = %+v, want beacon at 100.71.6.4", c)
	}
	if c.OS != "Debian 12" {
		t.Fatalf("candidate OS = %q, want the agent-reported OS", c.OS)
	}
}

func TestDiscoverFallsBackToPeerName(t *testing.T) {
	// A hush-agent that reports an empty Host should fall back to the tailnet
	// hostname (with the trailing DNS dot trimmed).
	lister := fakeLister{peers: []discoveredPeer{
		{Host: "forge.", IP: "100.71.2.5", OS: "linux", Online: true},
	}}
	probe := probeFunc(map[string]testAgentResult{
		"http://100.71.2.5:8765": {OK: true, Host: "", OS: ""},
	})
	got := discoverCandidates(context.Background(), lister, probe, nil)
	if len(got.Candidates) != 1 || got.Candidates[0].Name != "forge" {
		t.Fatalf("candidates = %+v, want a single 'forge' from the peer name", got.Candidates)
	}
	if got.Candidates[0].OS != "linux" {
		t.Fatalf("candidate OS = %q, want the peer OS as fallback", got.Candidates[0].OS)
	}
}

func TestAPIDiscoverEndpoint(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(vitals.Snapshot{Host: "beacon", OS: "Debian 12"})
	}))
	defer agent.Close()

	// A lister reporting the test agent (probed as running hush-agent) yields one
	// candidate through the endpoint's cold-cache live scan.
	host := hostFromAddr(agent.URL)
	store := newTestStore(t, nil)
	disco := &discoverySource{}
	disco.set(fakeLister{peers: []discoveredPeer{{Host: "beacon", IP: host, OS: "linux", Online: true}}})
	probe := probeFunc(map[string]testAgentResult{
		"http://" + host + ":" + discoverPort: {OK: true, Host: "beacon", OS: "Debian 12"},
	})
	mux := buildMux(store, newDiscoverer(disco, probe, store.Snapshot), "")

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/discover", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got discoverResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.Available || got.Error != "" {
		t.Fatalf("discover result = %+v, want available with no error", got)
	}
	if len(got.Candidates) != 1 || got.Candidates[0].Name != "beacon" {
		t.Fatalf("candidates = %+v, want a single 'beacon'", got.Candidates)
	}
}

// muxDiscoverer builds a discoverer for buildMux in tests that don't exercise
// discovery: LAN mode (no lister) with a probe that never reaches anything.
func muxDiscoverer(store *agentStore) *discoverer {
	return newDiscoverer(&discoverySource{}, func(string) testAgentResult {
		return testAgentResult{Error: "unreachable"}
	}, store.Snapshot)
}

func TestAPIDiscoverRejectsPOST(t *testing.T) {
	store := newTestStore(t, nil)
	mux := buildMux(store, muxDiscoverer(store), "")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/discover", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST /api/discover: status = %d, want 405", rr.Code)
	}
}

func TestAPIDiscoverUnavailableInLANMode(t *testing.T) {
	// No lister set (LAN mode): the endpoint responds, but reports discovery
	// unavailable rather than erroring.
	store := newTestStore(t, nil)
	mux := buildMux(store, muxDiscoverer(store), "")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/discover", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got discoverResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Available {
		t.Fatalf("LAN-mode discover: Available = true, want false")
	}
}

func TestDiscovererCachesAndRescans(t *testing.T) {
	// A lister whose peer set changes between scans, to prove the endpoint serves
	// the cache until an explicit rescan refreshes it.
	lister := &mutableLister{peers: []discoveredPeer{
		{Host: "beacon", IP: "100.71.6.4", OS: "linux", Online: true},
	}}
	disco := &discoverySource{}
	disco.set(lister)
	probe := probeFunc(map[string]testAgentResult{
		"http://100.71.6.4:8765": {OK: true, Host: "beacon"},
		"http://100.71.2.5:8765": {OK: true, Host: "forge"},
	})
	store := newTestStore(t, nil)
	d := newDiscoverer(disco, probe, store.Snapshot)

	// Warm the cache with the first scan (what run() does at startup).
	if got := d.scan(context.Background()); len(got.Candidates) != 1 {
		t.Fatalf("first scan: %d candidates, want 1", len(got.Candidates))
	}
	mux := buildMux(store, d, "")

	// A new agent appears, but a plain GET still serves the cached single result.
	lister.set([]discoveredPeer{
		{Host: "beacon", IP: "100.71.6.4", OS: "linux", Online: true},
		{Host: "forge", IP: "100.71.2.5", OS: "linux", Online: true},
	})
	if got := discoverGET(t, mux, "/api/discover"); len(got.Candidates) != 1 {
		t.Fatalf("cached GET: %d candidates, want 1 (cache not yet refreshed)", len(got.Candidates))
	}

	// An explicit rescan re-probes and now sees both.
	if got := discoverGET(t, mux, "/api/discover?rescan=1"); len(got.Candidates) != 2 {
		t.Fatalf("rescan GET: %d candidates, want 2", len(got.Candidates))
	}
	// ...and the refreshed result is now what a plain GET serves.
	if got := discoverGET(t, mux, "/api/discover"); len(got.Candidates) != 2 {
		t.Fatalf("post-rescan cached GET: %d candidates, want 2", len(got.Candidates))
	}
}

// mutableLister is a peerLister whose peer set can change between scans, for the
// caching/rescan test.
type mutableLister struct {
	mu    sync.RWMutex
	peers []discoveredPeer
}

func (m *mutableLister) set(p []discoveredPeer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers = p
}

func (m *mutableLister) Peers(context.Context) ([]discoveredPeer, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.peers, nil
}

func discoverGET(t *testing.T, mux http.Handler, path string) discoverResult {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET %s: status = %d", path, rr.Code)
	}
	var got discoverResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode %s: %v", path, err)
	}
	return got
}
