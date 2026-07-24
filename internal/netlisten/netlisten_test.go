package netlisten

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestScopeOfIP pins the classification that decides whether the console says
// "local only" or "serving". Getting this wrong inverts a safety claim.
func TestScopeOfIP(t *testing.T) {
	cases := []struct {
		ip   string
		want string
	}{
		{"127.0.0.1", ExposureLoopback},
		{"::1", ExposureLoopback},
		{"0.0.0.0", ExposureOpen},
		{"::", ExposureOpen},
		{"100.87.180.34", ExposureTailnet}, // tailnet CGNAT
		{"100.63.255.255", ExposureOpen},   // just below 100.64/10
		{"100.128.0.0", ExposureOpen},      // just above 100.64/10
		{"192.168.0.30", ExposureOpen},     // LAN is not "safe"
	}
	for _, tc := range cases {
		t.Run(tc.ip, func(t *testing.T) {
			if got := ScopeOfIP(net.ParseIP(tc.ip)); got != tc.want {
				t.Fatalf("ScopeOfIP(%s) = %q, want %q", tc.ip, got, tc.want)
			}
		})
	}
}

// TestDecodeAddr pins the little-endian /proc decoding. The encodings are
// asserted literally rather than round-tripped, so a byte-order regression
// can't pass by being wrong consistently in both directions.
func TestDecodeAddr(t *testing.T) {
	cases := []struct {
		hex  string
		want string
	}{
		{"0100007F", "127.0.0.1"},
		{"00000000", "0.0.0.0"},
		{"22B45764", "100.87.180.34"},
		{"1E00A8C0", "192.168.0.30"},
		{"00000000000000000000000001000000", "::1"},
		{strings.Repeat("0", 32), "::"},
	}
	for _, tc := range cases {
		t.Run(tc.hex, func(t *testing.T) {
			ip, err := decodeAddr(tc.hex)
			if err != nil {
				t.Fatalf("decodeAddr(%q): %v", tc.hex, err)
			}
			if got := ip.String(); got != tc.want {
				t.Fatalf("decodeAddr(%q) = %s, want %s", tc.hex, got, tc.want)
			}
		})
	}

	for _, bad := range []string{"zz", "0100007", "00"} {
		if _, err := decodeAddr(bad); err == nil {
			t.Errorf("decodeAddr(%q) succeeded, want error", bad)
		}
	}
}

// writeTable writes a synthetic /proc/net/tcp table with the given
// "hexaddr:hexport" LISTEN entries.
func writeTable(t *testing.T, entries ...string) string {
	t.Helper()
	var b strings.Builder
	b.WriteString("  sl  local_address rem_address   st\n")
	for i, e := range entries {
		b.WriteString("   " + string(rune('0'+i)) + ": " + e + " 00000000:0000 0A\n")
	}
	path := filepath.Join(t.TempDir(), "tcp")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// TestParseListenersWidestWins checks that a port bound both to loopback and to
// a tailnet address reports the tailnet scope — the reachable truth, not the
// reassuring half of it.
func TestParseListenersWidestWins(t *testing.T) {
	path := writeTable(t,
		"0100007F:1F9B", // 127.0.0.1:8091
		"22B45764:1F9B", // 100.87.180.34:8091
		"0100007F:2CEE", // 127.0.0.1:11502
	)
	got := parseListeners(path)

	b, ok := Widest(got["8091"])
	if !ok {
		t.Fatal("port 8091 not found")
	}
	if b.Scope != ExposureTailnet {
		t.Errorf("port 8091 scope = %q, want %q", b.Scope, ExposureTailnet)
	}
	if !HasLoopback(got["8091"]) {
		t.Error("port 8091 should still report a loopback binding alongside the tailnet one")
	}

	b2, _ := Widest(got["11502"])
	if b2.Scope != ExposureLoopback {
		t.Errorf("port 11502 scope = %q, want %q", b2.Scope, ExposureLoopback)
	}
}

// TestParseListenersUndecodableIsOpen guards the fail-loud direction: a socket
// whose address can't be decoded must widen the port's reported scope, never
// silently vanish and leave the port looking safer than it is.
func TestParseListenersUndecodableIsOpen(t *testing.T) {
	path := writeTable(t,
		"0100007F:1F9B", // 127.0.0.1:8091
		"zzzzzzzz:1F9B", // undecodable, same port
	)
	b, ok := Widest(parseListeners(path)["8091"])
	if !ok {
		t.Fatal("port 8091 not found")
	}
	if b.Scope != ExposureOpen {
		t.Fatalf("scope = %q, want %q — an undecodable bind must not read as safe", b.Scope, ExposureOpen)
	}
	if got := b.Addr("8091"); got != "0.0.0.0:8091" {
		t.Errorf("addr = %q, want the wildcard it was scored as", got)
	}
}

func TestParseListenersUnreadable(t *testing.T) {
	if got := parseListeners("/nonexistent/proc/net/tcp"); len(got) != 0 {
		t.Fatalf("unreadable table yielded %v, want nothing", got)
	}
}

// TestWidestAndWildcard pins the helpers callers lean on when turning a port's
// bindings into a single reported address.
func TestWidestAndWildcard(t *testing.T) {
	if _, ok := Widest(nil); ok {
		t.Error("Widest(nil) should report no binding")
	}
	wild := Binding{IP: net.ParseIP("0.0.0.0"), Scope: ExposureOpen}
	if !wild.Wildcard() {
		t.Error("0.0.0.0 should be a wildcard bind")
	}
	pinned := Binding{IP: net.ParseIP("100.87.180.34"), Scope: ExposureTailnet}
	if pinned.Wildcard() {
		t.Error("a pinned address is not a wildcard bind")
	}
	// A binding whose address didn't decode renders as the wildcard it was
	// conservatively scored as.
	undecoded := Binding{Scope: ExposureOpen}
	if got := undecoded.Addr("4096"); got != "0.0.0.0:4096" {
		t.Errorf("undecoded addr = %q, want 0.0.0.0:4096", got)
	}
}
