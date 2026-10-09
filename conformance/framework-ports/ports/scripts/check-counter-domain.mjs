import assert from "node:assert/strict";
import { createHash } from "node:crypto";
import { copyFileSync, existsSync, lstatSync, mkdirSync, mkdtempSync, readFileSync, realpathSync, renameSync, rmSync, writeFileSync } from "node:fs";
import path from "node:path";
import { createRequire } from "node:module";
import { fileURLToPath } from "node:url";
import { spawnSync } from "node:child_process";
import { parseArgs } from "node:util";

// The control has two halves. Emission runs the compiler: it checks and builds the authored domain, the native
// harness and the increment mutant into an artifact directory. Verification never runs the compiler: it proves the
// emitted JavaScript, declarations and native programs against the leaf's TypeScript, Bun and Node. With no options
// the control builds a fresh current-source compiler and runs both halves. --emit DIR runs only emission into DIR,
// with --compiler naming a compiler built from the current source; --verify DIR runs only verification of DIR. The
// repository gate emits with the bin/ef it built in the same run and keys verification on the emitted bytes, so a
// compiler change that leaves every artifact identical does not repeat verification.
const options = parseArgs({ options: { compiler: { type: "string" }, emit: { type: "string" }, verify: { type: "string" } }, strict: true }).values;
assert.ok(!(options.emit && options.verify), "--emit and --verify are separate halves");
assert.ok(!(options.compiler && options.verify), "verification never runs a compiler");
const leaf = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..");
const repository = path.resolve(leaf, "../../..");
const generated = path.join(leaf, ".generated");
mkdirSync(generated, { recursive: true });
const work = mkdtempSync(path.join(generated, "counter-domain-"));
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
// Both harnesses print COUNTER_CHECK=<label> before each check. The native entry reports an unhandled failure by its
// tag alone; the host harness reports its failure as one structured COUNTER_FAILURE JSON line.
const nativeChecks = ["CounterInitial: expected 0", "ClickedIncrement: expected 1", "ClickedDecrement: expected 0", "ClickedDecrement: expected -1", "ClickedReset: expected 0 from -1", "ClickedIncrement: expected 1 again", "ClickedReset: expected 0 from 1", "I64Increment: max wraps to min", "I64Decrement: min wraps to max"];
const intendedCheck = "ClickedIncrement: expected 1";
const checksThrough = label => nativeChecks.slice(0, nativeChecks.indexOf(label) + 1);
const intendedLabels = checksThrough(intendedCheck);
const checkedLabels = stdout => [...stdout.matchAll(/^COUNTER_CHECK=(.*)$/gm)].map(match => match[1]);
const failureLines = stdout => [...stdout.matchAll(/^COUNTER_FAILURE=(.*)$/gm)].map(match => match[1]);
const lastLine = stdout => stdout.trimEnd().split("\n").at(-1);
// The mutant turns the first increment into 0 - 1, so the intended check observes -1 where 1 is expected.
const intendedObservation = { actual: "-1", expected: "1" };
function classifyMutant(record, target) {
  assert.ok(Number.isInteger(record.exit) && record.exit !== 0, `${target} mutant needs a real nonzero exit`);
  assert.equal(record.signal ?? null, null, `${target} mutant cannot be satisfied by a signal`);
  assert.match(record.stdout, /^COUNTER_INITIAL_ZERO=0$/m, `${target} mutant reaches successful initialization`);
  assert.deepEqual(checkedLabels(record.stdout), intendedLabels, `${target} mutant stops at the intended labelled check`);
  assert.doesNotMatch(record.stdout, /COUNTER_(?:NATIVE|DOMAIN)_TRACE_WRAP_GREEN/);
  if (target === "Go") {
    assert.deepEqual(failureLines(record.stdout), [], "native harness has no host failure line");
    assert.equal(lastLine(record.stdout), `COUNTER_CHECK=${intendedCheck}`, "native output ends at the intended check");
    assert.equal(record.stderr, "AssertionFailed\n", "native public Assert failure identity and no other cause");
    return { target, setupAndInitialZeroReached: true, intendedAssertion: intendedCheck, failure: "AssertionFailed", actualMutantExit: record.exit };
  }
  assert.match(record.stdout, new RegExp(`^COUNTER_SETUP_OK=${target}$`, "m"), "actual public module import/setup succeeded");
  const lines = failureLines(record.stdout);
  assert.equal(lines.length, 1, `${target} mutant reports exactly one structured failure`);
  assert.equal(lastLine(record.stdout), `COUNTER_FAILURE=${lines[0]}`, "the failure is the final output");
  const failure = JSON.parse(lines[0]);
  assert.equal(failure.name, "AssertionError", "host assertion failure, not a setup error");
  assert.equal(failure.code, "ERR_ASSERTION");
  assert.equal(failure.message, intendedCheck, "failure message is exactly the intended label");
  assert.deepEqual(failure.observed, intendedObservation, "intended check observed the mutant value");
  assert.equal(record.stderr, "", `${target} mutant has no uncaught host error`);
  return { target, setupAndInitialZeroReached: true, intendedAssertion: intendedCheck, failure, actualMutantExit: record.exit };
}
// In-memory negative controls: the classifier must accept the intended failure and reject near misses.
function classifierControls() {
  const labelLines = labels => labels.flatMap(label => label === nativeChecks[0] ? [`COUNTER_CHECK=${label}`, "COUNTER_INITIAL_ZERO=0"] : [`COUNTER_CHECK=${label}`]);
  const lines = items => items.length ? `${items.join("\n")}\n` : "";
  const hostOutput = (target, labels, failure) => lines([`COUNTER_SETUP_OK=${target}`, ...labelLines(labels), ...(failure ? [`COUNTER_FAILURE=${JSON.stringify(failure)}`] : [])]);
  const assertion = (message, actual, expected) => ({ name: "AssertionError", code: "ERR_ASSERTION", message, observed: { actual, expected } });
  const again = "ClickedIncrement: expected 1 again";
  const decrement = "ClickedDecrement: expected 0";
  const excerpt = `check("${intendedCheck}", domain.counterCount(state), 1n);\nerror: ${decrement}\n`;
  const cases = [];
  for (const target of ["Bun", "Node"]) {
    cases.push(
      [target, "intended", true, { exit: 1, stdout: hostOutput(target, intendedLabels, assertion(intendedCheck, "-1", "1")), stderr: "" }],
      [target, "later again-label failure", false, { exit: 1, stdout: hostOutput(target, checksThrough(again), assertion(again, "2", "1")), stderr: "" }],
      [target, "again message at the intended position", false, { exit: 1, stdout: hostOutput(target, intendedLabels, assertion(again, "-1", "1")), stderr: "" }],
      [target, "decrement failure with increment excerpt", false, { exit: 1, stdout: hostOutput(target, checksThrough(decrement), assertion(decrement, "1", "0")), stderr: excerpt }],
      [target, "setup failure", false, { exit: 1, stdout: lines([`COUNTER_FAILURE=${JSON.stringify({ name: "Error", code: "ERR_MODULE_NOT_FOUND", message: `Cannot find module; ${intendedCheck}`, observed: null })}`]), stderr: "" }],
      [target, "build failure", false, { exit: 1, stdout: "", stderr: `EF102 near ${intendedCheck}\n` }],
      [target, "missing failure line", false, { exit: 1, stdout: hostOutput(target, intendedLabels, null), stderr: "" }],
      [target, "zero exit", false, { exit: 0, stdout: hostOutput(target, intendedLabels, assertion(intendedCheck, "-1", "1")), stderr: "" }],
    );
  }
  cases.push(
    ["Go", "intended", true, { exit: 1, stdout: lines(labelLines(intendedLabels)), stderr: "AssertionFailed\n" }],
    ["Go", "later again-label failure", false, { exit: 1, stdout: lines(labelLines(checksThrough(again))), stderr: "AssertionFailed\n" }],
    ["Go", "decrement failure", false, { exit: 1, stdout: lines(labelLines(checksThrough(decrement))), stderr: "AssertionFailed\n" }],
    ["Go", "build failure", false, { exit: 1, stdout: "", stderr: `EF106 near ${intendedCheck}\n` }],
    ["Go", "missing failure", false, { exit: 1, stdout: lines(labelLines(intendedLabels)), stderr: "" }],
    ["Go", "additional cause", false, { exit: 1, stdout: lines(labelLines(intendedLabels)), stderr: "AssertionFailed; defect: cleanup\n" }],
  );
  return cases.map(([target, name, accepted, record]) => {
    let rejection = null;
    try { classifyMutant({ signal: null, ...record }, target); } catch (error) { rejection = error instanceof Error ? error.message : String(error); }
    assert.equal(rejection === null, accepted, `classifier control ${target} "${name}" must be ${accepted ? "accepted" : "rejected"}${rejection ? `: ${rejection}` : ""}`);
    return { target, name, expected: accepted ? "accepted" : "rejected", rejection };
  });
}
const mode = options.emit ? "emit" : options.verify ? "verify" : "complete";
// Emission records stay outside the artifact directory: compiler reports carry timings, and the directory's bytes are
// verification's cache key.
const evidence = { schema: "effra-counter-domain-controls-v2", mode, claim: "Domain/ABI controls only; all 170 Effra coverage rows pending", work, source: { path: domainPath, sha256: fileHash(domainPath) }, harness: { path: harnessPath, sha256: fileHash(harnessPath) }, mutation: { original: mutation, replacement: mutation.replace("+ 1", "- 1"), productionSha256: hash(domain), mutantSha256: hash(mutant) }, records, artifacts: [], nativePublicationNamespace: { buildCwd: work, snapshots: path.join(work, "dist/go/apps"), owner: "cmd/ef/main.go:690 cwd-relative publication" } };
const receipt = path.join(generated, mode === "emit" ? "counter-domain-emit-evidence.json" : "counter-domain-evidence.json");
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
function intendedMutant(record, target) {
  record.causalIdentity = classifyMutant(record, target);
  save();
}

