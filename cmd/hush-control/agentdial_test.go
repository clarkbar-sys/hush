package main

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAgentDialerClientUsesDialer proves an http.Client built over the dialer
// actually connects through the dialer's dial func — the seam that lets tsnet
// mode redirect every agent connection onto the tailnet node.
func TestAgentDialerClientUsesDialer(t *testing.T) {
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "hello")
	}))
	defer backend.Close()

	d := newAgentDialer()
	resp, err := d.client(0).Get(backend.URL)
	if err != nil {
		t.Fatalf("get through dialer: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "hello" {
		t.Fatalf("body = %q, want %q", body, "hello")
	}
}

// TestAgentDialerRedirectsAfterSwap proves that swapping the dial func (what
// useTsnet does when the node comes up) redirects a client's connections to a
// different backend, even for the same requested address. This is the whole
// point of the seam: in tsnet mode the same http://100.x:8765 request must
// reach the agent over the tailnet node rather than the host kernel.
func TestAgentDialerRedirectsAfterSwap(t *testing.T) {
	kernel := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "kernel")
	}))
	defer kernel.Close()
	tailnet := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "tailnet")
	}))
	defer tailnet.Close()

	d := newAgentDialer()
	client := d.client(0)

	got := func() string {
		resp, err := client.Get(kernel.URL)
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b)
	}

	if s := got(); s != "kernel" {
		t.Fatalf("before swap = %q, want %q", s, "kernel")
	}

	// Mimic useTsnet: install a dial func that reaches the tailnet backend
	// regardless of the requested address, the way srv.Dial routes onto the
	// tailnet stack. (useTsnet itself needs a live *tsnet.Server, so we drive
	// the same use() seam it does — which also flushes the pooled kernel conn.)
	tailnetAddr := tailnet.Listener.Addr().String()
	d.use(func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, tailnetAddr)
	})

	if s := got(); s != "tailnet" {
		t.Fatalf("after swap = %q, want %q", s, "tailnet")
	}
}
