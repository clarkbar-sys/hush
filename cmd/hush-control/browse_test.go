package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clarkbar-sys/hush/internal/browse"
)

// browseFleet wires a fake agent to a control mux so a request to
// /api/machines/{host}/browse exercises the real proxy path end to end.
func browseFleet(t *testing.T, agent http.Handler) (http.Handler, *agentStore) {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "nas", Addr: srv.URL, IP: "100.71.4.2"}})
	mux := buildMux(store, muxDiscoverer(store), "")
	return mux, store
}

func TestProxyBrowseRelaysListing(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/browse" {
			t.Errorf("agent got path %q, want /browse", r.URL.Path)
		}
		if got := r.URL.Query().Get("path"); got != "/mnt/tank" {
			t.Errorf("agent got path query %q, want /mnt/tank", got)
		}
		json.NewEncoder(w).Encode(browse.Listing{
			Path:    "/mnt/tank",
			Entries: []browse.Entry{{Name: "media", IsDir: true}},
		})
	})
	mux, _ := browseFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/browse?path=/mnt/tank", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got browse.Listing
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Path != "/mnt/tank" || len(got.Entries) != 1 || got.Entries[0].Name != "media" {
		t.Fatalf("unexpected listing: %+v", got)
	}
}

func TestProxyBrowsePreservesStatus(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "permission denied", http.StatusForbidden)
	})
	mux, _ := browseFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/browse?path=/root", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (agent's status must pass through)", rec.Code)
	}
}

func TestProxyBrowseUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mux, _ := browseFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/browse?path=/", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for unknown machine", rec.Code)
	}
}

func TestProxyFileForwardsRangeAndRelays(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/file" {
			t.Errorf("agent path = %q, want /file", r.URL.Path)
		}
		if r.Header.Get("Range") != "bytes=0-3" {
			t.Errorf("Range not forwarded: %q", r.Header.Get("Range"))
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Accept-Ranges", "bytes")
		w.WriteHeader(http.StatusPartialContent)
		w.Write([]byte("data"))
	})
	mux, _ := browseFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/file?path=/srv/clip.mp4", nil)
	req.Header.Set("Range", "bytes=0-3")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusPartialContent {
		t.Fatalf("status = %d, want 206 (agent's status must pass through)", rec.Code)
	}
	if rec.Header().Get("Content-Type") != "video/mp4" {
		t.Errorf("Content-Type not relayed: %q", rec.Header().Get("Content-Type"))
	}
	if rec.Body.String() != "data" {
		t.Errorf("body = %q", rec.Body.String())
	}
}

func TestProxyFileUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	mux, _ := browseFleet(t, agent)
	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/file?path=/x", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestStoreFindByNameThenIP(t *testing.T) {
	store := newTestStore(t, []Agent{
		{Name: "nas", Addr: "http://a", IP: "100.0.0.1"},
		{Name: "", Addr: "http://b", IP: "100.0.0.2"},
	})
	if a, ok := store.find("nas"); !ok || a.Addr != "http://a" {
		t.Errorf("find by name: %+v ok=%v", a, ok)
	}
	if a, ok := store.find("100.0.0.2"); !ok || a.Addr != "http://b" {
		t.Errorf("find by ip: %+v ok=%v", a, ok)
	}
	if _, ok := store.find("nope"); ok {
		t.Error("find should miss unknown host")
	}
}
