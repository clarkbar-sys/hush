package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/clarkbar-sys/hush/internal/restic"
)

// stubResticAgent points restic.Binary at a fake that answers every subcommand
// the manager uses, so Add/Run/Snapshots run without restic installed. snapshot
// controls whether `snapshots` returns one entry (to exercise LastSnapshot) or
// an empty list.
func stubResticAgent(t *testing.T, withSnapshot bool) {
	t.Helper()
	snaps := "[]"
	if withSnapshot {
		snaps = `[{"id":"aaaa1111bbbb","short_id":"aaaa1111","time":"2026-07-18T03:00:00Z","hostname":"debian","paths":["/"],"tags":["hush","x"]}]`
	}
	dir := t.TempDir()
	path := filepath.Join(dir, "restic")
	script := "#!/bin/sh\ncase \"$1\" in\n" +
		"version) echo 'restic 0.16.0'; exit 0;;\n" +
		"init) exit 0;;\n" +
		"snapshots) echo '" + snaps + "'; exit 0;;\n" +
		"backup) echo 'files new 1'; exit 0;;\n" +
		"restore) echo 'restoring'; exit 0;;\n" +
		"ls) cat <<'LSJSON'\n" + lsStubNDJSON + "\nLSJSON\nexit 0;;\n" +
		"*) echo \"unknown $1\" >&2; exit 2;;\nesac\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	old := restic.Binary
	restic.Binary = path
	t.Cleanup(func() { restic.Binary = old })
}

// lsStubNDJSON is what the stub restic emits for `ls`: a snapshot header line
// then a small /etc tree, so the LS handler's single-level filtering is exercised.
const lsStubNDJSON = `{"time":"2026-07-18T03:00:00Z","paths":["/etc"],"hostname":"debian","struct_type":"snapshot"}
{"name":"hostname","type":"file","path":"/etc/hostname","size":7,"struct_type":"node"}
{"name":"ssh","type":"dir","path":"/etc/ssh","struct_type":"node"}
{"name":"sshd_config","type":"file","path":"/etc/ssh/sshd_config","size":3200,"struct_type":"node"}`

func newTestManager(t *testing.T) *backupManager {
	t.Helper()
	return newBackupManager(filepath.Join(t.TempDir(), "backups.json"))
}

func validReq() (string, string, string, []string, []string, bool, string) {
	return "debian-root", "rest:http://nas:8000/homelab", "s3cret", []string{"/", "/etc"}, []string{"*.tmp"}, true, ""
}

func TestValidateBackupOK(t *testing.T) {
	b, err := validateBackup(validReq())
	if err != nil {
		t.Fatal(err)
	}
	if b.ID == "" || b.CreatedAt == "" {
		t.Fatalf("expected id and createdAt filled in: %+v", b)
	}
	if len(b.Paths) != 2 || b.Paths[0] != "/" {
		t.Fatalf("unexpected paths: %v", b.Paths)
	}
	if !b.OneFileSystem {
		t.Fatal("expected oneFileSystem preserved")
	}
}

func TestValidateBackupRejects(t *testing.T) {
	cases := []struct {
		name            string
		bname, repo, pw string
		paths           []string
		want            string
	}{
		{"no name", "", "rest:http://nas/", "pw", []string{"/"}, "name"},
		{"no repo", "n", "", "pw", []string{"/"}, "repository is required"},
		{"no password", "n", "rest:http://nas/", "", []string{"/"}, "password"},
		{"no paths", "n", "rest:http://nas/", "pw", []string{"  "}, "at least one path"},
		{"relative path", "n", "rest:http://nas/", "pw", []string{"etc"}, "must be absolute"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := validateBackup(c.bname, c.repo, c.pw, c.paths, nil, false, "")
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("want error containing %q, got %v", c.want, err)
			}
		})
	}
}

func TestBackupViewOmitsPassword(t *testing.T) {
	m := newTestManager(t)
	def, err := validateBackup(validReq())
	if err != nil {
		t.Fatal(err)
	}
	view := m.view(def)
	b, err := json.Marshal(view)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "s3cret") || strings.Contains(strings.ToLower(string(b)), "password") {
		t.Fatalf("view leaked the password: %s", b)
	}
	if !strings.Contains(string(b), "debian-root") {
		t.Fatalf("view should carry the name: %s", b)
	}
}

