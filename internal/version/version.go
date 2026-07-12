// Package version exposes the build version shared by hush-agent and
// hush-control. The release workflow stamps [Version] via -ldflags at build
// time; go-installed or `go build`ed binaries fall back to the module version
// recorded in the build info, and to "dev" when nothing is available.
package version

import "runtime/debug"

// Version is the release tag this binary was built from, e.g. "v1.2.0". It is
// overwritten at release time with:
//
//	-ldflags "-X github.com/clarkbar-sys/hush/internal/version.Version=v1.2.0"
//
// Left as "dev" for local builds; Current() upgrades that to the module
// version when the binary was produced by `go install ...@vX.Y.Z`.
var Version = "dev"

// Current returns the best available version string for this binary. It
// prefers the ldflags-stamped Version, then the main module's version from the
// embedded build info, and finally "dev".
func Current() string {
	if Version != "dev" && Version != "" {
		return Version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if v := info.Main.Version; v != "" && v != "(devel)" {
			return v
		}
	}
	return "dev"
}
