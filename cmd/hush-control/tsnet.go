// tsnet mode: hush-control joins the tailnet as its own node (name e.g. "hush")
// and serves the console over HTTPS on :443 using tsnet's auto TLS. This makes
// the console reachable at https://<hostname>.<tailnet>.ts.net with a real cert
// and no keys to manage — the tailnet provides reachability and identity.
//
// Prerequisites in the tailnet: MagicDNS + HTTPS certificates enabled. The node
// is served tailnet-only; hush never uses Tailscale Funnel (no public exposure).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"strings"

	"tailscale.com/tsnet"
)

// serveTsnet brings up a tsnet node and serves mux over HTTPS on :443, gating
// every request on Tailscale identity. It blocks (log.Fatal) until the server
// exits, mirroring the LAN path in main.
func serveTsnet(mux http.Handler, hostname, stateDir string, allow []string) {
	authKey := os.Getenv("TS_AUTHKEY")
	if authKey == "" {
		log.Fatal("tsnet mode requires an auth key in TS_AUTHKEY")
	}

	srv := &tsnet.Server{
		Hostname: hostname,
		AuthKey:  authKey,
	}
	if stateDir != "" {
		srv.Dir = stateDir
	}
	defer srv.Close()

	// Bring the node onto the tailnet before we need its identity API.
	if _, err := srv.Up(context.Background()); err != nil {
		log.Fatalf("tsnet: bringing node up: %v", err)
	}
	lc, err := srv.LocalClient()
	if err != nil {
		log.Fatalf("tsnet: local client: %v", err)
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

	log.Printf("hush-control serving over tailnet as %q on :443 (HTTPS)", hostname)
	if len(allow) > 0 {
		log.Printf("hush-control: allowlist = %s", strings.Join(allow, ", "))
	} else {
		log.Printf("hush-control: any tailnet member may connect (no allowlist)")
	}
	log.Fatal(http.Serve(ln, identityGate(whois, allow, mux)))
}

// callerCtxKey carries the resolved caller login on the request context.
type callerCtxKey struct{}

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
