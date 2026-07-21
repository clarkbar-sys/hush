// Users lists the human login accounts on a box, so the console can show who
// could run a session here and offer to create another — the read half of the
// same "Add a user" construct whose write half (useradd, composed by the
// console) never touches hush; see the package doc and docs/SESSIONS.md.
//
// Detection reads /etc/passwd, world-readable on every mainstream Linux, so
// like session detection it needs no privilege and holds no state.

package sessions

import (
	"os"
	"sort"
	"strconv"
	"strings"
)

// SystemUser is one human login account on a box, as reported by /etc/passwd.
type SystemUser struct {
	Name  string `json:"name"`
	UID   int    `json:"uid"`
	Home  string `json:"home,omitempty"`
	Shell string `json:"shell,omitempty"`
}

// UsersSnapshot is a single reading of a box's human user accounts, served by
// the agent's /users endpoint.
type UsersSnapshot struct {
	Host  string       `json:"host"`
	Users []SystemUser `json:"users"`
}

// minHumanUID is the lowest uid a login account gets on the distros hush
// targets (Debian, Ubuntu, Fedora — see scripts/install.sh): below it sits the
// range `useradd --system` hands out, which is how hush's own service account
// is created. Filtering on it keeps hush's own account, and every other daemon
// account, out of a list meant to answer "who could I hand this box to".
const minHumanUID = 1000

// nonLoginShells mark an account as not meant for interactive login regardless
// of its uid — belt and suspenders alongside the uid floor above, since some
// distros assign service accounts a uid past 1000 too.
var nonLoginShells = map[string]struct{}{
	"/usr/sbin/nologin": {},
	"/sbin/nologin":     {},
	"/bin/false":        {},
	"/usr/bin/false":    {},
	"":                  {},
}

// CollectUsers reads this host's /etc/passwd and returns the human accounts on
// it, alongside the hostname, for the agent's /users endpoint.
func CollectUsers() UsersSnapshot {
	host, _ := os.Hostname()
	return UsersSnapshot{Host: host, Users: ListUsers("/etc/passwd")}
}

// ListUsers parses passwdPath (the standard "name:x:uid:gid:gecos:home:shell"
// colon-separated format) and returns the accounts that look like real human
// logins rather than system or service accounts: uid at or above
// minHumanUID, with a shell that allows interactive login. Sorted by name so
// the console's list is stable across polls. A missing or unreadable file
// yields nil rather than an error, the same "absent means unknown" contract
// Collect uses for sessions.
func ListUsers(passwdPath string) []SystemUser {
	data, err := os.ReadFile(passwdPath)
	if err != nil {
		return nil
	}
	var out []SystemUser
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, ":")
		if len(f) < 7 {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < minHumanUID {
			continue
		}
		shell := f[6]
		if _, skip := nonLoginShells[shell]; skip {
			continue
		}
		out = append(out, SystemUser{Name: f[0], UID: uid, Home: f[5], Shell: shell})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
