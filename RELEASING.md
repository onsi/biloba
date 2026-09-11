# Releasing Biloba on npm

Biloba uses `BILOBA_VERSION` in `biloba.go` as the version source for the Go module, Claude Code
plugins, and TypeScript client. The same TypeScript build is published under the npm names `biloba`
and `@onsi/biloba`.

## Hook for the existing release script

After the private release script updates `BILOBA_VERSION`, it should continue to invoke the existing
`make sync-plugin-versions` hook. That target now also writes the version to
`typescript/package.json`. Before creating the release tag, run:

```bash
./scripts/publish-npm-packages.sh --dry-run vX.Y.Z
```

After the normal release tests pass and the matching tag is created, publish with:

```bash
./scripts/publish-npm-packages.sh --publish vX.Y.Z
```

The publishing environment must already be authenticated with npm and authorized for both package
names. The script contains and reads no credentials. It builds once, stages identical package trees
under `.release/npm/`, validates both with `npm pack --dry-run`, and then publishes them. Published
npm versions are immutable, so a retry skips a name whose matching version already exists and
continues with the missing one.

The equivalent Make targets are `make npm-pack TAG=vX.Y.Z` and `make npm-publish` (the latter derives
the tag from the current package version).

## Initial publication

The first run claims each available package name. The publishing account must have 2FA enabled (or
use an npm granular token allowed to publish) and must belong to the `onsi` npm organization to
publish `@onsi/biloba`.

The `@biloba` scope is not an option: the `biloba` npm org/user already exists and belongs to another
account (the registry reports the scope with no public members), so nobody else can publish there.
