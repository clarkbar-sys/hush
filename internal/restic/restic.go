// Package restic wraps the restic backup tool — the engine behind the Backup
// construct ("a Job that hauls a Machine into a Store, dedup'd"). restic gives
// hush the three things a NAS-local copy can't: content-defined dedup, snapshot
// history, and client-side encryption. This package is a thin, streaming shell
// over the restic binary; it does not reimplement any of that.
//
// The repository and its encryption password are handed to restic through the
// environment (RESTIC_REPOSITORY / RESTIC_PASSWORD), never as command-line
// arguments, so the password never lands in the process table or an audit log.
// The command is run with an explicit argument slice (no `sh -c`), so a path
// that looks like a flag or contains shell metacharacters is passed through
// literally rather than interpreted — the reason a backup wants stricter
// argument handling than the Task runner's deliberately-unjailed `sh -c`.
package restic

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	pathpkg "path"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Binary is the restic executable, resolved from $PATH by default. It's a
// package var rather than a constant so tests can point it at a stub script and
// exercise the streaming and parsing without restic installed.
var Binary = "restic"

const (
	// DefaultBackupTimeout bounds a single backup run. It is deliberately long —
	// an initial full backup of a multi-terabyte machine is measured in hours —
	// but finite, so a wedged run can't stream forever.
	DefaultBackupTimeout = 24 * time.Hour
	// maxOutput caps captured output per run. restic's progress is compact, but a
	// days-long run could still accumulate; past the cap the run keeps going and
	// Truncated is reported on the exit event, mirroring package exec.
	maxOutput = 4 << 20 // 4 MiB
)

// Repo identifies a restic repository and the password that decrypts it.
type Repo struct {
	Backend  string // RESTIC_REPOSITORY, e.g. "rest:http://nas:8000/homelab" or "/mnt/pool/restic"
	Password string // RESTIC_PASSWORD — the repo's encryption key
}

// env returns the process environment plus the repo's location and password, so
// restic reads both from the environment rather than argv.
func (r Repo) env() []string {
	return append(os.Environ(),
		"RESTIC_REPOSITORY="+r.Backend,
		"RESTIC_PASSWORD="+r.Password,
	)
}

// Event is one frame in a run's lifecycle, delivered to the caller as it
// happens. It is the same JSON shape package exec emits, so the agent's SSE
// layer relays a backup run and a Task run through identical plumbing.
type Event struct {
	Kind      string `json:"kind"`                // "start" | "out" | "exit" | "error"
	Stream    string `json:"stream,omitempty"`    // "stdout" | "stderr" (kind=out)
	Data      string `json:"data,omitempty"`      // output chunk (out) or message (error)
	PID       int    `json:"pid,omitempty"`       // kind=start
	Code      int    `json:"code,omitempty"`      // kind=exit: restic's exit code (-1 if signalled)
	Signal    string `json:"signal,omitempty"`    // kind=exit: "timeout", "canceled", or a signal name
	MS        int64  `json:"ms,omitempty"`        // kind=exit: wall-clock duration in ms
	Truncated bool   `json:"truncated,omitempty"` // kind=exit: output cap was hit
}

// Spec describes what a backup run captures.
type Spec struct {
	Paths         []string // absolute paths to back up
	Excludes      []string // restic --exclude patterns
	OneFileSystem bool     // --one-file-system: don't cross mount points (whole-machine mode)
	Tags          []string // restic --tag values, so snapshots can be filtered back to this backup
}

// buildBackupArgs assembles the `restic backup …` argument slice. Tags and
// excludes come first as flags; a literal `--` then stops flag parsing so a path
// beginning with `-` is treated as a path, not an option.
func buildBackupArgs(spec Spec) []string {
	args := []string{"backup"}
	for _, t := range spec.Tags {
		args = append(args, "--tag", t)
	}
	if spec.OneFileSystem {
		args = append(args, "--one-file-system")
	}
	for _, e := range spec.Excludes {
		if strings.TrimSpace(e) == "" {
			continue
		}
		args = append(args, "--exclude", e)
	}
	args = append(args, "--")
	args = append(args, spec.Paths...)
	return args
}

// Available reports restic's version string and whether the binary is present
// and runnable, so the agent can answer a create with a clear "restic is not
// installed" rather than a cryptic exec error at backup time.
func Available(ctx context.Context) (string, bool) {
	out, err := exec.CommandContext(ctx, Binary, "version").Output()
	if err != nil {
		return "", false
	}
	return strings.TrimSpace(string(out)), true
}

// Init initialises a repository, tolerating one that already exists — creating
// a repo and pointing a second machine at the same one are both routine, and
// only the first machine actually initialises it. Any other failure (bad
// backend URL, unreachable rest-server) is returned so create can fail fast.
func Init(ctx context.Context, repo Repo) error {
	cmd := exec.CommandContext(ctx, Binary, "init")
	cmd.Env = repo.env()
	out, err := cmd.CombinedOutput()
	if err == nil {
		return nil
	}
	s := string(out)
	if strings.Contains(s, "already initialized") ||
		strings.Contains(s, "already exists") ||
		strings.Contains(s, "already a repository") {
		return nil
	}
	return fmt.Errorf("restic init: %v: %s", err, strings.TrimSpace(s))
}

