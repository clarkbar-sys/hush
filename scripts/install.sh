#!/usr/bin/env bash
# Installs hush-agent and/or hush-control as systemd services, running under
# a dedicated, unprivileged "hush" system user rather than root. Same install
# as the root-level install.sh, but for a local clone with an already-built
# binary — it reads the unit files and fleet.example.json from disk instead
# of fetching them over the network, so it works offline / on forks / on
# unreleased branches.
#
# Usage:
#   sudo ./scripts/install.sh agent           # hush-agent only
#   sudo ./scripts/install.sh control-tsnet   # hush-control only (tailnet/tsnet)
#   sudo ./scripts/install.sh control         # alias for control-tsnet
#   sudo ./scripts/install.sh all             # agent + control, one box
#
# hush-control serves the console only over the tailnet (tsnet); the old
# plain-HTTP LAN mode has been removed, so "control" == "control-tsnet".
#
# Binaries must already be built (see README's "Install" section: either
# `go install ./cmd/...` with $GOBIN on $PATH, or `go build ./cmd/...`).
# Override the source binary with HUSH_AGENT_BIN / HUSH_CONTROL_BIN if it
# isn't on $PATH.
#
# Binaries install to /usr/local/bin by default. On immutable-root distros
# (SteamOS, Fedora Silverblue/Kinoite) /usr is read-only, so the installer
# falls back to a writable directory and rewrites the unit's ExecStart to
# match. Force a specific location with HUSH_BIN_DIR=/some/dir.
#
# Targets systemd + shadow-utils (useradd) distros: Debian, Ubuntu, Fedora,
# Arch, RHEL, openSUSE. Not Alpine (OpenRC) or NixOS (declarative modules).
#
# Re-running is safe: it never overwrites an existing /etc/hush/*.env file,
# so local edits survive upgrades.

set -euo pipefail

SERVICE_USER="${HUSH_USER:-hush}"
SERVICE_GROUP="${HUSH_GROUP:-hush}"
CONFIG_DIR="/etc/hush"
UNIT_DIR="/etc/systemd/system"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "error: must run as root (sudo)" >&2
    exit 1
  fi
}

# resolve_bin_dir picks where the binaries land and sets BIN_DIR. The usual
# /usr/local/bin is unwritable on immutable-root distros (SteamOS, Fedora
# Silverblue/Kinoite) where /usr is mounted read-only, so fall back to a
# writable path there. An explicit HUSH_BIN_DIR always wins. Must run after
# require_root, since the fallbacks live outside the user's home.
resolve_bin_dir() {
  if [[ -n "${HUSH_BIN_DIR:-}" ]]; then
    BIN_DIR="$HUSH_BIN_DIR"
    if ! mkdir -p "$BIN_DIR" 2>/dev/null || [[ ! -w "$BIN_DIR" ]]; then
      echo "error: HUSH_BIN_DIR='$BIN_DIR' is not writable" >&2
      exit 1
    fi
    return
  fi
  local cand
  for cand in /usr/local/bin /opt/hush/bin /var/lib/hush/bin; do
    if mkdir -p "$cand" 2>/dev/null && [[ -w "$cand" ]]; then
      BIN_DIR="$cand"
      if [[ "$cand" != /usr/local/bin ]]; then
        echo "note: /usr/local/bin is read-only; installing binaries to $cand instead"
        echo "  (set HUSH_BIN_DIR to pick a different location)"
      fi
      return
    fi
  done
  echo "error: found no writable install dir (tried /usr/local/bin, /opt/hush/bin, /var/lib/hush/bin)" >&2
  echo "  set HUSH_BIN_DIR=/some/writable/dir and re-run" >&2
  exit 1
}

create_user() {
  if id "$SERVICE_USER" &>/dev/null; then
    return
  fi
  echo "creating system user '$SERVICE_USER'"
  useradd --system --no-create-home --shell /usr/sbin/nologin \
    --user-group "$SERVICE_USER"
  # Best-effort: GPU vitals (nvidia-smi) need access to /dev/nvidia*, which
  # is normally gated by group membership. Skip quietly if the box has no GPU.
  for g in video render; do
    if getent group "$g" >/dev/null 2>&1; then
      usermod -aG "$g" "$SERVICE_USER"
    fi
  done
}

install_binary() {
  local name="$1" src="$2" resolved=""
  if command -v "$src" >/dev/null 2>&1; then
    resolved="$(command -v "$src")"
  elif [[ -x "$src" ]]; then
    resolved="$src"
  fi
  if [[ -z "$resolved" ]]; then
    echo "error: can't find the $name binary (looked for '$src' on \$PATH and as a file path)" >&2
    echo "  build it first: go build ./cmd/$name" >&2
    exit 1
  fi
  install -o root -g root -m 0755 "$resolved" "$BIN_DIR/$name"
  echo "installed $BIN_DIR/$name"
}

install_unit() {
  local unit="$1"
  # Point ExecStart (and the updater's ReadWritePaths) at the resolved BIN_DIR
  # so a non-default location — e.g. the SteamOS read-only-/usr fallback — is
  # reflected in the unit. A straight copy when BIN_DIR is /usr/local/bin.
  if [[ "$BIN_DIR" != /usr/local/bin ]]; then
    local tmp
    tmp="$(mktemp)"
    sed "s#/usr/local/bin#$BIN_DIR#g" "$REPO_ROOT/systemd/$unit" >"$tmp"
    install -o root -g root -m 0644 "$tmp" "$UNIT_DIR/$unit"
    rm -f "$tmp"
  else
    install -o root -g root -m 0644 "$REPO_ROOT/systemd/$unit" "$UNIT_DIR/$unit"
  fi
  echo "installed $UNIT_DIR/$unit"
}

