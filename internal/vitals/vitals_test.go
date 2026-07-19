package vitals

import (
	"testing"
)

func TestClamp(t *testing.T) {
	cases := map[int]int{-5: 0, 0: 0, 42: 42, 100: 100, 137: 100}
	for in, want := range cases {
		if got := clamp(in); got != want {
			t.Errorf("clamp(%d) = %d, want %d", in, got, want)
		}
	}
}

func TestRound(t *testing.T) {
	cases := map[float64]int{0.4: 0, 0.5: 1, 12.49: 12, 12.5: 13, 99.9: 100}
	for in, want := range cases {
		if got := round(in); got != want {
			t.Errorf("round(%v) = %d, want %d", in, got, want)
		}
	}
}

func TestCounterRate(t *testing.T) {
	cases := []struct {
		name      string
		prev, cur uint64
		want      int
	}{
		{"steady growth", 1000, 1500, 500},
		{"no traffic", 1000, 1000, 0},
		{"counter reset (interface flap)", 5000, 200, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := counterRate(tc.prev, tc.cur); got != tc.want {
				t.Errorf("counterRate(%d, %d) = %d, want %d", tc.prev, tc.cur, got, tc.want)
			}
		})
	}
}

func TestDeriveStatus(t *testing.T) {
	vram := func(v int) *int { return &v }
	tests := []struct {
		name           string
		cpu, mem, disk int
		vram           *int
		svcs           []Service
		want           string
	}{
		{"idle", 5, 10, 20, nil, nil, "good"},
		{"failed service is critical", 5, 10, 20, nil, []Service{{State: "failed"}}, "crit"},
		{"full disk is critical", 5, 10, 95, nil, nil, "crit"},
		{"busy cpu is a warning", 90, 10, 20, nil, nil, "warn"},
		{"vram pressure is a warning", 5, 10, 20, vram(95), nil, "warn"},
		{"running service stays good", 5, 10, 20, nil, []Service{{State: "running"}}, "good"},
	}
	for _, tc := range tests {
		if got := deriveStatus(tc.cpu, tc.mem, tc.disk, tc.vram, tc.svcs); got != tc.want {
			t.Errorf("%s: deriveStatus = %q, want %q", tc.name, got, tc.want)
		}
	}
}
