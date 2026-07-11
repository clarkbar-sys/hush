// First-run setup for tsnet mode. When hush-control is asked to serve over the
// tailnet but has no auth key and no saved node state (see needsSetup), it can't
// yet reach the tailnet — so there's nowhere private to host a setup UI. Instead
// we bind a deliberately temporary, unauthenticated page to the LAN (-listen,
// default 0.0.0.0:8080) where the operator pastes a Tailscale auth key from a
// browser — no SSH, no editing env files by hand.
//
// This page is inherently exposed: plain HTTP, no identity gate, reachable by
// anyone on the LAN. That's the tradeoff for zero-touch first setup, so the page
// wears a loud warning banner and exists for exactly one moment — the instant a
// key provisions the node, runSetup returns the live node and the process hands
// the console off to tailnet HTTPS (serveConsoleOverTsnet). Once the node has
// state, needsSetup is false forever and this page never appears again.
package main

import (
	"context"
	"errors"
	"html/template"
	"log"
	"net/http"
	"strings"
	"sync"

	"tailscale.com/tsnet"
)

// provisionFunc brings a tsnet node up from a submitted auth key and hostname,
// returning the live server and its HTTPS console URL. It's injected so the
// setup handler is testable without a live tailnet.
type provisionFunc func(ctx context.Context, authKey, hostname string) (*tsnet.Server, string, error)

// runSetup serves the first-run setup page on listen (plain HTTP, LAN-exposed)
// and blocks until the operator submits an auth key that successfully joins the
// tailnet. It then shuts the setup server down and returns the live node, ready
// to serve the console over HTTPS. defaultHostname prefills the form.
func runSetup(listen, defaultHostname, stateDir string) *tsnet.Server {
	return runSetupWith(listen, defaultHostname, provisionTsnet(stateDir))
}

// runSetupWith is runSetup with an injectable provisioner (for tests).
func runSetupWith(listen, defaultHostname string, provision provisionFunc) *tsnet.Server {
	s := &setupServer{defaultHostname: defaultHostname, provision: provision, result: make(chan *tsnet.Server, 1)}

	httpSrv := &http.Server{Addr: listen, Handler: s.routes()}
	go func() {
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("setup: serving on %s: %v", listen, err)
		}
	}()

	live := <-s.result
	// Shutdown drains the in-flight success response before closing the
	// listener, so the operator's browser still receives the page.
	_ = httpSrv.Shutdown(context.Background())
	return live
}

// setupServer holds the state for the first-run setup page. done guards against
// a second submission racing in after the node is already provisioned.
type setupServer struct {
	defaultHostname string
	provision       provisionFunc
	result          chan *tsnet.Server

	mu   sync.Mutex
	done bool
}

func (s *setupServer) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/setup", s.handleSubmit)
	mux.HandleFunc("/", s.handleRoot)
	return mux
}

// handleRoot renders the setup form. Any non-GET falls through to 405 so the
// form's own POST goes to /setup.
func (s *setupServer) handleRoot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.renderForm(w, s.defaultHostname, "")
}

// handleSubmit takes the pasted key + hostname, provisions the node, and on
// success renders the HTTPS URL and hands the live node back to runSetupWith.
func (s *setupServer) handleSubmit(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	authKey := strings.TrimSpace(r.FormValue("authkey"))
	hostname := strings.TrimSpace(r.FormValue("hostname"))
	if hostname == "" {
		hostname = s.defaultHostname
	}
	if authKey == "" {
		s.renderForm(w, hostname, "Paste your Tailscale auth key to continue.")
		return
	}

	s.mu.Lock()
	alreadyDone := s.done
	s.mu.Unlock()
	if alreadyDone {
		s.renderDone(w)
		return
	}

	srv, url, err := s.provision(r.Context(), authKey, hostname)
	if err != nil {
		s.renderForm(w, hostname, "Couldn't join the tailnet: "+err.Error())
		return
	}

	s.mu.Lock()
	if s.done { // lost a race with a concurrent submit; discard the extra node
		s.mu.Unlock()
		srv.Close()
		s.renderDone(w)
		return
	}
	s.done = true
	s.mu.Unlock()

	s.renderSuccess(w, url)
	s.result <- srv
}

func (s *setupServer) renderForm(w http.ResponseWriter, hostname, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if errMsg != "" {
		w.WriteHeader(http.StatusBadRequest)
	}
	if err := setupFormTmpl.Execute(w, struct {
		Hostname string
		Error    string
	}{hostname, errMsg}); err != nil {
		log.Printf("setup: render form: %v", err)
	}
}

func (s *setupServer) renderSuccess(w http.ResponseWriter, url string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupSuccessTmpl.Execute(w, struct{ URL string }{url}); err != nil {
		log.Printf("setup: render success: %v", err)
	}
}

func (s *setupServer) renderDone(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := setupDoneTmpl.Execute(w, nil); err != nil {
		log.Printf("setup: render done: %v", err)
	}
}

