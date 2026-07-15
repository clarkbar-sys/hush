package main

import (
	"context"
	"os/exec"
	"sort"
	"sync"
	"time"

	hexec "github.com/clarkbar-sys/hush/internal/exec"
)

// runAsProbeTimeout bounds a single sudo probe so an odd PAM or sudoers setup
// can't wedge a /vitals response. A local `sudo -l` is near-instant; this is a
// generous ceiling, not a target.
const runAsProbeTimeout = 3 * time.Second

// runAsCacheTTL is how long a verification result is reused before the next
// /vitals reprobes. The canonical way run-as grants change (the console's
// Run-as users sheet) restarts the agent, so a fresh process always reprobes;
// this TTL only bounds how stale an out-of-band sudoers edit can look.
const runAsCacheTTL = 30 * time.Second

// sudoCanRunAs reports whether the hush user may run a command as user right
// now, without a password — the same `sudo -n -u <user>` a Task would use. It
// asks sudo in list mode (`-l`), so nothing is executed as the target user; a
// zero exit means the sudoers grant is in place and the user resolves. A
// missing grant, a passworded rule, or an unknown user all yield false, which
// is exactly when a real Task would fail. The name is passed as sudo's own
// argument and is gated by ValidUserName first, so it never reaches a shell.
func sudoCanRunAs(user string) bool {
	if !hexec.ValidUserName(user) {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), runAsProbeTimeout)
	defer cancel()
	// `sudo -n -l -u <user> sh` — may hush run `sh` (what /exec runs) as <user>?
	return exec.CommandContext(ctx, "sudo", "-n", "-l", "-u", user, "sh").Run() == nil
}

// verifyRunAs returns the subset of users that probe accepts, preserving the
// input order. It's split from the probing so tests can drive it without sudo.
// The result is always non-nil (even when empty) so a caller can tell "verified,
// none granted" apart from "never verified".
func verifyRunAs(users []string, probe func(string) bool) []string {
	granted := make([]string, 0, len(users))
	for _, u := range users {
		if probe(u) {
			granted = append(granted, u)
		}
	}
	return granted
}

// runAsChecker verifies an agent's advertised run-as users against the box's
// actual sudoers grant and caches the answer for ttl, so /vitals can report
// which users are truly runnable without shelling out to sudo on every poll.
// It is safe for concurrent use.
type runAsChecker struct {
	users []string
	ttl   time.Duration
	now   func() time.Time
	probe func(string) bool

	mu      sync.Mutex
	cache   []string
	expires time.Time
	valid   bool
}

// newRunAsChecker builds a checker for the given advertised users, probing the
// real sudoers grant via sudo. users is copied and sorted so the reported
// granted subset is stable across polls.
func newRunAsChecker(users []string) *runAsChecker {
	cp := append([]string(nil), users...)
	sort.Strings(cp)
	return &runAsChecker{users: cp, ttl: runAsCacheTTL, now: time.Now, probe: sudoCanRunAs}
}

// granted returns the advertised users whose sudoers grant is currently in
// place, recomputing at most once per ttl. The returned slice is non-nil even
// when empty, so the console can distinguish "verified, none granted" from
// "never verified" (a nil RunAsGranted).
func (c *runAsChecker) granted() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.valid && c.now().Before(c.expires) {
		return c.cache
	}
	c.cache = verifyRunAs(c.users, c.probe)
	c.expires = c.now().Add(c.ttl)
	c.valid = true
	return c.cache
}
