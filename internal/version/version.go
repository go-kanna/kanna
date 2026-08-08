// Package version reports the version of the running binary.
package version

import "runtime/debug"

// String reports what the go command stamped into this binary: the module
// version when it was installed, a pseudo-version derived from the commit when
// it was built from a checkout, or "(devel)" under `go run`, which records no
// version of its own. The fallback covers a binary carrying no build info at
// all.
func String() string {
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		return info.Main.Version
	}
	return "unknown"
}
