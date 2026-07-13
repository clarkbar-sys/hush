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

func newTestScheduler(t *testing.T) (*scheduler, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "jobs.json")
	return newScheduler(path), path
}

func TestValidateJob(t *testing.T) {
	cases := []struct {
		name, sched, cmd string
		wantErr          string // substring; "" means success
	}{
		{"", "* * * * *", "echo hi", "name"},
		{"backup", "", "echo hi", "schedule"},
		{"backup", "* * * * *", "", "command"},
		{"backup", "not a cron", "echo hi", "invalid schedule"},
		{"backup", "*/15 * * * *", "echo hi", ""},
		{"nightly", "@daily", "echo hi", ""},
	}
	for _, c := range cases {
		j, err := validateJob(c.name, c.sched, c.cmd)
		if c.wantErr == "" {
			if err != nil {
				t.Errorf("validateJob(%q,%q,%q) = %v, want ok", c.name, c.sched, c.cmd, err)
				continue
			}
			if j.ID == "" || j.CreatedAt == "" {
				t.Errorf("validateJob(%q): id/createdAt not filled: %+v", c.name, j)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), c.wantErr) {
			t.Errorf("validateJob(%q,%q,%q) err = %v, want contains %q", c.name, c.sched, c.cmd, err, c.wantErr)
		}
	}
}

func TestSchedulerAddPersistsAndReloads(t *testing.T) {
	s, path := newTestScheduler(t)
	job, err := validateJob("backup", "* * * * *", "echo hi")
	if err != nil {
		t.Fatalf("validateJob: %v", err)
	}
	if _, err := s.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got := s.List(); len(got) != 1 || got[0].Name != "backup" {
		t.Fatalf("List after Add = %+v", got)
	}

	// A fresh scheduler over the same file must reload the job and register it.
	reloaded := newScheduler(path)
	got := reloaded.List()
	if len(got) != 1 || got[0].ID != job.ID {
		t.Fatalf("reloaded List = %+v", got)
	}
	if _, ok := reloaded.entries[job.ID]; !ok {
		t.Fatalf("reloaded scheduler did not register a cron entry for %s", job.ID)
	}
}

func TestSchedulerDelete(t *testing.T) {
	s, _ := newTestScheduler(t)
	job, _ := validateJob("backup", "* * * * *", "echo hi")
	if _, err := s.Add(job); err != nil {
		t.Fatalf("Add: %v", err)
	}
	removed, err := s.Delete(job.ID)
	if err != nil || !removed {
		t.Fatalf("Delete = removed=%v err=%v", removed, err)
	}
	if _, ok := s.entries[job.ID]; ok {
		t.Fatal("cron entry survived Delete")
	}
	if removed, _ := s.Delete(job.ID); removed {
		t.Fatal("second Delete reported something removed")
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("List after Delete = %+v", got)
	}
}

func TestSchedulerAddRejectsBadScheduleWithoutPersisting(t *testing.T) {
	s, _ := newTestScheduler(t)
	// Bypass validateJob to prove Add itself rolls back a job cron won't accept.
	_, err := s.Add(Job{ID: "x", Name: "bad", Schedule: "nonsense", Cmd: "echo hi"})
	if err == nil {
		t.Fatal("Add accepted an unschedulable job")
	}
	if got := s.List(); len(got) != 0 {
		t.Fatalf("unschedulable job was left persisted: %+v", got)
	}
}

func TestRunRecordsOutcome(t *testing.T) {
	s, _ := newTestScheduler(t)

	ok, _ := validateJob("good", "* * * * *", "true")
	if _, err := s.Add(ok); err != nil {
		t.Fatalf("Add good: %v", err)
	}
	s.run(ok)
	st := s.List()[0].Status
	if st.Runs != 1 || st.LastCode != 0 || st.Running {
		t.Fatalf("after successful run, status = %+v", st)
	}

	bad, _ := validateJob("bad", "* * * * *", "exit 3")
	if _, err := s.Add(bad); err != nil {
		t.Fatalf("Add bad: %v", err)
	}
	s.run(bad)
	for _, v := range s.List() {
		if v.ID == bad.ID {
			if v.Status.LastCode != 3 {
				t.Fatalf("failing run recorded code %d, want 3 (%+v)", v.Status.LastCode, v.Status)
			}
		}
	}
}

