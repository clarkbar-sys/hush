# hush

> Know your fleet is backed up — from your phone.

[![CI (Go)](https://github.com/clarkbar-sys/hush/actions/workflows/ci-go.yml/badge.svg)](https://github.com/clarkbar-sys/hush/actions/workflows/ci-go.yml)
[![License](https://img.shields.io/badge/license-GPL--2.0-blue.svg)](./LICENSE)

**[Latest release &rarr;](https://clarkbar-sys.github.io/hush/)**

## Overview

`hush` is a phone-first **backup console** for a homelab of
[Tailscale](https://tailscale.com) machines. It answers the question a homelab
usually can't at a glance — *is everything backed up, and could I get it back?* —
on a legible map where every machine is coloured by its **backup posture**
(protected, at risk, unprotected, failed), so the box that isn't safe is the one
that stands out. It grew out of a general "see and run your fleet" console
(*Factorio for your homelab*), and the read-only spine of that — machines,
stores, and links — is still here; backups are the point now.

See [`docs/DESIGN.md`](./docs/DESIGN.md) for the design and
[issue #6](https://github.com/clarkbar-sys/hush/issues/6) for the full
initiative.

**Status:** hush is a **read-only backup monitor** for the fleet. Backups are
restic runs set up on each box over SSH (the
[backup convention](./docs/BACKUP-CONVENTION.md)); the console reads their status
and makes it legible — the fleet map leads with backup posture, and a header
**alert bell** ranks the backups that need attention, so the box that isn't safe
is the one that stands out. The read-only substrate is live too — machine
vitals, file browsing, a windirstat-style disk-usage treemap, and a live
htop-style CPU/network panel per machine. Next up: push/email alert delivery,
cross-site replication, and retention policy. See
[`docs/DESIGN.md#roadmap`](./docs/DESIGN.md#roadmap) for the full picture.

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
it's opt-in: pass `control-tsnet` to serve the console over the tailnet (see
[Serve over the tailnet](#serve-over-the-tailnet-https)), e.g.
`... | sudo sh -s -- control-tsnet`. `control` is an alias for the same thing —
the old plain-HTTP LAN mode has been removed, so `hush-control` only ever serves
over the tailnet now. Pass `all` to install both on one box.
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

`hush-control` serves the console **only over the tailnet** — it joins the
tailnet as its own node and serves HTTPS with a real cert at
`https://<hostname>.<tailnet>.ts.net`, gated on Tailscale identity. (There is no
plain-HTTP LAN mode; the only plain-HTTP surface is the one-time first-run setup
page, below.)

```bash
# 1. run an agent on each machine you want to watch (binds this box's tailnet IP)
hush-agent -listen tailnet

# 2. run the control plane, joining the tailnet as a node named "hush"
TS_AUTHKEY=tskey-auth-… hush-control -tsnet -hostname hush -state-dir ./tsstate

# 3. open the console at the tailnet URL it prints
open https://hush.<your-tailnet>.ts.net
```

Each `hush-agent` binds the tailnet interface — no public, no LAN exposure.
`-listen tailnet` (or `HUSH_AGENT_LISTEN=tailnet`) resolves this machine's
Tailscale IP automatically, waiting for `tailscaled` to come up on boot rather
than hardcoding a `100.x` address. This is the default for `install.sh`, so a
freshly installed agent is discoverable over the tailnet with no post-install
edit. Use `tailnet:PORT` for a non-default port, or a literal `host:port` (e.g.
`127.0.0.1:8765`) to pin a specific interface for local testing.

To watch a real fleet, copy [`fleet.example.json`](./fleet.example.json) to
`fleet.json` and list your agents' tailnet addresses — or just add them from the
console's **＋ Build → Machine** sheet once it's up.

Working from a clone instead? Swap `hush-control` for `go run ./cmd/hush-control`
(and likewise for the agent). Add `-web web` to serve the UI from the on-disk
`web/` directory when you want to iterate on it live.

### Serve over the tailnet (HTTPS)

`hush-control` joins the tailnet as its own node and serves HTTPS on `:443` with
a real cert at `https://<hostname>.<tailnet>.ts.net`:

```bash
# provision the node with an auth key; persist its state in -state-dir
TS_AUTHKEY=tskey-auth-… hush-control -tsnet -hostname hush -state-dir ./tsstate

# optionally restrict to specific operators (repeatable; omit = any tailnet member)
TS_AUTHKEY=tskey-auth-… hush-control -tsnet -allow you@example.com
```

**First run from your phone (no auth key on the command line).** Starting
`hush-control` with no `TS_AUTHKEY` and no saved node state serves a one-time
**setup page** on the LAN (`-listen`, default `:8080`) — no SSH, no editing env
files. Open `http://<box-ip>:8080` in a browser, paste a
[Tailscale auth key](https://login.tailscale.com/admin/settings/keys) and the
hostname, and the same process joins the tailnet and bounces you to the HTTPS
URL. This setup page is the **only** plain-HTTP surface hush ever exposes — it
wears a warning banner and exists **only until the node is provisioned**, after
which it never reappears. The `install.sh` `control-tsnet` install ships with an
empty `TS_AUTHKEY`, so this is the default first-run experience.

(`-tsnet` is now implied — it's kept only as an accepted, ignored flag so units
that still pass it keep working. Serving over the tailnet is unconditional.)

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

The sheet can also find agents for you: **Scan tailnet** reads your tailnet's
device list (the same table Tailscale keeps, much like DHCP leases), probes each
online node on the agent port, and lists the ones running `hush-agent` that
aren't in your fleet yet. Tap one to add it — no IP hunting. Discovery uses the
tailnet handle the tsnet node provides, so it's available as soon as the console
is up.

`hush-control` also rescans the tailnet in the background, so the **＋ Build**
button carries a count badge when new agents appear that you haven't
added — you don't have to open the sheet to notice a fresh box. Adding stays a
deliberate tap; discovery only ever suggests.

### Back up a machine (Backups)

hush **watches** your backups; it doesn't run them. A backup needs root and holds
a repository credential — and `hush-agent` runs unprivileged and must never hold
that — so backups are set up **on the box, over SSH**, and the console reports
their status read-only. Every Machine view's **Backups** section shows that box's
scheduled [restic](https://restic.net) backups: posture (protected / at risk /
failed), the last run and where it ships to, and a strip of recent runs. The
Fleet view rolls them all up and floats the box that isn't safe to the top, with
a header **alert bell** for anything that needs attention.

#### Setting up a backup

`scripts/install-backup.sh` writes the whole convention in one go — repo
credentials, the paths, a schedule, and a `restic-backup@<name>` systemd timer.
Download and read it first: it takes a credential and runs as root, so it's
deliberately **not** a `curl | sudo sh` one-liner.

```bash
sudo sh scripts/install-backup.sh --name deck-nas \
    --repo 'rest:http://nas:8000/deck/' --repo-user deck \
    --paths '/home/deck' --schedule 04:00
```

Run it with no flags to be prompted for each. The box you back up *to* is
typically a [rest-server](https://github.com/restic/rest-server) on the NAS in
**append-only** mode, so a compromised source can add snapshots but never delete
old ones:

```bash
# on the NAS — one repo host for the whole fleet, reached over the tailnet
rest-server --path /srv/restic --listen :8000 --append-only \
  --htpasswd-file /srv/restic/.htpasswd
```

`sftp:` and local/mounted paths work too, without the append-only guarantee. The
exact files, the secret-free status-file contract, and how the agent reads it are
in [`docs/BACKUP-CONVENTION.md`](./docs/BACKUP-CONVENTION.md).

**The key lives on the box.** restic encrypts with a password that
`install-backup.sh` generates once and stores in `/etc/restic/<name>.env` (root,
`0600`) — the same box the backup exists to survive, so its disk holds the *only*
copy. The script prints it once so you can escrow it off-box (a password manager,
not a box you back up). Lose it and the snapshots are unrecoverable by design.

**Restore** the same way you set up — on the box, over SSH, where the key already
is: `restic -r <repo> restore <snapshot> --target <dir>`. The console tells you
which snapshot ran clean; the recovery happens where the credential lives.

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
sudo ./scripts/install.sh control-tsnet    # hush-control, over the tailnet (HTTPS)
sudo ./scripts/install.sh control          # alias for control-tsnet
sudo ./scripts/install.sh all              # agent + control, one box
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
