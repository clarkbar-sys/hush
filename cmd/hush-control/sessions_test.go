package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clarkbar-sys/hush/internal/sessions"
)

// sessionsFleet wires a fake agent to a control mux so a request to
// /api/machines/{host}/sessions exercises the real proxy path end to end.
func sessionsFleet(t *testing.T, agent http.Handler) (http.Handler, *agentStore) {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "citadel", Addr: srv.URL, IP: "100.71.8.9"}})
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
	return mux, store
}

func TestProxySessionsRelaysSnapshot(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sessions" {
			t.Errorf("agent got path %q, want /sessions", r.URL.Path)
		}
		json.NewEncoder(w).Encode(sessions.Snapshot{
			Host:  "citadel",
			Match: []string{"opencode", "claude"},
			Sessions: []sessions.Session{
				{PID: 4821, User: "josh", Tool: "opencode", Cmd: "opencode", Uptime: 300},
			},
		})
	})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/citadel/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got sessions.Snapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Host != "citadel" || len(got.Sessions) != 1 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if s := got.Sessions[0]; s.PID != 4821 || s.User != "josh" || s.Tool != "opencode" {
		t.Fatalf("unexpected session: %+v", s)
	}
}

// A pre-/sessions agent answers 404; it must pass through so the console can
// say the feature isn't supported rather than showing an empty section.
func TestProxySessionsPreservesStatus(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/citadel/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (agent's status must pass through)", rec.Code)
	}
}

func TestProxySessionsUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/sessions", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown machine", rec.Code)
	}
}
