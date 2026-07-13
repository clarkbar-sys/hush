package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	hexec "github.com/clarkbar-sys/hush/internal/exec"
)

// Step is one command in a Workflow: a shell line run on a named machine. It is
// deliberately the same primitive as a Task — the agent's /exec runs it as the
// unprivileged hush user, unjailed — so a Workflow adds sequencing and reuse on
// top of a capability that already exists, nothing more.
type Step struct {
	Host string `json:"host"` // machine id (agent name or IP) the step runs on
	Cmd  string `json:"cmd"`  // shell command line, run via sh -c on that box
}

// Workflow is a saved, reusable blueprint: the design's "wired sequence
// (cd X → git pull → restart) — reusable, stampable". It's an ordered list of
// Steps run across the fleet, persisted so the console can re-run it later. A
// run is fail-fast — the first step to exit non-zero stops the sequence, the
// same contract `set -e` gives a shell script.
type Workflow struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Steps     []Step `json:"steps"`
	CreatedAt string `json:"createdAt"` // RFC3339 UTC
}

const (
	// maxSteps caps a blueprint so a single workflow can't grow unbounded.
	maxSteps = 50
	// maxCmdLen caps one step's command, well under the agent's 64 KiB body
	// limit while staying generous for a real one-liner.
	maxCmdLen = 8192
	// stepTimeoutSec bounds each step, mirroring the Task run view's default.
	stepTimeoutSec = 300
)

// workflowsPath places workflows.json next to the fleet config, so it lands in
// the same already-writable directory the systemd unit grants (ReadWritePaths=
// /etc/hush) — no extra install wiring needed.
func workflowsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), "workflows.json")
}

// workflowStore holds saved blueprints in memory and persists them to disk on
// every change, mirroring agentStore. Blueprints are additive and non-critical:
// a missing or unreadable file starts empty rather than aborting the console,
// since a broken workflows.json must not take the fleet map down with it.
type workflowStore struct {
	mu   sync.RWMutex
	path string
	wfs  []Workflow
}

func newWorkflowStore(path string) *workflowStore {
	return &workflowStore{path: path, wfs: loadWorkflows(path)}
}

func loadWorkflows(path string) []Workflow {
	b, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("read %s: %v; starting with no workflows", path, err)
		}
		return []Workflow{}
	}
	var wfs []Workflow
	if err := json.Unmarshal(b, &wfs); err != nil {
		log.Printf("parse %s: %v; starting with no workflows", path, err)
		return []Workflow{}
	}
	return wfs
}

// Snapshot returns a copy of the saved workflows, safe to read without the lock.
func (s *workflowStore) Snapshot() []Workflow {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Workflow, len(s.wfs))
	copy(out, s.wfs)
	return out
}

func (s *workflowStore) find(id string) (Workflow, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, w := range s.wfs {
		if w.ID == id {
			return w, true
		}
	}
	return Workflow{}, false
}

// Add persists a new blueprint and returns the stored copy (with its generated
// id and timestamp filled in).
func (s *workflowStore) Add(wf Workflow) (Workflow, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	updated := append(s.wfs, wf)
	if err := saveWorkflows(s.path, updated); err != nil {
		return Workflow{}, fmt.Errorf("save %s: %w — check that its directory is writable (see the -config flag and the systemd unit's ReadWritePaths)", s.path, err)
	}
	s.wfs = updated
	return wf, nil
}

// Update replaces an existing blueprint's name and steps in place, preserving
// its id and CreatedAt so a saved workflow keeps its identity across edits. It
// reports whether a workflow with that id existed, so the handler can answer
// 404 for an unknown id, and returns the stored copy on success.
func (s *workflowStore) Update(id, name string, steps []Step) (Workflow, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i, w := range s.wfs {
		if w.ID != id {
			continue
		}
		updated := make([]Workflow, len(s.wfs))
		copy(updated, s.wfs)
		updated[i].Name = name
		updated[i].Steps = steps
		if err := saveWorkflows(s.path, updated); err != nil {
			return Workflow{}, true, fmt.Errorf("save %s: %w", s.path, err)
		}
		s.wfs = updated
		return updated[i], true, nil
	}
	return Workflow{}, false, nil
}

