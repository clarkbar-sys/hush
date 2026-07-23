package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// layoutResp mirrors the {"order": [...]} envelope the launcher endpoint speaks.
type layoutResp struct {
	Order []string `json:"order"`
}

// getLayout drives GET /api/launcher/layout and returns the decoded order.
func getLayout(t *testing.T, mux http.Handler) layoutResp {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/launcher/layout", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got layoutResp
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode layout: %v", err)
	}
	return got
}

// putLayout drives PUT /api/launcher/layout with the given order and returns the
// recorder so the caller can assert on status and body.
func putLayout(t *testing.T, mux http.Handler, order []string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string][]string{"order": order})
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPut, "/api/launcher/layout", bytes.NewReader(body)))
	return rr
}

// TestLauncherLayoutDefaultsEmpty: with nothing saved, GET returns an empty
// order (never null) so the console falls back to its baked-in default.
func TestLauncherLayoutDefaultsEmpty(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	got := getLayout(t, mux)
	if got.Order == nil {
		t.Fatal("order is null, want []")
	}
	if len(got.Order) != 0 {
		t.Fatalf("order = %v, want empty", got.Order)
	}
}

// TestLauncherLayoutRoundTrips: a PUT order comes back verbatim from a later GET
// and is persisted to launcher.json beside the fleet config.
func TestLauncherLayoutRoundTrips(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	want := []string{"payphone", "fleet", "plug", "github", "tally"}
	if rr := putLayout(t, mux, want); rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}

	got := getLayout(t, mux)
	if len(got.Order) != len(want) {
		t.Fatalf("order = %v, want %v", got.Order, want)
	}
	for i := range want {
		if got.Order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q", i, got.Order[i], want[i])
		}
	}

	// The order must land on disk beside fleet.json so it survives a restart.
	onDisk := filepath.Join(filepath.Dir(store.path), "launcher.json")
	b, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read launcher.json: %v", err)
	}
	var persisted []string
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatalf("parse launcher.json: %v", err)
	}
	if len(persisted) != len(want) || persisted[0] != "payphone" {
		t.Fatalf("persisted = %v, want %v", persisted, want)
	}
}

// TestLauncherLayoutRejectsBadOrders: malformed ids, duplicates, and oversized
// lists are 400s and never touch the store.
func TestLauncherLayoutRejectsBadOrders(t *testing.T) {
	tooMany := make([]string, maxLauncherTiles+1)
	for i := range tooMany {
		tooMany[i] = "tile" + string(rune('a'+i%26)) + string(rune('a'+i/26))
	}
	cases := map[string][]string{
		"duplicate":      {"fleet", "fleet"},
		"empty id":       {"fleet", ""},
		"uppercase":      {"Fleet"},
		"path traversal": {"../secret"},
		"spaces":         {"my tile"},
		"too many":       tooMany,
	}
	for name, order := range cases {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t, nil)
			mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
			if rr := putLayout(t, mux, order); rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
			}
			// A rejected PUT must not have written anything.
			if got := getLayout(t, mux); len(got.Order) != 0 {
				t.Fatalf("order = %v after rejected PUT, want empty", got.Order)
			}
		})
	}
}

// TestLauncherLayoutMethodNotAllowed: only GET and PUT are wired.
func TestLauncherLayoutMethodNotAllowed(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/launcher/layout", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
