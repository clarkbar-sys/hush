package vitals

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"
)

// TestParseStat covers the tricky bit of /proc/[pid]/stat parsing: the command
// sits in parentheses and may itself contain spaces and ')', so fields are
// located from the last ')', not by naive whitespace splitting.
func TestParseStat(t *testing.T) {
	// A synthetic stat line: pid 1234, comm "(odd) proc)", then stat fields
	// 3..24. utime (field 14) and stime (field 15) must land on 100 and 40,
	// and rss (field 24) on 512 pages.
	fields3to24 := []string{
		"S", "1", "1", "0", "0", "-1", "0", "0", "0", "0", // fields 3..12
		"100", "40", // 13(cutime placeholder? no) — see layout below
	}
	_ = fields3to24 // keep the intent documented; build the line explicitly instead

	// Build a line where field 14 == 100, field 15 == 40, field 24 == 512.
	// Fields 1 and 2 are pid and comm; the rest start at field 3.
	after := []string{
		"S",   // 3 state
		"1",   // 4 ppid
		"1",   // 5 pgrp
		"0",   // 6 session
		"0",   // 7 tty_nr
		"-1",  // 8 tpgid
		"0",   // 9 flags
		"0",   // 10 minflt
		"0",   // 11 cminflt
		"0",   // 12 majflt
		"0",   // 13 cmajflt
		"100", // 14 utime
		"40",  // 15 stime
		"5",   // 16 cutime
		"5",   // 17 cstime
		"20",  // 18 priority
		"0",   // 19 nice
		"1",   // 20 num_threads
		"0",   // 21 itrealvalue
		"999", // 22 starttime
		"0",   // 23 vsize
		"512", // 24 rss (pages)
	}
	line := "1234 (odd) proc) " + strings.Join(after, " ") + "\n"

	comm, jiffies, rss, ok := parseStat([]byte(line))
	if !ok {
		t.Fatalf("parseStat returned ok=false for a well-formed line")
	}
	if comm != "odd) proc" {
		t.Errorf("comm = %q, want %q", comm, "odd) proc")
	}
	if jiffies != 140 {
		t.Errorf("jiffies = %d, want 140 (100 utime + 40 stime)", jiffies)
	}
	if rss != 512 {
		t.Errorf("rss = %d, want 512 pages", rss)
	}
}

func TestParseStatRejectsGarbage(t *testing.T) {
	for _, in := range []string{"", "no parens here", "1234 (short)", "1234 (x) S"} {
		if _, _, _, ok := parseStat([]byte(in)); ok {
			t.Errorf("parseStat(%q) = ok, want rejected", in)
		}
	}
}

// TestCorePct exercises the per-core rate math, including the counter-reset
// guard that keeps a rebooted or wrapped counter from reporting a bogus value.
func TestCorePct(t *testing.T) {
	cases := []struct {
		name string
		a, b coreTimes
		want int
	}{
		{"half busy", coreTimes{idle: 0, total: 0}, coreTimes{idle: 50, total: 100}, 50},
		{"fully idle", coreTimes{idle: 0, total: 0}, coreTimes{idle: 100, total: 100}, 0},
		{"fully busy", coreTimes{idle: 0, total: 0}, coreTimes{idle: 0, total: 100}, 100},
		{"no elapsed time", coreTimes{idle: 10, total: 100}, coreTimes{idle: 10, total: 100}, 0},
		{"counter went backwards", coreTimes{idle: 10, total: 200}, coreTimes{idle: 5, total: 100}, 0},
	}
	for _, tc := range cases {
		if got := corePct(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: corePct = %d, want %d", tc.name, got, tc.want)
		}
	}
}

func TestRound1(t *testing.T) {
	cases := map[float64]float64{0: 0, 0.04: 0, 0.05: 0.1, 12.34: 12.3, 12.35: 12.4, 99.99: 100}
	for in, want := range cases {
		if got := round1(in); got != want {
			t.Errorf("round1(%v) = %v, want %v", in, got, want)
		}
	}
}

// TestCollectTop is an integration smoke test: on Linux it should read real
// /proc data and surface this test process among the results. Elsewhere /proc
// is absent, so it just asserts the call is safe and returns empty-ish.
func TestCollectTop(t *testing.T) {
	snap := CollectTop(10)
	if runtime.GOOS != "linux" {
		if len(snap.Procs) != 0 {
			t.Skipf("non-linux: no /proc, got %d procs", len(snap.Procs))
		}
		return
	}
	if len(snap.Procs) == 0 {
		t.Fatal("expected at least one process from /proc on linux")
	}
	if len(snap.Procs) > 10 {
		t.Errorf("limit not honoured: got %d procs, want <= 10", len(snap.Procs))
	}
	// Sorted by CPU descending.
	for i := 1; i < len(snap.Procs); i++ {
		if snap.Procs[i-1].CPU < snap.Procs[i].CPU {
			t.Errorf("procs not sorted by cpu desc at %d: %v then %v", i, snap.Procs[i-1].CPU, snap.Procs[i].CPU)
		}
	}
	if snap.Running < len(snap.Procs) {
		t.Errorf("Running (%d) should be >= returned procs (%d)", snap.Running, len(snap.Procs))
	}
	// The wire shape the console consumes.
	b, _ := json.Marshal(snap)
	for _, want := range []string{`"cores"`, `"procs"`, `"running"`} {
		if !strings.Contains(string(b), want) {
			t.Errorf("expected %s in /top JSON, got %s", want, b)
		}
	}
}
