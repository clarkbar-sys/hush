// The payphone app (web/src/apps/payphone) is the console's AOL-era buddy list:
// clicking a fleet model opens a classic IM window that streams a real
// completion through the chat proxy (see llmchat.go). This file gives those
// conversations a memory. A "session" is one saved transcript — which box,
// which model, and the turns so far — persisted on control so every browser on
// the tailnet sees the same list of active chats, exactly the way the launcher
// tile order (launcher.go) is a single global arrangement everyone shares.
//
// It stays inside hush's read-only spirit the same way the launcher order does:
// a session is cosmetic, rebuildable state that lives on control, never on a
// fleet box. The chat proxy itself remains non-persistent — it relays tokens and
// forgets them; the transcript the browser keeps is what gets mirrored up here,
// so losing payphone.json just means the buddy list starts with no saved chats.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"sync"

	"github.com/clarkbar-sys/hush/internal/store"
)

// Caps keep a malformed or hostile PUT from growing payphone.json without
// bound. A real homelab has a handful of chats going; these ceilings are far
// above any honest use and still trivially small on disk.
const (
	maxSessions     = 200      // total saved sessions; oldest pruned past this
	maxSessionMsgs  = 500      // turns kept per session
	maxSessionBody  = 1 << 20  // 1 MiB — a transcript, not an upload
	maxSessionField = 256      // host / model / kind
	maxSessionTitle = 160      // the buddy-list label
	maxMessageText  = 32 << 10 // 32 KiB per turn
)

// sessionID constrains a session id to a short, filesystem- and DOM-safe slug so
// a PUT can't smuggle a path or a payload in through the URL. The client mints
// ids from a timestamp plus a little randomness, which fit comfortably.
var sessionID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// chatMessage is one turn in a saved transcript. Role is the OpenAI-shaped
// "user" or "assistant"; the client maps its "me"/"them" lines onto these so a
// resumed session replays in the right voices.
type chatMessage struct {
	Role string `json:"role"`
	Text string `json:"text"`
}

// chatSession is one saved payphone conversation: which box and model it's with,
// a human label, when it started and last moved, and the turns so far. Started
// is preserved across upserts so a chat keeps its age as it grows.
type chatSession struct {
	ID       string        `json:"id"`
	Host     string        `json:"host"`
	Model    string        `json:"model"`
	Kind     string        `json:"kind,omitempty"`
	Title    string        `json:"title"`
	Started  int64         `json:"started"`
	Updated  int64         `json:"updated"`
	Messages []chatMessage `json:"messages"`
}

// payphoneStore persists the saved sessions — the one global list every browser
// sees — to payphone.json beside the fleet config. Like launcherLayout it rides
// internal/store's tolerant load and crash-safe atomic save: a missing or
// corrupt file just means "no saved chats yet." Every mutation advances the
// in-memory copy only after the write succeeds, so a failed save leaves the
// store exactly as it was.
type payphoneStore struct {
	mu       sync.Mutex
	path     string
	sessions []chatSession
}

// newPayphoneStore loads path into a store, tolerating a missing or unreadable
// file by starting with no saved sessions (see store.Load).
func newPayphoneStore(path string) *payphoneStore {
	return &payphoneStore{path: path, sessions: store.Load[chatSession](path, "payphone sessions")}
}

// list returns a copy of the saved sessions, newest activity first, so the buddy
// list shows the freshest chats at the top.
func (p *payphoneStore) list() []chatSession {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]chatSession, len(p.sessions))
	copy(out, p.sessions)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Updated > out[j].Updated })
	return out
}

// upsert stores s, replacing an existing session with the same id or appending a
// new one, then prunes back to the newest maxSessions. A replace preserves the
// original Started so a growing chat keeps its age even when the client omits it.
func (p *payphoneStore) upsert(s chatSession) (chatSession, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	next := append([]chatSession(nil), p.sessions...)
	found := false
	for i := range next {
		if next[i].ID == s.ID {
			if s.Started == 0 {
				s.Started = next[i].Started
			}
			next[i] = s
			found = true
			break
		}
	}
	if !found {
		if s.Started == 0 {
			s.Started = s.Updated
		}
		next = append(next, s)
	}
	next = pruneSessions(next)
	if err := store.Save(p.path, next); err != nil {
		return chatSession{}, err
	}
	p.sessions = next
	return s, nil
}

// remove deletes the session with id and persists the result, reporting whether
// anything was removed so a handler can answer 404 for an unknown id.
func (p *payphoneStore) remove(id string) (bool, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	kept := make([]chatSession, 0, len(p.sessions))
	for _, s := range p.sessions {
		if s.ID != id {
			kept = append(kept, s)
		}
	}
	if len(kept) == len(p.sessions) {
		return false, nil
	}
	if err := store.Save(p.path, kept); err != nil {
		return false, err
	}
	p.sessions = kept
	return true, nil
}

