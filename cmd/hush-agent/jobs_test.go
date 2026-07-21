package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// writeJobFile is a helper for the fixtures below.
func writeJobFile(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestReadJobStatusesMissingDirIsEmptyNotError(t *testing.T) {
	// A box that runs no tracked jobs is the normal case. Reporting it as a
	// failure would draw a healthy machine as broken.
	jobs, err := readJobStatuses(filepath.Join(t.TempDir(), "nope"), time.Now())
	if err != nil {
		t.Fatalf("missing dir should not error, got %v", err)
	}
	if len(jobs) != 0 {
		t.Fatalf("want empty, got %d", len(jobs))
	}
	if jobs == nil {
		t.Fatal("want an empty slice, not nil — the console renders without a nil check")
	}
}

func TestReadJobStatusesSortedAndNamedFromFilename(t *testing.T) {
	dir := t.TempDir()
	writeJobFile(t, dir, "zeta.json", `{"name":"zeta","ok":true}`)
	// No name field: the filename has to stand in, or the card is anonymous.
	writeJobFile(t, dir, "alpha.json", `{"ok":true}`)

	jobs, err := readJobStatuses(dir, time.Now())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2 jobs, got %d", len(jobs))
	}
	if jobs[0].Name != "alpha" || jobs[1].Name != "zeta" {
		t.Fatalf("want alpha,zeta in order, got %q,%q", jobs[0].Name, jobs[1].Name)
	}
}

func TestReadJobStatusesSkipsProgressTwin(t *testing.T) {
	// The regression this guards: .progress.json ends in .json, so an
	// unfiltered enumeration gives every running job a phantom second card
	// named "<name>.progress" for exactly as long as it runs.
	dir := t.TempDir()
	writeJobFile(t, dir, "fetch.json", `{"name":"fetch","state":"running"}`)
	writeJobFile(t, dir, "fetch.progress.json", `{"updated":"2026-07-20T12:00:00Z"}`)

	jobs, err := readJobStatuses(dir, time.Now())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(jobs) != 1 {
		t.Fatalf("want exactly 1 job, got %d — progress twin leaked through", len(jobs))
	}
	if jobs[0].Name != "fetch" {
		t.Fatalf("want fetch, got %q", jobs[0].Name)
	}
}

func TestReadJobStatusesUnparseableFileIsSkippedNotFatal(t *testing.T) {
	dir := t.TempDir()
	writeJobFile(t, dir, "good.json", `{"name":"good","ok":true}`)
	writeJobFile(t, dir, "bad.json", `{not json`)

	jobs, err := readJobStatuses(dir, time.Now())
	if err != nil {
		t.Fatalf("one bad file must not fail the read: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != "good" {
		t.Fatalf("want just the good job, got %+v", jobs)
	}
}

func TestFreshProgressIsAttachedToRunningJob(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeJobFile(t, dir, "fetch.json", `{"name":"fetch","state":"running"}`)
	writeJobFile(t, dir, "fetch.progress.json",
		`{"updated":"`+now.Add(-10*time.Second).Format(time.RFC3339)+`","percent_done":0.4}`)

	jobs, err := readJobStatuses(dir, now)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if jobs[0].State != "running" {
		t.Fatalf("want state running, got %q", jobs[0].State)
	}
	if len(jobs[0].Progress) == 0 {
		t.Fatal("fresh sample should be attached")
	}
	// Raw passthrough: a field the agent has never heard of must survive, or
	// every new job field needs a matching agent release.
	var got map[string]any
	if err := json.Unmarshal(jobs[0].Progress, &got); err != nil {
		t.Fatalf("progress is not valid JSON: %v", err)
	}
	if got["percent_done"] != 0.4 {
		t.Fatalf("percent_done did not ride through raw: %v", got["percent_done"])
	}
}

func TestStaleProgressDowngradesToStaleAndWithholdsSample(t *testing.T) {
	// The case this exists for: a job killed outright never runs the cleanup
	// that removes its progress file. Defaulting to fresh would leave a dead
	// job showing a confident percentage forever.
	dir := t.TempDir()
	now := time.Now()
	writeJobFile(t, dir, "fetch.json", `{"name":"fetch","state":"running"}`)
	writeJobFile(t, dir, "fetch.progress.json",
		`{"updated":"`+now.Add(-30*time.Minute).Format(time.RFC3339)+`","percent_done":0.4}`)

	jobs, err := readJobStatuses(dir, now)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if jobs[0].State != "stale" {
		t.Fatalf("want state stale, got %q", jobs[0].State)
	}
	if len(jobs[0].Progress) != 0 {
		t.Fatal("a stale sample must be withheld, not shown as live")
	}
}

func TestRunningJobWithNoProgressFileIsStale(t *testing.T) {
	dir := t.TempDir()
	writeJobFile(t, dir, "fetch.json", `{"name":"fetch","state":"running"}`)

	jobs, err := readJobStatuses(dir, time.Now())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if jobs[0].State != "stale" {
		t.Fatalf("a running job that never published should read stale, got %q", jobs[0].State)
	}
}

func TestFinishedJobKeepsNoProgressEvenIfSampleLeftBehind(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()
	writeJobFile(t, dir, "fetch.json",
		`{"name":"fetch","finished":"2026-07-20T12:00:00Z","ok":true,"exit_code":0}`)
	writeJobFile(t, dir, "fetch.progress.json",
		`{"updated":"`+now.Format(time.RFC3339)+`","percent_done":0.9}`)

	jobs, err := readJobStatuses(dir, now)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if jobs[0].State != "" {
		t.Fatalf("finished job should carry no state, got %q", jobs[0].State)
	}
	if len(jobs[0].Progress) != 0 {
		t.Fatal("a finished job must not be decorated with progress")
	}
}

func TestHandleJobsServesArrayAndRejectsNonGET(t *testing.T) {
	dir := t.TempDir()
	writeJobFile(t, dir, "fetch.json", `{"name":"fetch","ok":true}`)
	h := handleJobs(dir)

	rec := httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("want 200, got %d", rec.Code)
	}
	var jobs []jobStatus
	if err := json.Unmarshal(rec.Body.Bytes(), &jobs); err != nil {
		t.Fatalf("body is not a JSON array: %v", err)
	}
	if len(jobs) != 1 || jobs[0].Name != "fetch" {
		t.Fatalf("unexpected body: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	h(rec, httptest.NewRequest(http.MethodPost, "/jobs", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("want 405 for POST, got %d", rec.Code)
	}
}

func TestHandleJobsEmptyDirIsEmptyArrayNotNull(t *testing.T) {
	rec := httptest.NewRecorder()
	handleJobs(t.TempDir())(rec, httptest.NewRequest(http.MethodGet, "/jobs", nil))
	if got := rec.Body.String(); got != "[]\n" {
		t.Fatalf("want an empty array, got %q — null breaks the console's render", got)
	}
}
