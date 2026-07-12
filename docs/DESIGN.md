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
- **Phase 3 — Workflows.** The visual blueprint builder (Scheme DSL).
- **Phase 4 — Backups & Store.** The NAS view; intelligent dedup'd backups.

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
