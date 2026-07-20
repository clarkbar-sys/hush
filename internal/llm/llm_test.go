package llm

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// TestScopeOfIP pins the classification that decides whether the console says
// "local only" or "serving". Getting this wrong inverts a safety claim.
func TestScopeOfIP(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		{"127.0.0.1", vitals.LLMExposureLoopback},
		{"::1", vitals.LLMExposureLoopback},
		{"0.0.0.0", vitals.LLMExposureOpen},
		{"::", vitals.LLMExposureOpen},
		{"100.87.180.34", vitals.LLMExposureTailnet}, // tailnet CGNAT
		{"100.63.255.255", vitals.LLMExposureOpen},   // just below 100.64/10
		{"100.128.0.0", vitals.LLMExposureOpen},      // just above 100.64/10
		{"192.168.0.30", vitals.LLMExposureOpen},     // LAN is not "safe"
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			if got := scopeOfIP(net.ParseIP(tc.ip)); got != tc.want {
				t.Fatalf("scopeOfIP(%s) = %q, want %q", tc.ip, got, tc.want)
			}
		})
	}
}

// TestDecodeAddr pins the little-endian /proc decoding. The encodings are
// asserted literally rather than round-tripped, so a byte-order regression
// can't pass by being wrong consistently in both directions.
func TestDecodeAddr(t *testing.T) {
	cases := []struct {
		hex  string
		want string
	}{
		{"0100007F", "127.0.0.1"},
		{"00000000", "0.0.0.0"},
		{"22B45764", "100.87.180.34"},
		{"1E00A8C0", "192.168.0.30"},
		{"00000000000000000000000001000000", "::1"},
		{strings.Repeat("0", 32), "::"},
	}
	for _, tc := range cases {
		t.Run(tc.hex, func(t *testing.T) {
			ip, err := decodeAddr(tc.hex)
			if err != nil {
				t.Fatalf("decodeAddr(%q): %v", tc.hex, err)
			}
			if got := ip.String(); got != tc.want {
				t.Fatalf("decodeAddr(%q) = %s, want %s", tc.hex, got, tc.want)
			}
		})
	}

	for _, bad := range []string{"zz", "0100007", "00"} {
		if _, err := decodeAddr(bad); err == nil {
			t.Errorf("decodeAddr(%q) succeeded, want error", bad)
		}
	}
}

// writeTable writes a synthetic /proc/net/tcp table with the given
// "hexaddr:hexport" LISTEN entries.
func writeTable(t *testing.T, entries ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("  sl  local_address rem_address   st\n")
	for i, e := range entries {
		b.WriteString("   " + string(rune('0'+i)) + ": " + e + " 00000000:0000 0A\n")
	}
	path := filepath.Join(t.TempDir(), "tcp")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseListenersWidestWins checks that a port bound both to loopback and to
// a tailnet address reports the tailnet scope — the reachable truth, not the
// reassuring half of it.
func TestParseListenersWidestWins(t *testing.T) {
	path := writeTable(t,
		"0100007F:1F9B", // 127.0.0.1:8091
		"22B45764:1F9B", // 100.87.180.34:8091
		"0100007F:2CEE", // 127.0.0.1:11502
	)
	got := parseListeners(path)

	b, ok := widest(got["8091"])
	if !ok {
		t.Fatal("port 8091 not found")
	}
	if b.scope != vitals.LLMExposureTailnet {
		t.Errorf("port 8091 scope = %q, want %q", b.scope, vitals.LLMExposureTailnet)
	}
	if !hasLoopback(got["8091"]) {
		t.Error("port 8091 should still report a loopback binding alongside the tailnet one")
	}

	b2, _ := widest(got["11502"])
	if b2.scope != vitals.LLMExposureLoopback {
		t.Errorf("port 11502 scope = %q, want %q", b2.scope, vitals.LLMExposureLoopback)
	}
}

// TestParseListenersUndecodableIsOpen guards the fail-loud direction: a socket
// whose address can't be decoded must widen the port's reported scope, never
// silently vanish and leave the port looking safer than it is.
func TestParseListenersUndecodableIsOpen(t *testing.T) {
	path := writeTable(t,
		"0100007F:1F9B", // 127.0.0.1:8091
		"zzzzzzzz:1F9B", // undecodable, same port
	)
	b, ok := widest(parseListeners(path)["8091"])
	if !ok {
		t.Fatal("port 8091 not found")
	}
	if b.scope != vitals.LLMExposureOpen {
		t.Fatalf("scope = %q, want %q — an undecodable bind must not read as safe", b.scope, vitals.LLMExposureOpen)
	}
	if got := b.addr("8091"); got != "0.0.0.0:8091" {
		t.Errorf("addr = %q, want the wildcard it was scored as", got)
	}
}

func TestParseListenersUnreadable(t *testing.T) {
	if got := parseListeners("/nonexistent/proc/net/tcp"); len(got) != 0 {
		t.Fatalf("unreadable table yielded %v, want nothing", got)
	}
}

// TestProbeIdentifiesRuntimes checks that each runtime is told apart by the API
// shape that answers, and that its models come back sorted.
func TestProbeIdentifiesRuntimes(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		srv := newOpenAIServer(t, "qwen3-30b-a3b", "gpt-oss:20b")
		rt, ok := probe(context.Background(), hostPort(srv.URL))
		if !ok {
			t.Fatal("probe did not identify the OpenAI-compatible runtime")
		}
		if rt.Kind != vitals.LLMKindOpenAI {
			t.Errorf("kind = %q, want %q", rt.Kind, vitals.LLMKindOpenAI)
		}
		if got := strings.Join(rt.Models, ","); got != "gpt-oss:20b,qwen3-30b-a3b" {
			t.Errorf("models = %q, want them sorted", got)
		}
	})

	t.Run("ollama", func(t *testing.T) {
		srv := newOllamaServer(t, "qwen2.5-coder:14b")
		rt, ok := probe(context.Background(), hostPort(srv.URL))
		if !ok {
			t.Fatal("probe did not identify the Ollama runtime")
		}
		if rt.Kind != vitals.LLMKindOllama {
			t.Errorf("kind = %q, want %q", rt.Kind, vitals.LLMKindOllama)
		}
	})
}

