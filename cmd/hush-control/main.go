// Command hush-control is the fleet control plane. It runs on one machine
// (the NAS), fans out to every agent to collect vitals, and serves the web UI.
//
// Agents are listed in a JSON config file (see fleet.example.json). With no
// config it assumes a single local agent, which is handy for development.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/clarkbar-sys/hush/internal/updater"
	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
	"github.com/clarkbar-sys/hush/web"
)

// Agent is one entry in the fleet config.
type Agent struct {
	Name string `json:"name"` // display id; falls back to the host's own hostname
	Addr string `json:"addr"` // base URL of the agent, e.g. http://100.71.8.9:8765
	IP   string `json:"ip"`   // tailnet address, shown in the UI
	Role string `json:"role"` // optional: store, gateway, ...
}

// Machine is the shape the web UI consumes (one entry of /api/fleet).
type Machine struct {
	ID       string           `json:"id"`
	OS       string           `json:"os"`
	IP       string           `json:"ip"`
	Role     string           `json:"role"`
	Status   string           `json:"status"`
	CPU      int              `json:"cpu"`
	Mem      int              `json:"mem"`
	Disk     int              `json:"disk"`
	GPU      *int             `json:"gpu"`
	VRAM     *int             `json:"vram"`
	GPUName  string           `json:"gpuName,omitempty"`
	VRAMText string           `json:"vramText,omitempty"`
	Up       string           `json:"up"`
	Load     string           `json:"load"`
	Services []vitals.Service `json:"services"`
	Jobs     []any            `json:"jobs"`
	Tasks    []any            `json:"tasks"`
	Online   bool             `json:"online"`
	Alert    string           `json:"alert,omitempty"`
}

func main() {
	listen := flag.String("listen", ":8080", "address to serve the console on (LAN mode)")
	configPath := flag.String("config", "fleet.json", "path to the fleet config JSON")
	webDir := flag.String("web", "", "serve UI assets from this directory instead of the embedded ones (dev)")

	// tsnet mode: join the tailnet as our own node and serve HTTPS on :443.
	// Off by default — LAN mode is unchanged when -tsnet is unset.
	useTsnet := flag.Bool("tsnet", false, "join the tailnet as our own node and serve HTTPS on :443")
	hostname := flag.String("hostname", "hush", "tsnet node hostname (tsnet mode)")
	stateDir := flag.String("state-dir", "", "directory to persist tsnet node state (tsnet mode; default: OS config dir)")
	var allow stringList
	flag.Var(&allow, "allow", "allowed caller login, e.g. login@example.com; repeatable; empty = any tailnet member (tsnet mode)")
	showVersion := flag.Bool("version", false, "print the hush-control version and exit")
	selfUpdate := flag.Bool("self-update", false, "check for a newer release and replace this binary in place, then exit (run as root by hush-control-update.service)")
	flag.Parse()

	if *showVersion {
		fmt.Printf("hush-control %s\n", version.Current())
		os.Exit(0)
	}
	if *selfUpdate {
		os.Exit(runSelfUpdate())
	}

	agents := loadAgents(*configPath)
	log.Printf("hush-control: %d agent(s) configured", len(agents))

	mux := buildMux(agents, *webDir)

	if *useTsnet {
		serveTsnet(mux, *listen, *hostname, *stateDir, allow)
		return
	}

	log.Printf("hush-control serving on %s (LAN mode)", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}

// buildMux wires the console routes: live fleet JSON plus the static UI. It is
// transport-agnostic, so the same handler serves both LAN and tsnet modes.
// The UI is served from the embedded assets unless webDir is set (dev override).
func buildMux(agents []Agent, webDir string) http.Handler {
	client := &http.Client{Timeout: 2 * time.Second}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/fleet", func(w http.ResponseWriter, r *http.Request) {
		fleet := collectFleet(client, agents)
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fleet); err != nil {
			log.Printf("encode fleet: %v", err)
		}
	})
	vc := &versionChecker{client: &http.Client{Timeout: 5 * time.Second}}
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vc.status(r.Context())); err != nil {
			log.Printf("encode version: %v", err)
		}
	})
	if webDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
	} else {
		mux.Handle("/", http.FileServerFS(web.FS))
	}
	return mux
}

// stringList is a repeatable string flag (e.g. -allow a -allow b).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// collectFleet queries every agent concurrently, preserving config order.
func collectFleet(client *http.Client, agents []Agent) []Machine {
	out := make([]Machine, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a Agent) {
			defer wg.Done()
			out[i] = fetchOne(client, a)
		}(i, a)
	}
	wg.Wait()
	return out
}

