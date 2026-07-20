// Package llm detects local LLM runtimes on a box — an OpenAI-compatible
// server such as llama-swap, or an Ollama daemon — and reports what they serve
// along with how far that service is reachable.
//
// The reachability half is the point. A model list on its own implies the fleet
// can call it, which is false for the common case: these runtimes bind loopback
// by default, so the models exist but nothing off-box can reach them. Detect
// therefore classifies each runtime's bind scope from the kernel's own listener
// table (/proc/net/tcp{,6}) rather than trusting the probe succeeding — the
// probe always runs over loopback, so it can never tell you the difference.
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

// DefaultEndpoints are the loopback addresses probed when the agent isn't told
// otherwise: llama-swap's conventional port and Ollama's fixed one.
var DefaultEndpoints = []string{"127.0.0.1:8091", "127.0.0.1:11434"}

// probeTimeout bounds a single runtime probe. These are loopback requests to a
// process that is either up or not, so a short ceiling is plenty and keeps a
// hung runtime from stalling the whole detection pass.
const probeTimeout = 2 * time.Second

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
// longer serves.
func StartProbe(ctx context.Context, endpoints []string, interval time.Duration) {
	refresh(ctx, endpoints)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				refresh(ctx, endpoints)
			}
		}
	}()
}

func refresh(ctx context.Context, endpoints []string) {
	cap := &vitals.LLMCapability{Runtimes: Detect(ctx, endpoints)}
	cacheMu.Lock()
	cached = cap
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

// Detect probes each endpoint and returns a runtime entry for every one that
// answers, sorted by address so the wire output is stable across polls. An
// endpoint that doesn't answer is simply absent — nothing is reported as
// "down", because an unconfigured port and a stopped daemon are the same
// observation from here and neither is worth alarming on.
func Detect(ctx context.Context, endpoints []string) []vitals.LLMRuntime {
	listeners := listenScopes()

	var out []vitals.LLMRuntime
	for _, addr := range endpoints {
		rt, ok := probe(ctx, addr)
		if !ok {
			continue
		}
		// Exposure comes from the listener table, not the probe. Falling back to
		// "unknown" (rather than assuming loopback) matters: /proc unreadable must
		// not render as a safety claim the agent didn't actually verify.
		rt.Exposure = vitals.LLMExposureUnknown
		if _, port, err := net.SplitHostPort(addr); err == nil {
			if scope, found := listeners[port]; found {
				rt.Exposure = scope
			}
		}
		out = append(out, rt)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Addr < out[j].Addr })
	return out
}

// probe identifies what's listening at addr by asking for its model list. The
// two runtimes are told apart by which API shape answers: llama-swap (and any
// OpenAI-compatible server) serves /v1/models, Ollama serves /api/tags.
//
// A match requires at least one model, not merely a 200 that decodes. Any JSON
// object decodes cleanly into these structs — unknown fields are discarded — so
// an unrelated service on the port would otherwise be reported as an LLM
// runtime serving nothing. Requiring a model also costs nothing real: a runtime
// with an empty catalogue has no capability to disclose.
func probe(ctx context.Context, addr string) (vitals.LLMRuntime, bool) {
	client := &http.Client{Timeout: probeTimeout}

	// Ollama is checked first, and the order is load-bearing: current Ollama
	// serves an OpenAI-compatible /v1/models alongside its native API, so
	// probing /v1/models first would label every Ollama daemon "openai". Only
	// /api/tags is unique to Ollama, so a hit there settles the kind; llama-swap
	// doesn't serve it and falls through to the generic branch below.
	if tags, ok := getJSON[ollamaTags](ctx, client, "http://"+addr+"/api/tags"); ok && len(tags.Models) > 0 {
		rt := vitals.LLMRuntime{Kind: vitals.LLMKindOllama, Addr: addr}
		for _, m := range tags.Models {
			rt.Models = append(rt.Models, m.Name)
		}
		sort.Strings(rt.Models)
		return rt, true
	}

	if models, ok := getJSON[openAIModels](ctx, client, "http://"+addr+"/v1/models"); ok && len(models.Data) > 0 {
		rt := vitals.LLMRuntime{Kind: vitals.LLMKindOpenAI, Addr: addr}
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
// that isn't a clean 200 of decodable JSON. Callers apply the stricter
// content check (see probe).
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

// --- bind-scope classification ----------------------------------------------

// listenScopes reads the kernel listener table and returns, per port, the
// widest scope anything is listening on. Widest wins: a runtime bound to both
// 127.0.0.1 and a tailnet address is reachable off-box, so it must not report
// as loopback just because a loopback bind also exists.
func listenScopes() map[string]string {
	scopes := map[string]string{}
	rank := map[string]int{
		vitals.LLMExposureLoopback: 0,
		vitals.LLMExposureTailnet:  1,
		vitals.LLMExposureOpen:     2,
	}
	for _, path := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		for port, scope := range parseListeners(path) {
			if cur, ok := scopes[port]; !ok || rank[scope] > rank[cur] {
				scopes[port] = scope
			}
		}
	}
	return scopes
}

// tcpStateListen is the value /proc/net/tcp uses for a socket in LISTEN.
const tcpStateListen = "0A"

// parseListeners extracts the widest bind scope per port from one /proc/net
// table. A file it can't read yields nothing, which surfaces upstream as
// "unknown" rather than a false all-clear.
func parseListeners(path string) map[string]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	rank := map[string]int{
		vitals.LLMExposureLoopback: 0,
		vitals.LLMExposureTailnet:  1,
		vitals.LLMExposureOpen:     2,
	}
	out := map[string]string{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // first line is the column header
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != tcpStateListen {
			continue
		}
		host, port, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(port, 16, 32)
		if err != nil {
			continue
		}
		dec := strconv.FormatUint(n, 10)
		scope := scopeOf(host)
		if cur, ok := out[dec]; !ok || rank[scope] > rank[cur] {
			out[dec] = scope
		}
	}
	return out
}

// scopeOf classifies a hex local address from /proc/net/tcp{,6}. The addresses
// are little-endian per 32-bit word, but the only distinctions that matter here
// — all-zero wildcard, loopback, and the Tailscale CGNAT range — survive a
// byte-wise decode without reassembling host order.
func scopeOf(hexAddr string) string {
	b, err := hexBytes(hexAddr)
	if err != nil {
		return vitals.LLMExposureOpen // undecodable: assume the worse of the two
	}

	allZero := true
	for _, c := range b {
		if c != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		return vitals.LLMExposureOpen // 0.0.0.0 / :: — every interface
	}

	switch len(b) {
	case 4:
		// IPv4 words are little-endian, so 127.0.0.1 arrives as 0100007F.
		ip := net.IPv4(b[3], b[2], b[1], b[0])
		return scopeOfIP(ip)
	case 16:
		// IPv6, four little-endian 32-bit words. Reverse each to get wire order.
		ip := make(net.IP, 16)
		for i := 0; i < 16; i += 4 {
			ip[i], ip[i+1], ip[i+2], ip[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		return scopeOfIP(ip)
	}
	return vitals.LLMExposureOpen
}

// tailnetCGNAT is the 100.64.0.0/10 range Tailscale assigns node addresses from.
var tailnetCGNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func scopeOfIP(ip net.IP) string {
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
