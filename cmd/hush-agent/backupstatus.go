package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// A box can carry backups the agent does not run. The Backup construct
// (backups.go) executes restic inside hush-agent, which is unprivileged by
// design — so its reach stops at what that user can read, which on a box with
// per-service homes and 0700 user data excludes most of what a backup exists
// to protect. Those backups run as root from a systemd timer instead, following
// docs/BACKUP-CONVENTION.md, and report by writing a status file:
//
//	/var/lib/hush-backups/<name>.json
//
// This file is the read side of that convention. It is the reason the agent
// needs no new privilege to report a privileged backup: the runner writes a
// file containing no secrets, and the agent only ever reads it.
//
// Unlike /backups, /backup-status is served unconditionally — not behind
// -backup. That flag gates the agent *running* restic, which reads whatever
// paths it is pointed at; reading a status file that holds no secrets and
// names no paths carries none of that risk, and gating it would mean a box
// that reports its backups only if it also grants the agent the power to make
// new ones.
const defaultBackupStatusDir = "/var/lib/hush-backups"

// conventionBackupStatus is one <name>.json as written by scripts/restic-backup-run.
//
// Repository is recorded with its userinfo already stripped by the writer,
// because restic's rest: backend carries HTTP auth inline in the URL — the raw
// repository string is itself a credential. Nothing here is a secret, which is
// what makes the file world-readable and this endpoint ungated.
type conventionBackupStatus struct {
	Name       string          `json:"name"`
	Repository string          `json:"repository"`
	Paths      []string        `json:"paths,omitempty"`
	Started    string          `json:"started"`
	Finished   string          `json:"finished"`
	ExitCode   int             `json:"exit_code"`
	OK         bool            `json:"ok"`
	Incomplete bool            `json:"incomplete"`
	Summary    json.RawMessage `json:"summary,omitempty"`

	// State is "running" while a run is in flight — the runner writes it at
	// start and overwrites it with the outcome fields above when restic exits.
	// It is declared here, rather than left to the raw-passthrough treatment
	// Summary and History get, precisely because the agent unmarshals and
	// re-marshals this struct: an unknown field is dropped on the way through,
	// so a "state" the runner set would never reach the console unless it is a
	// field the agent knows. Empty for a finished run, so omitempty keeps the
	// wire shape identical to before for every box that isn't mid-run.
	State string `json:"state,omitempty"`

	// Paused is true when this backup's systemd timer has been deliberately
	// stopped — disabled or masked — rather than left enabled and scheduled.
	// It is the read side of "pause a backup": the convention
	// (docs/BACKUP-CONVENTION.md) defines a live backup as its files plus one
	// *enabled* timer, so `systemctl disable --now restic-backup@<name>.timer`
	// is how you pause one, and this field is how the console learns of it.
	//
	// Like State, it is a *declared* field for a reason: the agent unmarshals
	// and re-marshals this struct, so a field it does not know is dropped on the
	// way through. Unlike State the runner never writes it — the agent fills it
	// in from systemd (timerPaused), the same live-probe idiom as NextRun — so a
	// paused backup is reported paused even by a runner too old to know the idea
	// exists. omitempty keeps the wire shape identical for every box whose timer
	// is enabled, which is every box that isn't paused.
	Paused bool `json:"paused,omitempty"`

	// Progress is how far the run in flight has got, read from the
	// <name>.progress.json the runner maintains beside this file. Raw JSON, for
	// the same reason Summary is: the agent stays ignorant of the schema, so a
	// field the runner starts publishing reaches the console without a change
	// here. Only ever populated for a run that is actually running — a progress
	// file outliving its run (killed mid-flight, so the runner's cleanup never
	// ran) must not decorate a finished backup with a stale percentage.
	Progress json.RawMessage `json:"progress,omitempty"`

	// History and NextRun are assembled by the agent rather than read from the
	// status file: history lives in its own append-only log, and the next fire
	// is systemd's to answer, not something a finished run can know.
	History []json.RawMessage `json:"history,omitempty"`
	NextRun string            `json:"next_run,omitempty"`
}

