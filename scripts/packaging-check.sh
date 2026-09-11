#!/usr/bin/env bash
# packaging-check.sh: install the packed npm tarballs into an empty project the way a user would,
# then type-check and run one Vitest spec against a real page.  `make packaging-check` runs
# `make npm-pack` and then this; CI runs the two separately so it can pack on one Node and install
# on another.
#
# Usage: scripts/packaging-check.sh [vitest-spec]   (default vitest@latest - what
#        `npm install -D vitest biloba` gives a user)
#
# Steps: copy scripts/packaging-check/ into a temp dir -> `npm install -D` the biloba tarball,
# this machine's platform tarball, and vitest -> tsc -> `npx biloba install-chrome` ->
# `npx vitest run`.
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"
vitest_spec=${1:-vitest@latest}

version=$(node -p 'require("./typescript/package.json").version')
platform=$(node -p '`${process.platform}-${process.arch}`')
# The type-check tooling is pinned to the repo's own versions so a TypeScript release can't turn
# this check red on its own.
typescript=$(node -p '`typescript@${require("./typescript/package.json").devDependencies.typescript}`')
types_node=$(node -p '`@types/node@${require("./typescript/package.json").devDependencies["@types/node"]}`')
tarballs="$repo_root/.release/tarballs"
for tarball in "biloba-$version.tgz" "biloba-$platform-$version.tgz"; do
	[[ -f "$tarballs/$tarball" ]] || { echo "packaging check: $tarballs/$tarball is missing - run make npm-pack first (or this platform is unsupported)" >&2; exit 1; }
done

tmp=${TMPDIR:-/tmp}
project=$(mktemp -d "${tmp%/}/biloba-packaging-check.XXXXXX")
trap 'rm -rf "$project"' EXIT
cp -R scripts/packaging-check/. "$project"
cd "$project"

echo "==> node $(node --version), npm $(npm --version)"
echo "==> npm install -D biloba-$version.tgz biloba-$platform-$version.tgz $vitest_spec $typescript $types_node"
npm install --no-audit --no-fund -D "$tarballs/biloba-$version.tgz" "$tarballs/biloba-$platform-$version.tgz" "$vitest_spec" "$typescript" "$types_node"
npm ls biloba "biloba-$platform" vitest

echo "==> npx tsc"
npx tsc

echo "==> npx biloba install-chrome"
npx biloba install-chrome

echo "==> npx vitest run"
npx vitest run
