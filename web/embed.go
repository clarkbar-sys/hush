// Package web bundles the console's static UI into the binary so a
// `go install`ed or `scp`ed hush-control is self-contained — no loose files to
// ship alongside it. hush-control serves these assets by default; pass -web to
// serve a live directory instead (handy while iterating on the UI).
package web

import "embed"

// FS holds the static console assets: the single-page UI shell plus the PWA
// support files (manifest, service worker, and icons) that make the console
// installable as an Android home-screen app. Data streams in over the API at
// runtime; these are just the shell.
//
//go:embed index.html manifest.webmanifest sw.js icon-192.png icon-512.png icon-192-maskable.png icon-512-maskable.png apple-touch-icon.png
var FS embed.FS