// historyLimit is how many past runs are reported. The console draws a
// two-week strip; the runner keeps a little more on disk than this.
const historyLimit = 14

// readHistory returns up to historyLimit past runs, oldest first, from the
// append-only log beside the status file.
//
// Each line is passed through as raw JSON for the same reason the summary is:
// the agent stays ignorant of restic's schema, so a field added to the runner's
// history entry reaches the console without a change here. A line that does not
// parse is dropped rather than failing the read — a truncated final line (a
// crash mid-append) must not cost the reader the other thirteen days.
func readHistory(dir, name string) []json.RawMessage {
	b, err := os.ReadFile(filepath.Join(dir, name+".history.jsonl"))
	if err != nil {
		return nil
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) > historyLimit {
		lines = lines[len(lines)-historyLimit:]
	}
	out := make([]json.RawMessage, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !json.Valid([]byte(line)) {
			continue
		}
		out = append(out, json.RawMessage(line))
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// readProgress returns the live progress of a run in flight, or nil.
//
// Passed through as raw JSON rather than unmarshalled, exactly as the summary
// and the history lines are — the agent has no opinion about restic's numbers,
// and a runner that starts publishing another field should not need a matching
// release of the agent to make it visible.
//
// A missing file is the normal case and not an error: it means a run that has
// not published yet, a runner too old to publish at all, or progress switched
// off. The console draws the indeterminate shuttle it always drew for those.
// A file that does not parse is dropped for the same reason — a half-written
// number must not take out the whole status read.
func readProgress(dir, name string) json.RawMessage {
	b, err := os.ReadFile(filepath.Join(dir, name+".progress.json"))
	if err != nil {
		return nil
	}
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || !json.Valid(b) {
		return nil
	}
	return json.RawMessage(b)
}

// nextRun asks systemd when this backup's timer fires next, as RFC 3339.
//
// It is queried live rather than recorded by the runner because a recorded
// value goes stale the moment the schedule changes — and a console showing a
// next-run time that no longer matches the timer is worse than showing none.
// Everything about this degrades to an empty string: no systemd, no such timer,
// a disabled timer, or a systemctl that takes too long. The field is omitempty,
// so the console simply doesn't draw it.
func nextRun(name string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	unit := backupTimerUnit(name)
	// list-timers --output=json, NOT `show --property=NextElapseUSecRealtime`.
	// Despite the USec name, that property renders as a locale- and
	// timezone-formatted string ("Mon 2026-07-20 00:00:03 EDT"), so parsing it
	// as a number always fails — and with a silent fallback that means the
	// field is simply never populated, on every box, forever. list-timers emits
	// real microseconds.
	out, err := exec.CommandContext(ctx, "systemctl", "list-timers", unit,
		"--no-pager", "--output=json").Output()
	if err != nil {
		return ""
	}
	return parseNextElapse(out, unit)
}

func backupTimerUnit(name string) string { return "restic-backup@" + name + ".timer" }

// backupServiceUnit is the convention's service unit, the companion to
// backupTimerUnit. docs/BACKUP-CONVENTION.md already fixes both names, so
// reading them here adds no coupling the convention did not have.
func backupServiceUnit(name string) string { return "restic-backup@" + name + ".service" }

// timerPaused reports whether this backup's timer has been deliberately stopped
// — disabled or masked — as opposed to enabled and scheduled.
//
// Indirected through a variable, like runningBackups, so tests can drive the
// paused path without a systemd whose timers match the test's fixtures.
var timerPaused = systemdTimerPaused

// systemdTimerPaused asks systemd whether the backup's timer is enabled.
//
// This is the read side of "pause a backup". docs/BACKUP-CONVENTION.md defines a
// live backup as its files plus one *enabled* timer, so `systemctl disable
// --now` (or `mask`) is how you pause one — and the console needs to tell that
// deliberate stop apart from a nightly that silently quit firing. Without it a
// paused backup ages past its schedule and reads as "at risk", exactly like a
// broken one, so the operator is nagged about a box they turned off on purpose.
//
// Only a definite "disabled" or "masked" is treated as paused. Everything else —
// enabled, a static timer, a systemd too old for the query, no such unit, or a
// systemctl that times out — reports not-paused, because the safe default for a
// backup console is to keep watching a backup, not to fall silent about one.
//
// is-enabled prints the state word to stdout and exits non-zero for a
// disabled/masked unit, so the error is expected and ignored: the word is the
// answer, the exit code merely echoes it.
func systemdTimerPaused(name string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, _ := exec.CommandContext(ctx, "systemctl", "is-enabled",
		backupTimerUnit(name), "--no-pager").Output()
	switch strings.TrimSpace(string(out)) {
	case "disabled", "masked":
		return true
	default:
		return false
	}
}

// runningBackups reports which convention backups are executing right now, as
// name -> start time (RFC 3339, empty when systemd will not say).
//
// Indirected through a variable so tests can stub it. Without that, the probe
// enumerates whatever restic-backup@ units the *test machine* is running, and
// a test asserting "two statuses" fails on a developer box that is mid-backup.
var runningBackups = systemdRunningBackups

// systemdRunningBackups asks systemd which convention backups are in flight.
//
// The status file cannot answer this on its own. The runner writes a
// "state":"running" marker at start, but that only helps once the *runner* is
// current: a box whose /usr/local/bin/restic-backup-run predates that change
// reports nothing while it works, and the agent self-updates its own binary
// without ever refreshing that script — so the two drift, silently, and the
// console goes blind exactly when it has something to say. systemd is the box's
// own record of what is executing. It needs no cooperation from the runner, it
// is right about a run that started before the agent did, and it is there for a
// backup that has never once completed and so has no status file to enrich.
//
// Everything degrades to an empty map: no systemd, no such units, a systemctl
// too old for --output=json, or one that takes too long. The caller then
// behaves exactly as it did before this existed.
func systemdRunningBackups() map[string]string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "systemctl", "list-units",
		"restic-backup@*.service", "--all", "--no-pager", "--output=json").Output()
	if err != nil {
		return nil
	}
	names := parseRunningBackupUnits(out)
	if len(names) == 0 {
		return nil
	}
	running := make(map[string]string, len(names))
	for _, name := range names {
		running[name] = systemdRunStart(backupServiceUnit(name))
	}
	return running
}

