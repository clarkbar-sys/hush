package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
)

// Backups, like Jobs, live on the agent — the box that holds the data holds the
// repository key, which never passes through hush-control. So these are
// pass-through proxies, not a control-side store: the console reaches
// GET/POST /backups, DELETE /backups/{id}, POST /backups/{id}/run (streamed),
// and GET /backups/{id}/snapshots the same way it reaches everything else,
// through hush-control, since the phone can't address agents directly in tsnet
// mode. An agent started without -backup answers 403, relayed verbatim.

// proxyBackups forwards the collection request (GET list / POST create) to one
// agent's /backups and relays the response verbatim. A create carries the repo
// password in its body, so — unlike a Task or Job create — the audit log records
// only the backup's name and repository, never the body.
func proxyBackups(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent) {
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
		log.Printf("backup create on %s by %s: %s", a.Name, caller, backupCreatePreview(body))
	}

	u := strings.TrimRight(a.Addr, "/") + "/backups"
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

// proxyBackup forwards a single-backup request (DELETE /backups/{id}) to the
// agent and relays its response verbatim. Deleting a backup forgets its
// definition (the repo's snapshots are left intact), so it's audited like a
// create.
func proxyBackup(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caller := callerFrom(r.Context())
	if caller == "" {
		caller = "lan"
	}
	log.Printf("backup delete on %s by %s: %s", a.Name, caller, id)

	u := strings.TrimRight(a.Addr, "/") + "/backups/" + id
	req, err := http.NewRequestWithContext(r.Context(), http.MethodDelete, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	relayAgentJSON(w, req, client, a.Name)
}

// proxyBackupSnapshots forwards a snapshots listing to the agent's
// /backups/{id}/snapshots and relays the JSON verbatim. It rides a client with a
// longer timeout than the fleet poll, since listing a repository reaches across
// the tailnet to the rest-server.
func proxyBackupSnapshots(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := strings.TrimRight(a.Addr, "/") + "/backups/" + id + "/snapshots"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	relayAgentJSON(w, req, client, a.Name)
}

// proxyBackupSnapshotLS forwards a snapshot directory listing to the agent's
// /backups/{id}/snapshots/{snap}/ls and relays the JSON verbatim — the browse-
// inside-a-snapshot read path. Like the snapshots listing it's read-only and
// reaches across the tailnet to the rest-server, so it rides the same longer
// client rather than the 2s fleet-poll one. The ?path= query is carried through.
func proxyBackupSnapshotLS(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id, snap string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	u := strings.TrimRight(a.Addr, "/") + "/backups/" + id + "/snapshots/" + snap + "/ls"
	if q := r.URL.RawQuery; q != "" {
		u += "?" + q
	}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, u, nil)
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	relayAgentJSON(w, req, client, a.Name)
}

// proxyBackupRun forwards a run to the agent's /backups/{id}/run and streams the
// Server-Sent Events back to the phone, flushing each frame — the same streaming
// relay proxyExec does. A run can take a very long time (an initial full backup),
// so it rides the no-timeout streamClient, and every run is audited with who ran
// it, where, and which backup.
func proxyBackupRun(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	caller := callerFrom(r.Context())
	if caller == "" {
		caller = "lan"
	}
	log.Printf("backup run on %s by %s: %s", a.Name, caller, id)

	u := strings.TrimRight(a.Addr, "/") + "/backups/" + id + "/run"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u, nil)
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

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flushCopy(w, resp.Body)
}

// proxyBackupRestore forwards a restore to the agent's /backups/{id}/restore and
// streams the Server-Sent Events back, like proxyBackupRun. A restore writes data
// onto the box, so — like a run — it's audited with who asked, where, and which
// backup (the target rides the body, logged here for the trail).
func proxyBackupRestore(w http.ResponseWriter, r *http.Request, client *http.Client, a Agent, id string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		http.Error(w, "bad request body", http.StatusBadRequest)
		return
	}
	caller := callerFrom(r.Context())
	if caller == "" {
		caller = "lan"
	}
	log.Printf("backup restore on %s by %s: %s %s", a.Name, caller, id, backupRestorePreview(body))

	u := strings.TrimRight(a.Addr, "/") + "/backups/" + id + "/restore"
	req, err := http.NewRequestWithContext(r.Context(), http.MethodPost, u, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		http.Error(w, "agent unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	if ct := resp.Header.Get("Content-Type"); ct != "" {
		w.Header().Set("Content-Type", ct)
	}
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(resp.StatusCode)
	flushCopy(w, resp.Body)
}

// backupRestorePreview pulls the snapshot and target out of a restore body for
// the audit log (no secrets ride this body, but keep it tidy).
func backupRestorePreview(body []byte) string {
	var req struct {
		Snapshot string `json:"snapshot"`
		Target   string `json:"target"`
	}
	_ = json.Unmarshal(body, &req)
	snap := strings.TrimSpace(req.Snapshot)
	if snap == "" {
		snap = "latest"
	}
	return snap + " → " + strings.TrimSpace(req.Target)
}

// backupCreatePreview pulls just the name and repository out of a create body
// for the audit log — deliberately never the password, which rides the same body.
func backupCreatePreview(body []byte) string {
	var req struct {
		Name string `json:"name"`
		Repo string `json:"repo"`
	}
	_ = json.Unmarshal(body, &req)
	name := strings.TrimSpace(req.Name)
	repo := strings.TrimSpace(req.Repo)
	if len(repo) > 120 {
		repo = repo[:120] + "…"
	}
	return name + " → " + repo
}

// relayAgentJSON performs an upstream request to an agent and copies its status
// and body straight through to the caller, shaping "agent unreachable" as a 502.
// It's the shared tail of the backup proxies — the same verbatim relay
// proxyBrowse does, factored out because several handlers need it.
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
		log.Printf("relay agent %s: %v", name, err)
	}
}
