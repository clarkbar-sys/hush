package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sessionsResp mirrors the {"sessions": [...]} envelope the list endpoint speaks.
type sessionsResp struct {
	Sessions []chatSession `json:"sessions"`
}

// listSessions drives GET /api/payphone/sessions and returns the decoded list.
func listSessions(t *testing.T, mux http.Handler) sessionsResp {
	t.Helper()
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/payphone/sessions", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET status = %d, body = %s", rr.Code, rr.Body.String())
	}
	var got sessionsResp
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode sessions: %v", err)
	}
	return got
}

// putSession drives PUT /api/payphone/sessions/{id} with a raw JSON body and
// returns the recorder so the caller can assert on status and body.
func putSession(t *testing.T, mux http.Handler, id, body string) *httptest.ResponseRecorder {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPut, "/api/payphone/sessions/"+id, strings.NewReader(body))
	mux.ServeHTTP(rr, req)
	return rr
}

// putSessionJSON marshals s and PUTs it under its own id.
func putSessionJSON(t *testing.T, mux http.Handler, s chatSession) *httptest.ResponseRecorder {
	t.Helper()
	b, _ := json.Marshal(s)
	return putSession(t, mux, s.ID, string(b))
}

// TestPayphoneSessionsDefaultEmpty: with nothing saved, GET returns an empty
// list (never null) so the buddy list simply shows no active chats.
func TestPayphoneSessionsDefaultEmpty(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	got := listSessions(t, mux)
	if got.Sessions == nil {
		t.Fatal("sessions is null, want []")
	}
	if len(got.Sessions) != 0 {
		t.Fatalf("sessions = %v, want empty", got.Sessions)
	}
}

// TestPayphoneSessionRoundTrips: a PUT session comes back from a later GET and is
// persisted to payphone.json beside the fleet config.
func TestPayphoneSessionRoundTrips(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	want := chatSession{
		ID: "s-abc123", Host: "nas", Model: "llama-3", Kind: "openai",
		Title: "weather please", Started: 1000, Updated: 2000,
		Messages: []chatMessage{
			{Role: "user", Text: "hi"},
			{Role: "assistant", Text: "hello there"},
		},
	}
	if rr := putSessionJSON(t, mux, want); rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}

	got := listSessions(t, mux)
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %v, want 1", got.Sessions)
	}
	s := got.Sessions[0]
	if s.ID != want.ID || s.Host != want.Host || s.Model != want.Model || len(s.Messages) != 2 {
		t.Fatalf("session = %+v, want %+v", s, want)
	}

	onDisk := filepath.Join(filepath.Dir(store.path), "payphone.json")
	b, err := os.ReadFile(onDisk)
	if err != nil {
		t.Fatalf("read payphone.json: %v", err)
	}
	var persisted []chatSession
	if err := json.Unmarshal(b, &persisted); err != nil {
		t.Fatalf("parse payphone.json: %v", err)
	}
	if len(persisted) != 1 || persisted[0].ID != want.ID {
		t.Fatalf("persisted = %v, want one session %q", persisted, want.ID)
	}
}

