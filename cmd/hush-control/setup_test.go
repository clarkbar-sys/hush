package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"tailscale.com/tsnet"
)

// needsSetup / hasTsnetState decide whether the first-run setup page runs.
func TestNeedsSetup(t *testing.T) {
	empty := t.TempDir()

	provisioned := t.TempDir()
	if err := os.WriteFile(filepath.Join(provisioned, "tailscaled.state"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name     string
		authKey  string
		stateDir string
		want     bool
	}{
		{"no key, no state -> setup", "", empty, true},
		{"no key, existing state -> no setup", "", provisioned, false},
		{"auth key present -> never setup", "tskey-auth-x", empty, false},
		{"auth key present, even with state -> no setup", "tskey-auth-x", provisioned, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := needsSetup(tc.authKey, tc.stateDir); got != tc.want {
				t.Fatalf("needsSetup(%q, dir) = %v, want %v", tc.authKey, got, tc.want)
			}
		})
	}
}

// GET / renders the form with the warning banner and prefilled hostname.
func TestSetupFormRenders(t *testing.T) {
	s := &setupServer{defaultHostname: "hush", result: make(chan *tsnet.Server, 1)}
	rr := httptest.NewRecorder()
	s.routes().ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/", nil))

	if rr.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rr.Code)
	}
	body := rr.Body.String()
	for _, want := range []string{"INSECURE SETUP PAGE", `name="authkey"`, `value="hush"`} {
		if !strings.Contains(body, want) {
			t.Errorf("form body missing %q", want)
		}
	}
}

// A missing auth key is rejected without invoking the provisioner.
func TestSetupSubmitEmptyKey(t *testing.T) {
	provisionCalled := false
	s := &setupServer{
		defaultHostname: "hush",
		result:          make(chan *tsnet.Server, 1),
		provision: func(context.Context, string, string) (*tsnet.Server, string, error) {
			provisionCalled = true
			return nil, "", nil
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("authkey=&hostname=hush"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.routes().ServeHTTP(rr, req)

	if provisionCalled {
		t.Fatal("provision should not run with an empty key")
	}
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "Paste your Tailscale auth key") {
		t.Error("expected the missing-key prompt in the re-rendered form")
	}
}

// A provisioner error re-renders the form with the message and does not signal
// completion (the setup loop must keep waiting for a working key).
func TestSetupSubmitProvisionError(t *testing.T) {
	s := &setupServer{
		defaultHostname: "hush",
		result:          make(chan *tsnet.Server, 1),
		provision: func(context.Context, string, string) (*tsnet.Server, string, error) {
			return nil, "", errors.New("invalid key")
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("authkey=tskey-bad&hostname=hush"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.routes().ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "invalid key") {
		t.Error("expected the provisioner error surfaced in the form")
	}
	if s.done {
		t.Error("a failed provision must not mark setup done")
	}
	select {
	case <-s.result:
		t.Error("a failed provision must not signal completion")
	default:
	}
}

// A successful submit renders the HTTPS URL, marks done, and hands the live node
// back over the result channel for the caller to serve the console.
func TestSetupSubmitSuccess(t *testing.T) {
	sentinel := &tsnet.Server{Hostname: "hush"}
	s := &setupServer{
		defaultHostname: "hush",
		result:          make(chan *tsnet.Server, 1),
		provision: func(_ context.Context, key, host string) (*tsnet.Server, string, error) {
			if key != "tskey-good" || host != "nas" {
				t.Errorf("provision got (%q,%q), want (tskey-good,nas)", key, host)
			}
			return sentinel, "https://nas.example.ts.net", nil
		},
	}
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/setup", strings.NewReader("authkey=tskey-good&hostname=nas"))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	s.routes().ServeHTTP(rr, req)

	if !strings.Contains(rr.Body.String(), "https://nas.example.ts.net") {
		t.Error("expected the console URL on the success page")
	}
	if !s.done {
		t.Error("a successful provision must mark setup done")
	}
	select {
	case got := <-s.result:
		if got != sentinel {
			t.Error("result channel delivered the wrong server")
		}
	default:
		t.Error("a successful provision must signal completion")
	}
}
