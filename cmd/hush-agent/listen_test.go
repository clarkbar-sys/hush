package main

import (
	"errors"
	"net"
	"testing"
)

func TestTailnetPort(t *testing.T) {
	tests := []struct {
		listen   string
		wantPort string
		wantOK   bool
	}{
		{"tailnet", defaultAgentPort, true},
		{"tailnet:9000", "9000", true},
		{"tailnet:", "", true}, // explicit empty port; JoinHostPort would reject later
		{":8765", "", false},
		{"127.0.0.1:8765", "", false},
		{"100.64.0.1:8765", "", false},
		{"tailnetish", "", false}, // sentinel must be exact, not a prefix
		{"", "", false},
	}
	for _, tt := range tests {
		port, ok := tailnetPort(tt.listen)
		if ok != tt.wantOK || port != tt.wantPort {
			t.Errorf("tailnetPort(%q) = (%q, %v), want (%q, %v)", tt.listen, port, ok, tt.wantPort, tt.wantOK)
		}
	}
}

func ipNet(cidr string) *net.IPNet {
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		panic(err)
	}
	n.IP = ip
	return n
}

func TestTailnetIPv4(t *testing.T) {
	tests := []struct {
		name  string
		addrs []net.Addr
		want  string // empty means expect ok=false
	}{
		{
			name:  "picks the tailscale CGNAT address",
			addrs: []net.Addr{ipNet("192.168.1.20/24"), ipNet("100.122.199.17/32"), ipNet("10.0.0.5/8")},
			want:  "100.122.199.17",
		},
		{
			name:  "ignores non-tailnet 100.x below the CGNAT range",
			addrs: []net.Addr{ipNet("100.63.255.255/32")}, // 100.63.x is outside 100.64.0.0/10
			want:  "",
		},
		{
			name:  "no tailnet address present",
			addrs: []net.Addr{ipNet("192.168.1.20/24"), &net.IPAddr{IP: net.ParseIP("::1")}},
			want:  "",
		},
		{
			name:  "handles IPAddr as well as IPNet",
			addrs: []net.Addr{&net.IPAddr{IP: net.ParseIP("100.64.0.1")}},
			want:  "100.64.0.1",
		},
		{
			name:  "empty",
			addrs: nil,
			want:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ip, ok := tailnetIPv4(tt.addrs)
			if tt.want == "" {
				if ok {
					t.Fatalf("tailnetIPv4() = (%v, true), want ok=false", ip)
				}
				return
			}
			if !ok || ip.String() != tt.want {
				t.Fatalf("tailnetIPv4() = (%v, %v), want (%s, true)", ip, ok, tt.want)
			}
		})
	}
}

func TestWaitForTailnetIPv4(t *testing.T) {
	tailnet := []net.Addr{ipNet("100.122.199.17/32")}
	lan := []net.Addr{ipNet("192.168.1.20/24")}

	t.Run("found on first attempt", func(t *testing.T) {
		ip, err := waitForTailnetIPv4(func() ([]net.Addr, error) { return tailnet, nil }, 0, 3)
		if err != nil || ip.String() != "100.122.199.17" {
			t.Fatalf("got (%v, %v), want (100.122.199.17, nil)", ip, err)
		}
	})

	t.Run("appears after a couple of retries", func(t *testing.T) {
		calls := 0
		got, err := waitForTailnetIPv4(func() ([]net.Addr, error) {
			calls++
			if calls < 3 {
				return lan, nil // tailscale not up yet
			}
			return tailnet, nil
		}, 0, 5)
		if err != nil || got.String() != "100.122.199.17" {
			t.Fatalf("got (%v, %v), want (100.122.199.17, nil)", got, err)
		}
		if calls != 3 {
			t.Fatalf("polled %d times, want 3", calls)
		}
	})

	t.Run("gives up after attempts", func(t *testing.T) {
		calls := 0
		_, err := waitForTailnetIPv4(func() ([]net.Addr, error) {
			calls++
			return lan, nil
		}, 0, 4)
		if err == nil {
			t.Fatal("expected error when tailnet address never appears")
		}
		if calls != 4 {
			t.Fatalf("polled %d times, want 4", calls)
		}
	})

	t.Run("surfaces the interface error when it never resolves", func(t *testing.T) {
		sentinel := errors.New("netlink boom")
		_, err := waitForTailnetIPv4(func() ([]net.Addr, error) { return nil, sentinel }, 0, 2)
		if !errors.Is(err, sentinel) {
			t.Fatalf("error = %v, want it to wrap %v", err, sentinel)
		}
	})
}

func TestResolveListenPassthrough(t *testing.T) {
	// A literal address must pass through untouched (no tailnet resolution).
	for _, addr := range []string{":8765", "127.0.0.1:8765", "100.122.199.17:8765"} {
		got, err := resolveListen(addr)
		if err != nil || got != addr {
			t.Errorf("resolveListen(%q) = (%q, %v), want (%q, nil)", addr, got, err, addr)
		}
	}
}
