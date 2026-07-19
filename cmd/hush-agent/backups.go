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
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/clarkbar-sys/hush/internal/restic"
	"github.com/clarkbar-sys/hush/internal/store"
	"github.com/robfig/cron/v3"
)

// Backup is the design's "a scheduled restic run that hauls a Machine into a
// Store, dedup'd": a saved set of paths this box sends into a restic repository,
// encrypted and dedup'd. It's a typed restic invocation, not a shell line.
//
// The Password is the repo's encryption key. It is persisted here, in the
// agent's 0700 state dir — the same box that holds the data
// holds the key — and is deliberately the one field the API never hands back
// (see backupView): hush-control and the phone drive backups without the key
// ever passing through them, so the control plane can never become the thing
// that leaks it.
type Backup struct {
	ID            string   `json:"id"`
	Name          string   `json:"name"`
	Repo          string   `json:"repo"`     // restic backend, e.g. rest:http://nas:8000/homelab
	Password      string   `json:"password"` // repo encryption key; stored 0700, never returned by GET
	Paths         []string `json:"paths"`
	Excludes      []string `json:"excludes,omitempty"`
	OneFileSystem bool     `json:"oneFileSystem,omitempty"` // --one-file-system: whole-machine mode
	Schedule      string   `json:"schedule,omitempty"`      // 5-field cron / @macro; empty = manual-only
	CreatedAt     string   `json:"createdAt"`               // RFC3339 UTC
}

// backupStatus is a Backup's volatile run history since the agent started —
// like jobStatus, not persisted, because a restart honestly forgets runs it
// never performed.
type backupStatus struct {
	Running      bool   `json:"running"`
	Runs         int    `json:"runs"`
	LastRun      string `json:"lastRun,omitempty"`
	LastMS       int64  `json:"lastMs,omitempty"`
	LastCode     int    `json:"lastCode"`
	LastError    string `json:"lastError,omitempty"`
	LastSnapshot string `json:"lastSnapshot,omitempty"` // short id of the snapshot the last run wrote
}

// backupView is what GET /backups returns: a definition without its password,
// plus runtime status. Password is omitted by construction — it is never copied
// into the view — so the key can't leak through the read path.
type backupView struct {
	ID            string       `json:"id"`
	Name          string       `json:"name"`
	Repo          string       `json:"repo"`
	Paths         []string     `json:"paths"`
	Excludes      []string     `json:"excludes,omitempty"`
	OneFileSystem bool         `json:"oneFileSystem,omitempty"`
	Schedule      string       `json:"schedule,omitempty"`
	NextRun       string       `json:"nextRun,omitempty"` // RFC3339 UTC of the next scheduled fire, if scheduled
	CreatedAt     string       `json:"createdAt"`
	Status        backupStatus `json:"status"`
}

const (
	// maxBackupPaths caps how many roots one backup names, and maxBackupField the
	// length of any single path/exclude/repo — generous for real use, a guard
	// against a pathological request filling the store.
	maxBackupPaths = 256
	maxBackupField = 4096
	// createDeadline bounds the network work a create does (init + a snapshots
	// probe against the rest-server) so a wrong address fails fast instead of
	// hanging the request.
	createDeadline = 45 * time.Second
	// snapshotsDeadline bounds a snapshots listing.
	snapshotsDeadline = 30 * time.Second
)

type backupStore = store.JSON[Backup]

func newBackupStore(path string) *backupStore {
	return store.New(path, "backups", func(b Backup) string { return b.ID })
}

// backupManager owns the persisted backup definitions, their runtime status, and
// a robfig/cron engine that fires the scheduled ones unattended, on the box. A
// backup with an empty Schedule is manual-only; it just never gets a
// cron entry. running guards a definition against two overlapping runs (a
// scheduled fire while a manual run is in flight, say), which would race on the
// repo lock. A run streams to the console when triggered there, and to nowhere
// when the clock fires it — the outcome lands in status either way.
type backupManager struct {
	mu      sync.Mutex
	store   *backupStore
	status  map[string]*backupStatus
	running map[string]bool
	cron    *cron.Cron
	entries map[string]cron.EntryID // backup id -> cron entry, for removal and next-run
}

