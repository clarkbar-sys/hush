package exec

import (
	"context"
	"strings"
	"testing"
)

func TestValidUserName(t *testing.T) {
	valid := []string{"media", "deploy", "backup", "_svc", "a", "user-1", "web$"}
	for _, u := range valid {
		if !ValidUserName(u) {
			t.Errorf("ValidUserName(%q) = false, want true", u)
		}
	}
	invalid := []string{
		"",                      // empty
		"root; rm -rf /",        // shell metacharacters
		"-rf",                   // leading hyphen (would look like a flag to sudo)
		"1user",                 // leading digit
		"UPPER",                 // uppercase
		"has space",             // space
		"a$b",                   // $ only allowed as a trailing marker
		strings.Repeat("a", 40), // too long
	}
	for _, u := range invalid {
		if ValidUserName(u) {
			t.Errorf("ValidUserName(%q) = true, want false", u)
		}
	}
}

// With no user, the command is the historical `sh -c <cmd>` — sudo is never
// involved, so an agent that never opted into run-as behaves exactly as before.
func TestCommandForNoUser(t *testing.T) {
	c, err := commandFor(context.Background(), "", "echo hi")
	if err != nil {
		t.Fatalf("commandFor: %v", err)
	}
	want := []string{"sh", "-c", "echo hi"}
	if !argsEqual(c.Args, want) {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
}

// With a user, the command is wrapped in `sudo -n -u <user> --`, and the
// username rides as its own argument — never interpolated into the shell line,
// so it can't break out into the command.
func TestCommandForWithUser(t *testing.T) {
	c, err := commandFor(context.Background(), "media", "echo hi")
	if err != nil {
		t.Fatalf("commandFor: %v", err)
	}
	want := []string{"sudo", "-n", "-u", "media", "--", "sh", "-c", "echo hi"}
	if !argsEqual(c.Args, want) {
		t.Errorf("args = %v, want %v", c.Args, want)
	}
}

// A malformed user is rejected before building the command, so a bad value can
// never reach sudo's argv.
func TestCommandForRejectsBadUser(t *testing.T) {
	if _, err := commandFor(context.Background(), "root; id", "echo hi"); err == nil {
		t.Fatal("commandFor accepted a malformed user, want error")
	}
}

// Run surfaces a malformed user as a single error event rather than shelling
// out — the same shape callers already handle for an empty command.
func TestRunRejectsBadUser(t *testing.T) {
	evs := collect(context.Background(), Spec{Cmd: "echo hi", User: "bad user"})
	if len(evs) != 1 || evs[0].Kind != "error" {
		t.Fatalf("want a single error event, got %+v", evs)
	}
}

func argsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
