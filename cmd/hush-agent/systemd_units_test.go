package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// systemd's ExecStart/EnvironmentFile expansion supports only "$VAR" and
// "${VAR}" — NOT shell-style defaults like "${VAR:-default}" (or ":+", "%",
// etc). When such a form is used systemd passes the whole "${VAR:-default}"
// token through literally, so the binary receives the raw string as its flag
// value and fails at startup (e.g. `-listen ${HUSH_AGENT_LISTEN:-tailnet}` →
// net.Listen("${HUSH_AGENT_LISTEN:-tailnet}") → "unknown port", crash-loop).
//
// A default belongs in an Environment= line, which the optional
// EnvironmentFile= below it overrides when present. This test guards every
// shipped unit against a regression back to the unsupported syntax, since a
// broken ExecStart only shows up at deploy time (re-running the installer),
// never in a normal `go test`/`go build`.
var shellDefaultExpansion = regexp.MustCompile(`\$\{[A-Za-z_][A-Za-z0-9_]*:[-+?=]`)

func unitDir(t *testing.T) string {
	t.Helper()
	// Tests run with the package directory as the working directory, so the
	// repo's systemd/ dir is two levels up from cmd/hush-agent.
	dir := filepath.Join("..", "..", "systemd")
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("cannot locate systemd unit dir %s: %v", dir, err)
	}
	return dir
}

func TestUnitsHaveNoShellStyleDefaultExpansion(t *testing.T) {
	dir := unitDir(t)
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read %s: %v", dir, err)
	}
	var checked int
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".service") {
			continue
		}
		checked++
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatalf("read %s: %v", e.Name(), err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			// Only executable/environment directives matter; comments explaining
			// the pitfall may legitimately mention the syntax.
			trimmed := strings.TrimSpace(line)
			if strings.HasPrefix(trimmed, "#") {
				continue
			}
			if !strings.HasPrefix(trimmed, "ExecStart") &&
				!strings.HasPrefix(trimmed, "ExecStartPre") &&
				!strings.HasPrefix(trimmed, "ExecStartPost") &&
				!strings.HasPrefix(trimmed, "Environment=") {
				continue
			}
			if m := shellDefaultExpansion.FindString(line); m != "" {
				t.Errorf("%s:%d uses unsupported shell-style expansion %q — systemd passes it through literally; express the default with Environment= instead:\n\t%s",
					e.Name(), i+1, m, trimmed)
			}
		}
	}
	if checked == 0 {
		t.Fatalf("no .service files found in %s", dir)
	}
}
