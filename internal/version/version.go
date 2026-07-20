// Package version exposes the build version shared by hush-agent and
// hush-control. The release workflow stamps [Version] via -ldflags at build
// time; a source checkout falls back to a "dev-<hash>" string built from the
// git commit the Go toolchain embeds automatically, then to the main
// module's version from the build info (e.g. `go install ...@vX.Y.Z`), and
// finally to "dev" when nothing is available.
package version

import "runtime/debug"

// Version is the release tag this binary was built from, e.g. "v1.2.0". It is
// overwritten at release time with:
//
//	-ldflags "-X github.com/clarkbar-sys/hush/internal/version.Version=v1.2.0"
//
// Left as "dev" for local builds; Current() upgrades that to a "dev-<hash>"
// string for a plain `go build`/`go install` inside a git checkout, or to the
// module version for `go install ...@vX.Y.Z`.
var Version = "dev"

// Current returns the best available version string for this binary. It
// prefers the ldflags-stamped Version, then a "dev-<hash>" derived from the
// git commit the Go toolchain stamps into builds made inside a git checkout
// (so it wins over the uninformative "(devel)" — or ugly pseudo-version —
// module version `go build` would otherwise report), then the main module's
// version from the embedded build info, and finally "dev".
func Current() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	if v := devVersion(info); v != "" {
		return v
	}
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return "dev"
}

// devVersion builds a "dev-<shorthash>" string (with a "-dirty" suffix for an
// uncommitted working tree) from the VCS info the Go toolchain embeds
// automatically when building inside a git checkout. It returns "" when no
// revision was embedded, e.g. a build from a module-cache checkout that isn't
// a git working tree.
func devVersion(info *debug.BuildInfo) string {
	var revision string
	var modified bool
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value == "true"
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	v := "dev-" + revision
	if modified {
		v += "-dirty"
	}
	return v
}
