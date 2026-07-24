package sessions

import (
	"net"
	"os"
	"os/user"
	"path/filepath"
	"testing"
	"time"

	"github.com/clarkbar-sys/hush/internal/netlisten"
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
	got := Detect(root, []string{"opencode", "claude"}, now, nil)
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
	if got := Detect(root, nil, time.Now(), nil); got != nil {
		t.Errorf("empty match should detect nothing, got %+v", got)
	}
	if got := Detect(root, []string{"  "}, time.Now(), nil); got != nil {
		t.Errorf("all-whitespace match should detect nothing, got %+v", got)
	}
}

func TestDetectInstalled(t *testing.T) {
	// A shared bin dir holding an executable opencode, plus a claude that isn't
	// executable (a stray non-binary of the same name mustn't count as installed)
	// and a directory named like a tool (also must not count).
	binA := t.TempDir()
	binB := t.TempDir()
	if err := os.WriteFile(filepath.Join(binA, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(binB, "claude"), []byte("not-a-binary"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(binB, "opencode.d"), 0o755); err != nil {
		t.Fatal(err)
	}

	got := DetectInstalled([]string{"opencode", "claude"}, []string{binA, binB})
	if len(got) != 2 {
		t.Fatalf("want 2 tools, got %d: %+v", len(got), got)
	}
	if !got[0].Present || got[0].Tool != "opencode" || got[0].Path != filepath.Join(binA, "opencode") {
		t.Errorf("opencode = %+v, want present at %s", got[0], filepath.Join(binA, "opencode"))
	}
	// claude exists but isn't executable — reported present:false, no path.
	if got[1].Present || got[1].Tool != "claude" || got[1].Path != "" {
		t.Errorf("claude = %+v, want not present", got[1])
	}
}

func TestDetectInstalledFirstHitWins(t *testing.T) {
	// PATH order matters: the earlier dir's binary is the one reported.
	first := t.TempDir()
	second := t.TempDir()
	for _, d := range []string{first, second} {
		if err := os.WriteFile(filepath.Join(d, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	got := DetectInstalled([]string{"opencode"}, []string{first, second})
	if len(got) != 1 || got[0].Path != filepath.Join(first, "opencode") {
		t.Errorf("got %+v, want path in first dir %s", got, first)
	}
}

func TestDetectInstalledEmptyDisables(t *testing.T) {
	if got := DetectInstalled(nil, []string{t.TempDir()}); got != nil {
		t.Errorf("empty tool set should report nothing, got %+v", got)
	}
	if got := DetectInstalled([]string{"  ", ""}, []string{t.TempDir()}); got != nil {
		t.Errorf("all-blank tool set should report nothing, got %+v", got)
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

func TestServerPort(t *testing.T) {
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"no flag falls back to default", []string{"opencode", "serve"}, DefaultServerPort},
		{"--port with space", []string{"opencode", "serve", "--port", "5000"}, "5000"},
		{"--port=", []string{"opencode", "serve", "--port=5001"}, "5001"},
		{"-p short", []string{"opencode", "serve", "-p", "5002"}, "5002"},
		{"-p= short", []string{"opencode", "serve", "-p=5003"}, "5003"},
		{"non-numeric falls back", []string{"opencode", "serve", "--port", "abc"}, DefaultServerPort},
		{"out-of-range falls back", []string{"opencode", "serve", "--port", "70000"}, DefaultServerPort},
		{"trailing flag with no value falls back", []string{"opencode", "serve", "--port"}, DefaultServerPort},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := serverPort(tc.args); got != tc.want {
				t.Errorf("serverPort(%v) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestResolveServer covers the whole judgement: is this a server, on what port,
// and how far does it reach — including the two directions that must never be
// gotten wrong (a loopback bind is not "reachable", and an unfound socket is
// "unknown", never silently "loopback").
func TestResolveServer(t *testing.T) {
	tailnet := map[string][]netlisten.Binding{
		"4096": {{IP: net.ParseIP("100.87.180.34"), Scope: netlisten.ExposureTailnet}},
	}
	loopback := map[string][]netlisten.Binding{
		"4096": {{IP: net.ParseIP("127.0.0.1"), Scope: netlisten.ExposureLoopback}},
	}

	t.Run("interactive opencode is not a server", func(t *testing.T) {
		s := Session{Tool: "opencode"}
		resolveServer(&s, []string{"opencode"}, tailnet)
		if s.Server {
			t.Fatalf("plain opencode marked as server: %+v", s)
		}
	})

	t.Run("claude with a stray serve token is not a server", func(t *testing.T) {
		s := Session{Tool: "claude"}
		resolveServer(&s, []string{"claude", "serve"}, tailnet)
		if s.Server {
			t.Fatalf("claude marked as server on a stray token: %+v", s)
		}
	})

	t.Run("tailnet-bound server is reachable at its address", func(t *testing.T) {
		s := Session{Tool: "opencode"}
		resolveServer(&s, []string{"opencode", "serve", "--hostname", "0.0.0.0", "--port", "4096"}, tailnet)
		if !s.Server || s.Addr != "100.87.180.34:4096" || s.Exposure != netlisten.ExposureTailnet {
			t.Fatalf("got %+v, want a tailnet server at 100.87.180.34:4096", s)
		}
	})

	t.Run("loopback-bound server is a server but not reachable off-box", func(t *testing.T) {
		s := Session{Tool: "opencode"}
		resolveServer(&s, []string{"opencode", "serve"}, loopback)
		if !s.Server || s.Exposure != netlisten.ExposureLoopback || s.Addr != "127.0.0.1:4096" {
			t.Fatalf("got %+v, want a loopback server the console will not hand out", s)
		}
	})

	t.Run("server whose socket wasn't found reports unknown, not loopback", func(t *testing.T) {
		s := Session{Tool: "opencode"}
		resolveServer(&s, []string{"opencode", "serve", "--port", "4096"}, nil)
		if !s.Server || s.Exposure != netlisten.ExposureUnknown {
			t.Fatalf("got %+v, want exposure unknown when no listener matched", s)
		}
		if s.Addr != "0.0.0.0:4096" {
			t.Errorf("addr = %q, want the wildcard at the resolved port", s.Addr)
		}
	})
}

// TestDetectResolvesServerFromProc is the end-to-end: a fabricated /proc plus a
// listener table must produce a Session flagged as a reachable server, proving
// Detect threads the listeners through to resolveServer.
func TestDetectResolvesServerFromProc(t *testing.T) {
	root := writeProc(t, "1000.00\n", map[int]struct {
		cmdline string
		stat    string
	}{
		201: {"opencode\x00serve\x00--port\x004096\x00", stat(201, "opencode", 50000)},
	})
	listeners := map[string][]netlisten.Binding{
		"4096": {{IP: net.ParseIP("100.87.180.34"), Scope: netlisten.ExposureTailnet}},
	}
	got := Detect(root, []string{"opencode"}, time.Unix(1_000_000, 0), listeners)
	if len(got) != 1 {
		t.Fatalf("want 1 session, got %d: %+v", len(got), got)
	}
	s := got[0]
	if !s.Server || s.Addr != "100.87.180.34:4096" || s.Exposure != netlisten.ExposureTailnet {
		t.Fatalf("got %+v, want a tailnet-reachable opencode server", s)
	}
}
