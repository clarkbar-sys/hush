package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
	Started    string          `json:"started"`
	Finished   string          `json:"finished"`
	ExitCode   int             `json:"exit_code"`
	OK         bool            `json:"ok"`
	Incomplete bool            `json:"incomplete"`
	Summary    json.RawMessage `json:"summary,omitempty"`
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
