// Command hush-control is the fleet control plane. It runs on one machine
// (the NAS), fans out to every agent to collect vitals, and serves the web UI.
//
// Agents are listed in a JSON config file (see fleet.example.json). With no
// config it assumes a single local agent, which is handy for development.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"mime"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/clarkbar-sys/hush/internal/store"
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
	ID                   string                `json:"id"`
	AgentVersion         string                `json:"agentVersion,omitempty"`
	LatestVersion        string                `json:"latestVersion,omitempty"`        // latest published release, when known
	AgentUpdateAvailable bool                  `json:"agentUpdateAvailable,omitempty"` // true when AgentVersion is older than LatestVersion
	OS                   string                `json:"os"`
	IP                   string                `json:"ip"`
	Role                 string                `json:"role"`
	Status               string                `json:"status"`
	CPU                  int                   `json:"cpu"`
	Mem                  int                   `json:"mem"`
	Disk                 int                   `json:"disk"`
	GPU                  *int                  `json:"gpu"`
	VRAM                 *int                  `json:"vram"`
	GPUName              string                `json:"gpuName,omitempty"`
	VRAMText             string                `json:"vramText,omitempty"`
	Up                   string                `json:"up"`
	Load                 string                `json:"load"`
	NetRx                int                   `json:"netRx"`         // inbound bytes/sec
	NetTx                int                   `json:"netTx"`         // outbound bytes/sec
	LLM                  *vitals.LLMCapability `json:"llm,omitempty"` // local inference runtimes and how far each is reachable; nil = agent doesn't report it
	Online               bool                  `json:"online"`
	Alert                string                `json:"alert,omitempty"`
}

// Report is the downloadable fleet snapshot served by /api/report: the same
// per-machine data the console renders, wrapped with a timestamp and the
// control-plane version so the file stands alone as an artifact for offline
// analysis (e.g. handing it to an agent).
type Report struct {
	GeneratedAt    string    `json:"generatedAt"`    // RFC3339 UTC, when the snapshot was taken
	ControlVersion string    `json:"controlVersion"` // hush-control build version
	MachineCount   int       `json:"machineCount"`
	Machines       []Machine `json:"machines"`
}

func main() {
	listen := flag.String("listen", ":8080", "plain-HTTP LAN address for the first-run setup page only, served until the tailnet node is provisioned (then the console is tailnet-HTTPS only)")
	configPath := flag.String("config", "fleet.json", "path to the fleet config JSON")
	webDir := flag.String("web", "", "serve UI assets from this directory instead of the embedded ones (dev)")

	// hush-control serves the console only over the tailnet (tsnet): it joins the
	// tailnet as its own node and serves HTTPS on :443, gated on Tailscale
	// identity. The old plain-HTTP LAN mode has been removed. -tsnet is kept as an
	// accepted no-op so units that still pass it (and older ones that don't) both
	// keep working across a binary-only auto-update, without a flag-parse crash.
	useTsnet := flag.Bool("tsnet", false, "deprecated and ignored: serving over the tailnet is now the only mode (kept for compatibility)")
	hostname := flag.String("hostname", "hush", "tsnet node hostname")
	stateDir := flag.String("state-dir", "", "directory to persist tsnet node state (default: OS config dir)")
	var allow stringList
	flag.Var(&allow, "allow", "allowed caller login, e.g. login@example.com; repeatable; empty = any tailnet member")
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
	store := newAgentStore(*configPath, agents)

	// dialer routes every connection to an agent. In tsnet mode the control plane
	// is its own userspace tailnet node, so it must reach agents through that node
	// (not the host kernel, which has no route to their 100.x addresses); useTsnet
	// flips it there once the node is up. See agentdial.go.
	dialer := newAgentDialer()

	// disco starts empty; it's populated with a tailnet peer lister once the tsnet
	// node is up (before that — during first-run setup — it stays nil and
	// discovery reports itself unavailable). The discoverer polls it in the
	// background so the console can badge newly appeared agents without re-probing
	// the tailnet on every request. Its probe rides the shared dialer so candidate
	// checks reach 100.x agents over the tailnet, same as the fleet poll.
	disco := &discoverySource{}
	discoClient := dialer.client(2 * time.Second)
	probe := func(addr string) testAgentResult { return testAgent(discoClient, addr) }
	discoverer := newDiscoverer(disco, probe, store.Snapshot)
	go discoverer.run(context.Background())

	mux, fc := buildMux(store, discoverer, dialer, *webDir)
	go fc.run(context.Background())

	// The console is served only over the tailnet now — plain-HTTP LAN mode has
	// been removed. -tsnet is ignored (serving over the tailnet is unconditional);
	// note it for anyone whose old LAN unit dropped the flag after a binary update.
	if !*useTsnet {
		log.Printf("hush-control: plain-HTTP LAN mode has been removed — serving over the tailnet (tsnet). See the README to provision the node.")
	}
	serveTsnet(mux, disco, dialer, *listen, *hostname, *stateDir, allow)
}

