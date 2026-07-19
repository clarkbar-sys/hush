package main

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
)

// /api/backup-status aggregates every agent's /backup-status — the root-run
// backups described in docs/BACKUP-CONVENTION.md — into one fleet-wide answer,
// so the console asks once instead of once per machine.
//
// Unlike the Backup construct's endpoints this is not a per-machine proxy. A
// backup is a *direction* (this box → that store), and the question a reader
// actually has is "is everything backed up?", which is fleet-shaped. Asking it
// per machine would put the burden of assembling that answer on the phone.
//
// It deliberately does NOT get the fleetCache treatment that /api/fleet needed.
// That cache exists because the console polls /api/fleet every 2.5s, so one
// powered-off box stalled every cycle (#104). Backup status is viewed on
// demand and changes once a day; fanning out concurrently with the shared
// per-agent client timeout means an offline box costs one timeout in total,
// not one per machine, and it reports as unreachable rather than vanishing.

// hostBackupStatus is one machine's answer: its convention backups, or the
// fact that it could not be reached.
//
// Backups rides through as raw JSON so hush-control never has to know the
// status schema — the same reason the agent passes restic's own summary
// through untouched. A field added to the status file reaches the console
// without a change here.
type hostBackupStatus struct {
	Host      string            `json:"host"`
	Reachable bool              `json:"reachable"`
	Backups   []json.RawMessage `json:"backups"`
}

// collectBackupStatus fans out to every agent concurrently, preserving the
// caller's agent order so the console's list doesn't reshuffle between loads.
func collectBackupStatus(client *http.Client, agents []Agent) []hostBackupStatus {
	out := make([]hostBackupStatus, len(agents))
	var wg sync.WaitGroup
	for i, a := range agents {
		wg.Add(1)
		go func(i int, a Agent) {
			defer wg.Done()
			out[i] = fetchBackupStatus(client, a)
		}(i, a)
	}
	wg.Wait()
	return out
}

func fetchBackupStatus(client *http.Client, a Agent) hostBackupStatus {
	// Default to unreachable; a successful fetch overwrites it. A box that is
	// down must still appear — a backup console that silently omits a machine
	// is worse than one that says it cannot tell.
	h := hostBackupStatus{Host: a.Name, Backups: []json.RawMessage{}}
	if h.Host == "" {
		h.Host = a.Addr
	}

	resp, err := client.Get(strings.TrimRight(a.Addr, "/") + "/backup-status")
	if err != nil {
		return h
	}
	defer resp.Body.Close()

	// An agent predating this endpoint 404s. That is "nothing to report", not a
	// failure, and it must not be drawn as a broken machine during a rollout
	// where some boxes are still on an older build.
	if resp.StatusCode != http.StatusOK {
		h.Reachable = true
		return h
	}

	var backups []json.RawMessage
	if err := json.NewDecoder(resp.Body).Decode(&backups); err != nil {
		h.Reachable = true
		return h
	}
	h.Reachable = true
	if backups != nil {
		h.Backups = backups
	}
	return h
}

// handleBackupStatus serves the aggregated fleet view.
func handleBackupStatus(client *http.Client, agents func() []Agent) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-cache")
		_ = json.NewEncoder(w).Encode(collectBackupStatus(client, agents()))
	}
}
