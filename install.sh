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
# On immutable-root distros (SteamOS, Fedora Silverblue/Kinoite) /usr is
# read-only, so /usr/local/bin can't be written. There the installer falls
# back to a writable directory automatically and rewrites the unit's
# ExecStart to match. Force a specific location with HUSH_BIN_DIR=/some/dir.
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

# resolve_bin_dir picks where the binaries land and sets BIN_DIR. The usual
# /usr/local/bin is unwritable on immutable-root distros (SteamOS, Fedora
# Silverblue/Kinoite) where /usr is mounted read-only, so fall back to a
# writable path there. An explicit HUSH_BIN_DIR always wins. Must run after
# require_root, since the fallbacks live outside the user's home.
resolve_bin_dir() {
  if [ -n "${HUSH_BIN_DIR:-}" ]; then
    BIN_DIR="$HUSH_BIN_DIR"
    if ! mkdir -p "$BIN_DIR" 2>/dev/null || [ ! -w "$BIN_DIR" ]; then
      echo "error: HUSH_BIN_DIR='$BIN_DIR' is not writable" >&2
      exit 1
    fi
    return
  fi
  for cand in /usr/local/bin /opt/hush/bin /var/lib/hush/bin; do
    if mkdir -p "$cand" 2>/dev/null && [ -w "$cand" ]; then
      BIN_DIR="$cand"
      if [ "$cand" != /usr/local/bin ]; then
        echo "note: /usr/local/bin is read-only; installing binaries to $cand instead" >&2
        echo "  (set HUSH_BIN_DIR to pick a different location)" >&2
      fi
      return
    fi
  done
  echo "error: found no writable install dir (tried /usr/local/bin, /opt/hush/bin, /var/lib/hush/bin)" >&2
  echo "  set HUSH_BIN_DIR=/some/writable/dir and re-run" >&2
  exit 1
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

# curl_network_error inspects a failed curl's exit code and, if it looks like
# a connectivity problem (DNS, connection refused, timeout, TLS) rather than a
# real HTTP response, prints a network-specific diagnosis and returns 0. It
# returns 1 for everything else (notably exit 22, curl's -f code for HTTP >=
# 400 — a genuine 404). Callers use it so a transient DNS/network blip is not
# reported as "no tagged release / unsupported platform": the two are
# indistinguishable without the exit code, and conflating them sends people
# down the build-from-source path when the release is fine and only their
# resolver hiccuped.
curl_network_error() {
  rc="$1"
  url="$2"
  # Pull the host out of the URL so the diagnosis names the box that actually
  # failed to resolve (github.com vs raw.githubusercontent.com).
  host="$(printf '%s\n' "$url" | sed -e 's#^[a-z]*://##' -e 's#/.*##')"
  case "$rc" in
    6)
      echo "error: couldn't resolve $host — DNS/name resolution failed on this machine." >&2
      echo "  This is a local network problem, not a missing release. The download URL is valid:" >&2
      echo "    $url" >&2
      echo "  Check DNS (e.g. 'getent hosts $host', your resolv.conf / upstream resolver)," >&2
      echo "  then re-run the installer — no need to build from source." >&2
      return 0
      ;;
    7 | 28 | 35 | 56)
      echo "error: couldn't reach $host (curl exit $rc: connection/timeout/TLS) — a network" >&2
      echo "  problem on this machine, not a missing release. URL: $url" >&2
      echo "  Check this box's connectivity/proxy/firewall to $host, then re-run." >&2
      return 0
      ;;
  esac
  return 1
}

fetch_binary() {
  name="$1"
  url="https://github.com/$REPO/releases/latest/download/${name}_${OS_NAME}_${ARCH_NAME}.tar.gz"
  echo "downloading $name ($OS_NAME/$ARCH_NAME)..." >&2
  rc=0
  curl -fsSL "$url" -o "$TMP_DIR/$name.tar.gz" || rc=$?
  if [ "$rc" -ne 0 ]; then
    curl_network_error "$rc" "$url" && exit 1
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
  rc=0
  curl -fsSL "$url" -o "$TMP_DIR/$unit" || rc=$?
  if [ "$rc" -ne 0 ]; then
    curl_network_error "$rc" "$url" && exit 1
    echo "error: couldn't fetch $url (curl exit $rc)" >&2
    exit 1
  fi
  # Point ExecStart (and the updater's ReadWritePaths) at the resolved BIN_DIR
  # so a non-default location — e.g. the SteamOS read-only-/usr fallback — is
  # reflected in the unit. A no-op when BIN_DIR is /usr/local/bin.
  if [ "$BIN_DIR" != /usr/local/bin ]; then
    sed "s#/usr/local/bin#$BIN_DIR#g" "$TMP_DIR/$unit" >"$TMP_DIR/$unit.patched"
    mv "$TMP_DIR/$unit.patched" "$TMP_DIR/$unit"
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
  # enable --now only starts a stopped unit; for one that's already running
  # (a re-run of the installer to pick up a freshly swapped binary) it's a
  # no-op, so the old process keeps running. Force it to pick up the new
  # binary.
  systemctl restart "$1"
  echo "enabled + started $1" >&2
}

