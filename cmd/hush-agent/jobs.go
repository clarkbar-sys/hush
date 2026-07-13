package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	hexec "github.com/clarkbar-sys/hush/internal/exec"
	"github.com/clarkbar-sys/hush/internal/store"
	"github.com/robfig/cron/v3"
)

// Job is the design's "cron / timer — fires on a schedule": a saved command the
// agent runs on its own box, unattended, on a cron schedule. It is the same
// primitive /exec runs — a shell line executed as the unprivileged hush user,
// unjailed — with a schedule bolted on. The definition is persisted; when it
// last fired and how that went is runtime state (jobStatus), tracked in memory
// so a per-minute tick doesn't rewrite the file.
type Job struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Schedule  string `json:"schedule"`  // 5-field cron spec, e.g. "*/15 * * * *" (or @daily)
	Cmd       string `json:"cmd"`       // shell command line, run via sh -c as the hush user
	CreatedAt string `json:"createdAt"` // RFC3339 UTC
}

// jobStatus is a Job's volatile run history since the agent started. It is not
// persisted — a restart resets the counters, which is honest: the scheduler
// only knows about fires it actually performed.
type jobStatus struct {
	Running   bool   `json:"running"`             // a fire is in flight right now
	Runs      int    `json:"runs"`                // fires since the agent started
	LastRun   string `json:"lastRun,omitempty"`   // RFC3339 UTC when the last fire began
	LastMS    int64  `json:"lastMs,omitempty"`    // last fire's wall-clock duration
	LastCode  int    `json:"lastCode"`            // last fire's exit code (-1 if signalled or failed to start)
	LastError string `json:"lastError,omitempty"` // last fire's signal/timeout/error, if any
}

// jobView is what GET /jobs returns: a Job's definition plus its runtime status.
type jobView struct {
	Job
	Status jobStatus `json:"status"`
}

// maxJobCmdLen caps a job's command, mirroring the control-side workflow/task
// limit — generous for a real one-liner, well under the agent's body limit.
const maxJobCmdLen = 8192

// jobStore persists Job definitions through the shared store.JSON primitive, the
// same one hush-control uses for Workflows and Tasks. Keyed by id.
type jobStore = store.JSON[Job]

func newJobStore(path string) *jobStore {
	return store.New(path, "jobs", func(j Job) string { return j.ID })
}

// scheduler owns the persisted jobs and the cron engine that fires them,
// keeping the two in lockstep: every Job in the store has a live cron entry, and
// removing a Job removes its entry. Fires run the command as the hush user via
// the same hexec path /exec uses. The zero timeout lets hexec apply its default.
type scheduler struct {
	mu      sync.Mutex
	store   *jobStore
	cron    *cron.Cron
	entries map[string]cron.EntryID // job id -> cron entry, for removal
	status  map[string]*jobStatus   // job id -> runtime status
	timeout time.Duration
}

// newScheduler loads persisted jobs and registers each with cron. A job whose
// schedule no longer parses is skipped with a warning rather than aborting the
// agent — a broken jobs.json must not keep the box from booting its agent. Call
// Start to begin firing and Stop on shutdown.
func newScheduler(path string) *scheduler {
	s := &scheduler{
		store:   newJobStore(path),
		cron:    cron.New(),
		entries: make(map[string]cron.EntryID),
		status:  make(map[string]*jobStatus),
	}
	for _, j := range s.store.Snapshot() {
		if err := s.register(j); err != nil {
			log.Printf("hush-agent: skipping job %s (%q): %v", j.ID, j.Name, err)
		}
	}
	return s
}

func (s *scheduler) Start() { s.cron.Start() }

// Stop halts the cron engine. It returns once any in-flight fire's goroutine has
// been signalled to stop; hexec kills a run's process group when its context is
// cancelled, and cron.Stop waits for running jobs to return.
func (s *scheduler) Stop() { s.cron.Stop() }

// register wires one job into cron and seeds its status. The caller holds s.mu
// (or, at construction, has exclusive access). The closure captures j by value,
// so each entry fires its own command.
func (s *scheduler) register(j Job) error {
	id, err := s.cron.AddFunc(j.Schedule, func() { s.run(j) })
	if err != nil {
		return err
	}
	s.entries[j.ID] = id
	if _, ok := s.status[j.ID]; !ok {
		s.status[j.ID] = &jobStatus{}
	}
	return nil
}

// run fires a job's command as the hush user and records the outcome. It streams
// nothing — a scheduled run has no client — so it keeps only the terminal
// exit/error event to update the job's status.
func (s *scheduler) run(j Job) {
	s.mark(j.ID, func(st *jobStatus) { st.Running = true })
	start := time.Now()
	var last hexec.Event
	hexec.Run(context.Background(), hexec.Spec{Cmd: j.Cmd, Timeout: s.timeout}, func(ev hexec.Event) {
		if ev.Kind == "exit" || ev.Kind == "error" {
			last = ev
		}
	})
	s.mark(j.ID, func(st *jobStatus) {
		st.Running = false
		st.Runs++
		st.LastRun = start.UTC().Format(time.RFC3339)
		st.LastMS = last.MS
		st.LastCode = last.Code
		st.LastError = last.Signal
		if last.Kind == "error" {
			// The command never produced an exit event (bad shell, spawn failure).
			st.LastCode = -1
			st.LastError = last.Data
		}
	})
}

