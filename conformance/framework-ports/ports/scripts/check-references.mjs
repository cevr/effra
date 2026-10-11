import assert from "node:assert/strict";
import { existsSync, mkdirSync, readdirSync, readFileSync, realpathSync, rmSync } from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";
import { spawn, spawnSync } from "node:child_process";
import { parseArgs } from "node:util";

// --counter-domain-artifacts DIR verifies counter-domain artifacts an earlier emission wrote (see
// check-counter-domain.mjs); without it the counter-domain control builds a fresh compiler and emits them itself.
const { "counter-domain-artifacts": counterArtifacts } = parseArgs({ options: { "counter-domain-artifacts": { type: "string" } }, strict: true }).values;
const root = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const compiler = path.join(root, "node_modules/typescript/bin/tsc");
const compilerPackage = JSON.parse(readFileSync(path.join(root, "node_modules/typescript/package.json"), "utf8"));
assert.equal(compilerPackage.version, "6.0.3", "references require leaf TypeScript 6.0.3");
assert.ok(realpathSync(compiler).startsWith(`${path.join(root, "node_modules")}/`), "no root TypeScript fallback");
const env = { ...process.env, NODE_ENV: "test", NO_COLOR: "1", FORCE_COLOR: "0" };
console.log("reference profile: React development act (NODE_ENV=test); Solid/yield browser-production exports; leaf TypeScript6.0.3 skipLibCheck=true");
function run(label, executable, args) {
  return settle(label, executable, args, spawnSync(executable, args, { cwd: root, env, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 }));
}
// Children started alongside others; a failure joins them before the runner exits.
const pending = new Map();
// start runs a command alongside others; its output is reported, as run reports it, when the caller awaits it.
function start(label, executable, args) {
  const exited = new Promise(resolve => {
    const child = spawn(executable, args, { cwd: root, env });
    const stdout = [];
    const stderr = [];
    const text = chunks => Buffer.concat(chunks).toString("utf8");
    child.stdout.on("data", chunk => stdout.push(chunk));
    child.stderr.on("data", chunk => stderr.push(chunk));
    child.on("error", error => resolve({ status: null, signal: null, error, stdout: text(stdout), stderr: text(stderr) }));
    child.on("close", (status, signal) => resolve({ status, signal, error: null, stdout: text(stdout), stderr: text(stderr) }));
  });
  pending.set(label, exited);
  return exited.then(result => () => {
    pending.delete(label);
    return settle(label, executable, args, result);
  });
}
// A failed command is reported where it fails; its exit status becomes the runner's once started children are joined.
class CommandFailed extends Error {
  constructor(label, status) {
    super(`${label} failed`);
    this.status = status;
  }
}
function settle(label, executable, args, result) {
  console.log(`${label}_COMMAND=${JSON.stringify([executable, ...args])}`);
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
  console.log(`${label}_EXIT=${result.status}`);
  if (result.error) throw result.error;
  if (result.status !== 0) {
    if (result.signal) console.error(`${label}_SIGNAL=${result.signal}`);
    throw new CommandFailed(label, result.status ?? 1);
  }
  return `${result.stdout ?? ""}\n${result.stderr ?? ""}`;
}
try {
  await references();
} catch (error) {
  // The first failure decides the outcome; a started child still runs to its end, so none outlives the runner.
  for (const [label, exited] of pending) {
    const { status, signal } = await exited;
    console.log(`${label}_JOINED_AFTER_FAILURE_EXIT=${status}${signal ? ` signal ${signal}` : ""}`);
  }
  if (!(error instanceof CommandFailed)) throw error;
  process.exitCode = error.status;
}
// .generated is the run's own: the consumers resolve the domain through it (TypeScript tries counter.mts before
// counter.d.mts; Node resolves effect from .generated/node_modules first), so a name nobody keyed could decide the
// outcome. Before anything reads it, the runner removes every entry except the emitted artifacts it verifies and the
// emission's record, which nothing here reads. A failing control's workspace is kept until this next run.
function clearGenerated() {
  const generated = path.join(root, ".generated");
  if (!existsSync(generated)) return;
  const kept = new Set(["counter-domain-emit-evidence.json"]);
  if (counterArtifacts) {
    const relative = path.relative(generated, path.resolve(counterArtifacts));
    if (relative !== "" && !relative.startsWith("..") && !path.isAbsolute(relative)) kept.add(relative.split(path.sep)[0]);
  }
  for (const name of readdirSync(generated)) {
    if (!kept.has(name)) rmSync(path.join(generated, name), { recursive: true, force: true });
  }
}
async function references() {
  clearGenerated();
  const bunVersion = run("BUN_VERSION", "bun", ["--version"]).trim();
  assert.equal(bunVersion, "1.4.2", "references require Bun1.4.2");
  run("CAPTURE", process.execPath, ["scripts/check-yield-capture.mjs"]);
  run("DEPENDENCIES", process.execPath, ["scripts/check-dependency-profile.mjs"]);
  // The vendor build writes into dist without clearing it. Starting from none makes every built file this run's own
  // output, so no stale file there can reach the checks below, and the gate's key leaves dist out.
  rmSync(path.join(root, "vendor/solid-yield/dist"), { recursive: true, force: true });
  // The counter-domain control and the vendor build share nothing, so they run together; the consumer check needs
  // both. Emitting the vendor declarations type-checks the same program a separate --noEmit pass would.
  const counterDomain = start("COUNTER_DOMAIN", process.execPath, ["scripts/check-counter-domain.mjs", ...(counterArtifacts ? ["--verify", path.resolve(counterArtifacts)] : [])]);
  run("VENDOR_BUILD", process.execPath, ["vendor/solid-yield/scripts/build.mjs"]);
  run("VENDOR_DECLARATIONS", process.execPath, [compiler, "-p", "tsconfig.yield.json"]);
  (await counterDomain)();
  run("CONSUMER_NOEMIT", process.execPath, [compiler, "--noEmit", "-p", "tsconfig.json"]);
  run("PUBLIC_EXPORTS", process.execPath, ["--conditions=browser", "--conditions=production", "scripts/check-dependency-profile.mjs", "--built"]);
  // Bun hides per-test pass lines when it detects an AI agent caller, so per-host results come from its JUnit record.
  const junit = path.join(root, ".generated/framework-references.junit.xml");
  mkdirSync(path.dirname(junit), { recursive: true });
  rmSync(junit, { force: true });
  const mounted = run("MOUNTED", "bun", ["--conditions=browser", "--conditions=production", "test", "--preload", "./tests/process-dom-preload.ts", "--reporter=junit", `--reporter-outfile=${junit}`, "tests/framework-references.test.tsx"]);
  assert.match(mounted, /^\s*3 pass\s*$/m, "all three mounted cases must execute");
  assert.match(mounted, /^\s*0 fail\s*$/m, "mounted failures cannot pass");
  assert.doesNotMatch(mounted, /^\s*[1-9][0-9]* (?:skip|todo)\s*$/m, "skipped mounted cases cannot pass");
  const report = readFileSync(junit, "utf8");
  assert.match(report, /<testsuites name="bun test" tests="3" assertions="[1-9][0-9]*" failures="0" skipped="0"/, "JUnit record: three executed cases, no failures or skips");
  assert.equal((report.match(/<testcase /g) ?? []).length, 3, "JUnit record lists exactly three cases");
  for (const host of ["react-atom", "solid-effect", "solid-yield"]) {
    const name = `${host} reference mounts, applies source events below zero, resets from nonzero and releases its subscriptions`;
    assert.match(report, new RegExp(`<testcase name="${name}"[^>]* assertions="[1-9][0-9]*" />`), `missing executed host: ${host}`);
  }
  console.log("FRAMEWORK_REFERENCES_EXIT=0 (three mounted Number-reference hosts; no compiled Effra ports)");
}
