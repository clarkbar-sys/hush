package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	hexec "github.com/clarkbar-sys/hush/internal/exec"
)

// execHandler runs a one-shot command and streams its output as Server-Sent
// Events — the write half of the Task construct, and the first agent endpoint
// that changes the box. It is opt-in: main only registers it when the agent was
// started with -exec (or HUSH_AGENT_EXEC=1), so an untouched agent stays
// read-only. The command runs as the unprivileged "hush" user with no jail,
// exactly like /browse — the Unix identity is the boundary, not this handler.
//
// A request may name a run-as user; the command then runs as that user via
// `sudo -u`. runAs is the set of users this agent will honour (from -run-as);
// an empty set means the feature is off and any run-as request is refused. This
// allowlist is the ceiling on what a caller can become, so it must never include
// root or a sudo-capable user.
//
// Each event is one JSON object on an SSE data line: start (pid), out
// (stdout/stderr chunks), then exit (code, signal, duration) — or error. The
// stream flushes per frame so output appears live on the phone, and closing the
// connection (the caller hanging up) cancels the request context, which kills
// the command's whole process group.
func execHandler(runAs map[string]bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		var req struct {
			Cmd        string `json:"cmd"`
			TimeoutSec int    `json:"timeoutSec"`
			User       string `json:"user"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "bad request body", http.StatusBadRequest)
			return
		}
		if strings.TrimSpace(req.Cmd) == "" {
			http.Error(w, "cmd is required", http.StatusBadRequest)
			return
		}
		user := strings.TrimSpace(req.User)
		if user != "" && !runAs[user] {
			http.Error(w, fmt.Sprintf("run-as user %q is not allowed on this agent (see -run-as)", user), http.StatusForbidden)
			return
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
		// Defeat any reverse-proxy response buffering so frames arrive as they're
		// written rather than in one lump at the end.
		h.Set("X-Accel-Buffering", "no")
		w.WriteHeader(http.StatusOK)
		flusher.Flush()

		emit := func(ev hexec.Event) {
			b, err := json.Marshal(ev)
			if err != nil {
				return
			}
			// JSON has no literal newlines, so the whole event rides one data line.
			fmt.Fprintf(w, "data: %s\n\n", b)
			flusher.Flush()
		}
		hexec.Run(r.Context(), hexec.Spec{
			Cmd:     req.Cmd,
			Timeout: time.Duration(req.TimeoutSec) * time.Second,
			User:    user,
		}, emit)
	}
}
