# hush — design

> Give your fleet a place and a face.

`hush` is a control plane for a homelab of tailnet machines, driven from a
phone. Not by SSHing and typing — by looking at a map and placing things. The
mental model is **Factorio for your homelab**: see the whole base at a glance,
build by pointing, and once something is laid down it runs itself.

The full initiative and rationale live in
[issue #6](https://github.com/clarkbar-sys/hush/issues/6). This document is the
distilled, in-repo reference.

## Principle

**Every thing is a place. Read is a glance, write is deliberate, nothing hides.**

Factorio is legible because of two things, and sysadmin has neither:

1. **Spatial persistence** — every thing lives somewhere; you navigate by place.
2. **Status at a glance** — the map tells you what's wrong; red means go look.

## The construct vocabulary (8 nouns)

Everything you can put on the fleet is exactly one of these. If it can't be
expressed as one of these, it doesn't belong in v1.

| Construct | What it is |
|---|---|
| **Machine** | a tailnet host — has vitals, holds everything else |
| **Service** | a systemd unit — persistent, running or stopped |
| **Job** | a cron / timer — fires on a schedule |
| **Task** | a one-shot run of a program — ephemeral |
| **Workflow** | a wired sequence (`cd X → git pull → restart`) — reusable, stampable |
| **Store** | a disk / dataset — the NAS especially |
| **Backup** | a Job that hauls a Machine into a Store, dedup'd |
| **Link** | the tailnet edge between two machines |

## UX — semantic zoom

One canvas, three depths. You never navigate away, you get closer. Phone-first.

- **Fleet** — every machine as a node, sorted so trouble floats to the top;
  health aura + severity stripe, live dual-ring vitals, a load sparkline, a
  status badge. Alerts surface as badges on the map.
- **Machine** — "enter the building": header (OS, tailnet IP, uptime, GPU),
  full-size vitals, and constructs grouped into Services / Jobs / Tasks.
- **Construct** — one thing, full detail: state, metadata, controls, and (from
  Phase 1) a live-tailing journal.

### Vitals — concentric dual rings

- **Outer arc** = utilization (CPU or GPU), teal → amber → red by threshold.
- **Inner arc** = memory (RAM or VRAM), violet, its own thresholds; a violet
  dot ties it to the `% mem` / `% vram` sub-label.
- **Disk** is a single ring. Machines with a dGPU show compute · disk · gpu;
  headless boxes show compute · disk — the absence is information too.

An interactive reference lives at
[`docs/mockups/fleet-console.html`](./mockups/fleet-console.html) (open it
directly for the demo fleet; served by `hush-control` it shows live data).

## Architecture

Imperative, execute-directly. Tapping "do X on Y" runs it immediately over the
tailnet. Git is **not** foundational — GitHub-as-IaC is just a Workflow you
build later. No reconciler, no convergence loop.

```
  phone browser ── https over tailnet ──▶  hush-control (on the NAS)
                                              │  fans out, concurrently
                                              ▼
        hush-agent ── hush-agent ── hush-agent ──  (one per machine)
        reads /proc, systemd, nvidia-smi; returns JSON vitals
```

- **`hush-agent`** — one static Go binary per machine, no runtime deps. Reports
  vitals over the tailnet interface. Read-only in Phase 0.
- **`hush-control`** — runs on the NAS; fans out to every agent, aggregates,
  and serves the web UI. Config in `fleet.json` (see `fleet.example.json`),
  editable by hand or through the console's "Add machine" flow, which POSTs
  to `/api/agents` after confirming the address with `/api/agents/test`.
- **Transport** — the tailnet already provides encrypted transport + identity,
  so `hush` is a thin authenticated RPC, not a reimplementation of SSH. Agents
  listen only on the tailnet interface; no public exposure. `hush-control` can
  join the tailnet as its **own node** via `tsnet` (see below) rather than
  riding the host's Tailscale identity.
- **Web UI** — a single static page (`web/index.html`), vanilla HTML/CSS/JS.

Language: **Go** across the backend. **Scheme** is reserved for the Workflow DSL
(Phase 3 — blueprints are a Lisp's home turf).

## Roadmap

Each phase layers on the same map.

- **Phase 0 — Proof of life (read-only).** Fleet map + live vitals + drill into
  a machine to *see* its services. No construct button changes anything; the
  one exception is fleet membership itself — adding a machine through the
  console. ← we are here
- **Phase 1 — Actions.** Start / stop / restart Services; live journal tail.
- **Phase 2 — Creation.** Build new Services and Jobs from the palette.
- **Phase 3 — Workflows.** The visual blueprint builder (Scheme DSL). A first
  slice lands early: saved multi-step blueprints that sequence the existing
  `/exec` in plain Go (see below), so Workflows are usable before the Lisp.
- **Phase 4 — Backups & Store.** The NAS view; intelligent dedup'd backups.
  A first slice lands early: read-only **file browsing** on every machine
  (see below), so the NAS is walkable before dedup'd backups exist.

## Store — browsing files

Every machine exposes its filesystem as a **Store** you can walk from the
Machine view: Fleet → Machine → Store → directory → directory. The agent's
`/browse?path=` endpoint returns a directory listing (name, size, mode, mtime,
symlink target); `hush-control` proxies it at
`/api/machines/{host}/browse`, so the phone reaches it the same way it reaches
everything else (agents aren't directly addressable in tsnet mode). It is
read-only — opening/streaming a file is the next step.

**No jail — the Unix user is the boundary.** Browse is not rooted at a
configured directory; any absolute path is listable, and the *only* thing that
gates what comes back is what the unprivileged `hush` user can read on that box.
A directory it can't read returns permission-denied (surfaced as `403`), exactly
as it would for that user in a shell. This is deliberate: the security boundary
is the OS identity, not application logic, which is the same model a future
"run a command on this machine" capability wants. Widen or tighten a box's reach
by changing the `hush` user's group membership (`usermod -aG …`), not by editing
hush. The agent's systemd unit is hardened but intentionally omits
`ProtectHome`/`PrivateTmp`, which would blank out readable paths and make the
sandbox — rather than the user — the real fence.

## Tasks — running a command

The **Task** construct ("a one-shot run of a program — ephemeral") is the write
half of the browse model above: the agent's `POST /exec` runs a shell command
and streams its output back as [Server-Sent Events](https://developer.mozilla.org/docs/Web/API/Server-sent_events)
(`start` → `out` → `exit`); `hush-control` proxies it at
`/api/machines/{host}/exec`, flushing each frame so output appears live on the
phone. The console launches a run from **＋ Build → Task** or a Machine's
**Tasks** section, then shows a full-screen live terminal. An ad-hoc run is
ephemeral — recorded per session, not persisted.

**Saving a Task — the reusable atom.** A run can graduate from ephemeral to a
named building block: **Save as Task** in the run view mints a `{name, host,
cmd}` the console keeps in `tasks.json` beside `workflows.json` (same
already-writable directory), exposed at `/api/tasks` — `GET`/`POST` to list and
save, `PUT`/`DELETE /api/tasks/{id}` to edit and remove, and `POST
/api/tasks/{id}/run` to execute. A saved run resolves its command server-side
and fans out to the **same `/exec`** an ad-hoc Task uses, so it's audited and
bounded identically — the pinned machine is validated against the fleet at save
time, the way a Workflow step's is. Saved Tasks surface in the Fleet page's
**Tasks** rollup (Run / Edit / Delete) and are the pieces a Workflow is built
from.

**Same boundary as browse: the Unix user, not app logic.** A Task runs `sh -c`
as the unprivileged `hush` user with no jail and no allowlist of binaries —
whatever that user can do in a shell, a Task can do. This is the deliberate
end of the model the Store section describes ("the same model a future *run a
command on this machine* capability wants"). A run is bounded, not sandboxed:
its own process group (so a timeout or a client hang-up kills the whole tree),
a default 5-minute / max 60-minute timeout, and a 1 MiB output cap.

**Exec is on by default, opt-out per agent.** A box opts out with `-exec=false`
(or `HUSH_AGENT_EXEC=0`, so a systemd env file can toggle it without editing
`ExecStart`), after which `/exec` returns `403` and the agent is read-only.
Because `/exec` is new agent code, only agents running the release that
introduced it (or newer) can run Tasks — `hush-control` proxies to `/exec`, so
an older agent simply reports exec as unavailable. In tsnet mode every run is
gated by the same Tailscale identity as everything else, and `hush-control`
logs who ran what against which box.

## Workflows — wiring a sequence

The **Workflow** construct ("a wired sequence (`cd X → git pull → restart`) —
reusable, stampable") is sequencing layered on the Task primitive: a saved,
ordered list of steps, each a `{machine, command}` pair. The builder lets you
type a step inline **or drop in a saved Task** — a Workflow is, at heart, a
chain of Tasks — copying its `{machine, command}` into a step so the two stores
stay decoupled (a Workflow keeps working if a Task it was built from is later
deleted). `hush-control` stores
blueprints in `workflows.json` beside `fleet.json` (same writable directory the
systemd unit already grants) and exposes them at `/api/workflows` —
`GET`/`POST` to list and save, `DELETE /api/workflows/{id}` to remove, and
`POST /api/workflows/{id}/run` to execute. A run fans out to the **same
`/exec`** each Task uses, one step at a time, and streams a combined SSE log
(`step` → `out` → `stepExit`, then a terminal `done`) so the console can group
each command's output under its step and show live progress. The builder and
run view live under **＋ Build → Workflow**.

**Fail-fast, like `set -e`.** Steps run in order and the first one to exit
non-zero — or error, or end without a status — stops the run; the `done` frame
carries `failedStep` so the UI marks where it stopped. A blueprint is validated
at save time (every step's machine must be in the fleet), and each step inherits
the Task run's bounds: unjailed as the `hush` user, a 5-minute per-step timeout,
audited by `hush-control` with the caller's Tailscale identity.

**No Scheme yet.** The design reserves Scheme for a visual blueprint DSL; this
first slice is plain Go over the existing exec plumbing, so Workflows are usable
now and the Lisp can land later without changing the runtime beneath them.

## Running it (dev)

```bash
# agent on the box you want to watch
go run ./cmd/hush-agent -listen 127.0.0.1:8765

# control plane serving the UI (defaults to a single local agent)
go run ./cmd/hush-control -listen 127.0.0.1:8080 -web web
# open http://127.0.0.1:8080
```

Point `hush-control` at a real fleet by copying `fleet.example.json` to
`fleet.json` and editing the agent addresses.

## Run modes

`hush-control` serves the same console two ways. LAN mode is the Phase 0
default; tsnet mode is the secure, reach-from-anywhere target.

### LAN mode (default)

Plain HTTP on `-listen`, agents addressed by IP in `fleet.json`. It is
**unauthenticated** — trusted networks only, never expose agent ports publicly.
Good for dev and trusted-LAN use; the UI falls back to demo data when
`/api/fleet` is unreachable.

### tsnet mode (`-tsnet`)

`hush-control` joins the tailnet as its **own node** (default hostname `hush`,
independent of the host's Tailscale identity) using
[`tailscale.com/tsnet`](https://pkg.go.dev/tailscale.com/tsnet), and serves the
console over HTTPS on `:443` with a real auto-provisioned cert — reachable at
`https://<hostname>.<tailnet>.ts.net`, no warnings on the phone.

```bash
# auth key provisions the node on first run; state persists in -state-dir
TS_AUTHKEY=tskey-auth-… ./hush-control -tsnet -hostname hush -state-dir ./tsstate

# restrict to specific operators (repeatable; omit for any tailnet member)
TS_AUTHKEY=tskey-auth-… ./hush-control -tsnet -allow you@example.com
```

Every request is gated by Tailscale identity: `tsnet`'s `WhoIs` resolves the
caller's login from the connection. Reaching the node at all requires tailnet
membership (network membership *is* the first auth gate); the optional
`-allow` allowlist narrows that to named logins on top.

**Prerequisites in the tailnet:** [MagicDNS] and [HTTPS certificates] must be
enabled (Admin console → DNS). The node is served **tailnet-only** — hush never
uses Tailscale Funnel, so the console is never publicly exposed.

[MagicDNS]: https://tailscale.com/kb/1081/magicdns
[HTTPS certificates]: https://tailscale.com/kb/1153/enabling-https
