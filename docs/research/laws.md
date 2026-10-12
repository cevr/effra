# Laws: obligations types cannot state, their discharge and their evidence

**Status:** design note and decision record for the spike in [#125](https://github.com/cevr/effra/issues/125), revision 1, 2026-10-12. It changes no compiler behaviour. Line references are at `main` `13e62ad4` unless stated otherwise. The prototype is on branch `wf10/spike-laws-proto`, head `5f428883` (three commits on `13e62ad4`: `8e781dc3` the construct, runner and evidence; `e239e708` the host evidence boundary; `5f428883` the end-to-end run). That branch is never merged. The TypeScript boundary probe and every receipt quoted here are under [`benchmarks/laws/`](../../benchmarks/laws/).

Two inputs are read from branches that are not on this base: spike-where's `docs/research/where-dispatch.md` on `wf10/spike-where` (head `8478e688`), and spike-rows' `docs/research/effect-rows-as-types.md` on `main` since `effb24d4`. They are cited by section, not linked.

## Owner direction

- **2026-10-08, `docs/design.md:42`.** `law` / `contract` is admitted, not implemented: "a visible construct for service and runtime laws, written `law name() uses {...} { ... }` inside a service or contract. A law suite checks the laws, and a refuted law refuses the build."
- **2026-10-08, the five categories.** `docs/research/lawful-runtime-contract.md` adopts one obligation vocabulary (nominally checked, trusted decision, tested law, refuted, unresolved) for domain ingress, services and runtimes, and says finite tests are evidence, never proof.
- **2026-10-12, laws are a construct** ("laws is for sure a construct"). Laws widen beyond services and runtimes to any behaviour that types cannot state, and they feed GDP evidence. This note extends `design.md:42`; it does not replace it.
- **2026-10-12, on #124.** Alternate runtimes "would be the same laws, I suppose, I would have to follow. So it would be an approximation." Runtime laws move up the queue because "it's part of the language constructs".
- **2026-10-12, the generic surface.** The generic signatures read "more typescript than go … I'm not sure if they look simple". Sketches here use the simplest Go-like spelling (implicit kinds, named constraints in the type-parameter list, no row variables), and **the spelling is provisional pending #135**. This note settles semantics, not spelling (§1.1).
- **2026-10-12, composability** (comment on #125). Every construct must show lossless wrapping, a nesting matrix, first-class recipes, orthogonality and composition laws. For laws: a law-bearing contract composes (a generic type derives its laws from its parts' contracts); laws can state composition algebra; and an ordinary function can wrap a law-checked thing without losing evidence (§8).
- **2026-10-12, coordinator rulings carried from spike-where.** The evidence boundary rule (round 2, N1) is binding, and round 3 adds three more rules: host evidence is never trusted (inward), evidence covers a deep Effra-owned snapshot (stability), and canonical calls use private bindings with frozen companions. Each must be shown with strict tsc (§4.6, §5.2).
- **The construct ladder** (memory `syntax-over-special-library`). Laws are rung 4, a visible construct, because discharge, receipts and build refusal are compiler-special. The compiler may key on `law`, `contract`, `satisfies` and `evidence`, never on a contract's or a method's name.

## Verdict

**Laws are declarations inside a `contract`, a `service` or a `machine`. A `satisfies` declaration binds a subject to a contract with a finite case budget. Laws are discharged per subject by one deterministic property runner that runs the same cases, from the same seeds, on Go and JavaScript, and writes one receipt per target. Evidence is a nominal wrapper over its carrier, indexed by the declaration a lawful satisfaction binds, minted only where that index is a declaration, and re-established on entry from the host.**

The deciding evidence:

1. **The categories need no second vocabulary.** A receipt's `status` is `tested`, `refuted`, `trusted` or `unresolved`, which are the spellings of lawful-runtime-contract's tested law, refuted, trusted decision and unresolved. A law that type-checks is nominally checked. `verified` is reserved, has no category, and is refused until the owner amends the five (§2.1, §2.9).
2. **The prototype discharges two unrelated clients on both targets, with identical receipts.** An `Order` contract over a lawful compare (5 laws tested, 1 trusted), a broken compare (antisymmetry and transitivity refuted, each with a shrunk counterexample) and a JSON codec round-trip (lossless codec tested, lossy codec refuted). Go and JavaScript agree on every status, case count, case digest and counterexample (`agree:true`, §6). The compiler holds no `Order`, `compare`, `RoundTrip` or `Codec` literal (§5.3).
3. **Laws erase completely, and evidence erases except at the host boundary.** With and without laws, the Go program, the JavaScript module and the `.d.mts` are byte-identical apart from the source-revision header (§5.1). With and without evidence, the Go program is identical and the JavaScript differs only by the boundary adapters and the brand declarations.
4. **The host boundary is fail-safe under every listed attack.** Laundering a reversed compare, spread override, array covariance, method bivariance, property covariance, both aliasing routes, a getter, an `Object.assign` patch, never fillers, a never rest, an untyped value and `Object` augmentation are each refused or harmless, under strict tsc 6.0.3 with 0 `any` keywords. The same library with naive trust gives a wrong answer through three of those routes with no cast (§5.2).
5. **Tested is not proven, and the note measures it.** With a 200-case budget the broken compare's antisymmetry is refuted under all 9 seeds tried, and its transitivity under 5 of 9 (§2.11).
6. **R1-R15 are Effra laws over trace observations,** declared once in a runtime contract and discharged per provider, with host fixtures as a named discharge mode only for behaviour Effra cannot observe (§7).

**Losers, with their reasons kept (§11):** laws as a library of property tests (the compiler could neither refuse a build nor gate evidence on them); laws on receiver methods alone (no home for laws relating two methods); type-directed `Arbitrary` instances users write (a second dispatch mechanism); random seeds by default (gates must be deterministic); a branded inward parameter (TypeScript's unsoundness delivers non-evidence to it with no cast); host fixtures as the only form of runtime laws (each provider re-ports them).

## 1. Declaration sites

### 1.1 The spelling is provisional (#135)

The sketches use one provisional spelling, chosen to read like Go:
- type parameters in square brackets with implicit kinds, `contract Order[T]`;
- named constraints in the type-parameter list, `fn sort[T Order](xs: Seq[T])`, as in Go's `[T cmp.Ordered]`;
- grouped parameter types, `law transitive(a, b, c: T)`;
- methods named through the owner module, `T.compare`, as spike-where §2.1 resolves them;
- no row variables.

Effra has no `&&`, `||` or `!` today (the absent-syntax table, `internal/compiler/absent_syntax.go:92`, refuses `!`), so law bodies use two ordinary helpers, `fn implies(p, q: bool) -> bool { if p { q } else { true } }` and `fn both(p, q: bool) -> bool { if p { q } else { false } }`.

The prototype parses an older spelling: `contract Order<T>`, `fn(T, T) -> i64` entries bound by name in the satisfaction, and a `mint` keyword. The table maps one to the other. #135 decides how users write it; it cannot change what the checker checks.

| Concept | Provisional sketch | Prototype (`wf10/spike-laws-proto`) |
| --- | --- | --- |
| Contract | `contract Order[T] { T.compare: fn(T, T) -> i64 … }` | `contract Order<T> { compare: fn(T, T) -> i64 … }` |
| Law | `law transitive(a, b, c: T) { … }` | `law transitive(a: T, b: T, c: T) { … }` |
| Effect law | `effect law roundTrip(value: T) raises {…} uses {…} { … }` | same, `uses` not yet parsed |
| Satisfaction | `User satisfies Order cases 200` (methods resolved through User's owner module) | `User satisfies Order<User> cases 200 { compare = compareUsers }` |
| Trusted or unresolved law | `trusted transitive: "why"` inside the satisfaction | same |
| Evidence kind | `evidence SortedBy[S] by Order.compare` | `evidence SortedBy<S> by Order.compare` |
| Minting | `SortedBy[Trio, User.compare](sorted)`, construction in the evidence's owner module | `mint SortedBy<Trio, compareUsers>(sorted)` |
| Admission | `admit SortedBy[Trio, User.compare] by sortedTrio` | same, angle brackets |
| Requiring evidence | `fn search(xs: SortedBy[Trio, User.compare], x: User) -> bool` | angle brackets |

### 1.2 On a contract: Order and Eq

Spike-where §2.10 recommends that laws attach to a named contract that groups `where` entries, satisfied by a declaration in the type's owner module. This note adopts that.

```
// provisional spelling (#135)
contract Eq[T] {
    T.equals: fn(T, T) -> bool
    law reflexive(a: T) { T.equals(a, a) }
    law symmetric(a, b: T) { T.equals(a, b) == T.equals(b, a) }
    law transitive(a, b, c: T) { implies(both(T.equals(a, b), T.equals(b, c)), T.equals(a, c)) }
}

contract Order[T Eq] {                       // a named constraint: Order refines Eq
    T.compare: fn(T, T) -> i64
    law antisymmetric(a, b: T) { implies(both(T.compare(a, b) <= 0, T.compare(b, a) <= 0), T.compare(a, b) == 0) }
    law transitive(a, b, c: T) { implies(both(T.compare(a, b) <= 0, T.compare(b, c) <= 0), T.compare(a, c) <= 0) }
    law agrees(a, b: T) { (T.compare(a, b) == 0) == T.equals(a, b) }   // relates two methods
}

User satisfies Order cases 200               // in User's owner module
fn sort[T Order](xs: Seq[T]) -> SortedBy[Seq[T], T.compare]
```

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
- **An effect law** (`effect law`) may `run` effects. Its failures and services are explicit, `raises {…} uses {…}`, as every public effect contract is. A raised failure refutes the law with the failure recorded, which is how a codec's decode failure becomes a counterexample (§1.5). The prototype parses `raises`; `uses` is follow-up L3.
- **Only deterministic providers.** A law's services are filled by the subject or by providers the satisfaction names, and each must be deterministic: `TestClock`, `TestScheduler`, in-memory stores. `LiveClock` and other live providers are refused, because a law must give the same result on both targets from the same seed (§3.2).
- **One fresh scope per case.** Each case runs in its own scope with a fresh test provider, closed before the next case, so cases cannot leak state into each other.
- **A law is not a value.** It cannot be called, passed or stored; its name resolves only in receipts, inspection and satisfactions. It may call any function, but it cannot mint evidence, because no law body is in a position to construct an evidence wrapper (§4.3).
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
| (none; the law type-checks) | **Nominally checked** | The contract, its laws, the satisfaction's bindings and the law identities type-check. | None. | Listed with the law. |
| `tested` | **Tested law** | Every case in the budget passed, on this target, for this source digest and root seed. | Allowed. | Case count, seed and digest shown. |
| `refuted` | **Refuted** | A case failed. The receipt carries the original and the shrunk counterexample and the failure. | **Refused** (EF170). | The counterexample, with the law named. |
| `trusted` | **Trusted decision** | The satisfaction marks the law `trusted law: "why"`. It is not run. | Allowed. | Justifier shown; the empty justifier is refused (EF002); listed by `ef inspect` as a trusted decision. |
| `unresolved` | **Unresolved / unavailable** | Not run: the satisfaction marks it `unresolved law: "why"`, the budget is missing, a generator is missing for a law the satisfaction marks unresolved, or the receipt is missing or stale. | Allowed with EF173, a visible note. It blocks minting (§4.3). | Reason shown. |
| `verified` | none | Reserved (§2.9). | Refused as unknown. | — |

The words are the receipt's spelling of the five categories, not a second vocabulary. A receipt for an R law (§7) uses the same field and the same values.

### 2.2 Receipts

Each `ef test` run writes `dist/<name>.laws.<target>.json` (prototype `internal/compiler/laws_receipt.go`):
- `schemaVersion`, `revision` (the generated program's revision), `source` (the digest of the user's sources, so receipts from two targets compare even though bundled imports give each target its own revision), `target` and `rootSeed`;
- per law: `id` (`Contract.law@Subject`), `status`, `seed`, `cases`, `digest` (FNV-1a over the rendered cases, so two targets prove they generated the same cases, not merely the same verdict), and, when refuted, `original`, `counterexample`, `failure` and `shrinkEvaluations`; when trusted or unresolved, `justifier`.

lawful-runtime-contract requires receipts to bind provider revision, target, toolchain, fixture revision, law IDs, seed and raw outcome. The prototype's receipt carries all but the toolchain and provider revision, which RS1 adds (§7).

### 2.3 Generators

**Derivation is structural and by type, decided by the compiler.** There are no user-written `Arbitrary` instances, because that would be a second type-directed lookup beside spike-where's owner-module resolution.

| Type | Generator | Prototype |
| --- | --- | --- |
| `i64` | Size-ramped: small zigzag values while the size is below 16, then a class choice among tiny, ±65 537, a boundary table (`0`, `±1`, `MinInt64`, `MaxInt64` and neighbours) and full 64-bit; with probability 1/4 a value near one already drawn in this case | yes |
| `string` | Length up to `min(size, 8)` over a 13-character alphabet that includes `"`, `\`, a newline, `é`, `€` and `😀` | yes |
| `bool` | One draw | yes |
| record | Each field in declaration order | yes |
| enum | A variant draw, then its fields | yes |
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
- **Observed** (default seed, `order-broken.ef`). Transitivity first failed at `a = User { age: -6, name: "\\A\"0😀\n" }, b = User { age: -7, name: "\nzz00€ " }, c = User { age: -8, name: "\"c" }` and shrank to `a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }` in 234 evaluations. Antisymmetry shrank from `a = User { age: 1, name: "" }, b = User { age: 0, name: "c" }` in 17. The receipt keeps both the original and the shrunk case.

### 2.5 Seeds

- **A fixed root seed by default** (`0`), overridden by `EFFRA_LAW_SEED`. ZIO Test and MoonBit fix theirs too (ZIO `TestRandom.DefaultData`, MoonBit `seed? = 37`); QuickCheck, proptest and fast-check draw a random seed per run (§9). A gate must give the same verdict for the same source, so Effra fixes it.
- **Per-law seeds.** Each law's seed is `fnv1a32(rootSeed + "/" + id)`, so adding or removing a law changes no other law's cases.
- **The generator is mulberry32 on both targets,** with the same integer arithmetic, so Go and JavaScript draw the same numbers (the shared `digest` in §6 is the receipt).
- **Exploration is separate.** New seeds belong in a non-gating exploration job, and a counterexample it finds is pinned as an explicit example on the satisfaction, as proptest checks in its regression seeds (§13 Q6).

### 2.6 Budget

- **The case budget is required** (`cases N`), under "justify, don't ban" (`docs/design.md`, the owner rule of 2026-10-07): there is no silent default. The prototype treats a missing budget as unresolved for every law, which blocks minting (EF181 "no case budget"); §13 Q2 recommends making the omission a diagnostic instead.
- The budget sits on the satisfaction, where the subject is bound, and a law may lower or raise it for itself. A machine law adds `depth`.
- `unbounded` is not a legal case budget: an unbounded run never produces a receipt.
- The shrink limit (2 000 evaluations) and the size ramp (`case % 65`) are fixed analysis budgets, like the compiler's other analysis budgets, not user budgets.

### 2.7 Refuted: the diagnostic

`ef test` prints one line per refuted law and exits non-zero:

```
EF170 law Order.antisymmetric@User refuted after 2 cases (seed 3747600544): returned false at a = User { age: -1, name: "" }, b = User { age: 0, name: "" }
EF170 law Order.transitive@User refuted after 53 cases (seed 224550084): returned false at a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }
```

`ef build` then refuses:

```
EF170 refuted laws refuse the build: Order.antisymmetric@User at a = User { age: -1, name: "" }, b = User { age: 0, name: "" }; Order.transitive@User at a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }
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

Unresolved has five sources, each with a visible reason in the receipt:
1. `unresolved law: "why"` in the satisfaction;
2. no case budget;
3. a law that cannot be generated and is marked unresolved;
4. no receipt for the current source digest on this target (`ef build` prints EF173 "laws are unresolved for this revision on go: run ef test to discharge them");
5. an unavailable target or provider, for R laws (§7).

An unresolved law never refuses a build. Sources 1 to 3 are known when the program is checked, so they block minting evidence through that satisfaction at check time (EF181, §4.3). Sources 4 and 5 are known only when receipts are read; the prototype then builds with EF173 even if the program mints, which leaves its evidence resting on laws not yet run. §13 Q8 recommends refusing that case.

### 2.11 What tested means

A finite run is evidence, not proof (lawful-runtime-contract). `benchmarks/laws/runs/seed-survey.txt` runs the broken compare under nine root seeds with a 200-case budget:

| Root seed | antisymmetric | transitive |
| --- | --- | --- |
| default (0) | refuted at case 2 | refuted at case 53 |
| 1 | refuted at 2 | refuted at 69 |
| 2 | refuted at 1 | tested ×200 |
| 3 | refuted at 2 | tested ×200 |
| 4 | refuted at 7 | tested ×200 |
| 5 | refuted at 4 | tested ×200 |
| 6 | refuted at 12 | refuted at 141 |
| 7 | refuted at 7 | refuted at 80 |
| 8 | refuted at 2 | refuted at 3 |

Go and JavaScript agree on every row. Transitivity of `compareNear` fails only for three ages within two of each other, which a 64-bit generator rarely draws, and the near-value pool (§2.3) is what makes 5 of 9 find it. A gate that runs the default seed catches this defect; a different defect could need more cases or an explicit example. That is why a receipt says `tested` and never `proven`.

## 3. When laws run

| Command | Runs laws? | What it does with them |
| --- | --- | --- |
| `ef check`, LSP, MCP | **No** | Checks declarations and generators (EF171), and reports each law's status from the latest receipt for the current source digest. Inspection never executes user code, so CLI and MCP keep one semantic model. |
| `ef test` | **Yes**, on the selected target | Runs every runnable law beside the tests, writes the receipt, prints EF170 per refutation and exits non-zero. A program with laws and no tests is accepted. |
| `ef test --target both` | **Yes**, on Go and JavaScript | Runs both, compares the receipts, and reports a disagreement as EF172. |
| `ef build`, `ef run` | **No** | Reads the receipt for the target and source digest: refuted refuses (EF170), missing or stale prints EF173 and builds. A build never executes user code. |
| The gate | **Yes**, through `ef test --target both` on every program with laws | Refutations and disagreements fail the gate. |

**Same cases on both targets.** The harness generates each case from the per-law seed in the target's own code, with the shared algorithm (§2.4, §2.5), so the targets need not exchange cases. The `digest` field proves they generated the same ones.

**A cross-target disagreement is a refutation.** If the Go and JavaScript receipts differ in any law's status, case count, digest or counterexample, the law is refuted for both targets: one of them computes a different function from the same source, which is the unchecked-passthrough class of defect Effra refuses. The prototype detects and reports it (`CompareLawReceipts`, EF172); its fault control feeds a synthetic disagreement (`TestCompareLawReceiptsReportsDisagreement`), because no real one occurred. Recording the disagreement as `refuted` in both receipts is follow-up L2.

**Cost, measured.** On the workbox at load averages 4 to 11 (2026-10-12), `ef test` on one law program took 0.10 to 0.15 s per target when its test binary was cached and 0.39 s (Go) or 0.15 s (JavaScript) after a source change. That covers 1 000 cases for `order.ef`'s six laws. Laws add one test-harness run per program per target, inside the gate's existing test steps. Gate speed matters only when it blocks, and this does not block.

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
- An **evidence type** `SortedBy[Trio, User.compare]` is a nominal wrapper over the carrier `Trio`. Inside Effra it reads as the carrier (field access, matching) and is accepted wherever the carrier is.
- **The index resolves to a declaration.** `User.compare` is the declaration spike-where's resolution yields for `T.compare` at `T = User`; in the prototype it is the bound function `compareUsers`. Two values sorted by the same declaration share a type, so `merge(a, b: SortedBy[S, T.compare])` type-checks without GDP's rank-2 naming. A conditional method's index is the canonical key (§1.3).
- An index that is not a declaration is refused: "evidence index must name a declaration" (`TestLawEvidenceRefusals`, closure index).

### 4.3 Who mints, and when

A discharged law does not mint a value. It makes its satisfaction **lawful**, and a lawful satisfaction is what lets its bound declarations index evidence:

| Source | May mint `E[C, I]`? | Prototype |
| --- | --- | --- |
| Code constructing `E[C, I](value)` where `I` is a declaration bound to the kind's entry by a satisfaction whose laws are all tested or trusted | **Yes**, in the evidence kind's owner module | `mint`, EF181 otherwise |
| The same with a satisfaction that has an unresolved law, or no budget | No: "unresolved (needs a fixture)", "no case budget" | EF181 |
| `I` a declaration not bound to the entry (`reversed`) | No: "reversed is not bound to Order.compare" | EF181 |
| `I` a parameter or closure (`f`, a `…By` comparator) | No: "f is not a function declaration" | EF181 |
| A satisfaction whose receipt says refuted | Minting checks, but the build is refused | EF170 at build |
| The host-boundary adapter, through `admit E[C, I] by check` when `check(value)` holds | **Yes** | EF186 on failure |
| A law body | No (§1.8) | — |

- **Owner module.** Construction of an evidence wrapper is private to the module that declares the kind, as construction of a `pub readonly` type is private to its owner. The prototype has one module, so this is follow-up L4.
- **A mint is a trusted decision.** Minting asserts that `sortTrio`'s result is sorted. Nothing proves the sort correct. §13 Q4 recommends a law the compiler derives for each minting function, which turns that trust into a test.
- **Wrong carrier and unminted results.** Minting `SortedBy<Trio, …>` over a `User` is EF182; declaring an evidence result and returning the bare carrier is EF183.

### 4.4 Requiring evidence

`search(xs: SortedBy<Trio, compareUsers>, x: User)` accepts only evidence (`examples/laws/order.ef`). Passing a plain trio is EF184 "search requires SortedBy<Trio, compareUsers> for xs, but the argument is the carrier Trio with no evidence", and passing evidence for another index is EF184 "the argument is SortedBy<Trio, reversed>" (`TestLawEvidenceRefusals`).

### 4.5 What mints nothing

- **A `…By` closure.** It has no declaration identity, so it cannot be an index (EF181). `sortTrioBy(t, compare)` returns the carrier.
- **A function a TypeScript caller passes.** It arrives as a parameter, so it is a `…By` closure (EF181), and the export returns the carrier (§4.6, rule 1).
- **A derived or structural value.** Derivation, zero values, record reconstruction and foreign conversion never construct the wrapper (opaque-values).
- Naming a comparator *value* in a type needs GDP's fresh-name mechanism. That stays a separate candidate in opaque-values, and until it exists none of these mint.

### 4.6 The host boundary

The four rules are spike-where §2.10's requirements, made concrete:

1. **Outward (forms i and ii).** An exported function whose result is evidence either binds the canonical method on the Effra side, so its TypeScript signature has no method parameter (form i, `sortTrio(t: Trio) => SortedBy<Trio, "compareUsers">`), or returns the unbranded carrier (form ii, `sortTrioBy(t, compare) => Trio`). The Effra checker enforces this already, because a parameter cannot be an index (§4.5).
2. **Inward: host evidence is never trusted.** An exported function with an evidence parameter takes the **unbranded carrier** in its `.d.mts` and re-establishes the evidence on entry through the owner's admission check, or it is refused (EF185, "declare `admit SortedBy<Trio, compareUsers> by <check>` … or keep search private"). The admission is `admit SortedBy<Trio, compareUsers> by sortedTrio`, where the check must be a pure function declaration from the carrier to `bool` and the index must be lawfully bound (EF187). A value that fails the check throws `TypeError: EF186 search: xs is not SortedBy<Trio, compareUsers>; host evidence is re-established on entry and this value fails sortedTrio`.
3. **Stability: a deep, Effra-owned snapshot.** Every data argument of an evidence-carrying export is copied into a fresh, deep-frozen value through own data properties only (an accessor is refused with EF186), and type-checked (`i64` must be a 64-bit `bigint`) before any check or mint. Returned evidence is deep-frozen.
4. **Private canonical bindings.** Adapters call module-private `const` bindings (`__ef_function_sortedTrio`, `__ef_function_search`). An ES module namespace is already non-extensible and its bindings read-only, so `Object.assign` on it throws; patching `call` on an exported function does not reach the adapter. W3's frozen companion keeps this when companions exist.

**Outward brand.** `.d.mts` declares `declare class EfEvidence<E extends string, S, I extends string> { private readonly __ef_evidence: readonly [E, S, I]; }` and `export type SortedBy<S, I extends string> = S & EfEvidence<"SortedBy", S, I>`. A private member of a declared class cannot be supplied by a literal or by `Object` augmentation, so a plain trio is not `SortedBy` (TS2322) and evidence for one index is not evidence for another (TS2322). The brand is a convenience for TypeScript callers that track evidence among themselves; because no Effra export trusts it, forging it with a double cast gains nothing. That is the honest name for this boundary.

**Scope of the prototype.** Evidence occurs only in top-level parameters and results there. Evidence nested in a record, a container or a callback is follow-up L4/L5: every input occurrence is re-established and every output occurrence branded.

### 4.7 Wrapping without losing evidence

- **Inside Effra.** Evidence flows through `let`, calls, `run`, and `if`/`match` branches that agree, so an ordinary function wrapping a minting function returns evidence without minting. `TestLawEvidenceAcceptsMintedFlow` accepts `again(xs: SortedBy<…>) -> SortedBy<…> { xs }` and `pick(flag, t) -> SortedBy<…> { let sorted = sortTrio(t); if flag { sorted } else { again(sorted) } }`, then passes `pick`'s result to `search`. Branches that disagree give the carrier.
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

Run time with laws is therefore zero by construction. The host adapter's cost, one deep copy of the arguments per call of an evidence-carrying export, was not measured.

### 5.2 `.d.mts` under attack

`benchmarks/laws/ts/run.sh` checks the prototype's emitted library (`ts/proto/lib.mjs`, `lib.d.mts`, built from `examples/laws/sorted.ef` at `5f428883`) with the repository's TypeScript 6.0.3, `--strict`, and runs each probe on node 24.11.1. The receipt is `benchmarks/laws/runs/ts-boundary.txt`: every tsc run exits 0, 5 of 5 directives fire with their declared codes (4 in `proto/`, 1 in `counterfactual/`), and every file has 0 `any` keywords.

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
| G1 getter answering differently per read | 3 | type-checks | EF186 ".first is not an own data property" |
| R10 `Object.assign(lib, { compareUsers: reversed })` | 4, private bindings | type-checks | `TypeError` (read-only namespace) |
| R10b patch `call`/`apply` on the exported compare | 4 | type-checks | correct: `search` is unaffected |
| N1 never filler `search(none[0], ada)` | standard route | type-checks | EF186 "xs is not a Trio" |
| N2 never rest `const v: (...rest: never[]) => boolean = search; v()` | standard route | type-checks | EF186 |
| N3 untyped value `search(JSON.parse(…), ada)` | standard route | type-checks | EF186 "xs.first.age is not an i64" |
| O1 `Object` augmentation, then `const f: SortedBy<…> = trio` | standard route | TS2322 (private member) | EF186 if passed to `search` |
| F1 plain trio as `SortedBy` | outward brand | TS2322 | — |
| F2 evidence for `compareUsers` as evidence for `reversed` | outward brand | TS2322 | — |

**Counterfactual.** `benchmarks/laws/ts/counterfactual/` is the same library with the `search` adapter removed and its parameter branded, which is naive trust. R2, R3 and X2 type-check with no cast and `search` answers `false` for a user who is in the trio (`R2-array-covariance=false R3-method-bivariance=false X2-property-covariance=false`). The run fails if a hole closes, so the comparison cannot go stale. This is why the inward parameter is unbranded: TypeScript's documented unsoundness delivers non-evidence to a branded parameter without forging the brand.

Each row of spike-where's expected-failure table is now a refusal or a correct result: R1, R2, R3, X2 and R10 refuse, and A1 and A2 are correct.

### 5.3 Name-neutrality

The prototype's law, evidence, receipt and boundary code (`internal/compiler/laws*.go` on `5f428883`) contains no string literal `"Order`, `"RoundTrip`, `"compare`, `"SortedBy`, `"Codec` or `"codec` (`grep` over the four files finds none). Contract, law, entry and evidence names are read from the source.

## 6. Prototype

**Files on `wf10/spike-laws-proto`** (2 535 lines added):
- `internal/compiler/laws.go`: declarations, elaboration of each law per satisfaction into a checked `law__C__S__l` function returning `bool`, per-law seeds, evidence checks EF180-EF184 and mint authority;
- `internal/compiler/laws_emit.go`: generator derivation and EF171, and the Go and JavaScript harnesses, which share the generator, shrinker and digest algorithms;
- `internal/compiler/laws_receipt.go`: the receipt, its comparison by source digest, and the build verdict (EF170, EF173);
- `internal/compiler/laws_boundary.go`: `admit`, EF185 and EF187, and the JavaScript adapter and brand;
- `internal/compiler/laws_test.go`: elaboration, accepted flows, every refusal, the receipt comparison fault control, and both erasure tests;
- `cmd/ef/laws.go`: `ef test --target both`, receipt files and build verdicts;
- `examples/laws/{order,order-broken,codec,sorted}.ef` and `examples/laws/run.sh`.

**How to run.** On the proto branch, `examples/laws/run.sh <scratch>` builds `ef` and runs every example on both targets, then checks the expected statuses and counterexamples; it exits non-zero on any mismatch. The full Go suite (`go test ./internal/... ./cmd/...`) passes at `5f428883`, and the law tests are listed in `benchmarks/laws/runs/proto-law-tests.txt`.

**Results** (`benchmarks/laws/runs/proto-e2e.txt`; Go and JavaScript identical, `agree:true` for every program):

| Law | Status | Cases | Digest | Counterexample |
| --- | --- | --- | --- | --- |
| `Order.reflexive@User` | tested | 200 | `5b7e1eea` | |
| `Order.antisymmetric@User` | tested | 200 | `038ffad4` | |
| `Order.transitive@User` | tested | 200 | `74b44f4f` | |
| `Order.reflexive@Trio` | tested | 100 | `d75a6abf` | |
| `Order.antisymmetric@Trio` | tested | 100 | `ea87a6bb` | |
| `Order.transitive@Trio` | trusted | 0 | — | justifier "lexicographic composition of Order@User, whose transitivity is tested" |
| `Order.reflexive@User` (broken compare) | tested | 200 | `5b7e1eea` | |
| `Order.antisymmetric@User` (broken) | **refuted** | 2 | `fc398e3b` | `a = User { age: -1, name: "" }, b = User { age: 0, name: "" }` |
| `Order.transitive@User` (broken) | **refuted** | 53 | `796ca8a3` | `a = User { age: 0, name: "" }, b = User { age: -1, name: "" }, c = User { age: -2, name: "" }` |
| `RoundTrip.roundTrip@accountJson` | tested | 200 | `3333374f` | |
| `RoundTrip.roundTrip@halfJson` | **refuted** | 2 | `60e410b1` | `value = Profile { active: false, name: "", score: -1 }` |

Then:
- `ef build order-broken.ef` is refused with EF170 (§2.7); `ef build order.ef` is admitted; `ef run order.ef` prints `true` on Go and on JavaScript (`search` over minted evidence);
- `EFFRA_LAW_SEED=7` changes `rootSeed` to `7` in both receipts and they still agree;
- the evidence program `order.ef` exercises minting (`sortTrio`), requirement (`search`) and admission (`sortedTrio`); `sorted.ef` is the host-boundary library of §5.2.

**What the prototype does not do:** `uses` on effect laws, service and machine laws, generic and recursive data generators, conditional satisfaction, owner-module minting across modules, nested evidence, and recording a cross-target disagreement as `refuted` in the receipts. Each is a follow-up unit (§12).

## 7. The #124 bridge

**Decision: R1-R15 are Effra laws over trace observations, declared once in a `Runtime` contract and discharged per provider. A host fixture is a named discharge mode for a declared law, used only for behaviour an Effra program cannot observe, and it writes the same receipt.**

```
// provisional spelling (#135)
contract Runtime {
    effect law releaseIsLifo(n: Count) uses {Trace} {                 // R1
        lifo(run observe(acquireAll(n)))
    }
    effect law closeAwaitsChildCleanup(d: Millis) uses {Trace, Clock} {   // R2
        precedes(run observe(closeWithSlowChild(d)), ChildCleanup, OwnerRelease)
    }
    law constructionRunsNothing(k: Script) { … }                      // R11, a pure law over a built recipe's trace
    effect law panicIsDefectWithCleanup() fixture "<host test id>"      // R15, host-only; names the host test
}
GoRuntime satisfies Runtime cases 100
EffectRuntime satisfies Runtime cases 100
```

The reasons:
1. **The laws are language meaning.** The owner placed runtime laws in the language-construct tier, and a third provider "would be the same laws". Declared in Effra, each law has one statement that every provider passes; as host fixtures, each provider would re-port and re-interpret the statement.
2. **The trace is already the observation.** #124's acceptance requires both providers to emit one event vocabulary (`construct`, `admit`, … `publish`) with owners and identities, and a reference checker over partial orders. A test provider that exposes those events to Effra makes each R law a pure predicate over observed data, and the reference checker is ordinary Effra code.
3. **The mechanism already exists in this design.** Same seeds on both targets, cross-target comparison, one receipt schema and the five categories all carry over. Law parameters are the program's shape (resource count, child delay, failure placement), so the causal mutants become ordinary refutations with shrunk counterexamples: `m1` (FIFO release) should refute R1 at `n = 2`.
4. **Composition laws join the same contract** (§1.7): nested deadlines, monad laws and provide's distribution are runtime laws too.
5. **Host fixtures stay honest where they must.** A managed panic raised from host code (R15) and anything outside the managed protocol cannot be produced from Effra. Such a law is still declared in `Runtime`, so its ID, statement and receipt are uniform; its discharge names the fixture, and the receipt says `fixture` beside the status.

**Prerequisites and order.** This needs effect laws with `uses` (L3), service-style satisfactions by providers (L3), the trace service from #124's trace model, and sequences (#120) for traces. Until a law's Effra form can run, its receipt is `unresolved` with the reason "needs trace sequences (#120)", not a silently passing host test. #124 can start its trace model and reference checker in parallel; the checker is written so it moves into the law bodies unchanged.

**The losing option, kept:** host fixtures under the same receipt schema, as cats-effect's law modules run on `TestContext`. It is available now and needs no Effra feature, but it gives each provider its own copy of the statement, it cannot shrink a counterexample program, and a third provider must port every fixture.

## 8. Composability

The five properties the owner asked for, for laws and evidence.

**1. Lossless wrapping.** An ordinary function that wraps a law-checked thing keeps everything:
- evidence, through calls and agreeing branches (§4.7), and through generics once 4a lands;
- rows, ownership and observations, because laws and evidence add no emitted code (§5.1), so the wrapper's 4a contract (#128) is unchanged;
- lawfulness, because a satisfaction belongs to the declaration, not to a call path.
The one thing a wrapper cannot carry is a method's declaration identity through a closure (§4.7).

**2. Nesting matrix.** Rows are law-bearing constructs; columns are contexts.

| Construct ↓ / context → | Generic function | Service | Machine | Effect recipe, scope, timeout | Another module | Host boundary |
| --- | --- | --- | --- | --- | --- | --- |
| Contract + satisfaction | `where T: Order` expands to entries (spike-where) | A service is its own contract over operations (§1.4) | A machine may require `where Event: Eq` | Not applicable (declarations) | Satisfaction in the subject's owner module; transported in interface summaries (L1) | Erased; the `.d.mts` has methods only |
| Law | Laws over an abstract `T` discharge per instantiation (§1.3) | Effect laws over providers (§1.4) | Reachable-state laws (§1.6) | Effect law bodies run recipes, each case in a fresh scope (§1.8) | Runs in the satisfying module's `ef test` | Erased |
| Evidence value | Passes as a type argument after 4a (L4) | Service operations may take or return evidence; a host-supplied provider's evidence is re-checked on entry | A machine state may carry evidence | `run` of a recipe returning evidence yields evidence | Kind owned by its module; construction private | Outward branded, inward re-checked (§4.6) |
| Admission check | — | — | — | Must be pure | Declared in the evidence kind's module | Runs in the adapter |

Refused nestings: a law inside a law, a law quantified over laws or contracts, a satisfaction inside a function, and evidence indexed by a closure.

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
- **What they share and Effra does not.** Each keeps one runtime per target and treats properties as tests a library adds. Effra makes laws a construct because a law gates the build and gates evidence, and because Effra intends more than one runtime provider (#124), which only shared laws can hold to one meaning.

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
| Keyed on a name? | Only on the words `contract`, `law`, `satisfies`, `evidence` and the satisfaction's contextual `trusted`, `unresolved`, `cases`; never on Order, compare or Codec (§5.3). |
| Readable with Go's simplicity | Contracts read like a Go interface with laws; the spelling is #135's. |
| Same meaning in TypeScript, no `any` | Laws erase; evidence projects to a private-member brand outward and to the carrier inward, with 0 `any` (§5.2). |
| Erases into Go | Byte-identical without laws; evidence erases (§5.1). |
| Two or more unrelated clients | Order, codec round-trip, services, machines and the runtime contract. |
| Drift into a proof language | None: predicates only, no proof terms, `verified` reserved. |

## 11. Decision record

**Decided 2026-10-12 (spike #125, revision 1).**

- **Chosen.**
  - Laws are declared in a `contract`, a `service` or a `machine`, with typed universally quantified parameters. A law body is pure, or an `effect law` with explicit `raises` and `uses` over deterministic providers.
  - A `satisfies` declaration in the subject's owner module binds the subject and states a required case budget; it may mark laws `trusted` or `unresolved` with a justifier.
  - Discharge is one deterministic runner: compiler-derived structural generators, choice-sequence shrinking, a fixed root seed with per-law seeds, the same algorithm on Go and JavaScript, and one receipt per target whose statuses are lawful-runtime-contract's categories.
  - `ef test` runs laws, `--target both` compares receipts, a disagreement is a refutation, the gate runs both targets, `ef build` reads receipts and refuses refuted laws, and `ef check` never executes.
  - Conditional satisfactions discharge per instantiation reached, and compose from their parts.
  - Evidence is a nominal wrapper over its carrier, indexed by a declaration bound by a lawful satisfaction, constructed only in its kind's owner module, never indexed by a closure or a host-passed function.
  - The host boundary follows the four rules: outward forms i and ii with a private-member brand, inward re-establishment through a declared admission check, a deep snapshot, and private canonical bindings.
  - R1-R15 are Effra laws over trace observations in a `Runtime` contract, discharged per provider, with host fixtures as a named discharge mode for unobservable behaviour.
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

## 12. Follow-up units

Prerequisites costed elsewhere: spike-rows unit 3 (receiver methods, explicit type arguments) and unit 4a (#128, whole-value type parameters and the recipe-parameter contract); spike-where W1 (`where` clauses, conditional methods) and W3 (projection and frozen companions); machines (the checked machine declaration); #120 (sequences).

| Unit | What lands | Tests and acceptance | After | Estimate |
| --- | --- | --- | --- | --- |
| **L1. Contracts, laws and satisfactions** | Parser, formatter and LSP spans in #135's spelling; contracts as groups of `where` entries; elaboration of laws into checked functions; satisfactions in the owner module with `cases`, `trusted`, `unresolved`; EF002-family diagnostics; interface-summary transport (schema bump, round trip, `TestEvidenceSchemaIsClassified`); inspection facts in CLI and MCP; reachability in test mode. | The prototype's declaration tests and refusals with messages; a cross-module satisfaction; inherited laws run once; the name-neutrality screen. | rows 3, W1, #135 | M to L, 4 to 5 days |
| **L2. The runner** | Generators for scalars, records, enums, generic data (after 4a) and recursive data; the choice-sequence shrinker; seeds and receipts; Go and JavaScript harnesses sharing one algorithm; `ef test --target both`; disagreement recorded as refuted (EF172); gate integration. | The prototype's results on both targets, byte-identical receipts; a generic `Option[User]` and a recursive list law; the seed survey; a disagreement fault control that changes the receipts. | L1, 4a | M to L, 4 to 5 days |
| **L3. Effect, service and runtime-contract laws** | `effect law` with `uses`; deterministic-provider refusal for live providers; service satisfactions per provider with `using`; one fresh scope per case. | The codec law; a service law over two providers, one deliberately lossy; a live provider refused; a failure raised in a law shows in the counterexample. | L2, 4a | M, 3 days |
| **L4. Evidence** | Evidence kinds as nominal wrapper types (generic-transparent after 4a); construction private to the kind's owner module; index resolution to declarations and canonical keys; admissions; EF180-EF187; the mint-soundness law (§13 Q4). | The prototype's evidence tests; cross-module minting refused; `Option[SortedBy[…]]` through a generic; nested evidence in a record. | L1, 4a, W1 | M to L, 4 to 5 days |
| **L5. Host evidence boundary** | The JavaScript adapter (snapshot, admission, deep freeze) for every input and output occurrence; the private-member brand; `benchmarks/laws/ts` promoted to a generated-consumer fixture. | Every row of §5.2 with its pinned outcome, plus nested and callback occurrences; the counterfactual kept; 0 `any`. | L4, W3 | M, 2 to 2.5 days |
| **L6. Machine laws** | Reachable-state laws with `depth`; event-sequence generation; trace counterexamples shrunk by length first. | A Session invariant tested; a deliberately broken step refuted with a numbered trace; the same trace on both targets. | machines, L2 | M, 2.5 to 3 days |
| **L7. Conditional satisfaction** | Per-instantiation discharge with composed generators; verdict composition from parts; library-declared instantiations; canonical keys in receipts. | `Pair[i64, User]` and `Option[Option[User]]` discharged; an unresolved part makes the composite unresolved; Trio's trusted transitivity replaced by a tested composite. | W1 conditional methods, L2 | M, 2 to 3 days |
| **L8. Docs and preview** | `docs/design.md:42` extended; GLOSSARY "Contract", "Satisfaction", "Evidence kind", "Admission", "Law receipt"; lawful-runtime-contract cross-reference; CHANGELOG; the dated language evolution preview (§13 Q1). | Docs review. | L1 | S, 0.5 to 1 day |

**Total:** about 22 to 27.5 implementor-days plus review rounds.

**Order against spike-rows phase 2 and spike-where W1-W6:**
1. rows unit 3, then 4a (#128), as spike-rows plans;
2. W1, with W2 and W6 beside it, as spike-where plans;
3. **L1** after W1 and #135; **L8**'s preview can land as soon as this decision is accepted;
4. **L2** after L1 and 4a; rows 4b and 4c can run beside it;
5. **L3 before rows unit 6**, so provide's library declaration carries its distribution law, and rows 5 to 7 gain composition laws as acceptance;
6. **L4** after L1 and W1; W3 then **L5**;
7. **L7** after W1's conditional methods; **L6** once machines lands;
8. #124's RS1 laws after L3 and #120, with its trace model and reference checker started in parallel.

## 13. Open questions

- **Q1. The language evolution preview.** `docs/wayfinder/README.md:52` requires a dated preview for a construct decision, but this spike may commit only this note and `benchmarks/laws/`. Recommendation: add the preview in a docs-only change right after acceptance, or as the first part of L8, showing the §1 sketches in #135's spelling once it is chosen.
- **Q2. A missing case budget: unresolved or an error?** The prototype treats it as unresolved, which blocks minting. Recommendation: **an error**, under "justify, don't ban": a budget is a required choice, and `unresolved law: "why"` remains for a deliberate gap.
- **Q3. Which functions does EF185 cover?** The prototype refuses every function with an evidence parameter and no admission, because it has no `pub`. Recommendation: only `pub` functions reachable from the host boundary, once `pub` exists; private functions take evidence freely.
- **Q4. Should minting be tested?** A mint asserts a fact nothing checks. Recommendation: **yes**, by a law the compiler derives for each minting function whose parameters are generatable: `admissionCheck(mintingFunction(args))`, using the kind's declared admission check (`sortedTrio(sortTrio(t))`). A kind with no admission check keeps its mint sites as trusted decisions in inspection.
- **Q5. Keyword budget.** The prototype adds `contract`, `law`, `satisfies`, `evidence`, `mint` and `admit`. Recommendation: keep the first four; drop `mint` in favour of construction under the owner-module rule (§1.1), and make `admit` a clause of the evidence declaration.
- **Q6. Regression examples.** Recommendation: allow explicit examples on a satisfaction (`example transitive(a, b, c) = …`), run before generated cases on both targets, so a counterexample found by exploration stays in the gate, as proptest's checked-in regression seeds do.
- **Q7. Toolchain and provider revision in receipts.** Recommendation: add both in L2, as lawful-runtime-contract requires for RS1, so a receipt from a different Go or node version is stale.
- **Q8. A build that mints evidence without a current receipt.** The prototype builds with EF173. Recommendation: **refuse** when the program constructs evidence whose index's satisfaction has no current receipt for the build target, because that evidence would rest on laws nobody ran; keep EF173 as a note for programs whose laws gate nothing.
