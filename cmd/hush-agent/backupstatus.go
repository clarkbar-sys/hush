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

	out := make([]conventionBackupStatus, 0, len(entries))
	for _, e := range entries {
		// The .history.jsonl logs sit in this directory too, but end in
		// .jsonl, so this filter already passes over them; they are read per
		// backup below rather than enumerated as statuses of their own.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
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
		s.History = readHistory(dir, strings.TrimSuffix(e.Name(), ".json"))
		s.NextRun = nextRun(s.Name)
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