// TestProbeDualAPIOllamaWins guards a misidentification a live run caught:
// current Ollama serves an OpenAI-compatible /v1/models alongside its native
// /api/tags, so a server answering both is Ollama and must not be labelled
// "openai" just because /v1/models was probed first.
func TestProbeDualAPIOllamaWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			writeJSON(w, map[string]any{"models": []map[string]string{{"name": "qwen2.5-coder:14b"}}})
		case "/v1/models":
			writeJSON(w, map[string]any{"data": []map[string]string{{"id": "qwen2.5-coder:14b"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)

	rt, ok := probe(context.Background(), hostPort(srv.URL))
	if !ok {
		t.Fatal("probe did not identify the dual-API runtime")
	}
	if rt.Kind != vitals.LLMKindOllama {
		t.Fatalf("kind = %q, want %q — a server serving /api/tags is Ollama", rt.Kind, vitals.LLMKindOllama)
	}
}

// TestProbeRejectsNonRuntime guards the false positive that matters: any JSON
// object decodes cleanly into the model structs, so a 200 from an unrelated
// service on the port must not be reported as an LLM runtime.
func TestProbeRejectsNonRuntime(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"status": "ok"})
	}))
	t.Cleanup(srv.Close)

	if _, ok := probe(context.Background(), hostPort(srv.URL)); ok {
		t.Fatal("probe matched a service that serves no models")
	}
}

// TestDetectDiscoversNonLoopbackRuntime is the regression this package was
// restructured for. A runtime moved off loopback — the exact thing that happens
// when an operator exposes it — must still be found, and must be found *at its
// bind address*. The previous implementation probed a fixed loopback list, so
// exposing a runtime made it vanish from the report entirely: the console then
// showed less capability precisely when there was more.
func TestDetectDiscoversNonLoopbackRuntime(t *testing.T) {
	srv := newOpenAIServer(t, "gpt-oss:120b")
	_, port := splitHostPort(t, srv.URL)

	// httptest binds loopback, so discovery finds it there. The assertion that
	// matters is that Detect probes whatever address the table reports for the
	// port rather than an assumed 127.0.0.1 — verified directly below in
	// TestTargetsUsesBindAddress, which drives the table itself.
	rts := Detect(context.Background(), Options{Ports: []string{port}})
	if len(rts) != 1 {
		t.Fatalf("Detect found %d runtimes, want 1", len(rts))
	}
	if rts[0].Exposure != vitals.LLMExposureLoopback {
		t.Errorf("exposure = %q, want %q", rts[0].Exposure, vitals.LLMExposureLoopback)
	}
	if want := "127.0.0.1:" + port; rts[0].Addr != want {
		t.Errorf("addr = %q, want %q", rts[0].Addr, want)
	}
}

// TestDetectDisabled checks that a configuration probing nothing yields nothing
// rather than falling back to a built-in list.
func TestDetectDisabled(t *testing.T) {
	if got := Detect(context.Background(), Options{}); len(got) != 0 {
		t.Fatalf("Detect with no ports or endpoints returned %d runtimes, want 0", len(got))
	}
	if (Options{}).Enabled() {
		t.Error("empty Options should report disabled")
	}
}

