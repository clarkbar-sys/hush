// Package sessions detects coding-agent processes running on a box — an
// opencode or a claude session someone spawned to work on that machine — and
// reports them so the console can show what's running. It is the read half of
// a construct whose write half never touches hush: a session is *spawned* and
// *stopped* by a sudo command the operator runs over SSH (JuiceSSH from a
// phone), and hush only ever watches. There is no execution path here.
//
// Detection reads /proc, exactly as the /top process table does: a process's
// argv and its owning uid are world-readable, so this needs no privilege and
// the agent stays the unprivileged "hush" user. A session is any process whose
// program name matches the configured set (opencode, claude by default) — hush
// doesn't track "sessions it spawned" specially, because it can't read another
// user's tmux socket or environment without being that user; what it can read,
// honestly, is "a coding agent is running here, owned by this user", which is
// the visualisation the console wants.
//
// The package also reports which of those tools are *installed* on the box (see
// DetectInstalled), so the console can offer to install or update them. That is
// presence only — a stat of the binary on the box's search list — and holds to
// the same rule: it looks a binary up, it never runs it, so there is still no
// execution path here. A tool is only visible when installed somewhere the
// unprivileged "hush" user can see, which is precisely a system-wide install.
package sessions

import (
	"os"
	"os/user"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// DefaultProcs are the program names counted as coding-agent sessions when the
// agent isn't told otherwise — the two the console knows how to spawn.
var DefaultProcs = []string{"opencode", "claude"}

// Session is one running coding-agent process as the console understands it.
type Session struct {
	PID     int    `json:"pid"`
	User    string `json:"user"`              // login that owns the process (falls back to the numeric uid)
	Tool    string `json:"tool"`              // matched program name: "opencode", "claude", …
	Cmd     string `json:"cmd,omitempty"`     // sanitised, truncated argv, for a glance at what it's doing
	Started int64  `json:"started,omitempty"` // unix seconds the process started, best-effort
	Uptime  int64  `json:"uptime,omitempty"`  // seconds it has been running, best-effort
}

// InstalledTool reports whether a known coding-agent CLI is present on the box
// and where its binary lives. It is presence only, by deliberate design: hush
// *looks the binary up* (a stat of a path on the box's search list) but never
// runs it, so this stays as read-only and privilege-free as the /proc scan and
// the agent keeps its "no execution path" contract. Because detection walks a
// shared search list (the box PATH plus the usual system bin dirs), a tool only
// shows up when it's installed somewhere the unprivileged hush user can see —
// which is precisely a system-wide install (e.g. /usr/local/bin), not a copy
// tucked inside another user's home.
type InstalledTool struct {
	Tool    string `json:"tool"`           // the coding agent: "opencode", "claude", …
	Present bool   `json:"present"`        // an executable of that name was found on the search list
	Path    string `json:"path,omitempty"` // where it was found, for a glance at which install answered
}

// Snapshot is a single reading of a box's coding-agent state, served by the
// agent's /sessions endpoint: the sessions running now, plus which of the known
// tools are installed system-wide so the console can offer to install or update
// them.
type Snapshot struct {
	Host      string          `json:"host"`
	Sessions  []Session       `json:"sessions"`
	Match     []string        `json:"match"`               // the program names looked for, so the console can explain what "none" means
	Installed []InstalledTool `json:"installed,omitempty"` // presence of each known tool on the box's search list; omitted (never a partial guess) when match is cleared
}

// userHZ is the kernel's clock-tick rate (USER_HZ), used to turn a process's
// starttime in /proc/[pid]/stat into wall-clock seconds. It is 100 on every
// mainstream Linux and there is no cheap way to read the real SC_CLK_TCK from
// pure Go; a wrong value only skews the reported uptime, never detection, so
// the near-universal constant is the pragmatic choice.
const userHZ = 100

// cmdMax caps the sanitised argv length so a pathological command line can't
// bloat the JSON — enough to see the tool and its working directory at a glance.
const cmdMax = 200

// Collect reads the live /proc on this host for processes whose program name is
// in match, and returns them with the hostname. An empty match disables
// detection (the endpoint reports no sessions), mirroring how clearing the LLM
// flags turns that detection off.
func Collect(match []string) Snapshot {
	host, _ := os.Hostname()
	return Snapshot{
		Host:      host,
		Sessions:  Detect("/proc", match, time.Now()),
		Match:     match,
		Installed: DetectInstalled(match, binDirs()),
	}
}

// systemBinDirs are the shared locations a system-wide install lands in (or that
// a tool is commonly found in), searched in addition to the box's own PATH so a
// tool installed to /usr/local/bin is seen even when the agent's PATH — as a
// systemd unit's — is narrower than a login shell's.
var systemBinDirs = []string{"/usr/local/bin", "/usr/bin", "/bin", "/usr/local/sbin", "/usr/sbin", "/opt/homebrew/bin"}

// binDirs is the ordered, de-duplicated search list DetectInstalled walks: the
// box PATH first (an operator's own choices win), then the system bin dirs.
func binDirs() []string {
	seen := make(map[string]struct{})
	var dirs []string
	add := func(d string) {
		if d == "" {
			return
		}
		if _, ok := seen[d]; ok {
			return
		}
		seen[d] = struct{}{}
		dirs = append(dirs, d)
	}
	for _, d := range filepath.SplitList(os.Getenv("PATH")) {
		add(d)
	}
	for _, d := range systemBinDirs {
		add(d)
	}
	return dirs
}

// DetectInstalled reports which of tools exist as an executable anywhere in
// dirs. It is the presence half of the Snapshot: a stat and an executable-bit
// check per candidate path, first hit wins — it never runs the binary, so it
// asks for no privilege and mirrors the /proc scan's read-only posture. An
// empty tool set returns nil, so clearing -session-procs disables installed
// reporting exactly as it disables session reporting (the console reads the
// absent field as "this agent doesn't report", never as "nothing installed").
func DetectInstalled(tools, dirs []string) []InstalledTool {
	seen := make(map[string]struct{}, len(tools))
	var out []InstalledTool
	for _, t := range tools {
		t = strings.ToLower(strings.TrimSpace(t))
		if t == "" {
			continue
		}
		if _, ok := seen[t]; ok {
			continue
		}
		seen[t] = struct{}{}
		it := InstalledTool{Tool: t}
		for _, d := range dirs {
			p := path.Join(d, t)
			if isExecutable(p) {
				it.Present = true
				it.Path = p
				break
			}
		}
		out = append(out, it)
	}
	return out
}

// isExecutable reports whether p is a regular file (symlinks are followed, so a
// launcher symlinked into a bin dir counts) with any execute bit set.
func isExecutable(p string) bool {
	fi, err := os.Stat(p)
	if err != nil || fi.IsDir() {
		return false
	}
	return fi.Mode().Perm()&0o111 != 0
}

// Detect scans procRoot for coding-agent processes, resolving each to a
// Session. procRoot and now are parameters so the walk is testable against a
// fabricated /proc without root; production passes "/proc" and time.Now().
//
// A process matches when either its comm (the kernel's short name) or the base
// name of its argv[0] is one of match — the launcher's own name, case-folded.
// Detection deliberately keys on the program name rather than a hush-planted
// marker: hush can't read another user's environment to find such a marker, and
// "what coding agent is running here" is the honest, privilege-free answer.
func Detect(procRoot string, match []string, now time.Time) []Session {
	set := make(map[string]struct{}, len(match))
	for _, m := range match {
		if m = strings.ToLower(strings.TrimSpace(m)); m != "" {
			set[m] = struct{}{}
		}
	}
	if len(set) == 0 {
		return nil
	}

	sysUptime := readUptime(procRoot)

	entries, err := os.ReadDir(procRoot)
	if err != nil {
		return nil
	}
	var out []Session
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue // not a process dir
		}
		args := readArgs(path.Join(procRoot, e.Name(), "cmdline"))
		comm, startTicks := readStat(path.Join(procRoot, e.Name(), "stat"))

		tool := matchTool(set, comm, args)
		if tool == "" {
			continue
		}

		s := Session{PID: pid, Tool: tool, Cmd: sanitizeCmd(args)}
		if fi, err := os.Stat(path.Join(procRoot, e.Name())); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				s.User = userName(st.Uid)
			}
		}
		if sysUptime > 0 && startTicks > 0 {
			if up := sysUptime - float64(startTicks)/userHZ; up >= 0 {
				s.Uptime = int64(up)
				s.Started = now.Unix() - int64(up)
			}
		}
		out = append(out, s)
	}

	// Longest-running first: the session you've had open all afternoon sorts
	// above one you just spawned. PID breaks ties for a stable order.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Uptime != out[j].Uptime {
			return out[i].Uptime > out[j].Uptime
		}
		return out[i].PID < out[j].PID
	})
	return out
}

