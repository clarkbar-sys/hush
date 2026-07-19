package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func statusAgent(t *testing.T, code int, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backup-status" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(code)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestCollectBackupStatusPassesBackupsThrough(t *testing.T) {
	srv := statusAgent(t, http.StatusOK,
		`[{"name":"jaassh-nas","ok":true,"repository":"rest:http://nas:8000/jaassh/"}]`)

	got := collectBackupStatus(srv.Client(), []Agent{{Name: "jaassh", Addr: srv.URL}})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if !got[0].Reachable || got[0].Host != "jaassh" {
		t.Fatalf("got %+v, want reachable jaassh", got[0])
	}
	if len(got[0].Backups) != 1 {
		t.Fatalf("backups len = %d, want 1", len(got[0].Backups))
	}
	// Raw passthrough: a field the control plane has never heard of must still
	// reach the console.
	var b map[string]any
	if err := json.Unmarshal(got[0].Backups[0], &b); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if b["repository"] != "rest:http://nas:8000/jaassh/" {
		t.Fatalf("passthrough lost fields: %v", b)
	}
}

func TestCollectBackupStatusMarksUnreachable(t *testing.T) {
	// A closed port stands in for a powered-off box.
	srv := statusAgent(t, http.StatusOK, `[]`)
	addr := srv.URL
	srv.Close()

	client := &http.Client{Timeout: 500 * time.Millisecond}
	got := collectBackupStatus(client, []Agent{{Name: "nas", Addr: addr}})
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	// It must still appear. A backup console that silently omits a machine is
	// worse than one that admits it cannot tell.
	if got[0].Reachable {
		t.Fatalf("want unreachable, got %+v", got[0])
	}
	if got[0].Host != "nas" {
		t.Fatalf("host = %q, want nas", got[0].Host)
	}
	if got[0].Backups == nil {
		t.Fatal("Backups must be an empty array, never null")
	}
}

func TestCollectBackupStatusOlderAgentIsReachableWithNoBackups(t *testing.T) {
	// An agent predating /backup-status 404s. During a rollout that is
	// "nothing to report", not a broken machine.
	srv := statusAgent(t, http.StatusOK, `[]`)
	old := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(old.Close)

	got := collectBackupStatus(srv.Client(), []Agent{{Name: "old-box", Addr: old.URL}})
	if !got[0].Reachable {
		t.Fatalf("a 404 should read as reachable-but-empty: %+v", got[0])
	}
	if len(got[0].Backups) != 0 {
		t.Fatalf("backups = %v, want none", got[0].Backups)
	}
}

func TestCollectBackupStatusPreservesAgentOrder(t *testing.T) {
	a := statusAgent(t, http.StatusOK, `[]`)
	b := statusAgent(t, http.StatusOK, `[]`)
	c := statusAgent(t, http.StatusOK, `[]`)

	// Concurrent fan-out must not reshuffle the list between loads.
	got := collectBackupStatus(a.Client(), []Agent{
		{Name: "alpha", Addr: a.URL},
		{Name: "bravo", Addr: b.URL},
		{Name: "charlie", Addr: c.URL},
	})
	want := []string{"alpha", "bravo", "charlie"}
	for i, w := range want {
		if got[i].Host != w {
			t.Fatalf("order[%d] = %q, want %q", i, got[i].Host, w)
		}
	}
}

func TestHandleBackupStatusServesArrayAndRejectsPost(t *testing.T) {
	srv := statusAgent(t, http.StatusOK, `[]`)
	agents := func() []Agent { return []Agent{{Name: "jaassh", Addr: srv.URL}} }
	h := handleBackupStatus(srv.Client(), agents)

	rr := httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodGet, "/api/backup-status", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var out []hostBackupStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(out) != 1 || out[0].Host != "jaassh" {
		t.Fatalf("got %+v", out)
	}

	rr = httptest.NewRecorder()
	h(rr, httptest.NewRequest(http.MethodPost, "/api/backup-status", nil))
	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST status = %d, want 405", rr.Code)
	}
}
