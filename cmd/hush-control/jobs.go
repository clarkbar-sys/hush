package main

import (
	"io"
	"log"
	"net/http"
	"strings"
)

// proxyJobs forwards a request to one agent's /jobs and relays its response
// verbatim, the same shape as proxySessions.
//
// Per-machine rather than a fleet rollup, which is the opposite of the choice
// /api/backup-status makes — and deliberately so. A backup is a direction (this
// box → that store) and the question a reader has is fleet-shaped: "is
// everything backed up?". A job is something happening *on a box you are
// already looking at*; nobody asks "is the fleet downloading?". Aggregating
// would cost a fan-out on every poll to answer a question nobody has.
//
// An agent too old to serve /jobs answers 404, relayed as-is, so the console can
// say "update the agent" rather than drawing the machine as having no jobs — a
// partial rollout must not look like a quiet fleet.
func proxyJobs(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	u := strings.TrimRight(a.Addr, "/") + "/jobs"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy jobs %s: %v", a.Name, err)
	}
}
