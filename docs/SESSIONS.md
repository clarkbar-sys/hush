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

Every Machine view grows a **Sessions** section listing the coding agents
running on that box: the tool, the user it runs as, how long it's been up, and
its command line. Each has a **Stop** button; a **＋ Spawn a session** button
composes a new launch command. It reads a new agent endpoint, `/sessions`.

## The agent — `/sessions`, a read-only `/proc` scan

`GET /sessions` returns the box's running coding agents:

```json
{
  "host": "citadel",
  "match": ["opencode", "claude"],
  "sessions": [
    { "pid": 48213, "user": "josh", "tool": "opencode", "cmd": "opencode", "uptime": 5400, "started": 1721470000 }
  ]
}
```

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

## Spawning — one `sudo` command you paste

The **Spawn** sheet composes a single `sudo` command. You pick the **user** to
run as and the **tool**; for opencode you also pick which of the fleet's
tailnet-reachable LLM boxes to point it at. The command, for `opencode` run as
`josh` pointed at `citadel`'s runtime:

```bash
sudo -u josh -H bash -lc '
  command -v opencode >/dev/null 2>&1 || curl -fsSL https://opencode.ai/install | bash &&
  export PATH="$HOME/.opencode/bin:$HOME/.local/bin:$PATH" &&
  mkdir -p "$HOME/.config/opencode" &&
  printf %s "<base64 opencode.json>" | base64 -d > "$HOME/.config/opencode/opencode.json" &&
  exec tmux new-session -A -s hush-opencode opencode'
```

Every part maps to a step of the workflow:

- **Pick a user; fail if it doesn't exist.** `sudo -u josh` *is* the whole
  preflight — if `josh` isn't a user on the box, the command fails on its own,
  loudly, before doing anything. hush doesn't need to check first.
- **Install if missing.** `command -v … || <installer>` installs the chosen
  tool only when absent — opencode or claude, whichever you picked, not both.
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
step, so a failed install never launches a half-configured agent.

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
