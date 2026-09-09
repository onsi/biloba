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

const tag = process.env.GITHUB_REF_TYPE === "tag" ? process.env.GITHUB_REF_NAME : process.argv[2];
if (tag && tag !== `v${version}`) throw new Error(`release tag ${tag} does not match Biloba v${version}`);

console.log(`npm release metadata is consistent at Biloba ${version}`);
