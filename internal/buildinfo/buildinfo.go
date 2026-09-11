// Package buildinfo carries the version string stamped into release
// binaries. Release builds set Version at link time:
//
//	go build -ldflags "-X github.com/jstevewhite/snp/internal/buildinfo.Version=v0.1.0"
//
// The Makefile does this from `git describe`; the GitHub release
// workflow does it from the pushed tag. A plain `go build` leaves it
// empty, which String reports as "dev".
package buildinfo

// Version is the release tag (e.g. "v0.1.0"), or empty for a local
// build. Set via -ldflags -X; do not assign it in code.
var Version string

// String returns Version, or "dev" when no version was linked in.
func String() string {
	if Version == "" {
		return "dev"
	}
	return Version
}
