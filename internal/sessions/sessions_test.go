package sessions

import (
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"
)

// writeProc fabricates a /proc-like tree under a temp dir: one directory per
// fake pid with a cmdline and stat file, plus a system uptime file. It returns
// the root to hand to Detect, so the walk is exercised without root or a real
// process to point at.
func writeProc(t *testing.T, uptime string, procs map[int]struct {
	cmdline string // NUL-separated argv (use "\x00" between args)
	stat    string // full /proc/[pid]/stat line
}) string {
	t.Helper()
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "uptime"), []byte(uptime), 0o644); err != nil {
		t.Fatal(err)
	}
	for pid, p := range procs {
		dir := filepath.Join(root, itoa(pid))
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "cmdline"), []byte(p.cmdline), 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "stat"), []byte(p.stat), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// A non-numeric entry (like /proc/self) must be skipped, not parsed.
	if err := os.Mkdir(filepath.Join(root, "self"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func itoa(n int) string {
	return string(rune('0'+n/100%10)) + string(rune('0'+n/10%10)) + string(rune('0'+n%10))
}

// stat builds a /proc/[pid]/stat line whose starttime (field 22) is startTicks;
// every other field is padding, since Detect only reads comm and starttime.
func stat(pid int, comm string, startTicks uint64) string {
	// 19 padding fields after the ')' (indices 0..18), then starttime at index
	// 19 — which is real stat field 22.
	return itoa(pid) + " (" + comm + ") S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 0 20 0 1 " + utoa(startTicks) + " 0 0\n"
}

func utoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestDetect(t *testing.T) {
	root := writeProc(t, "1000.00 900.00\n", map[int]struct {
		cmdline string
		stat    string
	}{
		// a plain opencode launcher — matched on comm
		101: {"opencode\x00", stat(101, "opencode", 70000)}, // uptime 1000 - 700 = 300
		// claude launched via an absolute path with node as comm — matched on argv[0]
		102: {"/home/josh/.local/bin/claude\x00chat\x00", stat(102, "node", 90000)}, // uptime 100
		// an ordinary shell — no match, excluded
		103: {"bash\x00", stat(103, "bash", 50000)},
	})

	now := time.Unix(1_000_000, 0)
	got := Detect(root, []string{"opencode", "claude"}, now)
	if len(got) != 2 {
		t.Fatalf("want 2 sessions, got %d: %+v", len(got), got)
	}

	// Longest-running first: pid 101 (uptime 300) sorts above pid 102 (uptime 100).
	if got[0].PID != 101 || got[0].Tool != "opencode" {
		t.Errorf("first session = %+v, want pid 101 opencode", got[0])
	}
	if got[0].Uptime != 300 {
		t.Errorf("pid 101 uptime = %d, want 300", got[0].Uptime)
	}
	if got[0].Started != now.Unix()-300 {
		t.Errorf("pid 101 started = %d, want %d", got[0].Started, now.Unix()-300)
	}
	if got[1].PID != 102 || got[1].Tool != "claude" {
		t.Errorf("second session = %+v, want pid 102 claude", got[1])
	}
	if got[1].Cmd != "/home/josh/.local/bin/claude chat" {
		t.Errorf("pid 102 cmd = %q", got[1].Cmd)
	}

	// The processes are owned by whoever runs the test; assert Detect attributes
	// them to that user rather than leaving it blank.
	if u, err := user.Current(); err == nil {
		want := u.Username
		if want == "" {
			want = u.Uid
		}
		if got[0].User != want {
			t.Errorf("owner = %q, want %q", got[0].User, want)
		}
	}
}

func TestDetectEmptyMatchDisables(t *testing.T) {
	root := writeProc(t, "1000.00\n", map[int]struct {
		cmdline string
		stat    string
	}{
		101: {"opencode\x00", stat(101, "opencode", 70000)},
	})
	if got := Detect(root, nil, time.Now()); got != nil {
		t.Errorf("empty match should detect nothing, got %+v", got)
	}
	if got := Detect(root, []string{"  "}, time.Now()); got != nil {
		t.Errorf("all-whitespace match should detect nothing, got %+v", got)
	}
}

func TestSanitizeCmd(t *testing.T) {
	if got := sanitizeCmd([]string{"opencode", "run", "--flag"}); got != "opencode run --flag" {
		t.Errorf("got %q", got)
	}
	// control characters become spaces and runs collapse
	if got := sanitizeCmd([]string{"a\tb", "c\nd"}); got != "a b c d" {
		t.Errorf("got %q", got)
	}
	// long argv is truncated with an ellipsis
	long := make([]string, 100)
	for i := range long {
		long[i] = "xxxxxxxx"
	}
	got := sanitizeCmd(long)
	if len([]rune(got)) > cmdMax {
		t.Errorf("cmd not truncated: %d runes", len([]rune(got)))
	}
}
