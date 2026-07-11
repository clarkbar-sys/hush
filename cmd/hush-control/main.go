// Command hush-control is the fleet control plane. It runs on one machine
// (the NAS), fans out to every agent to collect vitals, and serves the web UI.
//
// Agents are listed in a JSON config file (see fleet.example.json). With no
// config it assumes a single local agent, which is handy for development.
package main

import (
	"encoding/json"
	"flag"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

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
	flag.Parse()

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
