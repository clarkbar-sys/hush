package store

import (
	"os"
	"path/filepath"
	"testing"
)

type item struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func idOf(i item) string { return i.ID }

func newItemStore(t *testing.T) (*JSON[item], string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "items.json")
	return New(path, "items", idOf), path
}

func TestAddPersistsAndReloads(t *testing.T) {
	s, path := newItemStore(t)
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("fresh store: want empty, got %d", len(got))
	}
	if _, err := s.Add(item{ID: "a", Name: "one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// A second store over the same file must see the persisted item.
	reloaded := New(path, "items", idOf)
	got := reloaded.Snapshot()
	if len(got) != 1 || got[0].ID != "a" || got[0].Name != "one" {
		t.Fatalf("reloaded snapshot = %+v", got)
	}
}

func TestFind(t *testing.T) {
	s, _ := newItemStore(t)
	if _, err := s.Add(item{ID: "a", Name: "one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if got, ok := s.Find("a"); !ok || got.Name != "one" {
		t.Fatalf("Find(a) = %+v, %v", got, ok)
	}
	if _, ok := s.Find("missing"); ok {
		t.Fatal("Find(missing): want not found")
	}
}

func TestUpdatePreservesUntouchedFields(t *testing.T) {
	s, _ := newItemStore(t)
	if _, err := s.Add(item{ID: "a", Name: "one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	got, found, err := s.Update("a", func(i *item) { i.Name = "two" })
	if err != nil || !found {
		t.Fatalf("Update: %v, found=%v", err, found)
	}
	if got.ID != "a" || got.Name != "two" {
		t.Fatalf("Update returned %+v, want id a / name two", got)
	}
	// Updating an unknown id reports not-found without error.
	if _, found, err := s.Update("missing", func(*item) {}); err != nil || found {
		t.Fatalf("Update(missing) = found=%v err=%v", found, err)
	}
}

func TestDelete(t *testing.T) {
	s, _ := newItemStore(t)
	if _, err := s.Add(item{ID: "a", Name: "one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	removed, err := s.Delete("a")
	if err != nil || !removed {
		t.Fatalf("Delete(a) = removed=%v err=%v", removed, err)
	}
	if removed, _ := s.Delete("a"); removed {
		t.Fatal("Delete(a) twice: second call should report nothing removed")
	}
	if got := s.Snapshot(); len(got) != 0 {
		t.Fatalf("after delete: want empty, got %d", len(got))
	}
}

func TestLoadToleratesMissingAndCorrupt(t *testing.T) {
	dir := t.TempDir()

	// Missing file -> empty, non-nil.
	if got := Load[item](filepath.Join(dir, "nope.json"), "items"); got == nil || len(got) != 0 {
		t.Fatalf("Load(missing) = %v (want empty non-nil)", got)
	}

	// Corrupt file -> empty rather than a crash or error.
	corrupt := filepath.Join(dir, "corrupt.json")
	if err := os.WriteFile(corrupt, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := Load[item](corrupt, "items"); len(got) != 0 {
		t.Fatalf("Load(corrupt) = %v (want empty)", got)
	}
}

func TestSnapshotIsACopy(t *testing.T) {
	s, _ := newItemStore(t)
	if _, err := s.Add(item{ID: "a", Name: "one"}); err != nil {
		t.Fatalf("Add: %v", err)
	}
	snap := s.Snapshot()
	snap[0].Name = "mutated"
	if got, _ := s.Find("a"); got.Name != "one" {
		t.Fatalf("mutating a snapshot leaked into the store: %+v", got)
	}
}

func TestSaveIsAtomicAndReadable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "items.json")
	if err := Save(path, []item{{ID: "a", Name: "one"}}); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if got := Load[item](path, "items"); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("round-trip = %+v", got)
	}
	// No .tmp sidecar should survive a successful write.
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Fatalf("temp file lingered: err=%v", err)
	}
}
