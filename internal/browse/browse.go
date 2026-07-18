// Package browse lists a directory on the host filesystem. It is read-only and
// deliberately unjailed: any absolute path is fair game, and the only thing
// that gates what comes back is the OS permissions of the user the agent runs
// as (the unprivileged "hush" system user). Read a dir it can't reach and the
// OS returns permission-denied, exactly as it would for that user in a shell —
// the Unix identity is the security boundary, not this code.
package browse

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

// maxEntries caps a single listing so a directory with pathological fan-out
// (e.g. a Maildir with 200k files) can't blow up the response. Truncation is
// surfaced on the Listing so the UI can say "showing first N".
const maxEntries = 2000

// Entry is one item in a directory: a child file, dir, or symlink. Fields
// mirror what `ls -l` shows so the console can render a familiar listing.
type Entry struct {
	Name    string `json:"name"`
	IsDir   bool   `json:"isDir"`
	Size    int64  `json:"size"`              // bytes; meaningless for dirs
	Mode    string `json:"mode"`              // e.g. "-rw-r--r--"
	ModTime string `json:"modTime"`           // RFC3339, empty if unreadable
	Symlink string `json:"symlink,omitempty"` // target if this entry is a symlink
}

// Listing is the result of reading one directory.
type Listing struct {
	Path      string  `json:"path"`      // cleaned absolute path that was listed
	Parent    string  `json:"parent"`    // parent dir; "" when Path is "/"
	Entries   []Entry `json:"entries"`   // dirs first, then files, each alphabetical
	Truncated bool    `json:"truncated"` // true if maxEntries clipped the listing
}

// List reads the directory at path and returns its entries. An empty path is
// treated as "/". The path is cleaned (so ".." collapses to the real parent —
// there is no jail to escape) but must be absolute. Errors from the OS
// (permission denied, no such dir, not a directory) are returned unwrapped so
// callers can map them with os.IsPermission / os.IsNotExist / errors.Is.
func List(path string) (Listing, error) {
	if path == "" {
		path = "/"
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return Listing{}, &os.PathError{Op: "browse", Path: path, Err: os.ErrInvalid}
	}

	dirents, err := os.ReadDir(clean)
	if err != nil {
		return Listing{}, err
	}

	l := Listing{Path: clean, Parent: parentOf(clean)}
	if len(dirents) > maxEntries {
		dirents = dirents[:maxEntries]
		l.Truncated = true
	}

	l.Entries = make([]Entry, 0, len(dirents))
	for _, de := range dirents {
		e := Entry{Name: de.Name(), IsDir: de.IsDir()}
		// Info is lstat-based: it describes the entry itself, so a symlink
		// reports as a symlink rather than following through to its target.
		if info, ierr := de.Info(); ierr == nil {
			e.Size = info.Size()
			e.Mode = info.Mode().String()
			e.ModTime = info.ModTime().UTC().Format(time.RFC3339)
			if info.Mode()&os.ModeSymlink != 0 {
				if target, lerr := os.Readlink(filepath.Join(clean, de.Name())); lerr == nil {
					e.Symlink = target
				}
			}
		}
		l.Entries = append(l.Entries, e)
	}

	sort.Slice(l.Entries, func(i, j int) bool {
		if l.Entries[i].IsDir != l.Entries[j].IsDir {
			return l.Entries[i].IsDir // directories first
		}
		return l.Entries[i].Name < l.Entries[j].Name
	})
	return l, nil
}

// parentOf returns the parent directory of an absolute path, or "" when the
// path is the filesystem root (which has no parent to climb to).
func parentOf(clean string) string {
	if clean == "/" {
		return ""
	}
	return filepath.Dir(clean)
}

// maxDuFiles bounds how many files a single Du call will stat, summed across
// every immediate child directory it recurses into. Without a cap, sizing
// something enormous — a whole root filesystem, a NAS's media pool — could
// run for as long as the disk takes to enumerate; hitting the cap (or ctx's
// deadline) reports Truncated instead of blocking the caller indefinitely.
const maxDuFiles = 300_000

// DuEntry is one immediate child of the directory being sized, carrying its
// full recursive size: a file's own size, or the sum of everything under a
// directory. Unlike Entry.Size (meaningless for dirs), this is always real —
// it's what a treemap needs to size each child's box.
type DuEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// DuListing is the result of sizing one directory's immediate children — one
// level of a treemap. The caller drills deeper by calling Du again on
// whichever child looks big, mirroring how List navigates one directory at a
// time rather than returning the whole subtree up front.
type DuListing struct {
	Path      string    `json:"path"`
	Parent    string    `json:"parent"`
	Entries   []DuEntry `json:"entries"`   // sorted by size, largest first
	Truncated bool      `json:"truncated"` // hit maxDuFiles or ctx's deadline before finishing every child
}

// Du sizes every immediate child of path, recursing into subdirectories to
// total their contents. Symlinks are reported at their own (tiny) lstat size
// and never followed — walking through one risks a cycle (a link back up the
// tree) or double-counting a subtree two links share. ctx bounds the whole
// call, so a directory too large to fully walk within the deadline reports
// Truncated rather than hanging the request.
func Du(ctx context.Context, path string) (DuListing, error) {
	if path == "" {
		path = "/"
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		return DuListing{}, &os.PathError{Op: "browse", Path: path, Err: os.ErrInvalid}
	}

	dirents, err := os.ReadDir(clean)
	if err != nil {
		return DuListing{}, err
	}

	l := DuListing{Path: clean, Parent: parentOf(clean)}
	visited := 0
	for _, de := range dirents {
		if ctx.Err() != nil {
			l.Truncated = true
			break
		}
		info, ierr := de.Info()
		if ierr != nil {
			continue // vanished between readdir and stat — drop it rather than fail the whole listing
		}
		full := filepath.Join(clean, de.Name())
		switch {
		case info.Mode()&os.ModeSymlink != 0:
			l.Entries = append(l.Entries, DuEntry{Name: de.Name(), Size: info.Size()})
		case de.IsDir():
			size, complete := dirSize(ctx, full, &visited)
			if !complete {
				l.Truncated = true
			}
			l.Entries = append(l.Entries, DuEntry{Name: de.Name(), IsDir: true, Size: size})
		default:
			visited++
			l.Entries = append(l.Entries, DuEntry{Name: de.Name(), Size: info.Size()})
		}
		if visited >= maxDuFiles {
			l.Truncated = true
			break
		}
	}

	sort.Slice(l.Entries, func(i, j int) bool { return l.Entries[i].Size > l.Entries[j].Size })
	return l, nil
}

// dirSize walks root recursively and returns its total size in bytes. visited
// is a counter shared across the whole Du call, so the maxDuFiles budget is
// spent across sibling directories rather than reset per-directory; complete
// is false if the walk stopped early on that counter or ctx's deadline.
func dirSize(ctx context.Context, root string, visited *int) (size int64, complete bool) {
	complete = true
	_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // permission-denied subtree etc — skip it, keep summing the rest
		}
		if ctx.Err() != nil {
			complete = false
			return filepath.SkipAll
		}
		if d.Type()&os.ModeSymlink != 0 || d.IsDir() {
			return nil // don't follow symlinks or count dirs themselves, just the files under them
		}
		info, ierr := d.Info()
		if ierr != nil {
			return nil
		}
		size += info.Size()
		*visited++
		if *visited >= maxDuFiles {
			complete = false
			return filepath.SkipAll
		}
		return nil
	})
	return size, complete
}
