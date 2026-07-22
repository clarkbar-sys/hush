package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestMain neutralises the systemd probe for the whole package.
//
// readConventionBackupStatuses now enumerates the machine's restic-backup@
// units, so without this a developer box that happens to be mid-backup injects
// a real, unrelated backup into every test that reads a status directory —
// failing assertions that have nothing to do with this feature. Tests that care
// about the probe stub it themselves via stubRunningBackups.
func TestMain(m *testing.M) {
	runningBackups = func() map[string]string { return nil }
	// Neutralise the timer probe too: without a stub every status read shells
	// out to `systemctl is-enabled`, so a test box whose systemctl answers
	// differently (or has a matching unit) would flip Paused under tests that
	// never meant to exercise it. Tests that care stub it via stubTimerPaused.
	timerPaused = func(string) bool { return false }
	os.Exit(m.Run())
}

// stubTimerPaused makes the timer probe report the given names as paused for
// one test, restoring the previous probe afterwards.
func stubTimerPaused(t *testing.T, paused ...string) {
	t.Helper()
	set := make(map[string]bool, len(paused))
	for _, n := range paused {
		set[n] = true
	}
	prev := timerPaused
	timerPaused = func(name string) bool { return set[name] }
	t.Cleanup(func() { timerPaused = prev })
}

// stubRunningBackups makes the systemd probe answer with a fixed set for one
// test. A fresh copy is handed out per call, so a test cannot be affected by
// what the code under test does with the map it receives.
func stubRunningBackups(t *testing.T, running map[string]string) {
	t.Helper()
	prev := runningBackups
	runningBackups = func() map[string]string {
		out := make(map[string]string, len(running))
		for k, v := range running {
			out[k] = v
		}
		return out
	}
	t.Cleanup(func() { runningBackups = prev })
}

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

