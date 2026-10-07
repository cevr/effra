# Regular language abstractions for the bundled library

Status: authorized extension of the Wayfinder implementation goal, 2026-10-06. The owner asks for Go-like simplicity, Effect-style guarantees and Borgo-like ADTs/abstractions. Upstream source review and independent counsel identify concrete missing mechanisms; exact new syntax remains subject to executable examples and review.

## Design decisions

Keep `run` as the visible execution of a lazy effect recipe. A fiber is already running; a recipe is reusable program data. Preserve ordinary control flow and explicit public function success/failure/service contracts. Local function inference may remove redundant spelling, but it cannot silently widen a public row. Owner-directed [layer inference](layers.md) is a separate explicit rule: optional annotations bound inferred construction contracts.

Use one canonical type representation for nominal identity, applications, function parameters/results and effect rows. String display forms are derived output, not a second semantic authority. Share repeated type structure and keep provenance/evaluation analyses bounded independently; interning types alone does not bound an analysis that enumerates paths through them.

Separate the value produced by an expression from the failures/services incurred while evaluating it. A named effect-function value, a constructed recipe and an executed result are different contracts. Compute evaluation contributions once per checked node rather than repeatedly walking nested subtrees.

Carried callable, recipe and fiber rows belong to their canonical type identities; expression evaluation rows are separate facts. A branch join or record projection cannot replace a carried contract with its own evaluation row. Final checked facts must not accidentally retain an incomplete summary prepass. Named function values also contribute resolved declaration dependencies, including when passed as arguments; respect lexical shadowing when ordering summaries and selecting emission roots.

Canonical type positions serialize as bounded references to shared definitions, including nested callable parameters/results and application slots. Keeping a recursive legacy tree beside interned IDs defeats that boundary. Explicitly version the changed inspection schema and retain readable type displays as derived output. Qualify nominal declarations independently from their display names; interface content/version and compiler producer identity remain separate snapshot inputs.

Add ordinary typed function values and explicit finite row parameters together. A reusable helper must preserve its argument's failure/service requirements without a checker branch named after that helper. Instantiate row parameters from argument contracts, support finite union and concrete-label elimination, and reject unsupported ambiguous inference. General conditional type computation and whole-program inference are outside this design.

## Immediate soundness regression

The existing opaque `Handler` accepts a function requiring a service and loses that requirement when passed through a helper parameter. A public program with `route uses {Users}`, `serveIt(h: Handler) uses {Http}` and `main = run serveIt(route).provide<Http>(GoHttp)` currently checks despite missing Users. Independent counsel executed the resulting server and observed a missing-service defect; root independently verified the unchecked requirement in CLI inspection.

Further independent source probes at `4f6aed8` admit the same missing requirement through `if` selection of a handler and through a record's Handler field, while a direct local alias preserves the requirement. These are distinct acceptance cases for the common representation; a special repair to Http.serve's helper parameter alone is insufficient.

Replace that erasure with the common function contract. Re-express the current HTTP handler through it and diagnose missing services through the value-flow chain. Preserve existing correctly provisioned HTTP examples. A legacy Handler spelling may remain only as a sound explicit contract; it cannot erase arbitrary rows.

Acceptance:

- The missing-Users program rejects before build, including aliases and helper forwarding.
- A function with narrower failures/services can satisfy a declared wider callback contract; the reverse rejects. Check parameters and success types soundly as well.
- Function contracts survive supported locals, parameters, provider configuration and record fields; any unsupported placement diagnoses.
- Forward calls and generic helper composition preserve row contributions. Concrete catch/provide operations remove only their named labels; abstract-label subtraction must be explicitly supported or diagnosed.
- Borrowed input provenance remains distinct from newly acquired invocation results through function calls. Named functions are the first supported values; closures require explicit capture checking before admission.
- Unresolved callback-result provenance remains a deferred callable relation or conservative potential ownership, never harmless unknown. Paired borrowed/acquired controls cross multiple forwarding levels and declaration-order permutations; supported conditional callees preserve every possible acquisition. A scope inside a generic helper may conservatively diagnose unresolved ownership until a checked obligation model is available.
- CLI/MCP expose the same canonical function/application/row contracts and useful missing-requirement paths.
- Existing source checks, Go/JS behavior and public diagnostics remain truthful. Adversarial nested types and helper fan-out have bounded checking receipts.

## Subsequent reusable data and recovery