// Delete removes a blueprint by id, persisting the result. It reports whether
// anything was removed so the handler can answer 404 for an unknown id.
func (s *workflowStore) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	kept := make([]Workflow, 0, len(s.wfs))
	for _, w := range s.wfs {
		if w.ID != id {
			kept = append(kept, w)
		}
	}
	if len(kept) == len(s.wfs) {
		return false, nil
	}
	if err := saveWorkflows(s.path, kept); err != nil {
		return false, fmt.Errorf("save %s: %w", s.path, err)
	}
	s.wfs = kept
	return true, nil
}

// saveWorkflows writes the blueprints atomically (temp file then rename), the
// same crash-safe dance saveAgents uses for fleet.json.
func saveWorkflows(path string, wfs []Workflow) error {
	b, err := json.MarshalIndent(wfs, "", "  ")
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

// newWorkflowID derives a stable, readable id from the name plus a random
// suffix, so two workflows named the same never collide.
func newWorkflowID(name string) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	suffix := hex.EncodeToString(b[:])
	slug := slugify(name)
	if slug == "" {
		return suffix
	}
	return slug + "-" + suffix
}

// slugify lowercases a name and keeps only [a-z0-9-], collapsing runs of other
// characters to a single dash — enough to make an id legible in a URL and logs.
func slugify(name string) string {
	var sb strings.Builder
	dash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			sb.WriteRune(r)
			dash = false
		default:
			if !dash && sb.Len() > 0 {
				sb.WriteByte('-')
				dash = true
			}
		}
	}
	return strings.Trim(sb.String(), "-")
}

// checkWorkflow validates a name and its steps, returning the trimmed name and
// cleaned steps on success. resolve reports whether a step's host is actually in
// the fleet, so we reject a blueprint that points at a machine hush doesn't know
// before it's ever run. Both create and update flow through here so a workflow's
// rules can't drift between the two.
func checkWorkflow(name string, steps []Step, resolve func(string) bool) (string, []Step, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", nil, errors.New("name is required")
	}
	if len(steps) == 0 {
		return "", nil, errors.New("a workflow needs at least one step")
	}
	if len(steps) > maxSteps {
		return "", nil, fmt.Errorf("a workflow can have at most %d steps", maxSteps)
	}
	clean := make([]Step, 0, len(steps))
	for i, st := range steps {
		host := strings.TrimSpace(st.Host)
		cmd := strings.TrimSpace(st.Cmd)
		if host == "" {
			return "", nil, fmt.Errorf("step %d: pick a machine", i+1)
		}
		if cmd == "" {
			return "", nil, fmt.Errorf("step %d: command is required", i+1)
		}
		if len(cmd) > maxCmdLen {
			return "", nil, fmt.Errorf("step %d: command is too long", i+1)
		}
		if !resolve(host) {
			return "", nil, fmt.Errorf("step %d: %s is not in the fleet", i+1, host)
		}
		clean = append(clean, Step{Host: host, Cmd: cmd})
	}
	return name, clean, nil
}

