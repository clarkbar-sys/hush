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
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/clarkbar-sys/hush/internal/browse"
	hexec "github.com/clarkbar-sys/hush/internal/exec"
	"github.com/clarkbar-sys/hush/internal/updater"
	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
)

func main() {
	listen := flag.String("listen", ":8765", `address to listen on, or "tailnet" to bind this machine's Tailscale IP (tailnet-only; "tailnet:PORT" for a non-default port)`)
	showVersion := flag.Bool("version", false, "print the hush-agent version and exit")
	selfUpdate := flag.Bool("self-update", false, "check for a newer release and replace this binary in place, then exit (run as root by hush-agent-update.service)")
	allowExec := flag.Bool("exec", true, "serve /exec, the Task construct's one-shot command runner (on by default; -exec=false disables). Commands run as the unprivileged hush user")
	allowJobs := flag.Bool("jobs", false, "serve /jobs, the Job construct's cron scheduler (off by default; -jobs enables). Jobs fire unattended as the unprivileged hush user")
	runAsFlag := flag.String("run-as", "", "comma-separated OS users a Task may run as via `sudo -u` (e.g. media,deploy). Empty = off. Needs a matching sudoers grant; never list root or a sudo-capable user")
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

	// The run-as allowlist: users a Task may become via `sudo -u`. The env var
	// wins over the flag so the systemd unit's env file can set it without
	// editing ExecStart, mirroring HUSH_AGENT_EXEC/JOBS. Malformed names are
	// dropped with a warning rather than aborting the agent — one typo in the
	// list shouldn't keep the box's agent from booting.
	runAsSpec := *runAsFlag
	if v, ok := os.LookupEnv("HUSH_AGENT_RUNAS"); ok {
		runAsSpec = v
	}
	runAs := parseRunAs(runAsSpec)

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

	// advertisedRunAs is the run-as allowlist as reported in /vitals, so the
	// console can offer a per-machine picker. It's only meaningful with exec on,
	// so a box with exec disabled advertises none even if -run-as was set.
	var advertisedRunAs []string
	// runAsCheck verifies the advertised users against the box's real sudoers
	// grant so /vitals can report which are actually runnable. It's only built
	// when the feature is on (exec enabled with a non-empty list); otherwise the
	// snapshot leaves RunAsGranted nil and the console makes no claim.
	var runAsCheck *runAsChecker
	if execEnabled {
		advertisedRunAs = sortedKeys(runAs)
		if len(advertisedRunAs) > 0 {
			runAsCheck = newRunAsChecker(advertisedRunAs)
		}
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/vitals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		snap := vitals.Collect()
		snap.RunAs = advertisedRunAs
		if runAsCheck != nil {
			g := runAsCheck.granted()
			snap.RunAsGranted = &g
		}
		if err := json.NewEncoder(w).Encode(snap); err != nil {
			log.Printf("encode vitals: %v", err)
		}
	})
	mux.HandleFunc("/browse", handleBrowse)
	mux.HandleFunc("/file", handleFile)
	// /exec is always routed so a box that opted out returns a clear "disabled"
	// rather than a bare 404 (which would be indistinguishable from an old agent).
	execHandle := execHandler(runAs)
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if !execEnabled {
			http.Error(w, "exec is disabled on this agent (started with -exec=false)", http.StatusForbidden)
			return
		}
		execHandle(w, r)
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
		if len(runAs) > 0 {
			log.Printf("hush-agent: run-as users allowed via sudo -u: %s", strings.Join(sortedKeys(runAs), ", "))
		}
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

// parseRunAs turns the comma-separated -run-as / HUSH_AGENT_RUNAS value into the
// set of users /exec will honour. Entries are trimmed and de-duplicated; a name
// that isn't a syntactically valid username is dropped with a warning rather
// than aborting the agent, so one typo can't keep the box's agent from booting.
// It deliberately does not check whether each user exists on the box — a name
// may be allowed before it's created (sudo reports "unknown user" at run time).
func parseRunAs(spec string) map[string]bool {
	set := make(map[string]bool)
	for _, raw := range strings.Split(spec, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		if !hexec.ValidUserName(name) {
			log.Printf("hush-agent: ignoring invalid -run-as user %q", name)
			continue
		}
		set[name] = true
	}
	return set
}

// sortedKeys returns a set's keys in a stable order, only for tidy log output.
func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
