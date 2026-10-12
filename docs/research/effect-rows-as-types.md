# Effect rows as types: catch, provide and timeout as library functions

**Status:** design note for the spike in [#115](https://github.com/cevr/effra/issues/115), phase 1, revision 6, 2026-10-12. It changes no compiler behavior. Every line reference is at `main` `f900240c`, unless stated otherwise; the branch has since merged `main` `dfbdbd58` and then `13e62ad4`, which change none of the cited behaviour. Paths are under `internal/compiler/` unless stated otherwise. Phase 2 implements only what a review of this note accepts.

Revision 6 applies the rulings of re-plan 1, round 3, after the fifth review:

| Item | What this revision does |
| --- | --- |
| M1 | **The handler payload is fail-safe too.** A member is left out of a handler's payload only when its tag is provably a different single plain value; every other member is included. The payload is the fifth review's verified repair (`__ef_may_equal`). It closes A7, a branded enum member that catch removed while recover's payload left it out, and A10, a template tag whose hole is branded. Two precision pins flip to included, M03 and F09. Generated Effra tags are plain literals, so their payloads stay exact (§2.8). §2.8.1 adds a member-tag form table, and a generated self-attack over 1065 forms finds no form left out whose run-time tag can be the label. Probe C2 r6 has 102 directives (§3). |
| m1 | H2d is named as TypeScript's rest-arity unsoundness, separately from value lies (§2.8.1, §3). |
| m2 | The Gleam row of §4.1 is corrected. Gleam hides opaque constructors, but its declared classes stay structural, so a TypeScript user can build a value without a cast. |
| o1 | Boundary 3 adds control-flow narrowing across a mutating call (TypeScript #9998) (§2.8.1). |
| o2 | The probe's unit tagger takes the highest unit among the functions a statement uses, as its comment says (§3). |
| Prior art | §4 and §4.1 cite Effect 4.0.1's `catchTag` as having the same payload hole (A12), not as having closed it. §4.1 adds how compile-to-host languages keep a tag's run-time value and its payload type from drifting apart. |
| Cost | About 0.1 day more inside unit 7; the total stays at about 18 to 21 (§8). |

Revision 5 applied the rulings of re-plan 1, round 2, after the fourth review:

| Item | What this revision does |
| --- | --- |
| M1 | **The TypeScript projection is fail-safe, not refuse-only.** Removal and payload narrowing happen only when the guard's own check holds. When it fails, nothing is removed and the handler sees the whole row. Refusal is a diagnostic. The declarations are the review's verified repair, and services are removed by identifier and brand together (`I & { _effraService: N }`). The claim is narrowed to "no declared type removes a label unless the check holds for the tag type" (§2.8). Probe C2 r5 has 99 directives, and every review-4 route (method bivariance, `Object` augmentation, `never[]` rests, never fillers) is shown refused or fail-safe, with its runtime outcome (§3). |
| m1 | The case table adds the two conservative refusals: a TypeScript generic over the row (F04d), and assignment to single-literal overloads (F05b). It also adds the review's other forms and a bypass table (§2.8.1). |
| m2 | Three boundaries are listed as the host trust envelope (§2.8.1): the consumer's own unchecked type; a hand-made service whose identifier and key disagree; and value-type lies. |
| m3 | Unit 5's fixture takes only catch's 60 directives. Provide's 29 and recover's 10 move to units 6 and 7, and the note states what TypeScript exports if gate 2 fails (§7). |
| m4 | The stale "`Exclude<R, I>` with `I` unconstrained" text is replaced. A fixed service projects as `Exclude<R, ClockRequirement>`; a label-parameter service uses the gated alias (§2.3, §2.8). |
| o1 | The directive count is corrected: r4 had 11 + 4 + 39 + 12 = 66 (§3). |
| o2 | The two refusal mechanisms are stated to be diagnostics only (§2.8). |
| o3 | Distribution with an abstract label keeps every fixed label (§2.3). |
| Prior art | §4.1 adds a second table: no compile-to-host language trusts host refusal (Gleam, ReScript genType, Elixir, MoonBit, Koka, PureScript, Effect-TS). |
| Cost | About 0.25 day more in unit 5; the total stays at about 18 to 21 (§8). |

Revision 4 applied re-plan 1, after the third review:

| Item | What this revision does |
| --- | --- |
| M1 | The TypeScript guard is literal-only and runs on the service brand field. Attacking it found three more holes, now closed: an unbranded service identifier (`{}`, `object`, `unknown`, `Partial`, `Omit`) removed every requirement; a handler's payload omitted members whose tag is `string`, a union or a template; and an `any` tag (for example from `JSON.parse`, with no cast) removed every failure. §2.8 has the guard, and §2.8.1 the exhaustive case table. Probe C2 r4 has 66 negatives, all firing (§3). |
| m1 | F3b substitution distributes over abstract bindings; "result position" is defined by polarity; `withTestClock` checks (§2.3). |
| m2 | The stale "lacks is free" and "`Lacks` implicit" claims are deleted (§2.1, §4). |
| m3 | A planned unit 4c, "D4 and GLOSSARY amendment", reclassifies declared-contract primitives as ordinary library under D4. The wrapper-parity test proves composition; the correctness pins are the ownership matrix and corpus parity against each construct before it is deleted (§2.6.5, §7). |
| m4 | Each declaration moves, with its controls, to the unit that owns it: `fake<S>` and provide's declaration to unit 6, catch's to unit 5, recover's to unit 7. Unit 4a keeps clause 6 for constant providers (§7). |
| o1 | Clause 6 records a provision only for the recipe that reaches the result; a discarded provision is a body-local fact (§2.6.2). |
| o2 | Re-costed (§8). |
| o3 | One documented probe C2 command (§3). |
| Prior art | §4.1 compares literal-tag discipline in Effect-TS (`Exclude`, `catchTag`), TypeScript, ReScript polymorphic variants, Elixir, Gleam, MoonBit and OCaml, at pinned commits. |

Revision 3 applied the round-3 rulings on the second review:

| Ruling | What this revision does |
| --- | --- |
| B1 | The F3b equation `ρ - L = ρ` is deleted; only `ρ - L ⊆ ρ` remains, with composition rules and the reviewer's `leak<F>` counterexample as a negative control of the provide unit (§2.3). |
| M1, Q9 | User type parameters may be instantiated with callable types used as whole values. `examples/callables-factory.ef` is a mandatory gate-1 program; the only listed probe artefacts are `host-types.ef` and `callables-state.ef` (§2.6.3, §3, §7). |
| M2, Q6 | A reference body where one exists (catch, over recover); otherwise a declared contract in the inferred vocabulary, pinned by the ownership matrix and a wrapper-parity test (§2.6.5, §2.12). |
| M3 | The contract gains a provision clause; graph and lint provision edges derive from it, never from signature shape (§2.6.2, §2.11). |
| M4 | The TypeScript guard also refuses a widened `string` tag; the reviewer's `extra.mts` case is a negative control (§2.8, probe C2). |
| M5 | F6 moves into unit 4b, before gate 2, as a coercion on direct call arguments (§2.12, §7). |
| Minors m1-m8 | Summary transport in unit 4a; F3b clients and positions; the owner-vector in-place test with negative controls; span parity in gate 1; F6 as a coercion; the B1 test counts; open questions reconciled with the rulings; "closed at the edge" marked as not yet shown. |
| Split | Note r3, F3, F1 plus receivers, 4a, 4b, catch, provide, recover; re-costed (§8). |

Revision 2 applied the round-2 rulings on the first review:

| Ruling | What this revision does |
| --- | --- |
| B1 | Catch's fallback is evaluated strictly, like any argument. §2.9 lists the emission difference and every affected file. |
| B2 | F4 is replaced by one general **recipe-parameter contract**, inferred for every ordinary body (§2.6). A prototype measures it with user wrappers (§3, probe D). |
| M1, Q4 | Observations travel with the contract, so `m3_nonrec.ef` and `m4_rec_simple.ef` stay accepted. The unused-provide refusal at `examples/causal.ef:47` is dropped from the negative controls (§2.3). |
| M2 | Dot calls resolve through the receiver-method rule (§2.10). |
| M3 | Graph, lint and reachability facts derive from signatures and the contract (§2.11). |
| M4 | A label parameter admits exactly one label. A real runtime module backs the `.d.mts` (§2.8, probe C2). |
| M5 | The residue is restated (§2.13). The ladder treats the contract consistently (§5). |
| Q2 | One `recover<E>(handler)` (§2.12). |
| Minors | Fixed throughout. |

## Owner direction

- 2026-10-11, recalling 2026-10-08: "make provide, catch … library level code that the compiler doesn't need a special case for."
- 2026-10-11, verbatim: "I don't consider types special casing, kind of like its counterpart in typescript."

So a library function whose behaviour comes from general type machinery is ordinary library code. Special casing means a checker, emitter or reachability branch keyed on an item's identity or name. This note applies the owner's 2026-10-11 construct-vs-types ladder, which the run plan records and phase 3 writes into NORTH_STAR:

1. ordinary library;
2. library plus a general type feature. The feature must be describable without naming the construct, have at least two unrelated clients, read with Go's simplicity, project into the TypeScript declarations with the same meaning and no `any`, and erase into plain Go or Go generics. It must be a specific feature, not a type-level language, and no higher-kinded types;
3. one-to-one notation;
4. a construct, when the meaning is evaluation, lifetime or control, or when the only expressing type feature has one client, or when tooling must read the structure.

## Verdict

- **Rows are not the obstacle.**
  - The existing finite solver already removes a fixed label at a call site (probe A).
  - Small rung-2 features make removal general:
    - extension patterns with a presence check (F3);
    - a row difference in result positions (F3b), for removal without presence;
    - label parameters (F2);
    - explicit type arguments (F1).
  - The `.d.mts` projection is valid strict TypeScript with `Exclude` and no `any`, backed by a runtime module that a strict consumer imports and runs. It is fail-safe: removal and payload narrowing happen only when the literal-only check holds, and refusal is a diagnostic. Probes C and C2 back it; the guard case table, §2.8.1, has 102 directives in C2 r6. Every review-4 route is refused or fail-safe, and a handler's payload includes every member whose tag may equal the label at run time.
- **Evidence through a function edge is the obstacle, and for recipe parameters a general contract removes it.**
  - Today an ordinary function edge treats a recipe parameter as opaque (`semantic.go:4057-4060`), so wrapping `.catch` in a user function loses ownership, failure and observation evidence (probe B).
  - The recipe-parameter contract (§2.6) records, for every function, what its body does with each recipe parameter. It reuses the relation atoms that callable parameters already use. A caller substitutes the actual recipe.
  - A prototype infers it for every ordinary body (probe D). User wrappers with the library-shaped generic signature now check exactly like the direct form:
    - catch: 145 of 147 programs;
    - provide: 162 of 164;
    - timeout: 29 of 29.

    In round 1 the same wrappers matched 1, 4 and 11 programs. Of the remaining differences, two are artefacts of the textual rewrite (`host-types.ef`, `callables-state.ef`). The third, `examples/callables-factory.ef`, is a real gap: its catch fallbacks are callable values, so a library `catch<A: type, …>` must accept callable type arguments. Unit 4a closes it (Q9 extended) and must pass it in gate 1; otherwise an accepted example would become refused.
  - **In place: shown. Closed at the edge: not yet shown.** The contract's in-place half carries catch, provide and timeout's intrinsic. A user wrapper whose body closes its own `scope` around the parameter (a scope-bodied timeout model) still scores 0 of 29; it is a named follow-up, not needed by catch, provide or recover (§3).
  - **Callback parameters are not yet covered.** The same prototype matches 20 of 44 `.recover` programs through a wrapper. The gaps are two callback clauses and the pure-to-effect coercion F6 (§2.6.6). Recover is therefore conditional on gate 2.
- **Recommendation:**

  | Form | Phase 2 |
  | --- | --- |
  | `.catch<E>(v)` | GO, once unit 4a passes gate 1 (unit 5) |
  | `.provide<S>(p)` | GO, after catch (unit 6) |
  | `.recover<E>(h)` | CONDITIONAL. GO only if unit 4b brings the recover wrapper corpus to identity with the single `recover` signature (gate 2); otherwise recover stays a construct (D1 for recover alone). |
  | `.timeout(ms)` | NO-GO. Its rows and its owner are expressible; its first-completion race and the abandonment of the losing work are control (rung 4). |
  | `.provide(Layer)` | NO-GO. Tooling reads the static layer plan (rung 4). |
  | `.orFail()` | Stays syntax until `GoResult` is ordinary data (audit S8). |

  D1 narrows to `.timeout`, `.provide(Layer)` and, for now, `.orFail`, plus `.recover` if its gate fails.

## 1. Current state

### 1.1 Rows and the row arena

- A row is a normalized, duplicate-free, sorted set of string labels:
  - `RowID uint32`, with `emptyRowID = 0` (`semantic.go:248-251`);
  - `normalized` sorts and compacts (`semantic.go:2141-2145`);
  - `union` and `remove` (`semantic.go:2146-2147`).
- `internRow` (`semantic.go:3137-3158`) interns a set by its joined key into `c.rows []RowNode`. It records any label that names a row parameter in `RowNode.Parameters`.
- The public row identity is a hash of the labels (`rowNodeIDForLabels`, `semantic.go:3102-3105`).
- Type nodes carry `FailureRow` and `ServiceRow` RowIDs (`semantic.go:235-245`).
- Assignability treats rows as upper bounds: `rowsWithin` is label-set inclusion (`callables.go:447-449`; recipes at `callables.go:426-431`).
- The machine planner interns rows through the same arena. Nothing in this design changes the arena's representation or the meaning of a RowID, so machine plans are unaffected.

### 1.2 Row parameters and the finite solver

- **Parsing.** Row parameters are parsed as `<E: raises, R: uses>` (`syntax.go:1272-1290`), at most eight.
- **Identity.** A row parameter is a declaration-qualified label, `row-parameter:<identity>:<kind>:<name>`, registered in `rowDefinitions` (`callables.go:210-235`).
- **Where they are admitted.** Module functions, service operations and implementation methods (`callables.go:237-245`). User type parameters are refused with EF127 (`semantic.go:2938-2939`). Only bundled declarations may be generic in a type.
- **The solver,** `inferRows` (`callables.go:303-357`). For each recipe or callback parameter (`rowCarrier`, `callables.go:365-367`), it splits the formal row into one variable and fixed labels, then binds `variable := actual \ fixed` (`callables.go:333-345`). The binding is a set difference, so a fixed label beside a variable is already removed at the call site.
- **Today's refusals:**
  - more than one variable per row (`:327`);
  - subtracting fixed labels from an abstract actual row (`:337-343`);
  - a variable left unbound (`:351`);
  - a mismatched instantiated argument, reported as EF106 "argument has incompatible instantiated callback contract" (`:355-357`).
- **No presence check.** A fixed label beside a variable is an upper bound: an argument that lacks it is accepted silently (probe A).
- **Inside a body.** The forms refuse a row parameter in their receiver's row with EF125 "… requires an unsupported row difference constraint":
  - catch: `semantic.go:5072-5074`;
  - recover: `recovery.go:21-23`;
  - provide: `semantic.go:5055-5057`;
  - provideLayer: `layers.go:615-617`.

  So today no user function can wrap `.catch` over an abstract row (`recovery_test.go:165`).

### 1.3 The checker cases

Each case computes a row and, separately, ownership and failure evidence.

| Form | Row rule | Evidence rule (not in any signature) |
| --- | --- | --- |
| `.catch<E>(v)` (`semantic.go:5070-5101`) | **Presence:** EF107 "effect does not admit failure E" (`:5081`). **Removal:** `handledRow` (`recovery.go:99-106`). | Joins the recipe's result occurrence with the lifted fallback (`:5091-5095`). Drops E's raised payload evidence (`:5099`). Keeps E in the row when E is pending from an unobserved forked child (`handledRow`, `completeHandled`, `recovery.go:108-118`). Keeps the recipe's observations (`:5093-5095`). Fallback must be a pure value of the result type (`:5083`). |
| `.recover<E>(h)` (`recovery.go:19-97`) | Removes E. Adds the handler's rows when the handler is an effect (`:87-91`). | Invokes the handler inside the failing execution, so `Exec(0)` stays open (`handlerResult`, `recovery.go:163-198`; `freeRecoveredLayer`, `:128-142`). Refuses a handler that dereferences a payload owned by a closed owner (`:60-63`). Refuses a builtin label, EF106 "recover binds a declared error payload; handle builtin failure X with catch" (`:34-35`). |
| `.provide<S>(p)` (`semantic.go:5053-5069`) | `remove(services, S)` (`:5068`). **No presence check:** providing an unused service is accepted. | Unions the provider's captures (`:5067`). The provider must be a provider value of S, EF104 (`:5066`). |
| `.provide(Layer)` (`layers.go:597-659`) | `services \ Provides ∪ Requirements` (`:620-621`). | The program's layer 0 runs under a fresh provision owner that closes at the edge (`:622-634`). Refuses escapes (`:635-651`). Records static plan reasons (`:652-656`). |
| `.timeout(ms)` (`semantic.go:4977-5004`) | `∪ {Timeout}` and `∪ {Scheduler}` (`:5004`). | Closes the timed layer under a fresh timeout owner, `closeOccurrence(…, closedOwner(ownershipOwnerTimeout, …))` (`:5000-5003`). |
| `.orFail()` (`semantic.go:4865-4879`) | `∪ {GoError}` on an Effect returning `GoResult`. | None beyond pass-through. |

**Pending evidence** (design §5.3) is a fact about the evidence, not the type.

- A recipe whose layer 0 forks a child it never joins may raise that child's failure when the *executor's* owner closes. That happens outside the recipe, so a handler around the recipe cannot see it.
- The label stays in the row: `handledRow` keeps it.
- Today the evidence ends at every type boundary. `refuseCarriedPending` (`recipes.go:44`) refuses such a recipe as a call argument, provider configuration or failure payload, with EF107 "recipe may leave forked child failures unobserved (…)". `requireObservedChildren` (`semantic.go:4290-4300`) refuses a function with pending children used as a callable value.
- This rule is tested (`fork_observation_test.go:194`).

**Observations.** A `join` records the fork instance it observes (`t.observes = [fork]`, `semantic.go:5523`), and `run` turns a recipe's observations into kills (`semantic.go:5038`). An ordinary function edge sets no observations from its callee, so a join passed through a function is not seen by the caller (review probe j1).

### 1.4 TypeScript projection today

- **Failure labels** project as follows (`jsRowsContract`, `emit.go:62-98`):
  - an error with fields becomes `<E>Error`;
  - any other error becomes `{ readonly _tag: "<E>" }`;
  - an empty row becomes `never`.
- **Services** project to the branded `<S>Requirement` interfaces. Each required service also has a `Context.Service<<S>Requirement, <S>Provider>` tag (`emit.go:242-261`).
- **Row parameters** (`jsRowFunctionSignature`, `callables.go:612-735`):
  - each occurrence gets its own variable, `__ef_row_<i>_<E> extends { readonly _tag: string }`;
  - fixed labels beside the variable already project as `Exclude<__ef_row_i_E, fixed…>` in the result (`callables.go:677-700`).

  So the projection already speaks `Exclude`. It lacks presence and label witnesses.
- **No test exercises the `Exclude` branch.** Its only fixture is refused today (`recovery_test.go:165`).
- **Bundled functions get no `.d.mts` and no exported JS.** Only current-module declarations are exported and declared (`emit.go:314-316`).
- **The strict consumer harness** is `checkStrictTypeScript` (`data_test.go:490-534`). It runs `tsc --noEmit --strict --module NodeNext …`.

### 1.5 Emission today

Emission is keyed on the expression kind.

**JS** (`emit.go:606-735`):

| Form | Emitted JS |
| --- | --- |
| catch | `__ef_catch(left, "E", () => (v))` (`:700-701`) |
| recover | `__ef_recover(left, "E", …)` (`:702-709`) |
| provide | `Effect.provideService(left, __ef_service_S, p)` (`:696-697`) |
| provideLayer | `__ef_layer_L(left)` (`:698-699`) |
| timeout | `__ef_timeout(left, ms)` (`:630-631`) |

- The helpers are prelude chunks (`js_prelude.go:43-91`). `prelude/catch.mjs:1` is `(program, tag, fallback) => __ef_recover(program, tag, () => Effect.sync(fallback))`. `__ef_recover` handles only a solitary `Fail` whose `_tag` equals the tag (`prelude/recover.mjs:4-10`).
- orFail has no JS lowering: programs that import Go are Go-only (`emit.go:165-167`).

**Go** (`emit_go.go:1255-1426`):

| Form | Emitted Go |
| --- | --- |
| catch | `efCatch(left, "E", func() A { <fallback statements> return right })` (`:1397-1402`) |
| recover | `efRecover(tP, "E", func(efPayload efType_E) efEffect[A] {…})` (`recovery.go:213-233`); a pure handler is wrapped in a succeed adapter there |
| provide | `efProvide_S(t, p)` (`:1388-1393`), which assigns one `efContext` field (`:379-380`) |
| provideLayer | `efLayer_L(left)` (`layer_emit.go:11-75`) |
| timeout | `efTimeout(t, ms)` (`:1329-1334`) |
| orFail | an inline `er.OrFail` closure (`:1272-1275`) |

- The helper bodies are at `emit_go.go:330-341`. `efCatch` takes `fallback func()A` (`:331`).
- Ordinary call arguments are evaluated before the call, with their statements written ahead of it (`emit_go.go:1361` and following). Only catch's fallback is deferred.
- At run time, Go matches failures by tag string (`runtime/effra/effect.go:13-16,282-315`). `Recover` passes a zero payload when the failure carries none (`runtime/effra/effect.go:302-314`).
- Payload-free failures carry a nil payload, so a Go type parameter alone cannot identify a label. Both targets need the tag string.
- The scheduler-driven test providers are constructed by provider name (`emit_go.go:1307-1312`; the provider bodies at `:350` and `:352`). This is audit 1-H and is not changed here.

**Generic and row parameters:**

- Row parameters erase on both targets.
- Bundled type parameters become Go generics `[T any]`, with no monomorphization (`emit_go.go:815-816`).
- JS erases both kinds.

**Reachability** (`reachability.go:852-864`):

- catch, recover and orFail retain their helper (`:852-853`);
- timeout also retains `Scheduler` (`:854-857`);
- provide retains the provider's service (`:858-861`);
- provideLayer retains the plan.

### 1.6 Name and kind sites (the phase-2 inventory)

These are the sites the acceptance grep must clear for each form that becomes library:

- `syntax.go:1641-1676`: the parser forms.
- `semantic.go`:
  - `4865`: orFail;
  - `4977`: timeout;
  - `5045`: provideLayer;
  - `5053`: provide;
  - `5070`: catch;
  - `5102`: recover.
- `recovery.go`: the whole file.
- `layers.go:597`.
- `emit.go`:
  - `630`: timeout;
  - `696-709`: provide, provideLayer, catch and recover.
- `emit_go.go`:
  - `331-334`: the helpers;
  - `1272`: orFail;
  - `1329`: timeout;
  - `1388-1402`: provide, provideLayer and catch.
- `js_prelude.go:51-53,84-85`.
- `reachability.go:852-864`.
- `graph.go`:
  - `204`: provide;
  - `214`: provideLayer;
  - `474`: `providerOrigin` looks through provide, catch and recover.
- `lint_facts.go:309-311` and `:364`.
- `resolution.go:196-202`.
- `interface_summary.go:209` (the `timeout` owner kind).
- The builtin-label exceptions F2 retires: `recovery.go:34-35` and `semantic.go:3023` (`parameterTypeKnown`).

`absent_syntax.go:80-82,203` (help text for an absent `try`/`catch`) and `semantic.go:2876` (TypeScript reserved words) are unrelated uses of the word. `ProvisionDirect = "provide"` (`lint/facts.go:219`) is a wire enum value, not a branch, and the acceptance grep exempts it.

## 2. Design

### 2.1 Row variables

No new representation. Row variables stay what `functionRows` makes them: declaration-qualified labels in an ordinary interned set.

The design adds no Rémy presence variables, no scoped duplicate labels and no `Lacks` constraint. F3 needs no absence fact: an extension pattern checks that its fixed labels are present and binds the variable to `actual \ fixed`, and a binding that over-approximates is sound because rows are upper bounds. Absence is never inferred (§2.3).

### 2.2 Label parameters and tag witnesses

**Feature F2, the label parameter.** It is declared as `E: error` or `S: service` beside the existing `raises` and `uses` kinds.

- **One label.** A label parameter denotes exactly one nominal label. It is bound only by an explicit type argument (§2.5), never inferred, so it never widens. An argument that is not a single declared failure or service name, for example a row `{ A, B }` or a row parameter, is refused: "label parameter E takes one failure name".
- **Positions.** It can stand:
  - in a row, as a label: `{ E, F }`;
  - as a type. An `error` parameter is its payload type, as in `effect fn(E) -> A`. A `service` parameter is used in `Provider<S>`, the provider value type that already exists as the `provider:S` type node (`emit.go:32-34`). `Provider<S>` gains a source spelling with F2.
- **Builtin labels.** A predeclared failure (`IoError`, `Timeout`, `GoError`, `AssertionFailed`; `builtins.go:53`) is declared with no fields (`semantic.go:2352-2355`). As a type it denotes that empty payload. So `fn(Timeout) -> A` is an ordinary handler type: the exceptions at `recovery.go:34-35` and `semantic.go:3023` go, and no builtin-name exception remains in the generic checker. Go's `Recover` already passes a zero payload for a nil one (§1.5).
- **Identity.** `label-parameter:<identity>:<kind>:<name>`, interned like a row parameter.

**Erasure.** A label is matched by its tag at run time (§1.5), so an `error` parameter is reified as a hidden **tag witness**:

- An `error` parameter's witness is the label's tag string. A `service` parameter's witness is the context accessor: a `func(efContext, efService_S) efContext` on Go and the `Context.Service` value on JS.
- **Placement.** Witnesses are ordinary leading parameters placed directly after the subject (the receiver, or the first parameter of a non-receiver function), in label-parameter order, by the same rule on Go, JS and in the `.d.mts`. For a bundled intrinsic lowered at the call site (§2.9), the literal lands exactly where today's emitter writes `"E"`.
- The payload type erases into a Go type parameter, as `efRecover[A, P any]` already does (`emit_go.go:332`).
- `Provider<S>` erases to the provider value (`efService_S` on Go, the provider object on JS). Its setter witness is the accessor above.

This passes the "erases into Go" test with no reflection.

### 2.3 Extension patterns, presence and the row difference

**Feature F3, extension patterns.** A parameter row written `{ L1, …, Ln, ρ }`, where ρ is a row variable and each `Li` is a fixed or label-parameter label, is an **extension pattern**:

- Each `Li` must be in the argument's row. Otherwise the call is refused with the label's membership diagnostic (§2.7).
- ρ binds to the argument's row minus the `Li`.
- A signature removes `Li` simply by not writing it in the result: `Effect<A, { E, F }, { R }>` in, `Effect<A, { F }, { R }>` out.

Rows without a variable keep today's upper-bound meaning. This is the standard reading of a row pattern: PureScript's `Cons l a r1 r2`, Rémy's `{l : τ | ρ}`.

**Reach.** Presence applies to recipe and callback parameters alike.

- For a recipe parameter, it is a safety rule: catching a label the recipe never raises is the existing EF107.
- For a callback parameter, a callback that never raises the label is a strictly safer argument. Refusing it is a deliberate refinement for consistency, not a safety rule. Example: `fn guard<E: raises>(task: effect fn(string) -> string raises { Rejected, E }) -> …` refuses a `task` that raises only `{ Denied }`. A signature that wants to accept it writes the row without the fixed label, `raises { E }`, and removes with F3b.
- Probe B found only one checked-in program or test with a fixed label beside a variable in a parameter row: `recovery_test.go:165`, which is refused today and becomes accepted. No currently accepted program is affected.

**Inside a generic body, removing a present label from an abstract row is sound and needs no difference constraint.**

- Removing `L` from a row `{ L, ρ }` that literally contains it yields `{ ρ }`, so the EF125 sites listed in §1.2 go away.
- Suppose ρ's eventual instantiation also contains `L`. Then the result over-approximates the real row, because catch handles every raise of `L`. Rows are upper bounds (§1.1), so over-approximation is sound.
- Removing a label the row does not literally contain is refused by the presence rule, even when ρ might contain it. That is conservative.
- No subtraction from an unknown row is ever needed. That keeps the solver finite and removes the "abstract row subtraction" refusal at `callables.go:337-343`.

**Feature F3b, the row difference.** A result row may write `{ ρ - L }`: ρ with the label `L` removed if present. It is removal without presence.

- **Substitution distributes over the binding.** At a call, ρ is bound to a row that may itself contain the caller's row variables and difference labels, when the caller is generic. Substituting that binding into `ρ - L` removes `L` from its fixed labels and carries the removal into each variable:
  - `({ a1, …, an, ρ' }) - L = ({ a1, …, an } \ { L }) ∪ { ρ' - L }`;
  - then composition applies, so `(ρ' - M) - L = ρ' - { M, L }`;
  - a binding with no variable is concrete, and the result is plain `remove`.

  Plain `remove` on `{ ρ' }` would keep `ρ'` and lose the removal. That is sound, because it over-approximates, but it refuses the provision helper below. Dropping `ρ'` would be unsound. `L` may itself be a caller's label parameter; the difference keeps it as a label until the caller is instantiated. With an abstract `L`, every fixed label of the binding is kept: `{ a1 } \ { L }` cannot assume `L ≠ a1`, so it stays `{ a1 }`, and `ρ'` becomes `ρ' - L`. That over-approximates, which is sound; the label is removed when `L` is instantiated.
- In a generic body, `ρ - L` stays symbolic as one interned label, `row-difference:<ρ>:<L>`. Its rules:
  - `ρ - L ⊆ ρ`. This is the only relation to ρ. In particular `{ ρ }` is not assignable to `{ ρ - L }`.
  - `(ρ - L) - L = ρ - L`, and `(ρ - L) - M = (ρ - M) - L`: differences are kept as a sorted label set per variable.
  - An extension pattern that requires `L` in `{ ρ - L }` is refused, because `L` is not literally present (conservative).
  - There is no absence rule. Round 2 claimed `ρ - L = ρ` when ρ was bound beside `L`; that is unsound, because `inferRows` unions a variable's bindings across every argument (`callables.go:345`), so a pattern listing `L` in one position does not make ρ lack `L`. If absence is ever needed, it must be an explicit constraint checked at every binding contribution and explicit instantiation, as PureScript's `Lacks` is, never derived from one pattern.
- **Positions, by polarity.** A result position is a position of positive polarity in an authored signature. The function's result is positive and each parameter is negative; inside a callable type, the result keeps the polarity of the callable and each parameter flips it. So:
  - admitted: the function's result row, the result row of a callable it returns, and the parameter rows of a callback parameter (the function supplies those values, so they are obligations on its body, like a result);
  - refused, with "a row difference may appear only in a result": a parameter row, and the result row of a callback parameter (negative: the caller supplies that callback, and nothing makes its rows lack `L`).
- **Authored versus instantiated.** The polarity rule applies to signatures as written. An instantiated parameter type may contain a difference label anywhere, because a generic caller's binding put it there: calling `keep<R2: uses>(work: Effect<A, { E }, { R2 }>)` with a `{ R - Clock }` argument instantiates the parameter as `{ R - Clock }`. That is a label in the caller's row, not an authored difference, and it is carried like any label.
- TypeScript: a fixed service projects as `Exclude<R, ClockRequirement>`; a service that is a label parameter uses the gated `Exclude<R, __ef_removed_service<I>>`, which removes nothing unless the brand check holds (§2.8). Neither claims absence.
- **Clients:** `provide` (§2.12), and user provision helpers that write `{ R - S }` in their own signatures, as Effect users write `Exclude<R, X>` (`fn withTestClock<A: type, E: raises, R: uses>(work: Effect<A, { E }, { R }>) -> Effect<A, { E }, { R - Clock }>`). That meets rung 2 under the owner's TypeScript-counterpart reading ("kind of like its counterpart in typescript"): it is the TypeScript feature itself. Round 2's "ignore-style" failure helper is withdrawn: its body would need catch or recover, which require the label to be present.
- **Worked check: `withTestClock`.**

  ```ef
  fn withTestClock<A: type, E: raises, R: uses>(work: Effect<A, { E }, { R }>) -> Effect<A, { E }, { R - Clock }> {
      work.provide<Clock>(testClock)
  }
  ```

  - In the body, provide's `R'` binds to `{ R }`, a row holding the caller's variable `R`, and `S` binds to `Clock`.
  - The result row is `{ R' - S }` with that binding. By distribution it is `{ R - Clock }`, exactly the declared row, so the body checks.
  - A concrete caller with `R = { Clock, Console }` gets `{ Console }` by `remove`.
  - A generic caller with `R = { Clock, R2 }` gets `{ R2 - Clock }`, by distribution again.
  - Without distribution the body's row would be `{ R }`, which is not within `{ R - Clock }` (`ρ - L ⊆ ρ` only), and the helper would be refused.
- **Negative control (provide unit).** The reviewer's counterexample must be refused:

  ```ef
  fn leak<F: raises>(first: Effect<string, { Denied, F }, {}>, raw: Effect<string, { F }, {}>) -> Effect<string, { F - Denied }, {}> {
      raw
  }
  ```

  The body returns `{ F }` where `{ F - Denied }` is declared, and `{ F }` is not within `{ F - Denied }`, so the body is refused with the row mismatch. Its TypeScript projection is refused the same way.

**The unused-provide case (amended Q4).** Providing a service the work does not use is safe: the provider is installed and never read. `examples/causal.ef:47` does it today, and it runs correctly. It is not unsound, so it stays accepted, and the negative control "providing a service the effect doesn't use" is dropped from acceptance item 3. `provide` therefore uses F3b, not F3. `examples/causal.ef` is unchanged, and the name-keyed scheduler injection (§1.5) is not disturbed. Catch keeps presence: catching an absent label is the existing EF107 refusal.

### 2.4 Call-site inference

The algorithm extends `inferRows` in place.

1. Bind label parameters from the explicit type arguments.
2. For each recipe or callback argument and each row pair, as today:
   - substitute the bound labels into the formal row;
   - split it into one variable and fixed labels;
   - **check presence** of each fixed label in the actual row;
   - bind `variable := actual \ fixed`, unioning across arguments as today.
3. Instantiate the result, computing each `ρ - L` by distribution over ρ's binding (§2.3), which is plain `remove` when the binding is concrete.

The procedure is a single pass, linear in the parameters times the labels per row, with one variable per row as today. It is decidable by construction: there is no unification over row variables, no constraint solving and no fixed point. The cost added over today's solver is one membership test per fixed label.

### 2.5 Explicit type arguments

**Feature F1.** A call may supply a prefix of its type arguments, `Effect.catch<NotFound>(work, "x")` or `work.catch<NotFound>("x")`, using Go's rule (`f[T](x)`, prefix lists allowed).

- Label parameters are ordered first and are bound only by explicit arguments (§2.2). The remaining type and row parameters are inferred from the value arguments, as today. This resolves the round-1 contradiction: labels are never inferred; the rest are.
- The parser needs a `name<types>(` lookahead in expression position, because `a < b > (c)` is otherwise a comparison chain. The disambiguation already exists after `.` for the postfix forms (`syntax.go:1661-1674`, `expectGenericOpen`). The C# and TypeScript rule applies: commit to a type argument list when it closes directly before `(`.

**Clients:**

- the library functions;
- `effra/conversions.witness` (`bundled/conversions/witness.ef:1`), today the only generic bundled function. Its `T` and `W` are inferable, so the client is explicit or partial instantiation. The F1 unit adds fixtures for `witness<T, W>(…)` with the full list and with the prefix `witness<T>(…)`.
- F1 is plainly a general Go feature, and generic member calls (§2.10) need it.

### 2.6 The recipe-parameter contract

This replaces round 1's F4. It is one contract for every function, inferred for ordinary bodies and declared for intrinsics. An intrinsic gains nothing beyond what a body with the same contract would get.

#### 2.6.1 The problem

- A function edge summarizes a recipe parameter only by its held captures. What executing it returns "is unresolved inside this function" (`semantic.go:4057-4060`; `opaqueRecipe`, `recipes.go:262`).
- Its result facts are never parameter-relative (`instantiateCheckedFacts`, `semantic.go:1222-1232`); a missing fact becomes `unknown` or `helper` (`instantiateOwnershipFacts`, `semantic.go:1233-1300`).
- So a user wrapper `fn w(work: Effect<…>) -> Effect<…> { work.catch<E>(x) }` loses three things the direct form keeps:
  - the result occurrence and held captures (EF123 downstream);
  - the observations of a join inside the argument (review probe j1, and the round-1 `m3`/`m4` refusals);
  - the pending evidence of an unjoined child, which is refused at the argument instead of kept on the row.

#### 2.6.2 The contract

For each recipe parameter `p` of a function `f`, the summary records, relative to `p`:

1. **Result flow.** Which occurrences of `f`'s result come from executing `p`, with an owner vector that says what `f`'s body wrapped around that execution: nothing (in place), an owner the body closes at the edge (a `scope`, the timeout owner, a provision owner), or a lift into a later layer. This is the existing relation atom (`callbackResultRelation`, `callback_ownership.go:28`; `relationInvocation`, `:319`) with `p` as the callee. The owner vectors are rewritten by the existing `closeRewrite`, `liftRewrite` and `envRewrite` (`owner_layers.go:106-181,314`), so whatever the body does to `p` is recorded by machinery that exists today.
2. **Held flow.** `p`'s held captures at each layer, as today, plus the layers inside a type-variable success (a `T` that is itself a recipe at the call).
3. **Failure flow.** For each label of a row variable in `p`'s type, the payload evidence of the actual labels it stands for.
4. **Observations.** Whether executing `f`'s result executes `p`, so the fork instances `p`'s argument observes count as observed at the call. For an effect function, running `f`'s body counts too, because the body runs in the call's layer 0.
5. **Pending.** If `f`'s result recipe executes `p` in place, the pending children of `p`'s argument stay pending on the call's result, and their labels stay in the call's row. This is exactly what `handledRow` does in the direct form. Composite-cause companions are rewritten as `handleWith` does: a label the result no longer raises is replaced by what the result raises instead.
6. **Provision.** Which provider is installed for which service around the execution of `p`: "provider `q` provides `S` to the in-place execution of `p`", where `q` is a parameter of `f` or a constant provider (a module-level provider value).
   - **Only what reaches the result.** The clause records a provision only when the recipe that installs it is the recipe that reaches `f`'s result, through clause 1's result flow (for an effect function, also when the body executes it in layer 0). A body that builds `work.provide<S>(q)` and discards it, or returns a different recipe, records no provision. Building a provision expression is a body-local fact: `graph.go:204` already records such expressions whatever the result flow, and that local fact stays local; it is not part of the contract and never becomes an edge at `f`'s call sites.
   - **Branches mean "may provide".** A provision that reaches the result in some branches only is recorded as "may provide", as the other clauses treat branch-local facts.
   - **Substitution.** At a call, a parameter `q` is replaced by the actual provider binding of its argument; a constant provider stays itself.
   - A body that ignores its provider argument records no provision, whatever its signature says.
   - **Staging.** Unit 4a infers the clause for constant providers, because the provide construct today requires a static provider node (`semantic.go:5054-5066`), so a wrapper can only provide a constant. Providers derived from a parameter arrive with `Provider<S>` in unit 6 (§7).

The contract values are inferred, not declared, for ordinary bodies. "Executes `p` in place" is clause 1 with an identity owner vector; "executes `p` under an owner closed at the edge" is clause 1 with a closed owner. Neither names a construct.

**The in-place test is the owner vector.** The prototype approximated "in place" by "`param:p` reaches the result's observations" (§3). Production decides it from clause 1's owner vector alone, and unit 4a's acceptance carries negative controls for work that is run under `scope`, run under timeout, forked and not joined, never run, run twice, and run in one branch only. Each must get the contract value its owner vector says (closed, closed, not in place, absent, in place with both occurrences joined, joined with the absent branch) and the same verdict as the direct form.

**What is shown.** The in-place half is evidenced by probe D. The closed-at-the-edge half is not yet shown: the scope-bodied timeout model scores 0 of 29 (§3). It is a named follow-up of unit 4a; catch, provide and recover do not need it.

#### 2.6.3 Inference

The checker checks each body once, as today, with each recipe parameter bound to a **deferred execution** instead of an opaque recipe:

- its result occurrence is a relation atom over `p` (a whole-value atom at the root path when the success type is a type variable);
- its held layers are the parameter's held captures plus a tail atom for the layers inside a type-variable success;
- each row-variable label carries a payload atom;
- its observation set is the token `param:p`.

Whatever the body does to the deferred execution (run it, close it under a scope, lift it, join it, pass it on) is recorded in the atoms' owner vectors by the existing rewrites. The function's summary keeps the atoms that reach its result, captures and failures, and records which `param:` tokens reach its result's observations or, for an effect function, its body's kills.

**Whole-value type parameters.** A value of a type-variable type is one whole value: its facts are kept at the root path, not merged into a bounded wildcard (`normalizeFacts`, `mergeKey`, `semantic.go:1330`). Inside a generic body no field layout is needed, which retires EF127 "field layout of an unresolved type parameter is unavailable" on this path (`field_occurrences.go:175`).

**User type parameters.** The go/no-go test needs a user wrapper with the library's signature, and that signature has `A: type`. Today user type parameters are refused (EF127, `semantic.go:2938-2939`). Ruling Q9 (round 2, extended in round 3) admits user type parameters that the body uses only as whole values (passed, returned, joined, carried in a recipe), and keeps EF127 for anything that needs a layout (a field read, a template instantiation). Go generics erase them as `[T any]`, as bundled functions already do (`emit_go.go:815-816`); JS erases them, and `jsRowFunctionSignature` already projects type parameters (`callables.go:612-630`).

**Callable type arguments.** A type parameter, user or bundled, may be instantiated with a callable type, such as `effect fn(string) -> string raises { Broken } uses { Directory }`. The value is carried as a whole value together with its callable contract (its callable evidence and its relation atoms), as a callable local is today. This is required, not optional: `examples/callables-factory.ef:63-68` catches with callable fallbacks (pure, effectful with rows, nested), and once catch is the generic receiver function of §2.12 its `A` is that callable type. Today the bundled path refuses it (`templateDataArgument`, `templates.go:350,955-980`; pinned by `templates_test.go:127-141`), and the probe's relaxed user generics refuse it the same way (EF103, EF106, EF127 in probe D). Unit 4a closes the gap, `callables-factory.ef` is a mandatory gate-1 program, and the `templates_test.go:127` assertion is rewritten to keep EF127 only for layout use.

#### 2.6.4 Instantiation at a call

At a call of `f`, the existing instantiation (`instantiateCallbackFacts`, `callback_ownership.go:492`; `instantiateFailures`, `:484`) substitutes the actual recipe argument for each atom over `p`:

- a result atom becomes the argument's result facts, mapped through the atom's owner vector;
- a held atom becomes the argument's held captures at that layer (or every layer at and inside the tail);
- a payload atom becomes the argument's payload evidence for the labels the row variable stands for;
- the call's observations are the arguments' observations for the parameters the summary lists;
- the in-place arguments' pending keys move to the call's result, and their labels join its row.

**Call-site qualification.** Owners that the callee introduced (a closed scope, timeout or provision owner, a lexical owner) are named by the call site, as a function edge already renumbers fork instances. Two calls of the same wrapper close two owners. Without it, diagnostics deduplicated by owner root (`reportOwnershipWith`, `recipes.go:135`) would merge across call sites.

**Effect bodies keep today's pending rule.** Clause 5 applies where the *result recipe* executes `p` (the recipe's type is what carries the pending label, as `handledRow` does). An effect function whose body runs `p` passes observations (clause 4) but keeps today's argument refusal for pending children, so `ownership_sweep/r1_arg.ef` keeps its diagnostic.

#### 2.6.5 Intrinsics declare the same contract

Ruling Q6, as amended in round 3, fixes the form:

- **A reference body where one exists.** catch's reference body is recover with a constant handler: `work.recover<E>(const(fallback))`, where the handler returns the captured `fallback`. Its contract is inferred from that body exactly as for user code, and the compiler only chooses its backend. The body works in both gate-2 outcomes: over the library recover if gate 2 passes, over the recover construct if it fails.

  **One precondition is missing today.** The constant handler captures `fallback`, and Effra admits named functions as values but not closures: "closures require explicit capture checking before admission" (`docs/specs/language-abstractions.md:124`). So the reference body can be written only once closures, or another way to bind a value into a callable, are admitted. Until then catch takes the second form below, with exactly the contract its reference body will infer, and the wrapper-parity test compares the two when the body becomes writable. This note does not propose admitting closures; that is a separate design.
- **Otherwise a declared contract in the inferred vocabulary.** catch until closures exist, and recover and provide permanently for now, have no reference body: Effra has no operation that inspects a failed execution's cause (`Exit` is not exposed), and the provide construct requires a static provider node whose name matches the service (`semantic.go:5054-5066`), so `provider: Provider<S>` cannot be passed to it. Writing `work.recover<E>(h)` inside `effra/effect` would resolve to the function itself. These two primitives declare their contract using only what inference produces:
  - relation atoms with owner vectors (clause 1 and 2);
  - payload atoms (clause 3);
  - `param:` observation tokens (clause 4);
  - pending transfer (clause 5);
  - the provision clause (clause 6);
  - for recover, the callback clauses of §2.6.6.

  The declaration claims nothing that vocabulary cannot express; there is no free-form trusted summary. It is carried in the bundled interface-summary transport (`interface_summary.go`, schema 5 at `:15`; admitted through `interface_admission.go:21` and following), which unit 4a extends (§7). The trust boundary, "a primitive's declared contract", is written into the audit's D4 text and the GLOSSARY by unit 4c (§7), before the first primitive declares a contract.
- **Why this is ordinary library under D4, once amended.** D4 says today that a library intrinsic's "signature is the only source of semantic facts" (`docs/research/compiler-known-constructs-audit.md:214`), and the GLOSSARY entry "Library intrinsic" says the same (`GLOSSARY.md:74`). As written, a declared contract would make catch, provide and recover class 1. Unit 4c amends both: the signature, together with a contract written only in the inferred contract vocabulary, is the only source of semantic facts, provided that the declared contract is consumed by the same instantiation path as an inferred one, and that no checker, emitter or reachability branch is keyed on the item's identity beyond D4's backend selection. Under that text the three primitives are class 2, not lang items.
- **What pins a declared contract.** Three tests, which prove different things:
  - **The ownership matrix (correctness).** It runs the primitive on Go and JS and holds every ownership verdict to runtime truth (`docs/specs/language-abstractions.md`).
  - **Corpus parity against the construct (correctness).** Before a construct is deleted, the library form is checked on the whole corpus against the construct it replaces: identical verdicts, diagnostics and spans, allowing only the listed anchor moves (§2.7). Units 5, 6 and 7 each run it for their construct.
  - **The wrapper-parity test (composition only).** A user function with the same signature, whose body calls the primitive, infers the identical summary and gives identical verdicts, diagnostics and spans on the corpus. This proves that a declared contract composes through the inference exactly like an inferred one. It does not prove the declared contract is correct: the wrapper's summary is derived from the primitive's own declaration, so a wrong declaration would pass it.

So the intrinsic gains nothing that a body with the same contract would not get, and it selects only its backend (§2.9). A lower primitive pair (a fold over a failed execution's cause, as Effect's `matchCause`, and a local-context operation, as Effect's `provideContext`) would later let recover and provide have reference bodies too; that is the long-term route recorded in §2.13.

The contracts of the three library functions:

| Function | Recipe parameter `work` | Callback parameter |
| --- | --- | --- |
| `catch` (declared now; reference body once closures exist) | executed in place; result joins the fallback value; `E`'s raised payload dropped; observations and pending kept | none (its reference body passes a constant handler to recover) |
| `provide` | executed in place; captures union the provider's; observations and pending kept; provision: `provider` provides `S` to `work` | none |
| `recover` | executed in place; `E`'s raised payload dropped; observations and pending kept | `handler` invoked on a solitary `E` failure of `work`, with `work`'s layer 0 still open, given `E`'s payload |

#### 2.6.6 Callback parameters: what is still missing

Callable parameters already have relation atoms; recover's handler needs two more clauses and one coercion, measured in probe D:

1. **Pending through an in-place callback.** A handler with an unjoined child is refused at the argument by `requireObservedChildren` (`semantic.go:4290-4300`), where the direct form keeps the label pending on the result. This affects 6 programs, one of which (`ownership_sweep/i2_handler_declared.ef`) is accepted today.
2. **The payload a callback is given.** The direct form refuses a handler that dereferences a payload owned by a closed owner (`recovery.go:60-63`), and accepts one that does not. Through a wrapper, the payload reaches the handler as an atom over `work`'s failure, and the check fires later as "recipe holds a value owned by a closed scope". 15 programs move their refusal; 2 accepted programs (`ownership_sweep/k2_forward_inside.ef`, `p1_code_only.ef`) are refused.
3. **Mixed handler modes, which need F6.** `examples/recovery-codec.ef` passes an effect handler (`invalid`, line 38) and a pure handler (`explain`, line 45) to recover in one program. Round 2 misfiled it as a callable-`T` case; its success type is `string`. The probe chooses one handler mode per program, so no single wrapper fits it. With the single `recover` signature, it needs F6 (§2.12).

The two clauses are the recipe contract applied to callback parameters ("invoked in place", "given the payload of `p`'s failure `E`"). None names recover. F6 is a general coercion. All three belong to unit 4b, and gate 2 runs the recover wrapper corpus with the single `recover` signature, not with per-program mode variants (§7).

### 2.7 Diagnostics

The rule: same code, same text where the fact is the same, and a better anchor where the library form can give one. Each diagnostic below reaches CLI, MCP and LSP through the shared diagnostics model, as today.

**Anchors.** Today the forms anchor at the method span. The library forms anchor at the offending argument: the receiver (the dot or pipe span, as `pipedArgumentMismatch` already does) or the value argument. LSP ranges move accordingly, and every moved anchor is listed with the commit that moves it.

| Program | Today | Library route |
| --- | --- | --- |
| Catching a label not in the row | EF107 "effect does not admit failure E" at `.catch` (`semantic.go:5081`) | Same code and text from the presence check, anchored at the receiver |
| Unknown failure name | EF102 "unknown failure E" (`semantic.go:5078`) | EF102 from type-argument resolution, same text, anchored at the type argument |
| A non-Effect receiver | EF105 "catch requires an Effect value", "provide requires …", "recover requires …" (`semantic.go:5076`, `:5059`; `recovery.go:30`; `recovery_test.go:162`) | The receiver-method resolution failure: the receiver's type has no method `catch`. The code is chosen in the receiver-method unit; the test at `recovery_test.go:162` changes with it. |
| Wrong fallback or handler type | EF106 "prototype catch fallback must be a pure string" at the fallback (`semantic.go:5083`) | **Must not regress** to the generic EF106 "argument has incompatible instantiated callback contract" at the call (`callables.go:357`). Phase 2 replaces that message for every generic call with "argument `fallback` must be `string`", using the instantiated parameter type at the argument's span. This is a general improvement. |
| A label argument that is not one name | Not expressible | New: "label parameter E takes one failure name" |
| A row difference in a parameter row | Not expressible | New: "a row difference may appear only in a result" (§2.3) |
| A body that returns `{ F }` where `{ F - L }` is declared (`leak<F>`, §2.3) | Not expressible | The existing row mismatch for a result, naming `L` |
| Providing a service the effect doesn't use | Accepted | Accepted (F3b, §2.3). Not a negative control. |
| Wrong provider | EF104 "provider must implement S" (`semantic.go:5066`) | The argument type mismatch against `Provider<S>`. Keep EF104 by mapping provider-typed parameter mismatches to it. |
| Removing from a closed row versus an open row | Closed: EF107 presence. Open: EF125 "recovery of an abstract row requires an unsupported row difference constraint" | Closed: EF107 presence. Open with the label literally present: accepted. Open without it: EF107 presence. The EF125 text disappears. |
| A recipe with a pending child failure | Accepted with the label kept; the consumer reports EF107 "undeclared failures: X" if undeclared | Identical: the contract keeps the label pending (§2.6.2, clause 5). Probe D moves no pending refusal for catch, provide or timeout. |
| Recover on a builtin label | EF106 "recover binds a declared error payload; handle builtin failure X with catch" (`recovery.go:35`) | Accepted: the handler takes the empty payload (§2.2). A listed refused-to-accepted change. |
| Row parameter in a message | Leaks the internal identity, as in `row-parameter:function:module:file:module:twoVars:raises:E` (probe A, p2) | Phase 2 renders the source name `E` everywhere |

The risk named in the spec ("expected row … found row" noise) does not arise: presence failures name the one missing label.

### 2.8 TypeScript projection

**The principle: the projection is fail-safe, not refuse-only.** A declared type removes a label, or narrows a handler's payload, only when the guard's own check holds for the tag or brand type. When the check fails, nothing is removed, and the handler sees the whole row. When it holds, the payload still leaves out a member only when that member's tag is provably a different single plain value.

The refusals are diagnostics. Soundness never depends on TypeScript refusing a call, because TypeScript cannot be made to guarantee a refusal. The fourth review got four refused forms past the r4 guard, each with no cast and no `any`, and each type-checked to a row with labels missing:
- method-signature bivariance;
- a global `interface Object` augmentation;
- a `never[]` rest;
- never-typed filler arguments.

Under this revision each of them either is refused or keeps the whole row, and the runtime outcome matches the row (probe C2 r6, §3).

**The claim, narrowed:** no declared type removes a label unless the guard's check holds for the tag type, or for the brand type of a service, and no handler payload leaves out a member unless its tag is provably a different single plain value. A value that lies about its own type is outside this claim, because no declared type can see it. §2.8.1 lists the three such boundaries.

The intended `.d.mts` for `effra/effect`, revision 6. It is the fourth review's verified repair (`review-4/fix/effect.d.mts`), with the payload replaced by the fifth review's verified repair (`review-5/fix2`), and with comments. Probe C2 r6 checks it with strict `tsc` 6.0.3 against `effect` 4.0.1, together with the runtime module that implements it (§3):

```ts
import type { Context, Effect } from 'effect';
type __ef_tag = { readonly _tag: string };
// Presence, the same guard the projection uses for a fixed label beside a row variable:
// the label must be within the row, or the argument meets an object naming the absent label.
type __ef_present<Row, Label> = [Label] extends [Row] ? unknown : { readonly __ef_absent_label: Label };
// The check: a label parameter holds exactly one literal label. A type whose Record has no
// required key (string, a template literal with a hole, a branded string, never) is not a literal;
// a union is not one label; the unchecked type (0 extends 1 & T) fails first.
type __ef_any<T> = 0 extends 1 & T ? true : false;
type __ef_union<T, U = T> = T extends unknown ? ([U] extends [T] ? false : true) : never;
type __ef_nonliteral<K> = [K] extends [string] ? ({} extends Record<K, 1> ? true : false) : true;
type __ef_label_ok<K> = true extends __ef_any<K> ? false : true extends __ef_nonliteral<K> ? false : true extends __ef_union<K> ? false : true;
// A service identifier is one object with a required brand field, and the brand is one literal.
type __ef_service_ok<I> = true extends __ef_any<I> ? false : true extends __ef_union<I> ? false : [I] extends [{ readonly _effraService: infer N }] ? __ef_label_ok<N> : false;
// Diagnostics only. The intersection gives the argument-site message (and refuses assignment to
// a function type with a union tag); the trailing rest tuple names the rule for an unchecked tag.
type __ef_single<K> = true extends __ef_label_ok<K> ? unknown : { readonly __ef_single_label_required: K };
type __ef_label_guard<K> = true extends __ef_label_ok<K> ? [] : [never];
type __ef_single_service<I> = true extends __ef_service_ok<I> ? unknown : { readonly __ef_single_service_required: I };
type __ef_service_guard<I> = true extends __ef_service_ok<I> ? [] : [never];
// The runtime compares tag values, so removal and payload use the tag's value type V = `${K}`.
// The payload is fail-safe too: a member is left out only when its tag is provably a different
// single value. Per tag constituent T: assignable to V, in (so removal implies payload); not a
// single literal key (string, a template or mapping with a hole, anything branded, a template with
// a branded hole), in; a single literal or enum member, in exactly when its value `${T}` is V.
// never is left out (uninhabited); the unchecked type distributes to both branches, so it is in.
type __ef_may_equal<T, V extends string> = T extends unknown ? ([T] extends [V] ? true : {} extends Record<T & string, 1> ? true : [`${T & string}`] extends [V] ? true : false) : never;
type __ef_payload<F extends __ef_tag, V extends string> = F extends unknown ? (true extends __ef_may_equal<F["_tag"], V> ? F : never) : never;
// Soundness: what a label removes and what the handler receives, gated on the same check.
// Check fails: nothing is removed (never), and the handler receives the whole row F.
type __ef_removed<K> = true extends __ef_label_ok<K> ? { readonly _tag: `${K & string}` } : never;
type __ef_handled<F extends __ef_tag, K> = true extends __ef_label_ok<K> ? __ef_payload<F, `${K & string}`> : F;
// A service is removed by its identifier and its brand together, so an identifier whose brand
// comes only from an augmented Object removes only the requirements carrying that brand.
type __ef_removed_service<I> = true extends __ef_service_ok<I> ? ([I] extends [{ readonly _effraService: infer N }] ? I & { readonly _effraService: N } : never) : never;

// fn (work: Effect<A, {E, F}, {R}>) catch<E: error, A: type, F: raises, R: uses>(fallback: A) -> Effect<A, {F}, {R}>
export declare const catch_: <A, F extends __ef_tag, R, K extends string>(arg_work: Effect.Effect<A, F, R>, tag: K & __ef_present<F["_tag"], K> & __ef_single<K>, arg_fallback: A, ...tag_must_be_one_literal_label: __ef_label_guard<K>) => Effect.Effect<A, Exclude<F, __ef_removed<K>>, R>;

// fn (work: Effect<A, {E, F}, {R}>) recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(handler: effect fn(E) -> A raises {G} uses {S}) -> Effect<A, {F, G}, {R, S}>
export declare const recover: <A, F extends __ef_tag, R, K extends string, G extends __ef_tag, S>(arg_work: Effect.Effect<A, F, R>, tag: K & __ef_present<F["_tag"], K> & __ef_single<K>, arg_handler: (arg0: __ef_handled<F, K>) => Effect.Effect<A, G, S>, ...tag_must_be_one_literal_label: __ef_label_guard<K>) => Effect.Effect<A, Exclude<F, __ef_removed<K>> | G, R | S>;

// fn (work: Effect<A, {E}, {R}>) provide<S: service, A: type, E: raises, R: uses>(provider: Provider<S>) -> Effect<A, {E}, {R - S}>
// Providing a service the effect does not use is accepted (Effect's provideService).
export declare const provide: <A, E extends __ef_tag, R, I, P>(arg_work: Effect.Effect<A, E, R>, service: Context.Service<I, P> & __ef_single_service<I>, arg_provider: P, ...service_must_have_one_literal_brand: __ef_service_guard<I>) => Effect.Effect<A, E, Exclude<R, __ef_removed_service<I>>>;
```

The runtime module beside it, `effra/effect.mjs`, really exports these values:

```js
import { Effect, Exit } from 'effect';
export const recover = (work, tag, handler) => Effect.uninterruptibleMask(restore =>
  Effect.flatMap(Effect.exit(restore(work)), exit => {
    if (Exit.isSuccess(exit)) return exit;
    const cause = exit.cause, reason = cause.reasons[0];
    return cause.reasons.length === 1 && reason._tag === "Fail" && reason.error?._tag === tag
      ? restore(Effect.suspend(() => handler(reason.error))) : Effect.failCause(cause);
  }));
export const catch_ = (work, tag, fallback) => recover(work, tag, () => Effect.succeed(fallback));
export const provide = (work, service, provider) => Effect.provideService(work, service, provider);
```

`recover` has today's prelude semantics (`prelude/recover.mjs`); `catch_` takes an evaluated fallback (B1).

**Projection rules:**

- **Label parameters.** A label parameter becomes its witness parameter, placed after the subject (§2.2), plus one trailing rest tuple:
  - `tag: K & __ef_present<F["_tag"], K> & __ef_single<K>`, with `K extends string`, and `...tag_must_be_one_literal_label: __ef_label_guard<K>`;
  - `service: Context.Service<I, P> & __ef_single_service<I>`, and `...service_must_have_one_literal_brand: __ef_service_guard<I>`.

  Removal and payload go through the gated aliases `__ef_removed`, `__ef_handled` and `__ef_removed_service`, never through `K` or `I` directly.

  This is Effect's `catchTag`/`provideService` shape with four corrections. Each corrects a hole that Effect's own declarations share. Effect 4.0.1 admits a union `K`, a never filler and a `JSON.parse` service with no cast (`effect-baseline.mts`, §3).
  - **One literal label.** Effect admits a union `K` and removes every member, while its runtime compares one tag by equality. Effra's rule is one label, and the TypeScript check for it is literal-only. `__ef_label_ok<K>` holds exactly when `K` is not the unchecked type, is a string whose `Record<K, 1>` has a required key, and is not a union.
    - A string literal gives a required key. So does a single enum member, or a template or string mapping that resolves to one literal.
    - `string`, a template literal with a hole, `string & {}`, a branded string and `never` give an index signature or `{}`. For them `{} extends Record<K, 1>` holds, and the check fails.
  - **The brand field, not the identifier.** For a service, the literal check runs on the identifier's brand field, after a union check on `I` itself. An identifier with no required brand field fails the check.
  - **Removal by identifier and brand together.** A service removes ``I & { readonly _effraService: N }``, where `N` is the brand the check accepted.
    - With a global `interface Object` augmentation, `object` and `{}` acquire a brand through their apparent type and pass the check.
    - Removal then takes only the requirements that carry that brand, which is the key the runtime installs (`augment.mts`).
    - Round 4's `Exclude<R, I>` removed every requirement there.
  - **Fail-safe removal and payload.** The runtime compares tag values. So when the check holds:
    - removal is ``Exclude<F, { readonly _tag: `${K}` }>``;
    - the handler's payload is ``__ef_payload<F, `${K}`>``. It is fail-safe in the same way: a member is left out only when its tag is provably a different single plain value. With `V` = ``${K}``, `__ef_may_equal` decides each constituent `T` of a member's tag:
      - `T` assignable to `V` is included, so every member that removal takes is also handed to the handler;
      - `T` that is not a single literal key is included. That covers `string`, a template or mapping with a hole, anything branded, and a template whose hole is branded. The test is the guard's own `{} extends Record<…>`;
      - a single literal or enum member is included exactly when its value type ``${T}`` is `V`;
      - `never` is left out, which is sound because no honest value has it. A `never`-typed value, for example from `Object.assign`'s `T & U` over two conflicting tags, is boundary 3 (§2.8.1). The unchecked type distributes to both branches, so its member is included.

      Removal being a subset of the payload is not enough. A template whose hole is branded is kept by removal, which is sound, and the runtime still hands it to the handler (A10, §2.8.1).
    - **The precision guarantee.** Effra-generated errors always have plain literal tags, and for them the payload is exact: the members whose tag is `V`. Only a hand-written tag that is not a single plain literal costs precision, by being in payloads it cannot match (§2.8.1).

    When the check fails, removal is `Exclude<F, never>`, which is `F`, and the payload is `F`. A route that gets a refused form past TypeScript therefore only loses precision.

  **The two refusal mechanisms are diagnostics.** Neither carries soundness, so a later simplification can weigh them by their messages alone.
  - The intersection on the witness parameter gives the argument-site message that names the requirement. It also refuses assigning the function to a function type whose tag parameter is a union.
  - The trailing rest tuple names `tag_must_be_one_literal_label` or `service_must_have_one_literal_brand` for an argument of the unchecked type, which absorbs any intersection.
  - A valid call supplies no extra argument, and the runtime functions take exactly their declared parameters, so the rest tuple is invisible to correct callers.
  - Probe C2's `failsafe-only` variant (r5, and r6 with the r6 payload) removes both mechanisms. Every false claim is still refused, by the result type, and only the diagnostics are lost (§3).

  **Presence** is the same `__ef_present` guard the projection already uses for a fixed label beside a row variable (below), so a missing label reads `"Busy" & { readonly __ef_absent_label: "Busy" }`. Bypassing presence is harmless: removing an absent label removes nothing.

  The witness is also why TypeScript callers need no partial type arguments, which TypeScript lacks.

  Effra-emitted errors always have literal tags, but the `.d.mts` is public API, and hand-written Effect errors with `_tag: string` are common.

  The guard and removal aliases are exported from `effra/effect.d.mts`. Every generated declaration that has a label parameter imports them, including user functions such as `recoverAll` and user `{ R - S }` signatures whose `S` is a label parameter. catch's declarations are unit 5, provide's unit 6 and recover's unit 7 (§7).
- **Extension patterns with fixed labels.**
  - The argument row becomes a single variable.
  - Each fixed label adds a presence guard intersected onto the parameter, `__ef_present<Row, Label> = [Label] extends [Row] ? unknown : { readonly __ef_absent_label: Label }`. Intersecting with `unknown` is the identity, so inference is unaffected.
  - The result's ρ becomes `Exclude<variable, fixed…>`. This replaces today's per-occurrence split at `callables.go:677-700` only where a pattern has fixed labels.
- **Row difference.** `{ R - S }` projects as follows:
  - when `S` is a label parameter, it uses the same gated alias, `Exclude<R, __ef_removed_service<I>>`;
  - a fixed service projects as `Exclude<R, ClockRequirement>`, as in `withTestClock`. A generated identifier carries one literal brand, so the check holds by construction.
- **No `any`.** A row the projection cannot express in these terms is refused with a diagnostic (the #71 rule), never widened to `any` or `never`.
- **Checker-only evidence is not projected.** Ownership, observations and pending evidence (§2.6) are facts of Effra's checker; TypeScript has no counterpart, so the projection carries none of them. This is the same for every function and every contract value (§5).

**Where the files live (Q3).** One `.d.mts` per bundled module, here `effra/effect.d.mts`, imported by the generated declarations that mention it, with `effra/effect.mjs` emitted beside it. Effra's own call sites keep lowering at the call site to the prelude helpers (§2.9); the module exists so a TypeScript consumer can import what the declarations promise. This is Gleam's `gleam.mjs` and `gleam.d.mts` pair.

**Divergence from Effect:** Effect's `timeout` adds no service (Effect's `Clock` is a default reference), while Effra charges `Scheduler`. This is the audit's 1-G debt, unchanged by this note.

#### 2.8.1 The guard case table

The guard failed in rounds 2, 3 and 4. Each time it failed on a form, or on a route around refusal, that nobody had listed. Since revision 5, the table's verdicts describe diagnostics. Soundness is the fail-safe rule of §2.8, and the table records it in three places:
- **"Refuse"** means `tsc` reports a diagnostic. Where the reason is the check (`__ef_label_ok` or `__ef_service_ok`), the check fails. So if a route gets the form past TypeScript, nothing is removed and the handler sees the whole row.
- **"Refuse (presence)"** means the check holds but the label is absent from the row. Bypassing it removes nothing, because nothing in the row has that label.
- **The bypass table** lists the routes that get refused forms past TypeScript, with their outcome under revision 5, which revision 6 leaves unchanged.

This table lists every TypeScript type form that can occupy a tag position (`K`) or a brand position: the identifier `I` and its brand `I["_effraService"]`. It also lists the forms of a row member's own tag. That is not a guard position, but removal and payload must stay sound for every one of them.

Every row is a line in probe C2 r6 (`probes-r6/ts/`). The last column holds the case label in `cases.mts`, `indirect.mts`, `forms.mts`, `bypass.mts`, `augment.mts`, `boundary.mts`, `payload.mts` or `payload-run.mts`, or a file and negative number.
- A refusal is a `@ts-expect-error` line. When opened, every directive fires for its stated reason (§3).
- An admission is a plain line, pinned with an exact-type assertion wherever it removes something.
- "New" marks a form that an earlier guard mishandled; the round is named.

**Tag `K`** (catch and recover):

| Form | Example | Verdict | Why | Probe |
| --- | --- | --- | --- | --- |
| String literal | `"NotFound"` | Admit | `Record` has a required key; one member | T01; consumer |
| Union | `"NotFound" \| "Denied"`, also as an explicit type argument with a literal value | Refuse | `__ef_union` | consumer-neg 4, 5; T02b |
| `string` | a `let`-widened tag | Refuse | `Record<string, 1>` is an index signature | consumer-neg 1-3 |
| Template literal with a hole | `` `Not${string}` ``, `` `Id${number}` `` | Refuse | a pattern index signature | extra4 1, 2; T05 |
| Template literal without a hole | `` `Not${"Found"}` `` | Admit | it is the literal `"NotFound"` | T06 |
| `string & {}` | the literal-union idiom | Refuse | an index signature | T07 |
| Branded string | `string & { [brand]: "B" }` | Refuse | an index signature keyed by the intersection | T08 |
| Branded literal | `"NotFound" & { [brand]: "B" }` | Refuse (conservative) | not usable as a property name, so its `Record` is `{}`. It denotes one runtime value, so admitting it would also be sound, but no Effra or Effect tag is branded. | T09; F01a |
| Literal intersected with a supertype | `"NotFound" & string`, `` `${string}` & "NotFound" ``, `"NotFound" & {}` | Admit | they reduce to, or keep the key of, the literal | F01b-d |
| String enum member | `Tag.NotFound` | Admit | an enum literal is a property key. Removal and payload use ``${K}`` = `"NotFound"`, so a plain `"NotFound"` member is removed and handed to the handler, as the runtime does. **New (r4):** round 3 kept that member in the row and gave the handler `never`. | T10 |
| Whole enum | `Tag` | Refuse | a union of its members | T11 |
| Single-member enum | `One` | Admit | one value | T12 |
| `never` | explicit; inferred from a `never` value; `"A" & "B"` | Refuse | `Record<never, 1>` is `{}`. **New (r4):** round 3 admitted a `never` value. | T13, T13b, T21 |
| The unchecked type | a value; an explicit type argument; `JSON.parse(…)` with no cast | Refuse | `__ef_any`. **New (r4):** round 3 removed every failure. Under r5 a bypassed one removes nothing (H2a). | T14, T14b, T14c |
| `unknown` | explicit; a value | Refuse | fails `K extends string`; a value leaves `K` at `string` | T15, T15b |
| Generic tag parameter | `T extends "NotFound"`, or a curried `K` inside a TypeScript generic | Refuse (conservative) | the guard stays deferred and `T` is not assignable to it. A TypeScript wrapper calls with a literal. | T16; F04a |
| Indexed access, `keyof`, mapped or conditional type | `{ a: "NotFound" }["a"]`, `keyof { NotFound: 1 }`, `{ [P in "NotFound"]: P }["NotFound"]`, an `as`-filtered key, `keyof Pick<…>`, `keyof typeof` an `as const` object, a resolved conditional; `(A \| B)["_tag"]`, `keyof { [k: string]: 1 }`, an enum-keyed map, a template key, a number key, a distributive conditional to a union | The verdict of the type it resolves to: the first group admits, the second refuses | evaluated before the guard | T17a-f; F02a-f, F03b-c |
| Deferred conditional | `T extends string ? "NotFound" : "Denied"` with `T` free | Refuse (conservative) | deferred | F03a |
| String mapping | `Uppercase<string>`; `Uppercase<"notfound">` | Refuse; admit | an index signature; the literal `"NOTFOUND"` | T18, T18b |
| `as const` tuple element | `tags[0]` or a destructured element; `tags[i]`, `for … of`, `.map` | Admit; refuse | one literal; a union | F07a-e |
| Number literal, unique symbol | `1`, `typeof brand` | Refuse | not a string | T19, T19b |
| Empty string | `""` | Admit | a literal key | T20 |
| One literal absent from the row | `"Busy"` | Refuse (presence) | presence, `__ef_absent_label` names it | T22; consumer-neg 6 |
| A literal on a row that is a TypeScript generic | `F \| NotFoundError` with `F` free, tag `"NotFound"` | Refuse (conservative, presence) | presence stays deferred over `F`. If admitted, removal over `F`'s instantiation would be sound. **New (r5 table).** | F04d |

**Service identifier `I` and its brand** (provide):

| Form | Example | Verdict | Why | Probe |
| --- | --- | --- | --- | --- |
| Interface with a literal brand | `ConsoleRequirement` | Admit | brand `"Console"`; removes `ConsoleRequirement & { _effraService: "Console" }` | consumer |
| Union of identifiers | `ConsoleRequirement \| ClockRequirement` | Refuse | `__ef_union` on `I` | consumer-neg 10 |
| Union brand | `{ _effraService: "Console" \| "Clock" }` | Refuse | the brand is a union | extra4 4 |
| `string` brand | `{ _effraService: string }` | Refuse | the brand is not a literal | extra4 3 |
| Template-literal, branded-string, unique-symbol or branded-literal brand | `` `Con${string}` ``, `string & Brand`, `typeof sym`, `"Console" & { [sym]: true }` | Refuse | the brand is not a literal | B05, B06; F08a, F08b |
| Enum-member brand | `{ _effraService: SvcTag.Console }` | Admit | one value. It removes only requirements with that enum brand; `Exclude` is structural, so a plain-literal brand is kept, which is sound. | B07 |
| `never` or unchecked brand | `{ _effraService: never }` | Refuse | brand check | B08, B09 |
| No brand field | `{}`, `Omit<ConsoleRequirement, "_effraService">`, `object`, `unknown`, `{ log(): void }`, `Date`, a function type, an array type | Refuse | **New (r4).** With round 3's `Exclude<R, I>`, `Exclude<R, {}>` removed every requirement while the runtime installed one key (`holes.mts`). | B10, B10b, B11, B12, B16; F08d-f; holes |
| Identifier branded only by an augmented `Object` | `declare global { interface Object { readonly _effraService: "Console" } }`, then `Context.Service<object, P>` | Admit | the check holds through the apparent brand. Removal is `object & { _effraService: "Console" }`, so only Console-branded requirements are removed, and Console's key is what the runtime installs. **New (r5):** r4's `Exclude<R, object>` removed every requirement (review `h3-augment.mts`). | H3; augment |
| The unchecked identifier | `Context.Service<any, P>` | Refuse | `__ef_any` on `I`. **New (r4).** | B13 |
| Two identifiers intersected | `ConsoleRequirement & ClockRequirement` | Refuse | reduces to `never` | B14 |
| Optional brand | `Partial<ConsoleRequirement>`, `{ _effraService?: string }` | Refuse | not a required brand. **New (r4):** the optional widened brand removed every requirement. | B15, B15b |
| `Pick` of the brand | `Pick<ConsoleRequirement, "_effraService">` | Admit | the same identifier | B17 |
| Subtype of an identifier | `ConsoleRequirement & { extra: 1 }` | Admit | it removes only requirements assignable to it (none here) | B18 |
| Identifier intersected with a widened brand | `ConsoleRequirement & { _effraService: string }` | Admit | the brand is `"Console"`; removes Console only | F08c |
| Generic identifier | `I extends ConsoleRequirement` | Refuse (conservative) | deferred | B19 |
| Mutable brand field | `{ _effraService: "Console" }` | Admit | `readonly` does not affect assignability | B20 |

**A row member's own tag** (not a guard position):
- When the check holds, removal removes the members whose tag type lies within `K`'s one value.
- The payload holds every member whose tag is not provably a different single plain value (§2.8). Every removed member is in it, apart from a `never` tag, which no value has.
- When the check fails, neither applies: nothing is removed and the payload is the whole row.

| Member tag | Removed by `"NotFound"` | In the payload | Probe |
| --- | --- | --- | --- |
| Literal `"NotFound"` / `"Denied"` | yes / no | yes / no | T01 |
| `string` | no | yes. **New (r4):** round 3 omitted it, and `holes.mts` printed a `TypeError` from a handler typed for `NotFoundError`. | M02, M02b; holes |
| Template with a hole, or a mapping of `string`: `` `Not${string}` ``, `Lowercase<string>`, `` `${number}` `` | no | yes (conservative), for every label. **Changed (r6):** r5 included it only when its pattern could match, which a branded hole defeated (A10). Two precision pins flip: M03's `recover(tmpl, "Denied")` and F09's `recover(lowerRow, "Denied")` now also hand over the template member, which cannot be `"Denied"` but is not a single plain literal. | M03; F09 |
| Union `"NotFound" \| "Other"`; `` `${boolean}` `` | no | per constituent: yes for the first (one constituent is the label); no for `` `${boolean}` `` (`"true" \| "false"`, two other plain literals). **New (r4)** for the union. | M04, M04b; P39 |
| Enum `Tag.NotFound`, or `` `${Tag.NotFound}` `` | yes (an enum literal is assignable to its value) | yes: it is assignable to the label | M05, M05b; F09. A plain literal on a row whose only match is enum-tagged is refused by presence (conservative), M05c |
| Branded literal | yes | yes: a branded tag is not a single literal key, so it is included whatever its value. Refused by presence when no plain member matches (conservative). | M06, M06b |
| Branded enum member `Tag.NotFound & B` | yes | yes. **New (r6):** r5's overlap test left it out while catch removed it, and a handler typed for the plain member died with `TypeError` (A7). | P06; payload-run A7 |
| Template with a branded hole: `` `Not${"Found" & B}` ``, `` `${Tag.NotFound & B}` `` | no (sound: the type is not within the label) | yes. **New (r6):** r5 left both out, and the runtime handed them to a handler typed for the plain member, which died (A10). | P12, P14; payload-run A10a, A10b |
| Branded string | no | yes | M07 |
| `never` | yes | no | no honest value has it, so either is sound. A `never`-typed value is boundary 3 |
| The unchecked type | yes, by every tag | yes | M08: boundary 1, below |
| Requirement brand `string` or a union (provide) | no | – | M09 |

**The payload form table** (revision 6). The fifth review found two kinds of tag that r5's payload left out while the runtime handed them to the handler. So the payload is attacked here as the guard was: every TypeScript form that a hand-written member tag can take, with the label `"NotFound"`. Every row is pinned in `payload.mts` (P01-P39, type-check only). The reproduced holes run in `payload-run.mts` (A7, A10a, A10b), where a handler typed for the plain member alone is a refused claim and a handler written for the payload runs. "Precision cost" marks a member that cannot be `"NotFound"` at run time and is still included, because TypeScript cannot prove it distinct.

| Form | Example | In the payload for `"NotFound"` | Why | Probe |
| --- | --- | --- | --- | --- |
| Plain literal, the label | `"NotFound"` | in | assignable to the label | NF; Gen1 |
| Plain literal, another value | `"Denied"`, `""` | out | a different single plain value | P01, P38; Gen2 (exact) |
| Branded literal | `"NotFound" & B` | in | not a single literal key | P02 |
| Branded literal, another value | `"Denied" & B` | in (precision cost) | not a single literal key: the brand hides the value from the literal test | P03 |
| Literal with `{}` | `"NotFound" & {}` | in | reduces to the literal | P30 (A11 X7) |
| Enum member | `Tag.NotFound` | in | assignable to the label | P04 |
| Another enum member | `Tag.Denied` | out | its value type ``${T}`` is a different single value | P05 |
| Branded enum member | `Tag.NotFound & B` | in. **New (r6)** (A7) | not a single literal key | P06; payload-run A7 |
| Branded enum member, another value | `Tag.Denied & B` | in (precision cost) | not a single literal key | P07 |
| Whole enum; branded whole enum | `Tag`; `Tag & B` | in; in. **New (r6)** for the branded one (A8 M2) | it distributes: `Tag.NotFound` is assignable; the branded members are not literal keys | P08, P09 |
| Template, plain hole | `` `Not${"Found"}` ``; `` `Not${"Here"}` `` | in; out | it resolves to `"NotFound"`; to the plain `"NotHere"` | P10; P11 |
| Template, enum hole | `` `Not${Part.Found}` `` | in | it resolves to `"NotFound"` | P13 (A11 X3) |
| Template, branded hole | `` `Not${"Found" & B}` ``; `` `${Tag.NotFound & B}` `` | in; in. **New (r6)** (A10) | not a single literal key | P12, P14; payload-run A10a, A10b |
| Template with `{}`, branded or nested holes | `` `${Tag.NotFound & {}}` ``, `` `Not${"Found" & {}}` ``, `` `${Part.Not}${Part.Found & O}` ``, `` `${`Not${"Found" & O}`}` ``, `` `${"Not" & O}Found` `` | in. **New (r6):** r5 left 4 of the review's 8 A11 forms out | each either resolves to `"NotFound"` or is not a single literal key | P31-P35 (A11 X1, X2, X4, X6, X8); `originals/a11` |
| Template, `string` hole | `` `Not${string}` ``; `` `Den${string}` `` | in; in (precision cost) | a pattern, not a literal key | P15; P16 |
| Number and boolean patterns | `` `${number}` ``; `` `${boolean}` `` | in (precision cost); out | a pattern; the union `"true" \| "false"` of plain literals, neither the label | P27; P39 |
| Intrinsic on a literal | `Capitalize<"notFound">`; `Uppercase<"notfound">` | in; out | it resolves to `"NotFound"`; to the plain `"NOTFOUND"` | P17; P18 |
| Intrinsic on `string` | `Capitalize<string>`; `Lowercase<string>` | in; in (precision cost) | a pattern | P20; P19 |
| Intrinsic on a branded literal | `Capitalize<"notFound" & O>` | in | not a single literal key | P21 (A11 X5) |
| Branded template or mapping | `` `Not${string}` & B ``; `Lowercase<string> & B` | in; in (precision cost) | not a single literal key | P36; P37 (A8 M4, M5) |
| Union | `"NotFound" \| "Other"`; `"Denied" \| "Other"`; `("NotFound" \| "Other") & B` | in; out; in | per constituent: one is the label; each is another plain value; branded constituents | P22; P23; P24 (A8 M3) |
| `string`, `string & {}` | `string` | in | not a literal key | P25, P26 |
| `never` | `never` | out | no honest value has it. A `never`-typed value, for example from `Object.assign`'s `T & U` over two conflicting tags, is boundary 3. Removal takes it by assignability: the one member removed and not in the payload, which is sound | P28 |
| The unchecked type | `ReturnType<typeof JSON.parse>` | in | it distributes to both branches | P29 |

- **Self-attack.** `tools/gen-payload.cjs` composes 15 atoms (plain literals, enum members, the whole enum, `string` and `` `${number}` ``) with the operators a hand-written tag can use: a brand, `& {}`, three template wrappings, a union and the four intrinsic mappings, to depth 2. That gives 1065 forms. For each, it models the possible run-time strings, and it checks with the TypeScript API that every form whose value may be `"NotFound"` is in the payload.
  - Under r6, 236 forms may equal the label, and all 236 are in the payload. Of the 829 that cannot, 619 are left out and 210 are kept in (the precision cost).
  - Every form left out is a finite set of plain or enum values that contains no `"NotFound"` and carries no brand.
  - Under the r5 payload, 79 of the 236 are left out, so the attack reports r5 unsound.
- **Removal is a subset of the payload**, apart from a `never` tag (P28). `payload.mts` pins both facts.
- **The precision guarantee.** Generated Effra tags are plain literals, and the payload is exact for them: `recover(…, "Denied")` on a generated row hands over exactly the Denied member (Gen1-Gen3). The cost falls only on hand-written tags that TypeScript cannot prove distinct.

**Indirect use, and the routes that get past refusal** (bypass, H, rows are revision 5; the r4 guard admitted each H claim):

| Route | Verdict under r5 | Runtime outcome | Probe |
| --- | --- | --- | --- |
| `.call`, `.apply`, `.bind` | Refuse, even with a literal (conservative): `strictBindCallApply` erases `K` to its bound | – | I01-I04, I06, I07 |
| Assigning to a function type with a union tag or a two-service identifier | Refuse (the intersection) | – | I05, I09, I10 |
| Spreading a tuple with a union tag | Refuse (the rest tuple) | – | I08 |
| Assigning to a function type with one literal tag | Admit; claiming the sibling removed is refused | – | I11, I12 |
| A lifted copy (higher-order generic inference) | Keeps the guard: the signature stays generic | – | I13, I13b |
| Assigning to an overload set of single-literal signatures | Refuse (conservative): the target provides too few arguments for the rest tuple. **New (r5 table).** | – | F05b |
| `satisfies` a function type whose tag is a union | Refuse; the satisfied object keeps the generic signature and refuses a union at the call | – | F06, F06b |
| A function type whose tag parameter the consumer typed as the unchecked type | The claim is refused: the check fails, so nothing is removed. A slot whose result is the whole row is admitted. **New (r5):** r4 admitted the claim as a boundary. | – | I14, I14b |
| **Method-signature slot** (bivariant parameters), catch or recover, with a union tag | The claim is refused; an honest slot is admitted and removes nothing (fail-safe). **New (r5):** r4 removed every failure (review `h1b-method.mts`). | `FAILED Fail(Denied)`, which the type keeps | H1a, H1b |
| **A `never[]` rest**, catch or provide | Fail-safe: the claim is refused and the honest type removes nothing. **New (r5):** r4's provide removed every requirement (review `h2-never.mts` d). | catch: `FAILED Fail(Denied)`. provide: no argument reaches it, so it dies with `TypeError`. That is a defect from TypeScript's rest-arity unsoundness (boundary 3), not a removed label: the type keeps both requirements. | H2b, H2d |
| **A never-typed filler** for the rest slot: a `JSON.parse` tag or service, or an explicit union `K` or two-service `I` with `none[0]` | Fail-safe: the claim is refused and the call removes nothing. **New (r5):** r4 removed every failure or requirement (review `h2b-never.mts`, `h5-explicit-never.mts`). | catch and recover: `FAILED Fail(Denied)`. provide with a `JSON.parse` service: runs once both services are provided. provide with `none[0]` as the service: dies with `TypeError` (a value lie, boundary 3) | H2a, H2c, H5a-c |
| **Object augmentation** | See the brand table: removes only the augmented brand | runs once Clock is provided | H3 |

**What the check accepts:** exactly the single string-literal types. That includes a single enum member, and a template or mapping that resolves to one literal. Only those types remove anything or narrow a payload.

**The claim.** No declared type removes a label unless the check holds for the declared tag type, or for the brand type of a service. No handler payload leaves out a member unless its tag is provably a different single plain value. Refusal is a diagnostic. Probe C2 r6 shows every review-4 route refused or fail-safe, with its runtime outcome, and every review-5 payload form included (§3).

**The host trust envelope: three boundaries.** In each, a value's own type is false, so no declaration can see it. Effect's `catchTag` and `provideService` share all three (`boundary.mts`).
1. **The consumer's own unchecked type on a row member.** A member typed `{ _tag: any }` is removed by every tag (M08, E1). The false claim is the consumer's annotation. Guarding it would need a helper in every generated removal, including in user functions.
2. **A hand-made service whose identifier type and key disagree.** For example, `Context.Service<ClockRequirement, P>("effra/probe/Console")`: providing it removes Clock while the runtime installs Console's key, and the run prints `Service not found: effra/probe/Clock` (review `h4-key.mts`, E2). Generated services derive brand and key from one service name (`internal/compiler/emit.go:254`, `:259-260`), so only services the consumer builds by hand are affected.
3. **TypeScript's own unsound rules: value-type lies and rest arity.** TypeScript accepts these for every function, with no Effra declaration involved.
   - **Value-type lies.** Each of these puts a value of another type into a typed slot:
     - a literal-typed slot holding another value, such as an empty `"Denied"[]` element or a `never[]` element (review `h6-value-lie.mts`, E3a, E3b). The check holds for the declared type `"Denied"`, the type removes Denied, and the runtime keeps it;
     - a `never`-typed value from TypeScript's own signatures, with no cast. `Object.assign({}, denied, found)` returns `T & U`, so two conflicting tags give `_tag: never`. A member built from it is removed by every label and left out of every payload. At run time, recover's handler dies with `TypeError`, and catch fails with the value's real tag (review `review-6/attack/b3-assign-never.mts`). This is E3b's class, and Effect's `ExcludeTag` shares it. **New (review 6).**
     - control-flow narrowing across a mutating call. After `let t: "NotFound" | "Denied" = "NotFound"; flip()`, where `flip` assigns `"Denied"`, TypeScript keeps `t` narrowed to `"NotFound"` (TypeScript #9998). So `catch_(work, t, …)` is typed as removing NotFound, and at run time it fails with NotFound (review `a3-narrowing.mts`). **New (r6).**
     - mutable aliasing of a readonly `_tag`;
     - calls through `Function`, `Reflect.apply` or `.apply.call`;
     - a never-typed filler as the service value, `none[0]` (H5c).
   - **Rest-arity unsoundness.** A function assigned to a type whose remaining parameters are a `never[]` rest can be called with no arguments. `const viaRest: (...rest: never[]) => number = (s: string) => s.length; viaRest()` type-checks and throws (review `a9-rest-baseline.mts`). H2d is this rule applied to provide: its type keeps both requirements, which is honest, and the `TypeError` is a defect, not a removed label. **Changed (r6):** r5 listed H2d as a value lie.

   These are TypeScript's own boundaries, the same for every function with a literal-typed parameter or a required parameter.

Everything else in the declarations contains no `any`. The guard positions (`K`, `I` and the brand) fail the check on the unchecked type, including one that arrives with no cast from `JSON.parse`.

**Scope.** The table covers tag and brand forms and the routes around refusal. Which causes the runtime treats as a solitary failure is the runtime contract of §1.5 and `prelude/recover.mjs`, and this note leaves it unchanged.

### 2.9 Go and JS lowering, and the strict fallback (B1)

**Call-site lowering.** The library functions are bundled intrinsics, lowered at the call site to today's helpers: `efCatch`, `efRecover`, `efProvide_S` on Go; `__ef_catch`, `__ef_recover`, `Effect.provideService` on JS. Emission keyed on the intrinsic is a backend fact, which audit D4 permits for class 2. It is not a semantic branch. Today's bundled intrinsics are bodies of emitted wrapper functions, and `admitBundledIntrinsic` accepts one parameter only (`i64_text.go:33-47`); both change, to n parameters and call-site lowering for intrinsics that return recipes.

**The fallback is evaluated strictly** (ruling B1), like every argument (Go's rule; Gleam `result.unwrap`; ReScript `getWithDefault`). The one systematic emission difference:

| Target | Today | Library |
| --- | --- | --- |
| Go call site | `efCatch(left, "E", func() A {\n<stmts>return right\n})` | `efCatch(left, "E", right)`, with `<stmts>` written ahead of the call like any argument's |
| Go helper | `fallback func()A`, passed to `er.Catch` | `fallback A`, passed as `func() A { return fallback }` |
| JS call site | `__ef_catch(left, "E", () => (right))` | `__ef_catch(left, "E", right)` |
| JS helper | `__ef_recover(program, tag, () => Effect.sync(fallback))` | `__ef_recover(program, tag, () => Effect.succeed(fallback))` |

**Behaviour.** A fallback is a pure value of the result type (`semantic.go:5083`), and division by zero is unrepresentable (`divisor.go:14`). The only observable difference is a fallback that does not terminate: today it is never evaluated unless the failure is caught; under B1 it is evaluated once, when the recipe is built. Review probe `lazy2.ef` (`ok().catch<Boom>(spin(0))`) prints `ok` today and spins under B1. Laziness stays available through `recover`, whose handler is a named function: with `fn late(e: Boom) -> string { spin(0) }`, write `ok().recover<Boom>(late)` (Effra admits named functions as values, not closures; `docs/specs/language-abstractions.md:124`). A by-name parameter feature is rejected (§4).

**Every affected file.** Every catch site changes its bytes for that one reason. Every fallback in the examples and conformance programs is total and pure (literals, string concatenations, record constructors, function values and `void`), so no program's output changes.

| File | Lines with `.catch<` |
| --- | --- |
| `examples/callables-factory.ef` | 4 |
| `examples/callables-service.ef` | 2 |
| `examples/causal.ef` | 1 |
| `examples/checkout.ef` | 3 |
| `examples/codec-transforms.ef` | 6 |
| `examples/codecs.ef` | 2 |
| `examples/concurrency.ef` | 2 |
| `examples/host-types.ef` | 1 |
| `examples/http.ef` | 2 |
| `examples/imports.ef` | 2 |
| `examples/layers-workflow.ef` | 2 |
| `examples/lifecycle.ef` | 1 |
| `examples/lintpack/testdata/forbidden_failure.ef` | 5 |
| `examples/main.ef` | 2 |
| `examples/recipes-queue.ef` | 1 |
| `examples/recipes-retry.ef` | 1 |
| `examples/recovery-outcome.ef` | 1 |
| `examples/testing.ef` | 1 |
| `examples/workflow.ef` | 6 |
| `conformance/size/fixtures/managed.ef` | 2 |

That is 45 lines in 19 example files and 2 in one conformance fixture. Beyond those:

- At `f900240c`, `.catch<` also appears in 161 lines of 128 compiler testdata programs, 147 lines of 41 `internal/compiler` Go test files, and 9 lines of 6 `cmd/ef` tests (including `readme_smoke_test.go:207` and `lsp_smoke_test.go:549`). None compares emitted bytes for a catch except the JS helper tests in `layer_causal_test.go:34-192`, which call `__ef_catch` with a lazy fallback (a recovery counter and a fallback that throws). The catch unit moves those assertions to `__ef_recover` with a handler and adds a strict-value case.
- The size receipts (`conformance/size/receipts/`) record a hash of the emitted `managed` program; the catch unit re-records them and lists the change.

### 2.10 Spelling: receiver methods

**The rule (M2).** A dot call resolves through the receiver-method rule (GLOSSARY: "a function declared with an explicit receiver in its type's owner module … `u.display()` is exactly `User.display(u)`"). The parser routes no names.

- **Owner module.** The language names `effra/effect` as the owner module of the predeclared `Effect` type (ruling Q1).
- **Declarations.** `effra/effect` declares receiver functions (§2.12): `fn (work: Effect<A, { E, F }, { R }>) catch<…>(fallback: A) -> …`.
- **Resolution.** `x.f<T…>(…)` is a generic member call (F1 after `.`). The resolver takes the type of `x`, finds its owner module, and looks up a receiver function named `f` there. `work.catch<NotFound>("x")` is exactly `Effect.catch<NotFound>(work, "x")`, and the pipe spelling `work |> Effect.catch<NotFound>("x")` is the same call.
- **Fields and unrelated types.** Only the receiver's type decides. A user record with a field named `catch`, or a user type with its own `catch` receiver method, is unaffected; a test pins both. Within one type, a field and a receiver method may not share a name (Go's rule).
- **No migration.** Source spelling, formatter output and the roughly 700 occurrences in more than 300 files stay as they are.
- The resolution is parser and resolver work. It adds no checker, emitter or reachability branch.

Receiver methods are admitted but not implemented (`docs/specs/language-abstractions.md:105`). `Effect` is a reserved predeclared language type (audit S11b); naming its owner module is the one language decision this needs.

### 2.11 Graph, lint and reachability facts (M3)

These facts derive from signatures and the contract, never from the identity of an intrinsic (audit D2).

- **Provision edges** (`graph.go:204`, `lint_facts.go:309-311,364`). A call provides `S` exactly when its contract's provision clause (§2.6.2, clause 6) says that an argument is installed as the provider of `S` around an in-place recipe argument. The provider identity is the binding of that argument at the call. Signature shape alone never yields an edge: `fn fake<S: service, …>(work: Effect<A, { E }, { R }>, p: Provider<S>) -> Effect<A, { E }, { R - S }>` that ignores `p` and returns a constant recipe is legal (rows are upper bounds), and it records no provision. A user helper that does provide, through any signature, gets the edge from its inferred clause. The provide primitive declares the clause (§2.6.5).
- **Provider origin** (`graph.go:474`, today a look-through on provide, catch and recover). The origin of a call's result is the origin of each recipe argument the contract says the result executes in place (§2.6.2, clause 1). It covers any in-place wrapper, not three kinds.
- **Reachability** (`reachability.go:852-861`).
  - Helper retention is a backend fact of the intrinsic (`RequiresHelper`, `reachability.go:848-849`), as D4 allows.
  - The provider's service is retained because the provider value is an ordinary argument value, as every value is.
  - Timeout keeps its `Scheduler` retention, because timeout stays a construct.
- `ProvisionDirect` (`lint/facts.go:219`) stays as a wire enum value.

### 2.12 The library declarations

```ef
// effra/effect, the owner module of Effect.
// The body is an intrinsic whose declared contract (§2.6.5) is exactly what
// its reference body, work.recover<E>(const(fallback)), will infer once
// closures are admitted: work runs in place and, on a solitary typed
// failure E, the result is the fallback.
fn (work: Effect<A, { E, F }, { R }>) catch<E: error, A: type, F: raises, R: uses>(fallback: A) -> Effect<A, { F }, { R }> {
    "effra:intrinsic effect.catch"
}

// The one named recovery callback (language-abstractions.md:147). A pure
// handler fn(E) -> A is coerced to an effect handler with empty rows (F6).
// A primitive: its contract is declared in the inferred vocabulary.
fn (work: Effect<A, { E, F }, { R }>) recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(handler: effect fn(E) -> A raises { G } uses { S }) -> Effect<A, { F, G }, { R, S }> {
    "effra:intrinsic effect.recover"
}

// Removal without presence (F3b): providing an unused service is accepted.
// A primitive: its declared contract includes the provision clause
// "provider provides S to work" (§2.6.2, clause 6).
fn (work: Effect<A, { E }, { R }>) provide<S: service, A: type, E: raises, R: uses>(provider: Provider<S>) -> Effect<A, { E }, { R - S }> {
    "effra:intrinsic effect.provide"
}
```

**One `recover` (ruling Q2).** `docs/specs/language-abstractions.md:147` already specifies one `recover<E>(handler)` whose handler is "a pure `fn(E) -> T` or an `effect fn(E) -> T raises {F} uses {S}`", and `catch<E>(value)` as the constant fallback. A single library signature needs:

- **Feature F6, the pure-to-effect coercion on direct call arguments** (ruling Q4/F6). It is a coercion, not a subtyping rule, because it changes representation (a value result becomes a recipe):
  - **Where.** Only a direct argument of a function or method call whose parameter type is an effect callable `effect fn(P) -> T raises { G } uses { S }`. Constructor fields, provider configuration, stored values, nested callable slots (a callable inside a record or another callable's parameter or result) and returns keep today's rule, and the recursive `assignable` (`callables.go:399-440`, including "Pure/effect callable modes are never implicitly lifted" at `:401`) is unchanged.
  - **What.** A pure callable `fn(P) -> T` is accepted there; `G` and `S` bind to empty rows; the argument is wrapped in the succeed adapter that recover's Go emission already uses for pure handlers (`recovery.go:221-231`), and in `Effect.succeed` on JS.
  - **Clients:** recover, and any user combinator with an effect-callback parameter that callers may serve with a pure function (a retry predicate, a map over a recipe). Kotlin's SAM conversion and Scala's eta-expansion are the same kind of call-site coercion.
  - **TypeScript.** The projection describes the adapted ABI: the parameter is a function returning `Effect.Effect<A, G, S>`, and a TypeScript caller writes `Effect.succeed` itself.
- F6 belongs to unit 4b, before gate 2, because gate 2's single `recover` signature cannot pass `recovery-codec.ef` without it (§2.6.6).

Recover on a builtin label becomes ordinary (§2.2).

**A user-written removal** (acceptance item 4) checks through the same rules, with no bundled identity involved:

```ef
fn recoverAll<E: raises, R: uses>(work: Effect<string, { NotFound, Denied, E }, { R }>) -> Effect<string, { E }, { R }> {
    work.catch<NotFound>("not found").catch<Denied>("denied")
}
```

Its contract is inferred (§2.6.3): `work` is executed in place, so observations and pending children pass through exactly as with the direct form.

### 2.13 The residue: `.timeout`, `.provide(Layer)`, `.orFail`

Under the contract, "executes the parameter under a fresh owner closed at the edge" is an ordinary inferred contract value (§2.6.2, clause 1). So neither timeout nor layer provision is residue because of its owner. Round 1 recorded the owner as the reason, and that was wrong.

**`.timeout(ms)`: the first-completion race (control).**

- Its rows are ordinary addition: `Effect<A, { E }, { R }> -> Effect<A, { E, Timeout }, { R, Scheduler }>`.
- Its owner is a contract value.
- What no type feature carries is the race: the timed work and a timer run concurrently, the first to complete decides the result, and the losing work is abandoned and interrupted (`semantic.go:4993-5004`; `TimeoutWithEffect`, `runtime/effra/effect.go:376`; `prelude/timeout.mjs`). That is control (rung 4).
- **The route by which it could later become library:** a general first-completion construct, a Go-`select`-like race over fibers. `timeout` would then be ordinary `.ef` over `scope`, `fork` and that construct, with an inferred contract. That is a separate design. The `Scheduler` charge stays audit 1-G either way.
- The closed-at-the-edge owner of such a body is not yet shown to infer (§2.6.2), which is a second reason this route is future work.

**The long-term route for recover and provide.** Both are primitives with declared contracts (§2.6.5). A lower general pair would let them have reference bodies: a fold over a failed execution's cause (Effect's `matchCause`, over `Exit`) and a local-context operation (Effect's `provideContext`, Koka's handler). Those primitives would need declared contracts of their own, so the pair moves the trust boundary rather than removing it; it is recorded here, not proposed.

**`.provide(Layer)`: the static layer plan (tooling reads the structure).**

- Its owner is a contract value, and its rows are `services \ Provides ∪ Requirements`.
- What no type feature carries is the static plan: graph (`graph.go:214`), lint (`lint_facts.go:311-319`) and inspection (`layers.go:652-656`) read the plan's identity and structure. That is rung 4. A first-class `Layer<Out, Err, In>` value would carry the rows, not the plan.

**`.orFail()`.**

- Its rows are addition: `Effect<GoResult<A>, { E }, { R }> -> Effect<A, { E, GoError }, { R }>`.
- It becomes library once `GoResult` is ordinary generic data. Its input type is predeclared host-interop shape today (audit S5/S8).
- It is Go-only, with no JS lowering.
- It stays syntax until then, per audit D1's existing `.orFail` clause.

These are the facts the phase-3 D1 note records.

## 3. Feasibility probes

I built every probe outside the repository, on scratch branches from `f900240c` that were never pushed. The host load average was 20 to 86 throughout, set by other sessions, so no timing below is a measurement.

The round-2 probe compiler carries:

- round 1's relaxations: the EF125 abstract-row refusal dropped when the label is literally present; the EF127 user-generic refusal dropped; an optional presence check in `inferRows`;
- the contract prototype (§2.6), about 300 lines in one new file plus small hooks in `semantic.go`, `callback_ownership.go`, `field_occurrences.go` and `syntax.go`, behind an environment switch, so the same binary compiles the baseline (today's checker) and the contract.

### Probe A: rows through an ordinary function, today's compiler

**Pass-through and addition** check as written:

- `fn keep<E: raises, R: uses>(work: Effect<string, { E }, { R }>) -> Effect<string, { E }, { R }>`;
- `fn addDenied<…>(…) -> Effect<string, { E, Denied }, { R }>`.

**Removal at the call site already works.**

- `fn dropNotFound<E: raises, R: uses>(work: Effect<string, { NotFound, E }, { R }>) -> Effect<string, { E }, { R }>`, called on a `{ NotFound, Denied }` recipe, binds `E = { Denied }`.
- Its body (`work.catch<NotFound>(…)`) is refused with EF125 (`semantic.go:5073`).

**Presence is not checked.** `dropNotFound` on a recipe without `NotFound` is accepted silently. With the presence check enabled, the same call is refused with EF107.

**Contracts match, evidence does not.** A three-step chain written as wrappers and the same chain as constructs produce the same public contract signature. The wrapper version alone carries an `unknown`/`helper` capture fact.

**Display names leak.** p2: a row-parameter identity appears in an EF106 message.

### Probe B: the corpus through a function edge, without the contract (round 1)

- **Corpus:** `examples/**/*.ef`; `internal/compiler/testdata/ownership_sweep/*.ef`; `ownership_probes/*.ef`; the conformance `.ef` files.
- **Rewrite:** each `.catch<L>(` became `|> wrap_L(` (likewise for timeout and provide). The wrapper body is the construct itself over a row-polymorphic parameter.
- **Comparison:** every program's verdict and diagnostic multiset against the direct construct.

| Wrapper | catch (147) | provide (164) | timeout (29) |
| --- | --- | --- | --- |
| monomorphic `string` | 86 identical | – | 11 |
| type-generic `T: type` | 1 | 4 | 11 |

The monomorphic 86 of 147 was the better predictor of the evidence work: a call site sees a concrete `A`, and the generic 1 of 147 mostly measured EF127 and EF123 inside a generic body. Of the 61 monomorphic differences, 14 were wrapper type mismatches (probe artefacts), 45 moved a pending refusal, about 6 gained EF123, and 2 (`m3_nonrec.ef`, `m4_rec_simple.ef`) were refused because the edge dropped the join's observation.

### Probe D: the corpus through a function edge, with the contract

Same corpus and rewrite as probe B, type-generic wrappers with the library-shaped signature (`T: type, E: raises, R: uses`, one wrapper per label, because label parameters do not exist yet). The baseline is today's checker; the wrapped program is checked with the contract inferred for every function.

| Form | Programs | Identical | Different |
| --- | --- | --- | --- |
| catch | 147 | 145 | 2: `examples/callables-factory.ef`, a **real gap**: `T` is bound to a callable type, which generic instantiation refuses (EF103, EF106, EF127), exactly as the bundled path does today; `examples/host-types.ef`, a rewrite artefact (EF002, a pipe beside `+`) |
| provide | 164 | 162 | 2: `examples/callables-factory.ef`, the same real gap; `examples/callables-state.ef`, a rewrite artefact (EF102, the wrapper names a provider local to a function) |
| timeout | 29 | 29 | none |
| recover | 44 | 20 | 24: 6 pending through a callback, 17 payload given to a callback, 1 mixed handler modes (`recovery-codec.ef`, needs F6); §2.6.6 |

The only listed probe artefacts are `host-types.ef` and `callables-state.ef`, each with its reason. `callables-factory.ef` is accepted today, so a library catch that refuses it would break ruling M1/Q4; it is closed by callable type arguments (§2.6.3) and mandatory in gate 1. Every program counted identical is identical in verdict and in its full sorted multiset of codes and messages. **Spans are not compared:** the metric says nothing about anchors, so gate 1 adds span parity, with spans mapped relative to the call and only the anchor move of §2.7 allowed.

**The in-place test in the prototype is a proxy.** It treats a parameter as executed in place when `param:p` reaches the result's observations. Production uses clause 1's owner vector, with the negative controls of §2.6.2.

**How the catch count grew,** which shows which clause carries what:

| Prototype stage | catch identical |
| --- | --- |
| round 1, generic wrapper, no contract | 1 |
| result, held and failure atoms, plus observations; pending refused at the argument | 102 (the other 43 were already refused; only the text and location moved) |
| plus pending pass-through (clause 5) | 132 |
| plus composite-cause companions | 145 |

Separate checks:

- **m3, m4 (M1).** `ownership_sweep/m3_nonrec.ef` and `m4_rec_simple.ef` are identical through the wrapper and stay accepted.
- **j1.** Review probe `j1.ef` routes `g.join()` through an effect-function wrapper `wrapBoom(w, fallback) { run w.catch<Boom>(fallback) }`. Today it is refused with "undeclared failures: Boom"; with the contract it is accepted, exactly like the direct form `j2.ef`.
- **r1_arg.** An effect function that runs a recipe parameter with a pending child keeps today's argument refusal (§2.6.4).
- **A scope-bodied timeout model** (`effect fn …{ scope { run work } }`) scores 0 of 29: 12 programs are refused inside the wrapper body with "value owned by closing scope cannot escape", and 17 differ. Clause 1 records a body-closed owner, but the prototype does not yet show that a deferred whole value holds no scope-owned handle. So the closed-at-the-edge half of the contract is **not yet shown**. Neither catch, provide nor recover needs it; it is a named follow-up of unit 4a, not part of gate 1.
- **The compiler test suite with the contract on for every function:** see §3, suite run.
- **Timing.** These are not measurements: the host load was 38 to 86, set by other sessions. For the record, the last run took catch 8.1 s direct and 11.2 s wrapped, provide 3.5 s and 3.8 s, timeout 0.5 s and 0.7 s. Each unit's acceptance-5 measurement replaces them.

**Suite run.** The whole compiler package test suite (`go test ./internal/compiler`, including the ownership matrix, the ownership sweep, the emission and conformance tests) ran twice on the probe binary: once with the contract off, once with it inferred for every function in every test program.

- **Contract off: 2 failures,** both caused by the probe patch itself: the evidence-schema audit does not know the probe's new fact field, and `recovery_test.go:165` is accepted (the intended F3 flip).
- **Contract on: the same 2, plus 2:**
  - `TestRecipeBranchEnumMissingEvidenceStaysConservative` (`recipes_test.go:649`). Its verdict assertion still holds: the leaking program is refused with exactly one EF123. Its second assertion inspects the old representation of a recipe-parameter arm (an `Exec` owner fact at `Open.file`), which the contract replaces with a relation atom. The test is rewritten to assert the verdict and the atom.
  - `TestBundledWitnessUnavailableGenericLayoutsDiagnose` (`templates_test.go:127`). It asserts that a user `fn keep<T: type>(value: T) -> T { value }` is refused with EF127. The contract treats `T` as a whole value, so the probe accepts it. This is the user-type-parameter decision of §2.6.3.
- No other test changes: every ownership-matrix verdict, every sweep golden and every emission test agrees with the contract on.

### Probe C: TypeScript projection of the signatures (round 1)

Strict `tsc` 6.0.3 with `effect` 4.0.1 accepted the round-1 declarations and a consumer with six `@ts-expect-error` negatives (exit 0, no `any`). The review added the union-tag counterexample (`union.mts`), which C2 closes.

### Probe C2: the declarations with the runtime module

**The one documented command.** Every C2 run, in rounds 3 to 6 and in unit 5's fixture, uses these flags. Without `--lib`, `effect` 4.0.1's declarations do not type-check.

```sh
cd probes-r6/ts    # effect 4.0.1 resolves through ts/node_modules
node $TSC --noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022 \
  --lib ES2022,DOM,ESNext.Disposable consumer.mts consumer-neg.mts extra4.mts cases.mts indirect.mts \
  forms.mts bypass.mts boundary.mts payload.mts payload-run.mts && node consumer.mts
```

- `$TSC` is TypeScript 6.0.3's `lib/tsc.js`, and node 24 runs `.mts` directly. `payload-run.mts` declares an enum, so node runs it with `--experimental-transform-types`.
- `augment.mts` augments the global `Object`, so it runs as its own program with the same flags.
- `probes-r6/run.sh` runs this command and everything below, and writes `runs/probe-c2-r6.txt`. It exits 0 only when every expectation holds. Rounds 3 to 5 ran the same command without the two payload files, in `probes-r5/` and before.

#### C2 r3 (round 3)

- **Input:** the round-3 `effect.d.mts`, the `effect.mjs` beside it, a consumer that imports and runs them, and a negative consumer with eleven `@ts-expect-error` lines:
  1. a widened `string` tag for catch on a row with a `{ readonly _tag: string }` member (review `extra.mts`);
  2. the same for recover;
  3. a widened `string` tag on a literal row;
  4. a union-typed tag for catch;
  5. a union-typed tag for recover;
  6. catching an absent label;
  7. a fallback of the wrong type;
  8. a handler that does not accept the payload;
  9. a provider of the wrong shape;
  10. a union of two service identifiers, one spelling of a service key that stands for two services (the union-field and widened brands are `extra4` 3 and 4 in r4);
  11. a remaining row that still contains a sibling.
- tsc exit 0; node printed the three expected lines; no `any`. With round 2's guard, cases 1 and 2 were accepted (TS2578).
- The third review found three holes this guard admits: a template-literal tag, a widened service brand and a union-field service brand (review `extra3.mts`, `svc.mts`).

#### C2 r4 (re-plan 1, round 1)

- **Inputs** (`probes-r4/ts/`):
  - the r4 declarations, with removal not yet gated;
  - `consumer.mts`;
  - `consumer-neg.mts` (11);
  - `extra4.mts` (4);
  - `cases.mts`;
  - `indirect.mts`;
  - `holes.mts`.
- **Results:**
  - `tsc` exited 0.
  - There were **66** `@ts-expect-error` directives: 11 + 4 + 39 + 12. Round 4's note said "11 + 4 + 40 + 12", because `grep -c` also counted the header comment of `cases.mts`. The r5 `run.sh` counts only line-leading directives.
  - Every directive fired for its stated reason when opened.
  - There was no `any` in the declarations.
  - Under the round-3 guard, `holes.mts` type-checked and then failed at run time with `payload failed TypeError …` and `identifier failed … Service not found: effra/probe/Clock`. On `cases.mts`, 15 directives went unused and 6 exact-type pins failed.
- **The argument withdrawn in r5.** Round 4 argued that the intersection and the rest tuple were each needed. With the intersection alone, T13b, T14, T14b, T14c and T21 go unused. With the rest tuple alone, I05, I09 and I10 do. That argument rested on refusal carrying soundness. Since r5 both mechanisms are diagnostics.
- **The fourth review** got four refused forms past this guard with no cast and no `any`: method bivariance, `Object` augmentation, a `never[]` rest and never-typed fillers. Each type-checked to a row with labels missing and then failed at run time.

#### C2 r5 (re-plan 1, round 2)

**Inputs** (`probes-r5/`):
- `ts/effect.d.mts`: the §2.8 declarations, which are the review's verified repair with comments. `ts/effect.mjs` is unchanged.
- The r4 fixture: `consumer.mts`, `consumer-neg.mts`, `extra4.mts`, `cases.mts` and `indirect.mts`.
  - I14 is now marked as refused, because nothing is removed.
  - I14b is its fail-safe form.
- The review's `forms.mts`: 19 directives, plus 3 deliberate refusals that are now marked:
  - F04d, a TypeScript generic over the row;
  - F05b, a single-literal overload set;
  - F06, `satisfies` with a union-tag function type.
- `bypass.mts` covers every review route in strict form with no casts: h1b (H1a, H1b), h2b and h2 (H2a to H2d), and h5 (H5a to H5c). For each route it has:
  - the attack's claim, as a refused directive;
  - the same route with no claim, pinned to the whole row;
  - a node run of that value.
- `augment.mts`: h3 (H3) in its own program.
- `boundary.mts`: the trust envelope, admitted on purpose and pinned to the false type. It covers E1 (the consumer's own unchecked member), E2 (h4) and E3a/E3b (h6).
- `r5-original/` holds the review's attack files unchanged, run under r5.
- `r4-guard/` holds the same attack files under the r4 declarations.
- `variants/failsafe-only/` holds the r5 declarations with both refusal mechanisms deleted.
- `effect-baseline.mts` holds Effect 4.0.1's own `catchTag` and `provideService` on the same routes.

**Results** (`probes-r5/runs/probe-c2-r5.txt`; `run.sh` exit 0):
- **Type checks:**
  - The fixture type-checks (`tsc` 6.0.3 strict, exit 0), and `augment.mts` type-checks as its own program (exit 0).
  - There are **99** directives: 11 + 4 + 39 + 13 + 22 + 9 + 1. That is the r4 66, the review's 19, the 4 deliberate refusals marked (F04d, F05b, F06, I14), the 9 bypass claims and the H3 claim.
  - Opened one by one, all 99 have a diagnostic for their stated reason. The 66 r4 directives keep their r4 diagnostics. I14, newly marked, is TS2322, because nothing is removed. Every bypass and augment claim is TS2322: a result type that keeps the row.
  - Every exact-type pin of `cases.mts` and `forms.mts` holds, B18 included.
- **The fixture split by owning unit** (§7), counted by the tool from the functions each statement uses:
  - catch (unit 5): 60;
  - provide (unit 6): 29;
  - recover (unit 7): 10.
- **`consumer.mts`** prints `catch fallback`, `recover recovered missing` and `catch-other failed Denied`.
- **Every route is refused or fail-safe**, with its runtime outcome:

  | Route | Claim | Without the claim (pinned) | Runtime |
  | --- | --- | --- | --- |
  | H1a, H1b: method slot, catch and recover | refused | keeps `NotFound \| Denied` | `FAILED Fail(Denied)` |
  | H2a: `JSON.parse` tag plus never filler | refused | keeps `NotFound \| Denied` | `FAILED Fail(Denied)` |
  | H2b: `never[]` rest, catch | refused | keeps `NotFound \| Denied` | `FAILED Fail(Denied)` |
  | H2c: `JSON.parse` service plus never filler | refused | keeps `Console \| Clock` | `done` once both are provided |
  | H2d: `never[]` rest, provide | refused | keeps `Console \| Clock` | dies with `TypeError … reading 'key'`: no argument reached provide (a defect; r6 names it TypeScript's rest-arity unsoundness, boundary 3) |
  | H5a, H5b: explicit union `K` plus fillers | refused | keeps `NotFound \| Denied` | `FAILED Fail(Denied)` |
  | H5c: explicit two-service `I` plus fillers | refused | keeps `Console \| Clock` | dies with `TypeError`: the service value is `none[0]` (E3) |
  | H3: `Object` augmentation | refused | keeps `Clock` (Console removed) | `done` once Clock is provided |

- **The review's attack files, unchanged:**
  - Under r5, h1b, h2b, h2, h3 and h5 are refused, and every error is TS2322 on a claim.
  - h4 and h6 type-check, and fail at run time as recorded (E2, E3).
  - Under the r4 declarations, every file type-checks and fails at run time as the review reported. The one exception is h2's catch through a `never[]` rest, which r4 already refused.
- **The trust envelope, at run time** (`boundary.mts`):
  - E1: `FAILED Fail(Denied)` with the type showing no failures;
  - E2: `Service not found: effra/probe/Clock` with the type showing no requirements;
  - E3a and E3b: `FAILED Fail(Denied)` with Denied removed from the type.

  All three are outside the claim (§2.8.1).
- **Soundness without refusal** (`variants/failsafe-only/`):
  - With both mechanisms deleted, every bypass and augment claim is still refused, and `holes.mts` is still refused.
  - `direct.mts` pins a union tag, a `string` tag, a template tag, a `JSON.parse` tag, a two-service identifier and an unbranded identifier as admitted, each removing nothing.
  - What is lost are diagnostics: 5 of `consumer-neg.mts` and the 4 of `extra4.mts` go unused.
- **Effect 4.0.1** admits a union `K`, a never filler and a `JSON.parse` service with `tsc` exit 0 (`effect-baseline.mts`).
- **`any`, counted as AST keywords:**
  - 0 in `effect.d.mts`, `effect.mjs`, `consumer.mts`, `consumer-neg.mts`, `extra4.mts`, `forms.mts`, `bypass.mts`, `augment.mts`, `boundary.mts` and `holes.mts`.
  - The only keywords are the deliberate inputs in `cases.mts` (T14, T14b, B13, M08) and `indirect.mts` (I14, I14b).
- **Providing an unused service is accepted** and removes nothing, matching §2.3.

**What the probe does not show.** These are type-level facts about the declarations. Two acceptances belong to the units:
- that every Effra-generated declaration with a label parameter uses these aliases: catch in unit 5, provide in unit 6, recover in unit 7;
- that each unit's fixture is its own share of these directives.

#### C2 r6 (re-plan 1, round 3)

**Inputs** (`probes-r6/`, a copy of `probes-r5/` with these changes):
- `ts/effect.d.mts` and `variants/failsafe-only/effect.d.mts` carry the r6 payload, `__ef_may_equal`. This is the fifth review's `fix2`, and `ts/effect.d.mts` is the §2.8 block.
- `ts/payload.mts` is §2.8.1's payload form table, P01-P39. Each row is pinned in, out, or in at a precision cost. The file also pins removal as a subset of the payload, and the exact payload of a generated row. It is type-check only.
- `ts/payload-run.mts` runs A7 and both A10 holes, with tags built by `Brand.nominal` and a plain generic template function, and no casts. Each handler typed for the plain member alone is a refused claim, 3 directives in all. A handler written for the payload runs.
- The two precision pins that flip are changed in place, each with a comment that says why: M03 (`cases.mts:165`) and F09 (`forms.mts:137`).
- `tools/gen-payload.cjs` is the generated payload self-attack (§2.8.1).
- `tools/open-negatives.cjs`, the unit tagger, now takes the highest unit among the functions a statement uses, as its comment says (o2). No statement uses two of them, so the counts are unchanged.
- `originals/`, renamed from `r5-original/`, adds the review-5 attack files unchanged: a7, a10, a11, a12, a3 and a9.
- `r5-decl/` holds the r5 declarations, with a7, a10, a11 and the self-attack, to show the holes.

**Results** (`probes-r6/runs/probe-c2-r6.txt`; `run.sh` exit 0, `fail=0`):
- **Type checks.** The fixture, now with `payload.mts` and `payload-run.mts`, type-checks (exit 0). `augment.mts` type-checks as its own program (exit 0).
- **102 directives:** 11 + 4 + 39 + 13 + 22 + 9 + 3 + 1. These are r5's 99 and the 3 payload claims.
  - Opened one by one, all 102 have a diagnostic for their stated reason.
  - The 3 new ones are TS2339, for example `Property 'id' does not exist on type 'NF | Legacy'`: the payload includes the branded member.
- **The fixture split by owning unit:** catch (unit 5) 60; provide (unit 6) 29, that is 28 plus the augment program's 1; recover (unit 7) 13.
- **`payload-run.mts`** prints:
  - `A7 catch (type: no failures): fallback`;
  - `A7 recover, payload NF | Legacy: legacy 7`;
  - `A10 branded-literal hole, payload NF | Legacy1: legacy 8`;
  - `A10 branded-enum hole, payload NF | Legacy2: legacy 9`.
- **The payload self-attack.** Under r6: 1065 forms, no diagnostics; all 236 forms that may equal the label are in the payload; of the 829 that cannot, 619 are left out and 210 kept in. Under the r5 declarations it reports 79 diagnostics, only 157 of the 236 in the payload, `UNSOUND`, and exit 1.
- **The review-5 attack files, unchanged:**

  | File | Under r6 | Under r5 (`r5-decl/`) |
  | --- | --- | --- |
  | a7: branded enum member | refused, TS2339 at 21 | type-checks; `FAILED Cause([Die(TypeError: … reading 'length')])` |
  | a10: template with a branded hole, both forms | refused, TS2339 at 22 and 23 | type-checks; `FAILED Cause([Die(TypeError: … reading 'toUpperCase')])` |
  | a11: the 8 hole forms | every pin holds (exit 0) | 4 pins fail (TS2322 at 22) |
  | a12: Effect 4.0.1's own `catchTag` on A10's row | type-checks (Effect's baseline, prior art for the hole); `FAILED Cause([Die(TypeError: … reading 'toUpperCase')])` | – (no Effra declaration) |
  | a3: narrowing across a mutating call | type-checks (boundary 3); `FAILED Cause([Fail({"_tag":"NotFound","id":"x"})])` with NotFound removed from the type | – |
  | a9: rest arity | type-checks (boundary 3); `threw: TypeError: … reading 'length'` | – (no Effra declaration) |

- **Every r5 result holds unchanged.** That covers the review-4 attack files under r6 and under the r4 guard, the trust envelope, `consumer.mts`, and the `failsafe-only` variant with the r6 payload.
- **`any`, counted as AST keywords:** 0 in `effect.d.mts`, `effect.mjs`, `payload.mts` and `payload-run.mts`. The payload table writes the unchecked type as `ReturnType<typeof JSON.parse>`, so it needs no `any` keyword.

**What the probe does not show** is as in r5. Recover's share of the fixture is now 13 directives, which unit 7 takes with the payload table and the self-attack (§7).

## 4. Prior art

Pinned sources were read through okra repo caches or shallow clones. The question for each language is how it handles the three problems this note meets: removal in a library signature, laziness of a library argument, and facts that cross a function edge. For the compile-to-host languages (Gleam, ReScript, Elixir, MoonBit, Koka, PureScript, Scala.js, Effect-TS on TypeScript), the table also names the lowering path.

| Language (target) | Removal and recovery | Laziness of a fallback | Intrinsics and edge facts | Lowering | Bearing on this design |
| --- | --- | --- | --- | --- | --- |
| **Gleam** (Erlang, JS) | `Result` library: `result.try`, `map_error`, `try_recover` (`stdlib@13359e5` `src/gleam/result.gleam:83,143,397`); no rows | Strict: `result.unwrap(r, default)`; explicit `lazy_unwrap(fn() { … })` | `@external` binds a typed signature to a host function and changes neither evaluation nor checking | Library calls are plain calls; the prelude ships as `gleam.mjs` with `gleam.d.mts` | B1 (strict), Q3 and M4: a real runtime module behind the declarations |
| **ReScript** (JS) | Untyped exceptions (`exception.mdx:9-40`); polymorphic variants narrow only to named sets (`polymorphic-variant.mdx:146-180`) | Strict: `getWithDefault`; laziness by an explicit `unit => 'a` | `external` bindings are typed and trusted; no ownership | Externals inline to the host call | B1; an external never supplies deferred evaluation |
| **Elixir** (BEAM) | `try` is a special form (`lib/elixir/lib/kernel/special_forms.ex:2343`); set-theoretic types put no raised rows in signatures (`lib/elixir/lib/module/types/expr.ex:520-557`) | `&&`, `\|\|` and `if` are lazy only as **macros**; `\\` defaults are evaluated per call | No edge summaries | Macros expand before lowering | Laziness needs a construct, so by-name is not ordinary (B1); a fixed form for control fits timeout's race (§2.13) |
| **MoonBit** (Wasm, JS, C) | `raise`, `try`/`catch` as syntax; a catch does not narrow (`next/language/error-handling.md:119-146,222-227`) | Strict | `%` intrinsics behind declared signatures change only the implementation; `with_timeout` is library over async (`moonbitlang/async@e72901a` `src/async.mbt:96-100`) because MoonBit tracks no ownership | Intrinsics lower per backend | D4's reading: an intrinsic swaps the implementation, never the checking or evaluation. Its library timeout shows the timeout residue is Effra's own (race plus ownership checking) |
| **Koka** (C, JS) | `try` is a library function over handler syntax and rows: `handle/try(hndl, action : () -> <exn\|e> a)` (`koka@9c55695` `lib/std/core/exn.kk:54-56`); duplicates allowed; auto-opening (`src/Type/Operations.hs:83-98`) | The action is an explicit thunk | Evidence passing; the compiler still names `exn` (`src/Common/NamePrim.hs:316`) | Evidence-passing translation to C and JS | Borrowed: the extension pattern (F3). Not borrowed: duplicates or auto-opening, which defeat presence |
| **Effect-TS** (TS) | `catchTag`: `K extends Tags<E>`, `ExcludeTag` (`effect@66257d29` `Effect.ts:2812-2858`, `Types.ts:165`); `provideService`: `Exclude<R, I>`, no presence (`:6456-6472`) | `orElseSucceed(f: (e) => A2)`, a callback (`Effect.ts:8366` in 4.0.1) | Tags and `Context.Key` witness values | A library over a fiber runtime | The projection target (F2, F3b). Its holes are closed, not copied: a union, widened or template `K`, and an unbranded service identifier. Removal is gated on the literal check, so the routes around refusal that Effect also admits lose precision only. Effect 4.0.1's `catchTag` is also prior art for a payload hole: `ExtractTag` keeps a member only when `K extends T`, so a member whose tag is branded or interpolated is left out of the handler's type while the runtime's `===` hands it over, and the handler dies (`a12-effect-baseline-hole.mts`, A12). Effra's revision 5 payload had the same hole; revision 6 includes every member not provably distinct (§2.8, §2.8.1) |
| **PureScript** (JS) | `Prim.Row` `Cons`, `Lacks` (`Docs/Prim.hs:510-531`); `Record.delete` needs `Lacks` (`purescript-record@c89cd1a` `src/Record.purs:117-124`); `Run.catch` over higher-kinded rows (`purescript-run@abec7c3` `src/Run/Except.purs:126`) | Strict | Type classes resolved by dictionary passing | Dictionary passing to JS | F3 takes `Cons`; tag witnesses are dictionary passing (F2). `Lacks` is an explicit constraint, never inferred from one pattern: the reason F3b claims no absence (§2.3) |
| **GHC** (native, via STG and Cmm) | Library `catch` over a primop | Lazy by default | Primops declare demand and strictness signatures (`primops.txt.pp`) that the optimiser trusts and tests pin | STG, Cmm | The closest analogue to a primitive's declared contract (§2.6.5): no body in the source language, a declared contract in the vocabulary the analysis infers |
| **Scala** (JVM, JS) / **Swift** / **Kotlin** | Library combinators | By-name `=> A` and `@autoclosure` (`??`, `assert`) are general declared features; Kotlin `inline` takes explicit lambdas | Kotlin `inline` is visible in the signature; Kotlin SAM conversion and Scala eta-expansion are call-site coercions | Scala.js and Swift lower a by-name argument to a thunk at every call | By-name is rejected for Effra (ruling B1): it adds an evaluation rule Go readers do not expect, and `recover` already gives explicit laziness. The call-site coercions are the model for F6 (§2.12) |
| **Rust** (native) | `?` through lang items | Strict; closures | Ownership facts crossing a call are in the signature (lifetimes), the same for std and user code, inferred within a body and declared at the boundary | Monomorphization | The model for §2.6: edge facts are one contract that user and library functions share, never granted to library bodies alone |
| **Links** | Rémy rows with presence variables (`links@7100e28` `core/types.mli:150-154`); handlers are a special form | n/a | n/a | n/a | Presence variables are a second kind with no TypeScript analogue: not borrowed |
| **Unison** | Set abilities; `handle` is a special form; `pruneWanted` subtracts once (`unison@a1652e3a` `Typechecker/Context.hs:2789-2808`) | Delayed computations `'a` are explicit | n/a | n/a | The same set semantics as Effra; Effra adds presence |
| **Roc**, **OCaml** | Open tag unions without narrowing (`roc@3ee70f0c9` `docs/langref/tag-unions.md:50-69`); polymorphic variants narrow only to a named set (`ocaml@6471927d` `polyvariant.etex:62-70,98-137`) | Strict | n/a | Roc monomorphizes | Open unions alone do not give removal: nothing borrowed |

**The route Effra matches:**

- **Unison's set semantics** for rows;
- **PureScript's `Cons` extension pattern** for removal, with no absence fact at all (F3 checks presence only, and F3b claims no absence), and dictionary passing for the tag witness;
- **Effect's `catchTag`/`provideService` shape** as the TypeScript meaning, with removal gated on the literal-only check (fail-safe), a payload that leaves out only members provably distinct (where Effect's `ExtractTag` has a hole, A12), and refusal kept as a diagnostic, as Gleam and ReScript project conservatively (§4.1);
- **Rust's discipline for edge facts:** one contract for user and library functions, inferred in bodies and declared at intrinsic boundaries. No compile-to-host language requires every primitive to have a body in the source language (Gleam `@external`, ReScript and Koka `extern`, MoonBit `%` primitives, GHC primops, Rust intrinsics); all trust a declared signature at the primitive boundary and test it, which is the amended Q6;
- **Gleam's and ReScript's strict library arguments,** with laziness as an explicit function (`recover`).

Like Koka and Elixir, Effra keeps a construct for what is control or tooling structure rather than type: timeout's race and the layer plan. Unlike Koka, removal needs no handler syntax, because the runtime helpers are the handlers.

### 4.1 Literal-tag discipline (the TypeScript guard)

The question here is whether the tag that selects a failure to catch, or a requirement to remove, can be a non-literal type, and what the runtime compares. Sources were read at the pinned commits below.

| Language | What a tag is, and what the runtime compares | Bearing on the guard |
| --- | --- | --- |
| **Effect 4** (`effect-smol@3a1128c7`) | `Tags<E> = E extends { _tag: string } ? E["_tag"] : never` (`packages/effect/src/Types.ts:120`). `ExcludeTag` is `Exclude<E, { readonly _tag: K }>` and `ExtractTag` keeps `E` when `K extends T` (`:154`, `:187`). `catchTag` takes `const K extends Tags<E> \| NonEmptyReadonlyArray<Tags<E>>` (`Effect.ts:2705`, flattened by `K[number]` at `:2715`, `:2725`). At run time an array is matched by `k.includes(e._tag)` and a single tag by `self["_tag"] === tag` (`internal/effect.ts:2861-2863`, `Predicate.ts:1171`). `provideService` returns `Effect<A, E, Exclude<R, I>>` (`Effect.ts:6221-6236`). | Effect's array form is a set of literals, matched by membership. A single union `K`, or a `string` or template `K` drawn from a member typed `_tag: string`, is matched by equality while `ExcludeTag` removes every member it covers. Effra has no array form (one label), so the guard demands one literal, which is the only case where `Exclude` and `===` agree. `ExtractTag` is not the payload's mirror of `ExcludeTag` either: a member whose tag is branded or interpolated, such as `` `Not${"Found" & Brand}` ``, is kept by `ExcludeTag`, left out by `ExtractTag`, and matched by `===` (A12). So Effra's payload does not use `ExtractTag`'s test (§2.8). |
| **TypeScript** (`microsoft/typescript@a1ef42b9e`, the Go port) | `type Exclude<T, U> = T extends U ? never : T` (`tsc/internal/bundled/libs/lib.es5.d.ts:1618`), so removal is assignability. `isTypeUsableAsPropertyName` holds only for string, number and unique-symbol literals (`tsc/internal/checker/utilities.go:922-923`). `isValidIndexKeyType` accepts `string`, `number`, `symbol`, pattern template literals and non-generic intersections such as `string & {}` (`checker.go:20174-20177`, `27033-27037`). A mapped key that is not a literal becomes an index signature (`checker.go:21338-21350`). | This is why `{} extends Record<K, 1>` is the literal test. A literal key gives a required property; every non-literal string form gives an index signature or nothing. |
| **ReScript** (`rescript-lang.org@a62233b`) | Polymorphic variant bounds are sets of literal tags: closed `[#Red]`, lower `[> #Red]`, upper `[< #Red \| #Blue]` (`apps/docs/markdown-pages/docs/manual/polymorphic-variant.mdx:330-342`, `361-391`). A payload-free tag compiles to a string, and a payload tag to `{ NAME, VAL }` (`:256-258`). Coercion to `string` is allowed only for closed types (`:397-420`). | No type lets a tag be "any string", so no guard is needed. Effra's checker is in the same position: a label parameter is bound by an explicit nominal type argument. Only the TypeScript surface, where a host user supplies the tag, needs the guard. |
| **Elixir** (`elixir@7a40579`) | `atom(as)` is a finite union of singleton atoms, and `atom()` is the infinite top (`lib/elixir/lib/module/types/descr.ex:49,98-99`). A literal atom pattern gets `atom([atom])`, while a variable pattern only binds (`pattern.ex:643-644`, `838-842`). `rescue e in …` must expand to literal module atoms, or compilation fails (`elixir_clauses.erl:466-494`, `:603`). At run time, rescue compares `__struct__` with the alias (`elixir_erl_try.erl:119-120`). | It has the same split: only a literal narrows, and the catch form demands literals. The guard is the TypeScript statement of Elixir's `rescue` rule. |
| **Gleam** (`gleam@d799dc7`) | Each constructor is a class on JavaScript (`compiler-core/src/javascript.rs:866-871`), matched by `instanceof` (`javascript/decision.rs:1667-1709`). On Erlang it is an atom, or a tuple headed by one (`erlang/pattern.rs:199-217`). | Tags are declared constructor names that the compiler owns; users never write tag strings, so there is nothing to widen. |
| **MoonBit** (`moonbit-docs@8d9f3ba`) | `suberror` declares error constructors (`next/sources/language/src/error/top.mbt:1-11`), and `catch` matches them by name (`top.mbt:108-114`). Matching on `Error` needs a `_` arm (`next/language/error-handling.md:31-46`). | Catch names a declared constructor; there are no computed tags. |
| **OCaml** (`ocaml@6471927d`) | A polymorphic variant tag is a compile-time hash (`typing/btype.ml:246`, `stdlib/obj.ml:51-57`), lowered to `Const_int(hash)` or a block (`lambda/translcore.ml:321-328`) and matched on that integer (`lambda/matching.ml:2109`). Hash collisions are rejected (`typing/ctype.ml:3756-3760`). | The runtime compares constants known when compiling. |

None of these languages lets a non-literal tag select a failure at the type level. In the ones that compile tags themselves (Gleam, MoonBit, OCaml, ReScript), the compiler owns the tag, so widening cannot happen. Elixir's set-theoretic types and its `rescue` rule draw the literal-versus-top line explicitly. Effect's declarations do not draw it, and Effra's `.d.mts` is the one place where a host user can supply a non-literal tag. So the guard lives there, and Effra's own checker never needs it.

**No compile-to-host language trusts host refusal** (the fourth review's point, revision 5). The table above asks what a tag is. This one asks how each language projects a discipline its own checker owns into a host whose type system is unsound by design. The citations are the fourth review's, checked there against the sources (`round-4-findings.md`, Prior art).

| Language | How it projects to the host | Bearing on the fail-safe rule |
| --- | --- | --- |
| **Gleam** (Erlang, JS) | Generates `.d.mts` (`compiler-core/src/javascript/typescript.rs`). Opaque and private types do not export their constructors (`:443-447`). The declared classes stay structural, though. At `d799dc7`, the opaque `Animal$ = Cat \| Dog` declares `Cat` with a public field and no private marker (`compiler-core/src/javascript/tests/snapshots/gleam_core__javascript__tests__custom_types__opaque_types_typescript.snap`), and `CustomType` has only `withFields` (`compiler-core/templates/prelude.d.mts:2-5`). So a TypeScript user can build a value without a cast, for example `Object.assign(new CustomType(), { goes_outside: true })`. **Corrected (r6):** revision 5 said a value could not be forged. | It hides what it can, and accepts the host's structural typing as a boundary, as §2.8.1's trust envelope does. |
| **ReScript** (JS) | genType exports a type it cannot express faithfully, such as a GADT or an abstract type, as **opaque**: an abstract class with a `protected opaque!` field (`compiler/gentype/emit_type.ml:363-374`, `emit_js.ml:88-97`). It does not export a precise but forgeable type. | A conservative fallback when the host cannot express the fact. Gating `Exclude` on the check is the same rule applied to removal. |
| **Elixir** (BEAM) | No TypeScript projection. `rescue` literals are enforced by Elixir's own compiler (`elixir_clauses.erl:466-494`). | The discipline lives in the language's compiler, never in a host checker. |
| **MoonBit** (Wasm, JS, C) | FFI signatures are trusted declarations that must match the implementation (`next/language/ffi.md:102`). | An explicit foreign trust contract, like §2.8.1's trust envelope. |
| **Koka** (C, JS) | Library `try` removes `exn` by installing a handler (`koka@9c55695`, `lib/std/core/exn.kk:54-56`), lowered by evidence passing. There is no TypeScript surface. | The effect proof lives in Koka's own checker. |
| **PureScript** (JS) | `Record.delete` takes `IsSymbol`, `Cons` and an explicit `Lacks`, and the reflected symbol selects the runtime key (`purescript-record@c89cd1a`, `Record.purs:110-124`). FFI types are assigned at a trusted boundary. | The runtime key is derived from the same symbol as the type. Effra derives a service's brand and key from one name in the same way (`emit.go:254,259-260`), which is why a hand-made mismatch is the only service boundary (E2). |
| **Effect-TS** | Works inside TypeScript's deliberately unsound compatibility rules. Effect 4.0.1 admits a union `K`, a never filler and a `JSON.parse` service with no cast (`effect-baseline.mts`). Its `catchTag` payload leaves out a branded or interpolated member that the runtime hands over (A12). | The r4 guard closed Effect's direct spellings but inherited its bypass routes, because removal stayed unconditional. Gated removal keeps Effect's shape and is strictly stronger. Effect's payload hole is the one Effra's r5 payload shared; r6 over-includes instead. |

Every inspected language that compiles its own tags enforces its label discipline in its own compiler. The projection to the host is either conservative or sits behind an explicit trust contract. The conservative ones are Gleam's hidden constructors, with structural declarations as an accepted boundary, and ReScript's opaque fallback. The trust contracts are MoonBit's and PureScript's FFI. Revisions 5 and 6 follow that path:
- TypeScript's refusal is a diagnostic;
- the declared types claim no removal that the check does not back;
- a handler's payload leaves out no member that the runtime might hand over;
- §2.8.1 writes down the host trust envelope.

**A tag's run-time value against its payload type** (the fifth review's question, revision 6). No inspected language that compiles its own tags lets the two drift apart, under its stated trust contract. Effect-TS, a TypeScript library, does (A12). The others avoid drift in one of two ways:
- **One compiler-owned source.** Gleam matches constructors by class (`javascript/decision.rs:1667-1709`), and users never write tag strings. ReScript genType derives the discriminant and the payload from one variant description. Elixir expands a `rescue` alias to its literal module atom when compiling (`elixir_clauses.erl:466-494`), and compares `__struct__` with that atom at run time (`elixir_erl_try.erl:119-120`), so nominal identity is normalized before any comparison. PureScript's `Record.delete` takes the run-time key from the same `IsSymbol` witness that `Cons` and `Lacks` use (`Record.purs:110-124`).
- **No tag strings.** Koka's `try` selects by operation through a handler (`lib/std/core/exn.kk:54-56`). MoonBit's `catch` matches declared constructors (`next/language/error-handling.md:31-46`).
- TypeScript can do neither for a hand-written, branded `_tag`: it cannot normalize a brand the way Elixir normalizes an alias. So Effra's payload over-includes what it cannot prove distinct, and generated tags, which are plain literals, keep the exact payload. Inclusion uses the guard's own literal test, which echoes ReScript's single source for the tag and the payload.

## 5. Ladder check

The recipe-parameter contract is treated the same way in every row: it is checker-only evidence, so its TypeScript column is "not applicable", and a contract value never decides a rung by itself.

| Candidate feature | Describable without naming the construct, with at least two clients | Readable with Go's simplicity | Same meaning in TypeScript, no `any` | Erases into Go | Rung |
| --- | --- | --- | --- | --- | --- |
| F1 explicit type arguments | Yes: the library functions, `witness`, generic member calls | Yes: Go `f[T](x)` with prefix lists | Yes: a witness where TypeScript cannot take partial lists | Yes: Go generics | 2 |
| F2 label parameters with tag witnesses | Yes: catch, recover, provide, user combinators | Yes: one kind word, `E: error` | Yes: a witness `K` with the presence guard, and removal and payload gated on the literal-only check (§2.8, §2.8.1) | Yes: a hidden string or accessor, the payload as a Go type parameter | 2 |
| F3 extension patterns with presence | Yes: every removal with presence, plus user functions such as `recoverAll` | Yes: "a label beside a row variable must be present" | Yes: `Exclude` plus a presence guard | Yes: rows erase | 2 |
| F3b row difference (result positions only, no absence rule) | Yes: provide and user provision helpers that write `{ R - S }`, as Effect users write `Exclude<R, X>` | Yes: "R without S" | Yes: `Exclude<R, ClockRequirement>` for a fixed service, the gated `Exclude<R, __ef_removed_service<I>>` for a label parameter; neither claims absence | Yes: rows erase | 2, under the owner's TypeScript-counterpart reading |
| F6 pure-to-effect coercion on direct call arguments | Yes: recover, callback-taking user combinators | Yes: a pure function raises nothing | The projection describes the adapted ABI; TypeScript callers write `Effect.succeed` | Yes: the existing succeed adapter | 2 (ruling Q4/F6; a coercion, not subtyping) |
| Provision clause of the contract | Yes: provide, user provision helpers | Yes: inferred | Not applicable: checker and tooling evidence | Not applicable | 2 (part of the contract) |
| Recipe-parameter contract, in place | Yes: catch, provide, recover, every user wrapper | Yes: invisible in source, inferred | Not applicable: checker-only evidence | Not applicable: checker-only | 2 (general checker rule over every function) |
| Recipe-parameter contract, owner closed at the edge | Yes: user `scope` wrappers, timeout's owner, layer provision's owner | Yes: inferred | Not applicable | Not applicable | 2 in design, **not yet shown** (the scope-bodied model scores 0 of 29). It is not why timeout or provideLayer stay constructs. |
| Pending evidence through an in-place parameter | Yes: every in-place wrapper | Yes: "the label stays, as with the direct form" | Not applicable: checker-only, as `handledRow`'s pending label is today | Not applicable | 2 (part of the contract) |
| First-completion race (timeout) | No general construct exists yet | – | – | – | 4: keep the construct |
| Static layer plan (`provide(Layer)`) | Tooling reads it | Yes | Rows only (Effect's `provide(Layer)`) | Not carried | 4: keep the construct |
| By-name parameters | Yes (Scala, Swift) | **No:** an evaluation rule Go readers do not expect | Yes: a thunk | Yes | Rejected (ruling B1) |
| Rémy presence variables or scoped labels | Yes | No: a second kind of row content | No | Yes | Rejected |

## 6. Go/no-go

**GO for phase 2,** as the unit sequence in §7, with two gates.

- **Rows.** GO. F3 alone is useful even if everything after it stops, and probe A shows the solver already does most of the work. F3b is sound only as `ρ - L ⊆ ρ` with the composition rules of §2.3; the deleted equation `ρ - L = ρ` admitted `leak<F>`.
- **The recipe-parameter contract, in place.** GO. It is inferable for ordinary bodies by extending existing machinery: the prototype brings catch, provide and timeout wrappers to identity on every program except two probe artefacts (`host-types.ef`, `callables-state.ef`), keeps `m3`/`m4` accepted, and accepts j1 like the direct form. `callables-factory.ef` is not an artefact: it needs callable type arguments (Q9 extended), so it is a real gap that unit 4a closes.
- **Unit 4a is the go/no-go point. Gate 1:**
  - the catch, provide and timeout wrapper corpus identical to the direct form with the production contract, `callables-factory.ef` included, and only `host-types.ef` and `callables-state.ef` excluded, by name;
  - identical means verdict, codes, messages and spans mapped relative to the call, allowing only the anchor move listed in §2.7 (the prototype compared no spans);
  - `m3`, `m4` and j1 accepted;
  - the compiler suite green with the contract on for every function, `TestEvidenceSchemaIsClassified` included.
- **Callback clauses and F6.** Not yet shown: the prototype matches 20 of 44 recover programs. Unit 4b owns both. **Gate 2:** the recover wrapper corpus identical, by the same measure, under the single receiver signature, `recovery-codec.ef` included, with i2, k2 and p1 accepted. Until F2 lands in unit 5, the wrappers are fixed-label instantiations, one per label, as in probe D. If gate 2 fails, recover stays a construct (D1 for recover alone), and catch and provide still go ahead.
- **If unit 4a fails gate 1,** the recorded outcome is "rows general, evidence not": D1 stands for catch, provide and recover, and F1, F3 and F3b stay for user combinators over abstract rows.
- **The contract, closed at the edge.** Not yet shown: the scope-bodied timeout model scores 0 of 29 (§3). It is a named follow-up of unit 4a, not a gate. catch, provide and recover run their work in place and do not need it, and it is not why timeout or provideLayer stay constructs (§2.13).

**The residue that cannot be general,** with the exact fact for each (§2.13):

1. `.timeout(ms)`: the first-completion race and the abandonment of the losing work (control).
2. `.provide(Layer)`: the static layer plan that graph, lint and inspection read (tooling).
3. `.orFail()`: the predeclared `GoResult` shape, until it is ordinary generic data.

The postfix-word reservation (T1) becomes the ordinary receiver-method rule for `catch`, `provide` and `recover`. It stays a construct reservation for `timeout` and `orFail`.

**Behaviour changes phase 2 brings,** each listed with the commit that causes it:

- **Strict fallback** (B1): every catch site's emitted bytes (§2.9), and a non-terminating fallback now diverges at construction.
- **Anchors** move from the method span to the argument span on CLI, MCP and LSP (§2.7).
- **EF105** becomes the receiver-method resolution failure.
- **Refused programs that become accepted:**
  - `recovery_test.go:165`;
  - recover on a builtin label;
  - a pure function passed directly where an effect callback is expected (F6, unit 4b). It is a coercion through the succeed adapter on direct call arguments only. The recursive `assignable` is unchanged, so a pure callable in a constructor field, provider configuration, stored value, nested callable slot or result is still refused (§2.12);
  - j1-shaped programs, where a join passes through a function edge;
  - user functions with whole-value type parameters, including callable type arguments (Q9 extended, unit 4a). `templates_test.go:127-141` changes from a refusal to an acceptance.
- **New refusals, of new syntax only:** a row difference outside a result position, and an extension pattern that requires a label the difference removed (§2.3, §2.7). Neither is writable today.
- **No accepted program becomes refused.** This holds only with Q9's callable extension in unit 4a: without it, a library `catch` or `provide` wrapper over `callables-factory.ef` would be refused where the construct is accepted. The unused provide stays accepted, and pending children keep today's behaviour.
- **The generic EF106 argument message improves** for every generic call.

## 7. Phase-2 units

The split is the one adopted by the round-3 ruling, with re-plan 1's moves:
- note r4, then F3, then F1 plus receivers, then 4a;
- then 4b and 4c, which can run in parallel;
- then catch, provide and recover.

Each declaration and each negative control this note introduces is listed in the acceptance of the unit that owns it. A control is never listed in a unit that precedes the feature it needs.

Each unit:
- is its own branch and review;
- compiles and passes the gate;
- carries its own acceptance-5 measurement: checker cost on the examples and on the #109 wide-record shape, before and after, at recorded load;
- updates its own docs: GLOSSARY terms, CHANGELOG, and the NORTH_STAR and D1 text the unit makes true.

**Correctness pins for a construct that becomes library** (units 5, 6 and 7, §2.6.5):
- the ownership matrix;
- corpus parity against the construct before it is deleted: verdicts, diagnostics and spans, allowing only the listed anchor moves.

The wrapper-parity test proves composition only.

- **Unit 1. Revised note.** This revision.
- **Unit 2. Rows-presence (F3 alone).**
   - Presence in `inferRows`.
   - The abstract-subtraction refusal removed for literally present labels (`callables.go:337-343`).
   - The EF125 refusals dropped where the label is present (`semantic.go:5055,5072`, `recovery.go:21`, `layers.go:615`).
   - Source-name rendering for row parameters.
   - Tests:
     - `recovery_test.go:165` positive;
     - presence and closed-versus-open negatives;
     - a callback presence example (§2.3);
     - the `Exclude` projection, with the `__ef_present` guard, exercised by a strict consumer.
   - Catch stays a construct.
- **Unit 3. Explicit type arguments and receiver methods.**
   - F1: parser lookahead, formatter and LSP spans, prefix binding, the comparison-chain disambiguation, and the `witness<T, W>`/`witness<T>` fixtures.
   - The receiver-method rule: receiver declarations, owner modules, generic member calls, and `effra/effect` named as the owner module of `Effect`.
   - Tests: an unrelated user type with a `catch` field and one with a `catch` receiver method are unaffected.
- **Unit 4a. Edge contract (the go/no-go point).**
   - **The recipe-parameter contract for every function** (§2.6.2-2.6.4):
     - clauses 1 to 5;
     - deferred executions;
     - observations, including effect bodies;
     - pending pass-through with companions;
     - call-site owner qualification.

     "In place" is decided from clause 1's owner vector, not from the prototype's observation proxy. Negative controls: work run under `scope`, run under timeout, forked and not joined, never run, run twice, and run in one branch only. Each gets the contract value its owner vector says, and the direct form's verdict (§2.6.2).
   - **Q9 extended** (§2.6.3): whole-value type parameters in bundled and user functions, including callable type arguments carried with their callable contract.
     - EF127 stays only where a layout is needed.
     - `templates_test.go:127-141` is rewritten as an acceptance, with a refusal kept for a type parameter whose body needs a layout.
   - **The contract format and summary transport** (§2.6.5):
     - the interface-summary schema bump (`interface_summary.go`, schema 5 today), carrying `param:` observation tokens, in-place and pending transfer, payload atoms and provision;
     - round-trip tests;
     - classification in `TestEvidenceSchemaIsClassified`;
     - a declared contract read through the same instantiation path as an inferred one, exercised by a test-only declared contract (no library primitive declares one yet).
   - **Clause 6 for constant providers** (§2.6.2).
     - Wrappers over the static provide construct get the provision clause, and the graph and lint edges derive from it (§2.11).
     - Controls: a wrapper whose provision reaches its result yields the edge at its call sites; one that builds the provision and discards it yields none; a branch-only provision is "may provide".
   - **Gate 1** as in §6.
   - **Named follow-up, not a gate:** the closed-at-the-edge owner model, measured by the scope-bodied timeout corpus (0 of 29 today).
- **Unit 4b. Callback clauses and F6.**
   - The two callback clauses of §2.6.6: pending through an in-place callback, and the payload a callback is given. `recovery-codec.ef` is treated as mixed handler modes.
   - F6 as a coercion on direct call arguments through the succeed adapter (§2.12).
     - Negative controls: a pure callable in a constructor field, provider configuration, stored value, nested callable slot and result each stays refused.
     - The TypeScript projection describes the adapted ABI.
   - **Gate 2** as in §6. Label parameters (F2) arrive in unit 5, so gate 2's wrappers use fixed-label instantiations, one wrapper per label, as probe D did. Each wrapper keeps the single `recover` handler signature across pure and effect handlers; no per-program mode variants.
- **Unit 4c. D4 and GLOSSARY amendment** (docs only; after 4a, in parallel with 4b, before unit 5).
   - Amend audit D4 (`docs/research/compiler-known-constructs-audit.md:214`) and the GLOSSARY entry "Library intrinsic" (`GLOSSARY.md:74`). The signature, together with a contract written only in the inferred contract vocabulary, is the only source of semantic facts, under two conditions:
     - the declared contract is consumed by the same instantiation path as an inferred one (4a's test shows that);
     - no checker, emitter or reachability branch is keyed on the item's identity beyond backend selection.
   - Reclassify the declared-contract primitives (catch until closures exist, provide and recover) as class 2, ordinary library, under the amended D4. Record the trust boundary "a primitive's declared contract".
   - Say plainly that the wrapper-parity test proves composition. The correctness pins are the ownership matrix and corpus parity against each construct before deletion.
   - Add GLOSSARY entries "Declared contract" and "Recipe-parameter contract". The other new terms ("Label parameter", "Extension pattern", "Row difference") land with the unit that introduces each.
- **Unit 5. Catch library.**
   - F2: label parameters, tag witnesses and their placement, the single-label rule, builtin payloads.
   - `effra/effect` with the receiver `catch` as an intrinsic.
     - **catch's declared contract** (§2.6.5), with its wrapper-parity test. A reference body replaces the declaration once closures are admitted (Q10).
     - Call-site lowering, and n-parameter `admitBundledIntrinsic`.
   - B1 strict emission with the listed difference, and the `layer_causal_test.go` helper tests moved. The B1 inventory is 161 lines in 128 testdata programs, 147 lines in 41 `internal/compiler` tests, and 9 lines in 6 `cmd/ef` tests (§2.9).
   - `effra/effect.mjs`, and `effra/effect.d.mts` declaring `catch_` under the §2.8 rule: presence, the literal check, fail-safe removal through `__ef_removed`, and the two refusal mechanisms as diagnostics.
     - The label aliases are exported, and every generated declaration with a label parameter imports them.
     - Negative controls: **catch's share of probe C2 r6** as a fixture. That is 60 directives; `run.sh` tags each with its owning unit. It includes catch's exact-type pins, its bypass routes (H1a, H2a, H2b, H5a) with their fail-safe pins, and I14/I14b.
     - Provide's and recover's controls are not in this fixture. They land with the declarations they test, in units 6 and 7.
   - Diagnostics: EF105 and the generic EF106 argument message.
   - **Corpus parity against the catch construct**, then the catch parser form, checker case, emitter cases and the `graph.go:474` look-through are deleted.
   - The grep test, and Go and JS equivalence over every example and conformance program, apart from the listed B1 difference.
- **Unit 6. Provide library.**
   - First, a monomorphic provide probe with a provider parameter, modelling the two-argument `Provider<S>` ABI.
   - F3b: `ρ - L ⊆ ρ`, the composition rules and **distribution over abstract bindings**, with no equation and no absence rule. The positions are checked by polarity on authored signatures (§2.3).
     - Positive control: `withTestClock` checks, and its concrete and generic callers get `{ Console }` and `{ R2 - Clock }`.
     - Negative controls: `leak<F>` refused with the result row mismatch; a row difference in a parameter row, or in a callback parameter's result row, refused with "a row difference may appear only in a result"; an extension pattern requiring a removed label refused (§2.3, §2.7).
   - `Provider<S>` source type and witness. The receiver `provide` with **provide's declared contract**, provision clause included.
   - Clause 6 for providers derived from a parameter. Graph, lint and reachability facts come from the provision clause (§2.11), never from signature shape.
     - Negative control: **`fake<S: service, …>(…, p: Provider<S>)`**, which ignores `p` but has provide's signature, yields no provision edge and no lint fact.
   - `provide` added to `effra/effect.d.mts`, with `__ef_service_ok` and removal by identifier and brand (`__ef_removed_service`).
     - Negative controls: **provide's share of probe C2 r6**, 29 directives: B05-B19, `extra4` 3 and 4, consumer-neg 9 and 10, I07, I10, F08, H2c, H2d, H5c, and H3 in its own program.
     - The brand pins: B07, B17, B18 and F08c.
     - The trust-envelope record E2, a hand-made identifier and key that disagree.
     - The user projection of `{ R - S }` with a label-parameter `S` uses the same gated alias (§2.8).
   - **Corpus parity against the provide construct** before it is deleted. `examples/causal.ef` is unchanged.
- **Unit 7. Recover library** (only if gate 2 passed).
   - The single receiver `recover` with **recover's declared contract**, including the callback clauses from 4b, and its wrapper-parity test.
   - `recover` added to `effra/effect.d.mts`, with the gated payload `__ef_handled`.
     - Negative controls: **recover's share of probe C2 r6**, 13 directives: consumer-neg 2, 5 and 8, `extra4` 2, M02b, M04b, I06, I09, H1b and H5b, and the payload claims A7, A10a and A10b (`payload-run.mts`).
     - Recover's payload pins: T10's recover line, M02-M07, F09, and the payload form table (`payload.mts`, P01-P39, with removal as a subset of the payload and the exact payload of generated rows). M03 and F09 are pinned to their r6 values: the template member is in the payload for `"Denied"` too.
     - The payload self-attack, `gen-payload.cjs`, runs in the fixture and must report every may-equal form in the payload.
     - If gate 2 fails, recover stays a construct. `effra/effect.d.mts` then exports no `recover`, and `effect.mjs` keeps it as the internal helper that `catch_` uses.
   - Builtin payloads. `recovery.go` is deleted except for helpers the contract reuses. F6 is already in place from unit 4b.
   - **Corpus parity against the recover construct** before it is deleted.
   - Optionally, catch re-expressed over recover in ordinary Effra once closures are admitted, if its emission is acceptable.

## 8. Cost estimate

Redone for re-plan 1, per unit. The changes since round 3:
- 4a's parts are summed honestly, and callable type arguments are given 1 to 1.5 days;
- the declarations move to the units that own them;
- unit 4c is new;
- unit 5 carries catch's share of the guard fixture, and revision 5 adds about 0.25 day there for fail-safe removal and the review-4 routes;
- unit 6 carries distribution and `fake<S>`;
- unit 7 carries recover's share of the fixture, and revision 6 adds about 0.1 day there for the payload form table and its self-attack.

| Unit | Scope | Estimate |
| --- | --- | --- |
| 2 | F3 | S, 0.5 to 1 day |
| 3 | F1 and receiver methods | M, about 2 days (parser, resolver, formatter, LSP) |
| 4a | Edge contract, Q9 with callables, contract format and transport, clause 6 for constant providers | L, 5.5 to 7 days. The contract and its owner-vector controls are about 3 to 4; callable type arguments 1 to 1.5 (`templateDataArgument` and instantiated callback contracts); the schema bump, round-trip and classification about 1; clause 6 for constant providers about 0.5. Productionizing the prototype and running the full suite under the contract are where the risk sits. |
| 4b | Callback clauses and F6 | M, about 3 days (the clauses about 2, the coercion and its negatives about 1) |
| 4c | D4 and GLOSSARY amendment | S, about 0.25 day (docs) |
| 5 | Catch library | M to L, 3.25 to 3.75 days (F2 on two targets, catch's declaration, B1 emission, the runtime module with fail-safe removal and catch's share of the C2 r6 fixture, corpus parity, equivalence tests) |
| 6 | Provide library | M, 2 to 2.5 days (F3b with distribution, `Provider<S>`, provide's declaration, `fake<S>`, corpus parity) |
| 7 | Recover library | S to M, about 1.6 days, conditional on gate 2 (recover's declaration with the fail-safe payload, its 13 directives, the payload form table and self-attack, corpus parity) |

The total is about 18 to 21 implementor-days plus review rounds; the rows sum to 18.1 to 21.1. The third review's planning range was about 17 to 20. Revision 4 added about one day: half for the guard fixture and distribution, and a quarter each for unit 4c and the honest sum of 4a. Revision 5 adds about 0.25 day inside unit 5, which the fourth review costed. Revision 6 adds about 0.1 day inside unit 7, which the fifth review costed. Earlier estimates were round 3's 17 to 19.5, round 2's 13.5 to 16 and round 1's 8 to 10. The growth is in 4a: a general contract that crosses module boundaries needs the transport, and an honest provision edge needs its own clause.

## 9. Open questions

Settled by ruling, recorded here so the note does not ask again:

- **Q1.** `effra/effect` owns `Effect`, and dot calls go through the receiver-method rule (§2.10).
- **Q3.** One `.d.mts` per bundled module, with a real `.mjs` beside it (§2.8).
- **Q4/F6.** A pure function is admitted where an effect callback is expected, as a coercion on direct call arguments only (§2.12). It narrows "Pure/effect callable modes are never implicitly lifted" (`callables.go:401`) for that one position. Unit 4b.
- **Q6, amended.** A reference body where one exists; otherwise a declared contract in the inferred vocabulary; never a free-form trusted summary (§2.6.5). The correctness pins are the ownership matrix and corpus parity against the construct before it is deleted; the wrapper-parity test proves composition. The format and transport are unit 4a, the D4 amendment unit 4c, and each declaration the unit that owns it: catch 5, provide 6, recover 7.
- **Q7.** "Providing a service the effect doesn't use" is no longer a negative control, and F3b is a rung-2 feature.
- **Q8.** Folded into Q9: callable-typed `T` (`callables-factory.ef`) is now in scope for unit 4a.
- **Q9, extended.** Whole-value user type parameters are admitted where the body needs no layout, including callable type arguments (§2.6.3). Unit 4a.

Still open:

- **Q5.** Should `Scheduler` stop being charged by timeout (audit 1-G) when timeout is next taken up? Effect charges no service for timeout.
- **Q10.** catch's reference body `work.recover<E>(const(fallback))` needs a closure, and closures wait for explicit capture checking (`docs/specs/language-abstractions.md:124`). Until closures are admitted, catch carries a declared contract equal to what that body would infer, and its wrapper-parity test guards the equality. This note does not propose closure admission; when it lands, the declared contract is replaced by the body and the same tests must still pass.
- **Q11.** The closed-at-the-edge owner model (§6) is a named follow-up. Should it be scheduled with timeout's next change, or with the first user `scope` wrapper that needs it?
