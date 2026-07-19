// Command hush-agent runs on every machine in the fleet. It exposes the host's
// vitals as JSON over the tailnet and serves a read-only view of the box —
// /vitals, /top, /browse, /du, and /file — plus, when enabled, the Backup
// construct's restic backups on /backups.
//
// The one-shot -export-keys mode prints this box's backup repository keys as JSON
// to stdout and exits, for an operator to escrow them off-box over SSH — the key
// stays on the box otherwise, so a dead box would take the only copy with it.
//
// Deploy is one static binary with no runtime dependencies:
//
//	GOOS=linux GOARCH=arm64 go build ./cmd/hush-agent   # e.g. for the Pi
//	scp hush-agent pi-gate:/usr/local/bin/
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/clarkbar-sys/hush/internal/browse"
	"github.com/clarkbar-sys/hush/internal/restic"
	"github.com/clarkbar-sys/hush/internal/updater"
	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
)

func main() {
	listen := flag.String("listen", ":8765", `address to listen on, or "tailnet" to bind this machine's Tailscale IP (tailnet-only; "tailnet:PORT" for a non-default port)`)
	showVersion := flag.Bool("version", false, "print the hush-agent version and exit")
	selfUpdate := flag.Bool("self-update", false, "check for a newer release and replace this binary in place, then exit (run as root by hush-agent-update.service)")
	exportKeysFlag := flag.Bool("export-keys", false, "print this box's backup repository keys as JSON to stdout and exit — run it over SSH to escrow the keys off-box; the key never passes through hush-control")
	allowBackup := flag.Bool("backup", false, "serve /backups, the Backup construct's restic backups (off by default; -backup enables). Reads the paths you name and needs the restic binary")
	stateDir := flag.String("state-dir", "", "directory for persisted state such as backups.json (default: $STATE_DIRECTORY from systemd, else /var/lib/hush)")
	backupStatusDir := flag.String("backup-status-dir", defaultBackupStatusDir, "directory of status files written by root-run convention backups (see docs/BACKUP-CONVENTION.md). Read-only; served on /backup-status")
	flag.Parse()

	// Backups are OFF by default — the feature reads whatever paths you point
	// it at and stores a repository key on the box, so enabling it is a deliberate
	// choice. An env-over-flag toggle (HUSH_AGENT_BACKUP=1) lets the unit's
	// env file flip it without editing ExecStart.
	backupEnabled := *allowBackup
	if v, ok := os.LookupEnv("HUSH_AGENT_BACKUP"); ok {
		backupEnabled = v == "1" || v == "true" || v == "yes"
	}

	if *showVersion {
		fmt.Printf("hush-agent %s\n", version.Current())
		os.Exit(0)
	}
	if *selfUpdate {
		os.Exit(runSelfUpdate())
	}
	// -export-keys is a read-only, on-box escrow helper: it prints this box's repo
	// keys and exits without starting the agent, so an operator can pull them into
	// a password manager over their own SSH session. It resolves the state dir the
	// same way the running agent does (so it reads the real backups.json) and needs
	// no network — the key stays on the box's local stdout.
	if *exportKeysFlag {
		os.Exit(runExportKeys(resolveStateDir(*stateDir)))
	}

	listenAddr, err := resolveListen(*listen)
	if err != nil {
		log.Fatalf("hush-agent: %v", err)
	}

	vitals.StartSampler()

	// The Backup manager persists to the agent's state dir, so it is resolved
	// (and created 0700) only when backups are enabled — a default agent touches
	// no disk. resolveStateDir prefers systemd's $STATE_DIRECTORY (set by
	// StateDirectory=hush) and falls back to /var/lib/hush; the store tolerates a
	// missing file, so a first run simply starts empty.
	var stateDirPath string
	if backupEnabled {
		stateDirPath = resolveStateDir(*stateDir)
		if err := os.MkdirAll(stateDirPath, 0o700); err != nil {
			log.Printf("hush-agent: state dir %s not writable: %v — creation will fail until it is", stateDirPath, err)
		}
	}

	// The Backup manager holds the restic definitions (and their repo keys) in
	// backups.json under the state dir, and its own cron engine fires the
	// scheduled ones unattended.
	var backups *backupManager
	if backupEnabled {
		backups = newBackupManager(backupStatePath(stateDirPath))
		backups.Start()
		defer backups.Stop()
	}

	// Backup readiness is detected once at startup — restic's version (empty if
	// absent) and whether a rest-server binary is present to host a vault — and
	// advertised in /vitals so the console can generate the exact setup command.
	// Detecting once is enough because the generated command restarts the agent,
	// which re-detects; per-poll shelling would be wasteful.
	resticVersion, _ := restic.Available(context.Background())
	backupCap := &vitals.BackupCapability{
		Enabled: backupEnabled,
		Restic:  resticVersion,
		Vault:   hasRestServer(),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vitals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap := vitals.Collect()
		snap.Backup = backupCap
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			log.Printf("encode vitals: %v", err)
		}
	})
	// /top is read-only host telemetry, the live-detail companion to /vitals
	// (per-core load + the busiest processes). Like /vitals it's ungated — it
	// exposes the same tier of information as the services list /vitals already
	// carries — so the console's CPU/network drill-down works on every agent
	// without a new flag or restart.
	mux.HandleFunc("/top", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vitals.CollectTop(topProcLimit)); err != nil {
			log.Printf("encode top: %v", err)
		}
	})
	mux.HandleFunc("/browse", handleBrowse)
	mux.HandleFunc("/du", handleDu)
	mux.HandleFunc("/file", handleFile)
	// Ungated on purpose — unlike /backups it neither runs restic nor exposes a
	// secret, it just reads status files a root-run backup left behind. See
	// backupstatus.go.
	mux.HandleFunc("/backup-status", handleConventionBackupStatus(*backupStatusDir))
	// /backups is always routed so a box with backups off returns a clear
	// "disabled" (403) rather than a bare 404, indistinguishable from an agent too
	// old to have it.
	backupDisabled := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "backups are disabled on this agent (start with -backup to enable)", http.StatusForbidden)
	}
	if backupEnabled {
		mux.HandleFunc("/backups", backups.handleBackups)
		mux.HandleFunc("/backups/{id}", backups.handleBackup)
		mux.HandleFunc("/backups/{id}/run", backups.handleBackupRun)
		mux.HandleFunc("/backups/{id}/snapshots", backups.handleBackupSnapshots)
		mux.HandleFunc("/backups/{id}/snapshots/{snap}/ls", backups.handleBackupSnapshotLS)
		mux.HandleFunc("/backups/{id}/restore", backups.handleBackupRestore)
	} else {
		mux.HandleFunc("/backups", backupDisabled)
		mux.HandleFunc("/backups/{id}", backupDisabled)
		mux.HandleFunc("/backups/{id}/run", backupDisabled)
		mux.HandleFunc("/backups/{id}/snapshots", backupDisabled)
		mux.HandleFunc("/backups/{id}/snapshots/{snap}/ls", backupDisabled)
		mux.HandleFunc("/backups/{id}/restore", backupDisabled)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	if backupEnabled {
		if resticVersion != "" {
			log.Printf("hush-agent: /backups enabled — %s", resticVersion)
		} else {
			log.Printf("hush-agent: /backups enabled but restic was not found on $PATH — installs of a backup will fail until it is")
		}
	} else {
		log.Printf("hush-agent: /backups disabled (start with -backup to enable)")
	}
	log.Printf("hush-agent listening on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
}

// resolveStateDir picks where persisted agent state lives. An explicit -state-dir
// wins; otherwise systemd's $STATE_DIRECTORY (populated by StateDirectory=hush,
// which also creates it 0700 hush:hush and keeps it writable under
// ProtectSystem=strict) is used; failing both — e.g. a manual run outside
// systemd — it falls back to /var/lib/hush. $STATE_DIRECTORY may list several
// paths colon-separated; the first is ours.
func resolveStateDir(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
		if i := strings.IndexByte(sd, ':'); i >= 0 {
			return sd[:i]
		}
		return sd
	}
	return "/var/lib/hush"
}