First-order type parameters, ordinary generic closed enums and containers should support Option/Result and reusable collections without one compiler case per library type. Retain explicit boundary signatures and supported-layout diagnostics. Pure data outcomes and an effect's execution failure remain distinct; a future Result propagation operator must not silently execute effects.

The owner prohibits nil/null language values. Required fields and callable results contain initialized admitted values; absence uses a closed alternative, including bundled `Option<T>`. Do not add nullable unions, implicit zero-value construction or unchecked `Some` contents when introducing generics or host types. [Absence and host boundaries](absence-and-host-boundaries.md) defines native nil/error adaptation and its separate wire-protocol meaning. Ordinary generic records and closed enums use the common finite template owner, complete application arguments, constructor substitution and exhaustive single-subject match evidence. Bundled Option/Result are ordinary instances of that contract; richer host adaptation and typed recipe containers remain separate implementation obligations.

Multi-subject matching and named variant or-patterns provide reusable state/event decision functions. Diagnose missing pairs, unreachable arms and inconsistent alternative bindings through the ordinary matcher. Keep coverage and type instantiation bounded; exhausting a budget is a diagnostic, never an empty requirement row or a false exhaustiveness proof. Generic data and payload-aware recovery now have explicit Wayfinder tasks because the declaration-only [machine contract](state-machines.md) needs these ordinary mechanisms.

### Implemented product matches and alternatives

`match a, b { … }` evaluates its subjects once, left to right. Each arm names one pattern cell per subject, separated by commas; a cell lists named-variant alternatives separated by `|`, which binds tighter than the comma: `State.Idle | State.Active { key }, Event.Close => …`. Two real callers chose this spelling: a session state × event step table, and `Data.Option` × `Data.Result` (or a generic `Reply<T>`) absence pairing. It reads like the arm list it replaces, needs no tuple value and keeps the existing variant-pattern syntax. Catch-all cells remain rejected under the closed-data policy.

Alternatives in one cell bind the same names with identical payload types, callable failure and service rows included: rows are never unioned or taken from one alternative, so `A { f } | B { f }` over `effect fn() -> string` and `effect fn() -> string raises {Bad}` payloads is `EF121`. The bound value joins every alternative's payload ownership, captures and callable evidence. Names stay distinct across cells. The checker computes usefulness over closed variant sets (Maranget-style specialization, with variants that select the same arms explored once): missing combinations report declaration-ordered witnesses such as `missing match arm for Session.Closed, Event.Close`, at most eight with an explicit omission notice; unreachable arms and unreachable or duplicate alternatives are `EF117`; binder errors are `EF121`. One work budget (`maxMatchCoverageWork`) covers every question for a match. Exhausting it is `EF137`, which refuses the match without any coverage claim or plan.

The checked first-match plan holds subjects, per-arm cells and binders. Go and JavaScript both lower that plan: subjects into temporaries in order, each tested subject's variant decoded once, then ordered arm tests sharing one emitted body per arm. Neither backend re-derives coverage.

Payload-aware effectful recovery binds declared error fields, eliminates exactly the handled labels and adds the selected handler's rows. Preserve current behavior that a composite cause containing cleanup defects cannot be flattened into a recoverable expected failure. Reifying an outcome requires `Exit` success or failure with its full `Cause`; a four-way success/failure/defect/interruption enum alone loses composite causes and is insufficient.

Scoped acquisition must make acquisition plus finalizer registration cancellation-safe. Its surface should use the common typed-function/library interface where that is sufficient; a new keyword needs evidence that the ordinary interface cannot express the invariant. Provider sharing should remain explicit through recipes and materialized values; fallible acquisition needs rollback and completed release, and its failure-retention policy cannot be inferred from a generic memoization name.

These are finite next implementation contracts, not claims that collections, effectful recovery, public acquisition or fallible shared providers already exist. Each needs its own source cases, negative tests, both-target matrix and upstream behavior mapping before closure.

## Performance and restraint

Measure canonical checking, import reuse, emission and generated runtime separately. Compare enum representations on matched codec/server workloads before changing them; an interface representation is not automatically slower and a flattened variant struct can waste space. Keep raw losing results. Avoid renaming established syntax merely to anticipate an unused feature; introduce data-method syntax only with real callers.

The [native server specification](native-server-contracts.md) consumes these mechanisms. Routing, retry, caching, queues and RPC framing remain library policies behind ordinary checked interfaces. No route-specific syntax, implicit dependency search or unchecked host conversions are introduced by this spec.
