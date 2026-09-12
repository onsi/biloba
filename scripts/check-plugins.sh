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

for plugin in biloba-go biloba-vitest; do
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

for plugin in biloba-go biloba-vitest; do
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

	# The Gomega skills name their siblings without a plugin prefix, so the prefixed check above
	# validates nothing inside them.  Validate the unprefixed routing form here instead: every
	# "-> `name`" must name a skill of this same plugin.  The arrow is reserved for routing - do
	# not use it for a value.
	while IFS= read -r skill_name; do
		[[ -d "plugins/$plugin/skills/$skill_name" ]] ||
			fail "$plugin: routing reference to \`$skill_name\` does not name a skill in this plugin"
	done < <(grep -RhoE '→ `[a-z0-9-]+`' "plugins/$plugin"/skills 2>/dev/null |
		sed -e 's/^.* `//' -e 's/`$//' | sort -u)

	# An unprefixed name is only resolvable if the reader knows to reuse the prefix they loaded
	# the skill under, so any skill that uses the unprefixed routing form has to say so.  A skill
	# that references its siblings by full plugin:skill name needs no note.
	for skill_file in "plugins/$plugin"/skills/*/SKILL.md; do
		[[ -f "$skill_file" ]] || continue
		grep -qE '→ `[a-z0-9-]+`' "$skill_file" || continue
		grep -Fq 'invoke one with the same plugin prefix you loaded this skill under' "$skill_file" ||
			fail "$skill_file uses unprefixed skill references but is missing the invocation note"
	done
done

# A skill name unique to one client plugin must never be referenced from the other - it would
# route a reader to a skill they do not have installed.
for plugin in biloba-go biloba-vitest; do
	if [[ "$plugin" == biloba-go ]]; then
		other=biloba-vitest
	else
		other=biloba-go
	fi
	for other_skill in "plugins/$other"/skills/*/; do
		[[ -d "$other_skill" ]] || continue
		skill_name=$(basename "$other_skill")
		[[ -d "plugins/$plugin/skills/$skill_name" ]] && continue
		if grep -RFq "\`$skill_name\`" "plugins/$plugin"/skills; then
			fail "$plugin references \`$skill_name\`, which only exists in $other"
		fi
	done
done

[[ ! -e plugins/biloba ]] || fail "plugins/biloba is the removed compatibility alias; skills live in biloba-go and biloba-vitest"

marketplace_count=$(grep -c '"source": "./plugins/' .claude-plugin/marketplace.json)
[[ "$marketplace_count" == 2 ]] || fail "marketplace must contain exactly the two Biloba client plugins"

if grep -RniE 'typescript|vitest|biloba-vitest|biloba-go:|`biloba:|/biloba:' plugins/biloba-go/skills >/dev/null; then
	grep -RniE 'typescript|vitest|biloba-vitest|biloba-go:|`biloba:|/biloba:' plugins/biloba-go/skills >&2
	fail "Gomega skills contain client-specific routing"
fi

if grep -RniE 'ginkgo|gomega|biloba-go:' plugins/biloba-vitest >/dev/null; then
	grep -RniE 'ginkgo|gomega|biloba-go:' plugins/biloba-vitest >&2
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
grep -Fq 'biloba-go@biloba' README.md || fail "README does not advertise the Gomega plugin"
grep -Fq 'biloba-vitest@biloba' README.md || fail "README does not advertise the Vitest plugin"
grep -Fq 'biloba-vitest@biloba' typescript/README.md || fail "TypeScript README does not advertise the Vitest plugin"

printf 'plugin checks passed (Biloba %s)\n' "$version"