// hasRestServer reports whether a rest-server binary is on this box's PATH, so
// /vitals can advertise that the box could host a backup repository (a "vault").
// It's a presence hint for the console's setup helper, not a guarantee one is
// running — the create flow still proves the repo is actually reachable.
func hasRestServer() bool {
	_, err := exec.LookPath("rest-server")
	return err == nil
}

// handleBrowse serves a read-only directory listing for the Store construct.
// There is no jail: any absolute path is listed, bounded only by what the
// unprivileged "hush" user can read. The OS's own errors decide the outcome —
// permission denied and no-such-dir map to 403 and 404 so the console can tell
// "you can't see this" apart from "this isn't here".
func handleBrowse(w http.ResponseWriter, r *http.Request) {
	listing, err := browse.List(r.URL.Query().Get("path"))
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case os.IsPermission(err):
			status = http.StatusForbidden
		case os.IsNotExist(err):
			status = http.StatusNotFound
		case errors.Is(err, os.ErrInvalid), errors.Is(err, syscall.ENOTDIR):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(listing); err != nil {
		log.Printf("encode browse: %v", err)
	}
}

// duDeadline bounds a single /du walk. It's generous — sizing a directory
// tree means statting every file under it, which on a NAS-sized volume can
// take real time — but finite, so a request against something enormous still
// gets a (Truncated) answer back rather than hanging until the client gives up.
// topProcLimit caps how many processes /top returns — enough to fill an
// htop-style table without shipping a box's entire process list every poll.
const topProcLimit = 30

