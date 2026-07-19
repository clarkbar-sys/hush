# hush — design

> Know your fleet is backed up — from your phone.

`hush` is a **backup console** for a homelab of tailnet machines, driven from a
phone. Its first question is the one a homelab usually can't answer at a glance:
*is everything backed up, and could I actually get it back?* Every machine is a
place on a map coloured by its **backup posture** — protected, at risk, failed,
or unprotected — so the box that isn't safe is the box that stands out.

hush began as a general "see and run your fleet" console (the *Factorio for your
homelab* framing below), and that substrate is still here — live vitals, a
file/store browser, ad-hoc commands, scheduled jobs, wired workflows. But the
thing hush does that nothing else in a homelab does well is make **backups
legible and trustworthy**: restic snapshots flowing into a vault, an alert the
moment a nightly fails, and a snapshot you can walk on your phone to confirm your
data is really there before you ever need it. That is the product; the rest is
the substrate it stands on.

The full initiative and rationale live in
[issue #6](https://github.com/clarkbar-sys/hush/issues/6). This document is the
distilled, in-repo reference.

## Principle

**Protection is a place. Read is a glance, write is deliberate, nothing hides.**

A homelab has no idea whether it's backed up, and sysadmin tools don't help.
hush borrows the two things that make a Factorio base legible:

1. **Spatial persistence** — every machine (and its backup) lives somewhere; you
   navigate by place, not by remembering which box runs the nightly.
2. **Status at a glance** — the map tells you what's unprotected; red means go
   look. A failed backup isn't a line in a log you'll never read — it's a badge
   on the map and a bell in the header.

## The construct vocabulary (5 nouns)

Everything you can put on the fleet is exactly one of these — the substrate the
backup console stands on. **Machine, Store, and Backup are the spine** (what am I
protecting, where does it live, is it safe?); Service and Link are the read-only
supporting cast. hush once had a "and you can also run things" half — ad-hoc and
scheduled command execution (Tasks, Jobs, Workflows) — but that was removed;
what's left is a backup console over a read-only view of the fleet.

| Construct | What it is |
|---|---|
| **Machine** | a tailnet host — has vitals, holds everything else |
| **Service** | a systemd unit — persistent, running or stopped |
| **Store** | a disk / dataset — the NAS especially |
| **Backup** | a scheduled restic run that hauls a Machine into a Store, dedup'd |
| **Link** | the tailnet edge between two machines |

## UX — semantic zoom

One canvas, three depths. You never navigate away, you get closer. Phone-first.

- **Fleet** — every machine as a node, sorted by **backup posture** so an
  unprotected or failing box floats to the top. The summary counts protected / at
  risk / unprotected / failed; each card carries an always-on backup line (posture
  + last run + where it ships to). Live dual-ring vitals, a load sparkline, and a
  status badge still colour each card — vitals just no longer decide the fleet's
  verdict. Backup problems also aggregate into the header's **alert bell**.
- **Machine** — "enter the building": header (OS, tailnet IP, uptime, GPU),
  full-size vitals, the Services list, and its Backups. Tapping
  the CPU ring or the network panel opens a live **htop-style** read of the box —
  per-core meters and the busiest processes — served by the agent's `/top`
  endpoint (proxied at `/api/machines/{host}/top`) and re-polled every ~2s. Like
  `/vitals` it's ungated read-only telemetry sampled from `/proc`, so it works on
  every agent without a flag.
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

Imperative, read-then-act. The console reads the fleet live over the tailnet and
drives backups directly on the box that holds the data. No reconciler, no
convergence loop.

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

Language: **Go** across the backend.

## Roadmap

The original roadmap layered generic fleet capabilities over a read-only map
(the Phases below, mostly shipped). The **pivot** re-centres everything already
built around backups and sets the direction from here.

**Shipped — the substrate (generic fleet console).**

- **Phase 0 — Proof of life (read-only).** Fleet map + live vitals + drill into a
  machine to see its services, plus adding a machine through the console.
- **Store.** Read-only **file browsing** and a windirstat-style **disk-usage
  treemap** on every box (see "Store — browsing files").
- **Backups.** Root-run restic backups set up on the box over SSH (the
  [backup convention](./BACKUP-CONVENTION.md)); the console reports their status
  read-only over `/backup-status` (see "Backups — restic").

**Shipped — the backup-first pivot.**

- **Backup posture as the map's primary signal.** The fleet leads with protected
  / at risk / unprotected / failed, sorts trouble to the top, and every card
  carries a backup line. Vitals still colour the rings; they're no longer the
  fleet's verdict. (See "Backup posture and alerts".)
- **Alert center.** Fleet backup problems aggregated into a ranked list behind a
  header bell, each alert jumping to its machine.

**Next — making the backup console whole.**

- **Push / email delivery of alerts.** The in-console bell is the model; a failed
  nightly should reach you when you aren't looking at the console.
- **Cross-site replication.** An off-site copy of the repo — the durability layer
  that turns "a second copy on the same NAS" into a real 3-2-1 story.
- **Retention / prune policy.** keep-daily/weekly/monthly, managed from the
  console instead of `restic forget` by hand.
- **Vaults view.** Repositories as first-class objects (size, snapshot count,
  dedup ratio, health), not a string field per backup.
- **Scheduled restore-tests / `restic check`.** A green "verified restorable"
  badge, so confidence isn't a manual walk.

**Removed (the "run things" half).** hush once shipped ad-hoc and saved command
execution (Tasks), cron scheduling (Jobs), wired sequences (Workflows), and a
`sudo -u` run-as allowlist. After dogfooding, that whole half was cut: the agent
no longer serves `/exec` or `/jobs`. Dedicated Service start/stop/restart and a
live journal tail were never built and are not planned.

**Removed (the in-agent Backup construct).** hush also shipped restic *inside*
`hush-agent` — the `-backup` flag, the `/backups` API (create / run / schedule /
snapshots / restore), `backups.json`, and `-export-keys` off-box escrow — driving
backups from the phone. It put a repository key on the box in the agent's reach
and made the console a write path into your data, so it was cut in favour of the
[backup convention](./BACKUP-CONVENTION.md): backups are set up (and restored)
on the box over SSH, and hush stays a read-only reader. The console is now a
**read-only fleet and backup monitor** — no write path remains.

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

**Disk usage — a windirstat-style treemap.** The Store view's "Disk usage"
button sizes a directory's immediate children recursively via the agent's
`/du?path=` endpoint (proxied at `/api/machines/{host}/du`, same shape as
`/browse`) and renders them as a squarified treemap — box area proportional to
size — so the biggest thing on disk is the biggest thing on screen. Only one
level renders at a time; clicking a directory's box drills into it and fetches
that subtree's own children, the same lazy navigation `/browse` uses. Sizing is
bounded — a 25-second walk deadline and a cap on files stated — so pointing it
at something enormous (a whole root filesystem, a NAS's media pool) returns a
partial, `truncated` answer instead of hanging the request.

## Backups — restic, set up on the box

A **Backup** is a scheduled [restic](https://restic.net) run that hauls a
Machine's paths into a Store, encrypted and dedup'd. restic gives hush the three
things a bare NAS-local copy can't — content-defined **dedup**, **snapshot**
history, and client-side **encryption**.

**hush reads backups; it does not run them.** A backup needs root — it reads
`/home`, service state dirs, and other data an unprivileged user cannot — and it
holds a repository credential. `hush-agent` runs unprivileged by design and must
never hold that credential. So a backup is set up **on the box itself**, over
SSH, following the [backup convention](./BACKUP-CONVENTION.md): a few files under
`/etc/restic/` plus a `restic-backup@<name>` systemd timer. The privileged runner
writes a **secret-free status file**; the agent only ever *reads* it and reports
it on `GET /backup-status` (ungated — it runs no restic and exposes no secret).
Privilege flows one way, information the other, and nothing about the agent has
to change to support a new backup.

**Why not run it inside the agent?** hush once shipped a "Backup construct" that
did exactly that — restic *inside* `hush-agent`, with create/run/restore/browse
driven from the phone (the `-backup` flag, `/backups`, `backups.json`, off-box
key escrow). It read well but inverted the rule above: it put a repository key on
the box in the agent's reach and made the console a *write* path into your data.
After dogfooding it was cut. What remains is a **read-only backup monitor** — the
console shows posture, the last runs, history, and what's at risk; you set
backups up, and restore them, over SSH where the credential already lives.

**Setup.** `sudo sh scripts/install-backup.sh` (download and read it first — it
takes a credential and runs as root) asks for a name, repository URL,
credentials, paths, and a schedule, then writes the convention files,
initialises the repository, and enables the timer. The generated repository
password lives on the box the backup exists to survive, so escrow it off-box by
hand. See [BACKUP-CONVENTION.md](./BACKUP-CONVENTION.md).

**Restore.** A restore reads a repo credential too, so — like setup — it runs on
the box over SSH (`restic restore <snapshot> --target <dir>`), never from the
console. The console's job is to tell you *which* snapshot you'd reach for and
that it ran clean; the recovery itself happens where the key is.

**Backend.** The blessed target is a [rest-server](https://github.com/restic/rest-server)
on the Store box (e.g. the NAS), reached over the tailnet — it supports
append-only repos, so a compromised source can add snapshots but not wipe old
ones. `sftp:` and local paths work too. **Cross-site replication** of the repo —
the durability layer that makes distributing across a two-site tailnet fleet a
real 3-2-1 story — is the next slice. A whole-machine backup uses
`--one-file-system` with restic's standard excludes; it is a restorable
file-level backup of the live root, not a block image.

## Backup posture and alerts

These are the backup-first pivot: they turn the restic status feed above into a
console whose whole job is "is the fleet protected, and can I get it back?"

**Backup posture — the map's signal.** Every machine is reduced to one of five
states, read from the same `/api/backup-status` feed the Backups section renders
(so the map and the cards can never disagree), worst-wins:

| Posture | Means |
|---|---|
| **failed** | a run finished non-zero — restic couldn't write a snapshot |
| **none** | reachable, but no backup configured — a total gap |
| **at risk** | a run was incomplete (restic exit 3), or the newest snapshot is stale (older than ~36h — a nightly that missed a night) |
| **protected** | every backup ran clean and recently |
| **unknown** | the box couldn't be asked |

Posture drives the fleet summary counts, the per-card backup line, and the sort
order (`failed > none > at risk > unknown > protected`), so the least-safe box is
the one at the top of the map. It's derived on the client — no new endpoint —
because the status feed already carries everything it needs.

**Alert center.** The same posture feeds a ranked alert list (failed, then
unprotected, then at-risk) behind a header **bell** that shows a count and takes
the colour of the worst open alert. Tapping an alert opens its machine. It is
in-console today, deliberately: the alert *model* (what's wrong, how it's ranked,
how it reads) is the load-bearing part, and out-of-band delivery (push, email) is
a thin hop on top of it — the next slice, not a rebuild.

## Running it (dev)

```bash
# agent on the box you want to watch (a literal address is fine for local testing)
go run ./cmd/hush-agent -listen 127.0.0.1:8765

# control plane — joins the tailnet and serves the console over HTTPS.
# TS_AUTHKEY provisions the node on first run; -web serves the on-disk UI.
TS_AUTHKEY=tskey-auth-… go run ./cmd/hush-control -tsnet -hostname hush -state-dir ./tsstate -web web
# open the printed https://hush.<tailnet>.ts.net URL
```

Point `hush-control` at a real fleet by copying `fleet.example.json` to
`fleet.json` and editing the agent addresses (or add machines from the console).

## Run modes

`hush-control` serves the console **only over the tailnet** (tsnet). The old
plain-HTTP LAN mode has been removed: an unauthenticated console on a trusted
LAN was a Phase 0 convenience, but the security boundary hush wants is Tailscale
identity, and running two modes was a standing source of misconfiguration (e.g.
a box coming back from a reboot serving LAN-only with no tailnet node). One mode,
gated the same way every time.

### Serving over the tailnet (`-tsnet`, now implied)

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