// newBackupManager loads the persisted backups and registers every scheduled one
// with cron. A backup whose schedule no longer parses is skipped with a warning
// rather than aborting the agent — a broken backups.json must not keep the box
// from booting its agent. Call Start to begin firing and Stop on shutdown.
func newBackupManager(path string) *backupManager {
	m := &backupManager{
		store:   newBackupStore(path),
		status:  make(map[string]*backupStatus),
		running: make(map[string]bool),
		cron:    cron.New(),
		entries: make(map[string]cron.EntryID),
	}
	for _, b := range m.store.Snapshot() {
		m.status[b.ID] = &backupStatus{}
		if b.Schedule == "" {
			continue
		}
		if err := m.register(b); err != nil {
			log.Printf("hush-agent: skipping schedule for backup %s (%q): %v", b.ID, b.Name, err)
		}
	}
	return m
}

func (m *backupManager) Start() { m.cron.Start() }

// Stop halts the cron engine and waits for any in-flight scheduled fire to
// return. cron.Stop() only signals the run loop and returns a context that closes
// once running jobs finish — it does not itself block — so we wait on that
// context. Without the wait, a fire still inside restic (reading the package's
// restic.Binary, say) can outlive Stop and race a caller that tears down shared
// state afterward, which is exactly what -race catches in the schedule test.
func (m *backupManager) Stop() { <-m.cron.Stop().Done() }

// register wires one backup's schedule into cron so it fires unattended. The
// caller holds m.mu (or, at construction, has exclusive access).
func (m *backupManager) register(b Backup) error {
	id, err := m.cron.AddFunc(b.Schedule, func() { m.runScheduled(b.ID) })
	if err != nil {
		return err
	}
	m.entries[b.ID] = id
	return nil
}

// unregister removes a backup's cron entry, if it has one. The caller holds m.mu.
func (m *backupManager) unregister(id string) {
	if eid, ok := m.entries[id]; ok {
		m.cron.Remove(eid)
		delete(m.entries, id)
	}
}

// runScheduled fires a backup from the clock, with no client to stream to. A run
// already in flight (a manual run, or a previous fire that overran its interval)
// is skipped rather than queued, so a long backup can't stack copies of itself.
func (m *backupManager) runScheduled(id string) {
	if err := m.Run(context.Background(), id, func(restic.Event) {}); err != nil {
		log.Printf("hush-agent: scheduled backup %s skipped: %v", id, err)
	}
}

// repoFor pulls a definition's restic Repo (backend + password) out of the
// store. The password never leaves the agent — it is read here to build the
// restic environment and nowhere else.
func (b Backup) repo() restic.Repo {
	return restic.Repo{Backend: b.Repo, Password: b.Password}
}

// spec turns a definition into a restic backup spec, tagging every snapshot with
// "hush" and the backup's own id so Snapshots can filter a repo (which may hold
// several machines' backups) back to just this one.
func (b Backup) spec() restic.Spec {
	return restic.Spec{
		Paths:         b.Paths,
		Excludes:      b.Excludes,
		OneFileSystem: b.OneFileSystem,
		Tags:          []string{"hush", b.ID},
	}
}

func (m *backupManager) view(b Backup) backupView {
	m.mu.Lock()
	st := backupStatus{}
	if p, ok := m.status[b.ID]; ok {
		st = *p
	}
	eid, hasEntry := m.entries[b.ID]
	m.mu.Unlock()
	// Query cron outside m.mu: on a running engine Entry round-trips to the run
	// loop, and a fired job takes m.mu, so holding it here could serialise the two.
	next := ""
	if hasEntry {
		if e := m.cron.Entry(eid); e.Valid() && !e.Next.IsZero() {
			next = e.Next.UTC().Format(time.RFC3339)
		}
	}
	return backupView{
		ID:            b.ID,
		Name:          b.Name,
		Repo:          b.Repo,
		Paths:         b.Paths,
		Excludes:      b.Excludes,
		OneFileSystem: b.OneFileSystem,
		Schedule:      b.Schedule,
		NextRun:       next,
		CreatedAt:     b.CreatedAt,
		Status:        st,
	}
}

// List returns every backup as a view (no passwords), in creation order.
func (m *backupManager) List() []backupView {
	defs := m.store.Snapshot()
	out := make([]backupView, 0, len(defs))
	for _, b := range defs {
		out = append(out, m.view(b))
	}
	return out
}

