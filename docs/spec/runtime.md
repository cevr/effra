# Runtime rules

The runtime gives Effect's guarantees on Go's scheduler: structured tasks, cooperative interruption, release that always completes, and typed exits. Go supplies goroutines, the garbage collector and preemption; Ergo adds no scheduler of its own. Definitions are in the [glossary](../../GLOSSARY.md).

Status summary: the prototype's runtime (`prototypes/sketch9/sketch/rt`) implements the task tree, stop propagation, the stop check before each capability call, `timeout.After` and defect propagation. Every other rule here is **designed, not yet implemented**.

## Tasks

Status: the task tree is **implemented in prototype**; `go`, `Task`, `Wait` and `Stop` in source are **designed, not yet implemented**.

1. **Tree.** Every task has a parent, except the task that runs `main`. Stopping a task stops every task below it. A child that finishes detaches from its parent.
2. **Starting.** `task.Go() { … }` starts a task and yields a `Task[T, E]`. `T` is the body's value and `E` its error row.
3. **Resource.** A `Task` is a resource ([resources.md](resources.md)). Its owner scope stops and drains it at scope end if it was not consumed. A `Task` can move by `return`, into a provider field, into another task or into a container ([resources.md](resources.md#moves)). A channel of tasks is an error in v0 (README Gap 30).
4. **Consuming.** `t.Wait()` waits for the task and returns `T` or fails with `E`; a defect of the task re-raises in the waiter. `t.Stop()` interrupts the task and waits for it to finish, including its releases. Each consumes the task.
5. **Containers.** Tasks may live in a slice or map; the container is then a resource ([resources.md](resources.md#containers)). Indexing lends a task, so `running[j].Wait()` is an error. `running.Take(j)` moves a task out of the map before its fallible `Wait`, so a drain cannot run it twice.
6. **Captures.** A `go` block may not assign captured locals. Values copy into a task. A resource is lent to the task under the structured-lend rule, and moved into it otherwise ([resources.md](resources.md#structured-lend)). Only synchronised handles are shared: channels, `sync.Ref`, `sync.TxRef` and actor handles. *Prior art:* Swift `Sendable`; Go shares memory freely and relies on the race detector.

*Prior art:* Go `errgroup` is function-scoped in the same way; Effect fibers are forked into a scope; Elixir `Task.async` links the task to its caller, and `Task.shutdown` keeps the abnormal exit, as `Stop` keeps a defect here.

## I1. Interruption points

Status: **implemented in prototype** for capability calls; **designed, not yet implemented** for the rest.

1. Interruption is cooperative. A stopped task stops at its next interruption point.
2. The compiler inserts an interruption point before each capability call, channel operation, `Wait` and `Sleep`, and at each loop back-edge, in every function whose footprint is not empty ([rows.md](rows.md#footprints)).
3. An interruption point is one atomic load. When the task is stopped, the function exits with interruption ([Exit causes](#exit-causes)).
4. A loop with an empty footprint has no interruption point. Such a loop cannot be stopped until it ends, as with Effect on JavaScript. Go preemption keeps the scheduler fair but does not stop the loop.
5. A blocking runtime primitive waits for either its event or the stop, so a stopped task never stays parked.

*Prior art:* the `select` on `ctx.Done()` Go programmers write by hand; Effect checks interruption between steps; BEAM processes are preempted and can be killed outright, which Ergo does not do.

## I2. Automatic masks

Status: **designed, not yet implemented**.

These run uninterruptibly, to completion:

1. a resource release and a generated or written `Close` ([resources.md](resources.md));
2. a `scope.OnExit` body;
3. wire cleanup at the end of a `with`;
4. the step between a successful acquisition and its registration for release.

If an acquisition succeeds, its release runs. A blocking primitive inside a masked region waits uninterruptibly, unless it is explicitly marked as an interruptible wait in the mask (the `poll` pattern). Masks nest; an inner region restores the outer mask state, never an unmasked one. *Prior art:* Cats Effect `MonadCancel.bracket` and `uncancelable` with `poll`; Effect `acquireRelease` and `uninterruptibleMask`.

## Exit causes

Status: **partly implemented in prototype**.

1. A task ends with one exit: a success value, or a cause. A cause holds one or more of: a typed failure from the error row, a defect, and an interruption.
2. **Interruption is not a typed error.** No error row names it, and every `catch` passes it on ([rows.md](rows.md#catch)). In a function with no error row, interruption still propagates as part of the exit, not through the row (README Gap 2). Prototype: such a function panics with `rt.Interrupted`, and the nearest task boundary recovers it.
3. **Defects.** A Go panic in Ergo code is a defect. A defect in a child re-raises in the parent at the join, on every path, including the timeout and stop paths. Prototype: implemented for `timeout.After`.
4. **Release defects.** A defect or an interruption during a release joins the cause beside the body's outcome. No cause is dropped.
5. **Stop wins.** When a task is stopped while its body runs, interruption wins over the body's success.
6. **Entry.** When `main`'s exit is a cause, the program prints each reason and exits with status 130 when every reason is an interruption, and 1 otherwise, as the current [entry failure report](../design.md#entry-failure-report) does. Prototype: an interruption that reaches `main` exits 130; a defect prints `defect: …` and exits 2.

## Exit actions

Status: **designed, not yet implemented**.

1. `scope.OnExit() { … }` registers its block on the innermost enclosing function. The block runs when that function returns, on every path, and the blocks one call registered run last registered first. This is Go's `defer`, exactly. Each block runs masked ([I2](#i2-automatic-masks)).
2. A function declaration, a function literal and the block of `task.Go` are functions here: an OnExit in a task's block runs when the task ends. A `with` block, an `if` or `match` arm and a loop body are not: an OnExit inside them registers on the enclosing function, and one inside a loop registers once per iteration. Whether the block of another call (`timeout.After`, `sync.Atomically`) counts as a function is not decided here.
3. Cleanup at the end of a `with` block is a resource's release, not an OnExit: a resource bound in the block is released when the block ends ([resources.md](resources.md)).
4. The block reads its variables when it runs. Go evaluates a `defer` call's arguments at the `defer` statement instead, so `scripts/migrate196` rewrites `defer f(x)` only when `f` is a function name, each argument is a literal or a local that nothing writes, and the `defer` stands at the top level of a function body; any other `defer` is a hand edit.

*Prior art:* Go's `defer` is function-scoped, and Ergo takes it because Go is the target, so the lowering and the translation are exact. Swift and Zig `defer` run at the end of the enclosing block.

## Timeouts

Status: **implemented in prototype** as a name-keyed exception (README Gap 31); **designed** as the library function below.

```go
let payment = try timeout.After(500ms) { Gateway.Authorize(order) }
```

**Classification.** `timeout.After` is an ordinary library function in the `timeout` package, not a construct. Under the owner's rules, constructs get keywords, a dot form is library, and kernel primitives are functions (owner, 2026-10-12). Its kernel primitives are task start (`task.Go`), `Task.Stop` and `Task.Wait`, and the `Scheduler` capability. Its designed signature, in the generic spelling of [types.md](types.md#generics):

```go
func After[T, E, R](d duration, body func() T fails E needs R) T fails E | Expired needs Scheduler, R {
	// designed: the body runs as a child task raced against the deadline
}
```

The callback type carries `needs R`, so the requirements of the block appear in `After`'s own row. A trailing block is the `body` argument ([grammar.md](grammar.md#expressions)). *Prior art:* Effect's `Effect.timeout` is an ordinary combinator over fibers and the clock; Go's `context.WithTimeout` is a library function.

1. `timeout.After(d) { block }` needs `import "timeout"`. It takes one `duration` (S108) and a block (S108). The duration is a value position, so a fallible duration is S105.
2. It charges `Scheduler` and the block's requirements ([rows.md](rows.md#capability-rows)), because its signature says so.
3. Its block is a function literal and so a failure target: a `try` in the block escapes into the `timeout.After` expression, not into the enclosing function. The expression's error row is `timeout.Expired`, plus the block's last expression's row, plus every error a `try` in the block escapes, which is `E | Expired` in the signature.
4. The block runs as a child task. If the deadline passes first, the child is stopped and drained, and the expression fails with `timeout.Expired`. The result comes only after the child has finished, so completed cleanup beats deadline punctuality (NORTH_STAR tiebreak).
5. If the parent is stopped first, or while the block runs, the child is stopped and drained, and the expression's exit is an interruption.
6. A defect in the child re-raises in the parent on every path.
7. The block ends with an expression, and it may not contain `return` ([grammar.md](grammar.md#expressions), trailing-block rule; S109 in the prototype). Write an explicit `func` literal to return early.
8. **Lowering.** The block is the only Go func literal the prototype emits: the child task's body.

*Prior art:* Effect `Effect.timeout`; Go `context.WithTimeout`, which cancels but does not wait for the work to stop; Elixir `Task.yield` followed by `Task.shutdown`.

## Observability and supervision

Status: **designed, not yet implemented** ([#69](https://github.com/cevr/ergo/issues/69), [#200](https://github.com/cevr/ergo/issues/200), [#207](https://github.com/cevr/ergo/issues/207)).

### G1. Context through `with`

1. `with` takes context values as well as wires: `with trace.Span("ingest"), log.Annotate("region", "eu") { … }`.
2. A context value is immutable. Every task started inside the block inherits it. A restarted task inherits the values its factory captured.
3. *Prior art:* Java `ScopedValue`; Effect `References`; Go `context.WithValue`, which has to be threaded by hand.

### G2. Fork-tree defects

1. Each task records its parent, its span and its start site.
2. A defect prints the chain of tasks and spans that led to it, from the failing task to the root:

   ```text
   defect: index out of range [3] with length 3
     at handle (ingest.ergo:31)
     task #7 handle, started by tasks.Set #2 at main (ingest.ergo:24)
       span ingest  region=eu  peer=10.0.0.4
     task #1 main
   ```

3. Ancestry retention is bounded.

### G3. `ergo ps <pid>`

1. `ergo ps <pid>` shows the live task tree of a running program: each task's state, what it waits on, its span and its restarts.
2. It is served over a debug endpoint, for people and agents, with the same schema in CLI and MCP.
3. Spans are keyed to checked definition identities ([#188](https://github.com/cevr/ergo/issues/188)), so a trace line links to the source definition and to `ergo inspect`.

### G4. `tasks.Set`

1. `tasks.Set(policy)` owns an unbounded number of tasks and drops finished ones. It is a resource: the block that acquired it owns it, or the scope it moved to, such as a provider ([resources.md](resources.md) R4); its owner scope releases it when that scope ends (R5).
2. The policy is fail-fast, `tasks.LogFailures`, or restart with a `schedule`: `tasks.Restart(schedule.Exponential(1s).UpTo(5))`.
3. `Set.Go` takes a `func` literal, because the Set keeps it after `Go` returns. The literal escapes, so it may capture a resource only under the structured-lend rule: the Set must be released before the borrow of the resource ends ([resources.md](resources.md#closures)). In `set.Go(func() { actor.Pump(b, signals) })`, when `set` and `b` are bound in the same scope, `set` must be declared after `b`. Other owner arrangements follow R27 and R31: for example, a `set` bound inside a `with` block nested in `b`'s scope is released first and qualifies. In every arrangement, `b.Stop()` while `set` may still run the literal is an error.
4. A restart policy needs a reusable factory that captures no moved resource. A connection moved into its handler cannot restart, so such a Set uses `tasks.LogFailures`.
5. *Prior art:* Erlang/OTP `one_for_one` supervisors restart from the child specification; Effect `FiberSet` and `Supervisor`; Go has neither.

## Channels

Status: **designed, not yet implemented** ([#64](https://github.com/cevr/ergo/issues/64)).

1. A channel is the library type `queue.Queue[T]`: `queue.New[T](capacity)` makes one, `q.Push(v)` sends, `q.Pop()` receives, `for v in q.Values()` drains it and `q.Close()` closes it. `chan` is a reserved word, and there are no nil queues and no direction types.
2. Values copy into a channel. A channel whose element type is a resource is an error in v0 ([resources.md](resources.md#channels); README Gap 30).
3. `Push` and `Pop` are interruption points.
4. A send on a closed channel interrupts the sender instead of panicking. This is Ergo's own policy, not Effect parity: Effect's `Queue.offer` returns `false` after shutdown.
5. `q.Pop()` returns `Option[T]` when `T` is not defaultable ([types.md](types.md#zero-values-and-definite-initialisation)).
6. The capacity, close, cancellation and drain semantics of `queue.Queue` belong to [#207](https://github.com/cevr/ergo/issues/207); until it settles them, the rules above are a design, not a contract.

## Machines and actors

Status: **designed, not yet implemented** ([#57](https://github.com/cevr/ergo/issues/57)).

```go
type State = Closed | Open | HalfOpen
type Signal = Failure | Success | Retry

func step(s State, sig Signal) Decision[State, string] fails ProbeDown needs Probe {
	match (s, sig) {
	case (Closed, Failure):
		return Enter{Open{sig.Endpoint}}
	case (HalfOpen, Success):
		return Enter{Closed{0}}
	case (_, _):
		return Ignore{}
	}
}

type Breaker machine[State, Signal] {
	initial: Closed{0},
	step:    step,
}
```

1. **Machine.** `type X machine[S, E] { initial v; step f }` declares a state machine. `S` and `E` are unions. `initial` is a value of `S`; `step` names a function from `S` and `E` to `Decision[S, O]`, with any error and `needs` rows.
2. **Decision.** A step returns `Enter{next}` (re-enter, even with the same variant), `Stay{next}` (keep the entry), `Ignore{}`, `Reject{}` (observable, state unchanged) or `Done{output}` (terminal).
3. **Totality.** The step's switch is total over `S × E` (README Gap 12).
4. **Inspection.** `ergo inspect` draws the machine's graph from the step's cases. A helper the step calls is static in the graph only when its footprint is empty, not its row ([rows.md](rows.md#footprints)).
5. **Protocol.** A machine is a plan that any lawful provider can run; the actor below is the standard library's provider. *Prior art:* XState machines with interpreters; Erlang `gen_statem`.
6. **Actors are library code** (owner, 2026-10-27: "Let's see if we can make actor a library mechanism"; [#101](https://github.com/cevr/ergo/issues/101); [research note](../research/actor-library.md)). The `actor` package is ordinary code over generics, unions, function types with rows, resources and tasks. No compiler rule names it, and it has no syntax.
   - `actor.Start(initial, capacity, handle)` runs `handle func(S, M) actor.Next[S] fails E needs R` over a mailbox of `capacity` messages and returns an `actor.Actor[M, E]`. It charges `R`, which the actor captures. A handler failure ends the actor.
   - A message that wants an answer carries an `actor.Reply[T, E]`. `actor.Ask(to, request)` builds the message around a fresh reply and returns `T` or fails with `E | actor.Stopped | actor.NoReply`. `T` and `E` come from `request`'s parameter type, so a wrong reply type is a type error.
   - `Reply[T, E]` is a resource. `Send` and `Fail` consume it. Releasing it unanswered answers `NoReply` while the actor runs, and `Stopped` once its stop is requested. An answer nobody can receive, because the asker was stopped and detached, is released exactly once (README Gap 37); an answer or failure the library releases is released masked, by its kind, and a refusal releases nothing. A successful result discarded by stop-wins after the handoff (an answer taken just before the asker's task stops, a callback result discarded by a scope's stop) needs README Gap 47.
   - A message, a reply, an agent result and a state snapshot cross a task boundary, so each is a copy that shares no mutable storage with the sender's value (Tasks rule 6; README Gap 45). A resource moves.
   - `Send` waits only while the mailbox is full. Messages are handled in admission order.
   - `Stop` closes admission, then interrupts the actor and waits until it ends. After the stop is requested, no `Send` or `Ask` is admitted: each fails with `actor.Stopped`. Queued requests are answered `Stopped`, every queued message and then the state is released, and every release runs even when an earlier one panics. It returns the typed failure that ended the actor, if any, and every defect in order.
   - `actor.StartMachine(m, capacity)` runs a machine value `m Machine[S, E, O, F, R]` (README Gap 46) with `Send`, `Ask`, `State` and `Stop`. `Ask` returns the step's `Decision[S, O]` or the step's failure. `State` returns a snapshot, never a shared reference. A step failure goes to the asker; a step failure after a `Send` ends the actor, because it has no other observer.
   - `actor.StartAgent(initial, capacity)` and `actor.Call(a, f)` serve calls on a state value. The protocol is the functions whose first parameter is `*S`. `f func(r *S) T fails E` borrows the state for one call, and the borrow cannot escape ([types.md](types.md#borrows) Borrows rules 1 and 4; R17, R30, R33). `f` has an empty `needs` row, which is not purity: `f` runs on the actor's task, so a wait inside it is an interruption point (I1) that `Stop` interrupts. A typed failure of `f` is the caller's; an interruption or a defect ends the agent, and the agent releases its state.
   - `actor.Run` runs a machine under a `tasks.Set` for supervision; a restart starts from `initial`.
7. **Owner and address (resolved 2026-10-27).** `actor.Actor[M, E]` holds the actor's task, so it is a resource (R1, R14): its owner scope stops it at scope end, and `Stop` consumes it. `actor.Address[M]`, from `a.Address()`, holds only synchronised handles, so it copies into tasks and is not a resource. After the actor ends, `Send` and `Ask` on an address fail with `actor.Stopped`. The rules these items need from the checker and the linearity pass are README Gaps 35 to 47.

*Prior art:* Erlang/OTP `gen_server` and `gen_statem`; Gleam OTP `process.call` with a typed reply subject in the message, and Akka Typed `ask` with `replyTo`, which `actor.Ask` follows; Tokio's `oneshot::Sender`, whose drop answers the receiver as a released `Reply` does; Elixir `Agent` for `actor.Call`; Effect cluster entities. Swift and Pony give actors syntax to check shared mutable state; Ergo's actor state is a value owned by the actor's task, so it needs none.

## Host and lowering notes

These notes are not normative; they record the prototype's lowering, which the main compiler may change while keeping the rules above.

- A function takes a hidden environment pointer (one field per capability) and the task handle.
- Each interruption point is `if t.Stopped() { return zero, rt.Interrupted }`, or `t.Check()` in a function with no error row.
- `main` defers `rt.Recover`, which turns an interruption into exit 130.
- Source positions are carried with `//line` directives, so Go panics, pprof, delve and the race detector point at Ergo lines.
- *Prior art:* Ergo's earlier Go emitter built a closure per effect step and measured a 47.8 s `go build` for 1,024 sequential operations; direct calls rebuilt in 2.08 s in the same measurement. Gleam source maps and ReScript's readable output follow the same "erase to idiomatic host code" goal.