func fetchOne(client *http.Client, a Agent) Machine {
	// Default to the "unreachable" state; a successful fetch overwrites it.
	m := Machine{
		ID:       a.Name,
		IP:       a.IP,
		Role:     a.Role,
		Status:   "crit",
		Alert:    "unreachable",
		Services: []vitals.Service{},
		Jobs:     []any{},
		Tasks:    []any{},
	}
	if m.ID == "" {
		m.ID = a.Addr
	}

	resp, err := client.Get(strings.TrimRight(a.Addr, "/") + "/vitals")
	if err != nil {
		return m
	}
	defer resp.Body.Close()

	var s vitals.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return m
	}

	m.Online = true
	m.Alert = ""
	m.Status = s.Status
	if a.Name == "" && s.Host != "" {
		m.ID = s.Host
	}
	m.OS, m.Up, m.Load = s.OS, s.Up, s.Load
	m.CPU, m.Mem, m.Disk = s.CPU, s.Mem, s.Disk
	m.GPU, m.VRAM, m.GPUName, m.VRAMText = s.GPU, s.VRAM, s.GPUName, s.VRAMText
	if len(s.Services) > 0 {
		m.Services = s.Services
	}
	for _, sv := range s.Services {
		if sv.State == "failed" {
			m.Alert = sv.Name + ".service failed"
			break
		}
	}
	return m
}

func loadAgents(path string) []Agent {
	b, err := os.ReadFile(path)
	if err != nil {
		log.Printf("no config at %s; using a single local agent", path)
		return []Agent{{Addr: "http://127.0.0.1:8765", IP: "127.0.0.1"}}
	}
	var agents []Agent
	if err := json.Unmarshal(b, &agents); err != nil {
		log.Fatalf("parse %s: %v", path, err)
	}
	return agents
}

// VersionStatus is what /api/version returns, so the console can show the
// running version and whether a newer release is available.
type VersionStatus struct {
	Current         string `json:"current"`             // running version, e.g. "v1.2.0" or "dev"
	Latest          string `json:"latest,omitempty"`    // latest released version, when the check succeeded
	UpdateAvailable bool   `json:"updateAvailable"`     // true when Latest is newer than Current
	CheckedAt       string `json:"checkedAt,omitempty"` // RFC3339 time of the last successful upstream check
	Error           string `json:"error,omitempty"`     // populated when the upstream check failed
}

// versionChecker serves /api/version, caching the upstream GitHub lookup so a
// busy console never hammers the API (and stays clear of its unauthenticated
// rate limit). Only hush-control performs this check; agents never reach out.
type versionChecker struct {
	client *http.Client

	mu       sync.Mutex
	cached   VersionStatus
	cachedAt time.Time
}

// versionTTL is how long a successful check is reused before we ask GitHub again.
const versionTTL = time.Hour

func (v *versionChecker) status(ctx context.Context) VersionStatus {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !v.cachedAt.IsZero() && time.Since(v.cachedAt) < versionTTL && v.cached.Error == "" {
		return v.cached
	}

	current, latest, avail, err := updater.Check(ctx, v.client, "hush-control")
	st := VersionStatus{Current: current, Latest: latest, UpdateAvailable: avail}
	if err != nil {
		// Keep serving the last good answer if we have one; just note the error.
		st.Error = err.Error()
		if v.cached.Latest != "" {
			st.Latest, st.UpdateAvailable, st.CheckedAt = v.cached.Latest, v.cached.UpdateAvailable, v.cached.CheckedAt
		}
		v.cached = st
		return st
	}
	st.CheckedAt = time.Now().UTC().Format(time.RFC3339)
	v.cached, v.cachedAt = st, time.Now()
	return st
}

// runSelfUpdate performs a one-shot self-update and returns a process exit
// code. It is the entry point for `hush-control -self-update`, invoked as root
// by hush-control-update.service. On a successful swap it restarts the running
// service so the new binary takes over.
func runSelfUpdate() int {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := &http.Client{Timeout: 2 * time.Minute}
	res, err := updater.SelfUpdate(ctx, client, "hush-control")
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

// restartService bounces whichever control unit is active so the freshly
// swapped binary is what runs. try-restart is a no-op for an inactive unit, so
// naming both LAN and tsnet units restarts exactly the one that is running.
func restartService(ctx context.Context) error {
	cmd := exec.CommandContext(ctx, "systemctl", "try-restart",
		"hush-control.service", "hush-control-tsnet.service")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl try-restart: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
