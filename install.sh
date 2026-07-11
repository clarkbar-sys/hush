#!/usr/bin/env sh
# Downloads the prebuilt hush-agent / hush-control binaries from the latest
# GitHub Release and installs them onto $PATH. No Go toolchain required.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/clarkbar-sys/hush/main/install.sh | sh
#   curl -fsSL .../install.sh | sh -s -- agent      # just hush-agent
#   curl -fsSL .../install.sh | sh -s -- control    # just hush-control
#
# Installs to /usr/local/bin when run as root (or when it's writable),
# otherwise falls back to ~/.local/bin. Override with HUSH_INSTALL_DIR.
#
# Building from source instead? See the README's "Install" section
# (`go install github.com/clarkbar-sys/hush/cmd/hush-agent@latest`).

set -eu

REPO="clarkbar-sys/hush"
TARGET="${1:-all}"

os() {
  case "$(uname -s)" in
    Linux) echo linux ;;
    Darwin) echo darwin ;;
    *)
      echo "error: unsupported OS '$(uname -s)' — build from source instead:" >&2
      echo "  go install github.com/$REPO/cmd/hush-agent@latest" >&2
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

install_dir() {
  if [ -n "${HUSH_INSTALL_DIR:-}" ]; then
    printf '%s\n' "$HUSH_INSTALL_DIR"
  elif [ "$(id -u)" = "0" ] || [ -w /usr/local/bin ]; then
    printf '%s\n' /usr/local/bin
  else
    printf '%s\n' "$HOME/.local/bin"
  fi
}

OS_NAME="$(os)"
ARCH_NAME="$(arch)"
DEST="$(install_dir)"
TMP_DIR="$(mktemp -d)"
trap 'rm -rf "$TMP_DIR"' EXIT

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
  mkdir -p "$DEST"
  install -m 0755 "$TMP_DIR/$name" "$DEST/$name"
  echo "installed $DEST/$name" >&2
}

case "$TARGET" in
  agent) fetch_binary hush-agent ;;
  control) fetch_binary hush-control ;;
  all)
    fetch_binary hush-agent
    fetch_binary hush-control
    ;;
  *)
    echo "usage: install.sh [agent|control|all]" >&2
    exit 1
    ;;
esac

case ":$PATH:" in
  *":$DEST:"*) ;;
  *) echo "note: $DEST is not on your PATH — add it to your shell profile" >&2 ;;
esac
