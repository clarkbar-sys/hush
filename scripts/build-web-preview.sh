#!/usr/bin/env sh
# Stage web/ into a self-contained static bundle for per-PR GitHub Pages
# previews. The console already renders a full demo fleet with no backend —
# index.html's `fetch("api/fleet")` fails, which drops it into MODE=demo — so a
# preview is just the static shell, no hush-control required.
#
# The only fixups are paths: hush-control serves the console at "/", so the
# shell references its manifest, icons, and service worker as root-absolute
# ("/icon-512.png"). A PR preview is hosted under a subpath
# (/<repo>/pr-preview/pr-<N>/), where those 404. We rewrite them to relative in
# the staged copy and disable the service worker (whose precache list is
# likewise root-absolute) — the real builds are untouched.
set -eu

src="web"
out="${1:-_preview}"

rm -rf "$out"
mkdir -p "$out"
cp "$src/index.html" "$src/manifest.webmanifest" "$src"/*.png "$out/"

# index.html: root-absolute asset links -> relative for subpath hosting.
sed -i \
  -e 's#href="/manifest.webmanifest"#href="manifest.webmanifest"#' \
  -e 's#href="/icon-512.png"#href="icon-512.png"#' \
  -e 's#href="/apple-touch-icon.png"#href="apple-touch-icon.png"#' \
  "$out/index.html"

# Disable the service worker in the preview: its precache list is root-absolute
# and would 404 noisily under the preview subpath. Live builds keep it.
sed -i \
  "s#navigator.serviceWorker.register('/sw.js')#Promise.reject(new Error('sw disabled in preview'))#" \
  "$out/index.html"

# manifest: root-absolute -> relative so theme colour and icons still resolve.
sed -i \
  -e 's#"start_url": "/"#"start_url": "./"#' \
  -e 's#"scope": "/"#"scope": "./"#' \
  -e 's#"src": "/icon-#"src": "icon-#g' \
  "$out/manifest.webmanifest"

echo "staged web preview -> $out/"
