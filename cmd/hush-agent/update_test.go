package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// fakeSystemctl writes a stub `systemctl` onto a fresh PATH so restartService's
// exec runs a script we control instead of the real init system. The stub
// prints body to stderr and exits with code, letting a test drive the three
// outcomes restartService distinguishes: success, "not found", and a real
// failure.
func fakeSystemctl(t *testing.T, body string, code int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("POSIX shell stub; not applicable on Windows")
	}
	dir := t.TempDir()
	script := "#!/bin/sh\n"
	if body != "" {
		script += "echo '" + body + "' >&2\n"
	}
	script += "exit " + itoa(code) + "\n"
	path := filepath.Join(dir, "systemctl")
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}
	t.Setenv("PATH", dir)
}

// itoa keeps the stub script free of an fmt import in the test's hot path.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	if neg {
		b = append([]byte{'-'}, b...)
	}
	return string(b)
}

func TestRestartServiceSuccess(t *testing.T) {
	fakeSystemctl(t, "", 0)
	if err := restartService(context.Background()); err != nil {
		t.Fatalf("restartService = %v, want nil", err)
	}
}

func TestRestartServiceNotFoundIsSuccess(t *testing.T) {
	// A box where hush-agent isn't a systemd unit: try-restart reports "Unit
	// hush-agent.service not found." and exits non-zero. There's nothing to
	// restart, so restartService treats it as success.
	fakeSystemctl(t, "Unit hush-agent.service not found.", 5)
	if err := restartService(context.Background()); err != nil {
		t.Fatalf("restartService = %v, want nil (not-found is benign)", err)
	}
}

func TestRestartServiceRealFailure(t *testing.T) {
	fakeSystemctl(t, "Failed to restart hush-agent.service: some real error", 1)
	err := restartService(context.Background())
	if err == nil {
		t.Fatal("restartService = nil, want an error on a real failure")
	}
	if !strings.Contains(err.Error(), "hush-agent.service") {
		t.Fatalf("error %q missing unit name", err)
	}
}