// Add validates and persists a new backup, and — before saving — proves the
// repository is real and the password works: it initialises the repo (tolerating
// one that already exists) and lists its snapshots. A bad backend URL or wrong
// password fails here, at create time, rather than silently at the first run.
func (m *backupManager) Add(ctx context.Context, b Backup) (backupView, error) {
	if _, ok := restic.Available(ctx); !ok {
		return backupView{}, errors.New("restic is not installed on this box — install it, then add the backup")
	}
	repo := b.repo()
	if err := restic.Init(ctx, repo); err != nil {
		return backupView{}, err
	}
	if _, err := restic.Snapshots(ctx, repo); err != nil {
		return backupView{}, fmt.Errorf("repository reachable but the password or credentials were rejected: %w", err)
	}
	saved, err := m.store.Add(b)
	if err != nil {
		return backupView{}, err
	}
	m.mu.Lock()
	m.status[saved.ID] = &backupStatus{}
	if saved.Schedule != "" {
		if err := m.register(saved); err != nil {
			// The schedule was validated at create, so this is unexpected; still,
			// don't leave a persisted backup that will never fire — roll it back.
			delete(m.status, saved.ID)
			m.mu.Unlock()
			_, _ = m.store.Delete(saved.ID)
			return backupView{}, fmt.Errorf("schedule %q: %w", saved.Schedule, err)
		}
	}
	m.mu.Unlock()
	return m.view(saved), nil
}

// Delete forgets a backup's definition. It does not touch the repository — the
// snapshots stay put, to be pruned with restic directly — so a mistaken delete
// loses the schedule, never the data.
func (m *backupManager) Delete(id string) (bool, error) {
	removed, err := m.store.Delete(id)
	if err != nil {
		return false, err
	}
	if !removed {
		return false, nil
	}
	m.mu.Lock()
	delete(m.status, id)
	delete(m.running, id)
	m.unregister(id)
	m.mu.Unlock()
	return true, nil
}

// mark applies f to a backup's status under the lock.
func (m *backupManager) mark(id string, f func(*backupStatus)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	st, ok := m.status[id]
	if !ok {
		st = &backupStatus{}
		m.status[id] = st
	}
	f(st)
}

// errBackupRunning is returned when a run is requested for a backup that already
// has one in flight; two concurrent runs would deadlock on the repo lock.
var errBackupRunning = errors.New("a run for this backup is already in progress")

// Run streams a backup's restic run to emit and records the outcome. It refuses
// to start a second concurrent run of the same backup. After a successful run it
// best-effort records the new snapshot's short id, so the console can show what
// the run wrote without a separate call.
func (m *backupManager) Run(ctx context.Context, id string, emit func(restic.Event)) error {
	def, ok := m.store.Find(id)
	if !ok {
		return errBackupNotFound
	}
	m.mu.Lock()
	if m.running[id] {
		m.mu.Unlock()
		return errBackupRunning
	}
	m.running[id] = true
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		delete(m.running, id)
		m.mu.Unlock()
	}()

	m.mark(id, func(st *backupStatus) { st.Running = true })
	start := time.Now()
	var last restic.Event
	restic.Backup(ctx, def.repo(), def.spec(), restic.DefaultBackupTimeout, func(ev restic.Event) {
		if ev.Kind == "exit" || ev.Kind == "error" {
			last = ev
		}
		emit(ev)
	})
	code := last.Code
	errMsg := last.Signal
	if last.Kind == "error" {
		code = -1
		errMsg = last.Data
	}
	m.mark(id, func(st *backupStatus) {
		st.Running = false
		st.Runs++
		st.LastRun = start.UTC().Format(time.RFC3339)
		st.LastMS = last.MS
		st.LastCode = code
		st.LastError = errMsg
	})
	if code == 0 && errMsg == "" {
		// Best-effort: name the snapshot the run just wrote. A failure here doesn't
		// change the run's success — it just leaves LastSnapshot blank.
		sctx, cancel := context.WithTimeout(context.Background(), snapshotsDeadline)
		if snaps, err := restic.Snapshots(sctx, def.repo(), def.ID); err == nil && len(snaps) > 0 {
			latest := snaps[len(snaps)-1].ShortID
			m.mark(id, func(st *backupStatus) { st.LastSnapshot = latest })
		}
		cancel()
	}
	return nil
}

// Snapshots lists the repository's snapshots for one backup (filtered to its
// tag), so the console can show its history.
func (m *backupManager) Snapshots(ctx context.Context, id string) ([]restic.Snapshot, error) {
	def, ok := m.store.Find(id)
	if !ok {
		return nil, errBackupNotFound
	}
	return restic.Snapshots(ctx, def.repo(), def.ID)
}

// LS lists one directory level inside a snapshot of a backup — the browse-inside-
// a-snapshot read path. It only reads (a snapshot is immutable), so confirming
// what a snapshot holds before a restore can never harm the backup history.
func (m *backupManager) LS(ctx context.Context, id, snapshot, dir string) ([]restic.Node, bool, error) {
	def, ok := m.store.Find(id)
	if !ok {
		return nil, false, errBackupNotFound
	}
	return restic.List(ctx, def.repo(), snapshot, dir, restic.DefaultListLimit)
}

