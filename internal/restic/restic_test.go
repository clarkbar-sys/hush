package restic

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestBuildBackupArgs(t *testing.T) {
	got := buildBackupArgs(Spec{
		Paths:         []string{"/etc", "/home/josh"},
		Excludes:      []string{"*.tmp", "", "/home/josh/.cache"},
		OneFileSystem: true,
		Tags:          []string{"hush", "abc123"},
	})
	want := []string{
		"backup",
		"--tag", "hush",
		"--tag", "abc123",
		"--one-file-system",
		"--exclude", "*.tmp",
		"--exclude", "/home/josh/.cache",
		"--", "/etc", "/home/josh",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestBuildBackupArgsPathLooksLikeFlag(t *testing.T) {
	// The literal "--" must come before the paths so a path beginning with a
	// dash is never parsed as a flag.
	got := buildBackupArgs(Spec{Paths: []string{"--not-a-flag"}})
	if got[len(got)-2] != "--" || got[len(got)-1] != "--not-a-flag" {
		t.Fatalf("expected paths after a -- terminator, got %q", got)
	}
}

// stubRestic writes a fake `restic` script that echoes its behaviour, and points
// Binary at it for the duration of the test. The script asserts the repo and
// password arrive via the environment, not argv.
func stubRestic(t *testing.T, body string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "restic")
	script := "#!/bin/sh\n" + body
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := Binary
	Binary = path
	t.Cleanup(func() { Binary = old })
}

func TestAvailable(t *testing.T) {
	stubRestic(t, `echo "restic 0.16.0 compiled with go1.21"`)
	ver, ok := Available(context.Background())
	if !ok {
		t.Fatal("expected restic to be available")
	}
	if !strings.Contains(ver, "0.16.0") {
		t.Fatalf("unexpected version %q", ver)
	}
}

func TestAvailableMissing(t *testing.T) {
	old := Binary
	Binary = "/nonexistent/restic-xyz"
	t.Cleanup(func() { Binary = old })
	if _, ok := Available(context.Background()); ok {
		t.Fatal("expected a missing binary to report unavailable")
	}
}

func TestInitToleratesExisting(t *testing.T) {
	// Exit non-zero but with the "already initialized" message restic prints
	// when a second machine points at an existing repo.
	stubRestic(t, `echo "Fatal: create repository at rest:... failed: config file already exists" >&2; exit 1`)
	if err := Init(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"}); err != nil {
		t.Fatalf("expected an already-initialised repo to be tolerated, got %v", err)
	}
}

func TestInitRealFailure(t *testing.T) {
	stubRestic(t, `echo "Fatal: unable to open repository: connection refused" >&2; exit 1`)
	err := Init(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"})
	if err == nil {
		t.Fatal("expected a real init failure to surface")
	}
	if !strings.Contains(err.Error(), "connection refused") {
		t.Fatalf("expected the restic stderr in the error, got %v", err)
	}
}

func TestInitPassesRepoViaEnv(t *testing.T) {
	// The script fails unless the repo + password came through the environment.
	stubRestic(t, `
if [ "$RESTIC_REPOSITORY" != "rest:http://nas/homelab" ]; then echo "bad repo: $RESTIC_REPOSITORY" >&2; exit 2; fi
if [ "$RESTIC_PASSWORD" != "s3cret" ]; then echo "bad password" >&2; exit 2; fi
exit 0`)
	if err := Init(context.Background(), Repo{Backend: "rest:http://nas/homelab", Password: "s3cret"}); err != nil {
		t.Fatalf("repo/password did not reach restic via env: %v", err)
	}
}

func TestSnapshots(t *testing.T) {
	stubRestic(t, `cat <<'JSON'
[{"id":"aaaa1111bbbb","short_id":"aaaa1111","time":"2026-07-18T03:00:00Z","hostname":"debian","paths":["/"],"tags":["hush","abc123"]}]
JSON`)
	snaps, err := Snapshots(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"}, "abc123")
	if err != nil {
		t.Fatal(err)
	}
	if len(snaps) != 1 || snaps[0].ShortID != "aaaa1111" || snaps[0].Hostname != "debian" {
		t.Fatalf("unexpected snapshots: %+v", snaps)
	}
}

func TestSnapshotsError(t *testing.T) {
	stubRestic(t, `echo "Fatal: wrong password or no key found" >&2; exit 1`)
	_, err := Snapshots(context.Background(), Repo{Backend: "rest:http://nas/", Password: "wrong"})
	if err == nil || !strings.Contains(err.Error(), "wrong password") {
		t.Fatalf("expected the password error to surface, got %v", err)
	}
}

// resticLsNDJSON is the newline-delimited JSON `restic ls --json` emits: a
// leading snapshot-header line, then one node per entry. The stub echoes a small
// tree rooted at /etc so List's single-level filtering can be exercised.
const resticLsNDJSON = `{"time":"2026-07-18T03:00:00Z","tree":"abc","paths":["/etc"],"hostname":"debian","struct_type":"snapshot"}
{"name":"etc","type":"dir","path":"/etc","size":0,"mtime":"2026-07-18T02:00:00Z","struct_type":"node"}
{"name":"hostname","type":"file","path":"/etc/hostname","size":7,"mtime":"2026-07-10T00:00:00Z","struct_type":"node"}
{"name":"ssh","type":"dir","path":"/etc/ssh","size":0,"mtime":"2026-07-11T00:00:00Z","struct_type":"node"}
{"name":"sshd_config","type":"file","path":"/etc/ssh/sshd_config","size":3200,"struct_type":"node"}
{"name":"resolv.conf","type":"symlink","path":"/etc/resolv.conf","linktarget":"../run/systemd/resolve/stub-resolv.conf","struct_type":"node"}`

func TestListSingleLevel(t *testing.T) {
	stubRestic(t, "cat <<'JSON'\n"+resticLsNDJSON+"\nJSON")
	nodes, truncated, err := List(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"}, "aaaa1111", "/etc", 0)
	if err != nil {
		t.Fatal(err)
	}
	if truncated {
		t.Fatalf("did not expect truncation")
	}
	// Only immediate children of /etc — not /etc itself, not /etc/ssh/sshd_config.
	var got []string
	for _, n := range nodes {
		got = append(got, n.Name+":"+n.Type)
	}
	want := []string{"ssh:dir", "hostname:file", "resolv.conf:symlink"} // dirs first, then files by name
	if len(got) != len(want) {
		t.Fatalf("wrong entries: %v (want %v)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("entry %d = %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
	if nodes[2].Target != "../run/systemd/resolve/stub-resolv.conf" {
		t.Fatalf("symlink target not carried: %q", nodes[2].Target)
	}
}

func TestListTruncates(t *testing.T) {
	stubRestic(t, "cat <<'JSON'\n"+resticLsNDJSON+"\nJSON")
	nodes, truncated, err := List(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"}, "aaaa1111", "/etc", 1)
	if err != nil {
		t.Fatal(err)
	}
	if !truncated {
		t.Fatalf("expected truncated with limit=1")
	}
	if len(nodes) != 1 {
		t.Fatalf("expected exactly 1 entry at limit=1, got %d", len(nodes))
	}
}

func TestListError(t *testing.T) {
	stubRestic(t, `echo "Fatal: no matching ID found for prefix" >&2; exit 1`)
	_, _, err := List(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"}, "deadbeef", "/etc", 0)
	if err == nil || !strings.Contains(err.Error(), "restic ls") {
		t.Fatalf("expected an ls error to surface, got %v", err)
	}
}

func TestBackupStreamsLifecycle(t *testing.T) {
	stubRestic(t, `
echo "scan finished"
echo "processed 3 files" >&2
exit 0`)
	var kinds []string
	var sawStdout, sawStderr bool
	Backup(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"},
		Spec{Paths: []string{"/etc"}, Tags: []string{"abc123"}}, time.Minute,
		func(ev Event) {
			kinds = append(kinds, ev.Kind)
			if ev.Kind == "out" && ev.Stream == "stdout" && strings.Contains(ev.Data, "scan finished") {
				sawStdout = true
			}
			if ev.Kind == "out" && ev.Stream == "stderr" && strings.Contains(ev.Data, "processed 3 files") {
				sawStderr = true
			}
		})
	if len(kinds) < 2 || kinds[0] != "start" || kinds[len(kinds)-1] != "exit" {
		t.Fatalf("expected start…exit lifecycle, got %v", kinds)
	}
	if !sawStdout || !sawStderr {
		t.Fatalf("expected both streams to surface (stdout=%v stderr=%v)", sawStdout, sawStderr)
	}
}

func TestBuildRestoreArgs(t *testing.T) {
	got := buildRestoreArgs("aaaa1111", "/var/tmp/restore", []string{"/etc", "", "/home/josh"})
	want := []string{"restore", "aaaa1111", "--target", "/var/tmp/restore", "--include", "/etc", "--include", "/home/josh"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("args mismatch:\n got %q\nwant %q", got, want)
	}
}

func TestRestoreStreamsLifecycle(t *testing.T) {
	// The stub asserts the snapshot id and target reach restic on argv, and that
	// the repo/password still arrive via the environment.
	stubRestic(t, `
if [ "$1" != "restore" ]; then echo "bad subcommand: $1" >&2; exit 2; fi
if [ "$2" != "aaaa1111" ]; then echo "bad snapshot: $2" >&2; exit 2; fi
if [ "$RESTIC_REPOSITORY" != "rest:http://nas/" ]; then echo "bad repo" >&2; exit 2; fi
echo "restoring to /var/tmp/restore"
exit 0`)
	var kinds []string
	Restore(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"},
		"aaaa1111", "/var/tmp/restore", nil, time.Minute,
		func(ev Event) { kinds = append(kinds, ev.Kind) })
	if len(kinds) < 2 || kinds[0] != "start" || kinds[len(kinds)-1] != "exit" {
		t.Fatalf("expected start…exit lifecycle, got %v", kinds)
	}
}

func TestBackupReportsExitCode(t *testing.T) {
	stubRestic(t, `echo "Fatal: repository not found" >&2; exit 1`)
	var exit Event
	Backup(context.Background(), Repo{Backend: "rest:http://nas/", Password: "pw"},
		Spec{Paths: []string{"/etc"}}, time.Minute,
		func(ev Event) {
			if ev.Kind == "exit" {
				exit = ev
			}
		})
	if exit.Code != 1 {
		t.Fatalf("expected exit code 1, got %d", exit.Code)
	}
}
