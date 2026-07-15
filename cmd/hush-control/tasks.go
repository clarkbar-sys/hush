package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	hexec "github.com/clarkbar-sys/hush/internal/exec"
	"github.com/clarkbar-sys/hush/internal/store"
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
	Host      string `json:"host"`           // machine id (agent name or IP) the task runs on
	Cmd       string `json:"cmd"`            // shell command line, run via sh -c on that box
	User      string `json:"user,omitempty"` // optional: OS user to run as via sudo -u (must be on the agent's -run-as list); empty = the hush user
	CreatedAt string `json:"createdAt"`      // RFC3339 UTC
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

// taskStore holds saved Tasks in memory and persists them on every change. Like
// workflowStore it's a thin skin over the generic store.JSON — saved Tasks are
// additive and non-critical, so a missing or unreadable file starts empty rather
// than aborting the console — adding only the typed Update the handlers expect.
// Snapshot/Find/Add/Delete come straight from the embedded store.
type taskStore struct {
	*store.JSON[Task]
}

func newTaskStore(path string) *taskStore {
	return &taskStore{store.New(path, "tasks", func(t Task) string { return t.ID })}
}

// loadTasks reads tasks.json, tolerating a missing or corrupt file by starting
// empty so a broken tasks.json can't take the fleet map down with it.
func loadTasks(path string) []Task { return store.Load[Task](path, "tasks") }

// Update replaces a saved task's name, host, command, and run-as user, preserving
// its id and CreatedAt so it keeps its identity across edits. It reports whether a
// task with that id existed, so the handler can answer 404 for an unknown id.
func (s *taskStore) Update(id, name, host, cmd, user string) (Task, bool, error) {
	return s.JSON.Update(id, func(t *Task) {
		t.Name = name
		t.Host = host
		t.Cmd = cmd
		t.User = user
	})
}

// newTaskID derives a stable, readable id from the name plus a random suffix, so
// two tasks named the same never collide. It reuses newWorkflowID's scheme —
// slug + hex suffix — so ids read the same across both stores.
func newTaskID(name string) string { return newWorkflowID(name) }

// checkTask validates a saved task's fields, returning the trimmed name, host,
// command, and run-as user on success. resolve reports whether the host is
// actually in the fleet, so we reject a task pinned to a machine hush doesn't
// know before it's ever run — the same gate checkWorkflow applies to every step.
// Both create and update flow through here so a task's rules can't drift between
// the two. An empty user is fine (runs as the hush user); a non-empty one must be
// a syntactically valid username — the agent's -run-as allowlist is the real
// gate, but we reject obvious junk here so it never lands in tasks.json.
func checkTask(name, host, cmd, user string, resolve func(string) bool) (string, string, string, string, error) {
	name = strings.TrimSpace(name)
	host = strings.TrimSpace(host)
	cmd = strings.TrimSpace(cmd)
	user = strings.TrimSpace(user)
	if name == "" {
		return "", "", "", "", errors.New("name is required")
	}
	if host == "" {
		return "", "", "", "", errors.New("pick a machine")
	}
	if cmd == "" {
		return "", "", "", "", errors.New("command is required")
	}
	if len(cmd) > maxCmdLen {
		return "", "", "", "", errors.New("command is too long")
	}
	if user != "" && !hexec.ValidUserName(user) {
		return "", "", "", "", errors.New("run-as user is not a valid username")
	}
	if !resolve(host) {
		return "", "", "", "", fmt.Errorf("%s is not in the fleet", host)
	}
	return name, host, cmd, user, nil
}

// validateTask checks a create request and, on success, returns a stored Task
// with its id and timestamp filled in.
func validateTask(name, host, cmd, user string, resolve func(string) bool) (Task, error) {
	name, host, cmd, user, err := checkTask(name, host, cmd, user, resolve)
	if err != nil {
		return Task{}, err
	}
	return Task{
		ID:        newTaskID(name),
		Name:      name,
		Host:      host,
		Cmd:       cmd,
		User:      user,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}
