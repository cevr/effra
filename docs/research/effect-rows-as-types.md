# Effect rows as types: catch, provide and timeout as library functions

**Status:** design note for the spike in [#115](https://github.com/cevr/effra/issues/115), phase 1, revision 2, 2026-10-11. It changes no compiler behavior. Every line reference is at `main` `f900240c`; paths are under `internal/compiler/` unless stated otherwise. Phase 2 implements only what a review of this note accepts.

Revision 2 applies the round-2 rulings on the first review:

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
  - The `.d.mts` projection is valid strict TypeScript with `Exclude`, a single-label guard and no `any`, backed by a runtime module that a strict consumer imports and runs (probes C and C2).
- **Evidence through a function edge is the obstacle, and for recipe parameters a general contract removes it.**
  - Today an ordinary function edge treats a recipe parameter as opaque (`semantic.go:4057-4060`), so wrapping `.catch` in a user function loses ownership, failure and observation evidence (probe B).
  - The recipe-parameter contract (§2.6) records, for every function, what its body does with each recipe parameter. It reuses the relation atoms that callable parameters already use. A caller substitutes the actual recipe.
  - A prototype infers it for every ordinary body (probe D). User wrappers with the library-shaped generic signature now check exactly like the direct form:
    - catch: 145 of 147 programs;
    - provide: 162 of 164;
    - timeout: 29 of 29.

    The remaining differences are probe artefacts, not contract gaps (§3). In round 1 the same wrappers matched 1, 4 and 11 programs.
  - **Callback parameters are not yet covered.** The same prototype matches 20 of 44 `.recover` programs through a wrapper. The gaps are three named clauses for callbacks (§2.6.6). Recover is therefore conditional.
- **Recommendation:**

  | Form | Phase 2 |
  | --- | --- |
  | `.catch<E>(v)` | GO, once the edge-contract unit passes its gate (unit 4) |
  | `.provide<S>(p)` | GO, after catch (unit 5) |
  | `.recover<E>(h)` | CONDITIONAL. GO only if the edge-contract unit also brings the recover wrapper corpus to identity; otherwise recover stays a construct (D1 for recover alone). |
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

The design adds no Rémy presence variables, no scoped duplicate labels and no `Lacks` constraint. Under set semantics the "lacks" fact is free, because a binding is always computed as `actual \ fixed` (§2.3).

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

- At a call, ρ is bound to a concrete row, so `ρ - L` is computed by `remove`.
- In a generic body, `ρ - L` stays symbolic as one interned label, `row-difference:<ρ>:<L>`, with two rules: `ρ - L ⊆ ρ`, and `ρ - L = ρ` when ρ was bound by an extension pattern that listed `L` (set semantics make that ρ lack `L`).
- TypeScript: `Exclude<R, I>` with `I` unconstrained, which is exactly Effect's `provideService` (§2.8).
- **Clients:** `provide` (§2.12); user test helpers that provide a test service whether or not the work uses it (`fn withTestClock<…>(work: Effect<A, { E }, { R }>) -> Effect<A, { E }, { R - Clock }>`); and ignore-style failure helpers that drop a label if present (`fn quiet<…>(work: Effect<A, { F }, { R }>) -> Effect<A, { F - Denied }, { R }>`). That is two unrelated families beyond provide.

**The unused-provide case (amended Q4).** Providing a service the work does not use is safe: the provider is installed and never read. `examples/causal.ef:47` does it today, and it runs correctly. It is not unsound, so it stays accepted, and the negative control "providing a service the effect doesn't use" is dropped from acceptance item 3. `provide` therefore uses F3b, not F3. `examples/causal.ef` is unchanged, and the name-keyed scheduler injection (§1.5) is not disturbed. Catch keeps presence: catching an absent label is the existing EF107 refusal.

### 2.4 Call-site inference

The algorithm extends `inferRows` in place.

1. Bind label parameters from the explicit type arguments.
2. For each recipe or callback argument and each row pair, as today:
   - substitute the bound labels into the formal row;
   - split it into one variable and fixed labels;
   - **check presence** of each fixed label in the actual row;
   - bind `variable := actual \ fixed`, unioning across arguments as today.
3. Instantiate the result, computing each `ρ - L` by `remove`.

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

The contract values are inferred, not declared, for ordinary bodies. "Executes `p` in place" is clause 1 with an identity owner vector; "executes `p` under an owner closed at the edge" is clause 1 with a closed owner. Neither names a construct.

#### 2.6.3 Inference

The checker checks each body once, as today, with each recipe parameter bound to a **deferred execution** instead of an opaque recipe:

- its result occurrence is a relation atom over `p` (a whole-value atom at the root path when the success type is a type variable);
- its held layers are the parameter's held captures plus a tail atom for the layers inside a type-variable success;
- each row-variable label carries a payload atom;
- its observation set is the token `param:p`.

Whatever the body does to the deferred execution (run it, close it under a scope, lift it, join it, pass it on) is recorded in the atoms' owner vectors by the existing rewrites. The function's summary keeps the atoms that reach its result, captures and failures, and records which `param:` tokens reach its result's observations or, for an effect function, its body's kills.

**Whole-value type parameters.** A value of a type-variable type is one whole value: its facts are kept at the root path, not merged into a bounded wildcard (`normalizeFacts`, `mergeKey`, `semantic.go:1330`). Inside a generic body no field layout is needed, which retires EF127 "field layout of an unresolved type parameter is unavailable" on this path (`field_occurrences.go:175`).

**User type parameters.** The go/no-go test needs a user wrapper with the library's signature, and that signature has `A: type`. Today user type parameters are refused (EF127, `semantic.go:2938-2939`). The contract unit therefore admits user type parameters that the body uses only as whole values (passed, returned, joined, carried in a recipe), and keeps EF127 for anything that needs a layout (a field read, a template instantiation). Go generics erase them as `[T any]`, as bundled functions already do (`emit_go.go:815-816`); JS erases them. This admits user generics in that narrow form; it is listed as Q9.

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

A bundled intrinsic has no body to infer from, so it declares its contract in the summary form the checker infers. Two candidate formats, decided in the edge-contract unit (Q6):

- (a) a summary record in the compiler-private interface-summary transport, which already carries relations and owner vectors (`interface_summary.go`, schema 5), checked into the bundled module;
- (b) a reference body in ordinary Effra whose inferred contract is the declaration, where one exists (catch over recover, once recover is library).

Either way the intrinsic gains nothing beyond its contract, and two tests pin it:

- the ownership matrix runs it on Go and JS (`docs/specs/language-abstractions.md`, ownership verdicts held to runtime truth);
- the wrapper test: a user function with the same signature, whose body calls the intrinsic, infers the identical summary and gives identical verdicts and diagnostics on the corpus.

The intrinsic then selects only its backend (§2.9).

The contracts of the three library functions:

| Function | Recipe parameter `work` | Callback parameter |
| --- | --- | --- |
| `catch` | executed in place; result joins the fallback value; `E`'s raised payload dropped; observations and pending kept | none |
| `provide` | executed in place; captures union the provider's; observations and pending kept | none |
| `recover` | executed in place; `E`'s raised payload dropped; observations and pending kept | `handler` invoked on a solitary `E` failure of `work`, with `work`'s layer 0 still open, given `E`'s payload |

#### 2.6.6 Callback parameters: what is still missing

Callable parameters already have relation atoms; they lack three clauses that recover's handler needs, measured in probe D:

1. **Pending through an in-place callback.** A handler with an unjoined child is refused at the argument by `requireObservedChildren` (`semantic.go:4290-4300`), where the direct form keeps the label pending on the result. This affects 6 programs, one of which (`ownership_sweep/i2_handler_declared.ef`) is accepted today.
2. **The payload a callback is given.** The direct form refuses a handler that dereferences a payload owned by a closed owner (`recovery.go:60-63`), and accepts one that does not. Through a wrapper, the payload reaches the handler as an atom over `work`'s failure, and the check fires later as "recipe holds a value owned by a closed scope". 15 programs move their refusal; 2 accepted programs (`ownership_sweep/k2_forward_inside.ef`, `p1_code_only.ef`) are refused.
3. **User generics over callable-typed `T`.** One example (`recovery-codec.ef`) needs a template argument the probe's relaxed user generics do not support; the same gap appears in catch and provide (§3).

Each clause is the recipe contract applied to callback parameters ("invoked in place", "given the payload of `p`'s failure `E`"). None names recover. Whether they infer generally is the edge-contract unit's second gate (§7).

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
| Providing a service the effect doesn't use | Accepted | Accepted (F3b, §2.3). Not a negative control. |
| Wrong provider | EF104 "provider must implement S" (`semantic.go:5066`) | The argument type mismatch against `Provider<S>`. Keep EF104 by mapping provider-typed parameter mismatches to it. |
| Removing from a closed row versus an open row | Closed: EF107 presence. Open: EF125 "recovery of an abstract row requires an unsupported row difference constraint" | Closed: EF107 presence. Open with the label literally present: accepted. Open without it: EF107 presence. The EF125 text disappears. |
| A recipe with a pending child failure | Accepted with the label kept; the consumer reports EF107 "undeclared failures: X" if undeclared | Identical: the contract keeps the label pending (§2.6.2, clause 5). Probe D moves no pending refusal for catch, provide or timeout. |
| Recover on a builtin label | EF106 "recover binds a declared error payload; handle builtin failure X with catch" (`recovery.go:35`) | Accepted: the handler takes the empty payload (§2.2). A listed refused-to-accepted change. |
| Row parameter in a message | Leaks the internal identity, as in `row-parameter:function:module:file:module:twoVars:raises:E` (probe A, p2) | Phase 2 renders the source name `E` everywhere |

The risk named in the spec ("expected row … found row" noise) does not arise: presence failures name the one missing label.

### 2.8 TypeScript projection

The intended `.d.mts` for `effra/effect`. Probe C2 checks it with strict `tsc` 6.0.3 against `effect` 4.0.1, together with the runtime module that implements it:

```ts
import type { Context, Effect } from 'effect';
type __ef_tag = { readonly _tag: string };
// A label parameter admits exactly one label: a union-typed tag is refused.
type __ef_union<T, U = T> = T extends unknown ? ([U] extends [T] ? false : true) : never;
type __ef_single<K> = true extends __ef_union<K> ? { readonly __ef_single_label_required: K } : unknown;

// fn (work: Effect<A, {E, F}, {R}>) catch<E: error, A: type, F: raises, R: uses>(fallback: A) -> Effect<A, {F}, {R}>
export declare const catch_: <A, F extends __ef_tag, R, K extends F["_tag"]>(arg_work: Effect.Effect<A, F, R>, tag: K & __ef_single<K>, arg_fallback: A) => Effect.Effect<A, Exclude<F, { readonly _tag: K }>, R>;

// fn (work: Effect<A, {E, F}, {R}>) recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(handler: effect fn(E) -> A raises {G} uses {S}) -> Effect<A, {F, G}, {R, S}>
export declare const recover: <A, F extends __ef_tag, R, K extends F["_tag"], G extends __ef_tag, S>(arg_work: Effect.Effect<A, F, R>, tag: K & __ef_single<K>, arg_handler: (arg0: Extract<F, { readonly _tag: K }>) => Effect.Effect<A, G, S>) => Effect.Effect<A, Exclude<F, { readonly _tag: K }> | G, R | S>;

// fn (work: Effect<A, {E}, {R}>) provide<S: service, A: type, E: raises, R: uses>(provider: Provider<S>) -> Effect<A, {E}, {R - S}>
export declare const provide: <A, E extends __ef_tag, R, I, P>(arg_work: Effect.Effect<A, E, R>, service: Context.Service<I, P> & __ef_single<I>, arg_provider: P) => Effect.Effect<A, E, Exclude<R, I>>;
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

- **Label parameters.** A label parameter becomes its witness parameter, placed after the subject (§2.2):
  - `tag: K & __ef_single<K>` with `K extends F["_tag"]`;
  - `service: Context.Service<I, P> & __ef_single<I>`.

  This is Effect's `catchTag`/`provideService` shape, with one correction: Effect admits a union `K` and removes every member, while its runtime catches one (review counterexample `union.mts`). The `__ef_single` guard refuses a union, matching Effra's one-label rule. The `extends` constraint is the presence check. The witness is also why TypeScript callers need no partial type arguments, which TypeScript lacks.
- **Extension patterns with fixed labels.**
  - The argument row becomes a single variable.
  - Each fixed label adds a presence guard intersected onto the parameter, `__ef_present<Row, Label> = [Label] extends [Row] ? unknown : { readonly __ef_absent_label: Label }`. Intersecting with `unknown` is the identity, so inference is unaffected.
  - The result's ρ becomes `Exclude<variable, fixed…>`. This replaces today's per-occurrence split at `callables.go:677-700` only where a pattern has fixed labels.
- **Row difference.** `{ R - S }` becomes `Exclude<R, I>` with `I` unconstrained.
- **No `any`.** A row the projection cannot express in these terms is refused with a diagnostic (the #71 rule), never widened to `any` or `never`.
- **Checker-only evidence is not projected.** Ownership, observations and pending evidence (§2.6) are facts of Effra's checker; TypeScript has no counterpart, so the projection carries none of them. This is the same for every function and every contract value (§5).

**Where the files live (Q3).** One `.d.mts` per bundled module, here `effra/effect.d.mts`, imported by the generated declarations that mention it, with `effra/effect.mjs` emitted beside it. Effra's own call sites keep lowering at the call site to the prelude helpers (§2.9); the module exists so a TypeScript consumer can import what the declarations promise. This is Gleam's `gleam.mjs` and `gleam.d.mts` pair.

**Divergence from Effect:** Effect's `timeout` adds no service (Effect's `Clock` is a default reference), while Effra charges `Scheduler`. This is the audit's 1-G debt, unchanged by this note.

### 2.9 Go and JS lowering, and the strict fallback (B1)

**Call-site lowering.** The library functions are bundled intrinsics, lowered at the call site to today's helpers: `efCatch`, `efRecover`, `efProvide_S` on Go; `__ef_catch`, `__ef_recover`, `Effect.provideService` on JS. Emission keyed on the intrinsic is a backend fact, which audit D4 permits for class 2. It is not a semantic branch. Today's bundled intrinsics are bodies of emitted wrapper functions, and `admitBundledIntrinsic` accepts one parameter only (`i64_text.go:33-47`); both change, to n parameters and call-site lowering for intrinsics that return recipes.

**The fallback is evaluated strictly** (ruling B1), like every argument (Go's rule; Gleam `result.unwrap`; ReScript `getWithDefault`). The one systematic emission difference:

| Target | Today | Library |
| --- | --- | --- |
| Go call site | `efCatch(left, "E", func() A {\n<stmts>return right\n})` | `efCatch(left, "E", right)`, with `<stmts>` written ahead of the call like any argument's |
| Go helper | `fallback func()A`, passed to `er.Catch` | `fallback A`, passed as `func() A { return fallback }` |
| JS call site | `__ef_catch(left, "E", () => (right))` | `__ef_catch(left, "E", right)` |
| JS helper | `__ef_recover(program, tag, () => Effect.sync(fallback))` | `__ef_recover(program, tag, () => Effect.succeed(fallback))` |

**Behaviour.** A fallback is a pure value of the result type (`semantic.go:5083`), and division by zero is unrepresentable (`divisor.go:14`). The only observable difference is a fallback that does not terminate: today it is never evaluated unless the failure is caught; under B1 it is evaluated once, when the recipe is built. Review probe `lazy2.ef` (`ok().catch<Boom>(spin(0))`) prints `ok` today and spins under B1. Laziness stays available through `recover`, whose handler is a function: `ok().recover<Boom>(fn(e: Boom) -> string { spin(0) })`. A by-name parameter feature is rejected (§4).

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

- 199 lines in 128 compiler testdata programs and 41 Go test files spell `.catch<`. None compares emitted bytes for a catch except the JS helper tests in `layer_causal_test.go:34-192`, which call `__ef_catch` with a lazy fallback (a recovery counter and a fallback that throws). The catch unit moves those assertions to `__ef_recover` with a handler and adds a strict-value case.
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

- **Provision edges** (`graph.go:204`, `lint_facts.go:309-311,364`). A call provides `S` when its function's signature has a `Provider<S>` parameter and its result row removes `S` from the receiver row (`{ R - S }`). The provider identity is the binding of that argument. Any user function with that signature shape yields the same edge, for example a test helper that provides `Clock`.
- **Provider origin** (`graph.go:474`, today a look-through on provide, catch and recover). The origin of a call's result is the origin of each recipe argument the contract says the result executes in place (§2.6.2, clause 1). It covers any in-place wrapper, not three kinds.
- **Reachability** (`reachability.go:852-861`).
  - Helper retention is a backend fact of the intrinsic (`RequiresHelper`, `reachability.go:848-849`), as D4 allows.
  - The provider's service is retained because the provider value is an ordinary argument value, as every value is.
  - Timeout keeps its `Scheduler` retention, because timeout stays a construct.
- `ProvisionDirect` (`lint/facts.go:219`) stays as a wire enum value.

### 2.12 The library declarations

```ef
// effra/effect, the owner module of Effect.
// The body is an intrinsic with a declared contract (§2.6.5): it runs work
// in place and, on a solitary typed failure E, succeeds with the fallback.
fn (work: Effect<A, { E, F }, { R }>) catch<E: error, A: type, F: raises, R: uses>(fallback: A) -> Effect<A, { F }, { R }> {
    "effra:intrinsic effect.catch"
}

// The one named recovery callback (language-abstractions.md:147). A pure
// handler fn(E) -> A is an effect handler with empty rows (F6).
fn (work: Effect<A, { E, F }, { R }>) recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(handler: effect fn(E) -> A raises { G } uses { S }) -> Effect<A, { F, G }, { R, S }> {
    "effra:intrinsic effect.recover"
}

// Removal without presence (F3b): providing an unused service is accepted.
fn (work: Effect<A, { E }, { R }>) provide<S: service, A: type, E: raises, R: uses>(provider: Provider<S>) -> Effect<A, { E }, { R - S }> {
    "effra:intrinsic effect.provide"
}
```

**One `recover` (ruling Q2).** `docs/specs/language-abstractions.md:147` already specifies one `recover<E>(handler)` whose handler is "a pure `fn(E) -> T` or an `effect fn(E) -> T raises {F} uses {S}`", and `catch<E>(value)` as the constant fallback. A single library signature needs:

- **Feature F6, pure-to-effect callable subsumption at argument positions.** A pure callable `fn(P) -> T` is accepted where `effect fn(P) -> T raises { G } uses { S }` is expected, binding `G` and `S` to empty rows; the emitter adapts it with the succeed adapter that recover's Go emission already uses for pure handlers (`recovery.go:213-233`).
- It reverses "Pure/effect callable modes are never implicitly lifted" (`callables.go:401`) for arguments only. Assignment and return positions keep today's rule. This is the one rule change that needs owner confirmation (Q4 below).
- **Clients:** recover, and any user combinator that takes a callback and may receive a pure one (a retry predicate, a map over a recipe). It reads with Go's simplicity (a pure function is a function that raises nothing) and projects to TypeScript directly: a function returning `Effect.Effect<A, never, never>` is the adapted form.

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
| catch | 147 | 145 | 2: `examples/callables-factory.ef`, where `T` is bound to a callable type that the probe's relaxed user generics cannot instantiate (EF103, EF127); `examples/host-types.ef`, where the rewrite put a pipe beside `+` (EF002) |
| provide | 164 | 162 | 2: `examples/callables-factory.ef` as above; `examples/callables-state.ef`, where the wrapper names a provider local to a function (EF102) |
| timeout | 29 | 29 | none |
| recover | 44 | 20 | 24: the three missing callback clauses of §2.6.6 (6 pending, 17 payload, 1 user generics) |

None of the catch, provide or timeout differences comes from the contract; each is a gap in the probe's relaxed user generics or an artefact of the textual rewrite. Every program that is identical is identical in verdict and in its full diagnostic multiset, so ownership, observations and failure evidence agree.

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
- **A scope-bodied timeout model** (`effect fn …{ scope { run work } }`) is refused inside the wrapper body (12 programs) with "value owned by closing scope cannot escape". Clause 1 records a body-closed owner, but the prototype does not yet show that a deferred whole value holds no scope-owned handle. Neither catch, provide nor recover needs it; the edge-contract unit closes it or lists it.
- **The compiler test suite with the contract on for every function:** see §3, suite run.
- **Timing.** Checking the wrapped corpora took about as long as the direct corpora (catch 8.1 s direct and 11.2 s wrapped in the last run, provide 3.5 s and 3.8 s, timeout 0.5 s and 0.7 s), at load 50 to 86. The load dominates; this is not a measurement.

**Suite run.** The whole compiler package test suite (`go test ./internal/compiler`, including the ownership matrix, the ownership sweep, the emission and conformance tests) ran twice on the probe binary: once with the contract off, once with it inferred for every function in every test program.

- **Contract off: 2 failures,** both caused by the probe patch itself: the evidence-schema audit does not know the probe's new fact field, and `recovery_test.go:165` is accepted (the intended F3 flip).
- **Contract on: the same 2, plus 2:**
  - `TestRecipeBranchEnumMissingEvidenceStaysConservative` (`recipes_test.go:649`). Its verdict assertion still holds: the leaking program is refused with exactly one EF123. Its second assertion inspects the old representation of a recipe-parameter arm (an `Exec` owner fact at `Open.file`), which the contract replaces with a relation atom. The test is rewritten to assert the verdict and the atom.
  - `TestBundledWitnessUnavailableGenericLayoutsDiagnose` (`templates_test.go:127`). It asserts that a user `fn keep<T: type>(value: T) -> T { value }` is refused with EF127. The contract treats `T` as a whole value, so the probe accepts it. This is the user-type-parameter decision of §2.6.3.
- No other test changes: every ownership-matrix verdict, every sweep golden and every emission test agrees with the contract on.

### Probe C: TypeScript projection of the signatures (round 1)

Strict `tsc` 6.0.3 with `effect` 4.0.1 accepted the round-1 declarations and a consumer with six `@ts-expect-error` negatives (exit 0, no `any`). The review added the union-tag counterexample (`union.mts`), which C2 closes.

### Probe C2: the declarations with the runtime module

- **Input:** the §2.8 `effect.d.mts`, the `effect.mjs` beside it, a consumer that imports and runs them, and a negative consumer.
- **Command:** `tsc --noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022`, with TypeScript 6.0.3 and `effect` 4.0.1; then `node` on the consumer.
- **The consumer** type-checks and runs. It pins the remaining row of catch, recover and provide by reciprocal assignment, and prints `catch fallback`, `recover recovered missing` and `catch-other failed Denied`: catch and recover remove exactly the named label, and an unhandled label still fails.
- **The negative consumer** has eight `@ts-expect-error` lines, each refused for the intended reason:
  1. a union-typed tag for catch (the single-label guard);
  2. a union-typed tag for recover;
  3. catching an absent label;
  4. a fallback of the wrong type;
  5. a handler that does not accept the payload;
  6. a provider of the wrong shape;
  7. a service key that stands for two services;
  8. a remaining row that still contains a sibling.
- **Providing an unused service is accepted** and removes nothing, matching §2.3.
- `grep -c '\bany\b'` on all files: 0.

## 4. Prior art

Pinned sources were read through okra repo caches or shallow clones. The question for each language is how it handles the three problems this note meets: removal in a library signature, laziness of a library argument, and facts that cross a function edge. For the compile-to-host languages (Gleam, ReScript, Elixir, MoonBit, Koka, PureScript, Scala.js, Effect-TS on TypeScript), the table also names the lowering path.

| Language (target) | Removal and recovery | Laziness of a fallback | Intrinsics and edge facts | Lowering | Bearing on this design |
| --- | --- | --- | --- | --- | --- |
| **Gleam** (Erlang, JS) | `Result` library: `result.try`, `map_error`, `try_recover` (`stdlib@13359e5` `src/gleam/result.gleam:83,143,397`); no rows | Strict: `result.unwrap(r, default)`; explicit `lazy_unwrap(fn() { … })` | `@external` binds a typed signature to a host function and changes neither evaluation nor checking | Library calls are plain calls; the prelude ships as `gleam.mjs` with `gleam.d.mts` | B1 (strict), Q3 and M4: a real runtime module behind the declarations |
| **ReScript** (JS) | Untyped exceptions (`exception.mdx:9-40`); polymorphic variants narrow only to named sets (`polymorphic-variant.mdx:146-180`) | Strict: `getWithDefault`; laziness by an explicit `unit => 'a` | `external` bindings are typed and trusted; no ownership | Externals inline to the host call | B1; an external never supplies deferred evaluation |
| **Elixir** (BEAM) | `try` is a special form (`lib/elixir/lib/kernel/special_forms.ex:2343`); set-theoretic types put no raised rows in signatures (`lib/elixir/lib/module/types/expr.ex:520-557`) | `&&`, `\|\|` and `if` are lazy only as **macros**; `\\` defaults are evaluated per call | No edge summaries | Macros expand before lowering | Laziness needs a construct, so by-name is not ordinary (B1); a fixed form for control fits timeout's race (§2.13) |
| **MoonBit** (Wasm, JS, C) | `raise`, `try`/`catch` as syntax; a catch does not narrow (`next/language/error-handling.md:119-146,222-227`) | Strict | `%` intrinsics behind declared signatures change only the implementation; `with_timeout` is library over async (`moonbitlang/async@e72901a` `src/async.mbt:96-100`) because MoonBit tracks no ownership | Intrinsics lower per backend | D4's reading: an intrinsic swaps the implementation, never the checking or evaluation. Its library timeout shows the timeout residue is Effra's own (race plus ownership checking) |
| **Koka** (C, JS) | `try` is a library function over handler syntax and rows: `handle/try(hndl, action : () -> <exn\|e> a)` (`koka@9c55695` `lib/std/core/exn.kk:54-56`); duplicates allowed; auto-opening (`src/Type/Operations.hs:83-98`) | The action is an explicit thunk | Evidence passing; the compiler still names `exn` (`src/Common/NamePrim.hs:316`) | Evidence-passing translation to C and JS | Borrowed: the extension pattern (F3). Not borrowed: duplicates or auto-opening, which defeat presence |
| **Effect-TS** (TS) | `catchTag`: `K extends Tags<E>`, `ExcludeTag` (`effect@66257d29` `Effect.ts:2812-2858`, `Types.ts:165`); `provideService`: `Exclude<R, I>`, no presence (`:6456-6472`) | `orElseSucceed(f: (e) => A2)`, a callback (`Effect.ts:8366` in 4.0.1) | Tags and `Context.Key` witness values | A library over a fiber runtime | The projection target (F2, F3b). Its union-`K` hole is closed, not copied (M4) |
| **PureScript** (JS) | `Prim.Row` `Cons`, `Lacks` (`Docs/Prim.hs:510-531`); `Record.delete` needs `Lacks` (`purescript-record@c89cd1a` `src/Record.purs:117-124`); `Run.catch` over higher-kinded rows (`purescript-run@abec7c3` `src/Run/Except.purs:126`) | Strict | Type classes resolved by dictionary passing | Dictionary passing to JS | F3 takes `Cons` with `Lacks` free under sets; tag witnesses are dictionary passing (F2) |
| **Scala** (JVM, JS) / **Swift** / **Kotlin** | Library combinators | By-name `=> A` and `@autoclosure` (`??`, `assert`) are general declared features; Kotlin `inline` takes explicit lambdas | Kotlin `inline` is visible in the signature | Scala.js and Swift lower a by-name argument to a thunk at every call | Rejected for Effra (ruling B1): a by-name parameter adds an evaluation rule Go readers do not expect; `recover` already gives explicit laziness |
| **Rust** (native) | `?` through lang items | Strict; closures | Ownership facts crossing a call are in the signature (lifetimes), the same for std and user code, inferred within a body and declared at the boundary | Monomorphization | The model for §2.6: edge facts are one contract that user and library functions share, never granted to library bodies alone |
| **Links** | Rémy rows with presence variables (`links@7100e28` `core/types.mli:150-154`); handlers are a special form | n/a | n/a | n/a | Presence variables are a second kind with no TypeScript analogue: not borrowed |
| **Unison** | Set abilities; `handle` is a special form; `pruneWanted` subtracts once (`unison@a1652e3a` `Typechecker/Context.hs:2789-2808`) | Delayed computations `'a` are explicit | n/a | n/a | The same set semantics as Effra; Effra adds presence |
| **Roc**, **OCaml** | Open tag unions without narrowing (`roc@3ee70f0c9` `docs/langref/tag-unions.md:50-69`); polymorphic variants narrow only to a named set (`ocaml@6471927d` `polyvariant.etex:62-70,98-137`) | Strict | n/a | Roc monomorphizes | Open unions alone do not give removal: nothing borrowed |

**The route Effra matches:**

- **Unison's set semantics** for rows;
- **PureScript's `Cons` extension pattern** for removal, with `Lacks` implicit, and dictionary passing for the tag witness;
- **Effect's `catchTag`/`provideService` shape** as the TypeScript meaning, with the union hole closed;
- **Rust's discipline for edge facts:** one contract for user and library functions, inferred in bodies and declared at intrinsic boundaries;
- **Gleam's and ReScript's strict library arguments,** with laziness as an explicit function (`recover`).

Like Koka and Elixir, Effra keeps a construct for what is control or tooling structure rather than type: timeout's race and the layer plan. Unlike Koka, removal needs no handler syntax, because the runtime helpers are the handlers.

## 5. Ladder check

The recipe-parameter contract is treated the same way in every row: it is checker-only evidence, so its TypeScript column is "not applicable", and a contract value never decides a rung by itself.

| Candidate feature | Describable without naming the construct, with at least two clients | Readable with Go's simplicity | Same meaning in TypeScript, no `any` | Erases into Go | Rung |
| --- | --- | --- | --- | --- | --- |
| F1 explicit type arguments | Yes: the library functions, `witness`, generic member calls | Yes: Go `f[T](x)` with prefix lists | Yes: a witness where TypeScript cannot take partial lists | Yes: Go generics | 2 |
| F2 label parameters with tag witnesses | Yes: catch, recover, provide, user combinators | Yes: one kind word, `E: error` | Yes: `K extends F["_tag"]` plus a single-label guard | Yes: a hidden string or accessor, the payload as a Go type parameter | 2 |
| F3 extension patterns with presence | Yes: every removal with presence, plus user functions such as `recoverAll` | Yes: "a label beside a row variable must be present" | Yes: `Exclude` plus a presence guard | Yes: rows erase | 2 |
| F3b row difference | Yes: provide, test-provider helpers, ignore-style helpers | Yes: "R without S" | Yes: `Exclude<R, I>` | Yes: rows erase | 2 |
| F6 pure-to-effect callable subsumption at arguments | Yes: recover, callback-taking user combinators | Yes: a pure function raises nothing | Yes: an `Effect<A, never, never>`-returning function | Yes: the existing succeed adapter | 2, pending owner confirmation (Q4) |
| Recipe-parameter contract, in place | Yes: catch, provide, recover, every user wrapper | Yes: invisible in source, inferred | Not applicable: checker-only evidence | Not applicable: checker-only | 2 (general checker rule over every function) |
| Recipe-parameter contract, owner closed at the edge | Yes: user `scope` wrappers, timeout's owner, layer provision's owner | Yes: inferred | Not applicable | Not applicable | 2. It is not why timeout or provideLayer stay constructs. |
| Pending evidence through an in-place parameter | Yes: every in-place wrapper | Yes: "the label stays, as with the direct form" | Not applicable: checker-only, as `handledRow`'s pending label is today | Not applicable | 2 (part of the contract) |
| First-completion race (timeout) | No general construct exists yet | – | – | – | 4: keep the construct |
| Static layer plan (`provide(Layer)`) | Tooling reads it | Yes | Rows only (Effect's `provide(Layer)`) | Not carried | 4: keep the construct |
| By-name parameters | Yes (Scala, Swift) | **No:** an evaluation rule Go readers do not expect | Yes: a thunk | Yes | Rejected (ruling B1) |
| Rémy presence variables or scoped labels | Yes | No: a second kind of row content | No | Yes | Rejected |

## 6. Go/no-go

**GO for phase 2,** as the unit sequence in §7, with two gates.

- **Rows.** GO. F3 alone is useful even if everything after it stops, and probe A shows the solver already does most of the work.
- **The recipe-parameter contract.** GO. It is inferable for ordinary bodies by extending existing machinery: the prototype brings catch, provide and timeout wrappers to identity on every program except probe artefacts, keeps `m3`/`m4` accepted, and accepts j1 like the direct form. The edge-contract unit is the go/no-go point: its gate is the same wrapper corpus at identity with a production implementation, plus the compiler suite green with the contract on for every function.
- **Callback clauses.** Not yet shown. The prototype matches 20 of 44 recover programs. If the edge-contract unit cannot bring the recover wrapper corpus to identity, recover stays a construct (D1 for recover alone), and catch and provide still go ahead.
- **If the contract unit fails its first gate,** the recorded outcome is "rows general, evidence not": D1 stands for catch, provide and recover, and F1, F3 and F3b stay for user combinators over abstract rows.

**The residue that cannot be general,** with the exact fact for each (§2.13):

1. `.timeout(ms)`: the first-completion race and the abandonment of the losing work (control).
2. `.provide(Layer)`: the static layer plan that graph, lint and inspection read (tooling).
3. `.orFail()`: the predeclared `GoResult` shape, until it is ordinary generic data.

The postfix-word reservation (T1) becomes the ordinary receiver-method rule for `catch`, `provide` and `recover`. It stays a construct reservation for `timeout` and `orFail`.

**Behaviour changes phase 2 brings,** each listed with the commit that causes it:

- **Strict fallback** (B1): every catch site's emitted bytes (§2.9), and a non-terminating fallback now diverges at construction.
- **Anchors** move from the method span to the argument span on CLI, MCP and LSP (§2.7).
- **EF105** becomes the receiver-method resolution failure.
- **Refused programs that become accepted:** `recovery_test.go:165`; recover on a builtin label; a pure-handler argument where an effect callable is expected (F6); j1-shaped programs, where a join passes through a function edge; user functions with whole-value type parameters (Q9).
- **No accepted program becomes refused.** The unused provide stays accepted, and pending children keep today's behaviour.
- **The generic EF106 argument message improves** for every generic call.

## 7. Phase-2 units

The reviewer's split, adopted by ruling. Each unit is its own branch and review, compiles, passes the gate, carries its own acceptance-5 measurement (checker cost on the examples and on the #109 wide-record shape, before and after, at recorded load), and updates its own docs (GLOSSARY terms, CHANGELOG, and the NORTH_STAR and D1 text the unit makes true).

1. **Revised note.** This revision.
2. **Rows-presence (F3 alone).**
   - Presence in `inferRows`; the abstract-subtraction refusal removed for literally present labels (`callables.go:337-343`); the EF125 refusals dropped where the label is present (`semantic.go:5055,5072`, `recovery.go:21`, `layers.go:615`).
   - Source-name rendering for row parameters.
   - Tests: `recovery_test.go:165` positive; presence and closed-versus-open negatives; a callback presence example (§2.3); the `Exclude` projection exercised by a strict consumer.
   - Catch stays a construct.
3. **Explicit type arguments and receiver methods.**
   - F1: parser lookahead, formatter and LSP spans, prefix binding, the comparison-chain disambiguation, and the `witness<T, W>`/`witness<T>` fixtures.
   - The receiver-method rule: receiver declarations, owner modules, generic member calls, and `effra/effect` named as the owner module of `Effect`.
   - Tests: an unrelated user type with a `catch` field and one with a `catch` receiver method are unaffected.
4. **Edge contract (the go/no-go point).**
   - The recipe-parameter contract for every function (§2.6.2-2.6.4): deferred executions, whole-value type parameters in bundled and user functions (EF127 kept only where a layout is needed; Q9), observations including effect bodies, pending pass-through with companions, call-site owner qualification.
   - The callback clauses of §2.6.6.
   - The declared-contract format for intrinsics (Q6).
   - **Gate 1:** the wrapper corpus for catch, provide and timeout identical to the direct form, apart from listed probe artefacts; `m3`/`m4` accepted; j1 accepted; the compiler suite green with the contract on.
   - **Gate 2:** the recover wrapper corpus identical. If it fails, record it, and recover stays a construct.
5. **Catch library.**
   - F2: label parameters, tag witnesses and their placement, the single-label rule, builtin payloads.
   - `effra/effect` with the receiver `catch` as an intrinsic with a declared contract; call-site lowering; n-parameter `admitBundledIntrinsic`.
   - B1 strict emission with the listed difference, and the `layer_causal_test.go` helper tests moved.
   - `effra/effect.mjs` and `effra/effect.d.mts` with the single-label guard, and a strict consumer that imports and runs them.
   - Diagnostics: EF105 and the generic EF106 argument message.
   - The catch parser form, checker case, emitter cases and `graph.go:474` look-through deleted; the grep test; Go and JS equivalence over every example and conformance program, apart from the listed B1 difference.
6. **Provide library.**
   - First, a monomorphic provide probe with a provider parameter, modelling the two-argument `Provider<S>` ABI.
   - F3b; `Provider<S>` source type and witness; the receiver `provide`.
   - Graph, lint and reachability facts from signatures and the contract (§2.11).
   - `examples/causal.ef` unchanged.
7. **Recover library** (only if gate 2 passed).
   - F6, with owner confirmation; the single receiver `recover`; builtin payloads; `recovery.go` deleted except helpers the contract reuses.
   - Optionally, catch re-expressed over recover in ordinary Effra, if its emission is acceptable.

## 8. Cost estimate

Redone after B1 and B2, per unit.

| Unit | Scope | Estimate |
| --- | --- | --- |
| 2 | F3 | S, 0.5 to 1 day |
| 3 | F1 and receiver methods | M, about 2 days (parser, resolver, formatter, LSP) |
| 4 | Edge contract | L, 4 to 6 days. The prototype is the plan for gate 1; productionizing it, the callback clauses and the full suite under the contract are where the risk sits. |
| 5 | Catch library | M to L, about 3 days (F2 on two targets, B1 emission, the runtime module, equivalence tests) |
| 6 | Provide library | M, about 2 days |
| 7 | Recover library | M, about 2 days, conditional on gate 2 |

The total is about 13.5 to 16 implementor-days plus review rounds, up from round 1's 8 to 10. The general contract (unit 4) is larger than an intrinsic-only summary, as the review predicted, and the strict emission adds a listed byte change to unit 5.

## 9. Open questions

- **Q1.** Settled by ruling: `effra/effect` owns `Effect`, and dot calls go through the receiver-method rule (§2.10).
- **Q3.** Settled by ruling, with M4: one `.d.mts` per bundled module, with a real `.mjs` beside it (§2.8).
- **Q4.** F6 reverses "Pure/effect callable modes are never implicitly lifted" (`callables.go:401`) for argument positions. Ruling Q2 implies it; it needs explicit owner confirmation before unit 7.
- **Q5.** Should `Scheduler` stop being charged by timeout (audit 1-G) when timeout is next taken up? Effect charges no service for timeout.
- **Q6.** The format of an intrinsic's declared contract: a summary record in the interface-summary transport, or a reference body in Effra where one exists (§2.6.5). Decided in unit 4.
- **Q7.** Acceptance item 3 loses "providing a service the effect doesn't use" as a negative control (amended Q4, §2.3), and F3b joins the rung-2 features. The spec's acceptance list should be updated to match.
- **Q8.** User generics over a callable-typed `T` (`examples/callables-factory.ef`, `recovery-codec.ef` in probe D) are outside the contract. They belong to the user-generics work, not to this spike, but a library `catch` used on such a value inherits the gap until then.
- **Q9.** The go/no-go test needs whole-value user type parameters (§2.6.3), so the edge-contract unit admits `T: type` in user functions where the body needs no layout. This narrows today's EF127 refusal of user type parameters; it needs owner confirmation before unit 4 lands.