# The two hush-control modes are mutually exclusive — their units carry
# Conflicts= each other — but each install path only ever enabled its own
# unit. Switching modes on an existing box therefore left BOTH enabled and
# both WantedBy=multi-user.target, so at boot systemd has to drop one of the
# two conflicting jobs and which mode survives is a race. A box installed as
# tsnet could come back from a reboot serving LAN-only on 127.0.0.1 with no
# tailnet node at all. Turn the mode we are not installing off.
disable_control_mode() {
  if [ "$(systemctl is-enabled "$1" 2>/dev/null)" = "enabled" ]; then
    systemctl disable --now "$1"
    echo "disabled $1 (conflicting control mode)" >&2
  fi
}

# install_updater sets up the root-owned self-update path for hush-control:
# a oneshot that swaps the binary for the latest verified release, plus a
# timer that runs it. hush-control itself is unprivileged and can't do this.
install_updater() {
  fetch_unit hush-control-update.service
  fetch_unit hush-control-update.timer
  systemctl daemon-reload
  systemctl enable --now hush-control-update.timer
  echo "enabled hush-control-update.timer (auto-update checks)" >&2
}

# install_agent_updater is the hush-agent counterpart of install_updater: a
# root oneshot that swaps the agent binary for the latest verified release,
# plus a timer that runs it. The agent itself runs unprivileged and can't do
# this.
install_agent_updater() {
  fetch_unit hush-agent-update.service
  fetch_unit hush-agent-update.timer
  systemctl daemon-reload
  systemctl enable --now hush-agent-update.timer
  echo "enabled hush-agent-update.timer (auto-update checks)" >&2
}

install_agent() {
  fetch_binary hush-agent
  fetch_unit hush-agent.service
  install_env_file agent.env \
    "# hush-agent environment — edit, then: systemctl restart hush-agent" \
    "# 'tailnet' binds this machine's Tailscale IP so hush-control discovers it" \
    "# over the tailnet with no LAN or public exposure. Use 127.0.0.1:8765 for a" \
    "# local-only agent, or host:port to pin a specific interface." \
    "HUSH_AGENT_LISTEN=tailnet" \
    "" \
    "# Uncomment to enable the Job scheduler: saved commands that fire on a cron" \
    "# schedule as the unprivileged hush user, persisted to /var/lib/hush/jobs.json." \
    "# Off by default because Jobs run unattended. HUSH_AGENT_EXEC=0 likewise" \
    "# disables the one-shot /exec runner (on by default)." \
    "# HUSH_AGENT_JOBS=1"
  enable_service hush-agent.service
  install_agent_updater
}

install_control() {
  fetch_binary hush-control
  fetch_unit hush-control.service
  install_env_file control.env \
    "# hush-control environment — edit, then: systemctl restart hush-control" \
    "HUSH_CONTROL_LISTEN=127.0.0.1:8080" \
    "HUSH_CONTROL_CONFIG=$CONFIG_DIR/fleet.json"
  fetch_fleet_example
  disable_control_mode hush-control-tsnet.service
  enable_service hush-control.service
  install_updater
}

install_control_tsnet() {
  fetch_binary hush-control
  fetch_unit hush-control-tsnet.service
  install_env_file control-tsnet.env \
    "# hush-control (tsnet mode) environment" \
    "# TS_AUTHKEY provisions the node on first run; state then persists in" \
    "# HUSH_CONTROL_STATE_DIR, so the key can be removed afterwards." \
    "# Leave TS_AUTHKEY empty to provision from a browser instead: hush-control" \
    "# serves a one-time setup page on the LAN (:8080) until the node joins." \
    "TS_AUTHKEY=" \
    "HUSH_CONTROL_HOSTNAME=hush" \
    "HUSH_CONTROL_STATE_DIR=/var/lib/hush" \
    "# To restrict callers, add -allow flags with: systemctl edit --full hush-control-tsnet"
  disable_control_mode hush-control.service
  enable_service hush-control-tsnet.service
  install_updater
}

require_root
resolve_bin_dir
OS_NAME="$(os)"
ARCH_NAME="$(arch)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

create_user
# Owned by the service user, not root: hush-control runs as this user and
# persists fleet.json here (the web console can add/remove machines), so it
# needs write access to its own state directory.
install -d -o "$SERVICE_USER" -g "$SERVICE_GROUP" -m 0750 "$CONFIG_DIR"

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