// parseRunningBackupUnits pulls the in-flight backup names out of `systemctl
// list-units --output=json`. Split from the exec call so this is tested against
// captured systemd output rather than whatever the machine happens to be doing.
//
// The trap: these are Type=oneshot units, and a oneshot that is *executing*
// reports ActiveState "activating" with SubState "start" — NOT "active". The
// obvious check, `active == "active"`, is false for the entire life of every
// run, on every box, forever. "active"/"running" is accepted as well so a unit
// that is not oneshot still reads correctly; "active"/"exited" deliberately is
// not, since that is a finished RemainAfterExit run rather than a live one.
func parseRunningBackupUnits(out []byte) []string {
	var units []struct {
		Unit   string `json:"unit"`
		Active string `json:"active"`
		Sub    string `json:"sub"`
	}
	if err := json.Unmarshal(out, &units); err != nil {
		return nil
	}
	var names []string
	for _, u := range units {
		inFlight := u.Active == "activating" || (u.Active == "active" && u.Sub == "running")
		if !inFlight {
			continue
		}
		if name := backupNameFromUnit(u.Unit); name != "" {
			names = append(names, name)
		}
	}
	return names
}

// backupNameFromUnit is the inverse of backupServiceUnit, and returns "" for
// anything that is not one of the convention's service units.
func backupNameFromUnit(unit string) string {
	const prefix, suffix = "restic-backup@", ".service"
	if !strings.HasPrefix(unit, prefix) || !strings.HasSuffix(unit, suffix) {
		return ""
	}
	return strings.TrimSuffix(strings.TrimPrefix(unit, prefix), suffix)
}

