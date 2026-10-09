import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, writeFileSync } from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";

const leaf = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repository = path.resolve(leaf, "../../..");
const generated = path.join(leaf, ".generated");
mkdirSync(generated, { recursive: true });
const work = mkdtempSync(path.join(generated, "counter-domain-"));
const compiler = path.join(work, "ef");
const localTS = path.join(leaf, "node_modules/typescript/bin/tsc");
const env = { ...process.env, GOCACHE: "/home/exedev/.cache/go-build", GOPROXY: "off", GOTOOLCHAIN: "auto", GOWORK: "off" };
const records = [];
const hash = data => createHash("sha256").update(data).digest("hex");
const fileHash = file => hash(readFileSync(file));
const requiredValues = ["initialCounter", "counterState", "clickedDecrement", "clickedIncrement", "clickedReset", "updateCounter", "counterCount"];
const domainPath = path.join(leaf, "fixtures/counter.ef");
const harnessPath = path.join(leaf, "fixtures/counter-native.ef");
const domain = readFileSync(domainPath, "utf8");
const harness = readFileSync(harnessPath, "utf8");
const mutation = "CounterEvent.ClickedIncrement => CounterState {\n            count: state.count + 1\n        }";
assert.equal(domain.split(mutation).length, 2, "exactly one authored increment branch");
const mutant = domain.replace(mutation, mutation.replace("+ 1", "- 1"));
assert.notEqual(mutant, domain);
const positive = readFileSync(path.join(leaf, "tests/types/counter-positive.ts"), "utf8");
// The native entry reports an unhandled failure by its tag alone, so the native suffix labels each check before Assert.check.
const nativeChecks = ["CounterInitial: expected 0", "ClickedIncrement: expected 1", "ClickedDecrement: expected 0", "ClickedDecrement: expected -1", "ClickedReset: expected 0 from -1", "ClickedIncrement: expected 1 again", "ClickedReset: expected 0 from 1", "I64Increment: max wraps to min", "I64Decrement: min wraps to max"];
const intendedCheck = "ClickedIncrement: expected 1";
const checkedLabels = stdout => [...stdout.matchAll(/^COUNTER_CHECK=(.*)$/gm)].map(match => match[1]);
const evidence = { schema: "effra-counter-domain-controls-v1", claim: "Domain/ABI controls only; all 170 Effra coverage rows pending", work, source: { path: domainPath, sha256: fileHash(domainPath) }, harness: { path: harnessPath, sha256: fileHash(harnessPath) }, mutation: { original: mutation, replacement: mutation.replace("+ 1", "- 1"), productionSha256: hash(domain), mutantSha256: hash(mutant) }, records, artifacts: [], nativePublicationNamespace: { buildCwd: work, snapshots: path.join(work, "dist/go/apps"), owner: "cmd/ef/main.go:690 cwd-relative publication" } };
const receipt = path.join(generated, "counter-domain-evidence.json");
function save() { writeFileSync(receipt, `${JSON.stringify(evidence, null, 2)}\n`); }
function run(label, executable, args, expectedFailure = false, cwd = leaf) {
  console.log(`COUNTER_${label}_COMMAND=${JSON.stringify([executable, ...args])}`);
  const result = spawnSync(executable, args, { cwd, env, encoding: "utf8", maxBuffer: 32 * 1024 * 1024 });
  const record = { label, argv: [executable, ...args], cwd, exit: result.status, signal: result.signal, stdout: result.stdout ?? "", stderr: result.stderr ?? "", spawnError: result.error?.message ?? null, expectedFailure };
  records.push(record); save();
  if (result.stdout) process.stdout.write(result.stdout);
  if (result.stderr) process.stderr.write(result.stderr);
  console.log(`COUNTER_${label}_EXIT=${result.status}`);
  assert.equal(record.spawnError, null, `${label} must start`);
  assert.equal(record.signal, null, `${label} cannot be satisfied by a signal`);
  assert.ok(Number.isInteger(record.exit), `${label} needs a numeric target exit`);
  if (expectedFailure) assert.notEqual(record.exit, 0, `${label} must fail its intended assertion`);
  else assert.equal(record.exit, 0, `${label} must succeed`);
  return record;
}
function artifact(file) {
  assert.ok(lstatSync(file).isFile() && !lstatSync(file).isSymbolicLink(), `actual regular generated artifact: ${file}`);
  evidence.artifacts.push({ path: file, bytes: lstatSync(file).size, mode: (lstatSync(file).mode & 0o777).toString(8), sha256: fileHash(file) });
}
function abi(module) {
  const declarations = module.replace(/\.mjs$/, ".d.mts");
  const text = readFileSync(declarations, "utf8");
  assert.match(text, /export interface CounterState\b/);
  assert.match(text, /export type CounterEvent\b/);
  assert.match(text, /declare const __ef_brand_CounterState: unique symbol/);
  assert.match(text, /declare const __ef_brand_CounterEvent: unique symbol/);
  assert.match(text, /readonly count: bigint/);
  for (const name of requiredValues) assert.match(text, new RegExp(`export \\{[^}]*\\bas ${name}\\s*\\}`), `declared public export ${name}`);
  artifact(module); artifact(declarations); save();
}
function strictConsumer(label, module, filename = "positive") {
  const source = path.join(work, `${label}-${filename}.ts`);
  const specifier = path.relative(path.dirname(source), module).split(path.sep).join("/");
  const text = positive.replaceAll("../../.generated/counter.mjs", specifier.startsWith(".") ? specifier : `./${specifier}`);
  assert.ok(!text.includes("../../.generated/counter.mjs"));
  writeFileSync(source, text);
  const config = path.join(work, `${label}-${filename}.json`);
  writeFileSync(config, JSON.stringify({ extends: path.join(leaf, "tsconfig.counter-contract.json"), files: [source], include: [], exclude: [] }));
  run(`${label}_TYPES`, process.execPath, [localTS, "--noEmit", "-p", config]);
  artifact(source); artifact(config);
}
function nativeSource(label, source) {
  const filename = path.join(work, `${label}.ef`);
  const combined = `${source}\n${harness}`;
  writeFileSync(filename, combined);
  assert.equal(readFileSync(filename).subarray(0, Buffer.byteLength(source)).toString(), source, "native harness retains exact domain prefix bytes");
  evidence.artifacts.push({ path: filename, sha256: fileHash(filename), domainPrefixBytes: Buffer.byteLength(source), domainPrefixSha256: hash(source), assertionSuffixSha256: hash(`\n${harness}`) });
  return filename;
}
function intendedMutant(record, target) {
  assert.match(record.stdout, /^COUNTER_INITIAL_ZERO=0$/m, `${target} mutant reaches successful initialization`);
  if (target !== "Go") assert.match(record.stdout, new RegExp(`^COUNTER_SETUP_OK=${target}$`, "m"), "actual public module import/setup succeeded");
  if (target === "Go") {
    assert.deepEqual(checkedLabels(record.stdout), nativeChecks.slice(0, nativeChecks.indexOf(intendedCheck) + 1), "Go mutant stops at the intended labelled check");
    assert.equal(record.stderr, "AssertionFailed\n", "native public Assert failure identity and no other cause");
  } else assert.match(record.stderr, /ClickedIncrement: expected 1/, `${target} fails the intended event assertion`);
  assert.doesNotMatch(record.stdout, /COUNTER_(?:NATIVE|DOMAIN)_TRACE_WRAP_GREEN/);
  record.causalIdentity = { target, setupAndInitialZeroReached: true, intendedAssertion: intendedCheck, actualMutantExit: record.exit };
  save();
}