func TestBackupAddPersistsAndListsWithoutPassword(t *testing.T) {
	stubResticAgent(t, false)
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	list := m.List()
	if len(list) != 1 || list[0].Name != "debian-root" {
		t.Fatalf("unexpected list: %+v", list)
	}
	// The stored definition, on the other hand, must keep the password so restic
	// can use it — it just never leaves the box through the API.
	if got := m.store.Snapshot()[0].Password; got != "s3cret" {
		t.Fatalf("stored definition should retain the password, got %q", got)
	}
}

func TestBackupAddFailsWithoutRestic(t *testing.T) {
	old := restic.Binary
	restic.Binary = "/nonexistent/restic-xyz"
	t.Cleanup(func() { restic.Binary = old })
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	_, err := m.Add(context.Background(), def)
	if err == nil || !strings.Contains(err.Error(), "restic is not installed") {
		t.Fatalf("want a restic-not-installed error, got %v", err)
	}
}

func TestBackupRunRecordsStatus(t *testing.T) {
	stubResticAgent(t, true)
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	var sawStart, sawExit bool
	if err := m.Run(context.Background(), def.ID, func(ev restic.Event) {
		switch ev.Kind {
		case "start":
			sawStart = true
		case "exit":
			sawExit = true
		}
	}); err != nil {
		t.Fatal(err)
	}
	if !sawStart || !sawExit {
		t.Fatalf("expected the run to stream start and exit (start=%v exit=%v)", sawStart, sawExit)
	}
	st := m.List()[0].Status
	if st.Runs != 1 || st.LastCode != 0 {
		t.Fatalf("unexpected status: %+v", st)
	}
	if st.LastSnapshot != "aaaa1111" {
		t.Fatalf("expected LastSnapshot from the post-run listing, got %q", st.LastSnapshot)
	}
}

func TestBackupDeleteForgetsDefinition(t *testing.T) {
	stubResticAgent(t, false)
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	removed, err := m.Delete(def.ID)
	if err != nil || !removed {
		t.Fatalf("delete failed: removed=%v err=%v", removed, err)
	}
	if len(m.List()) != 0 {
		t.Fatal("expected the definition to be gone")
	}
	again, _ := m.Delete(def.ID)
	if again {
		t.Fatal("expected a second delete to report nothing removed")
	}
}

func TestBackupRestoreStreams(t *testing.T) {
	stubResticAgent(t, false)
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	var sawStart, sawExit bool
	err := m.Restore(context.Background(), def.ID, "aaaa1111", "/var/tmp/hush-restore", nil, func(ev restic.Event) {
		switch ev.Kind {
		case "start":
			sawStart = true
		case "exit":
			sawExit = true
		}
	})
	if err != nil {
		t.Fatal(err)
	}
	if !sawStart || !sawExit {
		t.Fatalf("expected the restore to stream start and exit (start=%v exit=%v)", sawStart, sawExit)
	}
}

func TestBackupRestoreUnknownID(t *testing.T) {
	stubResticAgent(t, false)
	m := newTestManager(t)
	err := m.Restore(context.Background(), "nope", "latest", "/var/tmp/x", nil, func(restic.Event) {})
	if !errors.Is(err, errBackupNotFound) {
		t.Fatalf("want errBackupNotFound, got %v", err)
	}
}

