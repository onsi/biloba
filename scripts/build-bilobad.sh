#!/usr/bin/env bash
# build-bilobad.sh: cross-compile the bilobad daemon for every platform Biloba ships an npm
# package for (darwin/linux x amd64/arm64 - Windows is out of scope for now).
#
# Usage: scripts/build-bilobad.sh <version> <outdir>
#   <version>  Biloba version to stamp into the binary via -ldflags -X main.version=<version>.
#              No leading "v" - matches BILOBA_VERSION, e.g. 0.15.4.
#   <outdir>   Directory to write into. Each target lands at <outdir>/<os>-<arch>/bilobad, using
#              Go's own GOOS/GOARCH spelling (darwin-arm64, darwin-amd64, linux-arm64,
#              linux-amd64), so callers can glob it directly without a name-mapping step.
#
# Interface is deliberately simple and stable: `make npm-pack` and the release workflow's binary
# packaging step both call this script the same way.
#
# CGO_ENABLED=0 keeps every target a static, cross-compilable binary - no C toolchain needed on
# the build host, since bilobad's dependencies are pure Go. -trimpath -s -w strip local build
# paths and debug symbols to shrink the binary.
set -euo pipefail

if [[ $# -ne 2 ]]; then
	echo "usage: $0 <version> <outdir>" >&2
	exit 2
fi

version=$1
outdir=$2
repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)

targets=(
	"darwin amd64"
	"darwin arm64"
	"linux amd64"
	"linux arm64"
)

for target in "${targets[@]}"; do
	read -r os arch <<<"$target"
	dest="$outdir/$os-$arch"
	mkdir -p "$dest"
	echo "building bilobad $version for $os/$arch -> $dest/bilobad"
	CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" go -C "$repo_root" build -trimpath -ldflags "-s -w -X main.version=$version" -o "$dest/bilobad" ./cmd/bilobad
done

echo "built bilobad $version for ${#targets[@]} targets in $outdir"
