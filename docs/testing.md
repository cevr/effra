# Testing with explicit effects

`ef test FILE [--target go|js] [--live] [--timeout-ms 30000]` discovers top-level `test_` functions in source order. Each must be an effect with no parameters returning `()`. Its only implicit service is `Assert`; other services must be supplied explicitly in source. The test file does not need `main`.

```rust
effect fn test_greeting() -> () throws {AssertionFailed, Missing} uses {Assert} {
    let actual = run greeting("42").provide<Directory>(FixtureDirectory)
    run Assert.equalText(actual, "Hello, Ada")
}
```

[testing.ef](../examples/testing.ef) defines the service/fixture and tests success, typed recovery and an owned child on both targets.

## Supplied guarantees

Each case runs serially with a fresh owning root scope. A normal result is reported after child shutdown and resource cleanup. Failure, defect and interruption reasons survive that boundary; assertion reasons include actual/expected values. A failed case does not stop later cases. Existing runtime conformance tests cover unobserved child failures and cleanup defects.

`Assert.check(bool, message)` and `Assert.equalText(actual, expected)` are lazy, typed operations admitting `AssertionFailed`. `Assertions` is their portable provider, also usable explicitly outside tests. Assertions are currently primitive; source spans identify checked source, but assertion results do not yet carry a precise failing-call span.

The CLI emits a JSON suite receipt with revision, target, per-case status/causes and watchdog state. User output is captured separately; at most 1 MiB of each output stream's tail is retained, with a truncation flag. The read-only MCP `project.tests` exposes contracts and whether live opt-in is required; it does not execute source.

## Host/time boundaries

The default mode conservatively rejects file-level native host features, live clock/environment providers and timeout operators. `--live` opts into them. Custom fixture providers remain ordinary checked provision. This is a capability policy over the whole file, including unused functions, not an OS sandbox or an analysis of only reachable test code. A pure imported Go function also needs live opt-in because the conservative import boundary does not prove purity.

The runner's real wall-clock watchdog defaults to 30 seconds for the suite process, after compilation. It forcibly stops the runner on expiry and reports `watchdogExpired: true`, `cleanupCompleted: false`. It does not claim to terminate unmanaged descendants or complete finalizers. The watchdog is a harness limit, distinct from a managed language timeout that waits for owned shutdown.

## What DI does not supply

Provider substitution does not by itself provide deterministic scheduling, network denial, scratch-directory isolation, process reaping or crash recovery. There is no virtual test clock yet. A no-op sleep provider would not control deadlines or scheduling and is not offered as one.

Next standard facilities: causal `Latch`/`Deferred`, a scheduler-backed test clock controlling sleeps and deadlines, scoped temporary directories, per-case console capture, deterministic randomness, and managed process fixtures with kill escalation and exit receipts. Use actual readiness/completion signals when testing concurrency; fixed waits are weak evidence.