// TestPayphoneSessionUpsertPreservesStarted: a second PUT for the same id updates
// the transcript in place and keeps the original Started even when omitted, so a
// growing chat keeps its age and doesn't multiply into duplicate rows.
func TestPayphoneSessionUpsertPreservesStarted(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	first := chatSession{ID: "s-1", Host: "nas", Model: "m", Started: 500, Updated: 500,
		Messages: []chatMessage{{Role: "user", Text: "hi"}}}
	if rr := putSessionJSON(t, mux, first); rr.Code != http.StatusOK {
		t.Fatalf("first PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}
	// Second PUT omits Started (0) and adds a turn.
	second := chatSession{ID: "s-1", Host: "nas", Model: "m", Updated: 900,
		Messages: []chatMessage{{Role: "user", Text: "hi"}, {Role: "assistant", Text: "yo"}}}
	if rr := putSessionJSON(t, mux, second); rr.Code != http.StatusOK {
		t.Fatalf("second PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}

	got := listSessions(t, mux)
	if len(got.Sessions) != 1 {
		t.Fatalf("sessions = %v, want a single upserted session", got.Sessions)
	}
	s := got.Sessions[0]
	if s.Started != 500 {
		t.Fatalf("Started = %d, want 500 preserved across the upsert", s.Started)
	}
	if len(s.Messages) != 2 {
		t.Fatalf("Messages = %d, want 2 after the upsert", len(s.Messages))
	}
}

// TestPayphoneSessionDelete: DELETE forgets a session and answers 404 for an
// unknown id.
func TestPayphoneSessionDelete(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	putSessionJSON(t, mux, chatSession{ID: "s-1", Host: "nas", Model: "m", Updated: 1})

	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/payphone/sessions/s-1", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("DELETE status = %d, want 204 (body: %s)", rr.Code, rr.Body.String())
	}
	if got := listSessions(t, mux); len(got.Sessions) != 0 {
		t.Fatalf("sessions = %v after delete, want empty", got.Sessions)
	}

	rr = httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/api/payphone/sessions/ghost", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown status = %d, want 404", rr.Code)
	}
}

// TestPayphoneSessionRejectsBad: a mismatched url/body id, a bad slug, a missing
// host, and an invalid role are all 400s that never touch the store.
func TestPayphoneSessionRejectsBad(t *testing.T) {
	cases := map[string]struct{ id, body string }{
		"id mismatch":   {"s-1", `{"id":"s-2","host":"nas","model":"m"}`},
		"bad slug":      {"Bad_Slug", `{"host":"nas","model":"m"}`},
		"missing host":  {"s-1", `{"model":"m"}`},
		"missing model": {"s-1", `{"host":"nas"}`},
		"bad role":      {"s-1", `{"host":"nas","model":"m","messages":[{"role":"system","text":"x"}]}`},
		"not json":      {"s-1", `not json`},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			store := newTestStore(t, nil)
			mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
			if rr := putSession(t, mux, c.id, c.body); rr.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, want 400 (body: %s)", rr.Code, rr.Body.String())
			}
			if got := listSessions(t, mux); len(got.Sessions) != 0 {
				t.Fatalf("sessions = %v after a rejected PUT, want empty", got.Sessions)
			}
		})
	}
}

// TestPayphoneSessionFillsBodyID: a body with no id inherits the id from the URL,
// so the client can PUT the transcript without repeating the id in the payload.
func TestPayphoneSessionFillsBodyID(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	if rr := putSession(t, mux, "s-9", `{"host":"nas","model":"m","updated":5}`); rr.Code != http.StatusOK {
		t.Fatalf("PUT status = %d, body = %s", rr.Code, rr.Body.String())
	}
	got := listSessions(t, mux)
	if len(got.Sessions) != 1 || got.Sessions[0].ID != "s-9" {
		t.Fatalf("sessions = %v, want one session with id s-9", got.Sessions)
	}
}

// TestPayphoneSessionsPrune: past maxSessions the oldest chats fall off, keeping
// the newest maxSessions by last activity.
func TestPayphoneSessionsPrune(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")

	for i := 0; i < maxSessions+10; i++ {
		s := chatSession{ID: fmt.Sprintf("s-%03d", i), Host: "nas", Model: "m", Updated: int64(i)}
		if rr := putSessionJSON(t, mux, s); rr.Code != http.StatusOK {
			t.Fatalf("PUT %d status = %d", i, rr.Code)
		}
	}
	got := listSessions(t, mux)
	if len(got.Sessions) != maxSessions {
		t.Fatalf("sessions = %d, want capped at %d", len(got.Sessions), maxSessions)
	}
	// Newest (highest Updated) survives; the oldest were pruned.
	if got.Sessions[0].Updated != int64(maxSessions+9) {
		t.Fatalf("newest Updated = %d, want %d", got.Sessions[0].Updated, maxSessions+9)
	}
	for _, s := range got.Sessions {
		if s.Updated < 10 {
			t.Fatalf("session %q with Updated %d should have been pruned", s.ID, s.Updated)
		}
	}
}

// TestPayphoneSessionsListMethod: only GET is wired on the collection route.
func TestPayphoneSessionsListMethod(t *testing.T) {
	store := newTestStore(t, nil)
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
	rr := httptest.NewRecorder()
	mux.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/api/payphone/sessions", bytes.NewReader(nil)))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
