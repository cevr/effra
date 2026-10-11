# Effect rows as types: catch, provide and timeout as library functions

**Status:** design note for the spike in [#115](https://github.com/cevr/effra/issues/115), phase 1, 2026-10-11. It changes no compiler behavior. Every line reference is at `main` `f900240c`; paths are under `internal/compiler/` unless stated otherwise. Phase 2 implements only what a review of this note accepts.

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
  - Three small rung-2 features make removal general:
    - label parameters;
    - extension patterns with a presence check;
    - explicit type arguments.
  - The `.d.mts` projection of the result is valid strict TypeScript with `Exclude` and no `any` (probe C).
- **The obstacle is ownership and failure evidence.** The current cases compute these facts beside the rows, and no row signature carries them.
  - Routing the forms through an ordinary function edge today loses that evidence (probe B):
    - 1 of 147 `.catch` programs keeps its exact result with a type-generic wrapper;
    - 86 of 147 keep it with a monomorphic one.
  - One general summary rule recovers the evidence for transformers that introduce no owner: §2.6, *transparent parametric summaries*.
  - It cannot recover a fact about an owner that the callee opens and closes.
- **Recommendation:**

  | Form | Phase 2 |
  | --- | --- |
  | `.catch` | GO, first commit series |
  | `.provide<S>` | GO, second |
  | `.recover` | GO, third |
  | `.timeout(ms)` | NO-GO. Its rows are expressible, but it opens and closes the timeout owner, which is a lifetime fact (rung 4). |
  | `.provide(Layer)` | NO-GO, for the same reason (the provision owner), and because tooling reads the static layer plan. |
  | `.orFail()` | Stays syntax until `GoResult` is ordinary data (audit S8). |

  D1 narrows to `.timeout`, `.provide(Layer)` and, for now, `.orFail`.

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
| `.catch<E>(v)` (`semantic.go:5070-5101`) | **Presence:** EF107 "effect does not admit failure E" (`:5081`). **Removal:** `handledRow` (`recovery.go:99-106`). | Joins the recipe's result occurrence with the lifted fallback (`:5091-5095`). Drops E's raised payload evidence (`:5099`). Keeps E in the row when E is pending from an unobserved forked child (`handledRow`, `completeHandled`, `recovery.go:108-118`). Fallback must be a pure value of the result type (`:5083`). |
| `.recover<E>(h)` (`recovery.go:19-97`) | Removes E. Adds the handler's rows when the handler is an effect (`:87-91`). | Invokes the handler inside the failing execution, so `Exec(0)` stays open (`handlerResult`, `recovery.go:163-198`; `freeRecoveredLayer`, `:128-142`). Refuses a handler that dereferences a payload owned by a closed owner (`:60-63`). Joins the result fields. |
| `.provide<S>(p)` (`semantic.go:5053-5069`) | `remove(services, S)` (`:5068`). **No presence check:** providing an unused service is accepted. | Unions the provider's captures (`:5067`). The provider must be a provider value of S, EF104 (`:5066`). |
| `.provide(Layer)` (`layers.go:597-659`) | `services \ Provides ∪ Requirements` (`:620-621`). | The program's layer 0 runs under a fresh provision owner that closes at the edge (`:626-634`). Refuses escapes (`:635-651`). Records static plan reasons (`:652-656`). |
| `.timeout(ms)` (`semantic.go:4977-5004`) | `∪ {Timeout}` and `∪ {Scheduler}` (`:5004`). | Closes the timed layer under a fresh timeout owner, `closeOccurrence(…, closedOwner(ownershipOwnerTimeout, …))` (`:5000-5003`). |
| `.orFail()` (`semantic.go:4865-4879`) | `∪ {GoError}` on an Effect returning `GoResult`. | None beyond pass-through. |

**Pending evidence** (design §5.3) is a fact about the evidence, not the type.

- A recipe whose layer 0 forks a child it never joins may raise that child's failure when the *executor's* owner closes. That happens outside the recipe, so a handler around the recipe cannot see it.
- The label stays in the row: `handledRow` keeps it.
- The evidence ends at every type boundary. `refuseCarriedPending` (`recipes.go:35-49`) refuses such a recipe as a call argument, provider configuration or failure payload, with EF107 "recipe may leave forked child failures unobserved (…)".
- This general rule already exists and is tested (`fork_observation_test.go:194`).

### 1.4 TypeScript projection today

- **Failure labels** project as follows (`jsRowsContract`, `emit.go:62-98`):
  - an error with fields becomes `<E>Error`;
  - any other error becomes `{ readonly _tag: "<E>" }`;
  - an empty row becomes `never`.
- **Services** project to the branded `<S>Requirement` interfaces. Each required service also has a `Context.Service<<S>Requirement, <S>Provider>` tag (`emit.go:242-261`).
- **Row parameters** (`jsRowFunctionSignature`, `callables.go:612-735`):
  - each occurrence gets its own variable, `__ef_row_<i>_<E> extends { readonly _tag: string }`;
  - fixed labels beside the variable already project as `Exclude<__ef_row_i_E, fixed…>` in the result (`callables.go:684-700`).

  So the projection already speaks `Exclude`. It lacks presence and label witnesses.
- **No test exercises the `Exclude` branch.** Its only fixture is refused today (`recovery_test.go:165`).
- **Bundled functions get no `.d.mts`.** Only current-module declarations are declared (`emit.go:314`).
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

- The helpers are prelude chunks (`js_prelude.go:43-91`). `prelude/catch.mjs:1` delegates to `__ef_recover`, which handles only a solitary `Fail` whose `_tag` equals the tag (`prelude/recover.mjs:4-10`).
- orFail has no JS lowering: programs that import Go are Go-only (`emit.go:165-167`).

**Go** (`emit_go.go:1255-1426`):

| Form | Emitted Go |
| --- | --- |
| catch | `efCatch(left, "E", func() A {…})` (`:1397-1402`) |
| recover | `efRecover(tP, "E", func(efPayload efType_E) efEffect[A] {…})` (`recovery.go:213-233`) |
| provide | `efProvide_S(t, p)` (`:1388-1393`), which assigns one `efContext` field (`:379-380`) |
| provideLayer | `efLayer_L(left)` (`layer_emit.go:11-75`) |
| timeout | `efTimeout(t, ms)` (`:1329-1334`) |
| orFail | an inline `er.OrFail` closure (`:1272-1275`) |

- The helper bodies are at `emit_go.go:330-341`.
- At run time, Go matches failures by tag string (`runtime/effra/effect.go:13-16,282-315`).
- Payload-free failures carry a nil payload, so a Go type parameter alone cannot identify a label. Both targets need the tag string.

**Generic and row parameters:**

- Row parameters erase on both targets.
- Bundled type parameters become Go generics `[T any]`, with no monomorphization (`emit_go.go:815-816`).
- JS erases both kinds.

**Reachability** (`reachability.go:852-864`):

- catch, recover and orFail retain their helper;
- timeout also retains `Scheduler` (`:854-857`);
- provide retains the provider's service;
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
- `lint_facts.go:309-311`.
- `resolution.go:196-202`.
- `interface_summary.go:209` (the `timeout` owner kind).

`absent_syntax.go:80-82,203` (help text for an absent `try`/`catch`) and `semantic.go:2876` (TypeScript reserved words) are unrelated uses of the word.

## 2. Design

### 2.1 Row variables

No new representation. Row variables stay what `functionRows` makes them: declaration-qualified labels in an ordinary interned set.

The design adds no Rémy presence variables, no scoped duplicate labels and no `Lacks` constraint. Under set semantics the "lacks" fact is free, because a binding is always computed as `actual \ fixed` (§2.3).

### 2.2 Label parameters and tag witnesses

**Feature F2, the label parameter.** It is declared as `E: error` or `S: service` beside the existing `raises` and `uses` kinds. It denotes exactly one nominal label, and can stand:

- in a row, as a label: `{ E, F }`;
- as a type: an `error` parameter is its payload type, as in `effect fn(E) -> A`; a `service` parameter is used in `Provider<S>`, the provider value type that already exists as the `provider:S` type node (`emit.go:32-34`).

**Identity.** `label-parameter:<identity>:<kind>:<name>`, interned like a row parameter. It is bound only by an explicit type argument (§2.5), never inferred, so it never widens.

**Erasure.** A label is matched by its tag at run time (§1.5), so an `error` parameter is reified as a hidden **tag witness**:

- At each call, the emitter passes the label's tag string. For a bundled intrinsic lowered at the call site (§2.9), the literal lands exactly where today's emitter writes `"E"`, so the output is byte-identical.
- A user function with an `error` parameter receives the witness as an ordinary extra `string` parameter on Go and on JS (dictionary passing).
- The payload type erases into a Go type parameter, as `efRecover[A, P any]` already does (`emit_go.go:332`).
- A `service` parameter's witness is the context accessor: a `func(efContext, efService_S) efContext` on Go and the `Context.Service` value on JS.

This passes the "erases into Go" test with no reflection.

### 2.3 Extension patterns: presence and removal

**Feature F3.** A parameter row written `{ L1, …, Ln, ρ }`, where ρ is a row variable and each `Li` is a fixed or label-parameter label, is an **extension pattern**:

- Each `Li` must be in the argument's row. Otherwise the call is refused with the label's membership diagnostic (§2.7).
- ρ binds to the argument's row minus the `Li`.
- A signature removes `Li` simply by not writing it in the result: `Effect<A, { E, F }, { R }>` in, `Effect<A, { F }, { R }>` out.

Rows without a variable keep today's upper-bound meaning.

This is the standard reading of a row pattern: PureScript's `Cons l a r1 r2`, Rémy's `{l : τ | ρ}`. Probe B found only one checked-in program or test with a fixed label beside a variable in a parameter row: `recovery_test.go:165`, which is refused today and becomes accepted. Admitting presence breaks no current caller.

**Inside a generic body, removing a present label from an abstract row is sound and needs no difference constraint.**

- Removing `L` from a row `{ L, ρ }` that literally contains it yields `{ ρ }`, so the EF125 sites listed in §1.2 go away.
- Suppose ρ's eventual instantiation also contains `L`. Then the result over-approximates the real row, because catch handles every raise of `L`. Rows are upper bounds (§1.1), so over-approximation is sound.
- Removing a label the row does not literally contain is refused by the presence rule, even when ρ might contain it. That is conservative.
- No subtraction from an unknown row is ever needed. That keeps the solver finite and removes the "abstract row subtraction" refusal at `callables.go:337-343`.

### 2.4 Call-site inference

The algorithm extends `inferRows` in place.

1. Bind label parameters from the explicit type arguments.
2. For each recipe or callback argument and each row pair, as today:
   - substitute the bound labels into the formal row;
   - split it into one variable and fixed labels;
   - **check presence** of each fixed label in the actual row;
   - bind `variable := actual \ fixed`, unioning across arguments as today.
3. Instantiate the result.

The procedure is a single pass, linear in the parameters times the labels per row, with one variable per row as today. It is decidable by construction: there is no unification over row variables, no constraint solving and no fixed point. The cost added over today's solver is one membership test per fixed label.

### 2.5 Explicit type arguments

**Feature F1.** A call may supply a prefix of its type arguments, `Effect.catch<NotFound>(work, "x")`, using Go's rule (`f[T](x)`, prefix lists allowed). The remaining type, row and label parameters are inferred.

- Label parameters are ordered first, so the prefix is exactly the labels.
- The parser needs a `name<types>(` lookahead in expression position, because `a < b > (c)` is otherwise a comparison chain. The disambiguation already exists after `.` for the postfix forms (`syntax.go:1661-1674`, `expectGenericOpen`). The C# and TypeScript rule applies: commit to a type argument list when it closes directly before `(`.

**Clients:**

- the three library functions;
- `effra/conversions.witness<T, W, …>`, which currently relies on inference;
- any future bundled generic whose type cannot be inferred.

### 2.6 Ownership and failure evidence: transparent parametric summaries

**The problem.** This is where a naive library route fails (probe B).

- A function edge summarizes recipe arguments only by their held captures. Its result facts are never parameter-relative (`instantiateCheckedFacts`, `semantic.go:1222-1232`).
- A missing argument fact becomes `unknown`/`helper` (`semantic.go:1246,1294`).
- A user generic wrapper's catch on a value of type `T` hits EF127 "field layout of an unresolved type parameter is unavailable".
- Downstream uses of the unknown result are then refused with EF123.

**Feature F4.** F4 is a summary rule for every bundled function whose body is an intrinsic and whose result is a recipe. It does not mention any particular function. It derives the summary from the signature alone, by parametricity: a function polymorphic in `A` can return only the `A` values it was given. It also derives from its declared **transparency**: the intrinsic runs its recipe arguments in the caller's executor and opens no owner.

1. **Result occurrence.** The result recipe's success occurrence is the join, as in today's `joinOccurrence`, of:
   - each recipe argument whose success type is `A`;
   - each `A`-typed value argument, lifted as `liftOccurrence` does today;
   - each callback argument returning `A`, invoked inside the recipe's execution with `Exec(0)` open, through today's `handlerResult` invocation.
2. **Captures.** The union of every argument's held facts, as `refuseClosedArgument` already reads them.
3. **Failure payload evidence.** For each label of the instantiated result row, the join of that label's evidence across the recipe arguments and callback results. A label the signature removes drops its raised evidence: today's `handleWith`/`withoutLabel`.
4. **Pending evidence.** None crosses the edge, because `refuseCarriedPending` already refuses it at the argument (§1.3). A general alternative is to let pending labels pass through and widen the result row. It is rejected: the row would no longer follow from the signature, and the `.d.mts` row would understate the runtime row, which fails the TypeScript-meaning test.
5. **Owners.** None are introduced. An intrinsic whose implementation closes an owner cannot be transparent.

**Which forms F4 reproduces:**

- **catch:** join with the lifted fallback, payload drop.
- **provide<S>:** captures union, pass-through.
- **recover:** handler invocation inside the failing execution, closed-payload refusal, handler rows.

It cannot reproduce timeout's closed timeout owner or the layer provision owner, which are rule 5's exclusions.

**Transparency is a trusted property of each intrinsic's implementation,** as the library-intrinsic contract (audit D4) allows. Conformance proves it: `efCatch`, `efRecover` and `efProvide_S` run the program in the caller's context (`emit_go.go:331-332,379-380`; `prelude/recover.mjs:4-10`). Ownership cases on both targets pin it.

**The intrinsic checker case becomes generic.** Its value is the declared result contract with the F4 summary. There is no switch on the operation name, unlike today's `i64` ops (`i64_text.go:52-71`), and no case keyed on catch, provide or recover.

### 2.7 Diagnostics

The rule: same code, same text where the fact is the same, and a better anchor where the library form can give one. Each diagnostic below reaches CLI, MCP and LSP through the shared diagnostics model, as today.

| Program | Today | Library route |
| --- | --- | --- |
| Catching a label not in the row | EF107 "effect does not admit failure E" at `.catch` (`semantic.go:5081`) | Same code and text from the presence check, anchored at the receiver argument (the dot or pipe span, as `pipedArgumentMismatch` already does) |
| Unknown failure name | EF102 "unknown failure E" (`:5077`) | EF102 from type-argument resolution, same text |
| Wrong fallback or handler type | EF106 "prototype catch fallback must be a pure string" at the fallback (`:5083`) | **Must not regress** to the generic EF106 "argument has incompatible instantiated callback contract" at the call (`callables.go:357`), which probe B shows. Phase 2 replaces that message for every generic call with "argument `fallback` must be `string`", using the instantiated parameter type at the argument's span. This is a general improvement. |
| Providing a service the effect doesn't use | Accepted silently | **New refusal** from the presence check. Proposed text: "effect does not require service S". The code is chosen in phase 2; EF107 is the failure-side sibling. `examples/causal.ef:47` provides an unused `Scheduler` and needs a spelling change (§3, probe B). |
| Wrong provider | EF104 "provider must implement S" (`:5066`) | The argument type mismatch against `Provider<S>`. Keep EF104 by mapping provider-typed parameter mismatches to it. |
| Removing from a closed row versus an open row | Closed: EF107 presence. Open: EF125 "recovery of an abstract row requires an unsupported row difference constraint" | Closed: EF107 presence. Open with the label literally present: accepted. Open without it: EF107 presence; the EF125 text disappears. |
| A recipe with a pending child failure | Accepted; the consumer reports EF107 "undeclared failures: X" if undeclared | EF107 "recipe may leave forked child failures unobserved (X)" at the argument. 39 corpus programs move their refusal; 2 accepted programs become refused (probe B). |
| Row parameter in a message | Leaks the internal identity, as in `row-parameter:function:module:file:module:twoVars:raises:E` (probe A, p2) | Phase 2 renders the source name `E` everywhere |

The risk named in the spec ("expected row … found row" noise) does not arise: presence failures name the one missing label.

### 2.8 TypeScript projection

The intended `.d.mts` (probe C checked it with strict `tsc` 6.0.3 against `effect` 4.0.1):

```ts
type __ef_tag = { readonly _tag: string };
type __ef_present<Row, Label> = [Label] extends [Row] ? unknown : { readonly __ef_absent_label: Label };

// catch<E: error, A: type, F: raises, R: uses>(work: Effect<A, {E, F}, {R}>, fallback: A) -> Effect<A, {F}, {R}>
export declare const catch_: <A, F extends __ef_tag, R, K extends F["_tag"]>(arg_work: Effect.Effect<A, F, R>, tag: K, arg_fallback: A) => Effect.Effect<A, Exclude<F, { readonly _tag: K }>, R>;

// recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(work: Effect<A, {E, F}, {R}>, handler: effect fn(E) -> A raises {G} uses {S}) -> Effect<A, {F, G}, {R, S}>
export declare const recover: <A, F extends __ef_tag, R, K extends F["_tag"], G extends __ef_tag, S>(arg_work: Effect.Effect<A, F, R>, tag: K, arg_handler: (arg0: Extract<F, { readonly _tag: K }>) => Effect.Effect<A, G, S>) => Effect.Effect<A, Exclude<F, { readonly _tag: K }> | G, R | S>;

// provide<S: service, A: type, E: raises, R: uses>(work: Effect<A, {E}, {S, R}>, provider: Provider<S>) -> Effect<A, {E}, {R}>
export declare const provide: <A, E extends __ef_tag, R, I extends R, P>(arg_work: Effect.Effect<A, E, R>, service: Context.Service<I, P>, arg_provider: P) => Effect.Effect<A, E, Exclude<R, I>>;

// timeout<A: type, E: raises, R: uses>(work: Effect<A, {E}, {R}>, ms: i64) -> Effect<A, {E, Timeout}, {R, Scheduler}>
export declare const timeout: <A, E extends __ef_tag, R>(arg_work: Effect.Effect<A, E, R>, arg_ms: bigint) => Effect.Effect<A, E | { readonly _tag: "Timeout" }, R | SchedulerRequirement>;

// user code: fn recoverAll<E: raises, R: uses>(work: Effect<string, {NotFound, Denied, E}, {R}>) -> Effect<string, {E}, {R}>
export declare const recoverAll: <E extends __ef_tag, R>(arg_work: Effect.Effect<string, E, R> & __ef_present<E, NotFound> & __ef_present<E, { readonly _tag: "Denied" }>) => Effect.Effect<string, Exclude<E, NotFound | { readonly _tag: "Denied" }>, R>;
```

**Projection rules:**

- **Label parameters.** A label parameter becomes its witness parameter: `tag: K` with `K extends F["_tag"]`, or `service: Context.Service<I, P>` with `I extends R`. This is exactly Effect's `catchTag`/`provideService` shape. The `extends` constraint is the presence check. The witness is also why TypeScript callers need no partial type arguments, which TypeScript lacks.
- **Extension patterns with fixed labels.**
  - The argument row becomes a single variable.
  - Each fixed label adds a `__ef_present` guard intersected onto the parameter. Intersecting with `unknown` is the identity, so inference is unaffected.
  - The result's ρ becomes `Exclude<variable, fixed…>`. This replaces today's per-occurrence split at `callables.go:677-700` only where a pattern has fixed labels.
- **Timeout.** `timeout` is plain union; it is listed for completeness, although `.timeout` stays a construct (§2.12).
- **No `any`.** A row the projection cannot express in these terms is refused with a diagnostic (the #71 rule), never widened to `any` or `never`.

**Which `.d.mts` carries the bundled declarations is open:**

- Today bundled functions get none (`emit.go:314`).
- Phase 2 either emits an `effra/effect` declaration module or inlines the declarations into each module that re-exports them. Open question Q3.

**Divergence from Effect:** Effect's `timeout` adds no service (Effect's `Clock` is a default reference), while Effra charges `Scheduler`. This is the audit's 1-G debt, unchanged by this note.

### 2.9 Go and JS lowering

Go lowering is unchanged in bytes.

- The library functions are bundled intrinsics, **lowered at the call site**. The emitter's intrinsic case emits:
  - `efCatch(left, "E", func() A {…})`;
  - `efRecover(…)`;
  - `efProvide_S(t, p)`;
  - on JS, `__ef_catch(left, "E", () => (v))`, `__ef_recover(…)` and `Effect.provideService(…)`.
- These are exactly today's shapes, so examples and conformance programs produce byte-identical Go and JS. The pipe lane's equivalence harness (`pipe_equivalence_test.go`) is the model for the test.
- Today's bundled intrinsics are instead the bodies of emitted wrapper functions. `admitBundledIntrinsic` also accepts one parameter only (`i64_text.go:33-47`). Both change: n parameters, and call-site lowering for intrinsics that return recipes.
- Emission keyed on the intrinsic is a backend fact. That is permitted for class 2 (audit D4) and is not a semantic branch.
- Reachability retains the helper through the intrinsic's `RequiresHelper` (`reachability.go:848-849`), not a kind case.

### 2.10 Spelling

**The spec names two spellings:**

- the admitted owner-module dot call, `work.catch<NotFound>("x")` meaning `Effect.catch<NotFound>(work, "x")`;
- the pipe.

**The obstacles to the dot call:**

- Receiver methods are admitted but not implemented (GLOSSARY "Receiver method"; `docs/specs/language-abstractions.md:105`).
- `Effect` is a reserved predeclared language type (audit S11b), so it has no owner module.

**Phase-2 default (decided by never-block and reversibility):**

- The language names `effra/effect` as the owner module of the predeclared recipe type.
- The parser resolves `.catch<…>(…)`, `.provide<…>(…)` and `.recover<…>(…)` on any receiver to that module's function, as the receiver-method rule prescribes. This is the first, narrowest implementation of that rule.
- Source spelling, formatter output and emitted code stay byte-identical, and the ~700 occurrences in more than 300 files need no migration.
- The dot-call resolution is parser and resolver work. It adds no checker, emitter or reachability branch.
- A record field named `catch` stays shadowed by the method rule, as Go's method and field namespaces collide. The audit's T1 reservation becomes the ordinary rule that a receiver method and a field of the same type cannot share a name.

**Fallback,** if the owner rejects naming an owner module for a predeclared type: migrate to the pipe, `work |> Effect.catch<NotFound>("x")`, mechanically. This is open question Q1.

### 2.11 The library declarations

```ef
// effra/effect: bundled/effect/catch.ef
// The body is compiler-owned: it runs the recipe and, on a solitary typed
// failure E, succeeds with the fallback instead.
fn catch<E: error, A: type, F: raises, R: uses>(work: Effect<A, { E, F }, { R }>, fallback: A) -> Effect<A, { F }, { R }> {
    "effra:intrinsic effect.catch"
}

// bundled/effect/recover.ef
fn recover<E: error, A: type, F: raises, R: uses, G: raises, S: uses>(work: Effect<A, { E, F }, { R }>, handler: effect fn(E) -> A raises { G } uses { S }) -> Effect<A, { F, G }, { R, S }> {
    "effra:intrinsic effect.recover"
}

// bundled/effect/provide.ef
fn provide<S: service, A: type, E: raises, R: uses>(work: Effect<A, { E }, { S, R }>, provider: Provider<S>) -> Effect<A, { E }, { R }> {
    "effra:intrinsic effect.provide"
}
```

**`recover` accepts a pure or an effect handler today** (`recovery.go:40-44,87-91`). Effra never lifts a pure callable implicitly (`callables.go:401`), so the library needs two functions:

- `recover` for an effect handler;
- `handle` for a pure handler `fn(E) -> A`.

The name is open question Q2. Callable-mode polymorphism is rejected: it fails "readable with Go's simplicity".

**A user-written removal** (acceptance item 4) checks through the same rules, with no bundled identity involved:

```ef
fn recoverAll<E: raises, R: uses>(work: Effect<string, { NotFound, Denied, E }, { R }>) -> Effect<string, { E }, { R }> {
    work.catch<NotFound>("not found").catch<Denied>("denied")
}
```

### 2.12 The residue: `.timeout`, `.provide(Layer)`, `.orFail`

**`.timeout(ms)`.**

- Its rows are ordinary addition and need nothing new: `Effect<A, { E }, { R }> -> Effect<A, { E, Timeout }, { R, Scheduler }>`.
- But its implementation runs the timed work under a fresh owner and closes that owner at the edge:
  - Go: `TimeoutWithEffect` (`runtime/effra/effect.go:376`);
  - JS: `__ef_scoped` in `prelude/timeout.mjs:13-44`.
- The checker mirrors that with `closeOccurrence(…, closedOwner(ownershipOwnerTimeout, …))` (`semantic.go:5003`), so a handle acquired inside the timed work is refused after the edge.
- A transparent F4 summary would accept that use, which is unsound: the runtime has released the handle.
- **No general type feature carries the fact.**
  - It is about the callee owning the execution of its argument. Under the ladder that is lifetime (rung 4).
  - The narrowest expressing feature, a "callee-scoped recipe parameter" marker, has one client today (timeout), which is also rung 4.
  - TypeScript has no counterpart, because the projection carries no ownership at all.
- **The route by which it could later become library:** a general first-completion construct, a Go-`select`-like race over fibers. `timeout` would then be ordinary `.ef` over `scope`, `fork` and that construct, and `scope` supplies the owner. That is a separate design. The `Scheduler` charge stays audit 1-G either way.

**`.provide(Layer)`.**

- The provision owner closes at the edge (`layers.go:626-634`), which is lifetime.
- The static plan identity is read by graph (`graph.go:214`), lint (`lint_facts.go:311-319`) and inspection (`layers.go:652-656`), so tooling reads the structure.
- **Both are rung 4.** A first-class `Layer<Out, Err, In>` value type would carry the rows (Effect's `provide(Layer)` shape), but not the owner or the static graph. It stays a construct.

**`.orFail()`.**

- Its rows are addition: `Effect<GoResult<A>, { E }, { R }> -> Effect<A, { E, GoError }, { R }>`.
- It becomes library once `GoResult` is ordinary generic data. Its input type is predeclared host-interop shape today (audit S5/S8).
- It is Go-only, with no JS lowering.
- It stays syntax until then, per audit D1's existing `.orFail` clause.

## 3. Feasibility probes

I built every probe outside the repository. The compiler was built from `f900240c` on a scratch branch (never pushed) carrying three throwaway relaxations:

- the EF125 abstract-row refusal dropped when the label is literally present;
- the EF127 user-generic refusal dropped;
- an optional presence check in `inferRows`.

The host load average was 20–55 throughout, set by other sessions.

### Probe A: rows through an ordinary function, today's compiler

**Pass-through and addition** check as written:

- `fn keep<E: raises, R: uses>(work: Effect<string, { E }, { R }>) -> Effect<string, { E }, { R }>`;
- `fn addDenied<…>(…) -> Effect<string, { E, Denied }, { R }>`.

**Removal at the call site already works.**

- `fn dropNotFound<E: raises, R: uses>(work: Effect<string, { NotFound, E }, { R }>) -> Effect<string, { E }, { R }>`, called on a `{ NotFound, Denied }` recipe, binds `E = { Denied }`.
- Its body (`work.catch<NotFound>(…)`) is refused with EF125 (`semantic.go:5073`).

**Presence is not checked.** `dropNotFound` on a recipe without `NotFound` is accepted silently.

**With the presence check enabled,** the same call is refused with EF107.

**Contracts match, ownership does not.** A three-step chain written as wrappers (`find(1) |> catchNotFound("x") |> timeoutW(10) |> provideConsole()`) and the same chain as constructs produce the same public contract signature (`v:d82008e5…`). The wrapper version alone carries an `unknown`/`helper` capture fact: the evidence loss of §2.6.

**Display names leak.** p2: a row-parameter identity appears in an EF106 message.

### Probe B: the corpus through a function edge

- **Corpus:**
  - `examples/**/*.ef`;
  - `internal/compiler/testdata/ownership_sweep/*.ef` (112 programs);
  - `ownership_probes/*.ef`;
  - the conformance `.ef` files.
- **Rewrite:** each `.catch<L>(` became `|> wrap_L(`. The wrapper body is the construct itself over a row-polymorphic parameter.
- **Comparison:** every program's verdict and diagnostic multiset, against the direct construct.

**Monomorphic `string` wrappers:**

| Form | Programs | Identical | Wrapper type mismatch (probe artefact) | Pending moved | Ownership (EF123) | Other |
| --- | --- | --- | --- | --- | --- | --- |
| catch | 147 | 86 | 14 | 45 | ≈6 | rewrite artefacts (pipe beside `+`, callable fallback) |
| timeout | 29 | 11 | most of the rest | – | 6 | – |

**Type-generic wrappers** (`T: type`, which a library `catch<A: type, …>` needs):

| Form | Programs | Identical | Refused with EF123 or EF127 |
| --- | --- | --- | --- |
| catch | 147 | 1 | 131 programs gained EF123; 16 of the 18 examples that use catch were refused |
| timeout | 29 | 11 | 18 gained EF123 |
| provide | 164 | 4 | EF123 dominant |

**What the numbers show:**

- **No regression in the row facts.**
  - **Failure rows.** Across all variants, no program gained a failure-row diagnostic other than the intended presence refusal and the pending move.
  - **Service rows.** The only new service-row refusal is the intended presence refusal on `examples/causal.ef:47`, which provides `Scheduler` to an effect that uses only `Clock`.
  - **Composite causes.** The one composite-cause difference, `ownership_sweep/q4_childcomposite.ef`, is failure evidence passing the edge, which F4's rule 3 owns.
- **Ownership evidence through a function edge is the gap.**
  - Generic results degrade to `unknown` and are refused downstream.
  - The pending rule moves 39 refusals from "undeclared failures: X" at the consumer to "recipe may leave forked child failures unobserved (X)" at the argument. It also refuses 2 programs accepted today: `ownership_sweep/m3_nonrec.ef` and `m4_rec_simple.ef`.
  - A scope-bodied timeout model (`effect fn …{ scope { run work } }`) behaved identically to the construct-bodied wrapper. A function edge whose body opens the owner keeps the owner, but only because the body is ordinary Effra. An intrinsic body has no such body to check.
- **Checker cost.** The direct and wrapped corpora checked in comparable time: 0.94 s against 0.93 s for 147 catch programs in one run, and 2.4 s against 1.9 s in a noisier one. The load dominates. The solver change is one membership test per fixed label (§2.4).
- **Not yet measured:** the wide-record shape from #109 (the 20,000-field record) and a clean before/after at recorded load. Both belong to phase 2, after the implementation exists (acceptance item 5).

### Probe C: TypeScript projection

- **Input:** the §2.8 declarations and a consumer.
- **Command:** `tsc --noEmit --strict --module NodeNext --moduleResolution NodeNext --target ES2022`, with TypeScript 6.0.3 from the framework-ports leaf and `effect` 4.0.1 from the root.
- **Result:** exit 0.
- **What the consumer pins:**
  - the remaining row of catch, recover, provide, timeout and `recoverAll`, by a reciprocal assignment;
  - six `@ts-expect-error` negatives, each refused for the intended reason:
    1. catching an absent label;
    2. a wrong fallback type;
    3. a handler of the wrong payload;
    4. providing an unused service;
    5. a removal from a row lacking the fixed labels;
    6. a row that still contains a removed sibling.
- `grep -c '\bany\b'` on the declarations: 0.

## 4. Prior art

Pinned sources were read through okra repo caches or shallow clones.

**Koka** (`koka@9c55695`).

- `try` is an ordinary function over handler syntax: `handle/try(hndl, action : () -> <exn|e> a) : e a` (`lib/std/core/exn.kk:54-56`).
- Removal is "extension in the parameter, bare tail in the result". That is Effra's §2.3 shape.
- Koka's rows are scoped and duplicates are allowed: the tour instantiates `catch` with `<raise, raise>` (`doc/spec/tour.kk.md:1352-1364`).
- Closed effects are opened automatically (`src/Type/Operations.hs:83-98`), so `try` on an action that cannot throw type-checks. Koka has no presence check.
- The compiler still names `exn` (`src/Common/NamePrim.hs:316`; `src/Type/Kind.hs:48`).
- It lowers by evidence passing to C and JS.
- **Borrowed:** the extension pattern.
- **Not borrowed:** duplicate labels, which have no TypeScript meaning, or auto-opening, which defeats presence.

**Effect-TS** (`effect@66257d29`, v4.0.2; Effra pins 4.0.1).

- `catchTag` uses `K extends Tags<E>` for presence, and `ExcludeTag<E, K>` = `Exclude<E, { readonly _tag: K }>` for the rest (`packages/effect/src/Effect.ts:2812-2858`; `Types.ts:165`).
- `provideService` returns `Exclude<R, I>`, with no presence check (`Effect.ts:6456-6472`).
- `provide(Layer)` returns `E | E2, RIn | Exclude<R, ROut>` (`:6012-6056`).
- `timeout` returns `E | Cause.TimeoutError` and adds no service (`:4665-4673`).
- The label travels as a runtime value: a tag or a `Context.Key`.
- **This is the projection target** (§2.8), witness parameter included. Effra adds presence to `provide`.

**PureScript** (`purescript@cb3c496`).

- `Prim.Row` provides `Cons`, `Lacks`, `Union` and `Nub` (`src/Language/PureScript/Docs/Prim.hs:510-531`).
- Rows allow duplicates, so `Record.delete` needs `Lacks l r1 => Cons l a r1 r2` (`purescript-record@c89cd1a` `src/Record.purs:117-124`).
- `Variant.on` uses `Cons` (`purescript-variant@8b2bd73` `src/Data/Variant.purs:83-91`).
- `Run.catch` is library code (`purescript-run@abec7c3` `src/Run/Except.purs:126`), but Run's rows are higher-kinded.
- **The closest model:** `Cons` with `Lacks` implicit under sets. Effra takes `Cons` as the extension pattern and drops `Lacks`, because set rows make it free. It rejects Run's higher-kinded rows.

**Links** (`links@7100e28`).

- Rémy rows with presence types: `Present`, `Absent` and presence variables (`core/types.mli:150-154`).
- Handlers are a special form. The checker frees a handled operation's presence rather than deleting it (`core/typeSugar.ml:4447,4486`).
- A user `catch` is an ordinary function with `Fail{_}` (`tests/handlers.tests:386`).
- **Not borrowed.** Presence variables are a second kind with no TypeScript analogue.

**Roc** (`roc@3ee70f0c9`).

- Open tag unions (`[A, B, ..others]`) with no narrowing: a catch-all keeps the full union (`docs/langref/tag-unions.md:50-69`).
- `map_err` is library code.
- It monomorphizes.
- **Nothing borrowed for removal.** It confirms that open unions alone do not give removal.

**OCaml polymorphic variants** (`ocaml@6471927d`, `manual/src/tutorials/polyvariant.etex`).

- `[<`/`[>` bounds.
- Narrowing works only to a named finite set through `#t as x` (`:98-137`). Open matches do not narrow (`:62-70`).
- OCaml 5 effects are untyped.
- **Nothing borrowed.**

**Unison** (`unison@a1652e3a`).

- Abilities are sets. `handle` is a special form, and `pruneWanted` subtracts handled abilities assuming one occurrence each (`parser-typechecker/src/Unison/Typechecker/Context.hs:2789-2808`).
- Library handlers such as `Abort.toOptional! : '{g, Abort} a ->{g} Optional a` are ordinary code.
- It has no presence check.
- **The same set semantics as Effra.** Effra adds presence.

**Elixir** (`elixir@91ee75b`).

- `try` is a special form (`lib/elixir/lib/kernel/special_forms.ex:2343`).
- Set-theoretic types type a rescued variable but put no raised-exception rows in signatures (`lib/elixir/lib/module/types/expr.ex:520-557`).
- **Supports the residue side of the verdict:** a fixed form for evaluation and lifetime. It is not evidence about checked rows.

**MoonBit** (`moonbit-docs@8d9f3ba`).

- `raise`, `try`/`catch` and `noraise` are syntax, with one error type per function and a `raise?` polymorphism bit (`next/language/error-handling.md:119-146`).
- A catch does not narrow: several error types collapse to `Error` (`:222-227`).
- `with_timeout` is library code over async (`moonbitlang/async@e72901a` `src/async.mbt:96-100`), because MoonBit tracks no ownership.
- **Supports the D1 residue** for a checked-error language without handlers. Its library timeout shows that the timeout residue comes from Effra's ownership checking, not from rows.

**Gleam** (`gleam-lang/stdlib@13359e5`).

- `Result` is library code: `result.try`, `map_error` and `try_recover` (`src/gleam/result.gleam:83,143,397`). There are no effect rows.
- `use` is callback sugar.
- **Confirms the library side** when no row needs removing.

**ReScript** (`rescript-lang.org@a62233b`).

- Exceptions are untyped (`exception.mdx:9-40`).
- Polymorphic variants narrow only to named types (`polymorphic-variant.mdx:146-180`).
- **Nothing borrowed.**

**The route Effra matches** has three parts:

- **Unison's set semantics** for rows;
- **PureScript's `Cons` extension pattern** for removal, with presence and `Lacks` implicit;
- **Effect's `catchTag`/`provideService` shape** as the TypeScript meaning, with the label passed as a value witness.

Like Koka, Effra keeps a construct for what is control or lifetime rather than type: Koka's `handler`, Effra's timeout and provision owners. Unlike Koka, removal needs no handler syntax, because the runtime helpers are the handlers.

## 5. Ladder check

| Candidate feature | Describable without naming the construct, with at least two clients | Readable with Go's simplicity | Same meaning in TypeScript, no `any` | Erases into Go | Rung |
| --- | --- | --- | --- | --- | --- |
| F1 explicit type arguments | Yes: catch, recover, provide, `witness` | Yes: Go `f[T](x)` with prefix lists | Yes: `f<T>(x)`, or a witness when TypeScript cannot take partial lists | Yes: Go generics | 2 |
| F2 label parameters with tag witnesses | Yes: catch, recover, provide, user combinators | Yes: one kind word, `E: error` | Yes: `K extends F["_tag"]` plus `tag: K` (Effect's shape) | Yes: a hidden string, the payload as a Go type parameter | 2 |
| F3 extension patterns with presence | Yes: every removal, plus user functions such as `recoverAll` | Yes: "a label beside a row variable must be present" | Yes: `Exclude` plus a presence guard (probe C) | Yes: rows erase | 2 |
| F4 transparent parametric summaries | Yes: every bundled intrinsic recipe transformer (catch, recover, provide, future `mapError`/`orElse`) | Yes: invisible in source, stated once in the intrinsic contract | Not applicable: the projection carries no ownership | Not applicable: checker-only | 2 (checker rule, keyed on "intrinsic returning a recipe", not on a name) |
| Callee-scoped recipe parameter (timeout) | One client | Yes | No counterpart | Yes | 4: keep the construct |
| First-class `Layer<Out, Err, In>` | Rows only; the owner and the static graph stay | Yes | Yes (Effect's shape) | Not carried | 4: keep the construct |
| Pending labels passing through the edge | Yes | Yes | **No:** the `.d.mts` row would understate the runtime row | Yes | Rejected; refuse at the argument |
| Rémy presence variables or scoped labels | Yes | No: a second kind of row content | No | Yes | Rejected |

## 6. Recommendation

**GO for phase 2**, as F1 through F4 plus three library functions:

- `.catch` first;
- then `.provide<S>`;
- then `.recover`.

**The residue that cannot be general,** with the exact fact for each:

1. **`.timeout(ms)`: the timeout owner.**
   - The callee opens and closes the owner of its argument's execution.
   - Values acquired inside the timed work are released at the edge, and the checker must refuse their use afterwards (`semantic.go:5003`).
   - No type feature with two clients carries it. It is lifetime (rung 4).
2. **`.provide(Layer)`: the provision owner and the static layer plan.**
   - The owner (`layers.go:626-634`) is lifetime.
   - Graph, lint and inspection read the plan's identity (rung 4, "tooling must read the structure").
3. **`.orFail()`: the predeclared `GoResult` shape.** It becomes library when `GoResult` is ordinary generic data. Its rows already are.

D1 stands for these three only. The postfix-word reservation (T1) becomes the ordinary receiver-method rule for `catch`, `provide` and `recover` (§2.10). It stays a construct reservation for `timeout` and `orFail`.

**Behaviour changes phase 2 brings,** each to be listed with the commit that causes it:

- **Presence on services.** Providing an unused service is refused, which changes `examples/causal.ef:47`.
  - `emit_go.go:324-326` injects the scheduler by provider name (audit 1-H). So removing `.provide<Scheduler>(TestScheduler)` there must be checked for a runtime change.
  - If it does change behaviour, that line keeps the provide and the example adds the requirement it really uses.
- **Pending recipes at an argument.**
  - 39 corpus programs change their refusal text and location.
  - 2 programs accepted today are refused: `ownership_sweep/m3_nonrec.ef` and `m4_rec_simple.ef`. Recovering a recipe whose unjoined child may fail is refused where the recipe crosses into `catch`; today it is accepted with the label kept.
  - This matches the documented rule that pending evidence ends at type boundaries.
- **`recovery_test.go:165` flips from refused to accepted.** That is the purpose of the change.
- **The generic EF106 argument message improves** for every generic call (§2.7).

## 7. Phase-2 plan

Each commit compiles and passes the gate. The `.catch` series (commits 1–6) is a self-contained unit, and the spec allows splitting there.

1. **Row extension patterns (F3).**
   - Add the presence check in `inferRows`.
   - Remove the abstract-subtraction refusal for literally present labels (`callables.go:337-343`).
   - Drop the EF125 refusals at `semantic.go:5055,5072`, `recovery.go:21` and `layers.go:615` when the label is literally present. They become the presence diagnostic otherwise.
   - Render source names for row parameters.
   - Tests:
     - `recovery_test.go:165` becomes positive;
     - negative controls for presence, closed versus open removal, and a user `recoverAll`;
     - the `Exclude` projection, finally exercised.
2. **Explicit type arguments (F1).**
   - Parser lookahead for `name<types>(`, plus formatter and LSP spans.
   - Bind type-parameter prefixes in `inferTypeArguments`.
   - Tests for the comparison-chain disambiguation.
3. **Label parameters and tag witnesses (F2).**
   - Add the `error` and `service` kinds and the `Provider<S>` source type.
   - Hidden witness arguments on Go and JS.
   - TypeScript witness parameters.
   - Tests on both targets, including a user function with an `error` parameter.
4. **Transparent parametric summaries (F4)**, the riskiest commit.
   - A generic intrinsic checker case with no name switch.
   - Bundled type-parameter ownership: retire EF127 "field layout of an unresolved type parameter" on this path.
   - Acceptance: the ownership corpus run through `.catch`, implemented as an intrinsic library function, keeps every verdict and diagnostic except the listed pending moves.
5. **`effra/effect` with `catch` as an intrinsic, plus the dot-call resolution.**
   - Call-site lowering of recipe intrinsics.
   - n-parameter `admitBundledIntrinsic`.
   - Delete the catch parser form, checker case, emitter cases (`emit.go:700`, `emit_go.go:1397`), the `graph.go:474` look-through and the JS chunk mapping keyed on the kind (re-keyed on the intrinsic).
   - A grep test that pins no `"catch"` kind or name branch in the checker, emitters or reachability.
   - Go and JS byte-equivalence over every example and conformance program, modelled on `pipe_equivalence_test.go`.
6. **TypeScript projection.**
   - Presence guards, witness parameters, and an `effra/effect` declaration (Q3).
   - A strict `tsc` consumer fixture for catch and the user `recoverAll`.
   - Refusal diagnostics for unprojectable rows (#71).
7. **`provide<S>`.** It moves to the library; presence on services; `examples/causal.ef`; graph and lint re-keyed on the intrinsic call.
8. **`recover` and `handle`.** They move to the library, and `recovery.go` is deleted except for helpers F4 reuses.
9. **Docs** (phase 3):
   - the NORTH_STAR bullet with the owner's 2026-10-11 clarification and the ladder;
   - audit D1 narrowed to §6's residue;
   - GLOSSARY: label parameter, extension pattern, tag witness, transparent intrinsic;
   - CHANGELOG.
10. **Measurements** (acceptance item 5): checker cost on the examples and on the #109 wide-record shape, before and after, at recorded load.

## 8. Cost estimate

The work is size L, larger than the spec's single unit if all three functions land.

| Commits | Scope | Estimate |
| --- | --- | --- |
| 1 | F3 | S, about 0.5 day |
| 2 | F1 | M, about 1 day (parser and formatter) |
| 3 | F2 | M, 1–1.5 days (two targets) |
| 4 | F4 | L, 2–3 days. This is where the risk sits: the ownership machinery around type parameters is unsupported today (EF127), and its corpus is large (143 sweep and probe programs). |
| 5–6 | catch library, emission and projection | M, about 1.5 days |
| 7–8 | provide and recover | M, about 1.5 days |
| 9–10 | docs and measurements | S, about 0.5 day |

The total is about 8–10 implementor-days plus review rounds.

**Recommendation:** split the unit. The first unit is commits 1–6 plus the docs for `.catch`. A second unit takes `provide` and `recover`. If commit 4 fails its corpus acceptance, the recorded outcome is "rows general, evidence not". D1 then stands for all three forms, with F1–F3 still worth keeping for user combinators over abstract rows.

## 9. Open questions

- **Q1.** May the language name `effra/effect` as the owner module of the predeclared `Effect` type, so `.catch` keeps its dot spelling through the receiver-method rule? The fallback is a mechanical migration to the pipe.
- **Q2.** What is the name of the pure-handler recovery function: `handle`, `recoverWith` or something else?
- **Q3.** Where do bundled library declarations live in the `.d.mts`: a generated `effra/effect` declaration module, or inline in each consuming module?
- **Q4.** Is the pending-at-argument behaviour change (two corpus programs refused) acceptable, as this note recommends? The alternative is keeping `.catch` a construct until a pending-carrying type exists. The `.d.mts` projection could not express that type.
- **Q5.** Should `Scheduler` stop being charged by timeout (audit 1-G) when the timeout owner question is next taken up? Effect charges no service for timeout.
