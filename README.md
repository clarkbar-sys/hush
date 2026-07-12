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

You don't have to edit `fleet.json` by hand. The console's **⊕ Add machine**
sheet takes a tailnet address, probes it to confirm a `hush-agent` is answering,
and persists it to `fleet.json` — the new machine shows up on the next poll, no
restart needed.

In **tsnet mode** the sheet can also find agents for you: **Scan tailnet** reads
your tailnet's device list (the same table Tailscale keeps, much like DHCP
leases), probes each online node on the agent port, and lists the ones running
`hush-agent` that aren't in your fleet yet. Tap one to add it — no IP hunting.
Discovery needs the tailnet handle tsnet provides, so the scan button falls back
to manual entry in LAN mode.

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

**Auto-update (hush-control only).** The `install.sh` `control` /
`control-tsnet` installs also set up `hush-control-update.timer`, which runs a
small **root** oneshot (`hush-control -self-update`) daily. It fetches the
latest release, verifies the asset's SHA-256 against the digest GitHub returns
over its authenticated API, atomically swaps `/usr/local/bin/hush-control`, and
restarts the service. The long-running control process stays unprivileged (the
`hush` user); this oneshot is the only piece that runs as root, and only
briefly. Trigger one on demand with `sudo systemctl start
hush-control-update.service`, or pause auto-updates with `sudo systemctl
disable --now hush-control-update.timer`. Agents are intentionally left out —
they stay read-only and are updated by re-running the one-line installer.

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
