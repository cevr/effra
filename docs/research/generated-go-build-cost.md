# Long effect-function build diagnostic

Measured on 2026-10-06 from the clean scheduler branch at `620e1455077f2d15b71d25f1d6256cc44ad2a49f`, before its pending continuation-isolation repair. This is a shared-host diagnostic with an ambient Go cache, not an admitted compile-speed comparison or a cold-build claim.

The retained source is the exact `TestSchedulerDrainsLongManagedContinuationAcrossTargets` fixture: sleep for 20 milliseconds, 1024 sequential latch signals, then sleep for 30 milliseconds, executed by the causal test harness. The public CLI generated and built the native test executable. A wrapper around the actual Go command separated backend build time from the independently repeated executable run.

| Stage | Observed wall time | Result |
| --- | ---: | --- |
| Go backend `go build -trimpath` | 47.754 seconds | Exit 0 |
| Complete public `ef test` command | 47.843 seconds | Test passed |
| Retained native test executable, independent run | 3.894 milliseconds | Test passed |

Go was 1.27.0/linux-amd64. Host load averages were approximately 6.06/5.42/6.23 at start and 5.75/5.51/6.21 at end. No CPU admission, isolation or comparative scoring is claimed. The observation establishes that this run's long delay occurs in the backend build, rather than in execution of the retained test. It does not by itself distinguish compiler lowering, Go optimization, cache state and shared-host contention.

Raw artifacts are in `/tmp/effra-scheduler-build-diagnostic/`: `probe.py`, the `go` timing wrapper, `long.ef`, `ef`, generated `dist/go/`, `dist/long.tests`, `backend-build.jsonl` and `receipt.json`. The receipt includes exact commands, stdout/stderr, source/toolchain identity and hashes of every generated file. The compiler metadata records the clean source revision above.

- Compiler SHA-256: `c828f7f15fff2b99f8c5d274fca439a9255b9343c946281ade41c30345b4fa06`.
- Native test SHA-256: `02d94cf8f4cca5e72ccfb92e4999f3a003c39a8f42f94e781806ba33c515bf18`.

This complements the implementation owner's earlier unretained build/run diagnostic; it does not retroactively supply its missing artifact. The measured fixture uses the old branch's `throws` spelling. Future integrated runs use separately identified `raises` source, preserving this receipt unchanged.

Before a compile-speed claim, reproduce with explicit cache/host admission, compare long straight-line bodies with many small functions, and retain frontend/emission/backend stage receipts. Profile the backend before changing lowering. Preserve cancellation checkpoints and source semantics; a smaller test or a longer watchdog is not a performance repair.
