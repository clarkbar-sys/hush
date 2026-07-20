// Package llm detects local LLM runtimes on a box — an OpenAI-compatible
// server such as llama-swap, or an Ollama daemon — and reports what they serve
// along with how far that service is reachable.
//
// The reachability half is the point. A model list on its own implies the fleet
// can call it, which is false for the common case: these runtimes bind loopback
// by default, so the models exist but nothing off-box can reach them.
//
// Both halves are answered from the kernel's own listener table
// (/proc/net/tcp{,6}) rather than from a fixed address list. Discovering where
// a runtime actually binds is what makes the report survive the interesting
// case: the moment an operator exposes a runtime by moving it off loopback, a
// loopback-only probe stops finding it altogether and the box reports *less*
// capability than before. Reading the bind table first means a runtime is found
// wherever it listens, and its scope is a fact about the socket rather than an
// inference from a probe that only ever ran over loopback.
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// DefaultPorts are the ports scanned for a local runtime when the agent isn't
// told otherwise: llama-swap's conventional port and Ollama's fixed one.
var DefaultPorts = []string{"8091", "11434"}

// probeTimeout bounds a single runtime probe. These are same-host requests to a
// process that is either up or not, so a short ceiling is plenty and keeps a
// hung runtime from stalling the whole detection pass.
const probeTimeout = 2 * time.Second

// Options controls how a detection pass finds runtimes.
type Options struct {
	// Ports are scanned in the listener table; whatever a runtime is bound to on
	// one of these is probed there. Empty disables discovery.
	Ports []string
	// Endpoints pins exact "host:port" targets and replaces discovery entirely.
	// It exists for a runtime on a non-standard port, or one reached through an
	// address the listener table can't describe. Empty means discover.
	Endpoints []string
}

// Enabled reports whether this configuration probes anything at all.
func (o Options) Enabled() bool { return len(o.Ports) > 0 || len(o.Endpoints) > 0 }

// --- cached background detection --------------------------------------------

var (
	cacheMu sync.RWMutex
	cached  *vitals.LLMCapability
)

// StartProbe runs one detection pass immediately, then repeats it on interval
// until ctx is cancelled. /vitals then serves the last completed pass, so a
// slow or hung runtime can never stall a vitals request.
//
// Detection repeats rather than running once at boot because the catalogue is
// genuinely mutable: llama-swap hot-reloads its config directory, so models
// appear and disappear with no restart to re-trigger a one-shot probe. A
// boot-time snapshot would drift into confidently reporting models the box no
// longer serves. Re-scanning also picks up a runtime that was rebound to a
// different address while the agent stayed up.
func StartProbe(ctx context.Context, opts Options, interval time.Duration) {
	refresh(ctx, opts)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh(ctx, opts)
			}
		}
	}()
}

func refresh(ctx context.Context, opts Options) {
	c := &vitals.LLMCapability{Runtimes: Detect(ctx, opts)}
	cacheMu.Lock()
	cached = c
	cacheMu.Unlock()
}

// Current returns the most recent completed detection pass, or nil if StartProbe
// was never called — which the console reads as "this agent doesn't report LLM
// state", distinct from "this box has none".
func Current() *vitals.LLMCapability {
	cacheMu.RLock()
	defer cacheMu.RUnlock()
	return cached
}

// --- detection ---------------------------------------------------------------

