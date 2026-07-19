package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"time"
)

// backupKeyRecord is one backup's escrow record: its definition together with the
// repo encryption password. Unlike backupView — which the API returns and which
// omits the password by construction — this deliberately INCLUDES the key, because
// exporting the keys is the whole point. It is only ever written to the local
// stdout of `hush-agent -export-keys`, never served over the network.
type backupKeyRecord struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Repo     string   `json:"repo"`
	Password string   `json:"password"`
	Paths    []string `json:"paths,omitempty"`
	Excludes []string `json:"excludes,omitempty"`
	Schedule string   `json:"schedule,omitempty"`
}

// backupKeyExport is the document `hush-agent -export-keys` prints: this host's
// backup definitions and their repo keys, for an operator to stash off-box (a
// password manager) so a dead box doesn't take the only copy of its keys with it.
// It is the escrow answer to backups' central rule — the key stays on the box —
// which otherwise leaves the sole copy of a repo key on the very disk the backup
// exists to survive.
type backupKeyExport struct {
	Host        string            `json:"host"`
	GeneratedAt string            `json:"generatedAt"`
	Backups     []backupKeyRecord `json:"backups"`
}

// exportKeys reads the box's persisted backups and writes their escrow document
// as indented JSON to w. It is the on-box counterpart to "the key stays on the
// box": the secret is emitted to a local stream the operator controls (their SSH
// session), and never passes through hush-control or the phone — the same trust
// boundary the running agent keeps. host is passed in rather than read here so
// the caller owns the os.Hostname lookup and the function stays a pure transform.
//
// It reads backups.json directly and does not need the agent to be running; the
// store tolerates a missing file, so a box with backups never configured simply
// exports an empty list.
func exportKeys(stateDir, host string, w io.Writer) error {
	defs := newBackupStore(backupStatePath(stateDir)).Snapshot()
	out := backupKeyExport{
		Host:        host,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		Backups:     make([]backupKeyRecord, 0, len(defs)),
	}
	for _, b := range defs {
		out.Backups = append(out.Backups, backupKeyRecord{
			ID:       b.ID,
			Name:     b.Name,
			Repo:     b.Repo,
			Password: b.Password,
			Paths:    b.Paths,
			Excludes: b.Excludes,
			Schedule: b.Schedule,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}

// runExportKeys is the `-export-keys` entry point: it prints the escrow document
// to stdout and returns a process exit code. JSON goes to stdout alone (the
// store's own load diagnostics go to stderr via the log package), so the output
// pipes cleanly into a file or a password manager without stray lines.
func runExportKeys(stateDir string) int {
	host, _ := os.Hostname()
	if err := exportKeys(stateDir, host, os.Stdout); err != nil {
		log.Printf("export-keys: %v", err)
		return 1
	}
	return 0
}
