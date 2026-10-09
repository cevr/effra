import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

// Mirrors fixtures/counter-native.ef: each check prints its label first, and any failure is
// reported as one structured COUNTER_FAILURE line so the runner can identify it exactly.
// assert.ok keeps the AssertionError message exactly equal to the label; the observed values travel separately.
let observed = null;
function check(label, actual, expected) {
  console.log(`COUNTER_CHECK=${label}`);
  observed = { actual: String(actual), expected: String(expected) };
  assert.ok(actual === expected, label);
  observed = null;
}

try {
  const [modulePath, target] = process.argv.slice(2);
  assert.ok(modulePath && ["Node", "Bun"].includes(target), "explicit generated module and target required");
  assert.equal(Boolean(process.versions.bun), target === "Bun", "actual runtime target");
  const domain = await import(pathToFileURL(modulePath).href);
  const domainExports = ["initialCounter", "counterState", "clickedDecrement", "clickedIncrement", "clickedReset", "updateCounter", "counterCount"];
  // Actual librarySurface roots builtin service tags and supported JS providers.
  // builtins.go:16-51,92-107; reachability.go:479-498; emit.go:223-247;
  // js_prelude.go:54-61 and emit.go:125 select exactly these provider values.
  const builtinServiceExports = ["Assert", "Console", "Clock", "Scheduler", "Sync", "Files", "Env", "Runtime", "Foreign"];
  const builtinProviderExports = ["Assertions", "Stdout", "LiveClock", "TestClock", "LiveScheduler", "TestScheduler", "TestSync", "LiveEnv"];
  const expectedPublicExports = [...domainExports, ...builtinServiceExports, ...builtinProviderExports];
  assert.deepEqual(Object.keys(domain).sort(), expectedPublicExports.sort(), "complete exact public library ABI, including standing builtin surface");
  console.log(`COUNTER_SETUP_OK=${target}`);
  let state = domain.initialCounter();
  check("CounterInitial: expected 0", domain.counterCount(state), 0n);
  console.log("COUNTER_INITIAL_ZERO=0");
  state = domain.updateCounter(state, domain.clickedIncrement());
  check("ClickedIncrement: expected 1", domain.counterCount(state), 1n);
  state = domain.updateCounter(state, domain.clickedDecrement());
  check("ClickedDecrement: expected 0", domain.counterCount(state), 0n);
  state = domain.updateCounter(state, domain.clickedDecrement());
  check("ClickedDecrement: expected -1", domain.counterCount(state), -1n);
  state = domain.updateCounter(state, domain.clickedReset());
  check("ClickedReset: expected 0 from -1", domain.counterCount(state), 0n);
  state = domain.updateCounter(state, domain.clickedIncrement());
  check("ClickedIncrement: expected 1 again", domain.counterCount(state), 1n);
  state = domain.updateCounter(state, domain.clickedReset());
  check("ClickedReset: expected 0 from 1", domain.counterCount(state), 0n);
  check("I64Increment: max wraps to min", domain.counterCount(domain.updateCounter(domain.counterState(9223372036854775807n), domain.clickedIncrement())), -9223372036854775808n);
  check("I64Decrement: min wraps to max", domain.counterCount(domain.updateCounter(domain.counterState(-9223372036854775808n), domain.clickedDecrement())), 9223372036854775807n);
  console.log(`COUNTER_DOMAIN_TRACE_WRAP_GREEN=${target}`);
} catch (error) {
  const failure = error instanceof Error ? { name: error.name, code: error.code ?? null, message: error.message, observed } : { name: typeof error, code: null, message: String(error), observed };
  console.log(`COUNTER_FAILURE=${JSON.stringify(failure)}`);
  process.exitCode = 1;
}
