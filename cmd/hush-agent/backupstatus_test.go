package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeStatus(t *testing.T, dir, name, body string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestReadBackupStatusesSortsByName(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "zulu.json", `{"name":"zulu","ok":true}`)
	writeStatus(t, dir, "alpha.json", `{"name":"alpha","ok":true}`)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	// Stable ordering matters: the console re-renders on every poll, and an
	// unstable order would make the list jump around under the reader's thumb.
	if got[0].Name != "alpha" || got[1].Name != "zulu" {
		t.Fatalf("not sorted by name: %q, %q", got[0].Name, got[1].Name)
	}
}

func TestReadBackupStatusesMissingDirIsEmptyNotError(t *testing.T) {
	// A box with no convention backups is the normal case, not a failure.
	got, err := readConventionBackupStatuses(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing dir should not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len = %d, want 0", len(got))
	}
}

func TestReadBackupStatusesSkipsMalformedAndNonJSON(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "good.json", `{"name":"good","ok":true}`)
	writeStatus(t, dir, "broken.json", `{"name":"broken",`)
	writeStatus(t, dir, "notes.txt", `not json at all`)
	if err := os.Mkdir(filepath.Join(dir, "sub.json"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	// One bad file must not cost the reader every other backup's status.
	if len(got) != 1 || got[0].Name != "good" {
		t.Fatalf("got %+v, want only the good entry", got)
	}
}

func TestReadBackupStatusesFallsBackToFilename(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "nameless.json", `{"ok":true}`)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 || got[0].Name != "nameless" {
		t.Fatalf("got %+v, want name derived from the filename", got)
	}
}

func TestHandleBackupStatusPreservesIncompleteAndSummary(t *testing.T) {
	dir := t.TempDir()
	// restic exits 3 when some source data could not be read: a snapshot
	// exists but is missing files. It must not reach the console as a success.
	writeStatus(t, dir, "jaassh-nas.json", `{
	  "name":"jaassh-nas",
	  "repository":"rest:http://nas:8000/jaassh/",
	  "exit_code":3,
	  "ok":false,
	  "incomplete":true,
	  "summary":{"snapshot_id":"4cef7f1f","data_added":248}
	}`)

	rr := httptest.NewRecorder()
	handleConventionBackupStatus(dir)(rr, httptest.NewRequest(http.MethodGet, "/backup-status", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	var got []conventionBackupStatus
	if err := json.Unmarshal(rr.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].OK || !got[0].Incomplete || got[0].ExitCode != 3 {
		t.Fatalf("exit 3 must surface as not-ok and incomplete: %+v", got[0])
	}
	// The summary rides through untouched so the console can show counts
	// without the agent having to know restic's schema.
	var summary map[string]any
	if err := json.Unmarshal(got[0].Summary, &summary); err != nil {
		t.Fatalf("summary should be valid JSON: %v", err)
	}
	if summary["snapshot_id"] != "4cef7f1f" {
		t.Fatalf("summary lost its contents: %v", summary)
	}
}

func TestHandleBackupStatusNeverEncodesNull(t *testing.T) {
	// The console renders the response directly; a null would need a nil check
	// at every call site, and one missed check is an empty screen.
	rr := httptest.NewRecorder()
	handleConventionBackupStatus(filepath.Join(t.TempDir(), "absent"))(rr, httptest.NewRequest(http.MethodGet, "/backup-status", nil))

	if body := rr.Body.String(); body != "[]\n" {
		t.Fatalf("body = %q, want %q", body, "[]\n")
	}
}

func TestHandleBackupStatusRejectsNonGET(t *testing.T) {
	rr := httptest.NewRecorder()
	handleConventionBackupStatus(t.TempDir())(rr, httptest.NewRequest(http.MethodPost, "/backup-status", nil))

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rr.Code)
	}
}
