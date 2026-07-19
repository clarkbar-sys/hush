package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// seedBackups writes a backups.json holding defs into a fresh state dir and
// returns that dir, so a test can exercise exportKeys against real persisted
// state the same way the agent would read it.
func seedBackups(t *testing.T, defs ...Backup) string {
	t.Helper()
	dir := t.TempDir()
	store := newBackupStore(backupStatePath(dir))
	for _, b := range defs {
		if _, err := store.Add(b); err != nil {
			t.Fatalf("seed backup %q: %v", b.Name, err)
		}
	}
	return dir
}

func TestExportKeysIncludesPassword(t *testing.T) {
	dir := seedBackups(t,
		Backup{ID: "aaaa", Name: "root", Repo: "rest:http://nas:8000/root", Password: "hunter2", Paths: []string{"/etc"}, Schedule: "0 3 * * *"},
		Backup{ID: "bbbb", Name: "media", Repo: "rest:http://nas:8000/media", Password: "correct-horse", Paths: []string{"/srv"}},
	)

	var buf bytes.Buffer
	if err := exportKeys(dir, "jaassh", &buf); err != nil {
		t.Fatalf("exportKeys: %v", err)
	}

	var got backupKeyExport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal export: %v\n%s", err, buf.String())
	}
	if got.Host != "jaassh" {
		t.Errorf("host = %q, want jaassh", got.Host)
	}
	if got.GeneratedAt == "" {
		t.Error("generatedAt is empty")
	}
	if len(got.Backups) != 2 {
		t.Fatalf("got %d backups, want 2", len(got.Backups))
	}
	// The escrow document exists to carry the keys — the one thing the API never
	// returns — so the passwords must be present and matched to their repos.
	byRepo := map[string]string{}
	for _, b := range got.Backups {
		byRepo[b.Repo] = b.Password
	}
	if byRepo["rest:http://nas:8000/root"] != "hunter2" {
		t.Errorf("root password = %q, want hunter2", byRepo["rest:http://nas:8000/root"])
	}
	if byRepo["rest:http://nas:8000/media"] != "correct-horse" {
		t.Errorf("media password = %q, want correct-horse", byRepo["rest:http://nas:8000/media"])
	}
}

// A box that never configured a backup has no backups.json; the store tolerates
// the missing file, so the export is a valid document with an empty list rather
// than an error — the command is safe to run anywhere.
func TestExportKeysNoBackups(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	if err := exportKeys(dir, "empty-box", &buf); err != nil {
		t.Fatalf("exportKeys: %v", err)
	}
	var got backupKeyExport
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal export: %v", err)
	}
	if len(got.Backups) != 0 {
		t.Errorf("got %d backups, want 0", len(got.Backups))
	}
	// "backups": [] must serialise as an empty array, not null, so a consumer can
	// iterate the field unconditionally.
	if !strings.Contains(buf.String(), `"backups": []`) {
		t.Errorf("expected an empty backups array, got:\n%s", buf.String())
	}
}

// exportKeys must read the same file the running agent persists to, so its path
// derivation has to match backupStatePath under the resolved state dir.
func TestExportKeysReadsAgentStatePath(t *testing.T) {
	// The seed persisted via backupStatePath(dir); exportKeys must derive the same
	// path from the state dir, so finding the seeded key in the output proves it
	// read the agent's real state file rather than some other location.
	dir := seedBackups(t, Backup{ID: "cccc", Name: "x", Repo: "rest:http://nas:8000/x", Password: "k", Paths: []string{"/"}})
	var buf bytes.Buffer
	if err := exportKeys(dir, "h", &buf); err != nil {
		t.Fatalf("exportKeys: %v", err)
	}
	if !strings.Contains(buf.String(), `"password": "k"`) {
		t.Errorf("export did not read the agent state path; got:\n%s", buf.String())
	}
}
