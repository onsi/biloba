#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

usage() {
	printf 'usage: %s --dry-run|--publish [vX.Y.Z]\n' "$0" >&2
	exit 2
}

[[ $# -ge 1 && $# -le 2 ]] || usage
mode=$1
tag=${2:-}
[[ "$mode" == "--dry-run" || "$mode" == "--publish" ]] || usage

version=$(node -p 'require("./typescript/package.json").version')
[[ -z "$tag" ]] && tag="v$version"

make npm-pack TAG="$tag"

if [[ "$mode" == "--dry-run" ]]; then
	npm publish .release/npm/unscoped --access public --dry-run
	npm publish .release/npm/scoped --access public --dry-run
	printf 'npm publish dry run passed for @biloba/biloba and biloba %s\n' "$version"
	exit 0
fi

publish_if_missing() {
	local package_name=$1
	local package_directory=$2

	if npm view "$package_name@$version" version >/dev/null 2>&1; then
		printf '%s@%s is already published; skipping\n' "$package_name" "$version"
		return
	fi

	npm publish "$package_directory" --access public
}

# There is no registry transaction spanning two names. Publishing the unscoped package first means
# the generally available name lands even if the scoped organization needs separate access. A retry
# skips either immutable version that already succeeded and continues with the missing one.
publish_if_missing biloba .release/npm/unscoped
publish_if_missing @biloba/biloba .release/npm/scoped
