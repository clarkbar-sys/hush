package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Task is a saved, reusable one-shot run: the write half of the browse model —
// "a one-shot run of a program" — but graduated from ephemeral to a named,
// re-runnable building block. It pins a single {host, cmd}, the same primitive a
// Workflow Step carries, so a saved Task is exactly one command you've decided to
// trust and keep. Workflows compose these: a step is a Task you've dropped into a
// sequence. Persisted beside workflows.json so it rides the same writable dir.
type Task struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Host      string `json:"host"`      // machine id (agent name or IP) the task runs on
	Cmd       string `json:"cmd"`       // shell command line, run via sh -c on that box
	CreatedAt string `json:"createdAt"` // RFC3339 UTC
}

// taskTimeoutSec bounds a saved Task's run, mirroring stepTimeoutSec and the
// Task run view's default so a saved run behaves exactly like an ad-hoc one.
const taskTimeoutSec = stepTimeoutSec

// tasksPath places tasks.json next to the fleet config and workflows.json, in
// the same already-writable directory the systemd unit grants — no extra install
// wiring, the same siting workflowsPath uses.
func tasksPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "tasks.json")
}

// taskStore holds saved Tasks in memory and persists them on every change,
// mirroring workflowStore. Like blueprints, saved Tasks are additive and
// non-critical: a missing or unreadable file starts empty rather than aborting
// the console, so a broken tasks.json can't take the fleet map down with it.
type taskStore struct {
	mu    sync.RWMutex
	path  string
	tasks []Task
}

func newTaskStore(path string) *taskStore {
	return &taskStore{path: path, tasks: loadTasks(path)}
}

func loadTasks(path string) []Task {
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("read %s: %v; starting with no tasks", path, err)
		}
		return []Task{}
	}
	var tasks []Task
	if err := json.Unmarshal(b, &tasks); err != nil {
		log.Printf("parse %s: %v; starting with no tasks", path, err)
		return []Task{}
	}
	return tasks
}

// Snapshot returns a copy of the saved tasks, safe to read without the lock.
func (s *taskStore) Snapshot() []Task {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Task, len(s.tasks))
	copy(out, s.tasks)
	return out
}

func (s *taskStore) find(id string) (Task, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, t := range s.tasks {
		if t.ID == id {
			return t, true
		}
	}
	return Task{}, false
}

// Add persists a new saved task and returns the stored copy (with its generated
// id and timestamp already filled in by validateTask).
func (s *taskStore) Add(t Task) (Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := append(s.tasks, t)
	if err := saveTasks(s.path, updated); err != nil {
		return Task{}, fmt.Errorf("save %s: %w — check that its directory is writable (see the -config flag and the systemd unit's ReadWritePaths)", s.path, err)
	}
	s.tasks = updated
	return t, nil
}

// Update replaces a saved task's name, host, and command in place, preserving its
// id and CreatedAt so it keeps its identity across edits. It reports whether a
// task with that id existed, so the handler can answer 404 for an unknown id.
func (s *taskStore) Update(id, name, host, cmd string) (Task, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, t := range s.tasks {
		if t.ID != id {
			continue
		}
		updated := make([]Task, len(s.tasks))
		copy(updated, s.tasks)
		updated[i].Name = name
		updated[i].Host = host
		updated[i].Cmd = cmd
		if err := saveTasks(s.path, updated); err != nil {
			return Task{}, true, fmt.Errorf("save %s: %w", s.path, err)
		}
		s.tasks = updated
		return updated[i], true, nil
	}
	return Task{}, false, nil
}

// Delete removes a saved task by id, persisting the result. It reports whether
// anything was removed so the handler can answer 404 for an unknown id.
func (s *taskStore) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]Task, 0, len(s.tasks))
	for _, t := range s.tasks {
		if t.ID != id {
			kept = append(kept, t)
		}
	}
	if len(kept) == len(s.tasks) {
		return false, nil
	}
	if err := saveTasks(s.path, kept); err != nil {
		return false, fmt.Errorf("save %s: %w", s.path, err)
	}
	s.tasks = kept
	return true, nil
}

// saveTasks writes the saved tasks atomically (temp file then rename), the same
// crash-safe dance saveWorkflows and saveAgents use.
func saveTasks(path string, tasks []Task) error {
	b, err := json.MarshalIndent(tasks, "", "  ")
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

// newTaskID derives a stable, readable id from the name plus a random suffix, so
// two tasks named the same never collide. It reuses newWorkflowID's scheme —
// slug + hex suffix — so ids read the same across both stores.
func newTaskID(name string) string { return newWorkflowID(name) }

// checkTask validates a saved task's fields, returning the trimmed name, host,
// and command on success. resolve reports whether the host is actually in the
// fleet, so we reject a task pinned to a machine hush doesn't know before it's
// ever run — the same gate checkWorkflow applies to every step. Both create and
// update flow through here so a task's rules can't drift between the two.
func checkTask(name, host, cmd string, resolve func(string) bool) (string, string, string, error) {
	name = strings.TrimSpace(name)
	host = strings.TrimSpace(host)
	cmd = strings.TrimSpace(cmd)
	if name == "" {
		return "", "", "", errors.New("name is required")
	}
	if host == "" {
		return "", "", "", errors.New("pick a machine")
	}
	if cmd == "" {
		return "", "", "", errors.New("command is required")
	}
	if len(cmd) > maxCmdLen {
		return "", "", "", errors.New("command is too long")
	}
	if !resolve(host) {
		return "", "", "", fmt.Errorf("%s is not in the fleet", host)
	}
	return name, host, cmd, nil
}

// validateTask checks a create request and, on success, returns a stored Task
// with its id and timestamp filled in.
func validateTask(name, host, cmd string, resolve func(string) bool) (Task, error) {
	name, host, cmd, err := checkTask(name, host, cmd, resolve)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID:        newTaskID(name),
		Name:      name,
		Host:      host,
		Cmd:       cmd,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