const duDeadline = 25 * time.Second

// handleDu serves recursive directory sizes for one level of the Store
// construct's treemap view, one level at a time like /browse. Same unjailed,
// OS-permission-bounded model and error mapping as handleBrowse; the walk
// itself is bounded by duDeadline rather than the request's own context, so a
// slow client disconnecting mid-walk doesn't change the answer.
func handleDu(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), duDeadline)
	defer cancel()
	listing, err := browse.Du(ctx, r.URL.Query().Get("path"))
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case os.IsPermission(err):
			status = http.StatusForbidden
		case os.IsNotExist(err):
			status = http.StatusNotFound
		case errors.Is(err, os.ErrInvalid), errors.Is(err, syscall.ENOTDIR):
			status = http.StatusBadRequest
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(listing); err != nil {
		log.Printf("encode du: %v", err)
	}
}

// handleFile streams a single file's contents — the "open it" half of the Store
// construct. Like /browse it is unjailed and bounded only by the hush user's
// read permission (permission denied → 403, missing → 404, a directory → 400).
// It leans on http.ServeContent, which handles Range requests (so a phone can
// seek within a video), Content-Type by extension, and If-Modified-Since for
// free. Pass ?download=1 to force a save dialog instead of inline rendering.
func handleFile(w http.ResponseWriter, r *http.Request) {
	path := filepath.Clean(r.URL.Query().Get("path"))
	if !filepath.IsAbs(path) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	f, err := os.Open(path)
	if err != nil {
		status := http.StatusInternalServerError
		switch {
		case os.IsPermission(err):
			status = http.StatusForbidden
		case os.IsNotExist(err):
			status = http.StatusNotFound
		}
		http.Error(w, err.Error(), status)
		return
	}
	defer f.Close()

	info, err := f.Stat()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if info.IsDir() {
		http.Error(w, "is a directory — use /browse", http.StatusBadRequest)
		return
	}
	if r.URL.Query().Get("download") != "" {
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", info.Name()))
	}
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

// runSelfUpdate performs a one-shot self-update and returns a process exit
// code. It is the entry point for `hush-agent -self-update`, invoked as root by
// hush-agent-update.service. The long-lived agent stays unprivileged (the hush
// user) and never calls GitHub itself; this root oneshot is the only piece that
// reaches out and rewrites the binary. On a successful swap it restarts the
// running service so the new binary takes over.
func runSelfUpdate() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := updater.SelfUpdate(ctx, client, "hush-agent")
	if err != nil {
		log.Printf("self-update: %v", err)
		return 1
	}
	if !res.Updated {
		log.Printf("self-update: already at the latest release (%s)", res.From)
		return 0
	}
	log.Printf("self-update: %s -> %s; restarting service", res.From, res.To)
	if err := restartService(ctx); err != nil {
		// The binary is already swapped; the next restart picks it up. Surface
		// the failure but don't pretend the update didn't happen.
		log.Printf("self-update: replaced binary but restart failed: %v", err)
		return 1
	}
	return 0
}

// restartService bounces hush-agent.service so the freshly swapped binary is
// what runs. try-restart is a no-op for an inactive unit, and a "not found"
// (the agent wasn't installed as a systemd service) is treated as success:
// there's nothing to restart, and the swapped binary is picked up whenever the
// operator next starts it.
func restartService(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "try-restart", "hush-agent.service")
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	msg := strings.TrimSpace(string(out))
	if strings.Contains(msg, "not found") {
		return nil // not installed as a service on this box
	}
	return fmt.Errorf("systemctl try-restart hush-agent.service: %v: %s", err, msg)
}
