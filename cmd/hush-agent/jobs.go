package main

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// A box runs long jobs that are not backups and not coding-agent sessions: a
// multi-hour model download, a build, a migration. They are invisible today —
// the only way to know how one is doing is to SSH in and look at file sizes,
// which is exactly the thing the console exists to spare you.
//
// This is the read side of docs/JOBS-CONVENTION.md, and it is deliberately the
// same shape as backupstatus.go: the job writes a secret-free status file, the
// unprivileged agent only ever reads it. hush executes nothing here. A job is
// started by whoever owns it — a shell, a systemd unit, a detached script — and
// the console reports it without being granted the power to run anything.
const defaultJobsDir = "/var/lib/hush-jobs"

// jobHeartbeatTTL is how fresh a progress sample must be to count as live.
//
// This is the load-bearing difference from backups. A convention backup is a
// systemd template unit, so the agent can ask systemd what is running and get a
// true answer even from a runner that never cooperated. A job has no such
// authority behind it: today's model download is three detached setsid scripts
// that systemd has never heard of. Liveness therefore comes from the writer's
// own heartbeat, and the only honest thing to do with a job that stopped
// publishing is to say so.
//
// Two minutes matches the window the console already applies to backup progress
// for the same reason: a frozen number presented as live is a worse lie than no
// number at all.
const jobHeartbeatTTL = 2 * time.Minute

// jobStatus is one <name>.json as written by scripts/hush-job-publish.
//
// Nothing here is a secret, which is what makes the file world-readable and this
// endpoint ungated — the same bargain /backup-status strikes. A job that wants
// to report something sensitive should report less, not expect the file to be
// protected.
type jobStatus struct {
	Name     string `json:"name"`
	Started  string `json:"started"`
	Finished string `json:"finished,omitempty"`
	ExitCode int    `json:"exit_code"`
	OK       bool   `json:"ok"`

	// Note is the job's one-line description of what it is doing right now
	// ("downloading Qwen3-Coder-Next, 19.5/49.3 GB"). Free text, because the
	// alternative is a schema every future job has to be bent to fit.
	Note string `json:"note,omitempty"`

	// State is "running", "stale", or empty for a finished job.
	//
	// "stale" is never written by the job — it is this agent's verdict on a job
	// that claims to be running but has stopped publishing. It has to be a
	// declared field rather than passed through, because the agent unmarshals
	// and re-marshals this struct and an unknown field would be dropped in
	// transit, exactly as backupstatus.go documents for its own State.
	State string `json:"state,omitempty"`

	// Progress is the live sample from <name>.progress.json, raw so the agent
	// stays ignorant of the schema and a job that starts publishing a new field
	// reaches the console without a change here. Only ever attached when the
	// sample is fresh; see jobHeartbeatTTL.
	Progress json.RawMessage `json:"progress,omitempty"`
}

// jobProgress is the sliver of <name>.progress.json the agent must understand.
//
// Everything else in that file rides through untouched as Progress above. The
// agent has to parse this one field because freshness is the whole basis for
// deciding a job is still alive, and that judgement cannot be delegated to the
// console: a stale sample must not be attached at all, or every reader has to
// re-derive the same verdict and one of them will forget.
type jobProgress struct {
	Updated string `json:"updated"`
}

// readJobProgress returns a job's live progress sample, and whether it is fresh
// enough to be believed.
//
// A missing file is the normal case, not an error: a job that has not published
// yet, or one that finished and cleaned up after itself. A file that does not
// parse is dropped for the same reason a half-written history line is — a
// truncated number must not take out the whole status read.
//
// An unparseable or absent `updated` is treated as stale rather than fresh. The
// failure this guards is a job killed outright (OOM, reboot, power) which never
// runs the cleanup that removes its progress file: the sample outlives the run,
// and defaulting to fresh would leave a dead job showing a confident percentage
// forever.
func readJobProgress(dir, name string, now time.Time) (json.RawMessage, bool) {
	b, err := os.ReadFile(filepath.Join(dir, name+".progress.json"))
	if err != nil {
		return nil, false
	}
	b = []byte(strings.TrimSpace(string(b)))
	if len(b) == 0 || !json.Valid(b) {
		return nil, false
	}
	var p jobProgress
	if err := json.Unmarshal(b, &p); err != nil {
		return nil, false
	}
	updated, err := time.Parse(time.RFC3339, p.Updated)
	if err != nil {
		return json.RawMessage(b), false
	}
	return json.RawMessage(b), now.Sub(updated) <= jobHeartbeatTTL
}

// readJobStatuses loads every <name>.json in dir, sorted by name so the
// console's ordering is stable between polls.
//
// A missing directory is not an error: a box that runs no tracked jobs is the
// normal case and reports an empty list. A file that does not parse is skipped
// and logged rather than silently dropped — a status file that stopped being
// readable is precisely what a job console must not hide.
func readJobStatuses(dir string, now time.Time) ([]jobStatus, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return []jobStatus{}, nil
		}
		return nil, err
	}

	out := make([]jobStatus, 0, len(entries))
	for _, e := range entries {
		// The <name>.progress.json files end in .json too, so they need
		// excluding explicitly. Left in, every running job would sprout a
		// phantom twin card named "<name>.progress" for exactly as long as it
		// ran — the same trap readConventionBackupStatuses documents.
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		if strings.HasSuffix(e.Name(), ".progress.json") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		b, err := os.ReadFile(path)
		if err != nil {
			log.Printf("jobs: cannot read %s: %v", path, err)
			continue
		}
		var s jobStatus
		if err := json.Unmarshal(b, &s); err != nil {
			log.Printf("jobs: cannot parse %s: %v", path, err)
			continue
		}
		if s.Name == "" {
			// Fall back to the filename so a status file that lost its name
			// field still identifies itself in the console.
			s.Name = strings.TrimSuffix(e.Name(), ".json")
		}
		// Progress is attached only to a job that claims to be running AND is
		// still publishing. A finished job keeps no progress even if a sample
		// was left behind, and a job that stopped publishing is downgraded to
		// "stale" with its last sample withheld: the console shows that the job
		// went quiet, rather than a percentage frozen at the moment it died.
		if s.State == "running" {
			progress, fresh := readJobProgress(dir, s.Name, now)
			if fresh {
				s.Progress = progress
			} else {
				s.State = "stale"
			}
		}
		out = append(out, s)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// handleJobs serves this box's tracked jobs as a JSON array. Always an array,
// never null, so the console can render it without a nil check.
func handleJobs(dir string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		jobs, err := readJobStatuses(dir, time.Now())
		if err != nil {
			http.Error(w, "cannot read jobs directory", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		if err := json.NewEncoder(w).Encode(jobs); err != nil {
			log.Printf("jobs: encode: %v", err)
		}
	}
}
