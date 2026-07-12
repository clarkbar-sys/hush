package browse

import (
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