func TestPausedBackupReportsPausedAndSurvivesRoundTrip(t *testing.T) {
	dir := t.TempDir()
	// A clean, finished run — the shape a paused backup carries: its last run
	// was fine, the timer is just switched off now. The status file itself says
	// nothing about pausing (the runner never writes it); the agent learns it
	// from systemd, so a "paused":true must appear on the wire even though the
	// file on disk has none.
	writeStatus(t, dir, "vault.json", `{"name":"vault","ok":true}`)
	stubTimerPaused(t, "vault")

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 || !got[0].Paused {
		t.Fatalf("Paused not set: %+v", got)
	}

	// The console reads the re-marshalled JSON, not the struct — an unknown
	// field would be dropped there. Confirm "paused":true reaches the wire.
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"paused":true`) {
		t.Fatalf("paused missing from wire JSON: %s", b)
	}
}

func TestEnabledBackupOmitsPaused(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "vault.json", `{"name":"vault","ok":true}`)
	// timerPaused defaults to false via TestMain — an enabled timer.

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if got[0].Paused {
		t.Fatalf("Paused set for an enabled timer: %+v", got[0])
	}
	// omitempty keeps the wire shape identical for the common (enabled) case.
	b, err := json.Marshal(got[0])
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(b), "paused") {
		t.Fatalf("paused should be omitted when false: %s", b)
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

// listUnitsFixture is real `systemctl list-units restic-backup@*.service --all
// --no-pager --output=json` output, captured from a box mid-backup. Held
// verbatim so the parser is tested against what systemd actually emits.
const listUnitsFixture = `[{"unit":"restic-backup@jaassh-nas.service","load":"loaded","active":"activating","sub":"start","job":"start","description":"restic backup: jaassh-nas"}]`

func TestParseRunningBackupUnitsAcceptsActivatingOneshot(t *testing.T) {
	// The trap this whole function exists to avoid: these are Type=oneshot
	// units, and a oneshot that is *executing* reports "activating"/"start",
	// never "active". A check for "active" — the one you write without looking —
	// is false for the entire life of every run, so the feature would appear to
	// work, ship, and never once fire.
	got := parseRunningBackupUnits([]byte(listUnitsFixture))
	if len(got) != 1 || got[0] != "jaassh-nas" {
		t.Fatalf("parseRunningBackupUnits = %v, want [jaassh-nas]", got)
	}
}

func TestParseRunningBackupUnitsIgnoresUnitsNotInFlight(t *testing.T) {
	// "active"/"exited" is a finished RemainAfterExit run, not a live one, and
	// is the case most likely to be waved through by a looser check.
	in := `[
	  {"unit":"restic-backup@dead.service","active":"inactive","sub":"dead"},
	  {"unit":"restic-backup@failed.service","active":"failed","sub":"failed"},
	  {"unit":"restic-backup@done.service","active":"active","sub":"exited"},
	  {"unit":"restic-backup@live.service","active":"active","sub":"running"}
	]`
	got := parseRunningBackupUnits([]byte(in))
	if len(got) != 1 || got[0] != "live" {
		t.Fatalf("parseRunningBackupUnits = %v, want [live]", got)
	}
}

func TestParseRunningBackupUnitsIgnoresForeignAndUnparseable(t *testing.T) {
	in := `[{"unit":"nginx.service","active":"active","sub":"running"}]`
	if got := parseRunningBackupUnits([]byte(in)); len(got) != 0 {
		t.Fatalf("foreign unit leaked in: %v", got)
	}
	// A systemd too old for --output=json prints a table, not JSON. That must
	// read as "nothing running", not panic or half-parse.
	if got := parseRunningBackupUnits([]byte("UNIT LOAD ACTIVE SUB\n")); len(got) != 0 {
		t.Fatalf("non-JSON output should yield nothing, got %v", got)
	}
}

func TestParseUnixTimestampProperty(t *testing.T) {
	// The formatted case is the real trap: it is what systemd prints WITHOUT
	// --timestamp=unix, and it must read as unknown rather than as some number
	// scraped out of the date. Silently accepting it would put a wrong start
	// time on the card.
	cases := []struct {
		name, in, want string
	}{
		{"unix form", "ExecMainStartTimestamp=@1784476487\n", "2026-07-19T11:54:47-04:00"},
		{"formatted form is refused", "ExecMainStartTimestamp=Sun 2026-07-19 11:54:47 EDT\n", ""},
		{"unset is @0", "ExecMainStartTimestamp=@0\n", ""},
		{"empty value", "ExecMainStartTimestamp=\n", ""},
		{"absent property", "OtherProperty=@1784476487\n", ""},
	}
	loc, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skipf("tzdata unavailable: %v", err)
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := parseUnixTimestampProperty([]byte(c.in), "ExecMainStartTimestamp")
			want := c.want
			if want != "" {
				// Compare as instants; the formatted zone depends on where the
				// test runs, and asserting a literal string would make this pass
				// only in one timezone.
				wantT, err := time.Parse(time.RFC3339, want)
				if err != nil {
					t.Fatalf("bad want: %v", err)
				}
				gotT, err := time.Parse(time.RFC3339, got)
				if err != nil {
					t.Fatalf("parse got %q: %v", got, err)
				}
				if !gotT.Equal(wantT) {
					t.Fatalf("got %s, want %s", gotT.In(loc), wantT.In(loc))
				}
				return
			}
			if got != "" {
				t.Fatalf("got %q, want empty", got)
			}
		})
	}
}

func TestReadBackupStatusesSynthesizesRunningWithoutStatusFile(t *testing.T) {
	// The bug this feature was built for. A box's first backup has never
	// finished, so it has written no status file, so the directory is empty —
	// which already means "no backups configured, this box is unprotected".
	// The longest run the box will ever do was indistinguishable from a box
	// nobody set up, for its entire duration.
	dir := t.TempDir()
	stubRunningBackups(t, map[string]string{"first-ever": "2026-07-19T11:54:47-04:00"})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 synthesized entry", len(got))
	}
	if got[0].Name != "first-ever" || got[0].State != "running" {
		t.Fatalf("got %+v, want name=first-ever state=running", got[0])
	}
	if got[0].Started != "2026-07-19T11:54:47-04:00" {
		t.Fatalf("Started = %q, want systemd's start time", got[0].Started)
	}
}