// systemdRunStart returns when the current run of unit began, as RFC 3339.
//
// --timestamp=unix is load-bearing, and it is the same trap parseNextElapse
// documents one screen up: by default systemd renders ExecMainStartTimestamp as
// a locale- and timezone-formatted string ("Sun 2026-07-19 11:54:47 EDT"), which
// no numeric parse recovers. --timestamp=unix asks for "@1784476487" instead.
// Empty on any failure — a running backup whose start time is unknown is still
// worth reporting as running.
func systemdRunStart(unit string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	out, err := exec.CommandContext(ctx, "systemctl", "show", unit,
		"--property=ExecMainStartTimestamp", "--timestamp=unix", "--no-pager").Output()
	if err != nil {
		return ""
	}
	return parseUnixTimestampProperty(out, "ExecMainStartTimestamp")
}

// parseUnixTimestampProperty reads a `Property=@1784476487` line as RFC 3339.
// A property systemd cannot answer comes back empty or absent, and an unset
// timestamp is "@0"; all of those are reported as unknown.
func parseUnixTimestampProperty(out []byte, prop string) string {
	for _, line := range strings.Split(string(out), "\n") {
		val, ok := strings.CutPrefix(strings.TrimSpace(line), prop+"=")
		if !ok {
			continue
		}
		val, ok = strings.CutPrefix(strings.TrimSpace(val), "@")
		if !ok {
			return ""
		}
		secs, err := strconv.ParseInt(val, 10, 64)
		if err != nil || secs <= 0 {
			return ""
		}
		return time.Unix(secs, 0).Format(time.RFC3339)
	}
	return ""
}

// markRunning rewrites a status to describe the run happening now.
//
// The outcome fields are cleared rather than kept: when a status file records a
// *finished* run and systemd says a new one is under way, every one of
// finished/exit_code/ok/incomplete/summary belongs to the previous run, and the
// console reads a run in flight as carrying no outcome yet. Leaving them would
// show the last run's verdict against the current run's clock. What survives is
// what is still true — the name, the repository, and the paths it covers.
//
// The result is byte-for-byte the shape the runner's own start marker writes,
// so a box detected this way and a box that announced itself render identically.
func markRunning(s *conventionBackupStatus, started string) {
	// Read the file's own state before overwriting it.
	describesThisRun := s.State == "running"

	s.State = "running"
	s.Finished = ""
	s.ExitCode = 0
	s.OK = false
	s.Incomplete = false
	s.Summary = nil

	switch {
	case started != "":
		// systemd is authoritative for the run in flight; the file may still be
		// describing the previous one.
		s.Started = started
	case !describesThisRun:
		// systemd would not say when, and the file's start time belongs to a run
		// that has already ended. Report none rather than one that is wrong.
		s.Started = ""
	}
}

// parseNextElapse pulls one unit's next fire out of `systemctl list-timers
// --output=json`, as RFC 3339. Split from the exec call so the parsing is
// testable without depending on which timers happen to exist on the machine.
//
// Returns "" for anything unusable — no match, a monotonic-only timer (next is
// 0), or output from a systemd too old to know --output=json.
func parseNextElapse(out []byte, unit string) string {
	var timers []struct {
		Unit string `json:"unit"`
		Next int64  `json:"next"`
	}
	if err := json.Unmarshal(out, &timers); err != nil {
		return ""
	}
	for _, t := range timers {
		if t.Unit == unit && t.Next > 0 {
			return time.UnixMicro(t.Next).Format(time.RFC3339)
		}
	}
	return ""
}

