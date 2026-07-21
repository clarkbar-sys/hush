# Jobs convention

How a box declares a long-running job so that hush can *report* it without
needing privilege or the power to start one.

A box runs work that is neither a backup nor a coding-agent session: a
multi-hour model download, a build, a migration. Today that work is invisible —
the only way to know how it is going is to SSH in and look at file sizes, which
is the thing the console exists to spare you.

This follows [the backup convention](./BACKUP-CONVENTION.md) deliberately: the
job writes a secret-free status file, and the unprivileged agent only ever reads
it. See [Why hush reads instead of runs](#why-hush-reads-instead-of-runs).

## The shape

A job is one status file named after it, plus a progress file while it runs.

```
/var/lib/hush-jobs/<name>.json            last-run status   0644, no secrets
/var/lib/hush-jobs/<name>.progress.json   live progress     0644, only while running
```

No unit files and no fleet-wide config. A job does not have to be a systemd
service — see [Liveness](#liveness-is-a-heartbeat-not-systemd) for what that
costs.

## Status file

```json
{
  "name": "llama-models",
  "started": "2026-07-20T12:15:03Z",
  "finished": "2026-07-21T06:31:44Z",
  "exit_code": 0,
  "ok": true,
  "note": "3 models staged"
}
```

While the job runs it carries `"state":"running"` and no outcome fields. `note`
is free text — the job's one-line account of what it is doing right now. Free
rather than structured because the alternative is a schema every future job has
to be bent to fit.

## Live progress

```json
{"name":"llama-models","updated":"2026-07-20T20:12:04Z","bytes_done":19540000000,
 "total_bytes":49300000000,"percent_done":0.3963,"note":"Qwen3-Coder-Next"}
```

Republished every few seconds and **deleted when the job ends**. Without it the
console can only draw an indeterminate shuttle — honest, but useless on a job
that runs for a day.

`percent_done` is emitted only when it can be computed. A job of unknown total
size omits it rather than reporting `0`, because 0% reads as stuck rather than
as unmeasured.

The agent passes this object through as raw JSON, so a job that starts
publishing a new field reaches the console with no Go change.

## Liveness is a heartbeat, not systemd

This is the one place the convention diverges from backups, and it is worth
understanding before relying on it.

A convention backup is a systemd template unit, so the agent can ask systemd
what is running and get a true answer even from a runner that never cooperated.
A job has no such authority behind it — it may be a detached shell script
systemd has never heard of. Liveness therefore comes from the freshness of
`updated` in the progress file.

The consequence: **a job whose writer dies goes `stale`, not `failed`.** The
agent downgrades a job that claims to be running but has not published within
two minutes, and withholds its last sample. A frozen percentage presented as
live is a worse lie than no percentage — this is the same judgement the backup
convention makes about a progress file that outlived its run.

What this loses, relative to backups: a job killed before it ever published is
indistinguishable from one that was never started, and nothing here can report
"this job should be running and isn't". If a job needs that guarantee, make it a
systemd unit and give it a timer, where systemd is the box's own record of what
executed.

## Publishing

```sh
hush-job-publish start    <name> [note]
hush-job-publish progress <name> <bytes_done> <total_bytes> [note]
hush-job-publish finish   <name> <exit_code> [note]
```

[`scripts/hush-job-publish`](../scripts/hush-job-publish) needs no privilege
beyond write access to the jobs directory, and depends on neither `jq` nor
`python` — it only ever emits JSON, never parses it. A status writer that has to
parse JSON in POSIX shell is exactly how malformed output starts.

Writes are atomic (temp file plus `rename`), because a reader polls this
directory on a timer with no lock between them: a half-written file would
otherwise eventually be read, showing up as the occasional unexplained parse
error in the agent's log.

### The directory

```
sudo groupadd -f hush-jobs
sudo install -d -m 2775 -o root -g hush-jobs /var/lib/hush-jobs
sudo usermod -aG hush-jobs <user-that-runs-jobs>
```

The setgid bit (`2775`) is load-bearing: it makes every file created in the
directory inherit the `hush-jobs` group, so a publisher does not have to
remember to fix ownership and a second user's job is readable by the first.

World-writable (`1777`) would avoid the group entirely and is deliberately not
used — a directory the console reads from should not be one that any local
process can drop files into.

## How hush reads it

`hush-agent` serves this box's jobs as a JSON array on **`GET /jobs`**. It is
ungated, for the same reason `/backup-status` is: reading a status file that
holds no secrets carries none of the risk gating exists to manage, and a box
should be able to report its jobs without also granting the agent the power to
start one. Point it elsewhere with `-jobs-dir`.

`hush-control` proxies it verbatim at **`GET /api/machines/{host}/jobs`**, the
same shape as `/top` and `/sessions`. An agent too old to know the endpoint
answers 404, relayed as-is, so the console can say "update the agent" rather
than drawing the machine as having no jobs — a partial rollout must not look
like a quiet fleet.

**Per-machine, not a fleet rollup** — the opposite of `/api/backup-status`, and
deliberately. A backup is a direction (this box → that store) and the question a
reader has is fleet-shaped: "is everything backed up?". A job is something
happening on a box you are already looking at; nobody asks "is the fleet
downloading?". Aggregating would cost a fan-out on every poll to answer a
question nobody has.

## Why hush reads instead of runs

The same reasoning as backups, and the same conclusion the removed "run things"
half reached: hush composes commands, and you run them.

A job worth tracking is usually a job that needs privilege the agent does not
have, or that outlives any request the console could hold open. Running it from
the agent would mean granting the console's access domain the power to execute
arbitrary long work on every box in the fleet — to gain a progress bar.

So the job runs with whatever privilege it actually needs, started by whoever
owns it, and hush stays a reader.

## Not yet implemented

History. A job reports its current and last run only; there is no
`<name>.history.jsonl` the way backups have one. Added when a job proves it
needs it — a download that runs once does not.
