#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

fail() {
	printf 'release version sync failed: %s\n' "$*" >&2
	exit 1
}

version=$(sed -n 's/^const BILOBA_VERSION = "\([^"]*\)"/\1/p' biloba.go)
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([+-][0-9A-Za-z.-]+)?$ ]] || fail "invalid BILOBA_VERSION: ${version:-missing}"

manifests=()
for manifest in plugins/biloba*/.claude-plugin/plugin.json; do
	[[ -f "$manifest" ]] && manifests+=("$manifest")
done
[[ ${#manifests[@]} -gt 0 ]] || fail "no Biloba plugin manifests found"

for manifest in "${manifests[@]}"; do
	version_fields=$(grep -c '^[[:space:]]*"version": "[^"]*",$' "$manifest" || true)
	[[ "$version_fields" == 1 ]] || fail "$manifest must contain exactly one version field"
	PLUGIN_VERSION="$version" perl -pi -e 's/("version": ")[^"]*(",)/$1$ENV{PLUGIN_VERSION}$2/' "$manifest"
done

PACKAGE_VERSION="$version" node -e '
const fs = require("node:fs");
const path = "typescript/package.json";
const manifest = JSON.parse(fs.readFileSync(path, "utf8"));
manifest.version = process.env.PACKAGE_VERSION;
fs.writeFileSync(path, `${JSON.stringify(manifest, null, 2)}\n`);
'

printf 'synced %s plugin manifests and the npm package to Biloba %s\n' "${#manifests[@]}" "$version"
