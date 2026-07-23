// The LLM chat proxy is the first control endpoint that makes a box do work.
// Every other per-machine route here is a read: vitals, a directory listing, a
// process table. This one relays an OpenAI-compatible chat-completions request
// to a fleet runtime and streams the tokens back — control → runtime, not
// control → agent. The browser only trusts the control node (tsnet identity),
// so it can't dial a box's 100.x:8091 itself; control does it on the browser's
// behalf, the same way opencode reaches that address from any tailnet node.
//
// It stays inside the read-only spirit by being gated, non-persistent, and
// reachable-only: a runtime is callable iff it's bound past loopback
// (exposure tailnet|open) — exactly the console's ocReachable / opencode-export
// verdict. A loopback-bound runtime is honestly unreachable from control's node
// and is signed off (409), never dialed.
package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net"
	"net/http"
	"strings"

	"github.com/clarkbar-sys/hush/internal/vitals"
)

// maxChatBody caps the request body we buffer to force stream:true. A chat
// request is small (a transcript, not an upload); the ceiling is generous
// headroom that still refuses an accidental or hostile firehose.
const maxChatBody = 1 << 20 // 1 MiB

// proxyLLMChat relays an OpenAI chat-completions request to a fleet runtime and
// streams the SSE response back token-by-token. It picks the runtime on m that
// serves the requested model and is reachable off-box, forces stream:true, and
// dials the runtime's reported address through the shared (tsnet) client. The
// upstream request carries r.Context(), so a client disconnect (closed IM
// window) cancels the call and frees the box's GPU.
func proxyLLMChat(w http.ResponseWriter, r *http.Request, client *http.Client, m Machine) {
	raw, err := io.ReadAll(io.LimitReader(r.Body, maxChatBody))
	if err != nil {
		http.Error(w, "read body", http.StatusBadRequest)
		return
	}
	var req map[string]any
	if err := json.Unmarshal(raw, &req); err != nil {
		http.Error(w, "body must be a JSON chat-completions object", http.StatusBadRequest)
		return
	}
	model, _ := req["model"].(string)

	rt, status, msg := pickRuntime(m, model)
	if status != 0 {
		http.Error(w, msg, status)
		return
	}

	// Force streaming: the whole point of this endpoint is token-by-token relay,
	// and a caller that forgot stream:true would otherwise get one buffered blob.
	req["stream"] = true
	body, err := json.Marshal(req)
	if err != nil {
		http.Error(w, "re-encode body", http.StatusInternalServerError)
		return
	}

	target, ok := runtimeChatURL(rt, m.IP)
	if !ok {
		// A wildcard-bound runtime on a box whose tailnet IP we don't know: the
		// address can't be turned into a routable target. Refuse rather than let
		// an empty host dial control's own loopback.
		http.Error(w, "runtime address is not routable from control", http.StatusBadGateway)
		return
	}
	ureq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		http.Error(w, "bad upstream request", http.StatusInternalServerError)
		return
	}
	ureq.Header.Set("Content-Type", "application/json")
	ureq.Header.Set("Accept", "text/event-stream")

	resp, err := client.Do(ureq)
	if err != nil {
		http.Error(w, "runtime unreachable", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Relay the runtime's content type (text/event-stream on the happy path) and
	// status verbatim, so a runtime-side error — a 400 for a bad request, say —
	// reaches the console as itself rather than masquerading as a stream.
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "text/event-stream"
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(resp.StatusCode)
	streamSSE(w, resp.Body)
}

// pickRuntime selects the runtime on m that should answer a chat for model,
// applying the same tailnet|open reachability gate as the console's ocReachable.
// It returns the HTTP status to send on failure (0, and a usable runtime, on
// success):
//
//   - no runtimes here, or none serving that model      -> 404
//   - model served only over loopback (or, when model is
//     omitted, every runtime is loopback-bound)          -> 409 "signed off"
//
// The loopback case is the safety gate surfacing, not an error: the box holds
// the model but hasn't exposed it past loopback, so control's node genuinely
// can't reach it. Mirrors ocReachable exactly.
func pickRuntime(m Machine, model string) (rt vitals.LLMRuntime, status int, msg string) {
	var runtimes []vitals.LLMRuntime
	if m.LLM != nil {
		runtimes = m.LLM.Runtimes
	}
	if len(runtimes) == 0 {
		return vitals.LLMRuntime{}, http.StatusNotFound, "no LLM runtime detected on this box"
	}

	// Candidates: runtimes serving the requested model — or every runtime, when
	// the caller didn't name one and takes the first reachable.
	var served []vitals.LLMRuntime
	for _, r := range runtimes {
		if model == "" || servesModel(r, model) {
			served = append(served, r)
		}
	}
	if len(served) == 0 {
		return vitals.LLMRuntime{}, http.StatusNotFound, "no runtime here serves that model"
	}
	for _, r := range served {
		if reachableRuntime(r) {
			return r, 0, ""
		}
	}
	return vitals.LLMRuntime{}, http.StatusConflict,
		"signed off: this runtime isn't reachable from control (bound to loopback, or its bind scope is unverified) — expose it past loopback to chat"
}

func servesModel(rt vitals.LLMRuntime, model string) bool {
	for _, id := range rt.Models {
		if id == model {
			return true
		}
	}
	return false
}

// reachableRuntime is the off-box callability verdict: bound to the tailnet or
// wider. It's the Go twin of the console's ocReachable and of
// vitals.LLMCapability.Reachable — an unknown scope is not evidence of reach.
func reachableRuntime(rt vitals.LLMRuntime) bool {
	return rt.Exposure == vitals.LLMExposureTailnet || rt.Exposure == vitals.LLMExposureOpen
}

// runtimeChatURL builds a runtime's chat-completions URL from its reported bind
// address, mirroring the console's splitAddr + ocConfig rule: use the address
// as the kernel reports it, but swap a wildcard bind (0.0.0.0 / ::) for the
// box's tailnet IP, since a wildcard isn't a routable host. Both llama.cpp /
// llama-swap (openai) and Ollama serve an OpenAI-compatible
// /v1/chat/completions, so the path is the same for either kind.
//
// It returns ok=false when the address can't be made routable — a wildcard or
// blank bind on a box whose tailnet IP we don't have. Building a URL anyway
// would leave an empty host, which Go dials as localhost: control would POST to
// its own node instead of failing honestly.
func runtimeChatURL(rt vitals.LLMRuntime, tailnetIP string) (string, bool) {
	host, port, err := net.SplitHostPort(rt.Addr)
	if err != nil {
		host, port = rt.Addr, ""
	}
	if host == "0.0.0.0" || host == "::" || host == "" {
		host = tailnetIP
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		return "", false
	}
	hostPart := host
	if strings.Contains(host, ":") {
		hostPart = "[" + host + "]"
	}
	u := "http://" + hostPart
	if port != "" {
		u += ":" + port
	}
	return u + "/v1/chat/completions", true
}

// streamSSE copies the runtime's response to the client, flushing after every
// chunk so tokens surface as they're generated instead of buffering to the end.
// It stops on the first read or write error — including the write failure that a
// client disconnect surfaces — leaving upstream cancellation to the request
// context proxyLLMChat threaded through.
func streamSSE(w http.ResponseWriter, body io.Reader) {
	flusher, _ := w.(http.Flusher)
	buf := make([]byte, 4096)
	for {
		n, rerr := body.Read(buf)
		if n > 0 {
			if _, werr := w.Write(buf[:n]); werr != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
		if rerr != nil {
			if rerr != io.EOF {
				log.Printf("llm chat stream: %v", rerr)
			}
			return
		}
	}
}

// findMachine resolves a console-supplied host id to a machine in a fleet
// snapshot. The console keys machines by display id, so that's matched first;
// the tailnet IP is the fallback for agents configured without a name, the same
// precedence agentStore.find uses.
func findMachine(machines []Machine, host string) (Machine, bool) {
	for _, m := range machines {
		if m.ID == host {
			return m, true
		}
	}
	for _, m := range machines {
		if m.IP == host {
			return m, true
		}
	}
	return Machine{}, false
}

// resolveMachine finds the machine (and its detected runtimes) behind a host id.
// It prefers the warm fleet snapshot — the runtimes are already collected there,
// no fresh round-trip — and falls back to a single live /vitals fetch of that
// one agent on a cold cache, so a chat can land before the first background poll
// without fanning out to the whole fleet.
func resolveMachine(fc *fleetCache, store *agentStore, client *http.Client, host string) (Machine, bool) {
	if machines, ok := fc.snapshot(); ok {
		if m, ok := findMachine(machines, host); ok {
			return m, true
		}
	}
	a, ok := store.find(host)
	if !ok {
		return Machine{}, false
	}
	return fetchOne(client, a, ""), true
}
