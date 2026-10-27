# runtime/rt receipt

Package `rt` is the kernel runtime that emitted sketch code links against (#198). It implements the runtime rules of `docs/spec/runtime.md` (I1, I2, G1, G2) and the release rules of `docs/spec/resources.md` (R5, R7, R8, R9, R27). It has no Effect runtime code and does not import the prototype. The start point was `prototypes/sketch9/sketch/rt`. That package's task tree, `After` and review tests were rewritten here, not copied as they were.

## Design notes

- **Stop flag.** The stop flag is an `atomic.Bool`. The prototype closed a channel and polled it with `select`. The poll is now `stopped.Load() && mask == 0`: one atomic load while no stop is requested. The stop channel exists only for blocking waits. `Done` makes it on first use, so a task that never blocks never allocates one. The benchmark below decided this: the atomic poll costs 1.7–2.1 ns. A `select` on a channel, the prototype's poll, costs 4.9–7.2 ns.
- **Masks.** Only the task's own goroutine changes or reads the mask depth, so the depth is a plain `uint32` and not part of the atomic word. The first version packed the depth and the flag into one `atomic.Uint64`. Measured with the entry layout before the change below, Acquire+release cost 44.7–49.5 ns with the packed word and 41.9–45.2 ns with the split field (three runs each). The split costs nothing and removes four read-modify-writes from each acquire and release. `Mask.Interruptible` is the poll pattern. Regions nest. A child that starts in a masked region inherits the mask (ZIO and Effect inherit runtime flags), so a release that starts a task, or runs `After`, still completes. A stop that a task can never observe does not turn its success into an interruption.
- **Exit accumulator.** `Cause` keeps reasons in order: the body's typed failure, each release defect (`InRelease`), and at most one interruption. Errors are canonical. A lone typed failure stays itself, a lone interruption is `Interrupted`, and anything else is `*Cause`. `Failure` gives a catch only a lone typed failure (rows.md catch rule 6, law R6).
- **Release.** `Scope.End` is deferred directly, so it can recover the body's panic. It releases in reverse order, masked. Each release recovers its own panic as a defect, so a later release still runs (R9). `Acquire` runs `open` and the registration in one masked step (R8).
- **Allocation-free registration.** A scope keeps four entries inline. An entry is a releaser and its resource. A releaser is a struct with at most one func field, which Go stores in an interface without allocating. The first version stored `closeWith[R]` as a func value. A func value of a generic instantiation binds its dictionary in a 16-byte heap closure, so that version measured 90–113 ns and 1 alloc/op. The releaser layout measures 28–32 ns and 0 allocs/op.
- **Tasks.** A `Handle` embeds its `Task`, so a start allocates the handle, its done channel and the goroutine. Children form an intrusive sibling list under the parent's mutex, so linking a child allocates nothing (the prototype used a map). `Interrupt` takes locks ancestor first. `detach` takes only the parent's lock, so a stop walk cannot deadlock with a finishing child.
- **Normalization (laws R3, R4, R6, R8).** `Stop`, an interrupted `Wait` and an expired `After` keep the stopped task's defects. They drop its interruption and an abandoned typed failure, so a lone `Expired` stays a catchable typed failure (runtime.md Timeouts rule 4). The spec phrase "body cause plus `Expired`" is read this way: the defects of the body's cause survive, then `Expired`.
- **Law suite seam (#124).** Each R1–R15 law that applies to this layer can be driven from outside the package. A test can see the task identity and chain (`ID`, `Span`, `Chain`), every reason of an exit (`Cause.Reasons`), the stop and mask state (`StopRequested`, `Masked`) and the timers (`Virtual.AwaitTimers`, `Pending`, `Advance`). Virtual time fires due timers in deadline order, ties in arming order. It does not wait for managed continuations to quiesce (R14). That is left to the law unit.

## Prior art

- Go `context`: the cancellation tree. Effra keeps the downward stop and adds a drain: `Stop` waits for the work and its releases.
- Effect `Fiber`, `Scope` and `Cause`: the guarantees. Release runs once on every exit. Interruption is not a typed error. A cause keeps every reason.
- Trio nurseries and Swift task groups: structured lend. A scope stops and drains its tasks before it releases the resources it acquired earlier.
- Erlang/OTP links: defect propagation. A child's panic reaches the joiner with the chain of task and span IDs.
- Tokio cooperative budget: cooperative interruption at the points the compiler inserts.
- Cats Effect `MonadCancel` and `uncancelable`/`poll`, and ZIO's inherited interruptibility: masks.

## Measured costs

Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64, `GOMAXPROCS=16`. The box was shared with other agents (load average 2.5–3.0 during the run), so read the minimum as the best estimate. Command: `go test -run '^$' -bench . -benchmem -count 6 ./runtime/rt/`. The raw output is in the unit directory as `bench-r1.txt`.

| Operation | rt (min / median ns/op) | allocs/op, B/op | Hand-written Go baseline | Baseline (min / median ns/op) | allocs/op, B/op |
| --- | --- | --- | --- | --- | --- |
| Poll on a running task | 1.656 / 1.845 | 0, 0 | `select` on `ctx.Done()` | 4.940 / 5.883 | 0, 0 |
| | | | `ctx.Err() != nil` | 2.690 / 3.218 | 0, 0 |
| Spawn + wait of an empty task | 786.1 / 842.5 | 3, 272 | `context.WithCancel` + `sync.WaitGroup` + `go` | 787.6 / 842.1 | 5, 152 |
| Acquire + release of a no-op resource | 27.61 / 28.33 | 0, 0 | `defer close(r)` | 4.310 / 4.609 | 0, 0 |

Reading the numbers (measured unless labeled):

- The poll is cheaper than both context idioms, because it is one load with no channel or mutex.
- Spawn and wait cost the same as the baseline: the goroutine start dominates. rt makes fewer allocations but more bytes, because the handle carries the task, the value, the exit and the done channel.
- Acquire and release cost about 23 ns more than a bare `defer`. The extra work is the masked region around `open`, the inline release stack, one `recover` per release, so that a panic in `Close` becomes a defect, and the `recover` in `End`. CPU profile: `NewScope` zeroing and copying the 168-byte scope is 23% of the samples, and `runRelease` with its `recover` is 20%. Inferred: an emitted function with an empty scope pays only the `End` fast path (`recover` and a length check), because `End` returns early when nothing was registered.
