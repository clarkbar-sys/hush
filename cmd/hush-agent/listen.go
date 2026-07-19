package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"time"
)

// tailnetListen is the sentinel -listen value that binds the agent to this
// machine's Tailscale address instead of a fixed host:port. It resolves the
// tailnet IP at startup, so the fleet reaches the agent over the tailnet
// without exposing it on any LAN or public interface — and without the
// operator having to hardcode a 100.x address that isn't known at install time.
const tailnetListen = "tailnet"

// defaultAgentPort is the port the agent listens on, and the one hush-control
// probes during tailnet discovery (see cmd/hush-control/discover.go). Used when
// -listen is the bare "tailnet" sentinel with no explicit port.
const defaultAgentPort = "8765"

// tailnetCGNAT is the 100.64.0.0/10 carrier-grade NAT range Tailscale draws
// every node's IPv4 from. Matching the range (rather than an interface name
// like "tailscale0") keeps detection portable across userspace mode and the
// various platform interface names.
const tailnetCGNAT = "100.64.0.0/10"

// tailnetPort reports whether listen requests tailnet binding and, if so, the
// port to bind. It accepts the bare sentinel "tailnet" (default port) and
// "tailnet:PORT" for an explicit port. Any other value is a literal listen
// address and returns ok=false.
func tailnetPort(listen string) (port string, ok bool) {
	if listen == tailnetListen {
		return defaultAgentPort, true
	}
	if rest, found := strings.CutPrefix(listen, tailnetListen+":"); found {
		return rest, true
	}
	return "", false
}

// tailnetIPv4 returns the first address in the Tailscale CGNAT range from addrs,
// which is the host's tailnet IPv4. ok is false when none is present (tailscale
// is down or not yet up).
func tailnetIPv4(addrs []net.Addr) (ip net.IP, ok bool) {
	_, cgnat, err := net.ParseCIDR(tailnetCGNAT)
	if err != nil { // unreachable: constant CIDR
		return nil, false
	}
	for _, a := range addrs {
		var candidate net.IP
		switch v := a.(type) {
		case *net.IPNet:
			candidate = v.IP
		case *net.IPAddr:
			candidate = v.IP
		}
		if v4 := candidate.To4(); v4 != nil && cgnat.Contains(v4) {
			return v4, true
		}
	}
	return nil, false
}

// waitForTailnetIPv4 polls interfaceAddrs until a tailnet address appears,
// retrying every interval up to attempts times. This absorbs the boot-time race
// where the agent (ordered only After=network-online.target) starts before
// tailscaled has assigned the 100.x address: rather than crash and lean on a
// systemd restart, it waits for the interface to come up and binds cleanly.
func waitForTailnetIPv4(interfaceAddrs func() ([]net.Addr, error), interval time.Duration, attempts int) (net.IP, error) {
	var lastErr error
	for i := 0; i < attempts; i++ {
		addrs, err := interfaceAddrs()
		if err != nil {
			lastErr = err
		} else if ip, ok := tailnetIPv4(addrs); ok {
			return ip, nil
		}
		if i == 0 {
			log.Printf("hush-agent: waiting for a tailnet address (%s) to appear — is tailscale up?", tailnetCGNAT)
		}
		if i < attempts-1 {
			time.Sleep(interval)
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("no tailnet (%s) address after %d attempts: %w", tailnetCGNAT, attempts, lastErr)
	}
	return nil, fmt.Errorf("no tailnet (%s) address after %d attempts", tailnetCGNAT, attempts)
}

// resolveListen turns a -listen value into a concrete host:port. A literal
// address passes through unchanged; the "tailnet" sentinel resolves to this
// machine's Tailscale IPv4, waiting for it if tailscale is still coming up.
func resolveListen(listen string) (string, error) {
	// An empty value means -listen was supplied but blank — most often a systemd
	// unit expanding ${HUSH_AGENT_LISTEN} when /etc/hush/agent.env is missing. The
	// flag's own default never applies there because the flag *was* provided, so
	// without this guard the empty string reaches net.Listen, which reads it as
	// ":80" and crash-loops with "permission denied" under the unprivileged hush
	// user. Fall back to the same default port the flag documents.
	if listen == "" {
		listen = ":" + defaultAgentPort
	}
	port, ok := tailnetPort(listen)
	if !ok {
		return listen, nil
	}
	// ~2 minutes of tolerance for tailscaled to assign the tailnet IP on boot.
	ip, err := waitForTailnetIPv4(net.InterfaceAddrs, 2*time.Second, 60)
	if err != nil {
		return "", err
	}
	addr := net.JoinHostPort(ip.String(), port)
	log.Printf("hush-agent: -listen %s resolved to %s (tailnet-only)", listen, addr)
	return addr, nil
}