// The emitted artifact directory: every file the compiler produced or was given, and a manifest naming the authored
// bytes they came from. Its contents are deterministic, so identical compiler behavior yields identical bytes.
const emitted = {
  production: "counter.mjs",
  productionNative: "production-native.ef",
  native: "counter-go",
  mutantNative: "mutant-native.ef",
  nativeMutant: "counter-go-mutant",
  hostMutant: target => `${target.toLowerCase()}-mutant`,
};
const hosts = [["Bun", "bun"], ["Node", process.execPath]];
const nativeSourceText = source => `${source}\n${harness}`;
function manifest() {
  return { schema: "effra-counter-domain-artifacts-v1", domainSha256: hash(domain), harnessSha256: hash(harness), mutantSha256: hash(mutant), nativePrefixBytes: { production: Buffer.byteLength(domain), mutant: Buffer.byteLength(mutant) } };
}
function nativeSource(dir, name, source) {
  const filename = path.join(dir, name);
  const combined = nativeSourceText(source);
  writeFileSync(filename, combined);
  assert.equal(readFileSync(filename).subarray(0, Buffer.byteLength(source)).toString(), source, "native harness retains exact domain prefix bytes");
  evidence.artifacts.push({ path: filename, sha256: fileHash(filename), domainPrefixBytes: Buffer.byteLength(source), domainPrefixSha256: hash(source), assertionSuffixSha256: hash(`\n${harness}`) });
  return filename;
}
function snapshotsStayInWork() {
  assert.equal(realpathSync(evidence.nativePublicationNamespace.snapshots), path.join(realpathSync(work), "dist/go/apps"), "native snapshots remain inside the fresh ignored workspace");
}