// validateWorkflow checks a create request and, on success, returns a stored
// Workflow with its id and timestamp filled in.
func validateWorkflow(name string, steps []Step, resolve func(string) bool) (Workflow, error) {
	name, clean, err := checkWorkflow(name, steps, resolve)
	if err != nil {
		return Workflow{}, err
	}
	return Workflow{
		ID:        newWorkflowID(name),
		Name:      name,
		Steps:     clean,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// workflowEvent is one frame in a run's SSE stream. It layers a step index over
// the Task exec vocabulary so the console can group each command's output under
// its step, and adds a terminal "done" frame the single-command Task stream has
// no need for. Zero-valued fields are omitted (omitempty), so the frontend
// reads each field only for the kinds that set it — the same convention the
// exec.Event stream uses (a missing exit code means 0).
type workflowEvent struct {
	Kind       string `json:"kind"`                 // step | out | stepExit | error | done
	Index      int    `json:"index,omitempty"`      // 0-based step index (step, out, stepExit, error)
	Count      int    `json:"count,omitempty"`      // total steps (kind=step)
	Host       string `json:"host,omitempty"`       // step's machine (kind=step)
	Cmd        string `json:"cmd,omitempty"`        // step's command (kind=step)
	Stream     string `json:"stream,omitempty"`     // stdout | stderr (kind=out)
	Data       string `json:"data,omitempty"`       // output chunk or error message
	Code       int    `json:"code,omitempty"`       // step exit code (kind=stepExit)
	Signal     string `json:"signal,omitempty"`     // kind=stepExit: timeout | canceled | signal
	MS         int64  `json:"ms,omitempty"`         // step wall-clock ms (kind=stepExit)
	Truncated  bool   `json:"truncated,omitempty"`  // step output cap hit (kind=stepExit)
	OK         bool   `json:"ok,omitempty"`         // kind=done: the whole run succeeded
	Ran        int    `json:"ran,omitempty"`        // kind=done: steps that ran
	FailedStep *int   `json:"failedStep,omitempty"` // kind=done: index of the failing step
}

// runWorkflow executes a blueprint's steps in order, streaming a combined SSE
// log of every step's output. It reuses each agent's /exec exactly as a Task
// does — one HTTP call per step — and stops at the first step that exits
// non-zero, errors, or ends without a status, emitting a terminal "done" frame
// either way. resolve maps a step's host back to its agent; passing it in keeps
// the executor testable without a live fleet.
func runWorkflow(ctx context.Context, w http.ResponseWriter, client *http.Client, resolve func(string) (Agent, bool), wf Workflow) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	emit := func(ev workflowEvent) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}

	for i, step := range wf.Steps {
		if ctx.Err() != nil {
			return // client hung up mid-run
		}
		emit(workflowEvent{Kind: "step", Index: i, Count: len(wf.Steps), Host: step.Host, Cmd: step.Cmd})
		a, found := resolve(step.Host)
		if !found {
			emit(workflowEvent{Kind: "error", Index: i, Data: step.Host + " is not in the fleet"})
			fs := i
			emit(workflowEvent{Kind: "done", FailedStep: &fs})
			return
		}
		if !runStep(ctx, emit, client, a, step, i) {
			if ctx.Err() != nil {
				return
			}
			fs := i
			emit(workflowEvent{Kind: "done", FailedStep: &fs})
			return
		}
	}
	emit(workflowEvent{Kind: "done", OK: true, Ran: len(wf.Steps)})
}

// runStep runs one step against its agent's /exec, forwarding the agent's SSE
// frames as step-tagged workflowEvents. It reports whether the step succeeded:
// a non-zero exit, a signal, an error frame, an unreachable agent, or a stream
// that ends without an exit all count as failure, so runWorkflow stops rather
// than marching a broken sequence forward.
func runStep(ctx context.Context, emit func(workflowEvent), client *http.Client, a Agent, step Step, index int) bool {
	payload, _ := json.Marshal(struct {
		Cmd        string `json:"cmd"`
		TimeoutSec int    `json:"timeoutSec"`
	}{Cmd: step.Cmd, TimeoutSec: stepTimeoutSec})

	u := strings.TrimRight(a.Addr, "/") + "/exec"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payload))
	if err != nil {
		emit(workflowEvent{Kind: "error", Index: index, Data: "bad upstream request"})
		return false
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		if ctx.Err() != nil {
			return false // canceled — runWorkflow returns without a done frame
		}
		emit(workflowEvent{Kind: "error", Index: index, Data: "agent " + a.Name + " unreachable"})
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := strings.TrimSpace(string(msg))
		if detail == "" {
			detail = fmt.Sprintf("agent returned %d", resp.StatusCode)
		}
		emit(workflowEvent{Kind: "error", Index: index, Data: detail})
		return false
	}

	sc := bufio.NewScanner(resp.Body)
	// One event rides one data line; the agent chunks output at 4 KiB, but give
	// the scanner room so a big line can't split a frame.
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	gotExit, stepOK := false, false
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		var ev hexec.Event
		if err := json.Unmarshal([]byte(strings.TrimSpace(line[len("data:"):])), &ev); err != nil {
			continue
		}
		switch ev.Kind {
		case "out":
			emit(workflowEvent{Kind: "out", Index: index, Stream: ev.Stream, Data: ev.Data})
		case "error":
			emit(workflowEvent{Kind: "error", Index: index, Data: ev.Data})
		case "exit":
			gotExit = true
			stepOK = ev.Code == 0 && ev.Signal == ""
			emit(workflowEvent{Kind: "stepExit", Index: index, Code: ev.Code, Signal: ev.Signal, MS: ev.MS, Truncated: ev.Truncated})
		}
	}
	if ctx.Err() != nil {
		return false
	}
	if !gotExit {
		emit(workflowEvent{Kind: "error", Index: index, Data: "step ended without an exit status"})
		return false
	}
	return stepOK
}
