package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

// identityGate is the only tsnet-mode logic verifiable without a live tailnet:
// it decides who may reach the console from a WhoIs result. The tsnet plumbing
// itself needs a real TS_AUTHKEY + tailnet and is exercised by the operator.
func TestIdentityGate(t *testing.T) {
	ok := func(login string) func(context.Context, string) (string, error) {
		return func(context.Context, string) (string, error) { return login, nil }
	}
	fail := func(context.Context, string) (string, error) {
		return "", errors.New("no identity")
	}

	cases := []struct {
		name  string
		whois func(context.Context, string) (string, error)
		allow []string
		want  int
	}{
		{"no identity is rejected", fail, nil, http.StatusForbidden},
		{"any member allowed when allowlist empty", ok("someone@example.net"), nil, http.StatusOK},
		{"listed login allowed", ok("me@example.com"), []string{"me@example.com"}, http.StatusOK},
		{"allowlist is case-insensitive", ok("Me@Example.com"), []string{"me@example.com"}, http.StatusOK},
		{"unlisted login rejected", ok("intruder@example.com"), []string{"me@example.com"}, http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusOK)
			})
			h := identityGate(tc.whois, tc.allow, next)
			rr := httptest.NewRecorder()
			h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/api/fleet", nil))
			if rr.Code != tc.want {
				t.Fatalf("status = %d, want %d", rr.Code, tc.want)
			}
		})
	}
}