// buildMux wires the console routes: live fleet JSON, fleet membership, and
// the static UI. It is transport-agnostic — the same handler is served over the
// tsnet HTTPS listener (and the plain-HTTP first-run setup page reuses the same
// wiring only until the node is provisioned). The UI is served from the embedded assets unless
// webDir is set (dev override). It also returns the fleetCache it built, so
// the caller can start its background polling loop (main() does; tests that
// don't care about fleet polling can discard it — /api/fleet still works,
// falling back to a live scan on the cold cache).
func buildMux(store *agentStore, discoverer *discoverer, dialer *agentDialer, webDir string) (http.Handler, *fleetCache) {
	// Go's mime table doesn't know .webmanifest; without this the PWA manifest
	// is served as text/plain and browsers grumble. Register the correct type
	// so http.FileServer(FS) picks it up.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")

	// A nil dialer (tests that don't exercise tsnet routing) dials directly,
	// which is what a bare http.Client did before and is right for loopback
	// httptest agents.
	if dialer == nil {
		dialer = newAgentDialer()
	}

	// Every agent-facing client rides the shared dialer so it reaches agents over
	// the tailnet node in tsnet mode. The version checker is the one exception: it
	// talks to GitHub, not an agent, so it keeps a plain client on the host stack.
	client := dialer.client(2 * time.Second)
	vc := &versionChecker{client: &http.Client{Timeout: 5 * time.Second}}
	fc := newFleetCache(client, store.Snapshot, func(ctx context.Context) string {
		return vc.status(ctx, false).Latest
	})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/fleet", func(w http.ResponseWriter, r *http.Request) {
		fleet, ok := fc.snapshot()
		if !ok {
			fleet = fc.scan(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(fleet); err != nil {
			log.Printf("encode fleet: %v", err)
		}
	})
	// Fleet-wide backup status. A backup is a direction (this box → that store),
	// so the useful question is fleet-shaped — see backupstatus.go for why this
	// aggregates rather than proxying per machine.
	mux.HandleFunc("/api/backup-status", handleBackupStatus(client, store.Snapshot))
	mux.HandleFunc("/api/machines/{host}/browse", func(w http.ResponseWriter, r *http.Request) {
		a, ok := store.find(r.PathValue("host"))
		if !ok {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		proxyBrowse(w, r, client, a)
	})
	// /top samples /proc twice (see the agent's topSampleInterval), so a call
	// takes a beat longer than a plain read — it rides a client with a little
	// more headroom than the 2s fleet-poll one so that sampling delay plus a
	// slow hop doesn't clip an otherwise-healthy response.
	topClient := dialer.client(4 * time.Second)
	mux.HandleFunc("/api/machines/{host}/top", func(w http.ResponseWriter, r *http.Request) {
		a, ok := store.find(r.PathValue("host"))
		if !ok {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		proxyTop(w, r, topClient, a)
	})
	// /sessions is a cheap /proc read (no double-sample like /top), so it rides
	// the standard fleet-poll client. It lists the box's running coding agents so
	// the console can show them and offer a stop command; an agent too old to
	// serve it answers 404, relayed as-is.
	mux.HandleFunc("/api/machines/{host}/sessions", func(w http.ResponseWriter, r *http.Request) {
		a, ok := store.find(r.PathValue("host"))
		if !ok {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		proxySessions(w, r, client, a)
	})
	// Streaming a file can take far longer than the 2s fleet-poll budget (a
	// whole video), so it rides its own client with no overall timeout.
	streamClient := dialer.client(0)
	// A recursive size walk can run for the agent's whole duDeadline (25s), far
	// past the 2s fleet-poll budget, so /du rides its own client with a timeout
	// just past that deadline rather than the no-timeout streamClient — a du
	// call that hangs (agent wedged, network gone) should still fail, just not
	// as impatiently as a fleet poll would.
	duClient := dialer.client(30 * time.Second)
	mux.HandleFunc("/api/machines/{host}/du", func(w http.ResponseWriter, r *http.Request) {
		a, ok := store.find(r.PathValue("host"))
		if !ok {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		proxyDu(w, r, duClient, a)
	})
	mux.HandleFunc("/api/machines/{host}/file", func(w http.ResponseWriter, r *http.Request) {
		a, ok := store.find(r.PathValue("host"))
		if !ok {
			http.Error(w, "unknown machine", http.StatusNotFound)
			return
		}
		proxyFile(w, r, streamClient, a)
	})
	mux.HandleFunc("/api/agents", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Name string `json:"name"`
			Addr string `json:"addr"`
			Role string `json:"role"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		addr := normalizeAddr(req.Addr)
		if addr == "" {
			http.Error(w, "addr is required", http.StatusBadRequest)
			return
		}
		a := Agent{
			Name: strings.TrimSpace(req.Name),
			Addr: addr,
			IP:   hostFromAddr(addr),
			Role: strings.TrimSpace(req.Role),
		}
		added, err := store.Add(a)
		if err != nil {
			status := http.StatusInternalServerError
			if errors.Is(err, errDuplicateAddr) {
				status = http.StatusConflict
			} else {
				log.Printf("add agent: %v", err)
			}
			http.Error(w, err.Error(), status)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(added); err != nil {
			log.Printf("encode agent: %v", err)
		}
	})
	mux.HandleFunc("/api/agents/test", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Addr string `json:"addr"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(testAgent(client, req.Addr)); err != nil {
			log.Printf("encode test result: %v", err)
		}
	})
	mux.HandleFunc("/api/discover", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// The passive badge poll reads the cached result (cheap); the console's
		// explicit "Rescan" asks for a fresh live probe with ?rescan=1. A cold
		// cache falls back to a live scan so the first request isn't empty.
		var result discoverResult
		if r.URL.Query().Get("rescan") == "1" {
			result = discoverer.scan(r.Context())
		} else if cached, ok := discoverer.snapshot(); ok {
			result = cached
		} else {
			result = discoverer.scan(r.Context())
		}
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(result); err != nil {
			log.Printf("encode discover result: %v", err)
		}
	})
	mux.HandleFunc("/api/report", func(w http.ResponseWriter, r *http.Request) {
		now := time.Now().UTC()
		machines := collectFleet(client, store.Snapshot(), vc.status(r.Context(), false).Latest)
		report := Report{
			GeneratedAt:    now.Format(time.RFC3339),
			ControlVersion: version.Current(),
			MachineCount:   len(machines),
			Machines:       machines,
		}
		filename := "hush-fleet-" + now.Format("20060102-150405Z") + ".json"
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", filename))
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			log.Printf("encode report: %v", err)
		}
	})
	mux.HandleFunc("/api/version", func(w http.ResponseWriter, r *http.Request) {
		force := r.URL.Query().Get("force") != ""
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vc.status(r.Context(), force)); err != nil {
			log.Printf("encode version: %v", err)
		}
	})
	if webDir != "" {
		mux.Handle("/", http.FileServer(http.Dir(webDir)))
	} else {
		mux.Handle("/", http.FileServerFS(web.FS))
	}
	return mux, fc
}