// Detect probes each target and returns a runtime entry for every one that
// answers, sorted by address so the wire output is stable across polls. A
// target that doesn't answer is simply absent — nothing is reported as "down",
// because an unconfigured port and a stopped daemon are the same observation
// from here and neither is worth alarming on.
func Detect(ctx context.Context, opts Options) []vitals.LLMRuntime {
	var out []vitals.LLMRuntime
	for _, t := range targets(opts) {
		rt, ok := probe(ctx, t.probe)
		if !ok {
			continue
		}
		rt.Addr = t.bind
		rt.Exposure = t.exposure
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out
}

// target is one address to contact plus the bind facts that describe it. probe
// and bind differ whenever a runtime isn't reachable at the address that best
// describes it — a wildcard bind is contacted over loopback but reported as
// what it is.
type target struct {
	probe    string
	bind     string
	exposure string
}

// targets resolves the configuration into concrete things to contact. Explicit
// endpoints replace discovery; their scope is still looked up in the listener
// table so a pinned target reports the same verified exposure as a found one.
func targets(opts Options) []target {
	if len(opts.Endpoints) > 0 {
		byPort := listeners()
		out := make([]target, 0, len(opts.Endpoints))
		for _, addr := range opts.Endpoints {
			t := target{probe: addr, bind: addr, exposure: vitals.LLMExposureUnknown}
			if _, port, err := net.SplitHostPort(addr); err == nil {
				if b, ok := widest(byPort[port]); ok {
					t.exposure = b.scope
				}
			}
			out = append(out, t)
		}
		return out
	}

	byPort := listeners()
	out := make([]target, 0, len(opts.Ports))
	for _, port := range opts.Ports {
		bs := byPort[port]
		b, ok := widest(bs)
		if !ok {
			continue
		}
		// Contact loopback whenever the socket is reachable there — it's bound
		// explicitly, or bound to every interface. Otherwise the runtime lives on
		// exactly one address and that's the only way in.
		probeAddr := b.addr(port)
		if b.wildcard() || hasLoopback(bs) {
			probeAddr = net.JoinHostPort("127.0.0.1", port)
		}
		out = append(out, target{probe: probeAddr, bind: b.addr(port), exposure: b.scope})
	}
	return out
}

// --- bind-scope classification ----------------------------------------------

// binding is one LISTEN socket from /proc/net/tcp{,6}.
type binding struct {
	ip    net.IP // nil when the address could not be decoded
	scope string
}

func (b binding) wildcard() bool { return b.ip != nil && b.ip.IsUnspecified() }

// addr renders the bind address for reporting. A binding whose address didn't
// decode is described as the wildcard it was conservatively scored as, so the
// reported address never contradicts the reported scope.
func (b binding) addr(port string) string {
	if b.ip == nil {
		return net.JoinHostPort("0.0.0.0", port)
	}
	return net.JoinHostPort(b.ip.String(), port)
}

// scopeRank orders bind scopes by how far they reach.
var scopeRank = map[string]int{
	vitals.LLMExposureLoopback: 0,
	vitals.LLMExposureTailnet:  1,
	vitals.LLMExposureOpen:     2,
}

// widest returns the binding that reaches furthest. Widest wins because a
// runtime bound to both loopback and a tailnet address is reachable off-box,
// and must not report as loopback just because a loopback bind also exists.
func widest(bs []binding) (binding, bool) {
	if len(bs) == 0 {
		return binding{}, false
	}
	out := bs[0]
	for _, b := range bs[1:] {
		if scopeRank[b.scope] > scopeRank[out.scope] {
			out = b
		}
	}
	return out, true
}

func hasLoopback(bs []binding) bool {
	for _, b := range bs {
		if b.scope == vitals.LLMExposureLoopback {
			return true
		}
	}
	return false
}

// listeners reads both kernel listener tables and returns every LISTEN socket
// grouped by port.
func listeners() map[string][]binding {
	out := map[string][]binding{}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for port, bs := range parseListeners(path) {
			out[port] = append(out[port], bs...)
		}
	}
	return out
}

// tcpStateListen is the value /proc/net/tcp uses for a socket in LISTEN.
const tcpStateListen = "0A"

// parseListeners extracts every LISTEN socket from one /proc/net table, grouped
// by port. A file it can't read yields nothing, which surfaces upstream as a
// port simply not being found rather than a false all-clear.
func parseListeners(path string) map[string][]binding {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string][]binding{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // first line is the column header
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != tcpStateListen {
			continue
		}
		hexAddr, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(hexPort, 16, 32)
		if err != nil {
			continue
		}
		port := strconv.FormatUint(n, 10)

		ip, err := decodeAddr(hexAddr)
		if err != nil {
			// Undecodable address: score it as the widest scope rather than drop
			// it. /proc's format is kernel-stable so this shouldn't happen, but
			// silently omitting a socket could only ever make a port look less
			// exposed than it is — the one direction that must not fail quietly.
			out[port] = append(out[port], binding{scope: vitals.LLMExposureOpen})
			continue
		}
		out[port] = append(out[port], binding{ip: ip, scope: scopeOfIP(ip)})
	}
	return out
}

