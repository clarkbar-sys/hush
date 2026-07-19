#!/usr/bin/env sh
# Sets up one restic backup on this box as a systemd timer, following the
# layout hush-agent reads (see docs/BACKUP-CONVENTION.md).
#
# A backup is four small files, each named after the backup, plus one enabled
# timer instance — no fleet-wide config to edit, nothing to hand-write:
#
#   /etc/restic/<name>.env        credentials          (0600, root-only)
#   /etc/restic/<name>.paths      what to back up      (one path per line)
#   /etc/restic/<name>.excludes   what to skip
#   /var/lib/hush-backups/<name>.json   last-run status (world-readable)
#
# Run it interactively and it asks; pass flags and it doesn't:
#
#   sudo sh install-backup.sh
#   sudo sh install-backup.sh --name jaassh-nas \
#       --repo 'rest:http://nas:8000/jaassh/' --repo-user jaassh \
#       --paths '/etc /home /srv' --schedule 03:00
#
# DOWNLOAD IT, READ IT, THEN RUN IT. Unlike install.sh this script accepts a
# credential, so it is deliberately not offered as a `curl | sudo sh`
# one-liner — piping a privileged script straight into a shell means running
# code you never saw.
#
# Re-running is safe and is how you adopt an existing backup: an .env that
# already exists is reused, never clobbered, so the repository password is
# generated exactly once and survives every later run.
#
# Needs: root, systemd, and the restic binary. On immutable-root distros
# (SteamOS, Fedora Silverblue/Kinoite) /usr/local/bin is read-only, so the
# runner falls back to a writable directory automatically — same behaviour as
# install.sh. Force one with HUSH_BIN_DIR=/some/dir.

set -eu

CONF_DIR="${RESTIC_CONF_DIR:-/etc/restic}"
STATUS_DIR="${HUSH_BACKUP_STATUS_DIR:-/var/lib/hush-backups}"
UNIT_DIR="${HUSH_UNIT_DIR:-/etc/systemd/system}"

NAME=""
REPO=""
REPO_USER=""
REPO_PASS=""
PATHS=""
SCHEDULE="03:00"
DRY_RUN=0

usage() {
  sed -n '2,30p' "$0" | sed 's/^# \{0,1\}//'
  exit "${1:-0}"
}

while [ $# -gt 0 ]; do
  case "$1" in
    --name) NAME="${2:?--name needs a value}"; shift 2 ;;
    --repo) REPO="${2:?--repo needs a value}"; shift 2 ;;
    --repo-user) REPO_USER="${2:?--repo-user needs a value}"; shift 2 ;;
    --paths) PATHS="${2:?--paths needs a value}"; shift 2 ;;
    --schedule) SCHEDULE="${2:?--schedule needs a value}"; shift 2 ;;
    --dry-run) DRY_RUN=1; shift ;;
    -h|--help) usage 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage 2 ;;
  esac
done

run() {
  if [ "$DRY_RUN" -eq 1 ]; then
    echo "  [dry-run] $*"
  else
    "$@"
  fi
}

if [ "$DRY_RUN" -eq 0 ] && [ "$(id -u)" != "0" ]; then
  echo "error: must run as root — this installs a systemd unit and writes /etc/restic" >&2
  echo "  sudo sh $0" >&2
  exit 1
fi

if ! command -v restic >/dev/null 2>&1; then
  echo "error: restic not found on PATH." >&2
  echo "  Debian/Ubuntu: apt install restic   Fedora: dnf install restic" >&2
  echo "  Arch/SteamOS:  pacman -S restic     or fetch a release binary from" >&2
  echo "  https://github.com/restic/restic/releases" >&2
  exit 1
fi

# Percent-encode userinfo. restic's rest: backend takes HTTP auth only inside
# the URL, so a password containing @ : / % silently corrupts the repository
# address if pasted in raw.
urlencode() {
  printf '%s' "$1" | awk '
    BEGIN { for (i = 0; i < 256; i++) ord[sprintf("%c", i)] = i }
    {
      n = length($0)
      for (i = 1; i <= n; i++) {
        c = substr($0, i, 1)
        if (c ~ /[A-Za-z0-9._~-]/) printf "%s", c
        else printf "%%%02X", ord[c]
      }
    }'
}

