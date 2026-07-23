package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The launcher ("wu") is the front door: it must be reachable at the pretty
// /wu URL as well as the raw /wu.html asset, and both must serve the same
// data-driven page that lists the fleet's apps.
func TestLauncherRoute(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	for _, path := range []string{"/wu", "/wu.html"} {
		rr := httptest.NewRecorder()
		mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("GET %s: status = %d, want 200", path, rr.Code)
		}
		body := rr.Body.String()
		for _, want := range []string{"front door", "const APPS", "payphone"} {
			if !strings.Contains(body, want) {
				t.Errorf("GET %s: body missing %q", path, want)
			}
		}
	}

	// The console still owns / — the launcher is additive, not a takeover.
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /: status = %d, want 200", rr.Code)
	}
}
