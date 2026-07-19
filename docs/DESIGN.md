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
- **Backups.** The restic-backed Backup construct: create / schedule / run /
  snapshots / restore / off-box key escrow (see "Backups — restic").

**Shipped — the backup-first pivot.**

- **Backup posture as the map's primary signal.** The fleet leads with protected
  / at risk / unprotected / failed, sorts trouble to the top, and every card
  carries a backup line. Vitals still colour the rings; they're no longer the
  fleet's verdict. (See "Backup posture, alerts, and snapshot browsing".)
- **Alert center.** Fleet backup problems aggregated into a ranked list behind a
  header bell, each alert jumping to its machine.
- **Browse inside a snapshot.** Walk a snapshot's file tree from the phone
  (`restic ls`, proxied read-only) to confirm the data is really there before
  trusting a restore.

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
no longer serves `/exec` or `/jobs`, and the console is purely a backup and
read-only fleet view. The write path that remains is backups. Dedicated Service
start/stop/restart and a live journal tail were never built and are not planned.

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

## Backups — restic, the first slice

The **Backup** construct ("a scheduled restic run that hauls a Machine into a Store, dedup'd") is
a saved set of paths a box sends into a [restic](https://restic.net) repository,
encrypted and dedup'd. restic gives hush the three things a bare NAS-local copy
can't — content-defined **dedup**, **snapshot** history, and client-side
**encryption** — and this package is a thin, streaming shell over the restic
binary, not a reimplementation of any of it. A Backup lives on the **agent**:
the box that holds the data holds the repository key, so the console
drives backups through hush-control's pass-through proxy
(`/api/machines/{host}/backups`, `…/{id}/run`, `…/{id}/snapshots`) rather than a
control-side store. A box serves `/backups` only when its agent is started with
`-backup` (or `HUSH_AGENT_BACKUP=1`) — off by default, since a backup reads
whatever paths you point it at; until then the endpoint returns `403`, surfaced
as "backups disabled".

**The key stays on the box.** A restic repo is a backend location plus an
encryption password. hush keeps that password in the agent's `0700` state dir
(`backups.json`) and hands it to restic through the
environment — `RESTIC_REPOSITORY` / `RESTIC_PASSWORD`, never argv, so it never
lands in the process table or an audit log. It is the one field the API never
returns (`GET /backups` omits it by construction) and never accepts back through
hush-control's audit log (a create logs only the backup's name and repo). The
control plane and the phone drive backups without the key ever passing through
them, so neither can become the thing that leaks it. restic is invoked with an
explicit argument slice (no `sh -c`), so a path that looks like a flag or holds
shell metacharacters is passed through literally — the stricter handling a typed
backup wants.

**Escrow without breaking the rule.** Keeping the key on the box has a circular
edge: the box a backup exists to survive is the one box holding the sole copy of
the key that decrypts its snapshots. `hush-agent -export-keys` closes that gap
without inverting the rule — run over SSH, it prints the box's own repo keys as
JSON on local stdout and exits, so an operator can escrow them into a password
manager while the secret still never transits hush-control or the phone (the same
boundary the running agent keeps). The console's **Escrow repo keys** sheet
*generates* that command rather than running it, mirroring the setup helper, and
records which boxes have been escrowed as a browser-local note only — a claim
about a key, never the key, so the control plane stays free of both.

**Picking what to save — the treemap doubles as a picker.** The Store's
windirstat-style disk-usage treemap (`/du`, below) does double duty: from the
backup sheet, "Pick from disk usage" opens it in a **select mode** where tapping
a box includes that path (a folder still drills in via its ⤢ corner) and a
running "N selected · size" totals the choice, so "opt in what I want to save" is
a matter of tapping the big boxes rather than typing paths. The selection writes
absolute paths back into the sheet; the whole-machine toggle (`--one-file-system`
over `/`) is the other end of the spectrum for a box you want in full.