// Snapshot is one entry from `restic snapshots --json`, trimmed to what the
// console shows.
type Snapshot struct {
	ID       string   `json:"id"`
	ShortID  string   `json:"short_id"`
	Time     string   `json:"time"`
	Hostname string   `json:"hostname"`
	Paths    []string `json:"paths"`
	Tags     []string `json:"tags"`
}

// Snapshots lists a repository's snapshots, optionally filtered to the given
// tags so a caller sees just one backup's history. A repo that exists but
// rejects the password surfaces here as an error, which is what makes this a
// usable password check at create time.
func Snapshots(ctx context.Context, repo Repo, tags ...string) ([]Snapshot, error) {
	args := []string{"snapshots", "--json"}
	for _, t := range tags {
		args = append(args, "--tag", t)
	}
	cmd := exec.CommandContext(ctx, Binary, args...)
	cmd.Env = repo.env()
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("restic snapshots: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("restic snapshots: %w", err)
	}
	var snaps []Snapshot
	if err := json.Unmarshal(out, &snaps); err != nil {
		return nil, fmt.Errorf("parse restic snapshots: %w", err)
	}
	return snaps, nil
}

// Node is one entry from `restic ls --json` — a file, directory, or symlink
// inside a snapshot, trimmed to what the console's snapshot browser shows. It is
// the restore-confidence read path: walk a snapshot's tree to confirm the data
// is really in there before trusting it, without writing anything.
type Node struct {
	Name   string `json:"name"`
	Type   string `json:"type"` // "dir" | "file" | "symlink"
	Path   string `json:"path"`
	Size   int64  `json:"size,omitempty"`
	MTime  string `json:"mtime,omitempty"`
	Target string `json:"linktarget,omitempty"` // symlink destination
}

const (
	// DefaultListLimit caps how many immediate children List returns for one
	// directory, so browsing a snapshot of a directory holding a million files
	// stays a bounded request — the same "partial, marked truncated" contract the
	// /du treemap walk uses, rather than streaming forever.
	DefaultListLimit = 2000
	// listScanCap bounds how many ls lines List reads before giving up, so a
	// pathological subtree can't make it walk the whole snapshot to find one
	// level's children.
	listScanCap = 500_000
)

// List returns the immediate children of dir within a snapshot by streaming
// `restic ls <snapshot> [dir] --json` and keeping only the nodes exactly one
// level deep. restic's ls is recursive; List filters to a single level so the
// console can walk a snapshot the same lazy, one-directory-at-a-time way it
// walks a live filesystem. It stops once limit children are collected (reporting
// truncated=true) so a huge directory returns a partial answer instead of
// hanging. dir is an absolute path inside the snapshot; "" or "/" lists the
// snapshot's roots. It only ever reads — a snapshot is immutable — so browsing
// can never harm the backup.
func List(ctx context.Context, repo Repo, snapshot, dir string, limit int) ([]Node, bool, error) {
	if limit <= 0 {
		limit = DefaultListLimit
	}
	d := "/"
	if s := strings.TrimSpace(dir); s != "" {
		d = pathpkg.Clean(s)
	}
	args := []string{"ls", snapshot}
	if d != "/" {
		args = append(args, d)
	}
	args = append(args, "--json")

	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	c := exec.CommandContext(runCtx, Binary, args...)
	c.Env = repo.env()
	// Own process group so cancelling (we stop early once we have a level's worth
	// of children) kills the whole restic tree, the same containment stream uses.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process != nil {
			return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	c.WaitDelay = 2 * time.Second

	stdout, err := c.StdoutPipe()
	if err != nil {
		return nil, false, err
	}
	var stderr strings.Builder
	c.Stderr = &stderr
	if err := c.Start(); err != nil {
		return nil, false, err
	}

	var out []Node
	truncated := false
	sc := bufio.NewScanner(stdout)
	// Deep paths make long lines; give the scanner room past the 64 KiB default.
	sc.Buffer(make([]byte, 0, 64*1024), 4<<20)
	scanned := 0
	for sc.Scan() {
		if scanned++; scanned > listScanCap {
			truncated = true
			break
		}
		var n Node
		if err := json.Unmarshal(sc.Bytes(), &n); err != nil {
			continue // the leading snapshot-header line, or a shape we don't model
		}
		if n.Name == "" || n.Path == "" {
			continue // header line carries paths[] but no single name/path
		}
		if pathpkg.Dir(n.Path) != d {
			continue // not an immediate child of the directory we're listing
		}
		out = append(out, n)
		if len(out) >= limit {
			truncated = true
			break
		}
	}
	// If we stopped early, cancel kills restic and its non-zero exit is expected;
	// only a failure on a full read (bad snapshot id, wrong password) is a real
	// error. When we read to the end, we must NOT cancel before Wait, or a clean
	// run would come back looking canceled — the defer'd cancel cleans up instead.
	stopped := truncated
	if stopped {
		cancel()
	}
	werr := c.Wait()
	if !stopped && werr != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = werr.Error()
		}
		return nil, false, fmt.Errorf("restic ls: %s", msg)
	}
	sortNodes(out)
	return out, truncated, nil
}

