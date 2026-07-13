package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewTaskIDUniqueAndSlugged(t *testing.T) {
	a := newTaskID("Restart API")
	b := newTaskID("Restart API")
	if a == b {
		t.Fatalf("ids collided: %q", a)
	}
	if !strings.HasPrefix(a, "restart-api-") {
		t.Errorf("id %q missing slug prefix", a)
	}
}

func TestValidateTask(t *testing.T) {
	inFleet := func(host string) bool { return host == "citadel" || host == "nas" }

	if _, err := validateTask("  ", "citadel", "uptime", inFleet); err == nil {
		t.Error("blank name should be rejected")
	}
	if _, err := validateTask("x", "  ", "uptime", inFleet); err == nil {
		t.Error("blank host should be rejected")
	}
	if _, err := validateTask("x", "citadel", "", inFleet); err == nil {
		t.Error("blank command should be rejected")
	}
	if _, err := validateTask("x", "ghost", "ls", inFleet); err == nil {
		t.Error("unknown host should be rejected")
	}

	tk, err := validateTask("Disk check", "nas", "  df -h  ", inFleet)
	if err != nil {
		t.Fatalf("valid task rejected: %v", err)
	}
	if tk.ID == "" || tk.CreatedAt == "" {
		t.Errorf("id/createdAt not filled: %+v", tk)
	}
	if tk.Cmd != "df -h" {
		t.Errorf("command not trimmed: %q", tk.Cmd)
	}
}

func TestTaskStoreRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	s := newTaskStore(path)
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("fresh store not empty: %d", len(got))
	}

	saved, err := s.Add(Task{ID: "t1", Name: "one", Host: "box", Cmd: "echo hi"})
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	if saved.ID != "t1" {
		t.Fatalf("add returned %+v", saved)
	}

	// A fresh store over the same path must see the persisted task.
	reloaded := newTaskStore(path)
	if got := reloaded.Snapshot(); len(got) != 1 || got[0].ID != "t1" {
		t.Fatalf("reload = %+v", got)
	}
	if _, ok := reloaded.Find("t1"); !ok {
		t.Error("find(t1) missed a persisted task")
	}

	up, found, err := reloaded.Update("t1", "one v2", "box", "echo bye")
	if err != nil || !found {
		t.Fatalf("update = %v, %v", found, err)
	}
	if up.Name != "one v2" || up.Cmd != "echo bye" {
		t.Errorf("update didn't apply: %+v", up)
	}

	removed, err := reloaded.Delete("t1")
	if err != nil || !removed {
		t.Fatalf("delete = %v, %v", removed, err)
	}
	if removed, _ := reloaded.Delete("t1"); removed {
		t.Error("second delete should report nothing removed")
	}
}

func TestLoadTasksCorruptStartsEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tasks.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadTasks(path); len(got) != 0 {
		t.Errorf("corrupt file should start empty, got %d", len(got))
	}
}

func TestTasksHTTPCreateListRunUpdateDelete(t *testing.T) {
	agent := fakeAgent(t, `{"kind":"exit","code":0}`)
	defer agent.Close()

	dir := t.TempDir()
	store := newAgentStore(filepath.Join(dir, "fleet.json"), []Agent{{Name: "box", Addr: agent.URL}})
	mux := buildMux(store, muxDiscoverer(store), "")

	// Create.
	body := `{"name":"Ping","host":"box","cmd":"echo ok"}`
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d: %s", rec.Code, rec.Body.String())
	}
	var created Task
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	if created.ID == "" {
		t.Fatal("created task has no id")
	}

	// Reject an unknown host.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks", strings.NewReader(`{"name":"x","host":"ghost","cmd":"ls"}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("unknown-host create = %d, want 400", rec.Code)
	}

	// List.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/tasks", nil))
	var list []Task
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 {
		t.Fatalf("list len = %d, want 1", len(list))
	}

	// Run streams SSE from the pinned host's /exec.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/"+created.ID+"/run", nil))
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("run content-type = %q", ct)
	}
	if !strings.Contains(rec.Body.String(), `"kind":"exit"`) {
		t.Errorf("run missing exit frame: %q", rec.Body.String())
	}

	// Update in place: same id, new name/host/cmd.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/tasks/"+created.ID, strings.NewReader(`{"name":"Ping v2","host":"box","cmd":"echo two"}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d: %s", rec.Code, rec.Body.String())
	}
	var updated Task
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	if updated.ID != created.ID || updated.CreatedAt != created.CreatedAt {
		t.Errorf("update changed identity: %+v vs %+v", updated, created)
	}
	if updated.Name != "Ping v2" || updated.Cmd != "echo two" {
		t.Errorf("update didn't apply: %+v", updated)
	}

	// Update on an unknown id is a 404.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/api/tasks/nope", strings.NewReader(`{"name":"x","host":"box","cmd":"ls"}`)))
	if rec.Code != http.StatusNotFound {
		t.Errorf("update unknown id = %d, want 404", rec.Code)
	}

	// Delete, then confirm the run 404s.
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodDelete, "/api/tasks/"+created.ID, nil))
	if rec.Code != http.StatusNoContent {
		t.Errorf("delete status = %d, want 204", rec.Code)
	}
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/api/tasks/"+created.ID+"/run", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("run after delete = %d, want 404", rec.Code)
	}
}
