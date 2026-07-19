# Backup convention

How a box declares a restic backup so that hush can *report* it without ever
holding a credential or needing privilege.

This is how backups work in hush: set up on the box, run with the privilege they
actually need, and *read* by the unprivileged agent — never run by it. See
[Why hush reads instead of runs](#why-hush-reads-instead-of-runs) for the reasoning.

## The shape

A backup on a box is four files named after it, plus one enabled timer
instance. Nothing else, and no fleet-wide config to edit.

```
/etc/restic/<name>.env                       credentials       0600 root
/etc/restic/<name>.paths                     what to back up   one path per line
/etc/restic/<name>.excludes                  what to skip      one pattern per line
/var/lib/hush-backups/<name>.json            last-run status   0644, no secrets
/var/lib/hush-backups/<name>.history.jsonl   past runs         0644, one per line
```

```
restic-backup@<name>.timer           enabled, scheduled
restic-backup@<name>.service         oneshot, runs the backup
```

Template units, so the second backup on a box enables an instance rather than
adding another pair of unit files.

## Why the split

The backup itself needs root: it reads `/home`, service state dirs, and other
things an unprivileged user cannot. `hush-agent` runs unprivileged by design
and must never hold a repo credential.

So privilege flows one way and information flows the other. The privileged
side writes a status file containing **no secrets**; the agent only ever reads
that file. Nothing needs to be granted to the agent, and nothing about the
agent needs to change to support a new backup.

The status file's `repository` field has its userinfo stripped, because
restic's `rest:` backend carries HTTP auth inline in the URL — so the raw
repository string is itself a credential.

## Status file

```json
{
  "name": "jaassh-nas",
  "repository": "rest:http://nas:8000/jaassh/",
  "paths": ["/etc", "/home/josh", "/data/gage"],
  "started": "2026-07-19T03:00:04-04:00",
  "finished": "2026-07-19T03:07:41-04:00",
  "exit_code": 0,
  "ok": true,
  "incomplete": false,
  "summary": { "...": "restic's own --json summary, embedded verbatim" }
}
```

`paths` is what the console shows a backup as covering. These are not secrets,
but they do describe the box's layout to any local user — the file is
world-readable so the unprivileged agent can read it. A box that considers its
directory names sensitive should point `HUSH_BACKUP_STATUS_DIR` somewhere
tighter and adjust the agent to match.

### History

`<name>.history.jsonl` holds one line per run, appended and trimmed on every
write (`HUSH_BACKUP_HISTORY_KEEP`, default 30). A separate file rather than an
array inside the status JSON, because appending a line needs no JSON parsing —
and parsing JSON in POSIX shell, with no `jq` or `python` to lean on, is exactly
how a status writer starts emitting malformed output.

```json
{"finished":"…","exit_code":0,"ok":true,"incomplete":false,"summary":{…}}
```

`summary` is restic's final `--json` summary line, stored as-is rather than
reparsed — no `jq` or `python` dependency on the box, and no chance of
mangling it in shell. It carries `snapshot_id`, `files_new`, `data_added`,
`total_duration` and friends.

**`incomplete` deserves its own field.** restic exits **3** when some source
data could not be read: a snapshot exists, but it is missing files. That is
the most dangerous outcome a backup has, because every summary count looks
plausible and the snapshot is really there. It is reported as a failure —
`ok: false` — on purpose.

## Adding a backup

```
sudo sh scripts/install-backup.sh
```

It asks for a name, a repository URL, credentials, paths, and a schedule, then
writes the files above, initialises the repository, and enables the timer.
Flags skip the questions:

```
sudo sh scripts/install-backup.sh --name deck-nas \
    --repo 'rest:http://nas:8000/deck/' --repo-user deck \
    --paths '/home/deck' --schedule 04:00
```

**Download it and read it before running it.** It takes a credential and runs
as root, so it is deliberately not offered as a `curl | sudo sh` one-liner the
way [`install.sh`](../install.sh) is.

Re-running is safe, and is how you adopt a backup that was set up by hand: an
existing `.env` is reused and never clobbered, so the repository password is
generated exactly once.

The script prints the generated repository password once, for escrow. **That
password lives on the box the backup exists to survive.** Losing it makes every
snapshot in the repository unrecoverable — that is restic's design, not a bug.
Put it somewhere that is not a box you back up.

## Why hush reads instead of runs

hush once ran restic *inside* `hush-agent` — a "Backup construct" with
create / run / restore driven from the phone. It was removed, because running the
backup from the agent can't be right for a whole-box backup, for two reasons:

1. **Reach.** The agent runs unprivileged, so restic under it can only read
   what that user can read. On a box with per-service home directories and
   `0700` user data, that excludes most of what a backup is for — the backup has
   to run as root to reach it.
2. **Credentials.** For the agent to run the backup it must hold the repository
   key — on the very box the backup exists to survive, in reach of a process the
   console can talk to. A backup should not be the reason a decryption key enters
   the console's access domain.

So the backup runs with the privilege it actually needs (root, on the box), and
hush stays a reader: the agent holds no key and only reports the secret-free
status file.

## How hush reads it

`hush-agent` serves this box's status files as a JSON array on
**`GET /backup-status`**. It is ungated — unlike `/backups` it neither runs
restic nor exposes a secret, so a box reports its backups without also having to
grant the agent the power to make new ones. Point it elsewhere with
`-backup-status-dir`.

`hush-control` aggregates every agent's into **`GET /api/backup-status`**, one
entry per machine, each marked `reachable` so a box that cannot be asked is
*named* rather than quietly dropped. An agent too old to know the endpoint reads
as reachable with no backups, so a partial rollout doesn't draw healthy machines
as broken.

The agent adds two fields the status file cannot hold: `history` (from the log
above) and `next_run`, which only systemd can answer.

`next_run` comes from `systemctl list-timers --output=json`, **not** from
`systemctl show --property=NextElapseUSecRealtime`. Despite the name, that
property renders a locale- and timezone-formatted string
(`Mon 2026-07-20 00:00:03 EDT`), so a numeric parse of it fails on every box —
and behind a silent fallback that means the field is simply never populated.
It is queried live rather than recorded at write time, because a recorded value
goes stale the moment the schedule changes.

The agent also asks systemd **which backups are running right now**, with
`systemctl list-units 'restic-backup@*.service' --output=json`, and marks those
`"state":"running"` regardless of what the status file says.

This is deliberately not left to the runner's own start marker. That marker only
works once the *runner* is current, and the agent self-updates its binary
without ever refreshing `/usr/local/bin/restic-backup-run` — so a box can carry
a new agent and an old runner and report nothing at all while it works. It also
cannot help the case that matters most: a backup that has never finished has
written no status file, so the directory is empty, and an empty directory
already means "no backups configured, this box is unprotected". A box's first
backup is the longest it will ever run, and it was invisible for all of it.
systemd needs no cooperation from the runner and is right about a run that began
before the agent did. A run it finds with no status file is reported from the
unit alone.

The same trap applies here: the start time comes from
`systemctl show --property=ExecMainStartTimestamp --timestamp=unix`, and
**`--timestamp=unix` is required** — the default rendering is the same
locale-formatted string. Note too that these are `Type=oneshot` units, so a run
in flight reports `ActiveState=activating`, *not* `active`; a check for `active`
is false for the entire life of every run.

systemd only ever **adds** `running` here, never clears it. A backup run by hand
— the restore drill above — has no unit at all, so "no active unit" does not
mean "not running". Deciding that a `running` marker has been orphaned stays
with the console, which ages it out against `next_run`.

The console shows them in a **Backups** rollup on the fleet view, which opens
itself when something is wrong. Each card reads `source → target`, the target
being pulled from the redacted repository URL, with the last fortnight of runs
as a strip.

Every field is drawn only when the box actually reported it. An older agent, a
first run, or a box without systemd shows less — rather than the console
inventing a plausible number.

Statuses ride through both hops as raw JSON, so neither hush-control nor the
console needs to know restic's schema — a new field in the status file reaches
the screen without a Go change.

## Not yet implemented

Retention. Append-only repositories cannot be pruned by the client that writes
them, so a repo grows until someone prunes it server-side. Nothing here reports
repository size or snapshot count over time.
