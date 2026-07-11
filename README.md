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

## Getting started

```bash
git clone https://github.com/clarkbar-sys/hush.git
cd hush

# 1. run an agent on a machine you want to watch
go run ./cmd/hush-agent -listen 127.0.0.1:8765

# 2. run the control plane serving the UI (defaults to one local agent)
go run ./cmd/hush-control -listen 127.0.0.1:8080 -web web

# 3. open the console
open http://127.0.0.1:8080
```

To watch a real fleet, copy `fleet.example.json` to `fleet.json`, list your
agents' tailnet addresses, and start `hush-control`. In production each
`hush-agent` binds to the tailnet interface — no public exposure.

### Serve over the tailnet (HTTPS)

The steps above run **LAN mode**: plain HTTP, unauthenticated — trusted
networks only. For the secure, reach-from-anywhere console, run `hush-control`
in **tsnet mode**: it joins the tailnet as its own node and serves HTTPS on
`:443` with a real cert at `https://<hostname>.<tailnet>.ts.net`.

```bash
# provision the node with an auth key; persist its state in -state-dir
TS_AUTHKEY=tskey-auth-… go run ./cmd/hush-control -tsnet -hostname hush -state-dir ./tsstate

# optionally restrict to specific operators (repeatable; omit = any tailnet member)
TS_AUTHKEY=tskey-auth-… go run ./cmd/hush-control -tsnet -allow you@example.com
```

Every request is gated by Tailscale identity (`WhoIs`). **Prerequisites:**
[MagicDNS](https://tailscale.com/kb/1081/magicdns) and
[HTTPS certificates](https://tailscale.com/kb/1153/enabling-https) enabled in
your tailnet. The node is served **tailnet-only** — hush never uses Tailscale
Funnel. See [`docs/DESIGN.md`](./docs/DESIGN.md#run-modes) for details.

## Components

| Path | What it is |
|---|---|
| `cmd/hush-agent` | one static Go binary per machine; reports vitals as JSON |
| `cmd/hush-control` | control plane on the NAS; fans out to agents, serves the UI |
| `internal/vitals` | Linux vitals collection (`/proc`, systemd, `nvidia-smi`) |
| `web/` | the console — a single static page |
| `docs/mockups/` | interactive UX reference (open directly for the demo fleet) |

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
it bumps the version and updates [CHANGELOG.md](./CHANGELOG.md).

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md) and the
[Coding Standards](./STANDARDS.md). Please also read our
[Code of Conduct](./CODE_OF_CONDUCT.md).

## Security

Found a vulnerability? See [SECURITY.md](./SECURITY.md) — please do **not** open a
public issue for security reports.

## License

Distributed under the terms of the [GNU GPL v2](./LICENSE).
