// Package web bundles the console's static UI into the binary so a
// `go install`ed or `scp`ed hush-control is self-contained — no loose files to
// ship alongside it. hush-control serves these assets by default; pass -web to
// serve a live directory instead (handy while iterating on the UI).
package web

import "embed"

// FS holds the static console assets (the single-page UI shell). Data streams
// in over the API at runtime; this is just the shell.
//
//go:embed index.html
var FS embed.FS
