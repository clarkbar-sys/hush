// Package store provides a small durable-list primitive: an in-memory slice of
// T mirrored to a JSON file, rewritten wholesale and atomically on every change.
// It factors out the persistence dance the fleet's constructs repeat — a
// tolerant load, a snapshot taken under a lock, and a crash-safe temp-then-rename
// write. The agent persists its restic backup definitions (backups.json) through
// it, and hush-control leans on the same atomic write for its fleet config, which
// is why this lives in internal/ rather than beside either binary.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"sync"
)

// JSON is a concurrency-safe list of T mirrored to a JSON file. It keys items by
// a caller-supplied id so Find/Update/Delete can address one item; the file is
// the whole slice, marshaled indented and replaced atomically on every change.
// A JSON store treats its file as additive and non-critical — a missing or
// corrupt file loads as empty (see Load) rather than failing — so it suits
// constructs the console can rebuild, not load-bearing config that should fail
// loudly. Every mutating method advances the in-memory slice only after the
// write succeeds, so a failed save leaves the store exactly as it was.
type JSON[T any] struct {
	mu    sync.RWMutex
	path  string
	idOf  func(T) string
	items []T
}

// New loads path into a store, tolerating a missing or unreadable file by
// starting empty (see Load). noun names the collection in Load's log lines; idOf
// extracts an item's stable id for Find/Update/Delete.
func New[T any](path, noun string, idOf func(T) string) *JSON[T] {
	return &JSON[T]{path: path, idOf: idOf, items: Load[T](path, noun)}
}

// Snapshot returns a copy of the stored items, safe to read without the lock.
func (s *JSON[T]) Snapshot() []T {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]T, len(s.items))
	copy(out, s.items)
	return out
}

// Find returns the item with the given id, reporting whether it was present.
func (s *JSON[T]) Find(id string) (T, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, it := range s.items {
		if s.idOf(it) == id {
			return it, true
		}
	}
	var zero T
	return zero, false
}

// Add appends an item and persists the result, returning the stored copy.
func (s *JSON[T]) Add(item T) (T, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := append(append([]T(nil), s.items...), item)
	if err := Save(s.path, updated); err != nil {
		var zero T
		return zero, fmt.Errorf("save %s: %w — check that its directory is writable (see the -config flag and the systemd unit's ReadWritePaths)", s.path, err)
	}
	s.items = updated
	return item, nil
}

// Update finds the item with id, applies mutate to a copy of it, and persists
// the result — preserving every field mutate leaves untouched, so an item keeps
// its id and creation time across edits. It reports whether the id existed and,
// on success, returns the updated copy. mutate must not change the item's id.
func (s *JSON[T]) Update(id string, mutate func(*T)) (T, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, it := range s.items {
		if s.idOf(it) != id {
			continue
		}
		updated := append([]T(nil), s.items...)
		mutate(&updated[i])
		if err := Save(s.path, updated); err != nil {
			var zero T
			return zero, true, fmt.Errorf("save %s: %w", s.path, err)
		}
		s.items = updated
		return updated[i], true, nil
	}
	var zero T
	return zero, false, nil
}

// Delete removes the item with id and persists the result, reporting whether
// anything was removed so a handler can answer 404 for an unknown id.
func (s *JSON[T]) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]T, 0, len(s.items))
	for _, it := range s.items {
		if s.idOf(it) != id {
			kept = append(kept, it)
		}
	}
	if len(kept) == len(s.items) {
		return false, nil
	}
	if err := Save(s.path, kept); err != nil {
		return false, fmt.Errorf("save %s: %w", s.path, err)
	}
	s.items = kept
	return true, nil
}

// Load reads path into a slice, tolerating a missing or corrupt file by
// returning an empty (non-nil) slice. A read error other than "not found" and
// any JSON parse error are logged — naming the collection with noun — but never
// fatal, so a broken file starts the store empty instead of taking the process
// down. Config that must fail loudly on a bad file should not use this.
func Load[T any](path, noun string) []T {
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("read %s: %v; starting with no %s", path, err, noun)
		}
		return []T{}
	}
	var items []T
	if err := json.Unmarshal(b, &items); err != nil {
		log.Printf("parse %s: %v; starting with no %s", path, err, noun)
		return []T{}
	}
	return items
}

// Save writes items to path as indented JSON, atomically: it writes a sibling
// temp file and renames it over the target, so a crash mid-write can't leave a
// half-written file. Exported so stores with their own load or validation rules
// (e.g. the fleet config, which fails loudly on a bad parse) share the same
// crash-safe write without adopting the tolerant Load.
func Save[T any](path string, items []T) error {
	b, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
