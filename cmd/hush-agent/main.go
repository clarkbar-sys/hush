// Command hush-agent runs on every machine in the fleet. It exposes the host's
// vitals as JSON over the tailnet and serves a read-only view of the box —
// /vitals, /top, /browse, /du, /file, and /backup-status.
//
// Backups are set up on the box itself (root, over SSH — see
// docs/BACKUP-CONVENTION.md); the agent only ever *reads* their status files and
// reports them on /backup-status, so it never holds a repository credential.
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
	"github.com/clarkbar-sys/hush/internal/llm"
	"github.com/clarkbar-sys/hush/internal/updater"
	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
)

func main() {
	listen := flag.String("listen", ":8765", `address to listen on, or "tailnet" to bind this machine's Tailscale IP (tailnet-only; "tailnet:PORT" for a non-default port)`)
	showVersion := flag.Bool("version", false, "print the hush-agent version and exit")
	selfUpdate := flag.Bool("self-update", false, "check for a newer release and replace this binary in place, then exit (run as root by hush-agent-update.service)")
	backupStatusDir := flag.String("backup-status-dir", defaultBackupStatusDir, "directory of status files written by root-run convention backups (see docs/BACKUP-CONVENTION.md). Read-only; served on /backup-status")
	duCacheTTL := flag.Duration("du-cache-ttl", time.Hour, "how long a /du sizing result stays fresh before it's recomputed on the next request (0 disables the cache — every /du re-walks)")
	duRefresh := flag.Duration("du-refresh", time.Hour, "how often to re-size recently-viewed /du paths in the background so the treemap loads warm (0 disables background refresh)")
	llmPorts := flag.String("llm-ports", strings.Join(llm.DefaultPorts, ","), "comma-separated ports to scan for local LLM runtimes (llama-swap/OpenAI-compatible or Ollama); whatever address a runtime is bound to on one of these is probed there. Empty disables discovery")
	llmEndpoints := flag.String("llm-endpoints", "", "comma-separated host:port targets that replace -llm-ports discovery, for a runtime on a non-standard port. Empty means discover")
	llmInterval := flag.Duration("llm-interval", 2*time.Minute, "how often to re-probe the LLM endpoints, so a hot-reloaded model catalogue doesn't go stale")
	flag.Parse()

	if *showVersion {
		fmt.Printf("hush-agent %s\n", version.Current())
		os.Exit(0)
	}
	if *selfUpdate {
		os.Exit(runSelfUpdate())
	}

	listenAddr, err := resolveListen(*listen)
	if err != nil {
		log.Fatalf("hush-agent: %v", err)
	}

	vitals.StartSampler()

	// The treemap re-walks a tree on every open, which is slow on a NAS-sized
	// volume. duCache memoizes each directory's sizing so reopening — or
	// drilling back — is instant, and the background refresher keeps
	// recently-viewed paths warm so even a first open of the session lands on a
	// recent number rather than a cold 25s walk. Both are on by default and can
	// be tuned or turned off with the flags above.
	duCache := browse.NewDuCache(*duCacheTTL, 0)
	go duCache.StartRefresher(context.Background(), *duRefresh, duDeadline, duRefreshRetain)

	// Local inference is detected in the background and reported on /vitals, so
	// the console can show which boxes serve models — and, just as importantly,
	// whether anything off-box can reach them. Detection reads the kernel
	// listener table to find where each runtime is actually bound, so exposing a
	// runtime doesn't hide it: a loopback-only probe would stop finding a
	// runtime the moment someone moved it onto the tailnet, reporting *less*
	// capability precisely when there is more. Probing is read-only HTTP plus a
	// /proc read, so it needs no privilege. Clearing both flags turns detection
	// off, which leaves the field absent rather than reporting an empty
	// catalogue — "this agent doesn't report" is not "this box has none".
	llmOpts := llm.Options{
		Ports:     splitList(*llmPorts),
		Endpoints: splitList(*llmEndpoints),
	}
	if llmOpts.Enabled() {
		llm.StartProbe(context.Background(), llmOpts, *llmInterval)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vitals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap := vitals.Collect()
		snap.LLM = llm.Current()
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			log.Printf("encode vitals: %v", err)
		}
	})
	// /top is read-only host telemetry, the live-detail companion to /vitals
	// (per-core load + the busiest processes). Like /vitals it's ungated — it
	// exposes the same tier of information /vitals already carries — so the
	// console's CPU/network drill-down works on every agent without a new
	// flag or restart.
	mux.HandleFunc("/top", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vitals.CollectTop(topProcLimit)); err != nil {
			log.Printf("encode top: %v", err)
		}
	})
	mux.HandleFunc("/browse", handleBrowse)
	mux.HandleFunc("/du", handleDu(duCache))
	mux.HandleFunc("/file", handleFile)
	// Read-only status of root-run convention backups. Ungated on purpose — it
	// neither runs restic nor exposes a secret, it just reads the status files a
	// root-run backup left behind. See backupstatus.go and docs/BACKUP-CONVENTION.md.
	mux.HandleFunc("/backup-status", handleConventionBackupStatus(*backupStatusDir))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Printf("hush-agent listening on %s", listenAddr)
	log.Fatal(http.ListenAndServe(listenAddr, mux))
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

// duRefreshRetain is how long a browsed /du path stays eligible for background
// re-sizing after it was last looked at. Paths untouched for longer age out of
// the cache instead of being re-walked forever, so the refresher only keeps
// warm what someone has recently shown interest in.
const duRefreshRetain = 6 * time.Hour

// handleDu serves recursive directory sizes for one level of the Store
// construct's treemap view, one level at a time like /browse. Same unjailed,
// OS-permission-bounded model and error mapping as handleBrowse; the walk
// itself is bounded by duDeadline rather than the request's own context, so a
// slow client disconnecting mid-walk doesn't change the answer. Results are
// served through cache, so a repeat open returns instantly; ?refresh=1 forces a
// fresh walk (the console's "re-size" button), bypassing any cached sizing.
func handleDu(cache *browse.DuCache) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(context.Background(), duDeadline)
		defer cancel()
		refresh := r.URL.Query().Get("refresh") != ""
		listing, err := cache.Get(ctx, r.URL.Query().Get("path"), refresh)
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

// splitList parses a comma-separated flag value into trimmed, non-empty
// entries. An empty or all-whitespace value yields nothing, which callers read
// as "this feature is off".
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
