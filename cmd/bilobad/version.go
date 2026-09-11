package main

import (
	"fmt"
	"io"
	"runtime/debug"
	"strings"
)

// version is stamped by the release build via `-ldflags "-X main.version=X.Y.Z"`.  Left empty for
// a plain `go build`/`go run` from a checkout, in which case resolveVersion falls back to Go's own
// build info.  Deliberately not read from the root github.com/onsi/biloba package's BILOBA_VERSION
// constant: importing that package would pull Ginkgo and Gomega into a binary that has no business
// linking either.
var version string

// resolveVersion determines bilobad's own version, in order: the ldflags-stamped version; the
// module version `go install github.com/onsi/biloba/cmd/bilobad@vX.Y.Z` resolves at install time
// (runtime/debug.ReadBuildInfo), with its leading "v" stripped; "dev" otherwise - which covers a
// local `go build`/`go run` ("(devel)") and a binary built with no embedded module info at all.
func resolveVersion() string {
	if version != "" {
		return version
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		if moduleVersion := info.Main.Version; moduleVersion != "" && moduleVersion != "(devel)" {
			return strings.TrimPrefix(moduleVersion, "v")
		}
	}
	return "dev"
}

// runVersion implements the `version` subcommand: print bilobad's own version and exit 0.
func runVersion(stdout io.Writer) error {
	_, err := fmt.Fprintln(stdout, resolveVersion())
	return err
}
