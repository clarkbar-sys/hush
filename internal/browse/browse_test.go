package browse

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestListSortsDirsFirstThenAlpha(t *testing.T) {
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "zeta"))
	mustMkdir(t, filepath.Join(dir, "alpha"))
	mustWrite(t, filepath.Join(dir, "beta.txt"), "hi")
	mustWrite(t, filepath.Join(dir, "aardvark.txt"), "yo")

	l, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	want := []string{"alpha", "zeta", "aardvark.txt", "beta.txt"}
	if len(l.Entries) != len(want) {
		t.Fatalf("got %d entries, want %d: %+v", len(l.Entries), len(want), l.Entries)
	}
	for i, name := range want {
		if l.Entries[i].Name != name {
			t.Errorf("entry %d = %q, want %q", i, l.Entries[i].Name, name)
		}
	}
}

func TestListReportsMetadata(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "file.txt"), "12345")

	l, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	e := l.Entries[0]
	if e.IsDir {
		t.Errorf("file.txt reported as dir")
	}
	if e.Size != 5 {
		t.Errorf("size = %d, want 5", e.Size)
	}
	if e.Mode == "" || e.ModTime == "" {
		t.Errorf("missing mode/modTime: %+v", e)
	}
}

func TestListSymlinkTarget(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink perms differ on windows")
	}
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "real.txt"), "x")
	if err := os.Symlink(filepath.Join(dir, "real.txt"), filepath.Join(dir, "link")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	l, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	var link *Entry
	for i := range l.Entries {
		if l.Entries[i].Name == "link" {
			link = &l.Entries[i]
		}
	}
	if link == nil {
		t.Fatal("link entry missing")
	}
	if link.Symlink == "" {
		t.Errorf("symlink target not resolved: %+v", link)
	}
}

func TestListParent(t *testing.T) {
	// A non-root dir reports its parent; "/" reports none.
	dir := t.TempDir()
	l, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if l.Parent != filepath.Dir(dir) {
		t.Errorf("parent = %q, want %q", l.Parent, filepath.Dir(dir))
	}

	root, err := List("/")
	if err != nil {
		t.Fatalf("List(/): %v", err)
	}
	if root.Parent != "" {
		t.Errorf("root parent = %q, want empty", root.Parent)
	}
}

func TestListDefaultsToRoot(t *testing.T) {
	l, err := List("")
	if err != nil {
		t.Fatalf("List(\"\"): %v", err)
	}
	if l.Path != "/" {
		t.Errorf("empty path listed %q, want /", l.Path)
	}
}