// Restore streams a `restic restore` of one snapshot into target, optionally
// narrowed to includes. It reads from the repository and writes into target
// only — the snapshots are never touched — so a restore can't harm the backup
// history. target must be absolute (validated by the handler).
func (m *backupManager) Restore(ctx context.Context, id, snapshot, target string, includes []string, emit func(restic.Event)) error {
	def, ok := m.store.Find(id)
	if !ok {
		return errBackupNotFound
	}
	restic.Restore(ctx, def.repo(), snapshot, target, includes, restic.DefaultBackupTimeout, emit)
	return nil
}

var errBackupNotFound = errors.New("no such backup")

// snapshotIDRE gates the snapshot id a restore names: restic ids are hex, and
// "latest" is the one non-hex value it accepts. Anything else is rejected before
// it reaches restic's argv.
var snapshotIDRE = regexp.MustCompile(`^([0-9a-fA-F]{6,64}|latest)$`)

// validateBackup checks a create request and, on success, returns a stored
// Backup with its id and timestamp filled in. Paths must be absolute — a backup
// names roots on the box, not relative fragments — and the repo and password are
// required, since a restic repo is meaningless without either.
func validateBackup(name, repo, password string, paths, excludes []string, oneFS bool, schedule string) (Backup, error) {
	name = strings.TrimSpace(name)
	repo = strings.TrimSpace(repo)
	schedule = strings.TrimSpace(schedule)
	if name == "" {
		return Backup{}, errors.New("name is required")
	}
	if repo == "" {
		return Backup{}, errors.New("repository is required (e.g. rest:http://nas:8000/homelab)")
	}
	if len(repo) > maxBackupField {
		return Backup{}, errors.New("repository is too long")
	}
	if password == "" {
		return Backup{}, errors.New("repository password is required")
	}
	cleanPaths := make([]string, 0, len(paths))
	for _, p := range paths {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if len(p) > maxBackupField {
			return Backup{}, errors.New("a path is too long")
		}
		if !filepath.IsAbs(p) {
			return Backup{}, fmt.Errorf("path %q must be absolute", p)
		}
		cleanPaths = append(cleanPaths, filepath.Clean(p))
	}
	if len(cleanPaths) == 0 {
		return Backup{}, errors.New("add at least one path to back up")
	}
	if len(cleanPaths) > maxBackupPaths {
		return Backup{}, errors.New("too many paths")
	}
	cleanEx := make([]string, 0, len(excludes))
	for _, e := range excludes {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if len(e) > maxBackupField {
			return Backup{}, errors.New("an exclude is too long")
		}
		cleanEx = append(cleanEx, e)
	}
	// A schedule is optional (empty = manual-only); when present it must parse as
	// a 5-field cron spec, so an invalid one is rejected here rather than silently
	// never firing.
	if schedule != "" {
		if _, err := cron.ParseStandard(schedule); err != nil {
			return Backup{}, fmt.Errorf(`invalid schedule (want a 5-field cron spec like "0 3 * * *", or @daily): %w`, err)
		}
	}
	return Backup{
		ID:            newBackupID(),
		Name:          name,
		Repo:          repo,
		Password:      password,
		Paths:         cleanPaths,
		Excludes:      cleanEx,
		OneFileSystem: oneFS,
		Schedule:      schedule,
		CreatedAt:     time.Now().UTC().Format(time.RFC3339),
	}, nil
}

func newBackupID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// backupStatePath places backups.json in the agent's state dir.
func backupStatePath(dir string) string { return filepath.Join(dir, "backups.json") }

