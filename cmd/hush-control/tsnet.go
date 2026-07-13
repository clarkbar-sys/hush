// tsnet mode: hush-control joins the tailnet as its own node (name e.g. "hush")
// and serves the console over HTTPS on :443 using tsnet's auto TLS. This makes
// the console reachable at https://<hostname>.<tailnet>.ts.net with a real cert
// and no keys to manage — the tailnet provides reachability and identity.
//
// Prerequisites in the tailnet: MagicDNS + HTTPS certificates enabled. The node
// is served tailnet-only; hush never uses Tailscale Funnel (no public exposure).
//
// First run: when no auth key is supplied and the node has no saved state yet,
// serveTsnet hands off to the LAN setup page (see setup.go) instead of failing —
// the operator provisions the node from a browser, and this same process then
// switches the console onto the tailnet HTTPS URL. See needsSetup.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"tailscale.com/tsnet"
)

// serveTsnet brings up a tsnet node and serves mux over HTTPS on :443, gating
// every request on Tailscale identity. It blocks (log.Fatal) until the server
// exits, mirroring the LAN path in main.
//
// The node is provisioned one of two ways: from TS_AUTHKEY (or existing saved
// state), or — when neither is present — interactively via the first-run setup
// page bound to listen (the -listen address). Either way we end up with a live
// *tsnet.Server that serveConsoleOverTsnet then serves the console over.
func serveTsnet(mux http.Handler, disco *discoverySource, listen, hostname, stateDir string, allow []string) {
	authKey := os.Getenv("TS_AUTHKEY")

	var srv *tsnet.Server
	if needsSetup(authKey, stateDir) {
		log.Printf("hush-control: unprovisioned — first-run setup on %s (LAN, plain HTTP)", listen)
		srv = runSetup(listen, hostname, stateDir)
	} else {
		srv = &tsnet.Server{Hostname: hostname, AuthKey: authKey}
		if stateDir != "" {
			srv.Dir = stateDir
		}
		if _, err := srv.Up(context.Background()); err != nil {
			log.Fatalf("tsnet: bringing node up: %v", err)
		}
	}
	defer srv.Close()

	serveConsoleOverTsnet(srv, disco, mux, allow)
}

// serveConsoleOverTsnet serves mux over HTTPS on :443 of an already-up tsnet
// node, gating every request on Tailscale identity. It blocks (log.Fatal) until
// the server exits.
func serveConsoleOverTsnet(srv *tsnet.Server, disco *discoverySource, mux http.Handler, allow []string) {
	lc, err := srv.LocalClient()
	if err != nil {
		log.Fatalf("tsnet: local client: %v", err)
	}

	// The tailnet is now reachable through lc, so /api/discover can enumerate it.
	if disco != nil {
		disco.set(tsnetPeerLister{lc: lc})
	}

	ln, err := srv.ListenTLS("tcp", ":443")
	if err != nil {
		log.Fatalf("tsnet: listen :443: %v", err)
	}
	defer ln.Close()

	// whois resolves a request's remote addr to its tailnet login name.
	whois := func(ctx context.Context, remoteAddr string) (string, error) {
		who, err := lc.WhoIs(ctx, remoteAddr)
		if err != nil {
			return "", err
		}
		if who.UserProfile == nil {
			return "", nil
		}
		return who.UserProfile.LoginName, nil
	}

	log.Printf("hush-control serving over tailnet as %q on :443 (HTTPS)", srv.Hostname)
	if len(allow) > 0 {
		log.Printf("hush-control: allowlist = %s", strings.Join(allow, ", "))
	} else {
		log.Printf("hush-control: any tailnet member may connect (no allowlist)")
	}
	log.Fatal(http.Serve(ln, identityGate(whois, allow, mux)))
}

// needsSetup reports whether tsnet mode should launch the first-run web setup:
// true only when no auth key was supplied and the node has no persisted state
// to come up from. Once either is present, the node provisions non-interactively.
func needsSetup(authKey, stateDir string) bool {
	if authKey != "" {
		return false
	}
	return !hasTsnetState(stateDir)
}

// hasTsnetState reports whether a tsnet node has already been provisioned, i.e.
// tsnet has written its state file. When stateDir is empty, tsnet defaults to
// <UserConfigDir>/tsnet-<prog>; we mirror that here (see tsnet.Server.Start) so
// detection matches wherever the node would actually persist its state.
func hasTsnetState(stateDir string) bool {
	dir := stateDir
	if dir == "" {
		dir = defaultTsnetDir()
	}
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "tailscaled.state"))
	return err == nil
}

// defaultTsnetDir mirrors tsnet's own default state directory,
// <UserConfigDir>/tsnet-<prog>, where prog is the lowercased executable name.
// Returns "" if the config dir or executable path can't be determined, in which
// case the caller treats the node as unprovisioned.
func defaultTsnetDir() string {
	conf, err := os.UserConfigDir()
	if err != nil {
		return ""
	}
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	prog := strings.TrimSuffix(strings.ToLower(filepath.Base(exe)), ".exe")
	return filepath.Join(conf, "tsnet-"+prog)
}

// callerCtxKey carries the resolved caller login on the request context.
type callerCtxKey struct{}

// callerFrom returns the Tailscale login identityGate attached to ctx, or "" in
// LAN mode where requests carry no per-request identity. Handlers use it to
// audit mutating actions (who ran what).
func callerFrom(ctx context.Context) string {
	s, _ := ctx.Value(callerCtxKey{}).(string)
	return s
}

// identityGate authenticates each request by Tailscale identity. It rejects
// callers with no tailnet identity, and — when allow is non-empty — callers
// whose login is not on the allowlist. The resolved login is attached to the
// request context for downstream handlers.
func identityGate(whois func(context.Context, string) (string, error), allow []string, next http.Handler) http.Handler {
	allowed := make(map[string]bool, len(allow))
	for _, a := range allow {
		allowed[strings.ToLower(a)] = true
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		login, err := whois(r.Context(), r.RemoteAddr)
		if err != nil {
			http.Error(w, "forbidden: no tailnet identity", http.StatusForbidden)
			return
		}
		if len(allowed) > 0 && !allowed[strings.ToLower(login)] {
			http.Error(w, "forbidden: not on the allowlist", http.StatusForbidden)
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), callerCtxKey{}, login))
		next.ServeHTTP(w, r)
	})
}
