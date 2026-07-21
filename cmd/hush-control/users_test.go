package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clarkbar-sys/hush/internal/sessions"
)

func TestProxyUsersRelaysSnapshot(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/users" {
			t.Errorf("agent got path %q, want /users", r.URL.Path)
		}
		json.NewEncoder(w).Encode(sessions.UsersSnapshot{
			Host: "citadel",
			Users: []sessions.SystemUser{
				{Name: "josh", UID: 1000, Home: "/home/josh", Shell: "/bin/bash"},
			},
		})
	})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/citadel/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got sessions.UsersSnapshot
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Host != "citadel" || len(got.Users) != 1 {
		t.Fatalf("unexpected snapshot: %+v", got)
	}
	if u := got.Users[0]; u.Name != "josh" || u.UID != 1000 {
		t.Fatalf("unexpected users[0]: %+v", u)
	}
}

// A pre-/users agent answers 404; it must pass through so the console can say
// the feature isn't supported rather than showing an empty section.
func TestProxyUsersPreservesStatus(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", http.StatusNotFound)
	})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/citadel/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 (agent's status must pass through)", rec.Code)
	}
}

func TestProxyUsersUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mux, _ := sessionsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/users", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown machine", rec.Code)
	}
}
