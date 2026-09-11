#!/usr/bin/env bash
# release.sh: cut a Biloba release.  The Release workflow (.github/workflows/release.yml) runs this
# after the test workflow passes; see RELEASING.md.
#
# Usage: scripts/release.sh patch|minor
#
# One run: release ## Unreleased as vX.Y.Z, commit and tag it, push both, attach bilobad archives
# to a GitHub release, and publish the five npm packages.  Re-running after a failure resumes: once
# the tag is on origin, the run builds from the tag and skips whatever is already released.
#
# BILOBA_RELEASE_DRY_RUN=1 does everything locally, then stops short of pushing, creating the
# GitHub release, and publishing (npm publish --dry-run instead).  A development aid only.
set -euo pipefail

bump=${1:-}
[[ "$bump" == patch || "$bump" == minor ]] || { echo "usage: $0 patch|minor" >&2; exit 2; }
dry_run=${BILOBA_RELEASE_DRY_RUN:-}

cd "$(dirname "${BASH_SOURCE[0]}")/.."

say() { printf '\n==> %s\n' "$*"; }
fail() { printf 'release failed: %s\n' "$*" >&2; exit 1; }
tool() { .release/release-tool "$@"; }
# shellcheck source=release-npm-wait.sh
source "$(dirname "${BASH_SOURCE[0]}")/release-npm-wait.sh"

[[ "$(git rev-parse --abbrev-ref HEAD)" == master ]] || fail "releases are cut from master"
[[ -z "$(git status --porcelain)" ]] || fail "the working tree is not clean"
if [[ -z "$dry_run" ]]; then
	# Trusted publishing (OIDC) needs npm >= 11.5.1.  Check before anything reaches origin.
	node -e 'const [a,b,c]=process.argv[1].split(".").map(Number); process.exit(a>11||(a==11&&(b>5||(b==5&&c>=1)))?0:1)' "$(npm --version)" ||
		fail "npm $(npm --version) is too old for trusted publishing (need >= 11.5.1)"
fi

mkdir -p .release && go build -o .release/release-tool ./scripts/release
version=$(tool next "$bump")
tag="v$version"

# ls-remote exits 2 when the tag is absent; anything else nonzero means origin could not be asked.
tag_status=0
git ls-remote --exit-code --tags origin "refs/tags/$tag" >/dev/null || tag_status=$?
[[ $tag_status == 0 || $tag_status == 2 ]] || fail "could not check origin for $tag"

if [[ $tag_status == 0 ]]; then
	say "$tag is already on origin - resuming the release from it"
	git fetch --force origin "refs/tags/$tag:refs/tags/$tag"
	git checkout --quiet --detach "$tag"
	[[ "$(tool current)" == "$version" ]] || fail "$tag does not have BILOBA_VERSION $version"
else
	say "Preparing $tag"
	tool prepare "$version"
	make sync-plugin-versions check-plugins
	make check-release TAG="$tag"
	git -c user.name="github-actions[bot]" -c user.email="41898282+github-actions[bot]@users.noreply.github.com" \
		commit --quiet --all --message "$tag"
	git tag "$tag"
	git show --stat HEAD
	if [[ -n "$dry_run" ]]; then
		say "dry run: not pushing master and $tag"
	else
		# Atomic: origin gets the release commit and its tag together or not at all, so a tag on
		# origin is proof the commit is there too.  Fast-forward only - fails if master moved.
		git push --atomic origin HEAD:refs/heads/master "refs/tags/$tag"
	fi
fi

say "Building bilobad $version"
rm -rf .release/bin .release/github
mkdir -p .release/github
scripts/build-bilobad.sh "$version" .release/bin
for dir in .release/bin/*/; do
	platform=$(basename "$dir")
	tar -czf ".release/github/bilobad_${version}_${platform%-*}_${platform#*-}.tar.gz" -C "$dir" bilobad -C "$PWD" LICENSE
done
(cd .release/github && if command -v sha256sum >/dev/null; then sha256sum bilobad_*.tar.gz; else shasum -a 256 bilobad_*.tar.gz; fi >SHA256SUMS)
tool notes "$version" >.release/notes.md
ls -l .release/github
cat .release/github/SHA256SUMS

if [[ -n "$dry_run" ]]; then
	say "dry run: not creating the GitHub release $tag"
elif gh release view "$tag" >/dev/null 2>&1; then
	say "Replacing the assets on the existing GitHub release $tag"
	gh release upload "$tag" --clobber .release/github/*
else
	say "Creating the GitHub release $tag"
	gh release create "$tag" --verify-tag --title "$tag" --notes-file .release/notes.md .release/github/*
fi

say "Publishing to npm"
(cd typescript && pnpm install --frozen-lockfile && pnpm build)
node scripts/prepare-npm-packages.mjs .release/bin
platform_packages=(biloba-darwin-arm64 biloba-darwin-x64 biloba-linux-arm64 biloba-linux-x64)
# The platform packages go first: biloba's exact-version optionalDependencies must resolve the
# moment it is published.
for package in "${platform_packages[@]}" biloba; do
	if [[ "$(npm view "$package@$version" version 2>/dev/null)" == "$version" ]]; then
		echo "$package@$version is already on npm - skipping"
		continue
	fi
	if [[ "$package" == biloba ]]; then
		if [[ -n "$dry_run" ]]; then
			say "dry run: not waiting for the registry to serve the platform packages"
		else
			say "Waiting for npm to serve the platform packages"
			wait_for_npm_packages "$version" "${platform_packages[@]}"
		fi
	fi
	# Trusted publishing attaches provenance on its own; no --provenance needed.
	npm publish ${dry_run:+--dry-run} --access public ".release/npm/$package"
done

say "Released $tag${dry_run:+ (dry run)}"
