// Command hush-agent runs on every machine in the fleet. It exposes the host's
// vitals as JSON over the tailnet, and serves /exec — the Task construct's
// one-shot command runner — on by default. A box can opt out with -exec=false
// (or HUSH_AGENT_EXEC=0), after which /exec returns 403 and everything else
// stays read-only.
//
// It can also serve /jobs — the Job construct's cron scheduler, which fires
// saved commands as the hush user on a schedule. Unlike /exec it is OFF by
// default (a Job runs unattended), enabled with -jobs or HUSH_AGENT_JOBS=1;
// definitions persist to jobs.json under the agent's state directory.
//
// Deploy is one static binary with no runtime dependencies:
//
//	GOOS=linux GOARCH=arm64 go build ./cmd/hush-agent   # e.g. for the Pi
//	scp hush-agent pi-gate:/usr/local/bin/
package main

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"syscall"

	"github.com/clarkbar-sys/hush/internal/browse"
	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
)

func main() {
	listen := flag.String("listen", ":8765", `address to listen on, or "tailnet" to bind this machine's Tailscale IP (tailnet-only; "tailnet:PORT" for a non-default port)`)
	showVersion := flag.Bool("version", false, "print the hush-agent version and exit")
	allowExec := flag.Bool("exec", true, "serve /exec, the Task construct's one-shot command runner (on by default; -exec=false disables). Commands run as the unprivileged hush user")
	allowJobs := flag.Bool("jobs", false, "serve /jobs, the Job construct's cron scheduler (off by default; -jobs enables). Jobs fire unattended as the unprivileged hush user")
	stateDir := flag.String("state-dir", "", "directory for persisted state such as jobs.json (default: $STATE_DIRECTORY from systemd, else /var/lib/hush)")
	flag.Parse()

	// Exec is on by default; a box can opt out with -exec=false or, so the
	// systemd unit's env file can toggle it without editing ExecStart, by setting
	// HUSH_AGENT_EXEC to a falsey value. A present env var always wins over the
	// flag default.
	execEnabled := *allowExec
	if v, ok := os.LookupEnv("HUSH_AGENT_EXEC"); ok {
		execEnabled = v != "0" && v != "false" && v != "no"
	}

	// Jobs is OFF by default — unlike /exec, a Job fires unattended, so gaining a
	// scheduled command runner should be a deliberate choice, not a side effect of
	// installing the agent. The same env-over-flag toggle lets the unit's env file
	// flip it (HUSH_AGENT_JOBS=1) without editing ExecStart.
	jobsEnabled := *allowJobs
	if v, ok := os.LookupEnv("HUSH_AGENT_JOBS"); ok {
		jobsEnabled = v == "1" || v == "true" || v == "yes"
	}

	if *showVersion {
		fmt.Printf("hush-agent %s\n", version.Current())
		os.Exit(0)
	}

	listenAddr, err := resolveListen(*listen)
	if err != nil {
		log.Fatalf("hush-agent: %v", err)
	}

	vitals.StartSampler()

	// The Job scheduler is only built when jobs are enabled, so a default agent
	// touches no disk and runs no scheduled commands. resolveStateDir prefers
	// systemd's $STATE_DIRECTORY (set by StateDirectory=hush) and falls back to
	// /var/lib/hush; the store tolerates a missing file, so a first run with no
	// jobs.json simply starts empty.
	var sched *scheduler
	if jobsEnabled {
		dir := resolveStateDir(*stateDir)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			log.Printf("hush-agent: state dir %s not writable: %v — job creation will fail until it is", dir, err)
		}
		sched = newScheduler(filepath.Join(dir, "jobs.json"))
		sched.Start()
		defer sched.Stop()
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vitals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vitals.Collect()); err != nil {
			log.Printf("encode vitals: %v", err)
		}
	})
	mux.HandleFunc("/browse", handleBrowse)
	mux.HandleFunc("/file", handleFile)
	// /exec is always routed so a box that opted out returns a clear "disabled"
	// rather than a bare 404 (which would be indistinguishable from an old agent).
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if !execEnabled {
			http.Error(w, "exec is disabled on this agent (started with -exec=false)", http.StatusForbidden)
			return
		}
		handleExec(w, r)
	})
	// /jobs is always routed so a box with jobs off returns a clear "disabled"
	// rather than a bare 404 (indistinguishable from an agent too old to have it).
	jobsDisabled := func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "jobs are disabled on this agent (start with -jobs to enable)", http.StatusForbidden)
	}
	if jobsEnabled {
		mux.HandleFunc("/jobs", sched.handleJobs)
		mux.HandleFunc("/jobs/{id}", sched.handleJob)
	} else {
		mux.HandleFunc("/jobs", jobsDisabled)
		mux.HandleFunc("/jobs/{id}", jobsDisabled)
	}
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	if execEnabled {
		log.Printf("hush-agent: /exec enabled — one-shot commands run as uid %d", os.Geteuid())
	} else {
		log.Printf("hush-agent: /exec disabled (-exec=false)")
	}
	if jobsEnabled {
		log.Printf("hush-agent: /jobs enabled — scheduled commands run as uid %d", os.Geteuid())
	} else {
		log.Printf("hush-agent: /jobs disabled (start with -jobs to enable)")
	}
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
