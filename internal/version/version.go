// Package version holds build and project-layout version information.
package version

// Version is the binary version, overridden at build time with
// -ldflags "-X github.com/darkcarard/sdd-framework/internal/version.Version=v0.1.0".
var Version = "dev"

// LayoutVersion is the version of the on-disk project layout (sdd.yaml, specs/, .sdd/).
// It is written to sdd.yaml as framework_version and bumped only on incompatible
// layout changes, which `sdd upgrade` migrates.
const LayoutVersion = 1
