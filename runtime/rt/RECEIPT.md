# runtime/rt receipt

Package `rt` is the kernel runtime that emitted sketch code links against (#198). It implements the runtime rules of `docs/spec/runtime.md` (I1, I2, G1, G2) and the release rules of `docs/spec/resources.md` (R5, R7, R8, R9, R27). It has no Effect runtime code and does not import the prototype. The start point was `prototypes/sketch9/sketch/rt`. That package's task tree, `After` and review tests were rewritten here, not copied as they were.

## Design notes

- **Stop flag.** The stop flag is an `atomic.Bool`. The prototype closed a channel and polled it with `select`. The poll is now `stopped.Load() && mask == 0`: one atomic load while no stop is requested. The stop channel exists only for blocking waits. `Done` makes it on first use, so a task that never blocks never allocates one. The benchmark below decided this: the atomic poll costs 1.4–1.6 ns. A `select` on a channel, the prototype's poll, costs 5.2–5.5 ns.
- **Masks.** Only the task's own goroutine changes or reads the mask depth, so the depth is a plain `uint32` and not part of the atomic word. The first version packed the depth and the flag into one `atomic.Uint64`. Measured with the entry layout before the change below, Acquire+release cost 44.7–49.5 ns with the packed word and 41.9–45.2 ns with the split field (three runs each). The split costs nothing and removes four read-modify-writes from each acquire and release. `Mask.Interruptible` is the poll pattern. Regions nest.
- **Child interruptibility.** `Go` starts a child interruptible, even inside a masked region such as an `Acquire`. This follows the pinned Effect 4 default, where fork is interruptible unless asked to inherit. `GoMasked` is the explicit inherit mode. Its body runs inside one masked region and receives that region as `restore`. `restore.Interruptible` opens an interruptible sub-region, as in ZIO's `uninterruptibleMask` and restore and in Effect's restore. A stop that the masked body never observes does not replace its result. A parent inside a masked region has not observed its own stop. A child that parent starts there therefore starts live: for example, a timeout inside a release keeps its deadline. The owner scope stops the child at scope end. Children that already exist when the stop arrives are stopped at once, as in a Go `context` tree.
- **One boundary.** `Run` is the task boundary. Every child started by `Go` runs its body through `Run`, and emitted `main` calls `Run` for the root (`os.Exit(rt.ExitCode(err))`). A later law adapter (#124) can use the same entry. `Run` and `Scope.End` share one finalization step (`settle`): a recovered panic becomes a defect, and a stop the task can observe beats success (Exit causes rule 5). This applies to empty scopes too, and to a stop that arrives during a release, which is observed when the release mask closes.
- **Exit accumulator.** `Cause` keeps reasons in order: the body's typed failure, each release defect (`InRelease`), and at most one interruption. Errors are canonical. A lone typed failure stays itself, a lone interruption is `Interrupted`, and anything else is `*Cause`. `Failure` gives a catch only a lone typed failure (rows.md catch rule 6, law R6).
- **Release.** `Scope.End` is deferred directly, so it can recover the body's panic. It releases in reverse order, masked. Each release recovers its own panic as a defect, so a later release still runs (R9). `Acquire` runs `open` and the registration in one masked step (R8).
- **Allocation-free registration.** A scope keeps four entries inline. An entry is a releaser and its resource. A releaser is a struct with at most one func field, which Go stores in an interface without allocating. The first version stored `closeWith[R]` as a func value. A func value of a generic instantiation binds its dictionary in a 16-byte heap closure, so that version measured 90–113 ns and 1 alloc/op. The releaser layout measures 29–31 ns and 0 allocs/op.
- **Tasks.** A `Handle` embeds its `Task`, so a start allocates the handle, its done channel and the goroutine. Children form an intrusive sibling list under the parent's mutex, so linking a child allocates nothing (the prototype used a map). `Interrupt` takes locks ancestor first. `detach` takes only the parent's lock, so a stop walk cannot deadlock with a finishing child.
- **Bounded ancestry (G2 rule 3).** The parent pointer is a live cancellation link only. `detach` drops it when the task finishes, and a handle drops its owner when it is consumed. A finished task therefore retains no ancestors. Diagnostic ancestry is a copy: a defect records its chain as at most 32 value frames while the task still runs. A test builds a 129-deep chain that has finished, then checks with weak pointers that the leaf keeps no ancestor reachable.
- **Wait.** `Wait` checks the owner's stop before the completion fast path, and again after it wakes. If the owner is stopped, `Wait` returns the task's defects and then the interruption, even when the task has already finished.
- **After's unwind boundary.** Every exit of `After` passes one deferred boundary, including a panic in the `Scheduler` or `Timer` provider. The boundary stops and drains a child that is still running, stops the timer as a release, and adds each cleanup defect after the exit it already holds.
- **Normalization (laws R3, R4, R6, R8).** `Stop`, an interrupted `Wait` and an expired `After` keep the stopped task's defects. They drop its interruption and an abandoned typed failure, so a lone `Expired` stays a catchable typed failure (runtime.md Timeouts rule 4). The spec phrase "body cause plus `Expired`" is read this way: the defects of the body's cause survive, then `Expired`.
- **Law suite seam (#124).** Each R1–R15 law that applies to this layer can be driven from outside the package. A test can see the task identity and chain (`ID`, `Span`, `Chain`), every reason of an exit (`Cause.Reasons`), the stop and mask state (`StopRequested`, `Masked`) and the timers (`Virtual.AwaitTimers`, `Pending`, `Advance`), and it enters through `Run`.
- **Known gaps, carried to #124 and the observability work.**
  - A completed, unobserved child's typed failure is dropped at normal scope close. R3 needs a finer classification.
  - Virtual time fires due timers in deadline order, ties in arming order, but does not wait for managed continuations to quiesce (R14).
  - A defect records no start site (G2 rule 1); it records only the task and span chain.

## Prior art

- Go `context`: the cancellation tree. Effra keeps the downward stop and adds a drain: `Stop` waits for the work and its releases.
- Effect `Fiber`, `Scope` and `Cause`: the guarantees. Release runs once on every exit. Interruption is not a typed error. A cause keeps every reason. In pinned Effect 4, fork is interruptible by default and inherits only on request.
- ZIO `uninterruptibleMask` with restore: the explicit masked start (`GoMasked`) and its restore.
- Trio nurseries and Swift task groups: structured lend. A scope stops and drains its tasks before it releases the resources it acquired earlier.
- Erlang/OTP links: defect propagation. A child's panic reaches the joiner with the chain of task and span IDs.
- Tokio cooperative budget: cooperative interruption at the points the compiler inserts.
- Cats Effect `MonadCancel` and `uncancelable`/`poll`: masks and the poll pattern.

## Measured costs

Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64, `GOMAXPROCS=16`. The box was shared with other agents (load average 2.4–3.0 during the run), so read the minimum as the best estimate. Command: `go test -run '^$' -bench . -benchmem -count 6 ./runtime/rt/`. The raw output is in the unit directory as `bench-r2.txt` (round 1: `bench-r1.txt`).

| Operation | rt (min / median ns/op) | allocs/op, B/op | Hand-written Go baseline | Baseline (min / median ns/op) | allocs/op, B/op |
| --- | --- | --- | --- | --- | --- |
| Poll on a running task | 1.390 / 1.484 | 0, 0 | `select` on `ctx.Done()` | 5.214 / 5.367 | 0, 0 |
| | | | `ctx.Err() != nil` | 2.472 / 2.552 | 0, 0 |
| Spawn + wait of an empty task | 736.0 / 804.1 | 3, 272 | `context.WithCancel` + `sync.WaitGroup` + `go` | 717.2 / 788.5 | 5, 152 |
| Acquire + release of a no-op resource | 29.10 / 29.37 | 0, 0 | `defer close(r)` | 4.150 / 4.339 | 0, 0 |

Reading the numbers (measured unless labeled):

- The poll is cheaper than both context idioms, because it is one load with no channel or mutex.
- Spawn and wait cost about the same as the baseline: the goroutine start dominates. rt makes fewer allocations but more bytes, because the handle carries the task, the value, the exit and the done channel.
- Acquire and release cost about 25 ns more than a bare `defer`. The extra work is the masked region around `open`, the inline release stack, one `recover` per release (so that a panic in `Close` becomes a defect), and `End`'s `recover` and stop-wins check. A round-1 CPU profile attributed 23% of the samples to `NewScope` zeroing and copying the 168-byte scope, and 20% to `runRelease` with its `recover`.
- Round 2 against round 1, by minimum: Acquire+release went from 27.61 to 29.10 ns (+1.5 ns), which matches the added stop-flag load and `settle` in `End`. The poll code did not change, so its move from 1.656 to 1.390 ns is noise on a shared box. Spawn+wait gained the deferred `Run` boundary and an owner field; its move from 786.1 to 736.0 ns is within the same noise, and its allocations are unchanged (3, 272 B).
- Inferred: an emitted function with an empty scope pays only the `End` fast path: a `recover`, a length check and one stop-flag load.