# install_env_file never clobbers an existing file, so admin edits persist
# across re-runs.
install_env_file() {
  local dest="$CONFIG_DIR/$1"
  shift
  if [[ -f "$dest" ]]; then
    echo "kept existing $dest"
    return
  fi
  printf '%s\n' "$@" >"$dest"
  chown "$SERVICE_USER:$SERVICE_GROUP" "$dest"
  chmod 0640 "$dest"
  echo "wrote $dest"
}

enable_service() {
  systemctl daemon-reload
  systemctl enable --now "$1"
  # enable --now only starts a stopped unit; for one that's already running
  # (a re-run of the installer to pick up a freshly swapped binary) it's a
  # no-op, so the old process keeps running. Force it to pick up the new
  # binary.
  systemctl restart "$1"
  echo "enabled + started $1"
}

# The two hush-control modes are mutually exclusive — their units carry
# Conflicts= each other — but each install path only ever enabled its own
# unit. Switching modes on an existing box therefore left BOTH enabled and
# both WantedBy=multi-user.target, so at boot systemd has to drop one of the
# two conflicting jobs and which mode survives is a race. A box installed as
# tsnet could come back from a reboot serving LAN-only on 127.0.0.1 with no
# tailnet node at all. Turn the mode we are not installing off.
disable_control_mode() {
  if [[ "$(systemctl is-enabled "$1" 2>/dev/null)" == "enabled" ]]; then
    systemctl disable --now "$1"
    echo "disabled $1 (conflicting control mode)"
  fi
}

# install_updater sets up the root-owned self-update path for hush-control:
# a oneshot that swaps the binary for the latest verified release, plus a
# timer that runs it. hush-control itself is unprivileged and can't do this.
install_updater() {
  install_unit hush-control-update.service
  install_unit hush-control-update.timer
  systemctl daemon-reload
  systemctl enable --now hush-control-update.timer
  echo "enabled hush-control-update.timer (auto-update checks)"
}

# install_agent_updater is the hush-agent counterpart of install_updater: a
# root oneshot that swaps the agent binary for the latest verified release,
# plus a timer that runs it. The agent itself runs unprivileged and can't do
# this.
install_agent_updater() {
  install_unit hush-agent-update.service
  install_unit hush-agent-update.timer
  systemctl daemon-reload
  systemctl enable --now hush-agent-update.timer
  echo "enabled hush-agent-update.timer (auto-update checks)"
}

install_agent() {
  install_binary hush-agent "${HUSH_AGENT_BIN:-hush-agent}"
  install_unit hush-agent.service
  # Owned by the service user, not root: this dir is shared with
  # hush-control (see install_control_tsnet), which persists fleet.json here
  # and needs write access to its own state directory.
  install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"
  install_env_file agent.env \
    "# hush-agent environment — edit, then: systemctl restart hush-agent" \
    "# Bind to the tailnet interface in production, not 127.0.0.1." \
    "HUSH_AGENT_LISTEN=127.0.0.1:8765" \
    "" \
    "# Backups are set up on the box over SSH (docs/BACKUP-CONVENTION.md); the" \
    "# agent only reports their status, so there is nothing to enable here."
  enable_service hush-agent.service
  install_agent_updater
}

install_control_tsnet() {
  install_binary hush-control "${HUSH_CONTROL_BIN:-hush-control}"
  # Owned by the service user, not root: hush-control runs as this user and
  # persists fleet.json here (the web console can add/remove machines), so
  # it needs write access to its own state directory.
  install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"
  install_unit hush-control-tsnet.service
  install_env_file control-tsnet.env \
    "# hush-control (tsnet mode) environment" \
    "# TS_AUTHKEY provisions the node on first run; state then persists in" \
    "# HUSH_CONTROL_STATE_DIR, so the key can be removed afterwards." \
    "TS_AUTHKEY=" \
    "HUSH_CONTROL_HOSTNAME=hush" \
    "HUSH_CONTROL_STATE_DIR=/var/lib/hush" \
    "# To restrict callers, add -allow flags with: systemctl edit --full hush-control-tsnet"
  if [[ ! -f "$CONFIG_DIR/fleet.example.json" ]]; then
    install -o root -g "$SERVICE_GROUP" -m 0640 \
      "$REPO_ROOT/fleet.example.json" "$CONFIG_DIR/fleet.example.json"
  fi
  # Turn off any control unit left over from the removed plain-HTTP LAN mode.
  disable_control_mode hush-control.service
  enable_service hush-control-tsnet.service
  install_updater
}

main() {
  require_root
  resolve_bin_dir
  create_user
  case "${1:-}" in
    agent) install_agent ;;
    # Plain-HTTP LAN mode has been removed; "control" is an alias for
    # "control-tsnet" (tailnet mode) so existing usage keeps working.
    control)
      echo "note: LAN mode was removed — installing hush-control in tailnet (tsnet) mode" >&2
      install_control_tsnet
      ;;
    control-tsnet) install_control_tsnet ;;
    all)
      install_agent
      install_control_tsnet
      ;;
    *)
      echo "usage: $0 {agent|control|control-tsnet|all}" >&2
      exit 1
      ;;
  esac
}

main "$@"
