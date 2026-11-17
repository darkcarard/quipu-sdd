// Package version holds build and project-layout version information.
package version

import "runtime/debug"

// Version is the binary version, overridden at build time with
// -ldflags "-X github.com/darkcarard/quipu-sdd/internal/version.Version=v0.1.0".
// Without it, a binary installed with `go install …@v0.1.0` reports the module
// version Go recorded in the build.
var Version = "dev"

func init() {
	if Version != "dev" {
		return
	}
	if bi, ok := debug.ReadBuildInfo(); ok && bi.Main.Version != "" && bi.Main.Version != "(devel)" {
		Version = bi.Main.Version
	}
}

// LayoutVersion is the version of the on-disk project layout (sdd.yaml, specs/, .sdd/).
// It is written to sdd.yaml as framework_version and bumped only on incompatible
// layout changes, which `sdd upgrade` migrates.
const LayoutVersion = 1
