package main

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// backupsFleet wires a fake agent behind a control mux so a request to
// /api/machines/{host}/backups exercises the real proxy path end to end, the way
// jobsFleet does for /jobs.
func backupsFleet(t *testing.T, agent http.Handler) (http.Handler, *agentStore) {
	t.Helper()
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "nas", Addr: srv.URL, IP: "100.71.4.2"}})
	mux, _ := buildMux(store, muxDiscoverer(store), "")
	return mux, store
}

func TestProxyBackupsRelaysList(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/backups" || r.Method != http.MethodGet {
			t.Errorf("agent got %s %q, want GET /backups", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"id": "abc123", "name": "debian-root", "repo": "rest:http://nas/homelab", "paths": []string{"/"}},
		})
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/backups", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got) != 1 || got[0]["name"] != "debian-root" {
		t.Fatalf("unexpected backups: %+v", got)
	}
}

func TestProxyBackupsForwardsCreate(t *testing.T) {
	var gotBody string
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("agent got method %q, want POST", r.Method)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"id":"abc123","name":"debian-root"}`))
	})
	mux, _ := backupsFleet(t, agent)

	// The password rides the create body through to the agent verbatim — but
	// (see below) must not reach the audit log.
	body := `{"name":"debian-root","repo":"rest:http://nas/homelab","password":"s3cret","paths":["/"],"oneFileSystem":true}`
	req := httptest.NewRequest(http.MethodPost, "/api/machines/nas/backups", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201", rec.Code)
	}
	if !strings.Contains(gotBody, "s3cret") || !strings.Contains(gotBody, "rest:http://nas/homelab") {
		t.Fatalf("agent did not receive the create body verbatim: %q", gotBody)
	}
}

func TestProxyBackupsRelaysDisabled(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backups disabled", http.StatusForbidden)
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/backups", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403 (backups disabled must pass through)", rec.Code)
	}
}

func TestProxyBackupDeleteForwards(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || r.URL.Path != "/backups/abc123" {
			t.Errorf("agent got %s %q, want DELETE /backups/abc123", r.Method, r.URL.Path)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodDelete, "/api/machines/nas/backups/abc123", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestProxyBackupRunStreams(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/backups/abc123/run" {
			t.Errorf("agent got %s %q, want POST /backups/abc123/run", r.Method, r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"kind\":\"start\",\"pid\":42}\n\n")
		io.WriteString(w, "data: {\"kind\":\"exit\",\"ms\":10}\n\n")
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodPost, "/api/machines/nas/backups/abc123/run", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "event-stream") {
		t.Fatalf("content-type = %q, want event-stream", ct)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"start"`) || !strings.Contains(rec.Body.String(), `"kind":"exit"`) {
		t.Fatalf("run did not stream the SSE frames: %q", rec.Body.String())
	}
}

func TestProxyBackupRestoreStreams(t *testing.T) {
	var gotBody string
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/backups/abc123/restore" {
			t.Errorf("agent got %s %q, want POST /backups/abc123/restore", r.Method, r.URL.Path)
		}
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"kind\":\"start\",\"pid\":9}\n\n")
		io.WriteString(w, "data: {\"kind\":\"exit\",\"ms\":5}\n\n")
	})
	mux, _ := backupsFleet(t, agent)

	body := `{"snapshot":"aaaa1111","target":"/var/tmp/hush-restore"}`
	req := httptest.NewRequest(http.MethodPost, "/api/machines/nas/backups/abc123/restore", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(gotBody, "aaaa1111") || !strings.Contains(gotBody, "/var/tmp/hush-restore") {
		t.Fatalf("agent did not receive the restore body verbatim: %q", gotBody)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"start"`) || !strings.Contains(rec.Body.String(), `"kind":"exit"`) {
		t.Fatalf("restore did not stream the SSE frames: %q", rec.Body.String())
	}
}

func TestBackupRestorePreview(t *testing.T) {
	got := backupRestorePreview([]byte(`{"snapshot":"aaaa1111","target":"/var/tmp/r"}`))
	if !strings.Contains(got, "aaaa1111") || !strings.Contains(got, "/var/tmp/r") {
		t.Fatalf("preview should name the snapshot and target: %q", got)
	}
	if def := backupRestorePreview([]byte(`{"target":"/x"}`)); !strings.Contains(def, "latest") {
		t.Fatalf("empty snapshot should read as latest: %q", def)
	}
}

func TestProxyBackupSnapshotsForwards(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/backups/abc123/snapshots" {
			t.Errorf("agent got %s %q, want GET /backups/abc123/snapshots", r.Method, r.URL.Path)
		}
		json.NewEncoder(w).Encode([]map[string]any{
			{"short_id": "aaaa1111", "time": "2026-07-18T03:00:00Z", "hostname": "debian"},
		})
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/backups/abc123/snapshots", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "aaaa1111") {
		t.Fatalf("snapshots not relayed: %q", rec.Body.String())
	}
}

func TestProxyBackupsUnknownMachine(t *testing.T) {
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("agent should not be reached for an unknown machine")
	})
	mux, _ := backupsFleet(t, agent)

	req := httptest.NewRequest(http.MethodGet, "/api/machines/ghost/backups", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestBackupCreatePreviewOmitsPassword(t *testing.T) {
	// The audit-log preview must never include the repo password, even though it
	// rides the same create body.
	body := []byte(`{"name":"debian-root","repo":"rest:http://nas/homelab","password":"s3cret","paths":["/"]}`)
	got := backupCreatePreview(body)
	if strings.Contains(got, "s3cret") {
		t.Fatalf("preview leaked the password: %q", got)
	}
	if !strings.Contains(got, "debian-root") || !strings.Contains(got, "rest:http://nas/homelab") {
		t.Fatalf("preview should name the backup and repo: %q", got)
	}
}