prompt() {
  # prompt <varname> <question> [default]
  _p_default="${3:-}"
  if [ -n "$_p_default" ]; then
    printf '%s [%s]: ' "$2" "$_p_default" >&2
  else
    printf '%s: ' "$2" >&2
  fi
  # EOF (non-interactive, or piped stdin) must fall back to the default rather
  # than tripping set -e halfway through a prompt.
  read -r _p_reply || _p_reply=""
  [ -n "$_p_reply" ] || _p_reply="$_p_default"
  eval "$1=\$_p_reply"
}

[ -n "$NAME" ] || prompt NAME "Backup name (e.g. jaassh-nas)"
case "$NAME" in
  ''|*[!A-Za-z0-9._-]*)
    echo "error: name must be non-empty and only [A-Za-z0-9._-]: '$NAME'" >&2
    exit 2 ;;
esac

ENV_FILE="$CONF_DIR/$NAME.env"
PATHS_FILE="$CONF_DIR/$NAME.paths"
EXCLUDES_FILE="$CONF_DIR/$NAME.excludes"

mkdir -p "$CONF_DIR"
chmod 0700 "$CONF_DIR"

# --- credentials -----------------------------------------------------------
# An existing .env wins, always. The repository password is unrecoverable if
# lost, so regenerating one over a working backup would orphan every snapshot
# already in the repo.
GENERATED_PASSWORD=""
if [ -f "$ENV_FILE" ]; then
  echo "==> reusing existing $ENV_FILE (repository password left untouched)"
else
  [ -n "$REPO" ] || prompt REPO "Repository URL, no credentials (e.g. rest:http://nas:8000/jaassh/)"
  [ -n "$REPO" ] || { echo "error: repository URL is required" >&2; exit 2; }

  if [ -z "$REPO_USER" ]; then
    prompt REPO_USER "Repository HTTP username (blank for none)" ""
  fi
  if [ -n "$REPO_USER" ]; then
    printf 'Repository HTTP password for %s: ' "$REPO_USER" >&2
    stty -echo 2>/dev/null || true
    read -r REPO_PASS || REPO_PASS=""
    stty echo 2>/dev/null || true
    echo >&2
    # rest:http://host/path -> rest:http://user:pass@host/path
    _scheme="${REPO%%://*}"
    _rest="${REPO#*://}"
    REPO="$_scheme://$(urlencode "$REPO_USER"):$(urlencode "$REPO_PASS")@$_rest"
  fi

  GENERATED_PASSWORD="$(head -c 24 /dev/urandom | od -An -tx1 | tr -d ' \n')"
  umask 077
  cat >"$ENV_FILE" <<EOF
RESTIC_REPOSITORY=$REPO
RESTIC_PASSWORD=$GENERATED_PASSWORD
RESTIC_CACHE_DIR=/var/cache/restic
EOF
  chmod 0600 "$ENV_FILE"
  umask 022
  echo "==> wrote $ENV_FILE (0600)"
fi

mkdir -p /var/cache/restic

# --- what to back up -------------------------------------------------------
if [ ! -f "$PATHS_FILE" ]; then
  [ -n "$PATHS" ] || prompt PATHS "Paths to back up, space separated" "/etc /home"
  : >"$PATHS_FILE"
  for p in $PATHS; do
    printf '%s\n' "$p" >>"$PATHS_FILE"
  done
  chmod 0644 "$PATHS_FILE"
  echo "==> wrote $PATHS_FILE"
else
  echo "==> keeping existing $PATHS_FILE"
fi

if [ ! -f "$EXCLUDES_FILE" ]; then
  cat >"$EXCLUDES_FILE" <<'EOF'
# One pattern per line. A bare name matches at any depth.
# Everything here is rebuildable from something that IS backed up.
node_modules
__pycache__
*.pyc
.venv
.npm
.cache
/var/cache
/var/tmp
EOF
  chmod 0644 "$EXCLUDES_FILE"
  echo "==> wrote $EXCLUDES_FILE (edit to taste)"
