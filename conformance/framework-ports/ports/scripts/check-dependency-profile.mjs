import assert from "node:assert/strict";
import { readFileSync, realpathSync, existsSync } from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const manifest = JSON.parse(readFileSync(path.join(root, "package.json"), "utf8"));
// Bun emits trailing commas; package strings cannot contain this newline boundary.
const lock = JSON.parse(readFileSync(path.join(root, "bun.lock"), "utf8").replace(/,(\s*[}\]])/g, "$1"));
const leafModules = path.join(root, "node_modules");
assert.ok(existsSync(leafModules), "missing leaf dependencies; install its changed lock explicitly");
function resolvePackage(name, from = path.join(root, "package.json")) {
  const resolver = createRequire(from);
  let entry;
  try { entry = resolver.resolve(name); }
  catch { entry = resolver.resolve(`${name}/package.json`); }
  let directory = path.dirname(realpathSync(entry));
  while (directory !== path.dirname(directory)) {
    const filename = path.join(directory, "package.json");
    if (existsSync(filename)) {
      const pkg = JSON.parse(readFileSync(filename, "utf8"));
      if (pkg.name === name) return { entry: realpathSync(entry), directory, filename, pkg };
    }
    directory = path.dirname(directory);
  }
  throw new Error(`no package owner for ${name} from ${from}`);
}
const dependencies = { ...manifest.dependencies, ...manifest.devDependencies };
for (const [name, version] of Object.entries(dependencies)) {
  if (name === "solid-yield") {
    assert.equal(version, "workspace:*");
    assert.equal(realpathSync(path.join(leafModules, name)), realpathSync(path.join(root, "vendor/solid-yield")));
    const pkg = JSON.parse(readFileSync(path.join(leafModules, name, "package.json"), "utf8"));
    assert.equal(pkg.version, "0.0.0");
  } else {
    const installed = resolvePackage(name);
    assert.ok(installed.directory.startsWith(`${leafModules}/`), `root fallback: ${name}`);
    assert.equal(installed.pkg.version, version, `installed version: ${name}`);
  }
}
const effect = resolvePackage("effect");
const atom = resolvePackage("@effect/atom-react");
const react = resolvePackage("react");
const reactDom = resolvePackage("react-dom");
assert.equal(resolvePackage("effect", atom.filename).entry, effect.entry, "Atom must use the same Effect instance");
assert.equal(resolvePackage("react", atom.filename).entry, react.entry, "Atom must use the same React instance");
assert.equal(resolvePackage("react", reactDom.filename).entry, react.entry, "React DOM must use the same React instance");
assert.equal(resolvePackage("scheduler", atom.filename).pkg.version, "0.27.0");
assert.equal(resolvePackage("scheduler", reactDom.filename).pkg.version, "0.28.0");
const effectRows = Object.entries(lock.packages).filter(([, row]) => row[0].startsWith("effect@"));
assert.equal(effectRows.length, 1, "one locked Effect resolution");
assert.equal(effectRows[0][1][0], "effect@4.0.1");
const schedulerRows = Object.entries(lock.packages).filter(([, row]) => row[0].startsWith("scheduler@"));
assert.deepEqual(schedulerRows.map(([, row]) => row[0]).sort(), ["scheduler@0.27.0", "scheduler@0.28.0"]);
for (const [key, row] of Object.entries(lock.packages)) {
  if (row[0].includes("@workspace:")) continue;
  assert.match(row.at(-1), /^sha512-[A-Za-z0-9+/]+={0,2}$/, `locked integrity: ${key}`);
}
console.log(JSON.stringify({
  effect: { version: effect.pkg.version, entry: effect.entry },
  atom: { version: atom.pkg.version, entry: atom.entry },
  yieldWorkspace: realpathSync(path.join(leafModules, "solid-yield")),
  schedulerOwners: {
    atom: resolvePackage("scheduler", atom.filename).directory,
    reactDom: resolvePackage("scheduler", reactDom.filename).directory
  },
  lockPackageCount: Object.keys(lock.packages).length
}, null, 2));

if (process.argv.includes("--built")) {
  const yieldRoot = realpathSync(path.join(root, "vendor/solid-yield"));
  for (const name of ["solid-yield", "solid-yield/h", "solid-yield/internal", "solid-yield/jsx-runtime"]) {
    const entry = createRequire(path.join(root, "package.json")).resolve(name);
    assert.ok(realpathSync(entry).startsWith(`${yieldRoot}/dist/`), `built workspace entry: ${name}`);
    await import(name);
  }
  for (const declaration of ["index.d.ts", "h.d.ts", "internal.d.ts"]) {
    assert.ok(existsSync(path.join(leafModules, "solid-yield/dist/types", declaration)), `installed declaration: ${declaration}`);
  }
  console.log("yield built JS imports and installed declarations: visible through exact workspace link");
}
