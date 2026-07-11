#!/usr/bin/env sh
# Installs hush-agent and/or hush-control as systemd services, running under
# a dedicated, unprivileged "hush" system user (never root). Fetches the
# release binaries, the matching systemd unit, and (for control) the fleet
# config template straight from GitHub — no Go toolchain, no git clone
# required on the target box. Must be run as root.
#
# Defaults to installing hush-agent alone — the same one-liner is correct on
# every machine in a fleet (that's most of them). hush-control is a one-off,
# deliberate install on a single box (e.g. the NAS), so it's opt-in:
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/clarkbar-sys/hush/main/install.sh | sudo sh
#   curl -fsSL .../install.sh | sudo sh -s -- agent           # hush-agent (default)
#   curl -fsSL .../install.sh | sudo sh -s -- control         # hush-control, LAN mode
#   curl -fsSL .../install.sh | sudo sh -s -- control-tsnet   # hush-control, tsnet mode
#   curl -fsSL .../install.sh | sudo sh -s -- all             # agent + control, one box
#
# Installs binaries to /usr/local/bin, the unit to /etc/systemd/system, and
# an editable env file to /etc/hush/*.env — never clobbered on re-run, so
# local edits survive upgrades.
#
# Targets systemd + shadow-utils (useradd) distros: Debian, Ubuntu, Fedora,
# Arch, RHEL, openSUSE. Not Alpine (OpenRC) or NixOS (declarative modules),
# and not macOS (no systemd) — build from source and run it yourself there.
#
# Working from a clone with an already-built local binary instead? Use
# scripts/install.sh — same install, but reads local files instead of
# fetching them over the network.

set -eu

REPO="clarkbar-sys/hush"
REF="main"
RAW_BASE="https://raw.githubusercontent.com/$REPO/$REF"
TARGET="${1:-agent}"
SERVICE_USER="${HUSH_USER:-hush}"
SERVICE_GROUP="${HUSH_GROUP:-hush}"
CONFIG_DIR="/etc/hush"
BIN_DIR="/usr/local/bin"
UNIT_DIR="/etc/systemd/system"

require_root() {
  if [ "$(id -u)" != "0" ]; then
    echo "error: must run as root (sudo) — hush installs as a systemd service" >&2
    echo "  curl -fsSL https://raw.githubusercontent.com/$REPO/main/install.sh | sudo sh" >&2
    exit 1
  fi
}

os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin)
      echo "error: this installer sets up a systemd service, which macOS doesn't have." >&2
      echo "  Build from source and run it yourself instead:" >&2
      echo "    go install github.com/$REPO/cmd/hush-agent@latest" >&2
      exit 1
      ;;
    *)
      echo "error: unsupported OS '$(uname -s)' — this installer is systemd-only (Linux)." >&2
      exit 1
      ;;
  esac
}

arch() {
  case "$(uname -m)" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *)
      echo "error: unsupported architecture '$(uname -m)' — build from source instead:" >&2
      echo "  go install github.com/$REPO/cmd/hush-agent@latest" >&2
      exit 1
      ;;
  esac
}

create_user() {
  if id "$SERVICE_USER" >/dev/null 2>&1; then
    return
  fi
  echo "creating system user '$SERVICE_USER'" >&2
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

fetch_binary() {
  name="$1"
  url="https://github.com/$REPO/releases/latest/download/${name}_${OS_NAME}_${ARCH_NAME}.tar.gz"
  echo "downloading $name ($OS_NAME/$ARCH_NAME)..." >&2
  if ! curl -fsSL "$url" -o "$TMP_DIR/$name.tar.gz"; then
    echo "error: no release binary found at $url" >&2
    echo "  (no tagged release yet, or unsupported platform — build from source instead:" >&2
    echo "   go install github.com/$REPO/cmd/$name@latest)" >&2
    exit 1
  fi
  tar -xzf "$TMP_DIR/$name.tar.gz" -C "$TMP_DIR" "$name"
  mkdir -p "$BIN_DIR"
  install -o root -g root -m 0755 "$TMP_DIR/$name" "$BIN_DIR/$name"
  echo "installed $BIN_DIR/$name" >&2
}

fetch_unit() {
  unit="$1"
  url="$RAW_BASE/systemd/$unit"
  echo "downloading $unit..." >&2
  if ! curl -fsSL "$url" -o "$TMP_DIR/$unit"; then
    echo "error: couldn't fetch $url" >&2
    exit 1
  fi
  install -o root -g root -m 0644 "$TMP_DIR/$unit" "$UNIT_DIR/$unit"
  echo "installed $UNIT_DIR/$unit" >&2
}

# install_env_file never clobbers an existing file, so admin edits persist
# across re-runs.
install_env_file() {
  dest="$CONFIG_DIR/$1"
  shift
  if [ -f "$dest" ]; then
    echo "kept existing $dest" >&2
    return
  fi
  : >"$dest"
  for line in "$@"; do
    printf '%s\n' "$line" >>"$dest"
  done
  chown "$SERVICE_USER:$SERVICE_GROUP" "$dest"
  chmod 0640 "$dest"
  echo "wrote $dest" >&2
}

fetch_fleet_example() {
  dest="$CONFIG_DIR/fleet.example.json"
  if [ -f "$dest" ]; then
    return
  fi
  if curl -fsSL "$RAW_BASE/fleet.example.json" -o "$TMP_DIR/fleet.example.json"; then
    install -o root -g "$SERVICE_GROUP" -m 0640 "$TMP_DIR/fleet.example.json" "$dest"
    echo "wrote $dest" >&2
  fi
}

enable_service() {
  systemctl daemon-reload
  systemctl enable --now "$1"
  echo "enabled + started $1" >&2
}

install_agent() {
  fetch_binary hush-agent
  fetch_unit hush-agent.service
  install_env_file agent.env \
    "# hush-agent environment — edit, then: systemctl restart hush-agent" \
    "# Bind to the tailnet interface in production, not 127.0.0.1." \
    "HUSH_AGENT_LISTEN=127.0.0.1:8765"
  enable_service hush-agent.service
}

install_control() {
  fetch_binary hush-control
  fetch_unit hush-control.service
  install_env_file control.env \
    "# hush-control environment — edit, then: systemctl restart hush-control" \
    "HUSH_CONTROL_LISTEN=127.0.0.1:8080" \
    "HUSH_CONTROL_CONFIG=$CONFIG_DIR/fleet.json"
  fetch_fleet_example
  enable_service hush-control.service
}

install_control_tsnet() {
  fetch_binary hush-control
  fetch_unit hush-control-tsnet.service
  install_env_file control-tsnet.env \
    "# hush-control (tsnet mode) environment" \
    "# TS_AUTHKEY provisions the node on first run; state then persists in" \
    "# HUSH_CONTROL_STATE_DIR, so the key can be removed afterwards." \
    "TS_AUTHKEY=" \
    "HUSH_CONTROL_HOSTNAME=hush" \
    "HUSH_CONTROL_STATE_DIR=/var/lib/hush" \
    "# To restrict callers, add -allow flags with: systemctl edit --full hush-control-tsnet"
  enable_service hush-control-tsnet.service
}

require_root
OS_NAME="$(os)"
ARCH_NAME="$(arch)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

create_user
install -d -o root -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"

case "$TARGET" in
  agent) install_agent ;;
  control) install_control ;;
  control-tsnet) install_control_tsnet ;;
  all)
    install_agent
    install_control
    ;;
  *)
    echo "usage: install.sh {agent|control|control-tsnet|all}" >&2
    exit 1
    ;;
esac