function emit(compiler, dir) {
  rmSync(dir, { recursive: true, force: true });
  mkdirSync(dir, { recursive: true });
  run("SOURCE_GO_CHECK", compiler, ["check", domainPath, "--target", "go"]);
  run("SOURCE_JS_CHECK", compiler, ["check", domainPath, "--target", "js"]);
  const production = path.join(dir, emitted.production);
  run("LIBRARY_BUILD", compiler, ["build", domainPath, "--target", "js", "-o", production]);
  artifact(production); artifact(production.replace(/\.mjs$/, ".d.mts"));
  for (const [source, name, binary, label] of [[domain, emitted.productionNative, emitted.native, "PRODUCTION"], [mutant, emitted.mutantNative, emitted.nativeMutant, "MUTANT"]]) {
    const native = nativeSource(dir, name, source);
    run(`GO_${label}_CHECK`, compiler, ["check", native, "--target", "go"]);
    run(`GO_${label}_BUILD`, compiler, ["build", native, "--target", "go", "-o", path.join(dir, binary)], false, work);
    snapshotsStayInWork();
    artifact(path.join(dir, binary));
  }
  for (const [target] of hosts) {
    const name = emitted.hostMutant(target);
    const source = path.join(dir, `${name}.ef`);
    writeFileSync(source, mutant);
    assert.equal(fileHash(source), hash(mutant)); artifact(source);
    run(`${target}_MUTANT_CHECK`, compiler, ["check", source, "--target", "js"]);
    const output = path.join(dir, `${name}.mjs`);
    run(`${target}_MUTANT_BUILD`, compiler, ["build", source, "--target", "js", "-o", output]);
    artifact(output); artifact(output.replace(/\.mjs$/, ".d.mts"));
  }
  writeFileSync(path.join(dir, "manifest.json"), `${JSON.stringify(manifest(), null, 2)}\n`);
  save();
}

