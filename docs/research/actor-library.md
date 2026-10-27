# Actors as an ordinary library (#101)

**Status:** spike record for [#101](https://github.com/cevr/effra/issues/101), round 4, 2026-10-27. Round 1 started from `main` at `48daf248`; round 2 merged `main` at `1ddc2b02` and answered the round-1 review; round 3 answered the round-2 review; round 4 merged `main` at `2bd71582` and answers the round-3 review. It changes no compiler behaviour. The examples are in [actor-library/examples](actor-library/examples/), labelled **Proposed**. The Go lowering is [`prototypes/actorlib`](../../prototypes/actorlib/doc.go).

**Owner direction, 2026-10-27, on #101:** "Let's see if we can make actor a library mechanism." **Owner decision, 2026-10-27, on the agent form:** "Okay, I like that recommendation": no variadic generics; one `actor.Call` that takes a function literal on a lent `*S`; function-literal parameter types inferred from the expected type later ([#214](https://github.com/cevr/effra/issues/214), blocked by [#196](https://github.com/cevr/effra/issues/196)).

## Verdict

**Route 1 holds for every part. The std `actor` package is ordinary library code.** It needs no compiler projection, no compiler-known `Actor` identity and no actor syntax. Route 2 is not needed for any guarantee. It would buy only brevity, measured on the same programs: an agent used inline takes 1.66x the tokens of a hypothetical actor syntax (1.54x with #214), a message-union handler actor 2.65x to 3.38x, and the machine-backed breaker 1.06x (tables in Q5). Against a hand-written Go actor with the same contract, the library costs 1.16x for an agent call, 1.25x for `Ask`, 1.29x for `Send`, 1.48x for start and stop and 1.60x for a machine-backed `Ask` (medians of ten paired runs under heavy load; the ranges are wide, 0.77x to 2.25x over all five, Q8). It allocates no more than that baseline, except one allocation for the machine's event envelope and one for the actor's task. Part of the time is the prototype's run-time copy decision, which a compiler removes.

**Every release the library performs goes through one release path.** A refused message, an undeliverable answer or failure, a queued message and the state are each released by one helper, `drop`: masked, so the release completes although the releasing task is stopped (R9); inside an rt owner scope, so a panic in it is an in-release defect; dispatched on the value held, so a state that is a resource union is released; and by outcome kind, so a refusal releases nothing and no zero value is closed. Q2 lists every release site. The actor's exit is recorded by `rt.Scope.End` and `rt.Run`, so causes keep their kind and order with no runtime change (Q6).

**One exception waits on a runtime and lowering rule: an unsuccessful result handoff.** When a boundary discards a successful result because a stop won, nobody releases that result. The prototype shows three forms: `rt.Run` zeros an answer the asker took just before its task stopped; `rt.Scoped` zeros a callback's candidate result when a release requests a stop; and with the `defer Scope.End(&err)` spelling the candidate stays beside an interruption, and `Call` does not close a failed function's value. That rule is A13, for #13 and #207, with the general result and drop lowering of #190 and #197 (Q2). "An answer nobody can receive is released exactly once" therefore holds for every release the library itself performs, and not yet across those handoffs.

The evidence is in three classes. Each claim below carries one of these labels:

- **Measured (Go lowering).** Go's own type checker or a `-race` test in `prototypes/actorlib` shows it. Typed Ask is in this class: a wrong reply type does not compile (Q1).
- **Designed (awaits #196/#197).** A spec rule gives it in checked source. Thirteen of those rules are not in `docs/spec` yet. They are general rules, and each serves clients other than actors: Gaps A1 to A13 below, which are spec README Gaps 35 to 47. One of them, A13, is a runtime rule for #13 and #207. Without them the guarantee falls back to its runtime form.
- **Runtime.** The library code checks it at run time, and a test shows it.

Three findings change the plan:

1. **The reply address is the message's business, not the compiler's.** `Ask(to Address[M], request func(Reply[T, E]) M) (T, error[E | Stopped | NoReply])` takes `T` and `E` from the request's parameter type. This is Gleam's `process.call` and Akka Typed's `ask`.
2. **Reply exactly once needs no new linearity rule.** `Reply[T, E]` is an ordinary resource. Its `Close` answers the caller. Automatic release (resources.md R5) gives "at least once, or the caller gets a cause", and use after consume (R20) gives "at most once". The general channel rule (A3) releases an answer nobody can receive. The cost is five resource rules that #197 must state in general form (A1 to A5). Tokio's `oneshot` works the same way: the sender's `Drop` completes the channel, and the receiver's `Drop` drops a value in flight.
3. **A protocol that is a set of methods needs no message union.** An agent (Elixir `Agent`) serves calls on its state. The protocol is the state's methods with a `*S` receiver, and `actor.Call(room, func(r *Room) (int64, error[RoomFull]) { return r.Join(name) })` lends the state to one call. The reply is the call's return value, so it is answered once by construction, with no resource rule at all.

## Library API

The full package is [actor.go.sketch](actor-library/examples/actor.go.sketch). It uses the provisional generic spelling of [types.md](../spec/types.md#generics) (#135). Every function result in it is `T`, `error[E]` or `(T, error[E])` ([grammar.md](../spec/grammar.md#declarations), Declarations rule 5).

| Item | Signature (sketch) | Kind |
| --- | --- | --- |
| Reply address | `type Reply[T, E] struct{…}`; `Send(v T)`, `Fail(e E)`, `Close()` | resource (has `Close`) |
| Copyable half | `type Address[M] struct{…}`; `Send(m M) error[Stopped]` | plain value (fields are channels) |
| Owner handle | `type Actor[M, E] struct{…}`; `Address()`, `Send`, `Stop() error[E]` | resource (holds a `Task`) |
| Start | `func Start(initial S, capacity int64, handle func(S, M) (Next[S], error[E]) uses R) Actor[M, E] uses R` | function |
| Ask | `func Ask(to Address[M], request func(Reply[T, E]) M) (T, error[E \| Stopped \| NoReply])` | function |
| Handler decision | `Next[S]`, `Continue(s)`, `End(s)` | plain value |
| Machine value | `type Machine[S, E, O, F, R] struct{ initial S; step func(S, E) (Decision[S, O], error[F]) uses R }` | plain value (A12) |
| Machine actor | `func StartMachine(m Machine[S, E, O, F, R], capacity int64) MachineActor[S, E, O, F] uses R`; `Send`, `Ask`, `State`, `Stop` | library over `Start` |
| Agent | `func StartAgent(initial S, capacity int64) Agent[S]`; `a.Address() AgentAddress[S]`; `func Call(a AgentAddress[S], f func(r *S) (T, error[E])) (T, error[E \| Stopped \| NoReply])` | library over `Start` |

Failures, each a typed failure in a row, never a defect:

- `Stopped`: the actor refused or discarded the request because its stop was requested or it had ended.
- `NoReply`: the actor dequeued the request and released its reply unanswered while it ran. A handler that drops the reply, or a handler defect while it holds the reply, gives `NoReply`. A defect in an agent's `f` gives `NoReply` to its caller, and `Stop` returns the defect.

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

The Go lowering is `actorlib.Ask[M, T any, E error]` ([actor.go:330](../../prototypes/actorlib/actor.go)). `TestReplyTypingIsCheckedByGo` ([compile_test.go:17](../../prototypes/actorlib/compile_test.go)) adds each testdata file to the examples package through a `go build -overlay`. It requires the exact Go type error, and a control file must compile:

| Negative case | Go's refusal |
| --- | --- |
| [wrong_reply_type.go](../../prototypes/actorlib/testdata/wrong_reply_type.go): a request built around `Reply[string, Never]` for `Get` | `cannot use r (variable of struct type actorlib.Reply[string, actorlib.Never]) as actorlib.Reply[int64, actorlib.Never] value in struct literal` |
| [wrong_reply_sent.go](../../prototypes/actorlib/testdata/wrong_reply_sent.go): a handler answers `Get` with a string | `cannot use "ten" (untyped string constant) as int64 value in argument to m.Reply.Send` |
| [wrong_reply_failure.go](../../prototypes/actorlib/testdata/wrong_reply_failure.go): a handler fails `Get`, whose row is empty, with `RoomFull` | `cannot use RoomFull{…} (value of struct type RoomFull) as actorlib.Never value in argument to m.Reply.Fail` |

Limit: Go checks one failure type per reply. A row with several types is checked by the Effra checker (#196); the lowering erases it to Go's `error`.

Methods cannot introduce type parameters in Go, so `Ask` is a function, not a method on `Address`. Gleam makes the same choice (`process.call`).

### Q2. Reply exactly once

**Designed as general resource and channel rules, with a runtime fallback that is measured.** `Reply[T, E]` has `Close`, so it is a resource (R1). No actor rule is added.

| Property | Mechanism | Class |
| --- | --- | --- |
| At most once | `Send` and `Fail` consume the reply; a second answer is a use after consume (R20) | Designed (A1, A4) |
| At most once, lowering | the slot's first answer wins, and a later one is released by the answering task: [actor.go:105](../../prototypes/actorlib/actor.go) | Runtime: `TestFirstAnswerWins`, `TestAnsweredButUnreceivedReplyIsReleased` |
| At least once, or a cause | a dropped reply is released at its owner's scope end (R5); its `Close` answers `NoReply`, or `Stopped` once the stop is requested | Designed (A1 to A5, A8) |
| At least once, lowering | the handler's emitted release: `TestDroppedReplyAnswersNoReply`, `TestHandlerDefectAnswersNoReplyThenStopped`, `TestStopAnswersInFlightAndQueuedStopped` | Runtime |
| Detached caller | a caller stopped while it waits detaches; an answer or failure payload nobody can receive is released exactly once, masked, for `Ask` and `Call`, on cancel, stop and drain, including an answer that is ready when the stop is | Designed (A3, receiving half); Runtime: `TestDetachedResourceReplyIsReleased`, `TestDetachedAgentResultIsReleased`, `TestCanceledReadyReplyReleasesResource` (4,096 rounds), `TestR3CanceledReplyReleaseRunsMasked`, `TestR3CanceledReplyDefectIsInRelease`, `TestInterruptedAskerDetaches` |
| Unsuccessful result handoff: an answer taken before the caller's task stops, or a callback's result discarded by a stop | the boundary that applies stop-wins discards a successful result | **Pending a runtime and lowering rule (A13; #13, #207, #190, #197)**; pinned by `TestR3Gap47TaskBoundaryDropsTakenAnswer`, `TestR3Gap47AgentInterruptedCallbackResult`, `TestR3Gap47CanonicalScopedDropsResultOnStop` |

The general rule for the detached caller is the channel rule's receiving half (A3): when the last owner of a channel releases it, its buffered values are released. A reply slot is a channel that the asker and the reply share. When the asker detaches, its share is released; the answer, sent before or after, is released with the last share. So a value nobody can receive is released, and a counted `Close` on it runs once. The lowering has no channel reference count, so the slot carries a one-shot state, open, answered or abandoned ([actor.go:84-176](../../prototypes/actorlib/actor.go)): `answer` delivers only from open, and `abandon` takes and releases an answer that arrived first. Tokio's `oneshot` has the same two halves: `Sender::send` returns the value when the receiver is gone (`tokio/src/sync/oneshot.rs:622`), and the receiver's `Drop` drops a completed value (`oneshot.rs:1238`).

**Ownership passes only to a live receiver (round-2 review M1).** A stop and an answer can be ready together. A `select` then may take the answer for a task that is already stopped, and that task's boundary (`rt.Run`) discards the value by stop-wins without releasing it: the round-2 probe lost 30 of 64 resources. `receive` checks the stop before it waits and again after it takes an answer, and releases a taken answer that it cannot deliver ([actor.go:146](../../prototypes/actorlib/actor.go)).

**One release path (round-3 review M1 to M3).** Three rounds found release paths that skipped a rule: an unmasked release, a release that tested the storage pointer, a release of a refusal's zero value. Round 4 closes the class. Every release the library performs calls one helper, `drop(t, v, err)` ([actor.go:558-590](../../prototypes/actorlib/actor.go)), with three rules:

1. **Masked, through rt's public release boundary.** `drop` opens a mask on the current task and releases `v` in an rt owner scope (`rt.NewScope`, `Scope.Own`, `defer Scope.End`), so the release runs to completion although the task is stopped (R9), and rt records a panic in it as an in-release defect with the task's chain. It applies no stop-wins; the caller's exit adds the interruption after the release cause. With an error row, the cause joins the caller's exit; with none (`Reply.Send`), it unwinds the caller, as `Scope.End(nil)` does, and the next boundary records it.
2. **On the value held, not its storage.** `drop` tests the dynamic value for `Close`, so a union or an interface releases its variant. An agent keeps its state in storage (`*S`) that calls borrow, and its loop passes the `S` the storage holds ([agent.go:49](../../prototypes/actorlib/agent.go)).
3. **By outcome kind.** A success releases its value and a failure its payload; a refusal releases nothing ([actor.go:70](../../prototypes/actorlib/actor.go)). A refusal that loses the race needs no task and releases nothing (`refuse`, [actor.go:116](../../prototypes/actorlib/actor.go)). No zero value is closed: a refusal's zero `T` is never passed, and `Call` does not pass a failed function's value.

Every release site, and the call it makes (lines in [actor.go](../../prototypes/actorlib/actor.go), [agent.go](../../prototypes/actorlib/agent.go), [machine.go](../../prototypes/actorlib/machine.go)):

| Release site | Task that releases | Call | Test |
| --- | --- | --- | --- |
| `Send` by a stopped sender | sender | `drop(t, m, &err)`, actor.go:284 | `TestEveryReleaseSiteRunsMasked` |
| `Send` refused (admission closed or stop requested) | sender | `drop(t, m, &err)`, actor.go:300 | `TestEveryReleaseSiteRunsMasked`, `TestRefusedSendReleaseDefectFollowsRefusal` |
| `Send` woken by the stop or the sender's stop while it waits | sender | `drop(t, m, &err)`, actor.go:319 | `TestBlockedSenderWakesOnStopAndReleasesItsMessage` |
| Rejected answer: a second answer, or an answer to a detached asker | answerer | `answer` → `outcome.release(t, nil)` → `drop`, actor.go:110 | `TestEveryReleaseSiteRunsMasked`, `TestLateAnswerReleaseDefectIsInRelease` |
| `Reply.Send` or `Fail` on a zero reply (a machine `Send`) | answerer | `drop(t, v, nil)`, actor.go:195, :206 | `TestEveryReleaseSiteRunsMasked` |
| Slot abandon: an answer that arrived first (stopped receiver, failed `Ask` or `Call` admission) | receiver | `abandon` → `outcome.release(t, err)` → `drop`, actor.go:129 (callers :150, :161, :333, agent.go:98) | `TestR3CanceledReplyReleaseRunsMasked`, `TestR3CanceledReplyDefectIsInRelease` |
| Post-take cancel: an answer taken for a stopped receiver | receiver | `outcome.release(t, &err)` → `drop`, actor.go:156 | `TestCanceledReadyReplyReleasesResource` |
| Failure payload nobody can receive | receiver or answerer | the two rows above, by kind `failed` | `TestEveryReleaseSiteRunsMasked` (failure row), `TestR3MatchedCanceledFailureIsReleased` (baseline) |
| Refusal (dropped reply, queued call, agent call's `Close`) | any | `refuse`: releases nothing, actor.go:116 | `TestRefusalReleasesNothing`, `TestR3LibraryResourceResultControl` |
| Queue drain: the held message and every queued message, admission order | actor | `rs.Defer(func() { drop(t, m, nil) })`, actor.go:538 | `TestDrainReleasesRunMasked`, `TestEveryMessageReleasedOnceUnderConcurrentStop` |
| A queued machine event | actor | the drain's `drop` runs the event's generated `Close`, which releases the event by its variant and the reply (`closeField`, machine.go:66) | `TestMachineQueuedEventIsReleased` |
| Actor state on every exit (`End`, a typed failure, a defect, `Stop`, owner-scope end) | actor | `releaseState` → `drop`, actor.go:502, registered first in the drain so released last | `TestEndReleasesStateAndRefusesLaterSends`, `TestOwnerScopeStopsTheActorAndReleasesItsState`, `TestDrainKeepsAllDefectsAndReleasesState` |
| Agent state on each of five exits (stop, callback panic, callback defect cause, typed failure then stop, owner-scope end) | agent | `releaseState` → `drop(t, held(s), nil)`, actor.go:504 | `TestR3AgentUnionStateReleasedOnEveryExit` (a resource union state), `TestAgentStateReleasedOnCallbackDefect`, `TestR3CanonicalAgentExitKeepsCauseOrder` |
| Owner-scope shutdown | owner, then actor | the owner scope stops the actor's task (`rt.Go`'s handle); the loop's exit runs the drain above | `TestOwnerScopeStopsTheActorAndReleasesItsState`, `TestR3AgentUnionStateReleasedOnEveryExit/owner-scope` |

No other code in the package calls `Close` on a value it owns. `closeField` runs only inside a value's generated `Close`, which a `drop` already masks and records.

**The exception: an unsuccessful result handoff (round-3 review m1).** A stop can still discard a successful result after the library has handed it on. The three forms are pinned by tests that assert today's count, so the fix will fail them:

- `rt.Run` zeros a result when a stop wins at the task boundary: an asker that took its answer and is stopped before its boundary loses it (`TestR3Gap47TaskBoundaryDropsTakenAnswer`).
- `rt.Scoped` zeros a candidate result when a release in it requests a stop: a callback that returns a lease from `rt.Scoped` loses it before `Call` sees it (`TestR3Gap47CanonicalScopedDropsResultOnStop`).
- With the `defer Scope.End(&err)` spelling, the candidate stays beside an interruption. `Call` does not close it: by Go's convention a failed function's value is not valid, and closing a zero value would be wrong (`TestR3Gap47AgentInterruptedCallbackResult`).

The public `Task` and `Scope` APIs cannot attach a release to a result that a later boundary discards, and holding a mask across the return would change interruption semantics. So this is not an actor rule. It is the general rule A13:

- **A13. A boundary releases a successful result it discards.** runtime.md Exit causes rule 5 makes a stop beat a body's success, and `rt.Run` and `rt.Scoped` then return the zero value. When the discarded result is a resource, or holds one, nobody releases it. *Recommendation:* every boundary that applies stop-wins to a successful result, a task boundary or a scope boundary, releases that result, as an owner scope releases what it owns (R5); and the lowering of a function result marks which value an unsuccessful exit still owns, so that no zero value is closed. Owners: #13 and #207 for the runtime, #190 and #197 for result and drop lowering. Until then "an answer nobody can receive is released exactly once" holds for every release the library performs, and not across these handoffs.

The runtime does not raise a defect for a dropped reply. Dropping is legal under automatic release: Effra resources are affine with a release, not linear. The caller gets `NoReply` as a typed failure. A rule that forbade the drop would need a "must consume" marker, which is a new linearity mode with no second client yet.

For a handler to answer a reply that arrived in a message, these rules must exist. Each is general:

- **A1. Consuming parameters and receivers.** A method whose body consumes its receiver (`Reply.Send` ends with `r.Close()`) consumes it at the call. R19 lists `Close`, `Task.Wait`, `Task.Stop` and an actor's `Stop` by name; the general rule replaces that list. *Prior art:* Rust by-value `self`; Swift `consuming` methods.
- **A2. Resource unions.** A union with a resource variant is a resource, and a type switch moves the variant out.
- **A3. Channels of resources.** README Gap 30's recommended rule: a send moves the value, the buffer owns values in flight, and releasing the channel's last owner releases them. `Address.Send` moves the message; on failure Send's own scope releases it. The receiving half covers the detached caller above.
- **A4. Consuming a resource field.** `m.Reply.Send(v)` consumes one field of an owned message. R39 forbids moving a field out; consuming one in place is a relaxation, and the generated `Close` (R3) then skips that field. *Prior art:* Rust partial moves.
- **A5. Owned parameters in function types.** The handler `func(S, M)` must own `m` to consume the reply inside it. A function type has no body to infer from, so it spells the mode. *Prior art:* Swift `(consuming T) -> U`.

### Q3. Rows

**Yes, by ordinary row polymorphism. Designed (#196); the lowering shows the capture.**

- `Start(…, handle func(S, M) (Next[S], error[E]) uses R) Actor[M, E] uses R`. The handler's requirements `R` are captured at `Start` and charged there, the constructor-capture rule of README Gap 17. Lowering: the breaker's step captures its `Probe` environment where the machine value is built ([breaker.go:110](../../prototypes/actorlib/examples/breaker.go)).
- The handler's failure row `E` becomes the owner handle's row: `Actor[M, E].Stop() error[E]`, as `Task[T, E]` carries its body's row.
- `Ask`'s row is `E | Stopped | NoReply`, with `E` from the request's reply type. `Send`'s row is `Stopped`. `Send` waits while the mailbox is full (runtime.md item 6), so there is no `Full`. A non-waiting `TrySend` with `error[Full]` is a later addition.
- Footprints rule 5 already keeps an actor whose handler uses a `with`-bound capability inside that `with`: the actor is a resource the block owns.

Row features that `docs/spec/rows.md` and `types.md` lack:

- **A6. Generic types with row parameters,** and a trailing row argument that is omitted means the empty row: `Reply[int64]` is `Reply[int64, (empty)]`. types.md shows generics only on functions, and its elision rule covers only rows that pass through.
- **A7. Failures as values.** The library carries a typed failure from the handler's task to the asker's task through a channel. That needs a value form of an outcome: a prelude `Exit[T, E]` with `Succeed`, `Fail` and `Get() (T, error[E])`. `Task.Wait` does this inside the kernel today. *Prior art:* Effect `Exit`; Gleam `Result`. Without A7, a closure `func() (T, error[E])` returning the failure works under the current rules, at a closure per reply.

### Q4. Machine-backed actors

**Yes: plain generics over the machine value. Measured for behaviour; the signature is designed.** `StartMachine` reads `m.initial` and calls `m.step`. `Ask` returns the step's `Decision[S, O]` (runtime.md item 2), and `State` returns a snapshot. The actor library depends on a machine only as a value with `initial` and `step`, so any lawful provider of that shape runs (runtime.md item 5).

It is written on top of `Start`, which shows that the core composes: the events travel in a private envelope with an optional reply ([machine.go:89](../../prototypes/actorlib/machine.go)). `TestBreakerMachineActor` drives the runtime.md breaker: three failures open it, a probe failure goes to the asker and leaves the state unchanged, `try b.Ask(Retry{}) catch { case ProbeDown: Reject{} }` yields `Reject`, a working probe moves it to `HalfOpen`, and `Retire` returns `Done{"retired"}` and ends the actor. `TestBreakerSendFailureEndsTheActor` shows that a step failure with no asker ends the actor, and `Stop` returns `ProbeDown`.

**How the step's whole signature reaches `StartMachine` (review O1).** Through ordinary types, if one rule is added (A12). The machine value's type is a generic struct whose function-typed field carries every row:

```go
type Machine[S, E, O, F, R] struct {
	initial S
	step    func(S, E) (Decision[S, O], error[F]) uses R
}

func StartMachine(m Machine[S, E, O, F, R], capacity int64) MachineActor[S, E, O, F] uses R
```

`type Breaker machine[State, Signal] { initial Closed{0}; step step }` then declares a value of type `Machine[State, Signal, string, ProbeDown, Probe]`, with `O`, `F` and `R` read from `step`'s signature. `StartMachine` passes `F` to the asker's row and to `MachineActor`'s `Stop`, and charges `R` where it is called, by the same constructor capture as `Start`. runtime.md item 1 gives `machine[S, E]` only two parameters, so the missing rule is:

- **A12. The type of a machine value.** A machine declaration's value has the prelude type `Machine[S, E, O, F, R]`, with `O`, `F` and `R` inferred from `step`. It needs A6 for `uses` row parameters too, and a function-typed field whose `uses` row is charged where the value is built.

The Go lowering carries `S`, `E` and `O` as type arguments, erases `F` to Go's `error` and captures `R` in the step's closure ([machine.go:38](../../prototypes/actorlib/machine.go), [breaker.go:110](../../prototypes/actorlib/examples/breaker.go)).

### Q5. Handler actors and agents

**Yes, without projection, in two shapes. The cost is source length, measured below.**

- **Message union (data protocol).** The user writes the union with a `Reply` field per answered variant, a handler with a type switch, and one client function per request ([roster.go.sketch](actor-library/examples/roster.go.sketch)). The protocol is data: `ef inspect` can show it from ordinary types, and a later durable or distributed provider can encode it.
- **Agent (method protocol).** The protocol is the state's methods with a lent `*S` receiver, `func (r *Room) Join(name string) (int64, error[RoomFull])`. One `actor.Call(a, f)` takes a function literal `func(r *S) (T, error[E])`: `actor.Call(room, func(r *Room) (int64, error[RoomFull]) { return r.Join(name) })` ([roster-agent.go.sketch](actor-library/examples/roster-agent.go.sketch)). After #214 the literal's parameter types come from `Call`'s expected type: `actor.Call(room, func(r) { r.Join(name) })`. The reply is the return value. The cost: a call is code, not data, so it cannot be logged, inspected, encoded or replayed.

An interface's methods cannot become messages without a projection or a code generator. Both are route 2, so this spike does not use them.

**The lent `*S` cannot escape the call.** No new rule is needed:

- `*T` appears only on parameters and receivers ([types.md](../spec/types.md#borrows) Borrows rule 1). So `*S` is not a result type, a field type or an element type, and `func(r *Room) *Room` is not a type.
- A borrow is never stored, returned, sent or captured by `go` (Borrows rule 1). So `f` cannot put `r` in a captured variable, return it, send it on a channel or start a task with it.
- A borrow and a lend obey the same no-escape rule (Borrows rule 4). So an escaping closure inside `f` cannot capture `r` (R17, R33), and a non-escaping closure may use it for the duration of its call (R30).
- What `f` returns is a value read through the borrow, such as `r.names`. It crosses into the caller's task, so it is a copy (Q7, A11), never an alias of the state. `TestRosterAgentMatchesTheHandlerActor` and `TestBoundaryCopiesMessagesAndReplies` change the returned slice and check that the agent keeps its own.

`f` changes the state in place. A typed failure does not roll back what `f` changed before it failed; `Room.Join` checks before it changes anything.

**`Call` decides by the kind of `f`'s exit (round-2 review M5).** In source, `Call`'s `catch` handles only a typed failure `E` (rows.md, catch rule 6), so only a lone typed failure is the caller's failure, and the agent goes on. An interruption or a defect passes through the catch and ends the agent with that cause: the active caller gets `NoReply` (or `Stopped` once the stop is requested), queued callers get `Stopped`, and `Stop` returns the cause without a stop's interruption. The lowering makes the same test with `rt.Failure`, which matches only a lone typed failure ([agent.go:76-101](../../prototypes/actorlib/agent.go)), so a defect cause that `f` returns from a nested scope (`defer local.End(&err)`) is the agent's defect, not a value. Tests: `TestAgentStopsOnManagedCallbackDefect`, `TestAgentCallbackDefectIsADefect`, `TestManagedInterruptionIsNotADefect`.

**The agent owns its state on every exit (round-2 review M3).** A call only borrows the state. In source the handler owns `s`, so R5 releases it when a call unwinds. In the lowering the loop keeps owning an agent's state while a call runs ([actor.go:381](../../prototypes/actorlib/actor.go), `lent`), so a panic or an agent-ending exit in `f` leaves the state to the loop's release, which runs once, after the queue. That release dispatches on the `S` the storage holds, not on the `*S` storage (round-3 review M2): a pointer to an interface has no `Close`, so round 3 missed a state that is a resource union. `TestAgentStateReleasedOnCallbackDefect` panics in `f` and counts one `Close` of a resource state; `TestR3AgentUnionStateReleasedOnEveryExit` holds a resource union as the state and counts one `Close` on each of the five exits.

**Empty `uses` is not purity (review M5).** `Call`'s `f` has an empty `uses` row. A caller that is stopped while it waits detaches, and its call may still run after the caller's `with` has released the providers that `f` would use. So `f` must not need a capability. An empty row still admits waits: a channel receive, a timer, a `sync` wait ([types.md](../spec/types.md#function-values) Function values rule 1: a function type carries requirements only, never purity). `f` runs on the agent's task, so each of its waits is an interruption point of the agent (I1), and `Stop` interrupts it; the caller then gets `Stopped`. In the Go lowering `Call` passes the agent's task to `f`: `f func(t *rt.Task, s *S) (T, error)` ([agent.go:76](../../prototypes/actorlib/agent.go)). `TestAgentChannelWaitObservesStop` blocks `f` on a channel and checks that `Stop` returns without the channel being closed.

Measured size of the same programs, `go run ./prototypes/actorlib/cmd/sketchsize docs/research/actor-library/examples/*.go.sketch` (code lines exclude blanks and comment-only lines; tokens are Go scanner tokens without comments and inserted semicolons). The agent form is the round-2 form A with spelled literal types:

| Program | Route 1, message union | Route 1, agent (form A) | Route 2 (hypothetical syntax) | Route 1 / route 2, tokens |
| --- | --- | --- | --- | --- |
| Counter | 22 lines, 199 tokens | — | 11 lines, 75 tokens | 2.65x |
| Roster (3 requests, 3 reply types, 1 failure) | 49 lines, 439 tokens | 34 lines, 348 tokens | 22 lines, 130 tokens | 3.38x (union), 2.68x (agent, named facade) |
| Breaker (machine-backed) | 53 lines, 406 tokens | — | 52 lines, 383 tokens | 1.06x |

The agent row above measures one chosen API: `roster-agent.go.sketch` declares three named client functions, `JoinRoom`, `LeaveRoom` and `RoomMembers`, and route 2 needs none. Those wrappers are optional; a caller can use `actor.Call` inline. So the agent is measured at four scopes (round-2 review m1; [sizes](actor-library/examples/sizes/)), and **the headline is the inline-call scope**, the like-for-like program:

| Scope | What each side contains | Route 1, agent | Route 2 | Ratio, tokens |
| --- | --- | --- | --- | --- |
| **Definitions plus an inline caller (headline)** | the state, its methods, start, and one exported `Use` that runs all three operations inline | 30 lines, 291 tokens | 27 lines, 175 tokens | **1.66x** |
| Same, with #214 literals | as above, with `func(r) { r.Join(name) }` | 30 lines, 270 tokens | 27 lines, 175 tokens | 1.54x |
| Definitions only | the state, its methods and start; no caller | 25 lines, 177 tokens | 22 lines, 130 tokens | 1.36x |
| Named facade | definitions plus the three named client functions (route 2 derives them) | 34 lines, 348 tokens | 22 lines, 130 tokens | 2.68x |

Command: `go run ./prototypes/actorlib/cmd/sketchsize docs/research/actor-library/examples/roster-agent.go.sketch docs/research/actor-library/examples/route2-roster.go.sketch docs/research/actor-library/examples/sizes/*.go.sketch`. The callers spell their rows; route 1's row names `actor.NoReply` and route 2's does not.

Reading the numbers:

- For a machine, the two routes are the same size. The machine declaration carries the protocol, so actor syntax saves only `actor.StartMachine(Breaker{}, 16)` against `spawn Breaker{} cap 16`.
- For a handler actor, route 2 is much shorter. Route 1 spends its tokens on the message union, the reply fields, the spelled parameter types of each request function, and the spelled rows of each exported client function (rows.md, Capability rows rule 6).
- For an agent used inline, route 1 is 1.66x route 2's tokens, and 1.54x with #214. The definitions alone are 1.36x: the `*Room` receivers, the `r.` field prefixes and the spelled `actor.StartAgent`. The named facade's 2.68x mostly measures the three optional wrappers, whose spelled signatures and rows route 2 derives.
- The agent's form A is the same size as round 1's state-passing form at the facade scope (34 lines and 348 tokens against 33 and 345): the `*Room` receivers remove the returned `Room` from each method and each literal, and the added `Members` method costs one line.
- Inferred, not measured: two general features would recover part of the remaining gap without actor syntax. One is #214, function literals whose parameter types come from the expected type (Go proposal 21498). The other is a named error row such as `type Asked = actor.Stopped | actor.NoReply`. Both serve every higher-order library. They are ergonomic follow-ups, not gaps, because no guarantee depends on them.

### Q6. Address split

**Yes: ordinary types plus resources. Designed; measured in the lowering.** This resolves runtime.md item 7.

- `Actor[M, E]` holds the actor's `Task`, so it is a resource by R1 and R14 with no actor-specific rule. Its owner scope stops it at scope end, and `Stop` consumes it. Lowering: `Start` registers the task's handle with the owner scope ([actor.go:372](../../prototypes/actorlib/actor.go)); `TestOwnerScopeStopsTheActorAndReleasesItsState`.
- `Address[M]` holds only channels and the shared control. It has no `Close`, so it is not a resource. It copies into tasks under runtime.md Tasks rule 6, and it does not keep the actor alive.
- **Send to a stopped actor fails with `Stopped`**, a typed failure. A sender that is waiting on a full mailbox wakes with `Stopped`. `Ask` gets `Stopped` before admission and for a queued request discarded by the stop. Tests: `TestStopAnswersInFlightAndQueuedStopped`, `TestBlockedSenderWakesOnStopAndReleasesItsMessage`, `TestEndReleasesStateAndRefusesLaterSends`. *Prior art:* Erlang's send to a dead pid succeeds silently and Akka sends to dead letters; Effect's `Queue.offer` returns `false` after shutdown. Effra takes Effect's choice and types it.
- **Admission is linearizable with `Stop` (review M2).** `Stop` closes admission before it interrupts the actor ([actor.go:407](../../prototypes/actorlib/actor.go)). Closing wakes waiting senders, then takes the admission lock exclusively once and sets `closed` ([actor.go:240](../../prototypes/actorlib/actor.go)). A sender holds the lock shared while it admits and refuses when `closed` is set or the actor's stop is requested ([actor.go:294](../../prototypes/actorlib/actor.go)). So a `Send`, `Ask` or `Call` that starts after the stop is requested returns `Stopped`, and none succeeds into a mailbox that will not run. `TestStopRefusesAdmissionBeforeHandlerFinishes` holds the handler, waits until the stop is requested, sends, and requires `Stopped`. The loop also refuses a message it dequeues after the stop request and releases it as a queued request.
- Residual window: a stop that does not come from `Stop` (an ancestor's stop, the owner scope's end) reaches the actor's task first. A sender that starts after it is refused, because the refusal also reads the task's stop request. A sender that was already waiting on a full mailbox may complete its send until the loop closes admission at its exit; the drain then releases that message as a queued request, so it is still released once and its request is answered `Stopped`.
- Every admitted message is released once. `TestEveryMessageReleasedOnceUnderConcurrentStop` admits 1,600 counted messages from 8 tasks while the actor stops. It checks one release for each message, whether handled, drained or refused, under `-race`.
- **The drain keeps every cause, through rt's own exit path (review M4, rounds 1 and 2).** The library classifies no recovered panic itself; rt does, on its public path ([actor.go:476-545](../../prototypes/actorlib/actor.go)). When the loop has something to release, `drain` registers each release in an rt owner scope, `defer rs.End(err)`, and raises the body's panic, if any, inside it: `Scope.End` records the body's exit, then the panic, then runs every release masked and records each release cause. When there is nothing to release, a body panic is raised again for the task boundary (`rt.Run`), which classifies it the same way. So the `rt.Interrupted` sentinel and an exit carried out of a nested scope (`Scope.End(nil)`) keep their kind, and only other panic values become defects. The drain releases the held message, the queue in admission order and last the state; every release runs even after an earlier one panicked, and each release cause keeps its kind, its `InRelease` mark and its order. A clean stop with nothing to release builds no scope and allocates nothing here. Round 2's hand-written recovery turned a managed interruption into a defect and wrapped a nested scope's two defects in one; the round-2 probes are now tests: `TestManagedInterruptionIsNotADefect`, `TestDrainPreservesNestedManagedCauses`, with `TestDrainKeepsAllDefectsAndReleasesState`. rt's public `Scope` gives this boundary, so no runtime change is needed for it.

Runtime.md Tasks rule 6 names "actor handles" among the shared handles. Under route 1 the rule can be structural: a value whose fields are all synchronised handles is shared by copy (A10).

### Q7. Message rules

**Already general. No named constraint.**

- A send moves a resource: A1 plus A3. A message with no resource copies, as every value copies into a task (runtime.md Tasks rule 6).
- A borrowed value cannot be sent: R17 (a lent resource must not be sent) and types.md Borrows rule 1 (a borrow is never sent). These rules hold for channels, queues (#64) and tasks alike.
- **Every value that crosses the actor boundary is a copy (review M3).** The rule is the one for values copied into a task (runtime.md Tasks rule 6), with value semantics (types.md Structs rule 6: a copy is a snapshot). It applies wherever a value leaves one task for another: an actor's or agent's initial state, a message, a reply's value and its typed failure, an agent result and its typed failure, a handler's typed failure, a machine's state snapshot and its decision. For Go, which shares slices and maps by reference, the lowering must clone each reachable slice and map. The spec does not yet say so for crossings other than `go` captures: **A11. The copy at a task boundary.** The compiler would emit the clone per static type, nothing for a type without a slice or map, and skip it on a last use. The prototype has no static types in generic code, so [snapshot.go](../../prototypes/actorlib/snapshot.go) decides per type at run time from a cached plan and clones by reflection. `Start`, `StartAgent`, `Address.Send`, `Reply.Send`, `Reply.Fail`, `Call`'s value and failure, a handler's failure and the machine's `State` and decision go through it.
- **A message that holds a resource is still copied, field by field (round-2 review M2).** A message with a `Reply` is a resource, and round 2 returned it as it was, so its ordinary fields stayed shared while it waited in the queue. A resource moves, and a move must not duplicate its identity. In this lowering a resource's identity is always a handle: a pointer, a channel or a function, such as a `Reply`'s slot. Handles are shared, so the move keeps them; the ordinary fields beside them are values, which the struct literal copied in source, and are cloned. A lowered resource whose identity were a slice or a map would need the compiler to mark it; none exists here. Tests (the round-2 probes): `TestAgentInitialValueIsCopied`, `TestQueuedReplyBearingMessageIsCopied`, `TestFailurePayloadIsCopied`, with `TestAgentFailurePayloadIsCopied`, `TestMachineStateIsASnapshot`, `TestBoundaryCopiesMessagesAndReplies` and `TestSnapshotSharesNoMutableStorage`. Its cost is in Q8. The general cost of copying `[]T` and `map[K]V` is a value-semantics cost for #81 and #211, not an actor cost.

### Q8. Cost

**Measured.** Machine: AMD EPYC 9554P (16 vCPU visible), Linux 6.12.93, go1.27.0 linux/amd64, `GOMAXPROCS=16`. The box was shared with other agents; see the load note under each table. Command: `go test -run '^$' -bench . -count 10 ./prototypes/actorlib/` (every benchmark calls `ReportAllocs`). Raw output: `bench-r1b.txt` (round 1), `bench-r2.txt` (round 2), `bench-r3.txt` (round 3), and `bench-r4.txt`, `bench-r4-paired.txt` and `bench-r4-vs-r3.txt` (round 4), in the unit directory.

There are two baselines ([bench_test.go](../../prototypes/actorlib/bench_test.go), [bench_matched_test.go](../../prototypes/actorlib/bench_matched_test.go)):

- **Raw:** the actor a Go programmer writes first: a goroutine, a buffered channel, a reply channel per request and `context` cancellation. It gives less: a send after stop blocks until the sender's own context ends, queued requests are not answered on stop, a dropped reply hangs the caller, and a message admitted during the stop is never released.
- **Matched (review M6 of rounds 1 and 2, M3 of round 3):** hand-written Go with the library's contract. A sender whose context is already cancelled is refused before it admits (I1). Admission is fenced (an `RWMutex`, a closing flag and a stopping channel). A one-shot reply slot with an atomic state completes a dropped reply, "no reply" while the actor runs and "stopped" once it stops, and releases an unreceivable answer, checking the receiver's context before and after it waits. It owns by kind, as the library does: a success releases its value, a failure its error payload, and a refusal nothing, so a duplicate refusal never closes a zero value; a release panic on the receiving or sending side comes back as an error, as the library's in-release defect does. Its release sites match the library's table in Q2, except the state release: the baseline states hold no resource. An agent callback receives the actor's context, so its waits observe the stop. The drain answers queued requests "stopped" and recovers each release's panic, in order; the loop's panic is recovered too, and stop waits. The machine baseline runs the same Breaker step logic, releases each event after its step and returns the same decision values. [bench_matched_contract_test.go](../../prototypes/actorlib/bench_matched_contract_test.go) checks each clause, including the round-2 probes `TestMatchedSendRejectsCanceledCaller` and `TestMatchedAgentCallbackObservesStop` and the round-3 probes `TestR3MatchedResourceResultDoesNotPanicOnRefusal` and `TestR3MatchedCanceledFailureIsReleased`, beside the library control `TestR3LibraryResourceResultControl`. Go has no masks, and a context cancellation never interrupts a `Close`, so the baseline's releases complete without one. Neither baseline copies at the boundary: `int64` and the breaker's states need no copy, which a compiler knows statically. The library's run-time copy decision is measured on its own below.

**Round 4, at `c4540338`, paired.** The round-3 review could not reproduce round 3's ratios (it measured 1.19x, 1.18x, 1.34x, 1.70x and 1.11x) and named the cause: shared load, and each benchmark's ten runs back to back, so the library and its baseline never ran under the same load. Round 4 therefore runs the library and both baselines once per round, ten rounds in turn, from one test binary (`bench-r4-paired.txt`, one-minute load 14.3 at the start and 15.5 at the end), and takes the ratio within each round. The table gives the median of the ten paired ratios and their range. The range is the noise bound that this box allows; a difference smaller than it is not measured.

| Operation | Library / matched, paired median | Range of the ten pairs | Library / raw, paired median | Range |
| --- | --- | --- | --- | --- |
| Send of a message with no reply, handled | 1.29x | 1.11x to 1.56x | 1.22x | 1.13x to 1.46x |
| Ask round trip (counter `Get`) | 1.25x | 0.87x to 1.41x | 1.40x (1.19x against a raw reply that carries an `error`) | 1.24x to 1.69x |
| Start and Stop of an idle actor | 1.48x | 1.26x to 2.25x | 1.60x | 1.24x to 2.01x |
| Machine-backed Ask (breaker, `Success`) | 1.60x | 1.30x to 1.82x | 2.01x, against the raw Ask | 1.81x to 2.36x |
| Agent Call | 1.16x | 0.77x to 1.28x | 1.47x, against the raw Ask | 1.24x to 1.69x |

Allocations do not depend on load. Library against matched baseline, allocs/op and B/op (`bench-r4.txt`): Send 0 and 0; Ask 2, 152 B against 3, 168 B; Start and Stop 11, 3376 B against 10, 2936 B; machine-backed Ask 5, 224 B against 4, 200 B; Agent Call 4, 200 B against 4, 200 B. The raw baseline: Send 0; Ask 1, 128 B (2, 136 B with an `error`); Start and Stop 6, 2576 B. The 16 more bytes for Start and Stop than in round 3 are the loop's `lent` field.

**Round 4 changed no hot path (measured).** `bench-r4-vs-r3.txt` runs the round-3 head (`eb48716d`) and the round-4 code (`c4540338`) in turn, ten rounds (one-minute load 11.3 at the start and 8.3 at the end). Round 4 against round 3, paired median (range): Send 0.99x (0.90x to 1.16x), Ask 1.02x (0.94x to 1.27x), Start and Stop 1.01x (0.73x to 1.52x), machine-backed Ask 0.97x (0.88x to 1.12x), Agent Call 1.02x (0.86x to 1.15x), with the same allocations. The one release path adds work only where something is released. So the change in ratios from round 3 comes from the run method and the load, not from round 4's code.

The unpaired run at the same commit (`bench-r4.txt`, one-minute load 10.9 at the start and 13.7 at the end), median (min) ratio against the matched baseline: Send 1.29x (1.21x), Ask 1.22x (1.22x), Start and Stop 1.01x (1.10x), machine-backed Ask 0.94x (1.06x), Agent Call 1.10x (1.14x). Its machine-backed and start-and-stop ratios fall outside the paired ranges, which shows the error of the unpaired method. The submitted round-3 run (`bench-r3.txt`) gave 1.26x, 1.12x, 1.24x, 1.46x and 0.91x by the same method; its Agent Call result below 1 was not reproduced, and it is a result of that run only. Round 2's matched ratios, against the weaker round-2 baseline, were 1.31x, 1.20x, 1.56x, 1.66x and 1.02x (`bench-r2.txt`). Library against the raw baseline, earlier rounds by the unpaired method: Send 1.18x, 1.24x and 0.86x (rounds 3, 2 and 1); Ask 1.33x, 1.39x, 1.15x; Start and Stop 1.96x, 1.54x, 1.52x; machine-backed Ask 1.79x, 2.03x, 1.45x; Agent Call 1.29x, 1.47x, 1.38x. Round 1 had none of the later guarantees.

A first round-3 version ran the whole loop exit through an rt owner scope. It cost two allocations and a stack growth on every stop (Start and Stop 13 allocs/op, 5343 / 6473 ns/op, in a run under load 10.8 to 15.4). The committed version enters the scope only when there is a panic or something to release, and an agent call skips the copy decision, because a call holds only handles (5 to 4 allocs/op).

The copy rule's cost (review M3), measured on its own. The "static" rows are the copy a compiler emits for the same static type:

| Value | Run-time copy (`snapshot`) | Static copy |
| --- | --- | --- |
| `int64` | 15.2 / 16.8 ns/op, 0 allocs/op | none: 0 ns |
| counter message union holding `Inc` | 31.1 / 36.4 ns/op, 0 allocs/op | none: 0 ns |
| `[]string` of 8 | 298.1 / 331.6 ns/op, 3 allocs/op, 176 B/op | `slices.Clone`: 112.1 / 140.4 ns/op, 1 allocs/op, 128 B/op |
| struct with an `int64` and that slice | 563.2 / 641.7 ns/op, 4 allocs/op, 216 B/op | field copy and `slices.Clone`: 181.8 / 197.4 ns/op, 1 allocs/op, 128 B/op |

Min / median over 10 runs, `bench-r4.txt`.

Where the library's extra time against the matched baseline is, by allocation profile and by the copy table (measured, except where marked):

- **Send.** The run-time copy decision for the message union is 31 to 36 ns of a gap of about 30 to 40 ns; the compiler emits no copy for `Inc`.
- **Ask, one allocation fewer.** Two copy decisions (the message and the `int64` reply, about 50 ns), and `rt`'s task operations on the asker's side (inferred). `Reply` is one pointer, so `Get{Reply}` boxes into the `CounterMsg` interface without an allocation, and the slot's outcome holds no pointer, so its channel is one allocation; the matched baseline's slot holds an `error` and allocates its buffer apart.
- **Start and Stop, one allocation more.** The actor's scope and `rt`'s task handle, done channel and stop channel against the baseline's goroutine and `context`; and the interruption reason that `rt` appends when the stopped task ends. The last one is a runtime finding for #13 and #207: a lone interruption could be a flag, not an appended reason.
- **Machine-backed Ask, one allocation more.** The event travels in the machine's private envelope union, boxed into an interface (the matched baseline sends a concrete struct). Two copy decisions walk the event and the decision by reflection, because both are unions.
- **Agent Call** matches the baseline's allocations; its paired ratio is the lowest of the five, and its range includes 1.

Candidate optimizations, not measured: a per-task reusable reply slot with a sequence number would remove the slot allocations from `Ask` (pillar 7); a compiler that knows the static types removes every copy decision above; lowering a one-type error row to that concrete Go type, as this prototype does for `Reply`, keeps outcomes pointer-free.

## Feature table

Every feature the library uses, and where the spec has it.

| Feature | Used for | Spec | Status |
| --- | --- | --- | --- |
| Generic functions with type and row parameters | `Start`, `Ask`, `Call` | [types.md Generics](../spec/types.md#generics) | Designed |
| Generic types | `Reply[T, E]`, `Address[M]`, `Actor[M, E]`, `Next[S]` | [grammar.md Declarations rule 4](../spec/grammar.md#declarations) ("generic type parameters") | Designed |
| Generic types with row parameters; omitted trailing row is empty | `Reply[T, E]`, `Actor[M, E]`, `Machine[S, E, O, F, R]` | none | **Gap A6** (#135, #196) |
| Unions and type switches | message unions, `Decision`, `Refusal` | [types.md Unions](../spec/types.md#unions) | Prototype |
| Function types with rows; escaping function parameters | the handler | [types.md Function values](../spec/types.md#function-values) | Designed; the escaping spelling is **Gap A10** (#196) |
| Borrows `*T` on parameters and receivers | agent methods, `Call`'s `f` | [types.md Borrows](../spec/types.md#borrows) rules 1 and 4; R17, R30, R33 | Designed (the argument side, `&s`, follows Go) |
| `go`, `Task`, `Stop` | the actor's task | [runtime.md Tasks](../spec/runtime.md#tasks) | Designed |
| `Task.Stop` returns the typed failure of a task that ended first | `Actor.Stop() error[E]` | none | **Gap A9** (#207, #13) |
| Channels, `select`, interruption points on channel operations | mailbox, reply slot, waits inside `f` | [runtime.md Channels](../spec/runtime.md#channels), I1 | Designed |
| `sync.RWMutex`, `sync.Latch` | admission fence, stop signal | [stdlib.md Sync](../spec/stdlib.md#sync-and-stm) (#208) | Designed (`RWMutex` is Go's) |
| Resources, automatic release, consume | `Reply`, `Actor` | [resources.md](../spec/resources.md) R1, R5, R19, R20 | Designed |
| Consuming parameters and receivers | `Reply.Send`, `Address.Send` | R19 lists names only | **Gap A1** (#197) |
| Resource unions | message unions with a reply | none | **Gap A2** (#197) |
| Channels of resources, both halves | mailbox of messages with a reply; release of an unreceivable answer | README Gap 30 (later version) | **Gap A3** (#197, #64) |
| Consuming a resource field in place | `m.Reply.Send(v)` | R39 forbids moving a field | **Gap A4** (#197) |
| Owned parameters in function types | the handler owns `m` | none | **Gap A5** (#196, #197) |
| Failures as values | the reply slot | none | **Gap A7** (#196) |
| Release of a value typed by a type parameter | the drain, a generic state | none | **Gap A8** (#197; lowering #190) |
| Capture of requirements at construction | `Start ... uses R`, a machine's step | README Gap 17 | Designed |
| Value semantics of messages | snapshots | [types.md Structs rule 6](../spec/types.md#structs) | Designed |
| The copy at every task boundary, and its lowering for slices and maps | messages, replies, agent results, machine snapshots | runtime.md Tasks rule 6 covers `go` captures | **Gap A11** (#196, #81, #211) |
| Shared handles by structure | `Address` copies into tasks | runtime.md Tasks rule 6 names actor handles | **Gap A10** (#196) |
| The type of a machine value | `StartMachine` reads `O`, `F`, `R` | runtime.md item 1 has `machine[S, E]` | **Gap A12** (#196, #57) |
| A boundary releases a successful result it discards by stop-wins | an answer taken just before the asker's task stops; a callback result discarded by `rt.Scoped` or beside an interruption | runtime.md Exit causes rule 5 says only that stop wins | **Gap A13** (#13, #207; lowering #190, #197) |

A8 in detail: the drain and the loop's state release handle a value of type `M` or `S` that may be a resource. The checker must treat a type parameter's value as possibly a resource. The lowering here checks for `Close` dynamically, on the cold release paths (`drop`, [actor.go:578](../../prototypes/actorlib/actor.go)), and on the reply path only to release an answer nobody received. Go's GC-shape stenciling has no per-type drop glue, so a hot generic release would need the dictionary or an extra function argument.

## Route 2: what it adds and what route 1 loses

| | Route 1 (library) | Route 2 (actor syntax) |
| --- | --- | --- |
| Typed Ask | the message's reply type; Go's checker rejects a wrong reply (measured) | derived from method signatures |
| Reply exactly once | resource rules A1 to A5 (designed); runtime first-answer-wins, release-answers and release of an unreceivable answer (measured); agents by construction | by construction: the method's return is the reply |
| Interface-shaped protocol | agent calls on a lent `*S` (code, not data) or a written message union | derived message union (data) |
| Source size, handler actor | message union 2.65x to 3.38x route 2's tokens; agent used inline 1.66x (1.54x with #214), definitions only 1.36x, named facade 2.68x | baseline |
| Source size, machine actor | 1.06x | baseline |
| New syntax | none | an `actor` declaration, `spawn`, `post`, and field assignment inside methods |
| Compiler knowledge | none | message derivation, client generation, reply plumbing |
| Userland providers | any library can write another `actor`-like package on the same features (lawful-protocol rule) | only the compiler's form |
| New general rules | A1 to A13, each with clients beyond actors: tasks in channels, queues (#64), `tasks.Set`, RPC replies (#53), batch waiters (#68), every `go` capture (A11), every machine provider (A12), every task result (A13) | none for replies; still needs A3 and A11 for other messages |

Route 2's only unique gain is brevity for handler actors. Under construct admission ([GLOSSARY](../../GLOSSARY.md), "Construct admission"), a construct must make bad code unrepresentable where a library cannot. Every bad case here is rejected by the library route: a wrong reply type (measured), a wrong failure type (measured), a double answer (designed, R20), a dropped answer (answered `NoReply`), a send after the stop request (`Stopped`, measured), a leaked message on stop (released once, measured), a leaked late answer (released once, measured, except the unsuccessful result handoffs that A13 closes), an aliased snapshot (copied, measured) and an escaping borrow of agent state (designed, Borrows rules 1 and 4). So route 2 does not pass admission for any part.

## Decision table

| Option | Guarantees | Cost | Choose? |
| --- | --- | --- | --- |
| 1a. Library, message union with `Reply` resource | all, with A1 to A13 | 2.65x to 3.38x tokens of route 2 for handler actors; Ask 1.25x the matched Go baseline (paired median), with one allocation fewer | **Yes**, default for data protocols |
| 1b. Library, agent: methods on a lent `*S` and one `Call(a, func(r *S) (T, error[E]))` | all; reply once by construction; needs no resource gap | calls are code, not data; `f` has an empty `uses` row but may wait; used inline, 1.66x tokens of route 2 (1.54x with #214); Call 1.16x the matched baseline (paired median, range 0.77x to 1.28x) | **Yes**, for method protocols (owner, 2026-10-27) |
| 1c. Library, `Reply` as a plain value (no A1 to A5) | typed Ask; a dropped reply hangs the caller until the actor stops or the caller's timeout (Gleam's behaviour) | weaker; no new rules | Fallback only if #197 rejects A1 to A5 |
| 1d. Agent methods as values: `actor.Call(room, (*Room).Join, name)` with variadic generics over the argument list | as 1b | checker pack expansion; no Go lowering except one copy per arity (monomorphisation) or boxing the arguments; harder signatures to read and to show in errors (pillars 3 and 6); TypeScript still types Effect's `pipe` with overloads despite variadic tuple types; Gleam, ReScript, Elixir and Go have none | **No** (owner, 2026-10-27). #214 removes the spelled literal types instead |
| 1e. Fixed-arity overloads `Call0`, `Call1`, `Call2` | as 1b | one function per arity, a ceiling on arguments, and no gain over a literal once #214 lands | No |
| 2. Actor syntax | all, reply once by construction | new syntax and compiler-known derivation; userland cannot provide another form | No: no guarantee needs it |

## Gaps handed on

A1 to A13 are spec README Gaps 35 to 47 ([README](../spec/README.md#gaps)).

- **#197 (linearity):** A1 consuming parameters and receivers, A2 resource unions, A3 channels of resources with both halves, A4 consuming a resource field in place, A5 owned parameters in function types (with #196), A8 release of a type-parameter value.
- **#196 (checker):** A6 generic types with row parameters and empty-row elision (with #135), A7 failures as values (`Exit[T, E]`), A10 the escaping function-parameter spelling and the structural shared-handle rule, A11 the copy at every task boundary (with #81 and #211 for its cost), A12 the type of a machine value (with #57).
- **#13 and #207 (runtime):** A13 a task or scope boundary releases a successful result it discards by stop-wins (`rt.Run`, `rt.Scoped`); the reply guarantee waits on it across unsuccessful result handoffs (Q2). #190 and #197 own the lowering half: which value an unsuccessful function exit still owns. A9 `Task.Stop`'s result. `rt.Handle.Stop` drops the typed failure of a task that ended before the stop; `actorlib` uses `Task.Interrupt` and `Handle.Wait` instead. Also: a lone interruption allocates a cause reason, one allocation per stopped task.
- **#214 (checker):** function-literal parameter types from the expected type, which shortens every agent call.

## Prior art

Pins are the commits read on 2026-10-27. Paths are relative to each repository.

- **Gleam OTP** (`gleam_erlang` `dfa7cd70`, v1.3.0 plus 9; `gleam_otp` `18b78224`, v1.3.0 plus 8). `Subject(message)` is an opaque `Subject(owner: Pid, tag: Dynamic)` (`src/gleam/erlang/process.gleam:78-81`). `process.call(subject, waiting: timeout, sending: fn(Subject(reply)) -> message) -> reply` (`process.gleam:693-699`) is the shape of `Ask`. Choice made: the reply address is in the message, typed by the request function. Gleam lowers to Erlang: `send` is `erlang:send(Pid, {Tag, Msg})`, and `call` makes a fresh subject, monitors the callee and selectively receives the reply or the monitor's `DOWN` (`process.gleam:603-634`), with the tagged selective receive in its Erlang FFI (`src/gleam_erlang_ffi.erl:52,82`). A callee that never answers makes the caller panic at the timeout. Effra answers a dropped reply at once with `NoReply`, because release is deterministic. `actor.continue`/`actor.stop` (`src/gleam/otp/actor.gleam:185,193`) are `Continue`/`End`.
- **Akka Typed** (`akka-core` `838fe727`, v2.10.24 plus 3). `ask[Res](replyTo: ActorRef[Res] => Req)` (`akka-actor-typed/src/main/scala/akka/actor/typed/scaladsl/AskPattern.scala:108`): the same reply-address-in-message typing. A send to a stopped actor goes to dead letters; Effra returns `Stopped`.
- **Erlang/OTP** (pin `516126e9`, [PRIOR_ART](../../PRIOR_ART.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)). `gen_server:call` tags a timed request with a process alias deactivated on timeout (`lib/stdlib/src/gen.erl:L251-L280`), so no stale reply reaches a later call. Effra gets the same from a fresh reply slot per request, and releases the stale reply. `gen_statem` is the prior art for the machine-backed actor.
- **Elixir** (pin `79e8784f`, [PRIOR_ART](../../PRIOR_ART.md#standing-comparison-languages-elixir-with-erlangotp-and-moonbit-2026-10-09)). `receive` is a special form (`lib/elixir/lib/kernel/special_forms.ex:2401`, read at `91ee75bb`). `send` and `spawn` are ordinary `Kernel` functions (`kernel.ex:1207,1247`), but the compiler does work on them too: `elixir_rewrite.erl:138-141` inlines `Kernel.send/2` and `Kernel.spawn/1,3` to the Erlang BIFs, so a call costs no extra function frame. Those are host optimizations of library functions, not a reason to make the operation a construct; an Effra lowering may inline `actor.Send` the same way. `Agent.get_and_update/3` (`lib/elixir/lib/agent.ex:385`) is the agent form: the server calls the function on its state (`lib/elixir/lib/agent/server.ex:20`); nothing types it as pure. Elixir lowers processes to BEAM processes; the library is plain modules over `GenServer`.
- **ReScript** (pin `30ce3698`, [PRIOR_ART](../../PRIOR_ART.md)). No actor or process construct: it compiles to single-threaded JavaScript and uses promises. Its lesson is representation and lowering. Variants are not all tagged host objects: constant constructors lower to strings, and the empty list to `0` (`compiler/core/lam_compile_const.ml:53`); constructors with arguments lower to tagged objects. Self tail calls lower to `while` loops (`compiler/core/lam_compile.ml:414`). Effra's lowering makes the same kind of choice per type: a union variant with no fields or one pointer field boxes into a Go interface without allocating, and `Reply` is one pointer for that reason (Q8).
- **Swift** (Swift 5.5, SE-0306 actors and SE-0302 `Sendable`). The syntax route: the compiler knows `actor` because it checks isolation of shared mutable state across `await`. Effra does not share mutable state with an actor: the state is a value owned by the actor's task, an agent lends it to one call at a time, and messages copy or move. So the guarantee Swift needs syntax for comes from value semantics, borrows and resources here.
- **Pony** (`ponyc` `f032e36a`, 0.77.1 plus 73). Behaviours (`be`) and reference capabilities (`iso` for sendable data), for example `actor Promise[A: Any #share]` with `be apply(value: A)` (`packages/promises/promise.pony:3,13`). The syntax route again, buying data-race freedom through capabilities; Effra gets it from value semantics, R17, A3 and A11.
- **Rust Tokio actor pattern** (`tokio` `b980c95d`, 1.54.1 plus 36; Alice Ryhl, "Actors with Tokio", 2021). A handle with an `mpsc` sender, a task, and a `oneshot` per request. `oneshot::Sender::send(mut self, t)` consumes the sender and returns `Err(t)` when the receiver is gone (`tokio/src/sync/oneshot.rs:622`); its `Drop` completes the channel so the receiver gets `RecvError` (`oneshot.rs:871-874`); the receiver's `Drop` drops a completed value (`oneshot.rs:1238`). This is exactly `Reply` and the slot: consume to answer, release to refuse, and an answer nobody receives is dropped.
- **Go actor libraries.** Proto.Actor Go (`protoactor-go` `288962e5`): `RequestFuture(pid *PID, message interface{}, timeout)` (`actor/root_context.go:144`). Ergo (`ergo` `224fdede`): `HandleCall(from gen.PID, ref gen.Ref, request any) (any, error)` (`act/actor.go:28`). Both type messages and replies as `any`, so a wrong reply is a run-time assertion failure. Effra's generic `Reply[T, E]` makes it a compile error.
- **Effect cluster `Entity`** (`effect-smol` `3a1128c7`, effect 4.0.0-beta.98). An entity's protocol is an `RpcGroup` value (`packages/effect/src/unstable/cluster/Entity.ts:80`), with explicit `mailboxCapacity` and `concurrency` options (`Entity.ts:140-141`). The client is a TypeScript mapped type over the group: library code with a general type feature Effra does not have. So Effra's data protocol is the user's message union, and a later durable provider encodes that union. Effect's `pipe` is typed with fixed-arity overloads, not variadic generics, which supports option 1d's rejection.
- **Koka and OCaml 5.** Effect handlers can implement actors as a library; OCaml 5 does not track effects in types, so a missing reply is not typed. They add no choice beyond Effect's here.

## What this spike does not claim

- No Effra checker accepts these sketches yet. Each sketch is **Proposed**. The Go lowering stands in for the emitted code.
- No route-2 syntax is proposed. The route-2 sketches exist only to measure size.
- The run-time copy in `snapshot.go` is a stand-in for per-type emitted copies. Its numbers bound the cost from above; they are not the compiler's cost.
- A successful result discarded by stop-wins at a boundary is not yet released: an answer taken by an asker whose task stops before its boundary, a callback result zeroed by `rt.Scoped`, and a candidate result beside an interruption. That needs A13 (#13, #207; lowering #190, #197). Three tests pin the current count.
- No supervision, durable address, persistence or distribution. `actor.Run` under `tasks.Set` keeps its runtime.md item 6 meaning and is not prototyped here.
- No byte budgets (`docs/specs/actors.md` earlier plan). The mailbox has an item capacity only.
- Self-call deadlock (a handler that asks its own actor) is not detected. It is a #13 item.
