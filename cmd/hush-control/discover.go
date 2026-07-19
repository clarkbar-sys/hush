// Tailnet discovery: instead of typing an agent's address by hand, hush-control
// can enumerate the tailnet's own device table (the same list Tailscale keeps,
// much like DHCP leases) and probe each node for a running hush-agent. The
// operator then adds the ones they want with a single tap, rather than hunting
// for IPs.
//
// Discovery needs the tsnet LocalClient to read the tailnet's peers. Until the
// node is provisioned (during the first-run setup page) there is no tailnet
// handle yet, so /api/discover reports itself unavailable and the console hides
// the scan affordance until the node comes up.
package main

import (
	"context"
	"strings"
	"sync"
	"time"

	"tailscale.com/client/local"
)

// discoverPort is the port every hush-agent listens on by default, and the one
// discovery probes on each tailnet node.
const discoverPort = "8765"

// discoverProbeConcurrency bounds how many peers we probe at once, so scanning a
// large tailnet doesn't open a connection to every node simultaneously.
const discoverProbeConcurrency = 16

// rescanInterval is how often the background discoverer re-scans the tailnet, so
// the console can badge newly-appeared agents without the operator opening the
// Add sheet. Tuned for a homelab: fresh enough to notice a new box within a
// minute, sparse enough not to hammer the tailnet.
const rescanInterval = time.Minute

// discoveredPeer is one tailnet node as seen by the discovery layer, reduced to
// just the fields discovery needs. It deliberately does not depend on Tailscale
// types, so the probe logic can be tested with a fake peerLister.
type discoveredPeer struct {
	Host   string
	IP     string // primary tailnet IPv4 address
	OS     string
	Online bool
}

// peerLister enumerates the tailnet's nodes. It is backed by the tsnet
// LocalClient; until the node is provisioned there is none, and discovery
// reports itself unavailable.
type peerLister interface {
	Peers(ctx context.Context) ([]discoveredPeer, error)
}

// discoverySource holds the active peerLister, if any. It is set once the tsnet
// node comes up (which happens after the mux is already built), so the handler
// reads it through this holder rather than capturing a lister at wiring time.
type discoverySource struct {
	mu     sync.RWMutex
	lister peerLister
}

func (d *discoverySource) set(l peerLister) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.lister = l
}

func (d *discoverySource) get() peerLister {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.lister
}

// tsnetPeerLister adapts a tsnet LocalClient to peerLister by reading the
// tailnet status and reducing each peer to a discoveredPeer.
type tsnetPeerLister struct {
	lc *local.Client
}

func (p tsnetPeerLister) Peers(ctx context.Context) ([]discoveredPeer, error) {
	st, err := p.lc.Status(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]discoveredPeer, 0, len(st.Peer))
	for _, ps := range st.Peer {
		var ip string
		for _, a := range ps.TailscaleIPs {
			if a.Is4() {
				ip = a.String()
				break
			}
		}
		if ip == "" {
			continue // no IPv4 tailnet address to reach it on
		}
		out = append(out, discoveredPeer{
			Host:   ps.HostName,
			IP:     ip,
			OS:     ps.OS,
			Online: ps.Online,
		})
	}
	return out, nil
}

// discoverResult is the response of GET /api/discover. Available is false in LAN
// mode (no tailnet handle); Error carries a status-read failure. Candidates are
// online tailnet nodes running hush-agent that are not already in the fleet.
type discoverResult struct {
	Available  bool                `json:"available"`
	Error      string              `json:"error,omitempty"`
	Candidates []discoverCandidate `json:"candidates"`
}

// discoverCandidate is one addable machine found on the tailnet — enough for the
// console to show it and, on a tap, POST it to /api/agents unchanged.
type discoverCandidate struct {
	Name      string `json:"name"` // hostname reported by the agent (or the peer's tailnet name)
	IP        string `json:"ip"`   // tailnet IPv4
	Addr      string `json:"addr"` // base URL, e.g. http://100.71.6.4:8765
	OS        string `json:"os,omitempty"`
	LatencyMs int64  `json:"latencyMs,omitempty"`
}