try {
  assert.equal(process.versions.node, "24.11.1", "selected actual Node version");
  const tsPackage = JSON.parse(readFileSync(path.join(leaf, "node_modules/typescript/package.json"), "utf8"));
  assert.equal(tsPackage.version, "6.0.3");
  assert.ok(realpathSync(localTS).startsWith(`${path.join(leaf, "node_modules")}/`), "leaf TypeScript owner, no root fallback");
  assert.equal(run("BUN_VERSION", "bun", ["--version"]).stdout.trim(), "1.4.2");
  assert.equal(run("LEAF_TS_VERSION", process.execPath, [localTS, "--version"]).stdout.trim(), "Version 6.0.3");
  evidence.parent = { head: run("SOURCE_HEAD", "git", ["rev-parse", "HEAD"], false, repository).stdout.trim(), headTree: run("SOURCE_TREE", "git", ["rev-parse", "HEAD^{tree}"], false, repository).stdout.trim() };
  const files = run("COMPILER_INPUTS", "git", ["ls-files", "-z", "--", "cmd", "internal", "go.mod", "go.sum"], false, repository).stdout.split("\0").filter(Boolean);
  evidence.compilerInputs = files.map(file => ({ path: file, sha256: fileHash(path.join(repository, file)) }));
  evidence.compilerInputsSha256 = hash(JSON.stringify(evidence.compilerInputs));
  run("COMPILER_BUILD", "go", ["build", "-trimpath", "-mod=readonly", "-o", compiler, "./cmd/ef"], false, repository);
  artifact(compiler);
  run("SOURCE_GO_CHECK", compiler, ["check", domainPath, "--target", "go"]);
  run("SOURCE_JS_CHECK", compiler, ["check", domainPath, "--target", "js"]);
  const production = path.join(generated, "counter.mjs");
  const effectPackagePath = path.join(leaf, "node_modules/effect/package.json");
  const effectPackage = JSON.parse(readFileSync(effectPackagePath, "utf8"));
  assert.equal(effectPackage.version, "4.0.1");
  const fromGenerated = createRequire(production).resolve("effect");
  const fromLeaf = createRequire(path.join(leaf, "package.json")).resolve("effect");
  assert.equal(realpathSync(fromGenerated), realpathSync(fromLeaf), "generated domain and Atom share the actual leaf Effect owner");
  evidence.leafEffect = { packagePath: effectPackagePath, physicalPackagePath: realpathSync(effectPackagePath), name: effectPackage.name, version: effectPackage.version, packageJsonSha256: fileHash(effectPackagePath), runtimeEntry: realpathSync(fromGenerated) };
  evidence.previousCanonicalOutputs = [];
  for (const file of [production, production.replace(/\.mjs$/, ".d.mts")]) {
    if (existsSync(file)) {
      assert.ok(lstatSync(file).isFile() && !lstatSync(file).isSymbolicLink());
      const destination = path.join(work, `previous-${path.basename(file)}`);
      evidence.previousCanonicalOutputs.push({ source: file, destination, sha256: fileHash(file) });
      renameSync(file, destination);
    }
  }
  assert.ok(!existsSync(production) && !existsSync(production.replace(/\.mjs$/, ".d.mts")));
  run("LIBRARY_BUILD", compiler, ["build", domainPath, "--target", "js", "-o", production]);
  abi(production);
  run("POSITIVE_TYPES", process.execPath, [localTS, "--noEmit", "-p", path.join(leaf, "tsconfig.counter-contract.json")]);
  for (const [kind, column, expected] of [["number", 14, /number[\s\S]*bigint/], ["state", 15, /__ef_brand_CounterState/], ["event", 33, /__ef_brand_CounterEvent/]]) {
    const source = path.join(leaf, `tests/types/counter-${kind}-negative.ts`);
    const config = path.join(work, `negative-${kind}.json`);
    writeFileSync(config, JSON.stringify({ extends: path.join(leaf, "tsconfig.counter-contract.json"), files: [source], include: [], exclude: [] }));
    const record = run(`NEGATIVE_${kind.toUpperCase()}`, process.execPath, [localTS, "--noEmit", "--pretty", "false", "-p", config], true);
    const output = `${record.stdout}\n${record.stderr}`;
    const diagnostic = `counter-${kind}-negative.ts(3,${column}): error TS2345:`;
    assert.ok(output.includes(diagnostic), `intended parameter/brand location ${diagnostic}`);
    assert.equal((output.match(/error TS[0-9]+:/g) ?? []).length, 1, "one intended negative diagnostic, not arbitrary compiler failure");
    assert.match(output, expected); record.intendedDiagnostic = diagnostic; save();
  }
  const productionNative = nativeSource("production-native", domain);
  run("GO_PRODUCTION_CHECK", compiler, ["check", productionNative, "--target", "go"]);
  const native = path.join(work, "counter-go");
  run("GO_PRODUCTION_BUILD", compiler, ["build", productionNative, "--target", "go", "-o", native], false, work);
  assert.equal(realpathSync(evidence.nativePublicationNamespace.snapshots), path.join(realpathSync(work), "dist/go/apps"), "native snapshots remain inside the fresh ignored workspace");
  artifact(native);
  const go = run("GO_PRODUCTION_RUN", native, []);
  assert.match(go.stdout, /^COUNTER_INITIAL_ZERO=0$/m);
  assert.deepEqual(checkedLabels(go.stdout), nativeChecks, "Go production runs every labelled check in order");
  assert.match(go.stdout, /^COUNTER_NATIVE_TRACE_WRAP_GREEN$/m);
  const mutantNative = nativeSource("mutant-native", mutant);
  run("GO_MUTANT_CHECK", compiler, ["check", mutantNative, "--target", "go"]);
  const nativeMutant = path.join(work, "counter-go-mutant");
  run("GO_MUTANT_BUILD", compiler, ["build", mutantNative, "--target", "go", "-o", nativeMutant], false, work);
  assert.equal(realpathSync(evidence.nativePublicationNamespace.snapshots), path.join(realpathSync(work), "dist/go/apps"), "native snapshots remain inside the fresh ignored workspace");
  artifact(nativeMutant);
  intendedMutant(run("GO_MUTANT_RUN", nativeMutant, [], true), "Go");
  for (const [target, executable] of [["Bun", "bun"], ["Node", process.execPath]]) {
    const name = target.toLowerCase();
    strictConsumer(`${target}_PRODUCTION`, production);
    const result = run(`${target}_PRODUCTION_RUN`, executable, [path.join(leaf, "tests/counter-domain.mjs"), production, target]);
    assert.match(result.stdout, new RegExp(`^COUNTER_DOMAIN_TRACE_WRAP_GREEN=${target}$`, "m"));
    const source = path.join(work, `${name}-mutant.ef`);
    writeFileSync(source, mutant);
    assert.equal(fileHash(source), hash(mutant)); artifact(source);
    run(`${target}_MUTANT_CHECK`, compiler, ["check", source, "--target", "js"]);
    const output = path.join(work, `${name}-mutant.mjs`);
    run(`${target}_MUTANT_BUILD`, compiler, ["build", source, "--target", "js", "-o", output]);
    abi(output); strictConsumer(`${target}_MUTANT`, output);
    intendedMutant(run(`${target}_MUTANT_RUN`, executable, [path.join(leaf, "tests/counter-domain.mjs"), output, target], true), target);
  }
  assert.equal(fileHash(domainPath), hash(domain));
  assert.equal(fileHash(harnessPath), hash(harness));
  for (const input of evidence.compilerInputs) assert.equal(fileHash(path.join(repository, input.path)), input.sha256, "compiler input PREPOST");
  evidence.result = "DOMAIN_ABI_PRODUCTION_GREEN_AND_THREE_INTENDED_MUTANTS_RED";
  save();
  console.log("COUNTER_DOMAIN_CONTROLS_EXIT=0 (Go/Bun/Node production trace+wrap; three causal authored-domain mutants RED; no mounted Effra ports)");
} catch (error) {
  evidence.error = error instanceof Error ? error.stack : String(error); save(); throw error;
}
