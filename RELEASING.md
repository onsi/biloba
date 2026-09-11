# Releasing Biloba

A release is one GitHub Actions run: **Actions → Release → Run workflow → bump: patch | minor**,
from `master`. From the command line:

```bash
gh workflow run release.yml -f bump=patch   # or -f bump=minor
```

The run:

1. Runs the whole test workflow (`test.yml`, including the npm packaging check). Nothing else
   happens unless it passes.
2. Runs `scripts/release.sh`, which
   - computes the next version from `BILOBA_VERSION` in `biloba.go`,
   - renames `## Unreleased` in `CHANGELOG.md` to `## X.Y.Z` (dropping empty `###` subsections) and
     puts a fresh, empty `## Unreleased` above it,
   - writes `BILOBA_VERSION` and runs `make sync-plugin-versions check-plugins check-release`,
   - commits `vX.Y.Z` as `github-actions[bot]`, tags it `vX.Y.Z`, and pushes the commit and the tag
     together (`git push --atomic`, fast-forward only),
   - builds `bilobad` for darwin/linux × amd64/arm64 and creates the GitHub release `vX.Y.Z` with
     `bilobad_X.Y.Z_<os>_<arch>.tar.gz` archives and `SHA256SUMS` attached. The release notes are
     the body of `## X.Y.Z`,
   - publishes `biloba-darwin-arm64`, `biloba-darwin-x64`, `biloba-linux-arm64`,
     `biloba-linux-x64`, then `biloba` to npm, using trusted publishing (no npm token; npm adds
     provenance).

The Go module is released by the `vX.Y.Z` tag. There is no other release tooling.

## The changelog

`CHANGELOG.md` starts with an `## Unreleased` section:

```markdown
## Unreleased

### Features

### Fixes

## 0.15.4
...
```

Add an entry under `## Unreleased` with each user-facing change. Never edit a released section.

If `## Unreleased` has no entries (only blank lines and `###` headings), the release stops before
changing anything.

## One-time npm setup

`biloba` already exists on npm. The four platform packages need to exist before a trusted publisher
can be attached to them, so claim each name once with a `0.0.0` placeholder, logged in to npm as a
maintainer:

```bash
for name in biloba-darwin-arm64 biloba-darwin-x64 biloba-linux-arm64 biloba-linux-x64; do
  dir=$(mktemp -d)
  cat >"$dir/package.json" <<EOF
{
  "name": "$name",
  "version": "0.0.0",
  "description": "Platform binary for biloba; install biloba instead.",
  "license": "MIT",
  "repository": {"type": "git", "url": "git+https://github.com/onsi/biloba.git"}
}
EOF
  (cd "$dir" && npm publish --access public)
done
```

`biloba`'s `optionalDependencies` pin exact versions, so nothing ever installs `0.0.0`.

`biloba-win32-x64` and `biloba-win32-arm64` are reserved the same way for a future Windows build.
The release workflow doesn't publish them and `biloba` doesn't depend on them, so they need no
trusted publisher until Windows ships.

Then, for each of the five packages (`biloba` and the four above), on npmjs.com → the package →
**Settings**:

1. **Trusted Publisher** → GitHub Actions: organization or user `onsi`, repository `biloba`,
   workflow filename `release.yml`, environment left blank. Check **Allow `npm publish`**: without
   it the publisher can only `npm stage publish`, and `release.sh` publishes directly.
2. **Publishing access** → "Require two-factor authentication and disallow tokens".

Trusted publishing needs npm 11.5.1 or later; the workflow uses Node 24, which ships it.

## If a release fails

Open the failed run and click **Re-run failed jobs**. A re-run checks out the same commit as the
first attempt and computes the same version. It resumes based on what already exists:

| Where it failed | What a re-run does |
|---|---|
| Tests, or before the push | Nothing reached GitHub. The re-run starts over. |
| The push (master moved during the run) | The re-run fails the same way. Start a new run from the current master. |
| After the push (the tag `vX.Y.Z` is on GitHub) | The re-run builds from the tag instead of making a new commit. |
| Creating the GitHub release | The release is created, or its assets are replaced if it already exists. |
| Part-way through npm | Packages already published at `X.Y.Z` are skipped; the rest are published. |

The commit and the tag are pushed together, so the tag being on GitHub means the release commit is
on master too.

Running the workflow again after a successful release changes nothing: it finds an empty
`## Unreleased` and fails with "nothing to release".

## Trying the release script locally

`BILOBA_RELEASE_DRY_RUN=1 scripts/release.sh patch` does the whole release locally — the commit and
tag, the archives in `.release/github`, the npm packages in `.release/npm` — then skips the push and
the GitHub release and runs `npm publish --dry-run`. It still checks `origin` for an existing tag,
so run it in a throwaway clone whose `origin` is a local bare repository, not in your working copy.

`make packaging-check` packs the npm packages, installs them into an empty project, and runs a Vitest
spec against them, as CI does. `make packaging-check VITEST=vitest@3` picks the Vitest version.