// handleBackups serves the collection: GET lists (without passwords), POST
// creates. It's only reached when the agent was started with backups enabled
// (main gates the route).
func (m *backupManager) handleBackups(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		writeJSON(w, m.List())
	case http.MethodPost:
		var req struct {
			Name          string   `json:"name"`
			Repo          string   `json:"repo"`
			Password      string   `json:"password"`
			Paths         []string `json:"paths"`
			Excludes      []string `json:"excludes"`
			OneFileSystem bool     `json:"oneFileSystem"`
			Schedule      string   `json:"schedule"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		def, err := validateBackup(req.Name, req.Repo, req.Password, req.Paths, req.Excludes, req.OneFileSystem, req.Schedule)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), createDeadline)
		defer cancel()
		view, err := m.Add(ctx, def)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusCreated)
		if err := json.NewEncoder(w).Encode(view); err != nil {
			log.Printf("encode backup: %v", err)
		}
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBackup serves one backup by id: DELETE forgets it (leaving repo data).
func (m *backupManager) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	removed, err := m.Delete(r.PathValue("id"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	if !removed {
		http.Error(w, "no such backup", http.StatusNotFound)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleBackupRun streams a run as Server-Sent Events, the same framing /exec
// uses so the console's run terminal renders a backup and a Task identically.
func (m *backupManager) handleBackupRun(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	id := r.PathValue("id")
	if _, ok := m.store.Find(id); !ok {
		http.Error(w, "no such backup", http.StatusNotFound)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	emit := func(ev restic.Event) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	if err := m.Run(r.Context(), id, emit); err != nil {
		// The stream is already 200 OK, so surface the refusal as an error frame
		// the run terminal renders rather than a status code it can't see.
		emit(restic.Event{Kind: "error", Data: err.Error()})
		emit(restic.Event{Kind: "exit", Code: -1})
	}
}

// handleBackupRestore streams a `restic restore` of one snapshot into a target
// directory, as the same SSE the run terminal renders. The snapshot id is
// validated (hex or "latest") and the target must be absolute — both are checked
// before the 200/stream begins so a bad request gets a real status code. The
// restore only writes into target; it never touches the snapshots.
func (m *backupManager) handleBackupRestore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	id := r.PathValue("id")
	if _, ok := m.store.Find(id); !ok {
		http.Error(w, "no such backup", http.StatusNotFound)
		return
	}
	var req struct {
		Snapshot string   `json:"snapshot"`
		Target   string   `json:"target"`
		Includes []string `json:"includes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	snapshot := strings.TrimSpace(req.Snapshot)
	if snapshot == "" {
		snapshot = "latest"
	}
	if !snapshotIDRE.MatchString(snapshot) {
		http.Error(w, "snapshot must be a restic snapshot id or \"latest\"", http.StatusBadRequest)
		return
	}
	target := strings.TrimSpace(req.Target)
	if !filepath.IsAbs(target) {
		http.Error(w, "target must be an absolute path", http.StatusBadRequest)
		return
	}
	target = filepath.Clean(target)
	includes := make([]string, 0, len(req.Includes))
	for _, inc := range req.Includes {
		if inc = strings.TrimSpace(inc); inc != "" {
			includes = append(includes, inc)
		}
	}

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

	emit := func(ev restic.Event) {
		b, err := json.Marshal(ev)
		if err != nil {
			return
		}
		fmt.Fprintf(w, "data: %s\n\n", b)
		flusher.Flush()
	}
	if err := m.Restore(r.Context(), id, snapshot, target, includes, emit); err != nil {
		emit(restic.Event{Kind: "error", Data: err.Error()})
		emit(restic.Event{Kind: "exit", Code: -1})
	}
}

// handleBackupSnapshots lists a backup's snapshots from the repository.
func (m *backupManager) handleBackupSnapshots(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotsDeadline)
	defer cancel()
	snaps, err := m.Snapshots(ctx, r.PathValue("id"))
	if err != nil {
		if errors.Is(err, errBackupNotFound) {
			http.Error(w, "no such backup", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	writeJSON(w, snaps)
}

// handleBackupSnapshotLS lists one directory level inside a snapshot, so the
// console can walk a snapshot's tree and confirm the data is really there before
// trusting a restore — restore confidence without writing anything. The snapshot
// id is validated (hex or "latest") and any path must be absolute, both before
// restic is invoked. It is read-only: `restic ls` never touches the repository.
func (m *backupManager) handleBackupSnapshotLS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	snap := strings.TrimSpace(r.PathValue("snap"))
	if !snapshotIDRE.MatchString(snap) {
		http.Error(w, "snapshot must be a restic snapshot id or \"latest\"", http.StatusBadRequest)
		return
	}
	dir := r.URL.Query().Get("path")
	if dir != "" && !filepath.IsAbs(dir) {
		http.Error(w, "path must be absolute", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), snapshotsDeadline)
	defer cancel()
	entries, truncated, err := m.LS(ctx, r.PathValue("id"), snap, dir)
	if err != nil {
		if errors.Is(err, errBackupNotFound) {
			http.Error(w, "no such backup", http.StatusNotFound)
			return
		}
		http.Error(w, err.Error(), http.StatusBadGateway)
		return
	}
	shown := "/"
	if dir != "" {
		shown = filepath.Clean(dir)
	}
	if entries == nil {
		entries = []restic.Node{}
	}
	writeJSON(w, map[string]any{"path": shown, "entries": entries, "truncated": truncated})
}

// writeJSON is the small encode-and-log helper the backup handlers share.
func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}
