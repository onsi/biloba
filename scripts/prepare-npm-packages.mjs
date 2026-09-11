#!/usr/bin/env node
// Stages the five packages `make npm-pack` (and, later, the release workflow) will `npm pack`:
// `biloba` (the TS client) plus four per-platform packages that each carry just a `bilobad`
// binary. Usage: prepare-npm-packages.mjs <binaries-dir>, where <binaries-dir> is the output of
// scripts/build-bilobad.sh (<binaries-dir>/<os>-<arch>/bilobad, Go's GOOS/GOARCH spelling).

import {chmod, cp, mkdir, readFile, rm, writeFile} from "node:fs/promises";
import {fileURLToPath} from "node:url";
import {dirname, resolve} from "node:path";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = resolve(repoRoot, "typescript");
const releaseRoot = resolve(repoRoot, ".release/npm");

const binariesDirArgument = process.argv[2];
if (!binariesDirArgument) {
  console.error("usage: prepare-npm-packages.mjs <binaries-dir>");
  console.error("  <binaries-dir>: output of scripts/build-bilobad.sh, i.e. <binaries-dir>/<os>-<arch>/bilobad per target.");
  process.exit(2);
}
const binariesDir = resolve(binariesDirArgument);

const sourceManifest = JSON.parse(await readFile(resolve(sourceRoot, "package.json"), "utf8"));
const version = sourceManifest.version;

// npm/node's os/cpu naming for each platform package, mapped to the Go build's GOOS/GOARCH
// directory name (Go says "amd64" where node says "x64").
const platforms = [
  {os: "darwin", cpu: "arm64", goDir: "darwin-arm64"},
  {os: "darwin", cpu: "x64", goDir: "darwin-amd64"},
  {os: "linux", cpu: "arm64", goDir: "linux-arm64"},
  {os: "linux", cpu: "x64", goDir: "linux-amd64"},
].map((platform) => ({...platform, name: `biloba-${platform.os}-${platform.cpu}`}));

await rm(releaseRoot, {recursive: true, force: true});

for (const platform of platforms) {
  const destination = resolve(releaseRoot, platform.name);
  const binDestination = resolve(destination, "bin");
  await mkdir(binDestination, {recursive: true});

  const sourceBinary = resolve(binariesDir, platform.goDir, "bilobad");
  const destinationBinary = resolve(binDestination, "bilobad");
  await cp(sourceBinary, destinationBinary);
  await chmod(destinationBinary, 0o755);

  const manifest = {
    name: platform.name,
    version,
    description: `bilobad daemon binary for ${platform.os}/${platform.cpu} - installed automatically as an optional dependency of biloba, never used directly.`,
    os: [platform.os],
    cpu: [platform.cpu],
    license: sourceManifest.license,
    repository: {type: sourceManifest.repository.type, url: sourceManifest.repository.url},
    homepage: sourceManifest.homepage,
    // No "bin" field: this package deliberately does not put a `bilobad` on the user's PATH.
    files: ["bin"],
  };
  await writeFile(resolve(destination, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
  await writeFile(resolve(destination, "README.md"), "Platform binary for biloba; install `biloba` instead.\n");
  await cp(resolve(repoRoot, "LICENSE"), resolve(destination, "LICENSE"));
}

{
  const destination = resolve(releaseRoot, "biloba");
  await mkdir(destination, {recursive: true});
  await cp(resolve(sourceRoot, "dist"), resolve(destination, "dist"), {recursive: true});
  // npm is expected to normalize the "bin" file's mode on pack/publish, but set it explicitly too
  // so a plain `tar -tvzf` of the staged tarball already shows it executable.
  await chmod(resolve(destination, "dist/cli.js"), 0o755);
  await cp(resolve(sourceRoot, "README.md"), resolve(destination, "README.md"));
  await cp(resolve(repoRoot, "LICENSE"), resolve(destination, "LICENSE"));

  const manifest = {...sourceManifest};
  delete manifest.devDependencies;
  delete manifest.packageManager;
  delete manifest.scripts;
  // Exact-version optionalDependencies exist only here, never in typescript/package.json: a
  // `pnpm install --frozen-lockfile` there would otherwise try to fetch platform-package versions
  // that haven't been published yet, breaking local dev and CI.
  manifest.optionalDependencies = Object.fromEntries(platforms.map((platform) => [platform.name, version]));
  await writeFile(resolve(destination, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
}

console.log(`staged biloba ${version} and its four platform packages in .release/npm`);