// provisionTsnet is the production provisioner: it brings up a tsnet node from
// the submitted key and derives the console URL from the node's MagicDNS name.
func provisionTsnet(stateDir string) provisionFunc {
	return func(ctx context.Context, authKey, hostname string) (*tsnet.Server, string, error) {
		srv := &tsnet.Server{Hostname: hostname, AuthKey: authKey}
		if stateDir != "" {
			srv.Dir = stateDir
		}
		st, err := srv.Up(ctx)
		if err != nil {
			srv.Close()
			return nil, "", err
		}
		url := "https://" + hostname
		if st != nil && st.Self != nil && st.Self.DNSName != "" {
			url = "https://" + strings.TrimSuffix(st.Self.DNSName, ".")
		}
		return srv, url, nil
	}
}

// The setup UI is a single self-contained page (no external assets), matching
// the console's static-page style. The warning banner is fixed to the top-right
// per the operator's request and stays visible while the page is up.
const setupHead = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>hush · first-run setup</title>
<style>
  :root { color-scheme: light dark; }
  * { box-sizing: border-box; }
  body { margin: 0; font: 16px/1.5 system-ui, -apple-system, sans-serif;
         display: flex; min-height: 100vh; align-items: center; justify-content: center;
         padding: 5.5rem 1rem 2rem; }
  .warn { position: fixed; top: .75rem; right: .75rem; max-width: 22rem; z-index: 10;
          background: #b91c1c; color: #fff; border-radius: .5rem; padding: .6rem .8rem;
          font-size: .78rem; line-height: 1.35; box-shadow: 0 4px 14px rgba(0,0,0,.35); }
  .warn b { display: block; font-size: .85rem; letter-spacing: .02em; }
  .card { width: 100%; max-width: 30rem; }
  h1 { font-size: 1.4rem; margin: 0 0 .25rem; }
  p.sub { margin: 0 0 1.4rem; opacity: .75; }
  label { display: block; font-weight: 600; margin: 1rem 0 .3rem; }
  input { width: 100%; padding: .6rem .7rem; font: inherit; border-radius: .45rem;
          border: 1px solid color-mix(in srgb, currentColor 30%, transparent); background: transparent; }
  small { display: block; opacity: .65; margin-top: .3rem; }
  button { width: 100%; margin-top: 1.5rem; padding: .7rem; font: 600 1rem/1 inherit;
           border: 0; border-radius: .45rem; background: #2563eb; color: #fff; cursor: pointer; }
  .err { margin: 1rem 0 0; padding: .6rem .8rem; border-radius: .45rem;
         background: color-mix(in srgb, #b91c1c 18%, transparent); color: #b91c1c; font-size: .9rem; }
  a { color: #2563eb; }
  code { background: color-mix(in srgb, currentColor 12%, transparent); padding: .1rem .3rem; border-radius: .25rem; }
</style>
<div class="warn">
  <b>⚠ INSECURE SETUP PAGE</b>
  Plain HTTP, no login — anyone on this network can reach it. It's live only for
  first-run setup and disappears the moment your node joins the tailnet.
</div>
`

var setupFormTmpl = template.Must(template.New("form").Parse(setupHead + `
<form class="card" method="post" action="/setup">
  <h1>Set up hush over your tailnet</h1>
  <p class="sub">Paste a Tailscale auth key to join this box to your tailnet and
    serve the console over HTTPS. Nothing here is stored on disk — the key only
    provisions the node.</p>
  {{if .Error}}<div class="err">{{.Error}}</div>{{end}}
  <label for="authkey">Tailscale auth key</label>
  <input id="authkey" name="authkey" type="password" autocomplete="off"
         autocapitalize="off" spellcheck="false" placeholder="tskey-auth-…" required>
  <small>Create one at
    <a href="https://login.tailscale.com/admin/settings/keys" target="_blank" rel="noreferrer">the Tailscale admin console</a>.</small>
  <label for="hostname">Node hostname</label>
  <input id="hostname" name="hostname" type="text" autocapitalize="off"
         spellcheck="false" value="{{.Hostname}}">
  <small>Sets your URL: <code>{{.Hostname}}</code> → <code>https://{{.Hostname}}.&lt;tailnet&gt;.ts.net</code></small>
  <button type="submit">Join tailnet &amp; start console</button>
</form>
`))

var setupSuccessTmpl = template.Must(template.New("success").Parse(setupHead + `
<div class="card">
  <h1>You're on the tailnet 🎉</h1>
  <p class="sub">This setup page is now shutting down. Open the secure console at:</p>
  <p><a href="{{.URL}}">{{.URL}}</a></p>
  <p class="sub">It's served over HTTPS and gated by your Tailscale identity. You
    can bookmark that link — this LAN page won't come back.</p>
</div>
`))

var setupDoneTmpl = template.Must(template.New("done").Parse(setupHead + `
<div class="card">
  <h1>Setup already completed</h1>
  <p class="sub">This node has joined the tailnet. Open the secure console at its
    <code>https://&lt;hostname&gt;.&lt;tailnet&gt;.ts.net</code> URL — this LAN page
    is shutting down.</p>
</div>
`))