// pruneSessions caps the store at the newest maxSessions by last activity, so an
// old chat falls off the end rather than letting the file grow forever.
func pruneSessions(ss []chatSession) []chatSession {
	if len(ss) <= maxSessions {
		return ss
	}
	sorted := append([]chatSession(nil), ss...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Updated > sorted[j].Updated })
	return append([]chatSession(nil), sorted[:maxSessions]...)
}

// payphoneStorePath places payphone.json beside the fleet config, in the same
// writable directory the launcher order and fleet membership already live in —
// the systemd unit's ReadWritePaths covers it, so no new path needs granting.
func payphoneStorePath(fleetConfig string) string {
	return filepath.Join(filepath.Dir(fleetConfig), "payphone.json")
}

// sanitizeSession validates and clamps a submitted session so a bad or hostile
// PUT can't wedge the store: the id must be a slug, host/model must be present
// and bounded, roles must be the two OpenAI values, and the over-long fields are
// trimmed rather than rejected (a chat shouldn't fail to save because a title ran
// long). It returns the cleaned copy or an error describing the first problem.
func sanitizeSession(s chatSession) (chatSession, error) {
	if !sessionID.MatchString(s.ID) {
		return chatSession{}, fmt.Errorf("invalid session id")
	}
	if s.Host == "" || len(s.Host) > maxSessionField {
		return chatSession{}, fmt.Errorf("invalid host")
	}
	if s.Model == "" || len(s.Model) > maxSessionField {
		return chatSession{}, fmt.Errorf("invalid model")
	}
	if len(s.Kind) > maxSessionField {
		return chatSession{}, fmt.Errorf("invalid kind")
	}
	if len(s.Messages) > maxSessionMsgs {
		return chatSession{}, fmt.Errorf("too many messages (max %d)", maxSessionMsgs)
	}
	if len(s.Title) > maxSessionTitle {
		s.Title = s.Title[:maxSessionTitle]
	}
	for i, m := range s.Messages {
		if m.Role != "user" && m.Role != "assistant" {
			return chatSession{}, fmt.Errorf("invalid message role %q", m.Role)
		}
		if len(m.Text) > maxMessageText {
			s.Messages[i].Text = m.Text[:maxMessageText]
		}
	}
	if s.Messages == nil {
		s.Messages = []chatMessage{}
	}
	return s, nil
}

// handlePayphoneSessions serves the saved-session list: GET returns every stored
// chat, newest first, in a {"sessions": [...]} envelope. It's the read the buddy
// list polls so a chat someone else started on the tailnet shows up here too.
func handlePayphoneSessions(ps *payphoneStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		writeSessions(w, ps.list())
	}
}

// handlePayphoneSession serves one session by id: PUT upserts it (the browser
// mirrors a conversation up after each turn), DELETE forgets it. The id in the
// path is authoritative — a body id must match it, or be omitted and filled in —
// so a session can't be written under a different key than it's addressed by.
func handlePayphoneSession(ps *payphoneStore) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		switch r.Method {
		case http.MethodPut:
			raw, err := io.ReadAll(io.LimitReader(r.Body, maxSessionBody))
			if err != nil {
				http.Error(w, "read body", http.StatusBadRequest)
				return
			}
			var s chatSession
			if err := json.Unmarshal(raw, &s); err != nil {
				http.Error(w, "body must be a JSON session object", http.StatusBadRequest)
				return
			}
			if s.ID == "" {
				s.ID = id
			}
			if s.ID != id {
				http.Error(w, "session id in body does not match the URL", http.StatusBadRequest)
				return
			}
			clean, err := sanitizeSession(s)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			stored, err := ps.upsert(clean)
			if err != nil {
				log.Printf("save payphone session: %v", err)
				http.Error(w, "could not save session", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			if err := json.NewEncoder(w).Encode(stored); err != nil {
				log.Printf("encode payphone session: %v", err)
			}
		case http.MethodDelete:
			ok, err := ps.remove(id)
			if err != nil {
				log.Printf("delete payphone session: %v", err)
				http.Error(w, "could not delete session", http.StatusInternalServerError)
				return
			}
			if !ok {
				http.Error(w, "unknown session", http.StatusNotFound)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}
}

// writeSessions encodes the {"sessions": [...]} envelope, normalizing a nil
// slice to [] so the client always gets a JSON array rather than null.
func writeSessions(w http.ResponseWriter, ss []chatSession) {
	if ss == nil {
		ss = []chatSession{}
	}
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(map[string][]chatSession{"sessions": ss}); err != nil {
		log.Printf("encode payphone sessions: %v", err)
	}
}
