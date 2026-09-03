#!/usr/bin/env bash
set -euo pipefail

repo_root=$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)
cd "$repo_root"

fail() {
	printf 'plugin check failed: %s\n' "$*" >&2
	exit 1
}

version=$(sed -n 's/^const BILOBA_VERSION = "\([^"]*\)"/\1/p' biloba.go)
[[ -n "$version" ]] || fail "could not read BILOBA_VERSION"
[[ -x scripts/sync-plugin-versions.sh ]] || fail "scripts/sync-plugin-versions.sh is missing or not executable"

python3 -m json.tool .claude-plugin/marketplace.json >/dev/null

for plugin in biloba-gomega biloba-vitest biloba; do
	manifest="plugins/$plugin/.claude-plugin/plugin.json"
	[[ -f "$manifest" ]] || fail "missing $manifest"
	python3 -m json.tool "$manifest" >/dev/null

	manifest_name=$(sed -n 's/^[[:space:]]*"name": "\([^"]*\)",/\1/p' "$manifest" | head -n 1)
	[[ "$manifest_name" == "$plugin" ]] || fail "$manifest name is $manifest_name, expected $plugin"

	manifest_version=$(sed -n 's/^[[:space:]]*"version": "\([^"]*\)",/\1/p' "$manifest")
	[[ "$manifest_version" == "$version" ]] || fail "$plugin version $manifest_version does not match Biloba $version"

	grep -Fq '"name": "'"$plugin"'"' .claude-plugin/marketplace.json || fail "$plugin is missing from the marketplace"
	grep -Fq '"source": "./plugins/'"$plugin"'"' .claude-plugin/marketplace.json || fail "$plugin marketplace source is wrong"

done

for plugin in biloba-gomega biloba-vitest; do
	for skill_file in "plugins/$plugin"/skills/*/SKILL.md; do
		[[ -f "$skill_file" ]] || continue
		skill_dir=$(basename "$(dirname "$skill_file")")
		skill_name=$(sed -n 's/^name: //p' "$skill_file" | head -n 1)
		[[ "$skill_name" == "$skill_dir" ]] || fail "$skill_file declares name $skill_name"
	done

	while IFS= read -r reference; do
		skill_name=${reference#*:}
		[[ -d "plugins/$plugin/skills/$skill_name" ]] || fail "$reference points to a missing skill"
	done < <(grep -RhoE "$plugin:[a-z0-9-]+" README.md docs "plugins/$plugin" | sort -u)
done

canonical_count=0
for canonical_skill in plugins/biloba-gomega/skills/*; do
	[[ -d "$canonical_skill" ]] || continue
	skill_name=$(basename "$canonical_skill")
	compatibility_skill="plugins/biloba/skills/$skill_name"
	expected_target="../../biloba-gomega/skills/$skill_name"
	[[ -L "$compatibility_skill" ]] || fail "$compatibility_skill must be a symlink"
	actual_target=$(readlink "$compatibility_skill")
	[[ "$actual_target" == "$expected_target" ]] || fail "$compatibility_skill points to $actual_target"
	[[ -f "$compatibility_skill/SKILL.md" ]] || fail "$compatibility_skill does not resolve to a skill"
	canonical_count=$((canonical_count + 1))
done

compatibility_count=0
for compatibility_skill in plugins/biloba/skills/*; do
	[[ -e "$compatibility_skill" || -L "$compatibility_skill" ]] || continue
	[[ -L "$compatibility_skill" ]] || fail "$compatibility_skill must be a symlink"
	compatibility_count=$((compatibility_count + 1))
done
[[ "$compatibility_count" == "$canonical_count" ]] || fail "compatibility skill count does not match canonical Gomega skills"

marketplace_count=$(grep -c '"source": "./plugins/biloba-' .claude-plugin/marketplace.json)
[[ "$marketplace_count" == 2 ]] || fail "marketplace must contain exactly two Biloba client plugins"

total_plugin_count=$(grep -c '"source": "./plugins/biloba' .claude-plugin/marketplace.json)
[[ "$total_plugin_count" == 3 ]] || fail "marketplace must contain two client plugins and one compatibility alias"

if grep -RniE 'typescript|vitest|biloba-vitest|biloba-gomega:|`biloba:|/biloba:' plugins/biloba-gomega/skills >/dev/null; then
	grep -RniE 'typescript|vitest|biloba-vitest|biloba-gomega:|`biloba:|/biloba:' plugins/biloba-gomega/skills >&2
	fail "canonical Gomega skills contain client-specific routing"
fi

if grep -RniE 'ginkgo|gomega|biloba-gomega:' plugins/biloba-vitest >/dev/null; then
	grep -RniE 'ginkgo|gomega|biloba-gomega:' plugins/biloba-vitest >&2
	fail "Vitest plugin contains Gomega guidance or cross-plugin routing"
fi

if grep -niE 'ginkgo|gomega' docs/vitest.md typescript/README.md >/dev/null; then
	grep -niE 'ginkgo|gomega' docs/vitest.md typescript/README.md >&2
	fail "Vitest documentation contains Gomega-specific guidance"
fi

if grep -RniE 'biloba:typescript|biloba-from-typescript|plugin install biloba@biloba' \
	README.md CLAUDE.md docs typescript plugins .claude-plugin >/dev/null; then
	grep -RniE 'biloba:typescript|biloba-from-typescript|plugin install biloba@biloba' \
		README.md CLAUDE.md docs typescript plugins .claude-plugin >&2
	fail "stale combined-plugin guidance found"
fi

grep -Fq 'Biloba for Vitest' docs/vitest.md || fail "Vitest docs page is missing"
grep -Fq 'biloba-gomega@biloba' README.md || fail "README does not advertise the Gomega plugin"
grep -Fq 'biloba-vitest@biloba' README.md || fail "README does not advertise the Vitest plugin"

printf 'plugin checks passed (Biloba %s)\n' "$version"