**Create proves the repo works.** Adding a backup initialises the repository
(tolerating one that already exists, so a second machine can point at the same
repo) and lists its snapshots to verify the password — a bad backend URL or
wrong key fails at create time, not silently at 3am. A run streams restic's
output to the console's run terminal as Server-Sent Events
(`start` → `out` → `exit`), and records the last run's outcome and the
snapshot it wrote. Deleting a backup forgets its definition only; the
repository's snapshots are left for `restic` to prune directly, so a mistaken
delete loses the schedule, never the data.

**Restore closes the loop.** From the Snapshots view, a snapshot restores into a
target directory (`POST /backups/{id}/restore` with `{snapshot, target}`),
streaming through the same run terminal a run does. The console defaults the
target to a scratch dir rather than the original location, so inspecting a
restore can't clobber the live files by accident — an in-place restore is
something the operator types the path for. The snapshot id is validated (hex or
`latest`) and the target must be absolute, both before the stream begins; a
restore only ever *writes* into the target, never touching the snapshots, so it
can't harm the backup history. That makes the lifecycle whole: configure → run
(or schedule) → snapshots → restore.

**Runs on demand or on a schedule.** A backup can be run by hand from the
console, or given an optional 5-field cron schedule (`@daily` macros too) so the
agent fires it unattended, on the box, via a robfig/cron engine. A scheduled fire
has no client to stream to; the outcome lands in the backup's status (and its
next-run time is reported to the console) either way. A fire is skipped, not
queued, while a run for the same backup is already in flight, so a long backup
can't stack copies of itself. The schedule lives in `backups.json` and is
re-registered on agent restart. An empty schedule is manual-only.

**Setup is generated, never applied.** Getting a box backup-ready needs three
things — `restic` installed, `-backup` enabled, and (for a vault box) a
`rest-server` — and `hush-control` is unprivileged and never
touches the box. So the agent advertises its readiness in `/vitals` (a
`BackupCapability`: is `-backup` on, restic's version, is a `rest-server` binary
present), and the console's **Set up backups** sheet turns that plus the box's OS
into the exact idempotent root command to paste over SSH — the right package
manager for restic, the `HUSH_AGENT_BACKUP=1` env line, and, optionally, an
append-only tailnet `rest-server` unit bound to the box's own tailnet IP. It adds
only the missing steps and ends with an agent restart so the box re-advertises.
Once a box reports a vault, the create sheet offers it as a one-tap repository
URL, so pointing one machine at another's vault is a chip, not a typed string.

**Backend.** The blessed target is a [rest-server](https://github.com/restic/rest-server)
on the Store box (e.g. the NAS), reached over the tailnet — it supports
append-only repos, so a compromised source can add snapshots but not wipe old
ones. `sftp:` and local paths work too. **Cross-site replication** of the repo —
the durability layer that makes distributing across a two-site tailnet fleet a
real 3-2-1 story — is the next slice. A whole-machine backup uses
`--one-file-system` with restic's standard excludes; it is a restorable
file-level backup of the live root, not a block image.

## Backup posture, alerts, and snapshot browsing

These three are the backup-first pivot: they turn the restic plumbing above into
a console whose whole job is "is the fleet protected, and can I get it back?"

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

**Browsing a snapshot — restore confidence.** A restore you've never tested is a
hope, not a backup. The agent's `GET /backups/{id}/snapshots/{snap}/ls?path=`
runs `restic ls` and returns one directory level (`{path, entries, truncated}`);
`hush-control` proxies it at
`/api/machines/{host}/backups/{id}/snapshots/{snap}/ls`, so the phone walks a
snapshot's tree the same lazy, one-directory-at-a-time way it walks a live
filesystem in the Store. It is strictly **read-only** — a snapshot is immutable
and `restic ls` never writes — so confirming your data is really in there can
never harm the backup. The listing is bounded (immediate children only, capped
and marked `truncated`, a 30s deadline) so browsing a snapshot of a million-file
directory returns a partial answer instead of hanging, the same contract the
`/du` treemap uses. The snapshot id is validated (hex or `latest`) and any path
must be absolute, both before restic is invoked.

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
