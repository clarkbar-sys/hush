package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// newTestStore builds an agentStore backed by a fleet.json under a fresh
// temp dir, so each test gets its own file and never touches the repo's own
// fleet.json / fleet.example.json.
func newTestStore(t *testing.T, initial []Agent) *agentStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "fleet.json")
	return newAgentStore(path, initial)
}

func TestAgentStoreAddPersists(t *testing.T) {
	store := newTestStore(t, []Agent{{Name: "nas", Addr: "http://100.71.4.2:8765", IP: "100.71.4.2"}})

	added, err := store.Add(Agent{Name: "beacon", Addr: "http://100.71.6.4:8765", IP: "100.71.6.4"})
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if added.Name != "beacon" {
		t.Fatalf("added.Name = %q, want beacon", added.Name)
	}

	if got := store.Snapshot(); len(got) != 2 {
		t.Fatalf("Snapshot() has %d agents, want 2", len(got))
	}

	b, err := os.ReadFile(store.path)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	var onDisk []Agent
	if err := json.Unmarshal(b, &onDisk); err != nil {
		t.Fatalf("parse persisted config: %v", err)
	}
	if len(onDisk) != 2 || onDisk[1].Addr != "http://100.71.6.4:8765" {
		t.Fatalf("persisted config = %+v, want the new agent appended", onDisk)
	}
}

func TestAgentStoreRejectsDuplicateAddr(t *testing.T) {
	store := newTestStore(t, []Agent{{Name: "nas", Addr: "http://100.71.4.2:8765"}})

	if _, err := store.Add(Agent{Name: "nas-again", Addr: "http://100.71.4.2:8765"}); err == nil {
		t.Fatal("expected an error adding a duplicate address, got nil")
	}
	if got := store.Snapshot(); len(got) != 1 {
		t.Fatalf("Snapshot() has %d agents after rejected add, want 1", len(got))
	}
}

func TestNormalizeAndHostFromAddr(t *testing.T) {
	cases := []struct{ in, wantAddr, wantHost string }{
		{"100.71.6.4:8765", "http://100.71.6.4:8765", "100.71.6.4"},
		{"  100.71.6.4:8765  ", "http://100.71.6.4:8765", "100.71.6.4"},
		{"http://100.71.6.4:8765", "http://100.71.6.4:8765", "100.71.6.4"},
		{"https://beacon.tailnet-1234.ts.net:8765", "https://beacon.tailnet-1234.ts.net:8765", "beacon.tailnet-1234.ts.net"},
		{"", "", ""},
	}
	for _, tc := range cases {
		addr := normalizeAddr(tc.in)
		if addr != tc.wantAddr {
			t.Errorf("normalizeAddr(%q) = %q, want %q", tc.in, addr, tc.wantAddr)
		}
		if host := hostFromAddr(addr); host != tc.wantHost {
			t.Errorf("hostFromAddr(%q) = %q, want %q", addr, host, tc.wantHost)
		}
	}
}

func TestTestAgentSuccess(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(vitals.Snapshot{Host: "beacon", OS: "Debian 12"})
	}))
	defer agent.Close()

	got := testAgent(agent.Client(), agent.URL)
	if !got.OK || got.Host != "beacon" || got.OS != "Debian 12" {
		t.Fatalf("testAgent() = %+v, want ok with host/os populated", got)
	}
}

func TestTestAgentUnreachable(t *testing.T) {
	got := testAgent(&http.Client{}, "http://127.0.0.1:1") // nothing listens on port 1
	if got.OK || got.Error == "" {
		t.Fatalf("testAgent() = %+v, want a failure with an error message", got)
	}
}

func TestTestAgentEmptyAddr(t *testing.T) {
	got := testAgent(&http.Client{}, "  ")
	if got.OK || got.Error == "" {
		t.Fatalf("testAgent(empty) = %+v, want a failure asking for an address", got)
	}
}

func TestAPIAgentsAddAndReject(t *testing.T) {
	store := newTestStore(t, nil)
	mux := buildMux(store, muxDiscoverer(store), "")

	post := func(body string) *httptest.ResponseRecorder {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(body))
		mux.ServeHTTP(rr, req)
		return rr
	}

	rr := post(`{"name":"beacon","addr":"100.71.6.4:8765","role":""}`)
	if rr.Code != http.StatusCreated {
		t.Fatalf("first add: status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var added Agent
	if err := json.Unmarshal(rr.Body.Bytes(), &added); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if added.Addr != "http://100.71.6.4:8765" || added.IP != "100.71.6.4" {
		t.Fatalf("added agent = %+v, want normalized addr and derived ip", added)
	}

	// Adding the same address again should be rejected, not silently duplicated.
	rr = post(`{"name":"beacon-dup","addr":"100.71.6.4:8765"}`)
	if rr.Code != http.StatusConflict {
		t.Fatalf("duplicate add: status = %d, want 409", rr.Code)
	}
	if got := store.Snapshot(); len(got) != 1 {
		t.Fatalf("fleet has %d agents after rejected duplicate, want 1", len(got))
	}

	// Missing addr is a client error, not a panic or silent no-op.
	rr = post(`{"name":"no-addr"}`)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing addr: status = %d, want 400", rr.Code)
	}
}

// TestAPIAgentsSaveFailureReturns500 makes sure a persistence failure (e.g.
// the config directory isn't writable) surfaces as a 500, not the 409 used
// for a genuine duplicate-address rejection — the two are different
// problems and the client needs to tell them apart. Pointed at a config
// path whose parent directory doesn't exist, so the write fails the same
// way it would against a read-only filesystem, but without depending on
// permission checks the test might run past as root.
func TestAPIAgentsSaveFailureReturns500(t *testing.T) {
	store := newAgentStore(filepath.Join(t.TempDir(), "nonexistent", "fleet.json"), nil)
	mux := buildMux(store, muxDiscoverer(store), "")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"addr":"100.71.6.4:8765"}`))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusInternalServerError {
		t.Fatalf("save failure: status = %d, body = %s, want 500", rr.Code, rr.Body.String())
	}
	if got := store.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet has %d agents after a failed save, want 0", len(got))
	}
}

func TestAPIAgentsRejectsGET(t *testing.T) {
	store := newTestStore(t, nil)
	mux := buildMux(store, muxDiscoverer(store), "")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/agents", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET /api/agents: status = %d, want 405", rr.Code)
	}
}

func TestAPIAgentsTestEndpoint(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(vitals.Snapshot{Host: "beacon", OS: "Debian 12"})
	}))
	defer agent.Close()

	store := newTestStore(t, nil)
	mux := buildMux(store, muxDiscoverer(store), "")

	u, _ := url.Parse(agent.URL)
	body, _ := json.Marshal(map[string]string{"addr": u.Host})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/agents/test", bytes.NewReader(body))
	mux.ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got testAgentResult
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if !got.OK || got.Host != "beacon" {
		t.Fatalf("test result = %+v, want ok with host populated", got)
	}

	// The test endpoint must not add the probed agent to the fleet.
	if got := store.Snapshot(); len(got) != 0 {
		t.Fatalf("fleet has %d agents after a test-only probe, want 0", len(got))
	}
}