function verify(dir) {
  // The artifacts must come from the current authored domain and harness.
  assert.deepEqual(JSON.parse(readFileSync(path.join(dir, "manifest.json"), "utf8")), manifest(), "emitted artifacts belong to the current domain, harness and mutant");
  assert.equal(readFileSync(path.join(dir, emitted.productionNative), "utf8"), nativeSourceText(domain), "emitted production native source is the current domain and harness");
  assert.equal(readFileSync(path.join(dir, emitted.mutantNative), "utf8"), nativeSourceText(mutant), "emitted mutant native source is the current mutant and harness");
  for (const [target] of hosts) assert.equal(readFileSync(path.join(dir, `${emitted.hostMutant(target)}.ef`), "utf8"), mutant, `emitted ${target} mutant source is the current mutant`);
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
  // The consumers import the canonical path, so the fresh emission is installed there.
  for (const [from, to] of [[path.join(dir, emitted.production), production], [path.join(dir, emitted.production.replace(/\.mjs$/, ".d.mts")), production.replace(/\.mjs$/, ".d.mts")]]) {
    copyFileSync(from, to);
    assert.equal(fileHash(to), fileHash(from), "canonical output is the emitted artifact");
  }
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
  const native = path.join(dir, emitted.native);
  artifact(path.join(dir, emitted.productionNative)); artifact(native);
  const go = run("GO_PRODUCTION_RUN", native, []);
  assert.match(go.stdout, /^COUNTER_INITIAL_ZERO=0$/m);
  assert.deepEqual(checkedLabels(go.stdout), nativeChecks, "Go production runs every labelled check in order");
  assert.match(go.stdout, /^COUNTER_NATIVE_TRACE_WRAP_GREEN$/m);
  const nativeMutant = path.join(dir, emitted.nativeMutant);
  artifact(path.join(dir, emitted.mutantNative)); artifact(nativeMutant);
  intendedMutant(run("GO_MUTANT_RUN", nativeMutant, [], true), "Go");
  for (const [target, executable] of hosts) {
    strictConsumer(`${target}_PRODUCTION`, production);
    const result = run(`${target}_PRODUCTION_RUN`, executable, [path.join(leaf, "tests/counter-domain.mjs"), production, target]);
    assert.deepEqual(checkedLabels(result.stdout), nativeChecks, `${target} production runs every labelled check in order`);
    assert.deepEqual(failureLines(result.stdout), [], `${target} production reports no failure`);
    assert.equal(result.stderr, "", `${target} production has no host error output`);
    assert.match(result.stdout, new RegExp(`^COUNTER_DOMAIN_TRACE_WRAP_GREEN=${target}$`, "m"));
    const output = path.join(dir, `${emitted.hostMutant(target)}.mjs`);
    abi(output); strictConsumer(`${target}_MUTANT`, output);
    intendedMutant(run(`${target}_MUTANT_RUN`, executable, [path.join(leaf, "tests/counter-domain.mjs"), output, target], true), target);
  }
}