func TestReadBackupStatusesMarksRunningAndClearsPreviousOutcome(t *testing.T) {
	// A status file describing a FINISHED run, while systemd says a new run is
	// under way. Every outcome field on disk belongs to the previous run, and
	// the console reads a run in flight as carrying no outcome yet — so leaving
	// them would show the last run's verdict against this run's clock.
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{
	  "name":"nightly","repository":"rest:http://nas/repo","paths":["/data"],
	  "started":"2026-07-18T03:00:00-04:00","finished":"2026-07-18T03:40:00-04:00",
	  "exit_code":3,"ok":false,"incomplete":true,"summary":{"files_new":7}
	}`)
	stubRunningBackups(t, map[string]string{"nightly": "2026-07-19T11:54:47-04:00"})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1 (enriched, not duplicated)", len(got))
	}
	s := got[0]
	if s.State != "running" {
		t.Fatalf("State = %q, want running", s.State)
	}
	if s.Started != "2026-07-19T11:54:47-04:00" {
		t.Fatalf("Started = %q, want this run's start, not the previous run's", s.Started)
	}
	if s.Finished != "" || s.ExitCode != 0 || s.OK || s.Incomplete || s.Summary != nil {
		t.Fatalf("previous run's outcome survived onto a running status: %+v", s)
	}
	// What is still true about the backup is kept.
	if s.Repository != "rest:http://nas/repo" || len(s.Paths) != 1 {
		t.Fatalf("repository/paths should survive: %+v", s)
	}
}

func TestReadBackupStatusesLeavesFinishedRunsAloneWhenNothingRunning(t *testing.T) {
	// systemd only ever ADDS "running" here. A hand-run backup — the restore
	// drill the runner explicitly supports — has no unit, so "no active unit"
	// must never be read as "not running" and used to clear a marker.
	dir := t.TempDir()
	writeStatus(t, dir, "done.json", `{"name":"done","ok":true,"exit_code":0,"finished":"2026-07-18T03:40:00-04:00"}`)
	writeStatus(t, dir, "handrun.json", `{"name":"handrun","state":"running","started":"2026-07-19T09:00:00-04:00"}`)
	stubRunningBackups(t, nil)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d, want 2", len(got))
	}
	if got[0].Name != "done" || !got[0].OK || got[0].State != "" {
		t.Fatalf("finished run was disturbed: %+v", got[0])
	}
	if got[1].Name != "handrun" || got[1].State != "running" {
		t.Fatalf("hand-run marker was cleared: %+v", got[1])
	}
}

func TestMarkRunningKeepsOwnStartWhenSystemdWillNotSay(t *testing.T) {
	// systemd knows the unit is up but not when it started. A file already
	// describing THIS run still has the right answer, so keep it...
	s := conventionBackupStatus{Name: "x", State: "running", Started: "2026-07-19T09:00:00-04:00"}
	markRunning(&s, "")
	if s.Started != "2026-07-19T09:00:00-04:00" {
		t.Fatalf("Started = %q, want the running file's own start time", s.Started)
	}

	// ...but a file describing a run that already ENDED does not, and a wrong
	// start time is worse than none: it is what the stalled check measures.
	prev := conventionBackupStatus{Name: "x", OK: true, Started: "2026-07-18T03:00:00-04:00"}
	markRunning(&prev, "")
	if prev.Started != "" {
		t.Fatalf("Started = %q, want empty rather than the previous run's", prev.Started)
	}
}

// --- live progress -----------------------------------------------------

func TestProgressAttachedToRunningBackup(t *testing.T) {
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{"name":"nightly","state":"running","started":"2026-07-19T11:54:47-04:00"}`)
	writeStatus(t, dir, "nightly.progress.json",
		`{"name":"nightly","updated":"2026-07-19T12:30:00-04:00","percent_done":0.42,"bytes_done":100,"total_bytes":238}`)
	stubRunningBackups(t, map[string]string{"nightly": "2026-07-19T11:54:47-04:00"})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Progress == nil {
		t.Fatal("running backup carries no progress")
	}
	// Passed through verbatim: the agent must not need a release to carry a
	// field the runner has started publishing.
	var p map[string]any
	if err := json.Unmarshal(got[0].Progress, &p); err != nil {
		t.Fatalf("progress is not valid JSON: %v", err)
	}
	if p["percent_done"] != 0.42 {
		t.Fatalf("percent_done = %v, want 0.42", p["percent_done"])
	}
}