func TestBackupSnapshotLS(t *testing.T) {
	stubResticAgent(t, true)
	m := newTestManager(t)
	def, _ := validateBackup(validReq())
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/backups/{id}/snapshots/{snap}/ls", m.handleBackupSnapshotLS)
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// Valid listing: immediate children of /etc only (not /etc/ssh/sshd_config).
	resp, err := http.Get(srv.URL + "/backups/" + def.ID + "/snapshots/aaaa1111/ls?path=/etc")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var out struct {
		Path      string        `json:"path"`
		Entries   []restic.Node `json:"entries"`
		Truncated bool          `json:"truncated"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Path != "/etc" {
		t.Fatalf("path = %q, want /etc", out.Path)
	}
	if len(out.Entries) != 2 {
		t.Fatalf("want 2 immediate children of /etc, got %d: %+v", len(out.Entries), out.Entries)
	}
	if out.Entries[0].Name != "ssh" || out.Entries[0].Type != "dir" {
		t.Fatalf("dirs should sort first, got %+v", out.Entries[0])
	}

	// A bad snapshot id is rejected before restic runs.
	bad, err := http.Get(srv.URL + "/backups/" + def.ID + "/snapshots/not$valid/ls")
	if err != nil {
		t.Fatal(err)
	}
	bad.Body.Close()
	if bad.StatusCode != http.StatusBadRequest {
		t.Fatalf("bad snapshot id status = %d, want 400", bad.StatusCode)
	}

	// A relative path is rejected.
	rel, err := http.Get(srv.URL + "/backups/" + def.ID + "/snapshots/aaaa1111/ls?path=etc")
	if err != nil {
		t.Fatal(err)
	}
	rel.Body.Close()
	if rel.StatusCode != http.StatusBadRequest {
		t.Fatalf("relative path status = %d, want 400", rel.StatusCode)
	}
}

func TestSnapshotIDValidation(t *testing.T) {
	for _, ok := range []string{"latest", "aaaa1111", "0123456789abcdef", "AABBCCDDEE"} {
		if !snapshotIDRE.MatchString(ok) {
			t.Errorf("%q should be accepted", ok)
		}
	}
	for _, bad := range []string{"", "../etc", "latest; rm -rf /", "xyz", "aa"} {
		if snapshotIDRE.MatchString(bad) {
			t.Errorf("%q should be rejected", bad)
		}
	}
}

func TestValidateBackupRejectsBadSchedule(t *testing.T) {
	_, err := validateBackup("n", "rest:http://nas/", "pw", []string{"/"}, nil, false, "not a cron spec")
	if err == nil || !strings.Contains(err.Error(), "invalid schedule") {
		t.Fatalf("want an invalid-schedule error, got %v", err)
	}
}

func TestValidateBackupAcceptsSchedule(t *testing.T) {
	b, err := validateBackup("nightly", "rest:http://nas/", "pw", []string{"/"}, nil, false, "0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if b.Schedule != "0 3 * * *" {
		t.Fatalf("schedule not preserved: %q", b.Schedule)
	}
}

func TestBackupScheduleRegistersAndFires(t *testing.T) {
	stubResticAgent(t, true)
	m := newTestManager(t)
	m.Start()
	defer m.Stop()

	// @every 1s so the test observes a real unattended fire without waiting a
	// minute — the same parser a "0 3 * * *" would use.
	def, err := validateBackup("nightly", "rest:http://nas:8000/homelab", "pw", []string{"/etc"}, nil, false, "@every 1s")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}

	m.mu.Lock()
	_, registered := m.entries[def.ID]
	m.mu.Unlock()
	if !registered {
		t.Fatal("expected a scheduled backup to get a cron entry")
	}
	v := m.List()[0]
	if v.Schedule != "@every 1s" {
		t.Fatalf("view schedule = %q", v.Schedule)
	}
	if v.NextRun == "" {
		t.Fatal("expected a running scheduled backup to report a next-run time")
	}

	// Wait for the clock to fire it at least once, unattended (no client).
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if m.List()[0].Status.Runs > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	st := m.List()[0].Status
	if st.Runs == 0 {
		t.Fatal("expected the scheduled backup to have fired unattended")
	}
	if st.LastCode != 0 {
		t.Fatalf("scheduled fire failed: %+v", st)
	}
}

func TestBackupDeleteUnregistersSchedule(t *testing.T) {
	stubResticAgent(t, false)
	m := newTestManager(t)
	def, err := validateBackup("nightly", "rest:http://nas/", "pw", []string{"/etc"}, nil, false, "0 3 * * *")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.Add(context.Background(), def); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Delete(def.ID); err != nil {
		t.Fatal(err)
	}
	m.mu.Lock()
	_, still := m.entries[def.ID]
	m.mu.Unlock()
	if still {
		t.Fatal("expected delete to remove the cron entry")
	}
}
