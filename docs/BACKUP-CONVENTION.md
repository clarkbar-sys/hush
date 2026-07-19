# Backup convention

How a box declares a restic backup so that hush can *report* it without ever
holding a credential or needing privilege.

This is the counterpart to the [Backup construct](./DESIGN.md#backups--restic-the-first-slice),
not a replacement for it — see [Why not the Backup construct?](#why-not-the-backup-construct)
for when each applies.

## The shape

A backup on a box is four files named after it, plus one enabled timer
instance. Nothing else, and no fleet-wide config to edit.

```
/etc/restic/<name>.env               credentials             0600 root
/etc/restic/<name>.paths             what to back up         one path per line
/etc/restic/<name>.excludes          what to skip            one pattern per line
/var/lib/hush-backups/<name>.json    last-run status         0644, no secrets
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
  "started": "2026-07-19T03:00:04-04:00",
  "finished": "2026-07-19T03:07:41-04:00",
  "exit_code": 0,
  "ok": true,
  "incomplete": false,
  "summary": { "...": "restic's own --json summary, embedded verbatim" }
}
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

## Why not the Backup construct?

hush's [Backup construct](./DESIGN.md#backups--restic-the-first-slice) runs
restic *inside* `hush-agent`. That is the right tool when the data is readable
by the agent's unprivileged user and you want create/run/schedule from the
phone.

It cannot be the tool for a whole-box backup, for two reasons:

1. **Reach.** The agent runs unprivileged, so restic under it can only read
   what that user can read. On a box with per-service home directories and
   `0700` user data, that excludes most of what a backup is for.
2. **Credentials.** Widening the agent's access is worse than it sounds,
   because the agent also serves `/exec` as that same user — so anything the
   agent can read becomes readable through the console. A backup should not be
   the reason personal data enters a console's access domain.

This convention exists for that case: the backup runs with the privilege it
actually needs, and hush stays a reader.

## Not yet implemented

`hush-agent` does not read `/var/lib/hush-backups/` yet — this change ships the
convention and the installer only. Discovery, the `/backups` surface, and the
fleet view are the next slice.
