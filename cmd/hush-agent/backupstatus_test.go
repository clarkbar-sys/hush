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

func TestHandleBackupStatusPreservesRunningState(t *testing.T) {
	dir := t.TempDir()
	// The runner writes this at the start of a run, before restic produces any
	// outcome — no finished, no ok, no summary. The whole point of the field is
	// that the console can tell "a run is in flight" from "no backup here" (an
	// empty status dir), so it must survive the agent's unmarshal/re-marshal. A
	// field the struct does not declare is silently dropped in that round trip,
	// which is exactly what an unstructured passthrough would have done here.
	writeStatus(t, dir, "jaassh-nas.json", `{
	  "name":"jaassh-nas",
	  "repository":"rest:http://nas:8000/jaassh/",
	  "paths":["/srv"],
	  "started":"2026-07-19T16:00:00Z",
	  "state":"running"
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
	if len(got) != 1 || got[0].State != "running" {
		t.Fatalf("running state lost in passthrough: %+v", got)
	}
}

func TestHandleBackupStatusOmitsStateForFinishedRun(t *testing.T) {
	dir := t.TempDir()
	// A finished run carries no state, and the field is omitempty, so the wire
	// shape is byte-for-byte what it was before this field existed — an older
	// console never sees a key it doesn't understand.
	writeStatus(t, dir, "done.json", `{"name":"done","ok":true,"exit_code":0}`)

	rr := httptest.NewRecorder()
	handleConventionBackupStatus(dir)(rr, httptest.NewRequest(http.MethodGet, "/backup-status", nil))

	var raw []map[string]any
	if err := json.Unmarshal(rr.Body.Bytes(), &raw); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, present := raw[0]["state"]; present {
		t.Fatalf("finished run must not carry a state key: %v", raw[0])
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

func TestReadHistoryReturnsOldestFirstAndCaps(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "many.json", `{"name":"many","ok":true}`)

	var lines string
	for i := 0; i < historyLimit+6; i++ {
		lines += `{"finished":"2026-07-` + string(rune('0'+i%10)) + `","seq":` + string(rune('0'+i%10)) + `}` + "\n"
	}
	writeStatus(t, dir, "many.history.jsonl", lines)

	got := readHistory(dir, "many")
	if len(got) != historyLimit {
		t.Fatalf("len = %d, want %d (capped)", len(got), historyLimit)
	}
}

func TestReadHistorySkipsTruncatedLine(t *testing.T) {
	dir := t.TempDir()
	// A crash mid-append leaves a partial final line. It must cost that one
	// entry, not the whole strip.
	writeStatus(t, dir, "b.history.jsonl", "{\"ok\":true}\n{\"ok\":false}\n{\"ok\":tr")

	got := readHistory(dir, "b")
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2 valid entries", len(got))
	}
}

func TestReadHistoryMissingIsNil(t *testing.T) {
	if got := readHistory(t.TempDir(), "absent"); got != nil {
		t.Fatalf("got %v, want nil so the field is omitted", got)
	}
}

func TestHistoryLogIsNotListedAsABackup(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "one.json", `{"name":"one","ok":true}`)
	writeStatus(t, dir, "one.history.jsonl", `{"ok":true}`)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 || got[0].Name != "one" {
		t.Fatalf("history log leaked in as a backup: %+v", got)
	}
	if len(got[0].History) != 1 {
		t.Fatalf("history not attached: %+v", got[0])
	}
}

func TestStatusCarriesPaths(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "p.json", `{"name":"p","ok":true,"paths":["/etc","/home/josh"]}`)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got[0].Paths) != 2 || got[0].Paths[0] != "/etc" {
		t.Fatalf("paths lost: %+v", got[0].Paths)
	}
}

func TestNextRunDegradesToEmpty(t *testing.T) {
	// No such timer (and possibly no systemd at all, e.g. in CI containers or
	// on a dev mac). Either way it must be an empty string, never a fabricated
	// time and never a failure.
	if got := nextRun("definitely-not-a-real-backup-name"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestParseNextElapseReadsRealSystemdOutput(t *testing.T) {
	// Captured verbatim from `systemctl list-timers <unit> --no-pager
	// --output=json` on systemd 257. The point of pinning real output is that
	// `show --property=NextElapseUSecRealtime` looks like it should work and
	// does not — it renders a formatted timestamp, not microseconds, so a
	// numeric parse fails silently and the field stays empty on every box.
	out := []byte(`[{"next":1784520003798018,"left":1784520003798018,"last":1784439292006193,"passed":3268279931,"unit":"logrotate.timer","activates":"logrotate.service"}]`)

	got := parseNextElapse(out, "logrotate.timer")
	if got == "" {
		t.Fatal("got empty, want a parsed timestamp")
	}
	if want := time.UnixMicro(1784520003798018).Format(time.RFC3339); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestParseNextElapseIgnoresOtherUnitsAndMonotonic(t *testing.T) {
	out := []byte(`[{"next":1784520003798018,"unit":"someone-else.timer"},{"next":0,"unit":"restic-backup@a.timer"}]`)

	if got := parseNextElapse(out, "restic-backup@b.timer"); got != "" {
		t.Fatalf("matched the wrong unit: %q", got)
	}
	// next == 0 is a monotonic-only timer: no realtime fire to report.
	if got := parseNextElapse(out, "restic-backup@a.timer"); got != "" {
		t.Fatalf("zero next should be empty, got %q", got)
	}
}

func TestParseNextElapseTolerantOfOldSystemd(t *testing.T) {
	// A systemd too old for --output=json prints a table, not JSON.
	if got := parseNextElapse([]byte("NEXT LEFT LAST PASSED UNIT\n"), "x.timer"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
	if got := parseNextElapse(nil, "x.timer"); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

func TestBackupTimerUnitNaming(t *testing.T) {
	if got := backupTimerUnit("jaassh-nas"); got != "restic-backup@jaassh-nas.timer" {
		t.Fatalf("got %q", got)
	}
}
