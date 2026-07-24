// Package netlisten reads the kernel's own listener table (/proc/net/tcp{,6})
// and classifies how far each LISTEN socket reaches — loopback, tailnet, or
// open. It is the shared, privilege-free way hush answers "what is bound on
// this port, and can anything off-box reach it", used both by LLM-runtime
// detection and by opencode-server detection.
//
// Reading the bind table rather than probing a fixed address is what makes a
// report survive the interesting case: the moment an operator moves a service
// off loopback, a loopback-only probe stops finding it and would report *less*
// reach than before. The listener table finds a socket wherever it binds, and
// its scope is a fact about the socket rather than an inference from a probe.
package netlisten

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Exposure scopes, narrowest first. Unknown is deliberately distinct from
// loopback: it means the listener table could not be read, so "not exposed"
// was never verified and must not be claimed.
const (
	ExposureLoopback = "loopback"
	ExposureTailnet  = "tailnet"
	ExposureOpen     = "open"
	ExposureUnknown  = "unknown"
)

// Binding is one LISTEN socket from /proc/net/tcp{,6}.
type Binding struct {
	IP    net.IP // nil when the address could not be decoded
	Scope string
}

// Wildcard reports whether the socket is bound to every interface (0.0.0.0 / ::).
func (b Binding) Wildcard() bool { return b.IP != nil && b.IP.IsUnspecified() }

// Addr renders the bind address for reporting. A binding whose address didn't
// decode is described as the wildcard it was conservatively scored as, so the
// reported address never contradicts the reported scope.
func (b Binding) Addr(port string) string {
	if b.IP == nil {
		return net.JoinHostPort("0.0.0.0", port)
	}
	return net.JoinHostPort(b.IP.String(), port)
}

// scopeRank orders bind scopes by how far they reach.
var scopeRank = map[string]int{
	ExposureLoopback: 0,
	ExposureTailnet:  1,
	ExposureOpen:     2,
}

// Widest returns the binding that reaches furthest. Widest wins because a
// service bound to both loopback and a tailnet address is reachable off-box,
// and must not report as loopback just because a loopback bind also exists.
func Widest(bs []Binding) (Binding, bool) {
	if len(bs) == 0 {
		return Binding{}, false
	}
	out := bs[0]
	for _, b := range bs[1:] {
		if scopeRank[b.Scope] > scopeRank[out.Scope] {
			out = b
		}
	}
	return out, true
}

// HasLoopback reports whether any of the bindings is a loopback bind.
func HasLoopback(bs []Binding) bool {
	for _, b := range bs {
		if b.Scope == ExposureLoopback {
			return true
		}
	}
	return false
}

// Listeners reads both kernel listener tables and returns every LISTEN socket
// grouped by port (decimal string).
func Listeners() map[string][]Binding {
	return ListenersFrom("/proc/net/tcp", "/proc/net/tcp6")
}

// ListenersFrom reads the given /proc-style tables and groups LISTEN sockets by
// port. The paths are a parameter so the parse is testable against fabricated
// tables without root.
func ListenersFrom(paths ...string) map[string][]Binding {
	out := map[string][]Binding{}
	for _, path := range paths {
		for port, bs := range parseListeners(path) {
			out[port] = append(out[port], bs...)
		}
	}
	return out
}

// tcpStateListen is the value /proc/net/tcp uses for a socket in LISTEN.
const tcpStateListen = "0A"

// parseListeners extracts every LISTEN socket from one /proc/net table, grouped
// by port. A file it can't read yields nothing, which surfaces upstream as a
// port simply not being found rather than a false all-clear.
func parseListeners(path string) map[string][]Binding {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	out := map[string][]Binding{}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] { // first line is the column header
		f := strings.Fields(line)
		if len(f) < 4 || f[3] != tcpStateListen {
			continue
		}
		hexAddr, hexPort, ok := strings.Cut(f[1], ":")
		if !ok {
			continue
		}
		n, err := strconv.ParseUint(hexPort, 16, 32)
		if err != nil {
			continue
		}
		port := strconv.FormatUint(n, 10)

		ip, err := decodeAddr(hexAddr)
		if err != nil {
			// Undecodable address: score it as the widest scope rather than drop
			// it. /proc's format is kernel-stable so this shouldn't happen, but
			// silently omitting a socket could only ever make a port look less
			// exposed than it is — the one direction that must not fail quietly.
			out[port] = append(out[port], Binding{Scope: ExposureOpen})
			continue
		}
		out[port] = append(out[port], Binding{IP: ip, Scope: ScopeOfIP(ip)})
	}
	return out
}

// decodeAddr converts a /proc-style hex local address into an IP. The addresses
// are little-endian per 32-bit word, so each word is reversed to get wire order.
func decodeAddr(hexAddr string) (net.IP, error) {
	b, err := hexBytes(hexAddr)
	if err != nil {
		return nil, err
	}
	switch len(b) {
	case 4:
		return net.IPv4(b[3], b[2], b[1], b[0]), nil
	case 16:
		ip := make(net.IP, 16)
		for i := 0; i < 16; i += 4 {
			ip[i], ip[i+1], ip[i+2], ip[i+3] = b[i+3], b[i+2], b[i+1], b[i]
		}
		return ip, nil
	}
	return nil, fmt.Errorf("unexpected address width %d in %q", len(b), hexAddr)
}

// tailnetCGNAT is the 100.64.0.0/10 range Tailscale assigns node addresses from.
var tailnetCGNAT = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

// ScopeOfIP classifies how far a bind address reaches. Anything that isn't
// loopback or a tailnet address is "open": a LAN address is reachable by
// everything on the subnet, which for an unauthenticated service is the same
// practical exposure as a wildcard bind.
func ScopeOfIP(ip net.IP) string {
	if ip.IsUnspecified() {
		return ExposureOpen
	}
	if ip.IsLoopback() {
		return ExposureLoopback
	}
	if v4 := ip.To4(); v4 != nil && tailnetCGNAT.Contains(v4) {
		return ExposureTailnet
	}
	return ExposureOpen
}

// hexBytes decodes a /proc-style hex address into raw bytes.
func hexBytes(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("odd-length hex address %q", s)
	}
	b := make([]byte, len(s)/2)
	for i := range b {
		v, err := strconv.ParseUint(s[i*2:i*2+2], 16, 8)
		if err != nil {
			return nil, err
		}
		b[i] = byte(v)
	}
	return b, nil
}