func TestProgressIgnoredWhenNotRunning(t *testing.T) {
	// The severe case. A run killed mid-flight (OOM, reboot) never runs the
	// runner's cleanup, so its last progress file outlives it. Attaching that
	// to a finished backup would show a frozen percentage that reads as live.
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{"name":"nightly","ok":true,"exit_code":0,"finished":"2026-07-19T12:00:00-04:00"}`)
	writeStatus(t, dir, "nightly.progress.json", `{"name":"nightly","percent_done":0.42}`)
	stubRunningBackups(t, nil)

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Progress != nil {
		t.Fatalf("finished backup carries stale progress: %s", got[0].Progress)
	}
}

func TestProgressFileIsNotListedAsABackup(t *testing.T) {
	// <name>.progress.json ends in .json, so unlike the .jsonl history log it
	// is not excluded by the suffix filter and must be skipped by name. Left
	// in, a running backup grows a phantom twin card called "nightly.progress".
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{"name":"nightly","state":"running"}`)
	writeStatus(t, dir, "nightly.progress.json", `{"name":"nightly","percent_done":0.5}`)
	stubRunningBackups(t, map[string]string{"nightly": ""})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		names := make([]string, len(got))
		for i, s := range got {
			names[i] = s.Name
		}
		t.Fatalf("len = %d, want 1; got %v", len(got), names)
	}
	if got[0].Name != "nightly" {
		t.Fatalf("name = %q, want nightly", got[0].Name)
	}
}

func TestProgressSynthesizedForFirstEverRun(t *testing.T) {
	// A backup that has never finished has no status file at all — the case the
	// systemd probe exists for — and it is also the longest run a box will ever
	// do, so it is exactly where a percentage is worth the most.
	dir := t.TempDir()
	writeStatus(t, dir, "first-ever.progress.json", `{"name":"first-ever","percent_done":0.07}`)
	stubRunningBackups(t, map[string]string{"first-ever": "2026-07-19T11:54:47-04:00"})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Name != "first-ever" || got[0].State != "running" {
		t.Fatalf("got %+v", got[0])
	}
	if got[0].Progress == nil {
		t.Fatal("first-ever run carries no progress")
	}
}

func TestProgressMalformedIsDroppedNotFatal(t *testing.T) {
	// A reader can catch a half-written file. Losing the percentage is fine;
	// losing the whole status read because of it is not.
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{"name":"nightly","state":"running"}`)
	writeStatus(t, dir, "nightly.progress.json", `{"name":"nightly","percent_don`)
	stubRunningBackups(t, map[string]string{"nightly": ""})

	got, err := readConventionBackupStatuses(dir)
	if err != nil {
		t.Fatalf("readConventionBackupStatuses: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len = %d, want 1", len(got))
	}
	if got[0].Progress != nil {
		t.Fatalf("malformed progress should be dropped, got %s", got[0].Progress)
	}
	if got[0].State != "running" {
		t.Fatalf("state = %q, want running", got[0].State)
	}
}

func TestProgressOmittedFromJSONWhenAbsent(t *testing.T) {
	// The wire shape must stay byte-identical for every box that is not
	// mid-run, so an older console sees exactly what it saw before.
	dir := t.TempDir()
	writeStatus(t, dir, "nightly.json", `{"name":"nightly","ok":true}`)
	stubRunningBackups(t, nil)

	rec := httptest.NewRecorder()
	handleConventionBackupStatus(dir)(rec, httptest.NewRequest(http.MethodGet, "/backup-status", nil))
	if body := rec.Body.String(); strings.Contains(body, "progress") {
		t.Fatalf("absent progress must not appear on the wire: %s", body)
	}
}