// readConventionBackupStatuses loads every <name>.json in dir, sorted by name so the
// console's ordering is stable between polls.
//
// A missing directory is not an error: a box with no convention backups is the
// normal case, and it reports an empty list rather than a failure. A file that
// does not parse is skipped and logged — never silently dropped, since a status
// file that stopped being readable is exactly the kind of thing a backup
// console must not quietly hide.
func readConventionBackupStatuses(dir string) ([]conventionBackupStatus, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []conventionBackupStatus{}, nil
		}
		return nil, err
	}

	// Asked once, before the loop. Names matched to a status file are recorded
	// rather than deleted, because the map belongs to the caller: mutating it
	// would quietly corrupt any implementation that returns a cached or shared
	// map instead of building a fresh one per call.
	running := runningBackups()
	matched := make(map[string]bool, len(running))

	out := make([]conventionBackupStatus, 0, len(entries))
	for _, e := range entries {
		// The .history.jsonl logs sit in this directory too, but end in
		// .jsonl, so this filter already passes over them; they are read per
		// backup below rather than enumerated as statuses of their own.
		//
		// The <name>.progress.json files need excluding explicitly, because
		// unlike the history logs they DO end in .json. Left in, every backup
		// would sprout a phantom twin named "<name>.progress" for exactly as
		// long as it was running — a second card, reporting no outcome, that
		// disappears when the run ends.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".progress.json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("backup-status: cannot read %s: %v", path, err)
			continue
		}
		var s conventionBackupStatus
		if err := json.Unmarshal(b, &s); err != nil {
			log.Printf("backup-status: cannot parse %s: %v", path, err)
			continue
		}
		if s.Name == "" {
			// Fall back to the filename so a status file that lost its name
			// field still identifies itself in the console.
			s.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		// systemd only ever adds "running" here, never removes it. A hand-run
		// backup — the restore drill the runner explicitly supports — has no unit
		// at all, so "no active unit" does not mean "not running", and clearing a
		// marker on that basis would report a live run as finished. Deciding a
		// marker is orphaned stays with the console, which has bkStalled for it.
		//
		// Progress is attached only here, gated on systemd's answer rather than
		// on the file's own "running" marker. A run killed mid-flight leaves
		// both the marker and the last progress file behind, and a frozen
		// percentage presented as live is a worse lie than no percentage: the
		// console's stalled detection would eventually catch the marker, but
		// the number beside it would have looked authoritative the whole time.
		if started, ok := running[s.Name]; ok {
			markRunning(&s, started)
			s.Progress = readProgress(dir, s.Name)
			matched[s.Name] = true
		}
		s.History = readHistory(dir, strings.TrimSuffix(e.Name(), ".json"))
		s.NextRun = nextRun(s.Name)
		s.Paused = timerPaused(s.Name)
		out = append(out, s)
	}

	// A backup that has never finished has no status file, so the loop above
	// cannot see it — and that is the case this probe exists for. A box's first
	// backup is the longest one it will ever run, and it was invisible for its
	// entire duration, indistinguishable from a box nobody ever set up.
	for name, started := range running {
		if matched[name] {
			continue
		}
		s := conventionBackupStatus{Name: name, State: "running", Started: started}
		s.Progress = readProgress(dir, name)
		s.History = readHistory(dir, name)
		s.NextRun = nextRun(name)
		s.Paused = timerPaused(name)
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// handleConventionBackupStatus serves this box's convention-backup statuses as a JSON
// array. Always an array, never null, so the console can render it without a
// nil check.
func handleConventionBackupStatus(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		statuses, err := readConventionBackupStatuses(dir)
		if err != nil {
			http.Error(w, "cannot read backup status directory", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		if err := json.NewEncoder(w).Encode(statuses); err != nil {
			log.Printf("backup-status: encode: %v", err)
		}
	}
}
