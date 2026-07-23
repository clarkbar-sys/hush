package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// sseRuntime is a fake OpenAI-compatible runtime: it answers
// /v1/chat/completions with a short SSE stream, after asserting that control
// forced stream:true. record, if non-nil, captures the decoded request body so
// a test can check model selection and the stream flag.
func sseRuntime(t *testing.T, record *map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("runtime got path %q, want /v1/chat/completions", r.URL.Path)
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("runtime decode body: %v", err)
		}
		if body["stream"] != true {
			t.Errorf("runtime: stream = %v, want true (control must force it)", body["stream"])
		}
		if record != nil {
			*record = body
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		fl, _ := w.(http.Flusher)
		for _, tok := range []string{"Hel", "lo"} {
			fmt.Fprintf(w, "data: {\"choices\":[{\"delta\":{\"content\":%q}}]}\n\n", tok)
			if fl != nil {
				fl.Flush()
			}
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
		if fl != nil {
			fl.Flush()
		}
	}))
}

// chatFleet wires an agent whose /vitals advertises one runtime (at runtimeAddr,
// with the given exposure and models) to a control mux, so a POST to
// /api/machines/nas/llm/chat exercises the whole resolve → pick → proxy path.
// ip is the machine's tailnet IP, used for the wildcard-bind substitution.
func chatFleet(t *testing.T, ip, runtimeAddr, exposure string, models ...string) http.Handler {
	t.Helper()
	agent := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/vitals" {
			http.Error(w, "not found", http.StatusNotFound)
			return
		}
		json.NewEncoder(w).Encode(vitals.Snapshot{
			Host:   "nas",
			Status: "good",
			LLM: &vitals.LLMCapability{Runtimes: []vitals.LLMRuntime{
				{Kind: vitals.LLMKindOpenAI, Addr: runtimeAddr, Exposure: exposure, Models: models},
			}},
		})
	})
	srv := httptest.NewServer(agent)
	t.Cleanup(srv.Close)
	store := newTestStore(t, []Agent{{Name: "nas", Addr: srv.URL, IP: ip}})
	mux, _ := buildMux(store, muxDiscoverer(store), nil, "")
	return mux
}

func postChat(mux http.Handler, host, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/machines/"+host+"/llm/chat", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	return rec
}

func TestLLMChatStreamsCompletion(t *testing.T) {
	var got map[string]any
	rt := sseRuntime(t, &got)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	rec := postChat(mux, "nas", `{"model":"qwen","messages":[{"role":"user","content":"hi"}]}`)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	out := rec.Body.String()
	if !strings.Contains(out, `"content":"Hel"`) || !strings.Contains(out, `"content":"lo"`) || !strings.Contains(out, "[DONE]") {
		t.Fatalf("stream not relayed verbatim: %q", out)
	}
	if got["model"] != "qwen" {
		t.Fatalf("runtime saw model %v, want qwen", got["model"])
	}
}

func TestLLMChatRejectsGET(t *testing.T) {
	rt := sseRuntime(t, nil)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	req := httptest.NewRequest(http.MethodGet, "/api/machines/nas/llm/chat", nil)
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("GET: status = %d, want 405", rec.Code)
	}
}

func TestLLMChatUnknownHost(t *testing.T) {
	rt := sseRuntime(t, nil)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	rec := postChat(mux, "ghost", `{"model":"qwen","messages":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown host: status = %d, want 404", rec.Code)
	}
}

func TestLLMChatModelNotServed(t *testing.T) {
	rt := sseRuntime(t, nil)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	rec := postChat(mux, "nas", `{"model":"not-here","messages":[]}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unserved model: status = %d, want 404", rec.Code)
	}
}

