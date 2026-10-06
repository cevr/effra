# Testing with explicit effects

`ef test FILE [--target go|js] [--live] [--timeout-ms 30000]` discovers top-level `test_` functions in source order. Each must be an effect with no parameters returning `()`. `Assert` is implicit; `Clock`, `Scheduler` and `Sync` are explicit capability rows that receive fresh deterministic fixtures when the test declares them. The test file does not need `main`.

```rust
effect fn test_greeting() -> () raises {AssertionFailed, Missing} uses {Assert} {
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

The default mode conservatively rejects file-level native host features and live clock/scheduler/environment providers. A declared `.timeout(ms)` uses the case's deterministic `TestScheduler` in default mode. `--live` permits explicit `LiveScheduler`, `LiveClock` and other live providers, but does not replace the default fixtures; a test must provision a live provider explicitly to use wall-clock time. Custom fixture providers remain ordinary checked provision. This is a capability policy over the whole file, including unused functions, not an OS sandbox or an analysis of only reachable test code. A pure imported Go function also needs live opt-in because the conservative import boundary does not prove purity.

The runner's real wall-clock watchdog defaults to 30 seconds for the suite process, after compilation. It forcibly stops the runner on expiry and reports `watchdogExpired: true`, `cleanupCompleted: false`. It does not claim to terminate unmanaged descendants or complete finalizers. The watchdog is a harness limit, distinct from a managed language timeout that waits for owned shutdown.

## Causal and time fixtures

`Sync.latch()` creates an opaque one-shot handle. `Sync.signal(latch)` is idempotent and `Sync.await(latch)` can be cancelled for one waiter without completing the shared latch. Use it for readiness and completion instead of fixed wall-clock waits.

`Clock.sleep(ms)` and `.timeout(ms)` share the case's scheduler-backed virtual time. The timeout driver is the `Scheduler.sleep(ms)` capability; `Scheduler.awaitRegistration()` is a readiness barrier that observes a timer after it has parked, and `Scheduler.advance(ms)` performs the strong adjustment on both targets: it flushes admitted managed work, selects and fires intermediate deadlines, and commits its target only when no runnable managed continuation or reserved wake can register earlier work. Cleanup parked on a registered virtual timer or managed signal may remain pending during a partial adjustment; the next adjustment or signal can finish it. Unmanaged goroutines and foreign blocking calls are not observable by the fixture and may delay completion. A provider that implements only `Clock.sleep` does not control timeout deadlines; timeout's explicit `Scheduler` row is the authority.

The real wall-clock watchdog remains independent of these fixtures. Provider substitution does not by itself provide network denial, scratch-directory isolation, process reaping or crash recovery.
