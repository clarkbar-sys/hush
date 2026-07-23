# `web/src` — the console shell, split by app

`web/index.html` is one big single-page app: a `<style>` block, a `<body>`, and
a single `<script>` IIFE, ~5.8k lines of it. That's fine to _ship_ but miserable
to _review_ — a one-line payphone tweak lands in the same file as the fleet
console, and "is this payphone change only a payphone change?" takes a scroll to
answer.

So the source lives here, split by app, and `index.html` is **generated** from
it. Each app owns a folder; a change to that app stays in that folder.

## Layout

```
src/
  core/       framework: <head>, theme vars, shared sheet/toast chrome, the
              <script>/IIFE open+close. The scaffolding every app sits inside.
  global/     cross-app popups: the boot / "hush is coming up" overlay and the
              "lost connection to hush-control" banner.
  launcher/   the home screen itself — the springboard, edit-mode reordering,
              and the frame the mini-apps slide into (launcher.close.html closes
              that frame after the app panels).
  apps/
    payphone/ the payphone app: AOL-era buddy list, IM window, Win95 taskbar.
    fleet/    the fleet console — the main app (grid, machine view, sessions,
              backups, disk, sheets, and the go() bootstrap).
    github/   the launcher's github tile → org-repo list (owns the shared
              gh-app slide-in panel shell that plug and riff reuse).
    plug/     the launcher's plug tile → data-sources coming-soon panel.
    riff/     the launcher's riff tile → pager panel with a Try-me page.
    tally/    the launcher's tally tile → hand-off to the tally tailnet node.
  manifest    the ordered list of partials to concatenate.
```

Each app folder holds its own `.css`, `.html`, and/or `.js` — the same document
regions (`<style>`, `<body>`, `<script>`) it contributes to. The launcher tiles
themselves (the grid buttons) live in `launcher/launcher.html`; each tile's
_app_ — its panel and logic — lives in that app's folder.

## How it's assembled

`manifest` lists every partial in document order. `go generate ./web` runs
`gen.go`, which concatenates them **verbatim, byte for byte**, into
`index.html`. There is no minifier or transform — the output is a plain
concatenation, so slicing and re-assembling round-trip losslessly. Because it's
one shared `<script>` IIFE, ordering matters and the manifest fixes it; the CSS
and HTML are split the same way for consistency.

## Editing

1. Edit the partial(s) for the app you're changing under `src/`.
2. Run `go generate ./web`.
3. Commit the partial changes **and** the regenerated `index.html`.

`TestIndexHTMLInSync` (in `web/index_sync_test.go`) fails CI if `index.html`
doesn't match a fresh assembly — i.e. if you forgot step 2. `index.html` is
marked `linguist-generated` (see the repo `.gitattributes`) so GitHub collapses
it in diffs; review the partials, not the concatenated output.