// testAgentResult is the response of a reachability probe against a
// candidate agent address, before it's added to the fleet.
type testAgentResult struct {
	OK        bool   `json:"ok"`
	Error     string `json:"error,omitempty"`
	Host      string `json:"host,omitempty"`
	OS        string `json:"os,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

// testAgent probes addr's /vitals endpoint without joining the fleet, so the
// UI can confirm reachability before the operator commits to adding it.
func testAgent(client *http.Client, rawAddr string) testAgentResult {
	addr := normalizeAddr(rawAddr)
	if addr == "" {
		return testAgentResult{Error: "enter an address"}
	}
	start := time.Now()
	resp, err := client.Get(strings.TrimRight(addr, "/") + "/vitals")
	if err != nil {
		return testAgentResult{Error: "couldn't reach that address — check the host:port and that hush-agent is running"}
	}
	defer resp.Body.Close()
	var s vitals.Snapshot
	if err := json.NewDecoder(resp.Body).Decode(&s); err != nil {
		return testAgentResult{Error: "reached that address, but it didn't look like hush-agent"}
	}
	return testAgentResult{
		OK:        true,
		Host:      s.Host,
		OS:        s.OS,
		LatencyMs: time.Since(start).Milliseconds(),
	}
}

// normalizeAddr fills in the http:// scheme when the operator typed a bare
// host:port (the common case), and trims stray whitespace.
func normalizeAddr(addr string) string {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return ""
	}
	if !strings.Contains(addr, "://") {
		addr = "http://" + addr
	}
	return addr
}

// hostFromAddr extracts the bare host (no scheme, no port) from an agent
// address, for display as the machine's IP in the UI.
func hostFromAddr(addr string) string {
	host := strings.TrimPrefix(strings.TrimPrefix(addr, "http://"), "https://")
	host = strings.SplitN(host, "/", 2)[0]
	if i := strings.LastIndex(host, ":"); i != -1 {
		host = host[:i]
	}
	return host
}

// agentStore holds the fleet membership in memory and persists it to the
// config file on every change, so additions survive a hush-control restart.
type agentStore struct {
	mu     sync.RWMutex
	path   string
	agents []Agent
}

func newAgentStore(path string, initial []Agent) *agentStore {
	return &agentStore{path: path, agents: initial}
}

// Snapshot returns a copy of the current fleet, safe to read without holding
// the store's lock.
func (s *agentStore) Snapshot() []Agent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Agent, len(s.agents))
	copy(out, s.agents)
	return out
}

// find resolves a machine identifier from the UI back to an agent. The console
// keys machines by their display name, so that's matched first; the tailnet IP
// is a fallback for agents configured without a name.
func (s *agentStore) find(host string) (Agent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, a := range s.agents {
		if a.Name == host {
			return a, true
		}
	}
	for _, a := range s.agents {
		if a.IP == host {
			return a, true
		}
	}
	return Agent{}, false
}

// errDuplicateAddr marks an Add rejection caused by an address already in
// the fleet, as opposed to a persistence failure — the HTTP handler uses
// errors.Is to tell the two apart and pick the right status code.
var errDuplicateAddr = errors.New("already in the fleet")

// Add appends a new agent and persists the updated fleet to disk. It rejects
// an address already in the fleet rather than silently duplicating it.
func (s *agentStore) Add(a Agent) (Agent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, existing := range s.agents {
		if existing.Addr == a.Addr {
			return Agent{}, fmt.Errorf("%s %w", a.Addr, errDuplicateAddr)
		}
	}
	updated := append(s.agents, a)
	// The fleet config shares the store package's crash-safe atomic write, but
	// keeps its own bespoke load (loadAgents falls back to a local agent and
	// fails loudly on a corrupt parse) — a derived cache can be rebuilt, but the
	// console must not silently forget its fleet.
	if err := store.Save(s.path, updated); err != nil {
		return Agent{}, fmt.Errorf("save %s: %w — check that its directory is writable (see the -config flag and the systemd unit's ReadWritePaths)", s.path, err)
	}
	s.agents = updated
	return a, nil
}

// stringList is a repeatable string flag (e.g. -allow a -allow b).
type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// collectFleet queries every agent concurrently, preserving config order.
// proxyBrowse forwards a directory-listing request to one agent's /browse and
// relays its response verbatim — including the status code, so the agent's
// 403 (can't read) / 404 (no such dir) reach the console unchanged. The phone
// can't address agents directly in tsnet mode, so every browse rides through
// hush-control; on the NAS the bytes never leave the box until they reach you.
func proxyBrowse(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/browse"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy browse %s: %v", a.Name, err)
	}
}

// proxyTop forwards a live process/core request to one agent's /top and relays
// its response verbatim, the same shape as proxyBrowse. An agent too old to
// serve /top answers 404, relayed as-is so the console can say "not supported".
func proxyTop(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/top"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy top %s: %v", a.Name, err)
	}
}

// proxySessions forwards a request to one agent's /sessions and relays its
// response verbatim, the same shape as proxyTop. The phone can't address agents
// directly in tsnet mode, so the session list rides through hush-control like
// every other per-machine read.
func proxySessions(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/sessions"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy sessions %s: %v", a.Name, err)
	}
}

// proxyDu forwards a directory-sizing request to one agent's /du and relays
// its response verbatim, the same shape as proxyBrowse. The one difference is
// the client it rides: a recursive walk can take real time, so the caller
// passes a client with a longer timeout than the 2s fleet-poll one.
func proxyDu(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/du"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy du %s: %v", a.Name, err)
	}
}

// proxyFile streams one file from an agent's /file through to the caller. It
// forwards the conditional/range request headers so byte-range seeks (video
// scrubbing) survive the hop, and relays the agent's response headers and
// status verbatim — including 206 Partial Content — before streaming the body.
// In tsnet mode the phone can't reach agents directly, so media rides through
// hush-control; on the NAS the bytes never leave the box until they reach you.
func proxyFile(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/file"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	for _, h := range []string{"Range", "If-Range", "If-Modified-Since", "If-None-Match"} {
		if v := r.Header.Get(h); v != "" {
			req.Header.Set(h, v)
		}
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	for k, vs := range resp.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy file %s: %v", a.Name, err)
	}
}

// collectFleet fans out to every agent's /vitals. latest is the latest
// published release tag (empty when unknown), used to flag agents running an
// older version — the same tag covers both binaries, since hush-agent and
// hush-control ship together in one GitHub release.
func collectFleet(client *http.Client, agents []Agent, latest string) []Machine {
	out := make([]Machine, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a Agent) {
			defer wg.Done()
			out[i] = fetchOne(client, a, latest)
		}(i, a)
	}
	wg.Wait()
	return out
}

func fetchOne(client *http.Client, a Agent, latest string) Machine {
	// Default to the "unreachable" state; a successful fetch overwrites it.
	m := Machine{
		ID:     a.Name,
		IP:     a.IP,
		Role:   a.Role,
		Status: "crit",
		Alert:  "unreachable",
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
	m.AgentVersion = s.Version
	m.LatestVersion = latest
	m.AgentUpdateAvailable = latest != "" && updater.Newer(latest, s.Version)
	if a.Name == "" && s.Host != "" {
		m.ID = s.Host
	}
	m.OS, m.Up, m.Load = s.OS, s.Up, s.Load
	m.CPU, m.Mem, m.Disk = s.CPU, s.Mem, s.Disk
	m.NetRx, m.NetTx = s.NetRx, s.NetTx
	m.GPU, m.VRAM, m.GPUName, m.VRAMText = s.GPU, s.VRAM, s.GPUName, s.VRAMText
	m.LLM = s.LLM
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
	Current         string `json:"current"`             // running version, e.g. "v1.2.0", "dev-a1b2c3d4e5f6", or "dev"
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

// status returns the cached version check, or performs a fresh upstream
// lookup when the cache is stale, empty, or the caller passes force=true
// (used by the console's "check now" click).
func (v *versionChecker) status(ctx context.Context, force bool) VersionStatus {
	v.mu.Lock()
	defer v.mu.Unlock()

	if !force && !v.cachedAt.IsZero() && time.Since(v.cachedAt) < versionTTL && v.cached.Error == "" {
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
// swapped binary is what runs. try-restart is a no-op for an inactive unit,
// but install.sh only ever installs the unit file for the mode actually
// chosen (LAN or tsnet), so the other name is routinely missing on any given
// box. try-restart per unit individually so a "not found" on the uninstalled
// mode doesn't mask a real failure restarting the one that's actually there.
func restartService(ctx context.Context) error {
	units := []string{"hush-control.service", "hush-control-tsnet.service"}
	var errs []string
	for _, unit := range units {
		cmd := exec.CommandContext(ctx, "systemctl", "try-restart", unit)
		out, err := cmd.CombinedOutput()
		if err == nil {
			continue
		}
		msg := strings.TrimSpace(string(out))
		if strings.Contains(msg, "not found") {
			continue // this mode's unit isn't installed on this box
		}
		errs = append(errs, fmt.Sprintf("%s: %v: %s", unit, err, msg))
	}
	if len(errs) > 0 {
		return fmt.Errorf("systemctl try-restart: %s", strings.Join(errs, "; "))
	}
	return nil
}
