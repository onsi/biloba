#!/usr/bin/env node

import {cp, mkdir, readFile, rm, writeFile} from "node:fs/promises";
import {fileURLToPath} from "node:url";
import {dirname, resolve} from "node:path";

const repoRoot = resolve(dirname(fileURLToPath(import.meta.url)), "..");
const sourceRoot = resolve(repoRoot, "typescript");
const releaseRoot = resolve(repoRoot, ".release/npm");
const sourceManifest = JSON.parse(await readFile(resolve(sourceRoot, "package.json"), "utf8"));
const variants = [
  {directory: "scoped", name: "@onsi/biloba"},
  {directory: "unscoped", name: "biloba"},
];

await rm(releaseRoot, {recursive: true, force: true});

for (const variant of variants) {
  const destination = resolve(releaseRoot, variant.directory);
  await mkdir(destination, {recursive: true});
  await cp(resolve(sourceRoot, "dist"), resolve(destination, "dist"), {recursive: true});
  await cp(resolve(sourceRoot, "README.md"), resolve(destination, "README.md"));
  await cp(resolve(repoRoot, "LICENSE"), resolve(destination, "LICENSE"));

  const manifest = {...sourceManifest, name: variant.name};
  delete manifest.devDependencies;
  delete manifest.packageManager;
  delete manifest.scripts;
  await writeFile(resolve(destination, "package.json"), `${JSON.stringify(manifest, null, 2)}\n`);
}

console.log(`staged @onsi/biloba and biloba ${sourceManifest.version} in .release/npm`);
