package main

import (
	"bufio"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// sseEvents drives the exec handler against a body and returns the decoded
// events. It uses no run-as allowlist, so plain commands run as the test user.
func sseEvents(t *testing.T, body string) []map[string]any {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(body))
	execHandler(nil)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Fatalf("content-type = %q, want text/event-stream", ct)
	}
	var evs []map[string]any
	sc := bufio.NewScanner(strings.NewReader(rec.Body.String()))
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(strings.TrimSpace(line[5:])), &m); err != nil {
			t.Fatalf("bad SSE frame %q: %v", line, err)
		}
		evs = append(evs, m)
	}
	return evs
}

func TestHandleExecStreamsRun(t *testing.T) {
	evs := sseEvents(t, `{"cmd":"echo hi"}`)
	if len(evs) < 2 {
		t.Fatalf("want start+exit at least, got %+v", evs)
	}
	if evs[0]["kind"] != "start" {
		t.Errorf("first frame = %v, want start", evs[0])
	}
	var out, last string
	for _, e := range evs {
		if e["kind"] == "out" {
			out += e["data"].(string)
		}
		last = e["kind"].(string)
	}
	if !strings.Contains(out, "hi") {
		t.Errorf("output = %q, want hi", out)
	}
	if last != "exit" {
		t.Errorf("last frame kind = %q, want exit", last)
	}
}

func TestHandleExecRejectsGet(t *testing.T) {
	rec := httptest.NewRecorder()
	execHandler(nil)(rec, httptest.NewRequest(http.MethodGet, "/exec", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("status = %d, want 405", rec.Code)
	}
}

func TestHandleExecRequiresCmd(t *testing.T) {
	rec := httptest.NewRecorder()
	execHandler(nil)(rec, httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"cmd":"  "}`)))
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

// A run-as user that isn't on the agent's allowlist is refused with 403 before
// anything is executed — the allowlist is the ceiling on what a caller can
// become, so this gate is the security-critical one.
func TestHandleExecRejectsUnlistedRunAsUser(t *testing.T) {
	runAs := map[string]bool{"media": true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"cmd":"id","user":"root"}`))
	execHandler(runAs)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}

// With the feature off (empty allowlist), any run-as request is refused — a
// caller can't reach sudo on an agent that never opted in.
func TestHandleExecRejectsRunAsWhenDisabled(t *testing.T) {
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"cmd":"id","user":"media"}`))
	execHandler(nil)(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d (%s), want 403", rec.Code, rec.Body.String())
	}
}

// A blank/omitted user is the historical path: it runs directly, never touching
// sudo, even when a run-as allowlist is configured.
func TestHandleExecBlankUserRunsDirectly(t *testing.T) {
	runAs := map[string]bool{"media": true}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec", strings.NewReader(`{"cmd":"echo hi","user":"  "}`))
	execHandler(runAs)(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d (%s), want 200", rec.Code, rec.Body.String())
	}
}