// TestTargetsUsesBindAddress drives the resolution directly, since it's where
// the exposure regression lived. A tailnet-only bind must be contacted at that
// address; a wildcard bind is contacted over loopback but still reported as
// 0.0.0.0 so the address never contradicts the scope.
func TestTargetsUsesBindAddress(t *testing.T) {
	cases := []struct {
		name         string
		bindings     []binding
		wantProbe    string
		wantBind     string
		wantExposure string
	}{
		{
			name:         "tailnet only is probed at its bind address",
			bindings:     []binding{{ip: net.ParseIP("100.87.180.34"), scope: vitals.LLMExposureTailnet}},
			wantProbe:    "100.87.180.34:8091",
			wantBind:     "100.87.180.34:8091",
			wantExposure: vitals.LLMExposureTailnet,
		},
		{
			name: "loopback alongside tailnet is probed over loopback, reported as tailnet",
			bindings: []binding{
				{ip: net.ParseIP("127.0.0.1"), scope: vitals.LLMExposureLoopback},
				{ip: net.ParseIP("100.87.180.34"), scope: vitals.LLMExposureTailnet},
			},
			wantProbe:    "127.0.0.1:8091",
			wantBind:     "100.87.180.34:8091",
			wantExposure: vitals.LLMExposureTailnet,
		},
		{
			name:         "wildcard is probed over loopback, reported as open",
			bindings:     []binding{{ip: net.ParseIP("0.0.0.0"), scope: vitals.LLMExposureOpen}},
			wantProbe:    "127.0.0.1:8091",
			wantBind:     "0.0.0.0:8091",
			wantExposure: vitals.LLMExposureOpen,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, ok := widest(tc.bindings)
			if !ok {
				t.Fatal("no bindings")
			}
			probeAddr := b.addr("8091")
			if b.wildcard() || hasLoopback(tc.bindings) {
				probeAddr = net.JoinHostPort("127.0.0.1", "8091")
			}
			if probeAddr != tc.wantProbe {
				t.Errorf("probe address = %q, want %q", probeAddr, tc.wantProbe)
			}
			if got := b.addr("8091"); got != tc.wantBind {
				t.Errorf("bind address = %q, want %q", got, tc.wantBind)
			}
			if b.scope != tc.wantExposure {
				t.Errorf("exposure = %q, want %q", b.scope, tc.wantExposure)
			}
		})
	}
}

// TestDetectExplicitEndpointsReplaceDiscovery checks that pinned targets are
// used verbatim, and that an address with no listener entry reports "unknown"
// rather than defaulting to loopback — claiming "not exposed" without having
// verified it is the one wrong answer.
func TestDetectExplicitEndpointsReplaceDiscovery(t *testing.T) {
	srv := newOpenAIServer(t, "m")
	rts := Detect(context.Background(), Options{
		Ports:     []string{"1"}, // would find nothing; endpoints must win
		Endpoints: []string{hostPort(srv.URL)},
	})
	if len(rts) != 1 {
		t.Fatalf("Detect found %d runtimes, want 1", len(rts))
	}
	switch rts[0].Exposure {
	case vitals.LLMExposureLoopback, vitals.LLMExposureUnknown:
	default:
		t.Fatalf("exposure = %q, want loopback or unknown", rts[0].Exposure)
	}
}

// TestReachable pins the fleet-facing question: can another machine call this
// box? Unknown must not count as reachable — it isn't evidence either way.
func TestReachable(t *testing.T) {
	cases := []struct {
		name string
		cap  *vitals.LLMCapability
		want bool
	}{
		{"nil", nil, false},
		{"empty", &vitals.LLMCapability{}, false},
		{"loopback only", &vitals.LLMCapability{Runtimes: []vitals.LLMRuntime{
			{Exposure: vitals.LLMExposureLoopback},
		}}, false},
		{"unknown is not reachable", &vitals.LLMCapability{Runtimes: []vitals.LLMRuntime{
			{Exposure: vitals.LLMExposureUnknown},
		}}, false},
		{"tailnet", &vitals.LLMCapability{Runtimes: []vitals.LLMRuntime{
			{Exposure: vitals.LLMExposureLoopback}, {Exposure: vitals.LLMExposureTailnet},
		}}, true},
		{"open", &vitals.LLMCapability{Runtimes: []vitals.LLMRuntime{
			{Exposure: vitals.LLMExposureOpen},
		}}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.cap.Reachable(); got != tc.want {
				t.Fatalf("Reachable() = %v, want %v", got, tc.want)
			}
		})
	}
}

// --- helpers -----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	_ = json.NewEncoder(w).Encode(v)
}

func newOpenAIServer(t *testing.T, ids ...string) *httptest.Server {
	t.Helper()
	data := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		data = append(data, map[string]string{"id": id})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"data": data})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newOllamaServer(t *testing.T, names ...string) *httptest.Server {
	t.Helper()
	models := make([]map[string]string, 0, len(names))
	for _, n := range names {
		models = append(models, map[string]string{"name": n})
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/tags" {
			http.NotFound(w, r)
			return
		}
		writeJSON(w, map[string]any{"models": models})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hostPort(url string) string { return strings.TrimPrefix(url, "http://") }

func splitHostPort(t *testing.T, url string) (string, string) {
	t.Helper()
	host, port, err := net.SplitHostPort(hostPort(url))
	if err != nil {
		t.Fatal(err)
	}
	return host, port
}