// sortNodes orders a directory listing the way a file browser reads: directories
// first, then files and symlinks, each group alphabetical by name.
func sortNodes(ns []Node) {
	sort.SliceStable(ns, func(i, j int) bool {
		di, dj := ns[i].Type == "dir", ns[j].Type == "dir"
		if di != dj {
			return di
		}
		return ns[i].Name < ns[j].Name
	})
}

// Backup runs `restic backup` for spec against repo, streaming its lifecycle to
// emit in order from the calling goroutine (emit is never called concurrently),
// the same contract package exec.Run offers. It returns once restic exits, the
// timeout trips, or ctx is cancelled. A zero timeout applies DefaultBackupTimeout.
func Backup(ctx context.Context, repo Repo, spec Spec, timeout time.Duration, emit func(Event)) {
	stream(ctx, repo, timeout, emit, buildBackupArgs(spec)...)
}

// buildRestoreArgs assembles `restic restore <snapshot> --target <dir> [--include
// <path>…]`. snapshot is a snapshot id (or "latest"); target is where the files
// land; includes narrow the restore to specific paths within the snapshot.
func buildRestoreArgs(snapshot, target string, includes []string) []string {
	args := []string{"restore", snapshot, "--target", target}
	for _, inc := range includes {
		if strings.TrimSpace(inc) == "" {
			continue
		}
		args = append(args, "--include", inc)
	}
	return args
}

// Restore runs `restic restore`, streaming its lifecycle to emit the same way
// Backup does. It reads from the repository and writes into target; it never
// touches the snapshots, so a restore can't harm the backup history.
func Restore(ctx context.Context, repo Repo, snapshot, target string, includes []string, timeout time.Duration, emit func(Event)) {
	stream(ctx, repo, timeout, emit, buildRestoreArgs(snapshot, target, includes)...)
}

// stream runs `restic <args>` with repo's environment, delivering its lifecycle
// as Events to emit in order from the calling goroutine (emit is never called
// concurrently) — the shared engine behind Backup and Restore. It returns once
// restic exits, the timeout trips, or ctx is cancelled; a zero timeout applies
// DefaultBackupTimeout. Output is capped at maxOutput, past which Truncated is
// set on the exit event.
func stream(ctx context.Context, repo Repo, timeout time.Duration, emit func(Event), args ...string) {
	if timeout <= 0 {
		timeout = DefaultBackupTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	c := exec.CommandContext(runCtx, Binary, args...)
	c.Env = repo.env()
	// Own process group so a timeout or a client hang-up kills the whole restic
	// tree, not just the parent — the same containment package exec uses.
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error {
		if c.Process != nil {
			return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
		}
		return nil
	}
	c.WaitDelay = 2 * time.Second

	stdout, err := c.StdoutPipe()
	if err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}
	stderr, err := c.StderrPipe()
	if err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}

	start := time.Now()
	if err := c.Start(); err != nil {
		emit(Event{Kind: "error", Data: err.Error()})
		return
	}
	emit(Event{Kind: "start", PID: c.Process.Pid})

	type chunk struct{ stream, data string }
	ch := make(chan chunk, 64)
	var total atomic.Int64
	var truncated atomic.Bool
	var wg sync.WaitGroup
	pump := func(rd io.Reader, label string) {
		defer wg.Done()
		buf := make([]byte, 4096)
		br := bufio.NewReader(rd)
		for {
			n, rerr := br.Read(buf)
			if n > 0 {
				if total.Add(int64(n)) > maxOutput {
					truncated.Store(true)
				} else {
					ch <- chunk{label, string(buf[:n])}
				}
			}
			if rerr != nil {
				return
			}
		}
	}
	wg.Add(2)
	go pump(stdout, "stdout")
	go pump(stderr, "stderr")
	go func() { wg.Wait(); close(ch) }()

	for c := range ch {
		emit(Event{Kind: "out", Stream: c.stream, Data: c.data})
	}

	werr := c.Wait()
	ev := Event{Kind: "exit", MS: time.Since(start).Milliseconds(), Truncated: truncated.Load()}
	switch {
	case werr == nil:
		ev.Code = 0
	default:
		ev.Code = -1
		if ee, ok := werr.(*exec.ExitError); ok {
			ev.Code = ee.ExitCode()
			if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
				ev.Signal = ws.Signal().String()
			}
		} else {
			ev.Signal = werr.Error()
		}
		switch {
		case runCtx.Err() == context.DeadlineExceeded:
			ev.Signal = "timeout"
		case ctx.Err() != nil:
			ev.Signal = "canceled"
		}
	}
	emit(ev)
}
