# Laws: obligations types cannot state, their discharge and their evidence

**Status:** design note and decision record for the spike in [#125](https://github.com/cevr/effra/issues/125), revision 4, 2026-10-12. It changes no compiler behaviour.
- **Revision 2** answered review 1 (seven Major and seven Minor findings), adopted the coordinator's decisions on Q2 and Q8, and added the owner's 2026-10-12 directions on guardrails, portable providers and readability.
- **Revision 3** answers review 2. Its four Major findings were paths that revision 2 called closed and that were still open:
  - a single-target run erased a cross-target refutation (N1, §2.2);
  - a satisfaction bound to one target authorized evidence on the other (N2, §2.10);
  - EF188 checked only a parameter's root node (N3, §4.6);
  - reordered named arguments with an omitted default bypassed EF184 (N4, §4.4).

  Each is now closed in the prototype at its root cause, with a probe that fails before the fix and passes after it (§6.1).

- **Revision 4** answers review 3. Its Major finding (P1) was that revision 3's hygiene disclosure rested on a false safety claim: a callable local spelled like the bound function captured the law's entry, so the law reported `tested` for a function that breaks it, and evidence was minted over that function. Law bodies are now elaborated binding-aware: a contract entry is a binder of the law's enclosing scope, bound to the declaration the satisfaction names, and it is never substituted by spelling (§1.8). Revision 4 also answers review 3's Minors and both Optionals:
  - an unreadable refutation ledger or receipt is refused, never read as absent (p1, EF191), and §2.2 says what deleting `dist/` or a fresh checkout loses;
  - "a changed source digest", not "a new revision", clears the ledger (p2);
  - EF190 decides by declaration kind, so it covers declared generic types (p3);
  - evidence rests on every satisfaction that binds its index (q1), and a `--live` receipt never gates (q2).

- **Line references** are at `main` `13e62ad4` unless stated otherwise. This branch has merged `main` at `8dca628a`.
- **The prototype** is on branch `wf10/spike-laws-proto`, head `ad08a82e`, six commits on `13e62ad4`:
  - `8e781dc3`: the construct, runner and evidence;
  - `e239e708`: the host evidence boundary;
  - `5f428883`: the end-to-end run (revision 1's head);
  - `63164e8a`: the review-1 fixes and their probes (revision 2's head);
  - `8b02919b`: the review-2 fixes and their probes (revision 3's head);
  - `ad08a82e`: the review-3 fixes and their probes.

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
   - An exported call that carries evidence costs about 0.8 µs, measured, and the adapter is almost all of it: the same bodies without the adapter take 9 and 29 ns (§5.1).
4. **Every evidence, receipt, provider and hygiene hole that reviews 1 to 3 found has a probe** (§6.1). Each probe fails on the prototype before its fix and is refused or correct after it, on Go and on JavaScript where both apply.
   - **Now.** At `ad08a82e`, 72 of 72 probe lines pass.
   - **Before revision 4** (`8b02919b`), 19 lines fail: eleven for P1 (hygiene), four for p1 (unreadable ledgers and receipts), two for p3, and one each for q1 and q2. The other 53 pass. They are revision 3's probes and controls, plus three new lines whose behaviour revision 4 does not change: the P1 callable control, the q1 order in which 8b02919b happened to refuse, and the fresh-checkout sequence that §2.2 documents.
   - **Before revision 3** (`63164e8a`), 12 of revision 3's 53 lines failed.
   - **Before revision 2** (`5f428883`), 37 of revision 2's 43 lines failed.

   The probes cover:
   - evidence indexed by a shadowing parameter, and generic forwarding;
   - a generic carrier with no snapshot plan, at the root or nested in a record or enum;
   - function values, `scope` bodies and results, pattern binders, and reordered named arguments with an omitted default;
   - `run` and effect exports;
   - builds with no receipt, from another directory, or after an exploration seed;
   - a cross-target disagreement, persisted and kept through a later single-target run;
   - satisfactions bound to one target, including declared generic types;
   - root-seed parsing, entry types, laws as values, and a missing budget;
   - unreadable ledgers and receipts, and a `--live` receipt;
   - every satisfaction that binds an index, in either declaration order;
   - law bodies whose binders, labels or nested scopes are spelled like the bound function or a contract type parameter.

   **Hygiene is now fixed, not disclosed.** Revision 3 said each capture case was "refused at check, never miscompiled". Review 3 showed that was false: a callable local spelled like the bound function captured the entry, the law reported `tested` for a function that breaks it, and the build branded evidence over that function. The guard was an accident. EF103 ("local value is not callable") fired only because the three probes used non-callable binders. Revision 4 does not complete the guard. It removes the substitution. A contract entry in a law body resolves through the law's enclosing entry scope to the declaration its satisfaction binds, so capture cannot be expressed (§1.8). The review-3 program is now refuted at `a = 0`, like its control, and its build is refused. Each former guard case is accepted and runs the bound function (§6.1).
5. **The host boundary is fail-safe under every listed attack** (§5.2). Under strict tsc 6.0.3, with 0 `any` keywords, each of these is refused or harmless:
   - laundering a reversed compare;
   - spread override, array covariance, method bivariance and property covariance;
   - both aliasing routes;
   - a getter, and a proxy that answers differently on each read (each field read exactly once, including the nested `User.age` and `User.name` that decide sortedness);
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
- **Contract entries are an enclosing scope, resolved by binding (revision 4, review 3 P1).** For each satisfaction, a law elaborates into its own checked function, and only the header changes: `law` becomes `fn`, the name becomes `law__C__S__l`, and the result becomes `bool`. The body's tokens are never rewritten. The resolver opens the function with one binder per contract entry, outside the parameters, and binds each to the declaration the satisfaction names (`same = sameI` binds `same` to `sameI`). A use of `same` resolves to that binder, and the resolved expression is a global reference to the declaration `sameI`: it names the declaration, not a spelling. What follows:
  - **Nothing can capture an entry.** A parameter, local, pattern binder or nested binder spelled like the bound function (`sameI`) is an unrelated binder, and a record field or named argument spelled like the entry (`same`) is an unrelated label. The law still calls the bound `sameI` (probes P1-callable, P1-parameter, P1-local, P1-binder, P1-nested, P1-record-label, P1-named-argument).
  - **Shadowing an entry is ordinary scoping.** A parameter or pattern binder spelled like the entry (`same`) shadows it, as any Effra binder shadows an enclosing one. A `let` cannot redeclare a visible binder, as everywhere in Effra (EF101), and the message names the binding: "duplicate local same: same is the contract entry Eq.same, which this law's satisfaction binds to sameI; a let cannot redeclare a visible binder" (probe P1-entry-let).
  - **A satisfaction entry binds one declaration, named on its own.** `same = sameI` is accepted; an expression in that position is EF002 "entry same binds a declaration, named on its own".
  - **Contract type parameters are type arguments.** In an annotation inside a law body, `T` parses as the satisfaction's type argument (`i64`, `Box<i64>`) through a sub-parser, so a record or variable spelled `T` is never captured (probe P1-type-parameter). `T.x` and `T<…>` are refused: "contract type parameter T names a whole type". Entry type checking (EF177) still substitutes inside type-only token ranges, where no binder can exist.
  - **Revision 3's mechanism, for the record.** It replaced every name token whose spelling was an entry with the bound function's spelling and then resolved lexically, so a binder in scope with that spelling captured the reference. Review 3's callable local made a law report `tested` for an irreflexive `sameI`, and the build branded `Same<bigint, "sameI">`. A complete refusal list of spellings was rejected as the fix, because it would keep a guard where the construct can make the error unrepresentable.
- **An effect law** (`effect law`) may `run` effects. Its failures and services are explicit, `raises {…} uses {…}`, as every public effect contract is. A raised failure refutes the law with the failure recorded, which is how a codec's decode failure becomes a counterexample (§1.5). The prototype checks `raises` and `uses` as part of the elaborated law function, and its harness supplies the built-in test providers (`probes/runtime.ef` uses `Assert`). Refusing live providers and naming test providers with `using` are follow-up L3. Revision 1 said `uses` was not parsed, which was wrong.
- **Only deterministic providers.** A law's services are filled by the subject or by providers the satisfaction names, and each must be deterministic: `TestClock`, `TestScheduler`, in-memory stores. `LiveClock` and other live providers are refused, because a law must give the same result on both targets from the same seed (§3.2).
- **One fresh scope per case.** Each case runs in its own scope with a fresh test provider, closed before the next case, so cases cannot leak state into each other.
- **A law is not a value.** It cannot be called, passed or stored. Its name resolves only in receipts, inspection and satisfactions.
  - **Calling it.** The prototype elaborates each law into a function (`law__Order__User__reflexive`), and a call to or reference of that function is refused with EF178: "… is the law Order.reflexive@User, and a law is not a value" (probe m3-call). Revision 3 exempted law bodies from EF178; revision 4 applies it there too, so a law cannot call another law (probe P1-law-call).
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
- **Refutations stand (review 2, N1).** A refutation is a counterexample already established, and another passing sample cannot invalidate it. So a refutation stays in force for its inputs until one of two events: an agreeing run on both targets over the same inputs, or a changed source digest.
  - **The prototype's inputs** are the `source` digest and the root seed (`LawRefutations.ForInputs`). The `source` digest covers the source file alone, so an edit to a dependency alone keeps the ledger. That is the conservative side: it can keep a refutation too long, never clear one too early. Revision 3 said "a new revision", which overstated it, since `revision` covers dependencies too (review 3, p2).
  - **L2's input identity** is the full one: source, dependencies and toolchain (Q7), plus whether the run was live (q2, below).
  - **The ledger.** Refutations live in a ledger beside the receipts, `dist/<name>.laws.refutations.json`, shared by both targets and keyed by those inputs (prototype `LawRefutations`).
  - **A single-target run never downgrades a refutation.** `ef test --target go` adds its own refutations and removes none. If it passes a law that the ledger refutes, its receipt still says `refuted`. It also reports the override as `lawRefutationsStanding` and exits non-zero with EF172.
  - **A disagreeing run on both targets** adds each disagreement as a refutation that holds on both targets.
  - **Only a complete run on both targets that agrees** replaces the ledger, with the refutations both targets agree on. An incomplete run, where one target produced no receipt, replaces nothing: each half that ran counts as a single-target run.
  - **A changed source digest or root seed** is new inputs, and the old ledger does not apply to it.
  - **Every reader applies it.** Every receipt written for the same inputs applies the ledger, and so does every build verdict, before the receipt is even read. A refutation therefore survives a later single-target run, a deleted receipt or a stale one: `ef build` refuses with "EF172 a cross-target refutation stands for this source digest" (probe N1).
  - **Revision 2's gap.** The disagreement was written into the two receipts only. `ef test --target go` then overwrote the Go receipt with `tested`, and `ef build --target go` was admitted, while the JavaScript receipt still said `refuted`.
- **Unreadable is not absent (revision 4, review 3 p1, EF191).** A ledger or receipt that does not exist is absent. One that exists but cannot be read, or that is JSON but not a ledger, is refused, because it may hold a refutation. Revision 3 read both as absent: after the `deep.ef` disagreement, a ledger of `{` let `ef test --target go` start a fresh one and `ef build --target go` succeed.
  - **The strict decoder** (`DecodeLawRefutations`) refuses unknown fields, trailing data, another schema version, a missing source digest or root seed, a missing refutation list, an entry with no key, a target other than go, js or both, and a single-target entry with no ID.
  - **`ef test --target go`** refuses before it writes anything: "EF191 the refutation ledger dist/deep.laws.refutations.json is unreadable (unexpected EOF): it is refused, never read as absent, because it may hold a refutation that stands; run ef test --target both, which replaces it when Go and JavaScript agree, or delete it to discard its local history deliberately". The ledger keeps its bytes, and the Go receipt stays `refuted` (probe p1-ledger-malformed).
  - **`ef build`** on either target refuses with EF191 for an unreadable ledger (p1-ledger-malformed, p1-ledger-schema), and for an unreadable receipt even when no evidence rests on the laws and an absent receipt would only give the EF173 note (p1-receipt-malformed, p1-both-malformed).
  - **`ef test --target both`** with an unreadable ledger, when the targets disagree, still writes the disagreements into both receipts, leaves the ledger untouched and exits with EF191. When they agree it replaces the ledger without reading it, as it replaces any ledger, which is the documented way out.
- **The ledger is local history, not evidence (review 3 p1).** It lives in `dist/`, a build-output directory, so it is neither reproducible nor durable.
  - **Deleting `dist/`, or a fresh checkout,** discards it. After that, `ef test --target go` alone admits the Go build: a single-target run has nothing to keep (probe p1-fresh-checkout, first half).
  - **The gate is what makes that acceptable.** It runs `ef test --target both` from a clean tree, which re-derives every deterministic disagreement and refuses both builds again (probe p1-fresh-checkout, second half). A disagreement that does not reproduce from a clean tree was host-dependent, and §3 already treats it as such.
  - **The durable form is in source.** A counterexample worth keeping becomes an explicit example on the satisfaction (Q6), which runs before generated cases on both targets. proptest checks its regression seeds into the repository, and Hypothesis pairs a disposable local example database with explicit `@example` decorators; Effra takes the same split.

Each receipt carries (prototype `internal/compiler/laws_receipt.go`):
- **Header:** `schemaVersion`, `revision`, `source`, `target` and `rootSeed`, plus `live: true` for a run under `ef test --live` (revision 4, review 3 q2). `--live` admits live capabilities, so the verdict can depend on the host; a live receipt is never current for a build, which refuses with EF174 "(the receipt is from ef test --live, whose live capabilities make the verdict depend on the host)" when evidence rests on it, and an ordinary run then admits the build (probe q2-live).
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
3. no current receipt on this target: none, a stale revision, an exploration seed, or a live run (revision 4);
4. an unavailable target or provider: a satisfaction bound to the other target (§7).

A missing budget was a fifth source in revision 1; it is now the error EF175 (§2.6).

**The rule (Q8, decided).** An unresolved law refuses a build exactly when evidence rests on it, and only then.
- **Check time.** Sources 1, 2 and 4 are known when the program is checked, which is per target. They block minting evidence through that satisfaction at check time (EF181, §4.3).
- **Target-scoped authority (review 2, N2).** Mint and admission authority resolve for the build target. A satisfaction bound to another target (`… target go` on a JavaScript build) gates nothing there. Its laws never run on that target, so no evidence can rest on them.
  - **At check time,** `satisfactionsFor` takes the target. When an index's only satisfaction is bound elsewhere, a mint is refused (EF181) and so is an admission (EF187): "compareUsers is bound to Order.compare only on go, by GoOrder satisfies Order target go; on js no satisfaction binds it, its laws never run here, and it mints nothing". Nothing is branded, and no `.d.mts` is written (probe N2-provider). On its own target, the same satisfaction mints, and the build after `ef test --target go` is admitted (probe N2-provider-control).
  - **At build time,** a skipped obligation is a refusal, never a discharge. Revision 2's `LawBuildVerdict` skipped the instances of a satisfaction bound elsewhere, and so treated an obligation it never ran as discharged. The check-time rule now makes that path unreachable, and the verdict refuses it in case it is ever reached.
  - **Revision 2's gap, in its stronger form.** With the 0/1 compare bound `target go`, `ef test --target both` refuted all four laws on Go. Yet `ef build --target js` exited 0 and branded `SortedBy` twice. Now that build is refused with EF181 (probe N2-refuted).
  - **`target` binds a provider, never a type (EF190).** A type has one representation on both targets, so its laws are one obligation: they hold on both or on neither. Binding them to one target would let one target's evidence rest on laws that target never runs. `User satisfies Order<User> cases 200 target go` is refused with "EF190 User satisfies Order target go, but User is a type with one representation on both targets …" (probe N2-type). `target` is for a provider that exists on one target only (`GoRuntime`, §7). In the prototype, a provider subject is any subject that is not a type; L3 gives it a declaration.
  - **EF190 decides by declaration kind (revision 4, review 3 p3).** A subject that names a declared type, generic or not, is a type. Revision 3 decided by whether the subject was a valid annotation. The bare name of a generic record is not one, so `Box satisfies Eq<Box<i64>> cases 5 target go` over `record Box<T: type>` checked. It is now refused, and so is the same over `enum Box<T: type>` (probes N2-generic-record and N2-generic-enum).
  - **A misspelled type is still a provider (review 3 q1, L3).** `Usr satisfies Order<User> … target go` is silently a provider subject until L3 declares providers. Review 3 found no false admission from it: the misspelled satisfaction only adds a target-bound binding, which can refuse a mint but never authorize one on the other target. Refusing it needs the declared provider, so it is L3's acceptance.
- **Evidence rests on every satisfaction that binds its index (revision 4, review 3 q1).** Revision 3's `satisfactionFor` took the first satisfaction declared that bound the function to the entry, so declaration order decided which one gated. Now mint and admission authority, and the build verdict's gating set, take all of them for the target.
  - Any one with an unresolved law refuses the mint, whichever is declared first: "EF181 … Twin satisfies Order with law total unresolved (needs a fixture); evidence cannot rest on an unresolved law, and it rests on every satisfaction that binds compareUsers to Order.compare" (probes q1-after and q1-before). Revision 3 refused only when `Twin` came first.
  - Each one's laws must have a current passing receipt.
  - Refusing duplicate bindings outright is left to L3. Requiring every binding to be lawful is the fail-safe reading, and it costs nothing now.
- **Receipt time.** Sources 3 and 4 are known only when receipts are read. `LawBuildVerdict` collects every satisfaction that gates evidence: one whose bound declaration indexes a mint or an admission in the program. If any of them lacks a current passing receipt for the build target (revision, target and root seed `0` all match, the run was not live, and every law tested or trusted), the build is refused before anything is emitted:

  ```
  EF174 evidence rests on laws with no current passing receipt on js (no receipt): User satisfies Order (for a mint of SortedBy<Trio, compareUsers>, the admission of SortedBy<Trio, compareUsers>); run ef test on this target
  ```

  So no `.d.mts` brands evidence that rests on laws nobody ran (probe M5; before revision 2 the same build was admitted with EF173 and its `.d.mts` declared `SortedBy`).
- **Laws that gate nothing** keep EF173 as a visible note, "laws are unresolved for this revision on go: run ef test to discharge them". This is the justifier lint lawful-runtime-contract asks of an unresolved obligation (`lawful-runtime-contract.md:205-207`); the codec program is the example.
- **Why refuse rather than project the carrier.** Review 1 offered both. Silently dropping the brand would change a library's TypeScript types depending on whether `ef test` ran first, so the same source would project two ways. Refusal keeps one projection and names the command that fixes it.

### 2.11 What tested means

A finite run is evidence, not proof (lawful-runtime-contract). `benchmarks/laws/runs/seed-survey.txt` (from `benchmarks/laws/proto-receipts.sh`, at `ad08a82e`) runs two programs under the default and exploration root seeds with a 200-case budget. Each exploration run writes its own receipt, and the script fails if the gating receipt ever stops carrying root seed `0`.

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
| `ef test` | **Yes**, on the selected target | Runs every runnable law beside the tests, writes the receipt (the gating path at seed 0, its own path for an exploration seed), prints EF170 per refutation and exits non-zero. An unreadable ledger refuses the run before anything is written (EF191). `--live` marks the receipt `live`, and a live receipt never gates. A program with laws and no tests is accepted. |
| `ef test --target both` | **Yes**, on Go and JavaScript | Removes stale receipts, runs both, and compares the receipts. A disagreement is added to the refutation ledger as holding on both targets and reported as EF172. Only a complete, agreeing comparison replaces the ledger (§2.2). |
| `ef build`, `ef run` | **No** | Reads the refutation ledger, then the gating receipt beside the source. Either one that exists but cannot be read is refused (EF191), never read as absent. A standing refutation refuses (EF172 for a cross-target one, EF170 otherwise), whatever the receipt says. So does a refuted law in the receipt (EF170). A missing, stale, exploration-only or live receipt refuses when evidence rests on it (EF174), and otherwise prints EF173 and builds. A build never executes user code. |
| The gate | **Yes**, through `ef test --target both` on every program with laws | Refutations and disagreements fail the gate. From a clean tree it re-derives every deterministic disagreement, which is why the local ledger can be lost (§2.2). |

**Same cases on both targets.** The harness generates each case from the per-law seed in the target's own code, with the shared algorithm (§2.4, §2.5), so the targets need not exchange cases. The `digest` field proves they generated the same ones.

**A cross-target disagreement is a refutation.** If the Go and JavaScript receipts differ in any law's status, case count, digest or counterexample, the law is refuted for both targets: one of them computes a different function from the same source, which is the unchecked-passthrough class of defect Effra refuses. The prototype detects it (`CompareLawReceipts`, EF172) and adds it to the refutation ledger (§2.2). A later `ef build` on either target is then refused, and a later single-target `ef test` cannot clear it.
- **A real disagreement.** `examples/laws/probes/deep.ef` makes one. Its law recurses up to 200 000 deep without a tail call, which overflows JavaScript's stack and not Go's. Go reports `tested`, JavaScript a defect, and both receipts end `refuted` with the failure "cross-target disagreement (EF172): status differs (go "tested", js "refuted"); …". `ef build` is refused on both targets (probes M6-disagree and M6-disagree-build).
- **What the fixture proves, and what is host-dependent (review 2, m1).** It proves two things: a status disagreement, and the refusal that follows it. Three other values depend on where the host's stack overflows, so they do not reproduce: the JavaScript case count, the shrunk counterexample, and so the case-count and counterexample parts of the failure text. For example, revision 2's committed receipt had 20 cases and `n = 9223372036854775807`, and review 2's rerun saw 18 cases and `29440`. The probes therefore assert only the statuses and the refusal, and they print neither host-dependent value.
- **The sequence that must stay refused** (probe N1, L2 acceptance):
  1. `ef test --target both deep.ef` refutes on both targets;
  2. `ef test --target go deep.ef` passes on Go and keeps the receipt `refuted`;
  3. `ef build --target go deep.ef` and `ef build --target js deep.ef` are both refused (EF172).

  Probe N1-revision is the control. After an edit to the source, the source digest has changed, the old ledger no longer applies, and the edited file's own Go run builds.
- **Before revision 2.** The disagreement was printed and no receipt changed, so the Go build was admitted (review 1, M6c). In revision 2, step 2 erased it (review 2, N1).
- **A target-bound provider with no counterpart.** A provider bound to one target (§7) with no provider of the same logical law on the other target compares as "missing on js" (or go). That counts as a disagreement, so it refutes on both. A protocol needs a provider on each target for its laws to gate.
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
  - **Arguments, by the call's own binding (review 2, N4).** A call with named arguments, defaults or both has a bound argument vector in evaluation order (authored arguments in source order, omitted defaults in their slots), together with that vector's argument-to-parameter mapping. The evidence check uses the same mapping that emission uses, so each argument is checked against the parameter it binds.
    - **Revision 2's gap.** The walker checked the bound vector by position whenever it existed. `wrapped(other: sortTrio(t), xs: t)`, with `wrapped(xs: SortedBy<Trio, compareUsers>, other: Trio, flag: bool = false)`, binds the vector `[sortTrio(t), t, false]` with mapping `[1, 0, 2]`. The walker checked `sortTrio(t)` against `xs`, while emission passed the unsorted `t` to `xs`, so `ef run` printed `false` for a user in the trio on both targets.
    - **Now** that call is refused on both targets (probe N4). The sound call, `wrapped(other: t, xs: sortTrio(t))`, which revision 2 wrongly refused, is accepted and prints `true` on both (probe N4-control).
- **The holes this closes** (review 1, M4). In the round-1 prototype, each of these was accepted:
  - `scope { search(t, x) }`, which printed `false` on both targets for a user in the trio;
  - `match b { Box.Value { xs } => search(xs, x) }` under an evidence parameter also named `xs`;
  - `apply(search, …)` and `let f = search`.

  Each is now refused on both targets (probes M4-scope, M4-binder, M4-apply, M4-let).
- **The scope of this guarantee.** It holds for evidence in top-level parameters and results, and for the outward effect-result form (the recipe rule, §4.6). Every other position is refused, not silently untracked (review 2, m2). The forms are listed with their diagnostics at the end of §4.6, and L4 and L5 replace each refusal with tracking.

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
   - **Every reachable node needs a snapshot plan, or the export is refused (EF188).** Before the adapter is emitted, the check walks every node reachable from each data parameter of an evidence-carrying export, through record fields and enum variant fields. A node with no snapshot plan refuses the export at check time and is never passed through. In the prototype, every generic record is such a node, for example `Holder<Trio>`.
     - **At the root:** "firstAge carries evidence across the host boundary, and its parameter h: Holder<Trio> has no snapshot plan" (probe M3).
     - **Nested:** "envelopeFind carries evidence across the host boundary, and its parameter e reaches e.held: Holder<Trio>, which has no snapshot plan" (probe N3-record), and the same through an enum variant, `p.held` (probe N3-enum).

     Callables at the root are exempt, because they are never snapshotted and never carry evidence (EF189). A callable nested in a data parameter has no plan, so it is refused like any other such node.
   - **The snapshot refuses what it cannot copy.** The emitted `__ef_snapshot` copies `i64`, `string`, `bool`, records and enums. Its default case now throws EF186 "has no snapshot plan", where revision 2's returned the host's value unchanged. The check-time walk makes that case unreachable, and the default is the run-time backstop (`TestSnapshotRefusesUnplannedNodes`).
   - **What it closes.**
     - **Before revision 2,** a root carrier with no plan skipped both the snapshot and the admission check: `return(a0)=>{return __ef_function_firstAge(a0);}` (review 1, M3).
     - **In revision 2,** a nested one passed through (review 2, N3). `record Envelope { held: Holder<Trio> }` was `checked:true`, and its adapter snapshotted `Envelope` but handed the body the host's own `Holder`. Review 2's attack used a getter on `held.value` that answered sorted to the admission check and unsorted to the body, so `envelopeFind(…, cy)` returned `false` for a user in the trio, with `value` read twice. This was where-dispatch's A3 attack, one level down.
   - **Current coverage.** The prototype admits only records and enums without type parameters, at every depth. L5 adds generic carriers with a snapshot plan per instantiation, and wrappers around an unsupported carrier stay in its acceptance.
3. **Stability: a deep, Effra-owned snapshot, read once.** Every data argument of an evidence-carrying export is copied into a fresh, deep-frozen value before any check or mint.
   - **How it is read.** The copy reads own data properties only, and an accessor is refused with EF186. Each value is type-checked, so an `i64` must be a 64-bit `bigint`.
   - **One read per field.** The admission check and the body both run on the snapshot, so the adapter reads each field of the host's argument exactly once.
   - **The acceptance counts reads, not outputs** (spike-where review 5, n1). An extra entry check over the caller's object can still return the right answer, so a correct output cannot show requirement 3. `benchmarks/laws/ts/proto/attacks.mts` therefore passes `search` a proxy whose data properties answer differently on every read, and it records `A3-read-count = true first:1 second:1 third:1`. The nested fields that decide sortedness are counted too. A trio whose users are proxies answering a hostile age or name after the first read records `A3-nested-read-count = true bob.age:1 bob.name:1 cy.age:1 cy.name:1 ada.age:1 ada.name:1`, counting both descriptor and `get` reads (review 2, o2). The getter attack records that the accessor was read 0 times (`G1-getter-reads = 0`).
   - **Returned evidence** is deep-frozen.
   - **The recipe rule.** An effect export brands its success value, not the recipe: `sortTrioLater: (arg_t: Trio) => Effect.Effect<SortedBy<Trio, "compareUsers">, never, never>`. Its adapter deep-freezes that value when the recipe succeeds (`Effect.map(call, __ef_deepFreeze)`). Before revision 2, the brand landed on the recipe, `SortedBy<Effect.Effect<Trio, never, never>, "compareUsers">`, and the adapter froze the recipe object (review 1, M4). Probe M4-effect-export and the consumer's `Effect.runSync(sortTrioLater(trio))` check the new form, and attack F3 shows that a recipe is not assignable to `SortedBy` (TS2322).
4. **Private canonical bindings.** Adapters call module-private `const` bindings (`__ef_function_sortedTrio`, `__ef_function_search`). An ES module namespace is already non-extensible and its bindings read-only, so `Object.assign` on it throws; patching `call` on an exported function does not reach the adapter. W3's frozen companion keeps this when companions exist.

**Outward brand.** `.d.mts` declares `declare class EfEvidence<E extends string, S, I extends string> { private readonly __ef_evidence: readonly [E, S, I]; }` and `export type SortedBy<S, I extends string> = S & EfEvidence<"SortedBy", S, I>`. A private member of a declared class cannot be supplied by a literal or by `Object` augmentation, so a plain trio is not `SortedBy` (TS2322) and evidence for one index is not evidence for another (TS2322). The brand is a convenience for TypeScript callers that track evidence among themselves. No Effra export trusts it, so delivering non-evidence to a branded slot, with or without a cast, gains nothing. The counterfactual shows routes that need no cast at all (§5.2). That is the honest name for this boundary.

**Scope of the prototype, disclosed (review 2, m2).**
- **Supported.** Evidence in top-level parameters and results, over record and enum carriers without type parameters, and the outward effect-result form, where an effect export's success value is branded and frozen (the recipe rule above).
- **Refused, with the diagnostic in each case.** These are where-dispatch §2.10 requirement 2's three inward occurrences, plus the variance rule. Each is a refusal, not a silently untracked value:

| Form | Prototype | Diagnostic | Replaced in |
| --- | --- | --- | --- |
| Evidence as a record or enum field: `record R { xs: SortedBy<Trio, compareUsers> }` | refused | EF102 "unknown or unsupported field type" (generic; L4 pins a specific code) | L4 |
| Evidence in a container (`Option`, sequences, maps) | not expressible: user generics are refused (EF127), and sequences and maps await #120 | EF127 | L4, after rows 4a |
| A callback that takes or returns evidence: `fn(SortedBy<…>) -> bool` as a parameter type | not expressible: a function type carries no evidence, so a callback cannot mention it | EF102 on the annotation | L4 |
| An evidence-requiring function passed as a value: `apply(search, …)`, `let f = search` | refused | EF189 | L4's variance rule |
| An asynchronous result carrying evidence inward: a host-supplied `Effect<SortedBy<…>>` | refused: an effect parameter type carries no evidence | EF102 on the annotation | L5 |
| A data parameter of an evidence-carrying export that reaches a node with no snapshot plan | refused | EF188, naming the path | L5 |

  L4 and L5 replace each refusal by tracking: every input occurrence is re-established, and every output occurrence is branded.

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

Run time with laws is therefore zero by construction. The diff above is regenerated at `ad08a82e`, where its revision headers are unchanged from revision 1 because `codec.ef` did not change.

**The host adapter's cost, measured.** The adapter makes one deep copy of the arguments per call of an evidence-carrying export, plus the admission check inward or a deep freeze outward. `benchmarks/laws/ts/adapter-cost.mjs` makes 200 000 calls per sample, with 11 samples after a warm-up, on node 24.11.1.
- **Revision 2's numbers are per exported call, not adapter overhead (review 2, m4).** `runs/cost.txt`, at load 9 to 24, gave these medians:
  - 4.7 ns for `oldest`, an export with no adapter;
  - 1 209 ns for `sortTrio`;
  - 1 268 ns for `search`.

  Those include each body, the sort and the search. `oldest` is a different operation, so it is a floor for a cheap export, not a control.
- **Revision 3 isolates the adapter.** It measures each evidence export twice: once as exported, and once as the same body with no adapter, from a copy of `lib.mjs` whose export line names the module-private `__ef_function_*` binding. `runs/adapter-overhead.txt`, at load 2.5, gives these medians:

| Export | With adapter | Same body, no adapter | Adapter |
| --- | --- | --- | --- |
| `sortTrio` (outward: snapshot of a three-user trio, deep freeze of the result) | 763 ns | 29 ns | 734 ns |
| `search` (inward: snapshot of both arguments, then `sortedTrio`) | 826 ns | 9 ns | 816 ns |

So an evidence-carrying export pays about 0.75 to 0.8 µs per call for a 7-object argument, and the adapter is about 96 to 99 % of the call. Calls inside Effra pay nothing, and neither does Go. The cost is the descriptor-based copy and the freezing: with bodies this small, the safety rules of §4.6 dominate. That is acceptable at a library boundary, and L5 owns making it cheaper. One way is a snapshot specialized per carrier, emitted as straight-line code instead of interpreting the node table.

### 5.2 `.d.mts` under attack

`benchmarks/laws/ts/run.sh` checks the prototype's emitted library with the repository's TypeScript 6.0.3, `--strict`, and runs each probe on node 24.11.1.
- **The library.** `ts/proto/lib.mjs` and `lib.d.mts`, built from `examples/laws/sorted.ef` at `8b02919b` by `ef test --target both`, then `ef build --target js`. A fresh build at `ad08a82e` is byte-identical below the provenance comment, since revision 4 changes no emission outside law harnesses. The build is refused with EF174 until the test has written a passing receipt.
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
| A3 nested read count: each user a proxy whose `age` and `name` answer differently after the first read | 3 | type-checks | correct, and each nested field read once: `true bob.age:1 bob.name:1 cy.age:1 cy.name:1 ada.age:1 ada.name:1` |
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
- **A3,** the getter that answers the entry check and the body differently, is refused before the getter is ever read. Its data-property form, a proxy, is correct with each field read exactly once, at the top level and one level down. Its nested form behind a carrier with no snapshot plan is refused at check time (EF188, §4.6).

### 5.3 Name-neutrality

The prototype's law, evidence, receipt and boundary code (`internal/compiler/laws*.go` and `cmd/ef/laws.go` on `ad08a82e`, tests excluded) contains no string literal `"Order`, `"RoundTrip`, `"compare`, `"SortedBy`, `"Codec` or `"codec`: `grep` over the five files finds none. Revision 4's binding-aware elaboration also touches `resolution.go`, `semantic.go` and `syntax.go`. Its added lines hold no contract, entry or probe name either, `Eq`, `same` and `sameI` included. Contract, law, entry and evidence names are read from the source, and an entry is resolved by its binding, never compared by spelling.

## 6. Prototype

**Files on `wf10/spike-laws-proto`.** 58 files changed on `13e62ad4`, with 5 493 lines added and 22 removed, the probes included. Revision 4's commit alone changes 21 files, adding 914 lines and removing 109.
- `internal/compiler/laws.go`:
  - declarations, and elaboration of each law per satisfaction into a checked `law__C__S__l` function returning `bool`, changing only the header; contract type parameters parsed as type arguments in annotations;
  - mint authority over every satisfaction that binds an index (q1), and EF190 by declaration kind (p3);
  - logical-ID seeds, and the optional `target` clause, refused on a type (EF190);
  - the evidence walker, by binding identity, lexical scope and the call's argument-to-parameter mapping (EF180-EF184, EF189), and mint authority resolved per target;
  - laws as values (EF178), budgets (EF175) and entry types (EF177).
- `internal/compiler/laws_emit.go`: generator derivation and EF171, and the Go and JavaScript harnesses. Both harnesses share the generator, shrinker and digest algorithms and the canonical root-seed parse (EF176).
- `internal/compiler/resolution.go`, `semantic.go` and `syntax.go` (revision 4): a law function's scope opens with its entry binders, each resolving to a global reference to the bound declaration; EF101 names an entry that a `let` redeclares; annotations parse contract type parameters through the satisfaction's type arguments.
- `internal/compiler/laws_receipt.go`: the receipt (with `live`), its comparison by logical law and provider, the refutation ledger and its strict decoder, the provider comparison, and the build verdict (EF170, EF172, EF173, EF174).
- `internal/compiler/laws_boundary.go`: `admit`, EF185, EF187 and EF188 over every reachable node, and the JavaScript adapter and brand, including the recipe rule and the refusing snapshot default.
- `internal/compiler/laws_test.go`: elaboration, accepted flows (including `run`), every refusal, the receipt comparison and disagreement controls, the ledger's transitions, target-scoped authority, the build verdict, the effect-export brand, the snapshot default, and both erasure tests.
- `cmd/ef/laws.go`: `ef test --target both`, location-keyed and exploration receipt and ledger paths, seed validation, build verdicts, and EF191 for an unreadable receipt or ledger.
- `examples/laws/{order,order-broken,codec,sorted}.ef`, `examples/laws/run.sh`, `examples/laws/probes.sh`, and `examples/laws/probes/`.

**How to run.** On the proto branch:
- `examples/laws/run.sh <scratch>` builds `ef` and runs every example on both targets. It checks the expected statuses and counterexamples, asserts `agree:true` for every program whatever its expected exit code (review 1, m6), then runs the probes. It exits non-zero on any mismatch.
- The full Go suite (`go test ./internal/... ./cmd/...`) passes at `ad08a82e`. The law tests, with their subtests, are listed in `benchmarks/laws/runs/proto-law-tests.txt`.

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

### 6.1 Review probes, before and after

`examples/laws/probes.sh <ef>` runs every probe on Go and on JavaScript and prints PASS or FAIL with the observation. It exits non-zero on any failure. The receipts are:
- `benchmarks/laws/runs/probes-after.txt`: `ef` built from `ad08a82e`, 72 of 72 lines pass;
- `benchmarks/laws/runs/probes-before-r3.txt`: the same script with `ef` built from `8b02919b` (revision 3), 19 probes fail (24 FAIL lines, since a refusal probe fails once per target) and 53 pass;
- `benchmarks/laws/runs/probes-before-r2.txt`: revision 3's script with `ef` built from `63164e8a` (revision 2), 12 lines fail and 45 pass;
- `benchmarks/laws/runs/probes-before.txt`: revision 2's script with `ef` built from `5f428883` (revision 1), 37 lines fail and 6 pass;
- `benchmarks/laws/runs/m6b-before.txt`: the seed-overwrite hole, reproduced by hand with the old binary.

**Review 3's findings.** Each has a probe that fails on `8b02919b` and passes on `ad08a82e`. Each P1 law binds an irreflexive `sameI` (`a < b`), except the named-argument one, so a refutation at 0 shows that the law ran the bound declaration. The named-argument law binds a lawful `a == b`, so `tested` shows that the label kept its parameter.

| Finding | Probe | Before (`8b02919b`) | After (`ad08a82e`) |
| --- | --- | --- | --- |
| P1 | review 3's program: `let sameI = alwaysTrue` in the law body (P1-callable) | `tested` ×20 on both targets: the law ran `alwaysTrue` (`__ef_local_sameI`) | refuted at `a = 0` on both; the law calls `__ef_function_sameI` |
| P1 | control: the same without the `let` (P1-callable-control) | refuted at `a = 0` | refuted at `a = 0` |
| P1 | the same with `evidence Same<S> by Eq.same` and a mint in `mk` (P1-callable-evidence) | both builds admitted; `.d.mts` declared `mk: (arg_n: bigint) => Same<bigint, "sameI">` | both builds refused (EF170); no `.d.mts` |
| P1 | a law parameter spelled `sameI` (P1-parameter) | EF103 "local value is not callable", by accident | refuted at `sameI = 0` |
| P1 | a non-callable local spelled `sameI` (P1-local) | EF103, by accident | refuted at `a = 0` |
| P1 | a pattern binder spelled `sameI` (P1-binder) | EF103, by accident | refuted at `a = 0, b = Box.Value { sameI: 0 }` |
| P1 | `Probe { same: a }` and `p.same`: a record field spelled like the entry (P1-record-label) | EF114 "unknown payload field sameI": the label was rewritten | refuted at `a = 0` |
| P1 | a local `sameI`, then a `match` arm binding `sameI`, inside an `if` (P1-nested) | EF103, by accident | refuted at `a = 0, b = Box.Value { sameI: 0 }` |
| P1 | `record Tagged { T: i64 }` and `let T = Tagged { T: a }` beside the contract parameter `T` (P1-type-parameter) | EF114 "unknown payload field i64": the label `T` was rewritten | refuted at `a = 0` |
| P1 | `pick(same: a)`, where `pick(same: i64 = 0, sameI: i64 = 0)`: a named argument spelled like the entry (P1-named-argument) | refuted at `a = -1`: the label became `sameI`, so `pick` returned its default, a miscompile | `tested`: the label keeps its parameter |
| P1 | `let same = alwaysTrue`, then `same(a, a)` (P1-entry-let) | `tested`: the rewritten `let sameI` captured the call, a miscompile | EF101 "same is the contract entry Eq.same, which this law's satisfaction binds to sameI" |
| P1 | `law again(a: T) { law__Eq__i64__reflexive(a) }` (P1-law-call) | checked: law bodies were exempt from EF178 | EF178 "law__Eq__i64__reflexive is the law Eq.reflexive@i64, and a law is not a value" |
| p1 | `deep.ef` after its disagreement, ledger replaced by `{`: `ef test --target go`, then both builds (p1-ledger-malformed) | `ef test` exited 0 and started an empty ledger | EF191; the ledger keeps its bytes; the Go receipt stays `refuted`; both builds refused (EF191) |
| p1 | ledger `{"schemaVersion": 1}`, Go build (p1-ledger-schema) | read as empty; built | EF191 "(no source digest)" |
| p1 | `codec.ef`, receipt replaced by `{`, no ledger, Go build (p1-receipt-malformed) | built with the EF173 note "(no receipt)" | EF191 "the law receipt … is unreadable" |
| p1 | the same with the ledger also `{` (p1-both-malformed) | built with the EF173 note | EF191 |
| p1 | `dist/deep.*` deleted after an edit, `ef test --target go`, Go build; then `ef test --target both` and both builds (p1-fresh-checkout) | built; then both refused (EF172) | the same: documented local history, re-derived by the both-target run (§2.2) |
| p3 | `Box satisfies Eq<Box<i64>> … target go` over `record Box<T: type>` (N2-generic-record) | checked | EF190, both targets |
| p3 | the same over `enum Box<T: type>` (N2-generic-enum) | checked | EF190, both targets |
| q1 | `Twin satisfies Order<User>` with `unresolved total`, declared after `User`'s lawful satisfaction (q1-after) | checked: the first match, `User`, gated alone | EF181 "Twin satisfies Order with law total unresolved", both targets |
| q1 | the same, declared before it (q1-before) | EF181 | EF181 |
| q2 | `ef test --live --target js`, then `ef build --target js`; then an ordinary run and the build (q2-live) | no `live` field; built | `live: true`; refused with EF174 "(the receipt is from ef test --live …)"; built after the ordinary run |

**Review 2's findings.** Each Major has a probe that fails on `63164e8a` and passes on `8b02919b`, and still passes on `ad08a82e`:

| Finding | Probe | Before (`63164e8a`) | After (`8b02919b`) |
| --- | --- | --- | --- |
| N1 | `deep.ef`: `ef test --target both`, then `ef test --target go`, then both builds | the Go receipt became `tested`, and the Go build was admitted | the Go receipt stays `refuted`; both builds refused (EF172) |
| N1 | control: the same file after an edit, `ef test --target go`, Go build | built | built: a changed source digest is new inputs |
| N2 | `User satisfies Order<User> cases 200 target go` | checked | EF190, both targets |
| N2 | `GoOrder satisfies Order<User> … target go`, built for js | built, and `.d.mts` declared `sortTrio: … => SortedBy<Trio, "compareUsers">` | EF181 "bound to Order.compare only on go"; no `.d.mts` |
| N2 | control: the same, built for go after `ef test --target go` | built | built |
| N2 | the 0/1 compare bound `target go`, refuted on Go, built for js | built, and `SortedBy` branded | EF181; no `.d.mts` |
| N3 | `record Envelope { held: Holder<Trio> }`; `enum Parcel { Packed { held: Holder<Trio> } }` | checked; the adapter handed the body the host's `Holder` | EF188 naming `e.held` and `p.held`, both targets |
| N4 | `wrapped(other: sortTrio(t), xs: t)` with a defaulted `flag` | checked; prints `false` | EF184, both targets |
| N4 | control: `wrapped(other: t, xs: sortTrio(t))` | EF184 (a false refusal) | accepted; prints `true` on both targets |
| m2 | evidence as a field, in `Option`, in a callback's parameter or result, in an `Effect` parameter | EF102 | EF102 (unchanged; now asserted) |
| o1 | `let s = scope { sortTrio(t) }; search(s, bob)`, and `scope { t }` | accepted, `true`; EF184 | the same (unchanged; now asserted) |
| m3 | a law parameter, local or pattern binder spelled `sameI`, the bound function | EF103 | EF103. Revision 3 called this "a guard: refused, never miscompiled, until L1". That was false: EF103, "local value is not callable", fired only because these binders were not callable, and a callable one miscompiled (review 3, P1). At `ad08a82e` each is accepted and runs the bound function (P1 rows above) |

**Review 1's findings.**

| Finding | Probe | Before (`5f428883`) | After (`63164e8a`, unchanged at `8b02919b` and `ad08a82e`) |
| --- | --- | --- | --- |
| M1 | `partial.ef`: the 0/1 compare | (control) without `total`, tested ×3 and the build admitted | with `total`, refuted at `a = -1, b = 0`; build refused (EF170). M1 is a contract change, so it also passes before |
| M2 | shadowing parameter in a mint; in a signature; a concrete forwarder | checked | EF181, both targets |
| M2 | control: another spelling, canonical evidence from `sortTrio` | accepted | accepted |
| M3 | `Holder<Trio>` export, a root generic carrier | checked; the adapter skips snapshot and check | EF188 (revision 3 extends it to nested carriers, N3) |
| M4 | `apply(search, …)`, `let f = search` | checked; prints `false` | EF189 |
| M4 | `scope { search(t, x) }` | checked; prints `false` | EF184 |
| M4 | binder spelled like the evidence parameter; binder under a renamed parameter | the binder spelled `xs` checked; the renamed one EF184 | EF184 for both |
| M4 | `let xs = run sortTrioLater(t); search(xs, bob)` | EF184 | accepted; prints `true` |
| M4 | effect export projection | `SortedBy<Effect.Effect<Trio, …>, …>`, recipe frozen | `Effect.Effect<SortedBy<Trio, …>, never, never>`, success value frozen |
| M5 | `ef build` of a minting program with no receipt | EF173 note, built, `.d.mts` branded | EF174, refused |
| M6a | `ef build /abs/…` from another directory beside a refuted receipt | EF173, built | EF170, refused |
| M6b | `near.ef`: seed 0 refutes; exploration seed 14 does not | the seed run overwrote the receipt, and the build was admitted (`m6b-before.txt`, seed 3) | own `.seed-14` receipt; gating receipt unchanged; build refused |
| M6c | `deep.ef`: Go tested, JavaScript stack overflow | EF172 printed, receipts unchanged, Go build admitted | both receipts `refuted` with the EF172 failure; both builds refused (a later single-target run no longer clears it, N1) |
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
- evidence nested in fields, containers, callbacks or asynchronous results, each refused with the diagnostic listed in §4.6;
- generic carriers at the host boundary, at any depth (refused with EF188);
- the variance rule (EF189 refuses every function value with an evidence parameter instead);
- law elaboration in #135's spelling, through the formatter and LSP spans, and across modules through interface summaries. The binding-aware core is done (§1.8); L1 carries it to those surfaces;
- toolchain, dependency, provider and live provenance in the refutation ledger's inputs (Q7, q2). The receipt records `live`, and a live receipt never gates;
- a declared provider subject: `target` accepts any subject that is not a declared type, so a misspelled type is a provider (§2.10);
- refusing duplicate bindings of one function to one entry. The prototype requires every such satisfaction to be lawful instead (q1, §2.10).

**EF103 is not a hygiene guard.** It is Effra's ordinary "local value is not callable" error and nothing else. Revision 3 presented it as one. No probe asserts it any longer, and no law-body case depends on it.

## 7. Laws as provider protocols, and the #124 bridge

**Decision.** A contract whose subjects are providers is a **provider protocol**: services, runtimes, codecs, or any effectful implementation a library offers.
- **Same laws, same receipt.** Every provider, built-in or userland, passes the same laws and gets a receipt in the same schema.
- **Comparable.** Providers of one law are compared by logical law and scenario.
- **R1-R15** are one such protocol, the `Runtime` contract.
- **Host fixtures.** A host fixture is a named discharge mode for a declared law. It is used only for behaviour an Effra program cannot observe, and it writes the same receipt.

**Why this is the general form** (owner, 2026-10-12, on guardrails and portable providers). A userland implementation "that may not necessarily be [an] effect runtime, but [is] effectual" may be better in its domain than the built-in one, but only if replacing the built-in one cannot change what the construct means. The protocol's laws are that guarantee: a provider that passes them may be swapped in, and one that fails them is refused at build time.
- **The failure this prevents.** F# computation-expression builders and Haskell's `RebindableSyntax` let a library rebind the meaning of a construct (`let!`, `do`, `>>=`) with no laws checked (F# language specification, "Computation Expressions"; GHC User's Guide, `RebindableSyntax`; neither verified locally). In both, a builder can make `do` or `let!` mean anything that type-checks, and nothing tells the reader that a familiar block no longer obeys the monad laws.
- **What Effra keeps.** Effra keeps the substitution, since a provider is chosen by `satisfies` and by provision. It does not keep the drift, since a provider that changes meaning fails a law and its build is refused.

**The protocol's parts**, each in the prototype at `ad08a82e`:

| Part | Rule | Prototype receipt |
| --- | --- | --- |
| Identity | A law's logical ID is `Contract<Args>.law`. The provider is receipt metadata (`provider`, and `boundTarget` for a provider that exists on one target). | Receipts carry `law`, `provider` and `boundTarget` (§2.2). |
| Seeds | Each law's seed comes from its logical ID, so all providers of one law run the same scenarios. | `accountJson` and `accountJsonAgain`: seed `607786003`, digest `0109b916` (probe M7-providers). |
| Target-bound providers | `GoRuntime satisfies Runtime cases 50 target go` runs only on Go, and `EffectRuntime … target js` only on JavaScript. Their receipts pair by logical law, not by per-provider ID. A target-bound satisfaction authorizes evidence on its own target only. `target` binds a provider, never a type (EF190), and a protocol needs a provider on each target for its laws to gate (§2.10, §3). | Same seed `990390378` and digest `03b4fb56` on each provider's target, `sameScenarios:true` (probe M7-runtime). Revision 1 would have reported "missing on …" for each (review 1, M7). Probes N2-type, N2-provider and N2-refuted (review 2, N2). |
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

Tested candidate, `examples/laws/probes/runtime.ef` on `ad08a82e`:
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

**What the hosts say about revision 3's rules** (review 2's comparison, at the same pins, cross-checked against the table above).
- **No persistent cross-target gate elsewhere.** No runner inspected keeps a verdict across runs or compares outcomes across targets.
  - Scala.js and cats cross-build one discipline suite for JavaScript, the JVM and Native, but cross-building does not compare outcomes or establish identical cases.
  - QuickCheck, ScalaCheck, StreamData and MoonBit report one run and forget it.

  Effra compares outcomes, which is stronger, and so the result must not be forgotten either: a refutation is sticky for its inputs (N1, §2.2). PureScript's quickcheck-laws (online, no local pin) shows the other side: laws as a library cannot gate anything, so there is nothing for them to forget.
- **Trusted foreign boundaries.** Gleam's `@external`, ReScript's externals, MoonBit's handwritten FFI signatures and Scala.js facade casts are all trusted and never checked, and a facade cast performs no runtime check. Effra re-establishes host evidence instead. A boundary that checks only the root of a value is a trusted boundary one level down, so the admission and the snapshot must cover every reachable node (N3, §4.6).
- **Host-dependent observations.** Gleam's JavaScript emitter lowers self-tail recursion to loops, and PureScript's CoreImp does tail-call elimination. So whether deep recursion overflows depends on the lowering and the host, which is why `deep.ef`'s case count and counterexample are labelled host-dependent and never asserted (m1, §3).
- **Vacuous passes.** QuickCheck's `GaveUp`, MoonBit's bounded discards and StreamData's filter error are the precedent for Q9's guard floor (§13).

**Hygiene: elaborating code into a scope it was not written in** (review 3, P1; revision 4). A law body is written once in the contract and elaborated once per satisfaction, which is the problem macro systems solve. The mature systems answer it by identity, not by spelling, and none uses a list of refused names.
- **Lean 4** `db7f10e9`: quotations are "auto-hygienic, by mangling identifiers introduced by them with a 'macro scope' supplied by the context". Identifiers from different macro scopes "produce different identifiers" (`src/Init/Prelude.lean:5635-5647`).
- **Elixir** `91ee75bb`: macros have "late resolution", which "guarantees that a variable defined inside a quote won't conflict with a variable defined in the context where that macro is expanded" (`lib/elixir/pages/meta-programming/macros.md:110-112`). Escaping is explicit (`var!`). Elixir is the compile-to-host language whose code generation is hygienic by default, and Effra's law elaboration now is too.
- **Racket's sets of scopes** (Flatt, POPL 2016), **Scheme's `syntax-rules`** (R7RS §4.3) and **Rust's syntax contexts** (`rustc_span::hygiene`) attach binding information to identifiers, so a use-site binder cannot capture an introduced reference. These were not verified locally: the Racket and Rust clones here do not include the expander sources.
- **Gleam** `52e735c8` and **ReScript** `30ce3698` have no user macros, but their code generators keep binder identity all the way to the host.
  - Gleam's JavaScript emitter numbers a shadowing local (`name$1`, `compiler-core/src/javascript/expression.rs:319-334`).
  - ReScript's identifiers carry a unique `stamp` beside the name (`compiler/ext/ident.ml:18-30`).

  Neither resolves a generated reference by its spelling. Effra's resolver applies the same discipline to entries.
- **What Effra takes.**
  - A law's entries are binders of an enclosing scope, the nearest analogue of Lean's macro scope.
  - The resolver binds each entry to the declaration its satisfaction names, as Elixir's late resolution resolves a quoted name in the macro's context.
  - Effra has no user macros, so it needs no escape like `var!`.

  Revision 3's substitution was the unhygienic expansion that each of these systems moved away from.

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

**Decided 2026-10-12 (spike #125, revision 1; amended in revision 2 after review 1, in revision 3 after review 2, and in revision 4 after review 3).**

- **Chosen.**
  - Laws are declared in a `contract`, a `service` or a `machine`, with typed universally quantified parameters. A law body is pure, or an `effect law` with explicit `raises` and `uses` over deterministic providers. A law is not a value, and no law body mints.
  - **A law body reads as ordinary boolean code** (revision 2).
  - A `satisfies` declaration in the subject's owner module binds the subject and **must** state a case budget (EF175). It may mark laws `trusted` or `unresolved` with a justifier. Each binding's type must match the contract entry's (EF177).
    - **`target`.** A satisfaction may bind a provider to one target (`target go`), never a type (EF190), decided by declaration kind. A target-bound satisfaction authorizes mints and admissions on its own target only (revision 3).
    - **Every satisfaction that binds an index gates it** (revision 4). Evidence rests on all of them, so one with an unresolved law refuses the mint, whatever the declaration order.
  - **Law bodies are elaborated binding-aware** (revision 4). A contract entry is a binder of the law's enclosing scope, bound to the declaration the satisfaction names. It is never substituted by spelling, so a binder or label in the body cannot capture it. A `let` cannot redeclare it (EF101), a law cannot call a law (EF178), and a contract type parameter is a whole type argument.
  - **`Order` has four laws:** reflexive, antisymmetric, transitive and total (revision 2).
  - **Discharge is one deterministic runner:**
    - compiler-derived structural generators and choice-sequence shrinking;
    - a fixed, canonically parsed root seed, with per-law seeds from the logical law;
    - the same algorithm on Go and JavaScript;
    - one receipt per target, whose statuses are lawful-runtime-contract's categories.
  - **Gating receipts are canonical:** keyed by the source's location, bound to the revision (source and dependencies) and to root seed 0. Exploration seeds write separate, non-gating receipts. L2 adds the toolchain and provider revision.
  - **The commands:**
    - `ef test` runs laws;
    - `--target both` compares receipts and adds a disagreement to the refutation ledger, as a refutation on both targets;
    - **a refutation stands** for its inputs (in the prototype the source digest and root seed; after L2 also dependencies, toolchain and live mode) until an agreeing run on both targets over the same inputs, or a changed source digest; a single-target run never clears it (revision 3, worded precisely in revision 4);
    - **the ledger is local history** in `dist/`: an unreadable one is refused (EF191), a deleted one is lost, and the gate's both-target run from a clean tree re-derives every deterministic disagreement; in-source examples (Q6) are the durable form (revision 4);
    - the gate runs both targets;
    - `ef build` reads the ledger and then the gating receipt, refuses either if unreadable (EF191), refuses standing and refuted laws, and refuses evidence with no current, non-live passing receipt (EF174);
    - `ef check` never executes.
  - Conditional satisfactions discharge per instantiation reached, and compose from their parts.
  - **Evidence:**
    - a nominal wrapper over its carrier;
    - indexed by the declaration bound at the site, by binding identity, by a satisfaction with passing laws;
    - constructed only in its kind's owner module;
    - never indexed by a closure, a parameter or a host-passed function;
    - checked at each argument against the parameter the call binds it to, by the call's own argument-to-parameter mapping (revision 3);
    - covariant in results and contravariant in parameters.
  - **The host boundary follows the four rules:**
    - outward forms i and ii by provenance, with generic forwarding projecting the carrier and a private-member brand;
    - inward re-establishment through a declared admission check, refusing an export if any node reachable from a data parameter has no snapshot plan (revision 3), with a snapshot that refuses rather than passes through;
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
- **Rejected: letting the latest run decide a receipt** (revision 3). Another passing sample cannot invalidate a known counterexample, and a single-target `ef test` erased a cross-target refutation (review 2, N1, §2.2).
- **Rejected: a target-bound satisfaction gating every target** (revision 3). The JavaScript build branded evidence over a compare refuted on Go, because skipping an obligation discharged it (review 2, N2, §2.10).
- **Rejected: `target` on a type** (revision 3). A type's laws are one obligation on both targets (EF190, §2.10). Revision 4 decides "a type" by declaration kind, so declared generic types are included (review 3, p3).
- **Rejected: elaborating law bodies by token substitution** (revision 4). It resolved a contract entry by spelling, so a callable local captured it and a law reported `tested` for a function that breaks it, with evidence minted over that function (review 3, P1, §1.8).
- **Rejected: a refusal list of spellings as the hygiene fix** (revision 4). Refusing every binder or label that collides with a bound name is a guard. It must stay complete as the language grows (labels, nested scopes, type parameters), and it refuses programs that are fine. Binding-aware elaboration makes capture unrepresentable instead.
- **Rejected: first-match satisfaction authority** (revision 4). Declaration order decided which satisfaction gated an index (review 3, q1, §2.10).
- **Rejected: reading an unreadable ledger or receipt as absent** (revision 4). Corruption erased standing refutations (review 3, p1, §2.2).

## 12. Follow-up units

Prerequisites costed elsewhere:
- spike-rows unit 3 (receiver methods, explicit type arguments) and unit 4a (#128, whole-value type parameters and the recipe-parameter contract);
- spike-where W1 (`where` clauses, conditional methods) and W3 (projection and frozen companions);
- machines (the checked machine declaration);
- #120 (sequences).

Every probe in §6.1 is acceptance for the unit named beside it. Each must be refused, or correct, on Go and on JavaScript, with the message pinned. Revision 2's added scope is costed in each estimate.

| Unit | What lands | Tests and acceptance | After | Estimate |
| --- | --- | --- | --- | --- |
| **L1. Contracts, laws and satisfactions** | Parser, formatter and LSP spans in #135's spelling (including its choice on `and`/`or`/`not` and `when`); contracts as groups of `where` entries; **binding-aware elaboration** of laws into checked functions, carried from the prototype's resolver (§1.8) to #135's spelling, the formatter, LSP spans and cross-module satisfactions; satisfactions in the owner module with a required `cases` (EF175), `target`, `trusted` and `unresolved`; **entry types checked against the contract** (EF177); laws not values (EF178); EF002-family diagnostics; interface-summary transport (schema bump, round trip, `TestEvidenceSchemaIsClassified`); inspection facts in CLI and MCP; reachability in test mode. | The prototype's declaration tests and refusals with messages; probes m2, m3 and Q2; the four-law `Order` with the 0/1 compare as a negative control (probe M1); **hygiene: every P1 probe** (a callable or non-callable local, a parameter, a pattern binder or a nested binder spelled like the bound function, a record field or named argument spelled like the entry, a binder spelled like a contract type parameter) checks and runs the bound function, and P1-entry-let and P1-law-call keep their pinned EF101 and EF178; the same cases in #135's spelling, through the formatter, and with the contract, the satisfaction and the bound function in three modules; a cross-module satisfaction; inherited laws run once; the name-neutrality screen; check time with laws measured on a quiet machine (§3). | rows 3, W1, #135 | L, 4.5 to 5.5 days |
| **L2. The runner and receipts** | Generators for scalars, records, enums, generic data (after 4a) and recursive data; the choice-sequence shrinker; logical-ID seeds; the canonical seed parse (EF176); Go and JavaScript harnesses sharing one algorithm; `ef test --target both`; **canonical gating receipts** (location-keyed, revision- and seed-bound, exploration on its own path); **persisted disagreement** (EF172); **the refutation ledger**, under which a refutation stands until an agreeing run on both targets over the same inputs or a changed input identity (§2.2), with its strict decoder and EF191 for an unreadable ledger or receipt; **the full input identity**, source, dependencies, toolchain, provider revision and live mode, in receipts, staleness and the ledger's inputs (Q7, q2); gate integration. | The prototype's results on both targets, with byte-identical receipts; a generic `Option[User]` and a recursive list law; the seed survey, including the near-order defect; probes M6a, M6b, M6c and m1; **the three-command sequence** of §3, `ef test --target both`, then `ef test --target go`, then `ef build` on each target, refused at the end (probe N1), with the changed-source control (probe N1-revision); an incomplete run on both targets clears nothing; a deleted receipt does not clear a standing refutation; **an unreadable or malformed ledger or receipt is refused by `ef test` and `ef build`, never read as absent** (probes p1-ledger-malformed, p1-ledger-schema, p1-receipt-malformed, p1-both-malformed); a deleted `dist/` is re-derived by the both-target run (p1-fresh-checkout); a dependency-only change and a toolchain change each make a receipt stale and the ledger's inputs new; a `--live` receipt never gates (q2-live); deep.ef's case count and counterexample never asserted. | L1, 4a | L, 5.5 to 6.5 days |
| **L3. Effect, service and provider-protocol laws** | `effect law` with `uses`; refusal of live providers; service satisfactions per provider with `using`; **target-bound providers and the provider comparison** by logical law; harness-owned observers (an `Assert`-style channel and a counter fixture); one fresh scope per case. | The codec law; a service law over two providers, one deliberately lossy; probes M7-runtime, M7-observer and M7-providers; **target-scoped authority**: probes N2-type, N2-generic-record, N2-generic-enum, N2-provider, N2-provider-control and N2-refuted, with a declared provider subject in place of the prototype's "any subject that is not a declared type", so a misspelled type (`Usr satisfies Order<User> … target go`) is refused; **duplicate bindings** of one function to one entry either refused or each required lawful, with probes q1-after and q1-before kept; a live provider refused; a failure raised in a law shows in the counterexample. | L2, 4a | M, 3.5 to 4.5 days |
| **L4. Evidence** | Evidence kinds as nominal wrapper types (generic-transparent after 4a), **covariant in results and contravariant in parameters** (replacing EF189's blanket refusal); construction private to the kind's owner module; **index resolution by binding identity** to declarations and canonical keys; evidence through `run` and recipes; admissions; EF180-EF187; the build verdict (EF174); the mint-soundness law (§13 Q4). | The prototype's evidence tests; probes M2, M4-apply, M4-let, M4-scope, M4-binder and M5 as refusals, and **M4-run and the o1 scope pair as acceptances**; **arguments by the call's binding**: probe N4 refused and N4-control accepted, plus reordered named arguments with and without omitted defaults, in both directions, on a function and a method; a mint through a target-bound satisfaction on its own target accepted and on the other refused (N2); a variance pair (result accepted, parameter refused); cross-module minting refused; `Option[SortedBy[…]]` through a generic; nested evidence in a record; **every `m2-nested-*` form tracked or refused with its own pinned code**, never the generic EF102. | L1, 4a, W1 | L, 5.5 to 6.5 days |
| **L5. Host evidence boundary** | The JavaScript adapter (snapshot, admission, deep freeze) for every input and output occurrence; **refusal of a carrier with no snapshot plan** (EF188) and snapshot plans for generic carriers; **the recipe rule** for effect exports; the private-member brand; `benchmarks/laws/ts` promoted to a generated-consumer fixture. | Every row of §5.2 with its pinned outcome, **including the read counts** (each argument field read once at every depth, not merely a correct output); probes M3, **N3-record and N3-enum** (a record or enum wrapper around an unsupported carrier is refused with the path named) and M4-effect-export; the snapshot's default refuses; a TypeScript-supplied compare to an exported generic sort projects the carrier; nested, callback and asynchronous occurrences; the counterfactual kept; 0 `any`; **the adapter's own cost measured against the same body with no adapter** (§5.1), with a target for a specialized snapshot. | L4, W3 | M, 3 to 3.5 days |
| **L6. Machine laws** | Reachable-state laws with `depth`; event-sequence generation; trace counterexamples shrunk by length first. | A Session invariant tested; a deliberately broken step refuted with a numbered trace; the same trace on both targets. | machines, L2 | M, 2.5 to 3 days |
| **L7. Conditional satisfaction** | Per-instantiation discharge with composed generators; verdict composition from parts; library-declared instantiations; canonical keys in receipts. | `Pair[i64, User]` and `Option[Option[User]]` discharged; an unresolved part makes the composite unresolved; Trio's trusted transitivity replaced by a tested composite. | W1 conditional methods, L2 | M, 2 to 3 days |
| **L8. Docs** | `docs/design.md:42` extended; GLOSSARY "Contract", "Satisfaction", "Evidence kind", "Admission", "Law receipt", "Provider protocol"; lawful-runtime-contract cross-reference; CHANGELOG; a new preview, superseding `docs/previews/2026-10-12-laws.md`, once #135 picks the spelling. | Docs review. | L1 | S, 0.5 day |

**Total:** about 27 to 33 implementor-days plus review rounds.
- Revision 1's estimate was 22 to 27.5 days.
- Revision 2's was 25.5 to 31.5 days, adding the review-1 scope in L1, L2, L3, L4 and L5.
- Revision 3's was 27.5 to 33.5 days. It added half a day to each of four units, for the review-2 scope:
  - L1: the hygiene cases;
  - L2: the ledger and its sequence;
  - L4: argument mapping and the pinned nested-evidence codes;
  - L5: nested snapshot plans and the isolated adapter cost.

  L3's target-scoped acceptance fit its existing estimate.
- Revision 4 moves scope, for review 3:
  - **L1 shrinks by a day.** Binding-aware elaboration, the hard part of hygiene, landed in the prototype with its probes, so L1 carries a working resolver rule to new surfaces instead of designing one.
  - **L2 is unchanged.** The strict decoder, EF191 and the `live` field exist in the prototype; adding dependencies and live mode to the inputs rides on Q7's work.
  - **L3 grows by half a day,** for duplicate bindings and declared providers.

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

Diagnostics added or changed in revision 4 (prototype numbering, pinned in L1 and L2):
- **EF191, new:** a law receipt or refutation ledger exists but is unreadable or malformed. `ef test` and `ef build` refuse it, never read it as absent, and name the two ways out: an agreeing `ef test --target both`, or deleting the file deliberately (§2.2).
- **EF101, extended:** a `let` in a law body that redeclares a contract entry is the ordinary duplicate-local error, and its message names the entry and the declaration it binds (§1.8).
- **EF178, widened:** it now applies inside law bodies too.
- **EF002, extended:** a satisfaction entry must bind a declaration named on its own, and a contract type parameter cannot take a member or type arguments.
- **EF103, unchanged and no longer cited as a guard** (§6).

Open:
- **Q3. Which functions does EF185 cover?** The prototype refuses every function with an evidence parameter and no admission, because it has no `pub`. Recommendation: only `pub` functions reachable from the host boundary, once `pub` exists; private functions take evidence freely.
- **Q4. Should minting be tested?** A mint asserts a fact nothing checks. Recommendation: **yes**, by a law the compiler derives for each minting function whose parameters are generatable: `admissionCheck(mintingFunction(args))`, using the kind's declared admission check (`sortedTrio(sortTrio(t))`). A kind with no admission check keeps its mint sites as trusted decisions in inspection.
- **Q5. Keyword budget.** The prototype adds `contract`, `law`, `satisfies`, `evidence`, `mint`, `admit` and the contextual `target`. Recommendation:
  - keep the first four;
  - drop `mint` in favour of construction under the owner-module rule (§1.1);
  - make `admit` a clause of the evidence declaration;
  - leave `and`, `or`, `not` and `when` to #135.
- **Q6. Regression examples.** Recommendation: allow explicit examples on a satisfaction (`example transitive(a, b, c) = …`), run before generated cases on both targets, so a counterexample found by exploration stays in the gate, as proptest's checked-in regression seeds do. Revision 4 makes this the durable form of a refutation: the ledger in `dist/` is local history that a fresh checkout does not have (§2.2), while an example in source travels with the code. Hypothesis draws the same line between its disposable example database and `@example`.
- **Q9. Boolean connectives or a guard?** For #135, with the preview as the comparison: `and`/`or`/`not` alone, or with a `when` guard (§1.1).
  - **Recommendation:** the keywords in any case, because they serve all code. Add the guard only if the owner finds it clearer, since it adds one clause to the law form.
  - **If there is a guard, how a guarded law ends (review 2, o3).** Counting a false guard as a vacuous pass is one way to make the gate terminate, but not the only one. A guard that rejects most cases tests almost nothing while still reporting `tested`. The alternatives in prior art are these:
    - **A discard bound that gives up.** QuickCheck has `maxDiscardRatio` and reports `GaveUp`, Hedgehog has a discard limit, and fast-check has `maxSkipsPerRun`. MoonBit's QuickCheck bounds discards and ends in `GaveUp` (core `e96ede8b`, `quickcheck/driver.mbt`).
    - **An error on too much filtering.** StreamData raises when a filter rejects too many generated values (`7e161eb7`).
    - **A declared floor.** Effra could give the law `unresolved` when fewer than N cases meet the guard.

    These citations are review 2's, at its pins in §9. Hedgehog, fast-check and QuickCheck were not verified locally.
  - **Recommendation for the guard.** Discards count against the case budget. Too few guard hits, below a declared floor or a fixed discard ratio, makes the law `unresolved` with the reason "the guard held in k of n cases", never `tested`. Since evidence never rests on an unresolved law, a vacuous pass cannot gate evidence. This matches MoonBit's and QuickCheck's `GaveUp`, maps onto the existing category instead of a new one, and keeps the gate deterministic.
- **Q10. A provider protocol with a provider on one target only.** Today a target-bound provider with no counterpart on the other target compares as missing, which is a cross-target disagreement, so it refutes on both (§3). Recommendation: keep that until L3. A protocol that is genuinely single-target, such as a Go-only host codec, should declare it once in the protocol, not per provider, so the comparison can expect the gap instead of refuting it.
