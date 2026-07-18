# hush

> A tailnet fleet console — see and run your homelab from your phone.

[![CI (Go)](https://github.com/clarkbar-sys/hush/actions/workflows/ci-go.yml/badge.svg)](https://github.com/clarkbar-sys/hush/actions/workflows/ci-go.yml)
[![License](https://img.shields.io/badge/license-GPL--2.0-blue.svg)](./LICENSE)

## Overview

`hush` gives a homelab of [Tailscale](https://tailscale.com) machines a place
and a face: a phone-first web console where you see every machine at a glance
and act on them by pointing, not typing. The mental model is *Factorio for your
homelab* — a legible map of what's running, with everything expressed as one of
eight constructs (Machine, Service, Job, Task, Workflow, Store, Backup, Link).

See [`docs/DESIGN.md`](./docs/DESIGN.md) for the design and
[issue #6](https://github.com/clarkbar-sys/hush/issues/6) for the full
initiative. **Status: Phase 0 — read-only proof of life.**

## Install

`install.sh` sets up `hush-agent` and/or `hush-control` as systemd services
running under a dedicated, unprivileged `hush` system user — no Go toolchain,
no git clone required on the target box. It **must run as root** (it fails
loudly otherwise, since there's no way to install a service without it):

```bash
curl -fsSL https://raw.githubusercontent.com/clarkbar-sys/hush/main/install.sh | sudo sh
```

That installs `hush-agent` alone, enabled and started — the same one-liner
is correct on every machine you want to watch, which is most of your fleet.
`hush-control` is a single, deliberate install on one box (e.g. the NAS), so
it's opt-in: pass `control` for LAN mode or `control-tsnet` for the
[tsnet HTTPS mode](#serve-over-the-tailnet-https), e.g.
`... | sudo sh -s -- control-tsnet`. Pass `all` to install both on one box.
It's systemd-only (Linux) — see [`scripts/install.sh`](./scripts/install.sh)
below for the same install from a local clone, and "Prefer building from
source" below for running the binary yourself without a service (e.g. on
macOS, which has no systemd).

On immutable-root distros — **SteamOS** (Steam Deck), Fedora Silverblue/Kinoite —
`/usr` is mounted read-only, so the usual `/usr/local/bin` can't be written and
the install would fail with a "read-only file system" error. The installer
detects this and falls back to a writable directory (`/opt/hush/bin`, then
`/var/lib/hush/bin`), rewriting the systemd unit's `ExecStart` to match — no
extra steps needed. Set `HUSH_BIN_DIR=/some/dir` to force a specific location,
e.g. `... | sudo HUSH_BIN_DIR=/opt/hush/bin sh`.

Prefer building from source? Both binaries also install with the Go toolchain
(Go 1.26+):

```bash
# control plane — on the box that serves the console (e.g. the NAS)
go install github.com/clarkbar-sys/hush/cmd/hush-control@latest

# agent — on each machine you want to watch
go install github.com/clarkbar-sys/hush/cmd/hush-agent@latest
```

They land in `$(go env GOBIN)` (or `$(go env GOPATH)/bin`). The console UI is
embedded in `hush-control`, so the binary is self-contained — nothing else to
copy. The `hush-agent` binary is stdlib-only and has no runtime dependencies.
Both builds are `CGO_ENABLED=0` static binaries, so the `linux/arm64` release
covers Pi/Alpine/NixOS boxes too — `install.sh` picks it up automatically.
For any other `GOOS`/`GOARCH`, cross-compile directly:
`GOOS=linux GOARCH=arm64 go build ./cmd/hush-agent` and copy the binary over.

## Getting started

```bash
# 1. run an agent on a machine you want to watch
hush-agent -listen 127.0.0.1:8765

# 2. run the control plane serving the UI (defaults to one local agent)
hush-control -listen 127.0.0.1:8080

# 3. open the console
open http://127.0.0.1:8080
```

To watch a real fleet, copy [`fleet.example.json`](./fleet.example.json) to
`fleet.json`, list your agents' tailnet addresses, and start `hush-control`. In
production each `hush-agent` binds to the tailnet interface — no public exposure.
Pass `-listen tailnet` (or set `HUSH_AGENT_LISTEN=tailnet`) and the agent binds
this machine's Tailscale IP automatically, waiting for `tailscaled` to come up on
boot rather than hardcoding a `100.x` address. This is the default for
`install.sh`, so a freshly installed agent is discoverable over the tailnet with
no post-install edit. Use `tailnet:PORT` for a non-default port, or a literal
`host:port` (e.g. `127.0.0.1:8765`) to pin a specific interface.

Working from a clone instead? Swap `hush-control` for `go run ./cmd/hush-control`
(and likewise for the agent). Add `-web web` to serve the UI from the on-disk
`web/` directory when you want to iterate on it live.

### Serve over the tailnet (HTTPS)

The steps above run **LAN mode**: plain HTTP, unauthenticated — trusted
networks only. For the secure, reach-from-anywhere console, run `hush-control`
in **tsnet mode**: it joins the tailnet as its own node and serves HTTPS on
`:443` with a real cert at `https://<hostname>.<tailnet>.ts.net`.

```bash
# provision the node with an auth key; persist its state in -state-dir
TS_AUTHKEY=tskey-auth-… hush-control -tsnet -hostname hush -state-dir ./tsstate

# optionally restrict to specific operators (repeatable; omit = any tailnet member)
TS_AUTHKEY=tskey-auth-… hush-control -tsnet -allow you@example.com
```

**First run from your phone (no auth key on the command line).** Starting
`hush-control -tsnet` with no `TS_AUTHKEY` and no saved node state serves a
one-time **setup page** on the LAN (`-listen`, default `:8080`) — no SSH, no
editing env files. Open `http://<box-ip>:8080` in a browser, paste a
[Tailscale auth key](https://login.tailscale.com/admin/settings/keys) and the
hostname, and the same process joins the tailnet and bounces you to the HTTPS
URL. This page is plain HTTP and unauthenticated — it wears a warning banner and
exists **only until the node is provisioned**, after which it never reappears.
The `install.sh` `control-tsnet` install ships with an empty `TS_AUTHKEY`, so
this is the default first-run experience.

Every request is gated by Tailscale identity (`WhoIs`). **Prerequisites:**
[MagicDNS](https://tailscale.com/kb/1081/magicdns) and
[HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) enabled in
your tailnet. The node is served **tailnet-only** — hush never uses Tailscale
Funnel. See [`docs/DESIGN.md`](./docs/DESIGN.md#run-modes) for details.

### Add machines from the console

You don't have to edit `fleet.json` by hand. The console's **＋ Build → Machine**
sheet takes a tailnet address, probes it to confirm a `hush-agent` is answering,
and persists it to `fleet.json` — the new machine shows up on the next poll, no
restart needed.

In **tsnet mode** the sheet can also find agents for you: **Scan tailnet** reads
your tailnet's device list (the same table Tailscale keeps, much like DHCP
leases), probes each online node on the agent port, and lists the ones running
`hush-agent` that aren't in your fleet yet. Tap one to add it — no IP hunting.
Discovery needs the tailnet handle tsnet provides, so the scan button falls back
to manual entry in LAN mode.

`hush-control` also rescans the tailnet in the background, so the **＋ Build**
button carries a count badge when new agents appear that you haven't
added — you don't have to open the sheet to notice a fresh box. Adding stays a
deliberate tap; discovery only ever suggests.

### Run a command (Tasks)

The **Task** construct runs a one-shot command on a machine and streams its
output live to the console — **＋ Build → Task**, or the **Tasks** section of any
Machine view. Commands run via `sh -c` as the unprivileged `hush` user with no
sandbox — the OS permissions of that user are the only boundary, the same model
as file browsing.

It's **on by default**; a box opts out by starting `hush-agent` with
`-exec=false` (or setting `HUSH_AGENT_EXEC=0` in its env file and restarting),
after which `/exec` returns `403` and the agent is read-only. Because `/exec`
is new agent code, only agents running this release or newer can run Tasks —
older ones report exec as unavailable until you re-run the installer. See
[`docs/DESIGN.md`](./docs/DESIGN.md#tasks--running-a-command).

#### Run a Task as another user

By default a Task runs as the unprivileged `hush` user. A box can also offer a
set of **run-as users** so a Task — ad-hoc, saved, or a Workflow step — runs as
one of them via `sudo -u`: least privilege per workload, without ever giving
`hush` blanket root. Start the agent with `-run-as media,deploy` (or
`HUSH_AGENT_RUNAS=media,deploy` in its env file); the agent advertises that list
and the console shows a **Run as** picker wherever it applies. Empty (the
default) leaves the feature off, and `/exec` refuses any user not on the list.

It needs a matching **sudoers grant** — `hush` must be allowed to `sudo -u`
those users. The installer never writes sudoers (a broken sudoers file is how
you lock yourself out); instead a Machine's **Run-as users** sheet generates the
exact root command to paste over SSH, using a `hush-runas` group so the file is
written once and later changes are just group membership. **Never list `root` or
a sudo-capable user:** the agent is reachable by anyone on the tailnet, so that
list is the ceiling on what a caller can become. Users needn't exist yet — allow
one now, `useradd` it later.

Because the advertised list (`HUSH_AGENT_RUNAS`) and the sudoers grant are set
separately, they can drift. The agent verifies each advertised user against the
*real* grant (a passwordless `sudo -n -l` probe, cached and re-checked
periodically) and reports the runnable subset in `/vitals`. The console's **Run
as** picker flags any user it can't actually `sudo -u` yet — "no sudoers
grant" — so a missing or drifted grant shows up as a ⚠ up front instead of a
Task failing at run time. A user listed but not yet granted (e.g. you set the
env but haven't pasted the **Run-as users** command) is exactly this case.

### Back up a machine (Backups)

The **Backup** construct sends a machine's paths into a
[restic](https://restic.net) repository — dedup'd, encrypted, snapshotted —
from **＋ Build → Backup** or the **Backups** section of any Machine view. Point
it at a restic backend (a [rest-server](https://github.com/restic/rest-server)
URL on the NAS is the blessed target, reachable over the tailnet and
append-only; `sftp:` and local paths work too), name the paths (or **pick them
from the disk-usage treemap**), and optionally tick **whole machine**
(`--one-file-system`). Give it a **cron schedule** and the agent fires it
unattended — nightly, on the box — the same clockwork Jobs use; leave the
schedule blank to run it only by hand. Creating a backup initialises the repo and
verifies the password against it, so a bad address or key fails then and not at
3am; a run streams restic's output to the same live terminal a Task uses.

It's **off by default** — a backup reads whatever paths you point it at — so a
box serves it only when `hush-agent` is started with `-backup` (or
`HUSH_AGENT_BACKUP=1` in its env file); until then `/backups` returns `403`,
surfaced as "backups disabled". The repository password is stored on the agent's
own `0700` state dir and handed to restic through the environment — it never
passes through hush-control or the phone, and the API never returns it. Needs the
`restic` binary on the box. See
[`docs/DESIGN.md`](./docs/DESIGN.md#backups--restic-the-first-slice).

Restore from the **Snapshots** view (the `⋯` on a backup): pick a snapshot, hit
`⤓`, and restic writes its files into a target folder — defaulting to a scratch
dir (`/var/tmp/hush-restore/<id>`) so you can inspect a restore before pointing
it at a live path.

#### Setting up backups end to end

You don't have to memorise any of this — the Machine view's **Set up backups**
button detects what a box is missing (`restic`, `-backup`, a vault) and generates
the exact idempotent command to paste over SSH, picking the right package manager
for the box's OS, the same way the **Run-as users** sheet generates its sudoers
grant. The steps below are what that command does, for when you'd rather do it by
hand.

A backup needs three things in place; once they are, the console does the rest.

1. **`restic` on every box you back up** — your distro's package (`apt install
   restic`, `pacman -S restic`, …) or restic's static binary. The agent reports
   "restic is not installed" at create time if it's missing.
2. **A repository to back up *to*.** The durable, self-hosted choice is a
   [rest-server](https://github.com/restic/rest-server) on the box that holds the
   disks (the NAS). Run it in **append-only** mode so a compromised source can add
   snapshots but never delete old ones, behind a password:

   ```bash
   # on the NAS — one repo host for the whole fleet, reached over the tailnet
   rest-server --path /srv/restic --listen :8000 --append-only \
     --htpasswd-file /srv/restic/.htpasswd
   ```

   Then a backup's **Repository** is `rest:http://<nas-tailnet-ip>:8000/<name>`
   (each machine can use its own `<name>` sub-repo, or share one — restic dedups
   across them). `sftp:` to the NAS or a local/mounted path work too, without the
   append-only guarantee.
3. **`-backup` on the agents.** Set `HUSH_AGENT_BACKUP=1` in the box's
   `/etc/hush/*.env` and `systemctl restart hush-agent` (backups are off by
   default). To fire them unattended, give the backup a cron schedule when you
   create it.

Then, from the console: **Build → Backup**, pick the machine, the repo, the paths
(or the treemap), a schedule — and verify it with a **Run**, then a **Restore**
of the snapshot it wrote into a scratch dir.

## Run as a service

`install.sh` (above) already does this — every install is a systemd service
under a dedicated `hush` user, enabled and started. It creates the `hush`
user, installs the binary to `/usr/local/bin`, fetches the matching unit from
[`systemd/`](./systemd), and writes an editable environment file to
`/etc/hush/*.env` (e.g. the listen address, or `TS_AUTHKEY` for tsnet's first
run) without ever clobbering one that already exists. After editing an env
file, apply it with `systemctl restart hush-agent` (or `hush-control` /
`hush-control-tsnet`). It targets systemd + `useradd` distros (Debian,
Ubuntu, Fedora, Arch, RHEL, openSUSE) — not Alpine (OpenRC) or NixOS, which
manage services differently, and not macOS, which has no systemd.

Working from a clone with an already-built binary instead (no network
fetch — useful offline, on a fork, or on an unreleased branch)? Use
[`scripts/install.sh`](./scripts/install.sh), the same install reading local
files instead:

```bash
git clone https://github.com/clarkbar-sys/hush && cd hush
go build ./cmd/hush-agent ./cmd/hush-control

sudo ./scripts/install.sh agent            # hush-agent, systemd-managed
sudo ./scripts/install.sh control          # hush-control, LAN mode
sudo ./scripts/install.sh control-tsnet    # — or — hush-control, tsnet mode
sudo ./scripts/install.sh all              # agent + control (LAN mode), one box
```

## Staying up to date

`hush-control` knows its own version and checks it against the latest GitHub
release. Both binaries report it:

```bash
hush-control -version   # e.g. "hush-control v1.2.0"
hush-agent  -version
```

The console shows a version chip in the header; when a newer release exists it
lights up as `v1.2.0 → v1.3.0` and links to the release notes. The check is
read-only, cached for an hour, and done **only by hush-control** — one call
from the one box, never a fleet of agents hammering the API.

Every `hush-agent` already reports its own version in its vitals, so
`hush-control` compares each one against that same cached release check and
flags outdated agents on the console — an `↑ update` badge on the machine's
Fleet card, and a chip next to `agent vX.Y.Z` on its Machine page. Tapping
either opens a sheet with the command to update that specific box: the
one-line installer + a service restart on Linux, or `go install
.../cmd/hush-agent@latest` on macOS (no systemd there). Still no agent ever
calls GitHub itself — this reuses hush-control's existing cached check.

**Auto-update.** Both binaries carry the same self-update path. The `install.sh`
`control` / `control-tsnet` installs set up `hush-control-update.timer`, and the
`agent` install sets up `hush-agent-update.timer`; each runs a small **root**
oneshot (`hush-control -self-update` / `hush-agent -self-update`) daily. It
fetches the latest release, verifies the asset's SHA-256 against the digest
GitHub returns over its API, atomically swaps `/usr/local/bin/hush-control` (or
`hush-agent`), and restarts the service. The long-running processes stay
unprivileged (the `hush` user); these oneshots are the only piece that runs as
root, and only briefly — the agent still never calls GitHub from its
long-running, read-only service. Because the agent timer runs on every box in a
fleet rather than the single control host, its `RandomizedDelaySec` is widened
to 6h so a fleet's checks spread across the day instead of stampeding the API at
once. Trigger one on demand with `sudo systemctl start
hush-agent-update.service` (or `hush-control-update.service`), or pause
auto-updates with `sudo systemctl disable --now hush-agent-update.timer` (or the
`hush-control` one). A box with the timer paused still shows its update badge on
the console and can be updated by re-running the one-line installer.

## Components

| Path | What it is |
|---|---|
| `cmd/hush-agent` | one static Go binary per machine; reports vitals as JSON |
| `cmd/hush-control` | control plane on the NAS; fans out to agents, serves the UI |
| `internal/vitals` | Linux vitals collection (`/proc`, systemd, `nvidia-smi`) |
| `web/` | the console — a single static page |
| `docs/mockups/` | interactive UX reference (open directly for the demo fleet) |
| `systemd/` | unit files for running the binaries as services |
| `install.sh` | installs hush as a systemd service under a dedicated `hush` user (fetches binaries + units over the network; root required) |
| `scripts/install.sh` | same install, from a local clone with an already-built binary (no network fetch) |

## Development

```bash
go build ./...   # build everything
go vet ./...     # vet
go test ./...    # tests
```

## Releases

Releases are automated with
[release-please](https://github.com/googleapis/release-please). Commit using
[Conventional Commits](https://www.conventionalcommits.org/) (`feat:`, `fix:`,
`chore:` …) and a **Release PR** is opened and kept up to date automatically —
it bumps the version and updates [CHANGELOG.md](./CHANGELOG.md). Merging it
tags the release and cross-compiles + attaches the `install.sh` binaries for
linux/darwin, amd64/arm64.

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) and the
[Coding Standards](./STANDARDS.md). Please also read our
[Code of Conduct](./CODE_OF_CONDUCT.md).

## Security

Found a vulnerability? See [SECURITY.md](./SECURITY.md) — please do **not** open a
public issue for security reports.

## License

Distributed under the terms of the [GNU GPL v2](./LICENSE).
