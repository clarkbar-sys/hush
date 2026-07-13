package main

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyExecStreamsFromAgent(t *testing.T) {
	// A stand-in agent that echoes an SSE run, flushing so the proxy relays it.
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/exec" || r.Method != http.MethodPost {
			t.Errorf("agent hit %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		if !strings.Contains(string(body), `"cmd"`) {
			t.Errorf("body not forwarded: %q", body)
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		io.WriteString(w, "data: {\"kind\":\"start\",\"pid\":42}\n\n")
		io.WriteString(w, "data: {\"kind\":\"exit\",\"code\":0}\n\n")
	}))
	defer agent.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/box/exec", strings.NewReader(`{"cmd":"echo hi"}`))
	proxyExec(rec, req, agent.Client(), Agent{Name: "box", Addr: agent.URL})

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("content-type = %q", ct)
	}
	if b := rec.Body.String(); !strings.Contains(b, `"kind":"start"`) || !strings.Contains(b, `"kind":"exit"`) {
		t.Errorf("relayed body missing frames: %q", b)
	}
}

func TestProxyExecRelaysDisabledStatus(t *testing.T) {
	agent := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "exec is disabled on this agent", http.StatusForbidden)
	}))
	defer agent.Close()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/box/exec", strings.NewReader(`{"cmd":"x"}`))
	proxyExec(rec, req, agent.Client(), Agent{Name: "box", Addr: agent.URL})

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestProxyExecRejectsGet(t *testing.T) {
	rec := httptest.NewRecorder()
	proxyExec(rec, httptest.NewRequest(http.MethodGet, "/api/machines/box/exec", nil), http.DefaultClient, Agent{Name: "box", Addr: "http://127.0.0.1:0"})
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestProxyExecUnreachableAgent(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/machines/box/exec", strings.NewReader(`{"cmd":"x"}`))
	// Port 0 never listens, so the dial fails fast.
	proxyExec(rec, req, http.DefaultClient, Agent{Name: "box", Addr: "http://127.0.0.1:0"})
	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

func TestExecCmdPreviewTruncates(t *testing.T) {
	long := strings.Repeat("a", 300)
	got := execCmdPreview([]byte(`{"cmd":"` + long + `"}`))
	if len([]rune(got)) > 201 || !strings.HasSuffix(got, "…") {
		t.Errorf("preview = %q (len %d)", got, len([]rune(got)))
	}
}