// matchTool returns the matched program name for a process, or "" if it isn't a
// coding agent. It checks the kernel comm first (cheap, always present) and then
// the base name of argv[0], which catches a launcher invoked by an absolute
// path (…/bin/opencode) whose comm may differ.
func matchTool(set map[string]struct{}, comm string, args []string) string {
	if comm != "" {
		if _, ok := set[strings.ToLower(comm)]; ok {
			return strings.ToLower(comm)
		}
	}
	if len(args) > 0 {
		base := strings.ToLower(path.Base(args[0]))
		if _, ok := set[base]; ok {
			return base
		}
	}
	return ""
}

// readArgs reads /proc/[pid]/cmdline, whose arguments are NUL-separated with a
// trailing NUL. A kernel thread has an empty cmdline; callers get an empty slice.
func readArgs(p string) []string {
	data, err := os.ReadFile(p)
	if err != nil || len(data) == 0 {
		return nil
	}
	parts := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

// readStat pulls the comm and starttime (jiffies since boot) out of one
// /proc/[pid]/stat line. comm sits in parentheses and can itself contain spaces
// or ')', so the trailing fields are located from the *last* ')', the standard
// safe parse (see vitals.parseStat). starttime is stat field 22.
func readStat(p string) (comm string, startTicks uint64) {
	data, err := os.ReadFile(p)
	if err != nil {
		return "", 0
	}
	s := string(data)
	open := strings.IndexByte(s, '(')
	shut := strings.LastIndexByte(s, ')')
	if open < 0 || shut < 0 || shut < open {
		return "", 0
	}
	comm = s[open+1 : shut]
	// Fields after ')' start at stat field 3 (state), so field N is index N-3
	// here: starttime is field 22 -> index 19.
	fields := strings.Fields(s[shut+1:])
	if len(fields) > 19 {
		startTicks, _ = strconv.ParseUint(fields[19], 10, 64)
	}
	return comm, startTicks
}

// readUptime returns the system's uptime in seconds from /proc/uptime (its
// first field). Zero on any read/parse failure, which just leaves per-session
// uptime unreported rather than wrong.
func readUptime(procRoot string) float64 {
	data, err := os.ReadFile(path.Join(procRoot, "uptime"))
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(fields[0], 64)
	return v
}

// sanitizeCmd joins a process's argv into a single line for display: control
// characters (a NUL that slipped through, an embedded newline) become spaces,
// runs of whitespace collapse, and the result is truncated so one process can't
// ship a kilobyte of arguments every poll.
func sanitizeCmd(args []string) string {
	if len(args) == 0 {
		return ""
	}
	joined := strings.Join(args, " ")
	var b strings.Builder
	b.Grow(len(joined))
	prevSpace := false
	for _, r := range joined {
		if r < 0x20 || r == 0x7f {
			r = ' '
		}
		if r == ' ' {
			if prevSpace {
				continue
			}
			prevSpace = true
		} else {
			prevSpace = false
		}
		b.WriteRune(r)
	}
	out := strings.TrimSpace(b.String())
	if r := []rune(out); len(r) > cmdMax {
		out = string(r[:cmdMax-1]) + "…"
	}
	return out
}

// userName resolves a uid to a login name, falling back to the numeric uid so a
// session is always attributable even when the passwd lookup fails.
func userName(uid uint32) string {
	name := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(name); err == nil && u.Username != "" {
		return u.Username
	}
	return name
}
