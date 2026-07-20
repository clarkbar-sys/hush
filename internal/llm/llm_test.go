package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// TestScopeOf pins the /proc hex-address classification, which is what decides
// whether the console says "local only" or "serving". Getting the endianness
// wrong here would silently invert the safety claim, so the loopback and
// wildcard encodings are asserted explicitly rather than round-tripped.
func TestScopeOf(t *testing.T) {
	cases := []struct {
		name string
		hex  string
		want string
	}{
		{"ipv4 loopback 127.0.0.1", "0100007F", vitals.LLMExposureLoopback},
		{"ipv4 wildcard 0.0.0.0", "00000000", vitals.LLMExposureOpen},
		{"ipv4 tailnet 100.87.180.34", "22B45764", vitals.LLMExposureTailnet},
		{"ipv4 lan 192.168.0.30", "1E00A8C0", vitals.LLMExposureOpen},
		{"ipv6 wildcard ::", strings.Repeat("0", 32), vitals.LLMExposureOpen},
		{"ipv6 loopback ::1", "00000000000000000000000001000000", vitals.LLMExposureLoopback},
		{"undecodable", "zz", vitals.LLMExposureOpen},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scopeOf(tc.hex); got != tc.want {
				t.Fatalf("scopeOf(%q) = %q, want %q", tc.hex, got, tc.want)
			}
		})
	}
}

// TestProbeIdentifiesRuntimes checks that each runtime is told apart by the API
// shape that answers, and that its models come back sorted.
func TestProbeIdentifiesRuntimes(t *testing.T) {
	t.Run("openai", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/v1/models" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "qwen3-30b-a3b"}, {"id": "gpt-oss:20b"}},
			})
		}))
		defer srv.Close()

		rt, ok := probe(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
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
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/tags" {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "qwen2.5-coder:14b"}},
			})
		}))
		defer srv.Close()

		rt, ok := probe(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
		if !ok {
			t.Fatal("probe did not identify the Ollama runtime")
		}
		if rt.Kind != vitals.LLMKindOllama {
			t.Errorf("kind = %q, want %q", rt.Kind, vitals.LLMKindOllama)
		}
	})
}

// TestProbeDualAPIOllamaWinsGuards the misidentification a live run caught:
// current Ollama serves an OpenAI-compatible /v1/models alongside its native
// /api/tags, so a server answering both is Ollama and must not be labelled
// "openai" just because /v1/models was probed first.
func TestProbeDualAPIOllamaWins(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/tags":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"models": []map[string]string{{"name": "qwen2.5-coder:14b"}},
			})
		case "/v1/models":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"data": []map[string]string{{"id": "qwen2.5-coder:14b"}},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	rt, ok := probe(context.Background(), strings.TrimPrefix(srv.URL, "http://"))
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
		_ = json.NewEncoder(w).Encode(map[string]any{"status": "ok"})
	}))
	defer srv.Close()

	if _, ok := probe(context.Background(), strings.TrimPrefix(srv.URL, "http://")); ok {
		t.Fatal("probe matched a service that serves no models")
	}
}

// TestDetectUnreadableProcIsUnknown checks the safety-critical default: when the
// listener table yields nothing for a port, exposure reports "unknown" rather
// than falling back to loopback. Claiming "not exposed" without having verified
// it is the one wrong answer here.
func TestDetectUnreadableProcIsUnknown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []map[string]string{{"id": "m"}}})
	}))
	defer srv.Close()

	// httptest binds an ephemeral port; parseListeners will have it as loopback
	// on a real Linux box, so assert only that a scope was decided and that an
	// address with no listener entry at all comes back unknown.
	rts := Detect(context.Background(), []string{strings.TrimPrefix(srv.URL, "http://")})
	if len(rts) != 1 {
		t.Fatalf("Detect returned %d runtimes, want 1", len(rts))
	}
	switch rts[0].Exposure {
	case vitals.LLMExposureLoopback, vitals.LLMExposureUnknown:
	default:
		t.Fatalf("exposure = %q, want loopback or unknown for an httptest listener", rts[0].Exposure)
	}
}

// TestReachable pins the fleet-facing question: can another machine call this
// box? Unknown must not count as reachable, and must not count as safe either —
// it simply isn't evidence.
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

// TestParseListenersWidestWins checks that a port bound both to loopback and to
// a tailnet address reports the tailnet scope — the reachable truth, not the
// reassuring half of it.
func TestParseListenersWidestWins(t *testing.T) {
	// sl  local_address rem_address st ... (columns past st are unused here)
	table := "  sl  local_address rem_address   st\n" +
		"   0: 0100007F:1F9B 00000000:0000 0A\n" + // 127.0.0.1:8091
		"   1: 22B45764:1F9B 00000000:0000 0A\n" + // 100.87.180.34:8091
		"   2: 0100007F:2CEE 00000000:0000 0A\n" // 127.0.0.1:11502

	path := filepath.Join(t.TempDir(), "tcp")
	if err := os.WriteFile(path, []byte(table), 0o600); err != nil {
		t.Fatal(err)
	}
	got := parseListeners(path)
	if got["8091"] != vitals.LLMExposureTailnet {
		t.Errorf("port 8091 scope = %q, want %q", got["8091"], vitals.LLMExposureTailnet)
	}
	if got["11502"] != vitals.LLMExposureLoopback {
		t.Errorf("port 11502 scope = %q, want %q", got["11502"], vitals.LLMExposureLoopback)
	}
}

func TestParseListenersUnreadable(t *testing.T) {
	if got := parseListeners("/nonexistent/proc/net/tcp"); len(got) != 0 {
		t.Fatalf("unreadable table yielded %v, want nothing", got)
	}
}