// A model served only over loopback is signed off: control's node can't reach
// it, so the 409 surfaces the reachability gate rather than dialing a runtime
// bound to 127.0.0.1.
func TestLLMChatLoopbackSignedOff(t *testing.T) {
	rt := sseRuntime(t, nil)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", "127.0.0.1:8091", vitals.LLMExposureLoopback, "qwen")

	rec := postChat(mux, "nas", `{"model":"qwen","messages":[]}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("loopback runtime: status = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "signed off") {
		t.Fatalf("409 body = %q, want it to name the sign-off", rec.Body.String())
	}
}

// A wildcard bind (0.0.0.0) isn't a routable host; the proxy must substitute the
// box's tailnet IP. The runtime here listens on loopback, and the machine's IP
// is set to that loopback so the substituted target actually lands on it —
// proving the substitution happened (a literal 0.0.0.0 dial would fail).
func TestLLMChatWildcardUsesTailnetIP(t *testing.T) {
	var got map[string]any
	rt := sseRuntime(t, &got)
	defer rt.Close()
	_, port, err := net.SplitHostPort(rt.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	mux := chatFleet(t, "127.0.0.1", "0.0.0.0:"+port, vitals.LLMExposureOpen, "qwen")

	rec := postChat(mux, "nas", `{"model":"qwen","messages":[]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("wildcard bind: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if got["model"] != "qwen" {
		t.Fatalf("runtime never received the request; substitution likely failed")
	}
}

// With no model named, the proxy takes the first reachable runtime.
func TestLLMChatModelOmittedPicksReachable(t *testing.T) {
	var got map[string]any
	rt := sseRuntime(t, &got)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	rec := postChat(mux, "nas", `{"messages":[{"role":"user","content":"hi"}]}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("model omitted: status = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
}

func TestLLMChatBadBody(t *testing.T) {
	rt := sseRuntime(t, nil)
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	rec := postChat(mux, "nas", `not json`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad body: status = %d, want 400", rec.Code)
	}
}

// A closed IM window must stop the box working: proxyLLMChat threads r.Context()
// into the upstream request, so cancelling it (what a client disconnect does)
// cancels the runtime call. The fake runtime blocks after its first chunk and
// reports when its own request context fires.
func TestLLMChatCancelsUpstreamOnDisconnect(t *testing.T) {
	started := make(chan struct{})
	canceled := make(chan struct{})
	rt := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		if fl, ok := w.(http.Flusher); ok {
			fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"hi\"}}]}\n\n")
			fl.Flush()
		}
		close(started)
		<-r.Context().Done() // block until the caller's cancellation propagates here
		close(canceled)
	}))
	defer rt.Close()
	mux := chatFleet(t, "100.71.4.2", rt.Listener.Addr().String(), vitals.LLMExposureTailnet, "qwen")

	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/api/machines/nas/llm/chat",
		strings.NewReader(`{"model":"qwen","messages":[]}`)).WithContext(ctx)
	done := make(chan struct{})
	go func() { mux.ServeHTTP(httptest.NewRecorder(), req); close(done) }()

	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("runtime never received the request")
	}
	cancel() // simulate the client (IM window) disconnecting
	select {
	case <-canceled:
	case <-time.After(5 * time.Second):
		t.Fatal("upstream request was not cancelled when the client disconnected")
	}
	<-done
}

func rt(kind, addr, exposure string, models ...string) vitals.LLMRuntime {
	return vitals.LLMRuntime{Kind: kind, Addr: addr, Exposure: exposure, Models: models}
}

// TestRuntimeChatURL pins the address-building rule directly (the httptest path
// can't distinguish 0.0.0.0 from 127.0.0.1 on Linux, since both dial loopback).
func TestRuntimeChatURL(t *testing.T) {
	tests := []struct {
		name, addr, exposure, ip, want string
		ok                             bool
	}{
		{"tailnet host as-is", "100.71.4.2:8091", vitals.LLMExposureTailnet, "100.71.4.2", "http://100.71.4.2:8091/v1/chat/completions", true},
		{"wildcard swaps to tailnet ip", "0.0.0.0:11434", vitals.LLMExposureOpen, "100.71.9.9", "http://100.71.9.9:11434/v1/chat/completions", true},
		{"ipv6 wildcard swaps to tailnet ip", "[::]:8091", vitals.LLMExposureOpen, "100.71.9.9", "http://100.71.9.9:8091/v1/chat/completions", true},
		{"ipv6 host is bracketed", "[fd7a::1]:8091", vitals.LLMExposureTailnet, "", "http://[fd7a::1]:8091/v1/chat/completions", true},
		{"wildcard with no tailnet ip is unroutable", "0.0.0.0:8091", vitals.LLMExposureOpen, "", "", false},
		{"blank host with no tailnet ip is unroutable", ":8091", vitals.LLMExposureOpen, "", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := runtimeChatURL(rt(vitals.LLMKindOpenAI, tc.addr, tc.exposure), tc.ip)
			if ok != tc.ok || got != tc.want {
				t.Fatalf("runtimeChatURL(%q, %q) = (%q, %v), want (%q, %v)", tc.addr, tc.ip, got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestPickRuntime covers the selection paths the end-to-end httptest fleet
// (single runtime only) can't reach — most importantly a model served by both a
// loopback and a reachable runtime, which must pick the reachable one.
func TestPickRuntime(t *testing.T) {
	mach := func(rts ...vitals.LLMRuntime) Machine {
		return Machine{ID: "nas", IP: "100.71.4.2", LLM: &vitals.LLMCapability{Runtimes: rts}}
	}
	tests := []struct {
		name       string
		m          Machine
		model      string
		wantStatus int
		wantAddr   string // runtime addr expected on success
	}{
		{"no LLM at all", Machine{ID: "nas"}, "qwen", http.StatusNotFound, ""},
		{"no runtimes", mach(), "qwen", http.StatusNotFound, ""},
		{"model not served", mach(rt(vitals.LLMKindOpenAI, "100.71.4.2:8091", vitals.LLMExposureTailnet, "qwen")), "llama", http.StatusNotFound, ""},
		{"loopback only", mach(rt(vitals.LLMKindOpenAI, "127.0.0.1:8091", vitals.LLMExposureLoopback, "qwen")), "qwen", http.StatusConflict, ""},
		{"unknown exposure only", mach(rt(vitals.LLMKindOpenAI, "10.0.0.5:8091", vitals.LLMExposureUnknown, "qwen")), "qwen", http.StatusConflict, ""},
		{"reachable wins", mach(
			rt(vitals.LLMKindOpenAI, "127.0.0.1:8091", vitals.LLMExposureLoopback, "qwen"),
			rt(vitals.LLMKindOllama, "100.71.4.2:11434", vitals.LLMExposureTailnet, "qwen"),
		), "qwen", 0, "100.71.4.2:11434"},
		{"model omitted picks first reachable", mach(
			rt(vitals.LLMKindOpenAI, "127.0.0.1:8091", vitals.LLMExposureLoopback, "deepseek"),
			rt(vitals.LLMKindOllama, "100.71.4.2:11434", vitals.LLMExposureOpen, "qwen"),
		), "", 0, "100.71.4.2:11434"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, status, _ := pickRuntime(tc.m, tc.model)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}
			if status == 0 && got.Addr != tc.wantAddr {
				t.Fatalf("picked runtime %q, want %q", got.Addr, tc.wantAddr)
			}
		})
	}
}

// Guard that streamSSE flushes each chunk rather than buffering to the end, so
// tokens surface as they arrive.
func TestStreamSSEFlushesPerChunk(t *testing.T) {
	pr, pw := io.Pipe()
	rec := httptest.NewRecorder()
	go func() {
		fmt.Fprint(pw, "data: one\n\n")
		pw.Close()
	}()
	streamSSE(rec, bufio.NewReader(pr))
	if !rec.Flushed {
		t.Fatal("streamSSE never flushed")
	}
	if !strings.Contains(rec.Body.String(), "data: one") {
		t.Fatalf("body = %q, want the chunk", rec.Body.String())
	}
}
