# hush — sessions (remote coding agents)

> Spawn opencode or claude on any box in the tailnet, from your phone — the hush
> way: hush hands you the command, you run it.

A **Session** is a coding agent — [opencode](https://opencode.ai) or
[Claude Code](https://www.anthropic.com/claude-code) — running on a box in the
fleet, in a `tmux` session you attach to from your phone (JuiceSSH). The point
is reach: the fleet already has a box with a GPU serving models on the tailnet
(see [Local inference](./DESIGN.md#local-inference--capability-and-reach)), and
this is how you sit an agent down in front of it from wherever you are.

## Principle — hush watches, you run

hush does not spawn a session and does not stop one. It **composes the command**
and hands it to you to paste into a root shell on the box; the agent then
**reads** which coding agents are running and shows them. Privilege flows one
way (your SSH session, as root, on the box), information the other (the agent's
read-only `/proc` scan) — exactly the posture the
[backup convention](./BACKUP-CONVENTION.md) uses, where hush reports restic
status but backups are set up and restored on the box over SSH.

**This is not the removed "run things" half.** hush once shipped ad-hoc and
scheduled command *execution* (Tasks, Jobs, Workflows, a `/exec` endpoint) and
cut all of it — the console was a write path onto every box. Sessions do not
bring that back, because **hush-control still executes nothing**. The write is a
command *you* run, in a shell *you* opened, authenticated by *your* SSH
identity. The console's whole contribution is composing a correct command and
visualising the result. There is no new endpoint that runs anything, on control
or agent.

## What you see — the Sessions section

Every Machine view grows a **Sessions** section. It opens with a **tools strip**
— one row per known coding agent (opencode, claude) showing whether it's
**installed system-wide** on the box and where its binary lives, with an
**Install** (absent) or **Update** (present) button that composes the one
command to fix it. Below the strip is the list of agents **running now**: the
tool, the user it runs as, how long it's been up, and its command line. Each
running session has a **Stop** button; a **＋ Spawn a session** button composes a
new launch command. It all reads one agent endpoint, `/sessions`.

## Installed vs running — two reads, one endpoint

The Sessions section answers two questions from the same `/sessions` snapshot:

- **What's installed here?** — the tools strip, from the snapshot's `installed`
  list. Presence only: the agent *looks the binary up* on the box's search list
  (its `PATH` plus the usual system bin dirs) but **never runs it**, so this
  stays as read-only and privilege-free as the `/proc` scan. A tool only shows
  as installed when it lives somewhere the unprivileged `hush` user can see —
  which is precisely a **system-wide** install (e.g. `/usr/local/bin`), not a
  copy tucked inside another user's home. That's the point of installing to the
  system: one copy serves every user *and* the agent can report it.
- **What's running here?** — the session list, from the `/proc` scan below.

## The agent — `/sessions`, a read-only `/proc` scan

`GET /sessions` returns the box's running coding agents:

```json
{
  "host": "citadel",
  "match": ["opencode", "claude"],
  "sessions": [
    { "pid": 48213, "user": "josh", "tool": "opencode", "cmd": "opencode", "uptime": 5400, "started": 1721470000 }
  ],
  "installed": [
    { "tool": "opencode", "present": true, "path": "/usr/local/bin/opencode" },
    { "tool": "claude", "present": false }
  ]
}
```

The `installed` list is presence only — `present` and, when found, the `path`
that answered. There's no version field: reporting a version would mean *running*
`opencode --version`, and the agent deliberately never executes anything (see the
package doc). The console therefore doesn't distinguish "outdated" from
"installed" — it always offers **Update**, since the install command is
idempotent and re-running it updates in place. `installed` is populated from the
same `match` set as sessions, so clearing `-session-procs` omits it entirely
(the console reads the absent field as "this agent doesn't report", never as
"nothing installed"). An agent old enough to serve `/sessions` but predating this
field simply omits it, and the tools strip stays hidden.

It is discovered the same way `/top` reads the process table: for each entry in
`/proc`, the agent reads the world-readable `cmdline` and the directory's owning
uid, and counts a process as a session when its program name (`comm`, or the
base name of `argv[0]`) is one of the configured set. **No privilege**, no
inference credential, no session state held anywhere — `/sessions` is as ungated
and read-only as `/vitals` and `/top`, and `hush-agent` stays the unprivileged
`hush` user.

Detection keys on the **program name**, not on a hush-planted marker, and that's
deliberate: hush can't read another user's environment or `tmux` socket to find
such a marker without *being* that user, and "which coding agent is running
here, owned by whom" is the honest, privilege-free answer. A consequence worth
knowing: hush reports *every* matching process, not only the ones it composed a
command for — a plain `opencode` you started by hand shows up too, which is
usually what you want from a fleet console.

Configure the set with `-session-procs` (default `opencode,claude`,
comma-separated, matched against `argv[0]`/`comm`). Clearing it disables session
reporting — the section reads that as "this agent doesn't report", never as
"this box has none", the same contract the LLM flags use.

**Deployment caveat — `hidepid`.** Detection relies on other users' processes
being visible in `/proc`. If a box mounts `/proc` with `hidepid=2`, the
unprivileged `hush` user only sees its own processes and a session owned by
another user won't appear. That's the OS boundary doing exactly what it's
configured to; widen it (a `gid=` on the `proc` mount whose group `hush` is in)
if you want cross-user visibility, the same way file browsing is widened by
group membership rather than by editing hush.

`hush-control` proxies the endpoint at `/api/machines/{host}/sessions`, verbatim
like `/top` — the phone can't address agents directly in tsnet mode, so the
session list rides through control like every other per-machine read. An agent
too old to serve `/sessions` answers `404`, relayed as-is so the console says
"update the agent" rather than showing an empty section.

## Installing — one `sudo` command, once per box

Install is now a **separate, one-time, system-wide** action, not something the
spawn command does on every launch. The tools strip composes it: a single
idempotent line that installs into npm's global prefix (usually `/usr/local/bin`,
on every user's `PATH` and visible to the `hush` agent). For opencode:

```bash
sudo npm install -g opencode-ai@latest
```

and for claude, `sudo npm install -g @anthropic-ai/claude-code@latest`. The same
line is **Install** when the tool is absent and **Update** when it's present —
`@latest` makes it idempotent, so there's no separate update path and no version
check to maintain. It needs Node/npm on the box; if that's missing the command
says so and you install Node first — the same "fails loudly on its own" posture
as a missing user in a spawn command.

Why npm-global rather than each tool's own `curl … | install` one-liner: the
official opencode and claude installers hardcode a **per-user** home target
(`~/.opencode/bin`, `~/.local/bin`) with no system-prefix override, so running
them as root just installs into `/root`'s home — still invisible to other users
and to the agent. `npm install -g` is the portable way to land one shared binary
in `/usr/local/bin`.

## Spawning — one `sudo` command you paste

The **Spawn** sheet composes a single `sudo` command. You pick the **user** to
run as and the **tool**; for opencode you also pick which of the fleet's
tailnet-reachable LLM boxes to point it at. It assumes the tool is already
installed system-wide (above) — so it no longer self-installs, and just runs the
binary that's on `PATH`. The command, for `opencode` run as `josh` pointed at
`citadel`'s runtime:

```bash
sudo -u josh -H bash -lc '
  mkdir -p "$HOME/.config/opencode" &&
  printf %s "<base64 opencode.json>" | base64 -d > "$HOME/.config/opencode/opencode.json" &&
  exec tmux new-session -A -s hush-opencode opencode'
```

Every part maps to a step of the workflow:

- **Pick a user; fail if it doesn't exist.** `sudo -u josh` *is* the whole
  preflight — if `josh` isn't a user on the box, the command fails on its own,
  loudly, before doing anything. hush doesn't need to check first.
- **Assumes the tool is installed system-wide.** No `command -v … || <installer>`
  line: install is a one-time system action from the tools strip, so the binary
  is already on `PATH`. If it isn't, the `exec` fails on its own the same honest
  way — and the Spawn sheet warns first when the agent has reported the tool
  absent on this box.
- **Refresh opencode's config from hush.** The chosen LLM box's `opencode.json`
  (the same one the [Inference section exports](./DESIGN.md#local-inference--capability-and-reach)
  — an OpenAI-compatible provider at the runtime's tailnet `baseURL`, its served
  models, cost pinned to zero) is written to `~/.config/opencode/opencode.json`.
  It's **base64-piped** rather than heredoc'd so the JSON's own quotes can never
  collide with the command's `sudo … bash -lc '…'` quoting — the one part of
  this that's easy to get subtly wrong by hand.
- **Spawn a tmux session you can attach to.** `tmux new-session -A -s
  hush-opencode` attaches if the session exists and creates it otherwise, so the
  command is idempotent: paste it again from a new phone and you reattach rather
  than stacking sessions. `exec` hands the shell to tmux, so when the agent
  exits the session is gone. Detach with `Ctrl-b d` and it keeps running — hush
  will still show it, and you reattach by re-running the command.

claude uses its own Anthropic login (run `claude` once in the session to sign
in); routing claude at a hush-net endpoint is a follow-up (below). The command
is one blob you copy into JuiceSSH — the `&&` chain stops at the first failed
step, so a failed config write never launches a half-configured agent (and a
tool that was never installed system-wide fails cleanly at the final `exec`).

**claude launches with `--remote-control`.** The session you get in tmux over
JuiceSSH is the same interactive session as before, but it's also steerable
from [claude.ai/code](https://claude.ai/code) or the Claude mobile app once
you've signed in — pick up the conversation from your phone the way you'd
reattach the tmux session from another terminal. This degrades the same way
the rest of the command does: an account that hasn't run `/login` yet just
gets a plain local session with a Remote Control failure notice, not a failed
chain. hush doesn't hold a credential for this — the sign-in happens on the
box, same as the Anthropic login above.

**claude also launches with `--dangerously-skip-permissions`.** `sudo -u
<user>` is already the permission gate — the operator picked the box and the
user before pasting the command — so a second per-tool-call prompt inside the
session is friction, not extra safety.

The command also sets `CLAUDE_REMOTE_CONTROL_SESSION_NAME_PREFIX` to the fleet
machine's hush name, so the session's auto-generated title follows Claude
Code's own convention — `<prefix>-adjective-noun` (its docs' own example is
`myhost-graceful-unicorn`) — prefixed with the name hush knows the box by
rather than its raw OS hostname, which isn't always the same thing. Send a
message and the title updates to reflect it, or `/rename` it yourself.

## Stopping — another command you paste

Stop is the same shape. The **Stop** button opens the command for the process
hush detected:

```bash
sudo -u josh kill 48213
```

hush never sends the signal — it hands you the line. Ending the agent closes its
`tmux` session with it (the `exec`'d process was the session's only window), and
it drops off the Sessions list on the next refresh.

## Permissions — the open question

Everything above deliberately needs **no new authorization model**, because
hush-control never acts: the operator's own `sudo` on the box is the only
privilege in play, and it's the box's `sudoers` — not hush — that decides who
may run what. That's the right v1: it ships real capability without hush growing
a write path or a secret.

What's genuinely open, and worth deciding before widening this, is **who inside
the console may compose these commands**. Today anyone who can reach the console
(a tailnet member, narrowed by the optional `-allow` login allowlist) can
compose a spawn command for any user on any box — but composing it is not
running it; it still dies at the box's `sudo` gate unless they can already
authenticate there. If a future slice ever has hush *run* a session (rather than
hand you the command), that is the moment this needs a real permission model,
and it should be designed then, against that concrete capability, not
speculatively now.

## Roadmap (the sub-issues of #145)

This is the first slice of the epic in
[#145](https://github.com/clarkbar-sys/hush/issues/145). Natural follow-ups,
each small on top of this substrate:

- **Route claude at a hush-net endpoint.** opencode speaks OpenAI-compatible and
  points straight at a local runtime; claude expects the Anthropic API shape, so
  pointing it at a local model wants a translating proxy (or claude's own
  provider config once it supports one). Until then claude runs against its own
  API.
- **Named / per-project sessions.** One `hush-<tool>` session per box is
  idempotent but coarse. A session name (project, worktree) would let several
  run side by side and stop precisely by name rather than by pid.
- **Richer tracking.** `/proc` gives the process and its owner; the working
  directory (`/proc/<pid>/cwd`, readable for own-user processes) and the `tmux`
  session name would let the console show *what* each agent is working on, not
  just that it's running.
- **Out-of-band notice.** A session that's been idle for hours, or one that
  exited non-zero, is the kind of thing the alert bell already knows how to
  surface.
