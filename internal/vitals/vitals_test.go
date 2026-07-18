package vitals

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestBackupCapabilityWire guards the JSON shape the console's setup generator
// reads: enabled always present, restic/vault omitted when empty/false, and a
// nil capability omitted entirely (an older agent makes no claim).
func TestBackupCapabilityWire(t *testing.T) {
	full, _ := json.Marshal(Snapshot{Backup: &BackupCapability{Enabled: true, Restic: "restic 0.16.0", Vault: true}})
	for _, want := range []string{`"backup"`, `"enabled":true`, `"restic":"restic 0.16.0"`, `"vault":true`} {
		if !strings.Contains(string(full), want) {
			t.Fatalf("expected %s in %s", want, full)
		}
	}
	// -backup off, restic missing, no vault: enabled:false present, others omitted.
	off, _ := json.Marshal(Snapshot{Backup: &BackupCapability{}})
	if !strings.Contains(string(off), `"enabled":false`) {
		t.Fatalf("expected enabled:false, got %s", off)
	}
	if strings.Contains(string(off), `"restic"`) || strings.Contains(string(off), `"vault"`) {
		t.Fatalf("empty restic/vault should be omitted, got %s", off)
	}
	// nil capability: the whole field is omitted.
	none, _ := json.Marshal(Snapshot{})
	if strings.Contains(string(none), `"backup"`) {
		t.Fatalf("nil capability should omit the field, got %s", none)
	}
}

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
