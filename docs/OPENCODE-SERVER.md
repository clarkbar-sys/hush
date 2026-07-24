# hush — opencode server (add a box in opencode mobile)

> See a box's opencode server and copy its URL straight into opencode mobile —
> or, when there's none, compose the one sandboxed `sudo` command that stands a
> reboot-surviving one up. The hush way: hush hands you the command, you run it.

An **opencode server** is a headless [`opencode serve`](https://opencode.ai)
running on a box, listening on the tailnet, that the **opencode mobile** app
attaches to under *Servers → add*. It's the natural companion to hush's
[local-inference](./DESIGN.md#local-inference--capability-and-reach) readout:
the fleet already has a GPU box serving models on the tailnet, and a server on
some box — pointed at that runtime — is how your phone talks to those models
through a real opencode, from wherever you are.

This is distinct from an interactive [Session](./SESSIONS.md): a session is an
`opencode`/`claude` you type into over SSH; a *server* is a socket a client
connects to. hush's contract is identical for both — **it reads, and it
composes commands; it never runs, spawns, or stops anything.**

## What you see — the opencode server section

Every Machine view grows an **opencode server** section, above Sessions. It
reads the same `/sessions` endpoint the Sessions view does (a server is just a
session flagged `server: true`), and shows one of three states:

- **A reachable server is running.** hush shows its **URL** —
  `http://<box tailnet ip>:<port>` — with a **Copy URL** button. Paste it into
  opencode mobile's *add server* and you're working against whatever fleet LLM
  this box was pointed at.
- **A server is running but bound to loopback.** It's real, but a phone can't
  reach `127.0.0.1` on another machine, so hush shows it as *local only* with no
  URL and explains why. Restart it bound to the tailnet (the Start command
  below does exactly that) to get an addable URL.
- **No server is running.** A **＋ Start opencode server** button opens the sheet
  that composes the launch command.

Either running state also gets a **Stop** button next to it.

The reachability verdict is not a guess. The agent reads the kernel's own
listener table (`/proc/net/tcp{,6}`, see `internal/netlisten`) to learn where
the server actually bound, exactly the way LLM reach is judged — so a
loopback-only server can never be advertised as if a phone could reach it, and a
server whose socket wasn't found reports *unknown* rather than a reassuring
"local only".

## The agent — a flagged Session on `/sessions`

Detection extends the existing `/proc` scan: an `opencode` process whose argv
contains the `serve` subcommand is a server. The agent reads its `--port` (or
opencode's default, `4096`), cross-references the listener table for that port,
and reports the bound address and exposure on the session:

```json
{
  "pid": 40771,
  "user": "josh",
  "tool": "opencode",
  "cmd": "opencode serve --hostname 0.0.0.0 --port 4096",
  "uptime": 9000,
  "server": true,
  "addr": "100.71.8.9:4096",
  "exposure": "tailnet"
}
```

`server` is set only for opencode (`serve` is opencode's subcommand; claude has
no headless-server mode, so a stray `serve` token elsewhere is never mistaken
for one). `exposure` is `loopback | tailnet | open | unknown`, and the console
only offers a URL for `tailnet` or `open`. This is the same ungated, read-only,
privilege-free telemetry as the rest of `/sessions` — no new endpoint, and the
agent stays the unprivileged `hush` user.

## Starting — one `sudo` command you paste

The **Start** sheet composes a single `sudo` command. You pick the **user** to
run as, the **port** (default `4096`), a **password** (auto-generated, editable),
and which of the fleet's tailnet-reachable LLM boxes to **expose models from**.
Every part maps to a requirement:

- **Pick a user; fail if it doesn't exist.** The command checks `id <user>` and
  bails loudly, before touching anything, if the account is missing.
- **A hush-owned, per-user work tree.** The server lives in
  `/var/lib/hush/opencode/<user>`, created up front — a dedicated tree hush
  knows about, not scattered into a home directory.
- **A real sandbox: that folder *is* the filesystem.** `opencode serve` runs
  inside a [bubblewrap](https://github.com/containers/bubblewrap) jail whose root
  (`--bind <work> /`) is the work tree, so the server can only ever see and work
  out of that folder — the rest of the box is invisible. The command checks for
  `bwrap` and fails with a clear line if it's absent, rather than silently
  running unconfined.
- **The fleet's models, ready on connect.** The chosen LLM box's `opencode.json`
  (the same export the [Inference section](./DESIGN.md#local-inference--capability-and-reach)
  builds — an OpenAI-compatible provider at the runtime's tailnet `baseURL`, its
  served models, cost pinned to zero) is written into the jail's config, so the
  phone sees those models the moment it adds the server. It's **base64-piped** so
  the JSON's quotes can't collide with the command's own quoting.
- **systemd, not a homebrew launcher.** The command installs a system unit
  (`/etc/systemd/system/opencode-server.service`) and `systemctl enable --now`s
  it, so the server survives a reboot and is managed like any other service —
  no `tmux`, no `nohup`, no hand-rolled `screen`.

The password is set as `OPENCODE_SERVER_PASSWORD` — the credential opencode
mobile sends on connect. hush composes it into the command and **never stores
it**. The unit binds `0.0.0.0` inside the jail; the host's tailscale is what
makes that reachable as the box's tailnet address, which is the URL the server
section then shows once detection sees the new bind.

The command is **idempotent**: re-pasting it rewrites the unit and restarts the
server, so changing the port or password is just a re-paste from a new phone.

## Stopping / changing

The server section's **Stop** button opens a sheet with the one command that
stops it: `sudo systemctl disable --now opencode-server`. Because the server
runs under a systemd unit with `Restart=on-failure`, a bare `kill` of the
process would just come back — disabling the unit is what actually stops it
and keeps it from returning on reboot. As with Start, hush only composes the
command; you paste it into a root shell yourself. Re-pasting the Start command
with a different port or password reconfigures the server in place.

## Permissions — same open question as Sessions

Like [Sessions](./SESSIONS.md#permissions--the-open-question), this needs **no
new authorization model**: hush-control never acts, and the operator's own
`sudo` on the box is the only privilege in play. Anyone who can reach the console
can *compose* a Start command for any box, but composing it isn't running it —
it still dies at the box's `sudo` gate. If a future slice ever has hush *run* the
server rather than hand you the command, that's the moment to design a real
permission model, against that concrete capability.
