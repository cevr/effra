import assert from "node:assert/strict";
import { readFileSync, realpathSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const compiler = path.join(root, "node_modules/typescript/bin/tsc");
const compilerPackage = JSON.parse(readFileSync(path.join(root, "node_modules/typescript/package.json"), "utf8"));
assert.equal(compilerPackage.version, "6.0.3", "references require leaf TypeScript 6.0.3");
assert.ok(realpathSync(compiler).startsWith(`${path.join(root, "node_modules")}/`), "no root TypeScript fallback");
const env = { ...process.env, NODE_ENV: "test", NO_COLOR: "1", FORCE_COLOR: "0" };
console.log("reference profile: React development act (NODE_ENV=test); Solid/yield browser-production exports; leaf TypeScript6.0.3 skipLibCheck=true");
function run(label, executable, args) {
  console.log(`${label}_COMMAND=${JSON.stringify([executable, ...args])}`);
  const result = spawnSync(executable, args, { cwd: root, env, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
  console.log(`${label}_EXIT=${result.status}`);
  if (result.error) throw result.error;
  if (result.status !== 0) {
    if (result.signal) console.error(`${label}_SIGNAL=${result.signal}`);
    process.exit(result.status ?? 1);
  }
  return `${result.stdout ?? ""}\n${result.stderr ?? ""}`;
}
const bunVersion = run("BUN_VERSION", "bun", ["--version"]).trim();
assert.equal(bunVersion, "1.4.2", "references require Bun1.4.2");
run("CAPTURE", process.execPath, ["scripts/check-yield-capture.mjs"]);
run("DEPENDENCIES", process.execPath, ["scripts/check-dependency-profile.mjs"]);
run("VENDOR_NOEMIT", process.execPath, [compiler, "--noEmit", "-p", "tsconfig.yield.json"]);
run("VENDOR_BUILD", process.execPath, ["vendor/solid-yield/scripts/build.mjs"]);
run("VENDOR_DECLARATIONS", process.execPath, [compiler, "-p", "tsconfig.yield.json"]);
run("CONSUMER_NOEMIT", process.execPath, [compiler, "--noEmit", "-p", "tsconfig.json"]);
run("PUBLIC_EXPORTS", process.execPath, ["--conditions=browser", "--conditions=production", "scripts/check-dependency-profile.mjs", "--built"]);
const mounted = run("MOUNTED", "bun", ["--conditions=browser", "--conditions=production", "test", "--preload", "./tests/process-dom-preload.ts", "tests/framework-references.test.tsx"]);
assert.match(mounted, /^\s*3 pass\s*$/m, "all three mounted cases must execute");
assert.match(mounted, /^\s*0 fail\s*$/m, "mounted failures cannot pass");
assert.doesNotMatch(mounted, /^\s*[1-9][0-9]* (?:skip|todo)\s*$/m, "skipped mounted cases cannot pass");
for (const host of ["react-atom", "solid-effect", "solid-yield"]) {
  assert.ok(mounted.includes(`(pass) ${host} reference mounts, applies source events below zero, resets from nonzero and releases its subscriptions`), `missing executed host: ${host}`);
}
console.log("FRAMEWORK_REFERENCES_EXIT=0 (three mounted Number-reference hosts; no compiled Effra ports)");
