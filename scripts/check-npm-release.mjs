#!/usr/bin/env node

import {readFile} from "node:fs/promises";
import {fileURLToPath} from "node:url";
import {dirname, resolve} from "node:path";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const bilobaSource = await readFile(resolve(repoRoot, "biloba.go"), "utf8");
const packageJSON = JSON.parse(await readFile(resolve(repoRoot, "typescript/package.json"), "utf8"));
const versionMatch = bilobaSource.match(/^const BILOBA_VERSION = "([^"]+)"/m);

if (!versionMatch) throw new Error("could not read BILOBA_VERSION from biloba.go");
const version = versionMatch[1];
if (packageJSON.version !== version) {
  throw new Error(`typescript/package.json version ${packageJSON.version} does not match Biloba ${version}`);
}
if (packageJSON.name !== "biloba") {
  throw new Error(`typescript/package.json name must be biloba, got ${packageJSON.name}`);
}
if (packageJSON.private) throw new Error("typescript/package.json must be publishable");
if (packageJSON.publishConfig?.access !== "public") {
  throw new Error("typescript/package.json publishConfig.access must be public");
}
if (packageJSON.repository?.url !== "git+https://github.com/onsi/biloba.git") {
  throw new Error("typescript/package.json repository must match github.com/onsi/biloba for npm provenance");
}
if (packageJSON.bin?.biloba !== "dist/cli.js") {
  throw new Error("typescript/package.json bin.biloba must point at dist/cli.js");
}
if (packageJSON.optionalDependencies !== undefined) {
  throw new Error("typescript/package.json must not commit optionalDependencies - they are injected only into the staged .release/npm/biloba manifest, or `pnpm install --frozen-lockfile` will try to fetch unpublished platform-package versions");
}
for (const name of Object.keys({...packageJSON.dependencies, ...packageJSON.devDependencies, ...packageJSON.peerDependencies})) {
  if (name.startsWith("@onsi/")) throw new Error(`typescript/package.json must not depend on a scoped @onsi/ package (${name}) - @onsi is not ours`);
}

const tag = process.env.GITHUB_REF_TYPE === "tag" ? process.env.GITHUB_REF_NAME : process.argv[2];
if (tag && tag !== `v${version}`) throw new Error(`release tag ${tag} does not match Biloba v${version}`);

console.log(`npm release metadata is consistent at Biloba ${version}`);