try {
  evidence.classifierControls = classifierControls(); save();
  assert.equal(process.versions.node, "24.11.1", "selected actual Node version");
  if (mode !== "emit") {
    const tsPackage = JSON.parse(readFileSync(path.join(leaf, "node_modules/typescript/package.json"), "utf8"));
    assert.equal(tsPackage.version, "6.0.3");
    assert.ok(realpathSync(localTS).startsWith(`${path.join(leaf, "node_modules")}/`), "leaf TypeScript owner, no root fallback");
    assert.equal(run("BUN_VERSION", "bun", ["--version"]).stdout.trim(), "1.4.2");
    assert.equal(run("LEAF_TS_VERSION", process.execPath, [localTS, "--version"]).stdout.trim(), "Version 6.0.3");
  }
  evidence.parent = { head: run("SOURCE_HEAD", "git", ["rev-parse", "HEAD"], false, repository).stdout.trim(), headTree: run("SOURCE_TREE", "git", ["rev-parse", "HEAD^{tree}"], false, repository).stdout.trim() };
  let compiler = null;
  if (mode !== "verify") {
    const files = run("COMPILER_INPUTS", "git", ["ls-files", "-z", "--", "cmd", "internal", "go.mod", "go.sum"], false, repository).stdout.split("\0").filter(Boolean);
    evidence.compilerInputs = files.map(file => ({ path: file, sha256: fileHash(path.join(repository, file)) }));
    evidence.compilerInputsSha256 = hash(JSON.stringify(evidence.compilerInputs));
    if (options.compiler) {
      // The caller built this compiler from the current source in the same run; the input hashes above and the
      // PREPOST check below bind the run to that source.
      compiler = path.resolve(options.compiler);
      evidence.compiler = { provided: options.compiler, path: compiler, sha256: fileHash(compiler) };
    } else {
      compiler = path.join(work, "ef");
      run("COMPILER_BUILD", "go", ["build", "-trimpath", "-mod=readonly", "-o", compiler, "./cmd/ef"], false, repository);
      artifact(compiler);
    }
  }
  const dir = path.resolve(options.emit ?? options.verify ?? path.join(work, "emitted"));
  evidence.emitted = dir;
  if (mode !== "verify") emit(compiler, dir);
  if (mode !== "emit") verify(dir);
  assert.equal(fileHash(domainPath), hash(domain));
  assert.equal(fileHash(harnessPath), hash(harness));
  if (evidence.compilerInputs) for (const input of evidence.compilerInputs) assert.equal(fileHash(path.join(repository, input.path)), input.sha256, "compiler input PREPOST");
  if (mode === "emit") {
    evidence.result = "DOMAIN_HARNESS_AND_MUTANTS_CHECKED_AND_EMITTED";
    save();
    console.log(`COUNTER_DOMAIN_EMIT_EXIT=0 (checked and emitted the domain, native harness and three mutants into ${dir})`);
  } else {
    evidence.result = "DOMAIN_ABI_PRODUCTION_GREEN_AND_THREE_INTENDED_MUTANTS_RED";
    save();
    console.log("COUNTER_DOMAIN_CONTROLS_EXIT=0 (Go/Bun/Node production trace+wrap; three causal authored-domain mutants RED; no mounted Effra ports)");
  }
} catch (error) {
  evidence.error = error instanceof Error ? error.stack : String(error); save(); throw error;
}
