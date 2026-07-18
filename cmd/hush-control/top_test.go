package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// topFleet wires a fake agent to a control mux so a request to
// /api/machines/{host}/top exercises the real proxy path end to end.
func topFleet(t *testing.T, agent http.Handler) (http.Handler, *agentStore) {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "nas", Addr: srv.URL, IP: "100.71.4.2"}})
	mux, _ := buildMux(store, muxDiscoverer(store), "")
	return mux, store
}

func TestProxyTopRelaysSnapshot(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/top" {
			t.Errorf("agent got path %q, want /top", r.URL.Path)
		}
		json.NewEncoder(w).Encode(vitals.TopSnapshot{
			Host:    "nas",
			CPU:     23,
			Cores:   []int{40, 10},
			Running: 137,
			Procs:   []vitals.Process{{PID: 42, User: "hush", Command: "hush-agent", CPU: 12.5, Mem: 1.2}},
		})
	})
	mux, _ := topFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/top", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got vitals.TopSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.CPU != 23 || len(got.Cores) != 2 || got.Running != 137 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if len(got.Procs) != 1 || got.Procs[0].Command != "hush-agent" || got.Procs[0].CPU != 12.5 {
		t.Fatalf("unexpected procs: %+v", got.Procs)
	}
}

// TestProxyTopPreservesStatus guards the "old agent" path: a 404 from the agent
// (no /top handler) must pass through so the console can say "not supported".
func TestProxyTopPreservesStatus(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux, _ := topFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/top", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (agent's status must pass through)", rec.Code)
	}
}

func TestProxyTopUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mux, _ := topFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/top", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown machine", rec.Code)
	}
}
