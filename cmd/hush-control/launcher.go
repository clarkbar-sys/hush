package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/clarkbar-sys/hush/internal/store"
)

// launcherLayout persists the console's launcher tile order — the single global
// arrangement every browser sees. The order is cosmetic, rebuildable state (the
// UI ships a sensible default baked into its markup), so it rides internal/store's
// tolerant load and crash-safe atomic save rather than anything heavier: a
// missing or corrupt file just means "no saved order yet, fall back to the
// default." The stored value is the ordered list of tile ids; its slice position
// is the order, so a PUT replaces it wholesale.
type launcherLayout struct {
	mu    sync.RWMutex
	path  string
	order []string
}

// newLauncherLayout loads path into a layout, tolerating a missing or unreadable
// file by starting with no saved order (see store.Load).
func newLauncherLayout(path string) *launcherLayout {
	return &launcherLayout{path: path, order: store.Load[string](path, "launcher layout")}
}

// get returns a copy of the saved order, safe to read without the lock.
func (l *launcherLayout) get() []string {
	l.mu.RLock()
	defer l.mu.RUnlock()
	out := make([]string, len(l.order))
	copy(out, l.order)
	return out
}

// set persists a new order, advancing the in-memory copy only after the write
// succeeds so a failed save leaves the layout exactly as it was.
func (l *launcherLayout) set(order []string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if err := store.Save(l.path, order); err != nil {
		return err
	}
	l.order = order
	return nil
}

// launcherLayoutPath places launcher.json beside the fleet config so the tile
// order lives in the same writable config directory the fleet membership does —
// the systemd unit's ReadWritePaths already covers it, so no new path needs
// granting.
func launcherLayoutPath(fleetConfig string) string {
	return filepath.Join(filepath.Dir(fleetConfig), "launcher.json")
}

// maxLauncherTiles caps a submitted order so a malformed or hostile PUT can't
// grow the file without bound. The console ships a handful of tiles; 64 is far
// above any real launcher and still trivially small on disk.
const maxLauncherTiles = 64

// tileID constrains a tile id to a short, filesystem- and DOM-safe slug so the
// stored order can't smuggle in arbitrary payloads. The UI's ids (fleet,
// payphone, github, …) all fit comfortably.
var tileID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)

// sanitizeOrder validates a submitted tile order: every id must match tileID,
// none may repeat, and the list may not exceed maxLauncherTiles. It deliberately
// does not check the ids against a known tile set — the console is the authority
// on which tiles exist, so the server stores whatever ordering hint it's handed
// and lets the client reconcile it against the tiles actually present. That
// keeps adding a tile in the UI from needing a server change.
func sanitizeOrder(order []string) ([]string, error) {
	if len(order) > maxLauncherTiles {
		return nil, fmt.Errorf("too many tiles (max %d)", maxLauncherTiles)
	}
	seen := make(map[string]bool, len(order))
	out := make([]string, 0, len(order))
	for _, id := range order {
		if !tileID.MatchString(id) {
			return nil, fmt.Errorf("invalid tile id %q", id)
		}
		if seen[id] {
			return nil, fmt.Errorf("duplicate tile id %q", id)
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// handleLauncherLayout serves the global launcher order: GET returns the saved
// arrangement (empty when none has been saved, so the client uses its default),
// PUT replaces it. The response envelope is {"order": [...]} both ways.
func handleLauncherLayout(layout *launcherLayout) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			writeLayout(w, layout.get())
		case http.MethodPut:
			var req struct {
				Order []string `json:"order"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request body", http.StatusBadRequest)
				return
			}
			order, err := sanitizeOrder(req.Order)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if err := layout.set(order); err != nil {
				log.Printf("save launcher layout: %v", err)
				http.Error(w, "could not save layout", http.StatusInternalServerError)
				return
			}
			writeLayout(w, order)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// writeLayout encodes the {"order": [...]} envelope, normalizing a nil slice to
// [] so the client always gets a JSON array rather than null.
func writeLayout(w http.ResponseWriter, order []string) {
	if order == nil {
		order = []string{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]string{"order": order}); err != nil {
		log.Printf("encode launcher layout: %v", err)
	}
}