// decodeAddr converts a /proc-style hex local address into an IP. The addresses
// are little-endian per 32-bit word, so each word is reversed to get wire order.
func decodeAddr(hexAddr string) (net.IP, error) {
	b, err := hexBytes(hexAddr)
	if err != nil {
		return nil, err
	}
	switch len(b) {
	case 4:
		return net.IPv4(b[3], b[2], b[1], b[0]), nil
	case 16:
		ip := make(net.IP, 16)
		for i := 0; i < 16; i += 4 {
			ip[i], ip[i+1], ip[i+2], ip[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		return ip, nil
	}
	return nil, fmt.Errorf("unexpected address width %d in %q", len(b), hexAddr)
}

// tailnetCGNAT is the 100.64.0.0/10 range Tailscale assigns node addresses from.
var tailnetCGNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// scopeOfIP classifies how far a bind address reaches. Anything that isn't
// loopback or a tailnet address is "open": a LAN address is reachable by
// everything on the subnet, which for an unauthenticated inference API is the
// same practical exposure as a wildcard bind.
func scopeOfIP(ip net.IP) string {
	if ip.IsUnspecified() {
		return vitals.LLMExposureOpen
	}
	if ip.IsLoopback() {
		return vitals.LLMExposureLoopback
	}
	if v4 := ip.To4(); v4 != nil && tailnetCGNAT.Contains(v4) {
		return vitals.LLMExposureTailnet
	}
	return vitals.LLMExposureOpen
}

// hexBytes decodes a /proc-style hex address into raw bytes.
func hexBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex address %q", s)
	}
	b := make([]byte, len(s)/2)
	for i := range b {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		b[i] = byte(v)
	}
	return b, nil
}

// --- runtime probing ---------------------------------------------------------

// probe identifies what's listening at addr by asking for its model list. The
// two runtimes are told apart by which API shape answers: llama-swap (and any
// OpenAI-compatible server) serves /v1/models, Ollama serves /api/tags.
//
// Ollama is checked first, and the order is load-bearing: current Ollama serves
// an OpenAI-compatible /v1/models alongside its native API, so probing
// /v1/models first would label every Ollama daemon "openai". Only /api/tags is
// unique to Ollama, so a hit there settles the kind; llama-swap doesn't serve
// it and falls through to the generic branch.
//
// A match requires at least one model, not merely a 200 that decodes. Any JSON
// object decodes cleanly into these structs — unknown fields are discarded — so
// an unrelated service on the port would otherwise be reported as an LLM
// runtime serving nothing. Requiring a model also costs nothing real: a runtime
// with an empty catalogue has no capability to disclose.
func probe(ctx context.Context, addr string) (vitals.LLMRuntime, bool) {
	client := &http.Client{Timeout: probeTimeout}

	if tags, ok := getJSON[ollamaTags](ctx, client, "http://"+addr+"/api/tags"); ok && len(tags.Models) > 0 {
		rt := vitals.LLMRuntime{Kind: vitals.LLMKindOllama}
		for _, m := range tags.Models {
			rt.Models = append(rt.Models, m.Name)
		}
		sort.Strings(rt.Models)
		return rt, true
	}

	if models, ok := getJSON[openAIModels](ctx, client, "http://"+addr+"/v1/models"); ok && len(models.Data) > 0 {
		rt := vitals.LLMRuntime{Kind: vitals.LLMKindOpenAI}
		for _, m := range models.Data {
			rt.Models = append(rt.Models, m.ID)
		}
		sort.Strings(rt.Models)
		return rt, true
	}

	return vitals.LLMRuntime{}, false
}

type openAIModels struct {
	Data []struct {
		ID string `json:"id"`
	} `json:"data"`
}

type ollamaTags struct {
	Models []struct {
		Name string `json:"name"`
	} `json:"models"`
}

// getJSON performs one GET and decodes the body, reporting failure for anything
// that isn't a clean 200 of decodable JSON. Callers apply the stricter content
// check (see probe).
func getJSON[T any](ctx context.Context, c *http.Client, url string) (T, bool) {
	var zero T
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return zero, false
	}
	resp, err := c.Do(req)
	if err != nil {
		return zero, false
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return zero, false
	}
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		return zero, false
	}
	return v, true
}