// mark applies f to a job's status under the lock, creating it if this is the
// first time the id is seen.
func (s *scheduler) mark(id string, f func(*jobStatus)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, ok := s.status[id]
	if !ok {
		st = &jobStatus{}
		s.status[id] = st
	}
	f(st)
}

// Add persists a new job and schedules it. If cron rejects the schedule after
// the write, the persisted job is rolled back so the store never holds a job
// that will never fire.
func (s *scheduler) Add(j Job) (Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	saved, err := s.store.Add(j)
	if err != nil {
		return Job{}, err
	}
	if err := s.register(saved); err != nil {
		_, _ = s.store.Delete(saved.ID)
		return Job{}, fmt.Errorf("schedule %q: %w", j.Schedule, err)
	}
	return saved, nil
}

// Delete removes a job's definition and its cron entry, reporting whether the id
// existed so the handler can answer 404.
func (s *scheduler) Delete(id string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	removed, err := s.store.Delete(id)
	if err != nil {
		return false, err
	}
	if !removed {
		return false, nil
	}
	if eid, ok := s.entries[id]; ok {
		s.cron.Remove(eid)
		delete(s.entries, id)
	}
	delete(s.status, id)
	return true, nil
}

// List returns every job with its current runtime status, newest-first-agnostic
// (store order, i.e. creation order).
func (s *scheduler) List() []jobView {
	defs := s.store.Snapshot()
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]jobView, 0, len(defs))
	for _, j := range defs {
		st := jobStatus{}
		if p, ok := s.status[j.ID]; ok {
			st = *p
		}
		out = append(out, jobView{Job: j, Status: st})
	}
	return out
}

// validateJob checks a create request and, on success, returns a stored Job with
// its id and timestamp filled in. The schedule is parsed with the same standard
// (5-field) parser cron uses, so an invalid spec is rejected here — before it's
// ever persisted — with a message that shows the expected shape.
func validateJob(name, schedule, cmd string) (Job, error) {
	name = strings.TrimSpace(name)
	schedule = strings.TrimSpace(schedule)
	cmd = strings.TrimSpace(cmd)
	if name == "" {
		return Job{}, errors.New("name is required")
	}
	if schedule == "" {
		return Job{}, errors.New("schedule is required")
	}
	if cmd == "" {
		return Job{}, errors.New("command is required")
	}
	if len(cmd) > maxJobCmdLen {
		return Job{}, errors.New("command is too long")
	}
	if _, err := cron.ParseStandard(schedule); err != nil {
		return Job{}, fmt.Errorf(`invalid schedule (want a 5-field cron spec like "*/15 * * * *", or @daily): %w`, err)
	}
	return Job{
		ID:        newJobID(),
		Name:      name,
		Schedule:  schedule,
		Cmd:       cmd,
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
	}, nil
}

// resolveStateDir picks where persisted agent state lives. An explicit -state-dir
// wins; otherwise systemd's $STATE_DIRECTORY (populated by StateDirectory=hush,
// which also creates it 0700 hush:hush and keeps it writable under
// ProtectSystem=strict) is used; failing both — e.g. a manual run outside
// systemd — it falls back to /var/lib/hush. $STATE_DIRECTORY may list several
// paths colon-separated; the first is ours.
func resolveStateDir(flagDir string) string {
	if flagDir != "" {
		return flagDir
	}
	if sd := os.Getenv("STATE_DIRECTORY"); sd != "" {
		if i := strings.IndexByte(sd, ':'); i >= 0 {
			return sd[:i]
		}
		return sd
	}
	return "/var/lib/hush"
}

// newJobID returns a short random hex id. Unlike the control-side constructs it
// doesn't slugify the name — there's no jobs UI addressing these by a pretty id
// yet — so a plain random id keeps the agent free of that helper.
func newJobID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// handleJobs serves the collection: GET lists jobs with status, POST creates one
// from a {name, schedule, cmd} body. It is only reached when the agent was
// started with jobs enabled (main gates the route).
func (s *scheduler) handleJobs(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(s.List()); err != nil {
			log.Printf("encode jobs: %v", err)
		}
	case http.MethodPost:
		var req struct {
			Name     string `json:"name"`
			Schedule string `json:"schedule"`
			Cmd      string `json:"cmd"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		job, err := validateJob(req.Name, req.Schedule, req.Cmd)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		saved, err := s.Add(job)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(saved); err != nil {
			log.Printf("encode job: %v", err)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleJob serves one job by id: DELETE removes it. A missing id is a 404 so
// the caller can tell "gone" from "never existed".
func (s *scheduler) handleJob(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	removed, err := s.Delete(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !removed {
		http.Error(w, "no such job", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
