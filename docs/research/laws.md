# Laws: obligations types cannot state, their discharge and their evidence

**Status:** design note and decision record for the spike in [#125](https://github.com/cevr/effra/issues/125), revision 2, 2026-10-12. It changes no compiler behaviour. Revision 2 answers review 1 (seven Major and seven Minor findings), adopts the coordinator's decisions on Q2 and Q8, and adds the owner's 2026-10-12 directions on guardrails, portable providers and readability.

- **Line references** are at `main` `13e62ad4` unless stated otherwise. This branch has merged `main` at `8dca628a`.
- **The prototype** is on branch `wf10/spike-laws-proto`, head `63164e8a`, four commits on `13e62ad4`:
  - `8e781dc3`: the construct, runner and evidence;
  - `e239e708`: the host evidence boundary;
  - `5f428883`: the end-to-end run (revision 1's head);
  - `63164e8a`: the review-1 fixes and their probes.

  That branch is never merged.
- **Receipts.** The TypeScript boundary probe and every receipt quoted here are under [`benchmarks/laws/`](../../benchmarks/laws/). A dated [language evolution preview](../previews/2026-10-12-laws.md) shows the surface.

Two inputs are cited by section, not linked:
- spike-where's [`docs/research/where-dispatch.md`](where-dispatch.md), on `main` since `8dca628a`. Its §2.10 hands #125 four evidence requirements and an expected-failure table. It names the contract `Ordering`; this note calls the same contract `Order`, after Effect's `Order` module.
- spike-rows' `docs/research/effect-rows-as-types.md`, on `main` since `effb24d4`.

## Owner direction

- **2026-10-08, `docs/design.md:42`.** `law` / `contract` is admitted, not implemented: "a visible construct for service and runtime laws, written `law name() uses {...} { ... }` inside a service or contract. A law suite checks the laws, and a refuted law refuses the build."
- **2026-10-08, the five categories.** `docs/research/lawful-runtime-contract.md` adopts one obligation vocabulary (nominally checked, trusted decision, tested law, refuted, unresolved) for domain ingress, services and runtimes, and says finite tests are evidence, never proof.
- **2026-10-12, laws are a construct** ("laws is for sure a construct"). Laws widen beyond services and runtimes to any behaviour that types cannot state, and they feed GDP evidence. This note extends `design.md:42`; it does not replace it.
- **2026-10-12, on #124.** Alternate runtimes "would be the same laws, I suppose, I would have to follow. So it would be an approximation." Runtime laws move up the queue because "it's part of the language constructs".
- **2026-10-12, the generic surface.** The generic signatures read "more typescript than go … I'm not sure if they look simple". Sketches here use the simplest Go-like spelling (implicit kinds, named constraints in the type-parameter list, no row variables), and **the spelling is provisional pending #135**. This note settles semantics, not spelling (§1.1).
- **2026-10-12, composability** (comment on #125). Every construct must show lossless wrapping, a nesting matrix, first-class recipes, orthogonality and composition laws. For laws: a law-bearing contract composes (a generic type derives its laws from its parts' contracts); laws can state composition algebra; and an ordinary function can wrap a law-checked thing without losing evidence (§8).
- **2026-10-12, coordinator rulings carried from spike-where.** The evidence boundary rule (round 2, N1) is binding, and round 3 adds three more rules: host evidence is never trusted (inward), evidence covers a deep Effra-owned snapshot (stability), and canonical calls use private bindings with frozen companions. Each must be shown with strict tsc (§4.6, §5.2).
- **2026-10-12, guardrails and portable providers.** "The goal is to provide a language that has enough guardrails so that bad code or illegal states cannot be represented … [constructs] need to be designed in a way that … [is] portable across different domains. So [userland] implementations … that may not necessarily be effect runtimes, but are effectual … could have a [userland] implementation that is superior in whatever domain they're in." Laws are the guardrail that lets a userland provider replace a built-in one without changing what the construct means (§7).
- **2026-10-12, readability.** Effra should be "easy to read, easy to understand, should not feel rust like, or Haskell like … expressive, declarative, and explicit". Law bodies therefore use ordinary boolean operators and `if`, not combinator helpers (§1.1).
- **2026-10-12, coordinator decisions on review 1.**
  - **Q8:** with no current passing receipt, minting is refused (an error, not an EF173 note), and the `.d.mts` must not brand.
  - **Q2:** a missing budget is an error.

  Both are decided here (§2.6, §2.10).
- **The construct ladder** (memory `syntax-over-special-library`). Laws are rung 4, a visible construct, because discharge, receipts and build refusal are compiler-special. The compiler may key on `law`, `contract`, `satisfies` and `evidence`, never on a contract's or a method's name.

## Verdict

**Laws are declarations inside a `contract`, a `service` or a `machine`.**
- **Satisfaction.** A `satisfies` declaration binds a subject to a contract with a required, finite case budget.
- **Discharge.** One deterministic property runner discharges the laws per subject. It runs the same cases, from seeds derived from the logical law, on Go and JavaScript, and writes one canonical receipt per target.
- **Evidence.** Evidence is a nominal wrapper over its carrier. Its index is the declaration that a lawful satisfaction binds, resolved by binding identity at the mint site. It exists only while a current passing receipt backs that satisfaction, and a value from the host is re-established on entry.
- **Provider protocols.** A contract whose subjects are providers (services, runtimes, codecs) is a provider protocol. Any provider, built-in or userland, passes the same laws and gets a receipt in the same schema.

The deciding evidence:

1. **The categories need no second vocabulary.** A receipt's `status` is `tested`, `refuted`, `trusted` or `unresolved`. These are the spellings of lawful-runtime-contract's tested law, refuted, trusted decision and unresolved. A law that type-checks is nominally checked. `verified` is reserved, has no category, and is refused until the owner amends the five (§2.1, §2.9).
2. **The prototype discharges two unrelated clients on both targets, with identical receipts** (§6). Go and JavaScript agree on every status, case count, case digest and counterexample (`agree:true`). The compiler holds no `Order`, `compare`, `RoundTrip` or `Codec` literal (§5.3).
   - **A four-law `Order`** (reflexive, antisymmetric, transitive, total):
     - a lawful compare: 7 laws tested, 1 trusted;
     - a broken compare: antisymmetry, transitivity and totality refuted, each with a shrunk counterexample;
     - a compare that is not an order: only the total law refutes it (§1.2).
   - **A JSON codec round trip:** the lossless codec tested, the lossy codec refuted.
3. **Laws erase completely, and evidence erases except at the host boundary.**
   - With and without laws, the Go program, the JavaScript module and the `.d.mts` are byte-identical apart from the source-revision header (§5.1).
   - With and without evidence, the Go program is identical. The JavaScript differs only by the boundary adapters and the brand declarations.
   - An adapter costs about 1.2 µs per call, measured (§5.1).
4. **Every hole review 1 found now has a probe.** Each probe fails on revision 1's prototype (`5f428883`) and is refused or correct on `63164e8a`, on Go and on JavaScript (§6.1). After the fixes, 33 of 33 probe lines pass. Before them, 37 lines fail and 6 pass, and those 6 are:
   - the controls;
   - M1, which is a change to the contract's source, not to the compiler;
   - the JavaScript half of the disagreement build.

   The probes cover:
   - evidence indexed by a shadowing parameter, and generic forwarding;
   - a generic carrier with no snapshot plan;
   - function values, `scope` bodies and pattern binders;
   - `run` and effect exports;
   - builds with no receipt, from another directory, or after an exploration seed;
   - persisted cross-target disagreement;
   - root-seed parsing, entry types, laws as values, and a missing budget.
5. **The host boundary is fail-safe under every listed attack** (§5.2). Under strict tsc 6.0.3, with 0 `any` keywords, each of these is refused or harmless:
   - laundering a reversed compare;
   - spread override, array covariance, method bivariance and property covariance;
   - both aliasing routes;
   - a getter, and a proxy that answers differently on each read (each field read exactly once);
   - an `Object.assign` patch;
   - never fillers, a never rest and an untyped value;
   - `Object` augmentation;
   - a recipe used as evidence.

   The same library with naive trust gives a wrong answer through three of those routes, with no cast.
6. **Tested is not proven, and the note measures it** (§2.11), with a 200-case budget:
   - the broken compare's antisymmetry and totality are refuted under all 9 seeds tried, and its transitivity under 7 of 9;
   - a near-order defect is refuted under 15 of 17.
7. **Laws are provider protocols** (§7):
   - **R1-R15** are Effra laws in a runtime contract, discharged per provider.
   - **Comparison and seeds** key on the logical law, so two providers of one law run the same scenarios, even when each exists on one target only (`GoRuntime` and `EffectRuntime` share seed and digest in the probe).
   - **R11** observes through a harness-owned channel, and a negative control shows that the channel fires.

**Losers, with their reasons kept (§11):**
- **Laws as a library of property tests:** the compiler could neither refuse a build nor gate evidence on them.
- **Laws on receiver methods alone:** no home for laws that relate two methods.
- **Type-directed `Arbitrary` instances users write:** a second dispatch mechanism.
- **Random seeds by default:** gates must be deterministic.
- **A branded inward parameter:** TypeScript's unsoundness delivers non-evidence to it with no cast.
- **Host fixtures as the only form of runtime laws:** each provider re-ports them.
- **Rebindable meaning without laws** (F# builders, Haskell `RebindableSyntax`): a provider can change what a construct means (§7).

## 1. Declaration sites

### 1.1 The spelling is provisional (#135)

The sketches use one provisional spelling, chosen to read like Go:
- type parameters in square brackets with implicit kinds, `contract Order[T]`;
- named constraints in the type-parameter list, `fn sort[T Order](xs: Seq[T])`, as in Go's `[T cmp.Ordered]`;
- grouped parameter types, `law transitive(a, b, c: T)`;
- methods named through the owner module, `T.compare`, as spike-where §2.1 resolves them;
- no row variables.

**Law bodies read as ordinary boolean code** (owner, 2026-10-12: "should not feel rust like, or Haskell like"). Revision 1 used helper combinators, `implies(both(p, q), r)`, which read like Lisp. Revision 2 drops them: a law states its claim with comparison operators, boolean operators and `if`.
- **What works today.** Effra has no `&&`, `||` or `!`: the absent-syntax table, `internal/compiler/absent_syntax.go:92`, refuses `!`. A conditional claim is therefore an `if` whose `else` branch is `true`, which is how the prototype writes every law (§1.2).
- **Proposed for #135: `and`, `or` and `not` as boolean keywords.** They read as English, and they keep Effra clear of C's symbols:
  - `law total(a, b: T) { T.compare(a, b) <= 0 or T.compare(b, a) <= 0 }`;
  - `if T.compare(a, b) <= 0 and T.compare(b, c) <= 0 { T.compare(a, c) <= 0 } else { true }`.
- **Proposed alternative for #135: a `when` guard.** `law transitive(a, b, c: T) when T.compare(a, b) <= 0 and T.compare(b, c) <= 0 { T.compare(a, c) <= 0 }` reads as "for these inputs, this holds".
  - A case whose guard is false is a vacuous pass. It counts against the budget, and it is never discarded and regenerated, so a guard cannot loop or silently shrink a run.
  - The receipt records how many cases met the guard, so a law whose guard almost never holds is visible.
  - QuickCheck's `==>` discards instead, and gives up after too many discards. Effra prefers the count, because a gate must finish.

The preview (`docs/previews/2026-10-12-laws.md`) shows these forms side by side for the owner.

The prototype parses an older spelling: `contract Order<T>`, `fn(T, T) -> i64` entries bound by name in the satisfaction, and a `mint` keyword. The table maps one to the other. #135 decides how users write it; it cannot change what the checker checks. spike-where's sketch name `Ordering<T>` with `where T: Ordering` is this note's `Order`.

| Concept | Provisional sketch | Prototype (`wf10/spike-laws-proto`) |
| --- | --- | --- |
| Contract | `contract Order[T] { T.compare: fn(T, T) -> i64 … }` | `contract Order<T> { compare: fn(T, T) -> i64 … }` |
| Law | `law transitive(a, b, c: T) { … }` | `law transitive(a: T, b: T, c: T) { … }` |
| Effect law | `effect law roundTrip(value: T) raises {…} uses {…} { … }` | same; `uses` names the harness's built-in test providers only (§1.8) |
| Satisfaction | `User satisfies Order cases 200` (methods resolved through User's owner module) | `User satisfies Order<User> cases 200 { compare = compareUsers }` |
| Target-bound provider | `GoRuntime satisfies Runtime cases 50 target go` | same (§7) |
| Trusted or unresolved law | `trusted transitive: "why"` inside the satisfaction | same |
| Evidence kind | `evidence SortedBy[S] by Order.compare` | `evidence SortedBy<S> by Order.compare` |
| Minting | `SortedBy[Trio, User.compare](sorted)`, construction in the evidence's owner module | `mint SortedBy<Trio, compareUsers>(sorted)` |
| Admission | `admit SortedBy[Trio, User.compare] by sortedTrio` | same, angle brackets |
| Requiring evidence | `fn search(xs: SortedBy[Trio, User.compare], x: User) -> bool` | angle brackets |

### 1.2 On a contract: Order and Eq

Spike-where §2.10 recommends that laws attach to a named contract that groups `where` entries, satisfied by a declaration in the type's owner module. This note adopts that.

```
// provisional spelling (#135); `and` and `or` are proposed (§1.1)
contract Eq[T] {
    T.equals: fn(T, T) -> bool
    law reflexive(a: T) { T.equals(a, a) }
    law symmetric(a, b: T) { T.equals(a, b) == T.equals(b, a) }
    law transitive(a, b, c: T) {
        if T.equals(a, b) and T.equals(b, c) { T.equals(a, c) } else { true }
    }
}

contract Order[T Eq] {                       // a named constraint: Order refines Eq
    T.compare: fn(T, T) -> i64
    law antisymmetric(a, b: T) {
        if T.compare(a, b) <= 0 and T.compare(b, a) <= 0 { T.compare(a, b) == 0 } else { true }
    }
    law transitive(a, b, c: T) {
        if T.compare(a, b) <= 0 and T.compare(b, c) <= 0 { T.compare(a, c) <= 0 } else { true }
    }
    law total(a, b: T) { T.compare(a, b) <= 0 or T.compare(b, a) <= 0 }
    law agrees(a, b: T) { (T.compare(a, b) == 0) == T.equals(a, b) }   // relates two methods
}

User satisfies Order cases 200               // in User's owner module
fn sort[T Order](xs: Seq[T]) -> SortedBy[Seq[T], T.compare]
```

**Totality is part of the contract.** Without `total`, a compare that is not an order passes every other law.
- **The hole.** `if a.age == b.age { 0 } else { 1 }` answers 1 in both directions for any two users of different ages. It is reflexive, and in `<=` form it is antisymmetric and transitive.
- **Measured.** `examples/laws/probes/partial.ef` with the total law cut out is tested ×200 on all three laws, and its build, which mints `SortedBy`, is admitted. With the total law it is refuted at `a = User { age: -1, name: "" }, b = User { age: 0, name: "" }` on both targets, and the build is refused (probe M1, §6.1). That compare stays a negative control in L1 and L2.
- **Prior art.** Effect states "total ordering laws" (`Order.ts:29`), and cats (`OrderLaws.scala:29-30`) and hedgehog-classes' `ordLaws` state totality.
- **The spec's three-law minimum** (reflexive, antisymmetric, transitive) was a prototype floor, not the design contract.

- A **contract** lists entries and laws. The entries are the `where` entries spike-where resolves, so `where T: Order` expands to `T.compare` and `T.equals` with no dictionary record (spike-where §2.10).
- A **satisfaction** binds one subject to one contract and states the case budget. It is written in the subject's owner module, so only the owner can claim that its methods are lawful, and coherence is spike-where's orphan rule.
- A refining contract (`Order[T Eq]`) inherits Eq's laws. A law inherited twice runs once, keyed by contract and law name, as discipline merges parent rule sets by name (§9).
- A law that relates two methods (`agrees`) has a home here, which is why laws do not attach to a lone receiver method (spike-where §2.10 (i)).

### 1.3 Conditional satisfaction: laws of a generic type come from its parts

```
// provisional spelling (#135)
fn (p: Pair[A, B]) compare(other: Pair[A, B]) -> i64 where A.compare: fn(A, A) -> i64, B.compare: fn(B, B) -> i64 { … }   // spike-where §2.9
Pair[A Order, B Order] satisfies Order cases 100
Option[T Order] satisfies Order cases 100
```

- **Meaning.** `Pair[A, B]` satisfies Order whenever A and B do. The laws are stated once, in Order.
- **Discharge per instantiation reached.** A law over an abstract `A` cannot be tested, because no generator exists for an abstract type (§2.3). The runner therefore discharges the conditional satisfaction at each concrete instantiation the program or its tests reach, such as `Pair[i64, User]`, with the generator composed from the parts' generators. This is a test-time instantiation only; emitted code still uses one generic body and curried adapters (spike-where §2.9, SH1).
- **Composition of verdicts.** An instantiation is lawful when its own laws are tested or trusted and every part's satisfaction is lawful. A part with an unresolved law makes the instantiation unresolved; a refuted part refuses the build anyway.
- **A library type's own instantiations.** A generic library satisfaction names the instantiations it tests in its own module (`cases 100 at Pair[i64, string], Pair[bool, i64]`), so the library's receipt exists before any caller.
- **Evidence names the composite key.** Evidence minted under a conditional satisfaction names spike-where's canonical resolved key, for example (`Pair.compare`, [`I64.compare`, `User.compare`]).
- **The prototype's form of this.** `examples/laws/order.ef` orders `Trio` lexicographically over `User` and marks Trio's transitivity `trusted` with the justifier "lexicographic composition of Order@User, whose transitivity is tested". Per-instantiation discharge is follow-up L7 (§12).

### 1.4 On a service (`design.md:42`)

```
// provisional spelling (#135)
service Users {
    effect fn save(user: User) -> void raises {DbError}
    effect fn find(id: UserId) -> User raises {NotFound, DbError}
    effect law saveThenFind(u: User) raises {NotFound, DbError} uses {Users} {
        run Users.save(u)
        User.equals(run Users.find(u.id), u)      // `==` stays on primitives (spike-where Q1)
    }
}
PostgresUsers satisfies Users cases 50 using TestDatabase
MemoryUsers satisfies Users cases 200
```

- The subject of a service law is a **provider**, not a type. Each provider satisfies the service and gets its own receipt, which is the service column of lawful-runtime-contract.
- `uses {Users}` is filled by the provider under test. Any other service the law or the provider needs is named in the satisfaction (`using TestDatabase`) and must be a deterministic provider (§1.8).

### 1.5 On a codec: round trip

```
// prototype spelling, examples/laws/codec.ef
contract RoundTrip<T> {
    codec: Codec<T>
    same: fn(T, T) -> bool
    effect law roundTrip(value: T) raises { JsonDecodeFailure, JsonEncodeFailure } {
        let text = run codec.encode(value)
        same(run codec.decode(text), value)
    }
}
accountJson satisfies RoundTrip<Account> cases 200 { codec = accountJson  same = sameAccount }
halfJson satisfies RoundTrip<Profile> cases 200 { codec = halfJson  same = sameProfile }
```

- The subject is a derived codec witness (`derive accountJson = Json.codec<Account>(…)`), not a type, so the law attaches to the value that `derive` made. A codec witness is a declaration, so it has an identity a satisfaction can bind.
- A failure the law raises refutes it, with the failure recorded. The lossy `halfJson` returns `false` instead: it halves the score on the wire, so `score: -1` comes back as `0`.
- Under #100, a derived codec's satisfaction could be emitted with the derive. It stays a written declaration here so the budget is visible.

### 1.6 On a machine: an invariant over reachable states

```
// provisional spelling (#135); docs/specs/state-machines.md's Session
machine Session {
    initial SessionState.Idle
    step sessionStep
    law keyed(state: SessionState) cases 200 depth 16 {
        match state {
            SessionState.Active { key } => key != ""
            SessionState.Idle => true
        }
    }
}
```

- A machine law quantifies its single State parameter over **reachable** states, never over arbitrary generated states.
- The runner generates event sequences up to `depth` from `initial`, applies the step function, and checks the law after every `Go` or `Stay`, as TLC checks each new state against every invariant (§9). `Ignore` and `Reject` leave the state unchanged and are not rechecked.
- **The counterexample is a trace:** the shrunk event sequence with each state it reached, numbered as TLC prints a behaviour. The shrinker shortens the sequence first, then the events.
- Events must be generatable (§2.3). An effectful step needs its services supplied by deterministic providers, as for service laws. Completion outcomes are generated as further events from `complete`.
- This is bounded random walking (TLC's `-simulate`), not exhaustive model checking. A machine law is tested, never verified.

### 1.7 Laws that state composition algebra

Laws can state how constructs compose. The runtime contract that §7 introduces holds them, so every provider passes them:

```
// provisional spelling (#135); observe, Script and the Trace service come from #124's trace model
contract Runtime {
    // Nested deadlines take the minimum (Go's context.WithDeadline states the same law, §9).
    effect law nestedDeadlinesTakeTheMin(outer, inner, work: Millis) uses {Clock, Trace} {
        sameOutcome(
            run observe(sleep(work).timeout(inner).timeout(outer)),
            run observe(sleep(work).timeout(min(inner, outer))))
    }
    // Effect monad laws, over programs described as data (functions cannot be generated, §1.9).
    effect law leftIdentity(x: i64, k: Script) uses {Trace} {
        sameTrace(run observe(succeed(x).flatMap(script(k))), run observe(script(k)(x)))
    }
    // script(andThen(f, g)) interprets as v => script(f)(v).flatMap(script(g)), so no closure is written here.
    effect law associativity(m, f, g: Script) uses {Trace} {
        sameTrace(
            run observe(script(m)(0).flatMap(script(f)).flatMap(script(g))),
            run observe(script(m)(0).flatMap(script(andThen(f, g)))))
    }
    // Provide distributes over sequencing in outcome, not in acquisitions (R13).
    effect law provideDistributesInOutcome(a, b: Script) uses {Trace} {
        sameOutcome(
            run observe(seq(script(a), script(b)).provide(Counter)),
            run observe(seq(script(a).provide(Counter), script(b).provide(Counter))))
    }
}
```

- `sameOutcome` compares exits; `sameTrace` compares exits and trace events under RS1's allowed partial order (lawful-runtime-contract, RS1).
- **A law states exactly which algebra holds.** A `sameTrace` version of the provide law would be refuted, because one build acquires each node once (R13) and two builds acquire twice. Writing the weaker law is the honest statement.
- `Script` is a small enum of steps (log, sleep, fail, succeed with a value) and `script(k)` interprets it. That is how a law quantifies over programs without generating functions, as cats-effect's laws quantify over generated `IO` values (#124's prior art).

### 1.8 What a law body may do

- **A pure law** (`law`) is an ordinary checked pure body returning `bool`. `false` refutes it. A defect (a managed panic) refutes it, with the defect recorded.
- **An effect law** (`effect law`) may `run` effects. Its failures and services are explicit, `raises {…} uses {…}`, as every public effect contract is. A raised failure refutes the law with the failure recorded, which is how a codec's decode failure becomes a counterexample (§1.5). The prototype checks `raises` and `uses` as part of the elaborated law function, and its harness supplies the built-in test providers (`probes/runtime.ef` uses `Assert`). Refusing live providers and naming test providers with `using` are follow-up L3. Revision 1 said `uses` was not parsed, which was wrong.
- **Only deterministic providers.** A law's services are filled by the subject or by providers the satisfaction names, and each must be deterministic: `TestClock`, `TestScheduler`, in-memory stores. `LiveClock` and other live providers are refused, because a law must give the same result on both targets from the same seed (§3.2).
- **One fresh scope per case.** Each case runs in its own scope with a fresh test provider, closed before the next case, so cases cannot leak state into each other.
- **A law is not a value.** It cannot be called, passed or stored. Its name resolves only in receipts, inspection and satisfactions.
  - **Calling it.** The prototype elaborates each law into a function (`law__Order__User__reflexive`), and a call to or reference of that function is refused with EF178: "… is the law Order.reflexive@User, and a law is not a value" (probe m3-call).
  - **Minting.** A law may call any function, but it cannot mint evidence, because a law body states an obligation and is never a construction site. EF181 says so (probe m3-mint).
- **No proofs.** A law body is a predicate, not a proof term. There are no tactics, lemmas or proof objects, which keeps laws from drifting into a proof language (the spec's first risk).

### 1.9 Typed quantified parameters

- Every law parameter is universally quantified and typed: `law transitive(a, b, c: T)`.
- **A parameter must be generatable**, or the law must be `trusted` or `unresolved` with a justifier. EF171 refuses anything else and says what to do (§2.3).
- **Functions are not quantified.** A function value cannot be generated, and Effra has no `==` on functions to compare results. Quantify over data that describes the function (`Script`, §1.7).
- **Resources are not quantified.** A File or a handle cannot be generated. Quantify over the data that produces it (a path, contents) and acquire it inside an effect law from a test provider, so the law's scope owns and releases it.
- **Type parameters are not quantified.** A law over an abstract `T` is discharged at concrete subjects only (§1.3).

## 2. Discharge

### 2.1 The categories, in lawful-runtime-contract's vocabulary

| Receipt `status` | lawful-runtime-contract category | Meaning | Build effect | Inspection and lint |
| --- | --- | --- | --- | --- |
| (none; the law type-checks) | **Nominally checked** | The contract, its laws, the satisfaction's bindings and the law identities type-check. Each binding's signature matches the contract entry's type with the contract's parameters substituted (EF177, probe m2). | None. | Listed with the law. |
| `tested` | **Tested law** | Every case in the budget passed, on this target, for this revision and root seed. | Allowed. | Case count, seed and digest shown. |
| `refuted` | **Refuted** | A case failed. The receipt carries the original and the shrunk counterexample and the failure. | **Refused** (EF170). | The counterexample, with the law named. |
| `trusted` | **Trusted decision** | The satisfaction marks the law `trusted law: "why"`. It is not run. | Allowed. | Justifier shown; the empty justifier is refused (EF002); listed by `ef inspect` as a trusted decision. |
| `unresolved` | **Unresolved / unavailable** | Not run: the satisfaction marks it `unresolved law: "why"`, a generator is missing for a law the satisfaction marks unresolved, or the receipt is missing or stale. | Allowed with EF173, a visible note, **unless evidence rests on it**: then the build is refused (EF174, §2.10). A declared unresolved law blocks minting at check time (§4.3). | Reason shown. |
| `verified` | none | Reserved (§2.9). | Refused as unknown. | — |

The words are the receipt's spelling of the five categories, not a second vocabulary. A receipt for an R law (§7) uses the same field and the same values.

### 2.2 Receipts

**The gating receipt is canonical.** It is the only receipt `ef build` reads, and it is bound to everything that gates:
- **Location.** It lives at `<directory of the resolved source>/dist/<name>.laws.<target>.json`, keyed by where the source is, not by the working directory. Before revision 2, `ef build /abs/…/order-broken.ef` run from another directory found no receipt and was admitted with EF173 beside a `refuted` one (review 1, M6a). Now it reads the refuted receipt and refuses (probe M6-cwd).
- **Root seed.** Only a run at the canonical root seed `0` writes the gating path. A run with `EFFRA_LAW_SEED=N` is exploration: it writes `dist/<name>.laws.<target>.seed-N.json`, which no build reads, and its comparison prints `gating:false`. Before revision 2, an exploration seed overwrote the gating receipt, so a defect that only some seeds find could pass the gate. Two receipts show the change:
  - `runs/m6b-before.txt`, with the old binary: seed 3 overwrites the receipt, and `ef build` exits 0 although seed 0 refutes transitivity;
  - probe M6-seed: the gating receipt keeps the refutation, seed 14 writes its own receipt, and the build is refused.
- **Revision.** `revision` digests the source and its dependencies, not the source alone. `LawBuildVerdict` compares it, which is stronger than the `source` digest that revision 1's text named (review 1, m7). `source` is kept so that receipts from two targets compare, since bundled imports give each target its own revision.
- **Disagreement.** A cross-target disagreement is persisted, not only printed: `ef test --target both` marks every disagreeing law `refuted` in both receipts, with the failure "cross-target disagreement (EF172): …" (§3).

Each receipt carries (prototype `internal/compiler/laws_receipt.go`):
- **Header:** `schemaVersion`, `revision`, `source`, `target` and `rootSeed`.
- **Per law:**
  - `id` (`Contract.law@Subject`);
  - `law`, the logical law (`Contract<Args>.law`, for example `Order<User>.transitive`);
  - `provider` (the subject), and `boundTarget` for a target-bound provider (§7);
  - `status`, `seed`, `cases`;
  - `digest`: FNV-1a over the rendered cases, so two targets prove they generated the same cases, not merely the same verdict;
  - when refuted: `original`, `counterexample`, `failure` and `shrinkEvaluations`;
  - when trusted or unresolved: `justifier`.

**Toolchain and provider revision (Q7, decided for L2).** lawful-runtime-contract requires receipts to bind provider revision, target, toolchain, fixture revision, law IDs, seed and raw outcome. The prototype's receipt binds all but the toolchain (Go and node versions) and the provider revision. L2 adds both to the receipt and to the staleness check, so a receipt made by a different Go or node version is stale.
- **The cost.** A toolchain upgrade re-runs every law program's laws once, which at the measured cost (§3) is about 0.1 to 0.5 s per program per target.
- **Why it is worth it.** A host bug can change a verdict, as the deliberate stack-overflow probe changes JavaScript's (§3).

### 2.3 Generators

**Derivation is structural and by type, decided by the compiler.** There are no user-written `Arbitrary` instances, because that would be a second type-directed lookup beside spike-where's owner-module resolution.

| Type | Generator | Prototype |
| --- | --- | --- |
| `i64` | Size-ramped: small zigzag values while the size is below 16, then a class choice among tiny, ±65 537, a boundary table (`0`, `±1`, `MinInt64`, `MaxInt64` and neighbours) and full 64-bit; with probability 1/4 a value near one already drawn in this case | yes |
| `string` | Length up to `min(size, 8)` over a 13-character alphabet that includes `"`, `\`, a newline, `é`, `€` and `😀` | yes |
| `bool` | One draw | yes |
| record | Each field in field-name order (`User { age, name }` for `record User { name, age }`), so the draw order is the same on both targets whatever order the declaration uses | yes |
| enum | A variant draw, then its fields in field-name order | yes |
| generic data (`Option[T]`, `Pair[A, B]`) | From the type arguments' generators | refused (EF171); L2 after rows 4a |
| recursive data | Size-bounded recursion, the size halving per level | refused (EF171); L2 |
| `pub readonly` validated type | Only through a constructor its owner names (`generate by makeEmail`), discarding failures within the budget; structural derivation must not manufacture validation (opaque-values, "Proposed common surface") | not in the prototype |
| `f64` | After #118 | — |
| sequences and maps | After #120 | — |
| functions, handles, files, effects | Never (§1.9) | refused (EF171) |

**A type with no generator is a compile-time diagnostic, not a runtime throw.** Clojure spec and Effect throw at run time when derivation fails; QuickCheck, proptest and MoonBit refuse at compile time through a missing instance (§9). Effra refuses at check time, on the satisfaction, and names the way out:

```
EF171 law Endo.idempotent@Twice quantifies f: fn(T) -> T, which has no derivable generator (a function value cannot be generated: it has no observable equality and no construction authority); bind a fixture generator or mark the law unresolved with a justifier
```

(`TestLawGeneratorRefusals`; the same source with `trusted idempotent: "…"` checks.) The final message should also name the data-quantification route of §1.9 and `trusted`; the prototype's "bind a fixture generator" names an escape it does not implement (§13 Q6 is the nearest).

### 2.4 Shrinking

- **Choice-sequence shrinking.** Generation records every draw in a choice buffer. The shrinker edits the buffer and regenerates the value from it, keeping an edit only when the law still fails. The passes, in order, are: delete chunks of 8, 4, 2 and 1 draws; set a draw to zero, then binary-search it downwards; lower one draw while deleting the next. The limit is 2 000 law evaluations.
- **Why this design.** It shrinks records, enums and validated types with no per-type shrinker, because every candidate is regenerated through the same generator, so it stays valid. External type-based shrinking (QuickCheck, ScalaCheck, Rust quickcheck, MoonBit) silently does nothing when a shrinker is missing. Integrated rose-tree shrinking (Hedgehog, ZIO, StreamData, Effect) composes through `map` but resets inner progress under `bind` (§9). The choice-sequence form is the design Hypothesis's reducer uses (MacIver and Donaldson, ECOOP 2020; not verified locally).
- **The same on both targets.** Go and JavaScript run one algorithm over one buffer encoding, so the shrunk counterexample is identical (§6).
- **Observed** (revision 1, default seed, `order-broken.ef`). Transitivity first failed at `a = User { age: -6, name: "\\A\"0😀\n" }, b = User { age: -7, name: "\nzz00€ " }, c = User { age: -8, name: "\"c" }` and shrank to `a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }` in 234 evaluations. Antisymmetry shrank from `a = User { age: 1, name: "" }, b = User { age: 0, name: "c" }` in 17. The receipt keeps both the original and the shrunk case. Revision 2's logical-ID seeds (§2.5) draw different cases, but the shrunk antisymmetry and transitivity counterexamples are unchanged (§6).

### 2.5 Seeds

- **A fixed root seed by default** (`0`). ZIO Test and MoonBit fix theirs too (ZIO `TestRandom.DefaultData`, MoonBit `seed? = 37`), while QuickCheck, proptest and fast-check draw a random seed per run (§9). A gate must give the same verdict for the same source, so Effra fixes it.
- **One canonical parse of `EFFRA_LAW_SEED`.** The driver accepts only a decimal uint32 with no sign and no leading zero, and refuses anything else with EF176 before any harness runs. Both harnesses apply the same rule, and the receipt records the canonical text.
  - **Before revision 2,** Go read the value with `fmt.Sscan` and silently fell back to 0, while JavaScript used `BigInt.asUintN`. As a result, `010` and `-1` gave false EF172s, and `abc` crashed JavaScript while Go recorded `rootSeed:"abc"` (review 1, m1).
  - **Now** `010`, `-1`, `abc` and `4294967296` are each refused (probe m1).
- **Per-law seeds come from the logical law.** Each law's seed is `fnv1a32(rootSeed + "/" + law)`, where `law` is the logical ID `Contract<Args>.law` (`Order<User>.transitive`), not the per-provider ID.
  - Adding or removing a law changes no other law's cases.
  - Two providers of one law run the same scenarios, which is what lets §7 compare providers. `accountJson` and `accountJsonAgain` share seed `607786003` and digest `0109b916` (probe M7-providers).
- **The generator is mulberry32 on both targets,** with the same integer arithmetic, so Go and JavaScript draw the same numbers. The shared `digest` in §6 is the receipt.
- **Exploration is separate.** Other seeds run in a non-gating job and write their own receipts (§2.2). A counterexample that exploration finds is pinned as an explicit example on the satisfaction, as proptest checks in its regression seeds (§13 Q6).

### 2.6 Budget

- **The case budget is required** (`cases N`), under "justify, don't ban" (`docs/design.md`, the owner rule of 2026-10-07): there is no silent default. **A satisfaction with a law to run and no budget is an error** (Q2, decided): "EF175 User satisfies Order with laws to run and no case budget: add `cases N`; a budget is a required choice, and `unresolved law: "why"` remains for a deliberate gap" (probe Q2). Revision 1 treated the omission as unresolved, which hid a forgotten budget behind a minting refusal.
- The budget sits on the satisfaction, where the subject is bound, and a law may lower or raise it for itself. A machine law adds `depth`.
- `unbounded` is not a legal case budget: an unbounded run never produces a receipt.
- The shrink limit (2 000 evaluations) and the size ramp (`case % 65`) are fixed analysis budgets, like the compiler's other analysis budgets, not user budgets.

### 2.7 Refuted: the diagnostic

`ef test` prints one line per refuted law and exits non-zero:

```
EF170 law Order.antisymmetric@User refuted after 2 cases (seed 582938622): returned false at a = User { age: -1, name: "" }, b = User { age: 0, name: "" }
EF170 law Order.total@User refuted after 18 cases (seed 1344773583): returned false at a = User { age: 9223372036854775807, name: "" }, b = User { age: 0, name: "" }
EF170 law Order.transitive@User refuted after 172 cases (seed 1953339788): returned false at a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }
```

`compareNear` computes `b.age + 2`, which wraps at `MaxInt64`, so totality fails there; both targets find the same boundary because the generator draws from the same boundary table (§2.3). `ef build` then refuses:

```
EF170 refuted laws refuse the build: Order.antisymmetric@User at a = User { age: -1, name: "" }, b = User { age: 0, name: "" }; Order.transitive@User at …; Order.total@User at …
```

The line names the law and its subject, the case count, the per-law seed, the failure (`returned false`, or the raised failure) and the shrunk counterexample, with each argument named by its law parameter. hedgehog-classes alone also prints the law's equation (§9); Effra's inspection can show the law body from its span, so the line stays one line.

### 2.8 Trusted

`trusted transitive: "lexicographic composition of Order@User, whose transitivity is tested"` inside a satisfaction. The justifier is required and non-empty (EF002 "needs a justifier"). The receipt records it, and `ef inspect` lists every trusted law as a trusted decision, as `dafny audit` lists assumptions and Lean's `#print axioms` lists axioms (§9). A trusted law needs no generator (`TestLawGeneratorRefusals`).

### 2.9 Verified (reserved)

lawful-runtime-contract gives verified propositions no category, because Effra has no proof checker. `verified` is a reserved word and is refused. Admitting it later needs all of:
- an owner amendment that adds a sixth category to lawful-runtime-contract, with its meaning at each boundary;
- a checker whose certificate is re-checked by a small kernel, as Bend's `--verdict` re-checks with a kernel proved in Lean, or Dafny and F\* discharge through an SMT solver with an audited trust base (§9);
- a statement of what the proof assumes about each target, because a proof over mathematical integers does not model `i64` overflow, as MoonBit's `moon prove` documents (§9).

### 2.10 Unresolved

Unresolved has four sources, each with a visible reason in the receipt:
1. `unresolved law: "why"` in the satisfaction;
2. a law that cannot be generated and is marked unresolved;
3. no current receipt on this target: none, a stale revision, or an exploration seed;
4. an unavailable target or provider, for R laws (§7).

A missing budget was a fifth source in revision 1; it is now the error EF175 (§2.6).

**The rule (Q8, decided).** An unresolved law refuses a build exactly when evidence rests on it, and only then.
- **Check time.** Sources 1 and 2 are known when the program is checked. They block minting evidence through that satisfaction at check time (EF181, §4.3).
- **Receipt time.** Sources 3 and 4 are known only when receipts are read. `LawBuildVerdict` collects every satisfaction that gates evidence: one whose bound declaration indexes a mint or an admission in the program. If any of them lacks a current passing receipt for the build target (revision, target and root seed `0` all match, and every law tested or trusted), the build is refused before anything is emitted:

  ```
  EF174 evidence rests on laws with no current passing receipt on js (no receipt): User satisfies Order (for a mint of SortedBy<Trio, compareUsers>, the admission of SortedBy<Trio, compareUsers>); run ef test on this target
  ```

  So no `.d.mts` brands evidence that rests on laws nobody ran (probe M5; before revision 2 the same build was admitted with EF173 and its `.d.mts` declared `SortedBy`).
- **Laws that gate nothing** keep EF173 as a visible note, "laws are unresolved for this revision on go: run ef test to discharge them". This is the justifier lint lawful-runtime-contract asks of an unresolved obligation (`lawful-runtime-contract.md:205-207`); the codec program is the example.
- **Why refuse rather than project the carrier.** Review 1 offered both. Silently dropping the brand would change a library's TypeScript types depending on whether `ef test` ran first, so the same source would project two ways. Refusal keeps one projection and names the command that fixes it.

### 2.11 What tested means

A finite run is evidence, not proof (lawful-runtime-contract). `benchmarks/laws/runs/seed-survey.txt` (from `benchmarks/laws/proto-receipts.sh`, at `63164e8a`) runs two programs under the default and exploration root seeds with a 200-case budget. Each exploration run writes its own receipt, and the script fails if the gating receipt ever stops carrying root seed `0`.

The broken compare, `order-broken.ef`:

| Root seed | antisymmetric | transitive | total |
| --- | --- | --- | --- |
| default (0) | refuted at case 2 | refuted at case 172 | refuted at case 18 |
| 1 | refuted at 6 | refuted at 56 | refuted at 21 |
| 2 | refuted at 24 | refuted at 57 | refuted at 37 |
| 3 | refuted at 2 | refuted at 50 | refuted at 35 |
| 4 | refuted at 1 | tested ×200 | refuted at 32 |
| 5 | refuted at 2 | refuted at 19 | refuted at 21 |
| 6 | refuted at 3 | tested ×200 | refuted at 48 |
| 7 | refuted at 2 | refuted at 105 | refuted at 58 |
| 8 | refuted at 6 | refuted at 80 | refuted at 40 |

**A defect only some seeds find** (`examples/laws/probes/near.ef`). Users within one year of each other compare equal, so only transitivity fails, and only for three ages that step down by one.
- **Where it is found.** It is refuted under the default seed (at case 172) and under 14 of seeds 1 to 16.
- **Where it is missed.** Seeds 14 and 15 miss it, so 15 of 17 runs find it.
- **Why that matters.** This is the case an exploration seed must never be allowed to gate (probe M6-seed).

**Reading the numbers.**
- **Agreement.** Go and JavaScript agree on every row.
- **How the defect is found.** The near-value pool (§2.3) is what finds these defects. Their failures need ages within two of each other, which a 64-bit generator rarely draws on its own.
- **What a gate catches.** A gate that runs the default seed catches both defects. A different defect could need more cases or an explicit example.

That is why a receipt says `tested` and never `proven`.

## 3. When laws run

| Command | Runs laws? | What it does with them |
| --- | --- | --- |
| `ef check`, LSP, MCP | **No** | Checks declarations and generators (EF171), and reports each law's status from the gating receipt for the current revision. Inspection never executes user code, so CLI and MCP keep one semantic model. |
| `ef test` | **Yes**, on the selected target | Runs every runnable law beside the tests, writes the receipt (the gating path at seed 0, its own path for an exploration seed), prints EF170 per refutation and exits non-zero. A program with laws and no tests is accepted. |
| `ef test --target both` | **Yes**, on Go and JavaScript | Removes stale receipts, runs both, compares the receipts, persists any disagreement as `refuted` in both, and reports it as EF172. |
| `ef build`, `ef run` | **No** | Reads the gating receipt beside the source. A refuted law refuses (EF170). A missing, stale or exploration-only receipt refuses when evidence rests on it (EF174), and otherwise prints EF173 and builds. A build never executes user code. |
| The gate | **Yes**, through `ef test --target both` on every program with laws | Refutations and disagreements fail the gate. |

**Same cases on both targets.** The harness generates each case from the per-law seed in the target's own code, with the shared algorithm (§2.4, §2.5), so the targets need not exchange cases. The `digest` field proves they generated the same ones.

**A cross-target disagreement is a refutation.** If the Go and JavaScript receipts differ in any law's status, case count, digest or counterexample, the law is refuted for both targets: one of them computes a different function from the same source, which is the unchecked-passthrough class of defect Effra refuses. The prototype detects it (`CompareLawReceipts`, EF172), then records it (`RecordDisagreements`), so that a later `ef build` reads `refuted` on either target.
- **A real disagreement.** `examples/laws/probes/deep.ef` makes one. Its law recurses up to 200 000 deep without a tail call, which overflows JavaScript's stack and not Go's. Go reports `tested`, JavaScript a defect, and both receipts end `refuted` with the failure "cross-target disagreement (EF172): status differs (go "tested", js "refuted"); cases differs …". `ef build` is refused on both targets (probes M6-disagree and M6-disagree-build).
- **Before revision 2.** The disagreement was printed and no receipt changed, so the Go build was admitted (review 1, M6c).
- **The synthetic fault control** in `TestCompareLawReceiptsReportsDisagreement` is kept.

**Cost, measured.** `benchmarks/laws/proto-cost.sh` writes `benchmarks/laws/runs/cost.txt`: 11 samples per series, every raw sample, and the conditions (load average 9 to 24 on 16 CPUs, go1.27.0, node 24.11.1, 2026-10-12).
- **The law program.** The two-provider codec program (`probes/providers.ef`, 400 cases), plus one trivial `test_` function.
- **The control.** The same file with its `contract` and `satisfies` blocks blanked, plus the same `test_` function.
- **Cold** means after a source edit, so the revision is new; **warm** means repeated.

| Series (median seconds) | With laws | Control | Laws add |
| --- | --- | --- | --- |
| `ef test`, Go, cold | 0.473 | 0.370 | 0.10 |
| `ef test`, Go, warm | 0.109 | 0.083 | 0.03 |
| `ef test`, JavaScript, cold | 0.162 | 0.122 | 0.04 |
| `ef test`, JavaScript, warm | 0.188 | 0.124 | 0.06 |
| `ef build`, Go, cold | 0.413 | 0.304 | 0.11, ranges overlap (0.32 to 0.69 against 0.25 to 0.37) |
| `ef build`, JavaScript, cold | 0.018 | 0.021 | none |

- **What it shows.** Laws add about 0.03 to 0.1 s per program per target to `ef test`, inside the gate's existing test steps. Gate speed matters only when it blocks, and this does not block. `order.ef` (eight laws, 1 100 cases) takes 0.48 s cold and 0.14 s warm on Go, and 0.16 s and 0.17 s on JavaScript.
- **One result is open.** The Go build with laws is slower at the median, with overlapping ranges, at a load that varied by 2.5×. Its output is byte-identical (§5.1), so any real difference is check time: the checker elaborates and checks each law function. That difference is not resolved here, and L1 should measure it on a quiet machine.
- **Not measured.** The marginal cost of laws on a quiet machine, and any program larger than these fixtures.

**Per build was rejected** because a build would then compile and run a test harness for each target, and builds would execute user code.

## 4. Evidence

### 4.1 What is reused from opaque values and checked evidence

This note does not fork `docs/research/opaque-values-and-evidence.md`. It applies that note's required boundaries to evidence that laws gate:
- "Construction is module-private and nominal identity is package-qualified" → minting is construction, owned by the evidence kind's module (§4.3).
- "Exported host APIs need their own checked boundary; target-language privacy alone does not establish this contract" → the admission check and adapter (§4.6).
- "Payloads used to justify evidence are immutable or snapshot/version-bound" → the deep snapshot (§4.6, rule 3).
- "Inspection … labels trusted predicates rather than presenting a proved business theorem" → a mint site and an admission check are listed as trusted decisions unless the mint-soundness law tests them (§13 Q4).
- "Pure phantom information can be erased only with evidence that both targets preserve the contract" → the erasure diff (§5.1).

What laws add is the condition on the index: evidence about `compare` exists only when `compare` is bound by a lawful satisfaction.

### 4.2 Evidence kinds and their names

```
// provisional spelling (#135)
evidence SortedBy[S] by Order.compare       // a kind: carrier S, indexed by an Order.compare binding
fn search(xs: SortedBy[Trio, User.compare], x: User) -> bool
```

- An **evidence kind** names its carrier parameter and the contract entry that indexes it.
- An **evidence type** `SortedBy[Trio, User.compare]` is a nominal wrapper over the carrier `Trio`. Inside Effra it reads as the carrier (field access, matching), and a value of it is accepted wherever a value of the carrier is.
- **Variance: covariant in results, contravariant in parameters.** A function type is a subtype of another only when each parameter of the expected type is a subtype of its own, and its result is a subtype of the expected result.
  - **What is accepted.** A `fn(Trio) -> SortedBy[…]` may stand where a `fn(Trio) -> Trio` is expected, since it forgets evidence on the way out.
  - **What is refused.** A `fn(SortedBy[…], User) -> bool` may not stand where a `fn(Trio, User) -> bool` is expected, since that would hand it a carrier it never checked. That is Effra's own R3 hole, method bivariance, which TypeScript has and Effra must not.
  - **In the prototype.** Function types do not carry evidence, so a function with an evidence parameter cannot be used as a value at all. EF189 refuses it: "search requires SortedBy<Trio, compareUsers> for xs, so it cannot be used as a function value: a function type carries no evidence in its parameters, and the value would erase the requirement". The refusal covers `apply(search, …)` and `let f = search` (probes M4-apply, M4-let). Before revision 2, both type-checked, and `apply(search, unsortedTrio, bob)` printed `false` for a user in the trio (review 1, M4). L4 replaces the blanket refusal with the variance rule, once evidence types are nominal types.
- **The index resolves by binding identity, at the mint site.** `User.compare` is the declaration that spike-where's resolution yields for `T.compare` at `T = User`. In the prototype it is the bound function `compareUsers`.
  - **Shadowing.** An index spelling that is shadowed at the mint site by a parameter, a local or a pattern binder names that binding, not the declaration, so it mints nothing. This applies in a body and in a signature.
  - **Sharing a type.** Two values sorted by the same declaration share a type, so `merge(a, b: SortedBy[S, T.compare])` type-checks without GDP's rank-2 naming.
  - **Conditional methods.** A conditional method's index is the canonical key (§1.3).
- An index that is not a declaration is refused: "evidence index must name a declaration" (`TestLawEvidenceRefusals`, closure index).

### 4.3 Who mints, and when

A discharged law does not mint a value. It makes its satisfaction **lawful**, and a lawful satisfaction is what lets its bound declarations index evidence:

| Source | May mint `E[C, I]`? | Prototype |
| --- | --- | --- |
| Code constructing `E[C, I](value)` where `I` resolves, at the mint site, to a declaration bound to the kind's entry by a satisfaction whose laws are all tested or trusted | **Yes**, in the evidence kind's owner module | `mint`, EF181 otherwise |
| The same with a satisfaction that declares an unresolved law | No: "unresolved (needs a fixture)" | EF181 |
| The same with no current passing receipt for the build target | Checks, but the build is refused (§2.10) | EF174 at build |
| A satisfaction whose receipt says refuted | Checks, but the build is refused | EF170 at build |
| `I` a declaration not bound to the entry (`reversed`) | No: "reversed is not bound to Order.compare" | EF181 |
| `I` a closure (a `…By` comparator) | No: "is not a function declaration" | EF181 |
| `I` spelled like a declaration but bound at the mint site to a parameter, local or binder | No: "compareUsers here is the parameter compareUsers of sortTrioByShadow" | EF181 (probe M2-mint) |
| An evidence result type whose index is a parameter | No: "evidence index compareUsers in the signature of sortTrioByShadow resolves to its parameter compareUsers, not to the declaration compareUsers" | EF181 (probes M2-signature, M2-forward) |
| The host-boundary adapter, through `admit E[C, I] by check` when `check(value)` holds | **Yes** | EF186 on failure |
| A law body | No (§1.8): "a law body states an obligation and is never a construction site" | EF181 (probe m3-mint) |

**Fail-safe by construction.** The walker that checks mints visits every function body, every `scope` body and every match arm. Any mint it never reached is refused rather than assumed sound, so a construct the walker does not know cannot hide a mint.

**The hole review 1 found** (M2), in the round-1 prototype:

```
fn sortTrioBy(t: Trio, compareUsers: fn(User, User) -> i64) -> SortedBy<Trio, compareUsers> {
    mint SortedBy<Trio, compareUsers>(sortWith(t, compareUsers))
}
```

- **Why it passed.** The index was looked up in the global function map by spelling, so this was `checked:true`.
- **What it projected.** The `.d.mts` was `(arg_t: Trio, arg_compareUsers: (…) => bigint) => SortedBy<Trio, "compareUsers">`: a TypeScript caller passed a reversed compare and got the canonical brand back, with no cast.
- **Now.** Both the mint and the signature are refused.
- **The control.** A method parameter with another spelling does not block canonical evidence obtained from `sortTrio` (probe M2-control).

- **Owner module.** Construction of an evidence wrapper is private to the module that declares the kind, as construction of a `pub readonly` type is private to its owner. The prototype has one module, so this is follow-up L4.
- **A mint is a trusted decision.** Minting asserts that `sortTrio`'s result is sorted. Nothing proves the sort correct. §13 Q4 recommends a law the compiler derives for each minting function, which turns that trust into a test.
- **Wrong carrier and unminted results.** Minting `SortedBy<Trio, …>` over a `User` is EF182; declaring an evidence result and returning the bare carrier is EF183.

### 4.4 Requiring evidence

`search(xs: SortedBy<Trio, compareUsers>, x: User)` accepts only evidence (`examples/laws/order.ef`).
- **The refusals.** Passing a plain trio is EF184: "search requires SortedBy<Trio, compareUsers> for xs, but the argument is the carrier Trio with no evidence". Passing evidence for another index is EF184: "the argument is SortedBy<Trio, reversed>" (`TestLawEvidenceRefusals`).
- **Evidence is tracked by binding identity in a lexical scope, never by spelling.**
  - **Bodies walked.** `scope` bodies and match arms are walked like any other body.
  - **Binders.** A pattern binder is a fresh binding that carries no evidence, whatever it is called.
  - **Calls.** A call through anything other than a function declaration cannot satisfy an evidence parameter (EF189, §4.2).
- **The holes this closes** (review 1, M4). In the round-1 prototype, each of these was accepted:
  - `scope { search(t, x) }`, which printed `false` on both targets for a user in the trio;
  - `match b { Box.Value { xs } => search(xs, x) }` under an evidence parameter also named `xs`;
  - `apply(search, …)` and `let f = search`.

  Each is now refused on both targets (probes M4-scope, M4-binder, M4-apply, M4-let).
- **The scope of this guarantee.** It holds for evidence in top-level parameters and results. Evidence nested in a record, a container or a callback is not tracked by the prototype; L4 and L5 cover it (§4.6).

### 4.5 What mints nothing

- **A `…By` closure.** It has no declaration identity, so it cannot be an index (EF181). `sortTrioBy(t, compare)` returns the carrier.
- **A function a TypeScript caller passes.** It arrives as a parameter, so it is a `…By` closure (EF181), and the export returns the carrier (§4.6, rule 1).
- **A derived or structural value.** Derivation, zero values, record reconstruction and foreign conversion never construct the wrapper (opaque-values).
- Naming a comparator *value* in a type needs GDP's fresh-name mechanism. That stays a separate candidate in opaque-values, and until it exists none of these mint.

### 4.6 The host boundary

The four rules are spike-where §2.10's requirements, made concrete:

1. **Outward (forms i and ii), by provenance.** An exported function whose result is evidence takes one of two forms:
   - **Form i** binds the canonical method on the Effra side, so its TypeScript signature has no method parameter: `sortTrio(t: Trio) => SortedBy<Trio, "compareUsers">`.
   - **Form ii** returns the unbranded carrier: `sortTrioBy(t, compare) => Trio`.

   The rule that enforces this is provenance. **An index names the declaration bound at its site, never a spelling**, so a parameter, local or binder that shadows the declaration indexes nothing, in a body or in a signature (§4.2, §4.3). Revision 1 claimed this was "enforced already". It was not, because the index was resolved by spelling (review 1, M2).
   - **Generic forwarding.** An exported generic or forwarding function projects the carrier (form ii). Its `where` entries become parameters a TypeScript caller supplies (where-dispatch §2.10, requirement 1). Evidence it obtains by forwarding to a minting function inside Effra never reaches TypeScript branded, because its result's index would resolve to its own method parameter.
   - **In the prototype.** User-written generics are refused there (EF127), so the forwarding probe uses a concrete stand-in: `fn sortForward(t: Trio, compareUsers: fn(User, User) -> i64) -> SortedBy<Trio, compareUsers> { sortTrio(t) }`. It forwards to canonical evidence, and its signature is refused because the index resolves to its parameter (probe M2-forward).
   - **For L5.** L5's attack table adds a TypeScript-supplied compare to an exported generic sort.
2. **Inward: host evidence is never trusted.** An exported function with an evidence parameter takes the **unbranded carrier** in its `.d.mts` and re-establishes the evidence on entry through the owner's admission check, or it is refused (EF185, "declare `admit SortedBy<Trio, compareUsers> by <check>` … or keep search private"). The admission is `admit SortedBy<Trio, compareUsers> by sortedTrio`, where the check must be a pure function declaration from the carrier to `bool` and the index must be lawfully bound (EF187). A value that fails the check throws `TypeError: EF186 search: xs is not SortedBy<Trio, compareUsers>; host evidence is re-established on entry and this value fails sortedTrio`.
   - **A carrier with no snapshot plan is refused at check time, never passed through.** An evidence-carrying export whose data parameter is opaque to the snapshot is refused with EF188. In the prototype, every generic record is opaque, for example `Holder<Trio>`: "firstAge carries evidence across the host boundary, and its parameter h: Holder<Trio> has no snapshot plan" (probe M3). Callables are exempt, because they are never snapshotted and never carry evidence (EF189).
   - **What it closes.** Before revision 2, such an adapter skipped both the snapshot and the admission check: `return(a0)=>{return __ef_function_firstAge(a0);}` (review 1, M3).
   - **Current coverage.** The prototype admits only record and enum carriers. L5 adds generic carriers with a snapshot plan per instantiation.
3. **Stability: a deep, Effra-owned snapshot, read once.** Every data argument of an evidence-carrying export is copied into a fresh, deep-frozen value before any check or mint.
   - **How it is read.** The copy reads own data properties only, and an accessor is refused with EF186. Each value is type-checked, so an `i64` must be a 64-bit `bigint`.
   - **One read per field.** The admission check and the body both run on the snapshot, so the adapter reads each field of the host's argument exactly once.
   - **The acceptance counts reads, not outputs** (spike-where review 5, n1). An extra entry check over the caller's object can still return the right answer, so a correct output cannot show requirement 3. `benchmarks/laws/ts/proto/attacks.mts` therefore passes `search` a proxy whose data properties answer differently on every read, and it records `A3-read-count = true first:1 second:1 third:1`. The getter attack records that the accessor was read 0 times (`G1-getter-reads = 0`).
   - **Returned evidence** is deep-frozen.
   - **The recipe rule.** An effect export brands its success value, not the recipe: `sortTrioLater: (arg_t: Trio) => Effect.Effect<SortedBy<Trio, "compareUsers">, never, never>`. Its adapter deep-freezes that value when the recipe succeeds (`Effect.map(call, __ef_deepFreeze)`). Before revision 2, the brand landed on the recipe, `SortedBy<Effect.Effect<Trio, never, never>, "compareUsers">`, and the adapter froze the recipe object (review 1, M4). Probe M4-effect-export and the consumer's `Effect.runSync(sortTrioLater(trio))` check the new form, and attack F3 shows that a recipe is not assignable to `SortedBy` (TS2322).
4. **Private canonical bindings.** Adapters call module-private `const` bindings (`__ef_function_sortedTrio`, `__ef_function_search`). An ES module namespace is already non-extensible and its bindings read-only, so `Object.assign` on it throws; patching `call` on an exported function does not reach the adapter. W3's frozen companion keeps this when companions exist.

**Outward brand.** `.d.mts` declares `declare class EfEvidence<E extends string, S, I extends string> { private readonly __ef_evidence: readonly [E, S, I]; }` and `export type SortedBy<S, I extends string> = S & EfEvidence<"SortedBy", S, I>`. A private member of a declared class cannot be supplied by a literal or by `Object` augmentation, so a plain trio is not `SortedBy` (TS2322) and evidence for one index is not evidence for another (TS2322). The brand is a convenience for TypeScript callers that track evidence among themselves. No Effra export trusts it, so delivering non-evidence to a branded slot, with or without a cast, gains nothing. The counterfactual shows routes that need no cast at all (§5.2). That is the honest name for this boundary.

**Scope of the prototype, disclosed.**
- **What it has.** Evidence occurs only in top-level parameters and results, over record and enum carriers.
- **L4 and L5 cover:**
  - evidence nested in a record or a container;
  - evidence passed to or returned from a callback;
  - generic carriers with a snapshot plan;
  - a variance rule in place of EF189's blanket refusal.

  There, every input occurrence is re-established and every output occurrence branded.

### 4.7 Wrapping without losing evidence

- **Inside Effra.** Evidence flows through `let`, direct calls, `run` and `if`/`match` branches that agree, so an ordinary function wrapping a minting function returns evidence without minting. Branches that disagree give the carrier.
  - **Direct calls and branches.** `TestLawEvidenceAcceptsMintedFlow` accepts `again(xs: SortedBy<…>) -> SortedBy<…> { xs }` and `pick(flag, t) -> SortedBy<…> { let sorted = sortTrio(t); if flag { sorted } else { again(sorted) } }`, then passes `pick`'s result to `search`.
  - **`run` and `scope`.** Calling an effect function whose result is evidence builds a recipe that carries that evidence, and `run` yields it. `let xs = run sortTrioLater(t); search(xs, bob)` is accepted and prints `true` on both targets (probe M4-run). `TestLawEvidenceAcceptsMintedFlow` also accepts the same `run` inside a `scope` body. Whether a `scope` body's result keeps evidence for a later call is not probed. Revision 1 claimed this, but its prototype refused it with EF184 (review 1, M4).
- **Generic wrappers.** In the final design evidence types are ordinary nominal types to the checker, so a generic `identity[T](x: T) -> T` or `Option[SortedBy[…]]` passes evidence through as any other type, once rows unit 4a admits whole-value type parameters (#128). The prototype tracks evidence beside the type, so this is follow-up L4.
- **Rows, ownership and observations.** Laws and evidence contribute no code (§5.1), so a wrapper's 4a contract is the same with or without them.
- **In TypeScript.** `const sortAndLog = (t: Trio) => sortTrio(t)` keeps `SortedBy<Trio, "compareUsers">` through inference (`benchmarks/laws/ts/proto/consumer.mts`).
- **What wrapping cannot do.** Wrapping a *method* in a closure loses its declaration identity, so the closure cannot index new evidence. That is the boundary of §4.5, not a loss: evidence already minted stays evidence.

## 5. Two-target honesty

### 5.1 Erasure, by diff

`benchmarks/laws/proto-receipts.sh <proto-checkout>` writes `benchmarks/laws/runs/erasure-diff.txt` (and the seed survey of §2.11), and exits 1 if any output differs beyond the header or a seed run disagrees across targets. It builds `order-broken.ef` and `codec.ef` twice with the prototype: once as written, once with every `contract` and `satisfies` block replaced by blank lines, so line directives do not move. The Go `main.go`, the JavaScript module and the `.d.mts` differ only in the source-revision header:

```
== diff codec.ef Go main.go
1c1
< // Generated by the Effra prototype. Source revision: a779285a…
---
> // Generated by the Effra prototype. Source revision: f5396905…
== diff codec.ef JavaScript mjs
1c1 (the revision header, abridged here)
== diff codec.ef JavaScript d.mts
== diff order-broken.ef Go main.go
1c1 (the revision header, abridged here)
== diff order-broken.ef JavaScript mjs
1c1 (the revision header, abridged here)
== diff order-broken.ef JavaScript d.mts
```

`TestLawsEraseFromBuildOutput` asserts the same for the Go build, the JavaScript entry, the JavaScript library and its declarations, and that no output names a law. Laws exist only in the test harness (`efFunction_law__…`).

`TestEvidenceErasesExceptAtTheHostBoundary` strips evidence from `order.ef` (evidence types become carriers, the mint becomes its operand). The Go build is identical. The JavaScript and `.d.mts` differ only in the adapter lines (`__ef_export_*`, `__ef_snapshot`, `__ef_deepFreeze`) and the brand declarations.

Run time with laws is therefore zero by construction. The diff above is regenerated at `63164e8a`, where its revision headers are unchanged from revision 1 because `codec.ef` did not change.

**The host adapter's cost, measured.** One deep copy of the arguments per call of an evidence-carrying export, plus the admission check inward or a deep freeze outward. `benchmarks/laws/ts/adapter-cost.mjs`, run by `proto-cost.sh`, makes 200 000 calls per sample, 11 samples after a warm-up, on node 24.11.1 at load 9 to 24. Median nanoseconds per call:
- `oldest`, with no adapter (the control): 4.7;
- `sortTrio`, an outward adapter (snapshot of a three-user trio, deep freeze of the result): 1 209;
- `search`, an inward adapter (snapshot of both arguments, then `sortedTrio`): 1 268.

So an evidence-carrying export pays about 1.2 µs per call for a 7-object argument, all of it at the host boundary. Calls inside Effra pay nothing, and neither does Go.

### 5.2 `.d.mts` under attack

`benchmarks/laws/ts/run.sh` checks the prototype's emitted library with the repository's TypeScript 6.0.3, `--strict`, and runs each probe on node 24.11.1.
- **The library.** `ts/proto/lib.mjs` and `lib.d.mts`, built from `examples/laws/sorted.ef` at `63164e8a` by `ef test --target both`, then `ef build --target js`. The build is refused with EF174 until the test has written a passing receipt.
- **The receipt** is `benchmarks/laws/runs/ts-boundary.txt`:
  - every tsc run exits 0;
  - 6 of 6 directives fire with their declared codes, 5 in `proto/` and 1 in `counterfactual/`;
  - every file has 0 `any` keywords.
- **The consumer** also runs `Effect.runSync(sortTrioLater(trio))` and checks that the success value is sorted, deep-frozen and accepted by `search`.

| Attack | Rule | TypeScript | Runtime |
| --- | --- | --- | --- |
| L1 laundering: `const x: SortedBy<…> = sortTrioBy(trio, reversed)` | 1, outward | TS2322 | — |
| L1 laundering: `search(sortTrioBy(trio, reversed), ada)` | 1 and 2 | type-checks (the parameter is unbranded) | EF186 |
| R1 spread override `{ ...sorted, first: zed }` | 2, inward | type-checks | EF186 |
| R2 array covariance | 2 | type-checks | EF186 |
| R3 method bivariance: `{ find: search }` in a slot over `Trio` | 2 | type-checks | EF186 |
| X2 mutable property covariance | 2 | type-checks | EF186 |
| A1 recheck alias: mutate the argument after `sortTrio` | 3, stability | type-checks | correct: the result keeps age 12 and `search` still finds bob |
| A2 element alias: mutate a shared user after sorting | 3 | type-checks | correct: the result is a frozen copy |
| A3 mutate returned evidence | 3 | needs a cast | `TypeError` (frozen) |
| G1 getter answering differently per read | 3 | type-checks | EF186 ".first is not an own data property"; the getter is read 0 times |
| A3 read count: a proxy whose data properties answer differently per read | 3 | type-checks | correct, and each field read once: `true first:1 second:1 third:1` |
| R10 `Object.assign(lib, { compareUsers: reversed })` | 4, private bindings | type-checks | `TypeError` (read-only namespace) |
| R10b patch `call`/`apply` on the exported compare | 4 | type-checks | correct: `search` is unaffected |
| N1 never filler `search(none[0], ada)` | standard route | type-checks | EF186 "xs is not a Trio" |
| N2 never rest `const v: (...rest: never[]) => boolean = search; v()` | standard route | type-checks | EF186 |
| N3 untyped value `search(JSON.parse(…), ada)` | standard route | type-checks | EF186 "xs.first.age is not an i64" |
| O1 `Object` augmentation, then `const f: SortedBy<…> = trio` | standard route | TS2322 (private member) | EF186 if passed to `search` |
| F1 plain trio as `SortedBy` | outward brand | TS2322 | — |
| F2 evidence for `compareUsers` as evidence for `reversed` | outward brand | TS2322 | — |
| F3 an effect export's recipe as `SortedBy` | recipe rule | TS2322 | — |

**Counterfactual.** `benchmarks/laws/ts/counterfactual/` is the same library with the `search` adapter removed and its parameter branded, which is naive trust. R2, R3 and X2 type-check with no cast and `search` answers `false` for a user who is in the trio (`R2-array-covariance=false R3-method-bivariance=false X2-property-covariance=false`). The run fails if a hole closes, so the comparison cannot go stale. It is derived from revision 1's emission (`5f428883`) and kept unchanged, because the inward rule it removes has not changed. This is why the inward parameter is unbranded: TypeScript's documented unsoundness delivers non-evidence to a branded parameter without forging the brand.

Each row of spike-where's expected-failure table (where-dispatch §2.10) is now a refusal or a fail-safe result:
- **Refused:** R1, R2, R3, X2 and R10.
- **Correct:** A1 and A2.
- **A3,** the getter that answers the entry check and the body differently, is refused before the getter is ever read. Its data-property form, a proxy, is correct with each field read exactly once.

### 5.3 Name-neutrality

The prototype's law, evidence, receipt and boundary code (`internal/compiler/laws*.go` and `cmd/ef/laws.go` on `63164e8a`) contains no string literal `"Order`, `"RoundTrip`, `"compare`, `"SortedBy`, `"Codec` or `"codec`: `grep` over the five files finds none. Contract, law, entry and evidence names are read from the source.

## 6. Prototype

**Files on `wf10/spike-laws-proto`.** 35 files and 3 967 lines added on `13e62ad4`, the probes included:
- `internal/compiler/laws.go`:
  - declarations, and elaboration of each law per satisfaction into a checked `law__C__S__l` function returning `bool`;
  - logical-ID seeds, and the optional `target` clause;
  - the evidence walker, by binding identity and lexical scope (EF180-EF184, EF189), and mint authority;
  - laws as values (EF178), budgets (EF175) and entry types (EF177).
- `internal/compiler/laws_emit.go`: generator derivation and EF171, and the Go and JavaScript harnesses. Both harnesses share the generator, shrinker and digest algorithms and the canonical root-seed parse (EF176).
- `internal/compiler/laws_receipt.go`: the receipt, its comparison by logical law and provider, persisted disagreement, the provider comparison, and the build verdict (EF170, EF173, EF174).
- `internal/compiler/laws_boundary.go`: `admit`, EF185, EF187 and EF188, and the JavaScript adapter and brand, including the recipe rule.
- `internal/compiler/laws_test.go`: elaboration, accepted flows (including `run`), every refusal, the receipt comparison and disagreement controls, the build verdict, the effect-export brand, and both erasure tests.
- `cmd/ef/laws.go`: `ef test --target both`, location-keyed and exploration receipt paths, seed validation, and build verdicts.
- `examples/laws/{order,order-broken,codec,sorted}.ef`, `examples/laws/run.sh`, `examples/laws/probes.sh`, and `examples/laws/probes/`.

**How to run.** On the proto branch:
- `examples/laws/run.sh <scratch>` builds `ef` and runs every example on both targets. It checks the expected statuses and counterexamples, asserts `agree:true` for every program whatever its expected exit code (review 1, m6), then runs the probes. It exits non-zero on any mismatch.
- The full Go suite (`go test ./internal/... ./cmd/...`) passes at `63164e8a`. The law tests, with their subtests, are listed in `benchmarks/laws/runs/proto-law-tests.txt`.

**Results** (`benchmarks/laws/runs/proto-e2e.txt`). Go and JavaScript are identical, with `agree:true` for every program:

| Law | Status | Cases | Digest | Counterexample |
| --- | --- | --- | --- | --- |
| `Order.reflexive@User` | tested | 200 | `00491737` | |
| `Order.antisymmetric@User` | tested | 200 | `5ce0e49e` | |
| `Order.transitive@User` | tested | 200 | `a68be510` | |
| `Order.total@User` | tested | 200 | `22b07559` | |
| `Order.reflexive@Trio` | tested | 100 | `53ca5cf1` | |
| `Order.antisymmetric@Trio` | tested | 100 | `1ced50c9` | |
| `Order.transitive@Trio` | trusted | 0 | — | justifier "lexicographic composition of Order@User, whose transitivity is tested" |
| `Order.total@Trio` | tested | 100 | `ddf07996` | |
| `Order.reflexive@User` (broken compare) | tested | 200 | `00491737` | |
| `Order.antisymmetric@User` (broken) | **refuted** | 2 | `474dbd6c` | `a = User { age: -1, name: "" }, b = User { age: 0, name: "" }` |
| `Order.transitive@User` (broken) | **refuted** | 172 | `6069a168` | `a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }` |
| `Order.total@User` (broken) | **refuted** | 18 | `84c73671` | `a = User { age: 9223372036854775807, name: "" }, b = User { age: 0, name: "" }` |
| `RoundTrip.roundTrip@accountJson` | tested | 200 | `0109b916` | |
| `RoundTrip.roundTrip@halfJson` | **refuted** | 2 | `417fb0d8` | `value = Profile { active: false, name: "", score: -1 }` |

The lawful and broken compares share `reflexive`'s digest. Both satisfy the same logical law, `Order<User>.reflexive`, so they draw the same cases (§2.5).

Then:
- **Builds.** `ef build order-broken.ef` is refused with EF170 (§2.7). `ef build order.ef` is admitted once its receipt exists. `ef run order.ef` prints `true` on Go and on JavaScript (`search` over minted evidence).
- **Exploration.** `EFFRA_LAW_SEED=7` writes `.seed-7` receipts with `rootSeed:"7"` and `gating:false`. They agree, and the gating receipt keeps `rootSeed:"0"`.
- **The evidence programs.** `order.ef` exercises minting (`sortTrio`), requirement (`search`) and admission (`sortedTrio`). `sorted.ef` is the host-boundary library of §5.2.

### 6.1 Review-1 probes, before and after

`examples/laws/probes.sh <ef>` runs every probe on Go and on JavaScript and prints PASS or FAIL with the observation. It exits non-zero on any failure. The receipts are:
- `benchmarks/laws/runs/probes-after.txt`: `ef` built from `63164e8a`, 33 of 33 lines pass;
- `benchmarks/laws/runs/probes-before.txt`: `ef` built from `5f428883`, 37 lines fail and 6 pass;
- `benchmarks/laws/runs/m6b-before.txt`: the seed-overwrite hole, reproduced by hand with the old binary.

| Finding | Probe | Before (`5f428883`) | After (`63164e8a`) |
| --- | --- | --- | --- |
| M1 | `partial.ef`: the 0/1 compare | (control) without `total`, tested ×3 and the build admitted | with `total`, refuted at `a = -1, b = 0`; build refused (EF170). M1 is a contract change, so it also passes before |
| M2 | shadowing parameter in a mint; in a signature; a concrete forwarder | checked | EF181, both targets |
| M2 | control: another spelling, canonical evidence from `sortTrio` | accepted | accepted |
| M3 | `Holder<Trio>` export | checked; the adapter skips snapshot and check | EF188 |
| M4 | `apply(search, …)`, `let f = search` | checked; prints `false` | EF189 |
| M4 | `scope { search(t, x) }` | checked; prints `false` | EF184 |
| M4 | binder spelled like the evidence parameter; binder under a renamed parameter | the binder spelled `xs` checked; the renamed one EF184 | EF184 for both |
| M4 | `let xs = run sortTrioLater(t); search(xs, bob)` | EF184 | accepted; prints `true` |
| M4 | effect export projection | `SortedBy<Effect.Effect<Trio, …>, …>`, recipe frozen | `Effect.Effect<SortedBy<Trio, …>, never, never>`, success value frozen |
| M5 | `ef build` of a minting program with no receipt | EF173 note, built, `.d.mts` branded | EF174, refused |
| M6a | `ef build /abs/…` from another directory beside a refuted receipt | EF173, built | EF170, refused |
| M6b | `near.ef`: seed 0 refutes; exploration seed 14 does not | the seed run overwrote the receipt, and the build was admitted (`m6b-before.txt`, seed 3) | own `.seed-14` receipt; gating receipt unchanged; build refused |
| M6c | `deep.ef`: Go tested, JavaScript stack overflow | EF172 printed, receipts unchanged, Go build admitted | both receipts `refuted` with the EF172 failure; both builds refused |
| M7 | `runtime.ef`: `GoRuntime` (`target go`) and `EffectRuntime` (`target js`) | no `target` clause; the program fails | paired by logical law; same seed `990390378`, same digest `03b4fb56`, `sameScenarios:true` |
| M7 | `runtime-control.ef`: the recipe run | fails | refuted on both providers: the observation channel is live |
| M7 | `providers.ef`: two codecs, one logical law | no provider comparison | both listed, same seed and digest |
| m1 | `EFFRA_LAW_SEED=010`, `-1`, `abc`, `4294967296` | false EF172s, or admitted | EF176 before any harness runs |
| m2 | entry `label: fn(T) -> string` bound to `fn(Trio) -> bool` | checked | EF177 |
| m3 | a call to a law; a mint in a law body | checked | EF178; EF181 |
| Q2 | a satisfaction with no `cases` | EF181 "no case budget" at the mint | EF175 at the satisfaction |

**What the prototype does not do.** Each item is a follow-up unit (§12):
- live-provider refusal and `using` on effect laws;
- service and machine laws;
- generic and recursive data generators;
- user-written generics, so generic forwarding is shown with a concrete stand-in;
- conditional satisfaction;
- owner-module minting across modules;
- evidence nested in records, containers or callbacks;
- generic carriers at the host boundary (refused with EF188);
- the variance rule (EF189 refuses every function value with an evidence parameter instead);
- hygienic elaboration of law bodies, which are rebuilt by token substitution (review 1, m2);
- toolchain and provider revisions in receipts (Q7).

## 7. Laws as provider protocols, and the #124 bridge

**Decision.** A contract whose subjects are providers is a **provider protocol**: services, runtimes, codecs, or any effectful implementation a library offers.
- **Same laws, same receipt.** Every provider, built-in or userland, passes the same laws and gets a receipt in the same schema.
- **Comparable.** Providers of one law are compared by logical law and scenario.
- **R1-R15** are one such protocol, the `Runtime` contract.
- **Host fixtures.** A host fixture is a named discharge mode for a declared law. It is used only for behaviour an Effra program cannot observe, and it writes the same receipt.

**Why this is the general form** (owner, 2026-10-12, on guardrails and portable providers). A userland implementation "that may not necessarily be [an] effect runtime, but [is] effectual" may be better in its domain than the built-in one, but only if replacing the built-in one cannot change what the construct means. The protocol's laws are that guarantee: a provider that passes them may be swapped in, and one that fails them is refused at build time.
- **The failure this prevents.** F# computation-expression builders and Haskell's `RebindableSyntax` let a library rebind the meaning of a construct (`let!`, `do`, `>>=`) with no laws checked (F# language specification, "Computation Expressions"; GHC User's Guide, `RebindableSyntax`; neither verified locally). In both, a builder can make `do` or `let!` mean anything that type-checks, and nothing tells the reader that a familiar block no longer obeys the monad laws.
- **What Effra keeps.** Effra keeps the substitution, since a provider is chosen by `satisfies` and by provision. It does not keep the drift, since a provider that changes meaning fails a law and its build is refused.

**The protocol's parts**, each in the prototype at `63164e8a`:

| Part | Rule | Prototype receipt |
| --- | --- | --- |
| Identity | A law's logical ID is `Contract<Args>.law`. The provider is receipt metadata (`provider`, and `boundTarget` for a provider that exists on one target). | Receipts carry `law`, `provider` and `boundTarget` (§2.2). |
| Seeds | Each law's seed comes from its logical ID, so all providers of one law run the same scenarios. | `accountJson` and `accountJsonAgain`: seed `607786003`, digest `0109b916` (probe M7-providers). |
| Target-bound providers | `GoRuntime satisfies Runtime cases 50 target go` runs only on Go, and `EffectRuntime … target js` only on JavaScript. Their receipts pair by logical law, not by per-provider ID. | Same seed `990390378` and digest `03b4fb56` on each provider's target, `sameScenarios:true` (probe M7-runtime). Revision 1 would have reported "missing on …" for each (review 1, M7). |
| Comparison | `ef test --target both` reports, per logical law, every provider's status, seed and digest, and whether the scenarios match. | `lawComparison.providers` in the output (schema 2). |
| Observation | A law observes through a channel the harness owns, not through a report from the provider under test. | R11 below. |

**R11 is an effect law with a harness-owned observer.** Revision 1 wrote R11 as a pure law "over a built recipe's trace". A pure law cannot run anything or read a service, so it could not observe construction (review 1, M7). R11 is now this effect law:

```rust
contract Runtime {
    effect law constructionRunsNothing(n: i64) raises { AssertionFailed } uses { Assert } {
        let recipe = explode(n)
        true
    }
}

effect fn explode(n: i64) -> bool raises { AssertionFailed } uses { Assert } {
    run Assert.check(false, "construction ran the recipe")
    true
}
```

Tested candidate, `examples/laws/probes/runtime.ef` on `63164e8a`:
- **How it observes.** The law builds a recipe and never runs it. If construction ran the recipe, `Assert.check(false, …)` would raise `AssertionFailed`, which refutes the law. The `Assert` provider is the harness's, not the runtime's.
- **The negative control.** `probes/runtime-control.ef` runs the recipe instead, and both providers refute it. So the channel is live.
- **The full form.** lawful-runtime-contract (`:86`) asks for a "construction-only fixture plus a side-effect counter". In RS1, the observer is a harness-owned counter service: the law builds the recipe and reads the counter as zero. The trace service is not the observer for this law.

**Trace fidelity is a trusted decision.** The other R laws observe the trace that the provider under test emits. A provider that emits a wrong trace could pass a law it breaks, so each provider's trace emission is listed by `ef inspect` as a trusted decision, with its justifier. Two things narrow that trust:
- the reference checker (#124) is shared across providers;
- laws that can use a harness-owned observer, as R11 does, should use one.

```
// provisional spelling (#135)
contract Runtime {
    effect law releaseIsLifo(n: Count) uses {Trace} {                 // R1
        lifo(run observe(acquireAll(n)))
    }
    effect law closeAwaitsChildCleanup(d: Millis) uses {Trace, Clock} {   // R2
        precedes(run observe(closeWithSlowChild(d)), ChildCleanup, OwnerRelease)
    }
    effect law constructionRunsNothing(k: Script) uses {Counter} {    // R11, harness-owned counter
        let recipe = script(k)(0)                                        // built, never run
        let count = run Counter.read()
        count == 0
    }
    effect law panicIsDefectWithCleanup() fixture "<host test id>"      // R15, host-only; names the host test
}
GoRuntime satisfies Runtime cases 100 target go
EffectRuntime satisfies Runtime cases 100 target js
```

The reasons:
1. **The laws are language meaning.** The owner placed runtime laws in the language-construct tier, and a third provider "would be the same laws". Declared in Effra, each law has one statement that every provider passes. As host fixtures, each provider would re-port and re-interpret the statement.
2. **The trace is already the observation, for most laws.** #124's acceptance requires both providers to emit one event vocabulary (`construct`, `admit`, … `publish`) with owners and identities, and a reference checker over partial orders. A test provider that exposes those events to Effra makes each such R law a predicate over observed data, and the reference checker is ordinary Effra code. Its fidelity is the trusted decision above.
3. **The mechanism exists.** One receipt schema and the five categories carry over, and seeds and comparison key on the logical law, so target-bound providers compare (table above). Law parameters are the program's shape (resource count, child delay, failure placement), so the causal mutants become ordinary refutations with shrunk counterexamples: `m1` (FIFO release) should refute R1 at `n = 2`.
4. **Composition laws join the same contract** (§1.7): nested deadlines, monad laws and provide's distribution are runtime laws too.
5. **Host fixtures stay honest where they must.** A managed panic raised from host code (R15), and anything else outside the managed protocol, cannot be produced from Effra. Such a law is still declared in `Runtime`, so its ID, statement and receipt are uniform. Its discharge names the fixture, and the receipt says `fixture` beside the status.

**Prerequisites and order.** This needs:
- service-style satisfactions by providers, with `using` and the live-provider refusal (L3);
- the trace service and a counter fixture from #124's trace model;
- sequences (#120) for traces.

Until a law's Effra form can run, its receipt is `unresolved` with the reason "needs trace sequences (#120)", not a silently passing host test. #124 can start its trace model and reference checker in parallel; the checker is written so that it moves into the law bodies unchanged.

**The losing option, kept:** host fixtures under the same receipt schema, as cats-effect's law modules run on `TestContext`.
- **For it.** It is available now and needs no Effra feature.
- **Against it.** It gives each provider its own copy of the statement, it cannot shrink a counterexample program, and a third provider, or a userland one, must port every fixture.

## 8. Composability

The five properties the owner asked for, for laws and evidence.

**1. Lossless wrapping.** An ordinary function that wraps a law-checked thing keeps everything:
- evidence, through direct calls, `run` and agreeing branches (§4.7), and through generics once 4a lands; a function value with an evidence parameter is refused rather than allowed to erase the requirement (§4.2);
- rows, ownership and observations, because laws and evidence add no emitted code (§5.1), so the wrapper's 4a contract (#128) is unchanged;
- lawfulness, because a satisfaction belongs to the declaration, not to a call path.
The one thing a wrapper cannot carry is a method's declaration identity through a closure (§4.7).

**2. Nesting matrix.** Rows are law-bearing constructs; columns are contexts.

| Construct ↓ / context → | Generic function | Service | Machine | Effect recipe, scope, timeout | Another module | Host boundary |
| --- | --- | --- | --- | --- | --- | --- |
| Contract + satisfaction | `where T: Order` expands to entries (spike-where) | A service is its own contract over operations (§1.4) | A machine may require `where Event: Eq` | Not applicable (declarations) | Satisfaction in the subject's owner module; transported in interface summaries (L1) | Erased; the `.d.mts` has methods only |
| Law | Laws over an abstract `T` discharge per instantiation (§1.3) | Effect laws over providers (§1.4) | Reachable-state laws (§1.6) | Effect law bodies run recipes, each case in a fresh scope (§1.8) | Runs in the satisfying module's `ef test` | Erased |
| Evidence value | Passes as a type argument after 4a (L4); as a function parameter, contravariant (§4.2) | Service operations may take or return evidence; a host-supplied provider's evidence is re-checked on entry | A machine state may carry evidence | `run` of a recipe returning evidence yields evidence (probe M4-run); an effect export brands the success value (§4.6) | Kind owned by its module; construction private | Outward branded, inward re-checked from a snapshot read once (§4.6) |
| Admission check | — | — | — | Must be pure | Declared in the evidence kind's module | Runs in the adapter |

Refused nestings:
- a law inside a law;
- a law quantified over laws or contracts;
- a law used as a value (EF178);
- a mint inside a law body (EF181);
- a satisfaction inside a function;
- evidence indexed by a closure or a parameter (EF181).

**3. First-class recipes.** An effect law's body is an ordinary recipe that the runner runs once per case, so laws state properties of recipe combinators (timeout, provide, flatMap) by composing recipes inside the body (§1.7). Laws themselves are deliberately not values: a law is an obligation on a declaration, like Bend's `law`, so it cannot be passed, stored or conditionally created. Evidence values are first-class.

**4. Orthogonality.**
- Laws change no typing, rows, ownership or emission (§5.1).
- Evidence changes typing only, by one nominal wrapper, and emission only at host-facing exports.
- Contracts are spike-where's `where` entries with laws added; dispatch, lowering and projection are unchanged (spike-where §2.10).
- derive (#100) produces methods; a satisfaction still states the laws and the budget, so derived methods are tested like written ones.
- Rows (spike-rows) and laws meet only in an effect law's explicit `raises`/`uses`, which are ordinary effect contracts.

**5. Composition laws.** Laws can state composition algebra (§1.7: nested deadlines take the minimum, monad identity and associativity over traces, provide distributes in outcome only), and a law-bearing contract composes (§1.3: a generic type's satisfaction is lawful when its parts are). Hickey's distinction applies: a law is stated beside the code it constrains, not braided into it, so removing every law leaves the program byte-identical (§9).

## 9. Prior art

Sources were read on 2026-10-12 from local clones at the pins below; nothing was built from them. Effect is read at `460272d3` (4.0.1) and checked against the 4.0.1 copy in this repository's `node_modules`. Go is the go1.27.0 toolchain module. Literature marked "not verified locally" was not checked against a source.

| Source, pin | Laws | Generators and missing generators | Shrinking | Seeds and default budget | Effra takes / differs |
| --- | --- | --- | --- | --- | --- |
| **QuickCheck** `3d8fe868` | Properties, no bundles | Type-directed `Arbitrary`; a missing instance is a GHC error; no generic `arbitrary` (`Arbitrary.hs:244-247`) | External; `shrink _ = []` by default (`Arbitrary.hs:325-326`) | Random per run; 100 cases (`Test.hs:170-177`) | Takes type-directed derivation, but the compiler derives; differs on seeds and silent non-shrinking |
| **Hedgehog** `75e06fb4` | Properties | Explicit generators | Integrated rose trees; `>>=` resets inner progress (`Tree.hs:452-461`) | Random or `HEDGEHOG_SEED`; prints `recheckAt` (`Report.hs:656-665`) | Takes integrated shrinking's goal; uses a choice buffer instead |
| **hedgehog-classes** `f6250814` | Law bundles per class (`Laws.hs:70-73`) | Explicit `Gen a` per bundle | Hedgehog's | Hedgehog's | Takes per-contract bundles and naming the law in the failure (`Laws.hs:88-96`) |
| **checkers** `ebaec5da` | `TestBatch` bundles (`Checkers.hs:90-93`) | Type-directed | QuickCheck's | 500 cases (`Checkers.hs:121-129`) | Bundles; budgets stay explicit |
| **cats-laws + discipline** `4a2ea736`, `7658404a` | `RuleSet`s with parents merged by name (`Laws.scala:36-92`); `OrderLaws` (`OrderLaws.scala:25-58`) | Implicit `Arbitrary` | ScalaCheck external, `shrinkAny` none (`Shrink.scala:26-29`) | 100 (`Test.scala:219-229`) | Takes inherited laws running once (§1.2) |
| **ZIO Test laws** `716eaeef` | `ZLaws`, `ZLawful` (`ZLaws.scala:30-46`) | Explicit, optional `DeriveGen` | Integrated, effectful (`Sample.scala:27`) | **Fixed seed** (`TestRandom.scala:649`); 200 samples | Takes the fixed seed |
| **proptest** `70cf37d4` | Properties | `any::<A>()`, derive | Value-tree state machine (`traits.rs:580-621`) | Random; 256; regression files of seeds checked in (`file.rs:301-312`) | Takes pinned regressions as explicit examples (§13 Q6) |
| **Rust quickcheck** `eb00091c` | Properties | Type-directed | External, greedy | Random, never printed | — |
| **fast-check** `v4.10.2` | Properties | Explicit arbitraries | Context-based | Random; prints `{seed, path}` (`RunDetailsFormatter.ts:115-119`) | The TypeScript ecosystem's choice; Effect no longer re-exports it |
| **Effect** 4.0.1 `460272d3` | **Prose only**: "total ordering laws" (`Order.ts:29`), "reflexive, symmetric, and transitive" (`Equivalence.ts:26`); no suites | Schema-directed `effect/Arbitrary`; a missing generator **throws at run time** (`internal/arbitrary/schema.ts:48-50`) | Integrated | Seed from `Random`; 100 runs (`runner.ts:613-622`) | Effra checks what Effect states in prose, and refuses at check time |
| **Clojure spec + test.check** `6b698eed`, `5ba3a25b` | `s/fdef` `:fn` relations | Spec-directed; "Unable to construct gen at" thrown at run time (`alpha.clj:289`) | Rose trees | `currentTimeMillis`; 1 000 in `stest/check` | Check-time refusal instead |
| **Liquid Haskell** `952f02cf`, **Dafny** `5f717bf4`, **F\*** `fb6ea91e`, **SPARK** `7d9baf4f`, **Lean 4** `db7f10e9`, **Idris 2** `1c630e67` | Verified contracts and theorems | — | — | — | Erasure is always by a typed marker (Dafny ghost, F\* `erasable`, Lean `Prop`, Idris quantity 0); trust is audited (`dafny audit`, `#print axioms`, `pragma Assume` justification). Effra takes erasure by construct and audited trust; it has no prover, so `verified` is reserved (§2.9) |
| **Eiffel** `85212702` | `require`/`ensure`/`invariant`, monitored at run time; blame client or supplier | — | — | Monitoring levels per class | Effra checks laws at test time, not per call; only the host boundary checks at run time |
| **Racket contracts** `ce815851` | Boundary contracts with blame (`blame.rkt:490-540`) | — | — | — | Effra's adapter is a boundary contract: checks only where the host enters |
| **TLA+ / TLC** `94d0c507`, **Apalache** `8677897c` | Invariants over reachable states; numbered counterexample traces (`MP.java:385,753`) | — | — | `-simulate` depth 100; BMC length 10 | Machine laws take reachable-state quantification and trace counterexamples (§1.6), bounded |
| **GDP** `853abfd4`, **gdp-ts** `ebd0af9c` | `SortedBy comp name` evidence; `axiom`/`assert` minting (`Main.hs:26-67`) | — | — | — | Evidence indexed by a comparator; Effra names the declaration instead of a rank-2 fresh name, and mints only under a lawful satisfaction |
| **Bend 2** `51ab8e8e` | **First-class `law`**, proven by a paired `def` (`GUIDE.md:279-305`); "a law with no def is an open claim" (`:341`); template laws over operations (`template_law.bend:7-31`) | — | — | — | The nearest construct; Effra tests where Bend proves, and an open claim is Effra's `unresolved` |

**Compile-to-host languages.** None has a law construct.
- **Elixir** `91ee75bb`: doctests are executable specs (`lib/ex_unit/lib/ex_unit/doc_test.ex:6-22`). Property testing is userland StreamData `7e161eb7` through the documented extension point `ExUnit.Case.register_test/6` (`case.ex:665-680`), a macro route Effra does not have. ExUnit prints its seed and mixes it with module and test names (`ex_unit.ex:387-392`), the same idea as Effra's per-law seed. StreamData's generators are explicit values because Elixir has no static types to direct them. Elixir's pipe is a pure call rewrite (`kernel.ex:L4438-L4509`), so wrapping a piped call changes nothing but the call; Effra's laws likewise add no code, so wrapping a law-checked function is unaffected.
- **MoonBit** docs `8d9f3ba2`, core `e96ede8b`: built-in `test` blocks and snapshot tests (`next/language/tests.md:7-87`), a core `quickcheck` package with compiler-derived `Arbitrary` and `Shrink` (`derive.md:103-119`), a **fixed default seed of 37** (`quickcheck/driver.mbt:405-416`), greedy external shrinking, and experimental `moon prove` through Why3, whose integers are mathematical, so "arithmetic proofs do not currently model runtime overflow" (`verification.md:454-473`). It is the closest compile-to-host precedent: compiler-derived generators and a fixed seed, both of which Effra takes. It has no law bundle and no build refusal.
- **Gleam** `52e735c8`: nothing in the compiler; `gleam test` runs a module's `main` (`compiler-cli/src/run.rs:103`). qcheck `cf683fc3` is userland, with random seeds that are never printed.
- **ReScript** `30ce3698`: no construct; docstring code-block extraction is tooling (`tests/docstring_tests/DocTest.res`), and `rescript-fast-check` is a userland binding last published in 2022 (found with `npm view`, not inspected).
- **Roc** `3ee70f0c`: a compiler-known `expect`. A top-level `expect` is a test that `roc test` runs, and an inline `expect` is "omitted from optimized builds (such as the default for `roc build`)" (`docs/langref/statements.md:192-243`). The lowering runs the same checked program with inline expects either kept or omitted (`src/compile/test/shared_expect_lowering_test.zig:52`). It is the nearest compile-to-host precedent for a construct that erases from build output, and Effra's laws erase the same way (§5.1). Effra's `law` differs in three ways: it is quantified over generated values, a refutation refuses the build, and a lawful satisfaction gates evidence.
- **Scala.js and cats** `4a2ea736`: `kernelLaws` is a `crossProject(JSPlatform, JVMPlatform, NativePlatform)` (`build.sbt:119`), so one discipline/ScalaCheck law suite runs on JavaScript, the JVM and Native. It is the closest precedent for one law suite on several targets. It does not compare the targets' outcomes, and laws gate no evidence. Effra adds both: identical cases from one seed, receipts compared field by field, and a disagreement as a refutation (§3).
- **PureScript** `purescript-quickcheck-laws` (userland; not verified locally, cited from its README at `github.com/purescript-contrib/purescript-quickcheck-laws`): law suites for the Prelude classes, Ord included, written as ordinary QuickCheck tests. It is the compile-to-JavaScript precedent for law bundles. Like cats, it is a library: it can fail a test run but cannot refuse a build or gate evidence.
- **What they share and Effra does not.** Each keeps one runtime per target and treats properties as tests a library adds. Effra makes laws a construct for two reasons:
  - a law gates the build and gates evidence;
  - Effra intends more than one provider of a construct, built-in or userland (#124, §7), and only shared laws can hold them to one meaning.

**Lowering paths of the compile-to-host languages**, from review 1's comparison, checked at the pins above:

| Language | Laws, properties, evidence | Lowering path | Effra takes or differs |
| --- | --- | --- | --- |
| Gleam | No construct; `gleam test` runs the `<name>_test` module's `main` (`run.rs:100-106`); qcheck is userland | Tests compile through the ordinary Erlang and JavaScript emitters; nothing is erased; tests are separate modules | Laws gate the build and evidence, which a userland library cannot do |
| ReScript | No construct; doctest extraction is tooling | Ordinary JavaScript emission | Tooling is not a construct |
| Elixir / StreamData | `property` macros register ordinary tests (`case.ex:665-680`); seed mixed with module and test name | Macro expansion into ordinary Erlang forms; no law runtime | Takes the per-law seed; Effra has no user macros, hence the construct |
| MoonBit | Built-in `test`; compiler-derived `Arbitrary` and `Shrink`; fixed seed 37 | Test blocks lower to test functions run by `moon test`, apart from release builds | Takes derivation and a fixed seed; adds bundles, build refusal and evidence |
| PureScript | Prelude documents the laws; `quickcheck-laws` runs them in userland | CoreImp to JavaScript, with inlining and tail-call elimination | Law bundles as a construct, not a library |
| Roc | Compiler-known `expect`, erased from optimized builds | Inline expects kept or omitted at lowering | Erasure by construct; adds quantification and gating |
| Scala.js / cats | One law suite cross-built for JavaScript, the JVM and Native | Scala.js IR with link-time dead-code elimination | One suite on several targets; adds cross-target comparison and evidence |

**Meaning drift without laws** (§7). F# computation expressions let a builder object define `let!`, `return` and the rest, and Haskell's `RebindableSyntax` makes `do` notation use whatever `>>=` is in scope (F# language specification, "Computation Expressions"; GHC User's Guide, "Rebindable syntax"; neither verified locally). Both let a library swap the meaning of a familiar construct, and neither checks a law. Effra keeps the swap, a provider chosen by `satisfies` and provision, and adds the check.

**Composability sources.**
- **Haskell monad and arrow laws.** The Haskell 2010 Report states the three monad laws on class `Monad` (§6.3.6; not verified locally), and Hughes's arrows (Science of Computer Programming 37, 2000) state theirs as equations (not verified locally). PureScript, a compile-to-JavaScript language, states them in documentation only: left and right identity (`purescript-prelude` `fc4c9d23`, `src/Control/Monad.purs:29-30`) and associativity (`src/Control/Bind.purs:45`). Effra states the same laws as runtime-contract laws that run (§1.7).
- **Koka** `9c55695d`: recovery is an ordinary function over a handler (`lib/std/core/exn.kk:54-56`), so wrapping composes through row-polymorphic effect types; Effra's lossless wrapping relies on rows unit 4a for the same property.
- **Go** 1.27.0: `context.WithDeadline` documents the nested-deadline law: "If the parent's deadline is already earlier than d, WithDeadline(parent, d) is semantically equivalent to parent" (`src/context/context.go:616-620`). Effra turns that sentence into `nestedDeadlinesTakeTheMin`.
- **Effect** 4.0.1: Order and Equivalence state their laws in comments only (above); nothing runs them.
- **Hickey, "Simple Made Easy"** (Strange Loop 2011, talk; not a pinned source): simple means not interleaved. A law is a separate declaration about a subject, so it can be added or removed without touching the subject's code, and the erasure diff shows nothing else changes.

## 10. Ladder check

| Question | Answer |
| --- | --- |
| Rung | 4: a visible construct, because discharge, receipts, build refusal, generator derivation and mint authority are compiler-special (`docs/design.md:27`). |
| Keyed on a name? | Only on the words `contract`, `law`, `satisfies`, `evidence` and the satisfaction's contextual `trusted`, `unresolved`, `cases`, `target`; never on Order, compare or Codec (§5.3). |
| Readable with Go's simplicity | Contracts read like a Go interface with laws. Law bodies are ordinary boolean code, with no combinator helpers; `and`/`or`/`not` and a `when` guard are proposed to #135 (§1.1, preview). |
| Same meaning in TypeScript, no `any` | Laws erase; evidence projects to a private-member brand outward and to the carrier inward, with 0 `any` (§5.2). |
| Erases into Go | Byte-identical without laws; evidence erases (§5.1). |
| Two or more unrelated clients | Order, codec round-trip, services, machines and the runtime contract. |
| Drift into a proof language | None: predicates only, no proof terms, `verified` reserved. |

## 11. Decision record

**Decided 2026-10-12 (spike #125, revision 1; amended in revision 2 after review 1).**

- **Chosen.**
  - Laws are declared in a `contract`, a `service` or a `machine`, with typed universally quantified parameters. A law body is pure, or an `effect law` with explicit `raises` and `uses` over deterministic providers. A law is not a value, and no law body mints.
  - **A law body reads as ordinary boolean code** (revision 2).
  - A `satisfies` declaration in the subject's owner module binds the subject and **must** state a case budget (EF175). It may bind a target (`target go`) and may mark laws `trusted` or `unresolved` with a justifier. Each binding's type must match the contract entry's (EF177).
  - **`Order` has four laws:** reflexive, antisymmetric, transitive and total (revision 2).
  - **Discharge is one deterministic runner:**
    - compiler-derived structural generators and choice-sequence shrinking;
    - a fixed, canonically parsed root seed, with per-law seeds from the logical law;
    - the same algorithm on Go and JavaScript;
    - one receipt per target, whose statuses are lawful-runtime-contract's categories.
  - **Gating receipts are canonical:** keyed by the source's location, bound to the revision (source and dependencies) and to root seed 0. Exploration seeds write separate, non-gating receipts. L2 adds the toolchain and provider revision.
  - **The commands:**
    - `ef test` runs laws;
    - `--target both` compares receipts and persists a disagreement as a refutation in both;
    - the gate runs both targets;
    - `ef build` reads the gating receipt, refuses refuted laws, and refuses evidence with no current passing receipt (EF174);
    - `ef check` never executes.
  - Conditional satisfactions discharge per instantiation reached, and compose from their parts.
  - **Evidence:**
    - a nominal wrapper over its carrier;
    - indexed by the declaration bound at the site, by binding identity, by a satisfaction with passing laws;
    - constructed only in its kind's owner module;
    - never indexed by a closure, a parameter or a host-passed function;
    - covariant in results and contravariant in parameters.
  - **The host boundary follows the four rules:**
    - outward forms i and ii by provenance, with generic forwarding projecting the carrier and a private-member brand;
    - inward re-establishment through a declared admission check, refusing a carrier with no snapshot plan;
    - a deep snapshot read once per field;
    - private canonical bindings.

    An effect export brands and freezes its success value.
  - **Provider protocols.** A contract over providers is a provider protocol, compared across providers by logical law. R1-R15 are Effra laws in a `Runtime` contract, discharged per provider. R11 observes through a harness-owned channel, trace fidelity is a trusted decision, and host fixtures are a named discharge mode for unobservable behaviour.
  - The surface spelling is provisional until #135.
- **Rejected: laws as a library of property tests** (fast-check or Effect `Arbitrary` called from tests). The compiler could neither refuse a build on a refuted law nor gate evidence on lawfulness, and it would key on a library's identity to do either, which `docs/design.md:27` forbids.
- **Rejected: laws on the receiver method alone.** No home for laws that relate methods, and a `where` clause could not ask for them (spike-where §2.10).
- **Rejected: user-written `Arbitrary` instances.** A second type-directed lookup beside owner-module resolution, and a missing instance would move the refusal into whichever module forgot it. Explicit generators remain possible later as a named generator on a satisfaction, if a type needs one.
- **Rejected: random seeds by default.** A gate verdict must depend on the source alone. Exploration with other seeds is a separate, non-gating job.
- **Rejected: rose-tree integrated shrinking.** Correct, but it resets inner progress under `bind`, and porting two rose-tree libraries byte-for-byte across targets is more code than one choice buffer.
- **Rejected: running laws on `ef check` with a budget.** Inspection would execute user code, and CLI, MCP and LSP share one checked snapshot that must not.
- **Rejected: running laws per build.** It doubles build cost and makes builds execute user code (§3).
- **Rejected: a branded inward parameter** (naive trust). Three no-cast routes deliver non-evidence and give a wrong answer (§5.2, counterfactual).
- **Rejected: an `Omit<S, symbol> & class` brand.** It refused spread, bivariance and augmentation in a scratch probe, but forgetting evidence then needs a double cast, and it buys nothing once the inward parameter is unbranded.
- **Rejected: host fixtures as the only form of R1-R15** (§7).
- **Rejected for now: `verified`** (§2.9).
- **Rejected: projecting the carrier when no receipt exists** (revision 2, for Q8). It would make a library's TypeScript types depend on whether `ef test` ran first. Refusal (EF174) keeps one projection per source (§2.10).
- **Rejected: a missing budget as unresolved** (revision 2, for Q2). It hid a forgotten budget behind a minting refusal elsewhere in the program (§2.6).
- **Rejected: per-provider seeds** (revision 2). Two providers of one law would run different cases, so they could not be compared, and a target-bound provider would have no partner receipt (§7).
- **Rejected: resolving an evidence index by spelling** (revision 2). A shadowing parameter minted canonical evidence over a caller's compare (review 1, M2).
- **Rejected: helper combinators in law bodies** (`implies(both(…))`, revision 2). They read like Lisp, and ordinary boolean code says the same thing (§1.1).

## 12. Follow-up units

Prerequisites costed elsewhere:
- spike-rows unit 3 (receiver methods, explicit type arguments) and unit 4a (#128, whole-value type parameters and the recipe-parameter contract);
- spike-where W1 (`where` clauses, conditional methods) and W3 (projection and frozen companions);
- machines (the checked machine declaration);
- #120 (sequences).

Every probe in §6.1 is acceptance for the unit named beside it. Each must be refused, or correct, on Go and on JavaScript, with the message pinned. Revision 2's added scope is costed in each estimate.

| Unit | What lands | Tests and acceptance | After | Estimate |
| --- | --- | --- | --- | --- |
| **L1. Contracts, laws and satisfactions** | Parser, formatter and LSP spans in #135's spelling (including its choice on `and`/`or`/`not` and `when`); contracts as groups of `where` entries; **hygienic, scope-aware elaboration** of laws into checked functions; satisfactions in the owner module with a required `cases` (EF175), `target`, `trusted` and `unresolved`; **entry types checked against the contract** (EF177); laws not values (EF178); EF002-family diagnostics; interface-summary transport (schema bump, round trip, `TestEvidenceSchemaIsClassified`); inspection facts in CLI and MCP; reachability in test mode. | The prototype's declaration tests and refusals with messages; probes m2, m3 and Q2; the four-law `Order` with the 0/1 compare as a negative control (probe M1); a cross-module satisfaction; inherited laws run once; the name-neutrality screen; check time with laws measured on a quiet machine (§3). | rows 3, W1, #135 | L, 5 to 6 days |
| **L2. The runner and receipts** | Generators for scalars, records, enums, generic data (after 4a) and recursive data; the choice-sequence shrinker; logical-ID seeds; the canonical seed parse (EF176); Go and JavaScript harnesses sharing one algorithm; `ef test --target both`; **canonical gating receipts** (location-keyed, revision- and seed-bound, exploration on its own path); **persisted disagreement** (EF172); **toolchain and provider revision** in receipts and staleness (Q7); gate integration. | The prototype's results on both targets, with byte-identical receipts; a generic `Option[User]` and a recursive list law; the seed survey, including the near-order defect; probes M6a, M6b, M6c and m1; a toolchain change makes a receipt stale. | L1, 4a | L, 5 to 6 days |
| **L3. Effect, service and provider-protocol laws** | `effect law` with `uses`; refusal of live providers; service satisfactions per provider with `using`; **target-bound providers and the provider comparison** by logical law; harness-owned observers (an `Assert`-style channel and a counter fixture); one fresh scope per case. | The codec law; a service law over two providers, one deliberately lossy; probes M7-runtime, M7-observer and M7-providers; a live provider refused; a failure raised in a law shows in the counterexample. | L2, 4a | M, 3 to 4 days |
| **L4. Evidence** | Evidence kinds as nominal wrapper types (generic-transparent after 4a), **covariant in results and contravariant in parameters** (replacing EF189's blanket refusal); construction private to the kind's owner module; **index resolution by binding identity** to declarations and canonical keys; evidence through `run` and recipes; admissions; EF180-EF187; the build verdict (EF174); the mint-soundness law (§13 Q4). | The prototype's evidence tests; probes M2, M4-apply, M4-let, M4-scope, M4-binder and M5 as refusals, and **M4-run as an acceptance**; a variance pair (result accepted, parameter refused); cross-module minting refused; `Option[SortedBy[…]]` through a generic; nested evidence in a record. | L1, 4a, W1 | L, 5 to 6 days |
| **L5. Host evidence boundary** | The JavaScript adapter (snapshot, admission, deep freeze) for every input and output occurrence; **refusal of a carrier with no snapshot plan** (EF188) and snapshot plans for generic carriers; **the recipe rule** for effect exports; the private-member brand; `benchmarks/laws/ts` promoted to a generated-consumer fixture. | Every row of §5.2 with its pinned outcome, **including the read count** (each argument field read once, not merely a correct output); probes M3 and M4-effect-export; a TypeScript-supplied compare to an exported generic sort projects the carrier; nested and callback occurrences; the counterfactual kept; 0 `any`; the adapter cost re-measured (§5.1). | L4, W3 | M, 2.5 to 3 days |
| **L6. Machine laws** | Reachable-state laws with `depth`; event-sequence generation; trace counterexamples shrunk by length first. | A Session invariant tested; a deliberately broken step refuted with a numbered trace; the same trace on both targets. | machines, L2 | M, 2.5 to 3 days |
| **L7. Conditional satisfaction** | Per-instantiation discharge with composed generators; verdict composition from parts; library-declared instantiations; canonical keys in receipts. | `Pair[i64, User]` and `Option[Option[User]]` discharged; an unresolved part makes the composite unresolved; Trio's trusted transitivity replaced by a tested composite. | W1 conditional methods, L2 | M, 2 to 3 days |
| **L8. Docs** | `docs/design.md:42` extended; GLOSSARY "Contract", "Satisfaction", "Evidence kind", "Admission", "Law receipt", "Provider protocol"; lawful-runtime-contract cross-reference; CHANGELOG; a new preview, superseding `docs/previews/2026-10-12-laws.md`, once #135 picks the spelling. | Docs review. | L1 | S, 0.5 day |

**Total:** about 25.5 to 31.5 implementor-days plus review rounds. Revision 1's estimate was 22 to 27.5; the increase is the review-1 scope in L1, L2, L3, L4 and L5.

**Order against spike-rows phase 2 and spike-where W1-W6:**
1. rows unit 3, then 4a (#128), as spike-rows plans;
2. W1, with W2 and W6 beside it, as spike-where plans;
3. **L1** after W1 and #135;
4. **L2** after L1 and 4a; rows 4b and 4c can run beside it;
5. **L3 before rows unit 6**, so provide's library declaration carries its distribution law, and rows 5 to 7 gain composition laws as acceptance;
6. **L4** after L1 and W1; W3 then **L5**;
7. **L7** after W1's conditional methods; **L6** once machines lands;
8. #124's RS1 laws after L3 and #120, with its trace model, counter fixture and reference checker started in parallel.

## 13. Open questions

Closed in revision 2:
- **Q1. The language evolution preview.** Added as `docs/previews/2026-10-12-laws.md`, labelled Tested candidate and Proposed, with an index row.
- **Q2. A missing case budget.** Decided: **an error**, EF175 (§2.6).
- **Q7. Toolchain and provider revision in receipts.** Decided: added in L2, with the cost stated (§2.2).
- **Q8. A build that mints evidence without a current receipt.** Decided: **refused**, EF174. EF173 stays a note only for laws that gate nothing (§2.10).

Open:
- **Q3. Which functions does EF185 cover?** The prototype refuses every function with an evidence parameter and no admission, because it has no `pub`. Recommendation: only `pub` functions reachable from the host boundary, once `pub` exists; private functions take evidence freely.
- **Q4. Should minting be tested?** A mint asserts a fact nothing checks. Recommendation: **yes**, by a law the compiler derives for each minting function whose parameters are generatable: `admissionCheck(mintingFunction(args))`, using the kind's declared admission check (`sortedTrio(sortTrio(t))`). A kind with no admission check keeps its mint sites as trusted decisions in inspection.
- **Q5. Keyword budget.** The prototype adds `contract`, `law`, `satisfies`, `evidence`, `mint`, `admit` and the contextual `target`. Recommendation:
  - keep the first four;
  - drop `mint` in favour of construction under the owner-module rule (§1.1);
  - make `admit` a clause of the evidence declaration;
  - leave `and`, `or`, `not` and `when` to #135.
- **Q6. Regression examples.** Recommendation: allow explicit examples on a satisfaction (`example transitive(a, b, c) = …`), run before generated cases on both targets, so a counterexample found by exploration stays in the gate, as proptest's checked-in regression seeds do.
- **Q9. Boolean connectives or a guard?** For #135, with the preview as the comparison: `and`/`or`/`not` alone, or with a `when` guard whose false cases are counted vacuous passes (§1.1). Recommendation: the keywords in any case, because they serve all code; the guard only if the owner finds it clearer, since it adds one clause to the law form.
