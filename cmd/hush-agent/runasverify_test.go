package main

import (
	"testing"
	"time"
)

// verifyRunAs keeps only the users the probe accepts, in input order.
func TestVerifyRunAs(t *testing.T) {
	users := []string{"media", "deploy", "backup"}
	ok := map[string]bool{"media": true, "backup": true}
	got := verifyRunAs(users, func(u string) bool { return ok[u] })
	want := []string{"media", "backup"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Fatalf("verifyRunAs = %v, want %v", got, want)
	}
}

// A verified-but-none-granted result is a non-nil empty slice, so the caller can
// tell it apart from "never verified" (a nil RunAsGranted).
func TestVerifyRunAsNoneGrantedIsNonNil(t *testing.T) {
	got := verifyRunAs([]string{"media"}, func(string) bool { return false })
	if got == nil {
		t.Fatal("verifyRunAs returned nil; want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("verifyRunAs = %v, want empty", got)
	}
}

// The checker reuses its result within the TTL and reprobes once it expires.
func TestRunAsCheckerCaches(t *testing.T) {
	calls := 0
	now := time.Unix(0, 0)
	c := &runAsChecker{
		users: []string{"media"},
		ttl:   30 * time.Second,
		now:   func() time.Time { return now },
		probe: func(string) bool { calls++; return true },
	}
	c.granted()
	c.granted()
	if calls != 1 {
		t.Fatalf("probe called %d times within TTL, want 1", calls)
	}
	now = now.Add(31 * time.Second)
	c.granted()
	if calls != 2 {
		t.Fatalf("probe called %d times after TTL expiry, want 2", calls)
	}
}

// newRunAsChecker sorts its user list so the granted subset is stable across
// polls regardless of the order the allowlist was given in.
func TestNewRunAsCheckerSorts(t *testing.T) {
	c := newRunAsChecker([]string{"deploy", "media", "backup"})
	want := []string{"backup", "deploy", "media"}
	for i := range want {
		if c.users[i] != want[i] {
			t.Fatalf("newRunAsChecker users = %v, want %v", c.users, want)
		}
	}
}
