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
#   sudo ./scripts/install.sh control         # hush-control only, LAN mode
#   sudo ./scripts/install.sh control-tsnet   # hush-control only, tsnet mode
#   sudo ./scripts/install.sh all             # agent + control (LAN mode)
#
# Binaries must already be built (see README's "Install" section: either
# `go install ./cmd/...` with $GOBIN on $PATH, or `go build ./cmd/...`).
# Override the source binary with HUSH_AGENT_BIN / HUSH_CONTROL_BIN if it
# isn't on $PATH.
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
BIN_DIR="/usr/local/bin"
UNIT_DIR="/etc/systemd/system"
REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

require_root() {
  if [[ "$(id -u)" -ne 0 ]]; then
    echo "error: must run as root (sudo)" >&2
    exit 1
  fi
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
  install -o root -g root -m 0644 "$REPO_ROOT/systemd/$unit" "$UNIT_DIR/$unit"
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
  echo "enabled + started $1"
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

install_agent() {
  install_binary hush-agent "${HUSH_AGENT_BIN:-hush-agent}"
  install_unit hush-agent.service
  # Owned by the service user, not root: this dir is shared with
  # hush-control (see install_control), which persists fleet.json here and
  # needs write access to its own state directory.
  install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"
  install_env_file agent.env \
    "# hush-agent environment — edit, then: systemctl restart hush-agent" \
    "# Bind to the tailnet interface in production, not 127.0.0.1." \
    "HUSH_AGENT_LISTEN=127.0.0.1:8765"
  enable_service hush-agent.service
}

install_control() {
  install_binary hush-control "${HUSH_CONTROL_BIN:-hush-control}"
  # Owned by the service user, not root: hush-control runs as this user and
  # persists fleet.json here (the web console can add/remove machines), so
  # it needs write access to its own state directory.
  install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"
  install_unit hush-control.service
  install_env_file control.env \
    "# hush-control environment — edit, then: systemctl restart hush-control" \
    "HUSH_CONTROL_LISTEN=127.0.0.1:8080" \
    "HUSH_CONTROL_CONFIG=$CONFIG_DIR/fleet.json"
  if [[ ! -f "$CONFIG_DIR/fleet.example.json" ]]; then
    install -o root -g "$SERVICE_GROUP" -m 0640 \
      "$REPO_ROOT/fleet.example.json" "$CONFIG_DIR/fleet.example.json"
  fi
  enable_service hush-control.service
  install_updater
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
  enable_service hush-control-tsnet.service
  install_updater
}

main() {
  require_root
  create_user
  case "${1:-}" in
    agent) install_agent ;;
    control) install_control ;;
    control-tsnet) install_control_tsnet ;;
    all)
      install_agent
      install_control
      ;;
    *)
      echo "usage: $0 {agent|control|control-tsnet|all}" >&2
      exit 1
      ;;
  esac
}

main "$@"
