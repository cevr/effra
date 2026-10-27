# Actors as an ordinary library (#101)

**Status:** spike record for [#101](https://github.com/cevr/effra/issues/101), round 1, 2026-10-27. Base `main` at `48daf248`. It changes no compiler behaviour. The examples are in [actor-library/examples](actor-library/examples/), labelled **Proposed**. The Go lowering is [`prototypes/actorlib`](../../prototypes/actorlib/doc.go).

**Owner direction, 2026-10-27, on #101:** "Let's see if we can make actor a library mechanism."

## Verdict

**Route 1 holds for every part. The std `actor` package is ordinary library code.** It needs no compiler projection, no compiler-known `Actor` identity and no actor syntax. Route 2 is not needed for any guarantee. It would buy only brevity for handler actors, measured on the same programs: route 1 takes 2.65x to 3.38x the tokens of a hypothetical actor syntax for the counter and the roster, and 1.06x for the machine-backed breaker (table in Q5). The library also costs more than a hand-written Go actor for `Ask` (1.15x) and for start and stop (1.52x), measured in Q8.

The evidence is in three classes. Each claim below carries one of these labels:

- **Measured (Go lowering).** Go's own type checker or a `-race` test in `prototypes/actorlib` shows it. Typed Ask is in this class: a wrong reply type does not compile (Q1).
- **Designed (awaits #196/#197).** A spec rule gives it in checked source. Nine of those rules are not in `docs/spec` yet. They are general rules, and each serves clients other than actors: Gaps A1 to A10 below. Without them the guarantee falls back to its runtime form.
- **Runtime.** The library code checks it at run time, and a test shows it.

Three findings change the plan:

1. **The reply address is the message's business, not the compiler's.** `Ask(to Address[M], request func(Reply[T, E]) M) (T, error[E | Stopped | NoReply])` takes `T` and `E` from the request's parameter type. This is Gleam's `process.call` and Akka Typed's `ask`.
2. **Reply exactly once needs no new linearity rule.** `Reply[T, E]` is an ordinary resource. Its `Close` answers the caller. Automatic release (resources.md R5) gives "at least once, or the caller gets a cause", and use after consume (R20) gives "at most once". The cost is five resource rules that #197 must state in general form (A1 to A5). Tokio's `oneshot::Sender` works the same way: its `Drop` completes the channel.
3. **A protocol that is a set of methods needs no message union.** An agent (Elixir `Agent`) sends calls on its state. The reply is the call's return value, so it is answered once by construction, with no resource rule at all.

## Library API

The full package is [actor.go.sketch](actor-library/examples/actor.go.sketch). It uses the provisional generic spelling of [types.md](../spec/types.md#generics) (#135).

| Item | Signature (sketch) | Kind |
| --- | --- | --- |
| Reply address | `type Reply[T, E] struct{…}`; `Send(v T)`, `Fail(e E)`, `Close()` | resource (has `Close`) |
| Copyable half | `type Address[M] struct{…}`; `Send(m M) error[Stopped]` | plain value (fields are channels) |
| Owner handle | `type Actor[M, E] struct{…}`; `Address()`, `Send`, `Stop() error[E]` | resource (holds a `Task`) |
| Start | `func Start(initial S, capacity int64, handle func(S, M) (Next[S], error[E]) uses R) Actor[M, E] uses R` | function |
| Ask | `func Ask(to Address[M], request func(Reply[T, E]) M) (T, error[E \| Stopped \| NoReply])` | function |
| Handler decision | `Next[S]`, `Continue(s)`, `End(s)` | plain value |
| Machine actor | `func StartMachine(m machine[S, E], capacity int64) Machine[S, E, O, F] uses R`; `Send`, `Ask`, `State`, `Stop` | library over `Start` |
| Agent | `func StartAgent(initial S, capacity int64) Agent[S]`; `func Call(a Agent[S], f func(S) (S, T, error[E])) (T, error[E \| Stopped \| NoReply])` | library over `Start` |

Failures, each a typed failure in a row, never a defect:

- `Stopped`: the actor refused or discarded the request because it was stopping or had ended.
- `NoReply`: the actor dequeued the request and released its reply unanswered while it ran. A handler that drops the reply, or a handler defect while it holds the reply, gives `NoReply`.

A handler's own typed failure ends the actor, and `Stop` returns it. A per-request failure goes to the asker through `Reply.Fail`. Each failure therefore has exactly one observer: the asker or the owner. A machine step's failure goes to the asker when there is one (`Ask`); a `Send` has no asker, so the step failure ends the actor.

## Answers

### Q1. Typed Ask without projection

**Yes. Measured.** The message carries its typed reply address. `Ask` takes a function from reply address to message, so `T` and `E` come from that function's parameter type. No projection reads a protocol.

```go
type Get struct{ Reply actor.Reply[int64] }

func Count(c actor.Address[CounterMsg]) (int64, error[actor.Stopped | actor.NoReply]) {
	return actor.Ask(c, func(r actor.Reply[int64]) CounterMsg { return Get{r} })
}
```

The Go lowering is `actorlib.Ask[M, T any, E error]` ([actor.go:178](../../prototypes/actorlib/actor.go)). `TestReplyTypingIsCheckedByGo` ([compile_test.go:17](../../prototypes/actorlib/compile_test.go)) adds each testdata file to the examples package through a `go build -overlay`. It requires the exact Go type error, and a control file must compile:

| Negative case | Go's refusal |
| --- | --- |
| [wrong_reply_type.go](../../prototypes/actorlib/testdata/wrong_reply_type.go): a request built around `Reply[string, Never]` for `Get` | `cannot use r (variable of struct type actorlib.Reply[string, actorlib.Never]) as actorlib.Reply[int64, actorlib.Never] value in struct literal` |
| [wrong_reply_sent.go](../../prototypes/actorlib/testdata/wrong_reply_sent.go): a handler answers `Get` with a string | `cannot use "ten" (untyped string constant) as int64 value in argument to m.Reply.Send` |
| [wrong_reply_failure.go](../../prototypes/actorlib/testdata/wrong_reply_failure.go): a handler fails `Get`, whose row is empty, with `RoomFull` | `cannot use RoomFull{…} (value of struct type RoomFull) as actorlib.Never value in argument to m.Reply.Fail` |

Limit: Go checks one failure type per reply. A row with several types is checked by the Effra checker (#196); the lowering erases it to Go's `error`.

Methods cannot introduce type parameters in Go, so `Ask` is a function, not a method on `Address`. Gleam makes the same choice (`process.call`).

### Q2. Reply exactly once

**Designed as a general resource rule, with a runtime fallback that is measured.** `Reply[T, E]` has `Close`, so it is a resource (R1). No actor rule is added.

| Property | Mechanism | Class |
| --- | --- | --- |
| At most once | `Send` and `Fail` consume the reply; a second answer is a use after consume (R20) | Designed (A1, A4) |
| At most once, lowering | the slot has capacity one and the first answer wins: [actor.go:94](../../prototypes/actorlib/actor.go) | Runtime: `TestFirstAnswerWins` |
| At least once, or a cause | a dropped reply is released at its owner's scope end (R5); its `Close` answers `NoReply`, or `Stopped` while the actor stops | Designed (A1 to A5, A8) |
| At least once, lowering | the handler's emitted release: `TestDroppedReplyAnswersNoReply`, `TestHandlerDefectAnswersNoReplyThenStopped`, `TestStopAnswersInFlightAndQueuedStopped` | Runtime |
| Detached caller | the slot is buffered, so a late answer never blocks the actor | Runtime: `TestInterruptedAskerDetaches` |

The runtime does not raise a defect for a dropped reply. Dropping is legal under automatic release: Effra resources are affine with a release, not linear. The caller gets `NoReply` as a typed failure. A rule that forbade the drop would need a "must consume" marker, which is a new linearity mode with no second client yet.

For a handler to answer a reply that arrived in a message, these rules must exist. Each is general:

- **A1. Consuming parameters and receivers.** A method whose body consumes its receiver (`Reply.Send` ends with `r.Close()`) consumes it at the call. R19 lists `Close`, `Task.Wait`, `Task.Stop` and an actor's `Stop` by name; the general rule replaces that list. *Prior art:* Rust by-value `self`; Swift `consuming` methods.
- **A2. Resource unions.** A union with a resource variant is a resource, and a type switch moves the variant out.
- **A3. Channels of resources.** README Gap 30's recommended rule: a send moves the value, the buffer owns values in flight, and releasing the channel's last owner releases them. `Address.Send` moves the message; on failure Send's own scope releases it.
- **A4. Consuming a resource field.** `m.Reply.Send(v)` consumes one field of an owned message. R39 forbids moving a field out; consuming one in place is a relaxation, and the generated `Close` (R3) then skips that field. *Prior art:* Rust partial moves.
- **A5. Owned parameters in function types.** The handler `func(S, M)` must own `m` to consume the reply inside it. A function type has no body to infer from, so it spells the mode. *Prior art:* Swift `(consuming T) -> U`.

### Q3. Rows

**Yes, by ordinary row polymorphism. Designed (#196); the lowering shows the capture.**

- `Start(…, handle func(S, M) (Next[S], error[E]) uses R) Actor[M, E] uses R`. The handler's requirements `R` are captured at `Start` and charged there, the constructor-capture rule of README Gap 17. Lowering: the breaker's step captures its `Probe` environment where the machine value is built ([breaker.go:111](../../prototypes/actorlib/examples/breaker.go)).
- The handler's failure row `E` becomes the owner handle's row: `Actor[M, E].Stop() error[E]`, as `Task[T, E]` carries its body's row.
- `Ask`'s row is `E | Stopped | NoReply`, with `E` from the request's reply type. `Send`'s row is `Stopped`. `Send` waits while the mailbox is full (runtime.md item 6), so there is no `Full`. A non-waiting `TrySend` with `error[Full]` is a later addition.
- Footprints rule 5 already keeps an actor whose handler uses a `with`-bound capability inside that `with`: the actor is a resource the block owns.

Row features that `docs/spec/rows.md` and `types.md` lack:

- **A6. Generic types with row parameters,** and a trailing row argument that is omitted means the empty row: `Reply[int64]` is `Reply[int64, (empty)]`. types.md shows generics only on functions, and its elision rule covers only rows that pass through.
- **A7. Failures as values.** The library carries a typed failure from the handler's task to the asker's task through a channel. That needs a value form of an outcome: a prelude `Exit[T, E]` with `Succeed`, `Fail` and `Get() (T, error[E])`. `Task.Wait` does this inside the kernel today. *Prior art:* Effect `Exit`; Gleam `Result`. Without A7, a closure `func() (T, error[E])` returning the failure works under the current rules, at a closure per reply.

### Q4. Machine-backed actors

**Yes: plain generics over the machine value. Measured for behaviour; the signature is designed.** `StartMachine(m machine[S, E], capacity)` reads `m.initial` and calls `m.step`. The step's output `O`, failure row `F` and requirements `R` pass through. `Ask` returns the step's `Decision[S, O]` (runtime.md item 2), and `State` returns a snapshot. The actor library depends on `machine` only as a value with `initial` and `step`, so any lawful provider of that shape runs (runtime.md item 5).

It is written on top of `Start`, which shows that the core composes: the events travel in a private envelope with an optional reply ([machine.go:79](../../prototypes/actorlib/machine.go)). `TestBreakerMachineActor` drives the runtime.md breaker: three failures open it, a probe failure goes to the asker and leaves the state unchanged, `try b.Ask(Retry{}) catch { case ProbeDown: Reject{} }` yields `Reject`, a working probe moves it to `HalfOpen`, and `Retire` returns `Done{"retired"}` and ends the actor. `TestBreakerSendFailureEndsTheActor` shows that a step failure with no asker ends the actor, and `Stop` returns `ProbeDown`.

Design input for the machine spelling: `StartMachine` needs `machine[S, E]` to be a type whose values expose `initial` and `step`, with `O`, `F` and `R` inferred from `step`. It needs nothing else.

### Q5. Handler actors

**Yes, without projection, in two shapes. The cost is source length, measured below.**

- **Message union (data protocol).** The user writes the union with a `Reply` field per answered variant, a handler with a type switch, and one client function per request ([roster.go.sketch](actor-library/examples/roster.go.sketch)). The protocol is data: `ef inspect` can show it from ordinary types, and a later durable or distributed provider can encode it.
- **Agent (method protocol).** A protocol defined as methods on a state type is served with no union and no reply address: `actor.Call(room, func(r Room) (Room, int64, error[RoomFull]) { return r.Join(name) })` ([roster-agent.go.sketch](actor-library/examples/roster-agent.go.sketch)). The reply is the return value. The cost: a call is code, not data, so it cannot be logged, inspected, encoded or replayed.

An interface's methods cannot become messages without a projection or a code generator. Both are route 2, so this spike does not use them.

Agent rule: `Call`'s `f` has an empty `uses` row. A caller that is stopped while it waits detaches. Its call may still run after the caller's `with` has released the providers that `f` would use. So an agent call is a pure state transition, and effects belong in handler actors, whose requirements are captured at `Start`.

Measured size of the same programs, `go run ./prototypes/actorlib/cmd/sketchsize docs/research/actor-library/examples/*.go.sketch` (code lines exclude blanks and comment-only lines; tokens are Go scanner tokens without comments and inserted semicolons):

| Program | Route 1, message union | Route 1, agent | Route 2 (hypothetical syntax) | Route 1 / route 2, tokens |
| --- | --- | --- | --- | --- |
| Counter | 22 lines, 199 tokens | — | 11 lines, 75 tokens | 2.65x |
| Roster (3 requests, 3 reply types, 1 failure) | 49 lines, 439 tokens | 33 lines, 345 tokens | 22 lines, 130 tokens | 3.38x (union), 2.65x (agent) |
| Breaker (machine-backed) | 53 lines, 406 tokens | — | 52 lines, 383 tokens | 1.06x |

Reading the numbers:

- For a machine, the two routes are the same size. The machine declaration carries the protocol, so actor syntax saves only `actor.StartMachine(Breaker{}, 16)` against `spawn Breaker{} cap 16`.
- For a handler actor, route 2 is much shorter. Route 1 spends its tokens on the message union, the reply fields, the spelled parameter types of each request function, and the spelled rows of each exported client function (rows.md, Capability rows rule 6).
- Inferred, not measured: two general features would recover part of the gap without actor syntax. One is function literals whose parameter types come from the parameter they are passed to (Go proposal 21498). The other is a named error row such as `type Asked = actor.Stopped | actor.NoReply`. Both serve every higher-order library. They are recorded here as ergonomic follow-ups, not gaps, because no guarantee depends on them.

### Q6. Address split

**Yes: ordinary types plus resources. Designed; measured in the lowering.** This resolves runtime.md item 7.

- `Actor[M, E]` holds the actor's `Task`, so it is a resource by R1 and R14 with no actor-specific rule. Its owner scope stops it at scope end, and `Stop` consumes it. Lowering: `Start` registers the task's handle with the owner scope ([actor.go:230](../../prototypes/actorlib/actor.go)); `TestOwnerScopeStopsTheActorAndReleasesItsState`.
- `Address[M]` holds only channels and the shared control. It has no `Close`, so it is not a resource. It copies into tasks under runtime.md Tasks rule 6, and it does not keep the actor alive.
- **Send to a stopped actor fails with `Stopped`**, a typed failure. A sender that is waiting on a full mailbox wakes with `Stopped`. `Ask` gets `Stopped` before admission and for a queued request discarded by the stop. Tests: `TestStopAnswersInFlightAndQueuedStopped`, `TestBlockedSenderWakesOnStopAndReleasesItsMessage`, `TestEndReleasesStateAndRefusesLaterSends`. *Prior art:* Erlang's send to a dead pid succeeds silently and Akka sends to dead letters; Effect's `Queue.offer` returns `false` after shutdown. Effra takes Effect's choice and types it.
- Admission is fenced ([actor.go:139](../../prototypes/actorlib/actor.go)). A sender holds a shared lock while it admits, and the stop takes it once, exclusively, before the drain. So no message is admitted after the drain, and every admitted message is released once. `TestEveryMessageReleasedOnceUnderConcurrentStop` admits 1,600 counted messages from 8 tasks while the actor stops. It checks one release for each message, whether handled, drained or refused, under `-race -count=5`.

Runtime.md Tasks rule 6 names "actor handles" among the shared handles. Under route 1 the rule can be structural: a value whose fields are all synchronised handles is shared by copy (A10).

### Q7. Message rules

**Already general. No named constraint.**

- A send moves a resource: A1 plus A3. A message with no resource copies, as every value copies into a task (runtime.md Tasks rule 6).
- A borrowed value cannot be sent: R17 (a lent resource must not be sent) and types.md Borrows rule 1 (a borrow is never sent). These rules hold for channels, queues (#64) and tasks alike.
- Value semantics make a message a snapshot (types.md Structs rule 6). The lowering copies where Go would alias: the roster's `Members` reply clones the slice, and `TestRosterRepliesOfSeveralTypes` mutates the returned slice and checks that the room keeps its names. The cost of copying `[]T` and `map[K]V` in messages is a general value-semantics cost for #81 and #211, not an actor cost.

### Q8. Cost

**Measured.** Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64, `GOMAXPROCS=16`. The box was shared with other agents; see the load note under the table. Command: `go test -run '^$' -bench . -benchmem -count 10 ./prototypes/actorlib/`. Raw output: `bench-r1b.txt` in the unit directory. The baseline is the hand-written Go actor: a goroutine, a buffered channel, a reply channel per request and `context` cancellation ([bench_test.go](../../prototypes/actorlib/bench_test.go)).

| Operation | Library, min / median | Library allocs/op, B/op | Hand-written Go baseline | Baseline, min / median | Baseline allocs/op, B/op | Median ratio |
| --- | --- | --- | --- | --- | --- | --- |
| Send of a message with no reply, handled | 101.3 / 108.4 ns/op | 0, 0 | channel send to the goroutine | 118.1 / 126.6 ns/op | 0, 0 | 0.86x |
| Ask round trip (counter `Get`) | 731.4 / 769.9 ns/op | 2, 144 | reply `chan int64` per request | 633.6 / 667.7 ns/op | 1, 128 | 1.15x |
| | | | reply channel whose element carries an `error` (`AskBaselineResult`) | 709.6 / 742.8 ns/op | 2, 136 | 1.04x |
| Start and Stop of an idle actor | 3318 / 3699 ns/op | 11, 3328 | goroutine, `context.WithCancel`, `sync.WaitGroup` | 2061 / 2434 ns/op | 6, 2576 | 1.52x |
| Machine-backed Ask (breaker, `Success`) | 919.0 / 967.7 ns/op | 4, 208 | the Ask baseline above | 633.6 / 667.7 ns/op | 1, 128 | 1.45x |
| Agent Call | 880.0 / 920.7 ns/op | 4, 216 | the Ask baseline above | 633.6 / 667.7 ns/op | 1, 128 | 1.38x |

Load average was 11.6 to 13.2 during the run, so the minimum is the better estimate. An earlier run under load 12 to 54 (`bench-r1.txt`) shows the same allocation counts and the same order of each pair except the machine and agent rows, which that load distorted.

What the baseline does not do: a send after stop blocks until the sender's own context ends, queued requests are not answered on stop, a dropped reply hangs the caller, and a message admitted during the stop is never released.

Where the extra cost is, by allocation profile (`-memprofile`, measured):

- **Ask, 2 allocations against 1.** One is the reply channel, as in the baseline. The other boxes `Get{Reply}` into the `CounterMsg` interface: a `Reply` is two words (slot and control), so the variant is not pointer-shaped, while the baseline's `baseGet{chan}` is one pointer. A reply that can carry a failure costs the baseline its second allocation too (`AskBaselineResult`), because a channel element with a pointer gets its buffer apart from the channel. The library avoids that: it keeps the failure as its own type `E`, so `outcome[int64, Never]` has no pointer (A7 lowering note).
- **Start and Stop, 11 allocations against 6.** The extra ones: the actor's scope (1), `rt`'s task handle, done channel and goroutine (3, against the baseline's goroutine and context), the stop channel that wakes blocked senders (1), the task's lazily made stop channel (1, the baseline's `ctx.Done()` is the same) and the interruption reason that `rt` appends when the stopped task ends (1). The last one is a runtime finding for #13 and #207: a lone interruption could be a flag, not an appended reason.
- **Send** costs no allocation in either. The library's shared admission lock adds two atomic operations per send. Send measures as fast as the baseline or faster: the library tries a non-blocking send first.

Candidate optimizations, not measured: a per-task reusable reply slot with a sequence number would remove the reply channel from `Ask` and beat the baseline (pillar 7); lowering a one-type error row to that concrete Go type, as this prototype does for `Reply`, keeps outcomes pointer-free.

## Feature table

Every feature the library uses, and where the spec has it.

| Feature | Used for | Spec | Status |
| --- | --- | --- | --- |
| Generic functions with type and row parameters | `Start`, `Ask`, `Call` | [types.md Generics](../spec/types.md#generics) | Designed |
| Generic types | `Reply[T, E]`, `Address[M]`, `Actor[M, E]`, `Next[S]` | [grammar.md Declarations rule 4](../spec/grammar.md#declarations) ("generic type parameters") | Designed |
| Generic types with row parameters; omitted trailing row is empty | `Reply[T, E]`, `Actor[M, E]` | none | **Gap A6** (#135, #196) |
| Unions and type switches | message unions, `Decision` | [types.md Unions](../spec/types.md#unions) | Prototype |
| Function types with rows; escaping function parameters | the handler | [types.md Function values](../spec/types.md#function-values) | Designed; the escaping spelling is **Gap A10** (#196) |
| `go`, `Task`, `Stop` | the actor's task | [runtime.md Tasks](../spec/runtime.md#tasks) | Designed |
| `Task.Stop` returns the typed failure of a task that ended first | `Actor.Stop() error[E]` | none | **Gap A9** (#207, #13) |
| Channels, `select`, interruption points on channel operations | mailbox, reply slot | [runtime.md Channels](../spec/runtime.md#channels), I1 | Designed |
| `sync.RWMutex`, `sync.Latch` | admission fence, stop signal | [stdlib.md Sync](../spec/stdlib.md#sync-and-stm) (#208) | Designed (`RWMutex` is Go's) |
| Resources, automatic release, consume | `Reply`, `Actor` | [resources.md](../spec/resources.md) R1, R5, R19, R20 | Designed |
| Consuming parameters and receivers | `Reply.Send`, `Address.Send` | R19 lists names only | **Gap A1** (#197) |
| Resource unions | message unions with a reply | none | **Gap A2** (#197) |
| Channels of resources | mailbox of messages with a reply | README Gap 30 (later version) | **Gap A3** (#197, #64) |
| Consuming a resource field in place | `m.Reply.Send(v)` | R39 forbids moving a field | **Gap A4** (#197) |
| Owned parameters in function types | the handler owns `m` | none | **Gap A5** (#196, #197) |
| Failures as values | the reply slot | none | **Gap A7** (#196) |
| Release of a value typed by a type parameter | the drain, a generic state | none | **Gap A8** (#197; lowering #190) |
| Capture of requirements at construction | `Start ... uses R` | README Gap 17 | Designed |
| Value semantics of messages | snapshots | [types.md Structs rule 6](../spec/types.md#structs) | Designed |
| Shared handles by structure | `Address` copies into tasks | runtime.md Tasks rule 6 names actor handles | **Gap A10** (#196) |

A8 in detail: the drain and the loop's state release handle a value of type `M` or `S` that may be a resource. The checker must treat a type parameter's value as possibly a resource. The lowering here checks for `Close` dynamically, on the cold drain path only ([actor.go:313](../../prototypes/actorlib/actor.go)). Go's GC-shape stenciling has no per-type drop glue, so a hot generic release would need the dictionary or an extra function argument.

## Route 2: what it adds and what route 1 loses

| | Route 1 (library) | Route 2 (actor syntax) |
| --- | --- | --- |
| Typed Ask | the message's reply type; Go's checker rejects a wrong reply (measured) | derived from method signatures |
| Reply exactly once | resource rules A1 to A5 (designed); runtime first-answer-wins and release-answers (measured); agents by construction | by construction: the method's return is the reply |
| Interface-shaped protocol | agent calls (code, not data) or a written message union | derived message union (data) |
| Source size, handler actor | 2.65x to 3.38x route 2's tokens | baseline |
| Source size, machine actor | 1.06x | baseline |
| New syntax | none | an `actor` declaration, `spawn`, `post`, and field assignment inside methods |
| Compiler knowledge | none | message derivation, client generation, reply plumbing |
| Userland providers | any library can write another `actor`-like package on the same features (lawful-protocol rule) | only the compiler's form |
| New general rules | A1 to A10, each with clients beyond actors: tasks in channels, queues (#64), `tasks.Set`, RPC replies (#53), batch waiters (#68) | none for replies; still needs A3 for other resource messages |

Route 2's only unique gain is brevity for handler actors. Under construct admission ([GLOSSARY](../../GLOSSARY.md), "Construct admission"), a construct must make bad code unrepresentable where a library cannot. Every bad case here is rejected by the library route: a wrong reply type (measured), a wrong failure type (measured), a double answer (designed, R20), a dropped answer (answered `NoReply`), a send after stop (`Stopped`) and a leaked message on stop (released once, measured). So route 2 does not pass admission for any part.

## Decision table

| Option | Guarantees | Cost | Choose? |
| --- | --- | --- | --- |
| 1a. Library, message union with `Reply` resource | all, with A1 to A10 | 2.65x to 3.38x tokens of route 2 for handler actors; Ask 2 allocations against the baseline's 1 | **Yes**, default for data protocols |
| 1b. Library, agent calls | all; reply once by construction; needs no resource gap | calls are code, not data; `f` must be pure | **Yes**, for method protocols |
| 1c. Library, `Reply` as a plain value (no A1 to A5) | typed Ask; a dropped reply hangs the caller until the actor stops or the caller's timeout (Gleam's behaviour) | weaker; no new rules | Fallback only if #197 rejects A1 to A5 |
| 2. Actor syntax | all, reply once by construction | new syntax and compiler-known derivation; userland cannot provide another form | No: no guarantee needs it |

## Gaps handed on

- **#197 (linearity):** A1 consuming parameters and receivers, A2 resource unions, A3 channels of resources, A4 consuming a resource field in place, A5 owned parameters in function types (with #196), A8 release of a type-parameter value.
- **#196 (checker):** A6 generic types with row parameters and empty-row elision (with #135), A7 failures as values (`Exit[T, E]`), A10 the escaping function-parameter spelling and the structural shared-handle rule.
- **#13 and #207 (runtime):** A9 `Task.Stop`'s result. `rt.Handle.Stop` drops the typed failure of a task that ended before the stop; `actorlib` uses `Task.Interrupt` and `Handle.Wait` instead. Also: a lone interruption allocates a cause reason, one allocation per stopped task.

## Prior art

Pins are the commits read on 2026-10-27. Paths are relative to each repository.

- **Gleam OTP** (`gleam_erlang` `dfa7cd70`, v1.3.0 plus 9; `gleam_otp` `18b78224`, v1.3.0 plus 8). `Subject(message)` is an opaque `Subject(owner: Pid, tag: Dynamic)` (`src/gleam/erlang/process.gleam:78-81`). `process.call(subject, waiting: timeout, sending: fn(Subject(reply)) -> message) -> reply` (`process.gleam:693-699`) is the shape of `Ask`. Choice made: the reply address is in the message, typed by the request function. Gleam lowers to Erlang: `send` is `erlang:send(Pid, {Tag, Msg})`, and `call` makes a fresh subject, monitors the callee and selectively receives the reply or the monitor's `DOWN` (`process.gleam:603-634`). A callee that never answers makes the caller panic at the timeout. Effra answers a dropped reply at once with `NoReply`, because release is deterministic. `actor.continue`/`actor.stop` (`src/gleam/otp/actor.gleam:185,193`) are `Continue`/`End`.
- **Akka Typed** (`akka-core` `838fe727`, v2.10.24 plus 3). `ask[Res](replyTo: ActorRef[Res] => Req)` (`akka-actor-typed/src/main/scala/akka/actor/typed/scaladsl/AskPattern.scala:108`): the same reply-address-in-message typing. A send to a stopped actor goes to dead letters; Effra returns `Stopped`.
- **Erlang/OTP** (pin `516126e9`, [PRIOR_ART](../../PRIOR_ART.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)). `gen_server:call` tags a timed request with a process alias deactivated on timeout (`lib/stdlib/src/gen.erl:L251-L280`), so no stale reply reaches a later call. Effra gets the same from a fresh reply slot per request. `gen_statem` is the prior art for the machine-backed actor.
- **Elixir** (pin `91ee75bb`, [PRIOR_ART](../../PRIOR_ART.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)). `receive` is a special form (`lib/elixir/lib/kernel/special_forms.ex:2401`), while `send` and `spawn` are ordinary `Kernel` functions over BIFs (`kernel.ex:1207,1247`): only the selective receive needs the compiler. Effra has no selective receive, so it needs no special form. `Agent.get_and_update/3` (`lib/elixir/lib/agent.ex:385`, read at `79e8784f`) is the agent form. Elixir lowers processes to BEAM processes; the library is plain modules over `GenServer`.
- **ReScript** (pin `30ce3698`, [PRIOR_ART](../../PRIOR_ART.md)). No actor or process construct: it compiles to single-threaded JavaScript and uses promises. Its relevant lesson is representation: variants lower to tagged host objects, as Effra unions lower to sealed Go interfaces.
- **Swift** (Swift 5.5, SE-0306 actors and SE-0302 `Sendable`). The syntax route: the compiler knows `actor` because it checks isolation of shared mutable state across `await`. Effra does not share mutable state with an actor: the state is a value owned by the actor's task, and messages copy or move. So the guarantee Swift needs syntax for comes from value semantics and resources here.
- **Pony** (`ponyc` `f032e36a`, 0.77.1 plus 73). Behaviours (`be`) and reference capabilities (`iso` for sendable data), for example `actor Promise[A: Any #share]` with `be apply(value: A)` (`packages/promises/promise.pony:3,13`). The syntax route again, buying data-race freedom through capabilities; Effra gets it from value semantics, R17 and A3.
- **Rust Tokio actor pattern** (`tokio` `b980c95d`, 1.54.1 plus 36; Alice Ryhl, "Actors with Tokio", 2021). A handle with an `mpsc` sender, a task, and a `oneshot` per request. `oneshot::Sender::send(mut self, t)` consumes the sender (`tokio/src/sync/oneshot.rs:622`), and its `Drop` completes the channel so the receiver gets `RecvError` (`oneshot.rs:871-874`). This is exactly `Reply`: consume to answer, release to refuse.
- **Go actor libraries.** Proto.Actor Go (`protoactor-go` `288962e5`): `RequestFuture(pid *PID, message interface{}, timeout)` (`actor/root_context.go:144`). Ergo (`ergo` `224fdede`): `HandleCall(from gen.PID, ref gen.Ref, request any) (any, error)` (`act/actor.go:28`). Both type messages and replies as `any`, so a wrong reply is a run-time assertion failure. Effra's generic `Reply[T, E]` makes it a compile error.
- **Effect cluster `Entity`** (`effect-smol` `3a1128c7`, effect 4.0.0-beta.98). An entity's protocol is an `RpcGroup` value (`packages/effect/src/unstable/cluster/Entity.ts:80`), with explicit `mailboxCapacity` and `concurrency` options (`Entity.ts:140-141`). The client is a TypeScript mapped type over the group: library code with a general type feature Effra does not have. So Effra's data protocol is the user's message union, and a later durable provider encodes that union.
- **Koka and OCaml 5.** Effect handlers can implement actors as a library; OCaml 5 does not track effects in types, so a missing reply is not typed. They add no choice beyond Effect's here.

## What this spike does not claim

- No Effra checker accepts these sketches yet. Each sketch is **Proposed**. The Go lowering stands in for the emitted code.
- No route-2 syntax is proposed. The route-2 sketches exist only to measure size.
- No supervision, durable address, persistence or distribution. `actor.Run` under `tasks.Set` keeps its runtime.md item 6 meaning and is not prototyped here.
- No byte budgets (`docs/specs/actors.md` earlier plan). The mailbox has an item capacity only.
- Self-call deadlock (a handler that asks its own actor) is not detected. It is a #13 item.
