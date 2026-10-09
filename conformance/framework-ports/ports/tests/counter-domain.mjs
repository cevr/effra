import assert from "node:assert/strict";
import { pathToFileURL } from "node:url";

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
assert.equal(domain.counterCount(state), 0n, "CounterInitial: expected 0");
console.log("COUNTER_INITIAL_ZERO=0");
state = domain.updateCounter(state, domain.clickedIncrement());
assert.equal(domain.counterCount(state), 1n, "ClickedIncrement: expected 1");
state = domain.updateCounter(state, domain.clickedDecrement());
assert.equal(domain.counterCount(state), 0n, "ClickedDecrement: expected 0");
state = domain.updateCounter(state, domain.clickedDecrement());
assert.equal(domain.counterCount(state), -1n, "ClickedDecrement: expected -1");
state = domain.updateCounter(state, domain.clickedReset());
assert.equal(domain.counterCount(state), 0n, "ClickedReset: expected 0 from -1");
state = domain.updateCounter(state, domain.clickedIncrement());
assert.equal(domain.counterCount(state), 1n, "ClickedIncrement: expected 1 again");
state = domain.updateCounter(state, domain.clickedReset());
assert.equal(domain.counterCount(state), 0n, "ClickedReset: expected 0 from 1");
assert.equal(domain.counterCount(domain.updateCounter(domain.counterState(9223372036854775807n), domain.clickedIncrement())), -9223372036854775808n, "I64Increment: max wraps to min");
assert.equal(domain.counterCount(domain.updateCounter(domain.counterState(-9223372036854775808n), domain.clickedDecrement())), 9223372036854775807n, "I64Decrement: min wraps to max");
console.log(`COUNTER_DOMAIN_TRACE_WRAP_GREEN=${target}`);
