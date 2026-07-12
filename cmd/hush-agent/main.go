// Command hush-agent runs on every machine in the fleet. It exposes the host's
// vitals as JSON over the tailnet. In Phase 0 it is read-only: no endpoint
// changes anything on the box.
//
// Deploy is one static binary with no runtime dependencies:
//
//	GOOS=linux GOARCH=arm64 go build ./cmd/hush-agent   # e.g. for the Pi
//	scp hush-agent pi-gate:/usr/local/bin/
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/clarkbar-sys/hush/internal/version"
	"github.com/clarkbar-sys/hush/internal/vitals"
)

func main() {
	listen := flag.String("listen", ":8765", "address to listen on (bind to the tailnet interface in production)")
	showVersion := flag.Bool("version", false, "print the hush-agent version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Printf("hush-agent %s\n", version.Current())
		os.Exit(0)
	}

	vitals.StartSampler()

	mux := http.NewServeMux()
	mux.HandleFunc("/vitals", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(vitals.Collect()); err != nil {
			log.Printf("encode vitals: %v", err)
		}
	})
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	})

	log.Printf("hush-agent listening on %s", *listen)
	log.Fatal(http.ListenAndServe(*listen, mux))
}
