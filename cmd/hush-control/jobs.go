package main

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"strings"
)

// proxyJobs forwards the console's Job requests to one agent's /jobs endpoint
// and relays the agent's response verbatim — status code included, so "jobs
// disabled" (403) and validation errors (400) reach the phone unchanged. Jobs
// are the one write construct that lives on the agent rather than in a
// control-side store: the cron scheduler runs on the box the job fires on, so
// hush-control is a pass-through, the way it is for /browse. The phone can't
// address agents directly in tsnet mode, so every request rides through here.
//
// GET lists a box's jobs (with run status); POST creates one from a
// {name, schedule, cmd} body. Creating a job schedules a command to run
// unattended as the hush user, so — like /exec — a create is logged with who
// asked, where, and the schedule: the audit trail the read path doesn't need.
func proxyJobs(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
	switch r.Method {
	case http.MethodGet, http.MethodPost:
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var body []byte
	if r.Method == http.MethodPost {
		b, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		body = b
		caller := callerFrom(r.Context())
		if caller == "" {
			caller = "lan"
		}
		log.Printf("job create on %s by %s: %s", a.Name, caller, execCmdPreview(body))
	}

	u := strings.TrimRight(a.Addr, "/") + "/jobs"
	req, err := http.NewRequestWithContext(r.Context(), r.Method, u, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	if r.Method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
	}
	relayAgentJSON(w, req, client, a.Name)
}

// proxyJob forwards a single-job request (DELETE /jobs/{id}) to the agent and
// relays its response verbatim, so a 404 for an unknown id and a 204 for a
// removal reach the console unchanged. Deleting a job unschedules a command, so
// it's audited like a create.
func proxyJob(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caller := callerFrom(r.Context())
	if caller == "" {
		caller = "lan"
	}
	log.Printf("job delete on %s by %s: %s", a.Name, caller, id)

	u := strings.TrimRight(a.Addr, "/") + "/jobs/" + id
	req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	relayAgentJSON(w, req, client, a.Name)
}

// relayAgentJSON performs an upstream request to an agent and copies its status
// and body straight through to the caller, shaping "agent unreachable" as a 502.
// It's the shared tail of the Job proxies — the same verbatim relay proxyBrowse
// does, factored out because both the collection and the item handler need it.
func relayAgentJSON(w http.ResponseWriter, req *http.Request, client *http.Client, name string) {
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(resp.StatusCode)
	if _, err := io.Copy(w, resp.Body); err != nil {
		log.Printf("proxy jobs %s: %v", name, err)
	}
}
