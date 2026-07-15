package main

import "testing"

func TestParseRunAs(t *testing.T) {
	got := parseRunAs(" media, deploy ,, backup ,media")
	want := map[string]bool{"media": true, "deploy": true, "backup": true}
	if len(got) != len(want) {
		t.Fatalf("parseRunAs size = %d (%v), want %d", len(got), got, len(want))
	}
	for u := range want {
		if !got[u] {
			t.Errorf("parseRunAs missing %q; got %v", u, got)
		}
	}
}

// An empty spec yields the empty set — the feature stays off, and /exec refuses
// every run-as request.
func TestParseRunAsEmpty(t *testing.T) {
	if got := parseRunAs("   "); len(got) != 0 {
		t.Errorf("parseRunAs(empty) = %v, want empty set", got)
	}
}

// A malformed entry is dropped, but valid siblings on the same line survive —
// one typo doesn't take the whole allowlist (or the agent) down.
func TestParseRunAsDropsInvalid(t *testing.T) {
	got := parseRunAs("media, root; rm -rf /, deploy")
	if got["root; rm -rf /"] {
		t.Error("parseRunAs kept a malformed entry")
	}
	if !got["media"] || !got["deploy"] {
		t.Errorf("parseRunAs dropped valid entries; got %v", got)
	}
}