func TestNewSchedulerSkipsUnparseablePersistedJob(t *testing.T) {
	path := filepath.Join(t.TempDir(), "jobs.json")
	// Write a job whose schedule no longer parses alongside a valid one.
	content := `[
	  {"id":"a","name":"broken","schedule":"nope","cmd":"echo hi"},
	  {"id":"b","name":"fine","schedule":"* * * * *","cmd":"echo hi"}
	]`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	s := newScheduler(path)
	// Both definitions remain listed (the store is untouched), but only the
	// parseable one gets a live cron entry.
	if got := s.List(); len(got) != 2 {
		t.Fatalf("List = %d jobs, want 2", len(got))
	}
	if _, ok := s.entries["a"]; ok {
		t.Fatal("broken job should not have a cron entry")
	}
	if _, ok := s.entries["b"]; !ok {
		t.Fatal("valid job should have a cron entry")
	}
}

func TestHandleJobsCreateListDelete(t *testing.T) {
	s, _ := newTestScheduler(t)

	// POST creates.
	body := `{"name":"backup","schedule":"*/15 * * * *","cmd":"echo hi"}`
	rec := httptest.NewRecorder()
	s.handleJobs(rec, httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body)))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /jobs = %d, want 201 (%s)", rec.Code, rec.Body.String())
	}
	var created Job
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created job: %v", err)
	}
	if created.ID == "" {
		t.Fatal("created job has no id")
	}

	// GET lists it with a status object.
	rec = httptest.NewRecorder()
	s.handleJobs(rec, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /jobs = %d, want 200", rec.Code)
	}
	var views []jobView
	if err := json.Unmarshal(rec.Body.Bytes(), &views); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(views) != 1 || views[0].ID != created.ID {
		t.Fatalf("GET /jobs = %+v", views)
	}

	// DELETE removes it; a second delete is 404.
	req := httptest.NewRequest(http.MethodDelete, "/jobs/"+created.ID, nil)
	req.SetPathValue("id", created.ID)
	rec = httptest.NewRecorder()
	s.handleJob(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("DELETE /jobs/{id} = %d, want 204 (%s)", rec.Code, rec.Body.String())
	}
	rec = httptest.NewRecorder()
	s.handleJob(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("DELETE unknown = %d, want 404", rec.Code)
	}
}

func TestHandleJobsRejectsBadBody(t *testing.T) {
	s, _ := newTestScheduler(t)
	for _, body := range []string{`not json`, `{"name":"","schedule":"* * * * *","cmd":"x"}`, `{"name":"n","schedule":"bad","cmd":"x"}`} {
		rec := httptest.NewRecorder()
		s.handleJobs(rec, httptest.NewRequest(http.MethodPost, "/jobs", strings.NewReader(body)))
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("POST %q = %d, want 400", body, rec.Code)
		}
	}
}

func TestResolveStateDir(t *testing.T) {
	if got := resolveStateDir("/explicit"); got != "/explicit" {
		t.Fatalf("explicit flag = %q, want /explicit", got)
	}
	t.Setenv("STATE_DIRECTORY", "/run/state/hush:/other")
	if got := resolveStateDir(""); got != "/run/state/hush" {
		t.Fatalf("STATE_DIRECTORY first path = %q, want /run/state/hush", got)
	}
	t.Setenv("STATE_DIRECTORY", "")
	if got := resolveStateDir(""); got != "/var/lib/hush" {
		t.Fatalf("fallback = %q, want /var/lib/hush", got)
	}
}