// agentProbe reports whether an address answers like a hush-agent. In
// production it wraps testAgent over the shared *http.Client; tests supply a
// fake so the probe runs without a real network round-trip.
type agentProbe func(rawAddr string) testAgentResult

// discoverCandidates enumerates the tailnet, probes each online peer's default
// agent port, and returns the ones that answer like a hush-agent and are not
// already in the fleet. Probes run concurrently, bounded by
// discoverProbeConcurrency. A nil lister means discovery is unavailable (LAN
// mode), reported as Available:false rather than an error.
func discoverCandidates(ctx context.Context, lister peerLister, probe agentProbe, fleet []Agent) discoverResult {
	if lister == nil {
		return discoverResult{Available: false, Candidates: []discoverCandidate{}}
	}
	peers, err := lister.Peers(ctx)
	if err != nil {
		return discoverResult{Available: true, Error: err.Error(), Candidates: []discoverCandidate{}}
	}

	// Addresses already in the fleet, so we never offer to add a duplicate.
	inFleet := make(map[string]bool, len(fleet))
	for _, a := range fleet {
		inFleet[a.Addr] = true
	}

	type found struct {
		peer discoveredPeer
		addr string
		res  testAgentResult
	}
	results := make([]found, len(peers))
	sem := make(chan struct{}, discoverProbeConcurrency)
	var wg sync.WaitGroup
	for i, peer := range peers {
		if !peer.Online || peer.IP == "" {
			continue
		}
		addr := "http://" + peer.IP + ":" + discoverPort
		if inFleet[addr] {
			continue // already watching this machine
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, peer discoveredPeer, addr string) {
			defer wg.Done()
			defer func() { <-sem }()
			results[i] = found{peer: peer, addr: addr, res: probe(addr)}
		}(i, peer, addr)
	}
	wg.Wait()

	candidates := make([]discoverCandidate, 0, len(results))
	for _, f := range results {
		if !f.res.OK {
			continue // didn't answer, or didn't look like a hush-agent
		}
		name := f.res.Host
		if name == "" {
			name = strings.TrimSuffix(f.peer.Host, ".")
		}
		os := f.res.OS
		if os == "" {
			os = f.peer.OS
		}
		candidates = append(candidates, discoverCandidate{
			Name:      name,
			IP:        f.peer.IP,
			Addr:      f.addr,
			OS:        os,
			LatencyMs: f.res.LatencyMs,
		})
	}
	return discoverResult{Available: true, Candidates: candidates}
}

// discoverer runs discovery on a timer and caches the latest result, so the
// console can show a passive "new agents found" badge without every request
// re-probing the whole tailnet. It reads the active lister through the shared
// discoverySource, so it comes alive as soon as the tsnet node sets one; before
// that each scan short-circuits to "unavailable" at negligible cost.
type discoverer struct {
	disco *discoverySource
	probe agentProbe
	fleet func() []Agent

	mu       sync.RWMutex
	cached   discoverResult
	hasCache bool
}

// newDiscoverer builds a discoverer over a discoverySource, a probe (testAgent
// in production), and a snapshot of the current fleet (store.Snapshot), so each
// scan dedupes against machines already being watched.
func newDiscoverer(disco *discoverySource, probe agentProbe, fleet func() []Agent) *discoverer {
	return &discoverer{disco: disco, probe: probe, fleet: fleet}
}

// scan runs one discovery pass and refreshes the cache, returning the result.
// It backs both the background timer and an on-demand rescan from the console.
func (d *discoverer) scan(ctx context.Context) discoverResult {
	res := discoverCandidates(ctx, d.disco.get(), d.probe, d.fleet())
	d.mu.Lock()
	d.cached, d.hasCache = res, true
	d.mu.Unlock()
	return res
}

// snapshot returns the last cached result. ok is false until the first scan
// completes, letting the handler fall back to a live scan on a cold cache.
func (d *discoverer) snapshot() (result discoverResult, ok bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.cached, d.hasCache
}

// run scans immediately (to warm the cache at startup) and then on every tick
// until ctx is cancelled.
func (d *discoverer) run(ctx context.Context) {
	d.scan(ctx)
	t := time.NewTicker(rescanInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.scan(ctx)
		}
	}
}