else
  echo "==> keeping existing $EXCLUDES_FILE"
fi

# --- runner ----------------------------------------------------------------
# Mirrors install.sh: /usr/local/bin first, then a writable fallback for
# immutable-root distros.
pick_bin_dir() {
  if [ -n "${HUSH_BIN_DIR:-}" ]; then
    echo "$HUSH_BIN_DIR"; return
  fi
  for d in /usr/local/bin /opt/hush/bin /var/lib/hush/bin; do
    if mkdir -p "$d" 2>/dev/null && [ -w "$d" ]; then
      echo "$d"; return
    fi
  done
  echo "error: no writable directory for the runner; set HUSH_BIN_DIR" >&2
  exit 1
}

if [ "$DRY_RUN" -eq 1 ]; then
  BIN_DIR="${HUSH_BIN_DIR:-/usr/local/bin}"
else
  BIN_DIR="$(pick_bin_dir)"
fi
RUNNER="$BIN_DIR/restic-backup-run"

SRC_RUNNER="$(dirname "$0")/restic-backup-run"
if [ -r "$SRC_RUNNER" ]; then
  run install -m 0755 "$SRC_RUNNER" "$RUNNER"
  echo "==> installed $RUNNER"
else
  echo "error: cannot find restic-backup-run beside $0" >&2
  exit 1
fi

mkdir -p "$STATUS_DIR"
chmod 0755 "$STATUS_DIR"

# --- units -----------------------------------------------------------------
# Template units: one pair serves every backup on the box, so adding the next
# one enables an instance rather than writing another unit.
mkdir -p "$UNIT_DIR"
cat >"$UNIT_DIR/restic-backup@.service" <<EOF
[Unit]
Description=restic backup: %i
Documentation=https://github.com/clarkbar-sys/hush/blob/main/docs/BACKUP-CONVENTION.md
After=network-online.target
Wants=network-online.target

[Service]
Type=oneshot
EnvironmentFile=$CONF_DIR/%i.env
ExecStart=$RUNNER %i
Nice=10
IOSchedulingClass=idle
EOF

cat >"$UNIT_DIR/restic-backup@.timer" <<'EOF'
[Unit]
Description=scheduled restic backup: %i

[Timer]
OnCalendar=*-*-* 03:00:00
Persistent=true
RandomizedDelaySec=10m

[Install]
WantedBy=timers.target
EOF

# Per-instance schedule, so instances don't share the template's time.
DROPIN="$UNIT_DIR/restic-backup@$NAME.timer.d"
mkdir -p "$DROPIN"
cat >"$DROPIN/schedule.conf" <<EOF
[Timer]
OnCalendar=
OnCalendar=*-*-* $SCHEDULE:00
EOF
echo "==> units written, schedule $SCHEDULE daily"

# --- repository ------------------------------------------------------------
if [ "$DRY_RUN" -eq 1 ]; then
  echo "  [dry-run] restic init (if not already initialised)"
else
  set -a
  # shellcheck disable=SC1090
  . "$ENV_FILE"
  set +a
  if restic cat config >/dev/null 2>&1; then
    echo "==> repository already initialised"
  else
    echo "==> initialising repository"
    restic init
  fi
fi

run systemctl daemon-reload
run systemctl enable --now "restic-backup@$NAME.timer"

echo
echo "done: restic-backup@$NAME"
echo "  run now:  systemctl start restic-backup@$NAME.service"
echo "  watch:    journalctl -u restic-backup@$NAME -f"
echo "  status:   $STATUS_DIR/$NAME.json"

if [ -n "$GENERATED_PASSWORD" ]; then
  cat <<EOF

  ============================================================
  ESCROW THIS NOW — repository password for '$NAME':

      $GENERATED_PASSWORD

  It exists in exactly two places: $ENV_FILE on this box, and
  wherever you paste it. This box is the one the backup exists
  to survive. Lose this and every snapshot in the repository is
  unrecoverable, by design.
  ============================================================
EOF
fi
