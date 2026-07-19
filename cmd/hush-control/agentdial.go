// How hush-control reaches agents. In tsnet mode the control plane is its own
// userspace tailnet node (see tsnet.go): it joins the tailnet with its own
// WireGuard stack and does NOT add routes to the host kernel. That's the whole
// point of tsnet — the box running hush-control needn't have Tailscale
// installed at all. But it means a plain net.Dial to an agent's 100.x tailnet
// address has no route and fails: the kernel doesn't know the tailnet exists,
// only the tsnet node does. So every connection to an agent (the fleet poll,
// discovery probes, and the /browse, /top, /backups, ... proxies) must dial
// through the tsnet node's own stack via srv.Dial, not the kernel.
//
// agentDialer is the single seam that makes that switch. Every agent-facing
// http.Client is built over one, so wiring the tsnet node in one place
// (useTsnet, called once the node is up) redirects all of them at once. Until
// then — during first-run setup, and in tests — it dials directly, which is
// correct: agents are unreachable over the tailnet anyway before the node
// joins, and tests reach httptest servers on loopback where a kernel dial is
// exactly right.
package main

import (
	"context"
	"net"
	"net/http"
	"sync"
	"time"

	"tailscale.com/tsnet"
)

// agentDialer holds the dial function used to reach agents, swappable at
// runtime so the whole fleet's connections move onto the tailnet node the
// instant it comes up. It tracks the transports built over it so a swap can
// drop their pooled connections and force a re-dial down the new path. Safe
// for concurrent use.
type agentDialer struct {
	mu    sync.RWMutex
	dial  func(ctx context.Context, network, addr string) (net.Conn, error)
	trans []*http.Transport
}

// directDial mirrors http.DefaultTransport's dialer, so a direct (pre-tsnet)
// dial keeps the standard 30s connect timeout instead of hanging on the
// no-timeout streaming clients.
var directDial = (&net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}).DialContext

// newAgentDialer returns a dialer that connects directly (host kernel) until
// useTsnet redirects it through the tailnet node.
func newAgentDialer() *agentDialer {
	return &agentDialer{dial: directDial}
}

// DialContext dials addr using the currently selected path (direct or tsnet).
// Its signature matches http.Transport.DialContext so it drops straight in.
func (d *agentDialer) DialContext(ctx context.Context, network, addr string) (net.Conn, error) {
	d.mu.RLock()
	dial := d.dial
	d.mu.RUnlock()
	return dial(ctx, network, addr)
}

// useTsnet routes every subsequent agent dial through the tsnet node's
// userspace stack. Called once, right after the node is up, so all clients
// built over this dialer start reaching agents over the tailnet.
func (d *agentDialer) useTsnet(s *tsnet.Server) {
	d.use(s.Dial)
}

// use swaps the dial path and drops every pooled connection dialed the old way
// (e.g. a kernel dial on a box that's also a Tailscale node), so the next
// request re-dials down the new path instead of reusing a stale keep-alive.
// useTsnet is the production caller; tests drive it directly.
func (d *agentDialer) use(dial func(ctx context.Context, network, addr string) (net.Conn, error)) {
	d.mu.Lock()
	d.dial = dial
	trans := append([]*http.Transport(nil), d.trans...)
	d.mu.Unlock()
	for _, t := range trans {
		t.CloseIdleConnections()
	}
}

// client builds an http.Client for talking to agents over this dialer,
// carrying http.DefaultTransport's settings (proxy, idle-conn pooling) and
// only substituting the dial. timeout is the overall per-request budget; pass
// 0 for the streaming clients (/file, /backups run) that must not be cut off. The
// transport is tracked so a later use() swap can flush its pooled connections.
func (d *agentDialer) client(timeout time.Duration) *http.Client {
	t := http.DefaultTransport.(*http.Transport).Clone()
	t.DialContext = d.DialContext
	d.mu.Lock()
	d.trans = append(d.trans, t)
	d.mu.Unlock()
	return &http.Client{Timeout: timeout, Transport: t}
}
