package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// jobsFleet wires a fake agent behind a control mux so a request to
// /api/machines/{host}/jobs exercises the real proxy path end to end, the way
// browseFleet does for /browse.
func jobsFleet(t *testing.T, agent http.Handler) (http.Handler, *agentStore) {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "nas", Addr: srv.URL, IP: "100.71.4.2"}})
	mux := buildMux(store, muxDiscoverer(store), "")
	return mux, store
}

func TestProxyJobsRelaysList(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/jobs" {
			t.Errorf("agent got path %q, want /jobs", r.URL.Path)
		}
		if r.Method != http.MethodGet {
			t.Errorf("agent got method %q, want GET", r.Method)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "nightly-abc123", "name": "nightly-backup", "schedule": "0 3 * * *", "cmd": "restic backup /srv"},
		})
	})
	mux, _ := jobsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/jobs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "nightly-backup" {
		t.Fatalf("unexpected jobs: %+v", got)
	}
}

func TestProxyJobsForwardsCreate(t *testing.T) {
	var gotBody string
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("agent got method %q, want POST", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"nightly-abc123","name":"nightly-backup"}`))
	})
	mux, _ := jobsFleet(t, agent)

	body := `{"name":"nightly-backup","schedule":"0 3 * * *","cmd":"restic backup /srv"}`
	req := httptest.NewRequest(http.MethodPost, "/api/machines/nas/jobs", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !strings.Contains(gotBody, "nightly-backup") || !strings.Contains(gotBody, "0 3 * * *") {
		t.Fatalf("agent did not receive the create body verbatim: %q", gotBody)
	}
}

func TestProxyJobsRelaysDisabled(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "jobs disabled", http.StatusForbidden)
	})
	mux, _ := jobsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/jobs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (jobs disabled must pass through)", rec.Code)
	}
}

func TestProxyJobDeleteForwards(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete {
			t.Errorf("agent got method %q, want DELETE", r.Method)
		}
		if r.URL.Path != "/jobs/nightly-abc123" {
			t.Errorf("agent got path %q, want /jobs/nightly-abc123", r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux, _ := jobsFleet(t, agent)

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/nas/jobs/nightly-abc123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestProxyJobsUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("agent should not be reached for an unknown machine")
	})
	mux, _ := jobsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/jobs", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestProxyJobsRejectsBadMethod(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("agent should not be reached for a rejected method")
	})
	mux, _ := jobsFleet(t, agent)

	req := httptest.NewRequest(http.MethodPut, "/api/machines/nas/jobs", strings.NewReader("{}"))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}