func TestListRejectsRelative(t *testing.T) {
	_, err := List("some/relative/path")
	if err == nil {
		t.Fatal("expected error for relative path")
	}
	if !errors.Is(err, os.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestListNotFound(t *testing.T) {
	_, err := List(filepath.Join(t.TempDir(), "does-not-exist"))
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want IsNotExist", err)
	}
}

func TestListNotADir(t *testing.T) {
	dir := t.TempDir()
	f := filepath.Join(dir, "file.txt")
	mustWrite(t, f, "x")
	_, err := List(f)
	if err == nil {
		t.Fatal("expected error listing a file as a dir")
	}
}

func TestListTruncates(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < maxEntries+10; i++ {
		mustWrite(t, filepath.Join(dir, "f"+pad(i)), "")
	}
	l, err := List(dir)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if !l.Truncated {
		t.Error("expected Truncated=true")
	}
	if len(l.Entries) != maxEntries {
		t.Errorf("got %d entries, want %d", len(l.Entries), maxEntries)
	}
}

func mustMkdir(t *testing.T, p string) {
	t.Helper()
	if err := os.Mkdir(p, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", p, err)
	}
}

func mustWrite(t *testing.T, p, content string) {
	t.Helper()
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
}

func pad(i int) string {
	s := ""
	for _, d := range []int{1000, 100, 10, 1} {
		s += string(rune('0' + (i/d)%10))
	}
	return s
}

func TestDuSumsFileSizesRecursively(t *testing.T) {
	dir := t.TempDir()
	mustWrite(t, filepath.Join(dir, "top.txt"), "12345") // 5 bytes
	mustMkdir(t, filepath.Join(dir, "sub"))
	mustWrite(t, filepath.Join(dir, "sub", "a.txt"), "1234567890") // 10 bytes
	mustMkdir(t, filepath.Join(dir, "sub", "nested"))
	mustWrite(t, filepath.Join(dir, "sub", "nested", "b.txt"), "123") // 3 bytes

	l, err := Du(context.Background(), dir)
	if err != nil {
		t.Fatalf("Du: %v", err)
	}
	if l.Truncated {
		t.Error("unexpected Truncated=true")
	}
	if len(l.Entries) != 2 {
		t.Fatalf("got %d entries, want 2: %+v", len(l.Entries), l.Entries)
	}
	// Largest first: sub (13 bytes) before top.txt (5 bytes).
	if l.Entries[0].Name != "sub" || l.Entries[0].Size != 13 || !l.Entries[0].IsDir {
		t.Errorf("entries[0] = %+v, want sub dir size 13", l.Entries[0])
	}
	if l.Entries[1].Name != "top.txt" || l.Entries[1].Size != 5 || l.Entries[1].IsDir {
		t.Errorf("entries[1] = %+v, want top.txt size 5", l.Entries[1])
	}
}

func TestDuSymlinkNotFollowed(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink perms differ on windows")
	}
	dir := t.TempDir()
	mustMkdir(t, filepath.Join(dir, "real"))
	mustWrite(t, filepath.Join(dir, "real", "big.txt"), "0123456789")
	linkPath := filepath.Join(dir, "link")
	if err := os.Symlink(filepath.Join(dir, "real"), linkPath); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	lstat, err := os.Lstat(linkPath)
	if err != nil {
		t.Fatalf("lstat: %v", err)
	}

	l, err := Du(context.Background(), dir)
	if err != nil {
		t.Fatalf("Du: %v", err)
	}
	var link *DuEntry
	for i := range l.Entries {
		if l.Entries[i].Name == "link" {
			link = &l.Entries[i]
		}
	}
	if link == nil {
		t.Fatal("link entry missing")
	}
	if link.IsDir {
		t.Error("symlink reported as dir — should not be followed")
	}
	if link.Size != lstat.Size() {
		t.Errorf("symlink size = %d, want its own lstat size %d (not the target's contents)", link.Size, lstat.Size())
	}
}

func TestDuEmptyDir(t *testing.T) {
	dir := t.TempDir()
	l, err := Du(context.Background(), dir)
	if err != nil {
		t.Fatalf("Du: %v", err)
	}
	if len(l.Entries) != 0 {
		t.Errorf("got %d entries, want 0", len(l.Entries))
	}
	if l.Truncated {
		t.Error("unexpected Truncated=true")
	}
}

func TestDuRejectsRelative(t *testing.T) {
	_, err := Du(context.Background(), "some/relative/path")
	if !errors.Is(err, os.ErrInvalid) {
		t.Errorf("err = %v, want ErrInvalid", err)
	}
}

func TestDuNotFound(t *testing.T) {
	_, err := Du(context.Background(), filepath.Join(t.TempDir(), "does-not-exist"))
	if !os.IsNotExist(err) {
		t.Errorf("err = %v, want IsNotExist", err)
	}
}

func TestDuDeadlineTruncates(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 5; i++ {
		sub := filepath.Join(dir, "d"+pad(i))
		mustMkdir(t, sub)
		mustWrite(t, filepath.Join(sub, "f.txt"), "x")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // already expired — the very first ctx.Err() check should trip
	l, err := Du(ctx, dir)
	if err != nil {
		t.Fatalf("Du: %v", err)
	}
	if !l.Truncated {
		t.Error("expected Truncated=true with an already-cancelled context")
	}
}
